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

func TestInitializeRollbackPreservesUnpublishedConcurrentFile(t *testing.T) {
	home := t.TempDir()
	svc := initcmd.New(home, filepath.Join(home, "j.json"), func(operation.Plan) bool { return true }, func() error { return nil })
	var concurrentPath string
	calls := 0
	svc.BeforePublish = func(path string) error {
		calls++
		if calls == 2 {
			concurrentPath = path
			return os.WriteFile(path, []byte("concurrent user content"), 0o644)
		}
		return nil
	}
	if _, err := svc.Initialize(); err == nil {
		t.Fatal("expected concurrent unmanaged file to be refused")
	}
	contents, err := os.ReadFile(concurrentPath)
	if err != nil || string(contents) != "concurrent user content" {
		t.Fatalf("concurrent file lost during rollback: %q %v", contents, err)
	}
	if _, err := os.Stat(filepath.Join(home, "j.json")); !os.IsNotExist(err) {
		t.Fatalf("failed initialization recorded a journal: %v", err)
	}
}

func TestInitializeRollbackPreservesReplacedPublishedFile(t *testing.T) {
	home := t.TempDir()
	svc := initcmd.New(home, filepath.Join(home, "j.json"), func(operation.Plan) bool { return true }, func() error { return nil })
	var firstPath string
	svc.BeforePublish = func(path string) error {
		if firstPath == "" {
			firstPath = path
			return nil
		}
		if err := os.WriteFile(firstPath, []byte("concurrent replacement"), 0o600); err != nil {
			return err
		}
		return errors.New("later publication failed")
	}
	if _, err := svc.Initialize(); err == nil {
		t.Fatal("expected publication failure")
	}
	contents, err := os.ReadFile(firstPath)
	if err != nil || string(contents) != "concurrent replacement" {
		t.Fatalf("concurrent replacement lost during rollback: %q %v", contents, err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(firstPath), ".skill-manager-owner")); !os.IsNotExist(err) {
		t.Fatalf("operation's marker was not rolled back: %v", err)
	}
}

func TestInitializeRollbackPreservesIdenticalReplacementOwner(t *testing.T) {
	home := t.TempDir()
	svc := initcmd.New(home, filepath.Join(home, "j.json"), func(operation.Plan) bool { return true }, func() error { return nil })
	var firstPath string
	var replacement os.FileInfo
	svc.BeforePublish = func(path string) error {
		if firstPath == "" {
			firstPath = path
			return nil
		}
		contents, err := os.ReadFile(firstPath)
		if err != nil {
			return err
		}
		stage := firstPath + ".replacement"
		if err := os.WriteFile(stage, contents, 0o600); err != nil {
			return err
		}
		replacement, err = os.Lstat(stage)
		if err != nil {
			return err
		}
		if err := os.Rename(stage, firstPath); err != nil {
			return err
		}
		return errors.New("later publication failed")
	}
	if _, err := svc.Initialize(); !errors.Is(err, operation.ErrUnexpectedState) {
		t.Fatalf("Initialize()=%v", err)
	}
	info, err := os.Lstat(firstPath)
	if err != nil || !os.SameFile(replacement, info) {
		t.Fatalf("replacement owner lost: %v", err)
	}
}

func TestInitializeRollbackRestoresPreviouslyOwnedContent(t *testing.T) {
	home := t.TempDir()
	svc := initcmd.New(home, filepath.Join(home, "j.json"), func(operation.Plan) bool { return true }, func() error { return nil })
	if _, err := svc.Initialize(); err != nil {
		t.Fatal(err)
	}
	var firstPath string
	svc.BeforePublish = func(path string) error {
		if path == firstPath {
			return nil
		}
		return errors.New("later publication failed")
	}
	// Adapter iteration starts with Claude; keep an owned older version there.
	firstPath = filepath.Join(home, ".claude", "skills", "skill-manager-operator", "SKILL.md")
	if err := os.WriteFile(firstPath, []byte("older owned content"), 0o600); err != nil {
		t.Fatal(err)
	}
	journalBefore, err := os.ReadFile(filepath.Join(home, "j.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Initialize(); err == nil {
		t.Fatal("expected failure")
	}
	contents, err := os.ReadFile(firstPath)
	if err != nil || string(contents) != "older owned content" {
		t.Fatalf("old content lost: %q %v", contents, err)
	}
	journalAfter, err := os.ReadFile(filepath.Join(home, "j.json"))
	if err != nil || string(journalBefore) != string(journalAfter) {
		t.Fatalf("failed operation changed journal: %v", err)
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
