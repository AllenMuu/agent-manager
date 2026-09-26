// Package eval provides a local, deterministic evaluation harness.
//
// The first runner intentionally evaluates supplied candidate artifacts rather
// than invoking an agent. Agent execution remains an adapter concern; keeping
// the runner deterministic makes prompt, Skill, context, and adapter changes
// regressible without network access or hidden runtime state.
package eval

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const Version = "v1"

type Expectations struct {
	RequiredPoints  []string `yaml:"required_points,omitempty" json:"requiredPoints,omitempty"`
	ForbiddenPoints []string `yaml:"forbidden_points,omitempty" json:"forbiddenPoints,omitempty"`
}

type Verifier struct {
	Type string `yaml:"type" json:"type"`
}

type Case struct {
	Version      string            `yaml:"version" json:"version"`
	ID           string            `yaml:"id" json:"id"`
	Category     string            `yaml:"category" json:"category"`
	Input        map[string]string `yaml:"input" json:"input"`
	Expectations Expectations      `yaml:"expectations" json:"expectations"`
	Verifier     Verifier          `yaml:"verifier" json:"verifier"`
	Path         string            `yaml:"-" json:"-"`
}

func (c Case) Validate() error {
	if c.Version != "" && c.Version != Version {
		return fmt.Errorf("case %q has unsupported version %q", c.ID, c.Version)
	}
	if c.ID == "" {
		return fmt.Errorf("eval case id is required")
	}
	if c.Category == "" {
		return fmt.Errorf("eval case %q category is required", c.ID)
	}
	if c.Verifier.Type == "" {
		return fmt.Errorf("eval case %q verifier.type is required", c.ID)
	}
	if c.Verifier.Type != "rule" {
		return fmt.Errorf("eval case %q uses unsupported verifier %q", c.ID, c.Verifier.Type)
	}
	return nil
}

type Suite struct {
	Version  string
	ID       string
	Category string
	Root     string
	Cases    []Case
}

