// Package diagnostic inspects and repairs machine-local skill links.
package diagnostic

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/AllenMuu/skill-manager/internal/adapter"
	"github.com/AllenMuu/skill-manager/internal/operation"
	"github.com/AllenMuu/skill-manager/internal/resource"
)

// Finding is one non-mutating diagnostic result.
type Finding struct{ Path, Message string }

// BeforeReconcilePublish is an optional fault-injection seam.
var BeforeReconcilePublish func(string) error

// Scan reports catalog problems and project-local integration problems.
func Scan(library, project string, journals ...*operation.Journal) ([]Finding, error) {
	library, err := filepath.Abs(library)
	if err != nil {
		return nil, err
	}
	project, err = filepath.Abs(project)
	if err != nil {
		return nil, err
	}
	skillHandler := resource.NewSkillHandler()
	_, invalid, err := skillHandler.Discover(library)
	if err != nil {
		return nil, err
	}
	findings := make([]Finding, 0, len(invalid))
	journal := operation.New(filepath.Join(project, ".skill-manager", "journal.json"))
	if len(journals) > 0 && journals[0] != nil {
		journal = journals[0]
	}
	var managedPaths []string
	for _, d := range invalid {
		findings = append(findings, Finding{d.Path, "invalid catalog entry: " + d.Message})
	}
	for _, a := range adapter.Supported() {
		for _, kind := range a.ResourceKinds() {
			capabilities := a.Capabilities(kind)
			values := make([]string, len(capabilities))
			for i, capability := range capabilities {
				values[i] = string(capability)
			}
			if len(values) == 0 {
				values = []string{"none"}
			}
			findings = append(findings, Finding{
				Path:    string(a.Target()),
				Message: fmt.Sprintf("adapter capabilities: %s supports %s (%s)", a.Target(), kind, strings.Join(values, ", ")),
			})
		}
	}
	for _, a := range adapter.Supported() {
		inspections, err := a.Inspect(adapter.InspectionRequest{Project: project, Kind: resource.Skill})
		if err != nil {
			return nil, err
		}
		for _, inspection := range inspections {
			path := inspection.Path
			info, err := os.Lstat(path)
			if err != nil {
				return nil, err
			}
			if info.Mode()&os.ModeSymlink == 0 {
				findings = append(findings, Finding{path, "unmanaged resource"})
				continue
			}
			target, err := os.Readlink(path)
			if err != nil {
				return nil, err
			}
			managedByLibrary, err := skillHandler.ManagesSource(library, inspection.Identifier, target)
			if err != nil {
				return nil, err
			}
			if _, err := os.Stat(target); os.IsNotExist(err) {
				recorded, owned, recordErr := journal.RecordedLinkTarget(path)
				if recordErr != nil {
					return nil, recordErr
				}
				if managedByLibrary || (owned && recorded == target) {
					findings = append(findings, Finding{path, "orphaned managed link: " + target})
				} else {
					findings = append(findings, Finding{path, "ambiguous dangling link: refusing automatic reconciliation"})
				}
			} else if err != nil {
				return nil, fmt.Errorf("inspect link target %s: %w", target, err)
			} else if !managedByLibrary {
				findings = append(findings, Finding{path, "unmanaged resource"})
			}
			if managedByLibrary {
				managedPaths = append(managedPaths, path)
			}
		}
	}
	unsupportedRoots, err := adapter.UnsupportedAgentRoots(project)
	if err != nil {
		return nil, err
	}
	for _, root := range unsupportedRoots {
		findings = append(findings, Finding{root, "unsupported agent skill location"})
	}
	if _, err := os.Stat(filepath.Join(project, ".git")); err == nil {
		if err := exec.Command("git", "-C", project, "rev-parse", "--is-inside-work-tree").Run(); err != nil {
			findings = append(findings, Finding{project, "Git guidance: do not track managed absolute links; initialize/repair this Git worktree to inspect exact status"})
			return findings, nil
		}
		for _, path := range managedPaths {
			status := "would be tracked"
			rel, err := filepath.Rel(project, path)
			if err != nil {
				return nil, err
			}
			if err := exec.Command("git", "-C", project, "ls-files", "--error-unmatch", "--", rel).Run(); err == nil {
				status = "tracked"
			} else if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 1 {
				return nil, fmt.Errorf("inspect Git tracked state: %w", err)
			} else if err := exec.Command("git", "-C", project, "check-ignore", "-q", "--", rel).Run(); err == nil {
				status = "ignored"
			} else if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 1 {
				return nil, fmt.Errorf("inspect Git ignore state: %w", err)
			}
			findings = append(findings, Finding{path, "Git guidance: managed absolute link is " + status + "; add this exact managed path to .gitignore after confirmation"})
		}
	}
	return findings, nil
}

