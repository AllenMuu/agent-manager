package skillsync

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/AllenMuu/skill-manager/internal/adapter"
	"github.com/AllenMuu/skill-manager/internal/lifecycle"
	"github.com/AllenMuu/skill-manager/internal/operation"
	"github.com/AllenMuu/skill-manager/internal/skillrepo"
)

var ErrRecoveryConflict = errors.New("pending Skill synchronization batch has conflicting filesystem state")

type RecoveryAction string

const (
	RecoveryComplete RecoveryAction = "complete"
	RecoveryRollback RecoveryAction = "rollback"
	RecoveryResolve  RecoveryAction = "resolve-import"
)

type RecoveryStep struct {
	ID             string `json:"id"`
	Resource       string `json:"resource"`
	Action         string `json:"action"`
	BeforeState    string `json:"beforeState,omitempty"`
	CurrentState   string `json:"currentState,omitempty"`
	ExpectedAfter  string `json:"expectedAfter,omitempty"`
	Classification string `json:"classification"`
	Diagnostic     string `json:"diagnostic,omitempty"`
}

type RecoveryCandidate struct {
	BatchID    string           `json:"batchId"`
	Kind       string           `json:"kind"`
	Scope      string           `json:"scope"`
	Status     string           `json:"status"`
	Actions    []RecoveryAction `json:"actions"`
	Steps      []RecoveryStep   `json:"steps"`
	Diagnostic string           `json:"diagnostic,omitempty"`
}

type RecoveryPreview struct {
	Batches []RecoveryCandidate `json:"batches"`
}

// PreviewRecovery inspects pending batches and reports state without changing
// project files, repository versions, journals, or discovery records.
func (s *Service) PreviewRecovery() (RecoveryPreview, error) {
	store := operation.NewBatchStore(filepath.Join(s.syncRoot, "batches"))
	pending, err := store.Pending()
	if err != nil {
		return RecoveryPreview{}, err
	}
	preview := RecoveryPreview{Batches: make([]RecoveryCandidate, 0, len(pending))}
	for _, batch := range pending {
		if !s.recoveryBatchAccessible(batch) {
			continue
		}
		candidate, err := s.inspectRecoveryBatch(batch)
		if err != nil {
			return RecoveryPreview{}, err
		}
		preview.Batches = append(preview.Batches, candidate)
	}
	return preview, nil
}

func (s *Service) recoveryBatchAccessible(batch operation.Batch) bool {
	if batch.Kind == "import" {
		if batch.Scope != "local" || !pathsWithinRoot(s.repositoryRoot, batch.Resources) {
			return false
		}
		paths := make([]string, 0, len(batch.Steps))
		for _, step := range batch.Steps {
			paths = append(paths, step.Resource)
		}
		return len(paths) == 0 || pathsWithinRoot(s.repositoryRoot, paths)
	}

	var scopeRoot string
	switch batch.Scope {
	case "global":
		if s.globalJournal == nil {
			return false
		}
		scopeRoot = s.home
	case "project":
		if s.project == "" || s.projectJournal == nil {
			return false
		}
		scopeRoot = s.project
	default:
		return false
	}
	allowedRoots := []string{scopeRoot, s.repositoryRoot}
	if !pathsWithinRoots(allowedRoots, batch.Resources) {
		return false
	}

	// The paths that represent changed resources must stay inside the selected
	// scope. Supporting evidence may also refer to immutable versions in the
	// shared repository; constrain every path recovery may fingerprint or read.
	resourcePaths := make([]string, 0, len(batch.Steps)+len(batch.Plan.Changes))
	supportPaths := make([]string, 0, len(batch.Steps)*3+len(batch.LinkDependencies)*3)
	dependencyEntries := make([]string, 0, len(batch.LinkDependencies))
	dependencyEvidence := make([]string, 0, len(batch.LinkDependencies)*3)
	for _, step := range batch.Steps {
		resourcePaths = append(resourcePaths, step.Resource)
		for _, path := range []string{step.BackupPath, step.StagedPath, step.StageRoot} {
			if path != "" {
				supportPaths = append(supportPaths, path)
			}
		}
		if source := lifecycleExpectedSource(step.ExpectedAfter); source != "" && !filepath.IsAbs(source) {
			return false
		}
	}
	for _, change := range batch.Plan.Changes {
		resourcePaths = append(resourcePaths, change.Path)
	}
	if batch.Kind == "undo" {
		for _, dependency := range batch.LinkDependencies {
			dependencyEntries = append(dependencyEntries, dependency.EntryPath)
			dependencyEvidence = append(dependencyEvidence, dependency.RealPath)
			for _, hop := range dependency.LinkChain {
				dependencyEvidence = append(dependencyEvidence, hop.Path)
			}
		}
	}
	for _, materialization := range batch.Materializations {
		supportPaths = append(supportPaths, materialization.Destination, materialization.Source)
	}
	if batch.UndoEntry != nil {
		for _, snapshot := range append(append([]operation.Snapshot(nil), batch.UndoEntry.Before...), batch.UndoEntry.After...) {
			resourcePaths = append(resourcePaths, snapshot.Path)
			if snapshot.Backup != "" {
				supportPaths = append(supportPaths, snapshot.Backup)
			}
		}
		for _, materialization := range batch.UndoEntry.Materializations {
			supportPaths = append(supportPaths, materialization.Destination, materialization.Source)
		}
		for _, dependency := range batch.UndoEntry.LinkDependencies {
			dependencyEntries = append(dependencyEntries, dependency.EntryPath)
			dependencyEvidence = append(dependencyEvidence, dependency.RealPath)
			for _, hop := range dependency.LinkChain {
				dependencyEvidence = append(dependencyEvidence, hop.Path)
			}
		}
	}
	if len(dependencyEntries) > 0 && !pathsWithinRoot(scopeRoot, dependencyEntries) {
		return false
	}
	for _, path := range dependencyEvidence {
		if !filepath.IsAbs(path) {
			return false
		}
	}
	if len(resourcePaths) > 0 && !pathsWithinRoots(allowedRoots, resourcePaths) {
		return false
	}
	return len(supportPaths) == 0 || pathsWithinRoots([]string{scopeRoot, s.repositoryRoot}, supportPaths)
}