func LoadSuite(root string) (Suite, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return Suite{}, fmt.Errorf("resolve eval suite: %w", err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return Suite{}, fmt.Errorf("inspect eval suite: %w", err)
	}
	if !info.IsDir() {
		return Suite{}, fmt.Errorf("eval suite %s is not a directory", abs)
	}
	suite := Suite{Version: Version, ID: filepath.Base(abs), Root: abs}
	entries, err := os.ReadDir(filepath.Join(abs, "cases"))
	if err != nil {
		return Suite{}, fmt.Errorf("read eval cases: %w", err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		casePath := filepath.Join(abs, "cases", entry.Name(), "case.yaml")
		data, err := os.ReadFile(casePath)
		if err != nil {
			return Suite{}, fmt.Errorf("read eval case %s: %w", casePath, err)
		}
		var item Case
		if err := yaml.Unmarshal(data, &item); err != nil {
			return Suite{}, fmt.Errorf("parse eval case %s: %w", casePath, err)
		}
		item.Path = filepath.Dir(casePath)
		if err := item.Validate(); err != nil {
			return Suite{}, err
		}
		if suite.Category == "" {
			suite.Category = item.Category
		}
		if item.Category != suite.Category && item.Category != "" {
			return Suite{}, fmt.Errorf("eval case %q category %q does not match suite %q", item.ID, item.Category, suite.Category)
		}
		suite.Cases = append(suite.Cases, item)
	}
	sort.Slice(suite.Cases, func(i, j int) bool { return suite.Cases[i].ID < suite.Cases[j].ID })
	if len(suite.Cases) == 0 {
		return Suite{}, fmt.Errorf("eval suite %s contains no cases", abs)
	}
	return suite, nil
}

type Options struct {
	Agent         string
	ConfigVersion string
	CandidateDir  string
	Now           time.Time
}

type CaseResult struct {
	CaseID   string   `yaml:"case_id" json:"caseId"`
	Status   string   `yaml:"status" json:"status"`
	Score    int      `yaml:"score" json:"score"`
	Evidence []string `yaml:"evidence" json:"evidence"`
}

type Summary struct {
	Total   int `yaml:"total" json:"total"`
	Passed  int `yaml:"passed" json:"passed"`
	Failed  int `yaml:"failed" json:"failed"`
	Partial int `yaml:"partial" json:"partial"`
	Score   int `yaml:"score" json:"score"`
}

type Result struct {
	Version       string       `yaml:"version" json:"version"`
	RunID         string       `yaml:"run_id" json:"runId"`
	Suite         string       `yaml:"suite" json:"suite"`
	Agent         string       `yaml:"agent,omitempty" json:"agent,omitempty"`
	ConfigVersion string       `yaml:"config_version,omitempty" json:"configVersion,omitempty"`
	StartedAt     time.Time    `yaml:"started_at" json:"startedAt"`
	DurationMS    int64        `yaml:"duration_ms" json:"durationMs"`
	Cases         []CaseResult `yaml:"cases" json:"cases"`
	Summary       Summary      `yaml:"summary" json:"summary"`
}

func Run(suite Suite, options Options) (Result, error) {
	if len(suite.Cases) == 0 {
		return Result{}, fmt.Errorf("eval suite %q has no cases", suite.ID)
	}
	if options.Now.IsZero() {
		options.Now = time.Now().UTC()
	}
	started := options.Now.UTC()
	result := Result{
		Version: Version, RunID: "run-" + started.Format("20060102T150405.000000000Z"),
		Suite: suite.ID, Agent: options.Agent, ConfigVersion: options.ConfigVersion, StartedAt: started,
		Cases: make([]CaseResult, 0, len(suite.Cases)),
	}
	for _, item := range suite.Cases {
		content, source, err := candidate(item, options.CandidateDir)
		if err != nil {
			return Result{}, err
		}
		caseResult := evaluate(item, content, source)
		result.Cases = append(result.Cases, caseResult)
		result.Summary.Total++
		result.Summary.Score += caseResult.Score
		switch caseResult.Status {
		case "pass":
			result.Summary.Passed++
		case "fail":
			result.Summary.Failed++
		default:
			result.Summary.Partial++
		}
	}
	if result.Summary.Total > 0 {
		result.Summary.Score /= result.Summary.Total
	}
	result.DurationMS = time.Since(started).Milliseconds()
	if result.DurationMS < 0 {
		result.DurationMS = 0
	}
	return result, nil
}

func candidate(item Case, candidateDir string) (string, string, error) {
	paths := []string{}
	if candidateDir != "" {
		paths = append(paths,
			filepath.Join(candidateDir, item.ID+".md"),
			filepath.Join(candidateDir, item.ID+".txt"),
			filepath.Join(candidateDir, item.ID, "response.md"),
		)
	}
	if item.Path != "" {
		paths = append(paths, filepath.Join(item.Path, "response.md"), filepath.Join(item.Path, "response.txt"))
	}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err == nil {
			return string(data), path, nil
		}
		if !os.IsNotExist(err) {
			return "", "", fmt.Errorf("read eval candidate %s: %w", path, err)
		}
	}
	return "", "", nil
}

func evaluate(item Case, content, source string) CaseResult {
	result := CaseResult{CaseID: item.ID, Evidence: []string{}}
	if source == "" {
		result.Status = "partial"
		result.Evidence = append(result.Evidence, "candidate response was not provided")
		return result
	}
	lower := strings.ToLower(content)
	checks := 0
	passed := 0
	for _, required := range item.Expectations.RequiredPoints {
		checks++
		if strings.Contains(lower, strings.ToLower(required)) {
			passed++
			result.Evidence = append(result.Evidence, "required point present: "+required)
		} else {
			result.Evidence = append(result.Evidence, "required point missing: "+required)
		}
	}
	for _, forbidden := range item.Expectations.ForbiddenPoints {
		checks++
		if strings.Contains(lower, strings.ToLower(forbidden)) {
			result.Evidence = append(result.Evidence, "forbidden point present: "+forbidden)
		} else {
			passed++
			result.Evidence = append(result.Evidence, "forbidden point absent: "+forbidden)
		}
	}
	if checks == 0 {
		result.Status, result.Score = "pass", 100
		result.Evidence = append(result.Evidence, "rule verifier had no assertions")
		return result
	}
	result.Score = passed * 100 / checks
	if passed == checks {
		result.Status = "pass"
	} else {
		result.Status = "fail"
	}
	return result
}

