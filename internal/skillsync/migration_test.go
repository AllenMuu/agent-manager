package skillsync

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AllenMuu/skill-manager/internal/adapter"
	"github.com/AllenMuu/skill-manager/internal/lifecycle"
	"github.com/AllenMuu/skill-manager/internal/operation"
)

func TestMigrationUsesExactVersionOnlyForSelectedEntryAndHonorsCopyMode(t *testing.T) {
	fixture := newSyncFixture(t)
	firstSource := filepath.Join(t.TempDir(), "external", "demo")
	secondSource := filepath.Join(t.TempDir(), "external", "other")
	writeSkill(t, firstSource, "linked demo")
	writeSkill(t, secondSource, "untouched other")
	for _, source := range []string{firstSource, secondSource} {
		if err := os.MkdirAll(filepath.Join(fixture.home, ".claude", "skills"), 0o755); err != nil {
			t.Fatal(err)
		}
		name := filepath.Base(source)
		if err := os.Symlink(source, filepath.Join(fixture.home, ".claude", "skills", name)); err != nil {
			t.Skipf("create source entry: %v", err)
		}
	}
	service := fixture.service(t)
	_, groups, err := service.Inventory()
	if err != nil {
		t.Fatal(err)
	}
	var demoID string
	for _, group := range groups {
		if group.Identifier == "demo" {
			preview, err := service.Preview([]string{group.ID}, nil)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := service.ApplyDetailed(preview); err != nil {
				t.Fatal(err)
			}
			demoID = group.Sources[0].ID
		}
	}
	if demoID == "" {
		t.Fatal("demo import group was not discovered")
	}
	migration, err := service.PreviewMigration([]string{demoID}, adapter.MaterializeCopyOnly)
	if err != nil {
		t.Fatal(err)
	}
	if migration.Scope != ScopeGlobal || len(migration.Entries) != 1 || migration.Entries[0].VersionKey == "" || migration.Plan.Operation != MigrationOperation {
		t.Fatalf("migration preview = %#v", migration)
	}
	if !strings.Contains(strings.Join(migration.Warnings, " "), "Other projects") {
		t.Fatalf("global impact warning missing: %v", migration.Warnings)
	}
	result, err := service.ApplyMigrationUntil(migration, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if result.BatchID == "" || len(result.Entries) != 1 {
		t.Fatalf("migration result lacks durable evidence: %#v", result)
	}
	destination := filepath.Join(fixture.home, ".claude", "skills", "demo")
	info, err := os.Lstat(destination)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("copy-only migration destination = %v, err %v", info, err)
	}
	if _, err := os.Stat(filepath.Join(destination, "SKILL.md")); err != nil {
		t.Fatalf("managed copy is incomplete: %v", err)
	}
	if target, err := os.Readlink(filepath.Join(fixture.home, ".claude", "skills", "other")); err != nil || target != secondSource {
		t.Fatalf("unselected reference changed: target=%q err=%v", target, err)
	}
	if _, err := os.Stat(filepath.Join(firstSource, "SKILL.md")); err != nil {
		t.Fatalf("old source was removed: %v", err)
	}
	entry, found, err := operation.GlobalJournal(fixture.home).Latest()
	if err != nil || !found || entry.Operation != MigrationOperation || entry.BatchID != result.BatchID || len(entry.Materializations) != 1 || entry.Materializations[0].Method != "copy" {
		t.Fatalf("migration journal evidence = %#v (found=%v, err=%v)", entry, found, err)
	}
}