func lifecycleExpectedSource(expected string) string {
	parts := strings.Split(expected, "\x00")
	if len(parts) >= 2 && (parts[0] == "symlink" || parts[0] == "copy" || parts[0] == "auto") {
		return parts[1]
	}
	return ""
}

func pathsWithinRoot(root string, paths []string) bool {
	return pathsWithinRoots([]string{root}, paths)
}

func pathsWithinRoots(roots, paths []string) bool {
	if len(roots) == 0 || len(paths) == 0 {
		return false
	}
	absRoots := make([]string, 0, len(roots))
	for _, root := range roots {
		if root == "" {
			continue
		}
		absRoot, err := canonicalBoundaryPath(root, true)
		if err != nil {
			return false
		}
		absRoots = append(absRoots, absRoot)
	}
	if len(absRoots) == 0 {
		return false
	}
	for _, path := range paths {
		if path == "" {
			return false
		}
		abs, err := canonicalBoundaryPath(path, false)
		if err != nil {
			return false
		}
		within := false
		for _, root := range absRoots {
			rel, err := filepath.Rel(root, abs)
			if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel) {
				within = true
				break
			}
		}
		if !within {
			return false
		}
	}
	return true
}

// canonicalBoundaryPath resolves symlinked parent directories while keeping
// the final path component lexical. Recovery resources may themselves be
// symlinks, but their parent must remain inside the registered scope.
func canonicalBoundaryPath(path string, resolveFinal bool) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	abs = filepath.Clean(abs)
	if resolveFinal {
		if resolved, err := filepath.EvalSymlinks(abs); err == nil {
			return filepath.Clean(resolved), nil
		} else if !os.IsNotExist(err) {
			return "", err
		}
	}
	final := filepath.Base(abs)
	parent := filepath.Dir(abs)
	var suffix []string
	for {
		if resolved, err := filepath.EvalSymlinks(parent); err == nil {
			for i := len(suffix) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, suffix[i])
			}
			return filepath.Clean(filepath.Join(resolved, final)), nil
		} else if !os.IsNotExist(err) {
			return "", err
		}
		base := filepath.Base(parent)
		next := filepath.Dir(parent)
		if next == parent {
			return "", fmt.Errorf("cannot resolve an existing parent for %s", path)
		}
		suffix = append(suffix, base)
		parent = next
	}
}

// ApplyRecovery rechecks the whole batch under its operation lock before
// completing its journal/evidence commit or restoring its before-state.
func (s *Service) ApplyRecovery(batchID string, action RecoveryAction) (RecoveryCandidate, error) {
	store := operation.NewBatchStore(filepath.Join(s.syncRoot, "batches"))
	batch, ok, err := store.Get(batchID)
	if err != nil || !ok {
		return RecoveryCandidate{}, errors.Join(os.ErrNotExist, err)
	}
	if !s.recoveryBatchAccessible(batch) {
		return RecoveryCandidate{}, operation.ErrUnsafePath
	}
	if batch.Kind == "import" {
		unlock, err := s.lockImport()
		if err != nil {
			return RecoveryCandidate{}, err
		}
		defer unlock()
		batch, ok, err = store.Get(batchID)
		if err != nil || !ok {
			return RecoveryCandidate{}, errors.Join(os.ErrNotExist, err)
		}
		if !s.recoveryBatchAccessible(batch) {
			return RecoveryCandidate{}, operation.ErrUnsafePath
		}
		candidate, err := s.inspectRecoveryBatch(batch)
		if err != nil {
			return RecoveryCandidate{}, err
		}
		if action != RecoveryResolve || !hasAction(candidate.Actions, action) {
			return candidate, fmt.Errorf("recovery action %q is not available", action)
		}
		if err := store.CheckWritable(); err != nil {
			return candidate, err
		}
		return s.recoverImport(store, batch)
	}

	journal, err := s.journalForBatch(batch)
	if err != nil {
		return RecoveryCandidate{}, err
	}
	unlock, err := journal.LockOperation()
	if err != nil {
		return RecoveryCandidate{}, err
	}
	defer unlock()
	batch, ok, err = store.Get(batchID)
	if err != nil || !ok {
		return RecoveryCandidate{}, errors.Join(os.ErrNotExist, err)
	}
	if !s.recoveryBatchAccessible(batch) {
		return RecoveryCandidate{}, operation.ErrUnsafePath
	}
	candidate, err := s.inspectRecoveryBatch(batch)
	if err != nil {
		return RecoveryCandidate{}, err
	}
	if !hasAction(candidate.Actions, action) {
		return candidate, fmt.Errorf("recovery action %q is not available", action)
	}
	if err := store.CheckWritable(); err != nil {
		return candidate, err
	}
	if action == RecoveryComplete {
		return s.completeLifecycleBatch(store, journal, batch, candidate)
	}
	return s.rollbackLifecycleBatch(store, journal, batch, candidate)
}

