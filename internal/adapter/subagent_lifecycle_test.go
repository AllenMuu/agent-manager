package adapter_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AllenMuu/skill-manager/internal/adapter"
	"github.com/AllenMuu/skill-manager/internal/operation"
	"github.com/AllenMuu/skill-manager/internal/resource"
	"github.com/AllenMuu/skill-manager/internal/subagent"
)

func TestInstallAndRemoveSubAgentIsJournaledAndUndoable(t *testing.T) {
	root := t.TempDir()
	journal := operation.New(filepath.Join(root, "journal.json"))
	definition := subagent.Definition{
		Version: subagent.Version, ID: "reviewer", Name: "Reviewer", Role: "Reviews",
		Instructions: "Review changes.",
	}

	preview, err := adapter.InstallSubAgent(definition, adapter.SubAgentRequest{Root: root, Scope: adapter.SubAgentProject}, adapter.SubAgentFilesystemOptions{
		SourceRoot:            root,
		SkillReferenceChecker: func(string) bool { return true },
		Journal:               journal,
		Confirm:               func(p operation.Plan) bool { return p.ResourceKind == string(resource.SubAgent) },
	})
	if err != nil {
		t.Fatalf("InstallSubAgent() error = %v", err)
	}
	if preview.ResourceKind != string(resource.SubAgent) {
		t.Fatalf("install plan resource kind = %q", preview.ResourceKind)
	}
	destination := filepath.Join(root, ".claude", "agents", "reviewer.md")
	source := filepath.Join(root, ".agent-manager", "subagents", "claude-code", "reviewer.md")
	target, err := os.Readlink(destination)
	if err != nil || target != source {
		t.Fatalf("installed destination target = %q, %v; want %q", target, err, source)
	}
	content, err := os.ReadFile(destination)
	if err != nil || !strings.Contains(string(content), "Review changes.") {
		t.Fatalf("installed content = %q, %v", content, err)
	}
	entry, ok, err := journal.Latest()
	if err != nil || !ok || entry.ResourceKind != string(resource.SubAgent) {
		t.Fatalf("journal latest = %#v, %v, %v", entry, ok, err)
	}

	if _, err := adapter.RemoveSubAgent(definition, adapter.SubAgentRequest{Root: root, Scope: adapter.SubAgentProject}, adapter.SubAgentFilesystemOptions{
		SourceRoot:            root,
		SkillReferenceChecker: func(string) bool { return true },
		Journal:               journal,
		Confirm:               func(operation.Plan) bool { return true },
	}); err != nil {
		t.Fatalf("RemoveSubAgent() error = %v", err)
	}
	if _, err := os.Lstat(destination); !os.IsNotExist(err) {
		t.Fatalf("removed destination = %v, want absent", err)
	}
	if err := journal.UndoLatest(func(operation.Plan) bool { return true }); err != nil {
		t.Fatalf("UndoLatest() error = %v", err)
	}
	if _, err := os.Stat(destination); err != nil {
		t.Fatalf("undo did not restore destination: %v", err)
	}
}

