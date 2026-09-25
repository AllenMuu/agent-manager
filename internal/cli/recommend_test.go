package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/AllenMuu/skill-manager/internal/cli"
)

func recommendFixture(t *testing.T) (library, project, configPath string) {
	t.Helper()
	library = t.TempDir()
	writeSkill(t, library, "go-helper", "Go helper", "helps with go", "go\n")
	project = t.TempDir()
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, "go.mod"), []byte("module demo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	configPath = filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte("library: "+library+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return library, project, configPath
}

func TestRecommendJSONContractRecommendedScope(t *testing.T) {
	_, project, configPath := recommendFixture(t)
	root := cli.NewRootCommand()
	out := &bytes.Buffer{}
	root.SetOut(out)
	root.SetErr(out)
	root.SetArgs([]string{"--config", configPath, "recommend", "--project", project, "--json"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Project      string `json:"project"`
		ScanComplete bool   `json:"scanComplete"`
		Scopes       []struct {
			Path         string `json:"path"`
			Status       string `json:"status"`
			Technologies []struct {
				ID       string `json:"id"`
				Label    string `json:"label"`
				Category string `json:"category"`
			} `json:"technologies"`
			Evidence []struct {
				Path       string `json:"path"`
				Technology string `json:"technology"`
			} `json:"evidence"`
			Diagnostics []struct {
				Path    string `json:"path"`
				Message string `json:"message"`
			} `json:"diagnostics"`
			Recommendations []struct {
				Identifier       string `json:"identifier"`
				Description      string `json:"description"`
				MatchReason      string `json:"matchReason"`
				MatchingEvidence []struct {
					Technology string `json:"technology"`
					Field      string `json:"field"`
				} `json:"matchingEvidence"`
				Confidence string `json:"confidence"`
			} `json:"recommendations"`
			NextAction string `json:"nextAction"`
		} `json:"scopes"`
	}
	if err := json.Unmarshal(out.Bytes(), &doc); err != nil {
		t.Fatalf("invalid JSON %q: %v", out.String(), err)
	}
	if !filepath.IsAbs(doc.Project) || !doc.ScanComplete {
		t.Fatalf("project=%q scanComplete=%v", doc.Project, doc.ScanComplete)
	}
	if len(doc.Scopes) != 1 {
		t.Fatalf("scopes = %d", len(doc.Scopes))
	}
	scope := doc.Scopes[0]
	if scope.Path != "." || scope.Status != "recommended" || scope.NextAction != "review_recommendations" {
		t.Fatalf("scope = %#v", scope)
	}
	if len(scope.Technologies) != 1 || scope.Technologies[0].ID != "go" {
		t.Fatalf("technologies = %#v", scope.Technologies)
	}
	if len(scope.Evidence) != 1 || scope.Evidence[0].Path != "go.mod" {
		t.Fatalf("evidence = %#v", scope.Evidence)
	}
	if len(scope.Diagnostics) != 0 {
		t.Fatalf("diagnostics = %#v", scope.Diagnostics)
	}
	if len(scope.Recommendations) != 1 {
		t.Fatalf("recommendations = %#v", scope.Recommendations)
	}
	rec := scope.Recommendations[0]
	if rec.Identifier != "go-helper" || rec.Confidence != "high" || rec.MatchReason == "" {
		t.Fatalf("recommendation = %#v", rec)
	}
	if len(rec.MatchingEvidence) == 0 {
		t.Fatalf("matchingEvidence = %#v", rec.MatchingEvidence)
	}
}

func TestRecommendJSONInsufficientEvidenceIsEmptyState(t *testing.T) {
	library := t.TempDir()
	project := t.TempDir()
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte("library: "+library+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	root := cli.NewRootCommand()
	out := &bytes.Buffer{}
	root.SetOut(out)
	root.SetErr(out)
	root.SetArgs([]string{"--config", configPath, "recommend", "--project", project, "--json"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Scopes []struct {
			Status          string `json:"status"`
			Technologies    []any  `json:"technologies"`
			Evidence        []any  `json:"evidence"`
			Diagnostics     []any  `json:"diagnostics"`
			Recommendations []any  `json:"recommendations"`
			NextAction      string `json:"nextAction"`
		} `json:"scopes"`
	}
	if err := json.Unmarshal(out.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	scope := doc.Scopes[0]
	if scope.Status != "insufficient_evidence" || scope.NextAction != "use_catalog_search" {
		t.Fatalf("scope = %#v", scope)
	}
	if scope.Technologies == nil || scope.Evidence == nil || scope.Diagnostics == nil || scope.Recommendations == nil {
		t.Fatalf("empty arrays must be present: %#v", scope)
	}
}

func TestRecommendHumanOutputShowsEvidenceAndReason(t *testing.T) {
	_, project, configPath := recommendFixture(t)
	root := cli.NewRootCommand()
	out := &bytes.Buffer{}
	root.SetOut(out)
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"--config", configPath, "recommend", "--project", project})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"go-helper", "evidence: go.mod -> go", "reason: matched", "next action: review_recommendations"} {
		if !bytes.Contains(out.Bytes(), []byte(want)) {
			t.Fatalf("output missing %q: %s", want, out.String())
		}
	}
}

func TestRecommendMonorepoReportsIndependentScopes(t *testing.T) {
	library := t.TempDir()
	writeSkill(t, library, "go-helper", "Go helper", "helps with go", "go\n")
	writeSkill(t, library, "react-helper", "React helper", "helps with react", "react\n")
	project := t.TempDir()
	if err := os.MkdirAll(filepath.Join(project, "services", "api"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(project, "services", "web"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, "services", "api", "go.mod"), []byte("module api\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, "services", "web", "package.json"), []byte(`{"dependencies":{"react":"^18"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte("library: "+library+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	root := cli.NewRootCommand()
	out := &bytes.Buffer{}
	root.SetOut(out)
	root.SetErr(out)
	root.SetArgs([]string{"--config", configPath, "recommend", "--project", project, "--json"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Scopes []struct {
			Path         string `json:"path"`
			Status       string `json:"status"`
			Technologies []struct {
				ID string `json:"id"`
			} `json:"technologies"`
		} `json:"scopes"`
	}
	if err := json.Unmarshal(out.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Scopes) != 3 {
		t.Fatalf("scopes = %d, want 3", len(doc.Scopes))
	}
	if doc.Scopes[0].Path != "." || doc.Scopes[0].Status != "insufficient_evidence" {
		t.Fatalf("root = %#v", doc.Scopes[0])
	}
	if doc.Scopes[1].Path != "services/api" || len(doc.Scopes[1].Technologies) != 1 || doc.Scopes[1].Technologies[0].ID != "go" {
		t.Fatalf("api scope = %#v", doc.Scopes[1])
	}
	if doc.Scopes[2].Path != "services/web" || len(doc.Scopes[2].Technologies) != 2 || doc.Scopes[2].Technologies[0].ID != "nodejs" || doc.Scopes[2].Technologies[1].ID != "react" {
		t.Fatalf("web scope = %#v", doc.Scopes[2])
	}
}

func TestRecommendInvalidProjectRendersStructuredError(t *testing.T) {
	library := t.TempDir()
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte("library: "+library+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	root := cli.NewRootCommand()
	out := &bytes.Buffer{}
	root.SetOut(out)
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"--config", configPath, "recommend", "--project", filepath.Join(library, "missing"), "--json"})
	err := root.Execute()
	if err == nil {
		t.Fatal("invalid project accepted")
	}
	var doc struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
			Path    string `json:"path"`
		} `json:"error"`
	}
	if err := json.Unmarshal(out.Bytes(), &doc); err != nil {
		t.Fatalf("invalid error JSON %q: %v", out.String(), err)
	}
	if doc.Error.Code != "invalid_project" || doc.Error.Message == "" || doc.Error.Path == "" {
		t.Fatalf("error = %#v", doc.Error)
	}
}

func TestRecommendUnavailableCatalogRendersStructuredError(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing-library")
	project := t.TempDir()
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte("library: "+missing+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	root := cli.NewRootCommand()
	out := &bytes.Buffer{}
	root.SetOut(out)
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"--config", configPath, "recommend", "--project", project, "--json"})
	if err := root.Execute(); err == nil {
		t.Fatal("missing library accepted")
	}
	var doc struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(out.Bytes(), &doc); err != nil || doc.Error.Code != "catalog_unavailable" {
		t.Fatalf("error JSON = %q (%v)", out.String(), err)
	}
}
