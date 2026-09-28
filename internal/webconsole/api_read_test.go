package webconsole

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadAPIsExposeProjectWorkspaceAndCatalogWithoutMutation(t *testing.T) {
	root := t.TempDir()
	projectPath := filepath.Join(root, "project")
	libraryPath := filepath.Join(root, "library")
	homePath := filepath.Join(root, "home")
	dataRoot := filepath.Join(root, "data")
	for _, dir := range []string{projectPath, libraryPath, homePath, dataRoot} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(projectPath, "go.mod"), []byte("module example\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	skillPath := filepath.Join(libraryPath, "go-helper")
	if err := os.Mkdir(skillPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillPath, "SKILL.md"), []byte("---\nname: Go Helper\ndescription: Go project helper\ntags: [go]\n---\nUse Go tooling.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	definitionDir := filepath.Join(dataRoot, "subagents")
	if err := os.MkdirAll(definitionDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(definitionDir, "reviewer.yaml"), []byte("version: v1\nid: reviewer\nname: Reviewer\nrole: Review code\ninstructions: Review changes carefully.\nskills: [go-helper]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	server, err := New(Options{
		Origin: "http://127.0.0.1:8787", LibraryPath: libraryPath, HomeDir: homePath,
		DataRoot: dataRoot, ProjectPath: projectPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	projectID := server.project.ID

	for _, route := range []string{
		"/api/v1/system",
		"/api/v1/projects/" + projectID,
		"/api/v1/projects/" + projectID + "/agents",
		"/api/v1/projects/" + projectID + "/recommendations",
		"/api/v1/projects/" + projectID + "/resources",
		"/api/v1/projects/" + projectID + "/diagnostics",
		"/api/v1/projects/" + projectID + "/operations/latest",
		"/api/v1/skills?q=Go",
		"/api/v1/skills/go-helper",
		"/api/v1/subagents",
		"/api/v1/subagents/reviewer",
	} {
		response := request(t, server, http.MethodGet, route, "")
		if response.Code != http.StatusOK {
			t.Errorf("GET %s status = %d, body = %s", route, response.Code, response.Body.String())
			continue
		}
		var body any
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
			t.Errorf("GET %s returned invalid JSON: %v", route, err)
		}
	}

	recommendations := request(t, server, http.MethodGet, "/api/v1/projects/"+projectID+"/recommendations", "")
	if !strings.Contains(recommendations.Body.String(), `"scanComplete":true`) || !strings.Contains(recommendations.Body.String(), `"identifier":"go-helper"`) {
		t.Errorf("recommendations did not expose stable scan/recommendation JSON: %s", recommendations.Body.String())
	}
	detail := request(t, server, http.MethodGet, "/api/v1/skills/go-helper", "")
	if !strings.Contains(detail.Body.String(), "Use Go tooling.") {
		t.Errorf("Skill detail did not expose readable SKILL.md body: %s", detail.Body.String())
	}
	list := request(t, server, http.MethodGet, "/api/v1/subagents", "")
	if !strings.Contains(list.Body.String(), `"id":"reviewer"`) || !strings.Contains(list.Body.String(), `"diagnostics":[]`) {
		t.Errorf("SubAgent list did not expose canonical definitions and validation diagnostics: %s", list.Body.String())
	}
	for _, collection := range []string{`"skills":["go-helper"]`, `"compatibility":{"agents":[]}`, `"requiredCapabilities":[]`} {
		if !strings.Contains(list.Body.String(), collection) {
			t.Errorf("SubAgent response omitted normalized collection %s: %s", collection, list.Body.String())
		}
	}
	subAgentDetail := request(t, server, http.MethodGet, "/api/v1/subagents/reviewer", "")
	if !strings.Contains(subAgentDetail.Body.String(), `"compatibility":{"agents":[]}`) || !strings.Contains(subAgentDetail.Body.String(), `"requiredCapabilities":[]`) {
		t.Errorf("SubAgent detail omitted normalized empty collections: %s", subAgentDetail.Body.String())
	}

	if _, err := os.Stat(filepath.Join(projectPath, ".skill-manager", "journal.json")); !os.IsNotExist(err) {
		t.Errorf("read APIs changed journal state, stat error = %v", err)
	}
	if _, err := os.Lstat(filepath.Join(projectPath, ".agents")); !os.IsNotExist(err) {
		t.Errorf("read APIs changed project state, lstat error = %v", err)
	}
}

func TestReadAPIsRejectUnknownProjectAndReturnOnlyEligibleSkills(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "project")
	library := filepath.Join(root, "library")
	for _, dir := range []string{project, library} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(library, "invalid-skill"), 0o755); err != nil {
		t.Fatal(err)
	}
	server, err := New(Options{Origin: "http://127.0.0.1:8787", ProjectPath: project, LibraryPath: library, HomeDir: root, DataRoot: filepath.Join(root, "data")})
	if err != nil {
		t.Fatal(err)
	}
	response := request(t, server, http.MethodGet, "/api/v1/projects/other/agents", "")
	if response.Code != http.StatusNotFound {
		t.Fatalf("unknown project status = %d, body = %s", response.Code, response.Body.String())
	}
	response = request(t, server, http.MethodGet, "/api/v1/skills/invalid-skill", "")
	if response.Code != http.StatusNotFound {
		t.Fatalf("ineligible Skill status = %d, body = %s", response.Code, response.Body.String())
	}
}