func TestInstallSubAgentIdempotentDoesNotJournalOrMakeUndoDestructive(t *testing.T) {
	root := t.TempDir()
	journal := operation.New(filepath.Join(root, "journal.json"))
	definition := subagent.Definition{Version: subagent.Version, ID: "reviewer", Name: "Reviewer", Role: "Reviews", Instructions: "Review changes."}
	if _, err := adapter.InstallSubAgent(definition, adapter.SubAgentRequest{Root: root, Scope: adapter.SubAgentProject}, adapter.SubAgentFilesystemOptions{
		SourceRoot: root, SkillReferenceChecker: func(string) bool { return true }, Journal: journal, Confirm: func(operation.Plan) bool { return true },
	}); err != nil {
		t.Fatalf("initial InstallSubAgent() error = %v", err)
	}
	if err := journal.RecordPlan(operation.Plan{Version: "v1", ResourceKind: string(resource.SubAgent), Operation: "unrelated"}, nil, nil); err != nil {
		t.Fatalf("record unrelated journal entry: %v", err)
	}
	entryBefore, ok, err := journal.Latest()
	if err != nil || !ok {
		t.Fatalf("initial journal = %#v, %v, %v", entryBefore, ok, err)
	}
	plan, err := adapter.InstallSubAgent(definition, adapter.SubAgentRequest{Root: root, Scope: adapter.SubAgentProject}, adapter.SubAgentFilesystemOptions{
		SourceRoot: root, SkillReferenceChecker: func(string) bool { return true }, Journal: journal,
		Confirm: func(operation.Plan) bool { t.Fatal("idempotent install requested confirmation"); return true },
	})
	if err != nil {
		t.Fatalf("idempotent InstallSubAgent() error = %v", err)
	}
	if len(plan.Changes) != 0 || len(plan.Warnings) == 0 {
		t.Fatalf("idempotent plan = %#v, want warning-only no-op", plan)
	}
	entryAfter, ok, err := journal.Latest()
	if err != nil || !ok || entryAfter.At != entryBefore.At {
		t.Fatalf("idempotent journal changed: before=%#v after=%#v err=%v", entryBefore, entryAfter, err)
	}
	if err := journal.UndoLatest(func(operation.Plan) bool { return true }); err != nil {
		t.Fatalf("UndoLatest() error = %v", err)
	}
	if _, err := os.Lstat(filepath.Join(root, ".claude", "agents", "reviewer.md")); err != nil {
		t.Fatalf("undo removed the idempotently retained installation: %v", err)
	}
}

func TestInstallSubAgentRevalidatesSourceImmediatelyBeforePublication(t *testing.T) {
	root := t.TempDir()
	journal := operation.New(filepath.Join(root, "journal.json"))
	definition := subagent.Definition{Version: subagent.Version, ID: "reviewer", Name: "Reviewer", Role: "Reviews", Instructions: "Review changes."}
	source := filepath.Join(root, ".agent-manager", "subagents", "claude-code", "reviewer.md")
	_, err := adapter.InstallSubAgent(definition, adapter.SubAgentRequest{Root: root, Scope: adapter.SubAgentProject}, adapter.SubAgentFilesystemOptions{
		SourceRoot: root, SkillReferenceChecker: func(string) bool { return true }, Journal: journal,
		BeforeFinalPublish: func() error { return os.WriteFile(source, []byte("tampered"), 0o644) },
		Confirm:            func(operation.Plan) bool { return true },
	})
	if !errors.Is(err, adapter.ErrUnsafePath) {
		t.Fatalf("InstallSubAgent() error = %v, want unsafe path", err)
	}
	if _, err := os.Lstat(filepath.Join(root, ".claude", "agents", "reviewer.md")); !os.IsNotExist(err) {
		t.Fatalf("source revalidation still published destination: %v", err)
	}
}

func TestInstallSubAgentPreservesOriginalWhenStageDiscardFails(t *testing.T) {
	root := t.TempDir()
	destination := filepath.Join(root, ".codex", "agents", "reviewer.toml")
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destination, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	definition := subagent.Definition{Version: subagent.Version, ID: "reviewer", Name: "Reviewer", Role: "Reviews", Instructions: "Review changes."}
	failure := errors.New("discard failed")
	_, err := adapter.InstallSubAgent(definition, adapter.SubAgentRequest{Root: root, Scope: adapter.SubAgentProject}, adapter.SubAgentFilesystemOptions{
		Target: adapter.Codex, SourceRoot: root, Conflict: adapter.ConflictReplace, Force: true,
		SkillReferenceChecker: func(string) bool { return true }, Journal: operation.New(filepath.Join(root, "journal.json")),
		BeforeDiscard: func() error { return failure }, Confirm: func(operation.Plan) bool { return true },
	})
	if !errors.Is(err, failure) {
		t.Fatalf("InstallSubAgent() error = %v, want discard failure", err)
	}
	if got, readErr := os.ReadFile(destination); readErr != nil || string(got) != "original" {
		t.Fatalf("original after discard failure = %q, %v", got, readErr)
	}
}

