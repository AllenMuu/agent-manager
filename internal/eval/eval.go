// Package eval provides a local, deterministic evaluation harness.
//
// The first runner intentionally evaluates supplied candidate artifacts rather
// than invoking an agent. Agent execution remains an adapter concern; keeping
// the runner deterministic makes prompt, Skill, context, and adapter changes
// regressible without network access or hidden runtime state.
package eval

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/AllenMuu/skill-manager/internal/artifact"
	"github.com/AllenMuu/skill-manager/internal/policy"
	"github.com/AllenMuu/skill-manager/internal/run"
	"gopkg.in/yaml.v3"
)

const Version = "v1"
const CaseVersion = "v2"
const GovernanceCaseVersion = "v3"

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
	Version            string               `yaml:"version" json:"version"`
	ID                 string               `yaml:"id" json:"id"`
	Category           string               `yaml:"category" json:"category"`
	Input              map[string]string    `yaml:"input" json:"input"`
	Expectations       Expectations         `yaml:"expectations" json:"expectations"`
	Verifier           Verifier             `yaml:"verifier" json:"verifier"`
	Governance         *GovernanceAssertion `yaml:"governance,omitempty" json:"governance,omitempty"`
	governanceEvidence *GovernanceEvidence  `yaml:"-" json:"-"`
}

type GovernanceAssertion struct {
	Category            policy.EventCategory `yaml:"category,omitempty" json:"category,omitempty"`
	Tool                string               `yaml:"tool,omitempty" json:"tool,omitempty"`
	ActionType          string               `yaml:"action_type,omitempty" json:"action_type,omitempty"`
	Decision            policy.Outcome       `yaml:"decision,omitempty" json:"decision,omitempty"`
	ReasonCode          policy.ReasonCode    `yaml:"reason_code,omitempty" json:"reason_code,omitempty"`
	CapabilitiesReady   *bool                `yaml:"capabilities_ready,omitempty" json:"capabilities_ready,omitempty"`
	MissingCapabilities []policy.Control     `yaml:"missing_capabilities,omitempty" json:"missing_capabilities,omitempty"`
}

type GovernanceEvidence struct {
	Version          string                   `json:"version"`
	Events           []run.AuditRecord        `json:"events,omitempty"`
	CapabilityReport *policy.CapabilityReport `json:"capability_report,omitempty"`
}

