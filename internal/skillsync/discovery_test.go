package skillsync

import (
	"path/filepath"
	"testing"
	"time"
)

func TestDiscoveryStoreStagesAndCommitsIdempotently(t *testing.T) {
	root := filepath.Join(t.TempDir(), "discoveries")
	store := NewDiscoveryStore(root)
	record := DiscoveryRecord{
		ID: discoveryRecordID("batch-1", "source-1"), BatchID: "batch-1", Scope: ScopeGlobal, Location: "Codex",
		EntryPath: "/home/user/.codex/skills/demo", EntryType: "external-link",
		OriginalLink: "../../.agents/skills/demo", LinkChain: []LinkHop{{Path: "/home/user/.codex/skills/demo", Target: "../../.agents/skills/demo"}},
		RealPath: "/home/user/.agents/skills/demo", Checksum: "abc", Identifier: "demo",
		VersionKey: "sync-abc", ReusedVersion: true, ObservedAt: time.Unix(1, 0).UTC(),
	}
	staged, err := store.Prepare(record)
	if err != nil {
		t.Fatal(err)
	}
	if staged.Status != DiscoveryStaged {
		t.Fatalf("status = %q, want staged", staged.Status)
	}
	if _, err := store.Prepare(record); err != nil {
		t.Fatalf("retry should be idempotent: %v", err)
	}
	if err := store.Commit([]string{record.ID}); err != nil {
		t.Fatal(err)
	}
	reloaded, ok, err := NewDiscoveryStore(root).Get(record.ID)
	if err != nil || !ok {
		t.Fatalf("Get() = (%v, %v), want committed record", reloaded, err)
	}
	if reloaded.Status != DiscoveryCommitted || reloaded.RealPath != record.RealPath || reloaded.VersionKey != record.VersionKey {
		t.Fatalf("record not durably committed: %#v", reloaded)
	}
	if err := store.Commit([]string{record.ID}); err != nil {
		t.Fatalf("commit retry should be idempotent: %v", err)
	}
}

func TestDiscoveryStoreRejectsChangedRecordWithSameID(t *testing.T) {
	store := NewDiscoveryStore(filepath.Join(t.TempDir(), "discoveries"))
	record := DiscoveryRecord{ID: discoveryRecordID("batch-1", "source-1"), BatchID: "batch-1", Scope: ScopeProject, EntryPath: "/project/.claude/skills/demo", RealPath: "/source/demo", Checksum: "abc", Identifier: "demo", VersionKey: "sync-a"}
	if _, err := store.Prepare(record); err != nil {
		t.Fatal(err)
	}
	record.Checksum = "changed"
	if _, err := store.Prepare(record); err == nil {
		t.Fatal("changed source evidence with the same record ID was accepted")
	}
}
