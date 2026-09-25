package adapter

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/AllenMuu/skill-manager/internal/operation"
	"github.com/AllenMuu/skill-manager/internal/resource"
	"github.com/AllenMuu/skill-manager/internal/subagent"
)

// SubAgentFilesystemOptions controls a guarded filesystem-backed SubAgent
// installation or removal. SourceRoot is the managed root for rendered
// representations; it is never treated as user-owned target content.
type SubAgentFilesystemOptions struct {
	Target     Target
	SourceRoot string
	Conflict   ConflictStrategy
	Force      bool
	Journal    *operation.Journal
	Confirm    func(operation.Plan) bool
	// SkillReferenceChecker is the validated canonical Skill-registry boundary
	// used before a SubAgent can be rendered or installed.
	SkillReferenceChecker subagent.SkillReferenceChecker
	// BeforePublish is invoked after confirmation and before publication.
	BeforePublish func() error
	// BeforeRemove is invoked after capture and before atomic staging.
	BeforeRemove func() error
	// BeforeFinalPublish runs immediately before the destination is published.
	BeforeFinalPublish func() error
	// BeforeDiscard injects a staged cleanup failure while leaving the stage
	// available for recovery.
	BeforeDiscard func() error
}

// InstallSubAgent renders and installs one canonical SubAgent through its
// target adapter. Targets without a verified filesystem representation return
// an explicit unsupported error and do not request confirmation.
func InstallSubAgent(definition subagent.Definition, request SubAgentRequest, options SubAgentFilesystemOptions) (operation.Plan, error) {
	target := options.Target
	if target == "" {
		target = ClaudeCode
	}
	a, ok := ForAgent(target)
	if !ok {
		return operation.Plan{}, fmt.Errorf("unsupported target %q", target)
	}
	if options.SkillReferenceChecker == nil {
		return operation.Plan{}, errors.New("SubAgent installation requires a Skill reference checker")
	}
	if err := definition.Validate(options.SkillReferenceChecker); err != nil {
		return operation.Plan{}, err
	}
	rendered, err := a.PlanSubAgent(definition, request)
	if err != nil {
		return operation.Plan{}, err
	}
	if !rendered.Supported() {
		return operation.Plan{}, fmt.Errorf("%w for %s: unsupported fields=%v capabilities=%v", ErrSubAgentUnsupported, target, rendered.UnsupportedFields, rendered.UnsupportedCapabilities)
	}
	root := options.SourceRoot
	if root == "" {
		root = request.Root
	}
	if root == "" || options.Journal == nil {
		return operation.Plan{}, errors.New("SubAgent filesystem placement requires source root and operation journal")
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return operation.Plan{}, fmt.Errorf("resolve SubAgent source root: %w", err)
	}
	if request.Root == "" {
		return operation.Plan{}, errors.New("SubAgent filesystem placement requires project root")
	}
	project, err := filepath.Abs(request.Root)
	if err != nil {
		return operation.Plan{}, fmt.Errorf("resolve SubAgent project root: %w", err)
	}
	ext := ".md"
	if rendered.Format == "codex-toml" {
		ext = ".toml"
	}
	source := filepath.Join(root, ".agent-manager", "subagents", string(target), definition.ID+ext)
	if err := validateManagedSourceParents(root, source); err != nil {
		return operation.Plan{}, err
	}
	managed := resource.ManagedResource{
		Version: "v1", ID: definition.ID, Kind: resource.SubAgent,
		Provenance:           resource.Provenance{Source: source},
		RequiredCapabilities: append([]resource.Capability(nil), definition.RequiredCapabilities...),
	}
	plan := resource.PlacementPlan{Project: project, Resource: managed, Destination: rendered.Destination, Conflict: string(options.Conflict), Force: options.Force}
	return PlaceFilesystem(plan, FilesystemPlacementOptions{
		Project:            project,
		Target:             target,
		Destination:        rendered.Destination,
		SourceRoot:         root,
		SourceContent:      []byte(rendered.Content),
		Conflict:           options.Conflict,
		Force:              options.Force,
		Journal:            options.Journal,
		Confirm:            options.Confirm,
		BeforePublish:      options.BeforePublish,
		BeforeFinalPublish: options.BeforeFinalPublish,
		BeforeDiscard:      options.BeforeDiscard,
	})
}

