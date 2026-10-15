// Package lifecycle manages machine-local Skill links without interpreting Skill content.
package lifecycle

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/AllenMuu/skill-manager/internal/adapter"
	"github.com/AllenMuu/skill-manager/internal/catalog"
	"github.com/AllenMuu/skill-manager/internal/operation"
	"github.com/AllenMuu/skill-manager/internal/resource"
	"github.com/AllenMuu/skill-manager/internal/skillrepo"
)

var (
	ErrPlanChanged    = errors.New("operation plan changed since preview")
	ErrNotConfirmed   = operation.ErrNotConfirmed
	ErrUnsafePath     = resource.ErrUnsafeLifecycle
	ErrForceRequired  = resource.ErrForceRequired
	ErrConflict       = resource.ErrLifecycleConflict
	errConcurrentEdit = errors.New("project skill changed while operation was staged")
)

// ConflictStrategy selects how a mutating operation handles an existing
// conflicting destination path.
type ConflictStrategy string

const (
	// ConflictReplace removes a conflicting destination after force confirmation.
	ConflictReplace ConflictStrategy = "replace"
)

// Options guards a mutating lifecycle operation.
type Options struct {
	// Conflict selects the strategy for existing conflicting paths. The empty
	// default refuses conflicts without touching the filesystem.
	Conflict ConflictStrategy
	// Force supplies the explicit force confirmation a conflict strategy needs.
	Force bool
	// ExpectedConflictFingerprints binds force replacement to the exact paths
	// and content that were shown in the reviewed plan.
	ExpectedConflictFingerprints map[string]string
	// ExpectedSourceFingerprints binds activation to reviewed Skill directory trees.
	ExpectedSourceFingerprints map[string]string
	// TargetSelections narrows one batch to specific existing activations per
	// Skill. A nil map preserves the ordinary Skills-by-targets behavior.
	TargetSelections map[string][]adapter.Target
	// TargetBindings assigns an exact repository version to one existing
	// activation entry. It is reserved for maintenance plans such as resync,
	// where different entries with the same Skill ID may use different versions.
	TargetBindings []TargetBinding
	// Rematerialize is reserved for an explicit resync plan and replaces only
	// destinations that inventory proved are still managed.
	Rematerialize bool
	// JournalOperation distinguishes an explicit maintenance operation from a
	// normal activation while retaining the same atomic mutation path.
	JournalOperation string
	// LinkDependencies carry the pre-migration source chain into the Undo log.
	LinkDependencies []operation.LinkDependency
}

// TargetBinding binds one selected target entry to an exact Skill version.
type TargetBinding struct {
	Target adapter.Target
	Skill  catalog.Skill
}

func optionsOf(opts []Options) Options {
	if len(opts) == 0 {
		return Options{}
	}
	return opts[0]
}

// Status identifies how a project skill is owned.
type Status string

const (
	Managed     Status = "managed"
	Unmanaged   Status = "unmanaged"
	Orphaned    Status = "orphaned"
	Unsupported Status = "unsupported"
)

// Item is an entry in a project skill inventory.
type Item struct {
	Target          adapter.Target `json:"target"`
	Identifier      string         `json:"identifier"`
	Path            string         `json:"path"`
	SourcePath      string         `json:"sourcePath,omitempty"`
	Materialization string         `json:"materialization,omitempty"`
	Status          Status         `json:"status"`
}

// ConfirmFunc displays a plan and returns whether the user confirmed it.
type ConfirmFunc func(operation.Plan) bool

// Service performs guarded lifecycle changes against one local skill library.
type Service struct {
	LibraryPath string
	Journal     *operation.Journal
	BatchStore  *operation.BatchStore
	// OperationLockHeld is for coordinators that already hold Journal's
	// operation lock while applying a multi-step preview.
	OperationLockHeld bool
	Materialization   adapter.MaterializationMode
	LastBatchID       string
	// BeforePublish is an optional test seam invoked before each staged publish.
	BeforePublish func(string) error
	// BeforeFinalPublish is an optional test seam invoked after the adapter has
	// anchored the destination parent and immediately before link publication.
	BeforeFinalPublish func() error
	// BeforeDiscard is an optional test seam invoked after a replacement link is
	// published and before its staged original is discarded.
	BeforeDiscard     func() error
	confirm           ConfirmFunc
	resourceHandler   resource.ResourceHandler
	recordPublication func(string) error
	recordStageMove   func(string, string) error
	recordOwnedState  func(string, string) error
	linkDependencies  []operation.LinkDependency
}

// preservePathsError marks paths whose current owner won a no-replace race.
// A transaction may roll back its other publications, but restoring one of
// these snapshots would delete the owner that appeared after confirmation.
type preservePathsError struct {
	cause error
	paths map[string]struct{}
}

func (e *preservePathsError) Error() string { return e.cause.Error() }
func (e *preservePathsError) Unwrap() error { return e.cause }

func preservePath(cause error, path string) error {
	return preservePaths(cause, path)
}

func preservePaths(cause error, paths ...string) error {
	preserved := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		preserved[path] = struct{}{}
	}
	return &preservePathsError{cause: cause, paths: preserved}
}

// New constructs a lifecycle service. A nil confirmation function declines mutations.
func New(libraryPath string, journal *operation.Journal, confirm ConfirmFunc) *Service {
	return NewWithResourceHandler(libraryPath, journal, confirm, resource.NewSkillHandler())
}

// NewWithResourceHandler constructs the Skill lifecycle coordinator with an
// explicit handler. It is primarily useful for composition tests and future
// handler registration; Skill lifecycle methods still require Skill plans.
func NewWithResourceHandler(libraryPath string, journal *operation.Journal, confirm ConfirmFunc, handler resource.ResourceHandler) *Service {
	abs, err := filepath.Abs(libraryPath)
	if err == nil {
		libraryPath = abs
	}
	if handler == nil {
		handler = resource.NewSkillHandler()
	}
	return &Service{LibraryPath: libraryPath, Journal: journal, Materialization: adapter.MaterializeAuto, confirm: confirm, resourceHandler: handler}
}

// Add activates skill for each explicitly selected target using absolute soft links.
func (s *Service) Add(project string, skill catalog.Skill, targets []adapter.Target, opts ...Options) (operation.Plan, error) {
	options := optionsOf(opts)
	mode := s.Materialization
	if mode == "" {
		mode = adapter.MaterializeAuto
	}
	if err := mode.Validate(); err != nil {
		return operation.Plan{}, err
	}
	project, err := filepath.Abs(project)
	if err != nil {
		return operation.Plan{}, err
	}
	base, err := s.planSkill(resource.SkillLifecycleRequest{
		Action:       resource.LifecycleActivate,
		LibraryPath:  s.LibraryPath,
		CatalogEntry: &skill,
	})
	if err != nil {
		return operation.Plan{}, err
	}
	plan := operation.NewPlan("activate")
	paths := make([]string, 0, len(targets))
	requests := make([]resource.SkillLifecycleRequest, 0, len(targets))
	seen := map[adapter.Target]bool{}
	for _, target := range targets {
		if seen[target] {
			return plan, fmt.Errorf("%w: duplicate target %q", ErrUnsafePath, target)
		}
		seen[target] = true
		placement, err := s.resourcePlacement(project, target, base.Resource)
		if err != nil {
			return plan, err
		}
		request := resource.SkillLifecycleRequest{
			Action:        resource.LifecycleActivate,
			LibraryPath:   s.LibraryPath,
			ProjectPath:   project,
			Resource:      base.Resource,
			PlacementPath: placement.Destination,
			Target:        string(target),
			Conflict:      skillConflict(options.Conflict),
			Force:         options.Force,
		}
		planned, err := s.planSkill(request)
		if err != nil {
			return plan, err
		}
		planned = withMaterializationPlan(planned, mode)
		appendSkillPlan(&plan, planned)
		paths = append(paths, placement.Destination)
		requests = append(requests, request)
	}
	if len(paths) == 0 {
		return plan, nil
	}
	var beforeConfirmation []operation.Snapshot
	if s.Journal != nil {
		beforeConfirmation, err = s.Journal.Capture(paths)
		if err != nil {
			return plan, err
		}
	}
	for _, snapshot := range beforeConfirmation {
		expected, ok := options.ExpectedConflictFingerprints[snapshot.Path]
		if !ok {
			continue
		}
		if !snapshot.Exists {
			removeSnapshots(beforeConfirmation)
			return plan, ErrUnsafePath
		}
		actual, fingerprintErr := operation.FingerprintPath(snapshot.Backup)
		if fingerprintErr != nil || actual != expected {
			removeSnapshots(beforeConfirmation)
			return plan, errors.Join(ErrUnsafePath, fingerprintErr)
		}
	}
	if !s.confirmed(plan) {
		removeSnapshots(beforeConfirmation)
		return plan, ErrNotConfirmed
	}
	placementJournal, cleanup, err := newPlacementJournal()
	if err != nil {
		return plan, err
	}
	defer cleanup()
	return plan, s.mutateWithBeforeMaterializations(plan, paths, beforeConfirmation, func() error {
		for i := range requests {
			if err := adapter.ValidateProjectPlacement(adapter.Target(requests[i].Target), requests[i].ProjectPath, paths[i], base.Resource.ID); err != nil {
				return err
			}
			replanned, err := s.planSkill(requests[i])
			if err != nil {
				return err
			}
			if replanned.SourcePath != base.SourcePath {
				return ErrUnsafePath
			}
		}
		return nil
	}, func() ([]operation.Materialization, error) {
		for i, path := range paths {
			var initialSnapshot *operation.Snapshot
			if i < len(beforeConfirmation) {
				initialSnapshot = &beforeConfirmation[i]
			}
			if err := s.publishSkill(requests[i], path, base.SourcePath, placementJournal, initialSnapshot); err != nil {
				return nil, err
			}
		}
		return s.materializationRecords(paths, requests)
	})
}

// Activate is an alias for Add.
func (s *Service) Activate(project string, skill catalog.Skill, targets []adapter.Target) (operation.Plan, error) {
	return s.Add(project, skill, targets)
}

// PreviewMany builds a read-only plan for an explicit batch installation.
func (s *Service) PreviewMany(project string, skills []catalog.Skill, targets []adapter.Target, opts ...Options) (operation.Plan, error) {
	return s.PreviewAddMany(project, skills, targets, opts...)
}

// PreviewGlobalMany builds a mutation-free plan for global Skill links beneath
// the selected user's home directory.
func (s *Service) PreviewGlobalMany(home string, skills []catalog.Skill, targets []adapter.Target, opts ...Options) (operation.Plan, error) {
	preview, err := s.buildAddManyPreview(home, skills, targets, optionsOf(opts), true)
	return preview.plan, err
}

// ApplyMany applies a confirmed installation only while its plan still matches.
func (s *Service) ApplyMany(project string, skills []catalog.Skill, targets []adapter.Target, expected operation.Plan, confirmed bool, opts ...Options) (operation.Plan, error) {
	return s.applyMany(project, skills, targets, expected, confirmed, false, opts...)
}

