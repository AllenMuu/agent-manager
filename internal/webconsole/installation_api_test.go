package webconsole

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/AllenMuu/skill-manager/internal/installation"
	"github.com/AllenMuu/skill-manager/internal/operation"
)

func TestConsoleBatchInstallationUsesSavedCopyOnlyMode(t *testing.T) {
	for _, initialMode := range []string{"auto", "copy-only"} {
		t.Run(initialMode, func(t *testing.T) {
			library, project := t.TempDir(), t.TempDir()
			for _, id := range []string{"first", "second"} {
				writeInstallationSkill(t, library, id)
			}
			server := newInstallationSyncTestServer(t, project, library, initialMode)
			if initialMode == "auto" {
				response := request(t, server, http.MethodPut, "/api/v1/sync/settings", `{"syncMode":"copy-only"}`)
				if response.Code != http.StatusOK {
					t.Fatalf("save copy-only status=%d body=%s", response.Code, response.Body)
				}
			}
			preview := previewInstallationBatch(t, server)
			for _, change := range preview.Plan.Changes {
				if change.Action != "copy Skill tree" || !strings.Contains(change.Detail, "(copy-only)") {
					t.Fatalf("copy-only preview change=%+v", change)
				}
			}
			response := request(t, server, http.MethodPost, "/api/install/apply", `{"previewId":"`+preview.ID+`"}`)
			if response.Code != http.StatusOK {
				t.Fatalf("apply status=%d body=%s", response.Code, response.Body)
			}
			for _, id := range []string{"first", "second"} {
				destination := filepath.Join(project, ".codex", "skills", id)
				info, err := os.Lstat(destination)
				if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
					t.Fatalf("copy-only destination %s is not a real directory: info=%v err=%v", destination, info, err)
				}
				got, err := os.ReadFile(filepath.Join(destination, "SKILL.md"))
				if err != nil || !strings.Contains(string(got), "name: "+id+"\n") {
					t.Fatalf("copied Skill %s content=%q err=%v", id, got, err)
				}
			}
			entry, found, err := operation.ProjectJournal(project).Latest()
			if err != nil || !found || len(entry.Materializations) != 2 {
				t.Fatalf("batch journal entry=%+v found=%v err=%v", entry, found, err)
			}
		})
	}
}

func TestConsoleBatchInstallationRejectsPreviewAfterSyncModeChange(t *testing.T) {
	library, project := t.TempDir(), t.TempDir()
	for _, id := range []string{"first", "second"} {
		writeInstallationSkill(t, library, id)
	}
	server := newInstallationSyncTestServer(t, project, library, "auto")
	preview := previewInstallationBatch(t, server)
	response := request(t, server, http.MethodPut, "/api/v1/sync/settings", `{"syncMode":"copy-only"}`)
	if response.Code != http.StatusOK {
		t.Fatalf("save copy-only status=%d body=%s", response.Code, response.Body)
	}
	response = request(t, server, http.MethodPost, "/api/install/apply", `{"previewId":"`+preview.ID+`"}`)
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), `"stale":true`) {
		t.Fatalf("mode-changed apply status=%d body=%s; want stale conflict", response.Code, response.Body)
	}
	if _, err := os.Lstat(filepath.Join(project, ".codex")); !os.IsNotExist(err) {
		t.Fatalf("stale mode preview changed destinations: %v", err)
	}
	if _, found, err := operation.ProjectJournal(project).Latest(); err != nil || found {
		t.Fatalf("stale mode preview changed journal: found=%v err=%v", found, err)
	}
}

