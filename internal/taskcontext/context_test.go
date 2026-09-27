package taskcontext_test

import (
	"os"
	"path/filepath"
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