// ApplyGlobalMany applies a confirmed global installation only while its plan
// still matches the selected repository versions and global destinations.
func (s *Service) ApplyGlobalMany(home string, skills []catalog.Skill, targets []adapter.Target, expected operation.Plan, confirmed bool, opts ...Options) (operation.Plan, error) {
	return s.applyMany(home, skills, targets, expected, confirmed, true, opts...)
}

func (s *Service) applyMany(project string, skills []catalog.Skill, targets []adapter.Target, expected operation.Plan, confirmed, global bool, opts ...Options) (operation.Plan, error) {
	if !confirmed {
		return expected, ErrNotConfirmed
	}
	current, err := s.previewMany(project, skills, targets, global, opts...)
	if !samePlan(expected, current) {
		return current, ErrPlanChanged
	}
	if err != nil {
		return current, err
	}
	service := *s
	service.confirm = func(plan operation.Plan) bool { return samePlan(expected, plan) }
	service.linkDependencies = append([]operation.LinkDependency(nil), optionsOf(opts).LinkDependencies...)
	result, err := service.addMany(project, skills, targets, global, opts...)
	s.LastBatchID = service.LastBatchID
	return result, err
}

func (s *Service) previewMany(project string, skills []catalog.Skill, targets []adapter.Target, global bool, opts ...Options) (operation.Plan, error) {
	if global {
		return s.PreviewGlobalMany(project, skills, targets, opts...)
	}
	return s.PreviewMany(project, skills, targets, opts...)
}

func samePlan(left, right operation.Plan) bool {
	if left.Version != right.Version || left.ResourceKind != right.ResourceKind || left.Operation != right.Operation || len(left.Changes) != len(right.Changes) || len(left.Warnings) != len(right.Warnings) {
		return false
	}
	for i := range left.Changes {
		if left.Changes[i] != right.Changes[i] {
			return false
		}
	}
	for i := range left.Warnings {
		if left.Warnings[i] != right.Warnings[i] {
			return false
		}
	}
	return true
}

func placementConflict(path, source string) (bool, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(path)
		if err != nil {
			return false, err
		}
		if target == source {
			return false, nil
		}
	}
	return true, nil
}

// AddMany activates an explicit selection as one confirmed, journaled transaction.
func (s *Service) AddMany(project string, skills []catalog.Skill, targets []adapter.Target, opts ...Options) (operation.Plan, error) {
	return s.addMany(project, skills, targets, false, opts...)
}

// AddGlobalMany activates an explicit selection through guarded links in the
// selected targets' user-level Skill directories.
func (s *Service) AddGlobalMany(home string, skills []catalog.Skill, targets []adapter.Target, opts ...Options) (operation.Plan, error) {
	return s.addMany(home, skills, targets, true, opts...)
}

func (s *Service) addMany(project string, skills []catalog.Skill, targets []adapter.Target, global bool, opts ...Options) (operation.Plan, error) {
	options := optionsOf(opts)
	if err := verifySourceFingerprints(skills, options.ExpectedSourceFingerprints); err != nil {
		return operation.Plan{}, err
	}
	preview, err := s.buildAddManyPreview(project, skills, targets, options, global)
	if err != nil {
		return preview.plan, err
	}
	plan, paths, requests := preview.plan, preview.paths, preview.requests
	if len(paths) == 0 {
		return plan, nil
	}
	var beforeConfirmation []operation.Snapshot
	if s.Journal != nil {
		beforeConfirmation, err = s.Journal.Capture(paths)
		if err != nil {
			return plan, err
		}
	}
	for _, snapshot := range beforeConfirmation {
		expected, ok := options.ExpectedConflictFingerprints[snapshot.Path]
		if !ok {
			continue
		}
		if !snapshot.Exists {
			removeSnapshots(beforeConfirmation)
			return plan, ErrUnsafePath
		}
		actual, fingerprintErr := operation.FingerprintPath(snapshot.Backup)
		if fingerprintErr != nil || actual != expected {
			removeSnapshots(beforeConfirmation)
			return plan, errors.Join(ErrUnsafePath, fingerprintErr)
		}
	}
	if !s.confirmed(plan) {
		removeSnapshots(beforeConfirmation)
		return plan, ErrNotConfirmed
	}
	placementJournal, cleanup, err := newPlacementJournal()
	if err != nil {
		return plan, err
	}
	defer cleanup()
	journalPlan := plan
	journalPlan.Operation = "activate"
	if options.JournalOperation != "" {
		journalPlan.Operation = options.JournalOperation
	}
	return plan, s.mutateWithBeforeMaterializations(journalPlan, paths, beforeConfirmation, func() error {
		if err := verifySourceFingerprints(skills, options.ExpectedSourceFingerprints); err != nil {
			return err
		}
		for i := range requests {
			if expected, ok := options.ExpectedConflictFingerprints[paths[i]]; ok {
				current, err := operation.FingerprintPath(paths[i])
				if err != nil || current != expected {
					return errors.Join(ErrUnsafePath, err)
				}
			}
			if err := validateLifecyclePlacement(adapter.Target(requests[i].Target), requests[i].ProjectPath, paths[i], requests[i].Resource.ID, requests[i].GlobalScope); err != nil {
				return err
			}
			replanned, err := s.planSkill(requests[i])
			if err != nil {
				return err
			}
			if replanned.SourcePath != requests[i].Resource.Provenance.Source {
				return ErrUnsafePath
			}
		}
		return nil
	}, func() ([]operation.Materialization, error) {
		for i, path := range paths {
			if err := verifySourceFingerprints(skills, options.ExpectedSourceFingerprints); err != nil {
				return nil, err
			}
			var initialSnapshot *operation.Snapshot
			if i < len(beforeConfirmation) {
				initialSnapshot = &beforeConfirmation[i]
			}
			if err := s.publishSkill(requests[i], path, requests[i].Resource.Provenance.Source, placementJournal, initialSnapshot); err != nil {
				return nil, err
			}
			if s.BeforePublish != nil {
				if err := s.BeforePublish("add-many-published"); err != nil {
					return nil, err
				}
			}
		}
		return s.materializationRecords(paths, requests)
	})
}

func verifySourceFingerprints(skills []catalog.Skill, expected map[string]string) error {
	if expected == nil {
		return nil
	}
	for _, skill := range skills {
		digest, ok := expected[skill.SourcePath]
		if !ok {
			return errors.Join(ErrUnsafePath, fmt.Errorf("Skill source %q has no reviewed fingerprint", skill.SourcePath))
		}
		actual, err := operation.FingerprintPath(skill.SourcePath)
		if err != nil {
			return errors.Join(ErrUnsafePath, fmt.Errorf("fingerprint Skill source %q: %w", skill.SourcePath, err))
		}
		if actual != digest {
			return errors.Join(ErrUnsafePath, fmt.Errorf("Skill source %q changed after review", skill.SourcePath))
		}
	}
	return nil
}

type addManyPreview struct {
	plan     operation.Plan
	paths    []string
	requests []resource.SkillLifecycleRequest
}

// PreviewAddMany builds the exact guarded activation plan without staging
// snapshots or changing the project, library, or journal.
func (s *Service) PreviewAddMany(project string, skills []catalog.Skill, targets []adapter.Target, opts ...Options) (operation.Plan, error) {
	preview, err := s.buildAddManyPreview(project, skills, targets, optionsOf(opts), false)
	return preview.plan, err
}

func (s *Service) buildAddManyPreview(project string, skills []catalog.Skill, targets []adapter.Target, options Options, global bool) (addManyPreview, error) {
	preview := addManyPreview{plan: operation.NewPlan("activate selected skills")}
	mode := s.Materialization
	if mode == "" {
		mode = adapter.MaterializeAuto
	}
	if err := mode.Validate(); err != nil {
		return preview, err
	}
	project, err := filepath.Abs(project)
	if err != nil {
		return preview, err
	}
	bindings := append([]TargetBinding(nil), options.TargetBindings...)
	if options.TargetBindings == nil {
		for _, skill := range skills {
			selectedTargets := targets
			if options.TargetSelections != nil {
				selectedTargets = options.TargetSelections[skill.Identifier]
			}
			for _, target := range selectedTargets {
				bindings = append(bindings, TargetBinding{Target: target, Skill: skill})
			}
		}
	}
	seen := map[string]bool{}
	for _, binding := range bindings {
		skill, target := binding.Skill, binding.Target
		if skill.Identifier == "" || target == "" {
			return preview, ErrUnsafePath
		}
		base, err := s.planSkill(resource.SkillLifecycleRequest{
			Action:       resource.LifecycleActivate,
			LibraryPath:  s.LibraryPath,
			CatalogEntry: &skill,
		})
		if err != nil {
			return preview, err
		}
		placement, err := s.resourcePlacementInScope(project, target, base.Resource, global)
		if err != nil {
			return preview, err
		}
		path := placement.Destination
		if seen[path] {
			return preview, ErrUnsafePath
		}
		seen[path] = true
		request := resource.SkillLifecycleRequest{
			Action:               resource.LifecycleActivate,
			LibraryPath:          s.LibraryPath,
			ProjectPath:          project,
			GlobalScope:          global,
			Resource:             base.Resource,
			PlacementPath:        path,
			Target:               string(target),
			Conflict:             skillConflict(options.Conflict),
			Force:                options.Force,
			RematerializeManaged: options.Rematerialize,
		}
		if options.Rematerialize {
			materialization, status, ok, materialErr := s.copyActivation(path, skill.Identifier)
			if materialErr != nil {
				return preview, materialErr
			}
			if ok && status == Managed && materialization.Method == "copy" {
				request.JournalCopyMatches = true
				request.JournalMaterialization = "copy"
				request.JournalSource = materialization.Source
				request.JournalFingerprint = materialization.DestinationFingerprint
			}
		}
		planned, err := s.planSkill(request)
		if err != nil {
			conflict, conflictErr := placementConflict(path, base.Resource.Provenance.Source)
			if conflictErr != nil {
				return preview, conflictErr
			}
			if conflict && (errors.Is(err, ErrUnsafePath) || errors.Is(err, ErrForceRequired)) {
				action := "refuse conflicting destination"
				if options.Conflict == ConflictReplace && !options.Force {
					action = "replace conflict (force required)"
				}
				preview.plan.Changes = append(preview.plan.Changes, operation.Change{Path: path, Action: action, Detail: base.Resource.Provenance.Source})
			}
			return preview, err
		}
		preview.paths = append(preview.paths, path)
		preview.requests = append(preview.requests, request)
		planned = withMaterializationPlan(planned, mode)
		appendSkillPlan(&preview.plan, planned)
	}
	if s.BatchStore != nil {
		if err := s.BatchStore.CheckAvailable(preview.paths); err != nil {
			return preview, err
		}
	}
	return preview, nil
}

