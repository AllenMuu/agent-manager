package webconsole

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/AllenMuu/skill-manager/internal/adapter"
	"github.com/AllenMuu/skill-manager/internal/catalog"
	"github.com/AllenMuu/skill-manager/internal/lifecycle"
	"github.com/AllenMuu/skill-manager/internal/operation"
	"github.com/AllenMuu/skill-manager/internal/skillrepo"
	"github.com/AllenMuu/skill-manager/internal/skillsync"
)

func TestSyncAPIRequiresSessionAndTrustedOrigin(t *testing.T) {
	server, _, _, _ := newSyncTestServer(t, false)
	unauthenticated := httptest.NewRecorder()
	server.ServeHTTP(unauthenticated, httptest.NewRequest(http.MethodGet, "/api/v1/sync/sources", nil))
	if unauthenticated.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated sources status = %d, want 401", unauthenticated.Code)
	}

	request := authenticatedRequest(t, server, http.MethodPut, "/api/v1/sync/settings")
	request.Header.Set("Content-Type", "application/json")
	request.Body = io.NopCloser(strings.NewReader(`{"syncMode":"copy-only"}`))
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("settings mutation without trusted origin status = %d, body=%s, want 403", response.Code, response.Body.String())
	}

	request = authenticatedRequest(t, server, http.MethodGet, "/api/v1/sync/sources")
	request.Header.Set("Origin", "https://attacker.example")
	response = httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("sources with untrusted origin status = %d, body=%s, want 403", response.Code, response.Body.String())
	}
}

func TestSyncImportPlanExpiresWhileWaitingForSessionOperationLock(t *testing.T) {
	server, home, library, _ := newSyncTestServer(t, false)
	var nowMu sync.RWMutex
	now := time.Now().UTC()
	server.now = func() time.Time {
		nowMu.RLock()
		defer nowMu.RUnlock()
		return now
	}
	source := filepath.Join(home, ".claude", "skills", "queued-expiry")
	writeSyncSkill(t, source, "Queued Expiry", "A queued import expiry test Skill.")

	inventory := syncHTTPRequest(server, http.MethodGet, "/api/v1/sync/sources", "")
	var sources struct {
		Groups []skillsync.SourceGroup `json:"groups"`
	}
	if err := json.Unmarshal(inventory.Body.Bytes(), &sources); err != nil || inventory.Code != http.StatusOK || len(sources.Groups) != 1 {
		t.Fatalf("source inventory = %d %s; decode error = %v", inventory.Code, inventory.Body.String(), err)
	}
	preview := syncHTTPRequest(server, http.MethodPost, "/api/v1/sync/import/preview", `{"selectedGroupIds":["`+sources.Groups[0].ID+`"]}`)
	var plan struct {
		PlanID string `json:"planId"`
	}
	if err := json.Unmarshal(preview.Body.Bytes(), &plan); err != nil || preview.Code != http.StatusCreated || plan.PlanID == "" {
		t.Fatalf("import preview = %d %s; decode error = %v", preview.Code, preview.Body.String(), err)
	}

	server.operationMu.Lock()
	responseCh := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		responseCh <- syncHTTPRequest(server, http.MethodPost, "/api/v1/sync/import/apply", `{"planId":"`+plan.PlanID+`"}`)
	}()
	deadline := time.Now().Add(2 * time.Second)
	for {
		server.plansMu.Lock()
		_, exists := server.syncPlans[plan.PlanID]
		server.plansMu.Unlock()
		if !exists {
			break
		}
		if time.Now().After(deadline) {
			server.operationMu.Unlock()
			t.Fatal("import request did not consume its preview before waiting for the session operation lock")
		}
		runtime.Gosched()
	}
	nowMu.Lock()
	now = now.Add(syncPlanTTL)
	nowMu.Unlock()
	server.operationMu.Unlock()

	result := <-responseCh
	if result.Code != http.StatusConflict || !strings.Contains(result.Body.String(), "stale_plan") {
		t.Fatalf("queued expired import = %d %s, want stale_plan", result.Code, result.Body.String())
	}
	repository, err := skillrepo.Open(library)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.Resolve("queued-expiry", ""); err == nil {
		t.Fatal("expired queued import changed the central Skill library")
	}
}

