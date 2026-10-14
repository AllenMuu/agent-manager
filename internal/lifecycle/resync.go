package lifecycle

import (
	"fmt"
	"path/filepath"
	"reflect"
	"sort"

	"github.com/AllenMuu/skill-manager/internal/adapter"
	"github.com/AllenMuu/skill-manager/internal/catalog"
	"github.com/AllenMuu/skill-manager/internal/operation"
	"github.com/AllenMuu/skill-manager/internal/resource"
)

type ResyncScope string

const (
	ResyncProject   ResyncScope = "project"
	ResyncGlobal    ResyncScope = "global"
	ResyncOperation             = "resynchronize managed Skills"
)

// ResyncPlan binds a reviewed set of journal-owned activations to their exact
// repository versions, destinations, and current materialization strategy.
type ResyncPlan struct {
	Scope   ResyncScope
	Root    string
	Plan    operation.Plan
	Skills  []catalog.Skill
	Targets []adapter.Target
	Options Options
}

// PreviewResync returns a plan for journal-verified activations in one scope.
// Unmanaged, changed, and orphaned destinations are never included.
func (s *Service) PreviewResync(scope ResyncScope, root string) (ResyncPlan, error) {
	if scope != ResyncProject && scope != ResyncGlobal {
		return ResyncPlan{}, fmt.Errorf("unsupported Skill resync scope %q", scope)
	}
	absoluteRoot, err := filepath.Abs(root)
	if err != nil {
		return ResyncPlan{}, err
	}
	var items []Item
	if scope == ResyncGlobal {
		items, err = s.ListGlobal(absoluteRoot)
	} else {
		items, err = s.List(absoluteRoot)
	}
	if err != nil {
		return ResyncPlan{}, err
	}
	plan := ResyncPlan{Scope: scope, Root: absoluteRoot, Options: Options{
		Conflict: ConflictReplace, Force: true,
		ExpectedConflictFingerprints: map[string]string{}, ExpectedSourceFingerprints: map[string]string{},
		TargetSelections: map[string][]adapter.Target{},
		Rematerialize:    true,
	}}
	handler := resource.NewSkillHandler()
	skillIndices := map[string]int{}
	targetSet := map[adapter.Target]bool{}
	for _, item := range items {
		if item.Status != Managed {
			continue
		}
		source, err := filepath.Abs(item.SourcePath)
		if err != nil {
			return ResyncPlan{}, err
		}
		owned, err := handler.ManagesSource(s.LibraryPath, item.Identifier, source)
		if err != nil {
			return ResyncPlan{}, fmt.Errorf("managed Skill %s no longer resolves to the configured repository: %w", item.Identifier, err)
		}
		if !owned {
			return ResyncPlan{}, fmt.Errorf("managed Skill %s no longer resolves to the configured repository", item.Identifier)
		}
		versionKey := filepath.Base(source)
		if filepath.Base(filepath.Dir(source)) != "versions" {
			versionKey = ""
		}
		skill, err := handler.Resolve(s.LibraryPath, item.Identifier, versionKey)
		if err != nil {
			return ResyncPlan{}, fmt.Errorf("resolve repository version for %s: %w", item.Identifier, err)
		}
		canonical, err := filepath.Abs(skill.SourcePath)
		if err != nil || filepath.Clean(canonical) != filepath.Clean(source) {
			return ResyncPlan{}, fmt.Errorf("managed Skill %s source changed since activation", item.Identifier)
		}
		fingerprint, err := operation.FingerprintPath(source)
		if err != nil {
			return ResyncPlan{}, err
		}
		plan.Options.ExpectedSourceFingerprints[source] = fingerprint
		pathFingerprint, err := operation.FingerprintPath(item.Path)
		if err != nil {
			return ResyncPlan{}, err
		}
		plan.Options.ExpectedConflictFingerprints[item.Path] = pathFingerprint
		if _, exists := skillIndices[item.Identifier]; !exists {
			skillIndices[item.Identifier] = len(plan.Skills)
			plan.Skills = append(plan.Skills, skill)
		} else {
			prior := plan.Skills[skillIndices[item.Identifier]]
			if filepath.Clean(prior.SourcePath) != filepath.Clean(skill.SourcePath) {
				return ResyncPlan{}, fmt.Errorf("managed activations for %s refer to different repository versions", item.Identifier)
			}
		}
		if !targetSet[item.Target] {
			targetSet[item.Target] = true
			plan.Targets = append(plan.Targets, item.Target)
		}
		selected := plan.Options.TargetSelections[item.Identifier]
		alreadySelected := false
		for _, target := range selected {
			if target == item.Target {
				alreadySelected = true
				break
			}
		}
		if !alreadySelected {
			plan.Options.TargetSelections[item.Identifier] = append(selected, item.Target)
		}
	}
	sort.Slice(plan.Skills, func(i, j int) bool { return plan.Skills[i].Identifier < plan.Skills[j].Identifier })
	sort.Slice(plan.Targets, func(i, j int) bool { return plan.Targets[i] < plan.Targets[j] })
	if scope == ResyncGlobal {
		plan.Plan, err = s.PreviewGlobalMany(absoluteRoot, plan.Skills, plan.Targets, plan.Options)
	} else {
		plan.Plan, err = s.PreviewMany(absoluteRoot, plan.Skills, plan.Targets, plan.Options)
	}
	if err != nil {
		return ResyncPlan{}, err
	}
	return plan, nil
}

// ApplyResync repeats the managed inventory scan and exact preview before
// applying all activations in the selected scope as one journaled operation.
func (s *Service) ApplyResync(expected ResyncPlan, confirmed bool) (operation.Plan, error) {
	if !confirmed {
		return expected.Plan, ErrNotConfirmed
	}
	fresh, err := s.PreviewResync(expected.Scope, expected.Root)
	if err != nil {
		return expected.Plan, err
	}
	if !samePlan(expected.Plan, fresh.Plan) || !reflect.DeepEqual(expected.Skills, fresh.Skills) ||
		!reflect.DeepEqual(expected.Targets, fresh.Targets) || !reflect.DeepEqual(expected.Options, fresh.Options) {
		return fresh.Plan, ErrPlanChanged
	}
	if expected.Scope == ResyncGlobal {
		options := expected.Options
		options.JournalOperation = ResyncOperation
		return s.ApplyGlobalMany(expected.Root, expected.Skills, expected.Targets, expected.Plan, true, options)
	}
	options := expected.Options
	options.JournalOperation = ResyncOperation
	return s.ApplyMany(expected.Root, expected.Skills, expected.Targets, expected.Plan, true, options)
}