func TestInstallSubAgentKeepsFilesystemWhenJournalCommitSyncFails(t *testing.T) {
	root := t.TempDir()
	failure := errors.New("journal sync failed")
	journal := operation.New(filepath.Join(root, "journal.json"))
	journal.BeforeJournalDirectorySync = func() error { return failure }
	definition := subagent.Definition{Version: subagent.Version, ID: "reviewer", Name: "Reviewer", Role: "Reviews", Instructions: "Review changes."}
	_, err := adapter.InstallSubAgent(definition, adapter.SubAgentRequest{Root: root, Scope: adapter.SubAgentProject}, adapter.SubAgentFilesystemOptions{
		SourceRoot: root, SkillReferenceChecker: func(string) bool { return true }, Journal: journal, Confirm: func(operation.Plan) bool { return true },
	})
	if !errors.Is(err, operation.ErrJournalCommitted) {
		t.Fatalf("InstallSubAgent() error = %v, want journal-committed error", err)
	}
	if _, err := os.Lstat(filepath.Join(root, ".claude", "agents", "reviewer.md")); err != nil {
		t.Fatalf("filesystem was rolled back after committed journal: %v", err)
	}
	if _, ok, readErr := journal.Latest(); readErr != nil || !ok {
		t.Fatalf("committed journal missing: ok=%v err=%v", ok, readErr)
	}
}

func TestInstallSubAgentKeepsRenderedSourceInCanonicalRoot(t *testing.T) {
	project := t.TempDir()
	dataRoot := t.TempDir()
	definition := subagent.Definition{Version: subagent.Version, ID: "reviewer", Name: "Reviewer", Role: "Reviews", Instructions: "Review changes."}
	if _, err := adapter.InstallSubAgent(definition, adapter.SubAgentRequest{Root: project, Scope: adapter.SubAgentProject}, adapter.SubAgentFilesystemOptions{
		SourceRoot:            dataRoot,
		SkillReferenceChecker: func(string) bool { return true },
		Journal:               operation.New(filepath.Join(project, "journal.json")),
		Confirm:               func(operation.Plan) bool { return true },
	}); err != nil {
		t.Fatalf("InstallSubAgent() error = %v", err)
	}
	source := filepath.Join(dataRoot, ".agent-manager", "subagents", "claude-code", "reviewer.md")
	destination := filepath.Join(project, ".claude", "agents", "reviewer.md")
	if _, err := os.Stat(source); err != nil {
		t.Fatalf("canonical rendered source = %v", err)
	}
	if target, err := os.Readlink(destination); err != nil || target != source {
		t.Fatalf("destination target = %q, %v; want canonical source %q", target, err, source)
	}
}

func TestInstallSubAgentCodexUsesTOMLRepresentation(t *testing.T) {
	project := t.TempDir()
	dataRoot := t.TempDir()
	definition := subagent.Definition{Version: subagent.Version, ID: "reviewer", Name: "Reviewer", Role: "Reviews", Instructions: "Review changes."}
	if _, err := adapter.InstallSubAgent(definition, adapter.SubAgentRequest{Root: project, Scope: adapter.SubAgentProject}, adapter.SubAgentFilesystemOptions{
		Target: adapter.Codex, SourceRoot: dataRoot, SkillReferenceChecker: func(string) bool { return true }, Journal: operation.New(filepath.Join(project, "journal.json")), Confirm: func(operation.Plan) bool { return true },
	}); err != nil {
		t.Fatalf("InstallSubAgent() error = %v", err)
	}
	destination := filepath.Join(project, ".codex", "agents", "reviewer.toml")
	if _, err := os.Lstat(destination); err != nil {
		t.Fatalf("Codex destination = %v", err)
	}
	content, err := os.ReadFile(destination)
	if err != nil || !strings.Contains(string(content), "developer_instructions") {
		t.Fatalf("Codex content = %q, %v", content, err)
	}
}