func validateManagedSourceParents(root, source string) error {
	root, err := filepath.Abs(root)
	if err != nil {
		return fmt.Errorf("%w: resolve SubAgent source root", ErrUnsafePath)
	}
	root = filepath.Clean(root)
	source, err = filepath.Abs(source)
	if err != nil {
		return fmt.Errorf("%w: resolve SubAgent source", ErrUnsafePath)
	}
	parent := filepath.Dir(filepath.Clean(source))
	rel, err := filepath.Rel(root, parent)
	if err != nil || rel == ".." || filepath.IsAbs(rel) || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("%w: SubAgent source escapes source root", ErrUnsafePath)
	}
	info, err := os.Lstat(root)
	if os.IsNotExist(err) {
		for ancestor := filepath.Dir(root); ; ancestor = filepath.Dir(ancestor) {
			ancestorInfo, ancestorErr := os.Lstat(ancestor)
			if ancestorErr == nil {
				if ancestorInfo.Mode()&os.ModeSymlink != 0 || !ancestorInfo.IsDir() {
					return fmt.Errorf("%w: SubAgent source ancestor %q must be a real directory", ErrUnsafePath, ancestor)
				}
				break
			}
			if !os.IsNotExist(ancestorErr) {
				return fmt.Errorf("%w: inspect SubAgent source ancestor: %w", ErrUnsafePath, ancestorErr)
			}
			next := filepath.Dir(ancestor)
			if next == ancestor {
				break
			}
		}
		return nil
	}
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("%w: SubAgent source root must be a real directory", ErrUnsafePath)
	}
	if rel == "." {
		return nil
	}
	current := root
	for _, component := range splitPath(rel) {
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("%w: SubAgent source parent %q must be a real directory", ErrUnsafePath, current)
		}
	}
	return nil
}

func splitPath(path string) []string {
	parts := []string{}
	for path != "." && path != "" {
		parent, base := filepath.Dir(path), filepath.Base(path)
		if base != "" && base != "." {
			parts = append([]string{base}, parts...)
		}
		if parent == path {
			break
		}
		path = parent
	}
	return parts
}

