package skillsync

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/AllenMuu/skill-manager/internal/catalog"
	"github.com/AllenMuu/skill-manager/internal/operation"
	"github.com/AllenMuu/skill-manager/internal/skillrepo"
)

var (
	ErrInvalidSelection = errors.New("invalid Skill synchronization selection")
	ErrStalePreview     = errors.New("Skill synchronization preview changed; review a new preview")
	ErrNeedsChoice      = errors.New("choose one source version to make current for each new divergent Skill")
)

const importLockName = "imports.lock"

type SourceGroup struct {
	ID                    string   `json:"id"`
	Identifier            string   `json:"identifier"`
	Checksum              string   `json:"checksum"`
	VersionKey            string   `json:"versionKey"`
	Sources               []Source `json:"sources"`
	AlreadyPresent        bool     `json:"alreadyPresent"`
	Conflict              bool     `json:"conflict"`
	ConflictReason        string   `json:"conflictReason,omitempty"`
	ExistingCurrent       string   `json:"existingCurrent,omitempty"`
	RequiresCurrentChoice bool     `json:"requiresCurrentChoice,omitempty"`
	MakeCurrent           bool     `json:"makeCurrent,omitempty"`
}

type Preview struct {
	RepositoryPath        string            `json:"repositoryPath"`
	Groups                []SourceGroup     `json:"groups"`
	SelectedGroupIDs      []string          `json:"selectedGroupIds"`
	CurrentChoices        map[string]string `json:"currentChoices,omitempty"`
	CurrentChoiceRequired []string          `json:"currentChoiceRequired,omitempty"`
	RepositoryStates      map[string]string `json:"repositoryStates"`
	ConfirmationToken     string            `json:"confirmationToken"`
}

type Service struct {
	repositoryRoot      string
	home                string
	project             string
	syncRoot            string
	globalJournal       *operation.Journal
	projectJournal      *operation.Journal
	Now                 func() time.Time
	BeforeImportPublish func(int, skillrepo.Entry) error
}

func New(repositoryRoot, home, project string, globalJournal, projectJournal *operation.Journal) (*Service, error) {
	repository, err := skillrepo.Open(repositoryRoot)
	if err != nil {
		return nil, err
	}
	if home == "" {
		return nil, fmt.Errorf("home directory is required")
	}
	absHome, err := filepath.Abs(home)
	if err != nil {
		return nil, err
	}
	absHome, err = filepath.EvalSymlinks(absHome)
	if err != nil {
		return nil, fmt.Errorf("resolve user home directory: %w", err)
	}
	var absProject string
	if project != "" {
		absProject, err = filepath.Abs(project)
		if err != nil {
			return nil, err
		}
		absProject, err = filepath.EvalSymlinks(absProject)
		if err != nil {
			return nil, fmt.Errorf("resolve registered project directory: %w", err)
		}
	}
	return &Service{
		repositoryRoot: repository.Root(), home: filepath.Clean(absHome), project: filepath.Clean(absProject),
		syncRoot:      filepath.Join(absHome, ".agent-manager", "skill-sync"),
		globalJournal: globalJournal, projectJournal: projectJournal, Now: time.Now,
	}, nil
}

func (s *Service) Inventory() (Inventory, []SourceGroup, error) {
	inventory, err := Discover(s.home, s.project, s.repositoryRoot, s.globalJournal, s.projectJournal)
	if err != nil {
		return Inventory{}, nil, err
	}
	return inventory, groupSources(inventory.Sources), nil
}