func TestSyncImportPlanExpiresWhileWaitingForSharedImportLock(t *testing.T) {
	server, home, library, _ := newSyncTestServer(t, false)
	var nowMu sync.RWMutex
	now := time.Now().UTC()
	server.now = func() time.Time {
		nowMu.RLock()
		defer nowMu.RUnlock()
		return now
	}
	source := filepath.Join(home, ".claude", "skills", "shared-lock-expiry")
	writeSyncSkill(t, source, "Shared Lock Expiry", "A shared import lock expiry test Skill.")
	inventory := syncHTTPRequest(server, http.MethodGet, "/api/v1/sync/sources", "")
	var sources struct {
		Groups []skillsync.SourceGroup `json:"groups"`
	}
	if err := json.Unmarshal(inventory.Body.Bytes(), &sources); err != nil || inventory.Code != http.StatusOK || len(sources.Groups) != 1 {
		t.Fatalf("source inventory = %d %s; decode error = %v", inventory.Code, inventory.Body.String(), err)
	}
	preview := syncHTTPRequest(server, http.MethodPost, "/api/v1/sync/import/preview", `{"selectedGroupIds":["`+sources.Groups[0].ID+`"]}`)
	var plan struct {
		PlanID string `json:"planId"`
	}
	if err := json.Unmarshal(preview.Body.Bytes(), &plan); err != nil || preview.Code != http.StatusCreated || plan.PlanID == "" {
		t.Fatalf("import preview = %d %s; decode error = %v", preview.Code, preview.Body.String(), err)
	}
	unlock, err := operation.New(filepath.Join(home, ".agent-manager", "skill-sync", "imports.lock")).LockOperation()
	if err != nil {
		t.Fatal(err)
	}
	responseCh := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		responseCh <- syncHTTPRequest(server, http.MethodPost, "/api/v1/sync/import/apply", `{"planId":"`+plan.PlanID+`"}`)
	}()
	deadline := time.Now().Add(2 * time.Second)
	for {
		server.plansMu.Lock()
		_, exists := server.syncPlans[plan.PlanID]
		server.plansMu.Unlock()
		if !exists {
			break
		}
		if time.Now().After(deadline) {
			_ = unlock()
			t.Fatal("import request did not consume its preview before waiting for the shared import lock")
		}
		runtime.Gosched()
	}
	nowMu.Lock()
	now = now.Add(syncPlanTTL)
	nowMu.Unlock()
	if err := unlock(); err != nil {
		t.Fatal(err)
	}
	result := <-responseCh
	if result.Code != http.StatusConflict || !strings.Contains(result.Body.String(), "stale_plan") {
		t.Fatalf("queued expired import = %d %s, want stale_plan", result.Code, result.Body.String())
	}
	repository, err := skillrepo.Open(library)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.Resolve("shared-lock-expiry", ""); err == nil {
		t.Fatal("expired import changed the central Skill library after acquiring the shared lock")
	}
}

func TestActivationPreviewAndSyncSettingsUpdateAreRaceFree(t *testing.T) {
	server, _, library, _ := newSyncTestServer(t, true)
	repository, err := skillrepo.Open(library)
	if err != nil {
		t.Fatal(err)
	}
	skillSource := t.TempDir()
	writeSyncSkill(t, skillSource, "Settings Race", "A sync settings race test Skill.")
	if _, err := repository.AddVersion("settings-race", skillSource, skillrepo.VersionMetadata{Key: "v1", Version: "1"}, true); err != nil {
		t.Fatal(err)
	}

	const iterations = 40
	start := make(chan struct{})
	errors := make(chan string, 2)
	var workers sync.WaitGroup
	workers.Add(2)
	go func() {
		defer workers.Done()
		<-start
		for index := 0; index < iterations; index++ {
			mode := "auto"
			if index%2 == 0 {
				mode = "copy-only"
			}
			response := syncHTTPRequest(server, http.MethodPut, "/api/v1/sync/settings", `{"syncMode":"`+mode+`"}`)
			if response.Code != http.StatusOK {
				errors <- "settings update: " + response.Body.String()
				return
			}
		}
	}()
	go func() {
		defer workers.Done()
		<-start
		for index := 0; index < iterations; index++ {
			response := syncHTTPRequest(server, http.MethodPost, "/api/v1/projects/"+server.project.ID+"/plans/activate", `{"skillIdentifiers":["settings-race"],"targetAgents":["codex"]}`)
			if response.Code != http.StatusCreated {
				errors <- "activation preview: " + response.Body.String()
				return
			}
		}
	}()
	close(start)
	workers.Wait()
	close(errors)
	for err := range errors {
		t.Error(err)
	}
}

