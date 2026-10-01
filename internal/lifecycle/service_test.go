package lifecycle_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AllenMuu/skill-manager/internal/adapter"
	"github.com/AllenMuu/skill-manager/internal/catalog"
	"github.com/AllenMuu/skill-manager/internal/lifecycle"
	"github.com/AllenMuu/skill-manager/internal/operation"
	"github.com/AllenMuu/skill-manager/internal/resource"
)

type recordingSkillHandler struct {
	resource.SkillHandler
	actions []resource.LifecycleAction
}

func (h *recordingSkillHandler) PlanLifecycle(request resource.LifecycleRequest) (resource.LifecyclePlan, error) {
	plan, err := h.SkillHandler.PlanLifecycle(request)
	if err == nil {
		h.actions = append(h.actions, plan.(resource.SkillLifecyclePlan).Action)
	}
	return plan, err
}

func TestServiceCoordinatesActivationThroughResourceHandlerPlan(t *testing.T) {
	root, project, skill := fixture(t)
	handler := &recordingSkillHandler{}
	svc := lifecycle.NewWithResourceHandler(
		filepath.Join(root, "library"),
		operation.New(filepath.Join(root, "journal.json")),
		func(operation.Plan) bool { return true },
		handler,
	)
	if _, err := svc.Add(project, skill, []adapter.Target{adapter.Codex}); err != nil {
		t.Fatal(err)
	}
	if len(handler.actions) == 0 {
		t.Fatal("activation bypassed ResourceHandler.PlanLifecycle")
	}
	for _, action := range handler.actions {
		if action != resource.LifecycleActivate {
			t.Fatalf("handler actions = %v, want activation only", handler.actions)
		}
	}
}

func TestAddCreatesAbsoluteLinksForSelectedTargetsAndWarnsOnCompatibility(t *testing.T) {
	root, project, skill := fixture(t)
	var preview operation.Plan
	svc := lifecycle.New(filepath.Join(root, "library"), operation.New(filepath.Join(root, "journal.json")), func(p operation.Plan) bool { preview = p; return true })
	if _, err := svc.Add(project, skill, []adapter.Target{adapter.ClaudeCode, adapter.Codex}); err != nil {
		t.Fatal(err)
	}
	for _, target := range []adapter.Target{adapter.ClaudeCode, adapter.Codex} {
		a, _ := adapter.For(target)
		path := a.ProjectSkillPath(project, skill.Identifier)
		got, err := os.Readlink(path)
		if err != nil || got != skill.SourcePath || !filepath.IsAbs(got) {
			t.Fatalf("%s link = %q, %v", target, got, err)
		}
	}
	if len(preview.Warnings) != 1 {
		t.Fatalf("warnings = %#v, want one compatibility warning", preview.Warnings)
	}
}

func TestAddRequiresConfirmationAndRefusesUnmanagedDestination(t *testing.T) {
	root, project, skill := fixture(t)
	a, _ := adapter.For(adapter.Codex)
	path := a.ProjectSkillPath(project, skill.Identifier)
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	svc := lifecycle.New(filepath.Join(root, "library"), operation.New(filepath.Join(root, "journal.json")), func(operation.Plan) bool { return true })
	if _, err := svc.Add(project, skill, []adapter.Target{adapter.Codex}); !errors.Is(err, lifecycle.ErrUnsafePath) {
		t.Fatalf("Add error = %v, want unsafe path", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("unmanaged path was changed: %v", err)
	}

	otherRoot, otherProject, otherSkill := fixture(t)
	svc = lifecycle.New(filepath.Join(otherRoot, "library"), operation.New(filepath.Join(otherRoot, "journal.json")), func(operation.Plan) bool { return false })
	if _, err := svc.Add(otherProject, otherSkill, []adapter.Target{adapter.Codex}); !errors.Is(err, lifecycle.ErrNotConfirmed) {
		t.Fatalf("Add error = %v, want confirmation refusal", err)
	}
	path = a.ProjectSkillPath(otherProject, otherSkill.Identifier)
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("unconfirmed path changed: %v", err)
	}
}

func TestAddRejectsConflictChangedDuringConfirmation(t *testing.T) {
	root, project, skill := fixture(t)
	a, _ := adapter.For(adapter.Codex)
	destination := a.ProjectSkillPath(project, skill.Identifier)
	if err := os.MkdirAll(destination, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(destination, "original.txt"), []byte("original unmanaged"), 0o644); err != nil {
		t.Fatal(err)
	}
	journal := operation.New(filepath.Join(root, "journal.json"))
	svc := lifecycle.New(filepath.Join(root, "library"), journal, func(operation.Plan) bool {
		if err := os.RemoveAll(destination); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(destination, []byte("new owner"), 0o644); err != nil {
			t.Fatal(err)
		}
		return true
	})
	if _, err := svc.Add(project, skill, []adapter.Target{adapter.Codex}, lifecycle.Options{Conflict: lifecycle.ConflictReplace, Force: true}); !errors.Is(err, lifecycle.ErrUnsafePath) {
		t.Fatalf("Add() error = %v, want unsafe path", err)
	}
	if got, readErr := os.ReadFile(destination); readErr != nil || string(got) != "new owner" {
		t.Fatalf("new owner changed: %q, %v", got, readErr)
	}
	if _, ok, journalErr := journal.Latest(); journalErr != nil || ok {
		t.Fatalf("Add() journal state = ok=%v err=%v, want no entry", ok, journalErr)
	}
	assertCapturedOriginal(t, journal, "original.txt", "original unmanaged")
}

func TestAddPreservesLateOwnerAndStagedOriginalWhenDiscardFails(t *testing.T) {
	root, project, skill := fixture(t)
	a, _ := adapter.For(adapter.Codex)
	destination := a.ProjectSkillPath(project, skill.Identifier)
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destination, []byte("original owner"), 0o644); err != nil {
		t.Fatal(err)
	}
	journal := operation.New(filepath.Join(root, "journal.json"))
	svc := lifecycle.New(filepath.Join(root, "library"), journal, func(operation.Plan) bool { return true })
	svc.BeforeDiscard = func() error {
		if err := os.Remove(destination); err != nil {
			return err
		}
		if err := os.WriteFile(destination, []byte("late owner"), 0o644); err != nil {
			return err
		}
		return errors.New("discard failure")
	}
	if _, err := svc.Add(project, skill, []adapter.Target{adapter.Codex}, lifecycle.Options{Conflict: lifecycle.ConflictReplace, Force: true}); err == nil {
		t.Fatal("Add unexpectedly succeeded")
	}
	if got, err := os.ReadFile(destination); err != nil || string(got) != "late owner" {
		t.Fatalf("late owner = %q, %v", got, err)
	}
	recovery, err := filepath.Glob(destination + ".skill-manager-recovery-*")
	if err != nil || len(recovery) != 1 {
		t.Fatalf("recovery paths = %#v, %v", recovery, err)
	}
	if got, err := os.ReadFile(recovery[0]); err != nil || string(got) != "original owner" {
		t.Fatalf("staged original = %q, %v", got, err)
	}
	if _, ok, err := journal.Latest(); err != nil || ok {
		t.Fatalf("failed operation journal latest = ok=%v err=%v", ok, err)
	}
}