func TestConsoleBatchPreviewAndSyncSettingsUpdateAreRaceFree(t *testing.T) {
	library, project := t.TempDir(), t.TempDir()
	for _, id := range []string{"first", "second"} {
		writeInstallationSkill(t, library, id)
	}
	server := newInstallationSyncTestServer(t, project, library, "auto")
	start := make(chan struct{})
	var workers sync.WaitGroup
	workers.Add(2)
	go func() {
		defer workers.Done()
		<-start
		for index := 0; index < 40; index++ {
			mode := "auto"
			if index%2 == 0 {
				mode = "copy-only"
			}
			response := syncHTTPRequest(server, http.MethodPut, "/api/v1/sync/settings", `{"syncMode":"`+mode+`"}`)
			if response.Code != http.StatusOK {
				t.Errorf("save mode status=%d body=%s", response.Code, response.Body)
				return
			}
		}
	}()
	go func() {
		defer workers.Done()
		<-start
		for index := 0; index < 40; index++ {
			response := syncHTTPRequest(server, http.MethodPost, "/api/install/preview", `{"skillIds":["first","second"],"targets":["codex"]}`)
			var payload struct {
				Preview installation.Preview `json:"preview"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil || response.Code != http.StatusOK || len(payload.Preview.Plan.Changes) != 2 {
				t.Errorf("batch preview status=%d body=%s err=%v", response.Code, response.Body, err)
				return
			}
			changes := payload.Preview.Plan.Changes
			if changes[0].Action != changes[1].Action {
				t.Errorf("preview mixed synchronization modes: %+v", changes)
				return
			}
		}
	}()
	close(start)
	workers.Wait()
}

func previewInstallationBatch(t *testing.T, server *Server) installation.Preview {
	t.Helper()
	response := request(t, server, http.MethodPost, "/api/install/preview", `{"skillIds":["first","second"],"targets":["codex"]}`)
	var payload struct {
		Preview installation.Preview `json:"preview"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil || response.Code != http.StatusOK || payload.Preview.ID == "" || len(payload.Preview.Plan.Changes) != 2 {
		t.Fatalf("batch preview status=%d body=%s err=%v", response.Code, response.Body, err)
	}
	return payload.Preview
}

func newInstallationSyncTestServer(t *testing.T, project, library, mode string) *Server {
	t.Helper()
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte("sync_mode: "+mode+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	server, err := New(Options{
		Origin: "http://127.0.0.1:8787", ProjectPath: project, LibraryPath: library,
		ConfigPath: configPath, SyncMode: mode, HomeDir: t.TempDir(), DataRoot: t.TempDir(),
		SkillInstallation: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	return server
}

func TestInstallationAPIsRequireConsoleSessionAndOrigin(t *testing.T) {
	library, project := t.TempDir(), t.TempDir()
	writeInstallationSkill(t, library, "selected")
	server := newInstallationTestServer(t, project, library)
	for _, endpoint := range []struct {
		method, path, body string
	}{
		{http.MethodGet, "/api/catalog", ""},
		{http.MethodPost, "/api/install/preview", `{"skillIds":["selected"],"targets":["codex"]}`},
	} {
		t.Run(endpoint.path, func(t *testing.T) {
			for _, tc := range []struct {
				name   string
				mutate func(*http.Request)
				want   int
			}{
				{"missing bearer token", func(r *http.Request) { r.Header.Del("Authorization") }, http.StatusUnauthorized},
				{"wrong session", func(r *http.Request) { r.Header.Set("Authorization", "Bearer inactive") }, http.StatusUnauthorized},
				{"untrusted origin", func(r *http.Request) { r.Header.Set("Origin", "http://attacker.example") }, http.StatusForbidden},
				{"non-local host", func(r *http.Request) { r.Host = "192.0.2.4:8787" }, http.StatusForbidden},
				{"cross-site fetch", func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "cross-site") }, http.StatusForbidden},
			} {
				t.Run(tc.name, func(t *testing.T) {
					req := authenticatedRequest(t, server, endpoint.method, endpoint.path)
					req.Header.Set("Origin", "http://127.0.0.1:8787")
					req.Body = io.NopCloser(strings.NewReader(endpoint.body))
					tc.mutate(req)
					response := httptest.NewRecorder()
					server.ServeHTTP(response, req)
					if response.Code != tc.want {
						t.Fatalf("status=%d body=%s; want %d", response.Code, response.Body, tc.want)
					}
					if got := response.Header().Get("Access-Control-Allow-Origin"); got != "" {
						t.Fatalf("denied request received cross-origin grant %q", got)
					}
				})
			}
			response := request(t, server, endpoint.method, endpoint.path, endpoint.body)
			if response.Code != http.StatusOK {
				t.Fatalf("active session status=%d body=%s; want 200", response.Code, response.Body)
			}
		})
	}
	req := authenticatedRequest(t, server, http.MethodPost, "/api/install/preview")
	req.Body = io.NopCloser(strings.NewReader(`{"skillIds":["selected"],"targets":["codex"]}`))
	response := httptest.NewRecorder()
	server.ServeHTTP(response, req)
	if response.Code != http.StatusForbidden {
		t.Fatalf("mutation without origin status=%d body=%s; want 403", response.Code, response.Body)
	}
	if _, err := os.Lstat(filepath.Join(project, ".codex")); !os.IsNotExist(err) {
		t.Fatalf("authorization checks or preview changed the project: %v", err)
	}
}

func TestConsoleInstallationIsSessionScopedAndInstallsOnlyTheExplicitSelection(t *testing.T) {
	library := t.TempDir()
	writeInstallationSkill(t, library, "selected")
	writeInstallationSkill(t, library, "not-selected")
	if err := os.WriteFile(filepath.Join(library, "selected", ".skill-manager.yaml"), []byte("tags:\n  - go\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(library, "ineligible"), 0o755); err != nil {
		t.Fatal(err)
	}
	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, "go.mod"), []byte("module webui-test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	server := newInstallationTestServer(t, project, library)
	for _, body := range []string{`{"skillIds":[],"targets":["codex"]}`, `{"skillIds":["selected"],"targets":[]}`, `{"skillIds":["selected"]}`} {
		response := request(t, server, http.MethodPost, "/api/install/preview", body)
		if response.Code != http.StatusUnprocessableEntity {
			t.Fatalf("preview without explicit Skills or targets status=%d body=%s", response.Code, response.Body)
		}
	}

	catalogResponse := request(t, server, http.MethodGet, "/api/catalog", "")
	if catalogResponse.Code != http.StatusOK || !strings.Contains(catalogResponse.Body.String(), `"selected"`) || !strings.Contains(catalogResponse.Body.String(), `"not-selected"`) || strings.Contains(catalogResponse.Body.String(), `"ineligible"`) || !strings.Contains(catalogResponse.Body.String(), `"claude-code"`) {
		t.Fatalf("catalog response status=%d body=%s", catalogResponse.Code, catalogResponse.Body)
	}
	recommendations := request(t, server, http.MethodGet, "/api/recommend", "")
	if recommendations.Code != http.StatusOK || !strings.Contains(recommendations.Body.String(), `"identifier":"selected"`) {
		t.Fatalf("recommendation response status=%d body=%s", recommendations.Code, recommendations.Body)
	}
	previewResponse := request(t, server, http.MethodPost, "/api/install/preview", `{"skillIds":["selected"],"targets":["codex"]}`)
	if previewResponse.Code != http.StatusOK {
		t.Fatalf("preview status=%d body=%s", previewResponse.Code, previewResponse.Body)
	}
	var payload struct {
		Preview installation.Preview `json:"preview"`
	}
	if err := json.Unmarshal(previewResponse.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Preview.SkillIDs) != 1 || payload.Preview.SkillIDs[0] != "selected" {
		t.Fatalf("preview expanded the selection: %#v", payload.Preview.SkillIDs)
	}
	if len(payload.Preview.Plan.Warnings) == 0 {
		t.Fatal("preview omitted the undeclared compatibility warning")
	}
	destination := filepath.Join(project, ".codex", "skills", "selected")
	if _, err := os.Lstat(destination); !os.IsNotExist(err) {
		t.Fatalf("preview changed the project: %v", err)
	}

	otherSession := newInstallationTestServer(t, project, library)
	otherApply := request(t, otherSession, http.MethodPost, "/api/install/apply", `{"previewId":"`+payload.Preview.ID+`"}`)
	if otherApply.Code != http.StatusUnprocessableEntity {
		t.Fatalf("another session applied a preview: status=%d body=%s", otherApply.Code, otherApply.Body)
	}
	if _, err := os.Lstat(destination); !os.IsNotExist(err) {
		t.Fatalf("rejected session apply changed the project: %v", err)
	}

	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destination, []byte("keep this file"), 0o600); err != nil {
		t.Fatal(err)
	}
	apply := request(t, server, http.MethodPost, "/api/install/apply", `{"previewId":"`+payload.Preview.ID+`"}`)
	if apply.Code != http.StatusConflict || !strings.Contains(apply.Body.String(), `"stale":true`) {
		t.Fatalf("stale apply status=%d body=%s", apply.Code, apply.Body)
	}
	contents, err := os.ReadFile(destination)
	if err != nil || string(contents) != "keep this file" {
		t.Fatalf("stale apply altered conflict: %q %v", contents, err)
	}

	previewResponse = request(t, server, http.MethodPost, "/api/install/preview", `{"skillIds":["selected"],"targets":["codex"],"replaceConflicts":true}`)
	if previewResponse.Code != http.StatusOK {
		t.Fatalf("replacement preview status=%d body=%s", previewResponse.Code, previewResponse.Body)
	}
	if err := json.Unmarshal(previewResponse.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	apply = request(t, server, http.MethodPost, "/api/install/apply", `{"previewId":"`+payload.Preview.ID+`"}`)
	if apply.Code != http.StatusOK {
		t.Fatalf("apply status=%d body=%s", apply.Code, apply.Body)
	}
	if target, err := os.Readlink(destination); err != nil || target != filepath.Join(library, "selected") {
		t.Fatalf("installed link=%q err=%v", target, err)
	}
	if _, err := os.Lstat(filepath.Join(project, ".codex", "skills", "not-selected")); !os.IsNotExist(err) {
		t.Fatalf("unselected Skill was activated: %v", err)
	}
}

func newInstallationTestServer(t *testing.T, project, library string) *Server {
	t.Helper()
	server, err := New(Options{
		Origin:            "http://127.0.0.1:8787",
		ProjectPath:       project,
		LibraryPath:       library,
		HomeDir:           t.TempDir(),
		DataRoot:          t.TempDir(),
		SkillInstallation: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	return server
}

func writeInstallationSkill(t *testing.T, library, id string) {
	t.Helper()
	dir := filepath.Join(library, id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	contents := "---\nname: " + id + "\ndescription: " + id + " skill\n---\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}