func (s *Service) inspectRecoveryBatch(batch operation.Batch) (RecoveryCandidate, error) {
	candidate := RecoveryCandidate{BatchID: batch.ID, Kind: batch.Kind, Scope: batch.Scope, Status: string(batch.Status), Steps: make([]RecoveryStep, 0, len(batch.Steps))}
	if batch.Kind == "import" {
		return s.inspectImportBatch(batch, candidate)
	}
	allAfter, allSafe := true, true
	for _, step := range batch.Steps {
		state, exists, err := fingerprintIfExists(step.Resource)
		if err != nil {
			return RecoveryCandidate{}, err
		}
		classification := "conflict"
		beforeMatch := step.BeforeState != "" && state == step.BeforeState
		afterMatch, afterSourceChanged := false, false
		if step.AfterState != "" && state == step.AfterState {
			if step.ExpectedAfter == "" {
				afterMatch = true
			} else {
				afterMatch, err = matchesExpectedLifecycleState(step.Resource, step.ExpectedAfter, exists)
				if err != nil {
					return RecoveryCandidate{}, err
				}
			}
			if !afterMatch {
				afterSourceChanged, err = matchesLifecycleDestinationState(step.Resource, step.ExpectedAfter, exists)
				if err != nil {
					return RecoveryCandidate{}, err
				}
			}
		} else if !exists {
			afterMatch = step.ExpectedAfter == "absent"
		} else {
			afterMatch, err = matchesExpectedLifecycleState(step.Resource, step.ExpectedAfter, exists)
			if err != nil {
				return RecoveryCandidate{}, err
			}
			if !afterMatch {
				afterSourceChanged, err = matchesLifecycleDestinationState(step.Resource, step.ExpectedAfter, exists)
				if err != nil {
					return RecoveryCandidate{}, err
				}
			}
		}
		if beforeMatch && afterMatch {
			classification = "unchanged"
		} else if afterMatch {
			classification = "after"
		} else if beforeMatch {
			classification = "before"
		} else if afterSourceChanged {
			classification = "after-source-changed"
		} else if !exists && step.StagedPath != "" && step.StagedState != "" && (step.StagedState == step.BeforeState || step.StagedState == step.AfterState) {
			stagedState, stagedExists, stagedErr := fingerprintIfExists(step.StagedPath)
			if stagedErr != nil {
				return RecoveryCandidate{}, stagedErr
			}
			if stagedExists && stagedState == step.StagedState {
				classification = "staged"
			}
		}
		if classification != "after" && classification != "unchanged" {
			allAfter = false
		}
		if classification == "conflict" {
			allSafe = false
		}
		if !exists {
			state = "absent"
		}
		stepResult := RecoveryStep{ID: step.ID, Resource: step.Resource, Action: step.Action, BeforeState: step.BeforeState, CurrentState: state, ExpectedAfter: step.ExpectedAfter, Classification: classification}
		if classification == "conflict" {
			stepResult.Diagnostic = "current state matches neither the recorded before-state nor intended after-state"
		} else if classification == "after-source-changed" {
			stepResult.Diagnostic = "materialized source content changed after publication; completion is blocked, but rollback remains available"
		}
		candidate.Steps = append(candidate.Steps, stepResult)
	}
	if allAfter {
		candidate.Actions = append(candidate.Actions, RecoveryComplete)
	}
	if allSafe {
		candidate.Actions = append(candidate.Actions, RecoveryRollback)
	}
	if journal, err := s.journalForBatch(batch); err == nil {
		committed, err := journal.HasBatch(batch.ID)
		if err != nil {
			return RecoveryCandidate{}, err
		}
		if committed {
			candidate.Actions = []RecoveryAction{RecoveryComplete}
			for _, step := range candidate.Steps {
				if step.Classification != "after" && step.Classification != "unchanged" {
					candidate.Actions = nil
					candidate.Diagnostic = ErrRecoveryConflict.Error()
					break
				}
			}
		}
	}
	if batch.Kind == "undo" {
		journal, err := s.journalForBatch(batch)
		if err != nil {
			candidate.Actions = nil
			candidate.Diagnostic = err.Error()
			return candidate, nil
		}
		if batch.UndoEntry == nil {
			candidate.Actions = nil
			candidate.Diagnostic = "Undo batch is missing its journal entry evidence."
		} else {
			present, err := journal.ContainsEntry(*batch.UndoEntry)
			if err != nil {
				return RecoveryCandidate{}, err
			}
			if !present {
				if allAfter {
					candidate.Actions = []RecoveryAction{RecoveryComplete}
				} else {
					candidate.Actions = nil
				}
			}
			if hasAction(candidate.Actions, RecoveryComplete) {
				if err := lifecycle.ValidateLinkDependenciesWithBefore(batch.LinkDependencies, batch.UndoEntry.Before); err != nil {
					candidate.Actions = removeRecoveryAction(candidate.Actions, RecoveryComplete)
					candidate.Diagnostic = errors.Join(ErrRecoveryConflict, err).Error()
				}
			}
		}
	}
	if len(candidate.Actions) == 0 {
		candidate.Diagnostic = ErrRecoveryConflict.Error()
	}
	return candidate, nil
}