func TestMigrationRejectsStaleEntryAndLeavesItsNewOwnerUntouched(t *testing.T) {
	fixture := newSyncFixture(t)
	first := filepath.Join(t.TempDir(), "external", "demo")
	second := filepath.Join(t.TempDir(), "external", "demo-new")
	writeSkill(t, first, "original linked content")
	writeSkill(t, second, "new owner content")
	entry := filepath.Join(fixture.project, ".codex", "skills", "demo")
	if err := os.MkdirAll(filepath.Dir(entry), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(first, entry); err != nil {
		t.Skipf("create source link: %v", err)
	}
	service := fixture.service(t)
	_, groups, err := service.Inventory()
	if err != nil || len(groups) != 1 {
		t.Fatalf("inventory = %d groups, err %v", len(groups), err)
	}
	importPreview, err := service.Preview([]string{groups[0].ID}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ApplyDetailed(importPreview); err != nil {
		t.Fatal(err)
	}
	migration, err := service.PreviewMigration([]string{groups[0].Sources[0].ID}, adapter.MaterializeSymlinkOnly)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(entry); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(second, entry); err != nil {
		t.Skipf("replace source link: %v", err)
	}
	if _, err := service.ApplyMigrationUntil(migration, time.Time{}); !errors.Is(err, ErrStalePreview) {
		t.Fatalf("apply stale migration error = %v, want ErrStalePreview", err)
	}
	if target, err := os.Readlink(entry); err != nil || target != second {
		t.Fatalf("stale migration changed the new link owner: target=%q err=%v", target, err)
	}
}

func TestMigrationPreviewBlocksPendingImportForSelectedRepositoryIdentity(t *testing.T) {
	fixture := newSyncFixture(t)
	source := filepath.Join(t.TempDir(), "external", "demo")
	writeSkill(t, source, "import is awaiting discovery commit")
	entry := filepath.Join(fixture.home, ".claude", "skills", "demo")
	if err := os.MkdirAll(filepath.Dir(entry), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(source, entry); err != nil {
		t.Skipf("create source link: %v", err)
	}
	service := fixture.service(t)
	inventory, groups, err := service.Inventory()
	if err != nil || len(groups) != 1 || len(inventory.Sources) != 1 || !inventory.Sources[0].MigrationEligible {
		t.Fatalf("migration-eligible source inventory = %#v groups=%#v err=%v", inventory.Sources, groups, err)
	}
	importPreview, err := service.Preview([]string{groups[0].ID}, nil)
	if err != nil {
		t.Fatal(err)
	}
	service.BeforeDiscoveryCommit = func([]string) error { return errors.New("injected discovery commit failure") }
	if _, err := service.ApplyDetailed(importPreview); err == nil {
		t.Fatal("injected discovery commit failure unexpectedly succeeded")
	}
	if _, err := service.PreviewMigration([]string{inventory.Sources[0].ID}, adapter.MaterializeSymlinkOnly); !errors.Is(err, ErrMigrationBlocked) || !strings.Contains(err.Error(), "unresolved import") {
		t.Fatalf("migration preview with pending import = %v, want unresolved-import conflict", err)
	}
	if target, err := os.Readlink(entry); err != nil || target != source {
		t.Fatalf("blocked migration changed source reference: target=%q err=%v", target, err)
	}
}

func TestMigrationApplyRechecksImportBatchCreatedAfterPreview(t *testing.T) {
	fixture := newSyncFixture(t)
	source := filepath.Join(t.TempDir(), "external", "demo")
	writeSkill(t, source, "migration must wait for import evidence")
	entry := filepath.Join(fixture.home, ".claude", "skills", "demo")
	if err := os.MkdirAll(filepath.Dir(entry), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(source, entry); err != nil {
		t.Skipf("create source link: %v", err)
	}
	service := fixture.service(t)
	inventory, groups, err := service.Inventory()
	if err != nil || len(groups) != 1 {
		t.Fatalf("Inventory() = %d groups, err %v", len(groups), err)
	}
	firstImport, err := service.Preview([]string{groups[0].ID}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ApplyDetailed(firstImport); err != nil {
		t.Fatal(err)
	}
	migration, err := service.PreviewMigration([]string{inventory.Sources[0].ID}, adapter.MaterializeSymlinkOnly)
	if err != nil {
		t.Fatal(err)
	}
	store := operation.NewBatchStore(filepath.Join(service.syncRoot, "batches"))
	pending, err := store.Prepare(operation.Batch{
		ID: "pending-import-after-preview", Kind: "import", Scope: "local",
		Resources: []string{filepath.Join(fixture.repository, migration.Entries[0].Identifier)},
		Steps:     []operation.BatchStep{{ID: "pending-version", Resource: filepath.Join(fixture.repository, migration.Entries[0].Identifier), Action: "publish:" + migration.Entries[0].VersionKey, ExpectedAfter: migration.Entries[0].Checksum}},
	})
	if err != nil {
		t.Fatal(err)
	}
	pending.Status = operation.BatchNeedsRecovery
	if err := store.Update(pending); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ApplyMigrationUntil(migration, time.Time{}); !errors.Is(err, ErrStalePreview) || !errors.Is(err, operation.ErrPendingBatch) {
		t.Fatalf("migration apply with later pending import = %v, want stale pending-batch error", err)
	}
	if target, err := os.Readlink(entry); err != nil || target != source {
		t.Fatalf("stale migration changed source reference: target=%q err=%v", target, err)
	}
}

func TestMigrationBindsSameIdentifierToEachTargetVersionAndUndoRestoresLinks(t *testing.T) {
	fixture := newSyncFixture(t)
	claudeSource := filepath.Join(t.TempDir(), "external", "demo")
	codexSource := filepath.Join(t.TempDir(), "external", "demo")
	writeSkill(t, claudeSource, "Claude version")
	writeSkill(t, codexSource, "Codex version")
	claudeEntry := filepath.Join(fixture.home, ".claude", "skills", "demo")
	codexEntry := filepath.Join(fixture.home, ".codex", "skills", "demo")
	for _, entry := range []string{claudeEntry, codexEntry} {
		if err := os.MkdirAll(filepath.Dir(entry), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(claudeSource, claudeEntry); err != nil {
		t.Skipf("create Claude source link: %v", err)
	}
	if err := os.Symlink(codexSource, codexEntry); err != nil {
		t.Skipf("create Codex source link: %v", err)
	}
	service := fixture.service(t)
	_, groups, err := service.Inventory()
	if err != nil || len(groups) != 2 {
		t.Fatalf("divergent same-ID inventory groups = %d, err %v", len(groups), err)
	}
	groupIDs := []string{groups[0].ID, groups[1].ID}
	importPreview, err := service.Preview(groupIDs, map[string]string{"demo": groups[0].ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ApplyDetailed(importPreview); err != nil {
		t.Fatal(err)
	}
	selected := []string{}
	for _, group := range groups {
		if group.Sources[0].Target == adapter.ClaudeCode || group.Sources[0].Target == adapter.Codex {
			selected = append(selected, group.Sources[0].ID)
		}
	}
	if len(selected) != 2 {
		t.Fatalf("adapter-specific references = %#v", selected)
	}
	migration, err := service.PreviewMigration(selected, adapter.MaterializeSymlinkOnly)
	if err != nil {
		t.Fatal(err)
	}
	if len(migration.Entries) != 2 || migration.Entries[0].VersionKey == migration.Entries[1].VersionKey {
		t.Fatalf("migration did not preserve distinct exact versions: %#v", migration.Entries)
	}
	result, err := service.ApplyMigrationUntil(migration, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	for _, pair := range []struct{ path, source string }{{claudeEntry, claudeSource}, {codexEntry, codexSource}} {
		var expected string
		for _, entry := range result.Entries {
			if entry.RealPath == pair.source {
				expected = entry.VersionPath
			}
		}
		target, err := os.Readlink(pair.path)
		if err != nil || expected == "" || target != expected {
			t.Fatalf("%s target=%q, want exact version %q (err=%v)", pair.path, target, expected, err)
		}
	}
	journal := operation.GlobalJournal(fixture.home)
	entry, found, err := journal.Latest()
	if err != nil || !found || entry.BatchID != result.BatchID || len(entry.LinkDependencies) != 2 {
		t.Fatalf("migration Undo evidence = %#v (found=%v err=%v)", entry, found, err)
	}
	undo := lifecycle.New(service.repositoryRoot, journal, func(operation.Plan) bool { return true })
	undo.BatchStore = operation.NewBatchStore(filepath.Join(service.syncRoot, "batches"))
	if err := undo.UndoWithFingerprints(nil); err != nil {
		t.Fatalf("undo per-target migration: %v", err)
	}
	for _, pair := range []struct{ path, source string }{{claudeEntry, claudeSource}, {codexEntry, codexSource}} {
		target, err := os.Readlink(pair.path)
		if err != nil || target != pair.source {
			t.Fatalf("Undo %s target=%q err=%v, want original %q", pair.path, target, err, pair.source)
		}
	}
}

func TestMigrationRollbackIsAtomicWithinProjectScope(t *testing.T) {
	fixture := newSyncFixture(t)
	entries := []struct {
		target adapter.Target
		id     string
	}{
		{target: adapter.ClaudeCode, id: "claude-demo"},
		{target: adapter.Codex, id: "codex-demo"},
	}
	paths := map[string]string{}
	groupIDs := []string{}
	for _, item := range entries {
		source := filepath.Join(t.TempDir(), "external", item.id)
		writeSkill(t, source, item.id)
		adapterImpl, ok := adapter.For(item.target)
		if !ok {
			t.Fatalf("adapter %q is unavailable", item.target)
		}
		entry := adapterImpl.ProjectSkillPath(fixture.project, item.id)
		if err := os.MkdirAll(filepath.Dir(entry), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(source, entry); err != nil {
			t.Skipf("create source reference: %v", err)
		}
		paths[entry] = source
	}

	service := fixture.service(t)
	inventory, groups, err := service.Inventory()
	if err != nil || len(groups) != len(entries) {
		t.Fatalf("inventory groups = %d, err %v", len(groups), err)
	}
	sourceIDs := []string{}
	for _, group := range groups {
		groupIDs = append(groupIDs, group.ID)
		for _, source := range inventory.Sources {
			if source.Identifier == group.Identifier {
				sourceIDs = append(sourceIDs, source.ID)
			}
		}
	}
	importPreview, err := service.Preview(groupIDs, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ApplyDetailed(importPreview); err != nil {
		t.Fatal(err)
	}
	migration, err := service.PreviewMigration(sourceIDs, adapter.MaterializeSymlinkOnly)
	if err != nil {
		t.Fatal(err)
	}
	life := service.migrationLifecycle(migration.Scope, nil)
	life.Materialization = adapter.MaterializationMode(migration.SyncMode)
	published := 0
	life.BeforePublish = func(string) error {
		published++
		if published == 1 {
			return os.ErrPermission
		}
		return nil
	}
	_, err = life.ApplyMany(migration.root, migration.skills, migration.targets, migration.lifecyclePlan, true, migration.options)
	if !errors.Is(err, os.ErrPermission) || published != 1 {
		t.Fatalf("injected second-entry publication failure = %v after %d publishes", err, published)
	}
	for entry, source := range paths {
		if target, err := os.Readlink(entry); err != nil || target != source {
			t.Fatalf("scope rollback left %s at target %q, err %v; want original %s", entry, target, err, source)
		}
	}
	batches, err := operation.NewBatchStore(filepath.Join(service.syncRoot, "batches")).List()
	var migrationBatch *operation.Batch
	for i := range batches {
		if batches[i].Kind == MigrationOperation {
			migrationBatch = &batches[i]
		}
	}
	if err != nil || migrationBatch == nil || migrationBatch.Status != operation.BatchRolledBack {
		t.Fatalf("migration recovery batches = %#v, err %v; want a rolled-back migration batch", batches, err)
	}
}

func TestRecoveryRestoresDestinationMovedIntoDurablePublicationStage(t *testing.T) {
	fixture := newSyncFixture(t)
	source := filepath.Join(t.TempDir(), "external", "demo")
	writeSkill(t, source, "interrupted migration")
	entry := filepath.Join(fixture.project, ".claude", "skills", "demo")
	if err := os.MkdirAll(filepath.Dir(entry), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(source, entry); err != nil {
		t.Skipf("create source link: %v", err)
	}
	service := fixture.service(t)
	_, groups, err := service.Inventory()
	if err != nil || len(groups) != 1 {
		t.Fatalf("Inventory() = %d groups, err %v", len(groups), err)
	}
	importPreview, err := service.Preview([]string{groups[0].ID}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ApplyDetailed(importPreview); err != nil {
		t.Fatal(err)
	}
	migration, err := service.PreviewMigration([]string{groups[0].Sources[0].ID}, adapter.MaterializeSymlinkOnly)
	if err != nil {
		t.Fatal(err)
	}
	life := service.migrationLifecycle(migration.Scope, func(operation.Plan) bool { return true })
	life.Materialization = adapter.MaterializeSymlinkOnly
	life.BeforeFinalPublish = func() error { panic("simulated process termination after moving destination") }
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("migration did not reach the simulated crash point")
			}
		}()
		_, _ = life.ApplyMany(migration.root, migration.skills, migration.targets, migration.lifecyclePlan, true, migration.options)
	}()
	if _, err := os.Lstat(entry); !os.IsNotExist(err) {
		t.Fatalf("simulated crash did not leave destination moved away: %v", err)
	}
	store := operation.NewBatchStore(filepath.Join(service.syncRoot, "batches"))
	pending, err := store.Pending()
	if err != nil || len(pending) != 1 || pending[0].Steps[0].StagedPath == "" {
		t.Fatalf("durable staged publication state = %#v, err %v", pending, err)
	}
	preview, err := service.PreviewRecovery()
	if err != nil || len(preview.Batches) != 1 || !hasAction(preview.Batches[0].Actions, RecoveryRollback) || preview.Batches[0].Steps[0].Classification != "staged" {
		t.Fatalf("staged destination recovery preview = %#v, err %v", preview, err)
	}
	service.projectJournal.BeforeRestorePublish = func(string) error { panic("simulated recovery interruption") }
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("recovery did not reach the simulated interruption")
			}
		}()
		_, _ = service.ApplyRecovery(pending[0].ID, RecoveryRollback)
	}()
	service.projectJournal.BeforeRestorePublish = nil
	preview, err = service.PreviewRecovery()
	if err != nil || len(preview.Batches) != 1 || !hasAction(preview.Batches[0].Actions, RecoveryRollback) || preview.Batches[0].Steps[0].Classification != "staged" {
		t.Fatalf("recovery retry preview = %#v, err %v", preview, err)
	}
	if _, err := service.ApplyRecovery(pending[0].ID, RecoveryRollback); err != nil {
		t.Fatalf("restore staged destination: %v", err)
	}
	if target, err := os.Readlink(entry); err != nil || target != source {
		t.Fatalf("recovered source link = %q, err %v", target, err)
	}
	if _, err := os.Lstat(filepath.Dir(pending[0].Steps[0].StagedPath)); !os.IsNotExist(err) {
		t.Fatalf("recovery staging root was not removed: %v", err)
	}
}

func TestUndoRecoveryRestoresCurrentStateMovedIntoJournalStage(t *testing.T) {
	fixture := newSyncFixture(t)
	source := filepath.Join(t.TempDir(), "external", "demo")
	writeSkill(t, source, "interrupted undo")
	entry := filepath.Join(fixture.project, ".claude", "skills", "demo")
	if err := os.MkdirAll(filepath.Dir(entry), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(source, entry); err != nil {
		t.Skipf("create source link: %v", err)
	}
	service := fixture.service(t)
	_, groups, err := service.Inventory()
	if err != nil || len(groups) != 1 {
		t.Fatalf("Inventory() = %d groups, err %v", len(groups), err)
	}
	importPreview, err := service.Preview([]string{groups[0].ID}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ApplyDetailed(importPreview); err != nil {
		t.Fatal(err)
	}
	migration, err := service.PreviewMigration([]string{groups[0].Sources[0].ID}, adapter.MaterializeSymlinkOnly)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ApplyMigrationUntil(migration, time.Time{}); err != nil {
		t.Fatal(err)
	}
	managedTarget, err := os.Readlink(entry)
	if err != nil {
		t.Fatal(err)
	}
	life := service.migrationLifecycle(migration.Scope, func(operation.Plan) bool { return true })
	service.projectJournal.BeforeRestorePublish = func(string) error { panic("simulated Undo interruption") }
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("Undo did not reach the simulated interruption")
			}
		}()
		_ = life.Undo()
	}()
	service.projectJournal.BeforeRestorePublish = nil
	if _, err := os.Lstat(entry); !os.IsNotExist(err) {
		t.Fatalf("simulated Undo interruption did not move current path away: %v", err)
	}
	preview, err := service.PreviewRecovery()
	if err != nil || len(preview.Batches) != 1 || !hasAction(preview.Batches[0].Actions, RecoveryRollback) || preview.Batches[0].Steps[0].Classification != "staged" {
		t.Fatalf("interrupted Undo recovery preview = %#v, err %v", preview, err)
	}
	if _, err := service.ApplyRecovery(preview.Batches[0].BatchID, RecoveryRollback); err != nil {
		t.Fatalf("recover interrupted Undo to its current state: %v", err)
	}
	if target, err := os.Readlink(entry); err != nil || target != managedTarget {
		t.Fatalf("restored current link = %q, err %v", target, err)
	}
	if _, found, err := service.projectJournal.Latest(); err != nil || !found {
		t.Fatalf("interrupted Undo removed its journal entry: found=%v err=%v", found, err)
	}
}

func TestProjectReferenceThroughMigratedGlobalEntryRemainsMigratable(t *testing.T) {
	fixture := newSyncFixture(t)
	source := filepath.Join(t.TempDir(), "external", "demo")
	writeSkill(t, source, "indirectly referenced Skill")
	globalAdapter, ok := adapter.ForAgent(adapter.ClaudeCode)
	if !ok {
		t.Fatal("Claude adapter is unavailable")
	}
	projectAdapter, ok := adapter.For(adapter.Codex)
	if !ok {
		t.Fatal("Codex adapter is unavailable")
	}
	globalEntry := globalAdapter.GlobalSkillPath(fixture.home, "demo")
	projectEntry := projectAdapter.ProjectSkillPath(fixture.project, "demo")
	for _, entry := range []string{globalEntry, projectEntry} {
		if err := os.MkdirAll(filepath.Dir(entry), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(source, globalEntry); err != nil {
		t.Skipf("create global source reference: %v", err)
	}
	if err := os.Symlink(globalEntry, projectEntry); err != nil {
		t.Skipf("create project reference through global entry: %v", err)
	}
	service := fixture.service(t)
	inventory, groups, err := service.Inventory()
	if err != nil || len(groups) != 1 {
		t.Fatalf("initial inventory = %d groups, err %v", len(groups), err)
	}
	importPreview, err := service.Preview([]string{groups[0].ID}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ApplyDetailed(importPreview); err != nil {
		t.Fatal(err)
	}
	var globalSourceID string
	for _, item := range inventory.Sources {
		if item.Scope == ScopeGlobal && item.Target == adapter.ClaudeCode {
			globalSourceID = item.ID
		}
	}
	if globalSourceID == "" {
		t.Fatal("global reference was not discovered")
	}
	globalPreview, err := service.PreviewMigration([]string{globalSourceID}, adapter.MaterializeSymlinkOnly)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ApplyMigrationUntil(globalPreview, time.Time{}); err != nil {
		t.Fatalf("migrate global entry: %v", err)
	}

	updated, _, err := service.Inventory()
	if err != nil {
		t.Fatal(err)
	}
	var projectSource *Source
	for i := range updated.Sources {
		if updated.Sources[i].Scope == ScopeProject && updated.Sources[i].Target == adapter.Codex {
			projectSource = &updated.Sources[i]
		}
	}
	if projectSource == nil || !projectSource.MigrationEligible {
		t.Fatalf("project reference through migrated global entry = %#v, want independently migratable", projectSource)
	}
	projectPreview, err := service.PreviewMigration([]string{projectSource.ID}, adapter.MaterializeSymlinkOnly)
	if err != nil {
		t.Fatalf("preview project migration: %v", err)
	}
	if _, err := service.ApplyMigrationUntil(projectPreview, time.Time{}); err != nil {
		t.Fatalf("migrate project entry: %v", err)
	}
	projectTarget, err := os.Readlink(projectEntry)
	if err != nil || !strings.Contains(projectTarget, filepath.Join("repo", "demo", "versions")) {
		t.Fatalf("project entry target = %q, err %v; want a pinned library version", projectTarget, err)
	}
}