func TestAddManyRejectsConflictChangedDuringConfirmation(t *testing.T) {
	root, project, one := fixture(t)
	two := writeSkill(t, filepath.Join(root, "library", "two"), "two")
	a, _ := adapter.For(adapter.Codex)
	destination := a.ProjectSkillPath(project, one.Identifier)
	if err := os.MkdirAll(destination, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(destination, "original.txt"), []byte("original unmanaged"), 0o644); err != nil {
		t.Fatal(err)
	}
	journal := operation.New(filepath.Join(root, "journal.json"))
	svc := lifecycle.New(filepath.Join(root, "library"), journal, func(operation.Plan) bool {
		if err := os.RemoveAll(destination); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(destination, []byte("new owner"), 0o644); err != nil {
			t.Fatal(err)
		}
		return true
	})
	if _, err := svc.AddMany(project, []catalog.Skill{one, two}, []adapter.Target{adapter.Codex}, lifecycle.Options{Conflict: lifecycle.ConflictReplace, Force: true}); !errors.Is(err, lifecycle.ErrUnsafePath) {
		t.Fatalf("AddMany() error = %v, want unsafe path", err)
	}
	if got, readErr := os.ReadFile(destination); readErr != nil || string(got) != "new owner" {
		t.Fatalf("new owner changed: %q, %v", got, readErr)
	}
	if _, err := os.Lstat(a.ProjectSkillPath(project, two.Identifier)); !os.IsNotExist(err) {
		t.Fatalf("second destination after rejected AddMany() = %v, want absent", err)
	}
	if _, ok, journalErr := journal.Latest(); journalErr != nil || ok {
		t.Fatalf("AddMany() journal state = ok=%v err=%v, want no entry", ok, journalErr)
	}
	assertCapturedOriginal(t, journal, "original.txt", "original unmanaged")
}

func TestAddManyRejectsSourceEditedDuringConfirmation(t *testing.T) {
	root, project, skill := fixture(t)
	script := filepath.Join(skill.SourcePath, "script.sh")
	if err := os.WriteFile(script, []byte("reviewed"), 0o644); err != nil {
		t.Fatal(err)
	}
	digest, err := operation.FingerprintPath(skill.SourcePath)
	if err != nil {
		t.Fatal(err)
	}
	journal := operation.New(filepath.Join(root, "journal.json"))
	svc := lifecycle.New(filepath.Join(root, "library"), journal, func(operation.Plan) bool {
		if err := os.WriteFile(script, []byte("changed"), 0o644); err != nil {
			t.Fatal(err)
		}
		return true
	})
	_, err = svc.AddMany(project, []catalog.Skill{skill}, []adapter.Target{adapter.Codex}, lifecycle.Options{ExpectedSourceFingerprints: map[string]string{skill.SourcePath: digest}})
	if !errors.Is(err, lifecycle.ErrUnsafePath) {
		t.Fatalf("AddMany error = %v", err)
	}
	a, _ := adapter.For(adapter.Codex)
	if _, err := os.Lstat(a.ProjectSkillPath(project, skill.Identifier)); !os.IsNotExist(err) {
		t.Fatalf("destination changed: %v", err)
	}
	if _, ok, err := journal.Latest(); err != nil || ok {
		t.Fatalf("journal changed: %v %v", ok, err)
	}
}

func TestAddManyDeclineLeavesNoPartialLinks(t *testing.T) {
	root, project, one := fixture(t)
	two := writeSkill(t, filepath.Join(root, "library", "two"), "two")
	svc := lifecycle.New(filepath.Join(root, "library"), operation.New(filepath.Join(root, "j.json")), func(operation.Plan) bool { return false })
	if _, err := svc.AddMany(project, []catalog.Skill{one, two}, []adapter.Target{adapter.Codex}); !errors.Is(err, operation.ErrNotConfirmed) {
		t.Fatalf("err=%v", err)
	}
	for _, id := range []string{one.Identifier, two.Identifier} {
		if _, err := os.Lstat(filepath.Join(project, ".codex", "skills", id)); !os.IsNotExist(err) {
			t.Fatalf("%s created", id)
		}
	}
}

func TestAddManySecondPublicationFailureRollsBack(t *testing.T) {
	root, project, one := fixture(t)
	two := writeSkill(t, filepath.Join(root, "library", "two"), "two")
	journal := operation.New(filepath.Join(root, "j.json"))
	if err := journal.Record("baseline", nil, nil); err != nil {
		t.Fatal(err)
	}
	baseline, err := os.ReadFile(journal.Path)
	if err != nil {
		t.Fatal(err)
	}
	svc := lifecycle.New(filepath.Join(root, "library"), journal, func(operation.Plan) bool { return true })
	calls := 0
	svc.BeforePublish = func(string) error {
		calls++
		if calls == 2 {
			return errors.New("second")
		}
		return nil
	}
	if _, err := svc.AddMany(project, []catalog.Skill{one, two}, []adapter.Target{adapter.Codex}); err == nil {
		t.Fatal("expected failure")
	}
	for _, id := range []string{one.Identifier, two.Identifier} {
		if _, err := os.Lstat(filepath.Join(project, ".codex", "skills", id)); !os.IsNotExist(err) {
			t.Fatalf("%s remains", id)
		}
	}
	if got, readErr := os.ReadFile(journal.Path); readErr != nil || string(got) != string(baseline) {
		t.Fatalf("journal after interrupted publication = %q, %v; want unchanged %q", got, readErr, baseline)
	}
}