func TestSyncAPISettingsAndGlobalImportPreserveSources(t *testing.T) {
	server, home, library, _ := newSyncTestServer(t, false)
	source := filepath.Join(home, ".claude", "skills", "global-demo")
	writeSyncSkill(t, source, "Global Demo", "A global test Skill.")

	settings := request(t, server, http.MethodGet, "/api/v1/sync/settings", "")
	if settings.Code != http.StatusOK || !strings.Contains(settings.Body.String(), `"syncMode":"auto"`) {
		t.Fatalf("initial settings = %d %s, want default auto", settings.Code, settings.Body.String())
	}
	settings = request(t, server, http.MethodPut, "/api/v1/sync/settings", `{"syncMode":"copy-only"}`)
	if settings.Code != http.StatusOK || !strings.Contains(settings.Body.String(), `"syncMode":"copy-only"`) {
		t.Fatalf("save settings = %d %s, want copy-only", settings.Code, settings.Body.String())
	}

	response := request(t, server, http.MethodGet, "/api/v1/sync/sources", "")
	if response.Code != http.StatusOK {
		t.Fatalf("source inventory = %d %s", response.Code, response.Body.String())
	}
	var inventory struct {
		Groups []skillsync.SourceGroup `json:"groups"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &inventory); err != nil || len(inventory.Groups) != 1 {
		t.Fatalf("decode source inventory: groups=%+v err=%v", inventory.Groups, err)
	}
	group := inventory.Groups[0]
	if group.Identifier != "global-demo" || len(group.Sources) != 1 || group.Sources[0].Scope != skillsync.ScopeGlobal {
		t.Fatalf("source group = %+v, want one global Skill", group)
	}

	response = request(t, server, http.MethodPost, "/api/v1/sync/import/preview", `{"selectedGroupIds":["`+group.ID+`"],"path":"/tmp/untrusted"}`)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("arbitrary source path preview status = %d body=%s, want 400", response.Code, response.Body.String())
	}
	response = request(t, server, http.MethodPost, "/api/v1/sync/import/preview", `{"selectedGroupIds":["`+group.ID+`"]}`)
	if response.Code != http.StatusCreated {
		t.Fatalf("import preview = %d %s", response.Code, response.Body.String())
	}
	var preview struct {
		PlanID  string            `json:"planId"`
		Preview skillsync.Preview `json:"preview"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &preview); err != nil || preview.PlanID == "" {
		t.Fatalf("decode import preview: plan=%+v err=%v", preview, err)
	}
	sourceFile := filepath.Join(source, "SKILL.md")
	originalSource, err := os.ReadFile(sourceFile)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sourceFile, append(originalSource, []byte("changed after review\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	stale := request(t, server, http.MethodPost, "/api/v1/sync/import/apply", `{"planId":"`+preview.PlanID+`"}`)
	if stale.Code != http.StatusConflict {
		t.Fatalf("stale import apply = %d %s, want 409", stale.Code, stale.Body.String())
	}
	if err := os.WriteFile(sourceFile, originalSource, 0o644); err != nil {
		t.Fatal(err)
	}
	response = request(t, server, http.MethodPost, "/api/v1/sync/import/preview", `{"selectedGroupIds":["`+group.ID+`"]}`)
	if response.Code != http.StatusCreated {
		t.Fatalf("fresh import preview after stale rejection = %d %s", response.Code, response.Body.String())
	}
	if err := json.Unmarshal(response.Body.Bytes(), &preview); err != nil || preview.PlanID == "" {
		t.Fatalf("decode refreshed import preview: plan=%+v err=%v", preview, err)
	}
	response = request(t, server, http.MethodPost, "/api/v1/sync/import/apply", `{"planId":"`+preview.PlanID+`"}`)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"imported":1`) {
		t.Fatalf("apply import = %d %s, want one imported version", response.Code, response.Body.String())
	}
	if _, err := os.Stat(filepath.Join(source, "SKILL.md")); err != nil {
		t.Fatalf("import changed source Skill: %v", err)
	}
	repository, err := skillrepo.Open(library)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.Resolve("global-demo", ""); err != nil {
		t.Fatalf("imported version missing from central library: %v", err)
	}

	response = request(t, server, http.MethodPost, "/api/v1/sync/import/undo/preview", `{}`)
	if response.Code != http.StatusNotFound {
		t.Fatalf("import Undo route = %d %s, want not found because imported versions may be active in unregistered projects", response.Code, response.Body.String())
	}
	if _, err := repository.Resolve("global-demo", ""); err != nil {
		t.Fatalf("imported version was removed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(source, "SKILL.md")); err != nil {
		t.Fatalf("import changed source Skill: %v", err)
	}
}

func TestSyncAPIProjectResyncUsesScopedJournalAndUndo(t *testing.T) {
	server, _, library, project := newSyncTestServer(t, true)
	project = server.currentProject().Path
	source := filepath.Join(project, ".agents", "skills", "resync-demo")
	writeSyncSkill(t, source, "Resync Demo", "A resync test Skill.")
	repository, err := skillrepo.Open(library)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.AddVersion("resync-demo", source, skillrepo.VersionMetadata{Key: "v1", Version: "1"}, true); err != nil {
		t.Fatal(err)
	}
	skills, _, err := catalog.Discover(library)
	if err != nil || len(skills) != 1 {
		t.Fatalf("discover imported test Skill: skills=%+v err=%v", skills, err)
	}
	journal, err := server.journalForProject(server.currentProject())
	if err != nil {
		t.Fatal(err)
	}
	activation := lifecycle.New(library, journal, func(operation.Plan) bool { return true })
	activation.Materialization = adapter.MaterializeCopyOnly
	if _, err := activation.AddMany(project, skills, []adapter.Target{adapter.Codex}); err != nil {
		t.Fatalf("seed managed project activation: %v", err)
	}
	response := request(t, server, http.MethodPost, "/api/v1/sync/resync/undo/preview", `{"scope":"project"}`)
	if response.Code != http.StatusConflict {
		t.Fatalf("Undo preview for a normal activation = %d %s, want 409", response.Code, response.Body.String())
	}
	response = request(t, server, http.MethodPost, "/api/v1/sync/resync/preview", `{"scope":"project"}`)
	if response.Code != http.StatusCreated {
		t.Fatalf("project resync preview = %d %s", response.Code, response.Body.String())
	}
	var preview struct {
		PlanID string         `json:"planId"`
		Plan   operation.Plan `json:"plan"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &preview); err != nil || preview.PlanID == "" || len(preview.Plan.Changes) != 1 {
		t.Fatalf("decode project resync preview: %+v err=%v", preview, err)
	}
	target, _ := adapter.For(adapter.Codex)
	destination := target.ProjectSkillPath(project, "resync-demo")
	if preview.Plan.Changes[0].Path != destination {
		t.Fatalf("resync destination = %q, want %q", preview.Plan.Changes[0].Path, destination)
	}
	response = request(t, server, http.MethodPost, "/api/v1/sync/resync/apply", `{"planId":"`+preview.PlanID+`"}`)
	if response.Code != http.StatusOK {
		t.Fatalf("apply project resync = %d %s", response.Code, response.Body.String())
	}
	response = request(t, server, http.MethodGet, "/api/v1/sync/sources", "")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"projectResyncUndoAvailable":true`) {
		t.Fatalf("project inventory did not expose resync Undo availability: %d %s", response.Code, response.Body.String())
	}

	response = request(t, server, http.MethodPost, "/api/v1/sync/resync/undo/preview", `{"scope":"project"}`)
	if response.Code != http.StatusCreated {
		t.Fatalf("project resync Undo preview = %d %s", response.Code, response.Body.String())
	}
	if err := json.Unmarshal(response.Body.Bytes(), &preview); err != nil || preview.PlanID == "" {
		t.Fatalf("decode project resync Undo preview: %+v err=%v", preview, err)
	}
	response = request(t, server, http.MethodPost, "/api/v1/sync/resync/undo/apply", `{"planId":"`+preview.PlanID+`"}`)
	if response.Code != http.StatusOK {
		t.Fatalf("apply project resync Undo = %d %s", response.Code, response.Body.String())
	}
	info, err := os.Lstat(destination)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("Undo did not restore the exact prior copied activation: info=%v err=%v", info, err)
	}
	response = request(t, server, http.MethodGet, "/api/v1/sync/sources", "")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"projectResyncUndoAvailable":false`) {
		t.Fatalf("project inventory retained resync Undo after it was consumed: %d %s", response.Code, response.Body.String())
	}
}

