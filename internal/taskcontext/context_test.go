package taskcontext_test

import (
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