func withMaterializationPlan(plan resource.SkillLifecyclePlan, mode adapter.MaterializationMode) resource.SkillLifecyclePlan {
	for i := range plan.Changes {
		change := &plan.Changes[i]
		if change.Action == "create absolute link" {
			change.Action = adapter.MaterializationAction(mode, false)
			change.Detail = adapter.MaterializationDetail(change.Detail, mode)
		} else if change.Action == "replace conflicting path with absolute link" {
			change.Action = adapter.MaterializationAction(mode, true)
			change.Detail = adapter.MaterializationDetail(change.Detail, mode)
		}
	}
	return plan
}

func (s *Service) materializationRecords(paths []string, requests []resource.SkillLifecycleRequest) ([]operation.Materialization, error) {
	if len(paths) != len(requests) {
		return nil, ErrUnsafePath
	}
	result := make([]operation.Materialization, 0, len(paths))
	for i, path := range paths {
		source, err := filepath.Abs(requests[i].Resource.Provenance.Source)
		if err != nil {
			return nil, err
		}
		destination, err := filepath.Abs(path)
		if err != nil {
			return nil, err
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
				target, err = filepath.Abs(filepath.Join(filepath.Dir(destination), target))
				if err != nil {
					return nil, err
				}
			}
			if filepath.Clean(target) != filepath.Clean(source) {
				return nil, operation.ErrUnexpectedState
			}
			method = "symlink"
		} else if info.IsDir() {
			sourceDigest, err := operation.FingerprintPath(source)
			if err != nil {
				return nil, err
			}
			destinationDigest, err := operation.FingerprintPath(destination)
			if err != nil {
				return nil, err
			}
			if sourceDigest != destinationDigest {
				return nil, operation.ErrUnexpectedState
			}
			method = "copy"
		} else {
			return nil, operation.ErrUnexpectedState
		}
		result = append(result, operation.Materialization{
			Destination: destination, Source: source,
			SourceVersion: sourceVersionKey(source), Method: method,
		})
	}
	return result, nil
}

func sourceVersionKey(source string) string {
	if filepath.Base(filepath.Dir(source)) == "versions" {
		return filepath.Base(source)
	}
	return "legacy"
}

// List inventories supported project skill locations without changing them.
func (s *Service) List(project string) ([]Item, error) {
	project, err := filepath.Abs(project)
	if err != nil {
		return nil, err
	}
	items := make([]Item, 0)
	for _, a := range adapter.Supported() {
		inspections, err := a.Inspect(adapter.InspectionRequest{Project: project, Kind: resource.Skill})
		if err != nil {
			return nil, err
		}
		for _, inspection := range inspections {
			path := inspection.Path
			item := Item{Target: a.Target(), Identifier: inspection.Identifier, Path: path, Status: Unmanaged}
			info, err := os.Lstat(path)
			if err != nil {
				return nil, err
			}
			if info.Mode()&os.ModeSymlink != 0 {
				destination, err := os.Readlink(path)
				if err != nil {
					return nil, err
				}
				item.SourcePath = destination
				_, err = s.planSkill(resource.SkillLifecycleRequest{
					Action:        resource.LifecycleRemove,
					LibraryPath:   s.LibraryPath,
					Identifier:    inspection.Identifier,
					PlacementPath: path,
				})
				if err == nil {
					if _, err := os.Stat(destination); os.IsNotExist(err) {
						item.Status = Orphaned
					} else if err == nil {
						item.Status = Managed
					} else {
						return nil, fmt.Errorf("inspect managed library target %s: %w", destination, err)
					}
				} else if !errors.Is(err, ErrUnsafePath) {
					return nil, err
				}
				if item.Status == Managed {
					item.Materialization = "symlink"
				}
			} else if info.IsDir() {
				if materialization, status, ok, err := s.copyActivation(path, inspection.Identifier); err != nil {
					return nil, err
				} else if ok {
					item.SourcePath = materialization.Source
					item.Materialization = "copy"
					item.Status = status
				}
			}
			items = append(items, item)
		}
	}
	unsupported, err := unsupportedProjectSkills(project)
	if err != nil {
		return nil, err
	}
	items = append(items, unsupported...)
	return items, nil
}

// ListGlobal inventories global Skill directories for the supported targets.
// It only classifies symlinks into the configured repository as managed.
func (s *Service) ListGlobal(home string) ([]Item, error) {
	home, err := filepath.Abs(home)
	if err != nil {
		return nil, err
	}
	info, err := os.Lstat(home)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return nil, fmt.Errorf("%w: global skill home must be a real directory", ErrUnsafePath)
	}
	items := make([]Item, 0)
	handler := resource.NewSkillHandler()
	for _, target := range adapter.Supported() {
		agent, ok := adapter.ForAgent(target.Target())
		if !ok {
			continue
		}
		root := filepath.Dir(agent.GlobalSkillPath(home, "placeholder"))
		if err := adapter.ValidateGlobalResourcePlacement(target.Target(), home, agent.GlobalSkillPath(home, "placeholder"), resource.Skill, "placeholder"); err != nil {
			return nil, err
		}
		entries, err := os.ReadDir(root)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("inspect %s global Skills: %w", target.Target(), err)
		}
		for _, entry := range entries {
			path := filepath.Join(root, entry.Name())
			item := Item{Target: target.Target(), Identifier: entry.Name(), Path: path, Status: Unmanaged}
			pathInfo, err := os.Lstat(path)
			if err != nil {
				return nil, err
			}
			if pathInfo.Mode()&os.ModeSymlink != 0 {
				source, err := os.Readlink(path)
				if err != nil {
					return nil, err
				}
				item.SourcePath = source
				owned, err := handler.ManagesSource(s.LibraryPath, entry.Name(), source)
				if err != nil {
					return nil, err
				}
				if owned {
					if _, err := os.Stat(source); os.IsNotExist(err) {
						item.Status = Orphaned
					} else if err != nil {
						return nil, err
					} else {
						item.Status = Managed
					}
				}
				if item.Status == Managed {
					item.Materialization = "symlink"
				}
			} else if pathInfo.IsDir() {
				if materialization, status, ok, err := s.copyActivation(path, entry.Name()); err != nil {
					return nil, err
				} else if ok {
					item.SourcePath = materialization.Source
					item.Materialization = "copy"
					item.Status = status
				}
			}
			items = append(items, item)
		}
	}
	return items, nil
}

func (s *Service) copyActivation(path, identifier string) (operation.Materialization, Status, bool, error) {
	if s.Journal == nil {
		return operation.Materialization{}, Unmanaged, false, nil
	}
	materialization, ok, err := s.Journal.RecordedMaterialization(path)
	if err != nil || !ok || materialization.Method != "copy" {
		return materialization, Unmanaged, ok, err
	}
	status := Orphaned
	handler := resource.NewSkillHandler()
	owned, err := handler.ManagesSource(s.LibraryPath, identifier, materialization.Source)
	if err != nil {
		return materialization, status, true, err
	}
	if !owned {
		return materialization, status, true, nil
	}
	sourceFingerprint, err := operation.FingerprintPath(materialization.Source)
	if err != nil {
		if os.IsNotExist(err) {
			return materialization, status, true, nil
		}
		return materialization, status, true, err
	}
	if sourceFingerprint != materialization.SourceFingerprint {
		return materialization, status, true, nil
	}
	eligible, err := handler.Eligible(identifier, materialization.Source)
	if err != nil {
		return materialization, status, true, err
	}
	if eligible {
		status = Managed
	}
	return materialization, status, true, nil
}

func (s *Service) validateJournalCopy(request resource.SkillLifecycleRequest) error {
	if request.JournalMaterialization != "copy" {
		return nil
	}
	if s.Journal == nil || !request.JournalCopyMatches || request.JournalFingerprint == "" {
		return ErrUnsafePath
	}
	materialization, ok, err := s.Journal.RecordedMaterialization(request.PlacementPath)
	if err != nil {
		return err
	}
	if !ok || materialization.Method != "copy" || materialization.Source != request.JournalSource || materialization.DestinationFingerprint != request.JournalFingerprint {
		return ErrUnsafePath
	}
	return nil
}

// unsupportedProjectSkills inventories agent-shaped project locations without
// an adapter. Such entries are visible to operators but cannot be targets for
// lifecycle mutations because no adapter is registered for them.
func unsupportedProjectSkills(project string) ([]Item, error) {
	locations, err := adapter.UnsupportedAgentRoots(project)
	if err != nil {
		return nil, err
	}
	var items []Item
	for _, location := range locations {
		name := filepath.Base(location)
		root := filepath.Join(location, "skills")
		resourceEntries, err := os.ReadDir(root)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		target := adapter.Target(strings.TrimPrefix(name, "."))
		for _, resourceEntry := range resourceEntries {
			path := filepath.Join(root, resourceEntry.Name())
			items = append(items, Item{Target: target, Identifier: resourceEntry.Name(), Path: path, Status: Unsupported})
		}
	}
	return items, nil
}

// Remove removes only a managed project-side soft link.
func (s *Service) Remove(project string, target adapter.Target, identifier string) (operation.Plan, error) {
	return s.removeInScope(project, target, identifier, false)
}

// RemoveGlobal removes only a managed global Skill link beneath home.
func (s *Service) RemoveGlobal(home string, target adapter.Target, identifier string) (operation.Plan, error) {
	return s.removeInScope(home, target, identifier, true)
}

func (s *Service) removeInScope(root string, target adapter.Target, identifier string, global bool) (operation.Plan, error) {
	plan, root, placement, request, planned, err := s.buildRemovePreviewInScope(root, target, identifier, global)
	if err != nil {
		return plan, err
	}
	if !s.confirmed(plan) {
		return plan, ErrNotConfirmed
	}
	return plan, s.mutate(plan, []string{placement.Destination}, func() error {
		if err := validateLifecyclePlacement(target, root, placement.Destination, identifier, global); err != nil {
			return err
		}
		if err := s.validateJournalCopy(request); err != nil {
			return err
		}
		replanned, err := s.planSkill(request)
		if err != nil {
			return err
		}
		if replanned.CurrentSource != planned.CurrentSource {
			return ErrUnsafePath
		}
		return nil
	}, func() error {
		if err := validateLifecyclePlacement(target, root, placement.Destination, identifier, global); err != nil {
			return err
		}
		replanned, err := s.planSkill(request)
		if err != nil {
			return err
		}
		if replanned.CurrentSource != planned.CurrentSource {
			return ErrUnsafePath
		}
		if err := s.validateJournalCopy(request); err != nil {
			return err
		}
		if s.BeforePublish != nil {
			if err := s.BeforePublish("remove-before"); err != nil {
				return err
			}
		}
		if request.JournalMaterialization == "copy" {
			return adapter.RemoveManagedCopy(root, placement.Destination, request.JournalFingerprint)
		}
		return adapter.RemoveManagedLink(root, placement.Destination, planned.CurrentSource)
	})
}

// PreviewRemove builds the exact guarded removal plan without changing the
// project or journal.
func (s *Service) PreviewRemove(project string, target adapter.Target, identifier string) (operation.Plan, error) {
	plan, _, _, _, _, err := s.buildRemovePreview(project, target, identifier)
	return plan, err
}