func (s *Service) inspectImportBatch(batch operation.Batch, candidate RecoveryCandidate) (RecoveryCandidate, error) {
	discovery := NewDiscoveryStore(filepath.Join(s.syncRoot, "discoveries"))
	records, err := discovery.ForBatch(batch.ID)
	if err != nil {
		return RecoveryCandidate{}, err
	}
	recordChecksums := make(map[string]string, len(records))
	for _, record := range records {
		recordChecksums[record.Identifier+"\x00"+record.VersionKey] = record.Checksum
	}
	canResolve, anyPublished, anyConflict := true, false, false
	for _, step := range batch.Steps {
		parts := strings.SplitN(step.Action, ":", 2)
		if len(parts) != 2 || (parts[0] != "publish" && parts[0] != "reuse") {
			return RecoveryCandidate{}, fmt.Errorf("batch %s has invalid import intent", batch.ID)
		}
		checksum := step.ExpectedAfter
		if checksum == "" {
			checksum = recordChecksums[filepath.Base(step.Resource)+"\x00"+parts[1]]
		}
		versionPath := importVersionPath(step.Resource, parts[1])
		actual, exists, fingerprintErr := fingerprintIfExists(versionPath)
		if fingerprintErr != nil {
			return RecoveryCandidate{}, fingerprintErr
		}
		classification := "before"
		if exists {
			if checksum == "" || checksum != actualTreeChecksum(versionPath) {
				classification, anyConflict = "conflict", true
			} else {
				classification, anyPublished = "after", true
			}
		}
		if parts[0] == "reuse" && !exists {
			classification, anyConflict = "conflict", true
		}
		if classification == "conflict" {
			canResolve = false
		}
		candidate.Steps = append(candidate.Steps, RecoveryStep{ID: step.ID, Resource: versionPath, Action: step.Action, BeforeState: step.BeforeState, CurrentState: actual, ExpectedAfter: checksum, Classification: classification,
			Diagnostic: recoveryDiagnostic(classification)})
	}
	if canResolve {
		candidate.Actions = []RecoveryAction{RecoveryResolve}
		if !anyPublished {
			candidate.Diagnostic = "No new immutable versions were published; staged discovery records will be discarded."
		} else if anyConflict {
			candidate.Diagnostic = ErrRecoveryConflict.Error()
		} else {
			candidate.Diagnostic = "Matching immutable versions will be retained and their source discovery records resolved."
		}
	} else {
		candidate.Diagnostic = ErrRecoveryConflict.Error()
	}
	return candidate, nil
}

func importVersionPath(identityPath, versionKey string) string {
	if versionKey == "legacy" {
		return identityPath
	}
	return filepath.Join(identityPath, "versions", versionKey)
}

func (s *Service) recoverImport(store *operation.BatchStore, batch operation.Batch) (RecoveryCandidate, error) {
	discovery := NewDiscoveryStore(filepath.Join(s.syncRoot, "discoveries"))
	records, err := discovery.ForBatch(batch.ID)
	if err != nil {
		return RecoveryCandidate{}, err
	}
	checksums := map[string]string{}
	for _, record := range records {
		checksums[record.Identifier+"\x00"+record.VersionKey] = record.Checksum
	}
	versionActions := make(map[string]string, len(batch.Steps))
	for _, step := range batch.Steps {
		parts := strings.SplitN(step.Action, ":", 2)
		if len(parts) == 2 {
			versionActions[filepath.Base(step.Resource)+"\x00"+parts[1]] = parts[0]
		}
	}
	repository, err := skillrepo.Open(s.repositoryRoot)
	if err != nil {
		return RecoveryCandidate{}, err
	}
	commitIDs, abortIDs := []string{}, []string{}
	reconciled := make(map[string]bool)
	for _, record := range records {
		path := importVersionPath(filepath.Join(s.repositoryRoot, record.Identifier), record.VersionKey)
		checksum, err := skillrepo.ChecksumTree(path)
		if err == nil && checksum == record.Checksum {
			versionID := record.Identifier + "\x00" + record.VersionKey
			if !reconciled[versionID] {
				switch versionActions[versionID] {
				case "publish":
					if record.VersionMetadata.Key != "" {
						if _, err := repository.RecoverPublishedVersion(record.Identifier, record.VersionMetadata, record.MakeCurrent); err != nil {
							return RecoveryCandidate{}, errors.Join(ErrRecoveryConflict, err)
						}
					} else if entry, err := repository.Resolve(record.Identifier, record.VersionKey); err != nil || entry.Metadata.Checksum != record.Checksum {
						return RecoveryCandidate{}, errors.Join(ErrRecoveryConflict, err)
					}
				case "reuse":
					entry, err := repository.Resolve(record.Identifier, record.VersionKey)
					if err != nil || (entry.Metadata.Checksum != "" && entry.Metadata.Checksum != record.Checksum) {
						return RecoveryCandidate{}, errors.Join(ErrRecoveryConflict, err)
					}
				default:
					return RecoveryCandidate{}, ErrRecoveryConflict
				}
				reconciled[versionID] = true
			}
			commitIDs = append(commitIDs, record.ID)
		} else if os.IsNotExist(err) {
			abortIDs = append(abortIDs, record.ID)
		} else if err != nil {
			return RecoveryCandidate{}, errors.Join(ErrRecoveryConflict, err)
		} else {
			return RecoveryCandidate{}, ErrRecoveryConflict
		}
	}
	for _, step := range batch.Steps {
		parts := strings.SplitN(step.Action, ":", 2)
		if len(parts) != 2 || parts[0] != "publish" || step.BeforeState != "absent" {
			continue
		}
		identifier := filepath.Base(step.Resource)
		publishedForIdentity := false
		for _, candidate := range batch.Steps {
			candidateParts := strings.SplitN(candidate.Action, ":", 2)
			if len(candidateParts) == 2 && filepath.Base(candidate.Resource) == identifier && reconciled[identifier+"\x00"+candidateParts[1]] {
				publishedForIdentity = true
				break
			}
		}
		if publishedForIdentity {
			continue
		}
		if filepath.Clean(step.Resource) != filepath.Join(s.repositoryRoot, identifier) {
			return RecoveryCandidate{}, ErrRecoveryConflict
		}
		if err := removeEmptyUnpublishedIdentity(step.Resource); err != nil {
			batch.Status, batch.Error = operation.BatchNeedsRecovery, err.Error()
			if updateErr := store.Update(batch); updateErr != nil {
				return RecoveryCandidate{}, errors.Join(err, updateErr)
			}
			return RecoveryCandidate{}, err
		}
	}
	if len(commitIDs) > 0 {
		if err := discovery.Commit(commitIDs); err != nil {
			batch.Status, batch.Error = operation.BatchNeedsRecovery, err.Error()
			_ = store.Update(batch)
			return RecoveryCandidate{}, err
		}
	}
	if len(abortIDs) > 0 {
		if err := discovery.AbortIDs(abortIDs); err != nil {
			batch.Status, batch.Error = operation.BatchNeedsRecovery, err.Error()
			_ = store.Update(batch)
			return RecoveryCandidate{}, err
		}
	}
	if len(commitIDs) == 0 {
		batch.Status = operation.BatchRolledBack
		batch.Error = "recovered before any immutable version was published"
	} else {
		batch.Status = operation.BatchCommitted
		batch.Error = "recovered source records for published immutable versions"
	}
	for i := range batch.Steps {
		parts := strings.SplitN(batch.Steps[i].Action, ":", 2)
		if len(parts) != 2 {
			continue
		}
		if checksums[filepath.Base(batch.Steps[i].Resource)+"\x00"+parts[1]] != "" {
			batch.Steps[i].RecoveryClass = "version-retained"
		} else {
			batch.Steps[i].RecoveryClass = "unpublished-version"
		}
	}
	if err := store.Update(batch); err != nil {
		return RecoveryCandidate{}, err
	}
	if batch.Status == operation.BatchCommitted {
		s.removeImportSnapshot(filepath.Join(s.syncRoot, "import-snapshots", batch.ID))
	}
	return s.inspectRecoveryBatch(batch)
}

