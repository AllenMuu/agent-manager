package webui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/AllenMuu/skill-manager/internal/installation"
)

func TestWebUIIsSessionScopedAndInstallsOnlyTheExplicitSelection(t *testing.T) {
	library := t.TempDir()
	writeSkill(t, library, "selected")
	writeSkill(t, library, "not-selected")
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
	url, server, listener, err := NewLocalServer(project, library)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if !strings.HasPrefix(url, "http://127.0.0.1:") {
		t.Fatalf("WebUI URL is not loopback: %s", url)
	}
	authority := strings.TrimPrefix(url, "http://")

	page := serveRequest(server.Handler, authority, "127.0.0.1:12000", http.MethodGet, "/", "", "", "")
	if page.Code != http.StatusOK {
		t.Fatalf("index status=%d body=%s", page.Code, page.Body)
	}
	token := regexp.MustCompile(`name="agent-manager-session" content="([a-f0-9]+)"`).FindStringSubmatch(page.Body.String())
	if len(token) != 2 {
		t.Fatalf("session token missing from page: %s", page.Body)
	}
	asset := regexp.MustCompile(`src="\./(assets/[^\"]+\.js)"`).FindStringSubmatch(page.Body.String())
	if len(asset) != 2 {
		t.Fatalf("embedded JavaScript asset missing from page: %s", page.Body)
	}
	assetResponse := serveRequest(server.Handler, authority, "127.0.0.1:12000", http.MethodGet, "/"+asset[1], "", "", "")
	if assetResponse.Code != http.StatusOK || !strings.Contains(assetResponse.Header().Get("Content-Type"), "javascript") {
		t.Fatalf("embedded asset status=%d content-type=%q", assetResponse.Code, assetResponse.Header().Get("Content-Type"))
	}

	denied := serveRequest(server.Handler, authority, "192.0.2.4:12000", http.MethodGet, "/api/catalog", "", "", "")
	if denied.Code != http.StatusForbidden {
		t.Fatalf("non-loopback request status=%d; want 403", denied.Code)
	}
	denied = serveRequest(server.Handler, authority, "127.0.0.1:12000", http.MethodGet, "/api/catalog", "", "", "")
	if denied.Code != http.StatusForbidden {
		t.Fatalf("request without session status=%d; want 403", denied.Code)
	}
	denied = serveRequest(server.Handler, authority, "127.0.0.1:12000", http.MethodPost, "/api/install/preview", `{"skillIds":["selected"],"targets":["codex"]}`, token[1], "http://attacker.example")
	if denied.Code != http.StatusForbidden {
		t.Fatalf("cross-origin mutation status=%d; want 403", denied.Code)
	}

	catalogResponse := serveRequest(server.Handler, authority, "127.0.0.1:12000", http.MethodGet, "/api/catalog", "", token[1], "")
	if catalogResponse.Code != http.StatusOK || !strings.Contains(catalogResponse.Body.String(), `"selected"`) || !strings.Contains(catalogResponse.Body.String(), `"not-selected"`) || strings.Contains(catalogResponse.Body.String(), `"ineligible"`) || !strings.Contains(catalogResponse.Body.String(), `"claude-code"`) {
		t.Fatalf("catalog response status=%d body=%s", catalogResponse.Code, catalogResponse.Body)
	}
	recommendations := serveRequest(server.Handler, authority, "127.0.0.1:12000", http.MethodGet, "/api/recommend", "", token[1], "")
	if recommendations.Code != http.StatusOK || !strings.Contains(recommendations.Body.String(), `"identifier":"selected"`) {
		t.Fatalf("recommendation response status=%d body=%s", recommendations.Code, recommendations.Body)
	}
	previewResponse := serveRequest(server.Handler, authority, "127.0.0.1:12000", http.MethodPost, "/api/install/preview", `{"skillIds":["selected"],"targets":["codex"]}`, token[1], url)
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

	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destination, []byte("keep this file"), 0o600); err != nil {
		t.Fatal(err)
	}
	apply := serveRequest(server.Handler, authority, "127.0.0.1:12000", http.MethodPost, "/api/install/apply", `{"previewId":"`+payload.Preview.ID+`"}`, token[1], url)
	if apply.Code != http.StatusConflict || !strings.Contains(apply.Body.String(), `"stale":true`) {
		t.Fatalf("stale apply status=%d body=%s", apply.Code, apply.Body)
	}
	contents, err := os.ReadFile(destination)
	if err != nil || string(contents) != "keep this file" {
		t.Fatalf("stale apply altered conflict: %q %v", contents, err)
	}

	previewResponse = serveRequest(server.Handler, authority, "127.0.0.1:12000", http.MethodPost, "/api/install/preview", `{"skillIds":["selected"],"targets":["codex"],"replaceConflicts":true}`, token[1], url)
	if previewResponse.Code != http.StatusOK {
		t.Fatalf("replacement preview status=%d body=%s", previewResponse.Code, previewResponse.Body)
	}
	if err := json.Unmarshal(previewResponse.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	apply = serveRequest(server.Handler, authority, "127.0.0.1:12000", http.MethodPost, "/api/install/apply", `{"previewId":"`+payload.Preview.ID+`"}`, token[1], url)
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

func serveRequest(handler http.Handler, authority, remote, method, path, body, session, origin string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, "http://"+authority+path, strings.NewReader(body))
	request.Host = authority
	request.RemoteAddr = remote
	if session != "" {
		request.Header.Set("X-Agent-Manager-Session", session)
	}
	if origin != "" {
		request.Header.Set("Origin", origin)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func writeSkill(t *testing.T, library, id string) {
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