// AddGitignore appends only supplied managed project-link paths after confirmation.
func AddGitignore(project string, paths []string, confirm func(operation.Plan) bool, journals ...*operation.Journal) (operation.Plan, error) {
	project, err := filepath.Abs(project)
	if err != nil {
		return operation.Plan{}, err
	}
	gitignore := filepath.Join(project, ".gitignore")
	journal := operation.New(filepath.Join(project, ".skill-manager", "journal.json"))
	if len(journals) > 0 && journals[0] != nil {
		journal = journals[0]
	}
	plan := operation.NewPlan("update managed-link Git guidance")
	skillHandler := resource.NewSkillHandler()
	lines := make([]string, 0, len(paths))
	for _, path := range paths {
		info, err := os.Lstat(path)
		if err != nil {
			return plan, fmt.Errorf("inspect managed link %s: %w", path, err)
		}
		if info.Mode()&os.ModeSymlink == 0 {
			return plan, fmt.Errorf("refusing unmanaged path %s", path)
		}
		target, err := os.Readlink(path)
		if err != nil {
			return plan, err
		}
		_, identifier, supportedPlacement := adapter.MatchProjectSkillPath(project, path)
		if !supportedPlacement {
			return plan, fmt.Errorf("refusing unsupported managed-link placement %s", path)
		}
		targetOwned := false
		if ok, err := eligibleSkill(skillHandler, target, identifier); err == nil && ok {
			targetOwned = true
		} else if err != nil {
			return plan, err
		}
		if !targetOwned {
			recorded, owned, err := journal.RecordedLinkTarget(path)
			if err != nil {
				return plan, err
			}
			targetOwned = owned && recorded == target
		}
		if !targetOwned {
			return plan, fmt.Errorf("refusing link without configured-library or journal ownership: %s", path)
		}
		rel, err := filepath.Rel(project, path)
		if err != nil {
			return plan, err
		}
		if strings.HasPrefix(rel, "..") {
			return plan, fmt.Errorf("managed path outside project: %s", path)
		}
		slashRel := filepath.ToSlash(rel)
		lines = append(lines, "/"+slashRel)
		plan.Changes = append(plan.Changes, operation.Change{Path: gitignore, Action: "ignore managed link", Detail: "/" + slashRel})
	}
	if existing, err := os.ReadFile(gitignore); err == nil {
		kept := plan.Changes[:0]
		for _, change := range plan.Changes {
			if !strings.Contains("\n"+string(existing)+"\n", "\n"+change.Detail+"\n") {
				kept = append(kept, change)
			}
		}
		plan.Changes = kept
		if len(plan.Changes) == 0 {
			return plan, nil
		}
	} else if !os.IsNotExist(err) {
		return plan, err
	}
	if confirm == nil || !confirm(plan) {
		return plan, operation.ErrNotConfirmed
	}
	if info, err := os.Lstat(gitignore); err == nil && (!info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0) {
		return plan, fmt.Errorf("refusing unsafe .gitignore path")
	} else if err != nil && !os.IsNotExist(err) {
		return plan, err
	}
	before, err := journal.Capture([]string{gitignore})
	if err != nil {
		return plan, err
	}
	existing, err := os.ReadFile(gitignore)
	if err != nil && !os.IsNotExist(err) {
		return plan, err
	}
	if len(existing) > 0 && existing[len(existing)-1] != '\n' {
		existing = append(existing, '\n')
	}
	for _, line := range lines {
		if !strings.Contains("\n"+string(existing)+"\n", "\n"+line+"\n") {
			existing = append(existing, []byte(line+"\n")...)
		}
	}
	stage, err := os.CreateTemp(filepath.Dir(gitignore), ".skill-manager-gitignore-")
	if err != nil {
		return plan, err
	}
	name := stage.Name()
	defer os.Remove(name)
	if _, err := stage.Write(existing); err != nil {
		_ = stage.Close()
		return plan, errors.Join(err, journal.Restore(before))
	}
	if err := stage.Close(); err != nil {
		return plan, errors.Join(err, journal.Restore(before))
	}
	if err := os.Rename(name, gitignore); err != nil {
		return plan, errors.Join(err, journal.Restore(before))
	}
	after, err := journal.Capture([]string{gitignore})
	if err != nil {
		return plan, errors.Join(err, journal.Restore(before))
	}
	if err := journal.RecordPlan(plan, before, after); err != nil {
		if errors.Is(err, operation.ErrJournalCommitted) {
			return plan, err
		}
		return plan, errors.Join(err, journal.Restore(before))
	}
	return plan, nil
}

