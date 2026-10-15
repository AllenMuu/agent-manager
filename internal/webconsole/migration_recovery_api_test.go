package webconsole

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AllenMuu/skill-manager/internal/skillsync"
)

func TestSyncAPIMigrationPreviewApplyAndUndoUseServerOwnedEntries(t *testing.T) {
	server, home, library, _ := newSyncTestServer(t, false)
	source := filepath.Join(t.TempDir(), "external", "api-demo")
	writeSyncSkill(t, source, "api-demo", "API migration test Skill.")
	entry := filepath.Join(home, ".claude", "skills", "api-demo")
	if err := os.MkdirAll(filepath.Dir(entry), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(source, entry); err != nil {
		t.Skipf("create source reference: %v", err)
	}

	inventoryResponse := syncHTTPRequest(server, http.MethodGet, "/api/v1/sync/sources", "")
	var inventory struct {
		Sources []skillsync.Source `json:"sources"`
	}
	if inventoryResponse.Code != http.StatusOK || json.Unmarshal(inventoryResponse.Body.Bytes(), &inventory) != nil || len(inventory.Sources) != 1 {
		t.Fatalf("source inventory = %d %s", inventoryResponse.Code, inventoryResponse.Body.String())
	}
	importPreview := syncHTTPRequest(server, http.MethodPost, "/api/v1/sync/import/preview", `{"selectedGroupIds":["`+mustSourceGroupID(t, server)+`"]}`)
	if importPreview.Code != http.StatusCreated {
		t.Fatalf("import preview = %d %s", importPreview.Code, importPreview.Body.String())
	}
	var importPlan struct {
		PlanID string `json:"planId"`
	}
	if err := json.Unmarshal(importPreview.Body.Bytes(), &importPlan); err != nil {
		t.Fatal(err)
	}
	if response := syncHTTPRequest(server, http.MethodPost, "/api/v1/sync/import/apply", `{"planId":"`+importPlan.PlanID+`"}`); response.Code != http.StatusOK {
		t.Fatalf("import apply = %d %s", response.Code, response.Body.String())
	}
	previewResponse := syncHTTPRequest(server, http.MethodPost, "/api/v1/sync/migration/preview", `{"selectedSourceIds":["`+inventory.Sources[0].ID+`"]}`)
	if previewResponse.Code != http.StatusCreated {
		t.Fatalf("migration preview = %d %s", previewResponse.Code, previewResponse.Body.String())
	}
	var migrationPlan struct {
		PlanID  string                     `json:"planId"`
		Preview skillsync.MigrationPreview `json:"preview"`
	}
	if err := json.Unmarshal(previewResponse.Body.Bytes(), &migrationPlan); err != nil || len(migrationPlan.Preview.Entries) != 1 || migrationPlan.Preview.Plan.Operation != skillsync.MigrationOperation {
		t.Fatalf("decode migration preview = %#v, err %v", migrationPlan, err)
	}
	if strings.Contains(previewResponse.Body.String(), `"path":"/tmp/`) {
		t.Fatalf("preview unexpectedly accepted a client path: %s", previewResponse.Body.String())
	}
	applyResponse := syncHTTPRequest(server, http.MethodPost, "/api/v1/sync/migration/apply", `{"planId":"`+migrationPlan.PlanID+`"}`)
	if applyResponse.Code != http.StatusOK || !strings.Contains(applyResponse.Body.String(), `"batchId"`) {
		t.Fatalf("migration apply = %d %s", applyResponse.Code, applyResponse.Body.String())
	}
	if target, err := os.Readlink(entry); err != nil || !strings.HasPrefix(target, filepath.Join(library, "api-demo", "versions")) {
		t.Fatalf("migration target = %q, err %v", target, err)
	}
	updatedInventory := syncHTTPRequest(server, http.MethodGet, "/api/v1/sync/sources", "")
	if updatedInventory.Code != http.StatusOK || !strings.Contains(updatedInventory.Body.String(), `"globalMigrationUndoAvailable":true`) {
		t.Fatalf("migration Undo was not made available: %d %s", updatedInventory.Code, updatedInventory.Body.String())
	}
	undoPreview := syncHTTPRequest(server, http.MethodPost, "/api/v1/sync/migration/undo/preview", `{"scope":"global"}`)
	if undoPreview.Code != http.StatusCreated {
		t.Fatalf("migration Undo preview = %d %s", undoPreview.Code, undoPreview.Body.String())
	}
	var undo struct {
		PlanID string `json:"planId"`
	}
	if err := json.Unmarshal(undoPreview.Body.Bytes(), &undo); err != nil {
		t.Fatal(err)
	}
	if response := syncHTTPRequest(server, http.MethodPost, "/api/v1/sync/migration/undo/apply", `{"planId":"`+undo.PlanID+`"}`); response.Code != http.StatusOK {
		t.Fatalf("migration Undo apply = %d %s", response.Code, response.Body.String())
	}
	if target, err := os.Readlink(entry); err != nil || target != source {
		t.Fatalf("Undo did not restore the original link: target=%q err=%v", target, err)
	}
}

func TestSyncAPIMigrationPreviewIsInvalidatedWhenMaterializationModeChanges(t *testing.T) {
	server, home, _, _ := newSyncTestServer(t, false)
	source := filepath.Join(t.TempDir(), "external", "mode-bound")
	writeSyncSkill(t, source, "mode-bound", "Mode-bound migration test Skill.")
	entry := filepath.Join(home, ".claude", "skills", "mode-bound")
	if err := os.MkdirAll(filepath.Dir(entry), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(source, entry); err != nil {
		t.Skipf("create source reference: %v", err)
	}
	importPreview := syncHTTPRequest(server, http.MethodPost, "/api/v1/sync/import/preview", `{"selectedGroupIds":["`+mustSourceGroupID(t, server)+`"]}`)
	if importPreview.Code != http.StatusCreated {
		t.Fatalf("import preview = %d %s", importPreview.Code, importPreview.Body.String())
	}
	var importPlan struct {
		PlanID string `json:"planId"`
	}
	if err := json.Unmarshal(importPreview.Body.Bytes(), &importPlan); err != nil {
		t.Fatal(err)
	}
	if response := syncHTTPRequest(server, http.MethodPost, "/api/v1/sync/import/apply", `{"planId":"`+importPlan.PlanID+`"}`); response.Code != http.StatusOK {
		t.Fatalf("import apply = %d %s", response.Code, response.Body.String())
	}
	sourcesResponse := syncHTTPRequest(server, http.MethodGet, "/api/v1/sync/sources", "")
	var sources struct {
		Sources []skillsync.Source `json:"sources"`
	}
	if sourcesResponse.Code != http.StatusOK || json.Unmarshal(sourcesResponse.Body.Bytes(), &sources) != nil || len(sources.Sources) != 1 {
		t.Fatalf("source inventory = %d %s", sourcesResponse.Code, sourcesResponse.Body.String())
	}
	preview := syncHTTPRequest(server, http.MethodPost, "/api/v1/sync/migration/preview", `{"selectedSourceIds":["`+sources.Sources[0].ID+`"]}`)
	if preview.Code != http.StatusCreated {
		t.Fatalf("migration preview = %d %s", preview.Code, preview.Body.String())
	}
	var migrationPlan struct {
		PlanID string `json:"planId"`
	}
	if err := json.Unmarshal(preview.Body.Bytes(), &migrationPlan); err != nil {
		t.Fatal(err)
	}
	setting := syncHTTPRequest(server, http.MethodPut, "/api/v1/sync/settings", `{"syncMode":"copy-only"}`)
	if setting.Code != http.StatusOK {
		t.Fatalf("change sync mode = %d %s", setting.Code, setting.Body.String())
	}
	apply := syncHTTPRequest(server, http.MethodPost, "/api/v1/sync/migration/apply", `{"planId":"`+migrationPlan.PlanID+`"}`)
	if apply.Code != http.StatusConflict || !strings.Contains(apply.Body.String(), `"stale_plan"`) {
		t.Fatalf("mode-changed migration apply = %d %s; want stale plan", apply.Code, apply.Body.String())
	}
	if target, err := os.Readlink(entry); err != nil || target != source {
		t.Fatalf("stale migration changed the source link: target=%q err=%v", target, err)
	}
}

func TestSyncAPIRecoveryPreviewAndApplyResolvesImportEvidence(t *testing.T) {
	server, home, _, _ := newSyncTestServer(t, false)
	source := filepath.Join(home, ".agents", "skills", "recovery-demo")
	writeSyncSkill(t, source, "recovery-demo", "Recovery API test Skill.")
	service, _, err := server.newSkillSyncService()
	if err != nil {
		t.Fatal(err)
	}
	_, groups, err := service.Inventory()
	if err != nil || len(groups) != 1 {
		t.Fatalf("source inventory = %d, err %v", len(groups), err)
	}
	preview, err := service.Preview([]string{groups[0].ID}, nil)
	if err != nil {
		t.Fatal(err)
	}
	service.BeforeDiscoveryCommit = func([]string) error { return os.ErrPermission }
	if _, err := service.ApplyDetailed(preview); err == nil {
		t.Fatal("injected discovery failure unexpectedly succeeded")
	}
	listResponse := syncHTTPRequest(server, http.MethodGet, "/api/v1/sync/recovery", "")
	var pending struct {
		Batches []skillsync.RecoveryCandidate `json:"batches"`
	}
	if listResponse.Code != http.StatusOK || json.Unmarshal(listResponse.Body.Bytes(), &pending) != nil || len(pending.Batches) != 1 {
		t.Fatalf("recovery list = %d %s", listResponse.Code, listResponse.Body.String())
	}
	requestBody := `{"batchId":"` + pending.Batches[0].BatchID + `","action":"resolve-import"}`
	previewResponse := syncHTTPRequest(server, http.MethodPost, "/api/v1/sync/recovery/preview", requestBody)
	if previewResponse.Code != http.StatusCreated {
		t.Fatalf("recovery preview = %d %s", previewResponse.Code, previewResponse.Body.String())
	}
	var recoveryPlan struct {
		PlanID string `json:"planId"`
	}
	if err := json.Unmarshal(previewResponse.Body.Bytes(), &recoveryPlan); err != nil {
		t.Fatal(err)
	}
	if response := syncHTTPRequest(server, http.MethodPost, "/api/v1/sync/recovery/apply", `{"planId":"`+recoveryPlan.PlanID+`"}`); response.Code != http.StatusOK {
		t.Fatalf("recovery apply = %d %s", response.Code, response.Body.String())
	}
	listResponse = syncHTTPRequest(server, http.MethodGet, "/api/v1/sync/recovery", "")
	if listResponse.Code != http.StatusOK || !strings.Contains(listResponse.Body.String(), `"batches":[]`) {
		t.Fatalf("recovery batch remained pending: %d %s", listResponse.Code, listResponse.Body.String())
	}
}

func TestSyncAPIMigrationRejectsUntrustedOriginAndBrowserPaths(t *testing.T) {
	server, _, _, _ := newSyncTestServer(t, false)
	request := authenticatedRequest(t, server, http.MethodPost, "/api/v1/sync/migration/preview")
	request.Header.Set("Content-Type", "application/json")
	request.Body = io.NopCloser(strings.NewReader(`{"selectedSourceIds":[],"path":"/tmp/untrusted"}`))
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("untrusted migration preview = %d %s, want 403", response.Code, response.Body.String())
	}
	pathRequest := syncHTTPRequest(server, http.MethodPost, "/api/v1/sync/migration/preview", `{"selectedSourceIds":[],"path":"/tmp/untrusted"}`)
	if pathRequest.Code != http.StatusBadRequest {
		t.Fatalf("browser-supplied path was accepted: %d %s", pathRequest.Code, pathRequest.Body.String())
	}
}

func mustSourceGroupID(t *testing.T, server *Server) string {
	t.Helper()
	response := syncHTTPRequest(server, http.MethodGet, "/api/v1/sync/sources", "")
	var result struct {
		Groups []struct {
			ID string `json:"id"`
		} `json:"groups"`
	}
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &result) != nil || len(result.Groups) != 1 {
		t.Fatalf("read source group = %d %s", response.Code, response.Body.String())
	}
	return result.Groups[0].ID
}
