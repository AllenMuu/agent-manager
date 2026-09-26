// Package taskcontext assembles the stable inputs a role needs without
// coupling artifact storage to a particular agent runtime or memory provider.
package taskcontext

import (
	"errors"
	"fmt"
	"strings"

	"github.com/AllenMuu/skill-manager/internal/artifact"
	"github.com/AllenMuu/skill-manager/internal/catalog"
	"github.com/AllenMuu/skill-manager/internal/memory"
	"github.com/AllenMuu/skill-manager/internal/role"
)

type Bundle struct {
	ProjectRoot string
	TaskID      string
	Role        role.ID
	Artifacts   map[artifact.Kind]artifact.Document
	Missing     []artifact.Kind
	Skills      []catalog.Skill
	MemoryState memory.ProviderStatus
}

// Resolve builds an adapter-ready task context from repository-local
// artifacts and compatible local Skills. It only reads files; it never invokes
// a runtime, provider, network, or Skill content.
func Resolve(project, taskID, library string, contract role.Contract) (Bundle, error) {
	return resolve(project, taskID, library, contract, "")
}

// ResolveForAgent applies the selected agent's compatibility declaration while
// preserving the same role/artifact assembly behavior as Resolve.
func ResolveForAgent(project, taskID, library string, contract role.Contract, agent string) (Bundle, error) {
	return resolve(project, taskID, library, contract, agent)
}

func resolve(project, taskID, library string, contract role.Contract, agent string) (Bundle, error) {
	store, err := artifact.NewStore(project)
	if err != nil {
		return Bundle{}, err
	}
	bundle := Bundle{ProjectRoot: store.ProjectRoot, TaskID: taskID, Role: contract.ID, Artifacts: map[artifact.Kind]artifact.Document{}, Missing: []artifact.Kind{}, Skills: []catalog.Skill{}}
	for _, kind := range contract.Inputs {
		doc, _, loadErr := store.Load(taskID, kind)
		if loadErr != nil {
			if strings.Contains(loadErr.Error(), "no such file or directory") {
				bundle.Missing = append(bundle.Missing, kind)
				continue
			}
			return Bundle{}, loadErr
		}
		bundle.Artifacts[kind] = doc
	}
	if library != "" {
		skills, _, discoverErr := catalog.Discover(library)
		if discoverErr != nil {
			return Bundle{}, fmt.Errorf("discover compatible Skills: %w", discoverErr)
		}
		for _, skill := range skills {
			if agent == "" || len(skill.Compatibility) == 0 || contains(skill.Compatibility, agent) || contains(skill.Compatibility, "all") {
				bundle.Skills = append(bundle.Skills, skill)
			}
		}
	}
	return bundle, nil
}

// PromoteLessons is the explicit bridge from a lessons artifact to shared
// Memory. The caller must supply confirmation; no task resolution path calls
// this function implicitly.
func PromoteLessons(doc artifact.Document, provider memory.Provider, scope memory.Scope, confirm func() bool) error {
	if provider == nil {
		return errors.New("Memory provider is required")
	}
	items, err := doc.LessonItems()
	if err != nil {
		return err
	}
	if len(items) == 0 {
		return errors.New("lessons artifact contains no items")
	}
	if confirm == nil || !confirm() {
		return errors.New("lessons promotion was not confirmed")
	}
	var b strings.Builder
	for i, item := range items {
		if i > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "[%s][%s][confidence=%s] %s", item.Type, item.Scope, item.Confidence, item.Content)
	}
	return provider.Promote(scope, b.String())
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