// Reconcile repoints orphaned supported-agent links to eligible configured library skills.
func Reconcile(library, project string, journal *operation.Journal, confirm func(operation.Plan) bool) (operation.Plan, error) {
	return reconcileWithHandler(library, project, journal, confirm, resource.NewSkillHandler())
}

func reconcileWithHandler(library, project string, journal *operation.Journal, confirm func(operation.Plan) bool, handler resource.ResourceHandler) (operation.Plan, error) {
	if journal == nil {
		return operation.Plan{}, errors.New("operation journal is not configured")
	}
	library, err := filepath.Abs(library)
	if err != nil {
		return operation.Plan{}, err
	}
	project, err = filepath.Abs(project)
	if err != nil {
		return operation.Plan{}, err
	}
	plan := operation.NewPlan("reconcile")
	var paths, targets, originals []string
	var placementTargets []adapter.Target
	var requests []resource.SkillLifecycleRequest
	for _, a := range adapter.Supported() {
		inspections, err := a.Inspect(adapter.InspectionRequest{Project: project, Kind: resource.Skill})
		if err != nil {
			return plan, err
		}
		for _, inspection := range inspections {
			path := inspection.Path
			recorded, managed, err := journal.RecordedLinkTarget(path)
			if err != nil {
				return plan, err
			}
			baseRequest := resource.SkillLifecycleRequest{
				Action:        resource.LifecycleReconcile,
				LibraryPath:   library,
				ProjectPath:   project,
				Identifier:    inspection.Identifier,
				JournalSource: recorded,
				JournalOwned:  managed,
			}
			base, err := planSkillLifecycle(handler, baseRequest)
			if err != nil {
				return plan, err
			}
			placement, err := a.PlanPlacement(adapter.PlacementRequest{Project: project, Resource: base.Resource})
			if err != nil {
				return plan, err
			}
			if placement.Identifier != inspection.Identifier {
				return plan, fmt.Errorf("adapter placement identifier mismatch for %s", path)
			}
			if placement.Destination != path {
				return plan, fmt.Errorf("adapter placement destination mismatch for %s", path)
			}
			request := baseRequest
			request.PlacementPath = placement.Destination
			planned, err := planSkillLifecycle(handler, request)
			if err != nil {
				return plan, err
			}
			if !planned.Applicable {
				continue
			}
			appendSkillLifecyclePlan(&plan, planned)
			paths = append(paths, path)
			targets = append(targets, planned.SourcePath)
			originals = append(originals, planned.CurrentSource)
			placementTargets = append(placementTargets, a.Target())
			requests = append(requests, request)
		}
	}
	if len(paths) == 0 {
		return plan, nil
	}
	if confirm == nil || !confirm(plan) {
		return plan, operation.ErrNotConfirmed
	}
	// Preflight every exact source and construct all replacements before the
	// first publication. A later concurrent edit therefore leaves every link
	// unchanged.
	for i := range paths {
		if err := adapter.ValidateProjectPlacement(placementTargets[i], project, paths[i], requests[i].Identifier); err != nil {
			return plan, err
		}
		replanned, err := planSkillLifecycle(handler, requests[i])
		if err != nil {
			return plan, err
		}
		if !replanned.Applicable || replanned.CurrentSource != originals[i] || replanned.SourcePath != targets[i] {
			return plan, operation.ErrUnexpectedState
		}
	}
	stages := make([]string, len(paths))
	for i, path := range paths {
		stage, err := os.CreateTemp(filepath.Dir(path), ".skill-manager-reconcile-")
		if err != nil {
			for _, p := range stages {
				if p != "" {
					_ = os.Remove(p)
				}
			}
			return plan, err
		}
		if err := stage.Close(); err != nil {
			return plan, err
		}
		if err := os.Remove(stage.Name()); err != nil {
			return plan, err
		}
		if err := os.Symlink(targets[i], stage.Name()); err != nil {
			return plan, err
		}
		stages[i] = stage.Name()
	}
	defer func() {
		for _, stage := range stages {
			if stage != "" {
				_ = os.Remove(stage)
			}
		}
	}()
	before, err := journal.Capture(paths)
	if err != nil {
		return plan, err
	}
	published := []int{}
	rollback := func() error {
		snapshots := []operation.Snapshot{}
		for _, i := range published {
			snapshots = append(snapshots, before[i])
		}
		return journal.Restore(snapshots)
	}
	for i, path := range paths {
		if err := adapter.ValidateProjectPlacement(placementTargets[i], project, path, requests[i].Identifier); err != nil {
			return plan, errors.Join(err, rollback())
		}
		old, err := os.Readlink(path)
		if err != nil || old != originals[i] {
			return plan, errors.Join(operation.ErrUnexpectedState, rollback())
		}
		if BeforeReconcilePublish != nil {
			if err := BeforeReconcilePublish(path); err != nil {
				return plan, errors.Join(err, rollback())
			}
		}
		// Detect a replacement that raced after the last readlink, before the
		// staged rename can overwrite it.
		latest, err := os.Readlink(path)
		if err != nil || latest != originals[i] {
			return plan, errors.Join(operation.ErrUnexpectedState, rollback())
		}
		if err := adapter.PublishStagedLink(project, path, stages[i], originals[i]); err != nil {
			return plan, errors.Join(err, rollback())
		}
		stages[i] = ""
		published = append(published, i)
	}
	after, err := journal.Capture(paths)
	if err != nil {
		return plan, errors.Join(err, journal.Restore(before))
	}
	if err := journal.RecordPlan(plan, before, after); err != nil {
		if errors.Is(err, operation.ErrJournalCommitted) {
			return plan, err
		}
		return plan, errors.Join(err, journal.Restore(before))
	}
	return plan, nil
}

