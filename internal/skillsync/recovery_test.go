package skillsync

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/AllenMuu/skill-manager/internal/operation"
)

func TestImportVersionPathKeepsLegacyRecordsAtIdentityRoot(t *testing.T) {
	identityPath := filepath.Join(t.TempDir(), "demo")
	if got := importVersionPath(identityPath, "legacy"); got != identityPath {
		t.Fatalf("legacy version path = %q, want identity root %q", got, identityPath)
	}
	if got, want := importVersionPath(identityPath, "v1"), filepath.Join(identityPath, "versions", "v1"); got != want {
		t.Fatalf("versioned import path = %q, want %q", got, want)
	}
}

func TestRecoveryMaterializationsPreservesParenthesesInLegacySourceDetail(t *testing.T) {
	source := filepath.Join(t.TempDir(), "source (copy-only)")
	destination := filepath.Join(t.TempDir(), "demo")
	writeSkill(t, source, "parentheses in source path")
	if err := os.Symlink(source, destination); err != nil {
		t.Skipf("create source link: %v", err)
	}
	batch := operation.Batch{
		Plan: operation.Plan{Changes: []operation.Change{{
			Path: destination, Action: "create absolute link", Detail: source + " (symlink-only)",
		}}},
		Steps: []operation.BatchStep{{Resource: destination, ExpectedAfter: recoverySymlinkIntent(t, source)}},
	}
	materializations, err := recoveryMaterializations(batch)
	if err != nil || len(materializations) != 1 {
		t.Fatalf("recovery materializations = %#v, err %v", materializations, err)
	}
	if got, want := materializations[0].Source, source; got != want {
		t.Fatalf("materialization source = %q, want %q", got, want)
	}
}

func TestLifecycleRecoveryRollsBackOnlyVerifiedAfterState(t *testing.T) {
	fixture := newSyncFixture(t)
	service := fixture.service(t)
	source := filepath.Join(fixture.repository, "demo", "versions", "v1")
	writeSkill(t, source, "managed version")
	destination := filepath.Join(fixture.project, ".claude", "skills", "demo")
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		t.Fatal(err)
	}
	journal := operation.ProjectJournal(fixture.project)
	before, err := journal.Capture([]string{destination})
	if err != nil {
		t.Fatal(err)
	}
	if err := operation.SyncSnapshots(before); err != nil {
		t.Fatal(err)
	}
	store := operation.NewBatchStore(filepath.Join(service.syncRoot, "batches"))
	plan := operation.NewPlan("migrate Skill references")
	plan.Changes = []operation.Change{{Path: destination, Action: "create absolute link", Detail: source + " (symlink-only)"}}
	expectedAfter := recoverySymlinkIntent(t, source)
	batch, err := store.Prepare(operation.Batch{ID: "recovery-rollback", Kind: "reference-migration", Scope: "project", Resources: []string{destination}, Plan: plan, Steps: []operation.BatchStep{{ID: "entry-1", Resource: destination, Action: "create absolute link", BeforeState: "absent", ExpectedAfter: expectedAfter}}})
	if err != nil {
		t.Fatal(err)
	}
	batch.Status = operation.BatchPublishing
	if err := store.Update(batch); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(source, destination); err != nil {
		t.Skipf("create simulated publication: %v", err)
	}
	preview, err := service.PreviewRecovery()
	if err != nil || len(preview.Batches) != 1 || !hasAction(preview.Batches[0].Actions, RecoveryRollback) {
		t.Fatalf("recovery preview = %#v, err %v", preview, err)
	}
	if _, err := service.ApplyRecovery(batch.ID, RecoveryRollback); err != nil {
		t.Fatalf("rollback verified publication: %v", err)
	}
	if _, err := os.Lstat(destination); !os.IsNotExist(err) {
		t.Fatalf("recovery did not restore absent before-state: lstat err=%v", err)
	}
	recovered, ok, err := store.Get(batch.ID)
	if err != nil || !ok || recovered.Status != operation.BatchRolledBack {
		t.Fatalf("recovered batch = %#v (ok=%v, err=%v)", recovered, ok, err)
	}
}

