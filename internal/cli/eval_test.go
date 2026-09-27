package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AllenMuu/skill-manager/internal/artifact"
	"github.com/AllenMuu/skill-manager/internal/cli"
)

func TestEvalRunRequiresResponsesAndUsesRelativeProjectOnce(t *testing.T) {
	project := t.TempDir()
	caseDir := filepath.Join(project, "evals", "demo", "cases", "one")
	if err := os.MkdirAll(caseDir, 0o755); err != nil {
		t.Fatal(err)
	}
	caseYAML := "version: v2\nid: demo-001\ncategory: demo\ninput:\n  intent: intent.yaml\nexpectations:\n  required_points: [boundary]\n  evidence_points: [commit test]\nverifier:\n  type: rule\n"
	if err := os.WriteFile(filepath.Join(caseDir, "case.yaml"), []byte(caseYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	intent := artifact.New(artifact.Intent, "demo-001", ".", time.Now())
	intent.Set("summary", "Exercise a boundary")
	data, err := intent.YAML()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(caseDir, "intent.yaml"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	relativeProject, err := filepath.Rel(cwd, project)
	if err != nil {
		t.Fatal(err)
	}
	root := cli.NewRootCommand()
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"eval", "run", "demo", "--project", relativeProject})
	if err := root.Execute(); err == nil || !strings.Contains(err.Error(), "--candidate-dir") {
		t.Fatalf("run without responses error = %v", err)
	}
	responses := filepath.Join(project, "responses")
	if err := os.MkdirAll(responses, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(responses, "demo-001.md"), []byte("boundary; commit test"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := executeAgentManager(t, "eval", "run", "demo", "--project", relativeProject, "--candidate-dir", "responses", "--agent-label", "codex", "--config-version", "baseline")
	if !strings.Contains(out, "status: pass") {
		t.Fatalf("run output = %q", out)
	}
	entries, err := os.ReadDir(filepath.Join(project, ".agent-manager", "evals"))
	if err != nil || len(entries) != 1 {
		t.Fatalf("result directory = %v, err = %v", entries, err)
	}
	baselineResult := filepath.Join(project, ".agent-manager", "evals", entries[0].Name())
	worseResponses := filepath.Join(project, "worse-responses")
	if err := os.MkdirAll(worseResponses, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(worseResponses, "demo-001.md"), []byte("boundary only"), 0o644); err != nil {
		t.Fatal(err)
	}
	executeAgentManager(t, "eval", "run", "demo", "--project", relativeProject, "--candidate-dir", "worse-responses", "--agent-label", "codex", "--config-version", "candidate")
	entries, err = os.ReadDir(filepath.Join(project, ".agent-manager", "evals"))
	if err != nil || len(entries) != 2 {
		t.Fatalf("result directory after candidate = %v, err = %v", entries, err)
	}
	var candidateResult string
	for _, entry := range entries {
		path := filepath.Join(project, ".agent-manager", "evals", entry.Name())
		if path != baselineResult {
			candidateResult = path
		}
	}
	comparison := executeAgentManager(t, "eval", "compare", baselineResult, candidateResult, "--project", relativeProject)
	if !strings.Contains(comparison, "regressions: 1") {
		t.Fatalf("comparison = %q", comparison)
	}
}