// DeleteLibrarySkill deletes one eligible library directory only after force and confirmation.
func DeleteLibrarySkill(library, identifier string, force bool, confirm func(operation.Plan) bool, journals ...*operation.Journal) (operation.Plan, error) {
	if !force {
		return operation.Plan{}, errors.New("library deletion requires force confirmation because it can orphan managed links; run doctor first")
	}
	library, err := filepath.Abs(library)
	if err != nil {
		return operation.Plan{}, err
	}
	skillHandler := resource.NewSkillHandler()
	managedResource, err := skillHandler.LibraryResource(library, identifier)
	if err != nil {
		return operation.Plan{}, err
	}
	path := managedResource.Provenance.Source
	ok, err := skillHandler.Eligible(identifier, path)
	if err != nil {
		return operation.Plan{}, err
	}
	if !ok {
		return operation.Plan{}, fmt.Errorf("refusing ineligible library skill %q", identifier)
	}
	plan := operation.NewPlan("delete library skill")
	plan.Changes = []operation.Change{{Path: path, Action: "delete library skill"}}
	plan.Warnings = []string{"deletion can orphan managed links; run doctor before and after deletion"}
	if confirm == nil || !confirm(plan) {
		return plan, operation.ErrNotConfirmed
	}
	// Revalidate after confirmation: an attacker or concurrent process may have
	// replaced the directory with an unmanaged path while the plan was visible.
	ok, err = skillHandler.Eligible(identifier, path)
	if err != nil {
		return plan, err
	}
	if !ok {
		return plan, operation.ErrUnexpectedState
	}
	journal := operation.New(filepath.Join(filepath.Dir(library), ".skill-manager", "journal.json"))
	if len(journals) > 0 && journals[0] != nil {
		journal = journals[0]
	}
	before, err := journal.Capture([]string{path})
	if err != nil {
		return plan, err
	}
	stage, err := os.MkdirTemp(filepath.Dir(path), ".skill-manager-delete-")
	if err != nil {
		return plan, err
	}
	defer os.RemoveAll(stage)
	moved := filepath.Join(stage, identifier)
	if err := os.Rename(path, moved); err != nil {
		return plan, err
	}
	after, err := journal.Capture([]string{path})
	if err != nil {
		return plan, errors.Join(err, journal.Restore(before))
	}
	if err := journal.RecordPlan(plan, before, after); err != nil {
		if errors.Is(err, operation.ErrJournalCommitted) {
			return plan, err
		}
		return plan, errors.Join(err, journal.Restore(before))
	}
	if err := os.RemoveAll(moved); err != nil {
		return plan, err
	}
	return plan, nil
}

func planSkillLifecycle(handler resource.ResourceHandler, request resource.SkillLifecycleRequest) (resource.SkillLifecyclePlan, error) {
	planned, err := handler.PlanLifecycle(request)
	if err != nil {
		return resource.SkillLifecyclePlan{}, err
	}
	plan, ok := planned.(resource.SkillLifecyclePlan)
	if !ok {
		return resource.SkillLifecyclePlan{}, fmt.Errorf("resource handler returned %T for Skill lifecycle request", planned)
	}
	return plan, nil
}

func appendSkillLifecyclePlan(destination *operation.Plan, source resource.SkillLifecyclePlan) {
	for _, change := range source.Changes {
		destination.Changes = append(destination.Changes, operation.Change{Path: change.Path, Action: change.Action, Detail: change.Detail})
	}
	destination.Warnings = append(destination.Warnings, source.Warnings...)
}

func eligibleSkill(handler resource.SkillHandler, path, identifier string) (bool, error) {
	ok, err := handler.Eligible(identifier, path)
	if err != nil && os.IsNotExist(err) {
		return false, nil
	}
	return ok, err
}
