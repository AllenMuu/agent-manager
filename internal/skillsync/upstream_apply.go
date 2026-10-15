package skillsync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/AllenMuu/skill-manager/internal/operation"
	"github.com/AllenMuu/skill-manager/internal/resource"
)

const (
	maxPreservedFiles = 4096
	maxPreservedBytes = 128 << 20
)

type stagedUpdate struct {
	item           preparedUpdateItem
	stage          string
	backup         string
	fingerprint    string
	preserveBackup bool
}

// RevalidatedUpdatePlan carries a reviewed plan whose upstream hashes were
// checked after confirmation. Its contents cannot be set outside this package.
type RevalidatedUpdatePlan struct {
	plan PreparedUpdatePlan
}

// ApplyUpdates applies only content retained by a reviewed update plan. It
// revalidates upstream content before acquiring the journal operation lock,
// then revalidates local state and records one reversible journal entry.
func (s *Service) ApplyUpdates(ctx context.Context, plan PreparedUpdatePlan, journal *operation.Journal) error {
	validated, err := s.RevalidateUpdates(ctx, plan)
	if err != nil {
		return err
	}
	return s.ApplyRevalidatedUpdates(ctx, validated, journal)
}

// RevalidateUpdates checks that the remote content still matches the hashes
// shown in the reviewed plan. Call it before acquiring broader local-operation
// locks so a slow upstream does not block unrelated local mutations.
func (s *Service) RevalidateUpdates(ctx context.Context, plan PreparedUpdatePlan) (RevalidatedUpdatePlan, error) {
	if len(plan.items) == 0 || len(plan.items) != len(plan.Preview.Items) || plan.contentBytes > maxPreparedUpdateBytes {
		return RevalidatedUpdatePlan{}, ErrInvalidUpdateSelection
	}
	seen := map[string]bool{}
	for _, item := range plan.items {
		if item.check.ID == "" || seen[item.check.ID] {
			return RevalidatedUpdatePlan{}, ErrInvalidUpdateSelection
		}
		seen[item.check.ID] = true
		if err := ctx.Err(); err != nil {
			return RevalidatedUpdatePlan{}, err
		}
		if err := s.revalidateRemote(ctx, item); err != nil {
			return RevalidatedUpdatePlan{}, err
		}
	}
	return RevalidatedUpdatePlan{plan: plan}, nil
}