// PreviewRemoveGlobal builds a read-only plan for removing a global Skill link.
func (s *Service) PreviewRemoveGlobal(home string, target adapter.Target, identifier string) (operation.Plan, error) {
	plan, _, _, _, _, err := s.buildRemovePreviewInScope(home, target, identifier, true)
	return plan, err
}

func (s *Service) buildRemovePreview(project string, target adapter.Target, identifier string) (operation.Plan, string, adapter.Placement, resource.SkillLifecycleRequest, resource.SkillLifecyclePlan, error) {
	return s.buildRemovePreviewInScope(project, target, identifier, false)
}

func (s *Service) buildRemovePreviewInScope(project string, target adapter.Target, identifier string, global bool) (operation.Plan, string, adapter.Placement, resource.SkillLifecycleRequest, resource.SkillLifecyclePlan, error) {
	plan := operation.NewPlan("remove")
	project, err := filepath.Abs(project)
	if err != nil {
		return plan, "", adapter.Placement{}, resource.SkillLifecycleRequest{}, resource.SkillLifecyclePlan{}, err
	}
	baseRequest := resource.SkillLifecycleRequest{Action: resource.LifecycleRemove, LibraryPath: s.LibraryPath, ProjectPath: project, GlobalScope: global, Identifier: identifier}
	base, err := s.planSkill(baseRequest)
	if err != nil {
		return plan, project, adapter.Placement{}, baseRequest, resource.SkillLifecyclePlan{}, err
	}
	placement, err := s.resourcePlacementInScope(project, target, base.Resource, global)
	if err != nil {
		return plan, project, adapter.Placement{}, baseRequest, resource.SkillLifecyclePlan{}, err
	}
	request := baseRequest
	request.PlacementPath = placement.Destination
	if materialization, status, ok, err := s.copyActivation(placement.Destination, identifier); err != nil {
		return plan, project, placement, request, resource.SkillLifecyclePlan{}, err
	} else if ok && materialization.Method == "copy" && status == Managed {
		request.JournalCopyMatches = true
		request.JournalMaterialization = "copy"
		request.JournalSource = materialization.Source
		request.JournalFingerprint = materialization.DestinationFingerprint
	}
	planned, err := s.planSkill(request)
	if err != nil {
		return plan, project, placement, request, resource.SkillLifecyclePlan{}, err
	}
	appendSkillPlan(&plan, planned)
	return plan, project, placement, request, planned, nil
}

// Adopt moves an eligible unmanaged project directory into the library and links it back.
func (s *Service) Adopt(project string, target adapter.Target, identifier string) (operation.Plan, error) {
	project, err := filepath.Abs(project)
	if err != nil {
		return operation.Plan{}, err
	}
	baseRequest := resource.SkillLifecycleRequest{Action: resource.LifecycleAdopt, LibraryPath: s.LibraryPath, ProjectPath: project, Identifier: identifier}
	base, err := s.planSkill(baseRequest)
	if err != nil {
		return operation.Plan{}, err
	}
	placement, err := s.resourcePlacement(project, target, base.Resource)
	if err != nil {
		return operation.Plan{}, err
	}
	request := baseRequest
	request.PlacementPath = placement.Destination
	planned, err := s.planSkill(request)
	if err != nil {
		return operation.Plan{}, err
	}
	path := placement.Destination
	libraryPath := planned.Resource.Provenance.Source
	libraryRoot := filepath.Join(s.LibraryPath, identifier)
	plan := operation.NewPlan("adopt")
	appendSkillPlan(&plan, planned)
	if !s.confirmed(plan) {
		return plan, ErrNotConfirmed
	}
	return plan, s.mutate(plan, []string{path, libraryRoot}, func() error {
		if err := adapter.ValidateProjectPlacement(target, project, path, identifier); err != nil {
			return err
		}
		replanned, err := s.planSkill(request)
		if err != nil {
			return err
		}
		if replanned.SourcePath != planned.SourcePath || replanned.SourceChecksum != planned.SourceChecksum || replanned.Resource.Provenance.Source != libraryPath {
			return ErrUnsafePath
		}
		return nil
	}, func() error {
		if err := adapter.ValidateProjectPlacement(target, project, path, identifier); err != nil {
			return err
		}
		replanned, err := s.planSkill(request)
		if err != nil {
			if errors.Is(err, ErrConflict) {
				return preservePath(err, libraryRoot)
			}
			return err
		}
		if replanned.SourcePath != planned.SourcePath || replanned.SourceChecksum != planned.SourceChecksum || replanned.Resource.Provenance.Source != libraryPath {
			return ErrUnsafePath
		}
		projectAnchor, err := capturePathAnchor(filepath.Dir(path))
		if err != nil {
			return err
		}
		libraryAnchor, err := capturePathAnchor(libraryPath)
		if err != nil {
			return err
		}
		if err := libraryAnchor.validate(); err != nil {
			return err
		}
		repository, err := skillrepo.Open(s.LibraryPath)
		if err != nil {
			return err
		}
		entry, err := repository.AddVersionWithPostPublish(identifier, path, skillrepo.VersionMetadata{
			Key: filepath.Base(libraryPath), Version: "unknown", Source: path,
			SourceType: "local-adoption", Publisher: "unknown", ImmutableRef: "unknown",
			Checksum: planned.SourceChecksum, TrustLevel: "unknown", VerificationStatus: "unknown",
			LegacyProvenance: "adopted from project",
		}, true, func(published skillrepo.Entry, expectedMetadata skillrepo.Metadata) error {
			if filepath.Clean(published.Path) != filepath.Clean(libraryPath) || published.Identifier != identifier || published.Key != filepath.Base(libraryPath) || published.Metadata.Checksum != planned.SourceChecksum {
				return preservePath(errors.Join(ErrUnsafePath, fmt.Errorf("adopted Skill publication differs from confirmed version intent")), libraryRoot)
			}
			// Capture before validating the exact published layout. Only retain
			// this fingerprint if the following checks prove it was the intended
			// one-version identity; later edits then fail RestoreExpected's guard.
			ownedFingerprint, fingerprintErr := operation.FingerprintPath(libraryRoot)
			if fingerprintErr != nil {
				return preservePath(errors.Join(ErrUnsafePath, fingerprintErr), libraryRoot)
			}
			publishedChecksum, checksumErr := skillrepo.ChecksumTree(published.Path)
			if checksumErr != nil || publishedChecksum != planned.SourceChecksum {
				return preservePath(errors.Join(ErrUnsafePath, errConcurrentEdit, checksumErr), libraryRoot)
			}
			resolved, resolveErr := repository.Resolve(identifier, published.Key)
			if resolveErr != nil || filepath.Clean(resolved.Path) != filepath.Clean(libraryPath) || resolved.Metadata.Checksum != planned.SourceChecksum {
				return preservePath(errors.Join(ErrUnsafePath, resolveErr, fmt.Errorf("adopted Skill metadata does not match publication intent")), libraryRoot)
			}
			if err := verifyNewAdoptedIdentity(libraryRoot, published.Key, expectedMetadata); err != nil {
				return preservePath(errors.Join(ErrUnsafePath, err), libraryRoot)
			}
			if s.recordOwnedState != nil {
				if err := s.recordOwnedState(libraryRoot, ownedFingerprint); err != nil {
					return preservePath(errors.Join(ErrUnsafePath, err), libraryRoot)
				}
			}
			return nil
		})
		if err != nil {
			if errors.Is(err, skillrepo.ErrConflict) {
				return preservePath(err, libraryRoot)
			}
			if errors.Is(err, skillrepo.ErrUnsafe) {
				checksum, checksumErr := skillrepo.ChecksumTree(path)
				if checksumErr != nil || checksum != planned.SourceChecksum {
					return preservePaths(errors.Join(ErrUnsafePath, errConcurrentEdit, err, checksumErr), path, libraryRoot)
				}
				if _, statErr := os.Lstat(libraryRoot); statErr == nil || !os.IsNotExist(statErr) {
					return preservePath(errors.Join(ErrUnsafePath, err), libraryRoot)
				}
			}
			return err
		}
		if entry.Path != libraryPath {
			return errors.Join(ErrUnsafePath, fmt.Errorf("adopted Skill version was published at unexpected path %s", entry.Path))
		}
		if err := libraryAnchor.validate(); err != nil {
			return err
		}
		if s.BeforePublish != nil {
			if err := s.BeforePublish("adopt-staged"); err != nil {
				return err
			}
		}
		equal, err := sameTree(path, libraryPath)
		if err != nil {
			return errors.Join(ErrUnsafePath, err)
		}
		if !equal {
			return preservePath(errors.Join(ErrUnsafePath, errConcurrentEdit), path)
		}
		if err := projectAnchor.validate(); err != nil {
			return err
		}
		hook := func(step string) error {
			if s.BeforePublish != nil {
				return s.BeforePublish(step)
			}
			return nil
		}
		if err := stagedDirectoryLink(path, libraryPath, hook, projectAnchor); err != nil {
			if errors.Is(err, errConcurrentEdit) {
				return preservePath(errors.Join(ErrUnsafePath, err), path)
			}
			return err
		}
		return nil
	})
}

// Fork replaces a managed link with an independent project-local copy.
func (s *Service) Fork(project string, target adapter.Target, identifier string) (operation.Plan, error) {
	return s.ForkMany(project, identifier, []adapter.Target{target})
}

