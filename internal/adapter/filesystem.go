package adapter

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/AllenMuu/skill-manager/internal/operation"
	"github.com/AllenMuu/skill-manager/internal/resource"
)

var (
	// ErrPlacementConfiguration indicates that a guarded placement context was
	// not supplied to the generic adapter seam.
	ErrPlacementConfiguration = errors.New("filesystem placement is not configured")
	// ErrForceRequired is returned before confirmation when replacement would
	// remove an unmanaged destination.
	ErrForceRequired = errors.New("conflict strategy requires force confirmation")
	// ErrUnsafePath protects source and destination ownership boundaries.
	ErrUnsafePath = errors.New("refusing unmanaged or unexpected path")
	// ErrNotConfirmed indicates that no mutation was authorized.
	ErrNotConfirmed = operation.ErrNotConfirmed
	// ErrLateConflict identifies a no-replace publication that observed a new
	// owner after confirmation. Callers can preserve that owner while rolling
	// back other paths in a larger transaction.
	ErrLateConflict   = errors.New("destination changed during guarded publication")
	errLateConflict   = ErrLateConflict
	errStagePreserved = errors.New("staged destination preserved")
)

// PreservedPathError reports a publication failure for which the staged
// destination was deliberately retained or recovered. Callers coordinating a
// larger transaction must exclude Path from snapshot rollback: restoring the
// pre-operation snapshot there could overwrite an owner that appeared during
// the guarded publication.
type PreservedPathError struct {
	Path string
	Err  error
}

func (e *PreservedPathError) Error() string { return e.Err.Error() }
func (e *PreservedPathError) Unwrap() error { return e.Err }

// RemoveManagedLink removes a link only through the anchored project parent
// after confirming its exact target. It is used by lifecycle operations that
// must not follow a swapped project parent.
func RemoveManagedLink(project, destination, source string) error {
	parent, err := openAnchoredPublicationDirectoryNoCreate(project, filepath.Dir(destination))
	if err != nil {
		return err
	}
	defer parent.Close()
	if !parent.Current() {
		return errors.Join(ErrUnsafePath, ErrLateConflict)
	}
	name := filepath.Base(destination)
	target, err := parent.Readlink(name)
	if err != nil {
		return err
	}
	if target != source {
		return errors.Join(ErrUnsafePath, ErrLateConflict)
	}
	return parent.Remove(name)
}

// PublishCopy publishes a prepared project-local copy through an anchored
// destination parent. The parent is opened without creating missing
// directories, so a disappeared or replaced project tree fails closed.
func PublishCopy(project, destination, candidate string) error {
	parent, err := openAnchoredPublicationDirectoryNoCreate(project, filepath.Dir(destination))
	if err != nil {
		return err
	}
	defer parent.Close()
	if !parent.Current() {
		return errors.Join(ErrUnsafePath, ErrLateConflict)
	}
	if exists, err := parent.Exists(filepath.Base(destination)); err != nil {
		return err
	} else if exists {
		return errors.Join(ErrUnsafePath, ErrLateConflict)
	}
	return parent.RenameFrom(candidate, filepath.Base(destination))
}

// PublishStagedLink atomically moves a previously-created staged link into a
// managed project parent after rechecking the expected current owner. The
// destination parent is opened without following replaced ancestors.
func PublishStagedLink(project, destination, stage, expectedTarget string) error {
	parent, err := openAnchoredPublicationDirectoryNoCreate(project, filepath.Dir(destination))
	if err != nil {
		return err
	}
	defer parent.Close()
	if !parent.Current() {
		return errors.Join(ErrUnsafePath, ErrLateConflict)
	}
	name := filepath.Base(destination)
	actual, err := parent.Readlink(name)
	if err != nil {
		return err
	}
	if actual != expectedTarget {
		return errors.Join(ErrUnsafePath, ErrLateConflict)
	}
	return parent.RenameFrom(stage, name)
}

// ConflictStrategy controls an existing destination. The zero value refuses
// all conflicts; replacement is only available with Force and confirmation.
type ConflictStrategy string

const ConflictReplace ConflictStrategy = "replace"

