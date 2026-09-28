// Package lifecycle manages project-local skill links without interpreting skill content.
package lifecycle

import (
	"bytes"
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
)

var (
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
	Target     adapter.Target `json:"target"`
	Identifier string         `json:"identifier"`
	Path       string         `json:"path"`
	SourcePath string         `json:"sourcePath,omitempty"`
	Status     Status         `json:"status"`
}

// ConfirmFunc displays a plan and returns whether the user confirmed it.
type ConfirmFunc func(operation.Plan) bool

// Service performs guarded lifecycle changes against one local skill library.
type Service struct {
	LibraryPath string
	Journal     *operation.Journal
	// BeforePublish is an optional test seam invoked before each staged publish.
	BeforePublish func(string) error
	// BeforeFinalPublish is an optional test seam invoked after the adapter has
	// anchored the destination parent and immediately before link publication.
	BeforeFinalPublish func() error
	// BeforeDiscard is an optional test seam invoked after a replacement link is
	// published and before its staged original is discarded.
	BeforeDiscard   func() error
	confirm         ConfirmFunc
	resourceHandler resource.ResourceHandler
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
	return &preservePathsError{cause: cause, paths: map[string]struct{}{path: {}}}
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
	return &Service{LibraryPath: libraryPath, Journal: journal, confirm: confirm, resourceHandler: handler}
}

// Add activates skill for each explicitly selected target using absolute soft links.
func (s *Service) Add(project string, skill catalog.Skill, targets []adapter.Target, opts ...Options) (operation.Plan, error) {
	options := optionsOf(opts)
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
	return plan, s.mutateWithBefore(plan, paths, beforeConfirmation, func() error {
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
	}, func() error {
		for i, path := range paths {
			var initialSnapshot *operation.Snapshot
			if i < len(beforeConfirmation) {
				initialSnapshot = &beforeConfirmation[i]
			}
			if err := s.publishSkill(requests[i], path, base.SourcePath, placementJournal, initialSnapshot); err != nil {
				return err
			}
		}
		return nil
	})
}

// Activate is an alias for Add.
func (s *Service) Activate(project string, skill catalog.Skill, targets []adapter.Target) (operation.Plan, error) {
	return s.Add(project, skill, targets)
}