func (s *Service) Preview(selectedIDs []string, choices map[string]string) (Preview, error) {
	inventory, groups, err := s.Inventory()
	if err != nil {
		return Preview{}, err
	}
	allByID := make(map[string]SourceGroup, len(groups))
	variants := map[string]int{}
	for _, group := range groups {
		allByID[group.ID] = group
		variants[group.Identifier]++
	}
	selected, err := normalizeGroups(selectedIDs, allByID)
	if err != nil {
		return Preview{}, err
	}
	if len(selected) == 0 {
		return Preview{}, fmt.Errorf("%w: select at least one eligible Skill version", ErrInvalidSelection)
	}
	choices = cloneChoices(choices)
	selectedSet := make(map[string]bool, len(selected))
	for _, id := range selected {
		selectedSet[id] = true
	}
	preview := Preview{
		RepositoryPath: s.repositoryRoot, Groups: make([]SourceGroup, 0, len(selected)),
		SelectedGroupIDs: selected, CurrentChoices: choices, RepositoryStates: map[string]string{},
	}
	repository, err := skillrepo.Open(s.repositoryRoot)
	if err != nil {
		return Preview{}, err
	}
	states := map[string]repositoryIdentityState{}
	for _, groupID := range selected {
		group := allByID[groupID]
		state, ok := states[group.Identifier]
		if !ok {
			state, err = inspectRepositoryIdentity(repository, group.Identifier)
			if err != nil {
				group.Conflict = true
				group.ConflictReason = err.Error()
			} else {
				states[group.Identifier] = state
				preview.RepositoryStates[group.Identifier] = state.Fingerprint
			}
		}
		if state.Fingerprint != "" || state.exists {
			group.ExistingCurrent = state.current
		}
		for _, version := range state.versions {
			if version.Metadata.Checksum == group.Checksum || (version.Metadata.Checksum == "" && checksumMatches(version.Path, group.Checksum)) {
				group.AlreadyPresent = true
			}
			if version.Key == group.VersionKey && version.Metadata.Checksum != group.Checksum {
				group.Conflict = true
				group.ConflictReason = "repository version key exists with different content"
			}
		}
		if group.AlreadyPresent {
			group.MakeCurrent = false
		} else if state.exists {
			group.MakeCurrent = false
		} else if variants[group.Identifier] > 1 {
			group.RequiresCurrentChoice = true
			choice := choices[group.Identifier]
			if choice == "" || !selectedSet[choice] || allByID[choice].Identifier != group.Identifier {
				if !contains(preview.CurrentChoiceRequired, group.Identifier) {
					preview.CurrentChoiceRequired = append(preview.CurrentChoiceRequired, group.Identifier)
				}
			} else {
				group.MakeCurrent = choice == group.ID
			}
		} else {
			group.MakeCurrent = true
		}
		preview.Groups = append(preview.Groups, group)
	}
	for identifier, choice := range choices {
		group, exists := allByID[choice]
		if !exists || group.Identifier != identifier || !selectedSet[choice] {
			return Preview{}, fmt.Errorf("%w: current-version choice for %s must be one of its selected variants", ErrInvalidSelection, identifier)
		}
	}
	sort.Strings(preview.CurrentChoiceRequired)
	_ = inventory
	token, err := previewToken(preview)
	if err != nil {
		return Preview{}, err
	}
	preview.ConfirmationToken = token
	return preview, nil
}

func (s *Service) Apply(reviewed Preview) ([]skillrepo.Entry, error) {
	return s.ApplyUntil(reviewed, time.Time{})
}

