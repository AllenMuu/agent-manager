package webconsole

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/AllenMuu/skill-manager/internal/adapter"
	"github.com/AllenMuu/skill-manager/internal/operation"
)

func TestActivationPlanRequiresReviewAndExecutesOnce(t *testing.T) {
	server, project, _, skillID := operationFixture(t)
	projectID := server.project.ID
	body := `{"skillIdentifiers":["` + skillID + `"],"targetAgents":["codex"]}`
	preview := request(t, server, http.MethodPost, "/api/v1/projects/"+projectID+"/plans/activate", body)
	if preview.Code != http.StatusCreated {
		t.Fatalf("create plan status = %d, body = %s", preview.Code, preview.Body.String())
	}
	var envelope operationPlanEnvelope
	if err := json.Unmarshal(preview.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Plan.ID == "" || len(envelope.Plan.Changes) != 1 || envelope.Plan.ExpiresAt.IsZero() {
		t.Fatalf("plan = %#v, want opaque id, exact placement change, and expiry", envelope.Plan)
	}
	placement, _ := adapter.For(adapter.Codex)
	destination := placement.ProjectSkillPath(project, skillID)
	if _, err := os.Lstat(destination); !os.IsNotExist(err) {
		t.Fatalf("preview mutated project destination, lstat error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(project, ".skill-manager", "journal.json")); !os.IsNotExist(err) {
		t.Fatalf("preview created journal, stat error = %v", err)
	}

	executed := request(t, server, http.MethodPost, "/api/v1/plans/"+envelope.Plan.ID+"/execute", `{}`)
	if executed.Code != http.StatusOK {
		t.Fatalf("execute status = %d, body = %s", executed.Code, executed.Body.String())
	}
	if info, err := os.Lstat(destination); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("activation destination = %v, %v; want managed symlink", info, err)
	}
	if replay := request(t, server, http.MethodPost, "/api/v1/plans/"+envelope.Plan.ID+"/execute", `{}`); replay.Code != http.StatusConflict {
		t.Fatalf("replay status = %d, body = %s; want consumed-plan conflict", replay.Code, replay.Body.String())
	}
}

func TestActivationPlanRejectsEditedSkillSource(t *testing.T) {
	server, project, library, skillID := operationFixture(t)
	script := filepath.Join(library, skillID, "script.sh")
	if err := os.WriteFile(script, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	preview := request(t, server, http.MethodPost, "/api/v1/projects/"+server.project.ID+"/plans/activate", `{"skillIdentifiers":["`+skillID+`"],"targetAgents":["codex"]}`)
	var envelope operationPlanEnvelope
	if err := json.Unmarshal(preview.Body.Bytes(), &envelope); err != nil || preview.Code != http.StatusCreated {
		t.Fatalf("preview = %d %s: %v", preview.Code, preview.Body.String(), err)
	}
	if err := os.WriteFile(script, []byte("changed"), 0o644); err != nil {
		t.Fatal(err)
	}
	result := request(t, server, http.MethodPost, "/api/v1/plans/"+envelope.Plan.ID+"/execute", `{}`)
	if result.Code != http.StatusConflict || !strings.Contains(result.Body.String(), "stale_plan") {
		t.Fatalf("execute = %d %s", result.Code, result.Body.String())
	}
	target, _ := adapter.For(adapter.Codex)
	if _, err := os.Lstat(target.ProjectSkillPath(project, skillID)); !os.IsNotExist(err) {
		t.Fatalf("destination changed: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(project, ".skill-manager", "journal.json")); !os.IsNotExist(err) {
		t.Fatalf("journal changed: %v", err)
	}
}

func TestUndoPlanRejectsEditedBackup(t *testing.T) {
	server, project, _, skillID := operationFixture(t)
	target, _ := adapter.For(adapter.Codex)
	destination := target.ProjectSkillPath(project, skillID)
	if err := os.MkdirAll(destination, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(destination, "owner.txt"), []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	preview := request(t, server, http.MethodPost, "/api/v1/projects/"+server.project.ID+"/plans/activate", `{"skillIdentifiers":["`+skillID+`"],"targetAgents":["codex"],"conflictStrategy":"replace","forceConfirmed":true}`)
	var activation operationPlanEnvelope
	if err := json.Unmarshal(preview.Body.Bytes(), &activation); err != nil || preview.Code != http.StatusCreated {
		t.Fatalf("activation preview = %d %s: %v", preview.Code, preview.Body.String(), err)
	}
	if result := request(t, server, http.MethodPost, "/api/v1/plans/"+activation.Plan.ID+"/execute", `{}`); result.Code != http.StatusOK {
		t.Fatalf("activation = %d %s", result.Code, result.Body.String())
	}
	undo := request(t, server, http.MethodPost, "/api/v1/projects/"+server.project.ID+"/plans/undo", `{}`)
	var envelope operationPlanEnvelope
	if err := json.Unmarshal(undo.Body.Bytes(), &envelope); err != nil || undo.Code != http.StatusCreated {
		t.Fatalf("undo preview = %d %s: %v", undo.Code, undo.Body.String(), err)
	}
	journal, err := server.journalForProject(server.project)
	if err != nil {
		t.Fatal(err)
	}
	entry, ok, err := journal.Latest()
	if err != nil || !ok || len(entry.Before) != 1 || !entry.Before[0].Exists {
		t.Fatalf("entry = %#v, %v", entry, err)
	}
	journalBefore, err := os.ReadFile(journal.Path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(entry.Before[0].Backup, "owner.txt"), []byte("tampered"), 0o644); err != nil {
		t.Fatal(err)
	}
	result := request(t, server, http.MethodPost, "/api/v1/plans/"+envelope.Plan.ID+"/execute", `{}`)
	if result.Code != http.StatusConflict || !strings.Contains(result.Body.String(), "stale_plan") {
		t.Fatalf("undo = %d %s", result.Code, result.Body.String())
	}
	if info, err := os.Lstat(destination); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("destination changed: %v %v", info, err)
	}
	journalAfter, err := os.ReadFile(journal.Path)
	if err != nil || string(journalAfter) != string(journalBefore) {
		t.Fatalf("journal changed: %v", err)
	}
}

func TestConcurrentExecutionAllowsOnlyOneLifecycleCall(t *testing.T) {
	server, _, _, skillID := operationFixture(t)
	projectID := server.project.ID
	preview := request(t, server, http.MethodPost, "/api/v1/projects/"+projectID+"/plans/activate", `{"skillIdentifiers":["`+skillID+`"],"targetAgents":["codex"]}`)
	var envelope operationPlanEnvelope
	if err := json.Unmarshal(preview.Body.Bytes(), &envelope); err != nil || preview.Code != http.StatusCreated {
		t.Fatalf("create concurrent plan = status %d body %s err %v", preview.Code, preview.Body.String(), err)
	}

	const callers = 12
	start := make(chan struct{})
	statuses := make(chan int, callers)
	for i := 0; i < callers; i++ {
		go func() {
			<-start
			req := httptest.NewRequest(http.MethodPost, "/api/v1/plans/"+envelope.Plan.ID+"/execute", strings.NewReader(`{}`))
			req.Host = "127.0.0.1:8787"
			req.Header.Set("Authorization", "Bearer "+server.token)
			req.Header.Set("Origin", "http://127.0.0.1:8787")
			req.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			server.ServeHTTP(response, req)
			statuses <- response.Code
		}()
	}
	close(start)
	success, conflicts := 0, 0
	for i := 0; i < callers; i++ {
		switch status := <-statuses; status {
		case http.StatusOK:
			success++
		case http.StatusConflict:
			conflicts++
		default:
			t.Errorf("concurrent execute status = %d, want 200 or 409", status)
		}
	}
	if success != 1 || conflicts != callers-1 {
		t.Fatalf("concurrent execution results = %d successful, %d conflicts; want one lifecycle call", success, conflicts)
	}
}

func TestDifferentConcurrentPlansKeepBothJournalEntries(t *testing.T) {
	server, project, _, skillID := operationFixture(t)
	projectID := server.project.ID
	plans := make([]operationPlanView, 2)
	for i, target := range []string{"codex", "claude-code"} {
		preview := request(t, server, http.MethodPost, "/api/v1/projects/"+projectID+"/plans/activate", `{"skillIdentifiers":["`+skillID+`"],"targetAgents":["`+target+`"]}`)
		var envelope operationPlanEnvelope
		if err := json.Unmarshal(preview.Body.Bytes(), &envelope); err != nil || preview.Code != http.StatusCreated {
			t.Fatalf("create plan for %s = %d %s %v", target, preview.Code, preview.Body.String(), err)
		}
		plans[i] = envelope.Plan
	}

	start := make(chan struct{})
	statuses := make(chan int, len(plans))
	for _, plan := range plans {
		go func(plan operationPlanView) {
			<-start
			response := request(t, server, http.MethodPost, "/api/v1/plans/"+plan.ID+"/execute", `{}`)
			statuses <- response.Code
		}(plan)
	}
	close(start)
	for range plans {
		if status := <-statuses; status != http.StatusOK {
			t.Fatalf("concurrent different-plan execution status = %d, want 200", status)
		}
	}

	journalBytes, err := os.ReadFile(filepath.Join(project, ".skill-manager", "journal.json"))
	if err != nil {
		t.Fatal(err)
	}
	var entries []operation.Entry
	if err := json.Unmarshal(journalBytes, &entries); err != nil || len(entries) != 2 {
		t.Fatalf("journal entries = %d, %v; want both concurrent operations", len(entries), err)
	}
}

func TestForcedReplacementRejectsContentChangedAfterReview(t *testing.T) {
	server, project, _, skillID := operationFixture(t)
	target, _ := adapter.For(adapter.Codex)
	destination := target.ProjectSkillPath(project, skillID)
	if err := os.MkdirAll(destination, 0o755); err != nil {
		t.Fatal(err)
	}
	ownerPath := filepath.Join(destination, "owner.txt")
	if err := os.WriteFile(ownerPath, []byte("reviewed content"), 0o644); err != nil {
		t.Fatal(err)
	}
	preview := request(t, server, http.MethodPost, "/api/v1/projects/"+server.project.ID+"/plans/activate", `{"skillIdentifiers":["`+skillID+`"],"targetAgents":["codex"],"conflictStrategy":"replace","forceConfirmed":true}`)
	var envelope operationPlanEnvelope
	if err := json.Unmarshal(preview.Body.Bytes(), &envelope); err != nil || preview.Code != http.StatusCreated {
		t.Fatalf("create replacement plan = %d %s %v", preview.Code, preview.Body.String(), err)
	}
	if err := os.WriteFile(ownerPath, []byte("updated after review"), 0o644); err != nil {
		t.Fatal(err)
	}
	result := request(t, server, http.MethodPost, "/api/v1/plans/"+envelope.Plan.ID+"/execute", `{}`)
	if result.Code != http.StatusConflict || !strings.Contains(result.Body.String(), "stale_plan") {
		t.Fatalf("replacement after owner edit = %d %s, want stale plan", result.Code, result.Body.String())
	}
	if got, err := os.ReadFile(ownerPath); err != nil || string(got) != "updated after review" {
		t.Fatalf("updated owner content = %q, %v; replacement must preserve it", got, err)
	}
}

func TestExecutionRejectsReplacedRegisteredProjectDirectory(t *testing.T) {
	server, project, _, skillID := operationFixture(t)
	preview := request(t, server, http.MethodPost, "/api/v1/projects/"+server.project.ID+"/plans/activate", `{"skillIdentifiers":["`+skillID+`"],"targetAgents":["codex"]}`)
	var envelope operationPlanEnvelope
	if err := json.Unmarshal(preview.Body.Bytes(), &envelope); err != nil || preview.Code != http.StatusCreated {
		t.Fatalf("create project-bound plan = %d %s %v", preview.Code, preview.Body.String(), err)
	}
	oldProject := project + "-old"
	if err := os.Rename(project, oldProject); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(project, 0o755); err != nil {
		t.Fatal(err)
	}
	result := request(t, server, http.MethodPost, "/api/v1/plans/"+envelope.Plan.ID+"/execute", `{}`)
	if result.Code != http.StatusConflict || !strings.Contains(result.Body.String(), "stale_plan") {
		t.Fatalf("replaced project execution = %d %s, want stale plan", result.Code, result.Body.String())
	}
	read := request(t, server, http.MethodGet, "/api/v1/projects/"+server.project.ID+"/agents", "")
	if read.Code != http.StatusConflict || !strings.Contains(read.Body.String(), "stale_project") {
		t.Fatalf("read replaced project = %d %s, want stale project", read.Code, read.Body.String())
	}
	if _, err := os.Lstat(filepath.Join(project, ".codex", "skills", skillID)); !os.IsNotExist(err) {
		t.Fatalf("replacement directory was mutated, lstat error = %v", err)
	}
}

func TestSymlinkAtRegisteredProjectRootIsRejected(t *testing.T) {
	server, project, _, _ := operationFixture(t)
	original := project + "-original"
	if err := os.Rename(project, original); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(original, project); err != nil {
		t.Fatal(err)
	}
	read := request(t, server, http.MethodGet, "/api/v1/projects/"+server.project.ID+"/agents", "")
	if read.Code != http.StatusConflict || !strings.Contains(read.Body.String(), "stale_project") {
		t.Fatalf("symlinked project root read = %d %s, want stale project", read.Code, read.Body.String())
	}
}

func TestOperationJournalRejectsSymlinkedProjectJournalDirectories(t *testing.T) {
	for _, journalPath := range []string{".skill-manager", filepath.Join(".skill-manager", ".skill-manager-journal")} {
		t.Run(filepath.Base(journalPath), func(t *testing.T) {
			server, project, _, skillID := operationFixture(t)
			outside := filepath.Join(filepath.Dir(project), "outside")
			if err := os.Mkdir(outside, 0o755); err != nil {
				t.Fatal(err)
			}
			sentinel := filepath.Join(outside, "keep.txt")
			if err := os.WriteFile(sentinel, []byte("untouched"), 0o644); err != nil {
				t.Fatal(err)
			}
			link := filepath.Join(project, journalPath)
			if filepath.Base(journalPath) == ".skill-manager-journal" {
				if err := os.Mkdir(filepath.Join(project, ".skill-manager"), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.Symlink(outside, link); err != nil {
				t.Fatal(err)
			}
			result := request(t, server, http.MethodPost, "/api/v1/projects/"+server.project.ID+"/plans/activate", `{"skillIdentifiers":["`+skillID+`"],"targetAgents":["codex"]}`)
			if result.Code != http.StatusConflict {
				t.Fatalf("plan with symlinked journal directory = %d %s, want conflict", result.Code, result.Body.String())
			}
			if got, err := os.ReadFile(sentinel); err != nil || string(got) != "untouched" {
				t.Fatalf("outside sentinel = %q, %v; must remain untouched", got, err)
			}
			entries, err := os.ReadDir(outside)
			if err != nil || len(entries) != 1 || entries[0].Name() != "keep.txt" {
				t.Fatalf("outside directory entries = %v, %v; want only sentinel", entries, err)
			}
		})
	}
}

func TestCommittedJournalSyncErrorReportsAppliedOperation(t *testing.T) {
	server, project, _, skillID := operationFixture(t)
	server.beforeJournalDirectorySync = func() error { return errors.New("sync denied") }
	preview := request(t, server, http.MethodPost, "/api/v1/projects/"+server.project.ID+"/plans/activate", `{"skillIdentifiers":["`+skillID+`"],"targetAgents":["codex"]}`)
	var envelope operationPlanEnvelope
	if err := json.Unmarshal(preview.Body.Bytes(), &envelope); err != nil || preview.Code != http.StatusCreated {
		t.Fatalf("create plan for journal sync failure = %d %s %v", preview.Code, preview.Body.String(), err)
	}
	result := request(t, server, http.MethodPost, "/api/v1/plans/"+envelope.Plan.ID+"/execute", `{}`)
	if result.Code != http.StatusInternalServerError || !strings.Contains(result.Body.String(), "journal_committed") || !strings.Contains(result.Body.String(), "was applied") {
		t.Fatalf("journal sync failure = %d %s, want applied/committed status", result.Code, result.Body.String())
	}
	target, _ := adapter.For(adapter.Codex)
	if info, err := os.Lstat(target.ProjectSkillPath(project, skillID)); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("committed activation link = %v, %v; should remain applied", info, err)
	}
	if _, err := os.Stat(filepath.Join(project, ".skill-manager", "journal.json")); err != nil {
		t.Fatalf("committed journal record missing: %v", err)
	}
}

func TestLatestOperationDisablesUndoWhenRecordedStateChanged(t *testing.T) {
	server, project, _, skillID := operationFixture(t)
	preview := request(t, server, http.MethodPost, "/api/v1/projects/"+server.project.ID+"/plans/activate", `{"skillIdentifiers":["`+skillID+`"],"targetAgents":["codex"]}`)
	var envelope operationPlanEnvelope
	if err := json.Unmarshal(preview.Body.Bytes(), &envelope); err != nil || preview.Code != http.StatusCreated {
		t.Fatalf("create activation plan = %d %s %v", preview.Code, preview.Body.String(), err)
	}
	if result := request(t, server, http.MethodPost, "/api/v1/plans/"+envelope.Plan.ID+"/execute", `{}`); result.Code != http.StatusOK {
		t.Fatalf("activation execute = %d %s", result.Code, result.Body.String())
	}
	target, _ := adapter.For(adapter.Codex)
	destination := target.ProjectSkillPath(project, skillID)
	if err := os.Remove(destination); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destination, []byte("new owner"), 0o644); err != nil {
		t.Fatal(err)
	}
	latest := request(t, server, http.MethodGet, "/api/v1/projects/"+server.project.ID+"/operations/latest", "")
	if !strings.Contains(latest.Body.String(), `"undoAvailable":false`) {
		t.Fatalf("latest operation still offered Undo: %s", latest.Body.String())
	}
	undo := request(t, server, http.MethodPost, "/api/v1/projects/"+server.project.ID+"/plans/undo", `{}`)
	if undo.Code != http.StatusConflict || !strings.Contains(undo.Body.String(), "nothing_to_undo") {
		t.Fatalf("stale Undo plan = %d %s, want no current Undo", undo.Code, undo.Body.String())
	}
}

func TestUndoRejectsJournalSnapshotsOutsideProjectBoundary(t *testing.T) {
	for _, test := range []struct {
		name        string
		outsidePath bool
	}{
		{name: "snapshot path outside project", outsidePath: true},
		{name: "backup path outside journal directory"},
	} {
		t.Run(test.name, func(t *testing.T) {
			server, project, _, _ := operationFixture(t)
			backupRoot := filepath.Join(project, ".skill-manager", ".skill-manager-journal")
			if err := os.MkdirAll(backupRoot, 0o755); err != nil {
				t.Fatal(err)
			}
			trustedBackup := filepath.Join(backupRoot, "123-0")
			if err := os.WriteFile(trustedBackup, []byte("journal-owned state"), 0o600); err != nil {
				t.Fatal(err)
			}

			snapshotPath := filepath.Join(project, ".claude", "skills", "undo-target")
			if err := os.MkdirAll(filepath.Dir(snapshotPath), 0o755); err != nil {
				t.Fatal(err)
			}
			backupPath := trustedBackup
			if test.outsidePath {
				snapshotPath = filepath.Join(filepath.Dir(project), "external-undo-target")
			} else {
				backupPath = filepath.Join(filepath.Dir(project), "external-undo-backup")
				if err := os.WriteFile(backupPath, []byte("outside data"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			entry := operation.Entry{
				Operation: "remove",
				Before:    []operation.Snapshot{{Path: snapshotPath, Exists: true, Backup: backupPath}},
				After:     []operation.Snapshot{{Path: snapshotPath, Exists: false}},
			}
			data, err := json.Marshal([]operation.Entry{entry})
			if err != nil {
				t.Fatal(err)
			}
			journalPath := filepath.Join(project, ".skill-manager", "journal.json")
			if err := os.MkdirAll(filepath.Dir(journalPath), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(journalPath, data, 0o600); err != nil {
				t.Fatal(err)
			}

			latest := request(t, server, http.MethodGet, "/api/v1/projects/"+server.project.ID+"/operations/latest", "")
			if latest.Code != http.StatusConflict || !strings.Contains(latest.Body.String(), "unsafe_journal_entry") {
				t.Fatalf("latest operation with unsafe journal = %d %s", latest.Code, latest.Body.String())
			}
			undo := request(t, server, http.MethodPost, "/api/v1/projects/"+server.project.ID+"/plans/undo", `{}`)
			if undo.Code != http.StatusConflict || !strings.Contains(undo.Body.String(), "operation_conflict") {
				t.Fatalf("Undo plan with unsafe journal = %d %s", undo.Code, undo.Body.String())
			}
			if _, err := os.Lstat(snapshotPath); !os.IsNotExist(err) {
				t.Fatalf("unsafe journal created snapshot target %q: %v", snapshotPath, err)
			}
		})
	}
}

func TestLatestOperationDisablesUndoForUnmanagedProjectPaths(t *testing.T) {
	server, project, _, _ := operationFixture(t)
	project = server.project.Path
	backupRoot := filepath.Join(project, ".skill-manager", ".skill-manager-journal")
	if err := os.MkdirAll(backupRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(backupRoot, "234-0")
	if err := os.WriteFile(backup, []byte("hook content"), 0o600); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(project, ".git", "hooks", "post-checkout")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal([]operation.Entry{{
		Operation: "remove", ResourceKind: "skill",
		Before: []operation.Snapshot{{Path: target, Exists: true, Backup: backup}},
		After:  []operation.Snapshot{{Path: target, Exists: false}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, ".skill-manager", "journal.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	journal, err := server.journalForProject(server.project)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := journal.Latest(); err != nil {
		t.Fatalf("read structurally valid unmanaged-path journal: %v", err)
	}

	latest := request(t, server, http.MethodGet, "/api/v1/projects/"+server.project.ID+"/operations/latest", "")
	if latest.Code != http.StatusOK || !strings.Contains(latest.Body.String(), `"undoAvailable":false`) || strings.Contains(latest.Body.String(), `"undoPreview"`) {
		t.Fatalf("latest operation exposed Undo for an unmanaged hook path: %d %s", latest.Code, latest.Body.String())
	}
	undo := request(t, server, http.MethodPost, "/api/v1/projects/"+server.project.ID+"/plans/undo", `{}`)
	if undo.Code != http.StatusConflict || !strings.Contains(undo.Body.String(), "operation_conflict") {
		t.Fatalf("Undo plan for an unmanaged hook path = %d %s", undo.Code, undo.Body.String())
	}
	if _, err := os.Lstat(target); !os.IsNotExist(err) {
		t.Fatalf("unmanaged hook path was created: %v", err)
	}
}

func TestLatestOperationDisablesUndoForSharedLibraryAdoptPath(t *testing.T) {
	server, _, _, _ := operationFixture(t)
	project := server.project.Path
	backupRoot := filepath.Join(project, ".skill-manager", ".skill-manager-journal")
	if err := os.MkdirAll(backupRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(backupRoot, "456-0")
	if err := os.WriteFile(backup, []byte("shared library state"), 0o600); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(server.libraryPath, "library-skill")
	entry := operation.Entry{
		Operation: "adopt", ResourceKind: "skill",
		Before: []operation.Snapshot{{Path: target, Exists: true, Backup: backup}},
		After:  []operation.Snapshot{{Path: target, Exists: false}},
	}
	data, err := json.Marshal([]operation.Entry{entry})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, ".skill-manager", "journal.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	journal, err := server.journalForProject(server.project)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := journal.Latest(); err != nil {
		t.Fatalf("read structurally valid adopt journal: %v", err)
	}

	latest := request(t, server, http.MethodGet, "/api/v1/projects/"+server.project.ID+"/operations/latest", "")
	if latest.Code != http.StatusOK || !strings.Contains(latest.Body.String(), `"undoAvailable":false`) || strings.Contains(latest.Body.String(), `"undoPreview"`) {
		t.Fatalf("latest operation exposed Undo into the shared library: %d %s", latest.Code, latest.Body.String())
	}
	plan := request(t, server, http.MethodPost, "/api/v1/projects/"+server.project.ID+"/plans/undo", `{}`)
	if plan.Code != http.StatusConflict || !strings.Contains(plan.Body.String(), "operation_conflict") {
		t.Fatalf("Undo plan into the shared library = %d %s", plan.Code, plan.Body.String())
	}
	if _, err := os.Lstat(target); !os.IsNotExist(err) {
		t.Fatalf("shared library target was created: %v", err)
	}
}

func TestUndoAllowsManagedSubAgentPlacement(t *testing.T) {
	server, project, _, _ := operationFixture(t)
	project = server.project.Path
	destination := filepath.Join(project, ".claude", "agents", "reviewer.md")
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destination, []byte("installed representation"), 0o644); err != nil {
		t.Fatal(err)
	}
	backupRoot := filepath.Join(project, ".skill-manager", ".skill-manager-journal")
	if err := os.MkdirAll(backupRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(backupRoot, "345-0")
	if err := os.WriteFile(backup, []byte("installed representation"), 0o644); err != nil {
		t.Fatal(err)
	}
	entry := operation.Entry{
		Operation: "place resource", ResourceKind: "subagent",
		Before: []operation.Snapshot{{Path: destination, Exists: false}},
		After:  []operation.Snapshot{{Path: destination, Exists: true, Backup: backup}},
	}
	data, err := json.Marshal([]operation.Entry{entry})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, ".skill-manager", "journal.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	journal, err := server.journalForProject(server.project)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := journal.Latest(); err != nil {
		t.Fatalf("read structurally valid SubAgent journal: %v", err)
	}

	latest := request(t, server, http.MethodGet, "/api/v1/projects/"+server.project.ID+"/operations/latest", "")
	if latest.Code != http.StatusOK || !strings.Contains(latest.Body.String(), `"undoAvailable":true`) {
		t.Fatalf("latest managed SubAgent operation = %d %s", latest.Code, latest.Body.String())
	}
	plan := request(t, server, http.MethodPost, "/api/v1/projects/"+server.project.ID+"/plans/undo", `{}`)
	var envelope operationPlanEnvelope
	if err := json.Unmarshal(plan.Body.Bytes(), &envelope); err != nil || plan.Code != http.StatusCreated {
		t.Fatalf("managed SubAgent Undo plan = %d %s %v", plan.Code, plan.Body.String(), err)
	}
	executed := request(t, server, http.MethodPost, "/api/v1/plans/"+envelope.Plan.ID+"/execute", `{}`)
	if executed.Code != http.StatusOK {
		t.Fatalf("managed SubAgent Undo execute = %d %s", executed.Code, executed.Body.String())
	}
	if _, err := os.Lstat(destination); !os.IsNotExist(err) {
		t.Fatalf("managed SubAgent representation remains after Undo: %v", err)
	}
}

func TestActivationPlanRejectsCallerPathsAndStalePreview(t *testing.T) {
	server, project, _, skillID := operationFixture(t)
	projectID := server.project.ID
	pathAttempt := request(t, server, http.MethodPost, "/api/v1/projects/"+projectID+"/plans/activate", `{"skillIdentifiers":["`+skillID+`"],"targetAgents":["codex"],"destination":"/tmp/elsewhere"}`)
	if pathAttempt.Code != http.StatusBadRequest {
		t.Fatalf("caller path status = %d, body = %s; want strict JSON rejection", pathAttempt.Code, pathAttempt.Body.String())
	}

	preview := request(t, server, http.MethodPost, "/api/v1/projects/"+projectID+"/plans/activate", `{"skillIdentifiers":["`+skillID+`"],"targetAgents":["codex"]}`)
	var envelope operationPlanEnvelope
	if err := json.Unmarshal(preview.Body.Bytes(), &envelope); err != nil || preview.Code != http.StatusCreated {
		t.Fatalf("create plan = status %d body %s err %v", preview.Code, preview.Body.String(), err)
	}
	placement, _ := adapter.For(adapter.Codex)
	destination := placement.ProjectSkillPath(project, skillID)
	if err := os.MkdirAll(destination, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(destination, "owner.txt"), []byte("external owner"), 0o644); err != nil {
		t.Fatal(err)
	}
	executed := request(t, server, http.MethodPost, "/api/v1/plans/"+envelope.Plan.ID+"/execute", `{}`)
	if executed.Code != http.StatusConflict || !strings.Contains(executed.Body.String(), "stale_plan") {
		t.Fatalf("stale execution status = %d, body = %s", executed.Code, executed.Body.String())
	}
	if got, err := os.ReadFile(filepath.Join(destination, "owner.txt")); err != nil || string(got) != "external owner" {
		t.Fatalf("external owner changed after stale execution: %q, %v", got, err)
	}
}

func TestPlanCancellationAndExpiryAreEnforced(t *testing.T) {
	server, _, _, skillID := operationFixture(t)
	projectID := server.project.ID
	preview := request(t, server, http.MethodPost, "/api/v1/projects/"+projectID+"/plans/activate", `{"skillIdentifiers":["`+skillID+`"],"targetAgents":["codex"]}`)
	var envelope operationPlanEnvelope
	if err := json.Unmarshal(preview.Body.Bytes(), &envelope); err != nil || preview.Code != http.StatusCreated {
		t.Fatalf("create cancellable plan = status %d body %s err %v", preview.Code, preview.Body.String(), err)
	}
	cancelled := request(t, server, http.MethodPost, "/api/v1/plans/"+envelope.Plan.ID+"/cancel", `{}`)
	if cancelled.Code != http.StatusOK {
		t.Fatalf("cancel status = %d, body = %s", cancelled.Code, cancelled.Body.String())
	}
	if executed := request(t, server, http.MethodPost, "/api/v1/plans/"+envelope.Plan.ID+"/execute", `{}`); executed.Code != http.StatusConflict {
		t.Fatalf("cancelled execute status = %d, body = %s", executed.Code, executed.Body.String())
	}

	now := time.Now().UTC()
	server.now = func() time.Time { return now }
	preview = request(t, server, http.MethodPost, "/api/v1/projects/"+projectID+"/plans/activate", `{"skillIdentifiers":["`+skillID+`"],"targetAgents":["codex"]}`)
	if err := json.Unmarshal(preview.Body.Bytes(), &envelope); err != nil || preview.Code != http.StatusCreated {
		t.Fatalf("create expiring plan = status %d body %s err %v", preview.Code, preview.Body.String(), err)
	}
	now = now.Add(10*time.Minute + time.Nanosecond)
	if executed := request(t, server, http.MethodPost, "/api/v1/plans/"+envelope.Plan.ID+"/execute", `{}`); executed.Code != http.StatusGone {
		t.Fatalf("expired execute status = %d, body = %s", executed.Code, executed.Body.String())
	}
	if unknown := request(t, server, http.MethodPost, "/api/v1/plans/00000000000000000000000000000000/execute", `{}`); unknown.Code != http.StatusNotFound {
		t.Fatalf("unknown execute status = %d, body = %s", unknown.Code, unknown.Body.String())
	}
}

func TestPlanExpiresWhileWaitingForSessionOperationLock(t *testing.T) {
	server, project, _, skillID := operationFixture(t)
	var nowMu sync.RWMutex
	now := time.Now().UTC()
	server.now = func() time.Time {
		nowMu.RLock()
		defer nowMu.RUnlock()
		return now
	}
	preview := request(t, server, http.MethodPost, "/api/v1/projects/"+server.project.ID+"/plans/activate", `{"skillIdentifiers":["`+skillID+`"],"targetAgents":["codex"]}`)
	var envelope operationPlanEnvelope
	if err := json.Unmarshal(preview.Body.Bytes(), &envelope); err != nil || preview.Code != http.StatusCreated {
		t.Fatalf("create queued plan = %d %s %v", preview.Code, preview.Body.String(), err)
	}
	server.operationMu.Lock()
	responseCh := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		responseCh <- request(t, server, http.MethodPost, "/api/v1/plans/"+envelope.Plan.ID+"/execute", `{}`)
	}()
	deadline := time.Now().Add(2 * time.Second)
	for {
		server.plansMu.Lock()
		state := server.plans[envelope.Plan.ID].state
		server.plansMu.Unlock()
		if state == storedPlanExecuting {
			break
		}
		if time.Now().After(deadline) {
			server.operationMu.Unlock()
			t.Fatal("execute request did not queue behind the session operation lock")
		}
		runtime.Gosched()
	}
	nowMu.Lock()
	now = envelope.Plan.ExpiresAt
	nowMu.Unlock()
	server.operationMu.Unlock()
	result := <-responseCh
	if result.Code != http.StatusGone || !strings.Contains(result.Body.String(), "plan_expired") {
		t.Fatalf("queued expired execution = %d %s, want plan_expired", result.Code, result.Body.String())
	}
	target, _ := adapter.For(adapter.Codex)
	if _, err := os.Lstat(target.ProjectSkillPath(project, skillID)); !os.IsNotExist(err) {
		t.Fatalf("expired queued plan changed project state, lstat error = %v", err)
	}
}

func TestRemovalAndUndoUseReviewableJournalPlans(t *testing.T) {
	server, project, _, skillID := operationFixture(t)
	projectID := server.project.ID
	activation := request(t, server, http.MethodPost, "/api/v1/projects/"+projectID+"/plans/activate", `{"skillIdentifiers":["`+skillID+`"],"targetAgents":["codex"]}`)
	var activationEnvelope operationPlanEnvelope
	if err := json.Unmarshal(activation.Body.Bytes(), &activationEnvelope); err != nil || activation.Code != http.StatusCreated {
		t.Fatalf("activation plan = %d %s %v", activation.Code, activation.Body.String(), err)
	}
	if result := request(t, server, http.MethodPost, "/api/v1/plans/"+activationEnvelope.Plan.ID+"/execute", `{}`); result.Code != http.StatusOK {
		t.Fatalf("activation execute = %d %s", result.Code, result.Body.String())
	}
	target, _ := adapter.For(adapter.Codex)
	destination := target.ProjectSkillPath(project, skillID)

	removal := request(t, server, http.MethodPost, "/api/v1/projects/"+projectID+"/plans/remove", `{"skillIdentifier":"`+skillID+`","targetAgent":"codex"}`)
	var removalEnvelope operationPlanEnvelope
	if err := json.Unmarshal(removal.Body.Bytes(), &removalEnvelope); err != nil || removal.Code != http.StatusCreated {
		t.Fatalf("removal plan = %d %s %v", removal.Code, removal.Body.String(), err)
	}
	if result := request(t, server, http.MethodPost, "/api/v1/plans/"+removalEnvelope.Plan.ID+"/execute", `{}`); result.Code != http.StatusOK {
		t.Fatalf("removal execute = %d %s", result.Code, result.Body.String())
	}
	if _, err := os.Lstat(destination); !os.IsNotExist(err) {
		t.Fatalf("removed destination still exists, lstat error = %v", err)
	}

	undo := request(t, server, http.MethodPost, "/api/v1/projects/"+projectID+"/plans/undo", `{}`)
	var undoEnvelope operationPlanEnvelope
	if err := json.Unmarshal(undo.Body.Bytes(), &undoEnvelope); err != nil || undo.Code != http.StatusCreated || len(undoEnvelope.Plan.Changes) == 0 {
		t.Fatalf("Undo plan = %d %s %v", undo.Code, undo.Body.String(), err)
	}
	if result := request(t, server, http.MethodPost, "/api/v1/plans/"+undoEnvelope.Plan.ID+"/execute", `{}`); result.Code != http.StatusOK {
		t.Fatalf("Undo execute = %d %s", result.Code, result.Body.String())
	}
	if info, err := os.Lstat(destination); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("Undo did not restore managed link: %v, %v", info, err)
	}
}

func TestUndoPlanRejectsChangedPathAndPreservesCurrentOwner(t *testing.T) {
	server, project, _, skillID := operationFixture(t)
	projectID := server.project.ID
	activation := request(t, server, http.MethodPost, "/api/v1/projects/"+projectID+"/plans/activate", `{"skillIdentifiers":["`+skillID+`"],"targetAgents":["codex"]}`)
	var activationEnvelope operationPlanEnvelope
	if err := json.Unmarshal(activation.Body.Bytes(), &activationEnvelope); err != nil || activation.Code != http.StatusCreated {
		t.Fatalf("activation plan = %d %s %v", activation.Code, activation.Body.String(), err)
	}
	if result := request(t, server, http.MethodPost, "/api/v1/plans/"+activationEnvelope.Plan.ID+"/execute", `{}`); result.Code != http.StatusOK {
		t.Fatalf("activation execute = %d %s", result.Code, result.Body.String())
	}
	undo := request(t, server, http.MethodPost, "/api/v1/projects/"+projectID+"/plans/undo", `{}`)
	var undoEnvelope operationPlanEnvelope
	if err := json.Unmarshal(undo.Body.Bytes(), &undoEnvelope); err != nil || undo.Code != http.StatusCreated {
		t.Fatalf("Undo plan = %d %s %v", undo.Code, undo.Body.String(), err)
	}
	target, _ := adapter.For(adapter.Codex)
	destination := target.ProjectSkillPath(project, skillID)
	if err := os.Remove(destination); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destination, []byte("new owner"), 0o644); err != nil {
		t.Fatal(err)
	}
	result := request(t, server, http.MethodPost, "/api/v1/plans/"+undoEnvelope.Plan.ID+"/execute", `{}`)
	if result.Code != http.StatusConflict || !strings.Contains(result.Body.String(), "stale_plan") {
		t.Fatalf("stale Undo execute = %d %s", result.Code, result.Body.String())
	}
	if got, err := os.ReadFile(destination); err != nil || string(got) != "new owner" {
		t.Fatalf("Undo changed current path owner: %q, %v", got, err)
	}
}

func TestForcedReplacementRequiresAndCapturesExplicitConfirmation(t *testing.T) {
	server, project, _, skillID := operationFixture(t)
	projectID := server.project.ID
	target, _ := adapter.For(adapter.Codex)
	destination := target.ProjectSkillPath(project, skillID)
	if err := os.MkdirAll(destination, 0o755); err != nil {
		t.Fatal(err)
	}
	ownerPath := filepath.Join(destination, "owner.txt")
	if err := os.WriteFile(ownerPath, []byte("unmanaged"), 0o644); err != nil {
		t.Fatal(err)
	}
	withoutForce := request(t, server, http.MethodPost, "/api/v1/projects/"+projectID+"/plans/activate", `{"skillIdentifiers":["`+skillID+`"],"targetAgents":["codex"],"conflictStrategy":"replace"}`)
	if withoutForce.Code != http.StatusBadRequest {
		t.Fatalf("replacement without force confirmation status = %d, body = %s", withoutForce.Code, withoutForce.Body.String())
	}
	withForce := request(t, server, http.MethodPost, "/api/v1/projects/"+projectID+"/plans/activate", `{"skillIdentifiers":["`+skillID+`"],"targetAgents":["codex"],"conflictStrategy":"replace","forceConfirmed":true}`)
	var envelope operationPlanEnvelope
	if err := json.Unmarshal(withForce.Body.Bytes(), &envelope); err != nil || withForce.Code != http.StatusCreated || !envelope.Plan.ForceReplacementConfirmed {
		t.Fatalf("confirmed replacement plan = %d %#v body %s err %v", withForce.Code, envelope.Plan, withForce.Body.String(), err)
	}
	if result := request(t, server, http.MethodPost, "/api/v1/plans/"+envelope.Plan.ID+"/execute", `{}`); result.Code != http.StatusOK {
		t.Fatalf("replacement execute = %d %s", result.Code, result.Body.String())
	}
	if _, err := os.Stat(ownerPath); !os.IsNotExist(err) {
		t.Fatalf("confirmed replacement left conflict content in place, stat error = %v", err)
	}
	if info, err := os.Lstat(destination); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("replacement destination = %v, %v; want managed link", info, err)
	}
}

func TestActivationJournalFailureRollsBackPublishedLink(t *testing.T) {
	server, project, _, skillID := operationFixture(t)
	projectID := server.project.ID
	journalDir := filepath.Join(project, ".skill-manager")
	if err := os.MkdirAll(journalDir, 0o755); err != nil {
		t.Fatal(err)
	}
	journalPath := filepath.Join(journalDir, "journal.json")
	if err := os.WriteFile(journalPath, []byte("not a journal"), 0o600); err != nil {
		t.Fatal(err)
	}
	preview := request(t, server, http.MethodPost, "/api/v1/projects/"+projectID+"/plans/activate", `{"skillIdentifiers":["`+skillID+`"],"targetAgents":["codex"]}`)
	var envelope operationPlanEnvelope
	if err := json.Unmarshal(preview.Body.Bytes(), &envelope); err != nil || preview.Code != http.StatusCreated {
		t.Fatalf("activation plan = %d %s %v", preview.Code, preview.Body.String(), err)
	}
	result := request(t, server, http.MethodPost, "/api/v1/plans/"+envelope.Plan.ID+"/execute", `{}`)
	if result.Code != http.StatusInternalServerError || !strings.Contains(result.Body.String(), "operation_failed") {
		t.Fatalf("activation with invalid journal = %d %s", result.Code, result.Body.String())
	}
	target, _ := adapter.For(adapter.Codex)
	destination := target.ProjectSkillPath(project, skillID)
	if _, err := os.Lstat(destination); !os.IsNotExist(err) {
		t.Fatalf("failed activation left project link behind: lstat error = %v", err)
	}
	if contents, err := os.ReadFile(journalPath); err != nil || string(contents) != "not a journal" {
		t.Fatalf("failed activation changed journal bytes: %q, %v", contents, err)
	}
}

func operationFixture(t *testing.T) (*Server, string, string, string) {
	t.Helper()
	root := t.TempDir()
	project := filepath.Join(root, "project")
	library := filepath.Join(root, "library")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(library, 0o755); err != nil {
		t.Fatal(err)
	}
	skillID := "web-helper"
	skill := filepath.Join(library, skillID)
	if err := os.Mkdir(skill, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skill, "SKILL.md"), []byte("---\nname: Web Helper\ndescription: Helper\n---\nBody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	server, err := New(Options{Origin: "http://127.0.0.1:8787", LibraryPath: library, HomeDir: root, DataRoot: filepath.Join(root, "data"), ProjectPath: project})
	if err != nil {
		t.Fatal(err)
	}
	return server, project, library, skillID
}