func TestInstallSubAgentRefusesUnmanagedConflictWithoutForce(t *testing.T) {
	root := t.TempDir()
	destination := filepath.Join(root, ".codex", "agents", "reviewer.toml")
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destination, []byte("user-owned"), 0o644); err != nil {
		t.Fatal(err)
	}
	definition := subagent.Definition{Version: subagent.Version, ID: "reviewer", Name: "Reviewer", Role: "Reviews", Instructions: "Review changes."}
	_, err := adapter.InstallSubAgent(definition, adapter.SubAgentRequest{Root: root, Scope: adapter.SubAgentProject}, adapter.SubAgentFilesystemOptions{
		SourceRoot:            root,
		SkillReferenceChecker: func(string) bool { return true },
		Target:                adapter.Codex,
		Journal:               operation.New(filepath.Join(root, "journal.json")),
		Conflict:              adapter.ConflictReplace,
		Confirm:               func(operation.Plan) bool { t.Fatal("confirmation must not be requested without force"); return true },
	})
	if !errors.Is(err, adapter.ErrForceRequired) {
		t.Fatalf("InstallSubAgent() error = %v, want force-required", err)
	}
	content, readErr := os.ReadFile(destination)
	if readErr != nil || string(content) != "user-owned" {
		t.Fatalf("unmanaged content changed: %q, %v", content, readErr)
	}
}

func TestInstallSubAgentRollsBackRenderedSourceWhenPublicationFails(t *testing.T) {
	root := t.TempDir()
	journal := operation.New(filepath.Join(root, "journal.json"))
	definition := subagent.Definition{Version: subagent.Version, ID: "reviewer", Name: "Reviewer", Role: "Reviews", Instructions: "Review changes."}
	failure := errors.New("injected publication failure")
	_, err := adapter.InstallSubAgent(definition, adapter.SubAgentRequest{Root: root, Scope: adapter.SubAgentProject}, adapter.SubAgentFilesystemOptions{
		SourceRoot:            root,
		SkillReferenceChecker: func(string) bool { return true },
		Journal:               journal,
		Confirm:               func(operation.Plan) bool { return true },
		BeforeFinalPublish: func() error {
			return failure
		},
	})
	if !errors.Is(err, failure) {
		t.Fatalf("InstallSubAgent() error = %v, want injected publication failure", err)
	}
	source := filepath.Join(root, ".agent-manager", "subagents", "claude-code", "reviewer.md")
	destination := filepath.Join(root, ".claude", "agents", "reviewer.md")
	if _, statErr := os.Lstat(source); !os.IsNotExist(statErr) {
		t.Fatalf("failed installation left source: %v", statErr)
	}
	if _, statErr := os.Lstat(destination); !os.IsNotExist(statErr) {
		t.Fatalf("failed installation left destination: %v", statErr)
	}
	if _, found, journalErr := journal.Latest(); journalErr != nil || found {
		t.Fatalf("failed installation journal = found=%v, err=%v; want no entry", found, journalErr)
	}
}

func TestInstallSubAgentRefusesUnmanagedRenderedSource(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, ".agent-manager", "subagents", "claude-code", "reviewer.md")
	if err := os.MkdirAll(filepath.Dir(source), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, []byte("user-owned source"), 0o644); err != nil {
		t.Fatal(err)
	}
	definition := subagent.Definition{Version: subagent.Version, ID: "reviewer", Name: "Reviewer", Role: "Reviews", Instructions: "Review changes."}
	_, err := adapter.InstallSubAgent(definition, adapter.SubAgentRequest{Root: root, Scope: adapter.SubAgentProject}, adapter.SubAgentFilesystemOptions{
		SourceRoot:            root,
		SkillReferenceChecker: func(string) bool { return true },
		Journal:               operation.New(filepath.Join(root, "journal.json")),
		Confirm: func(operation.Plan) bool {
			t.Fatal("confirmation must not be requested for unmanaged source")
			return true
		},
	})
	if !errors.Is(err, adapter.ErrUnsafePath) {
		t.Fatalf("InstallSubAgent() error = %v, want unsafe path", err)
	}
	content, readErr := os.ReadFile(source)
	if readErr != nil || string(content) != "user-owned source" {
		t.Fatalf("unmanaged source changed: %q, %v", content, readErr)
	}
}

func TestInstallSubAgentRequiresSkillReferenceChecker(t *testing.T) {
	root := t.TempDir()
	definition := subagent.Definition{Version: subagent.Version, ID: "reviewer", Name: "Reviewer", Role: "Reviews", Instructions: "Review changes.", Skills: []string{"missing"}}
	_, err := adapter.InstallSubAgent(definition, adapter.SubAgentRequest{Root: root, Scope: adapter.SubAgentProject}, adapter.SubAgentFilesystemOptions{
		SourceRoot:            root,
		Journal:               operation.New(filepath.Join(root, "journal.json")),
		Confirm:               func(operation.Plan) bool { t.Fatal("invalid references must not request confirmation"); return true },
		SkillReferenceChecker: func(string) bool { return false },
	})
	if err == nil || !strings.Contains(err.Error(), "referenced Skill") {
		t.Fatalf("InstallSubAgent() error = %v, want missing referenced Skill diagnostic", err)
	}
	if _, statErr := os.Lstat(filepath.Join(root, ".claude")); !os.IsNotExist(statErr) {
		t.Fatalf("invalid install wrote target files: %v", statErr)
	}
}