// ForkMany forks selected managed links as one guarded transaction.
func (s *Service) ForkMany(project, identifier string, targets []adapter.Target) (operation.Plan, error) {
	project, err := filepath.Abs(project)
	if err != nil {
		return operation.Plan{}, err
	}
	plan := operation.NewPlan("fork")
	paths := make([]string, 0, len(targets))
	sources := make([]string, 0, len(targets))
	requests := make([]resource.SkillLifecycleRequest, 0, len(targets))
	baseRequest := resource.SkillLifecycleRequest{Action: resource.LifecycleFork, LibraryPath: s.LibraryPath, ProjectPath: project, Identifier: identifier}
	base, err := s.planSkill(baseRequest)
	if err != nil {
		return plan, err
	}
	seen := map[adapter.Target]bool{}
	for _, target := range targets {
		if seen[target] {
			return plan, fmt.Errorf("%w: duplicate target %q", ErrUnsafePath, target)
		}
		seen[target] = true
		placement, err := s.resourcePlacement(project, target, base.Resource)
		if err != nil {
			return plan, err
		}
		request := baseRequest
		request.PlacementPath = placement.Destination
		planned, err := s.planSkill(request)
		if err != nil {
			return plan, err
		}
		appendSkillPlan(&plan, planned)
		paths, sources = append(paths, placement.Destination), append(sources, planned.SourcePath)
		requests = append(requests, request)
	}
	if len(paths) == 0 {
		return plan, nil
	}
	if !s.confirmed(plan) {
		return plan, ErrNotConfirmed
	}
	return plan, s.mutate(plan, paths, func() error {
		for i := range paths {
			if err := adapter.ValidateProjectPlacement(targets[i], project, paths[i], identifier); err != nil {
				return err
			}
			replanned, err := s.planSkill(requests[i])
			if err != nil {
				return err
			}
			if replanned.CurrentSource != sources[i] {
				return ErrUnsafePath
			}
		}
		return nil
	}, func() error {
		candidates := make([]stagedCandidate, len(paths))
		anchors := make([]pathAnchor, len(paths))
		for i := range paths {
			if err := adapter.ValidateProjectPlacement(targets[i], project, paths[i], identifier); err != nil {
				return err
			}
			anchor, err := capturePathAnchor(filepath.Dir(paths[i]))
			if err != nil {
				return err
			}
			if err := anchor.validate(); err != nil {
				return err
			}
			anchors[i] = anchor
			candidate, err := prepareCopy(sources[i], paths[i])
			if err != nil {
				for _, prepared := range candidates {
					_ = os.RemoveAll(prepared.root)
				}
				return err
			}
			candidates[i] = candidate
			if err := anchor.validate(); err != nil {
				return err
			}
		}
		defer func() {
			for _, candidate := range candidates {
				_ = os.RemoveAll(candidate.root)
			}
		}()
		for i, path := range paths {
			if err := anchors[i].validate(); err != nil {
				return err
			}
			if err := adapter.RemoveManagedLink(project, path, sources[i]); err != nil {
				return err
			}
			if err := adapter.PublishCopy(project, path, candidates[i].path); err != nil {
				return err
			}
			if s.BeforePublish != nil {
				if err := s.BeforePublish("fork-published"); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

// Undo restores the pre-operation state of the latest confirmed operation.
func (s *Service) Undo() error {
	return s.UndoWithFingerprints(nil)
}

// UndoWithFingerprints restores the latest operation only when the reviewed
// journal backups still match the supplied evidence.
func (s *Service) UndoWithFingerprints(expected map[string]string) error {
	if s.Journal == nil {
		return fmt.Errorf("operation journal is not configured")
	}
	if s.BatchStore == nil {
		if s.OperationLockHeld {
			return s.Journal.UndoLatestWithHooksLocked(s.confirm, expected, nil, nil)
		}
		return s.Journal.UndoLatestWithFingerprints(s.confirm, expected)
	}
	var batch *operation.Batch
	beforeRestore := func(entry operation.Entry) error {
		if err := validateUndoLinkDependencies(entry); err != nil {
			return err
		}
		paths := make([]string, 0, len(entry.After))
		steps := make([]operation.BatchStep, 0, len(entry.After))
		for i, snapshot := range entry.After {
			paths = append(paths, snapshot.Path)
			before := "absent"
			if snapshot.Exists {
				var err error
				before, err = operation.FingerprintPath(snapshot.Path)
				if err != nil {
					return err
				}
			}
			after := "absent"
			backup := snapshot.Backup
			if i < len(entry.Before) && entry.Before[i].Exists {
				var err error
				after, err = operation.FingerprintPath(entry.Before[i].Backup)
				if err != nil {
					return err
				}
			}
			steps = append(steps, operation.BatchStep{ID: fmt.Sprintf("undo-%d", i), Resource: snapshot.Path, Action: "restore previous state", BeforeState: before, ExpectedAfter: "fingerprint\x00" + after, BackupPath: backup})
		}
		if err := s.BatchStore.CheckAvailable(paths); err != nil {
			return err
		}
		id, err := operation.NewBatchID()
		if err != nil {
			return err
		}
		entryCopy := entry
		prepared, err := s.BatchStore.Prepare(operation.Batch{ID: id, Kind: "undo", Scope: journalScope(s.Journal), Resources: paths, LinkDependencies: append([]operation.LinkDependency(nil), entry.LinkDependencies...), UndoEntry: &entryCopy, Steps: steps})
		if err != nil {
			return err
		}
		prepared.Status = operation.BatchPublishing
		if err := s.BatchStore.Update(prepared); err != nil {
			return err
		}
		batch = &prepared
		return nil
	}
	afterCommit := func(entry operation.Entry, outcome error) error {
		if batch == nil {
			return nil
		}
		if outcome != nil {
			batch.Status, batch.Error = operation.BatchNeedsRecovery, outcome.Error()
		} else {
			batch.Status, batch.Error = operation.BatchCommitted, ""
			for i := range batch.Steps {
				batch.Steps[i].Status = "restored"
				batch.Steps[i].RecoveryClass = "undo-complete"
				batch.Steps[i].StagedPath = ""
				batch.Steps[i].StageRoot = ""
				batch.Steps[i].StagedState = ""
				if i < len(entry.Before) && entry.Before[i].Exists {
					batch.Steps[i].AfterState, outcome = operation.FingerprintPath(entry.Before[i].Path)
					if outcome != nil {
						batch.Status, batch.Error = operation.BatchNeedsRecovery, outcome.Error()
						break
					}
				} else {
					batch.Steps[i].AfterState = "absent"
				}
			}
		}
		return s.BatchStore.Update(*batch)
	}
	previousRestoreStage := s.Journal.BeforeRestoreStage
	s.Journal.BeforeRestoreStage = func(path, stagedPath string) error {
		if previousRestoreStage != nil {
			if err := previousRestoreStage(path, stagedPath); err != nil {
				return err
			}
		}
		if batch == nil {
			return nil
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
				return s.BatchStore.Update(*batch)
			}
		}
		return ErrUnsafePath
	}
	defer func() { s.Journal.BeforeRestoreStage = previousRestoreStage }()
	if s.OperationLockHeld {
		return s.Journal.UndoLatestWithHooksLocked(s.confirm, expected, beforeRestore, afterCommit)
	}
	return s.Journal.UndoLatestWithHooks(s.confirm, expected, beforeRestore, afterCommit)
}

func validateUndoLinkDependencies(entry operation.Entry) error {
	return ValidateLinkDependenciesWithBefore(entry.LinkDependencies, entry.Before)
}

// ValidateLinkDependenciesWithBefore evaluates persisted source chains over a
// proposed before-state overlay. Paths present in before use their captured
// snapshots; external nodes are read from the current filesystem.
func ValidateLinkDependenciesWithBefore(dependencies []operation.LinkDependency, snapshots []operation.Snapshot) error {
	if len(dependencies) == 0 {
		return nil
	}
	before := make(map[string]operation.Snapshot, len(snapshots))
	for _, snapshot := range snapshots {
		before[filepath.Clean(snapshot.Path)] = snapshot
	}
	for _, dependency := range dependencies {
		current := filepath.Clean(dependency.EntryPath)
		for _, hop := range dependency.LinkChain {
			if current != filepath.Clean(hop.Path) {
				return errors.Join(operation.ErrUnexpectedState, fmt.Errorf("link dependency chain changed at %s", hop.Path))
			}
			target, err := projectedLinkTarget(current, before)
			if err != nil || target != hop.Target {
				return errors.Join(operation.ErrUnexpectedState, fmt.Errorf("link dependency changed at %s", current), err)
			}
			resolved := target
			if !filepath.IsAbs(resolved) {
				resolved = filepath.Join(filepath.Dir(current), resolved)
			}
			current, err = filepath.Abs(resolved)
			if err != nil {
				return errors.Join(operation.ErrUnexpectedState, err)
			}
			current = filepath.Clean(current)
		}
		if current != filepath.Clean(dependency.RealPath) {
			return errors.Join(operation.ErrUnexpectedState, fmt.Errorf("link dependency resolves to a different source"))
		}
		checksum, err := skillrepo.ChecksumTree(current)
		if err != nil || checksum != dependency.SourceChecksum {
			return errors.Join(operation.ErrUnexpectedState, fmt.Errorf("link dependency source content changed"), err)
		}
	}
	return nil
}

func rollbackExpectedCurrent(snapshots []operation.Snapshot, plan operation.Plan, additional map[string]string) (map[string]string, error) {
	afterByPath := make(map[string]string, len(plan.Changes))
	knownSources := plannedCopyFingerprints(plan)
	for _, change := range plan.Changes {
		path, err := filepath.Abs(change.Path)
		if err != nil {
			return nil, err
		}
		expected, err := expectedLifecycleAfterWithSourceFingerprints(change, knownSources)
		if err != nil {
			continue
		}
		afterByPath[filepath.Clean(path)] = expected
	}
	expected := make(map[string]string, len(snapshots))
	for _, snapshot := range snapshots {
		before := "absent"
		if snapshot.Exists {
			fingerprint, err := operation.FingerprintPath(snapshot.Backup)
			if err != nil {
				return nil, err
			}
			before = fingerprint
		}
		current, err := lifecycleFingerprintOrAbsent(snapshot.Path)
		if err != nil {
			expected[snapshot.Path] = "unverified-current-state"
			continue
		}
		if current == before {
			expected[snapshot.Path] = current
			continue
		}
		if after := additional[filepath.Clean(snapshot.Path)]; after != "" {
			matches, matchErr := matchesLifecyclePublication(snapshot.Path, after)
			if matchErr == nil && matches {
				expected[snapshot.Path] = current
				continue
			}
		}
		if after := afterByPath[filepath.Clean(snapshot.Path)]; after != "" {
			matches, matchErr := matchesLifecyclePublication(snapshot.Path, after)
			if matchErr == nil && matches {
				expected[snapshot.Path] = current
				continue
			}
		}
		expected[snapshot.Path] = "unowned-current-state"
	}
	return expected, nil
}

func verifyNewAdoptedIdentity(identity, versionKey string, expectedMetadata skillrepo.Metadata) error {
	entries, err := os.ReadDir(identity)
	if err != nil {
		return err
	}
	if len(entries) != 2 || entries[0].Name() != skillrepo.MetadataFilename || entries[1].Name() != "versions" {
		return fmt.Errorf("new adopted Skill identity contains unexpected entries")
	}
	identityInfo, err := os.Lstat(identity)
	if err != nil || !identityInfo.IsDir() || identityInfo.Mode()&os.ModeSymlink != 0 {
		return errors.Join(err, fmt.Errorf("new adopted Skill identity is not a real directory"))
	}
	metadataInfo, err := os.Lstat(filepath.Join(identity, skillrepo.MetadataFilename))
	if err != nil || !metadataInfo.Mode().IsRegular() || metadataInfo.Mode()&os.ModeSymlink != 0 {
		return errors.Join(err, fmt.Errorf("new adopted Skill metadata is not a regular file"))
	}
	metadataBytes, err := os.ReadFile(filepath.Join(identity, skillrepo.MetadataFilename))
	if err != nil {
		return err
	}
	var actualMetadata skillrepo.Metadata
	if err := json.Unmarshal(metadataBytes, &actualMetadata); err != nil {
		return err
	}
	actualMetadataJSON, err := json.Marshal(actualMetadata)
	if err != nil {
		return err
	}
	expectedMetadataJSON, err := json.Marshal(expectedMetadata)
	if err != nil {
		return err
	}
	if !bytes.Equal(actualMetadataJSON, expectedMetadataJSON) || actualMetadata.Identifier == "" || actualMetadata.CurrentVersion != versionKey || len(actualMetadata.Versions) != 1 || actualMetadata.Versions[0].Key != versionKey {
		return fmt.Errorf("new adopted Skill metadata differs from the published identity")
	}
	versionsPath := filepath.Join(identity, "versions")
	versionsInfo, err := os.Lstat(versionsPath)
	if err != nil || !versionsInfo.IsDir() || versionsInfo.Mode()&os.ModeSymlink != 0 {
		return errors.Join(err, fmt.Errorf("new adopted Skill versions path is not a real directory"))
	}
	versions, err := os.ReadDir(versionsPath)
	if err != nil {
		return err
	}
	if len(versions) != 1 || versions[0].Name() != versionKey || !versions[0].IsDir() {
		return fmt.Errorf("new adopted Skill identity contains an unexpected version entry")
	}
	versionInfo, err := os.Lstat(filepath.Join(versionsPath, versionKey))
	if err != nil || !versionInfo.IsDir() || versionInfo.Mode()&os.ModeSymlink != 0 {
		return errors.Join(err, fmt.Errorf("new adopted Skill version is not a real directory"))
	}
	return nil
}

func lifecycleFingerprintOrAbsent(path string) (string, error) {
	if _, err := os.Lstat(path); os.IsNotExist(err) {
		return "absent", nil
	} else if err != nil {
		return "", err
	}
	return operation.FingerprintPath(path)
}

func matchesLifecyclePublication(path, expected string) (bool, error) {
	if expected == "absent" {
		_, err := os.Lstat(path)
		return os.IsNotExist(err), err
	}
	parts := strings.Split(expected, "\x00")
	if len(parts) < 2 {
		return false, nil
	}
	switch parts[0] {
	case "fingerprint":
		if len(parts) != 2 {
			return false, nil
		}
		fingerprint, err := operation.FingerprintPath(path)
		return err == nil && fingerprint == parts[1], err
	case "symlink":
		info, err := os.Lstat(path)
		if os.IsNotExist(err) || err == nil && info.Mode()&os.ModeSymlink == 0 {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		target, err := os.Readlink(path)
		return err == nil && target == parts[1], err
	case "copy":
		want := parts[1]
		if len(parts) == 3 {
			want = parts[2]
		}
		fingerprint, err := operation.FingerprintPath(path)
		return err == nil && fingerprint == want, err
	case "auto":
		if len(parts) != 3 {
			return false, nil
		}
		info, err := os.Lstat(path)
		if err != nil {
			return false, err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(path)
			return err == nil && target == parts[1], err
		}
		fingerprint, err := operation.FingerprintPath(path)
		return err == nil && fingerprint == parts[2], err
	default:
		return false, nil
	}
}

func expectedLifecycleAfter(change operation.Change) (string, error) {
	return expectedLifecycleAfterWithSourceFingerprints(change, nil)
}

func expectedLifecycleAfterWithSourceFingerprints(change operation.Change, knownSources map[string]string) (string, error) {
	action := strings.ToLower(change.Action)
	if strings.Contains(action, "remove") {
		return "absent", nil
	}
	parts := strings.SplitN(change.Detail, " (", 2)
	if len(parts) == 0 || strings.TrimSpace(parts[0]) == "" {
		return "", fmt.Errorf("lifecycle publication is missing its source")
	}
	source, err := filepath.Abs(parts[0])
	if err != nil {
		return "", err
	}
	mode := "symlink-only"
	if strings.Contains(change.Detail, "copy-only") || strings.Contains(change.Action, "copy Skill tree") {
		mode = "copy-only"
	} else if strings.Contains(change.Detail, "auto: symlink first, copy fallback") {
		mode = "auto"
	} else if strings.Contains(change.Detail, "symlink-only") {
		mode = "symlink-only"
	}
	fingerprint, err := operation.FingerprintPath(source)
	if err != nil {
		if os.IsNotExist(err) {
			fingerprint = knownSources[filepath.Clean(source)]
		}
		if fingerprint == "" {
			return "", fmt.Errorf("fingerprint lifecycle source: %w", err)
		}
	}
	switch mode {
	case "copy-only":
		return "copy\x00" + source + "\x00" + fingerprint, nil
	case "auto":
		return "auto\x00" + source + "\x00" + fingerprint, nil
	default:
		if strings.Contains(action, "copy") {
			return "copy\x00" + source + "\x00" + fingerprint, nil
		}
		return "symlink\x00" + source + "\x00" + fingerprint, nil
	}
}

func plannedCopyFingerprints(plan operation.Plan) map[string]string {
	known := make(map[string]string)
	for _, change := range plan.Changes {
		if !strings.Contains(strings.ToLower(change.Action), "copy") {
			continue
		}
		parts := strings.SplitN(change.Detail, " (", 2)
		if len(parts) == 0 || strings.TrimSpace(parts[0]) == "" {
			continue
		}
		source, err := filepath.Abs(parts[0])
		if err != nil {
			continue
		}
		destination, err := filepath.Abs(change.Path)
		if err != nil {
			continue
		}
		fingerprint, err := operation.FingerprintPath(source)
		if err == nil {
			known[filepath.Clean(destination)] = fingerprint
		}
	}
	return known
}

func projectedLinkTarget(path string, before map[string]operation.Snapshot) (string, error) {
	if snapshot, ok := before[filepath.Clean(path)]; ok {
		if !snapshot.Exists || snapshot.Backup == "" {
			return "", operation.ErrUnexpectedState
		}
		info, err := os.Lstat(snapshot.Backup)
		if err != nil || info.Mode()&os.ModeSymlink == 0 {
			return "", errors.Join(operation.ErrUnexpectedState, err)
		}
		return os.Readlink(snapshot.Backup)
	}
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		return "", errors.Join(operation.ErrUnexpectedState, err)
	}
	return os.Readlink(path)
}

func (s *Service) resourcePlacement(project string, target adapter.Target, managed resource.ManagedResource) (adapter.Placement, error) {
	a, ok := adapter.For(target)
	if !ok {
		return adapter.Placement{}, fmt.Errorf("unsupported target %q", target)
	}
	return a.PlanPlacement(adapter.PlacementRequest{Project: project, Resource: managed})
}

func (s *Service) resourcePlacementInScope(root string, target adapter.Target, managed resource.ManagedResource, global bool) (adapter.Placement, error) {
	if !global {
		return s.resourcePlacement(root, target, managed)
	}
	a, ok := adapter.ForAgent(target)
	if !ok {
		return adapter.Placement{}, fmt.Errorf("unsupported target %q", target)
	}
	if managed.Kind != resource.Skill {
		return adapter.Placement{}, &adapter.UnsupportedResourceKindError{Target: target, Kind: managed.Kind}
	}
	destination := a.GlobalSkillPath(root, managed.ID)
	if err := adapter.ValidateGlobalResourcePlacement(target, root, destination, managed.Kind, managed.ID); err != nil {
		return adapter.Placement{}, err
	}
	return adapter.Placement{Target: target, Kind: managed.Kind, Identifier: managed.ID, Destination: destination}, nil
}

func validateLifecyclePlacement(target adapter.Target, root, destination, identifier string, global bool) error {
	if global {
		return adapter.ValidateGlobalResourcePlacement(target, root, destination, resource.Skill, identifier)
	}
	return adapter.ValidateProjectPlacement(target, root, destination, identifier)
}

func (s *Service) planSkill(request resource.SkillLifecycleRequest) (resource.SkillLifecyclePlan, error) {
	planned, err := s.resourceHandler.PlanLifecycle(request)
	if err != nil {
		return resource.SkillLifecyclePlan{}, err
	}
	plan, ok := planned.(resource.SkillLifecyclePlan)
	if !ok {
		return resource.SkillLifecyclePlan{}, fmt.Errorf("resource handler returned %T for Skill lifecycle request", planned)
	}
	return plan, nil
}

func appendSkillPlan(destination *operation.Plan, source resource.SkillLifecyclePlan) {
	for _, change := range source.Changes {
		destination.Changes = append(destination.Changes, operation.Change{Path: change.Path, Action: change.Action, Detail: change.Detail})
	}
	destination.Warnings = append(destination.Warnings, source.Warnings...)
}

func skillConflict(strategy ConflictStrategy) resource.SkillConflictStrategy {
	if strategy == ConflictReplace {
		return resource.SkillConflictReplace
	}
	return ""
}

func (s *Service) confirmed(plan operation.Plan) bool { return s.confirm != nil && s.confirm(plan) }
func (s *Service) mutate(plan operation.Plan, paths []string, validate func() error, change func() error) error {
	return s.mutateWithBefore(plan, paths, nil, validate, change)
}

func (s *Service) mutateWithBefore(plan operation.Plan, paths []string, before []operation.Snapshot, validate func() error, change func() error) error {
	return s.mutateWithBeforeMaterializations(plan, paths, before, validate, func() ([]operation.Materialization, error) {
		return nil, change()
	})
}

func (s *Service) mutateWithBeforeMaterializations(plan operation.Plan, paths []string, before []operation.Snapshot, validate func() error, change func() ([]operation.Materialization, error)) error {
	if s.Journal == nil {
		return fmt.Errorf("operation journal is not configured")
	}
	if !s.OperationLockHeld {
		unlock, err := s.Journal.LockOperation()
		if err != nil {
			return err
		}
		defer unlock()
	}
	if s.BatchStore != nil {
		if err := s.BatchStore.CheckAvailable(paths); err != nil {
			return err
		}
	}
	if err := validate(); err != nil {
		return err
	}
	var err error
	if before == nil {
		before, err = s.Journal.Capture(paths)
		if err != nil {
			return err
		}
	}
	if err := operation.SyncSnapshots(before); err != nil {
		removeSnapshots(before)
		return err
	}
	var batch *operation.Batch
	batchID := ""
	knownSources := plannedCopyFingerprints(plan)
	if s.BatchStore != nil {
		id, err := operation.NewBatchID()
		if err != nil {
			removeSnapshots(before)
			return err
		}
		batchID = id
		s.LastBatchID = id
		steps := make([]operation.BatchStep, 0, len(paths))
		for i, path := range paths {
			abs, err := filepath.Abs(path)
			if err != nil {
				removeSnapshots(before)
				return err
			}
			step := operation.BatchStep{ID: fmt.Sprintf("step-%d", i), Resource: abs, Action: "publish planned state"}
			if i < len(before) {
				if before[i].Exists {
					step.BeforeState, err = operation.FingerprintPath(before[i].Backup)
					if err != nil {
						removeSnapshots(before)
						return err
					}
				} else {
					step.BeforeState = "absent"
				}
				step.BackupPath = before[i].Backup
			}
			for _, planned := range plan.Changes {
				plannedPath, absErr := filepath.Abs(planned.Path)
				if absErr == nil && filepath.Clean(plannedPath) == filepath.Clean(abs) {
					step.Action = planned.Action
					step.ExpectedAfter, err = expectedLifecycleAfterWithSourceFingerprints(planned, knownSources)
					if err != nil {
						removeSnapshots(before)
						return err
					}
					break
				}
			}
			steps = append(steps, step)
		}
		prepared, err := s.BatchStore.Prepare(operation.Batch{ID: id, Kind: plan.Operation, Scope: journalScope(s.Journal), Resources: paths, LinkDependencies: append([]operation.LinkDependency(nil), s.linkDependencies...), Plan: plan, Steps: steps})
		if err != nil {
			removeSnapshots(before)
			return err
		}
		prepared.Status = operation.BatchPublishing
		if err := s.BatchStore.Update(prepared); err != nil {
			return err
		}
		batch = &prepared
		previousRecorder := s.recordPublication
		previousStageRecorder := s.recordStageMove
		s.recordPublication = func(path string) error {
			abs, err := filepath.Abs(path)
			if err != nil {
				return err
			}
			fingerprint, err := operation.FingerprintPath(abs)
			if err != nil {
				return err
			}
			for i := range batch.Steps {
				if filepath.Clean(batch.Steps[i].Resource) == filepath.Clean(abs) {
					batch.Steps[i].AfterState = fingerprint
					batch.Steps[i].Status = "published"
					batch.Steps[i].RecoveryClass = "after-state-recorded"
					batch.Steps[i].StagedPath = ""
					batch.Steps[i].StageRoot = ""
					batch.Steps[i].StagedState = ""
					return s.BatchStore.Update(*batch)
				}
			}
			return ErrUnsafePath
		}
		s.recordStageMove = func(destination, stagedPath string) error {
			absDestination, err := filepath.Abs(destination)
			if err != nil {
				return err
			}
			absStage, err := filepath.Abs(stagedPath)
			if err != nil {
				return err
			}
			state, err := operation.FingerprintPath(absDestination)
			if err != nil {
				return err
			}
			for i := range batch.Steps {
				if filepath.Clean(batch.Steps[i].Resource) == filepath.Clean(absDestination) {
					batch.Steps[i].StagedPath = filepath.Clean(absStage)
					batch.Steps[i].StageRoot = filepath.Dir(absStage)
					batch.Steps[i].StagedState = state
					batch.Steps[i].Status = "moving-away"
					batch.Steps[i].RecoveryClass = "staged-before-publication"
					return s.BatchStore.Update(*batch)
				}
			}
			return ErrUnsafePath
		}
		defer func() {
			s.recordPublication = previousRecorder
			s.recordStageMove = previousStageRecorder
		}()
		previousRestoreStage := s.Journal.BeforeRestoreStage
		s.Journal.BeforeRestoreStage = func(path, stagedPath string) error {
			if previousRestoreStage != nil {
				if err := previousRestoreStage(path, stagedPath); err != nil {
					return err
				}
			}
			return s.recordStageMove(path, stagedPath)
		}
		defer func() { s.Journal.BeforeRestoreStage = previousRestoreStage }()
	}
	additionalExpected := make(map[string]string)
	previousOwnedStateRecorder := s.recordOwnedState
	s.recordOwnedState = func(path, fingerprint string) error {
		abs, err := filepath.Abs(path)
		if err != nil {
			return err
		}
		abs = filepath.Clean(abs)
		if fingerprint == "" {
			return operation.ErrUnexpectedState
		}
		expected := "fingerprint\x00" + fingerprint
		additionalExpected[abs] = expected
		if batch == nil {
			return nil
		}
		for i := range batch.Steps {
			if filepath.Clean(batch.Steps[i].Resource) == abs {
				batch.Steps[i].AfterState = fingerprint
				batch.Steps[i].Status = "published"
				batch.Steps[i].RecoveryClass = "after-state-recorded"
				return s.BatchStore.Update(*batch)
			}
		}
		return operation.ErrUnexpectedState
	}
	defer func() { s.recordOwnedState = previousOwnedStateRecorder }()
	finishBatch := func(status operation.BatchStatus, cause error) error {
		if batch == nil {
			return nil
		}
		batch.Status = status
		batch.Error = ""
		if cause != nil {
			batch.Error = cause.Error()
		}
		return s.BatchStore.Update(*batch)
	}
	restoreBefore := func(snapshots []operation.Snapshot) error {
		expected, err := rollbackExpectedCurrent(snapshots, plan, additionalExpected)
		if err != nil {
			return err
		}
		return s.Journal.RestoreExpected(snapshots, expected)
	}
	materializations, err := change()
	if err != nil {
		var preserve *preservePathsError
		if errors.As(err, &preserve) {
			rollbackErr := restoreBefore(excludingPaths(before, preserve.paths))
			status := operation.BatchNeedsRecovery
			if rollbackErr == nil {
				status = operation.BatchRolledBack
			}
			finishErr := finishBatch(status, errors.Join(err, rollbackErr))
			return errors.Join(err, rollbackErr, finishErr)
		}
		if errors.Is(err, errConcurrentEdit) {
			return errors.Join(err, finishBatch(operation.BatchNeedsRecovery, err))
		}
		rollbackErr := restoreBefore(before)
		status := operation.BatchNeedsRecovery
		if rollbackErr == nil {
			status = operation.BatchRolledBack
		}
		finishErr := finishBatch(status, errors.Join(err, rollbackErr))
		return errors.Join(err, rollbackErr, finishErr)
	}
	if err := operation.SyncPaths(paths); err != nil {
		rollbackErr := restoreBefore(before)
		status := operation.BatchNeedsRecovery
		if rollbackErr == nil {
			status = operation.BatchRolledBack
		}
		finishErr := finishBatch(status, errors.Join(err, rollbackErr))
		return errors.Join(err, rollbackErr, finishErr)
	}
	after, err := s.Journal.Capture(paths)
	if err != nil {
		rollbackErr := restoreBefore(before)
		status := operation.BatchNeedsRecovery
		if rollbackErr == nil {
			status = operation.BatchRolledBack
		}
		finishErr := finishBatch(status, errors.Join(err, rollbackErr))
		return errors.Join(err, rollbackErr, finishErr)
	}
	if err := operation.SyncSnapshots(after); err != nil {
		removeSnapshots(after)
		rollbackErr := restoreBefore(before)
		status := operation.BatchNeedsRecovery
		if rollbackErr == nil {
			status = operation.BatchRolledBack
		}
		finishErr := finishBatch(status, errors.Join(err, rollbackErr))
		return errors.Join(err, rollbackErr, finishErr)
	}
	if batch != nil {
		for i := range batch.Steps {
			for _, snapshot := range after {
				if filepath.Clean(snapshot.Path) == filepath.Clean(batch.Steps[i].Resource) {
					if snapshot.Exists {
						batch.Steps[i].AfterState, err = operation.FingerprintPath(snapshot.Path)
						if err != nil {
							rollbackErr := restoreBefore(before)
							status := operation.BatchNeedsRecovery
							if rollbackErr == nil {
								status = operation.BatchRolledBack
							}
							finishErr := finishBatch(status, errors.Join(err, rollbackErr))
							return errors.Join(err, rollbackErr, finishErr)
						}
						batch.Steps[i].Status = "published"
					} else {
						batch.Steps[i].AfterState = "absent"
						batch.Steps[i].Status = "absent"
					}
				}
			}
		}
		batch.Materializations = append([]operation.Materialization(nil), materializations...)
		if err := s.BatchStore.Update(*batch); err != nil {
			rollbackErr := restoreBefore(before)
			status := operation.BatchNeedsRecovery
			if rollbackErr == nil {
				status = operation.BatchRolledBack
			}
			finishErr := finishBatch(status, errors.Join(err, rollbackErr))
			return errors.Join(err, rollbackErr, finishErr)
		}
	}
	if err := s.Journal.RecordPlanWithEvidence(plan, before, after, materializations, batchID, s.linkDependencies); err != nil {
		if errors.Is(err, operation.ErrJournalCommitted) {
			return errors.Join(err, finishBatch(operation.BatchNeedsRecovery, err))
		}
		rollbackErr := restoreBefore(before)
		status := operation.BatchNeedsRecovery
		if rollbackErr == nil {
			status = operation.BatchRolledBack
		}
		finishErr := finishBatch(status, errors.Join(err, rollbackErr))
		return errors.Join(err, rollbackErr, finishErr)
	}
	if err := finishBatch(operation.BatchCommitted, nil); err != nil {
		return fmt.Errorf("operation journal is committed but its batch marker could not be finalized: %w", err)
	}
	return nil
}

func journalScope(journal *operation.Journal) string {
	if journal != nil && filepath.Base(journal.Path) == "global.json" {
		return "global"
	}
	return "project"
}

func excludingPaths(snapshots []operation.Snapshot, paths map[string]struct{}) []operation.Snapshot {
	result := make([]operation.Snapshot, 0, len(snapshots))
	for _, snapshot := range snapshots {
		if _, skip := paths[snapshot.Path]; !skip {
			result = append(result, snapshot)
		}
	}
	return result
}

func removeSnapshots(snapshots []operation.Snapshot) {
	for _, snapshot := range snapshots {
		if snapshot.Exists && snapshot.Backup != "" {
			_ = os.RemoveAll(snapshot.Backup)
		}
	}
}

func newPlacementJournal() (*operation.Journal, func(), error) {
	root, err := os.MkdirTemp("", ".skill-manager-placement-")
	if err != nil {
		return nil, nil, err
	}
	return operation.New(filepath.Join(root, "journal.json")), func() { _ = os.RemoveAll(root) }, nil
}

func (s *Service) publishSkill(request resource.SkillLifecycleRequest, path, expectedSource string, journal *operation.Journal, initialSnapshot *operation.Snapshot) error {
	planned, err := s.planSkill(request)
	if err != nil {
		// A destination that appeared after the aggregate confirmation is a
		// no-replace conflict. Validate the source independently before retaining
		// the late owner and rolling back the other transaction paths.
		if errors.Is(err, ErrUnsafePath) || errors.Is(err, ErrForceRequired) {
			withoutPlacement := request
			withoutPlacement.PlacementPath = ""
			base, sourceErr := s.planSkill(withoutPlacement)
			if sourceErr != nil {
				return sourceErr
			}
			if base.SourcePath != expectedSource {
				return ErrUnsafePath
			}
			return preservePath(err, path)
		}
		return err
	}
	if planned.SourcePath != expectedSource {
		return ErrUnsafePath
	}
	beforeStageMove := func(stagedPath string) error {
		if s.recordStageMove != nil {
			return s.recordStageMove(path, stagedPath)
		}
		return nil
	}
	if err := placeFilesystem(planned, request, journal, initialSnapshot, s.Materialization, beforeStageMove, s.BeforeFinalPublish, s.BeforeDiscard); err != nil {
		var preserved *adapter.PreservedPathError
		if errors.As(err, &preserved) {
			return preservePath(err, preserved.Path)
		}
		if errors.Is(err, adapter.ErrLateConflict) || errors.Is(err, adapter.ErrUnsafePath) {
			return preservePath(errors.Join(ErrUnsafePath, err), path)
		}
		return err
	}
	if s.recordPublication != nil {
		if err := s.recordPublication(path); err != nil {
			return err
		}
	}
	return nil
}

func placeFilesystem(planned resource.SkillLifecyclePlan, request resource.SkillLifecycleRequest, journal *operation.Journal, initialSnapshot *operation.Snapshot, mode adapter.MaterializationMode, beforeStageMove func(string) error, beforeFinalPublish, beforeDiscard func() error) error {
	_, err := adapter.PlaceFilesystem(resource.PlacementPlan{Project: request.ProjectPath, Resource: planned.Resource}, adapter.FilesystemPlacementOptions{
		Project:            request.ProjectPath,
		Target:             adapter.Target(request.Target),
		Destination:        request.PlacementPath,
		GlobalScope:        request.GlobalScope,
		Conflict:           adapter.ConflictStrategy(request.Conflict),
		Force:              request.Force,
		Materialization:    mode,
		ExistingCopyOwned:  request.JournalCopyMatches,
		Rematerialize:      request.RematerializeManaged,
		Journal:            journal,
		InitialSnapshot:    initialSnapshot,
		BeforeFinalPublish: beforeFinalPublish,
		BeforeStageMove:    beforeStageMove,
		BeforeDiscard:      beforeDiscard,
		// The lifecycle coordinator already confirmed the aggregate plan. The
		// adapter still performs its own final source and destination checks.
		Confirm: func(operation.Plan) bool { return true },
	})
	return err
}

func rollbackError(err, rollback error) error {
	if rollback != nil {
		return errors.Join(err, rollback)
	}
	return err
}
func copyTree(source, destination string) error {
	info, err := os.Lstat(source)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(source)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
			return err
		}
		return os.Symlink(target, destination)
	}
	if info.IsDir() {
		if err := os.MkdirAll(destination, info.Mode().Perm()); err != nil {
			return err
		}
		entries, err := os.ReadDir(source)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if err := copyTree(filepath.Join(source, entry.Name()), filepath.Join(destination, entry.Name())); err != nil {
				return err
			}
		}
		return nil
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("unsupported skill file type at %s", source)
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return err
	}
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(destination, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode().Perm())
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func sameTree(left, right string) (bool, error) {
	li, err := os.Lstat(left)
	if err != nil {
		return false, fmt.Errorf("inspect %s: %w", left, err)
	}
	ri, err := os.Lstat(right)
	if err != nil || li.Mode() != ri.Mode() {
		if err != nil {
			return false, fmt.Errorf("inspect %s: %w", right, err)
		}
		return false, nil
	}
	if li.Mode()&os.ModeSymlink != 0 {
		l, e1 := os.Readlink(left)
		r, e2 := os.Readlink(right)
		if e1 != nil {
			return false, e1
		}
		if e2 != nil {
			return false, e2
		}
		return l == r, nil
	}
	if li.IsDir() {
		le, e1 := os.ReadDir(left)
		re, e2 := os.ReadDir(right)
		if e1 != nil || e2 != nil || len(le) != len(re) {
			if e1 != nil {
				return false, e1
			}
			if e2 != nil {
				return false, e2
			}
			return false, nil
		}
		for i := range le {
			if le[i].Name() != re[i].Name() {
				return false, nil
			}
			equal, err := sameTree(filepath.Join(left, le[i].Name()), filepath.Join(right, re[i].Name()))
			if err != nil {
				return false, err
			}
			if !equal {
				return false, nil
			}
		}
		return true, nil
	}
	if !li.Mode().IsRegular() {
		return false, nil
	}
	l, e1 := os.ReadFile(left)
	r, e2 := os.ReadFile(right)
	if e1 != nil {
		return false, e1
	}
	if e2 != nil {
		return false, e2
	}
	return bytes.Equal(l, r), nil
}

// stagedLink creates a sibling temporary link and atomically publishes it.
func stagedLink(destination, source string) error {
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return err
	}
	// Symlink creation is an atomic no-replace publication primitive.
	if err := os.Symlink(source, destination); err != nil {
		return fmt.Errorf("publish managed link: %w", err)
	}
	return nil
}

// stagedCopy copies into a sibling temporary directory before atomically replacing destination.
func stagedCopy(source, destination string, anchorPath ...string) error {
	candidate, err := prepareCopy(source, destination)
	if err != nil {
		return err
	}
	defer os.RemoveAll(candidate.root)
	if len(anchorPath) > 0 {
		return adapter.PublishCopy(anchorPath[0], destination, candidate.path)
	}
	return publishCopy(candidate, destination)
}

type stagedCandidate struct{ root, path string }

type pathAnchor struct {
	path string
	info os.FileInfo
}

// capturePathAnchor finds the nearest existing real directory for a path.
// Operations that may create descendants use this identity to fail closed if
// an ancestor is replaced by a link while the operation is staged.
func capturePathAnchor(path string) (pathAnchor, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return pathAnchor{}, err
	}
	for current := filepath.Clean(abs); ; current = filepath.Dir(current) {
		info, statErr := os.Lstat(current)
		if statErr == nil {
			if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
				return pathAnchor{}, errors.Join(ErrUnsafePath, fmt.Errorf("path anchor %q is not a real directory", current))
			}
			return pathAnchor{path: current, info: info}, nil
		}
		if !os.IsNotExist(statErr) {
			return pathAnchor{}, statErr
		}
		next := filepath.Dir(current)
		if next == current {
			return pathAnchor{}, errors.Join(ErrUnsafePath, fmt.Errorf("no existing path anchor for %q", path))
		}
	}
}