// ApplyRevalidatedUpdates changes local files from a plan that was already
// checked against the upstream after user confirmation.
func (s *Service) ApplyRevalidatedUpdates(ctx context.Context, validated RevalidatedUpdatePlan, journal *operation.Journal) error {
	plan := validated.plan
	if journal == nil || journal.Path == "" || len(plan.items) == 0 || len(plan.items) != len(plan.Preview.Items) || plan.contentBytes > maxPreparedUpdateBytes {
		return ErrInvalidUpdateSelection
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	unlock, err := journal.LockOperation()
	if err != nil {
		return err
	}
	defer unlock()
	if err := ctx.Err(); err != nil {
		return err
	}

	inventory, _, err := s.Inventory()
	if err != nil {
		return err
	}
	sources := make(map[string]Source, len(inventory.Sources))
	for _, source := range inventory.Sources {
		sources[source.ID] = source
	}
	seen := map[string]bool{}
	for i := range plan.items {
		item := &plan.items[i]
		if seen[item.check.ID] {
			return ErrInvalidUpdateSelection
		}
		seen[item.check.ID] = true
		source, ok := sources[item.check.ID]
		if !ok || !source.Eligible || source.AlreadyManaged || source.Path != item.check.Path || source.Identifier != item.check.Identifier || source.StoredHash != item.check.CurrentHash || source.HashKind != item.check.hashKind || source.LockPath != item.check.lockPath || source.LockKey != item.check.lockKey || source.Origin != item.check.Origin || source.OriginRef != item.check.Ref || source.OriginType != "github" {
			return ErrStaleUpdatePlan
		}
		if err := validateBelowBoundary(s.sourceBoundary(source.Scope), source.Path); err != nil {
			return ErrStaleUpdatePlan
		}
		localHash, err := hashLocalSkill(source.Path, source.HashKind)
		if err != nil || localHash != item.check.CurrentHash {
			return ErrStaleUpdatePlan
		}
		localFingerprint, err := operation.FingerprintPath(source.Path)
		if err != nil || localFingerprint != item.check.localFingerprint {
			return ErrStaleUpdatePlan
		}
		if source.HashKind == "content" {
			fileCount, countErr := localHashFileCount(source.Path, source.HashKind)
			if countErr != nil || fileCount != 1 || len(item.files) != 1 {
				return ErrStaleUpdatePlan
			}
		}
		lockHash, err := fingerprintRegularFile(source.LockPath)
		if err != nil || lockHash != item.check.lockFingerprint {
			return ErrStaleUpdatePlan
		}
	}

	staged := make([]stagedUpdate, 0, len(plan.items))
	backupFingerprints := map[string]string{}
	cleanup := func() {
		for _, item := range staged {
			if item.stage != "" && item.fingerprint != "" {
				removePathIfFingerprintMatches(item.stage, item.fingerprint)
			}
			if item.backup != "" && !item.preserveBackup {
				if expected := backupFingerprints[item.item.check.Path]; expected != "" {
					removePathIfFingerprintMatches(item.backup, expected)
				}
			}
		}
	}
	defer cleanup()

	for _, item := range plan.items {
		stage, err := stageSkill(item.check.Path, item.files)
		if err != nil {
			return err
		}
		update := stagedUpdate{item: item, stage: stage}
		staged = append(staged, update)
		fingerprint, err := operation.FingerprintPath(stage)
		if err != nil {
			return err
		}
		staged[len(staged)-1].fingerprint = fingerprint
		if item.check.hashKind == "content" {
			if err := preserveExcludedLocalFiles(item.check.Path, stage); err != nil {
				if fingerprint, fingerprintErr := operation.FingerprintPath(stage); fingerprintErr == nil {
					staged[len(staged)-1].fingerprint = fingerprint
				}
				return fmt.Errorf("preserve local Skill data: %w", err)
			}
		}
		fingerprint, err = operation.FingerprintPath(stage)
		if err != nil {
			return err
		}
		staged[len(staged)-1].fingerprint = fingerprint
		_, inspectErr := resource.NewSkillHandler().Inspect(stage)
		if inspectErr != nil {
			return fmt.Errorf("upstream Skill metadata is invalid or does not match %q: %w", item.check.Identifier, inspectErr)
		}
		updatedHash, err := hashLocalSkill(stage, item.check.hashKind)
		if err != nil || updatedHash != item.check.UpstreamHash {
			return errors.New("staged Skill content does not match the reviewed upstream hash")
		}
	}

	lockContents := map[string][]byte{}
	for _, item := range plan.items {
		if _, ok := lockContents[item.check.lockPath]; ok {
			continue
		}
		contents, err := updatedLockContents(item.check.lockPath, plan.items, s.currentTime())
		if err != nil {
			return err
		}
		lockContents[item.check.lockPath] = contents
	}
	paths := make([]string, 0, len(staged)+len(lockContents))
	for _, item := range staged {
		paths = append(paths, item.item.check.Path)
	}
	for path := range lockContents {
		paths = append(paths, path)
	}
	paths = uniqueSortedPaths(paths)
	before, err := journal.Capture(paths)
	if err != nil {
		return err
	}
	beforeByPath := make(map[string]operation.Snapshot, len(before))
	for _, snapshot := range before {
		beforeByPath[snapshot.Path] = snapshot
		if snapshot.Exists && snapshot.Backup != "" {
			if fingerprint, err := operation.FingerprintPath(snapshot.Backup); err == nil {
				backupFingerprints[snapshot.Path] = fingerprint
			}
		}
	}
	changed := map[string]string{}
	stagedByPath := make(map[string]*stagedUpdate, len(staged))
	for i := range staged {
		stagedByPath[staged[i].item.check.Path] = &staged[i]
	}
	rollback := func(cause error) error {
		var rollbackErrors []error
		for i := len(paths) - 1; i >= 0; i-- {
			path := paths[i]
			expected, changedByUs := changed[path]
			if !changedByUs {
				continue
			}
			current, fingerprintErr := operation.FingerprintPath(path)
			if fingerprintErr != nil || current != expected {
				if item := stagedByPath[path]; item != nil && item.backup != "" {
					item.preserveBackup = true
				}
				rollbackErrors = append(rollbackErrors, fmt.Errorf("preserved changed path %s because it no longer matches this update transaction", path))
				continue
			}
			if restoreErr := journal.Restore([]operation.Snapshot{beforeByPath[path]}); restoreErr != nil {
				if item := stagedByPath[path]; item != nil && item.backup != "" {
					item.preserveBackup = true
				}
				rollbackErrors = append(rollbackErrors, restoreErr)
			} else if item := stagedByPath[path]; item != nil && item.backup != "" {
				if removePathIfFingerprintMatches(item.backup, backupFingerprints[path]) {
					item.backup = ""
				}
			}
		}
		return errors.Join(append([]error{cause}, rollbackErrors...)...)
	}

	for i := range staged {
		item := &staged[i]
		if err := validateBelowBoundary(s.sourceBoundary(item.item.check.Scope), item.item.check.Path); err != nil {
			return rollback(ErrStaleUpdatePlan)
		}
		currentFingerprint, err := operation.FingerprintPath(item.item.check.Path)
		if err != nil || currentFingerprint != item.item.check.localFingerprint {
			return rollback(ErrStaleUpdatePlan)
		}
		backup, err := unusedSiblingPath(item.item.check.Path, ".agent-manager-update-before-")
		if err != nil {
			return rollback(err)
		}
		if err := os.Rename(item.item.check.Path, backup); err != nil {
			return rollback(fmt.Errorf("stage current Skill before update: %w", err))
		}
		item.backup = backup
		if err := os.Rename(item.stage, item.item.check.Path); err != nil {
			if restoreErr := os.Rename(backup, item.item.check.Path); restoreErr != nil {
				item.preserveBackup = true
				return rollback(errors.Join(fmt.Errorf("publish updated Skill: %w", err), restoreErr))
			}
			item.backup = ""
			return rollback(fmt.Errorf("publish updated Skill: %w", err))
		}
		item.stage = ""
		changed[item.item.check.Path] = item.fingerprint
	}

	lockPaths := make([]string, 0, len(lockContents))
	for path := range lockContents {
		lockPaths = append(lockPaths, path)
	}
	sort.Strings(lockPaths)
	for _, lockPath := range lockPaths {
		content := lockContents[lockPath]
		expectedFingerprint, found := plannedLockFingerprint(lockPath, plan.items)
		currentFingerprint, fingerprintErr := fingerprintRegularFile(lockPath)
		if !found || fingerprintErr != nil || currentFingerprint != expectedFingerprint {
			return rollback(ErrStaleUpdatePlan)
		}
		if err := replaceRegularFile(lockPath, content, expectedFingerprint); err != nil {
			return rollback(fmt.Errorf("update Skills CLI lock metadata: %w", err))
		}
		fingerprint, err := operation.FingerprintPath(lockPath)
		if err != nil {
			return rollback(err)
		}
		changed[lockPath] = fingerprint
	}

	after, err := journal.Capture(paths)
	if err != nil {
		return rollback(err)
	}
	operationPlan := operation.NewPlan("update upstream Skills")
	for _, item := range staged {
		operationPlan.Changes = append(operationPlan.Changes, operation.Change{Path: item.item.check.Path, Action: "update Skill", Detail: item.item.check.Identifier})
	}
	for _, path := range lockPaths {
		operationPlan.Changes = append(operationPlan.Changes, operation.Change{Path: path, Action: "update Skills CLI lock metadata"})
	}
	if err := journal.RecordPlan(operationPlan, before, after); err != nil {
		if !errors.Is(err, operation.ErrJournalCommitted) {
			return rollback(err)
		}
		return err
	}
	return nil
}

func (s *Service) sourceBoundary(scope Scope) string {
	if scope == ScopeProject {
		return s.project
	}
	return s.home
}

func (s *Service) revalidateRemote(ctx context.Context, item preparedUpdateItem) error {
	ownerRepo, ref, problem := validateGitHubOrigin(item.check.Origin, item.check.Ref, item.remote.Folder, item.check.CurrentHash, item.check.hashKind)
	if problem != "" {
		return ErrStaleUpdatePlan
	}
	latest, err := s.github.skillTree(ctx, ownerRepo, ref, item.remote.Folder)
	if err != nil {
		return err
	}
	if latest.TreeHash != item.remote.TreeHash || latest.Ref != item.remote.Ref {
		return ErrStaleUpdatePlan
	}
	if item.check.hashKind == "content" {
		files, err := s.github.fetchSkillFiles(ctx, latest)
		if err != nil {
			return err
		}
		if hashRemoteContent(files) != item.check.UpstreamHash {
			return ErrStaleUpdatePlan
		}
		if len(files) != 1 {
			return ErrStaleUpdatePlan
		}
	} else if remoteHasExcludedHashPath(latest) {
		return ErrStaleUpdatePlan
	}
	return nil
}

func stageSkill(target string, files []remoteFile) (string, error) {
	root, err := os.MkdirTemp(filepath.Dir(target), ".agent-manager-skill-update-")
	if err != nil {
		return "", err
	}
	rootInfo, err := os.Lstat(root)
	if err != nil || !rootInfo.IsDir() || rootInfo.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("could not establish ownership of the update staging directory")
	}
	cleanup := true
	defer func() {
		if cleanup {
			if current, statErr := os.Lstat(root); statErr == nil && os.SameFile(rootInfo, current) && current.IsDir() && current.Mode()&os.ModeSymlink == 0 {
				_ = os.RemoveAll(root)
			}
		}
	}()
	seen := map[string]bool{}
	for _, file := range files {
		if !validRemoteRelativePath(file.Path) || seen[file.Path] || (file.Mode != "100644" && file.Mode != "100755") || len(file.Content) > maxSkillFileBytes {
			return "", errors.New("upstream Skill contains an invalid or duplicate file")
		}
		seen[file.Path] = true
		path := filepath.Join(root, filepath.FromSlash(file.Path))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return "", err
		}
		mode := os.FileMode(0o644)
		if file.Mode == "100755" {
			mode = 0o755
		}
		output, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
		if err != nil {
			return "", err
		}
		_, writeErr := output.Write(file.Content)
		syncErr := output.Sync()
		closeErr := output.Close()
		if err := errors.Join(writeErr, syncErr, closeErr); err != nil {
			return "", err
		}
	}
	if !seen["SKILL.md"] {
		return "", errors.New("upstream Skill has no SKILL.md")
	}
	cleanup = false
	return root, nil
}