func TestInstallSubAgentRejectsMissingSkillReferenceChecker(t *testing.T) {
	root := t.TempDir()
	definition := subagent.Definition{Version: subagent.Version, ID: "reviewer", Name: "Reviewer", Role: "Reviews", Instructions: "Review changes."}
	_, err := adapter.InstallSubAgent(definition, adapter.SubAgentRequest{Root: root, Scope: adapter.SubAgentProject}, adapter.SubAgentFilesystemOptions{
		SourceRoot: root,
		Journal:    operation.New(filepath.Join(root, "journal.json")),
		Confirm:    func(operation.Plan) bool { t.Fatal("missing checker must fail before confirmation"); return true },
	})
	if err == nil || !strings.Contains(err.Error(), "Skill reference checker") {
		t.Fatalf("InstallSubAgent() error = %v, want checker boundary error", err)
	}
}

func TestInstallSubAgentRejectsRenderedSourceParentSwap(t *testing.T) {
	root := t.TempDir()
	external := t.TempDir()
	definition := subagent.Definition{Version: subagent.Version, ID: "reviewer", Name: "Reviewer", Role: "Reviews", Instructions: "Review changes."}
	journal := operation.New(filepath.Join(root, "journal.json"))
	_, err := adapter.InstallSubAgent(definition, adapter.SubAgentRequest{Root: root, Scope: adapter.SubAgentProject}, adapter.SubAgentFilesystemOptions{
		SourceRoot: root, Journal: journal, SkillReferenceChecker: func(string) bool { return true },
		Confirm: func(operation.Plan) bool { return true },
		BeforePublish: func() error {
			parent := filepath.Join(root, ".agent-manager", "subagents", "claude-code")
			if err := os.MkdirAll(filepath.Dir(parent), 0o755); err != nil {
				return err
			}
			if err := os.Mkdir(parent, 0o755); err != nil {
				return err
			}
			if err := os.Remove(parent); err != nil {
				return err
			}
			if err := os.Symlink(external, parent); err != nil {
				t.Skipf("symlink unavailable: %v", err)
			}
			return nil
		},
	})
	if !errors.Is(err, adapter.ErrUnsafePath) {
		t.Fatalf("InstallSubAgent() error = %v, want unsafe path", err)
	}
	if entries, readErr := os.ReadDir(external); readErr != nil || len(entries) != 0 {
		t.Fatalf("external source directory changed: entries=%v err=%v", entries, readErr)
	}
	if _, found, journalErr := journal.Latest(); journalErr != nil || found {
		t.Fatalf("journal = found=%v err=%v, want no entry", found, journalErr)
	}
}

func TestInstallSubAgentPiIsUnsupportedAndDoesNotWrite(t *testing.T) {
	root := t.TempDir()
	definition := subagent.Definition{Version: subagent.Version, ID: "reviewer", Name: "Reviewer", Role: "Reviews", Instructions: "Review changes."}
	_, err := adapter.InstallSubAgent(definition, adapter.SubAgentRequest{Root: root, Scope: adapter.SubAgentProject}, adapter.SubAgentFilesystemOptions{
		SourceRoot:            root,
		Target:                adapter.Pi,
		SkillReferenceChecker: func(string) bool { return true },
		Journal:               operation.New(filepath.Join(root, "journal.json")),
		Confirm:               func(operation.Plan) bool { t.Fatal("Pi must not request confirmation"); return true },
	})
	if !errors.Is(err, adapter.ErrSubAgentUnsupported) {
		t.Fatalf("InstallSubAgent() error = %v, want unsupported", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".pi")); !os.IsNotExist(err) {
		t.Fatalf("Pi installation wrote files: %v", err)
	}
}

