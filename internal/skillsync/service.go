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
	ErrInvalidSelection    = errors.New("invalid Skill synchronization selection")
	ErrStalePreview        = errors.New("Skill synchronization preview changed; review a new preview")
	ErrNeedsChoice         = errors.New("choose one source version to make current for each new divergent Skill")
	ErrNeedsIdentityChoice = errors.New("choose whether to use the source entry or resolved Skill identity before importing")
	ErrIncompleteInventory = errors.New("source inventory is incomplete; refresh and resolve scan diagnostics before importing")
)

const importLockName = "imports.lock"

type SourceGroup struct {
	ID                     string     `json:"id"`
	Identifier             string     `json:"identifier"`
	Checksum               string     `json:"checksum"`
	VersionKey             string     `json:"versionKey"`
	Sources                []Source   `json:"sources"`
	AlreadyPresent         bool       `json:"alreadyPresent"`
	Conflict               bool       `json:"conflict"`
	ConflictReason         string     `json:"conflictReason,omitempty"`
	ModifiedAt             *time.Time `json:"modifiedAt,omitempty"`
	NewerLocalModification bool       `json:"newerLocalModification,omitempty"`
	ExistingCurrent        string     `json:"existingCurrent,omitempty"`
	RequiresCurrentChoice  bool       `json:"requiresCurrentChoice,omitempty"`
	MakeCurrent            bool       `json:"makeCurrent,omitempty"`
	IdentityConflict       bool       `json:"identityConflict,omitempty"`
}

type Preview struct {
	RepositoryPath         string            `json:"repositoryPath"`
	InventoryComplete      bool              `json:"inventoryComplete"`
	Groups                 []SourceGroup     `json:"groups"`
	SelectedGroupIDs       []string          `json:"selectedGroupIds"`
	CurrentChoices         map[string]string `json:"currentChoices,omitempty"`
	CurrentChoiceRequired  []string          `json:"currentChoiceRequired,omitempty"`
	IdentityChoices        map[string]string `json:"identityChoices,omitempty"`
	IdentityChoiceRequired []string          `json:"identityChoiceRequired,omitempty"`
	RepositoryStates       map[string]string `json:"repositoryStates"`
	ConfirmationToken      string            `json:"confirmationToken"`
}

// ApplyResult identifies the durable import batch, exact published or reused
// versions, and committed discovery evidence.
type ApplyResult struct {
	BatchID         string            `json:"batchId"`
	Imported        int               `json:"imported"`
	Entries         []skillrepo.Entry `json:"entries"`
	SourceRecordIDs []string          `json:"sourceRecordIds"`
}

type Service struct {
	repositoryRoot        string
	home                  string
	project               string
	syncRoot              string
	globalJournal         *operation.Journal
	projectJournal        *operation.Journal
	Now                   func() time.Time
	BeforeImportPublish   func(int, skillrepo.Entry) error
	BeforeDiscoveryCommit func([]string) error
	github                *githubClient
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
		absProject = filepath.Clean(absProject)
	}
	return &Service{
		repositoryRoot: repository.Root(), home: filepath.Clean(absHome), project: absProject,
		syncRoot:      filepath.Join(absHome, ".agent-manager", "skill-sync"),
		globalJournal: globalJournal, projectJournal: projectJournal, Now: time.Now, github: newGitHubClient(),
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
	return s.PreviewWithIdentityChoices(selectedIDs, choices, nil)
}