// removeEmptyUnpublishedIdentity cleans only the directory shell created by an
// import that had no prior identity and published no version. Non-empty stage
// directories or any unexpected entries remain untouched and block recovery.
func removeEmptyUnpublishedIdentity(identity string) error {
	info, err := os.Lstat(identity)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return ErrRecoveryConflict
	}
	identityEntries, err := os.ReadDir(identity)
	if err != nil {
		return err
	}
	if len(identityEntries) == 0 {
		if err := os.Remove(identity); err != nil {
			return err
		}
		return operation.SyncPaths([]string{identity})
	}
	if len(identityEntries) != 1 || identityEntries[0].Name() != "versions" || !identityEntries[0].IsDir() {
		return ErrRecoveryConflict
	}
	versions := filepath.Join(identity, "versions")
	versionsInfo, err := os.Lstat(versions)
	if err != nil {
		return err
	}
	if !versionsInfo.IsDir() || versionsInfo.Mode()&os.ModeSymlink != 0 {
		return ErrRecoveryConflict
	}
	stages, err := os.ReadDir(versions)
	if err != nil {
		return err
	}
	for _, stage := range stages {
		if !strings.HasPrefix(stage.Name(), ".stage-") || !stage.IsDir() {
			return ErrRecoveryConflict
		}
		stagePath := filepath.Join(versions, stage.Name())
		stageInfo, err := os.Lstat(stagePath)
		if err != nil {
			return err
		}
		if !stageInfo.IsDir() || stageInfo.Mode()&os.ModeSymlink != 0 {
			return ErrRecoveryConflict
		}
		contents, err := os.ReadDir(stagePath)
		if err != nil {
			return err
		}
		if len(contents) != 0 {
			return ErrRecoveryConflict
		}
	}
	for _, stage := range stages {
		if err := os.Remove(filepath.Join(versions, stage.Name())); err != nil {
			return err
		}
	}
	if err := os.Remove(versions); err != nil {
		return err
	}
	if err := os.Remove(identity); err != nil {
		return err
	}
	return operation.SyncPaths([]string{identity})
}

