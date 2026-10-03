package stack_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/AllenMuu/skill-manager/internal/stack"
)

func TestVocabularyCoversRequiredTechnologies(t *testing.T) {
	required := []string{
		"go", "nodejs", "typescript", "maven", "gradle", "python", "rust", "ruby",
		"docker", "compose", "claude-code", "codex", "agents",
		"express", "nestjs", "react", "nextjs", "spring-boot", "django", "fastapi", "rails",
		"postgresql", "mysql", "redis", "mongodb",
	}
	for _, id := range required {
		if _, ok := stack.Lookup(id); !ok {
			t.Errorf("vocabulary missing %q", id)
		}
	}
}

func TestScanDetectsNodeStackWithFrameworks(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "package.json"), `{
		"dependencies": {"express": "^4.0.0", "@nestjs/core": "^10.0.0"},
		"devDependencies": {"typescript": "^5.0.0"}
	}`)
	write(t, filepath.Join(root, "tsconfig.json"), "{}")
	result, err := stack.Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Scopes) != 1 {
		t.Fatalf("scopes = %d, want 1", len(result.Scopes))
	}
	scope := result.Scopes[0]
	assertTechnologies(t, scope, "express", "nestjs", "nodejs", "typescript")
}

func TestScanDetectsTypeScriptFromTsconfigAlone(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "tsconfig.json"), "{}")
	result, err := stack.Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	assertTechnologies(t, result.Scopes[0], "typescript")
}

func TestScanDetectsDatabasesFromComposeAndDockerfile(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "Dockerfile"), "FROM postgres:16\nCOPY . .\n")
	write(t, filepath.Join(root, "compose.yml"), "services:\n  cache:\n    image: redis:7\n")
	result, err := stack.Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	// Docker and Compose markers contribute evidence without creating scopes,
	// so the root itself carries it.
	scope := result.Scopes[0]
	assertTechnologies(t, scope, "compose", "docker", "postgresql", "redis")
}

func TestScanAttributesNestedMarkersToContainingScope(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "go.mod"), "module example.com/root\n")
	write(t, filepath.Join(root, "deploy", "compose.yaml"), "services:\n  db:\n    image: postgres:16\n")
	result, err := stack.Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	if !result.ScanComplete || len(result.Scopes) != 1 {
		t.Fatalf("result=%#v", result)
	}
	assertTechnologies(t, result.Scopes[0], "compose", "docker", "go", "postgresql")
	for _, evidence := range result.Scopes[0].Evidence {
		if evidence.Technology != "go" && evidence.Path != "deploy/compose.yaml" {
			t.Fatalf("nested marker has wrong evidence path: %#v", evidence)
		}
	}
}

func TestScanKeepsNestedMarkersInsideIndependentChildScope(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "go.mod"), "module example.com/root\n")
	write(t, filepath.Join(root, "web", "package.json"), `{"dependencies":{"react":"^18"}}`)
	write(t, filepath.Join(root, "web", "deploy", "compose.yaml"), "services:\n  cache:\n    image: redis:7\n")
	result, err := stack.Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Scopes) != 2 {
		t.Fatalf("scopes=%#v", result.Scopes)
	}
	assertTechnologies(t, result.Scopes[0], "go")
	child := result.Scopes[1]
	if child.Path != "web" {
		t.Fatalf("child scope=%q", child.Path)
	}
	assertTechnologies(t, child, "compose", "docker", "nodejs", "react", "redis")
	for _, evidence := range child.Evidence {
		if evidence.Technology == "redis" && evidence.Path != "deploy/compose.yaml" {
			t.Fatalf("child evidence is not scope-relative: %#v", evidence)
		}
	}
}

func TestScanDetectsJvmPythonAndRubyStacks(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "pom.xml"), "<project><dependency>org.springframework.boot:spring-boot-starter</dependency></project>")
	write(t, filepath.Join(root, "build.gradle"), "implementation 'org.springframework.boot:spring-boot-starter-web'")
	write(t, filepath.Join(root, "pyproject.toml"), "[project]\ndependencies = [\"django>=5\", \"fastapi>=0.110\"]\n")
	write(t, filepath.Join(root, "Gemfile"), "gem 'rails', '~> 7.0'\n")
	result, err := stack.Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	assertTechnologies(t, result.Scopes[0],
		"django", "fastapi", "gradle", "maven", "python", "rails", "ruby", "spring-boot")
}

