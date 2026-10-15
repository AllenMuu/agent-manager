package webconsole

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/AllenMuu/skill-manager/internal/operation"
)

func TestUpstreamJournalValidatesAndUndoesSupportedSkillPaths(t *testing.T) {
	home := t.TempDir()
	skillPath := filepath.Join(home, ".agents", "skills", "journal-demo")
	lockPath := filepath.Join(home, ".agents", ".skill-lock.json")
	if err := os.MkdirAll(skillPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillPath, "SKILL.md"), []byte("before"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lockPath, []byte("before lock"), 0o600); err != nil {
		t.Fatal(err)
	}

	server := &Server{homeDir: home}
	journal, err := server.newUpstreamJournal(nil)
	if err != nil {
		t.Fatal(err)
	}
	paths := []string{skillPath, lockPath}
	before, err := journal.Capture(paths)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillPath, "SKILL.md"), []byte("after"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lockPath, []byte("after lock"), 0o600); err != nil {
		t.Fatal(err)
	}
	after, err := journal.Capture(paths)
	if err != nil {
		t.Fatal(err)
	}
	plan := operation.NewPlan(upstreamUpdateOperation)
	if err := journal.RecordPlan(plan, before, after); err != nil {
		t.Fatal(err)
	}
	if available, err := journal.UndoAvailable(); err != nil || !available {
		t.Fatalf("UndoAvailable = %v, err=%v; want validated update entry", available, err)
	}
	if err := journal.UndoLatest(func(plan operation.Plan) bool { return plan.Operation == "undo "+upstreamUpdateOperation }); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(filepath.Join(skillPath, "SKILL.md"))
	if err != nil || string(contents) != "before" {
		t.Fatalf("Skill after validated Undo = %q, err=%v", contents, err)
	}
	contents, err = os.ReadFile(lockPath)
	if err != nil || string(contents) != "before lock" {
		t.Fatalf("lock after validated Undo = %q, err=%v", contents, err)
	}
}

func TestUpstreamJournalRejectsSymlinkedJournalDirectories(t *testing.T) {
	home := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(home, ".agent-manager")); err != nil {
		t.Skipf("symlinks are unavailable: %v", err)
	}
	if err := validateUpstreamJournalLayout(home); err == nil {
		t.Fatal("journal layout accepted a symlinked state directory")
	}
}
