package skillsync

import (
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/AllenMuu/skill-manager/internal/operation"
)

const (
	UpdateStatusLocal       = "local"
	UpdateStatusAvailable   = "available"
	UpdateStatusCurrent     = "up-to-date"
	UpdateStatusModified    = "modified-locally"
	UpdateStatusUnsupported = "unsupported"
	UpdateStatusFailed      = "failed"

	maxUpdateCandidates    = 200
	maxTreeResponse        = 16 << 20
	maxSkillFiles          = 256
	maxSkillFileBytes      = 2 << 20
	maxSkillTotalBytes     = 8 << 20
	maxPreparedUpdateBytes = 64 << 20
)

var (
	ErrInvalidUpdateSelection = errors.New("invalid upstream Skill update selection")
	ErrStaleUpdatePlan        = errors.New("upstream Skill update plan changed; check and review again")
	ErrUpdatePlanTooLarge     = errors.New("selected upstream Skill updates exceed the 64 MiB review-plan limit")
	ownerRepoPattern          = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,99}$`)
)

type UpdateCheck struct {
	CheckedAt time.Time    `json:"checkedAt"`
	Items     []UpdateItem `json:"items"`
}

type UpdateItem struct {
	ID               string    `json:"id"`
	Identifier       string    `json:"identifier"`
	Scope            Scope     `json:"scope"`
	Path             string    `json:"path"`
	Origin           string    `json:"origin,omitempty"`
	OriginType       string    `json:"originType,omitempty"`
	Ref              string    `json:"ref,omitempty"`
	Status           string    `json:"status"`
	CurrentHash      string    `json:"currentHash,omitempty"`
	UpstreamHash     string    `json:"upstreamHash,omitempty"`
	CheckedAt        time.Time `json:"checkedAt"`
	Problem          string    `json:"problem,omitempty"`
	CanUpdate        bool      `json:"canUpdate"`
	localHash        string
	hashKind         string
	lockPath         string
	lockKey          string
	lockFingerprint  string
	localFingerprint string
}

type UpdatePreview struct {
	Items []UpdatePreviewItem `json:"items"`
}

type UpdatePreviewItem struct {
	ID           string `json:"id"`
	Identifier   string `json:"identifier"`
	Scope        Scope  `json:"scope"`
	Origin       string `json:"origin"`
	Ref          string `json:"ref"`
	Path         string `json:"path"`
	CurrentHash  string `json:"currentHash"`
	UpstreamHash string `json:"upstreamHash"`
}

// PreparedUpdatePlan retains bounded remote content in memory for one
// session-bound confirmation plan. The payload is intentionally not serialized.
type PreparedUpdatePlan struct {
	Preview      UpdatePreview
	items        []preparedUpdateItem
	contentBytes int64
}

func (plan *PreparedUpdatePlan) appendItem(item preparedUpdateItem) error {
	var itemBytes int64
	for _, file := range item.files {
		itemBytes += int64(len(file.Content))
	}
	if itemBytes > maxPreparedUpdateBytes || plan.contentBytes > maxPreparedUpdateBytes-itemBytes {
		return ErrUpdatePlanTooLarge
	}
	plan.contentBytes += itemBytes
	plan.items = append(plan.items, item)
	return nil
}

type preparedUpdateItem struct {
	check  UpdateItem
	files  []remoteFile
	remote remoteSkillTree
}

type githubClient struct {
	client  *http.Client
	apiBase string
	rawBase string
}

type githubTreeResponse struct {
	Truncated bool              `json:"truncated"`
	Tree      []githubTreeEntry `json:"tree"`
}

type githubTreeEntry struct {
	Path string `json:"path"`
	Mode string `json:"mode"`
	Type string `json:"type"`
	SHA  string `json:"sha"`
	Size int64  `json:"size"`
}

type remoteSkillTree struct {
	OwnerRepo string
	Ref       string
	Folder    string
	TreeHash  string
	Files     []githubTreeEntry
}

type remoteFile struct {
	Path    string
	Mode    string
	Content []byte
}

func newGitHubClient() *githubClient {
	return &githubClient{
		client: &http.Client{
			Timeout: 10 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		apiBase: "https://api.github.com",
		rawBase: "https://raw.githubusercontent.com",
	}
}

// CheckUpdates contacts GitHub only for eligible Skills whose supported Skills
// CLI lock entry contains a comparable hash and repository path. Inventory and
// source discovery themselves remain local and offline.
func (s *Service) CheckUpdates(ctx context.Context) (UpdateCheck, error) {
	inventory, _, err := s.Inventory()
	if err != nil {
		return UpdateCheck{}, err
	}
	if len(inventory.Sources) > maxUpdateCandidates {
		return UpdateCheck{}, fmt.Errorf("too many Skill sources to check in one request")
	}
	now := s.currentTime().UTC()
	result := UpdateCheck{CheckedAt: now, Items: make([]UpdateItem, 0, len(inventory.Sources))}
	treeCache := map[string]remoteSkillTree{}
	for _, source := range inventory.Sources {
		if !source.Eligible || source.AlreadyManaged {
			continue
		}
		item := UpdateItem{
			ID: source.ID, Identifier: source.Identifier, Scope: source.Scope, Path: source.Path,
			Origin: source.Origin, OriginType: source.OriginType, Ref: source.OriginRef, CheckedAt: now,
			CurrentHash: source.StoredHash, hashKind: source.HashKind, lockPath: source.LockPath, lockKey: source.LockKey,
		}
		if source.OriginType == "local" {
			item.Status = UpdateStatusLocal
			item.Problem = "Local source"
			result.Items = append(result.Items, item)
			continue
		}
		if source.OriginType != "github" {
			item.Status = UpdateStatusUnsupported
			item.Problem = "Only hash-tracked GitHub Skills are supported"
			result.Items = append(result.Items, item)
			continue
		}
		ownerRepo, ref, problem := validateGitHubOrigin(source.Origin, source.OriginRef, source.OriginSkillPath, source.StoredHash, source.HashKind)
		if problem != "" {
			item.Status = UpdateStatusUnsupported
			item.Problem = problem
			result.Items = append(result.Items, item)
			continue
		}
		localHash, localErr := hashLocalSkill(source.Path, source.HashKind)
		if localErr != nil {
			item.Status = UpdateStatusFailed
			item.Problem = "The installed Skill content could not be safely compared"
			result.Items = append(result.Items, item)
			continue
		}
		item.localHash = localHash
		if source.HashKind == "content" {
			fileCount, countErr := localHashFileCount(source.Path, source.HashKind)
			if countErr != nil || fileCount != 1 {
				item.Status = UpdateStatusUnsupported
				item.Problem = "Project lock hashes can only be compared safely for single-file Skills"
				result.Items = append(result.Items, item)
				continue
			}
		}
		if localHash != source.StoredHash {
			item.Status = UpdateStatusModified
			item.Problem = "Installed files differ from the recorded install hash"
			result.Items = append(result.Items, item)
			continue
		}
		cacheKey := ownerRepo + "@" + ref + "#" + source.OriginSkillPath
		remote, cached := treeCache[cacheKey]
		if !cached {
			remote, err = s.github.skillTree(ctx, ownerRepo, ref, source.OriginSkillPath)
			if err != nil {
				item.Status = UpdateStatusFailed
				item.Problem = safeRemoteError(err)
				result.Items = append(result.Items, item)
				continue
			}
			treeCache[cacheKey] = remote
		}
		latestHash := remote.TreeHash
		if source.HashKind == "content" {
			files, fetchErr := s.github.fetchSkillFiles(ctx, remote)
			if fetchErr != nil {
				item.Status = UpdateStatusFailed
				item.Problem = safeRemoteError(fetchErr)
				result.Items = append(result.Items, item)
				continue
			}
			if len(files) != 1 {
				item.Status = UpdateStatusUnsupported
				item.Problem = "Project lock hashes can only be compared safely for single-file Skills"
				result.Items = append(result.Items, item)
				continue
			}
			latestHash = hashRemoteContent(files)
		}
		item.UpstreamHash = latestHash
		if latestHash == source.StoredHash {
			item.Status = UpdateStatusCurrent
		} else {
			item.Status = UpdateStatusAvailable
			item.CanUpdate = true
		}
		result.Items = append(result.Items, item)
	}
	sort.Slice(result.Items, func(i, j int) bool { return result.Items[i].ID < result.Items[j].ID })
	return result, nil
}

// PrepareUpdates fetches selected update content into a bounded, non-persistent
// plan. It never changes a Skill directory or lock file.
func (s *Service) PrepareUpdates(ctx context.Context, selectedIDs []string) (PreparedUpdatePlan, error) {
	if len(selectedIDs) == 0 || len(selectedIDs) > maxUpdateCandidates {
		return PreparedUpdatePlan{}, fmt.Errorf("%w: select one or more available updates", ErrInvalidUpdateSelection)
	}
	seen := map[string]bool{}
	for _, id := range selectedIDs {
		if id == "" || strings.TrimSpace(id) != id || seen[id] {
			return PreparedUpdatePlan{}, ErrInvalidUpdateSelection
		}
		seen[id] = true
	}
	checked, err := s.CheckUpdates(ctx)
	if err != nil {
		return PreparedUpdatePlan{}, err
	}
	available := make(map[string]UpdateItem, len(checked.Items))
	for _, item := range checked.Items {
		if item.CanUpdate {
			available[item.ID] = item
		}
	}
	for _, id := range selectedIDs {
		if _, ok := available[id]; !ok {
			return PreparedUpdatePlan{}, fmt.Errorf("%w: selected source is no longer an available update", ErrInvalidUpdateSelection)
		}
	}
	inventory, _, err := s.Inventory()
	if err != nil {
		return PreparedUpdatePlan{}, err
	}
	sources := make(map[string]Source, len(inventory.Sources))
	for _, source := range inventory.Sources {
		sources[source.ID] = source
	}
	plan := PreparedUpdatePlan{Preview: UpdatePreview{Items: make([]UpdatePreviewItem, 0, len(selectedIDs))}}
	for _, id := range selectedIDs {
		item := available[id]
		source, ok := sources[id]
		if !ok || !source.Eligible || source.AlreadyManaged {
			return PreparedUpdatePlan{}, ErrStaleUpdatePlan
		}
		ownerRepo, ref, problem := validateGitHubOrigin(source.Origin, source.OriginRef, source.OriginSkillPath, source.StoredHash, source.HashKind)
		if problem != "" {
			return PreparedUpdatePlan{}, ErrStaleUpdatePlan
		}
		remote, err := s.github.skillTree(ctx, ownerRepo, ref, source.OriginSkillPath)
		if err != nil {
			return PreparedUpdatePlan{}, err
		}
		files, err := s.github.fetchSkillFiles(ctx, remote)
		if err != nil {
			return PreparedUpdatePlan{}, err
		}
		if source.HashKind == "git-tree" && remoteHasExcludedHashPath(remote) {
			return PreparedUpdatePlan{}, errors.New("upstream Skill contains a directory that cannot be safely updated")
		}
		latestHash := remote.TreeHash
		if source.HashKind == "content" {
			latestHash = hashRemoteContent(files)
		}
		if latestHash != item.UpstreamHash || latestHash == source.StoredHash {
			return PreparedUpdatePlan{}, ErrStaleUpdatePlan
		}
		localHash, err := hashLocalSkill(source.Path, source.HashKind)
		if err != nil || localHash != source.StoredHash {
			return PreparedUpdatePlan{}, ErrStaleUpdatePlan
		}
		localFingerprint, err := operation.FingerprintPath(source.Path)
		if err != nil {
			return PreparedUpdatePlan{}, ErrStaleUpdatePlan
		}
		lockFingerprint, err := fingerprintRegularFile(source.LockPath)
		if err != nil {
			return PreparedUpdatePlan{}, ErrStaleUpdatePlan
		}
		item.lockFingerprint = lockFingerprint
		item.localFingerprint = localFingerprint
		if err := plan.appendItem(preparedUpdateItem{check: item, files: files, remote: remote}); err != nil {
			return PreparedUpdatePlan{}, err
		}
		plan.Preview.Items = append(plan.Preview.Items, UpdatePreviewItem{
			ID: item.ID, Identifier: item.Identifier, Scope: item.Scope, Origin: item.Origin, Ref: remote.Ref,
			Path: item.Path, CurrentHash: item.CurrentHash, UpstreamHash: latestHash,
		})
	}
	sort.Slice(plan.Preview.Items, func(i, j int) bool { return plan.Preview.Items[i].ID < plan.Preview.Items[j].ID })
	return plan, nil
}

func validateGitHubOrigin(origin, ref, skillPath, hash, hashKind string) (string, string, string) {
	parts := strings.Split(origin, "/")
	if len(parts) != 2 || !ownerRepoPattern.MatchString(parts[0]) || !ownerRepoPattern.MatchString(parts[1]) || parts[0] == "." || parts[0] == ".." || parts[1] == "." || parts[1] == ".." {
		return "", "", "GitHub source must be recorded as owner/repository"
	}
	if len(ref) > 200 || strings.ContainsAny(ref, "\x00\r\n") {
		return "", "", "Recorded GitHub ref is invalid"
	}
	if _, err := skillFolderPath(skillPath); err != nil {
		return "", "", "A valid GitHub Skill path was not recorded"
	}
	if (hashKind == "git-tree" && !isHexHash(hash, 40)) || (hashKind == "content" && !isHexHash(hash, 64)) || (hashKind != "git-tree" && hashKind != "content") {
		return "", "", "A comparable install hash was not recorded"
	}
	return origin, ref, ""
}

func isHexHash(value string, length int) bool {
	if len(value) != length {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func skillFolderPath(value string) (string, error) {
	if value == "" || strings.ContainsAny(value, "\\\x00\r\n") || strings.HasPrefix(value, "/") {
		return "", errors.New("invalid relative Skill path")
	}
	clean := path.Clean(value)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", errors.New("Skill path escapes its source repository")
	}
	if path.Base(clean) == "SKILL.md" {
		clean = path.Dir(clean)
	}
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", errors.New("Skill folder path is invalid")
	}
	return clean, nil
}

func (c *githubClient) skillTree(ctx context.Context, ownerRepo, ref, skillPath string) (remoteSkillTree, error) {
	folder, err := skillFolderPath(skillPath)
	if err != nil {
		return remoteSkillTree{}, err
	}
	requestedRef := ref
	if requestedRef == "" {
		requestedRef = "main"
	}
	response, err := c.fetchRepoTree(ctx, ownerRepo, requestedRef)
	if err != nil && ref == "" && requestedRef == "main" {
		response, err = c.fetchRepoTree(ctx, ownerRepo, "master")
		if err == nil {
			requestedRef = "master"
		}
	}
	if err != nil {
		return remoteSkillTree{}, err
	}
	if response.Truncated {
		return remoteSkillTree{}, errors.New("GitHub repository tree was truncated")
	}
	var folderHash string
	files := make([]githubTreeEntry, 0)
	prefix := folder + "/"
	for _, entry := range response.Tree {
		if entry.Path == folder && entry.Type == "tree" {
			folderHash = entry.SHA
		}
		if strings.HasPrefix(entry.Path, prefix) {
			files = append(files, entry)
		}
	}
	if !isHexHash(folderHash, 40) {
		return remoteSkillTree{}, errors.New("recorded Skill folder was not found upstream")
	}
	return remoteSkillTree{OwnerRepo: ownerRepo, Ref: requestedRef, Folder: folder, TreeHash: strings.ToLower(folderHash), Files: files}, nil
}

func (c *githubClient) fetchRepoTree(ctx context.Context, ownerRepo, ref string) (githubTreeResponse, error) {
	parts := strings.Split(ownerRepo, "/")
	if len(parts) != 2 {
		return githubTreeResponse{}, errors.New("invalid GitHub repository")
	}
	endpoint := strings.TrimRight(c.apiBase, "/") + "/repos/" + url.PathEscape(parts[0]) + "/" + url.PathEscape(parts[1]) + "/git/trees/" + url.PathEscape(ref) + "?recursive=1"
	body, err := c.get(ctx, endpoint, maxTreeResponse, "application/vnd.github+json")
	if err != nil {
		return githubTreeResponse{}, err
	}
	var result githubTreeResponse
	if err := json.Unmarshal(body, &result); err != nil || result.Tree == nil {
		return githubTreeResponse{}, errors.New("GitHub returned an invalid repository tree")
	}
	return result, nil
}

func (c *githubClient) fetchSkillFiles(ctx context.Context, tree remoteSkillTree) ([]remoteFile, error) {
	files := make([]remoteFile, 0)
	totalBytes := int64(0)
	for _, entry := range tree.Files {
		if entry.Type == "tree" {
			continue
		}
		if entry.Type != "blob" || (entry.Mode != "100644" && entry.Mode != "100755") {
			return nil, fmt.Errorf("upstream Skill contains an unsupported filesystem entry: %s", entry.Path)
		}
		relative := strings.TrimPrefix(entry.Path, tree.Folder+"/")
		if !validRemoteRelativePath(relative) {
			return nil, errors.New("upstream Skill contains an invalid file path")
		}
		if hasExcludedHashComponent(relative) {
			continue
		}
		if len(files) >= maxSkillFiles || entry.Size < 0 || entry.Size > maxSkillFileBytes || totalBytes+entry.Size > maxSkillTotalBytes {
			return nil, errors.New("upstream Skill exceeds supported update size limits")
		}
		content, err := c.fetchRawFile(ctx, tree, relative)
		if err != nil {
			return nil, err
		}
		if int64(len(content)) != entry.Size {
			return nil, errors.New("upstream Skill file size changed while downloading")
		}
		totalBytes += int64(len(content))
		files = append(files, remoteFile{Path: relative, Mode: entry.Mode, Content: content})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	for _, file := range files {
		if file.Path == "SKILL.md" {
			return files, nil
		}
	}
	return nil, errors.New("upstream Skill has no SKILL.md")
}

func (c *githubClient) fetchRawFile(ctx context.Context, tree remoteSkillTree, relative string) ([]byte, error) {
	parts := strings.Split(tree.OwnerRepo, "/")
	segments := []string{parts[0], parts[1], tree.Ref}
	segments = append(segments, strings.Split(path.Join(tree.Folder, relative), "/")...)
	for i := range segments {
		segments[i] = url.PathEscape(segments[i])
	}
	endpoint := strings.TrimRight(c.rawBase, "/") + "/" + strings.Join(segments, "/")
	return c.get(ctx, endpoint, maxSkillFileBytes, "text/plain")
}

func (c *githubClient) get(ctx context.Context, endpoint string, limit int64, accept string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, errors.New("could not prepare upstream request")
	}
	req.Header.Set("Accept", accept)
	req.Header.Set("User-Agent", "Agent-Manager-Skill-Update-Check")
	response, err := c.client.Do(req)
	if err != nil {
		return nil, errors.New("upstream request failed")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("upstream returned HTTP %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil || int64(len(body)) > limit {
		return nil, errors.New("upstream response exceeded supported size limits")
	}
	return body, nil
}

func hashRemoteContent(files []remoteFile) string {
	ordered := append([]remoteFile(nil), files...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Path < ordered[j].Path })
	hash := sha256.New()
	for _, file := range ordered {
		_, _ = hash.Write([]byte(file.Path))
		_, _ = hash.Write(file.Content)
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func remoteHasExcludedHashPath(tree remoteSkillTree) bool {
	for _, entry := range tree.Files {
		relative := strings.TrimPrefix(entry.Path, tree.Folder+"/")
		if hasExcludedHashComponent(relative) {
			return true
		}
	}
	return false
}

func safeRemoteError(err error) string {
	message := err.Error()
	if strings.Contains(message, "HTTP ") {
		return "The upstream did not return a successful response"
	}
	return message
}

func validRemoteRelativePath(value string) bool {
	if value == "" || strings.ContainsAny(value, "\\\x00\r\n") || strings.HasPrefix(value, "/") {
		return false
	}
	clean := path.Clean(value)
	return clean == value && clean != "." && clean != ".." && !strings.HasPrefix(clean, "../")
}

func hasExcludedHashComponent(value string) bool {
	for _, component := range strings.Split(value, "/") {
		if component == ".git" || component == "node_modules" {
			return true
		}
	}
	return false
}

func computeContentHash(files []remoteFile) string { return hashRemoteContent(files) }

func gitBlobHash(data []byte) [20]byte {
	input := append([]byte(fmt.Sprintf("blob %d\x00", len(data))), data...)
	return sha1.Sum(input)
}

func gitTreeHash(files []remoteFile) [20]byte {
	type treeNode struct {
		name string
		dir  bool
		mode string
		hash [20]byte
		body []byte
	}
	root := map[string]any{}
	for _, file := range files {
		parts := strings.Split(file.Path, "/")
		current := root
		for _, component := range parts[:len(parts)-1] {
			child, ok := current[component]
			if !ok {
				child = map[string]any{}
				current[component] = child
			}
			current = child.(map[string]any)
		}
		mode := "100644"
		if file.Mode == "100755" {
			mode = "100755"
		}
		current[parts[len(parts)-1]] = treeNode{name: parts[len(parts)-1], mode: mode, hash: gitBlobHash(file.Content)}
	}
	var build func(map[string]any) [20]byte
	build = func(nodes map[string]any) [20]byte {
		items := make([]treeNode, 0, len(nodes))
		for name, raw := range nodes {
			if child, ok := raw.(map[string]any); ok {
				items = append(items, treeNode{name: name, dir: true, mode: "40000", hash: build(child)})
			} else {
				items = append(items, raw.(treeNode))
			}
		}
		sort.Slice(items, func(i, j int) bool {
			left, right := items[i].name, items[j].name
			if items[i].dir {
				left += "/"
			}
			if items[j].dir {
				right += "/"
			}
			return left < right
		})
		body := make([]byte, 0)
		for _, item := range items {
			body = append(body, []byte(item.mode+" "+item.name+"\x00")...)
			body = append(body, item.hash[:]...)
		}
		input := append([]byte(fmt.Sprintf("tree %d\x00", len(body))), body...)
		return sha1.Sum(input)
	}
	return build(root)
}