// ApplyUntil revalidates an approved import after waiting for the shared
// import lock and refuses to apply an expired Web console preview.
func (s *Service) ApplyUntil(reviewed Preview, expiresAt time.Time) ([]skillrepo.Entry, error) {
	if reviewed.ConfirmationToken == "" || reviewed.ConfirmationToken != mustPreviewToken(reviewed) {
		return nil, ErrStalePreview
	}
	if len(reviewed.CurrentChoiceRequired) > 0 {
		return nil, ErrNeedsChoice
	}
	unlock, err := s.lockImport()
	if err != nil {
		return nil, err
	}
	defer unlock()
	if !expiresAt.IsZero() && !s.currentTime().Before(expiresAt) {
		return nil, ErrStalePreview
	}
	fresh, err := s.Preview(reviewed.SelectedGroupIDs, reviewed.CurrentChoices)
	if err != nil || !reflect.DeepEqual(reviewed, fresh) {
		return nil, errors.Join(ErrStalePreview, err)
	}
	versions := make([]skillrepo.VersionSource, 0, len(fresh.Groups))
	groupsByID := make(map[string]SourceGroup, len(fresh.Groups))
	for _, group := range fresh.Groups {
		groupsByID[group.ID] = group
		if group.Conflict {
			return nil, fmt.Errorf("%w: %s: %s", skillrepo.ErrConflict, group.Identifier, group.ConflictReason)
		}
		if group.AlreadyPresent {
			continue
		}
		if group.RequiresCurrentChoice && !choiceWasReviewed(fresh, group) {
			return nil, ErrNeedsChoice
		}
		metadata, sourcePath, err := versionMetadata(group)
		if err != nil {
			return nil, err
		}
		versions = append(versions, skillrepo.VersionSource{Identifier: group.Identifier, SourcePath: sourcePath, Metadata: metadata, MakeCurrent: group.MakeCurrent})
	}
	if len(versions) == 0 {
		return []skillrepo.Entry{}, nil
	}
	transactionID, err := randomID()
	if err != nil {
		return nil, err
	}
	transactionRoot := filepath.Join(s.syncRoot, transactionID)
	backupRoot := filepath.Join(transactionRoot, "before")
	expected := make(map[string]string)
	for _, version := range versions {
		if _, present := expected[version.Identifier]; present {
			continue
		}
		expected[version.Identifier] = fresh.RepositoryStates[version.Identifier]
	}
	repository, err := skillrepo.Open(s.repositoryRoot)
	if err != nil {
		return nil, err
	}
	result, err := repository.AddVersionsAtomic(versions, backupRoot, expected, s.BeforeImportPublish)
	if err != nil {
		if !errors.Is(err, skillrepo.ErrRollbackIncomplete) {
			s.removeImportSnapshot(transactionRoot)
		}
		if errors.Is(err, skillrepo.ErrConflict) || errors.Is(err, skillrepo.ErrUnsafe) {
			return nil, errors.Join(ErrStalePreview, err)
		}
		return nil, err
	}
	// The snapshot protects atomic rollback while the batch is being published.
	// Successful imports are immutable repository versions; removing one later
	// cannot be proven safe without a complete registry of every project that
	// may activate it.
	s.removeImportSnapshot(transactionRoot)
	return result.Entries, nil
}

func (s *Service) removeImportSnapshot(path string) {
	if err := os.RemoveAll(path); err != nil {
		log.Printf("remove Skill import snapshot: %v", err)
	}
}

// lockImport serializes imports from separate local console processes.
func (s *Service) lockImport() (func() error, error) {
	if err := ensureNoSymlinkParents(s.home, s.syncRoot); err != nil {
		return nil, err
	}
	return operation.New(filepath.Join(s.syncRoot, importLockName)).LockOperation()
}

func (s *Service) currentTime() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

type repositoryIdentityState struct {
	exists      bool
	current     string
	versions    []skillrepo.Entry
	Fingerprint string
}

func inspectRepositoryIdentity(repository *skillrepo.Repository, identifier string) (repositoryIdentityState, error) {
	versions, err := repository.VersionRecords(identifier)
	if errors.Is(err, skillrepo.ErrNotFound) || os.IsNotExist(err) {
		return repositoryIdentityState{}, nil
	}
	if err != nil {
		return repositoryIdentityState{}, err
	}
	entry, err := repository.Resolve(identifier, "")
	if err != nil {
		return repositoryIdentityState{}, err
	}
	fingerprint, err := skillrepo.ChecksumTree(filepath.Join(repository.Root(), identifier))
	if err != nil {
		return repositoryIdentityState{}, err
	}
	return repositoryIdentityState{exists: true, current: entry.Key, versions: versions, Fingerprint: fingerprint}, nil
}

func checksumMatches(path, expected string) bool {
	checksum, err := skillrepo.ChecksumTree(path)
	return err == nil && checksum == expected
}

func groupSources(sources []Source) []SourceGroup {
	groups := map[string]*SourceGroup{}
	for _, source := range sources {
		if !source.Eligible || source.AlreadyManaged || source.Checksum == "" {
			continue
		}
		id := groupID(source.Identifier, source.Checksum)
		group := groups[id]
		if group == nil {
			group = &SourceGroup{ID: id, Identifier: source.Identifier, Checksum: source.Checksum, VersionKey: "sync-" + source.Checksum}
			groups[id] = group
		}
		group.Sources = append(group.Sources, source)
	}
	result := make([]SourceGroup, 0, len(groups))
	for _, group := range groups {
		sort.Slice(group.Sources, func(i, j int) bool {
			left, right := group.Sources[i], group.Sources[j]
			if left.Scope != right.Scope {
				return left.Scope < right.Scope
			}
			if left.Location != right.Location {
				return left.Location < right.Location
			}
			return left.Path < right.Path
		})
		result = append(result, *group)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Identifier != result[j].Identifier {
			return result[i].Identifier < result[j].Identifier
		}
		return result[i].Checksum < result[j].Checksum
	})
	return result
}