func TestAddManyRollbackRestoresReplacedPathWhenLaterPublicationFails(t *testing.T) {
	root, project, one := fixture(t)
	two := writeSkill(t, filepath.Join(root, "library", "two"), "two")
	a, _ := adapter.For(adapter.Codex)
	first := a.ProjectSkillPath(project, one.Identifier)
	if err := os.MkdirAll(first, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(first, "original"), []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	journal := operation.New(filepath.Join(root, "journal.json"))
	svc := lifecycle.New(filepath.Join(root, "library"), journal, func(operation.Plan) bool { return true })
	svc.BeforePublish = func(string) error { return errors.New("publication interrupted") }
	if _, err := svc.AddMany(project, []catalog.Skill{one, two}, []adapter.Target{adapter.Codex}, lifecycle.Options{Conflict: lifecycle.ConflictReplace, Force: true}); err == nil {
		t.Fatal("expected publication failure")
	}
	if got, readErr := os.ReadFile(filepath.Join(first, "original")); readErr != nil || string(got) != "original" {
		t.Fatalf("replaced path was not restored: %q, %v", got, readErr)
	}
	if info, err := os.Lstat(first); err != nil || !info.IsDir() {
		t.Fatalf("first path after rollback = %#v, %v", info, err)
	}
	if _, err := os.Lstat(a.ProjectSkillPath(project, two.Identifier)); !os.IsNotExist(err) {
		t.Fatalf("second path after rollback = %v, want absent", err)
	}
	if _, ok, err := journal.Latest(); err != nil || ok {
		t.Fatalf("failed batch journal state = ok=%v err=%v, want no entry", ok, err)
	}
}

func TestAddManyPreservesLateOwnerWhileRollingBackEarlierPublication(t *testing.T) {
	root, project, one := fixture(t)
	two := writeSkill(t, filepath.Join(root, "library", "two"), "two")
	a, _ := adapter.For(adapter.Codex)
	late := a.ProjectSkillPath(project, two.Identifier)
	journal := operation.New(filepath.Join(root, "journal.json"))
	svc := lifecycle.New(filepath.Join(root, "library"), journal, func(operation.Plan) bool { return true })
	called := 0
	svc.BeforePublish = func(string) error {
		called++
		if called == 1 {
			if err := os.WriteFile(late, []byte("late owner"), 0o644); err != nil {
				return err
			}
		}
		return nil
	}
	if _, err := svc.AddMany(project, []catalog.Skill{one, two}, []adapter.Target{adapter.Codex}); !errors.Is(err, lifecycle.ErrUnsafePath) {
		t.Fatalf("AddMany error = %v, want unsafe path", err)
	}
	if got, readErr := os.ReadFile(late); readErr != nil || string(got) != "late owner" {
		t.Fatalf("late owner changed: %q, %v", got, readErr)
	}
	if _, err := os.Lstat(a.ProjectSkillPath(project, one.Identifier)); !os.IsNotExist(err) {
		t.Fatalf("earlier publication was not rolled back: %v", err)
	}
	if _, ok, err := journal.Latest(); err != nil || ok {
		t.Fatalf("failed batch journal state = ok=%v err=%v, want no entry", ok, err)
	}
}

func TestAddDoesNotFollowSkillsParentSwappedBeforeFinalPublish(t *testing.T) {
	root, project, skill := fixture(t)
	a, _ := adapter.For(adapter.Codex)
	destination := a.ProjectSkillPath(project, skill.Identifier)
	parent := filepath.Dir(destination)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		t.Fatal(err)
	}
	recoverableParent := parent + "-recoverable"
	external := t.TempDir()
	sentinel := filepath.Join(external, "sentinel")
	if err := os.WriteFile(sentinel, []byte("external owner"), 0o644); err != nil {
		t.Fatal(err)
	}
	journal := operation.New(filepath.Join(root, "journal.json"))
	svc := lifecycle.New(filepath.Join(root, "library"), journal, func(operation.Plan) bool { return true })
	svc.BeforeFinalPublish = func() error {
		if err := os.Rename(parent, recoverableParent); err != nil {
			return err
		}
		return os.Symlink(external, parent)
	}

	if _, err := svc.Add(project, skill, []adapter.Target{adapter.Codex}); !errors.Is(err, lifecycle.ErrUnsafePath) {
		t.Fatalf("Add() error = %v, want unsafe path", err)
	}
	if got, err := os.ReadFile(sentinel); err != nil || string(got) != "external owner" {
		t.Fatalf("external sentinel changed: %q, %v", got, err)
	}
	if _, err := os.Lstat(filepath.Join(external, skill.Identifier)); !os.IsNotExist(err) {
		t.Fatalf("external destination was published: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(recoverableParent, skill.Identifier)); !os.IsNotExist(err) {
		t.Fatalf("managed link remained in recoverable parent: %v", err)
	}
	if _, ok, err := journal.Latest(); err != nil || ok {
		t.Fatalf("parent-swap journal state = ok=%v err=%v, want no entry", ok, err)
	}
}

func TestAddManyAggregatesCompatibilityWarningsBeforeConfirmation(t *testing.T) {
	root, project, one := fixture(t)
	two := writeSkill(t, filepath.Join(root, "library", "two"), "two")
	var plan operation.Plan
	svc := lifecycle.New(filepath.Join(root, "library"), operation.New(filepath.Join(root, "j.json")), func(p operation.Plan) bool { plan = p; return false })
	if _, err := svc.AddMany(project, []catalog.Skill{one, two}, []adapter.Target{adapter.Codex}); !errors.Is(err, operation.ErrNotConfirmed) {
		t.Fatal(err)
	}
	if len(plan.Warnings) != 2 {
		t.Fatalf("warnings=%#v", plan.Warnings)
	}
}

func TestPreviewAddManyDoesNotModifyFilesystemOrJournal(t *testing.T) {
	root, project, skill := fixture(t)
	journal := operation.New(filepath.Join(root, "journal.json"))
	before := filesystemState(t, root)
	svc := lifecycle.New(filepath.Join(root, "library"), journal, nil)

	plan, err := svc.PreviewAddMany(project, []catalog.Skill{skill}, []adapter.Target{adapter.Codex})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Changes) != 1 {
		t.Fatalf("preview changes = %#v, want one change", plan.Changes)
	}
	if after := filesystemState(t, root); after != before {
		t.Fatalf("activation preview changed filesystem\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

func TestPreviewRemoveDoesNotModifyFilesystemOrJournal(t *testing.T) {
	root, project, skill := fixture(t)
	journal := operation.New(filepath.Join(root, "journal.json"))
	if _, err := lifecycle.New(filepath.Join(root, "library"), journal, func(operation.Plan) bool { return true }).Add(project, skill, []adapter.Target{adapter.Codex}); err != nil {
		t.Fatal(err)
	}
	before := filesystemState(t, root)
	svc := lifecycle.New(filepath.Join(root, "library"), journal, nil)

	plan, err := svc.PreviewRemove(project, adapter.Codex, skill.Identifier)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Changes) != 1 {
		t.Fatalf("preview changes = %#v, want one change", plan.Changes)
	}
	if after := filesystemState(t, root); after != before {
		t.Fatalf("remove preview changed filesystem\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

func filesystemState(t *testing.T, root string) string {
	t.Helper()
	var state strings.Builder
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		fmt.Fprintf(&state, "%s|%s", relative, info.Mode())
		if info.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			fmt.Fprintf(&state, "|link=%s", target)
		} else if info.Mode().IsRegular() {
			contents, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			fmt.Fprintf(&state, "|bytes=%x", contents)
		}
		state.WriteByte('\n')
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return state.String()
}

func TestListRemoveAndUndoKeepLibrary(t *testing.T) {
	root, project, skill := fixture(t)
	journal := operation.New(filepath.Join(root, "journal.json"))
	svc := lifecycle.New(filepath.Join(root, "library"), journal, func(operation.Plan) bool { return true })
	if _, err := svc.Add(project, skill, []adapter.Target{adapter.Codex}); err != nil {
		t.Fatal(err)
	}
	a, _ := adapter.For(adapter.Codex)
	unmanaged := filepath.Join(filepath.Dir(a.ProjectSkillPath(project, skill.Identifier)), "local")
	if err := os.MkdirAll(unmanaged, 0o755); err != nil {
		t.Fatal(err)
	}
	items, err := svc.List(project)
	if err != nil {
		t.Fatal(err)
	}
	if !hasStatus(items, skill.Identifier, lifecycle.Managed) || !hasStatus(items, "local", lifecycle.Unmanaged) {
		t.Fatalf("inventory = %#v", items)
	}
	if _, err := svc.Remove(project, adapter.Codex, skill.Identifier); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(a.ProjectSkillPath(project, skill.Identifier)); !os.IsNotExist(err) {
		t.Fatalf("managed link was not removed: %v", err)
	}
	if _, err := os.Stat(skill.SourcePath); err != nil {
		t.Fatalf("library skill removed: %v", err)
	}
	if _, err := svc.Remove(project, adapter.Codex, "local"); !errors.Is(err, lifecycle.ErrUnsafePath) {
		t.Fatalf("remove unmanaged = %v", err)
	}
	if err := svc.Undo(); err != nil {
		t.Fatal(err)
	}
	if got, err := os.Readlink(a.ProjectSkillPath(project, skill.Identifier)); err != nil || got != skill.SourcePath {
		t.Fatalf("undo link = %q, %v", got, err)
	}
}

func TestListReportsOrphanedLibraryLink(t *testing.T) {
	root, project, skill := fixture(t)
	a, _ := adapter.For(adapter.Codex)
	path := a.ProjectSkillPath(project, skill.Identifier)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(skill.SourcePath, path); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(skill.SourcePath); err != nil {
		t.Fatal(err)
	}
	svc := lifecycle.New(filepath.Join(root, "library"), operation.New(filepath.Join(root, "journal.json")), func(operation.Plan) bool { return true })
	items, err := svc.List(project)
	if err != nil {
		t.Fatal(err)
	}
	if !hasStatus(items, skill.Identifier, lifecycle.Orphaned) {
		t.Fatalf("inventory = %#v", items)
	}
}

func TestListSkipsUnsupportedSymlinkSkillsLocation(t *testing.T) {
	root, project, _ := fixture(t)
	external := filepath.Join(root, "external-skills")
	if err := os.MkdirAll(filepath.Join(external, "local"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(project, ".foo"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, filepath.Join(project, ".foo", "skills")); err != nil {
		t.Fatal(err)
	}
	svc := lifecycle.New(filepath.Join(root, "library"), operation.New(filepath.Join(root, "journal.json")), func(operation.Plan) bool { return true })
	items, err := svc.List(project)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if item.Target == "foo" {
			t.Fatalf("unsupported symlink location was inventoried: %#v", item)
		}
	}
}

func TestAdoptRefusesConflictThenReplacesProjectDirectoryWithManagedLink(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "project")
	library := filepath.Join(root, "library")
	local := writeSkill(t, filepath.Join(project, ".codex", "skills", "demo"), "demo")
	writeSkill(t, filepath.Join(library, "demo"), "demo")
	svc := lifecycle.New(library, operation.New(filepath.Join(root, "journal.json")), func(operation.Plan) bool { return true })
	if _, err := svc.Adopt(project, adapter.Codex, "demo"); !errors.Is(err, lifecycle.ErrConflict) {
		t.Fatalf("adopt conflict = %v", err)
	}
	if _, err := os.Stat(local.SourcePath); err != nil {
		t.Fatalf("project skill changed on conflict: %v", err)
	}
	if err := os.RemoveAll(filepath.Join(library, "demo")); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Adopt(project, adapter.Codex, "demo"); err != nil {
		t.Fatal(err)
	}
	if got, err := os.Readlink(local.SourcePath); err != nil || got != filepath.Join(library, "demo") {
		t.Fatalf("adopted link = %q, %v", got, err)
	}
	assertNoAdoptionStages(t, filepath.Dir(local.SourcePath))
}

func TestForkMakesIndependentCopyWithoutChangingLibrary(t *testing.T) {
	root, project, skill := fixture(t)
	svc := lifecycle.New(filepath.Join(root, "library"), operation.New(filepath.Join(root, "journal.json")), func(operation.Plan) bool { return true })
	if _, err := svc.Add(project, skill, []adapter.Target{adapter.Codex}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Fork(project, adapter.Codex, skill.Identifier); err != nil {
		t.Fatal(err)
	}
	a, _ := adapter.For(adapter.Codex)
	path := a.ProjectSkillPath(project, skill.Identifier)
	if info, err := os.Lstat(path); err != nil || info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("forked path = %#v, %v", info, err)
	}
	if err := os.WriteFile(filepath.Join(path, "local.txt"), []byte("independent"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(skill.SourcePath, "local.txt")); !os.IsNotExist(err) {
		t.Fatalf("library changed by fork: %v", err)
	}
}

func TestExecutableSkillContentIsOpaqueDuringCopySnapshotRollbackAndUndo(t *testing.T) {
	root, project, skill := fixture(t)
	marker := filepath.Join(root, "executed")
	script := filepath.Join(skill.SourcePath, "run.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\ntouch "+marker+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	journal := operation.New(filepath.Join(root, "journal.json"))
	svc := lifecycle.New(filepath.Join(root, "library"), journal, func(operation.Plan) bool { return true })
	if _, err := svc.Add(project, skill, []adapter.Target{adapter.Codex}); err != nil {
		t.Fatal(err)
	}

	// An interrupted copy must roll back to the managed link and leave no
	// executable side effect or extra journal entry.
	svc.BeforePublish = func(string) error { return errors.New("interrupt") }
	if _, err := svc.Fork(project, adapter.Codex, skill.Identifier); err == nil {
		t.Fatal("interrupted fork unexpectedly succeeded")
	}
	a, _ := adapter.For(adapter.Codex)
	path := a.ProjectSkillPath(project, skill.Identifier)
	if info, err := os.Lstat(path); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("rollback path = %#v, %v; want managed link", info, err)
	}
	assertNoAdoptionStages(t, filepath.Dir(path))
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("executable ran during rollback; marker stat error = %v", err)
	}
	entry, ok, err := journal.Latest()
	if err != nil || !ok || entry.Operation != "activate" {
		t.Fatalf("journal after interrupted fork = %#v, ok=%v err=%v; want prior activation only", entry, ok, err)
	}

	// A successful copy and its journal snapshot preserve executable bytes and
	// mode, and undo restores the original link without invoking the file.
	svc.BeforePublish = nil
	if _, err := svc.Fork(project, adapter.Codex, skill.Identifier); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(path, "run.sh"))
	if err != nil || info.Mode().Perm() != 0o755 {
		t.Fatalf("copied executable metadata = %#v, %v", info, err)
	}
	contents, err := os.ReadFile(filepath.Join(path, "run.sh"))
	if err != nil || string(contents) != "#!/bin/sh\ntouch "+marker+"\n" {
		t.Fatalf("copied executable contents = %q, %v", contents, err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("executable ran during copy or snapshot; marker stat error = %v", err)
	}
	if err := svc.Undo(); err != nil {
		t.Fatal(err)
	}
	if target, err := os.Readlink(path); err != nil || target != skill.SourcePath {
		t.Fatalf("undo path = %q, %v; want managed link", target, err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("executable ran during undo; marker stat error = %v", err)
	}
}

func TestAddRejectsSkillOutsideConfiguredLibraryAndUnsafeIdentifier(t *testing.T) {
	root, project, skill := fixture(t)
	svc := lifecycle.New(filepath.Join(root, "library"), operation.New(filepath.Join(root, "journal.json")), func(operation.Plan) bool { return true })
	skill.SourcePath = filepath.Join(root, "elsewhere", "demo")
	if _, err := svc.Add(project, skill, []adapter.Target{adapter.Codex}); !errors.Is(err, lifecycle.ErrUnsafePath) {
		t.Fatalf("outside source = %v", err)
	}
	skill.SourcePath = filepath.Join(root, "library", "demo")
	skill.Identifier = "../escape"
	if _, err := svc.Add(project, skill, []adapter.Target{adapter.Codex}); !errors.Is(err, lifecycle.ErrUnsafePath) {
		t.Fatalf("unsafe identifier = %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "escape")); !os.IsNotExist(err) {
		t.Fatalf("unsafe identifier wrote outside target: %v", err)
	}
}

func TestManagedOperationsRejectLinkToDifferentLibraryEntry(t *testing.T) {
	root, project, skill := fixture(t)
	a, _ := adapter.For(adapter.Codex)
	path := a.ProjectSkillPath(project, skill.Identifier)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "library", "other"), path); err != nil {
		t.Fatal(err)
	}
	svc := lifecycle.New(filepath.Join(root, "library"), operation.New(filepath.Join(root, "journal.json")), func(operation.Plan) bool { return true })
	if _, err := svc.Remove(project, adapter.Codex, skill.Identifier); !errors.Is(err, lifecycle.ErrUnsafePath) {
		t.Fatalf("remove wrong target = %v", err)
	}
	if _, err := svc.Fork(project, adapter.Codex, skill.Identifier); !errors.Is(err, lifecycle.ErrUnsafePath) {
		t.Fatalf("fork wrong target = %v", err)
	}
}

func TestAddStagesAllTargetsBeforeWriting(t *testing.T) {
	root, project, skill := fixture(t)
	codex, _ := adapter.For(adapter.Codex)
	blocked := codex.ProjectSkillPath(project, skill.Identifier)
	if err := os.MkdirAll(blocked, 0o755); err != nil {
		t.Fatal(err)
	}
	svc := lifecycle.New(filepath.Join(root, "library"), operation.New(filepath.Join(root, "journal.json")), func(operation.Plan) bool { return true })
	if _, err := svc.Add(project, skill, []adapter.Target{adapter.ClaudeCode, adapter.Codex}); !errors.Is(err, lifecycle.ErrUnsafePath) {
		t.Fatalf("add = %v", err)
	}
	claude, _ := adapter.For(adapter.ClaudeCode)
	if _, err := os.Lstat(claude.ProjectSkillPath(project, skill.Identifier)); !os.IsNotExist(err) {
		t.Fatalf("earlier target changed after later failure: %v", err)
	}
}

func TestForkManyStagesAllTargetsBeforeReplacingLinks(t *testing.T) {
	root, project, skill := fixture(t)
	svc := lifecycle.New(filepath.Join(root, "library"), operation.New(filepath.Join(root, "journal.json")), func(operation.Plan) bool { return true })
	if _, err := svc.Add(project, skill, []adapter.Target{adapter.ClaudeCode, adapter.Codex}); err != nil {
		t.Fatal(err)
	}
	codex, _ := adapter.For(adapter.Codex)
	if err := os.Remove(codex.ProjectSkillPath(project, skill.Identifier)); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(codex.ProjectSkillPath(project, skill.Identifier), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ForkMany(project, skill.Identifier, []adapter.Target{adapter.ClaudeCode, adapter.Codex}); !errors.Is(err, lifecycle.ErrUnsafePath) {
		t.Fatalf("fork many = %v", err)
	}
	claude, _ := adapter.For(adapter.ClaudeCode)
	if info, err := os.Lstat(claude.ProjectSkillPath(project, skill.Identifier)); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("earlier link changed after later failure: %#v, %v", info, err)
	}
}

func TestUndoUsesServiceConfirmation(t *testing.T) {
	root, project, skill := fixture(t)
	confirmed := true
	svc := lifecycle.New(filepath.Join(root, "library"), operation.New(filepath.Join(root, "journal.json")), func(operation.Plan) bool { return confirmed })
	if _, err := svc.Add(project, skill, []adapter.Target{adapter.Codex}); err != nil {
		t.Fatal(err)
	}
	confirmed = false
	if err := svc.Undo(); !errors.Is(err, lifecycle.ErrNotConfirmed) {
		t.Fatalf("Undo = %v", err)
	}
}

func TestAddRevalidatesDestinationAfterConfirmation(t *testing.T) {
	root, project, skill := fixture(t)
	a, _ := adapter.For(adapter.Codex)
	destination := a.ProjectSkillPath(project, skill.Identifier)
	svc := lifecycle.New(filepath.Join(root, "library"), operation.New(filepath.Join(root, "journal.json")), func(operation.Plan) bool {
		if err := os.MkdirAll(destination, 0o755); err != nil {
			t.Fatal(err)
		}
		return true
	})
	if _, err := svc.Add(project, skill, []adapter.Target{adapter.Codex}); !errors.Is(err, lifecycle.ErrUnsafePath) {
		t.Fatalf("Add = %v", err)
	}
	if info, err := os.Stat(destination); err != nil || !info.IsDir() {
		t.Fatalf("new unmanaged destination changed: %#v, %v", info, err)
	}
}

func TestRemoveRevalidatesLinkAfterConfirmation(t *testing.T) {
	root, project, skill := fixture(t)
	a, _ := adapter.For(adapter.Codex)
	destination := a.ProjectSkillPath(project, skill.Identifier)
	svc := lifecycle.New(filepath.Join(root, "library"), operation.New(filepath.Join(root, "journal.json")), func(operation.Plan) bool { return true })
	if _, err := svc.Add(project, skill, []adapter.Target{adapter.Codex}); err != nil {
		t.Fatal(err)
	}
	svc = lifecycle.New(filepath.Join(root, "library"), operation.New(filepath.Join(root, "journal.json")), func(operation.Plan) bool {
		if err := os.Remove(destination); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(destination, []byte("owned"), 0o644); err != nil {
			t.Fatal(err)
		}
		return true
	})
	if _, err := svc.Remove(project, adapter.Codex, skill.Identifier); !errors.Is(err, lifecycle.ErrUnsafePath) {
		t.Fatalf("Remove = %v", err)
	}
	if data, err := os.ReadFile(destination); err != nil || string(data) != "owned" {
		t.Fatalf("replacement was changed: %q, %v", data, err)
	}
}

func TestRemoveRejectsProjectParentSwapBeforeAnchoredMutation(t *testing.T) {
	root, project, skill := fixture(t)
	svc := lifecycle.New(filepath.Join(root, "library"), operation.New(filepath.Join(root, "journal.json")), func(operation.Plan) bool { return true })
	if _, err := svc.Add(project, skill, []adapter.Target{adapter.Codex}); err != nil {
		t.Fatal(err)
	}
	a, _ := adapter.For(adapter.Codex)
	destination := a.ProjectSkillPath(project, skill.Identifier)
	external := filepath.Join(root, "external", "skills")
	if err := os.MkdirAll(external, 0o755); err != nil {
		t.Fatal(err)
	}
	externalTarget := filepath.Join(root, "external", "original")
	if err := os.MkdirAll(externalTarget, 0o755); err != nil {
		t.Fatal(err)
	}
	externalPath := filepath.Join(external, skill.Identifier)
	if err := os.Symlink(externalTarget, externalPath); err != nil {
		t.Fatal(err)
	}
	swapped := false
	svc.BeforePublish = func(step string) error {
		if step != "remove-before" || swapped {
			return nil
		}
		swapped = true
		codexRoot := filepath.Dir(filepath.Dir(destination))
		if err := os.Rename(codexRoot, codexRoot+".real"); err != nil {
			return err
		}
		return os.Symlink(filepath.Dir(external), codexRoot)
	}
	if _, err := svc.Remove(project, adapter.Codex, skill.Identifier); err == nil {
		t.Fatal("Remove unexpectedly succeeded after parent swap")
	}
	if got, err := os.Readlink(externalPath); err != nil || got != externalTarget {
		t.Fatalf("external owner changed: %q, %v", got, err)
	}
}

func TestAdoptRevalidatesLibraryConflictAfterConfirmation(t *testing.T) {
	root := t.TempDir()
	project, library := filepath.Join(root, "project"), filepath.Join(root, "library")
	local := writeSkill(t, filepath.Join(project, ".codex", "skills", "demo"), "demo")
	svc := lifecycle.New(library, operation.New(filepath.Join(root, "journal.json")), func(operation.Plan) bool {
		writeSkill(t, filepath.Join(library, "demo"), "demo")
		return true
	})
	if _, err := svc.Adopt(project, adapter.Codex, "demo"); !errors.Is(err, lifecycle.ErrConflict) {
		t.Fatalf("Adopt = %v", err)
	}
	if _, err := os.Stat(local.SourcePath); err != nil {
		t.Fatalf("project source changed: %v", err)
	}
}

func TestAddRequiresExistingEligibleConfiguredLibrarySkill(t *testing.T) {
	root, project, skill := fixture(t)
	svc := lifecycle.New(filepath.Join(root, "library"), operation.New(filepath.Join(root, "journal.json")), func(operation.Plan) bool { return true })
	if err := os.RemoveAll(skill.SourcePath); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Add(project, skill, []adapter.Target{adapter.Codex}); !errors.Is(err, lifecycle.ErrUnsafePath) || !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing source = %v", err)
	}
	if err := os.WriteFile(skill.SourcePath, []byte("file"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Add(project, skill, []adapter.Target{adapter.Codex}); !errors.Is(err, lifecycle.ErrUnsafePath) {
		t.Fatalf("file source = %v", err)
	}
	if err := os.Remove(skill.SourcePath); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(skill.SourcePath, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Add(project, skill, []adapter.Target{adapter.Codex}); !errors.Is(err, lifecycle.ErrUnsafePath) {
		t.Fatalf("invalid source = %v", err)
	}
}

func TestForkManyRejectsDuplicateTargets(t *testing.T) {
	root, project, skill := fixture(t)
	svc := lifecycle.New(filepath.Join(root, "library"), operation.New(filepath.Join(root, "journal.json")), func(operation.Plan) bool { return true })
	if _, err := svc.Add(project, skill, []adapter.Target{adapter.Codex}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ForkMany(project, skill.Identifier, []adapter.Target{adapter.Codex, adapter.Codex}); err == nil {
		t.Fatal("fork with a repeated target unexpectedly succeeded")
	}
	a, _ := adapter.For(adapter.Codex)
	if got, err := os.Readlink(a.ProjectSkillPath(project, skill.Identifier)); err != nil || got != skill.SourcePath {
		t.Fatalf("partial fork was not rolled back: %q, %v", got, err)
	}
}

func TestAddRejectsDuplicateTargetsBeforeMutation(t *testing.T) {
	root, project, skill := fixture(t)
	svc := lifecycle.New(filepath.Join(root, "library"), operation.New(filepath.Join(root, "journal.json")), func(operation.Plan) bool { return true })
	if _, err := svc.Add(project, skill, []adapter.Target{adapter.Codex, adapter.Codex}); !errors.Is(err, lifecycle.ErrUnsafePath) {
		t.Fatalf("Add = %v", err)
	}
	a, _ := adapter.For(adapter.Codex)
	if _, err := os.Lstat(a.ProjectSkillPath(project, skill.Identifier)); !os.IsNotExist(err) {
		t.Fatalf("duplicate activation mutated target: %v", err)
	}
}

func TestForkManyRestoresAllLinksWhenSecondPublicationFails(t *testing.T) {
	root, project, skill := fixture(t)
	svc := lifecycle.New(filepath.Join(root, "library"), operation.New(filepath.Join(root, "journal.json")), func(operation.Plan) bool { return true })
	if _, err := svc.Add(project, skill, []adapter.Target{adapter.ClaudeCode, adapter.Codex}); err != nil {
		t.Fatal(err)
	}
	calls := 0
	svc.BeforePublish = func(string) error {
		calls++
		if calls == 2 {
			return errors.New("publish failure")
		}
		return nil
	}
	if _, err := svc.ForkMany(project, skill.Identifier, []adapter.Target{adapter.ClaudeCode, adapter.Codex}); err == nil {
		t.Fatal("ForkMany unexpectedly succeeded")
	}
	for _, target := range []adapter.Target{adapter.ClaudeCode, adapter.Codex} {
		a, _ := adapter.For(target)
		if got, err := os.Readlink(a.ProjectSkillPath(project, skill.Identifier)); err != nil || got != skill.SourcePath {
			t.Fatalf("%s not restored: %q, %v", target, got, err)
		}
	}
}

func TestForkManyRejectsSecondProjectParentSwapWithoutExternalMutation(t *testing.T) {
	root, project, skill := fixture(t)
	svc := lifecycle.New(filepath.Join(root, "library"), operation.New(filepath.Join(root, "journal.json")), func(operation.Plan) bool { return true })
	if _, err := svc.Add(project, skill, []adapter.Target{adapter.ClaudeCode, adapter.Codex}); err != nil {
		t.Fatal(err)
	}
	external := filepath.Join(root, "external", "skills")
	if err := os.MkdirAll(external, 0o755); err != nil {
		t.Fatal(err)
	}
	externalTarget := filepath.Join(root, "external", "owner")
	if err := os.MkdirAll(externalTarget, 0o755); err != nil {
		t.Fatal(err)
	}
	externalPath := filepath.Join(external, skill.Identifier)
	if err := os.Symlink(externalTarget, externalPath); err != nil {
		t.Fatal(err)
	}
	calls := 0
	svc.BeforePublish = func(step string) error {
		if step != "fork-published" {
			return nil
		}
		calls++
		if calls != 1 {
			return nil
		}
		codexRoot := filepath.Join(project, ".codex")
		if err := os.Rename(codexRoot, codexRoot+".real"); err != nil {
			return err
		}
		return os.Symlink(filepath.Dir(external), codexRoot)
	}
	if _, err := svc.ForkMany(project, skill.Identifier, []adapter.Target{adapter.ClaudeCode, adapter.Codex}); err == nil {
		t.Fatal("ForkMany unexpectedly succeeded after parent swap")
	}
	if got, err := os.Readlink(externalPath); err != nil || got != externalTarget {
		t.Fatalf("external owner changed: %q, %v", got, err)
	}
	claude, _ := adapter.For(adapter.ClaudeCode)
	if got, err := os.Readlink(claude.ProjectSkillPath(project, skill.Identifier)); err != nil || got != skill.SourcePath {
		t.Fatalf("first fork target was not restored: %q, %v", got, err)
	}
}

func TestAdoptRefusesProjectEditAfterCopyIsStaged(t *testing.T) {
	root := t.TempDir()
	project, library := filepath.Join(root, "project"), filepath.Join(root, "library")
	local := writeSkill(t, filepath.Join(project, ".codex", "skills", "demo"), "demo")
	svc := lifecycle.New(library, operation.New(filepath.Join(root, "journal.json")), func(operation.Plan) bool { return true })
	svc.BeforePublish = func(step string) error {
		if step == "adopt-staged" {
			return os.WriteFile(filepath.Join(local.SourcePath, "edit"), []byte("user"), 0o644)
		}
		return nil
	}
	if _, err := svc.Adopt(project, adapter.Codex, "demo"); !errors.Is(err, lifecycle.ErrUnsafePath) {
		t.Fatalf("Adopt = %v", err)
	}
	if _, err := os.Lstat(local.SourcePath); err != nil {
		t.Fatalf("project skill was removed: %v", err)
	}
	if got, err := os.ReadFile(filepath.Join(local.SourcePath, "edit")); err != nil || string(got) != "user" {
		t.Fatalf("concurrent edit was not preserved: %q, %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(library, "demo")); !os.IsNotExist(err) {
		t.Fatalf("staged library copy was retained: %v", err)
	}
}

func TestAdoptRejectsProjectParentSwapBeforeLinkPublication(t *testing.T) {
	root := t.TempDir()
	project, library := filepath.Join(root, "project"), filepath.Join(root, "library")
	local := writeSkill(t, filepath.Join(project, ".codex", "skills", "demo"), "demo")
	external := filepath.Join(root, "external", "skills")
	if err := os.MkdirAll(external, 0o755); err != nil {
		t.Fatal(err)
	}
	externalTarget := filepath.Join(root, "external", "owner")
	if err := os.MkdirAll(externalTarget, 0o755); err != nil {
		t.Fatal(err)
	}
	externalPath := filepath.Join(external, "demo")
	if err := os.Symlink(externalTarget, externalPath); err != nil {
		t.Fatal(err)
	}
	svc := lifecycle.New(library, operation.New(filepath.Join(root, "journal.json")), func(operation.Plan) bool { return true })
	svc.BeforePublish = func(step string) error {
		if step != "adopt-staged" {
			return nil
		}
		codexRoot := filepath.Join(project, ".codex")
		if err := os.Rename(codexRoot, codexRoot+".real"); err != nil {
			return err
		}
		return os.Symlink(filepath.Dir(external), codexRoot)
	}
	if _, err := svc.Adopt(project, adapter.Codex, "demo"); err == nil {
		t.Fatal("Adopt unexpectedly succeeded after parent swap")
	}
	if got, err := os.Readlink(externalPath); err != nil || got != externalTarget {
		t.Fatalf("external owner changed: %q, %v", got, err)
	}
	if _, err := os.Stat(local.SourcePath); err != nil {
		t.Fatalf("original project tree missing: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(library, "demo")); !os.IsNotExist(err) {
		t.Fatalf("library copy remained after rejected adoption: %v", err)
	}
}

func TestAdoptPreservesEditCreatedAfterProjectTreeMovesAside(t *testing.T) {
	root := t.TempDir()
	project, library := filepath.Join(root, "project"), filepath.Join(root, "library")
	local := writeSkill(t, filepath.Join(project, ".codex", "skills", "demo"), "demo")
	svc := lifecycle.New(library, operation.New(filepath.Join(root, "journal.json")), func(operation.Plan) bool { return true })
	svc.BeforePublish = func(step string) error {
		if step == "adopt-moved" {
			if err := os.MkdirAll(local.SourcePath, 0o755); err != nil {
				return err
			}
			return os.WriteFile(filepath.Join(local.SourcePath, "edit"), []byte("user"), 0o644)
		}
		return nil
	}
	if _, err := svc.Adopt(project, adapter.Codex, "demo"); !errors.Is(err, lifecycle.ErrUnsafePath) {
		t.Fatalf("Adopt = %v", err)
	}
	if got, err := os.ReadFile(filepath.Join(local.SourcePath, "edit")); err != nil || string(got) != "user" {
		t.Fatalf("post-move edit was not preserved: %q, %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(library, "demo")); !os.IsNotExist(err) {
		t.Fatalf("library copy was retained: %v", err)
	}
}

func TestAdoptPreservesMovedOriginalWhenDestinationIsRecreated(t *testing.T) {
	root := t.TempDir()
	project, library := filepath.Join(root, "project"), filepath.Join(root, "library")
	local := writeSkill(t, filepath.Join(project, ".codex", "skills", "demo"), "demo")
	if err := os.WriteFile(filepath.Join(local.SourcePath, "resource"), []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	svc := lifecycle.New(library, operation.New(filepath.Join(root, "journal.json")), func(operation.Plan) bool { return true })
	svc.BeforePublish = func(step string) error {
		if step == "adopt-moved" {
			if err := os.MkdirAll(local.SourcePath, 0o755); err != nil {
				return err
			}
			return os.WriteFile(filepath.Join(local.SourcePath, "edit"), []byte("user"), 0o644)
		}
		return nil
	}
	if _, err := svc.Adopt(project, adapter.Codex, "demo"); !errors.Is(err, lifecycle.ErrUnsafePath) {
		t.Fatalf("Adopt = %v", err)
	}
	if got, err := os.ReadFile(filepath.Join(local.SourcePath, "edit")); err != nil || string(got) != "user" {
		t.Fatalf("concurrent edit = %q, %v", got, err)
	}
	matches, err := filepath.Glob(local.SourcePath + ".skill-manager-recovery-*")
	if err != nil || len(matches) != 1 {
		t.Fatalf("recovery path = %#v, %v", matches, err)
	}
	if got, err := os.ReadFile(filepath.Join(matches[0], "resource")); err != nil || string(got) != "original" {
		t.Fatalf("original resource = %q, %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(matches[0], "SKILL.md")); err != nil {
		t.Fatalf("original SKILL.md missing: %v", err)
	}
	assertNoAdoptionStages(t, filepath.Dir(local.SourcePath))
}

func TestAdoptKeepsPreviousStageWhenRecoveryPublicationFails(t *testing.T) {
	root := t.TempDir()
	project, library := filepath.Join(root, "project"), filepath.Join(root, "library")
	local := writeSkill(t, filepath.Join(project, ".codex", "skills", "demo"), "demo")
	if err := os.WriteFile(filepath.Join(local.SourcePath, "resource"), []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	svc := lifecycle.New(library, operation.New(filepath.Join(root, "journal.json")), func(operation.Plan) bool { return true })
	svc.BeforePublish = func(step string) error {
		switch step {
		case "adopt-moved":
			if err := os.MkdirAll(local.SourcePath, 0o755); err != nil {
				return err
			}
			return os.WriteFile(filepath.Join(local.SourcePath, "edit"), []byte("user"), 0o644)
		case "adopt-recovery":
			return errors.New("recovery publication failure")
		}
		return nil
	}
	if _, err := svc.Adopt(project, adapter.Codex, "demo"); !errors.Is(err, lifecycle.ErrUnsafePath) {
		t.Fatalf("Adopt = %v", err)
	}
	stages, err := filepath.Glob(filepath.Join(filepath.Dir(local.SourcePath), ".skill-manager-stage-*", "previous"))
	if err != nil || len(stages) != 1 {
		t.Fatalf("unrecovered previous = %#v, %v", stages, err)
	}
	if got, err := os.ReadFile(filepath.Join(stages[0], "resource")); err != nil || string(got) != "original" {
		t.Fatalf("previous resource = %q, %v", got, err)
	}
}

func assertNoAdoptionStages(t *testing.T, parent string) {
	t.Helper()
	stages, err := filepath.Glob(filepath.Join(parent, ".skill-manager-stage-*"))
	if err != nil || len(stages) != 0 {
		t.Fatalf("stages = %#v, %v", stages, err)
	}
}

func fixture(t *testing.T) (string, string, catalog.Skill) {
	t.Helper()
	root := t.TempDir()
	project := filepath.Join(root, "project")
	skill := writeSkill(t, filepath.Join(root, "library", "demo"), "demo")
	skill.Compatibility = []string{string(adapter.ClaudeCode)}
	return root, project, skill
}
func writeSkill(t *testing.T, dir, id string) catalog.Skill {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: demo\ndescription: demo\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return catalog.Skill{Identifier: id, SourcePath: dir}
}

func assertCapturedOriginal(t *testing.T, journal *operation.Journal, name, want string) {
	t.Helper()
	backups, err := filepath.Glob(filepath.Join(filepath.Dir(journal.Path), ".skill-manager-journal", "*"))
	if err != nil || len(backups) != 1 {
		t.Fatalf("captured prior content = %#v, %v; want one recoverable snapshot", backups, err)
	}
	if got, err := os.ReadFile(filepath.Join(backups[0], name)); err != nil || string(got) != want {
		t.Fatalf("captured prior content = %q, %v", got, err)
	}
}
func hasStatus(items []lifecycle.Item, id string, status lifecycle.Status) bool {
	for _, item := range items {
		if item.Identifier == id && item.Status == status {
			return true
		}
	}
	return false
}

func TestAddReplaceStrategyRequiresForce(t *testing.T) {
	root, project, skill := fixture(t)
	a, _ := adapter.For(adapter.Codex)
	path := a.ProjectSkillPath(project, skill.Identifier)
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	svc := lifecycle.New(filepath.Join(root, "library"), operation.New(filepath.Join(root, "journal.json")), func(operation.Plan) bool { return true })
	_, err := svc.Add(project, skill, []adapter.Target{adapter.Codex}, lifecycle.Options{Conflict: lifecycle.ConflictReplace})
	if err == nil || !errors.Is(err, lifecycle.ErrForceRequired) {
		t.Fatalf("Add error = %v, want force requirement", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("unmanaged path was changed: %v", err)
	}
}

func TestAddReplaceForceReplacesUnmanagedDirectoryAndUndoRestores(t *testing.T) {
	root, project, skill := fixture(t)
	a, _ := adapter.For(adapter.Codex)
	path := a.ProjectSkillPath(project, skill.Identifier)
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	original := filepath.Join(path, "SKILL.md")
	if err := os.WriteFile(original, []byte("unmanaged"), 0o644); err != nil {
		t.Fatal(err)
	}
	var preview operation.Plan
	journal := operation.New(filepath.Join(root, "journal.json"))
	svc := lifecycle.New(filepath.Join(root, "library"), journal, func(p operation.Plan) bool { preview = p; return true })
	opts := lifecycle.Options{Conflict: lifecycle.ConflictReplace, Force: true}
	if _, err := svc.Add(project, skill, []adapter.Target{adapter.Codex}, opts); err != nil {
		t.Fatal(err)
	}
	got, err := os.Readlink(path)
	if err != nil || got != skill.SourcePath {
		t.Fatalf("link = %q, %v; want link to %s", got, err, skill.SourcePath)
	}
	if len(preview.Changes) != 1 || preview.Changes[0].Action != "replace conflicting path with absolute link" {
		t.Fatalf("changes = %#v, want one replace change", preview.Changes)
	}
	if err := svc.Undo(); err != nil {
		t.Fatal(err)
	}
	restored, err := os.ReadFile(original)
	if err != nil || string(restored) != "unmanaged" {
		t.Fatalf("undo restored %q, %v; want original unmanaged content", restored, err)
	}
	if _, err := os.Lstat(path); err != nil || infoIsLink(t, path) {
		t.Fatalf("path after undo = %v", err)
	}
}

func TestAddManyReplaceForceReplacesConflictingLinks(t *testing.T) {
	root, project, one := fixture(t)
	two := writeSkill(t, filepath.Join(root, "library", "two"), "two")
	a, _ := adapter.For(adapter.Codex)
	path := a.ProjectSkillPath(project, two.Identifier)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("plain file"), 0o644); err != nil {
		t.Fatal(err)
	}
	journal := operation.New(filepath.Join(root, "j.json"))
	svc := lifecycle.New(filepath.Join(root, "library"), journal, func(operation.Plan) bool { return true })
	opts := lifecycle.Options{Conflict: lifecycle.ConflictReplace, Force: true}
	if _, err := svc.AddMany(project, []catalog.Skill{one, two}, []adapter.Target{adapter.Codex}, opts); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{one.Identifier, two.Identifier} {
		if _, err := os.Readlink(filepath.Join(project, ".codex", "skills", id)); err != nil {
			t.Fatalf("%s not a link: %v", id, err)
		}
	}
	if err := svc.Undo(); err != nil {
		t.Fatal(err)
	}
	restored, err := os.ReadFile(path)
	if err != nil || string(restored) != "plain file" {
		t.Fatalf("undo restored %q, %v; want original file content", restored, err)
	}
}

func TestAddManyReplacementKeepsContentEditedAfterPlanReview(t *testing.T) {
	root, project, skill := fixture(t)
	target, _ := adapter.For(adapter.Codex)
	destination := target.ProjectSkillPath(project, skill.Identifier)
	if err := os.MkdirAll(destination, 0o755); err != nil {
		t.Fatal(err)
	}
	ownerPath := filepath.Join(destination, "owner.txt")
	if err := os.WriteFile(ownerPath, []byte("reviewed"), 0o644); err != nil {
		t.Fatal(err)
	}
	approved, err := operation.FingerprintPath(destination)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ownerPath, []byte("edited after review"), 0o644); err != nil {
		t.Fatal(err)
	}
	service := lifecycle.New(filepath.Join(root, "library"), operation.New(filepath.Join(root, "journal.json")), func(operation.Plan) bool { return true })
	options := lifecycle.Options{
		Conflict: lifecycle.ConflictReplace, Force: true,
		ExpectedConflictFingerprints: map[string]string{destination: approved},
	}
	if _, err := service.AddMany(project, []catalog.Skill{skill}, []adapter.Target{adapter.Codex}, options); !errors.Is(err, lifecycle.ErrUnsafePath) {
		t.Fatalf("AddMany edited conflict error = %v, want stale conflict", err)
	}
	if got, err := os.ReadFile(ownerPath); err != nil || string(got) != "edited after review" {
		t.Fatalf("edited conflict content = %q, %v; it must remain intact", got, err)
	}
}

func TestAddReplaceNotConfirmedLeavesConflict(t *testing.T) {
	root, project, skill := fixture(t)
	a, _ := adapter.For(adapter.Codex)
	path := a.ProjectSkillPath(project, skill.Identifier)
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	svc := lifecycle.New(filepath.Join(root, "library"), operation.New(filepath.Join(root, "journal.json")), func(operation.Plan) bool { return false })
	opts := lifecycle.Options{Conflict: lifecycle.ConflictReplace, Force: true}
	if _, err := svc.Add(project, skill, []adapter.Target{adapter.Codex}, opts); !errors.Is(err, operation.ErrNotConfirmed) {
		t.Fatalf("Add error = %v, want confirmation refusal", err)
	}
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() {
		t.Fatalf("conflicting directory changed: %v", err)
	}
}

func infoIsLink(t *testing.T, path string) bool {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Mode()&os.ModeSymlink != 0
}

func TestPreviewManyIsReadOnlyAndApplyManyRequiresConfirmation(t *testing.T) {
	root, project, skill := fixture(t)
	targets := []adapter.Target{adapter.Codex}
	journal := operation.New(filepath.Join(root, "journal.json"))
	svc := lifecycle.New(filepath.Join(root, "library"), journal, nil)

	plan, err := svc.PreviewMany(project, []catalog.Skill{skill}, targets)
	if err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(project, ".codex", "skills", skill.Identifier)
	if _, err := os.Lstat(link); !os.IsNotExist(err) {
		t.Fatalf("preview created link: %v", err)
	}
	if _, ok, err := journal.Latest(); err != nil || ok {
		t.Fatalf("preview journal state = (%v, %v), want no entry", ok, err)
	}
	if _, err := svc.ApplyMany(project, []catalog.Skill{skill}, targets, plan, false); !errors.Is(err, operation.ErrNotConfirmed) {
		t.Fatalf("unconfirmed apply error = %v", err)
	}
	if _, err := svc.ApplyMany(project, []catalog.Skill{skill}, targets, plan, true); err != nil {
		t.Fatal(err)
	}
	if got, err := os.Readlink(link); err != nil || got != skill.SourcePath {
		t.Fatalf("link = %q, %v", got, err)
	}
	if _, ok, err := journal.Latest(); err != nil || !ok {
		t.Fatalf("successful apply journal state = (%v, %v), want entry", ok, err)
	}
}

func TestApplyManyRejectsChangedPlanWithoutReplacingDestination(t *testing.T) {
	root, project, skill := fixture(t)
	targets := []adapter.Target{adapter.Codex}
	svc := lifecycle.New(filepath.Join(root, "library"), operation.New(filepath.Join(root, "journal.json")), nil)
	plan, err := svc.PreviewMany(project, []catalog.Skill{skill}, targets)
	if err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(project, ".codex", "skills", skill.Identifier)
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(link, []byte("user data"), 0o644); err != nil {
		t.Fatal(err)
	}
	current, err := svc.ApplyMany(project, []catalog.Skill{skill}, targets, plan, true)
	if !errors.Is(err, lifecycle.ErrPlanChanged) {
		t.Fatalf("apply error = %v, want plan changed", err)
	}
	if len(current.Changes) != 1 || current.Changes[0].Action != "refuse conflicting destination" {
		t.Fatalf("updated plan = %#v", current)
	}
	contents, err := os.ReadFile(link)
	if err != nil || string(contents) != "user data" {
		t.Fatalf("destination contents = %q, %v", contents, err)
	}
}

func TestAddManyRestoresConflictWhenReplacementPublishFails(t *testing.T) {
	root, project, skill := fixture(t)
	a, _ := adapter.For(adapter.Codex)
	path := a.ProjectSkillPath(project, skill.Identifier)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("user-owned content"), 0o644); err != nil {
		t.Fatal(err)
	}
	svc := lifecycle.New(filepath.Join(root, "library"), operation.New(filepath.Join(root, "j.json")), func(operation.Plan) bool { return true })
	svc.BeforePublish = func(string) error { return errors.New("publish denied") }
	options := lifecycle.Options{Conflict: lifecycle.ConflictReplace, Force: true}
	if _, err := svc.AddMany(project, []catalog.Skill{skill}, []adapter.Target{adapter.Codex}, options); err == nil {
		t.Fatal("AddMany unexpectedly succeeded")
	}
	contents, err := os.ReadFile(path)
	if err != nil || string(contents) != "user-owned content" {
		t.Fatalf("conflicting destination after failed replacement = %q, %v", contents, err)
	}
	if _, err := os.Lstat(path); err != nil {
		t.Fatalf("original destination was not restored: %v", err)
	}
}