func TestLifecycleRecoveryCompletesJournalAndKeepsUndoEvidence(t *testing.T) {
	fixture := newSyncFixture(t)
	service := fixture.service(t)
	source := filepath.Join(fixture.repository, "demo", "versions", "v1")
	writeSkill(t, source, "managed version")
	destination := filepath.Join(fixture.project, ".claude", "skills", "demo")
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		t.Fatal(err)
	}
	journal := operation.ProjectJournal(fixture.project)
	before, err := journal.Capture([]string{destination})
	if err != nil {
		t.Fatal(err)
	}
	if err := operation.SyncSnapshots(before); err != nil {
		t.Fatal(err)
	}
	store := operation.NewBatchStore(filepath.Join(service.syncRoot, "batches"))
	plan := operation.NewPlan("migrate Skill references")
	plan.Changes = []operation.Change{{Path: destination, Action: "create absolute link", Detail: source + " (symlink-only)"}}
	expectedAfter := recoverySymlinkIntent(t, source)
	batch, err := store.Prepare(operation.Batch{ID: "recovery-complete", Kind: "reference-migration", Scope: "project", Resources: []string{destination}, Plan: plan, Steps: []operation.BatchStep{{ID: "entry-1", Resource: destination, Action: "create absolute link", BeforeState: "absent", ExpectedAfter: expectedAfter, BackupPath: before[0].Backup}}})
	if err != nil {
		t.Fatal(err)
	}
	batch.Status = operation.BatchPublishing
	if err := store.Update(batch); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(source, destination); err != nil {
		t.Skipf("create simulated publication: %v", err)
	}
	if _, err := service.ApplyRecovery(batch.ID, RecoveryComplete); err != nil {
		t.Fatalf("complete interrupted journal commit: %v", err)
	}
	completed, ok, err := store.Get(batch.ID)
	if err != nil || !ok || completed.Status != operation.BatchCommitted {
		t.Fatalf("completed batch = %#v (ok=%v, err=%v)", completed, ok, err)
	}
	entry, found, err := journal.Latest()
	if err != nil || !found || entry.BatchID != batch.ID {
		t.Fatalf("recovered journal entry = %#v (found=%v, err=%v)", entry, found, err)
	}
	if err := journal.UndoLatest(func(operation.Plan) bool { return true }); err != nil {
		t.Fatalf("Undo recovered operation: %v", err)
	}
	if _, err := os.Lstat(destination); !os.IsNotExist(err) {
		t.Fatalf("Undo did not restore before-state: lstat err=%v", err)
	}
}

