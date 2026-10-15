package skillsync

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/AllenMuu/skill-manager/internal/operation"
	"github.com/AllenMuu/skill-manager/internal/skillrepo"
)

func TestImportGroupsIdenticalSourcesAndPreservesSources(t *testing.T) {
	fixture := newSyncFixture(t)
	first := filepath.Join(fixture.home, ".claude", "skills", "demo")
	second := filepath.Join(fixture.project, ".agents", "skills", "demo")
	writeSkill(t, first, "same")
	writeSkill(t, second, "same")
	service := fixture.service(t)

	inventory, groups, err := service.Inventory()
	if err != nil {
		t.Fatal(err)
	}
	if len(inventory.Sources) != 2 || len(groups) != 1 || len(groups[0].Sources) != 2 {
		t.Fatalf("identical sources did not collapse into one group: sources=%+v groups=%+v", inventory.Sources, groups)
	}
	preview, err := service.Preview([]string{groups[0].ID}, nil)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := service.Apply(preview)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Metadata.SourceType != "local-web-sync" || len(entries[0].Metadata.SourceOrigins) != 2 {
		t.Fatalf("import provenance = %+v", entries)
	}
	if snapshots, err := os.ReadDir(filepath.Join(service.syncRoot, "import-snapshots")); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	} else {
		for _, snapshot := range snapshots {
			if snapshot.IsDir() {
				t.Errorf("successful import retained rollback snapshot %s", snapshot.Name())
			}
		}
	}
	if _, err := os.Stat(filepath.Join(first, "SKILL.md")); err != nil {
		t.Fatalf("source was modified: %v", err)
	}
	repository, _ := skillrepo.Open(fixture.repository)
	if _, err := repository.Resolve("demo", ""); err != nil {
		t.Fatalf("imported repository version is missing: %v", err)
	}
}

