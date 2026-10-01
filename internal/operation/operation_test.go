package operation_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/AllenMuu/skill-manager/internal/operation"
)

func TestLatestNormalizesLegacyJournalEntryWithoutRewritingFile(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "journal.json")
	legacy := []map[string]any{{"operation": "activate", "at": "2026-01-01T00:00:00Z", "before": []any{}, "after": []any{}}}
	contents, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, contents, 0o644); err != nil {
		t.Fatal(err)
	}

	entry, ok, err := operation.New(path).Latest()
	if err != nil || !ok {
		t.Fatalf("Latest() = %#v, %v, %v", entry, ok, err)
	}
	if entry.Version != "v1" || entry.ResourceKind != "skill" {
		t.Fatalf("normalized entry = %#v", entry)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != string(contents) {
		t.Fatalf("legacy journal was rewritten: %q, %v", got, err)
	}
}

func TestUndoLatestRestoresLegacyJournalEntry(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "project", "skill")
	journalPath := filepath.Join(root, "journal.json")
	journal := operation.New(journalPath)
	before, err := journal.Capture([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/library/demo", path); err != nil {
		t.Fatal(err)
	}
	after, err := journal.Capture([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	legacy := []map[string]any{{"operation": "activate", "at": "2026-01-01T00:00:00Z", "before": before, "after": after}}
	contents, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(journalPath, contents, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := journal.UndoLatest(func(operation.Plan) bool { return true }); err != nil {
		t.Fatalf("UndoLatest() error = %v", err)
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("restored legacy path = %v, want absent", err)
	}
}

func TestUndoLatestWithFingerprintsRejectsChangedBackupBeforeRestore(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "project", "skill")
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "owner.txt"), []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	journal := operation.New(filepath.Join(root, "journal.json"))
	before, err := journal.Capture([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/library/skill", path); err != nil {
		t.Fatal(err)
	}
	after, err := journal.Capture([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	if err := journal.Record("activate", before, after); err != nil {
		t.Fatal(err)
	}
	digest, err := operation.FingerprintPath(before[0].Backup)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(before[0].Backup, "owner.txt"), []byte("tampered"), 0o644); err != nil {
		t.Fatal(err)
	}
	err = journal.UndoLatestWithFingerprints(func(operation.Plan) bool { return true }, map[string]string{before[0].Backup: digest})
	if !errors.Is(err, operation.ErrUnexpectedState) {
		t.Fatalf("UndoLatestWithFingerprints error = %v", err)
	}
	if info, err := os.Lstat(path); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("project changed: %v %v", info, err)
	}
	if _, ok, err := journal.Latest(); err != nil || !ok {
		t.Fatalf("journal changed: %v %v", ok, err)
	}
}

func TestPreviewUndoDoesNotModifyJournalOrProject(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "project", "skill")
	journalPath := filepath.Join(root, "journal.json")
	journal := operation.New(journalPath)
	before, err := journal.Capture([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/library/demo", path); err != nil {
		t.Fatal(err)
	}
	after, err := journal.Capture([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	if err := journal.RecordPlan(operation.Plan{Operation: "activate"}, before, after); err != nil {
		t.Fatal(err)
	}
	journalBefore, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	pathsBefore := directoryState(t, root)

	plan, err := journal.PreviewUndo()
	if err != nil {
		t.Fatal(err)
	}
	if plan.Operation != "undo activate" || len(plan.Changes) != 1 || plan.Changes[0].Path != path {
		t.Fatalf("PreviewUndo() = %#v, want undo plan for %s", plan, path)
	}
	journalAfter, err := os.ReadFile(journalPath)
	if err != nil || string(journalAfter) != string(journalBefore) {
		t.Fatalf("Undo preview changed journal: %q, %v", journalAfter, err)
	}
	if pathsAfter := directoryState(t, root); !reflect.DeepEqual(pathsAfter, pathsBefore) {
		t.Fatalf("Undo preview changed directory entries\nbefore: %#v\nafter: %#v", pathsBefore, pathsAfter)
	}
	if target, err := os.Readlink(path); err != nil || target != "/library/demo" {
		t.Fatalf("Undo preview changed project link: %q, %v", target, err)
	}
}

func directoryState(t *testing.T, root string) []string {
	t.Helper()
	var paths []string
	err := filepath.WalkDir(root, func(path string, _ os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		paths = append(paths, relative)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return paths
}

func TestPlanRendersChangesAndWarnings(t *testing.T) {
	plan := operation.Plan{Operation: "activate", Changes: []operation.Change{{Path: "/project/.codex/skills/demo", Action: "create link", Detail: "/library/demo"}}, Warnings: []string{"not declared compatible with codex"}}
	if got := plan.String(); got == "" || !strings.Contains(got, "create link") || !strings.Contains(got, "not declared compatible") {
		t.Fatalf("plan did not render its preview: %q", got)
	}
}

func TestRecordPlanPersistsPlanSchemaAndResourceMetadata(t *testing.T) {
	root := t.TempDir()
	journal := operation.New(filepath.Join(root, "journal.json"))
	plan := operation.Plan{Version: "v1", ResourceKind: "skill", Operation: "activate"}
	if err := journal.RecordPlan(plan, nil, nil); err != nil {
		t.Fatal(err)
	}
	entry, ok, err := journal.Latest()
	if err != nil || !ok {
		t.Fatalf("Latest() = %#v, %v, %v", entry, ok, err)
	}
	if entry.Version != "v1" || entry.ResourceKind != "skill" || entry.Operation != "activate" {
		t.Fatalf("entry metadata = %#v", entry)
	}
}

func TestRecordPlanPreservesNonSkillPlanMetadataForUndo(t *testing.T) {
	for _, resourceKind := range []string{"subagent", "memory"} {
		t.Run(resourceKind, func(t *testing.T) {
			journal := operation.New(filepath.Join(t.TempDir(), "journal.json"))
			plan := operation.Plan{Version: "v1", ResourceKind: resourceKind, Operation: "install " + resourceKind}
			if err := journal.RecordPlan(plan, nil, nil); err != nil {
				t.Fatal(err)
			}

			entry, ok, err := journal.Latest()
			if err != nil || !ok {
				t.Fatalf("Latest() = %#v, %v, %v", entry, ok, err)
			}
			if entry.Version != plan.Version || entry.ResourceKind != plan.ResourceKind || entry.Operation != plan.Operation {
				t.Fatalf("entry metadata = %#v, want version=%q kind=%q operation=%q", entry, plan.Version, plan.ResourceKind, plan.Operation)
			}

			var undoPlan operation.Plan
			err = journal.UndoLatest(func(candidate operation.Plan) bool {
				undoPlan = candidate
				return false
			})
			if !errors.Is(err, operation.ErrNotConfirmed) {
				t.Fatalf("UndoLatest() = %v, want ErrNotConfirmed", err)
			}
			if undoPlan.Version != plan.Version || undoPlan.ResourceKind != plan.ResourceKind {
				t.Fatalf("undo plan metadata = %#v, want version=%q kind=%q", undoPlan, plan.Version, plan.ResourceKind)
			}
		})
	}
}

func TestRecordPlanRejectsUnknownMetadataWithoutWriting(t *testing.T) {
	for _, tc := range []struct {
		name                  string
		version, resourceKind string
	}{
		{name: "schema version", version: "v99", resourceKind: "skill"},
		{name: "resource kind", version: "v1", resourceKind: "unknown"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "journal.json")
			journal := operation.New(path)
			original := []byte(`[]`)
			if err := os.WriteFile(path, original, 0o644); err != nil {
				t.Fatal(err)
			}
			err := journal.RecordPlan(operation.Plan{
				Version: tc.version, ResourceKind: tc.resourceKind, Operation: "activate",
			}, nil, nil)
			if err == nil {
				t.Fatal("RecordPlan unexpectedly succeeded")
			}
			got, readErr := os.ReadFile(path)
			if readErr != nil || string(got) != string(original) {
				t.Fatalf("journal after rejected record = %q, %v; want unchanged %q", got, readErr, original)
			}
		})
	}
}

func TestUndoPreviewCarriesLegacyAndVersionedSkillMetadata(t *testing.T) {
	root := t.TempDir()
	for _, tc := range []struct {
		name                  string
		data                  map[string]any
		wantVersion, wantKind string
	}{
		{name: "legacy", data: map[string]any{"operation": "activate"}, wantVersion: "v1", wantKind: "skill"},
		{name: "versioned", data: map[string]any{"version": "v1", "resourceKind": "skill", "operation": "activate"}, wantVersion: "v1", wantKind: "skill"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			journalPath := filepath.Join(root, tc.name+".json")
			contents, err := json.Marshal([]map[string]any{tc.data})
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(journalPath, contents, 0o644); err != nil {
				t.Fatal(err)
			}
			var preview operation.Plan
			if err := operation.New(journalPath).UndoLatest(func(p operation.Plan) bool { preview = p; return false }); !errors.Is(err, operation.ErrNotConfirmed) {
				t.Fatalf("UndoLatest = %v", err)
			}
			if preview.Version != tc.wantVersion || preview.ResourceKind != tc.wantKind {
				t.Fatalf("preview metadata = %#v", preview)
			}
		})
	}
}

func TestJournalUndoRestoresLatestPreOperationState(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "project", "skill")
	journal := operation.New(filepath.Join(root, "journal.json"))
	before, err := journal.Capture([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/library/demo", path); err != nil {
		t.Fatal(err)
	}
	after, err := journal.Capture([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	if err := journal.Record("activate", before, after); err != nil {
		t.Fatal(err)
	}
	if err := journal.UndoLatest(func(operation.Plan) bool { return true }); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("path after undo = %v, want absent", err)
	}
}

func TestJournalUndoRequiresConfirmation(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "skill")
	journal := operation.New(filepath.Join(root, "journal.json"))
	before, err := journal.Capture([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/library/demo", path); err != nil {
		t.Fatal(err)
	}
	after, err := journal.Capture([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	if err := journal.Record("activate", before, after); err != nil {
		t.Fatal(err)
	}
	var preview operation.Plan
	if err := journal.UndoLatest(func(plan operation.Plan) bool { preview = plan; return false }); !errors.Is(err, operation.ErrNotConfirmed) {
		t.Fatalf("UndoLatest = %v", err)
	}
	if len(preview.Changes) == 0 {
		t.Fatal("undo did not preview changes")
	}
	if _, err := os.Lstat(path); err != nil {
		t.Fatalf("declined undo changed path: %v", err)
	}
}

func TestConcurrentJournalRecordsAreNotLost(t *testing.T) {
	const recordCount = 48
	path := filepath.Join(t.TempDir(), "journal.json")
	start := make(chan struct{})
	var workers sync.WaitGroup
	errs := make(chan error, recordCount)
	for i := 0; i < recordCount; i++ {
		workers.Add(1)
		go func(index int) {
			defer workers.Done()
			<-start
			err := operation.New(path).Record(fmt.Sprintf("operation-%02d", index), nil, nil)
			if err != nil {
				errs <- err
			}
		}(i)
	}
	close(start)
	workers.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}

	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var entries []operation.Entry
	if err := json.Unmarshal(contents, &entries); err != nil {
		t.Fatal(err)
	}
	if len(entries) != recordCount {
		t.Fatalf("journal has %d entries, want %d", len(entries), recordCount)
	}
}

func TestOperationLockSerializesProcesses(t *testing.T) {
	if journalPath := os.Getenv("AGENT_MANAGER_LOCK_HELPER_JOURNAL"); journalPath != "" {
		readyPath := os.Getenv("AGENT_MANAGER_LOCK_HELPER_READY")
		markerPath := os.Getenv("AGENT_MANAGER_LOCK_HELPER_MARKER")
		if err := os.WriteFile(readyPath, []byte("ready"), 0o600); err != nil {
			t.Fatal(err)
		}
		unlock, err := operation.New(journalPath).LockOperation()
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(markerPath, []byte("acquired"), 0o600); err != nil {
			_ = unlock()
			t.Fatal(err)
		}
		if err := unlock(); err != nil {
			t.Fatal(err)
		}
		return
	}

	root := t.TempDir()
	journalPath := filepath.Join(root, "journal.json")
	readyPath := filepath.Join(root, "ready")
	markerPath := filepath.Join(root, "acquired")
	unlock, err := operation.New(journalPath).LockOperation()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestOperationLockSerializesProcesses$")
	cmd.Env = append(os.Environ(),
		"AGENT_MANAGER_LOCK_HELPER_JOURNAL="+journalPath,
		"AGENT_MANAGER_LOCK_HELPER_READY="+readyPath,
		"AGENT_MANAGER_LOCK_HELPER_MARKER="+markerPath,
	)
	if err := cmd.Start(); err != nil {
		_ = unlock()
		t.Fatal(err)
	}
	finished := false
	defer func() {
		if !finished {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(readyPath); err == nil {
			break
		}
		if time.Now().After(deadline) {
			_ = unlock()
			t.Fatal("child process did not reach the lock")
		}
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond)
	if _, err := os.Stat(markerPath); err == nil {
		_ = unlock()
		t.Fatal("child process acquired the project lock while it was held")
	}
	if err := unlock(); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	finished = true
	if _, err := os.Stat(markerPath); err != nil {
		t.Fatalf("child process did not acquire the released project lock: %v", err)
	}
}

func TestJournalUndoRefusesUnexpectedCurrentState(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "skill")
	journal := operation.New(filepath.Join(root, "journal.json"))
	before, err := journal.Capture([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/library/demo", path); err != nil {
		t.Fatal(err)
	}
	after, err := journal.Capture([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	if err := journal.Record("activate", before, after); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := journal.UndoLatest(func(operation.Plan) bool { return true }); !errors.Is(err, operation.ErrUnexpectedState) {
		t.Fatalf("UndoLatest = %v", err)
	}
	if info, err := os.Stat(path); err != nil || !info.IsDir() {
		t.Fatalf("unexpected state was not preserved: %#v, %v", info, err)
	}
}

func TestRestoreSkipsMissingBackupButRestoresIndependentPaths(t *testing.T) {
	root := t.TempDir()
	first, second := filepath.Join(root, "first"), filepath.Join(root, "second")
	if err := os.WriteFile(first, []byte("before-first"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, []byte("before-second"), 0o644); err != nil {
		t.Fatal(err)
	}
	journal := operation.New(filepath.Join(root, "journal.json"))
	snapshots, err := journal.Capture([]string{first, second})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(first, []byte("current-first"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, []byte("current-second"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(snapshots[1].Backup); err != nil {
		t.Fatal(err)
	}
	if err := journal.Restore(snapshots); err == nil {
		t.Fatal("Restore unexpectedly succeeded with missing backup")
	}
	for _, want := range []struct{ path, text string }{{first, "before-first"}, {second, "current-second"}} {
		got, err := os.ReadFile(want.path)
		if err != nil || string(got) != want.text {
			t.Fatalf("%s = %q, %v", want.path, got, err)
		}
	}
}

func TestUndoPropagatesMissingExpectedAfterState(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "skill")
	journal := operation.New(filepath.Join(root, "journal.json"))
	before, err := journal.Capture([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/library/demo", path); err != nil {
		t.Fatal(err)
	}
	after, err := journal.Capture([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	if err := journal.Record("activate", before, after); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	err = journal.UndoLatest(func(operation.Plan) bool { return true })
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("UndoLatest = %v, want missing-target cause", err)
	}
}

func TestRestoreRestoresMovedCurrentPathWhenSecondPublishFails(t *testing.T) {
	root := t.TempDir()
	first, second := filepath.Join(root, "first"), filepath.Join(root, "second")
	if err := os.WriteFile(first, []byte("before-first"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, []byte("before-second"), 0o644); err != nil {
		t.Fatal(err)
	}
	journal := operation.New(filepath.Join(root, "journal.json"))
	snapshots, err := journal.Capture([]string{first, second})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(first, []byte("current-first"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, []byte("current-second"), 0o644); err != nil {
		t.Fatal(err)
	}
	calls := 0
	journal.BeforeRestorePublish = func(string) error {
		calls++
		if calls == 2 {
			return errors.New("publish failure")
		}
		return nil
	}
	err = journal.Restore(snapshots)
	if err == nil {
		t.Fatal("Restore unexpectedly succeeded")
	}
	for _, want := range []struct{ path, text string }{{first, "current-first"}, {second, "current-second"}} {
		got, err := os.ReadFile(want.path)
		if err != nil || string(got) != want.text {
			t.Fatalf("%s = %q, %v", want.path, got, err)
		}
	}
}

func TestRestoreRemovesPublishedCandidateWhenItHadNoCurrentPath(t *testing.T) {
	root := t.TempDir()
	first, second := filepath.Join(root, "first"), filepath.Join(root, "second")
	if err := os.WriteFile(first, []byte("before-first"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, []byte("before-second"), 0o644); err != nil {
		t.Fatal(err)
	}
	journal := operation.New(filepath.Join(root, "journal.json"))
	snapshots, err := journal.Capture([]string{first, second})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(first); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, []byte("current-second"), 0o644); err != nil {
		t.Fatal(err)
	}
	calls := 0
	journal.BeforeRestorePublish = func(string) error {
		calls++
		if calls == 2 {
			return errors.New("publish failure")
		}
		return nil
	}
	err = journal.Restore(snapshots)
	if err == nil {
		t.Fatal("Restore unexpectedly succeeded")
	}
	if _, err := os.Lstat(first); !os.IsNotExist(err) {
		t.Fatalf("published candidate remained at absent current path: %v", err)
	}
	if got, err := os.ReadFile(second); err != nil || string(got) != "current-second" {
		t.Fatalf("second current state = %q, %v", got, err)
	}
}

func TestRestorePreservesLateOwnerForPublishedAbsentSnapshot(t *testing.T) {
	root := t.TempDir()
	first, second := filepath.Join(root, "first"), filepath.Join(root, "second")
	if err := os.WriteFile(second, []byte("before-second"), 0o644); err != nil {
		t.Fatal(err)
	}
	journal := operation.New(filepath.Join(root, "journal.json"))
	snapshots, err := journal.Capture([]string{first, second})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, []byte("current-second"), 0o644); err != nil {
		t.Fatal(err)
	}
	calls := 0
	journal.BeforeRestorePublish = func(path string) error {
		calls++
		if calls == 2 {
			if err := os.WriteFile(first, []byte("late owner"), 0o644); err != nil {
				return err
			}
			return errors.New("publish failure")
		}
		return nil
	}
	if err := journal.Restore(snapshots); err == nil || !strings.Contains(err.Error(), "preserved restore staging") {
		t.Fatal("Restore unexpectedly succeeded")
	}
	if got, readErr := os.ReadFile(first); readErr != nil || string(got) != "late owner" {
		t.Fatalf("late owner = %q, %v; restore removed an unowned target", got, readErr)
	}
}

func TestRestoreDoesNotOverwriteLateOwnerForPublishedExistingSnapshot(t *testing.T) {
	root := t.TempDir()
	first, second := filepath.Join(root, "first"), filepath.Join(root, "second")
	if err := os.WriteFile(first, []byte("before-first"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, []byte("before-second"), 0o644); err != nil {
		t.Fatal(err)
	}
	journal := operation.New(filepath.Join(root, "journal.json"))
	snapshots, err := journal.Capture([]string{first, second})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(first, []byte("current-first"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, []byte("current-second"), 0o644); err != nil {
		t.Fatal(err)
	}
	calls := 0
	journal.BeforeRestorePublish = func(string) error {
		calls++
		if calls == 2 {
			if err := os.WriteFile(first, []byte("late owner"), 0o644); err != nil {
				return err
			}
			return errors.New("publish failure")
		}
		return nil
	}
	restoreErr := journal.Restore(snapshots)
	if restoreErr == nil || !strings.Contains(restoreErr.Error(), "preserved restore staging") {
		t.Fatal("Restore unexpectedly succeeded")
	}
	if got, readErr := os.ReadFile(first); readErr != nil || string(got) != "late owner" {
		t.Fatalf("late owner = %q, %v; restore overwrote an unowned target", got, readErr)
	}
	marker := "preserved restore staging at "
	start := strings.Index(restoreErr.Error(), marker)
	if start < 0 {
		t.Fatalf("restore error omitted recovery staging path: %v", restoreErr)
	}
	staging := restoreErr.Error()[start+len(marker):]
	if end := strings.Index(staging, ": "); end >= 0 {
		staging = staging[:end]
	}
	if got, readErr := os.ReadFile(filepath.Join(staging, "previous")); readErr != nil || string(got) != "current-first" {
		t.Fatalf("staged original was not retained for recovery: %q, %v", got, readErr)
	}
}

func TestRestoreRefusesReplacedParentWithoutTouchingExternalTree(t *testing.T) {
	root := t.TempDir()
	parent := filepath.Join(root, "project")
	if err := os.MkdirAll(parent, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(parent, "skill")
	if err := os.WriteFile(path, []byte("before"), 0o644); err != nil {
		t.Fatal(err)
	}
	journal := operation.New(filepath.Join(root, "journal.json"))
	snapshots, err := journal.Capture([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("current"), 0o644); err != nil {
		t.Fatal(err)
	}
	external := t.TempDir()
	sentinel := filepath.Join(external, "sentinel")
	if err := os.WriteFile(sentinel, []byte("external owner"), 0o644); err != nil {
		t.Fatal(err)
	}
	recoverable := parent + "-recoverable"
	journal.BeforeRestorePublish = func(string) error {
		if err := os.Rename(parent, recoverable); err != nil {
			return err
		}
		if err := os.Symlink(external, parent); err != nil {
			t.Skipf("symlink unavailable: %v", err)
		}
		return nil
	}
	err = journal.Restore(snapshots)
	if !errors.Is(err, operation.ErrUnsafePath) {
		t.Fatalf("Restore() error = %v, want unsafe path", err)
	}
	if got, readErr := os.ReadFile(sentinel); readErr != nil || string(got) != "external owner" {
		t.Fatalf("external tree changed: %q, %v", got, readErr)
	}
}

func TestRestoreRejectsNestedSymlinkAncestor(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	if err := os.MkdirAll(filepath.Join(target, "child"), 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "nested-link")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	path := filepath.Join(link, "child", "skill")
	if err := os.WriteFile(path, []byte("before"), 0o644); err != nil {
		t.Fatal(err)
	}
	journal := operation.New(filepath.Join(root, "journal.json"))
	snapshots, err := journal.Capture([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("current"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := journal.Restore(snapshots); !errors.Is(err, operation.ErrUnsafePath) {
		t.Fatalf("Restore() error = %v, want unsafe path", err)
	}
	if got, readErr := os.ReadFile(path); readErr != nil || string(got) != "current" {
		t.Fatalf("nested symlink ancestor target changed: %q, %v", got, readErr)
	}
}