func removePathIfFingerprintMatches(path, expected string) bool {
	if expected == "" {
		return false
	}
	actual, err := operation.FingerprintPath(path)
	if err != nil || actual != expected {
		return false
	}
	return os.RemoveAll(path) == nil
}

func preserveExcludedLocalFiles(source, destination string) error {
	fileCount := 0
	byteCount := int64(0)
	return filepath.WalkDir(source, func(current string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(source, current)
		if err != nil || relative == "." {
			return err
		}
		relative = filepath.ToSlash(relative)
		if !hasExcludedHashComponent(relative) {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || (!info.IsDir() && !info.Mode().IsRegular()) {
			return errors.New("local .git or node_modules content contains an unsupported filesystem entry")
		}
		target := filepath.Join(destination, filepath.FromSlash(relative))
		if entry.IsDir() {
			if _, err := os.Lstat(target); os.IsNotExist(err) {
				if err := os.MkdirAll(target, info.Mode().Perm()); err != nil {
					return err
				}
			} else if err != nil {
				return err
			}
			return nil
		}
		fileCount++
		byteCount += info.Size()
		if fileCount > maxPreservedFiles || byteCount > maxPreservedBytes {
			return errors.New("local .git or node_modules content exceeds the safe preservation limit")
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		in, err := os.Open(current)
		if err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, info.Mode().Perm())
		if err != nil {
			_ = in.Close()
			return err
		}
		copied, copyErr := io.Copy(out, io.LimitReader(in, maxPreservedBytes+1))
		closeInErr := in.Close()
		closeOutErr := out.Close()
		if copied != info.Size() || byteCount > maxPreservedBytes {
			return errors.New("local .git or node_modules file changed or exceeds the safe preservation limit")
		}
		return errors.Join(copyErr, closeInErr, closeOutErr)
	})
}

func updatedLockContents(lockPath string, items []preparedUpdateItem, nowTime time.Time) ([]byte, error) {
	info, err := os.Lstat(lockPath)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() > maxSkillLockBytes {
		return nil, errors.New("Skills CLI lock file is not a safe regular file")
	}
	contents, err := os.ReadFile(lockPath)
	if err != nil || int64(len(contents)) != info.Size() {
		return nil, errors.New("Skills CLI lock file changed while being read")
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(contents, &envelope); err != nil {
		return nil, errors.New("Skills CLI lock file is invalid")
	}
	var version int
	var skills map[string]json.RawMessage
	if json.Unmarshal(envelope["version"], &version) != nil || version < 1 || json.Unmarshal(envelope["skills"], &skills) != nil || skills == nil {
		return nil, errors.New("Skills CLI lock file uses an unsupported format")
	}
	for _, item := range items {
		if item.check.lockPath != lockPath {
			continue
		}
		var record map[string]json.RawMessage
		if json.Unmarshal(skills[item.check.lockKey], &record) != nil || record == nil {
			return nil, errors.New("Skills CLI lock record changed or is invalid")
		}
		var source, sourceType, skillPath, storedHash string
		_ = json.Unmarshal(record["source"], &source)
		_ = json.Unmarshal(record["sourceType"], &sourceType)
		_ = json.Unmarshal(record["skillPath"], &skillPath)
		if item.check.hashKind == "git-tree" {
			_ = json.Unmarshal(record["skillFolderHash"], &storedHash)
		} else {
			_ = json.Unmarshal(record["computedHash"], &storedHash)
		}
		lockSkillPath, pathErr := skillFolderPath(skillPath)
		if source != item.check.Origin || sourceType != "github" || pathErr != nil || normalizeSkillPath(lockSkillPath) != normalizeSkillPath(item.remote.Folder) || !strings.EqualFold(storedHash, item.check.CurrentHash) {
			return nil, ErrStaleUpdatePlan
		}
		if item.check.hashKind == "git-tree" {
			record["skillFolderHash"], _ = json.Marshal(item.check.UpstreamHash)
			record["updatedAt"], _ = json.Marshal(nowTime.Format("2006-01-02T15:04:05.999999999Z07:00"))
		} else {
			record["computedHash"], _ = json.Marshal(item.check.UpstreamHash)
		}
		updatedRecord, err := json.Marshal(record)
		if err != nil {
			return nil, err
		}
		skills[item.check.lockKey] = updatedRecord
	}
	updatedSkills, err := json.Marshal(skills)
	if err != nil {
		return nil, err
	}
	envelope["skills"] = updatedSkills
	updated, err := json.MarshalIndent(envelope, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(updated, '\n'), nil
}

func replaceRegularFile(path string, contents []byte, expectedFingerprint string) error {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("Skills CLI lock file is not a regular file")
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".agent-manager-lock-update-")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	tmpInfo, err := os.Lstat(tmpPath)
	if err != nil || !tmpInfo.Mode().IsRegular() || tmpInfo.Mode()&os.ModeSymlink != 0 {
		_ = tmp.Close()
		return errors.New("could not establish ownership of the updated lock staging file")
	}
	defer func() {
		if current, statErr := os.Lstat(tmpPath); statErr == nil && os.SameFile(tmpInfo, current) && current.Mode().IsRegular() && current.Mode()&os.ModeSymlink == 0 {
			_ = os.Remove(tmpPath)
		}
	}()
	if err := tmp.Chmod(info.Mode().Perm()); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(contents); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	currentFingerprint, err := fingerprintRegularFile(path)
	if err != nil || currentFingerprint != expectedFingerprint {
		return ErrStaleUpdatePlan
	}
	return os.Rename(tmpPath, path)
}

func plannedLockFingerprint(path string, items []preparedUpdateItem) (string, bool) {
	for _, item := range items {
		if item.check.lockPath == path {
			return item.check.lockFingerprint, true
		}
	}
	return "", false
}

func unusedSiblingPath(target, prefix string) (string, error) {
	dir, err := os.MkdirTemp(filepath.Dir(target), prefix)
	if err != nil {
		return "", err
	}
	if err := os.Remove(dir); err != nil {
		return "", err
	}
	return dir, nil
}

func uniqueSortedPaths(paths []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(paths))
	for _, path := range paths {
		path = filepath.Clean(path)
		if !seen[path] {
			seen[path] = true
			result = append(result, path)
		}
	}
	sort.Strings(result)
	return result
}