func (s *Service) completeLifecycleBatch(store *operation.BatchStore, journal *operation.Journal, batch operation.Batch, candidate RecoveryCandidate) (RecoveryCandidate, error) {
	if batch.Kind == "undo" {
		if batch.UndoEntry == nil {
			return candidate, ErrRecoveryConflict
		}
		if err := lifecycle.ValidateLinkDependenciesWithBefore(batch.LinkDependencies, batch.UndoEntry.Before); err != nil {
			return candidate, errors.Join(ErrRecoveryConflict, err)
		}
		if _, err := journal.RemoveLatestEntry(*batch.UndoEntry); err != nil {
			return candidate, err
		}
		batch.Status, batch.Error = operation.BatchCommitted, "recovered completed Undo journal commit"
		for i := range batch.Steps {
			batch.Steps[i].RecoveryClass = "undo-complete"
		}
		if err := store.Update(batch); err != nil {
			return candidate, err
		}
		return candidate, nil
	}
	hasRecord, err := journal.HasBatch(batch.ID)
	if err != nil {
		return RecoveryCandidate{}, err
	}
	if !hasRecord {
		for _, step := range candidate.Steps {
			if step.Classification != "after" && step.Classification != "unchanged" {
				return candidate, ErrRecoveryConflict
			}
		}
		before, err := recoveryBeforeSnapshots(batch)
		if err != nil {
			return candidate, err
		}
		paths := make([]string, len(batch.Steps))
		for i := range batch.Steps {
			paths[i] = batch.Steps[i].Resource
		}
		after, err := journal.Capture(paths)
		if err != nil {
			return candidate, err
		}
		if err := operation.SyncSnapshots(after); err != nil {
			removeRecoverySnapshots(after)
			return candidate, err
		}
		materials, err := recoveryMaterializations(batch)
		if err != nil {
			removeRecoverySnapshots(after)
			return candidate, err
		}
		plan := batch.Plan
		if plan.Operation == "" {
			plan = operation.NewPlan(batch.Kind)
			for _, step := range batch.Steps {
				plan.Changes = append(plan.Changes, operation.Change{Path: step.Resource, Action: step.Action})
			}
		}
		if err := journal.RecordPlanWithEvidence(plan, before, after, materials, batch.ID, batch.LinkDependencies); err != nil {
			if !errors.Is(err, operation.ErrJournalCommitted) {
				removeRecoverySnapshots(after)
			}
			return candidate, err
		}
	}
	batch.Status, batch.Error = operation.BatchCommitted, ""
	for i := range batch.Steps {
		batch.Steps[i].RecoveryClass = "after-state-committed"
	}
	if err := store.Update(batch); err != nil {
		return candidate, err
	}
	return candidate, nil
}

func (s *Service) rollbackLifecycleBatch(store *operation.BatchStore, journal *operation.Journal, batch operation.Batch, candidate RecoveryCandidate) (RecoveryCandidate, error) {
	for _, step := range candidate.Steps {
		if step.Classification == "conflict" {
			return candidate, ErrRecoveryConflict
		}
	}
	before, err := recoveryBeforeSnapshots(batch)
	if err != nil {
		return candidate, err
	}
	if batch.Kind != "undo" {
		if err := lifecycle.ValidateLinkDependenciesWithBefore(batch.LinkDependencies, before); err != nil {
			return candidate, errors.Join(ErrRecoveryConflict, err)
		}
	}
	var restore []operation.Snapshot
	for i, step := range candidate.Steps {
		if step.Classification == "after" || step.Classification == "after-source-changed" || step.Classification == "staged" {
			restore = append(restore, before[i])
		}
	}
	if len(restore) > 0 {
		expectedCurrent := make(map[string]string, len(restore))
		for _, step := range candidate.Steps {
			if step.Classification == "after" || step.Classification == "after-source-changed" || step.Classification == "staged" {
				expectedCurrent[step.Resource] = step.CurrentState
			}
		}
		previousStageHook := journal.BeforeRestoreStage
		journal.BeforeRestoreStage = func(path, stagedPath string) error {
			if previousStageHook != nil {
				if err := previousStageHook(path, stagedPath); err != nil {
					return err
				}
			}
			state, err := operation.FingerprintPath(path)
			if err != nil {
				return err
			}
			for i := range batch.Steps {
				if filepath.Clean(batch.Steps[i].Resource) == filepath.Clean(path) {
					batch.Steps[i].StagedPath = stagedPath
					batch.Steps[i].StageRoot = filepath.Dir(stagedPath)
					batch.Steps[i].StagedState = state
					batch.Steps[i].Status = "restoring-current"
					return store.Update(batch)
				}
			}
			return ErrRecoveryConflict
		}
		err := journal.RestoreExpected(restore, expectedCurrent)
		journal.BeforeRestoreStage = previousStageHook
		if err != nil {
			batch.Status, batch.Error = operation.BatchNeedsRecovery, err.Error()
			_ = store.Update(batch)
			return candidate, err
		}
	}
	for i := range batch.Steps {
		actual, exists, err := fingerprintIfExists(batch.Steps[i].Resource)
		if err != nil {
			return candidate, err
		}
		want := batch.Steps[i].BeforeState
		if !exists {
			actual = "absent"
		}
		if actual != want {
			return candidate, ErrRecoveryConflict
		}
		if err := removeVerifiedStage(&batch.Steps[i], batch.Kind); err != nil {
			batch.Status, batch.Error = operation.BatchNeedsRecovery, err.Error()
			_ = store.Update(batch)
			return candidate, err
		}
		batch.Steps[i].RecoveryClass = "before-state-restored"
	}
	batch.Status, batch.Error = operation.BatchRolledBack, "recovered to the recorded before-state"
	if err := store.Update(batch); err != nil {
		return candidate, err
	}
	return candidate, nil
}