func (r Result) Validate() error {
	if r.Version != Version {
		return fmt.Errorf("unsupported eval result version %q", r.Version)
	}
	if r.RunID == "" || r.Suite == "" || r.StartedAt.IsZero() {
		return fmt.Errorf("eval result requires run_id, suite, and started_at")
	}
	if len(r.Cases) != r.Summary.Total {
		return fmt.Errorf("eval result summary total %d does not match %d cases", r.Summary.Total, len(r.Cases))
	}
	return nil
}

func (r Result) YAML() ([]byte, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	return yaml.Marshal(r)
}

func WriteResult(path string, result Result) error {
	data, err := result.YAML()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create eval result directory: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write eval result: %w", err)
	}
	return nil
}

func LoadResult(path string) (Result, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Result{}, fmt.Errorf("read eval result: %w", err)
	}
	var result Result
	if err := yaml.Unmarshal(data, &result); err != nil {
		return Result{}, fmt.Errorf("parse eval result: %w", err)
	}
	if err := result.Validate(); err != nil {
		return Result{}, err
	}
	return result, nil
}

type Regression struct {
	CaseID    string `yaml:"case_id" json:"caseId"`
	Baseline  string `yaml:"baseline" json:"baseline"`
	Candidate string `yaml:"candidate" json:"candidate"`
	Reason    string `yaml:"reason" json:"reason"`
}

type Comparison struct {
	Version      string       `yaml:"version" json:"version"`
	BaselineRun  string       `yaml:"baseline_run" json:"baselineRun"`
	CandidateRun string       `yaml:"candidate_run" json:"candidateRun"`
	Regressions  []Regression `yaml:"regressions" json:"regressions"`
	Improvements []Regression `yaml:"improvements" json:"improvements"`
	Unchanged    int          `yaml:"unchanged" json:"unchanged"`
}

func Compare(baseline, candidate Result) Comparison {
	comparison := Comparison{Version: Version, BaselineRun: baseline.RunID, CandidateRun: candidate.RunID, Regressions: []Regression{}, Improvements: []Regression{}}
	base := make(map[string]CaseResult, len(baseline.Cases))
	for _, item := range baseline.Cases {
		base[item.CaseID] = item
	}
	for _, item := range candidate.Cases {
		before, ok := base[item.CaseID]
		if !ok {
			comparison.Improvements = append(comparison.Improvements, Regression{CaseID: item.CaseID, Candidate: item.Status, Reason: "new case"})
			continue
		}
		if statusRank(item.Status) < statusRank(before.Status) || item.Score < before.Score {
			comparison.Regressions = append(comparison.Regressions, Regression{CaseID: item.CaseID, Baseline: before.Status, Candidate: item.Status, Reason: fmt.Sprintf("score %d -> %d", before.Score, item.Score)})
		} else if statusRank(item.Status) > statusRank(before.Status) || item.Score > before.Score {
			comparison.Improvements = append(comparison.Improvements, Regression{CaseID: item.CaseID, Baseline: before.Status, Candidate: item.Status, Reason: fmt.Sprintf("score %d -> %d", before.Score, item.Score)})
		} else {
			comparison.Unchanged++
		}
	}
	candidateIDs := make(map[string]struct{}, len(candidate.Cases))
	for _, item := range candidate.Cases {
		candidateIDs[item.CaseID] = struct{}{}
	}
	for _, item := range baseline.Cases {
		if _, ok := candidateIDs[item.CaseID]; !ok {
			comparison.Regressions = append(comparison.Regressions, Regression{CaseID: item.CaseID, Baseline: item.Status, Candidate: "missing", Reason: "case is missing from candidate run"})
		}
	}
	sort.Slice(comparison.Regressions, func(i, j int) bool { return comparison.Regressions[i].CaseID < comparison.Regressions[j].CaseID })
	sort.Slice(comparison.Improvements, func(i, j int) bool { return comparison.Improvements[i].CaseID < comparison.Improvements[j].CaseID })
	return comparison
}

func statusRank(status string) int {
	switch status {
	case "pass":
		return 2
	case "partial":
		return 1
	default:
		return 0
	}
}
