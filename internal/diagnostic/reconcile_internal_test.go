package diagnostic

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/AllenMuu/skill-manager/internal/operation"
	"github.com/AllenMuu/skill-manager/internal/resource"
)

type recordingReconcileHandler struct {
	resource.SkillHandler
	actions []resource.LifecycleAction
}

func (h *recordingReconcileHandler) PlanLifecycle(request resource.LifecycleRequest) (resource.LifecyclePlan, error) {
	planned, err := h.SkillHandler.PlanLifecycle(request)
	if err == nil {
		h.actions = append(h.actions, planned.(resource.SkillLifecyclePlan).Action)
	}
	return planned, err
}

func TestReconcileCoordinatesThroughResourceHandlerPlan(t *testing.T) {
	root := t.TempDir()
	library := filepath.Join(root, "library")
	project := filepath.Join(root, "project")
	writeDiagnosticSkill(t, filepath.Join(library, "demo"))
	old := filepath.Join(root, "old-library", "demo")
	writeDiagnosticSkill(t, old)
	link := filepath.Join(project, ".codex", "skills", "demo")
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(old, link); err != nil {
		t.Fatal(err)
	}
	journal := operation.New(filepath.Join(root, "journal.json"))
	after, err := journal.Capture([]string{link})
	if err != nil {
		t.Fatal(err)
	}
	if err := journal.Record("activate", nil, after); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Dir(old)); err != nil {
		t.Fatal(err)
	}
	handler := &recordingReconcileHandler{}

	if _, err := reconcileWithHandler(library, project, journal, func(operation.Plan) bool { return true }, handler); err != nil {
		t.Fatal(err)
	}
	if len(handler.actions) == 0 {
		t.Fatal("reconcile bypassed ResourceHandler.PlanLifecycle")
	}
	for _, action := range handler.actions {
		if action != resource.LifecycleReconcile {
			t.Fatalf("handler actions = %v, want reconcile only", handler.actions)
		}
	}
}

func writeDiagnosticSkill(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "SKILL.md"), []byte("---\nname: demo\ndescription: demo\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}