func TestRemoveSubAgentRejectsReplacementAfterConfirmation(t *testing.T) {
	root := t.TempDir()
	definition := subagent.Definition{Version: subagent.Version, ID: "reviewer", Name: "Reviewer", Role: "Reviews", Instructions: "Review changes."}
	journal := operation.New(filepath.Join(root, "journal.json"))
	if _, err := adapter.InstallSubAgent(definition, adapter.SubAgentRequest{Root: root, Scope: adapter.SubAgentProject}, adapter.SubAgentFilesystemOptions{
		SourceRoot: root, Journal: journal, Confirm: func(operation.Plan) bool { return true },
		SkillReferenceChecker: func(string) bool { return true },
	}); err != nil {
		t.Fatalf("InstallSubAgent() error = %v", err)
	}
	destination := filepath.Join(root, ".claude", "agents", "reviewer.md")
	_, err := adapter.RemoveSubAgent(definition, adapter.SubAgentRequest{Root: root, Scope: adapter.SubAgentProject}, adapter.SubAgentFilesystemOptions{
		SourceRoot: root, Journal: journal,
		Confirm: func(operation.Plan) bool {
			if err := os.Remove(destination); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(destination, []byte("replacement owner"), 0o644); err != nil {
				t.Fatal(err)
			}
			return true
		},
	})
	if !errors.Is(err, adapter.ErrUnsafePath) {
		t.Fatalf("RemoveSubAgent() error = %v, want unsafe path", err)
	}
	content, readErr := os.ReadFile(destination)
	if readErr != nil || string(content) != "replacement owner" {
		t.Fatalf("replacement owner changed: %q, %v", content, readErr)
	}
}

func TestRemoveSubAgentRejectsParentSwapWithoutTouchingExternalDirectory(t *testing.T) {
	root := t.TempDir()
	external := t.TempDir()
	definition := subagent.Definition{Version: subagent.Version, ID: "reviewer", Name: "Reviewer", Role: "Reviews", Instructions: "Review changes."}
	journal := operation.New(filepath.Join(root, "journal.json"))
	if _, err := adapter.InstallSubAgent(definition, adapter.SubAgentRequest{Root: root, Scope: adapter.SubAgentProject}, adapter.SubAgentFilesystemOptions{
		SourceRoot: root, Journal: journal, SkillReferenceChecker: func(string) bool { return true }, Confirm: func(operation.Plan) bool { return true },
	}); err != nil {
		t.Fatalf("InstallSubAgent() error = %v", err)
	}
	parent := filepath.Join(root, ".claude", "agents")
	_, err := adapter.RemoveSubAgent(definition, adapter.SubAgentRequest{Root: root, Scope: adapter.SubAgentProject}, adapter.SubAgentFilesystemOptions{
		SourceRoot: root, Journal: journal,
		Confirm: func(operation.Plan) bool { return true },
		BeforeRemove: func() error {
			if err := os.RemoveAll(parent); err != nil {
				t.Skipf("parent replacement unavailable: %v", err)
			}
			if err := os.Symlink(external, parent); err != nil {
				t.Skipf("symlink unavailable: %v", err)
			}
			return nil
		},
	})
	if !errors.Is(err, adapter.ErrUnsafePath) {
		t.Fatalf("RemoveSubAgent() error = %v, want unsafe path", err)
	}
	if entries, readErr := os.ReadDir(external); readErr != nil || len(entries) != 0 {
		t.Fatalf("external directory changed: entries=%v err=%v", entries, readErr)
	}
}