func TestSyncAPIGlobalResyncUsesGlobalJournalAndUndo(t *testing.T) {
	server, home, library, _ := newSyncTestServer(t, false)
	home = server.homeDir
	source := filepath.Join(home, ".agent", "skills", "global-resync")
	writeSyncSkill(t, source, "Global Resync", "A global resync test Skill.")
	repository, err := skillrepo.Open(library)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.AddVersion("global-resync", source, skillrepo.VersionMetadata{Key: "v1", Version: "1"}, true); err != nil {
		t.Fatal(err)
	}
	skills, _, err := catalog.Discover(library)
	if err != nil || len(skills) != 1 {
		t.Fatalf("discover global test Skill: skills=%+v err=%v", skills, err)
	}
	activation := lifecycle.New(library, operation.GlobalJournal(home), func(operation.Plan) bool { return true })
	activation.Materialization = adapter.MaterializeCopyOnly
	if _, err := activation.AddGlobalMany(home, skills, []adapter.Target{adapter.Codex}); err != nil {
		t.Fatalf("seed managed global activation: %v", err)
	}

	response := request(t, server, http.MethodPost, "/api/v1/sync/resync/preview", `{"scope":"global"}`)
	if response.Code != http.StatusCreated {
		t.Fatalf("global resync preview = %d %s", response.Code, response.Body.String())
	}
	var preview struct {
		PlanID string         `json:"planId"`
		Plan   operation.Plan `json:"plan"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &preview); err != nil || preview.PlanID == "" || len(preview.Plan.Changes) != 1 {
		t.Fatalf("decode global resync preview: %+v err=%v", preview, err)
	}
	target, _ := adapter.ForAgent(adapter.Codex)
	destination := target.GlobalSkillPath(home, "global-resync")
	if preview.Plan.Changes[0].Path != destination {
		t.Fatalf("global resync destination = %q, want %q", preview.Plan.Changes[0].Path, destination)
	}
	response = request(t, server, http.MethodPost, "/api/v1/sync/resync/apply", `{"planId":"`+preview.PlanID+`"}`)
	if response.Code != http.StatusOK {
		t.Fatalf("apply global resync = %d %s", response.Code, response.Body.String())
	}
	response = request(t, server, http.MethodGet, "/api/v1/sync/sources", "")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"globalResyncUndoAvailable":true`) {
		t.Fatalf("global inventory did not expose resync Undo availability: %d %s", response.Code, response.Body.String())
	}
	response = request(t, server, http.MethodPost, "/api/v1/sync/resync/undo/preview", `{"scope":"global"}`)
	if response.Code != http.StatusCreated {
		t.Fatalf("global resync Undo preview = %d %s", response.Code, response.Body.String())
	}
	if err := json.Unmarshal(response.Body.Bytes(), &preview); err != nil || preview.PlanID == "" {
		t.Fatalf("decode global resync Undo preview: %+v err=%v", preview, err)
	}
	response = request(t, server, http.MethodPost, "/api/v1/sync/resync/undo/apply", `{"planId":"`+preview.PlanID+`"}`)
	if response.Code != http.StatusOK {
		t.Fatalf("apply global resync Undo = %d %s", response.Code, response.Body.String())
	}
	info, err := os.Lstat(destination)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("global Undo did not restore the copied activation: info=%v err=%v", info, err)
	}
	response = request(t, server, http.MethodGet, "/api/v1/sync/sources", "")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"globalResyncUndoAvailable":false`) {
		t.Fatalf("global inventory retained resync Undo after it was consumed: %d %s", response.Code, response.Body.String())
	}
}

func TestResyncPlanExpiresWhileWaitingForScopeOperationLock(t *testing.T) {
	server, home, library, project := newSyncTestServer(t, true)
	project = server.currentProject().Path
	var nowMu sync.RWMutex
	now := time.Now().UTC()
	server.now = func() time.Time {
		nowMu.RLock()
		defer nowMu.RUnlock()
		return now
	}
	source := filepath.Join(home, ".claude", "skills", "resync-lock-expiry")
	writeSyncSkill(t, source, "Resync Lock Expiry", "A resync lock expiry test Skill.")
	repository, err := skillrepo.Open(library)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.AddVersion("resync-lock-expiry", source, skillrepo.VersionMetadata{Key: "v1", Version: "1"}, true); err != nil {
		t.Fatal(err)
	}
	skills, _, err := catalog.Discover(library)
	if err != nil || len(skills) != 1 {
		t.Fatalf("discover managed test Skill: skills=%+v err=%v", skills, err)
	}
	journal, err := server.journalForProject(server.currentProject())
	if err != nil {
		t.Fatal(err)
	}
	activation := lifecycle.New(library, journal, func(operation.Plan) bool { return true })
	activation.Materialization = adapter.MaterializeAuto
	if _, err := activation.AddMany(project, skills, []adapter.Target{adapter.Codex}); err != nil {
		t.Fatal(err)
	}
	if response := request(t, server, http.MethodPut, "/api/v1/sync/settings", `{"syncMode":"copy-only"}`); response.Code != http.StatusOK {
		t.Fatalf("save copy-only mode = %d %s", response.Code, response.Body.String())
	}
	preview := request(t, server, http.MethodPost, "/api/v1/sync/resync/preview", `{"scope":"project"}`)
	var plan struct {
		PlanID string `json:"planId"`
	}
	if err := json.Unmarshal(preview.Body.Bytes(), &plan); err != nil || preview.Code != http.StatusCreated {
		t.Fatalf("resync preview = %d %s %v", preview.Code, preview.Body.String(), err)
	}
	target, _ := adapter.For(adapter.Codex)
	destination := target.ProjectSkillPath(project, "resync-lock-expiry")
	unlock, err := operation.ProjectJournal(project).LockOperation()
	if err != nil {
		t.Fatal(err)
	}
	responseCh := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		responseCh <- syncHTTPRequest(server, http.MethodPost, "/api/v1/sync/resync/apply", `{"planId":"`+plan.PlanID+`"}`)
	}()
	deadline := time.Now().Add(2 * time.Second)
	for {
		server.plansMu.Lock()
		_, exists := server.syncPlans[plan.PlanID]
		server.plansMu.Unlock()
		if !exists {
			break
		}
		if time.Now().After(deadline) {
			_ = unlock()
			t.Fatal("resync request did not consume its preview before waiting for the scope operation lock")
		}
		runtime.Gosched()
	}
	nowMu.Lock()
	now = now.Add(syncPlanTTL)
	nowMu.Unlock()
	if err := unlock(); err != nil {
		t.Fatal(err)
	}
	result := <-responseCh
	if result.Code != http.StatusConflict || !strings.Contains(result.Body.String(), "stale_plan") {
		t.Fatalf("queued expired resync = %d %s, want stale_plan", result.Code, result.Body.String())
	}
	info, err := os.Lstat(destination)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("expired resync changed the existing link: info=%v err=%v", info, err)
	}
}

func TestResyncUndoRejectsAReplacementJournalEntryWithTheSamePlan(t *testing.T) {
	server, home, library, project := newSyncTestServer(t, true)
	project = server.currentProject().Path
	source := filepath.Join(home, ".claude", "skills", "undo-repeat")
	writeSyncSkill(t, source, "Undo Repeat", "A repeated resync Undo test Skill.")
	repository, err := skillrepo.Open(library)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.AddVersion("undo-repeat", source, skillrepo.VersionMetadata{Key: "v1", Version: "1"}, true); err != nil {
		t.Fatal(err)
	}
	skills, _, err := catalog.Discover(library)
	if err != nil || len(skills) != 1 {
		t.Fatalf("discover managed test Skill: skills=%+v err=%v", skills, err)
	}
	journal, err := server.journalForProject(server.currentProject())
	if err != nil {
		t.Fatal(err)
	}
	activation := lifecycle.New(library, journal, func(operation.Plan) bool { return true })
	activation.Materialization = adapter.MaterializeCopyOnly
	if _, err := activation.AddMany(project, skills, []adapter.Target{adapter.Codex}); err != nil {
		t.Fatal(err)
	}
	applyResync := func(attempt int) {
		preview := request(t, server, http.MethodPost, "/api/v1/sync/resync/preview", `{"scope":"project"}`)
		var plan struct {
			PlanID string `json:"planId"`
		}
		if err := json.Unmarshal(preview.Body.Bytes(), &plan); err != nil || preview.Code != http.StatusCreated {
			t.Fatalf("resync preview %d = %d %s %v", attempt, preview.Code, preview.Body.String(), err)
		}
		if result := request(t, server, http.MethodPost, "/api/v1/sync/resync/apply", `{"planId":"`+plan.PlanID+`"}`); result.Code != http.StatusOK {
			t.Fatalf("resync apply %d = %d %s", attempt, result.Code, result.Body.String())
		}
	}
	applyResync(1)
	undoPreview := request(t, server, http.MethodPost, "/api/v1/sync/resync/undo/preview", `{"scope":"project"}`)
	var undoPlan struct {
		PlanID string `json:"planId"`
	}
	if err := json.Unmarshal(undoPreview.Body.Bytes(), &undoPlan); err != nil || undoPreview.Code != http.StatusCreated {
		t.Fatalf("Undo preview = %d %s %v", undoPreview.Code, undoPreview.Body.String(), err)
	}
	applyResync(2)
	latestBefore, found, err := journal.Latest()
	if err != nil || !found {
		t.Fatalf("read latest resync journal entry: found=%v err=%v", found, err)
	}
	if result := request(t, server, http.MethodPost, "/api/v1/sync/resync/undo/apply", `{"planId":"`+undoPlan.PlanID+`"}`); result.Code != http.StatusConflict || !strings.Contains(result.Body.String(), "stale_plan") {
		t.Fatalf("stale Undo apply = %d %s, want stale_plan", result.Code, result.Body.String())
	}
	latestAfter, found, err := journal.Latest()
	if err != nil || !found || !reflect.DeepEqual(latestBefore, latestAfter) {
		t.Fatalf("stale Undo changed the latest journal entry: before=%+v after=%+v err=%v", latestBefore, latestAfter, err)
	}
}

func newSyncTestServer(t *testing.T, registerProject bool) (*Server, string, string, string) {
	t.Helper()
	root := t.TempDir()
	home := filepath.Join(root, "home")
	library := filepath.Join(home, ".agent-manager", "skills")
	project := filepath.Join(root, "project")
	for _, path := range []string{home, project} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	options := Options{
		Origin: "http://127.0.0.1:8787", LibraryPath: library,
		ConfigPath: filepath.Join(home, ".agent-manager", "config.yaml"),
		HomeDir:    home, DataRoot: filepath.Join(root, "data"),
		TokenSource: bytes.NewReader(make([]byte, 32)),
		Assets:      fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("console shell")}},
	}
	if err := os.MkdirAll(filepath.Dir(options.ConfigPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(options.ConfigPath, []byte("sync_mode: auto\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if registerProject {
		options.ProjectPath = project
	}
	server, err := New(options)
	if err != nil {
		t.Fatal(err)
	}
	return server, home, library, project
}

func writeSyncSkill(t *testing.T, path, name, description string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	contents := "---\nname: " + name + "\ndescription: " + description + "\n---\n\n# Instructions\n"
	if err := os.WriteFile(filepath.Join(path, "SKILL.md"), []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}

func syncHTTPRequest(server *Server, method, path, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Host = "127.0.0.1:8787"
	request.Header.Set("Authorization", "Bearer "+server.token)
	request.Header.Set("Origin", "http://127.0.0.1:8787")
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	return response
}