func groupID(identifier, checksum string) string { return identifier + "@" + checksum }

func normalizeGroups(values []string, groups map[string]SourceGroup) ([]string, error) {
	if len(values) == 0 {
		return nil, nil
	}
	seen := map[string]bool{}
	result := append([]string(nil), values...)
	for _, value := range result {
		if value == "" || strings.TrimSpace(value) != value || seen[value] {
			return nil, fmt.Errorf("%w: invalid or duplicate source group", ErrInvalidSelection)
		}
		if _, exists := groups[value]; !exists {
			return nil, fmt.Errorf("%w: source group is no longer eligible", ErrInvalidSelection)
		}
		seen[value] = true
	}
	sort.Strings(result)
	return result, nil
}

func versionMetadata(group SourceGroup) (skillrepo.VersionMetadata, string, error) {
	if len(group.Sources) == 0 {
		return skillrepo.VersionMetadata{}, "", ErrStalePreview
	}
	sourcePath := group.Sources[0].Path
	skills, _, err := catalog.Discover(filepath.Dir(sourcePath))
	if err != nil {
		return skillrepo.VersionMetadata{}, "", err
	}
	var skill *catalog.Skill
	for i := range skills {
		if skills[i].Identifier == group.Identifier {
			skill = &skills[i]
			break
		}
	}
	if skill == nil {
		return skillrepo.VersionMetadata{}, "", ErrStalePreview
	}
	origins := make([]skillrepo.SourceOrigin, 0, len(group.Sources))
	for _, source := range group.Sources {
		origins = append(origins, skillrepo.SourceOrigin{Scope: string(source.Scope), Location: source.Location, Path: source.Path})
	}
	metadata := skillrepo.VersionMetadata{
		Key: group.VersionKey, Version: "unknown", Source: sourcePath, SourceType: "local-web-sync",
		Publisher: "unknown", ImmutableRef: "unknown", Checksum: group.Checksum,
		Tags: append([]string(nil), skill.Tags...), Compatibility: append([]string(nil), skill.Compatibility...),
		SupportedLanguages:  append([]string(nil), skill.SupportedLanguages...),
		SupportedFrameworks: append([]string(nil), skill.SupportedFrameworks...),
		SupportedAgents:     append([]string(nil), skill.SupportedAgents...), SourceOrigins: origins,
		TrustLevel: "unknown", VerificationStatus: "unknown",
	}
	return metadata, sourcePath, nil
}

func previewToken(preview Preview) (string, error) {
	preview.ConfirmationToken = ""
	contents, err := json.Marshal(preview)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(contents)
	return hex.EncodeToString(digest[:]), nil
}

func mustPreviewToken(preview Preview) string { token, _ := previewToken(preview); return token }

func choiceWasReviewed(preview Preview, group SourceGroup) bool {
	choice := preview.CurrentChoices[group.Identifier]
	if choice == "" {
		return false
	}
	for _, candidate := range preview.Groups {
		if candidate.ID == choice && candidate.Identifier == group.Identifier {
			return true
		}
	}
	return false
}

func cloneChoices(choices map[string]string) map[string]string {
	if len(choices) == 0 {
		return map[string]string{}
	}
	copy := make(map[string]string, len(choices))
	for id, choice := range choices {
		copy[id] = choice
	}
	return copy
}

func contains(values []string, value string) bool {
	for _, item := range values {
		if item == value {
			return true
		}
	}
	return false
}

func randomID() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}

func ensureNoSymlinkParents(boundary, path string) error {
	boundary, err := filepath.Abs(boundary)
	if err != nil {
		return err
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return err
	}
	relative, err := filepath.Rel(boundary, path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return fmt.Errorf("%w: Agent Manager sync journal escaped the user home", ErrUnsafeSource)
	}
	current := boundary
	for _, component := range strings.Split(relative, string(filepath.Separator)) {
		if component == "." || component == "" {
			continue
		}
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%w: symlink in sync journal path", ErrUnsafeSource)
		}
	}
	return nil
}
