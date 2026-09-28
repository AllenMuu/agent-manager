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
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/AllenMuu/skill-manager/internal/artifact"
	"gopkg.in/yaml.v3"
)

const Version = "v1"
const CaseVersion = "v2"

var safeCaseID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

type Expectations struct {
	RequiredPoints  []string `yaml:"required_points,omitempty" json:"requiredPoints,omitempty"`
	ForbiddenPoints []string `yaml:"forbidden_points,omitempty" json:"forbiddenPoints,omitempty"`
	EvidencePoints  []string `yaml:"evidence_points,omitempty" json:"evidencePoints,omitempty"`
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
}

func (c Case) Validate() error {
	if c.Version != Version && c.Version != CaseVersion {
		return fmt.Errorf("case %q has unsupported version %q", c.ID, c.Version)
	}
	if !safeCaseID.MatchString(c.ID) || c.ID == "." || c.ID == ".." {
		return fmt.Errorf("eval case id %q is unsafe", c.ID)
	}
	if c.Category == "" {
		return fmt.Errorf("eval case %q category is required", c.ID)
	}
	if c.Version == CaseVersion {
		if c.Input["intent"] != "intent.yaml" {
			return fmt.Errorf("eval case %q input.intent must name the local intent.yaml", c.ID)
		}
		if len(c.Expectations.RequiredPoints) == 0 || len(c.Expectations.EvidencePoints) == 0 {
			return fmt.Errorf("eval case %q requires required_points and evidence_points", c.ID)
		}
	}
	for _, points := range [][]string{c.Expectations.RequiredPoints, c.Expectations.ForbiddenPoints, c.Expectations.EvidencePoints} {
		for _, point := range points {
			if strings.TrimSpace(point) == "" {
				return fmt.Errorf("eval case %q contains an empty expectation", c.ID)
			}
		}
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
	info, err := os.Lstat(abs)
	if err != nil {
		return Suite{}, fmt.Errorf("inspect eval suite: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return Suite{}, fmt.Errorf("eval suite %s is not a directory", abs)
	}
	suite := Suite{Version: Version, ID: filepath.Base(abs), Root: abs}
	casesRoot := filepath.Join(abs, "cases")
	casesInfo, err := os.Lstat(casesRoot)
	if err != nil {
		return Suite{}, fmt.Errorf("inspect eval cases: %w", err)
	}
	if casesInfo.Mode()&os.ModeSymlink != 0 || !casesInfo.IsDir() {
		return Suite{}, fmt.Errorf("eval cases %s must be a direct directory", casesRoot)
	}
	entries, err := os.ReadDir(casesRoot)
	if err != nil {
		return Suite{}, fmt.Errorf("read eval cases: %w", err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		caseDir := filepath.Join(casesRoot, entry.Name())
		caseDirInfo, err := os.Lstat(caseDir)
		if err != nil {
			return Suite{}, fmt.Errorf("inspect eval case directory %s: %w", caseDir, err)
		}
		if caseDirInfo.Mode()&os.ModeSymlink != 0 || !caseDirInfo.IsDir() {
			return Suite{}, fmt.Errorf("eval case directory %s must be a direct directory", caseDir)
		}
		casePath := filepath.Join(caseDir, "case.yaml")
		caseInfo, err := os.Lstat(casePath)
		if err != nil {
			return Suite{}, fmt.Errorf("inspect eval case %s: %w", casePath, err)
		}
		if caseInfo.Mode()&os.ModeSymlink != 0 || !caseInfo.Mode().IsRegular() {
			return Suite{}, fmt.Errorf("eval case %s must be a direct regular file", casePath)
		}
		data, err := os.ReadFile(casePath)
		if err != nil {
			return Suite{}, fmt.Errorf("read eval case %s: %w", casePath, err)
		}
		var item Case
		if err := yaml.Unmarshal(data, &item); err != nil {
			return Suite{}, fmt.Errorf("parse eval case %s: %w", casePath, err)
		}
		if err := item.Validate(); err != nil {
			return Suite{}, err
		}
		if item.Version == CaseVersion {
			intentPath := filepath.Join(caseDir, item.Input["intent"])
			intentInfo, err := os.Lstat(intentPath)
			if err != nil {
				return Suite{}, fmt.Errorf("inspect eval input %s: %w", intentPath, err)
			}
			if intentInfo.Mode()&os.ModeSymlink != 0 || !intentInfo.Mode().IsRegular() {
				return Suite{}, fmt.Errorf("eval input %s must be a direct regular file", intentPath)
			}
			intent, err := artifact.Load(intentPath)
			if err != nil {
				return Suite{}, fmt.Errorf("load eval input: %w", err)
			}
			if intent.Kind() != artifact.Intent || intent.ID() != item.ID {
				return Suite{}, fmt.Errorf("eval case %q input must be an intent artifact with the same id", item.ID)
			}
		}
		if suite.Category == "" {
			suite.Category = item.Category
		}
		if item.Category != suite.Category && item.Category != "" {
			return Suite{}, fmt.Errorf("eval case %q category %q does not match suite %q", item.ID, item.Category, suite.Category)
		}
		for _, existing := range suite.Cases {
			if existing.ID == item.ID {
				return Suite{}, fmt.Errorf("eval suite contains duplicate case id %q", item.ID)
			}
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
	AgentLabel    string
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
	AgentLabel    string       `yaml:"agent_label,omitempty" json:"agentLabel,omitempty"`
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
	if options.CandidateDir == "" {
		return Result{}, fmt.Errorf("eval run requires --candidate-dir with responses generated for the selected agent and configuration")
	}
	clockStart := time.Now()
	if options.Now.IsZero() {
		options.Now = time.Now().UTC()
	}
	started := options.Now.UTC()
	result := Result{
		Version: Version, RunID: "run-" + started.Format("20060102T150405.000000000Z"),
		Suite: suite.ID, AgentLabel: options.AgentLabel, ConfigVersion: options.ConfigVersion, StartedAt: started,
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
	result.DurationMS = time.Since(clockStart).Milliseconds()
	if result.DurationMS < 0 {
		result.DurationMS = 0
	}
	return result, nil
}

func candidate(item Case, candidateDir string) (string, string, error) {
	paths := []string{}
	if candidateDir != "" {
		base, err := filepath.Abs(candidateDir)
		if err != nil {
			return "", "", fmt.Errorf("resolve eval candidate directory: %w", err)
		}
		info, err := os.Lstat(base)
		if err != nil {
			return "", "", fmt.Errorf("inspect eval candidate directory %s: %w", base, err)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return "", "", fmt.Errorf("eval candidate directory %s must be a direct directory", base)
		}
		paths = append(paths,
			filepath.Join(base, item.ID+".md"),
			filepath.Join(base, item.ID+".txt"),
		)
		nested := filepath.Join(base, item.ID)
		if nestedInfo, nestedErr := os.Lstat(nested); nestedErr == nil {
			if nestedInfo.Mode()&os.ModeSymlink != 0 || !nestedInfo.IsDir() {
				return "", "", fmt.Errorf("eval candidate case directory %s must be a direct directory", nested)
			}
			paths = append(paths, filepath.Join(nested, "response.md"))
		} else if !os.IsNotExist(nestedErr) {
			return "", "", fmt.Errorf("inspect eval candidate case directory %s: %w", nested, nestedErr)
		}
	}
	for _, path := range paths {
		info, err := os.Lstat(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return "", "", fmt.Errorf("inspect eval candidate %s: %w", path, err)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return "", "", fmt.Errorf("eval candidate %s must be a direct regular file", path)
		}
		data, err := os.ReadFile(path)
		if err == nil {
			return string(data), path, nil
		}
		return "", "", fmt.Errorf("read eval candidate %s: %w", path, err)
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
	for _, evidence := range item.Expectations.EvidencePoints {
		checks++
		if strings.Contains(lower, strings.ToLower(evidence)) {
			passed++
			result.Evidence = append(result.Evidence, "evidence point present: "+evidence)
		} else {
			result.Evidence = append(result.Evidence, "evidence point missing: "+evidence)
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
	if !safeCaseID.MatchString(r.RunID) || r.RunID == "." || r.RunID == ".." {
		return fmt.Errorf("eval result has unsafe run_id %q", r.RunID)
	}
	if len(r.Cases) != r.Summary.Total {
		return fmt.Errorf("eval result summary total %d does not match %d cases", r.Summary.Total, len(r.Cases))
	}
	seen := make(map[string]struct{}, len(r.Cases))
	passed, failed, partial, score := 0, 0, 0, 0
	for _, item := range r.Cases {
		if !safeCaseID.MatchString(item.CaseID) {
			return fmt.Errorf("eval result contains unsafe case id %q", item.CaseID)
		}
		if _, ok := seen[item.CaseID]; ok {
			return fmt.Errorf("eval result contains duplicate case id %q", item.CaseID)
		}
		seen[item.CaseID] = struct{}{}
		if item.Score < 0 || item.Score > 100 {
			return fmt.Errorf("eval result case %q has out-of-range score %d", item.CaseID, item.Score)
		}
		switch item.Status {
		case "pass":
			passed++
		case "fail":
			failed++
		case "partial":
			partial++
		default:
			return fmt.Errorf("eval result case %q has unsupported status %q", item.CaseID, item.Status)
		}
		score += item.Score
	}
	if r.Summary.Passed != passed || r.Summary.Failed != failed || r.Summary.Partial != partial {
		return fmt.Errorf("eval result summary counts do not match case results")
	}
	if r.Summary.Total > 0 {
		score /= r.Summary.Total
	}
	if r.Summary.Score != score {
		return fmt.Errorf("eval result summary score %d does not match case average %d", r.Summary.Score, score)
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
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create eval result directory: %w", err)
	}
	return writeResultFile(path, data)
}

// WriteProjectResult stores the default result under real project-owned path
// components. Explicit --output paths use WriteResult instead.
func WriteProjectResult(project string, result Result) (string, error) {
	data, err := result.YAML()
	if err != nil {
		return "", err
	}
	store, err := artifact.NewStore(project)
	if err != nil {
		return "", err
	}
	current := store.ProjectRoot
	for _, component := range []string{".agent-manager", "evals"} {
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			if err := os.Mkdir(current, 0o755); err != nil && !os.IsExist(err) {
				return "", fmt.Errorf("create eval result directory %s: %w", current, err)
			}
			info, err = os.Lstat(current)
		}
		if err != nil {
			return "", fmt.Errorf("inspect eval result directory %s: %w", current, err)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return "", fmt.Errorf("eval result directory %s must be a real directory", current)
		}
	}
	path := filepath.Join(current, result.RunID+".yaml")
	if err := writeResultFile(path, data); err != nil {
		return "", err
	}
	return path, nil
}

func writeResultFile(path string, data []byte) error {
	dir := filepath.Dir(path)
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return fmt.Errorf("eval result destination %s must be a direct regular file", path)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect eval result destination: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".eval-result-")
	if err != nil {
		return fmt.Errorf("create eval result temp file: %w", err)
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write eval result temp file: %w", err)
	}
	if err := tmp.Chmod(0o644); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("publish eval result: %w", err)
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
	if result.AgentLabel == "" {
		var legacy struct {
			Agent string `yaml:"agent"`
		}
		if err := yaml.Unmarshal(data, &legacy); err != nil {
			return Result{}, fmt.Errorf("parse legacy eval agent label: %w", err)
		}
		result.AgentLabel = legacy.Agent
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
	Added        []Regression `yaml:"added" json:"added"`
	Unchanged    int          `yaml:"unchanged" json:"unchanged"`
}

func Compare(baseline, candidate Result) Comparison {
	comparison := Comparison{Version: Version, BaselineRun: baseline.RunID, CandidateRun: candidate.RunID, Regressions: []Regression{}, Improvements: []Regression{}, Added: []Regression{}}
	base := make(map[string]CaseResult, len(baseline.Cases))
	for _, item := range baseline.Cases {
		base[item.CaseID] = item
	}
	for _, item := range candidate.Cases {
		before, ok := base[item.CaseID]
		if !ok {
			comparison.Added = append(comparison.Added, Regression{CaseID: item.CaseID, Candidate: item.Status, Reason: "new case"})
			continue
		}
		becameMissing := missingCandidate(item) && !missingCandidate(before)
		statusRegressed := statusRank(item.Status) < statusRank(before.Status)
		statusImproved := statusRank(item.Status) > statusRank(before.Status)
		if becameMissing || statusRegressed || item.Score < before.Score {
			comparison.Regressions = append(comparison.Regressions, Regression{CaseID: item.CaseID, Baseline: before.Status, Candidate: item.Status, Reason: fmt.Sprintf("score %d -> %d", before.Score, item.Score)})
		} else if !missingCandidate(item) && (statusImproved || item.Score > before.Score) {
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
	sort.Slice(comparison.Added, func(i, j int) bool { return comparison.Added[i].CaseID < comparison.Added[j].CaseID })
	return comparison
}

func missingCandidate(item CaseResult) bool {
	for _, evidence := range item.Evidence {
		if evidence == "candidate response was not provided" {
			return true
		}
	}
	return false
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
