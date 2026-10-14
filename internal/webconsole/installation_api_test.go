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

	"github.com/AllenMuu/skill-manager/internal/installation"
)

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
