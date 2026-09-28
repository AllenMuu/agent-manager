package eval_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AllenMuu/skill-manager/internal/artifact"
	"github.com/AllenMuu/skill-manager/internal/eval"
)

func TestRunAndCompareDetectRuleRegression(t *testing.T) {
	root := t.TempDir()
	caseDir := filepath.Join(root, "cases", "001-demo")
	if err := os.MkdirAll(caseDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(caseDir, "case.yaml"), []byte(`version: v2
id: demo-001
category: demo
input:
  intent: intent.yaml
expectations:
  required_points: [boundary]
  evidence_points: [commit test]
  forbidden_points: [unsafe shortcut]
verifier:
  type: rule
`), 0o644); err != nil {
		t.Fatal(err)
	}
	writeIntent(t, caseDir)
	baselineDir := filepath.Join(root, "baseline")
	if err := os.MkdirAll(baselineDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(baselineDir, "demo-001.md"), []byte("The boundary is explicit; run a commit test."), 0o644); err != nil {
		t.Fatal(err)
	}
	suite, err := eval.LoadSuite(root)
	if err != nil {
		t.Fatal(err)
	}
	baseline, err := eval.Run(suite, eval.Options{CandidateDir: baselineDir, Now: time.Date(2026, 9, 26, 10, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	if baseline.Summary.Passed != 1 || baseline.Summary.Score != 100 {
		t.Fatalf("baseline = %#v", baseline)
	}
	if baseline.DurationMS < 0 || baseline.DurationMS > 60_000 {
		t.Fatalf("duration must measure the run, not elapsed time since Options.Now: %d ms", baseline.DurationMS)
	}
	outside := filepath.Join(root, "outside.yaml")
	if err := os.WriteFile(outside, []byte("do not replace"), 0o644); err != nil {
		t.Fatal(err)
	}
	resultLink := filepath.Join(root, "result-link.yaml")
	if err := os.Symlink(outside, resultLink); err != nil {
		t.Fatal(err)
	}
	if err := eval.WriteResult(resultLink, baseline); err == nil {
		t.Fatal("symlink result destination was accepted")
	}
	unchanged, err := os.ReadFile(outside)
	if err != nil || string(unchanged) != "do not replace" {
		t.Fatalf("symlink target changed: %q, %v", unchanged, err)
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
	if err := os.WriteFile(filepath.Join(caseDir, "case.yaml"), []byte("version: v2\nid: demo-001\ncategory: demo\ninput:\n  intent: intent.yaml\nexpectations:\n  required_points: [boundary]\n  evidence_points: [commit test]\nverifier:\n  type: rule\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeIntent(t, caseDir)
	if err := os.WriteFile(filepath.Join(caseDir, "response.md"), []byte("boundary and commit test"), 0o644); err != nil {
		t.Fatal(err)
	}
	suite, err := eval.LoadSuite(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := eval.Run(suite, eval.Options{}); err == nil {
		t.Fatal("eval run without candidate directory succeeded")
	}
	responses := filepath.Join(root, "responses")
	if err := os.MkdirAll(responses, 0o755); err != nil {
		t.Fatal(err)
	}
	result, err := eval.Run(suite, eval.Options{CandidateDir: responses})
	if err != nil {
		t.Fatal(err)
	}
	if result.Summary.Partial != 1 || result.Cases[0].Status != "partial" {
		t.Fatalf("result = %#v", result)
	}
}

func TestCompareDoesNotRewardMissingCandidate(t *testing.T) {
	baseline := eval.Result{Cases: []eval.CaseResult{{CaseID: "a", Status: "fail", Score: 0, Evidence: []string{"required point missing"}}}}
	missing := eval.Result{Cases: []eval.CaseResult{{CaseID: "a", Status: "partial", Score: 0, Evidence: []string{"candidate response was not provided"}}}}
	comparison := eval.Compare(baseline, missing)
	if len(comparison.Improvements) != 0 || len(comparison.Regressions) != 1 {
		t.Fatalf("missing candidate comparison = %#v", comparison)
	}
	pass := eval.Result{Cases: []eval.CaseResult{{CaseID: "a", Status: "pass", Score: 100}}}
	if got := eval.Compare(pass, missing); len(got.Regressions) != 1 {
		t.Fatalf("pass to missing = %#v", got)
	}
	if got := eval.Compare(baseline, pass); len(got.Improvements) != 1 {
		t.Fatalf("fail to pass = %#v", got)
	}
	partial := eval.Result{Cases: []eval.CaseResult{{CaseID: "a", Status: "partial", Score: 50}}}
	if got := eval.Compare(pass, partial); len(got.Regressions) != 1 {
		t.Fatalf("pass to partial = %#v", got)
	}
	if got := eval.Compare(baseline, partial); len(got.Improvements) != 1 {
		t.Fatalf("fail to partial = %#v", got)
	}
}

func TestLegacyV1CaseWithoutIntentFileRemainsRunnable(t *testing.T) {
	root := t.TempDir()
	caseDir := filepath.Join(root, "cases", "legacy")
	if err := os.MkdirAll(caseDir, 0o755); err != nil {
		t.Fatal(err)
	}
	legacy := "version: v1\nid: legacy-001\ncategory: demo\ninput:\n  intent: intent.yaml\nexpectations:\n  required_points: [boundary]\nverifier:\n  type: rule\n"
	if err := os.WriteFile(filepath.Join(caseDir, "case.yaml"), []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	suite, err := eval.LoadSuite(root)
	if err != nil {
		t.Fatalf("existing v1 case rejected: %v", err)
	}
	responses := t.TempDir()
	if err := os.WriteFile(filepath.Join(responses, "legacy-001.md"), []byte("boundary"), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := eval.Run(suite, eval.Options{CandidateDir: responses})
	if err != nil || result.Summary.Passed != 1 {
		t.Fatalf("legacy v1 result = %#v, err = %v", result, err)
	}
}

func TestLoadResultPreservesLegacyAgentAttribution(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy-result.yaml")
	legacy := "version: v1\nrun_id: run-old\nsuite: demo\nagent: codex\nstarted_at: 2026-09-26T10:00:00Z\nduration_ms: 1\ncases:\n  - case_id: demo-001\n    status: pass\n    score: 100\n    evidence: [matched]\nsummary:\n  total: 1\n  passed: 1\n  score: 100\n"
	if err := os.WriteFile(path, []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	loaded, err := eval.LoadResult(path)
	if err != nil || loaded.AgentLabel != "codex" {
		t.Fatalf("legacy result attribution = %q, err = %v", loaded.AgentLabel, err)
	}
}

func TestWriteProjectResultRejectsSymlinkedParentDirectories(t *testing.T) {
	project := t.TempDir()
	outside := t.TempDir()
	result := eval.Result{Version: eval.Version, RunID: "run-1", Suite: "demo", StartedAt: time.Now()}
	agentManager := filepath.Join(project, ".agent-manager")
	if err := os.Symlink(outside, agentManager); err != nil {
		t.Fatal(err)
	}
	if _, err := eval.WriteProjectResult(project, result); err == nil {
		t.Fatal("symlinked .agent-manager directory was accepted")
	}
	if err := os.Remove(agentManager); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(agentManager, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(agentManager, "evals")); err != nil {
		t.Fatal(err)
	}
	if _, err := eval.WriteProjectResult(project, result); err == nil {
		t.Fatal("symlinked evals directory was accepted")
	}
	entries, err := os.ReadDir(outside)
	if err != nil || len(entries) != 0 {
		t.Fatalf("external directory was written: %v, err = %v", entries, err)
	}
}

func writeIntent(t *testing.T, caseDir string) {
	t.Helper()
	doc := artifact.New(artifact.Intent, "demo-001", ".", time.Date(2026, 9, 26, 0, 0, 0, 0, time.UTC))
	doc.Set("summary", "Demo evaluation input")
	data, err := doc.YAML()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(caseDir, "intent.yaml"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}