// FilesystemPlacementOptions supplies the explicit, guarded mutation inputs.
// Resource content is treated as opaque bytes and is never interpreted.
type FilesystemPlacementOptions struct {
	// Project and Target bind this generic filesystem seam to one adapter-owned
	// project placement. They are required; Destination is checked against the
	// derived location rather than trusted as an arbitrary filesystem path.
	Project     string
	Target      Target
	Destination string
	// SourceRoot anchors rendered SubAgent source materialization. It is
	// required whenever SourceContent is non-nil.
	SourceRoot string
	// SourceContent optionally materializes a managed regular-file source after
	// confirmation. It is used by rendered SubAgents; Skill sources remain
	// directory-backed and opaque.
	SourceContent []byte
	Conflict      ConflictStrategy
	Force         bool
	Journal       *operation.Journal
	Confirm       func(operation.Plan) bool
	// InitialSnapshot is an optional snapshot captured before an outer
	// confirmation. Lifecycle transactions use it so aggregate confirmation
	// cannot move the replacement race window past this adapter boundary.
	InitialSnapshot *operation.Snapshot
	// BeforePublish is a test and integration seam invoked after the final
	// confirmation re-check and before publication.
	BeforePublish func() error
	// BeforeRemove runs after a conflicting destination is staged and before
	// its staged copy is discarded. It exists to exercise concurrent-edit
	// handling without granting a hook access to resource contents.
	BeforeRemove func() error
	// BeforeFinalPublish runs after the destination absence check.
	BeforeFinalPublish func() error
	// BeforeDiscard injects a staged cleanup failure while leaving the stage
	// available for recovery.
	BeforeDiscard func() error
}

