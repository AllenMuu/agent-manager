package eval_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AllenMuu/skill-manager/internal/eval"
)

func TestRunAndCompareDetectRuleRegression(t *testing.T) {
	root := t.TempDir()
	caseDir := filepath.Join(root, "cases", "001-demo")
	if err := os.MkdirAll(caseDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(caseDir, "case.yaml"), []byte(`version: v1
id: demo-001
category: demo
expectations:
  required_points: [boundary]
  forbidden_points: [unsafe shortcut]
verifier:
  type: rule
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(caseDir, "response.md"), []byte("The boundary is explicit."), 0o644); err != nil {
		t.Fatal(err)
	}
	suite, err := eval.LoadSuite(root)
	if err != nil {
		t.Fatal(err)
	}
	baseline, err := eval.Run(suite, eval.Options{Now: time.Date(2026, 9, 26, 10, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	if baseline.Summary.Passed != 1 || baseline.Summary.Score != 100 {
		t.Fatalf("baseline = %#v", baseline)
	}
	candidateDir := filepath.Join(root, "candidate")
	if err := os.MkdirAll(candidateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(candidateDir, "demo-001.md"), []byte("unsafe shortcut"), 0o644); err != nil {
		t.Fatal(err)
	}
	candidate, err := eval.Run(suite, eval.Options{CandidateDir: candidateDir, Now: time.Date(2026, 9, 26, 11, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	comparison := eval.Compare(baseline, candidate)
	if len(comparison.Regressions) != 1 || !strings.Contains(comparison.Regressions[0].Reason, "score") {
		t.Fatalf("comparison = %#v", comparison)
	}
}

func TestRunMarksMissingCandidatePartial(t *testing.T) {
	root := t.TempDir()
	caseDir := filepath.Join(root, "cases", "001-demo")
	if err := os.MkdirAll(caseDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(caseDir, "case.yaml"), []byte("version: v1\nid: demo-001\ncategory: demo\nverifier:\n  type: rule\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	suite, err := eval.LoadSuite(root)
	if err != nil {
		t.Fatal(err)
	}
	result, err := eval.Run(suite, eval.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Summary.Partial != 1 || result.Cases[0].Status != "partial" {
		t.Fatalf("result = %#v", result)
	}
}
