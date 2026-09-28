package taskcontext_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/AllenMuu/skill-manager/internal/artifact"
	"github.com/AllenMuu/skill-manager/internal/memory"
	"github.com/AllenMuu/skill-manager/internal/role"
	"github.com/AllenMuu/skill-manager/internal/taskcontext"
)

type fakeProvider struct {
	status memory.ProviderStatus
	got    string
}

type searchProvider struct{ results []string }

func (p searchProvider) Status() memory.ProviderStatus {
	return memory.ProviderStatus{Available: true, Capabilities: []memory.Capability{memory.CapabilityRead, memory.CapabilitySearch}, Scopes: []memory.Scope{memory.ScopeProject}}
}
func (p searchProvider) Search(memory.Scope, string) ([]string, error) { return p.results, nil }

func TestContextBudgetBoundsSkillsAndMemoryDeterministically(t *testing.T) {
	project, library := t.TempDir(), t.TempDir()
	store, err := artifact.NewStore(project)
	if err != nil {
		t.Fatal(err)
	}
	intent := artifact.New(artifact.Intent, "task-1", project, time.Now())
	intent.Set("summary", "budget query")
	if _, err := store.Init("task-1", intent); err != nil {
		t.Fatal(err)
	}
	for _, skill := range []struct {
		name string
		body string
	}{{"large", strings.Repeat("x", 270000)}, {"small", "small body"}} {
		path := filepath.Join(library, skill.name)
		if err := os.Mkdir(path, 0o755); err != nil {
			t.Fatal(err)
		}
		data := "---\nname: " + skill.name + "\ndescription: test\n---\n" + skill.body
		if err := os.WriteFile(filepath.Join(path, "SKILL.md"), []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	results := []string{strings.Repeat("m", 33000)}
	for i := 0; i < 12; i++ {
		results = append(results, "memory item")
	}
	contract := role.Contract{ID: "custom", Role: "custom", Inputs: []artifact.Kind{artifact.Intent}, Outputs: []artifact.Kind{artifact.Plan}, Permissions: role.Permissions{Filesystem: role.Read, Shell: role.Denied, Network: role.Denied}}
	options := taskcontext.Options{Project: project, TaskID: "task-1", Library: library, Contract: contract, SelectedSkills: []string{"large", "small"}, Memory: searchProvider{results: results}}
	first, err := taskcontext.ResolveWithOptions(options)
	if err != nil {
		t.Fatal(err)
	}
	second, err := taskcontext.ResolveWithOptions(options)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatal("context budget output is nondeterministic")
	}
	if len(first.Skills) != 1 || first.Skills[0].Identifier != "small" || len(first.Memory) != 10 || len(first.Warnings) < 2 {
		t.Fatalf("bounded bundle = %#v", first)
	}
	for _, item := range first.Memory {
		if len(item.Content) > 32768 {
			t.Fatalf("oversized memory item: %d", len(item.Content))
		}
	}
	encoded, err := json.Marshal(first)
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded) > 1024*1024 {
		t.Fatalf("bundle exceeds total context limit: %d", len(encoded))
	}
}

func TestExplicitSkillSelectionPreservesAdvisoryMismatch(t *testing.T) {
	project := t.TempDir()
	store, err := artifact.NewStore(project)
	if err != nil {
		t.Fatal(err)
	}
	intent := artifact.New(artifact.Intent, "task-1", project, time.Now())
	intent.Set("summary", "Use selected Skill")
	if _, err := store.Init("task-1", intent); err != nil {
		t.Fatal(err)
	}
	library := t.TempDir()
	skillDir := filepath.Join(library, "helper")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: Helper\ndescription: Help with tasks\n---\nInstructions.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, ".skill-manager.yaml"), []byte("compatibility: [claude-code]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	contract := role.Contract{ID: "custom", Role: "custom", Inputs: []artifact.Kind{artifact.Intent}, Outputs: []artifact.Kind{artifact.Plan}, Permissions: role.Permissions{Filesystem: role.Read, Shell: role.Denied, Network: role.Denied}}
	withoutSelection, err := taskcontext.ResolveWithOptions(taskcontext.Options{Project: project, TaskID: "task-1", Library: library, Contract: contract, Agent: "codex"})
	if err != nil {
		t.Fatal(err)
	}
	if len(withoutSelection.Skills) != 0 {
		t.Fatalf("unselected Skills were included: %#v", withoutSelection.Skills)
	}
	bundle, err := taskcontext.ResolveWithOptions(taskcontext.Options{Project: project, TaskID: "task-1", Library: library, Contract: contract, Agent: "codex", SelectedSkills: []string{"helper"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(bundle.Skills) != 1 || len(bundle.Warnings) != 1 || !strings.Contains(bundle.Warnings[0], "helper") {
		t.Fatalf("explicit selection = %#v", bundle)
	}
}

func TestContextDeduplicatesSelectedSkillsBeforeApplyingBudget(t *testing.T) {
	project, library := t.TempDir(), t.TempDir()
	store, err := artifact.NewStore(project)
	if err != nil {
		t.Fatal(err)
	}
	intent := artifact.New(artifact.Intent, "task-1", ".", time.Now())
	intent.Set("summary", "deduplicate selected Skills")
	if _, err := store.Init("task-1", intent); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"first", "second"} {
		skillDir := filepath.Join(library, name)
		if err := os.Mkdir(skillDir, 0o755); err != nil {
			t.Fatal(err)
		}
		content := "---\nname: " + name + "\ndescription: test\n---\n" + strings.Repeat("x", 120000)
		if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	contract := role.Contract{ID: "custom", Role: "custom", Inputs: []artifact.Kind{artifact.Intent}, Outputs: []artifact.Kind{artifact.Plan}, Permissions: role.Permissions{Filesystem: role.Read, Shell: role.Denied, Network: role.Denied}}
	bundle, err := taskcontext.ResolveWithOptions(taskcontext.Options{
		Project: project, TaskID: "task-1", Library: library, Contract: contract,
		SelectedSkills: []string{"first", "first", "second"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(bundle.Skills) != 2 || bundle.Skills[0].Identifier != "first" || bundle.Skills[1].Identifier != "second" {
		t.Fatalf("selected Skills = %#v, want unique Skills in requested order", bundle.Skills)
	}
	if len(bundle.Warnings) != 0 {
		t.Fatalf("deduplicated Skills unexpectedly exceeded budget: %v", bundle.Warnings)
	}
}

func (p *fakeProvider) Status() memory.ProviderStatus { return p.status }
func (p *fakeProvider) Promote(_ memory.Scope, knowledge string) error {
	p.got = knowledge
	return nil
}

func TestPromoteLessonsRequiresExplicitConfirmation(t *testing.T) {
	doc := artifact.New(artifact.Lessons, "task-1", "/tmp/project", time.Now())
	doc.Set("items", []map[string]string{{"type": "lesson", "scope": "project", "content": "write tests", "confidence": "high"}})
	provider := &fakeProvider{}
	if err := taskcontext.PromoteLessons(doc, provider, memory.ScopeProject, func() bool { return false }); err == nil {
		t.Fatal("unconfirmed promotion succeeded")
	}
	if provider.got != "" {
		t.Fatal("provider was written before confirmation")
	}
	if err := taskcontext.PromoteLessons(doc, provider, memory.ScopeProject, func() bool { return true }); err != nil {
		t.Fatal(err)
	}
	if provider.got == "" || provider.got != "[lesson][project][confidence=high] write tests" {
		t.Fatalf("promoted knowledge = %q", provider.got)
	}
	_ = role.Planner
}