// PlaceFilesystem publishes a resource directory as an absolute symlink at
// the requested destination. It previews and confirms before changing
// anything, journals the old state for undo, and never interprets resource
// content as executable code.
func PlaceFilesystem(plan resource.PlacementPlan, options FilesystemPlacementOptions) (operation.Plan, error) {
	if err := plan.Resource.Validate(); err != nil {
		return operation.Plan{}, err
	}
	if plan.Resource.Kind != resource.Skill && plan.Resource.Kind != resource.SubAgent {
		return operation.Plan{}, &UnsupportedResourceKindError{Target: options.Target, Kind: plan.Resource.Kind}
	}
	if options.Journal == nil {
		return operation.Plan{}, fmt.Errorf("filesystem placement requires an operation journal")
	}
	if options.Project == "" || options.Target == "" {
		return operation.Plan{}, ErrPlacementConfiguration
	}
	if plan.Project != "" {
		plannedProject, projectErr := filepath.Abs(plan.Project)
		selectedProject, selectedErr := filepath.Abs(options.Project)
		if projectErr != nil || selectedErr != nil || filepath.Clean(plannedProject) != filepath.Clean(selectedProject) {
			return operation.Plan{}, fmt.Errorf("%w: placement project does not match selected project", ErrUnsafePath)
		}
	}
	if err := ValidateProjectResourcePlacement(options.Target, options.Project, options.Destination, plan.Resource.Kind, plan.Resource.ID); err != nil {
		return operation.Plan{}, err
	}
	source, destination, err := safePlacementPaths(plan.Resource, options.Destination)
	if err != nil {
		return operation.Plan{}, err
	}
	if err := verifyPlacementSource(source, plan.Resource.Kind, options.SourceContent); err != nil {
		return operation.Plan{}, err
	}
	conflict, err := destinationConflict(destination, source)
	if err != nil {
		return operation.Plan{}, err
	}
	alreadyInstalled := destinationIsNoop(destination, source, options.SourceContent)
	initialConflict := conflict
	var before []operation.Snapshot
	if options.InitialSnapshot != nil {
		if filepath.Clean(options.InitialSnapshot.Path) != destination {
			return operation.Plan{}, fmt.Errorf("%w: initial destination snapshot does not match destination", ErrUnsafePath)
		}
		before = []operation.Snapshot{*options.InitialSnapshot}
		initialConflict, err = snapshotConflict(before[0], source)
		if err != nil {
			return operation.Plan{}, err
		}
	}
	preview := operation.NewPlan("place resource")
	preview.ResourceKind = string(plan.Resource.Kind)
	preview.Changes = nil
	if options.SourceContent != nil {
		preview.Changes = append(preview.Changes, operation.Change{Path: source, Action: "write managed SubAgent source"})
	}
	preview.Changes = append(preview.Changes, operation.Change{Path: destination, Action: "create absolute link", Detail: source})
	if alreadyInstalled {
		preview.Changes = nil
		preview.Warnings = append(preview.Warnings, "resource is already installed; no changes made")
		return preview, nil
	}
	if conflict {
		// The conflict plan is part of the user-visible decision even when
		// confirmation is refused. Render the replacement action before any
		// strategy/force error is returned so callers can explain the exact
		// mutation that was withheld.
		preview.Changes[len(preview.Changes)-1].Action = "replace conflicting path with absolute link"
		if options.Conflict != ConflictReplace {
			return preview, ErrUnsafePath
		}
		if !options.Force {
			return preview, ErrForceRequired
		}
	}
	if options.InitialSnapshot == nil && conflict {
		// Capture an existing conflict before asking for confirmation. The
		// snapshot both fingerprints the approved owner and preserves its
		// content if the owner is replaced during confirmation.
		before, err = options.Journal.Capture([]string{destination})
		if err != nil {
			return operation.Plan{}, err
		}
	}
	if options.Confirm == nil || !options.Confirm(preview) {
		if options.InitialSnapshot == nil {
			removeSnapshots(before)
		}
		return preview, ErrNotConfirmed
	}

	// Re-check after confirmation to close the concurrent-edit window. An
	// existing destination is compared with the pre-confirmation snapshot
	// before it can be staged, so a new owner is never replaced.
	if err := ValidateProjectResourcePlacement(options.Target, options.Project, options.Destination, plan.Resource.Kind, plan.Resource.ID); err != nil {
		return preview, err
	}
	if err := verifyPlacementSource(source, plan.Resource.Kind, options.SourceContent); err != nil {
		return preview, err
	}
	conflict, err = destinationConflict(destination, source)
	if err != nil {
		return preview, err
	}
	if destinationIsNoop(destination, source, options.SourceContent) {
		preview.Changes = nil
		preview.Warnings = append(preview.Warnings, "resource is already installed; no changes made")
		return preview, nil
	}
	if conflict && (options.Conflict != ConflictReplace || !options.Force) {
		return preview, ErrUnsafePath
	}
	if initialConflict {
		if !conflict || len(before) != 1 || !matchesSnapshot(before[0]) {
			return preview, errors.Join(ErrUnsafePath, errLateConflict)
		}
	} else if conflict {
		return preview, errors.Join(ErrUnsafePath, errLateConflict)
	}
	if len(before) == 0 {
		before, err = options.Journal.Capture([]string{destination})
		if err != nil {
			return preview, err
		}
	}
	if options.BeforePublish != nil {
		if err := options.BeforePublish(); err != nil {
			return preview, err
		}
	}
	var sourceState *managedSource
	if options.SourceContent != nil {
		if options.SourceRoot == "" {
			return preview, fmt.Errorf("%w: rendered SubAgent source root is required", ErrPlacementConfiguration)
		}
		sourceState, err = prepareManagedSource(options.SourceRoot, source, options.SourceContent)
		if err != nil {
			return preview, err
		}
		defer func() {
			if sourceState != nil {
				_ = sourceState.Close()
			}
		}()
	}
	if sourceState != nil && !sourceState.parent.Current() {
		return preview, errors.Join(ErrUnsafePath, errLateConflict, sourceState.Cleanup())
	}
	if err := verifyPublishedSource(sourceState, source, plan.Resource.Kind, options.SourceContent); err != nil {
		if sourceState != nil && sourceState.Created {
			return preview, errors.Join(err, sourceState.Cleanup())
		}
		return preview, err
	}
	if conflict && !matchesSnapshot(before[0]) {
		if sourceState != nil && sourceState.Created {
			return preview, errors.Join(ErrUnsafePath, errLateConflict, sourceState.Cleanup())
		}
		return preview, errors.Join(ErrUnsafePath, errLateConflict)
	}
	if err := ValidateProjectResourcePlacement(options.Target, options.Project, options.Destination, plan.Resource.Kind, plan.Resource.ID); err != nil {
		if sourceState != nil && sourceState.Created {
			return preview, errors.Join(err, sourceState.Cleanup())
		}
		return preview, err
	}
	if sourceState != nil && !sourceState.parent.Current() {
		return preview, errors.Join(ErrUnsafePath, errLateConflict, sourceState.Cleanup())
	}
	userBeforeFinalPublish := options.BeforeFinalPublish
	// Hooks run immediately before the final link operation. Revalidate the
	// source after the hook as well, so a hook cannot swap the rendered source
	// parent or replace its bytes after the earlier confirmation check.
	beforeFinalPublish := func() error {
		if userBeforeFinalPublish != nil {
			if err := userBeforeFinalPublish(); err != nil {
				return err
			}
		}
		return verifyPublishedSource(sourceState, source, plan.Resource.Kind, options.SourceContent)
	}
	if err := publishLink(options.Project, destination, source, conflict, before[0], options.BeforeRemove, beforeFinalPublish, options.BeforeDiscard); err != nil {
		// A late conflict has not been mutated; restoring its old snapshot would
		// wrongly delete the unmanaged file that won the race.
		if conflict && !errors.Is(err, errLateConflict) && !errors.Is(err, errStagePreserved) {
			if sourceState != nil && sourceState.Created {
				return preview, errors.Join(err, options.Journal.Restore(before), sourceState.Cleanup())
			}
			return preview, errors.Join(err, options.Journal.Restore(before))
		}
		if sourceState != nil && sourceState.Created {
			return preview, errors.Join(err, sourceState.Cleanup())
		}
		return preview, err
	}
	after, err := options.Journal.Capture([]string{destination})
	if err != nil {
		if sourceState != nil && sourceState.Created {
			return preview, errors.Join(err, options.Journal.Restore(before), sourceState.Cleanup())
		}
		return preview, errors.Join(err, options.Journal.Restore(before))
	}
	if err := options.Journal.RecordPlan(preview, before, after); err != nil {
		if errors.Is(err, operation.ErrJournalCommitted) {
			// The journal already contains the committed entry. Do not roll
			// back the filesystem or create a journal/filesystem split.
			return preview, err
		}
		if sourceState != nil && sourceState.Created {
			return preview, errors.Join(err, options.Journal.Restore(before), sourceState.Cleanup())
		}
		return preview, errors.Join(err, options.Journal.Restore(before))
	}
	return preview, nil
}

