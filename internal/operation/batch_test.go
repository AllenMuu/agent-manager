package operation

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestSyncDirectoryChainFlushesParentsAfterChild(t *testing.T) {
	root := filepath.Join(t.TempDir(), "created", "nested")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	physical, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	var synced []string
	if err := syncDirectoryChainWith(root, func(path string) error {
		synced = append(synced, path)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(synced) < 3 || synced[0] != physical || synced[1] != filepath.Dir(physical) || synced[2] != filepath.Dir(filepath.Dir(physical)) {
		t.Fatalf("directory sync order = %#v, want child then its parent chain", synced)
	}
	if synced[len(synced)-1] != filepath.VolumeName(physical)+string(filepath.Separator) {
		t.Fatalf("directory sync chain stopped before filesystem root: %#v", synced)
	}
}

func TestBatchStorePreparePersistsAndIsIdempotent(t *testing.T) {
	root := t.TempDir()
	resource := filepath.Join(t.TempDir(), "project", ".claude", "skills", "demo")
	store := NewBatchStore(root)
	prepared, err := store.Prepare(Batch{
		ID: "batch-1", Kind: "reference-migration", Scope: "project",
		Resources: []string{resource}, SourceRecordIDs: []string{"source-1"},
		Steps: []BatchStep{{ID: "entry-1", Resource: resource, Action: "replace"}},
		Plan:  Plan{Changes: []Change{{Path: resource, Action: "copy Skill tree", SourcePath: filepath.Dir(resource), MaterializationMode: "copy-only"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Status != BatchPrepared {
		t.Fatalf("status = %q, want %q", prepared.Status, BatchPrepared)
	}

	reloaded, ok, err := NewBatchStore(root).Get("batch-1")
	if err != nil || !ok {
		t.Fatalf("Get() = (%v, %v), want stored batch", reloaded, err)
	}
	if reloaded.ID != prepared.ID || reloaded.Resources[0] != resource || reloaded.Steps[0].Action != "replace" {
		t.Fatalf("reloaded batch lost prepared intent: %#v", reloaded)
	}
	if len(reloaded.Plan.Changes) != 1 || reloaded.Plan.Changes[0].SourcePath != filepath.Dir(resource) || reloaded.Plan.Changes[0].MaterializationMode != "copy-only" {
		t.Fatalf("reloaded batch lost structured materialization intent: %#v", reloaded.Plan.Changes)
	}
	if _, err := store.Prepare(Batch{
		ID: "batch-1", Kind: "reference-migration", Scope: "project",
		Resources: []string{resource}, SourceRecordIDs: []string{"source-1"},
		Steps: []BatchStep{{ID: "entry-1", Resource: resource, Action: "replace"}},
		Plan:  Plan{Changes: []Change{{Path: resource, Action: "copy Skill tree", SourcePath: filepath.Dir(resource), MaterializationMode: "copy-only"}}},
	}); err != nil {
		t.Fatalf("same prepare should be idempotent: %v", err)
	}
	if _, err := store.Prepare(Batch{
		ID: "batch-1", Kind: "reference-migration", Scope: "project",
		Resources: []string{resource}, SourceRecordIDs: []string{"source-1"},
		Steps: []BatchStep{{ID: "entry-1", Resource: resource, Action: "delete"}},
	}); err == nil {
		t.Fatal("changed intent with an existing batch ID was accepted")
	}
}

func TestBatchStoreBlocksOverlappingPendingResources(t *testing.T) {
	root := t.TempDir()
	entry := filepath.Join(t.TempDir(), "project", ".claude", "skills", "demo")
	store := NewBatchStore(root)
	if _, err := store.Prepare(Batch{ID: "pending", Kind: "activation", Scope: "project", Resources: []string{entry}}); err != nil {
		t.Fatal(err)
	}
	if err := store.CheckAvailable([]string{filepath.Dir(entry)}); !errors.Is(err, ErrPendingBatch) {
		t.Fatalf("overlapping resource error = %v, want ErrPendingBatch", err)
	}
	if err := store.CheckAvailable([]string{filepath.Join(t.TempDir(), "elsewhere")}); err != nil {
		t.Fatalf("unrelated resource was blocked: %v", err)
	}
}

func TestBatchStoreCommittedBatchNoLongerBlocksResources(t *testing.T) {
	root := t.TempDir()
	entry := filepath.Join(t.TempDir(), "project", "skill")
	store := NewBatchStore(root)
	batch, err := store.Prepare(Batch{ID: "done", Kind: "activation", Scope: "project", Resources: []string{entry}})
	if err != nil {
		t.Fatal(err)
	}
	batch.Status = BatchCommitted
	if err := store.Update(batch); err != nil {
		t.Fatal(err)
	}
	if err := store.CheckAvailable([]string{entry}); err != nil {
		t.Fatalf("committed batch still blocks its resource: %v", err)
	}
}