// AddMany activates an explicit selection as one confirmed, journaled transaction.
func (s *Service) AddMany(project string, skills []catalog.Skill, targets []adapter.Target, opts ...Options) (operation.Plan, error) {
	options := optionsOf(opts)
	if err := verifySourceFingerprints(skills, options.ExpectedSourceFingerprints); err != nil {
		return operation.Plan{}, err
	}
	preview, err := s.buildAddManyPreview(project, skills, targets, options)
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
	return plan, s.mutateWithBefore(plan, paths, beforeConfirmation, func() error {
		if err := verifySourceFingerprints(skills, options.ExpectedSourceFingerprints); err != nil {
			return err
		}
		for i := range requests {
			if err := adapter.ValidateProjectPlacement(adapter.Target(requests[i].Target), requests[i].ProjectPath, paths[i], requests[i].Resource.ID); err != nil {
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
	}, func() error {
		for i, path := range paths {
			if err := verifySourceFingerprints(skills, options.ExpectedSourceFingerprints); err != nil {
				return err
			}
			var initialSnapshot *operation.Snapshot
			if i < len(beforeConfirmation) {
				initialSnapshot = &beforeConfirmation[i]
			}
			if err := s.publishSkill(requests[i], path, requests[i].Resource.Provenance.Source, placementJournal, initialSnapshot); err != nil {
				return err
			}
			if s.BeforePublish != nil {
				if err := s.BeforePublish("add-many-published"); err != nil {
					return err
				}
			}
		}
		return nil
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
	preview, err := s.buildAddManyPreview(project, skills, targets, optionsOf(opts))
	return preview.plan, err
}

func (s *Service) buildAddManyPreview(project string, skills []catalog.Skill, targets []adapter.Target, options Options) (addManyPreview, error) {
	preview := addManyPreview{plan: operation.NewPlan("activate selected skills")}
	project, err := filepath.Abs(project)
	if err != nil {
		return preview, err
	}
	seen := map[string]bool{}
	for _, skill := range skills {
		base, err := s.planSkill(resource.SkillLifecycleRequest{
			Action:       resource.LifecycleActivate,
			LibraryPath:  s.LibraryPath,
			CatalogEntry: &skill,
		})
		if err != nil {
			return preview, err
		}
		for _, target := range targets {
			placement, err := s.resourcePlacement(project, target, base.Resource)
			if err != nil {
				return preview, err
			}
			path := placement.Destination
			if seen[path] {
				return preview, ErrUnsafePath
			}
			seen[path] = true
			request := resource.SkillLifecycleRequest{
				Action:        resource.LifecycleActivate,
				LibraryPath:   s.LibraryPath,
				ProjectPath:   project,
				Resource:      base.Resource,
				PlacementPath: path,
				Target:        string(target),
				Conflict:      skillConflict(options.Conflict),
				Force:         options.Force,
			}
			planned, err := s.planSkill(request)
			if err != nil {
				return preview, err
			}
			preview.paths = append(preview.paths, path)
			preview.requests = append(preview.requests, request)
			appendSkillPlan(&preview.plan, planned)
		}
	}
	return preview, nil
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
	plan, project, placement, request, planned, err := s.buildRemovePreview(project, target, identifier)
	if err != nil {
		return plan, err
	}
	if !s.confirmed(plan) {
		return plan, ErrNotConfirmed
	}
	return plan, s.mutate(plan, []string{placement.Destination}, func() error {
		if err := adapter.ValidateProjectPlacement(target, project, placement.Destination, identifier); err != nil {
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
		if err := adapter.ValidateProjectPlacement(target, project, placement.Destination, identifier); err != nil {
			return err
		}
		replanned, err := s.planSkill(request)
		if err != nil {
			return err
		}
		if replanned.CurrentSource != planned.CurrentSource {
			return ErrUnsafePath
		}
		if s.BeforePublish != nil {
			if err := s.BeforePublish("remove-before"); err != nil {
				return err
			}
		}
		return adapter.RemoveManagedLink(project, placement.Destination, planned.CurrentSource)
	})
}

// PreviewRemove builds the exact guarded removal plan without changing the
// project or journal.
func (s *Service) PreviewRemove(project string, target adapter.Target, identifier string) (operation.Plan, error) {
	plan, _, _, _, _, err := s.buildRemovePreview(project, target, identifier)
	return plan, err
}

func (s *Service) buildRemovePreview(project string, target adapter.Target, identifier string) (operation.Plan, string, adapter.Placement, resource.SkillLifecycleRequest, resource.SkillLifecyclePlan, error) {
	plan := operation.NewPlan("remove")
	project, err := filepath.Abs(project)
	if err != nil {
		return plan, "", adapter.Placement{}, resource.SkillLifecycleRequest{}, resource.SkillLifecyclePlan{}, err
	}
	baseRequest := resource.SkillLifecycleRequest{Action: resource.LifecycleRemove, LibraryPath: s.LibraryPath, ProjectPath: project, Identifier: identifier}
	base, err := s.planSkill(baseRequest)
	if err != nil {
		return plan, project, adapter.Placement{}, baseRequest, resource.SkillLifecyclePlan{}, err
	}
	placement, err := s.resourcePlacement(project, target, base.Resource)
	if err != nil {
		return plan, project, adapter.Placement{}, baseRequest, resource.SkillLifecyclePlan{}, err
	}
	request := baseRequest
	request.PlacementPath = placement.Destination
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
	plan := operation.NewPlan("adopt")
	appendSkillPlan(&plan, planned)
	if !s.confirmed(plan) {
		return plan, ErrNotConfirmed
	}
	return plan, s.mutate(plan, []string{path, libraryPath}, func() error {
		if err := adapter.ValidateProjectPlacement(target, project, path, identifier); err != nil {
			return err
		}
		replanned, err := s.planSkill(request)
		if err != nil {
			return err
		}
		if replanned.SourcePath != planned.SourcePath || replanned.Resource.Provenance.Source != libraryPath {
			return ErrUnsafePath
		}
		return nil
	}, func() error {
		if err := adapter.ValidateProjectPlacement(target, project, path, identifier); err != nil {
			return err
		}
		replanned, err := s.planSkill(request)
		if err != nil {
			return err
		}
		if replanned.SourcePath != planned.SourcePath || replanned.Resource.Provenance.Source != libraryPath {
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
		if err := stagedCopy(path, libraryPath, libraryAnchor.path); err != nil {
			return err
		}
		if err := libraryAnchor.validate(); err != nil {
			return err
		}
		if s.BeforePublish != nil {
			if err := s.BeforePublish("adopt-staged"); err != nil {
				return err
			}
		}
		if err := libraryAnchor.validate(); err != nil {
			return err
		}
		equal, err := sameTree(path, libraryPath)
		if err != nil {
			return errors.Join(ErrUnsafePath, err)
		}
		if !equal {
			if err := os.RemoveAll(libraryPath); err != nil {
				return errors.Join(ErrUnsafePath, errConcurrentEdit, err)
			}
			return errors.Join(ErrUnsafePath, errConcurrentEdit)
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
				var cleanupErr error
				if anchorErr := libraryAnchor.validate(); anchorErr != nil {
					cleanupErr = anchorErr
				} else {
					cleanupErr = os.RemoveAll(libraryPath)
				}
				return preservePath(errors.Join(ErrUnsafePath, err, cleanupErr), path)
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
	if s.Journal == nil {
		return fmt.Errorf("operation journal is not configured")
	}
	return s.Journal.UndoLatest(s.confirm)
}

func (s *Service) resourcePlacement(project string, target adapter.Target, managed resource.ManagedResource) (adapter.Placement, error) {
	a, ok := adapter.For(target)
	if !ok {
		return adapter.Placement{}, fmt.Errorf("unsupported target %q", target)
	}
	return a.PlanPlacement(adapter.PlacementRequest{Project: project, Resource: managed})
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
	if s.Journal == nil {
		return fmt.Errorf("operation journal is not configured")
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
	if err := change(); err != nil {
		if errors.Is(err, errConcurrentEdit) {
			return err
		}
		var preserve *preservePathsError
		if errors.As(err, &preserve) {
			return rollbackError(err, s.Journal.Restore(excludingPaths(before, preserve.paths)))
		}
		return rollbackError(err, s.Journal.Restore(before))
	}
	after, err := s.Journal.Capture(paths)
	if err != nil {
		return rollbackError(err, s.Journal.Restore(before))
	}
	if err := s.Journal.RecordPlan(plan, before, after); err != nil {
		if errors.Is(err, operation.ErrJournalCommitted) {
			return err
		}
		return rollbackError(err, s.Journal.Restore(before))
	}
	return nil
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
	if err := placeFilesystem(planned, request, journal, initialSnapshot, s.BeforeFinalPublish, s.BeforeDiscard); err != nil {
		var preserved *adapter.PreservedPathError
		if errors.As(err, &preserved) {
			return preservePath(err, preserved.Path)
		}
		if errors.Is(err, adapter.ErrLateConflict) || errors.Is(err, adapter.ErrUnsafePath) {
			return preservePath(errors.Join(ErrUnsafePath, err), path)
		}
		return err
	}
	return nil
}

func placeFilesystem(planned resource.SkillLifecyclePlan, request resource.SkillLifecycleRequest, journal *operation.Journal, initialSnapshot *operation.Snapshot, beforeFinalPublish, beforeDiscard func() error) error {
	_, err := adapter.PlaceFilesystem(resource.PlacementPlan{Project: request.ProjectPath, Resource: planned.Resource}, adapter.FilesystemPlacementOptions{
		Project:            request.ProjectPath,
		Target:             adapter.Target(request.Target),
		Destination:        request.PlacementPath,
		Conflict:           adapter.ConflictStrategy(request.Conflict),
		Force:              request.Force,
		Journal:            journal,
		InitialSnapshot:    initialSnapshot,
		BeforeFinalPublish: beforeFinalPublish,
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