func removeVerifiedStage(step *operation.BatchStep, batchKind string) error {
	if step.StagedPath == "" {
		return nil
	}
	stagePath, err := filepath.Abs(step.StagedPath)
	if err != nil {
		return err
	}
	stageRoot, err := filepath.Abs(step.StageRoot)
	if err != nil || filepath.Dir(filepath.Clean(stagePath)) != filepath.Clean(stageRoot) {
		return errors.Join(ErrRecoveryConflict, err)
	}
	rootInfo, err := os.Lstat(stageRoot)
	if os.IsNotExist(err) {
		step.StagedPath, step.StageRoot, step.StagedState = "", "", ""
		return nil
	}
	if err != nil {
		return err
	}
	if !rootInfo.IsDir() || rootInfo.Mode()&os.ModeSymlink != 0 {
		return ErrRecoveryConflict
	}
	name := filepath.Base(stageRoot)
	if !strings.HasPrefix(name, ".skill-manager-replace-") && !strings.HasPrefix(name, ".skill-manager-restore-") {
		return ErrRecoveryConflict
	}
	state, exists, err := fingerprintIfExists(stagePath)
	if err != nil {
		return err
	}
	if exists {
		if state != step.StagedState {
			return ErrRecoveryConflict
		}
		if err := os.RemoveAll(stagePath); err != nil {
			return err
		}
	}
	candidatePath := filepath.Join(stageRoot, "candidate")
	if _, err := os.Lstat(candidatePath); err == nil {
		candidateExpected := step.BeforeState
		if batchKind == "undo" {
			parts := strings.SplitN(step.ExpectedAfter, "\x00", 2)
			if len(parts) != 2 || parts[0] != "fingerprint" {
				return ErrRecoveryConflict
			}
			candidateExpected = parts[1]
		}
		candidateState, candidateExists, err := fingerprintIfExists(candidatePath)
		if err != nil {
			return err
		}
		if !candidateExists || candidateState != candidateExpected {
			return ErrRecoveryConflict
		}
		if err := os.RemoveAll(candidatePath); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.Remove(stageRoot); err != nil && !os.IsNotExist(err) {
		return err
	}
	step.StagedPath, step.StageRoot, step.StagedState = "", "", ""
	return nil
}

func (s *Service) journalForBatch(batch operation.Batch) (*operation.Journal, error) {
	switch batch.Scope {
	case "global":
		if s.globalJournal == nil {
			return nil, fmt.Errorf("global operation journal is unavailable for batch %s", batch.ID)
		}
		return s.globalJournal, nil
	case "project":
		if s.projectJournal == nil {
			return nil, fmt.Errorf("registered project operation journal is unavailable for batch %s", batch.ID)
		}
		return s.projectJournal, nil
	default:
		return nil, fmt.Errorf("unsupported recovery scope %q", batch.Scope)
	}
}

func recoveryBeforeSnapshots(batch operation.Batch) ([]operation.Snapshot, error) {
	result := make([]operation.Snapshot, len(batch.Steps))
	for i, step := range batch.Steps {
		if step.BeforeState == "" {
			return nil, fmt.Errorf("batch %s is missing before-state evidence", batch.ID)
		}
		snapshot := operation.Snapshot{Path: step.Resource, Exists: step.BeforeState != "absent", Backup: step.BackupPath}
		if snapshot.Exists {
			if snapshot.Backup == "" {
				return nil, fmt.Errorf("batch %s is missing a required recovery backup", batch.ID)
			}
			fingerprint, err := operation.FingerprintPath(snapshot.Backup)
			if err != nil || fingerprint != step.BeforeState {
				return nil, errors.Join(ErrRecoveryConflict, err)
			}
		}
		result[i] = snapshot
	}
	return result, nil
}

func recoveryMaterializations(batch operation.Batch) ([]operation.Materialization, error) {
	if len(batch.Materializations) > 0 {
		result := make([]operation.Materialization, len(batch.Materializations))
		for i, item := range batch.Materializations {
			destination, err := filepath.Abs(item.Destination)
			if err != nil {
				return nil, err
			}
			source, err := filepath.Abs(item.Source)
			if err != nil {
				return nil, err
			}
			intentSource, sourceFingerprint, ok := expectedMaterializationSource(batch, destination)
			if !ok || filepath.Clean(source) != filepath.Clean(intentSource) || item.SourceFingerprint != "" && item.SourceFingerprint != sourceFingerprint {
				return nil, ErrRecoveryConflict
			}
			item.Destination, item.Source = destination, source
			item.SourceFingerprint = sourceFingerprint
			result[i] = item
		}
		return result, nil
	}
	var result []operation.Materialization
	for _, change := range batch.Plan.Changes {
		if strings.Contains(strings.ToLower(change.Action), "remove") {
			continue
		}
		source := change.SourcePath
		if source == "" {
			source, _, _ = adapter.ParseMaterializationDetail(change.Detail)
		}
		if source == "" {
			continue
		}
		destination, err := filepath.Abs(change.Path)
		if err != nil {
			return nil, err
		}
		source, err = filepath.Abs(source)
		if err != nil {
			return nil, err
		}
		intentSource, sourceFingerprint, ok := expectedMaterializationSource(batch, destination)
		if !ok || filepath.Clean(source) != filepath.Clean(intentSource) {
			return nil, ErrRecoveryConflict
		}
		info, err := os.Lstat(destination)
		if err != nil {
			return nil, err
		}
		method := ""
		if info.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(destination)
			if err != nil {
				return nil, err
			}
			if !filepath.IsAbs(target) {
				target = filepath.Clean(filepath.Join(filepath.Dir(destination), target))
			}
			if filepath.Clean(target) != filepath.Clean(source) {
				return nil, ErrRecoveryConflict
			}
			method = "symlink"
		} else if info.IsDir() {
			destinationFingerprint, err := operation.FingerprintPath(destination)
			if err != nil || destinationFingerprint != sourceFingerprint {
				return nil, errors.Join(ErrRecoveryConflict, err)
			}
			method = "copy"
		} else {
			return nil, ErrRecoveryConflict
		}
		version := "legacy"
		if filepath.Base(filepath.Dir(source)) == "versions" {
			version = filepath.Base(source)
		}
		result = append(result, operation.Materialization{Destination: destination, Source: source, SourceVersion: version, Method: method, SourceFingerprint: sourceFingerprint})
	}
	return result, nil
}

