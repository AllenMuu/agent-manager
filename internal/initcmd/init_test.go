package initcmd_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AllenMuu/skill-manager/internal/initcmd"
	"github.com/AllenMuu/skill-manager/internal/operation"
)

func TestInitializeRequiresConfirmationThenInstallsOnlyOperatorSkill(t *testing.T) {
	home := t.TempDir()
	svc := initcmd.New(home, filepath.Join(home, "journal.json"), func(operation.Plan) bool { return false }, func() error { return nil })
	if _, err := svc.Initialize(); !errors.Is(err, operation.ErrNotConfirmed) {
		t.Fatalf("Initialize() = %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".codex")); !os.IsNotExist(err) {
		t.Fatalf("global state changed before confirmation: %v", err)
	}
	svc = initcmd.New(home, filepath.Join(home, "journal.json"), func(operation.Plan) bool { return true }, func() error { return nil })
	if _, err := svc.Initialize(); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{filepath.Join(home, ".codex", "skills", "skill-manager-operator", "SKILL.md"), filepath.Join(home, ".claude", "skills", "skill-manager-operator", "SKILL.md")} {
		contents, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, phrase := range []string{"search", "recommendation", "confirmation", "CLI"} {
			if !strings.Contains(string(contents), phrase) {
				t.Errorf("%s lacks %q", path, phrase)
			}
		}
		if !strings.Contains(string(contents), "agent-manager") {
			t.Errorf("%s lacks primary agent-manager guidance", path)
		}
		if strings.Contains(string(contents), "skill-manager search") {
			t.Errorf("%s still instructs the deprecated skill-manager command", path)
		}
	}
	for _, dir := range []string{".codex", ".claude"} {
		if _, err := os.Stat(filepath.Join(home, dir, "skills", "skill-manager-operator", ".skill-manager-owner")); err != nil {
			t.Fatalf("compatibility ownership marker missing for %s: %v", dir, err)
		}
	}
}

func TestInitializeVerifiesCLIAvailability(t *testing.T) {
	svc := initcmd.New(t.TempDir(), filepath.Join(t.TempDir(), "journal.json"), func(operation.Plan) bool { return true }, func() error { return errors.New("not available") })
	if _, err := svc.Initialize(); err == nil || !strings.Contains(err.Error(), "not available") {
		t.Fatalf("Initialize() = %v", err)
	}
}

func TestInitializeSecondTargetFailureRollsBack(t *testing.T) {
	home := t.TempDir()
	svc := initcmd.New(home, filepath.Join(home, "j.json"), func(operation.Plan) bool { return true }, func() error { return nil })
	calls := 0
	svc.BeforePublish = func(string) error {
		calls++
		if calls == 2 {
			return errors.New("second")
		}
		return nil
	}
	if _, err := svc.Initialize(); err == nil {
		t.Fatal("expected failure")
	}
	for _, dir := range []string{".codex", ".claude"} {
		if _, err := os.Stat(filepath.Join(home, dir, "skills", "skill-manager-operator", "SKILL.md")); !os.IsNotExist(err) {
			t.Fatalf("%s was published: %v", dir, err)
		}
		if _, err := os.Stat(filepath.Join(home, dir)); !os.IsNotExist(err) {
			t.Fatalf("%s parent remains: %v", dir, err)
		}
	}
}

func TestInitializeRejectsSymlinkOperatorSkill(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, ".codex", "skills", "skill-manager-operator", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(home, "other"), path); err != nil {
		t.Fatal(err)
	}
	svc := initcmd.New(home, filepath.Join(home, "j.json"), func(operation.Plan) bool { return true }, func() error { return nil })
	if _, err := svc.Initialize(); err == nil {
		t.Fatal("symlink accepted")
	}
}

func TestInitializeUpgradesMarkerOwnedOlderOperator(t *testing.T) {
	home := t.TempDir()
	svc := initcmd.New(home, filepath.Join(home, "j.json"), func(operation.Plan) bool { return true }, func() error { return nil })
	if _, err := svc.Initialize(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, ".codex", "skills", "skill-manager-operator", "SKILL.md")
	if err := os.WriteFile(path, []byte("older owned content"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Initialize(); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(path)
	if err != nil || strings.Contains(string(contents), "older") {
		t.Fatalf("not upgraded: %q %v", contents, err)
	}
}