func TestRecoveryPreservesSnapshotsAfterJournalPublicationSyncFailure(t *testing.T) {
	fixture := newSyncFixture(t)
	service := fixture.service(t)
	source := filepath.Join(fixture.repository, "demo", "versions", "v1")
	writeSkill(t, source, "managed version")
	oldSource := filepath.Join(t.TempDir(), "old", "demo")
	writeSkill(t, oldSource, "original version")
	destination := filepath.Join(fixture.project, ".claude", "skills", "demo")
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(oldSource, destination); err != nil {
		t.Skipf("create original link: %v", err)
	}
	journal := service.projectJournal
	before, err := journal.Capture([]string{destination})
	if err != nil {
		t.Fatal(err)
	}
	if err := operation.SyncSnapshots(before); err != nil {
		t.Fatal(err)
	}
	beforeState, err := operation.FingerprintPath(before[0].Backup)
	if err != nil {
		t.Fatal(err)
	}
	store := operation.NewBatchStore(filepath.Join(service.syncRoot, "batches"))
	plan := operation.NewPlan("migrate Skill references")
	plan.Changes = []operation.Change{{Path: destination, Action: "create absolute link", Detail: source + " (symlink-only)"}}
	expectedAfter := recoverySymlinkIntent(t, source)
	batch, err := store.Prepare(operation.Batch{
		ID: "journal-sync-recovery", Kind: "reference-migration", Scope: "project", Resources: []string{destination}, Plan: plan,
		Steps: []operation.BatchStep{{ID: "entry-1", Resource: destination, Action: "create absolute link", BeforeState: beforeState, ExpectedAfter: expectedAfter, BackupPath: before[0].Backup}},
	})
	if err != nil {
		t.Fatal(err)
	}
	batch.Status = operation.BatchPublishing
	if err := store.Update(batch); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(destination); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(source, destination); err != nil {
		t.Skipf("create managed link: %v", err)
	}
	journal.BeforeJournalDirectorySync = func() error { return errors.New("injected journal directory sync failure") }
	if _, err := service.ApplyRecovery(batch.ID, RecoveryComplete); err == nil {
		t.Fatal("recovery unexpectedly hid the committed-journal sync failure")
	}
	journal.BeforeJournalDirectorySync = nil
	entry, found, err := journal.Latest()
	if err != nil || !found || entry.BatchID != batch.ID {
		t.Fatalf("published recovery journal entry = %#v (found=%v, err=%v)", entry, found, err)
	}
	if _, err := os.Stat(entry.After[0].Backup); err != nil {
		t.Fatalf("journal after-state snapshot was deleted after commit: %v", err)
	}
	if _, err := service.ApplyRecovery(batch.ID, RecoveryComplete); err != nil {
		t.Fatalf("retry recovery after committed journal publication: %v", err)
	}
	if err := journal.UndoLatest(func(operation.Plan) bool { return true }); err != nil {
		t.Fatalf("Undo recovered operation after retry: %v", err)
	}
	if target, err := os.Readlink(destination); err != nil || target != oldSource {
		t.Fatalf("Undo restored link = %q, err %v", target, err)
	}
}

func TestLifecycleRecoveryBlocksChangedDestination(t *testing.T) {
	fixture := newSyncFixture(t)
	service := fixture.service(t)
	destination := filepath.Join(fixture.project, ".claude", "skills", "demo")
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		t.Fatal(err)
	}
	first := filepath.Join(t.TempDir(), "first")
	second := filepath.Join(t.TempDir(), "second")
	writeSkill(t, first, "one")
	writeSkill(t, second, "two")
	if err := os.Symlink(second, destination); err != nil {
		t.Skipf("create conflicting link: %v", err)
	}
	store := operation.NewBatchStore(filepath.Join(service.syncRoot, "batches"))
	plan := operation.NewPlan("migrate Skill references")
	plan.Changes = []operation.Change{{Path: destination, Action: "create absolute link", Detail: first + " (symlink-only)"}}
	batch, err := store.Prepare(operation.Batch{ID: "recovery-conflict", Kind: "reference-migration", Scope: "project", Resources: []string{destination}, Plan: plan, Steps: []operation.BatchStep{{ID: "entry-1", Resource: destination, Action: "create absolute link", BeforeState: "absent", ExpectedAfter: recoverySymlinkIntent(t, first)}}})
	if err != nil {
		t.Fatal(err)
	}
	batch.Status = operation.BatchPublishing
	if err := store.Update(batch); err != nil {
		t.Fatal(err)
	}
	preview, err := service.PreviewRecovery()
	if err != nil || len(preview.Batches) != 1 || len(preview.Batches[0].Actions) != 0 {
		t.Fatalf("conflicting recovery was offered: %#v, err %v", preview, err)
	}
	if _, err := service.ApplyRecovery(batch.ID, RecoveryRollback); err == nil {
		t.Fatal("recovery overwrote a conflicting link")
	}
}