// PreviewWithIdentityChoices prepares an import preview after the user has
// explicitly resolved any mismatch between an entry name and its real Skill
// directory identity. The entry IDs stay stable while the preview binds each
// selected source to the chosen repository identity.
func (s *Service) PreviewWithIdentityChoices(selectedIDs []string, choices, identityChoices map[string]string) (Preview, error) {
	inventory, groups, err := s.Inventory()
	if err != nil {
		return Preview{}, err
	}
	if !inventory.Complete {
		return Preview{}, ErrIncompleteInventory
	}
	allByID := make(map[string]SourceGroup, len(groups))
	for _, group := range groups {
		allByID[group.ID] = group
	}
	selected, err := normalizeGroups(selectedIDs, allByID)
	if err != nil {
		return Preview{}, err
	}
	if len(selected) == 0 {
		return Preview{}, fmt.Errorf("%w: select at least one eligible Skill version", ErrInvalidSelection)
	}
	choices = cloneChoices(choices)
	identityChoices = cloneChoices(identityChoices)
	preview := Preview{
		RepositoryPath: s.repositoryRoot, InventoryComplete: inventory.Complete, Groups: make([]SourceGroup, 0, len(selected)),
		SelectedGroupIDs: selected, CurrentChoices: choices, IdentityChoices: identityChoices, RepositoryStates: map[string]string{},
	}
	selectedSourceIDs := map[string]bool{}
	preparedGroups := map[string]*SourceGroup{}
	for _, selectedGroupID := range selected {
		base := allByID[selectedGroupID]
		for _, source := range base.Sources {
			selectedSourceIDs[source.ID] = true
			identifier := source.Identifier
			if source.IdentityConflict {
				switch identityChoices[source.ID] {
				case "entry":
				case "resolved":
					if source.ResolvedIdentifier == "" {
						return Preview{}, fmt.Errorf("%w: source %s has no resolved identity", ErrInvalidSelection, source.ID)
					}
					identifier = source.ResolvedIdentifier
				default:
					preview.IdentityChoiceRequired = append(preview.IdentityChoiceRequired, source.ID)
				}
			} else if _, exists := identityChoices[source.ID]; exists {
				return Preview{}, fmt.Errorf("%w: source %s does not have an identity conflict", ErrInvalidSelection, source.ID)
			}
			id := groupID(identifier, base.Checksum)
			prepared := preparedGroups[id]
			if prepared == nil {
				prepared = &SourceGroup{ID: id, Identifier: identifier, Checksum: base.Checksum, VersionKey: "sync-" + base.Checksum}
				preparedGroups[id] = prepared
			}
			prepared.Sources = append(prepared.Sources, source)
			prepared.IdentityConflict = prepared.IdentityConflict || source.IdentityConflict
			if source.ModifiedAt != nil && (prepared.ModifiedAt == nil || source.ModifiedAt.After(*prepared.ModifiedAt)) {
				modifiedAt := *source.ModifiedAt
				prepared.ModifiedAt = &modifiedAt
			}
		}
	}
	for id, choice := range identityChoices {
		if !selectedSourceIDs[id] || (choice != "entry" && choice != "resolved") {
			return Preview{}, fmt.Errorf("%w: identity choice must name a selected source and use entry or resolved", ErrInvalidSelection)
		}
	}
	sort.Strings(preview.IdentityChoiceRequired)
	previewGroups := make([]SourceGroup, 0, len(preparedGroups))
	previewByID := make(map[string]SourceGroup, len(preparedGroups))
	variants := map[string]int{}
	for _, group := range preparedGroups {
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
		variants[group.Identifier]++
		previewGroups = append(previewGroups, *group)
		previewByID[group.ID] = *group
	}
	sort.Slice(previewGroups, func(i, j int) bool {
		if previewGroups[i].Identifier != previewGroups[j].Identifier {
			return previewGroups[i].Identifier < previewGroups[j].Identifier
		}
		return previewGroups[i].Checksum < previewGroups[j].Checksum
	})
	selectedSet := make(map[string]bool, len(previewGroups))
	for _, group := range previewGroups {
		selectedSet[group.ID] = true
	}
	repository, err := skillrepo.Open(s.repositoryRoot)
	if err != nil {
		return Preview{}, err
	}
	states := map[string]repositoryIdentityState{}
	for _, candidate := range previewGroups {
		group := candidate
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
		matchingVersions := make([]skillrepo.Entry, 0, 1)
		for _, version := range state.versions {
			if version.Metadata.Checksum == group.Checksum || (version.Metadata.Checksum == "" && checksumMatches(version.Path, group.Checksum)) {
				group.AlreadyPresent = true
				matchingVersions = append(matchingVersions, version)
			}
			if version.Key == group.VersionKey && version.Metadata.Checksum != group.Checksum {
				group.Conflict = true
				group.ConflictReason = "repository version key exists with different content"
			}
		}
		if len(matchingVersions) > 0 {
			sort.Slice(matchingVersions, func(i, j int) bool { return matchingVersions[i].Key < matchingVersions[j].Key })
			group.VersionKey = matchingVersions[0].Key
		}
		if group.AlreadyPresent {
			group.MakeCurrent = false
		} else if state.exists {
			group.MakeCurrent = false
		} else if variants[group.Identifier] > 1 {
			group.RequiresCurrentChoice = true
			choice := choices[group.Identifier]
			if choice == "" || !selectedSet[choice] || previewByID[choice].Identifier != group.Identifier {
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
		group, exists := previewByID[choice]
		if !exists || group.Identifier != identifier || !selectedSet[choice] {
			return Preview{}, fmt.Errorf("%w: current-version choice for %s must be one of its selected variants", ErrInvalidSelection, identifier)
		}
	}
	sort.Strings(preview.CurrentChoiceRequired)
	token, err := previewToken(preview)
	if err != nil {
		return Preview{}, err
	}
	preview.ConfirmationToken = token
	return preview, nil
}

func (s *Service) Apply(reviewed Preview) ([]skillrepo.Entry, error) {
	result, err := s.ApplyDetailedUntil(reviewed, time.Time{})
	return entriesPublishedByPreview(reviewed, result.Entries), err
}

func (s *Service) ApplyDetailed(reviewed Preview) (ApplyResult, error) {
	return s.ApplyDetailedUntil(reviewed, time.Time{})
}

// ApplyUntil revalidates an approved import after waiting for the shared
// import lock and refuses to apply an expired Web console preview.
func (s *Service) ApplyUntil(reviewed Preview, expiresAt time.Time) ([]skillrepo.Entry, error) {
	result, err := s.ApplyDetailedUntil(reviewed, expiresAt)
	return entriesPublishedByPreview(reviewed, result.Entries), err
}

func entriesPublishedByPreview(preview Preview, entries []skillrepo.Entry) []skillrepo.Entry {
	publish := map[string]bool{}
	for _, group := range preview.Groups {
		if !group.AlreadyPresent {
			publish[group.Identifier+"\x00"+group.VersionKey] = true
		}
	}
	result := make([]skillrepo.Entry, 0, len(entries))
	for _, entry := range entries {
		key := entry.Identifier + "\x00" + entry.Key
		if publish[key] {
			result = append(result, entry)
		}
	}
	return result
}

// ApplyDetailedUntil revalidates an approved import after waiting for the
// shared import lock and returns exact versions with committed source evidence.
func (s *Service) ApplyDetailedUntil(reviewed Preview, expiresAt time.Time) (ApplyResult, error) {
	if reviewed.ConfirmationToken == "" || reviewed.ConfirmationToken != mustPreviewToken(reviewed) {
		return ApplyResult{}, ErrStalePreview
	}
	if len(reviewed.IdentityChoiceRequired) > 0 {
		return ApplyResult{}, ErrNeedsIdentityChoice
	}
	if len(reviewed.CurrentChoiceRequired) > 0 {
		return ApplyResult{}, ErrNeedsChoice
	}
	unlock, err := s.lockImport()
	if err != nil {
		return ApplyResult{}, err
	}
	defer unlock()
	if !expiresAt.IsZero() && !s.currentTime().Before(expiresAt) {
		return ApplyResult{}, ErrStalePreview
	}
	fresh, err := s.PreviewWithIdentityChoices(reviewed.SelectedGroupIDs, reviewed.CurrentChoices, reviewed.IdentityChoices)
	if err != nil || !reflect.DeepEqual(reviewed, fresh) {
		return ApplyResult{}, errors.Join(ErrStalePreview, err)
	}
	versions := make([]skillrepo.VersionSource, 0, len(fresh.Groups))
	selectedGroups := make([]SourceGroup, 0, len(fresh.Groups))
	metadataByGroup := make(map[string]skillrepo.VersionMetadata, len(fresh.Groups))
	for _, group := range fresh.Groups {
		if group.Conflict {
			return ApplyResult{}, fmt.Errorf("%w: %s: %s", skillrepo.ErrConflict, group.Identifier, group.ConflictReason)
		}
		if group.RequiresCurrentChoice && !choiceWasReviewed(fresh, group) {
			return ApplyResult{}, ErrNeedsChoice
		}
		metadata, sourcePath, err := versionMetadata(group)
		if err != nil {
			return ApplyResult{}, err
		}
		metadataByGroup[group.ID] = metadata
		selectedGroups = append(selectedGroups, group)
		if !group.AlreadyPresent {
			versions = append(versions, skillrepo.VersionSource{Identifier: group.Identifier, SourcePath: sourcePath, Metadata: metadata, MakeCurrent: group.MakeCurrent})
		}
	}
	transactionID, err := randomID()
	if err != nil {
		return ApplyResult{}, err
	}
	transactionRoot := filepath.Join(s.syncRoot, "import-snapshots", transactionID)
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
		return ApplyResult{}, err
	}
	batchStore := operation.NewBatchStore(filepath.Join(s.syncRoot, "batches"))
	discoveryStore := NewDiscoveryStore(filepath.Join(s.syncRoot, "discoveries"))
	resources := make([]string, 0, len(selectedGroups))
	steps := make([]operation.BatchStep, 0, len(selectedGroups))
	records := make([]DiscoveryRecord, 0)
	recordIDs := make([]string, 0)
	stepByIdentifier := map[string][]int{}
	for _, group := range selectedGroups {
		resource := filepath.Join(s.repositoryRoot, group.Identifier)
		resources = append(resources, resource)
		action := "reuse:" + group.VersionKey
		if !group.AlreadyPresent {
			action = "publish:" + group.VersionKey
		}
		step := operation.BatchStep{ID: group.ID, Resource: resource, Action: action}
		if !group.AlreadyPresent {
			step.ExpectedAfter = group.Checksum
		}
		steps = append(steps, step)
		if !group.AlreadyPresent {
			stepByIdentifier[group.Identifier] = append(stepByIdentifier[group.Identifier], len(steps)-1)
		}
		for _, source := range group.Sources {
			record := DiscoveryRecord{
				ID: discoveryRecordID(transactionID, source.ID), BatchID: transactionID, Scope: source.Scope,
				Location: source.Location, EntryPath: source.Path, EntryType: source.EntryType,
				OriginalLink: source.OriginalLink, LinkChain: append([]LinkHop(nil), source.LinkChain...),
				RealPath: source.RealPath, Checksum: group.Checksum, Identifier: group.Identifier,
				VersionKey: group.VersionKey, VersionMetadata: metadataByGroup[group.ID], MakeCurrent: group.MakeCurrent,
				ReusedVersion: group.AlreadyPresent, ObservedAt: s.currentTime().UTC(),
			}
			records = append(records, record)
			recordIDs = append(recordIDs, record.ID)
		}
	}
	prepared, err := batchStore.Prepare(operation.Batch{ID: transactionID, Kind: "import", Scope: "local", Resources: resources, SourceRecordIDs: recordIDs, Steps: steps})
	if err != nil {
		return ApplyResult{}, err
	}
	for i := range records {
		if _, err := discoveryStore.Prepare(records[i]); err != nil {
			prepared.Status, prepared.Error = operation.BatchRolledBack, err.Error()
			_ = batchStore.Update(prepared)
			return ApplyResult{}, err
		}
	}
	updateBatch := func() error { return batchStore.Update(prepared) }
	if len(versions) > 0 {
		_, err = repository.AddVersionsAtomicWithHooks(versions, backupRoot, expected, skillrepo.ImportHooks{
			BeforePublish: func(snapshots []skillrepo.ImportSnapshot) error {
				prepared.Status = operation.BatchPublishing
				for _, snapshot := range snapshots {
					for _, stepIndex := range stepByIdentifier[snapshot.Identifier] {
						step := &prepared.Steps[stepIndex]
						if snapshot.Exists {
							step.BeforeState = snapshot.Checksum
						} else {
							step.BeforeState = "absent"
						}
						step.BackupPath = snapshot.Backup
						step.Status = "ready"
					}
				}
				return updateBatch()
			},
			AfterPublish: func(index int, entry skillrepo.Entry) error {
				if s.BeforeImportPublish != nil {
					if err := s.BeforeImportPublish(index, entry); err != nil {
						return err
					}
				}
				for _, stepIndex := range stepByIdentifier[entry.Identifier] {
					step := &prepared.Steps[stepIndex]
					if strings.TrimPrefix(step.Action, "publish:") == entry.Key {
						step.AfterState = entry.Metadata.Checksum
						step.Status = "published"
						step.RecoveryClass = "immutable-version-published"
						break
					}
				}
				return updateBatch()
			},
		})
		if err != nil {
			if errors.Is(err, skillrepo.ErrRollbackIncomplete) {
				prepared.Status = operation.BatchNeedsRecovery
				prepared.Error = err.Error()
			} else {
				prepared.Status = operation.BatchRolledBack
				prepared.Error = err.Error()
				s.removeImportSnapshot(transactionRoot)
				_ = discoveryStore.Abort(transactionID)
			}
			_ = batchStore.Update(prepared)
			if errors.Is(err, skillrepo.ErrConflict) || errors.Is(err, skillrepo.ErrUnsafe) {
				return ApplyResult{}, errors.Join(ErrStalePreview, err)
			}
			return ApplyResult{}, err
		}
	}
	if s.BeforeDiscoveryCommit != nil {
		if err := s.BeforeDiscoveryCommit(append([]string(nil), recordIDs...)); err != nil {
			prepared.Status = operation.BatchNeedsRecovery
			prepared.Error = err.Error()
			_ = batchStore.Update(prepared)
			return ApplyResult{}, fmt.Errorf("imported Skill version is durable but source discovery records need recovery: %w", err)
		}
	}
	if err := discoveryStore.Commit(recordIDs); err != nil {
		prepared.Status = operation.BatchNeedsRecovery
		prepared.Error = err.Error()
		_ = batchStore.Update(prepared)
		return ApplyResult{}, fmt.Errorf("imported Skill version is durable but source discovery records need recovery: %w", err)
	}
	prepared.Status = operation.BatchCommitted
	prepared.Error = ""
	for i := range prepared.Steps {
		if strings.HasPrefix(prepared.Steps[i].Action, "reuse:") {
			prepared.Steps[i].Status = "reused"
			prepared.Steps[i].RecoveryClass = "existing-immutable-version"
		}
	}
	if err := batchStore.Update(prepared); err != nil {
		return ApplyResult{}, fmt.Errorf("import and discovery records committed but batch marker needs recovery: %w", err)
	}
	// Once the write-ahead batch and source evidence are committed, import
	// snapshots are no longer needed: imported versions are immutable and are
	// deliberately retained even if later reference migration is undone.
	s.removeImportSnapshot(transactionRoot)
	entries := make([]skillrepo.Entry, 0, len(selectedGroups))
	for _, group := range selectedGroups {
		entry, err := repository.Resolve(group.Identifier, group.VersionKey)
		if err != nil {
			return ApplyResult{}, fmt.Errorf("resolve committed version %s/%s: %w", group.Identifier, group.VersionKey, err)
		}
		entries = append(entries, entry)
	}
	sort.Slice(recordIDs, func(i, j int) bool { return recordIDs[i] < recordIDs[j] })
	return ApplyResult{BatchID: transactionID, Imported: len(versions), Entries: entries, SourceRecordIDs: recordIDs}, nil
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
		group.IdentityConflict = group.IdentityConflict || source.IdentityConflict
		group.Sources = append(group.Sources, source)
		if source.ModifiedAt != nil && (group.ModifiedAt == nil || source.ModifiedAt.After(*group.ModifiedAt)) {
			modifiedAt := *source.ModifiedAt
			group.ModifiedAt = &modifiedAt
		}
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
	newestByIdentifier := map[string]time.Time{}
	newestCount := map[string]int{}
	groupCount := map[string]int{}
	missingTimestamp := map[string]bool{}
	for _, group := range result {
		groupCount[group.Identifier]++
		if group.ModifiedAt == nil || group.ModifiedAt.IsZero() {
			missingTimestamp[group.Identifier] = true
			continue
		}
		newest := newestByIdentifier[group.Identifier]
		if group.ModifiedAt.After(newest) {
			newestByIdentifier[group.Identifier] = *group.ModifiedAt
			newestCount[group.Identifier] = 1
		} else if group.ModifiedAt.Equal(newest) {
			newestCount[group.Identifier]++
		}
	}
	for i := range result {
		identifier := result[i].Identifier
		if modifiedAt := result[i].ModifiedAt; groupCount[identifier] > 1 && !missingTimestamp[identifier] && modifiedAt != nil && !modifiedAt.IsZero() && newestCount[identifier] == 1 && modifiedAt.Equal(newestByIdentifier[identifier]) {
			result[i].NewerLocalModification = true
		}
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
	sourcePath := group.Sources[0].RealPath
	if sourcePath == "" {
		return skillrepo.VersionMetadata{}, "", ErrStalePreview
	}
	skill, err := catalog.ReadDirectory(sourcePath)
	if err != nil {
		return skillrepo.VersionMetadata{}, "", err
	}
	if skill.Identifier == "" || len(group.Sources) == 0 {
		return skillrepo.VersionMetadata{}, "", ErrStalePreview
	}
	for _, source := range group.Sources {
		if source.Checksum != group.Checksum || (group.Identifier != source.Identifier && group.Identifier != source.ResolvedIdentifier) {
			return skillrepo.VersionMetadata{}, "", ErrStalePreview
		}
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