// verifyPublishedSource keeps post-confirmation source validation anchored to
// the directory opened for publication. A path-based read here could follow a
// parent that was swapped after confirmation.
func verifyPublishedSource(state *managedSource, source string, kind resource.Kind, content []byte) error {
	if state == nil {
		return verifyPlacementSource(source, kind, content)
	}
	if kind != resource.SubAgent || content == nil {
		return verifyPlacementSource(source, kind, content)
	}
	if !state.parent.Current() {
		return errors.Join(ErrUnsafePath, errLateConflict)
	}
	if _, err := state.parent.Readlink(state.name); err == nil {
		return fmt.Errorf("%w: refusing to publish a rendered SubAgent source link", ErrUnsafePath)
	}
	current, err := state.parent.ReadFile(state.name)
	if err != nil || !bytes.Equal(current, content) {
		return fmt.Errorf("%w: rendered SubAgent source changed during publication", ErrUnsafePath)
	}
	return nil
}

func snapshotConflict(snapshot operation.Snapshot, source string) (bool, error) {
	if !snapshot.Exists {
		return false, nil
	}
	return destinationConflict(snapshot.Backup, source)
}

func removeSnapshots(snapshots []operation.Snapshot) {
	for _, snapshot := range snapshots {
		if snapshot.Exists && snapshot.Backup != "" {
			_ = os.RemoveAll(snapshot.Backup)
		}
	}
}

func safePlacementPaths(managed resource.ManagedResource, destination string) (string, string, error) {
	if managed.Provenance.Source == "" || destination == "" {
		return "", "", fmt.Errorf("%w: source and destination are required", ErrUnsafePath)
	}
	source, err := filepath.Abs(managed.Provenance.Source)
	if err != nil {
		return "", "", fmt.Errorf("%w: source and destination are required", ErrUnsafePath)
	}
	destination, err = filepath.Abs(destination)
	if err != nil || source == destination {
		return "", "", fmt.Errorf("%w: source and destination must differ", ErrUnsafePath)
	}
	base := filepath.Base(destination)
	if managed.Kind == resource.SubAgent {
		if filepath.Ext(base) == "" || strings.TrimSuffix(base, filepath.Ext(base)) != managed.ID {
			return "", "", fmt.Errorf("%w: destination basename does not match resource identifier", ErrUnsafePath)
		}
	} else if base != managed.ID {
		return "", "", fmt.Errorf("%w: destination basename does not match resource identifier", ErrUnsafePath)
	}
	return source, destination, nil
}

func verifyPlacementSource(source string, kind resource.Kind, content []byte) error {
	info, err := os.Lstat(source)
	if os.IsNotExist(err) && kind == resource.SubAgent && content != nil {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect resource source: %w", err)
	}
	if kind == resource.SubAgent && content != nil && info.Mode().IsRegular() {
		existing, readErr := os.ReadFile(source)
		if readErr == nil && bytes.Equal(existing, content) {
			return nil
		}
		return fmt.Errorf("%w: refusing to replace an existing rendered SubAgent source", ErrUnsafePath)
	}
	if kind == resource.Skill && info.IsDir() && info.Mode()&os.ModeSymlink == 0 {
		return nil
	}
	return fmt.Errorf("%w: resource source must remain a directory or managed SubAgent file", ErrUnsafePath)
}

type managedSource struct {
	parent  anchoredPublicationDirectory
	name    string
	content []byte
	Created bool
}