func TestImportFromExternalLinkUsesExactTargetAndReturnsCommittedEvidence(t *testing.T) {
	fixture := newSyncFixture(t)
	target := filepath.Join(t.TempDir(), "external", "demo")
	writeSkill(t, target, "linked source")
	if err := os.MkdirAll(filepath.Join(fixture.home, ".claude", "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(fixture.home, ".claude", "skills", "demo")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("create source symlink: %v", err)
	}
	// A neighboring directory must not participate in exact-target metadata
	// inspection or import.
	if err := os.Mkdir(filepath.Join(filepath.Dir(target), "not-a-skill"), 0o755); err != nil {
		t.Fatal(err)
	}
	service := fixture.service(t)
	_, groups, err := service.Inventory()
	if err != nil || len(groups) != 1 {
		t.Fatalf("Inventory() = %d groups, err %v", len(groups), err)
	}
	preview, err := service.Preview([]string{groups[0].ID}, nil)
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.ApplyDetailed(preview)
	if err != nil {
		t.Fatal(err)
	}
	if result.BatchID == "" || len(result.Entries) != 1 || len(result.SourceRecordIDs) != 1 {
		t.Fatalf("import result lacks exact batch/version/source evidence: %#v", result)
	}
	entry := result.Entries[0]
	if entry.Key != groups[0].VersionKey || entry.Path == "" {
		t.Fatalf("imported version = %#v, want exact selected version %q", entry, groups[0].VersionKey)
	}
	record, ok, err := NewDiscoveryStore(filepath.Join(service.syncRoot, "discoveries")).Get(result.SourceRecordIDs[0])
	if err != nil || !ok || record.Status != DiscoveryCommitted || record.RealPath != target || record.EntryPath != filepath.Join(service.home, ".claude", "skills", "demo") || record.VersionKey != entry.Key {
		t.Fatalf("committed source record = %#v (ok=%v, err=%v)", record, ok, err)
	}
}

func TestImportRequiresExplicitChoiceWhenEntryAndResolvedIdentityDiffer(t *testing.T) {
	for _, choice := range []string{"entry", "resolved"} {
		t.Run(choice, func(t *testing.T) {
			fixture := newSyncFixture(t)
			real := filepath.Join(t.TempDir(), "external", "real-skill")
			writeSkill(t, real, "content")
			entry := filepath.Join(fixture.home, ".claude", "skills", "alias-skill")
			if err := os.MkdirAll(filepath.Dir(entry), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(real, entry); err != nil {
				t.Skipf("create source symlink: %v", err)
			}
			service := fixture.service(t)
			inventory, groups, err := service.Inventory()
			if err != nil {
				t.Fatal(err)
			}
			if len(groups) != 1 || len(inventory.Sources) == 0 {
				t.Fatalf("identity-conflict source was not importable: sources=%+v groups=%+v", inventory.Sources, groups)
			}
			source := groups[0].Sources[0]
			if !source.Eligible || !source.IdentityConflict || source.MigrationEligible || source.ResolvedIdentifier != "real-skill" {
				t.Fatalf("identity conflict was not represented safely: %+v", source)
			}
			unresolved, err := service.Preview([]string{groups[0].ID}, nil)
			if err != nil {
				t.Fatal(err)
			}
			if len(unresolved.IdentityChoiceRequired) != 1 || unresolved.IdentityChoiceRequired[0] != source.ID {
				t.Fatalf("missing explicit choice was not returned: %+v", unresolved)
			}
			if _, err := service.Apply(unresolved); !errors.Is(err, ErrNeedsIdentityChoice) {
				t.Fatalf("Apply without an identity choice error = %v", err)
			}
			preview, err := service.PreviewWithIdentityChoices([]string{groups[0].ID}, nil, map[string]string{source.ID: choice})
			if err != nil {
				t.Fatal(err)
			}
			entries, err := service.Apply(preview)
			if err != nil {
				t.Fatal(err)
			}
			wantIdentifier := "alias-skill"
			if choice == "resolved" {
				wantIdentifier = "real-skill"
			}
			if len(entries) != 1 || entries[0].Identifier != wantIdentifier {
				t.Fatalf("imported identity = %+v, want %q", entries, wantIdentifier)
			}
			if _, err := os.Stat(entry); err != nil {
				t.Fatalf("source link was modified: %v", err)
			}
		})
	}
}

func TestImportRecordsSourceWhenVersionAlreadyExists(t *testing.T) {
	fixture := newSyncFixture(t)
	source := filepath.Join(fixture.home, ".claude", "skills", "demo")
	writeSkill(t, source, "reused source")
	service := fixture.service(t)
	_, groups, err := service.Inventory()
	if err != nil {
		t.Fatal(err)
	}
	preview, err := service.Preview([]string{groups[0].ID}, nil)
	if err != nil {
		t.Fatal(err)
	}
	first, err := service.ApplyDetailed(preview)
	if err != nil {
		t.Fatal(err)
	}
	preview, err = service.Preview([]string{groups[0].ID}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !preview.Groups[0].AlreadyPresent {
		t.Fatal("second preview did not recognize the existing version")
	}
	second, err := service.ApplyDetailed(preview)
	if err != nil {
		t.Fatal(err)
	}
	if second.BatchID == "" || second.BatchID == first.BatchID || len(second.Entries) != 1 || second.Entries[0].Key != first.Entries[0].Key || len(second.SourceRecordIDs) != 1 {
		t.Fatalf("reused import lost exact version/source evidence: first=%#v second=%#v", first, second)
	}
	store := NewDiscoveryStore(filepath.Join(service.syncRoot, "discoveries"))
	record, ok, err := store.Get(second.SourceRecordIDs[0])
	if err != nil || !ok || record.Status != DiscoveryCommitted || !record.ReusedVersion || record.VersionKey != first.Entries[0].Key {
		t.Fatalf("reused source record = %#v (ok=%v, err=%v)", record, ok, err)
	}
}

func TestImportSourceRecordCommitFailureRetainsPublishedVersionAndRecoveryEvidence(t *testing.T) {
	fixture := newSyncFixture(t)
	source := filepath.Join(fixture.home, ".claude", "skills", "demo")
	writeSkill(t, source, "published before evidence commit")
	service := fixture.service(t)
	_, groups, err := service.Inventory()
	if err != nil {
		t.Fatal(err)
	}
	preview, err := service.Preview([]string{groups[0].ID}, nil)
	if err != nil {
		t.Fatal(err)
	}
	service.BeforeDiscoveryCommit = func([]string) error { return errors.New("injected discovery commit failure") }
	if _, err := service.ApplyDetailed(preview); err == nil {
		t.Fatal("injected source record commit failure unexpectedly succeeded")
	}
	repository, _ := skillrepo.Open(fixture.repository)
	entry, err := repository.Resolve("demo", groups[0].VersionKey)
	if err != nil {
		t.Fatalf("successfully published immutable version was removed: %v", err)
	}
	batchStore := operation.NewBatchStore(filepath.Join(service.syncRoot, "batches"))
	pending, err := batchStore.Pending()
	if err != nil || len(pending) != 1 || pending[0].Status != operation.BatchNeedsRecovery {
		t.Fatalf("pending import batches = %#v, err %v", pending, err)
	}
	records, err := NewDiscoveryStore(filepath.Join(service.syncRoot, "discoveries")).ForBatch(pending[0].ID)
	if err != nil || len(records) != 1 || records[0].Status != DiscoveryStaged || records[0].VersionKey != entry.Key {
		t.Fatalf("staged recovery evidence = %#v, err %v", records, err)
	}
	// Simulate a crash after the immutable directory rename but before the
	// repository metadata rename. Recovery must reconstruct the index from its
	// durable import intent before it commits source discovery evidence.
	if err := os.Remove(filepath.Join(fixture.repository, "demo", skillrepo.MetadataFilename)); err != nil {
		t.Fatalf("remove repository metadata to simulate crash window: %v", err)
	}
	previewRecovery, err := service.PreviewRecovery()
	if err != nil || len(previewRecovery.Batches) != 1 || !hasAction(previewRecovery.Batches[0].Actions, RecoveryResolve) {
		t.Fatalf("recovery preview = %#v, err %v", previewRecovery, err)
	}
	if _, err := service.ApplyRecovery(pending[0].ID, RecoveryResolve); err != nil {
		t.Fatalf("recover import evidence: %v", err)
	}
	recoveredEntry, err := repository.Resolve("demo", entry.Key)
	if err != nil || recoveredEntry.Metadata.Checksum != records[0].Checksum || recoveredEntry.Metadata.SourceType != "local-web-sync" {
		t.Fatalf("recovery did not restore repository metadata: entry=%#v err=%v", recoveredEntry, err)
	}
	recovered, ok, err := batchStore.Get(pending[0].ID)
	if err != nil || !ok || recovered.Status != operation.BatchCommitted {
		t.Fatalf("recovered batch = %#v (ok=%v, err=%v)", recovered, ok, err)
	}
	record, ok, err := NewDiscoveryStore(filepath.Join(service.syncRoot, "discoveries")).Get(records[0].ID)
	if err != nil || !ok || record.Status != DiscoveryCommitted {
		t.Fatalf("recovered discovery record = %#v (ok=%v, err=%v)", record, ok, err)
	}
}

func TestImportRecoveryRemovesEmptyIdentityCreatedBeforeVersionPublication(t *testing.T) {
	fixture := newSyncFixture(t)
	source := filepath.Join(fixture.home, ".claude", "skills", "demo")
	writeSkill(t, source, "interrupted before version publication")
	service := fixture.service(t)
	_, groups, err := service.Inventory()
	if err != nil || len(groups) != 1 {
		t.Fatalf("source groups = %#v, err %v", groups, err)
	}
	group := groups[0]
	metadata, _, err := versionMetadata(group)
	if err != nil {
		t.Fatal(err)
	}
	batchID := "prepublish-import"
	recordID := discoveryRecordID(batchID, group.Sources[0].ID)
	store := operation.NewBatchStore(filepath.Join(service.syncRoot, "batches"))
	identity := filepath.Join(fixture.repository, group.Identifier)
	batch, err := store.Prepare(operation.Batch{
		ID: batchID, Kind: "import", Scope: "local", Resources: []string{identity},
		SourceRecordIDs: []string{recordID},
		Steps:           []operation.BatchStep{{ID: group.ID, Resource: identity, Action: "publish:" + group.VersionKey, BeforeState: "absent", ExpectedAfter: group.Checksum}},
	})
	if err != nil {
		t.Fatal(err)
	}
	batch.Status = operation.BatchPublishing
	if err := store.Update(batch); err != nil {
		t.Fatal(err)
	}
	discovery := NewDiscoveryStore(filepath.Join(service.syncRoot, "discoveries"))
	if _, err := discovery.Prepare(DiscoveryRecord{
		ID: recordID, BatchID: batchID, Scope: group.Sources[0].Scope,
		Location: group.Sources[0].Location, EntryPath: group.Sources[0].Path, EntryType: group.Sources[0].EntryType,
		OriginalLink: group.Sources[0].OriginalLink, LinkChain: group.Sources[0].LinkChain,
		RealPath: group.Sources[0].RealPath, Checksum: group.Checksum, Identifier: group.Identifier,
		VersionKey: group.VersionKey, VersionMetadata: metadata, ObservedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	stage := filepath.Join(identity, "versions", ".stage-interrupted")
	if err := os.MkdirAll(stage, 0o755); err != nil {
		t.Fatal(err)
	}

	preview, err := service.PreviewRecovery()
	if err != nil || len(preview.Batches) != 1 || !hasAction(preview.Batches[0].Actions, RecoveryResolve) {
		t.Fatalf("recovery preview = %#v, err %v", preview, err)
	}
	if _, err := service.ApplyRecovery(batchID, RecoveryResolve); err != nil {
		t.Fatalf("resolve interrupted pre-publication import: %v", err)
	}
	if _, err := os.Lstat(identity); !os.IsNotExist(err) {
		t.Fatalf("empty transaction-owned identity remains after rollback: %v", err)
	}
	importPreview, err := service.Preview([]string{group.ID}, nil)
	if err != nil {
		t.Fatalf("preview retry after recovery: %v", err)
	}
	if _, err := service.ApplyDetailed(importPreview); err != nil {
		t.Fatalf("retry import after recovery: %v", err)
	}
}

func TestImportRecoveryPreservesUnexpectedNonEmptyStage(t *testing.T) {
	root := t.TempDir()
	identity := filepath.Join(root, "demo")
	stage := filepath.Join(identity, "versions", ".stage-interrupted")
	stagedFile := filepath.Join(stage, "SKILL.md")
	if err := os.MkdirAll(stage, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stagedFile, []byte("unpublished staged content"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := removeEmptyUnpublishedIdentity(identity)
	if !errors.Is(err, ErrRecoveryConflict) {
		t.Fatalf("removeEmptyUnpublishedIdentity() error = %v, want recovery conflict", err)
	}
	contents, readErr := os.ReadFile(stagedFile)
	if readErr != nil || string(contents) != "unpublished staged content" {
		t.Fatalf("unexpected non-empty stage was changed: %q, %v", contents, readErr)
	}
}

func TestConcurrentImportsSerializeRepositoryPublication(t *testing.T) {
	fixture := newSyncFixture(t)
	writeSkill(t, filepath.Join(fixture.home, ".claude", "skills", "demo-a"), "source a")
	writeSkill(t, filepath.Join(fixture.project, ".agents", "skills", "demo-b"), "source b")
	first := fixture.service(t)
	second := fixture.service(t)
	_, groups, err := first.Inventory()
	if err != nil {
		t.Fatal(err)
	}
	previews := make(map[string]Preview, len(groups))
	for _, group := range groups {
		preview, err := first.Preview([]string{group.ID}, nil)
		if err != nil {
			t.Fatal(err)
		}
		previews[group.Identifier] = preview
	}

	firstAtPublish := make(chan struct{})
	releaseFirst := make(chan struct{})
	secondPublished := make(chan struct{})
	first.BeforeImportPublish = func(int, skillrepo.Entry) error {
		close(firstAtPublish)
		<-releaseFirst
		return nil
	}
	second.BeforeImportPublish = func(int, skillrepo.Entry) error {
		close(secondPublished)
		return nil
	}
	type applyResult struct{ err error }
	results := make(chan applyResult, 2)
	go func() {
		_, err := first.Apply(previews["demo-a"])
		results <- applyResult{err: err}
	}()
	select {
	case <-firstAtPublish:
	case <-time.After(2 * time.Second):
		t.Fatal("first import did not reach repository publication")
	}
	go func() {
		_, err := second.Apply(previews["demo-b"])
		results <- applyResult{err: err}
	}()

	secondPublishedEarly := false
	select {
	case <-secondPublished:
		secondPublishedEarly = true
	case <-time.After(50 * time.Millisecond):
	}
	close(releaseFirst)
	firstResult, secondResult := <-results, <-results
	if firstResult.err != nil || secondResult.err != nil {
		t.Fatalf("concurrent import errors: first=%v second=%v", firstResult.err, secondResult.err)
	}
	if secondPublishedEarly {
		t.Fatal("second import published while the first held the repository transaction")
	}
	select {
	case <-secondPublished:
	case <-time.After(2 * time.Second):
		t.Fatal("second import did not publish after the first transaction released its lock")
	}
	repository, _ := skillrepo.Open(fixture.repository)
	for _, identifier := range []string{"demo-a", "demo-b"} {
		if _, err := repository.Resolve(identifier, ""); err != nil {
			t.Fatalf("concurrent import lost %s: %v", identifier, err)
		}
	}
}

func TestImportDivergentVariantsRequireAndHonorCurrentChoice(t *testing.T) {
	fixture := newSyncFixture(t)
	global := filepath.Join(fixture.home, ".claude", "skills", "demo")
	project := filepath.Join(fixture.project, ".agents", "skills", "demo")
	writeSkill(t, global, "global version")
	writeSkill(t, project, "project version")
	service := fixture.service(t)
	_, groups, err := service.Inventory()
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 2 {
		t.Fatalf("variant count = %d, want 2", len(groups))
	}
	ids := []string{groups[0].ID, groups[1].ID}
	preview, err := service.Preview(ids, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(preview.CurrentChoiceRequired) != 1 || preview.CurrentChoiceRequired[0] != "demo" {
		t.Fatalf("preview choices = %+v", preview.CurrentChoiceRequired)
	}
	if _, err := service.Apply(preview); !errors.Is(err, ErrNeedsChoice) {
		t.Fatalf("apply without current choice = %v", err)
	}
	choices := map[string]string{"demo": groups[1].ID}
	preview, err = service.Preview(ids, choices)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := service.Apply(preview)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("import entries = %d, want both variants", len(entries))
	}
	repository, _ := skillrepo.Open(fixture.repository)
	current, err := repository.Resolve("demo", "")
	if err != nil {
		t.Fatal(err)
	}
	if current.Key != groups[1].VersionKey {
		t.Fatalf("current key = %s, want explicit choice %s", current.Key, groups[1].VersionKey)
	}
}

func TestImportIsNoOpForExistingChecksumAndRejectsStaleRepository(t *testing.T) {
	fixture := newSyncFixture(t)
	source := filepath.Join(fixture.home, ".codex", "skills", "demo")
	writeSkill(t, source, "same")
	service := fixture.service(t)
	_, groups, err := service.Inventory()
	if err != nil {
		t.Fatal(err)
	}
	preview, err := service.Preview([]string{groups[0].ID}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Apply(preview); err != nil {
		t.Fatal(err)
	}
	preview, err = service.Preview([]string{groups[0].ID}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !preview.Groups[0].AlreadyPresent {
		t.Fatal("matching repository version was not marked as present")
	}
	entries, err := service.Apply(preview)
	if err != nil || len(entries) != 0 {
		t.Fatalf("existing version apply = %v, %v", entries, err)
	}

	other := filepath.Join(fixture.home, ".agent", "skills", "new-skill")
	writeSkill(t, other, "new")
	_, groups, err = service.Inventory()
	if err != nil {
		t.Fatal(err)
	}
	var selected SourceGroup
	for _, group := range groups {
		if group.Identifier == "new-skill" {
			selected = group
		}
	}
	preview, err = service.Preview([]string{selected.ID}, nil)
	if err != nil {
		t.Fatal(err)
	}
	conflictPath := filepath.Join(fixture.repository, "new-skill")
	if err := os.MkdirAll(conflictPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Apply(preview); !errors.Is(err, ErrStalePreview) {
		t.Fatalf("stale repository apply = %v", err)
	}
}

func TestImportBatchFailureRollsBackEveryPublishedIdentity(t *testing.T) {
	fixture := newSyncFixture(t)
	first := filepath.Join(fixture.home, ".claude", "skills", "first")
	second := filepath.Join(fixture.project, ".agents", "skills", "second")
	writeSkill(t, first, "first")
	writeSkill(t, second, "second")
	service := fixture.service(t)
	_, groups, err := service.Inventory()
	if err != nil {
		t.Fatal(err)
	}
	selected := make([]string, 0, len(groups))
	for _, group := range groups {
		selected = append(selected, group.ID)
	}
	preview, err := service.Preview(selected, nil)
	if err != nil {
		t.Fatal(err)
	}
	service.BeforeImportPublish = func(index int, _ skillrepo.Entry) error {
		if index == 0 {
			return errors.New("injected repository publication failure")
		}
		return nil
	}
	if _, err := service.Apply(preview); err == nil {
		t.Fatal("injected failure unexpectedly succeeded")
	}
	repository, _ := skillrepo.Open(fixture.repository)
	for _, identifier := range []string{"first", "second"} {
		if _, err := repository.Resolve(identifier, ""); !errors.Is(err, skillrepo.ErrNotFound) {
			t.Fatalf("batch rollback left %s in the repository: %v", identifier, err)
		}
	}
	entries, err := os.ReadDir(filepath.Join(service.syncRoot, "import-snapshots"))
	if err != nil {
		if !os.IsNotExist(err) {
			t.Fatal(err)
		}
		return
	}
	for _, entry := range entries {
		if entry.IsDir() {
			t.Errorf("failed batch left import snapshot directory %s", entry.Name())
		}
	}
}

type syncFixture struct{ home, project, repository string }

func newSyncFixture(t *testing.T) syncFixture {
	t.Helper()
	root := t.TempDir()
	fixture := syncFixture{home: filepath.Join(root, "home"), project: filepath.Join(root, "project"), repository: filepath.Join(root, "repo")}
	for _, path := range []string{fixture.home, fixture.project} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return fixture
}

func (f syncFixture) service(t *testing.T) *Service {
	t.Helper()
	service, err := New(f.repository, f.home, f.project, operation.GlobalJournal(f.home), operation.ProjectJournal(f.project))
	if err != nil {
		t.Fatal(err)
	}
	return service
}
