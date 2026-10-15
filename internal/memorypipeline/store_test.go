package memorypipeline

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/AllenMuu/skill-manager/internal/memory"
	"github.com/AllenMuu/skill-manager/internal/run"
)

func TestInitializeProjectCreatesFTSStoreWithoutTouchingCanonicalMemory(t *testing.T) {
	project := t.TempDir()
	canonicalPath := filepath.Join(project, ".agent-manager", "project-memory.json")
	if err := os.MkdirAll(filepath.Dir(canonicalPath), 0o700); err != nil {
		t.Fatal(err)
	}
	canonical := []byte("{\"records\":[{\"content\":\"canonical remains unchanged\"}]}\n")
	if err := os.WriteFile(canonicalPath, canonical, 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := InitializeProject(project)
	if err != nil {
		t.Fatal(err)
	}
	if !store.FTS5Available() {
		t.Fatal("SQLite store opened without FTS5")
	}
	if got, want := store.Path(), filepath.Join(project, ".agent-manager", "memory.db"); got != want {
		t.Fatalf("database path = %q, want %q", got, want)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(canonicalPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(canonical) {
		t.Fatalf("canonical Memory changed during pipeline initialization: %q", after)
	}
	reopened, err := OpenStore(filepath.Join(project, ".agent-manager", "memory.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reopened.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestOpenStoreRejectsUnsupportedSchemaVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.db")
	store, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec("PRAGMA user_version = 999"); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenStore(path); !errors.Is(err, ErrUnsupportedSchemaVersion) {
		t.Fatal("future schema version was silently opened")
	}
}

func TestOpenStoreDoesNotChangeJournalModeForUnversionedDatabaseWithTables(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE unrelated (value TEXT)`); err != nil {
		t.Fatal(err)
	}
	var before string
	if err := db.QueryRow("PRAGMA journal_mode").Scan(&before); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if before != "delete" {
		t.Fatalf("fixture journal mode = %q, want delete", before)
	}
	if _, err := OpenStore(path); !errors.Is(err, ErrUnsupportedSchemaVersion) {
		t.Fatalf("unversioned database with tables error = %v", err)
	}

	check, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer check.Close()
	var after string
	if err := check.QueryRow("PRAGMA journal_mode").Scan(&after); err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Fatalf("rejected open changed journal mode to %q", after)
	}
	var tables int
	if err := check.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name='unrelated'`).Scan(&tables); err != nil || tables != 1 {
		t.Fatalf("rejected open changed existing schema: tables=%d err=%v", tables, err)
	}
}

func TestOpenStoreMigratesSchemaVersionOneCandidateEvidenceGeneration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`CREATE TABLE projects (project_id TEXT PRIMARY KEY, created_at TEXT NOT NULL, updated_at TEXT NOT NULL, revision INTEGER NOT NULL DEFAULT 0)`,
		`CREATE TABLE skill_candidates (id TEXT PRIMARY KEY, project_id TEXT NOT NULL, procedure_key TEXT NOT NULL, content TEXT NOT NULL, confidence REAL NOT NULL, review_state TEXT NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL)`,
		"PRAGMA user_version = 1",
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	store, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var version int
	if err := store.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != schemaVersion {
		t.Fatalf("schema version = %d, %v", version, err)
	}
	for _, table := range []string{"projects", "skill_candidates"} {
		var columns int
		if err := store.db.QueryRow(`SELECT count(*) FROM pragma_table_info(?) WHERE name = 'evidence_generation'`, table).Scan(&columns); err != nil || columns != 1 {
			t.Fatalf("%s evidence_generation columns = %d, %v", table, columns, err)
		}
	}
}

func TestOpenStoreMigratesSchemaVersionTwoProcedureClaims(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("PRAGMA user_version = 2"); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	store, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var version int
	if err := store.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != schemaVersion {
		t.Fatalf("schema version = %d, %v", version, err)
	}
	var tables int
	if err := store.db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name='procedure_claims'`).Scan(&tables); err != nil || tables != 1 {
		t.Fatalf("procedure claim table = %d, %v", tables, err)
	}
}

func TestOpenProjectStoreDoesNotInitializeImplicitly(t *testing.T) {
	project := t.TempDir()
	if _, err := OpenProjectStore(project); !errors.Is(err, ErrPipelineNotInitialized) {
		t.Fatalf("opening an uninitialized project store = %v", err)
	}
	if _, err := os.Stat(filepath.Join(project, ".agent-manager")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("read path created pipeline state implicitly: %v", err)
	}
	initialized, err := InitializeProject(project)
	if err != nil {
		t.Fatal(err)
	}
	if err := initialized.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenProjectStore(project)
	if err != nil {
		t.Fatal(err)
	}
	if err := reopened.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestListEvidenceRequiresExactProject(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	first := storeEvidenceRef(t, "project-list-a", "run-list-a", "a")
	second := storeEvidenceRef(t, "project-list-b", "run-list-b", "b")
	for _, ref := range []RunEvidenceRef{first, second} {
		if _, err := store.ImportEvidence(context.Background(), ref); err != nil {
			t.Fatal(err)
		}
	}
	refs, err := store.ListEvidence(context.Background(), "project-list-a")
	if err != nil || len(refs) != 1 || refs[0].ID != first.ID {
		t.Fatalf("project evidence list = %#v, %v", refs, err)
	}
}

func TestEvidenceGenerationAdvancesOnlyForNewEvidence(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	ref := storeEvidenceRef(t, "project-evidence-generation", "run-generation", "generation")
	if generation, err := store.EvidenceGeneration(ctx, ref.ProjectID); err != nil || generation != 0 {
		t.Fatalf("initial evidence generation = %d, %v", generation, err)
	}
	if _, err := store.ImportEvidence(ctx, ref); err != nil {
		t.Fatal(err)
	}
	if generation, err := store.EvidenceGeneration(ctx, ref.ProjectID); err != nil || generation != 1 {
		t.Fatalf("generation after import = %d, %v", generation, err)
	}
	if _, err := store.ImportEvidence(ctx, ref); err != nil {
		t.Fatal(err)
	}
	if generation, err := store.EvidenceGeneration(ctx, ref.ProjectID); err != nil || generation != 1 {
		t.Fatalf("generation after idempotent import = %d, %v", generation, err)
	}
}

func TestStorePersistsEvidenceDerivedMemoryAndSummaryPerProject(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.db")
	store, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	refA := storeEvidenceRef(t, "project-a", "run-a", "alpha")
	refB := storeEvidenceRef(t, "project-b", "run-b", "beta")
	for _, ref := range []RunEvidenceRef{refA, refA, refB} {
		if _, err := store.ImportEvidence(ctx, ref); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.Consolidate(ctx, storeExtraction(refA, "shared topic alpha")); err != nil {
		t.Fatal(err)
	}
	if err := store.Consolidate(ctx, storeExtraction(refB, "shared topic beta")); err != nil {
		t.Fatal(err)
	}
	resultsA, err := store.Search(ctx, "project-a", "shared topic", 10)
	if err != nil {
		t.Fatal(err)
	}
	resultsB, err := store.Search(ctx, "project-b", "shared topic", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(resultsA) != 1 || resultsA[0].ProjectID != "project-a" || len(resultsB) != 1 || resultsB[0].ProjectID != "project-b" {
		t.Fatalf("project-scoped FTS results = %#v / %#v", resultsA, resultsB)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	summary, err := reopened.Summary(ctx, "project-a")
	if err != nil {
		t.Fatal(err)
	}
	if summary.Content == "" || len(summary.EvidenceIDs) != 1 || summary.EvidenceIDs[0] != refA.ID {
		t.Fatalf("reopened summary = %#v", summary)
	}
	if _, err := reopened.Evidence(ctx, "project-a", refB.ID); err == nil {
		t.Fatal("project reader accessed another project's evidence")
	}
}

func TestConcurrentConsolidationDeduplicatesAndPreservesEveryEvidenceLink(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.db")
	first, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	second, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	defer second.Close()
	const count = 12
	start := make(chan struct{})
	var wg sync.WaitGroup
	errs := make(chan error, count)
	for i := 0; i < count; i++ {
		store := first
		if i%2 == 1 {
			store = second
		}
		ref := storeEvidenceRef(t, "project-concurrent", "run-concurrent-"+string(rune('a'+i)), "batch")
		if _, err := store.ImportEvidence(context.Background(), ref); err != nil {
			t.Fatal(err)
		}
		wg.Add(1)
		go func(store *Store, ref RunEvidenceRef) {
			defer wg.Done()
			<-start
			errs <- store.Consolidate(context.Background(), storeExtraction(ref, "the shared finding remains stable"))
		}(store, ref)
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	results, err := first.Search(context.Background(), "project-concurrent", "shared finding", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].UsageCount != 0 || results[0].Verification != VerificationUnverified {
		t.Fatalf("deduplicated memory = %#v", results)
	}
	if len(results[0].EvidenceIDs) != count {
		t.Fatalf("provenance links = %d, want %d: %#v", len(results[0].EvidenceIDs), count, results[0].EvidenceIDs)
	}
}

func TestConsolidationSupersedesOnlyExplicitSameProjectMemory(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	oldRef := storeEvidenceRef(t, "project-a", "run-old", "old")
	newRef := storeEvidenceRef(t, "project-a", "run-new", "new")
	otherRef := storeEvidenceRef(t, "project-b", "run-other", "other")
	for _, ref := range []RunEvidenceRef{oldRef, newRef, otherRef} {
		if _, err := store.ImportEvidence(ctx, ref); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.Consolidate(ctx, storeExtraction(oldRef, "previous project decision")); err != nil {
		t.Fatal(err)
	}
	old, err := store.Search(ctx, "project-a", "previous project", 10)
	if err != nil || len(old) != 1 {
		t.Fatalf("old result = %#v, %v", old, err)
	}
	updated := storeExtraction(newRef, "replacement project decision")
	updated.Memories[0].Supersedes = []string{old[0].ID}
	if err := store.Consolidate(ctx, updated); err != nil {
		t.Fatal(err)
	}
	if err := store.Consolidate(ctx, storeExtraction(otherRef, "cross project decision")); err != nil {
		t.Fatal(err)
	}
	other, err := store.Search(ctx, "project-b", "cross project", 10)
	if err != nil || len(other) != 1 {
		t.Fatalf("other project result = %#v, %v", other, err)
	}
	oldRecord, err := store.GetDerived(ctx, "project-a", old[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if oldRecord.State != DerivedSuperseded || len(oldRecord.EvidenceIDs) != 1 {
		t.Fatalf("superseded record lost state or provenance: %#v", oldRecord)
	}
	active, err := store.Search(ctx, "project-a", "project decision", 10)
	if err != nil || len(active) != 1 || active[0].Content != "replacement project decision" {
		t.Fatalf("active results = %#v, %v", active, err)
	}
	if err := store.Consolidate(ctx, func() Extraction {
		bad := storeExtraction(newRef, "malicious cross-owner replacement")
		bad.Memories[0].Supersedes = []string{other[0].ID}
		return bad
	}()); err == nil {
		t.Fatal("cross-project supersession was accepted")
	}
}

func TestSearchKeepsTrustSeparateAndUsageIsExplicit(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	ref := storeEvidenceRef(t, "project-usage", "run-usage", "usage")
	if _, err := store.ImportEvidence(ctx, ref); err != nil {
		t.Fatal(err)
	}
	extraction := storeExtraction(ref, "stable shared retrieval phrase")
	second := extraction.Memories[0]
	second.ID = "dm_second"
	second.Field = FieldKnownIssue
	second.Type = memory.TypeExperience
	extraction.Memories = append(extraction.Memories, second)
	if err := store.Consolidate(ctx, extraction); err != nil {
		t.Fatal(err)
	}
	first, err := store.Search(ctx, ref.ProjectID, "shared retrieval", 10)
	if err != nil {
		t.Fatal(err)
	}
	secondSearch, err := store.Search(ctx, ref.ProjectID, "shared retrieval", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 2 || len(secondSearch) != 2 {
		t.Fatalf("search results = %#v / %#v", first, secondSearch)
	}
	for i := range first {
		if first[i].ID != secondSearch[i].ID || first[i].Relevance != secondSearch[i].Relevance {
			t.Fatalf("search ordering changed: %#v / %#v", first, secondSearch)
		}
		if first[i].Verification != VerificationUnverified || first[i].UsageCount != 0 {
			t.Fatalf("search altered trust or usage: %#v", first[i])
		}
	}
	if first[0].ID > first[1].ID {
		t.Fatalf("equal lexical results are not ID-stable: %#v", first)
	}
	usedAt := time.Date(2026, 10, 15, 1, 0, 0, 0, time.UTC)
	if err := store.RecordUsage(ctx, ref.ProjectID, first[0].ID, usedAt); err != nil {
		t.Fatal(err)
	}
	used, err := store.GetDerived(ctx, ref.ProjectID, first[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if used.UsageCount != 1 || used.LastUsedAt == nil || !used.LastUsedAt.Equal(usedAt) || used.Verification != VerificationUnverified {
		t.Fatalf("usage metadata changed trust or was not stored independently: %#v", used)
	}
}

func TestRetentionExpiresOldMemoryWithoutClaimingSupersession(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	first := storeEvidenceRef(t, "project-retention", "run-retention-first", "first")
	if _, err := store.ImportEvidence(ctx, first); err != nil {
		t.Fatal(err)
	}
	initial := storeExtraction(first, "retention item 000")
	initial.Memories = make([]DerivedMemory, 0, maxActiveMemories)
	for i := 0; i < maxActiveMemories; i++ {
		item := storeExtraction(first, fmt.Sprintf("retention item %03d", i)).Memories[0]
		item.ID = fmt.Sprintf("dm_retention_%03d", i)
		initial.Memories = append(initial.Memories, item)
	}
	if err := store.Consolidate(ctx, initial); err != nil {
		t.Fatal(err)
	}
	second := storeEvidenceRef(t, "project-retention", "run-retention-second", "second")
	if _, err := store.ImportEvidence(ctx, second); err != nil {
		t.Fatal(err)
	}
	newItem := storeExtraction(second, "retention newest item").Memories[0]
	newItem.ID = "dm_retention_new"
	if err := store.Consolidate(ctx, Extraction{ProjectID: second.ProjectID, EvidenceID: second.ID, Summary: "A second summary", SummaryEvidenceIDs: []string{second.ID}, Memories: []DerivedMemory{newItem}}); err != nil {
		t.Fatal(err)
	}
	active, expired := 0, 0
	for i := 0; i < maxActiveMemories; i++ {
		item, err := store.GetDerived(ctx, first.ProjectID, fmt.Sprintf("dm_retention_%03d", i))
		if err != nil {
			t.Fatal(err)
		}
		switch item.State {
		case DerivedActive:
			active++
		case DerivedExpired:
			expired++
			if len(item.Supersedes) != 0 {
				t.Fatalf("retention-expired item falsely claims a replacement: %#v", item)
			}
		case DerivedSuperseded:
			t.Fatalf("retention pruning labeled an item as superseded without a successor: %#v", item)
		default:
			t.Fatalf("unexpected retention state: %#v", item)
		}
	}
	if active != maxActiveMemories-1 || expired != 1 {
		t.Fatalf("retained active=%d expired=%d, want active=%d expired=1", active, expired, maxActiveMemories-1)
	}
}

func TestOpenReadOnlyStoreDoesNotModifyDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.db")
	writable, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	ref := storeEvidenceRef(t, "project-read-only", "run-read-only", "readonly")
	if _, err := writable.ImportEvidence(ctx, ref); err != nil {
		t.Fatal(err)
	}
	if err := writable.Close(); err != nil {
		t.Fatal(err)
	}

	beforeDB, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	readOnly, err := OpenReadOnlyStore(path)
	if err != nil {
		t.Fatal(err)
	}
	proposals, err := readOnly.ListOutstandingProposals(ctx)
	if err != nil || len(proposals) != 0 {
		t.Fatalf("read outstanding proposals = %#v, %v", proposals, err)
	}
	if _, err := readOnly.ImportEvidence(ctx, ref); !errors.Is(err, ErrReadOnlyStore) {
		t.Fatalf("write through read-only store error = %v, want %v", err, ErrReadOnlyStore)
	}
	if err := readOnly.Close(); err != nil {
		t.Fatal(err)
	}

	afterDB, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(afterDB, beforeDB) {
		t.Fatal("read-only access modified the SQLite database")
	}
}

func storeEvidenceRef(t *testing.T, project, runID, variant string) RunEvidenceRef {
	t.Helper()
	evidence := RunEvidence{
		Ref:    RunEvidenceRef{ProjectID: project, Runtime: "codex", NativeRunID: runID, CreatedAt: time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)},
		Status: string(run.Completed), SummaryText: "Evidence " + variant,
	}
	ref, err := referenceFor(evidence)
	if err != nil {
		t.Fatal(err)
	}
	return ref
}

func storeExtraction(ref RunEvidenceRef, content string) Extraction {
	now := ref.CreatedAt.UTC()
	item := DerivedMemory{ID: "dm_" + ref.ID, ProjectID: ref.ProjectID, Field: FieldDiscovery, Type: memory.TypeFact, Content: content, EvidenceIDs: []string{ref.ID}, Confidence: 1, State: DerivedActive, Verification: VerificationUnverified, CreatedAt: now, UpdatedAt: now}
	return Extraction{ProjectID: ref.ProjectID, EvidenceID: ref.ID, Summary: "Summary for " + ref.NativeRunID, SummaryEvidenceIDs: []string{ref.ID}, Memories: []DerivedMemory{item}}
}