// RemoveSubAgent removes only a target representation owned by this manager.
// Ordinary files, directories, and links to another source are never removed.
func RemoveSubAgent(definition subagent.Definition, request SubAgentRequest, options SubAgentFilesystemOptions) (operation.Plan, error) {
	target := options.Target
	if target == "" {
		target = ClaudeCode
	}
	a, ok := ForAgent(target)
	if !ok {
		return operation.Plan{}, fmt.Errorf("unsupported target %q", target)
	}
	rendered, err := a.PlanSubAgent(definition, request)
	if err != nil {
		return operation.Plan{}, err
	}
	if !rendered.Supported() {
		return operation.Plan{}, fmt.Errorf("%w for %s: unsupported fields=%v capabilities=%v", ErrSubAgentUnsupported, target, rendered.UnsupportedFields, rendered.UnsupportedCapabilities)
	}
	if options.Journal == nil || request.Root == "" {
		return operation.Plan{}, errors.New("SubAgent filesystem removal requires project root and operation journal")
	}
	root := options.SourceRoot
	if root == "" {
		root = request.Root
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return operation.Plan{}, fmt.Errorf("resolve SubAgent source root: %w", err)
	}
	project, err := filepath.Abs(request.Root)
	if err != nil {
		return operation.Plan{}, fmt.Errorf("resolve SubAgent project root: %w", err)
	}
	ext := ".md"
	if rendered.Format == "codex-toml" {
		ext = ".toml"
	}
	source := filepath.Join(root, ".agent-manager", "subagents", string(target), definition.ID+ext)
	destination, err := filepath.Abs(rendered.Destination)
	if err != nil {
		return operation.Plan{}, err
	}
	if err := ValidateProjectResourcePlacement(target, project, destination, resource.SubAgent, definition.ID); err != nil {
		return operation.Plan{}, err
	}
	info, err := os.Lstat(destination)
	if err != nil {
		if os.IsNotExist(err) {
			return operation.Plan{}, fmt.Errorf("%w: managed SubAgent destination is absent", ErrUnsafePath)
		}
		return operation.Plan{}, err
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return operation.Plan{}, ErrUnsafePath
	}
	linked, err := os.Readlink(destination)
	if err != nil || linked != source {
		return operation.Plan{}, ErrUnsafePath
	}
	preview := operation.NewPlan("remove SubAgent")
	preview.ResourceKind = string(resource.SubAgent)
	preview.Changes = []operation.Change{{Path: destination, Action: "remove managed SubAgent"}}
	if options.Confirm == nil || !options.Confirm(preview) {
		return preview, ErrNotConfirmed
	}
	// Confirmation callbacks may involve user interaction. Re-open the parent
	// through the anchored no-follow publisher after confirmation so a parent
	// replacement cannot redirect removal to an external directory.
	parent, err := openAnchoredPublicationDirectoryNoCreate(project, filepath.Dir(destination))
	if err != nil {
		return preview, err
	}
	defer parent.Close()
	name := filepath.Base(destination)
	if !parent.Current() {
		return preview, errors.Join(ErrUnsafePath, errLateConflict)
	}
	linked, err = parent.Readlink(name)
	if err != nil || linked != source {
		return preview, ErrUnsafePath
	}
	before, err := options.Journal.Capture([]string{destination})
	if err != nil {
		return preview, err
	}
	if options.BeforeRemove != nil {
		if err := options.BeforeRemove(); err != nil {
			return preview, err
		}
	}
	if !parent.Current() {
		return preview, errors.Join(ErrUnsafePath, errLateConflict)
	}
	linked, err = parent.Readlink(name)
	if err != nil || linked != source {
		return preview, ErrUnsafePath
	}
	stage, err := parent.NewStage()
	if err != nil {
		return preview, err
	}
	stageClosed := false
	defer func() {
		if !stageClosed {
			_ = stage.Close()
		}
	}()
	if err := stage.MoveFrom(name); err != nil {
		_ = stage.Discard()
		stageClosed = true
		return preview, err
	}
	rollback := func(cause error) error {
		exists, inspectErr := parent.Exists(name)
		if inspectErr != nil {
			return errors.Join(cause, fmt.Errorf("restore staged SubAgent: %w (original remains at %s)", inspectErr, stage.Path()))
		}
		if !exists {
			if restoreErr := stage.RestoreAs(name); restoreErr != nil {
				return errors.Join(cause, restoreErr)
			}
			if closeErr := stage.Close(); closeErr != nil {
				return errors.Join(cause, closeErr)
			}
			stageClosed = true
			return cause
		}
		return errors.Join(cause, fmt.Errorf("restore staged SubAgent: destination was recreated; original remains at %s", stage.Path()))
	}
	if !stage.Matches(before[0].Backup) {
		return preview, rollback(ErrUnsafePath)
	}
	var discardErr error
	if options.BeforeDiscard != nil {
		discardErr = options.BeforeDiscard()
	} else {
		discardErr = stage.Discard()
	}
	if discardErr != nil {
		// Keep the staged original recoverable when cleanup fails. Rollback
		// restores it at the managed destination or reports its preserved stage
		// path without deleting it.
		return preview, rollback(discardErr)
	}
	stageClosed = true
	after, err := options.Journal.Capture([]string{destination})
	if err != nil {
		return preview, errors.Join(err, options.Journal.Restore(before))
	}
	if err := options.Journal.RecordPlan(preview, before, after); err != nil {
		if errors.Is(err, operation.ErrJournalCommitted) {
			// The journal entry is durable even though its final directory sync
			// reported an error. Preserve the published filesystem state.
			return preview, err
		}
		return preview, errors.Join(err, options.Journal.Restore(before))
	}
	return preview, nil
}