func prepareManagedSource(root, source string, content []byte) (*managedSource, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("%w: resolve rendered source root", ErrUnsafePath)
	}
	source, err = filepath.Abs(source)
	if err != nil {
		return nil, fmt.Errorf("%w: resolve rendered source", ErrUnsafePath)
	}
	if err := validateManagedSourceParents(root, source); err != nil {
		return nil, err
	}
	parent, err := openAnchoredPublicationDirectory(root, filepath.Dir(source))
	if err != nil {
		return nil, err
	}
	state := &managedSource{parent: parent, name: filepath.Base(source), content: append([]byte(nil), content...)}
	if !parent.Current() {
		_ = parent.Close()
		return nil, errors.Join(ErrUnsafePath, errLateConflict)
	}
	exists, err := parent.Exists(state.name)
	if err != nil {
		_ = parent.Close()
		return nil, err
	}
	if exists {
		if _, readErr := parent.Readlink(state.name); readErr == nil {
			_ = parent.Close()
			return nil, fmt.Errorf("%w: refusing to replace an existing rendered SubAgent source", ErrUnsafePath)
		}
		current, readErr := parent.ReadFile(state.name)
		if readErr != nil || !bytes.Equal(current, content) {
			_ = parent.Close()
			return nil, fmt.Errorf("%w: refusing to replace an existing rendered SubAgent source", ErrUnsafePath)
		}
		return state, nil
	}
	if err := parent.WriteFile(state.name, content); err != nil {
		_ = parent.Close()
		if errors.Is(err, os.ErrExist) {
			return nil, errors.Join(ErrUnsafePath, errLateConflict)
		}
		return nil, err
	}
	state.Created = true
	return state, nil
}

func (s *managedSource) Close() error {
	if s == nil || s.parent == nil {
		return nil
	}
	err := s.parent.Close()
	s.parent = nil
	return err
}

func (s *managedSource) Cleanup() error {
	if s == nil || !s.Created || s.parent == nil {
		return nil
	}
	if !s.parent.Current() {
		return errors.Join(ErrUnsafePath, errLateConflict)
	}
	current, err := s.parent.ReadFile(s.name)
	if err != nil || !bytes.Equal(current, s.content) {
		return nil
	}
	return s.parent.Remove(s.name)
}

func destinationConflict(destination, source string) (bool, error) {
	info, err := os.Lstat(destination)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		target, readErr := os.Readlink(destination)
		if readErr == nil && target == source {
			return false, nil
		}
	}
	return true, nil
}

func destinationIsNoop(destination, source string, content []byte) bool {
	info, err := os.Lstat(destination)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		return false
	}
	target, err := os.Readlink(destination)
	if err != nil || target != source {
		return false
	}
	if content == nil {
		return true
	}
	info, err = os.Lstat(source)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return false
	}
	existing, err := os.ReadFile(source)
	return err == nil && bytes.Equal(existing, content)
}

func matchesSnapshot(snapshot operation.Snapshot) bool {
	if !snapshot.Exists {
		_, err := os.Lstat(snapshot.Path)
		return os.IsNotExist(err)
	}
	return pathsMatch(snapshot.Path, snapshot.Backup)
}

// pathsMatch compares snapshots without following symlinks. Resource content
// is copied and compared as bytes, including executable files.
func pathsMatch(current, backup string) bool {
	currentInfo, err := os.Lstat(current)
	if err != nil {
		return false
	}
	backupInfo, err := os.Lstat(backup)
	if err != nil || currentInfo.Mode() != backupInfo.Mode() {
		return false
	}
	if currentInfo.Mode()&os.ModeSymlink != 0 {
		currentTarget, currentErr := os.Readlink(current)
		backupTarget, backupErr := os.Readlink(backup)
		return currentErr == nil && backupErr == nil && currentTarget == backupTarget
	}
	if currentInfo.IsDir() {
		currentEntries, err := os.ReadDir(current)
		if err != nil {
			return false
		}
		backupEntries, err := os.ReadDir(backup)
		if err != nil || len(currentEntries) != len(backupEntries) {
			return false
		}
		for i, entry := range currentEntries {
			if entry.Name() != backupEntries[i].Name() || !pathsMatch(filepath.Join(current, entry.Name()), filepath.Join(backup, entry.Name())) {
				return false
			}
		}
		return true
	}
	if !currentInfo.Mode().IsRegular() {
		return true
	}
	currentBytes, currentErr := os.ReadFile(current)
	backupBytes, backupErr := os.ReadFile(backup)
	return currentErr == nil && backupErr == nil && bytes.Equal(currentBytes, backupBytes)
}