func (a pathAnchor) validate() error {
	info, err := os.Lstat(a.path)
	if err != nil {
		return errors.Join(ErrUnsafePath, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() || !os.SameFile(a.info, info) {
		return errors.Join(ErrUnsafePath, fmt.Errorf("path anchor %q changed", a.path))
	}
	return nil
}

func prepareCopy(source, destination string) (stagedCandidate, error) {
	parent := filepath.Dir(destination)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return stagedCandidate{}, err
	}
	// Keep the stage rooted at the directory identity that was just prepared;
	// retaining the lexical destination parent here would make deferred cleanup
	// follow a later parent symlink swap into an external tree.
	stagedParent, err := filepath.EvalSymlinks(parent)
	if err != nil {
		return stagedCandidate{}, err
	}
	root, err := os.MkdirTemp(stagedParent, ".skill-manager-stage-")
	if err != nil {
		return stagedCandidate{}, err
	}
	candidate := stagedCandidate{root: root, path: filepath.Join(root, "skill")}
	if err := copyTree(source, candidate.path); err != nil {
		_ = os.RemoveAll(root)
		return stagedCandidate{}, err
	}
	return candidate, nil
}
func publishCopy(candidate stagedCandidate, destination string) error {
	if _, err := os.Lstat(destination); err == nil {
		return ErrUnsafePath
	} else if !os.IsNotExist(err) {
		return err
	}
	return os.Rename(candidate.path, destination)
}

// stagedDirectoryLink preserves an existing directory until a staged link is ready.

func stagedDirectoryLink(destination, source string, hook func(string) error, anchor pathAnchor) error {
	if err := anchor.validate(); err != nil {
		return err
	}
	stage, err := os.MkdirTemp(filepath.Dir(destination), ".skill-manager-stage-")
	if err != nil {
		return err
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.RemoveAll(stage)
		}
	}()
	link, previous := filepath.Join(stage, "link"), filepath.Join(stage, "previous")
	if err := anchor.validate(); err != nil {
		return err
	}
	if err := os.Symlink(source, link); err != nil {
		return err
	}
	info, err := os.Lstat(destination)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return ErrUnsafePath
	}
	if err := anchor.validate(); err != nil {
		return err
	}
	if err := os.Rename(destination, previous); err != nil {
		return err
	}
	if hook != nil {
		if err := hook("adopt-moved"); err != nil {
			cleanup = false
			preserveErr := preserveMovedOriginal(destination, previous, err, hook, anchor)
			if _, statErr := os.Lstat(previous); os.IsNotExist(statErr) {
				cleanup = true
			}
			return preserveErr
		}
	}
	if _, err := os.Lstat(destination); err == nil {
		cleanup = false
		preserveErr := preserveMovedOriginal(destination, previous, nil, hook, anchor)
		if _, statErr := os.Lstat(previous); os.IsNotExist(statErr) {
			cleanup = true
		}
		return preserveErr
	} else if !os.IsNotExist(err) {
		cleanup = false
		preserveErr := preserveMovedOriginal(destination, previous, err, hook, anchor)
		if _, statErr := os.Lstat(previous); os.IsNotExist(statErr) {
			cleanup = true
		}
		return preserveErr
	}
	equal, err := sameTree(previous, source)
	if err != nil || !equal {
		cleanup = false
		preserveErr := preserveMovedOriginal(destination, previous, err, hook, anchor)
		if _, statErr := os.Lstat(previous); os.IsNotExist(statErr) {
			cleanup = true
		}
		return preserveErr
	}
	if err := anchor.validate(); err != nil {
		return err
	}
	if err := os.Rename(link, destination); err != nil {
		if rollbackErr := os.Rename(previous, destination); rollbackErr != nil {
			return fmt.Errorf("publish staged link: %w; rollback failed: %v", err, rollbackErr)
		}
		return err
	}
	return nil
}