func TestRemoveSubAgentDoesNotRecreateMissingParent(t *testing.T) {
	root := t.TempDir()
	definition := subagent.Definition{Version: subagent.Version, ID: "reviewer", Name: "Reviewer", Role: "Reviews", Instructions: "Review changes."}
	journal := operation.New(filepath.Join(root, "journal.json"))
	if _, err := adapter.InstallSubAgent(definition, adapter.SubAgentRequest{Root: root, Scope: adapter.SubAgentProject}, adapter.SubAgentFilesystemOptions{
		SourceRoot: root, SkillReferenceChecker: func(string) bool { return true }, Journal: journal, Confirm: func(operation.Plan) bool { return true },
	}); err != nil {
		t.Fatalf("InstallSubAgent() error = %v", err)
	}
	parent := filepath.Join(root, ".claude", "agents")
	_, err := adapter.RemoveSubAgent(definition, adapter.SubAgentRequest{Root: root, Scope: adapter.SubAgentProject}, adapter.SubAgentFilesystemOptions{
		SourceRoot: root, Journal: journal,
		BeforeRemove: func() error { return os.RemoveAll(parent) },
		Confirm:      func(operation.Plan) bool { return true },
	})
	if !errors.Is(err, adapter.ErrUnsafePath) {
		t.Fatalf("RemoveSubAgent() error = %v, want unsafe path", err)
	}
	if _, statErr := os.Lstat(parent); !os.IsNotExist(statErr) {
		t.Fatalf("missing parent was recreated: %v", statErr)
	}
}

func TestRemoveSubAgentPreservesOriginalWhenStageDiscardFails(t *testing.T) {
	root := t.TempDir()
	definition := subagent.Definition{Version: subagent.Version, ID: "reviewer", Name: "Reviewer", Role: "Reviews", Instructions: "Review changes."}
	journal := operation.New(filepath.Join(root, "journal.json"))
	if _, err := adapter.InstallSubAgent(definition, adapter.SubAgentRequest{Root: root, Scope: adapter.SubAgentProject}, adapter.SubAgentFilesystemOptions{
		SourceRoot: root, SkillReferenceChecker: func(string) bool { return true }, Journal: journal, Confirm: func(operation.Plan) bool { return true },
	}); err != nil {
		t.Fatalf("InstallSubAgent() error = %v", err)
	}
	destination := filepath.Join(root, ".claude", "agents", "reviewer.md")
	failure := errors.New("discard failed")
	_, err := adapter.RemoveSubAgent(definition, adapter.SubAgentRequest{Root: root, Scope: adapter.SubAgentProject}, adapter.SubAgentFilesystemOptions{
		SourceRoot: root, Journal: journal, BeforeDiscard: func() error { return failure }, Confirm: func(operation.Plan) bool { return true },
	})
	if !errors.Is(err, failure) {
		t.Fatalf("RemoveSubAgent() error = %v, want discard failure", err)
	}
	if target, readErr := os.Readlink(destination); readErr != nil || target == "" {
		t.Fatalf("removed destination after discard failure: target=%q err=%v", target, readErr)
	}
}

func TestRemoveSubAgentKeepsFilesystemWhenJournalCommitSyncFails(t *testing.T) {
	root := t.TempDir()
	definition := subagent.Definition{Version: subagent.Version, ID: "reviewer", Name: "Reviewer", Role: "Reviews", Instructions: "Review changes."}
	journal := operation.New(filepath.Join(root, "journal.json"))
	if _, err := adapter.InstallSubAgent(definition, adapter.SubAgentRequest{Root: root, Scope: adapter.SubAgentProject}, adapter.SubAgentFilesystemOptions{
		SourceRoot: root, SkillReferenceChecker: func(string) bool { return true }, Journal: journal, Confirm: func(operation.Plan) bool { return true },
	}); err != nil {
		t.Fatalf("InstallSubAgent() error = %v", err)
	}
	journal.BeforeJournalDirectorySync = func() error { return errors.New("journal sync failed") }
	destination := filepath.Join(root, ".claude", "agents", "reviewer.md")
	_, err := adapter.RemoveSubAgent(definition, adapter.SubAgentRequest{Root: root, Scope: adapter.SubAgentProject}, adapter.SubAgentFilesystemOptions{
		SourceRoot: root, Journal: journal, Confirm: func(operation.Plan) bool { return true },
	})
	if !errors.Is(err, operation.ErrJournalCommitted) {
		t.Fatalf("RemoveSubAgent() error = %v, want journal-committed error", err)
	}
	if _, statErr := os.Lstat(destination); !os.IsNotExist(statErr) {
		t.Fatalf("filesystem was rolled back after committed journal: %v", statErr)
	}
	entry, ok, readErr := journal.Latest()
	if readErr != nil || !ok || entry.Operation != "remove SubAgent" {
		t.Fatalf("committed remove journal = %#v ok=%v err=%v", entry, ok, readErr)
	}
}