func TestLifecycleRecoveryRejectsChangedManagedSourceBehindPublishedLink(t *testing.T) {
	fixture := newSyncFixture(t)
	service := fixture.service(t)
	source := filepath.Join(fixture.repository, "demo", "versions", "v1")
	writeSkill(t, source, "reviewed managed version")
	sourceChecksum, err := operation.FingerprintPath(source)
	if err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(fixture.project, ".claude", "skills", "demo")
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		t.Fatal(err)
	}
	journal := service.projectJournal
	before, err := journal.Capture([]string{destination})
	if err != nil {
		t.Fatal(err)
	}
	if err := operation.SyncSnapshots(before); err != nil {
		t.Fatal(err)
	}
	store := operation.NewBatchStore(filepath.Join(service.syncRoot, "batches"))
	plan := operation.NewPlan("migrate Skill references")
	plan.Changes = []operation.Change{{Path: destination, Action: "create absolute link", Detail: source + " (symlink-only)"}}
	batch, err := store.Prepare(operation.Batch{
		ID: "changed-source-recovery", Kind: "reference-migration", Scope: "project", Resources: []string{destination}, Plan: plan,
		Steps: []operation.BatchStep{{ID: "entry-1", Resource: destination, Action: "create absolute link", BeforeState: "absent", ExpectedAfter: "symlink\x00" + source + "\x00" + sourceChecksum}},
	})
	if err != nil {
		t.Fatal(err)
	}
	batch.Status = operation.BatchPublishing
	if err := store.Update(batch); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(source, destination); err != nil {
		t.Skipf("create managed link: %v", err)
	}
	batch.Steps[0].AfterState, err = operation.FingerprintPath(destination)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Update(batch); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "SKILL.md"), []byte("edited after interruption"), 0o644); err != nil {
		t.Fatal(err)
	}

	preview, err := service.PreviewRecovery()
	if err != nil || len(preview.Batches) != 1 {
		t.Fatalf("recovery preview = %#v, err %v", preview, err)
	}
	if hasAction(preview.Batches[0].Actions, RecoveryComplete) {
		t.Fatalf("recovery offered completion for a changed managed version: %#v", preview.Batches[0])
	}
	if !hasAction(preview.Batches[0].Actions, RecoveryRollback) {
		t.Fatalf("recovery did not offer safe destination rollback after source drift: %#v", preview.Batches[0])
	}
	if _, err := service.ApplyRecovery(batch.ID, RecoveryRollback); err != nil {
		t.Fatalf("rollback destination after source drift: %v", err)
	}
	if _, err := os.Lstat(destination); !os.IsNotExist(err) {
		t.Fatalf("source-drift rollback did not restore absent destination: %v", err)
	}
}

func TestRecoveryCompletesInterruptedUndoJournalRemovalIdempotently(t *testing.T) {
	fixture := newSyncFixture(t)
	service := fixture.service(t)
	oldSource := filepath.Join(t.TempDir(), "external", "old")
	managed := filepath.Join(fixture.repository, "demo", "versions", "v1")
	writeSkill(t, oldSource, "old source")
	writeSkill(t, managed, "managed version")
	destination := filepath.Join(fixture.project, ".claude", "skills", "demo")
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(oldSource, destination); err != nil {
		t.Skipf("create original source link: %v", err)
	}
	journal := operation.ProjectJournal(fixture.project)
	before, err := journal.Capture([]string{destination})
	if err != nil {
		t.Fatal(err)
	}
	if err := operation.SyncSnapshots(before); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(destination); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(managed, destination); err != nil {
		t.Skipf("create managed link: %v", err)
	}
	after, err := journal.Capture([]string{destination})
	if err != nil {
		t.Fatal(err)
	}
	if err := operation.SyncSnapshots(after); err != nil {
		t.Fatal(err)
	}
	plan := operation.NewPlan("migrate Skill reference")
	if err := journal.RecordPlan(plan, before, after); err != nil {
		t.Fatal(err)
	}
	entry, found, err := journal.Latest()
	if err != nil || !found {
		t.Fatalf("original journal entry missing: found=%v err=%v", found, err)
	}
	restoredFingerprint, err := operation.FingerprintPath(entry.Before[0].Backup)
	if err != nil {
		t.Fatal(err)
	}
	currentFingerprint, err := operation.FingerprintPath(destination)
	if err != nil {
		t.Fatal(err)
	}
	store := operation.NewBatchStore(filepath.Join(service.syncRoot, "batches"))
	batch, err := store.Prepare(operation.Batch{
		ID: "interrupted-undo", Kind: "undo", Scope: "project", Resources: []string{destination}, UndoEntry: &entry,
		Steps: []operation.BatchStep{{ID: "undo-0", Resource: destination, Action: "restore previous state", BeforeState: currentFingerprint, ExpectedAfter: "fingerprint\x00" + restoredFingerprint, BackupPath: entry.After[0].Backup}},
	})
	if err != nil {
		t.Fatal(err)
	}
	batch.Status = operation.BatchPublishing
	if err := store.Update(batch); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(destination); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(oldSource, destination); err != nil {
		t.Skipf("simulate restored Undo state: %v", err)
	}
	if _, err := service.ApplyRecovery(batch.ID, RecoveryComplete); err != nil {
		t.Fatalf("complete interrupted Undo: %v", err)
	}
	if _, found, err := journal.Latest(); err != nil || found {
		t.Fatalf("interrupted Undo retained its journal entry: found=%v err=%v", found, err)
	}
	recovered, ok, err := store.Get(batch.ID)
	if err != nil || !ok || recovered.Status != operation.BatchCommitted {
		t.Fatalf("recovered Undo batch = %#v (ok=%v err=%v)", recovered, ok, err)
	}
	if _, err := service.ApplyRecovery(batch.ID, RecoveryComplete); err != nil {
		t.Fatalf("repeating completed Undo recovery: %v", err)
	}
}