func (c Case) Validate() error {
	if c.Version != Version && c.Version != CaseVersion && c.Version != GovernanceCaseVersion {
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
	if c.Version == GovernanceCaseVersion {
		if c.Input["governance_events"] != "events.json" {
			return fmt.Errorf("governance eval case %q input.governance_events must name local events.json", c.ID)
		}
		if c.Governance == nil {
			return fmt.Errorf("governance eval case %q requires governance assertions", c.ID)
		}
		assertion := c.Governance
		hasEventAssertion := assertion.Category != "" || assertion.Tool != "" || assertion.ActionType != "" || assertion.Decision != "" || assertion.ReasonCode != ""
		hasCapabilityAssertion := assertion.CapabilitiesReady != nil || len(assertion.MissingCapabilities) > 0
		if !hasEventAssertion && !hasCapabilityAssertion {
			return fmt.Errorf("governance eval case %q has no assertions", c.ID)
		}
		if hasEventAssertion {
			if !policy.IsKnownEventCategory(assertion.Category) || !policy.IsKnownOutcome(assertion.Decision) {
				return fmt.Errorf("governance eval case %q requires a supported event category and decision", c.ID)
			}
			if (assertion.Decision == policy.Deny || assertion.Decision == policy.RequireApproval) && !policy.IsKnownReasonCode(assertion.ReasonCode) {
				return fmt.Errorf("governance eval case %q requires a known reason code", c.ID)
			}
		}
		for _, control := range assertion.MissingCapabilities {
			if !policy.IsKnownControl(control) {
				return fmt.Errorf("governance eval case %q references unknown capability %q", c.ID, control)
			}
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
	Version        string
	ID             string
	Category       string
	Root           string
	Cases          []Case
	GovernanceOnly bool
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
	suite := Suite{Version: Version, ID: filepath.Base(abs), Root: abs, GovernanceOnly: true}
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
		caseDecoder := yaml.NewDecoder(bytes.NewReader(data))
		caseDecoder.KnownFields(true)
		if err := caseDecoder.Decode(&item); err != nil {
			return Suite{}, fmt.Errorf("parse eval case %s: %w", casePath, err)
		}
		var trailing any
		if err := caseDecoder.Decode(&trailing); err != io.EOF {
			return Suite{}, fmt.Errorf("eval case %s must contain exactly one YAML document", casePath)
		}
		if err := item.Validate(); err != nil {
			return Suite{}, err
		}
		if item.Version == GovernanceCaseVersion {
			evidencePath := filepath.Join(caseDir, item.Input["governance_events"])
			evidenceInfo, err := os.Lstat(evidencePath)
			if err != nil {
				return Suite{}, fmt.Errorf("inspect governance evidence %s: %w", evidencePath, err)
			}
			if evidenceInfo.Mode()&os.ModeSymlink != 0 || !evidenceInfo.Mode().IsRegular() {
				return Suite{}, fmt.Errorf("governance evidence %s must be a direct regular file", evidencePath)
			}
			data, err := os.ReadFile(evidencePath)
			if err != nil {
				return Suite{}, fmt.Errorf("read governance evidence %s: %w", evidencePath, err)
			}
			if len(data) > 4<<20 {
				return Suite{}, fmt.Errorf("governance evidence %s exceeds maximum size", evidencePath)
			}
			var evidence GovernanceEvidence
			decoder := json.NewDecoder(bytes.NewReader(data))
			decoder.DisallowUnknownFields()
			if err := decoder.Decode(&evidence); err != nil {
				return Suite{}, fmt.Errorf("parse governance evidence %s: %w", evidencePath, err)
			}
			var trailing any
			if err := decoder.Decode(&trailing); err != io.EOF {
				return Suite{}, fmt.Errorf("governance evidence %s must contain one JSON object", evidencePath)
			}
			if evidence.Version != Version {
				return Suite{}, fmt.Errorf("governance evidence %s has unsupported version %q", evidencePath, evidence.Version)
			}
			for _, event := range evidence.Events {
				if err := event.Validate(); err != nil {
					return Suite{}, fmt.Errorf("invalid governance evidence event: %w", err)
				}
			}
			if evidence.CapabilityReport != nil {
				if err := validateCapabilityReport(*evidence.CapabilityReport); err != nil {
					return Suite{}, err
				}
			}
			if len(evidence.Events) == 0 && evidence.CapabilityReport == nil {
				return Suite{}, fmt.Errorf("governance evidence %s is empty", evidencePath)
			}
			item.governanceEvidence = &evidence
		} else {
			suite.GovernanceOnly = false
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
	AgentLabel      string
	ConfigVersion   string
	CandidateDir    string
	GovernanceRunID string
	RunStoreRoot    string
	Now             time.Time
}

type CaseResult struct {
	CaseID          string                             `yaml:"case_id" json:"caseId"`
	Status          string                             `yaml:"status" json:"status"`
	Score           int                                `yaml:"score" json:"score"`
	Evidence        []string                           `yaml:"evidence" json:"evidence"`
	PolicySnapshots []artifact.PolicySnapshotReference `yaml:"policy_snapshots,omitempty" json:"policySnapshots,omitempty"`
}

type Summary struct {
	Total   int `yaml:"total" json:"total"`
	Passed  int `yaml:"passed" json:"passed"`
	Failed  int `yaml:"failed" json:"failed"`
	Partial int `yaml:"partial" json:"partial"`
	Score   int `yaml:"score" json:"score"`
}

type Result struct {
	Version         string                             `yaml:"version" json:"version"`
	RunID           string                             `yaml:"run_id" json:"runId"`
	Suite           string                             `yaml:"suite" json:"suite"`
	AgentLabel      string                             `yaml:"agent_label,omitempty" json:"agentLabel,omitempty"`
	ConfigVersion   string                             `yaml:"config_version,omitempty" json:"configVersion,omitempty"`
	StartedAt       time.Time                          `yaml:"started_at" json:"startedAt"`
	DurationMS      int64                              `yaml:"duration_ms" json:"durationMs"`
	Cases           []CaseResult                       `yaml:"cases" json:"cases"`
	PolicySnapshots []artifact.PolicySnapshotReference `yaml:"policy_snapshots,omitempty" json:"policySnapshots,omitempty"`
	Summary         Summary                            `yaml:"summary" json:"summary"`
}

func Run(suite Suite, options Options) (Result, error) {
	if len(suite.Cases) == 0 {
		return Result{}, fmt.Errorf("eval suite %q has no cases", suite.ID)
	}
	needsCandidate := false
	for _, item := range suite.Cases {
		if item.Version != GovernanceCaseVersion {
			needsCandidate = true
		}
	}
	if options.CandidateDir == "" && needsCandidate {
		return Result{}, fmt.Errorf("eval run requires --candidate-dir with responses generated for the selected agent and configuration")
	}
	var externalEvents []run.AuditRecord
	if options.GovernanceRunID != "" {
		var store *run.Store
		var err error
		if options.RunStoreRoot == "" {
			store, err = run.DefaultStore()
		} else {
			store, err = run.NewStore(options.RunStoreRoot)
		}
		if err != nil {
			return Result{}, err
		}
		externalEvents, err = store.Events(options.GovernanceRunID)
		if err != nil {
			return Result{}, err
		}
		if len(externalEvents) == 0 {
			return Result{}, fmt.Errorf("run %q has no governance audit events", options.GovernanceRunID)
		}
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
		var caseResult CaseResult
		if item.Version == GovernanceCaseVersion {
			evidence := GovernanceEvidence{}
			if item.governanceEvidence != nil {
				evidence = *item.governanceEvidence
			}
			if options.GovernanceRunID != "" {
				evidence.Events = externalEvents
			}
			caseResult = evaluateGovernance(item, evidence)
		} else {
			content, source, err := candidate(item, options.CandidateDir)
			if err != nil {
				return Result{}, err
			}
			caseResult = evaluate(item, content, source)
		}
		result.Cases = append(result.Cases, caseResult)
		for _, reference := range caseResult.PolicySnapshots {
			result.PolicySnapshots = appendSnapshotReference(result.PolicySnapshots, reference)
		}
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
	sort.Slice(result.PolicySnapshots, func(i, j int) bool {
		if result.PolicySnapshots[i].PolicyID == result.PolicySnapshots[j].PolicyID {
			return result.PolicySnapshots[i].Hash < result.PolicySnapshots[j].Hash
		}
		return result.PolicySnapshots[i].PolicyID < result.PolicySnapshots[j].PolicyID
	})
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

func evaluateGovernance(item Case, evidence GovernanceEvidence) CaseResult {
	result := CaseResult{CaseID: item.ID, Status: "pass", Score: 100, Evidence: []string{}}
	checks, passed := 0, 0
	assertion := item.Governance
	if assertion.Category != "" || assertion.Tool != "" || assertion.ActionType != "" || assertion.Decision != "" || assertion.ReasonCode != "" {
		checks++
		matches := make([]run.AuditRecord, 0)
		for _, event := range evidence.Events {
			if event.Category == assertion.Category && (assertion.Tool == "" || event.Tool == assertion.Tool) && (assertion.ActionType == "" || event.ActionType == assertion.ActionType) {
				matches = append(matches, event)
			}
		}
		if len(matches) == 0 {
			result.Evidence = append(result.Evidence, "no audit event matched the expected category, tool, and action")
		} else {
			matchPasses := true
			for _, event := range matches {
				result.PolicySnapshots = appendSnapshotReference(result.PolicySnapshots, artifact.PolicySnapshotReference{RunID: event.RunID, PolicyID: event.PolicyID, Version: event.PolicyVersion, Hash: event.PolicyHash, ResolvedAt: event.PolicyResolvedAt})
				if event.Decision != assertion.Decision || (assertion.ReasonCode != "" && event.ReasonCode != assertion.ReasonCode) {
					matchPasses = false
				}
			}
			if matchPasses {
				passed++
				result.Evidence = append(result.Evidence, fmt.Sprintf("%d matching audit event(s) recorded %s with the expected reason", len(matches), assertion.Decision))
			} else {
				result.Evidence = append(result.Evidence, fmt.Sprintf("matching audit events did not all record %s with reason %s", assertion.Decision, assertion.ReasonCode))
			}
		}
	}
	if assertion.CapabilitiesReady != nil || len(assertion.MissingCapabilities) > 0 {
		checks++
		report := evidence.CapabilityReport
		expectedReady := "not asserted"
		if assertion.CapabilitiesReady != nil {
			expectedReady = fmt.Sprint(*assertion.CapabilitiesReady)
		}
		if report == nil {
			result.Evidence = append(result.Evidence, "capability report was not provided")
		} else if (assertion.CapabilitiesReady != nil && *assertion.CapabilitiesReady != report.Ready) || !sameControls(assertion.MissingCapabilities, report.Missing) {
			result.Evidence = append(result.Evidence, fmt.Sprintf("capability report ready=%t missing=%v did not match expected ready=%s missing=%v", report.Ready, report.Missing, expectedReady, assertion.MissingCapabilities))
		} else {
			passed++
			result.Evidence = append(result.Evidence, fmt.Sprintf("capability report matched ready=%t missing=%v", report.Ready, report.Missing))
		}
	}
	if checks == 0 {
		result.Status, result.Score = "fail", 0
		result.Evidence = append(result.Evidence, "governance case contains no assertions")
	} else {
		result.Score = passed * 100 / checks
		if passed != checks {
			result.Status = "fail"
		}
	}
	return result
}

func validateCapabilityReport(report policy.CapabilityReport) error {
	if report.Ready != (len(report.Missing) == 0) {
		return fmt.Errorf("capability report ready flag does not match missing controls")
	}
	required := make(map[policy.Control]bool, len(report.Required))
	for _, control := range report.Required {
		required[control] = true
	}
	missing := make(map[policy.Control]bool, len(report.Missing))
	for _, control := range report.Missing {
		if !required[control] {
			return fmt.Errorf("missing capability %q was not declared required", control)
		}
		missing[control] = true
	}
	for _, control := range report.Warnings {
		if required[control] || missing[control] {
			return fmt.Errorf("optional capability warning %q conflicts with required controls", control)
		}
	}
	for _, list := range [][]policy.Control{report.Required, report.Missing, report.Warnings} {
		seen := map[policy.Control]bool{}
		for _, control := range list {
			if !policy.IsKnownControl(control) || seen[control] {
				return fmt.Errorf("capability report contains unknown or duplicate control %q", control)
			}
			seen[control] = true
		}
	}
	return nil
}

func sameControls(expected, actual []policy.Control) bool {
	left, right := append([]policy.Control(nil), expected...), append([]policy.Control(nil), actual...)
	sort.Slice(left, func(i, j int) bool { return left[i] < left[j] })
	sort.Slice(right, func(i, j int) bool { return right[i] < right[j] })
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func appendSnapshotReference(references []artifact.PolicySnapshotReference, reference artifact.PolicySnapshotReference) []artifact.PolicySnapshotReference {
	if reference.Validate() != nil {
		return references
	}
	for _, existing := range references {
		if existing == reference {
			return references
		}
	}
	return append(references, reference)
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
	allReferences := make([]artifact.PolicySnapshotReference, 0)
	passed, failed, partial, score := 0, 0, 0, 0
	for _, item := range r.Cases {
		if !safeCaseID.MatchString(item.CaseID) {
			return fmt.Errorf("eval result contains unsafe case id %q", item.CaseID)
		}
		if _, ok := seen[item.CaseID]; ok {
			return fmt.Errorf("eval result contains duplicate case id %q", item.CaseID)
		}
		seen[item.CaseID] = struct{}{}
		for _, reference := range item.PolicySnapshots {
			if err := reference.Validate(); err != nil {
				return fmt.Errorf("eval result case %q has invalid policy snapshot: %w", item.CaseID, err)
			}
			allReferences = appendSnapshotReference(allReferences, reference)
		}
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
	if len(allReferences) != len(r.PolicySnapshots) {
		return fmt.Errorf("eval result policy snapshot summary does not match case references")
	}
	resultReferences := make(map[artifact.PolicySnapshotReference]struct{}, len(r.PolicySnapshots))
	for _, reference := range r.PolicySnapshots {
		if err := reference.Validate(); err != nil {
			return fmt.Errorf("eval result has invalid policy snapshot: %w", err)
		}
		if _, duplicate := resultReferences[reference]; duplicate {
			return fmt.Errorf("eval result repeats a policy snapshot reference")
		}
		resultReferences[reference] = struct{}{}
	}
	for _, reference := range allReferences {
		if _, ok := resultReferences[reference]; !ok {
			return fmt.Errorf("eval result omits a case policy snapshot reference")
		}
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
		candidateRank := comparisonRank(item)
		baselineRank := comparisonRank(before)
		if candidateRank < baselineRank || item.Score < before.Score {
			comparison.Regressions = append(comparison.Regressions, Regression{CaseID: item.CaseID, Baseline: before.Status, Candidate: item.Status, Reason: fmt.Sprintf("score %d -> %d", before.Score, item.Score)})
		} else if candidateRank > baselineRank || item.Score > before.Score {
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

func comparisonRank(item CaseResult) int {
	if missingCandidate(item) {
		return -1
	}
	switch item.Status {
	case "pass":
		return 2
	case "partial":
		return 1
	case "fail":
		return 0
	default:
		return -1
	}
}