func expectedMaterializationSource(batch operation.Batch, destination string) (string, string, bool) {
	for _, step := range batch.Steps {
		stepPath, err := filepath.Abs(step.Resource)
		if err != nil || filepath.Clean(stepPath) != filepath.Clean(destination) {
			continue
		}
		parts := strings.Split(step.ExpectedAfter, "\x00")
		if len(parts) != 3 || parts[0] != "symlink" && parts[0] != "copy" && parts[0] != "auto" {
			return "", "", false
		}
		source, err := filepath.Abs(parts[1])
		if err != nil || parts[2] == "" {
			return "", "", false
		}
		return filepath.Clean(source), parts[2], true
	}
	return "", "", false
}

func matchesExpectedLifecycleState(path, expected string, exists bool) (bool, error) {
	if expected == "" {
		return false, nil
	}
	if expected == "absent" {
		return !exists, nil
	}
	parts := strings.Split(expected, "\x00")
	if len(parts) < 2 {
		return false, nil
	}
	checkLink := func(target string) (bool, error) {
		info, err := os.Lstat(path)
		if os.IsNotExist(err) || err == nil && info.Mode()&os.ModeSymlink == 0 {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		actual, err := os.Readlink(path)
		return err == nil && actual == target, err
	}
	checkCopy := func(fingerprint string) (bool, error) {
		info, err := os.Lstat(path)
		if os.IsNotExist(err) || err == nil && !info.IsDir() {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		actual, err := operation.FingerprintPath(path)
		return err == nil && actual == fingerprint, err
	}
	switch parts[0] {
	case "fingerprint":
		if parts[1] == "absent" {
			_, err := os.Lstat(path)
			if os.IsNotExist(err) {
				return true, nil
			}
			return false, err
		}
		actual, err := operation.FingerprintPath(path)
		return err == nil && actual == parts[1], err
	case "symlink":
		if len(parts) != 3 {
			return false, nil
		}
		matches, err := sourceMatchesFingerprint(parts[1], parts[2])
		if err != nil || !matches {
			return false, err
		}
		return checkLink(parts[1])
	case "copy":
		if len(parts) != 3 {
			return false, nil
		}
		matches, err := sourceMatchesFingerprint(parts[1], parts[2])
		if err != nil || !matches {
			return false, err
		}
		return checkCopy(parts[2])
	case "auto":
		if len(parts) != 3 {
			return false, nil
		}
		matches, err := sourceMatchesFingerprint(parts[1], parts[2])
		if err != nil || !matches {
			return false, err
		}
		link, err := checkLink(parts[1])
		if link || err != nil {
			return link, err
		}
		return checkCopy(parts[2])
	default:
		return false, nil
	}
}

func matchesLifecycleDestinationState(path, expected string, exists bool) (bool, error) {
	if !exists || expected == "" || expected == "absent" {
		return false, nil
	}
	parts := strings.Split(expected, "\x00")
	if len(parts) < 2 {
		return false, nil
	}
	checkLink := func(target string) (bool, error) {
		info, err := os.Lstat(path)
		if os.IsNotExist(err) || err == nil && info.Mode()&os.ModeSymlink == 0 {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		actual, err := os.Readlink(path)
		return err == nil && actual == target, err
	}
	checkCopy := func(fingerprint string) (bool, error) {
		info, err := os.Lstat(path)
		if os.IsNotExist(err) || err == nil && !info.IsDir() {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		actual, err := operation.FingerprintPath(path)
		return err == nil && actual == fingerprint, err
	}
	switch parts[0] {
	case "symlink":
		return checkLink(parts[1])
	case "copy":
		fingerprint := parts[1]
		if len(parts) == 3 {
			fingerprint = parts[2]
		}
		return checkCopy(fingerprint)
	case "auto":
		if len(parts) != 3 {
			return false, nil
		}
		link, err := checkLink(parts[1])
		if link || err != nil {
			return link, err
		}
		return checkCopy(parts[2])
	default:
		return false, nil
	}
}

func sourceMatchesFingerprint(source, expected string) (bool, error) {
	actual, err := operation.FingerprintPath(source)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return actual == expected, nil
}

func removeRecoveryAction(actions []RecoveryAction, remove RecoveryAction) []RecoveryAction {
	result := make([]RecoveryAction, 0, len(actions))
	for _, action := range actions {
		if action != remove {
			result = append(result, action)
		}
	}
	return result
}

func fingerprintIfExists(path string) (string, bool, error) {
	if _, err := os.Lstat(path); os.IsNotExist(err) {
		return "absent", false, nil
	} else if err != nil {
		return "", false, err
	}
	fingerprint, err := operation.FingerprintPath(path)
	return fingerprint, err == nil, err
}

func actualTreeChecksum(path string) string {
	checksum, _ := skillrepo.ChecksumTree(path)
	return checksum
}

func hasAction(actions []RecoveryAction, wanted RecoveryAction) bool {
	for _, action := range actions {
		if action == wanted {
			return true
		}
	}
	return false
}

func recoveryDiagnostic(classification string) string {
	if classification == "conflict" {
		return "filesystem state does not match the recorded import version"
	}
	return ""
}

func removeRecoverySnapshots(snapshots []operation.Snapshot) {
	for _, snapshot := range snapshots {
		if snapshot.Exists && snapshot.Backup != "" {
			_ = os.RemoveAll(snapshot.Backup)
		}
	}
}