func TestPreviewRecoveryKeepsGlobalBatchesVisibleWhenProjectJournalIsUnavailable(t *testing.T) {
	fixture := newSyncFixture(t)
	service, err := New(fixture.repository, fixture.home, "", operation.GlobalJournal(fixture.home), nil)
	if err != nil {
		t.Fatal(err)
	}
	store := operation.NewBatchStore(filepath.Join(service.syncRoot, "batches"))
	global, err := store.Prepare(operation.Batch{
		ID: "global-pending", Kind: "reference-migration", Scope: "global",
		Resources: []string{filepath.Join(fixture.home, ".claude", "skills", "demo")},
		Steps:     []operation.BatchStep{{ID: "global-step", Resource: filepath.Join(fixture.home, ".claude", "skills", "demo"), Action: "leave absent", BeforeState: "absent", ExpectedAfter: "absent"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	global.Status = operation.BatchPublishing
	if err := store.Update(global); err != nil {
		t.Fatal(err)
	}
	projectResource := filepath.Join(os.DevNull, "unavailable-project", "demo")
	project, err := store.Prepare(operation.Batch{
		ID: "unavailable-project-undo", Kind: "undo", Scope: "project", Resources: []string{projectResource},
		Steps: []operation.BatchStep{{ID: "project-step", Resource: projectResource, Action: "restore previous state", BeforeState: "absent", ExpectedAfter: "absent"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	project.Status = operation.BatchPublishing
	if err := store.Update(project); err != nil {
		t.Fatal(err)
	}
	preview, err := service.PreviewRecovery()
	if err != nil {
		t.Fatalf("PreviewRecovery failed because one project journal is unavailable: %v", err)
	}
	byID := make(map[string]RecoveryCandidate, len(preview.Batches))
	for _, candidate := range preview.Batches {
		byID[candidate.BatchID] = candidate
	}
	if candidate, ok := byID[global.ID]; !ok || !hasAction(candidate.Actions, RecoveryComplete) {
		t.Fatalf("global recovery candidate was hidden by another scope: %#v", candidate)
	}
	if candidate, ok := byID[project.ID]; ok {
		t.Fatalf("inaccessible project batch was inspected and exposed: %#v", candidate)
	}
}

func recoverySymlinkIntent(t *testing.T, source string) string {
	t.Helper()
	absSource, err := filepath.Abs(source)
	if err != nil {
		t.Fatal(err)
	}
	fingerprint, err := operation.FingerprintPath(absSource)
	if err != nil {
		t.Fatal(err)
	}
	return "symlink\x00" + absSource + "\x00" + fingerprint
}