func TestScanDetectsAgentDirectories(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	result, err := stack.Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	assertTechnologies(t, result.Scopes[0], "claude-code")
}

func TestScanIsolatesMonorepoScopes(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "services", "api", "go.mod"), "module example.com/api\n\ngo 1.22\n")
	write(t, filepath.Join(root, "services", "web", "package.json"), `{"dependencies": {"react": "^18"}}`)
	result, err := stack.Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Scopes) != 3 {
		t.Fatalf("scopes = %d, want 3 (root + api + web)", len(result.Scopes))
	}
	if result.Scopes[0].Path != "." || len(result.Scopes[0].Technologies) != 0 {
		t.Fatalf("root scope = %#v, want empty insufficient evidence", result.Scopes[0])
	}
	api := result.Scopes[1]
	if api.Path != "services/api" {
		t.Fatalf("scope order: got %q", api.Path)
	}
	assertTechnologies(t, api, "go")
	web := result.Scopes[2]
	if web.Path != "services/web" {
		t.Fatalf("scope order: got %q", web.Path)
	}
	assertTechnologies(t, web, "nodejs", "react")
}

func TestScanSkipsExcludedDirectories(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "node_modules", "dep", "package.json"), `{"dependencies": {}}`)
	write(t, filepath.Join(root, ".git", "config"), "")
	result, err := stack.Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Scopes) != 1 || len(result.Scopes[0].Technologies) != 0 {
		t.Fatalf("scopes = %#v, want one empty root scope", result.Scopes)
	}
	if !result.ScanComplete {
		t.Fatal("excluding directories must not mark the scan incomplete")
	}
}

func TestScanSkipsDirectorySoftLinkWithDiagnostic(t *testing.T) {
	root := t.TempDir()
	linked := t.TempDir()
	write(t, filepath.Join(linked, "go.mod"), "module linked\n")
	if err := os.Symlink(linked, filepath.Join(root, "linked")); err != nil {
		t.Fatal(err)
	}
	result, err := stack.Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	if result.ScanComplete {
		t.Fatal("skipped link must set scanComplete=false")
	}
	if len(result.Scopes[0].Diagnostics) == 0 {
		t.Fatalf("missing diagnostic: %#v", result.Scopes[0].Diagnostics)
	}
}

func TestScanRejectsOversizedMarker(t *testing.T) {
	root := t.TempDir()
	oversized := make([]byte, (1<<20)+1)
	if err := os.WriteFile(filepath.Join(root, "go.mod"), oversized, 0o644); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(root, "package.json"), `{"dependencies": {}}`)
	result, err := stack.Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	scope := result.Scopes[0]
	if result.ScanComplete {
		t.Fatal("oversized marker must set scanComplete=false")
	}
	assertTechnologies(t, scope, "nodejs")
	found := false
	for _, d := range scope.Diagnostics {
		if d.Path == "go.mod" {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing oversized diagnostic: %#v", scope.Diagnostics)
	}
}

func TestScanRejectsInvalidRoot(t *testing.T) {
	if _, err := stack.Scan(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("missing root accepted")
	}
	file := filepath.Join(t.TempDir(), "file.txt")
	write(t, file, "x")
	if _, err := stack.Scan(file); err == nil {
		t.Fatal("non-directory root accepted")
	}
}

func assertTechnologies(t *testing.T, scope stack.Scope, want ...string) {
	t.Helper()
	if len(scope.Technologies) != len(want) {
		t.Fatalf("technologies = %#v, want %v", scope.Technologies, want)
	}
	for i, id := range want {
		if scope.Technologies[i] != id {
			t.Fatalf("technologies = %#v, want %v", scope.Technologies, want)
		}
	}
}

func write(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}