// preserveMovedOriginal restores the original when its destination is still free;
// otherwise it moves it to an explicit sibling recovery path without deleting either tree.
func preserveMovedOriginal(destination, previous string, cause error, hook func(string) error, anchor pathAnchor) error {
	if _, err := os.Lstat(destination); os.IsNotExist(err) {
		if anchorErr := anchor.validate(); anchorErr != nil {
			return errors.Join(errConcurrentEdit, cause, anchorErr)
		}
		if restoreErr := os.Rename(previous, destination); restoreErr != nil {
			return errors.Join(errConcurrentEdit, cause, restoreErr)
		}
		return errors.Join(errConcurrentEdit, cause)
	}
	recovery := fmt.Sprintf("%s.skill-manager-recovery-%d", destination, time.Now().UnixNano())
	if hook != nil {
		if err := hook("adopt-recovery"); err != nil {
			return errors.Join(errConcurrentEdit, cause, err, fmt.Errorf("original remains at %s", previous))
		}
	}
	if anchorErr := anchor.validate(); anchorErr != nil {
		return errors.Join(errConcurrentEdit, cause, anchorErr, fmt.Errorf("original remains at %s", previous))
	}
	if err := os.Rename(previous, recovery); err != nil {
		return errors.Join(errConcurrentEdit, cause, fmt.Errorf("preserve moved original at %s: %w (original remains at %s)", recovery, err, previous))
	}
	return errors.Join(errConcurrentEdit, cause, fmt.Errorf("preserved moved original at %s", recovery))
}
