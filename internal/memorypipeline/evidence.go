// Package memorypipeline derives reviewable, project-local knowledge from
// explicitly imported run evidence. It never owns or mutates run history.
package memorypipeline

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/AllenMuu/skill-manager/internal/policy"
	"github.com/AllenMuu/skill-manager/internal/run"
)

var (
	ErrInvalidEvidence  = errors.New("invalid run evidence")
	ErrEvidenceChanged  = errors.New("run evidence changed since import")
	ErrEvidenceNotFound = errors.New("run evidence not found")
)

type ImportStatus string

const ImportCompleted ImportStatus = "completed"

// RunEvidenceRef is an immutable pointer to a normalized source snapshot.
// ProjectID is an opaque, registry-owned identity; filesystem paths stay in
// the provider and are never copied into the derived database.
type RunEvidenceRef struct {
	ID          string       `json:"id"`
	ProjectID   string       `json:"projectId"`
	Runtime     string       `json:"runtime"`
	NativeRunID string       `json:"nativeRunId"`
	SourceRef   string       `json:"sourceRef"`
	CreatedAt   time.Time    `json:"createdAt"`
	ContentHash string       `json:"contentHash"`
	Status      ImportStatus `json:"status"`
}

// RunEvidenceProvider discovers terminal runs and resolves an exact reference.
// Implementations must be read-only and must not infer unavailable transcript
// or output fields.
type RunEvidenceProvider interface {
	Discover(context.Context, string) ([]RunEvidenceRef, error)
	Read(context.Context, RunEvidenceRef) (RunEvidence, error)
}

// RunEvidence is the bounded evidence shape consumed by the deterministic
// extractor. Zero values mean the source did not contain that evidence.
type RunEvidence struct {
	Ref              RunEvidenceRef      `json:"ref"`
	Status           string              `json:"runStatus"`
	UpdatedAt        time.Time           `json:"updatedAt"`
	SummaryText      string              `json:"summary,omitempty"`
	Decisions        []string            `json:"decisions,omitempty"`
	Discoveries      []string            `json:"discoveries,omitempty"`
	Constraints      []string            `json:"constraints,omitempty"`
	KnownIssues      []string            `json:"knownIssues,omitempty"`
	Commands         []string            `json:"commands,omitempty"`
	FailedApproaches []string            `json:"failedApproaches,omitempty"`
	PolicyID         string              `json:"policyId,omitempty"`
	PolicyVersion    string              `json:"policyVersion,omitempty"`
	PolicyControls   []string            `json:"policyControls,omitempty"`
	Termination      string              `json:"termination,omitempty"`
	SourceDigest     string              `json:"sourceDigest,omitempty"`
	Events           []EvidenceEvent     `json:"events,omitempty"`
	Procedures       []ProcedureEvidence `json:"procedures,omitempty"`
	projectRoot      string
}

const (
	ProcedureOutcomeSucceeded = "succeeded"
	ProcedureOutcomeVerified  = "verified"
	ProcedureOutcomeFailed    = "failed"
)

// ProcedureEvidence is an explicit provider claim about one reusable
// procedure. Generalizable must be true; the pipeline never infers it from
// repeated text or project-local filenames.
type ProcedureEvidence struct {
	Key           string `json:"key"`
	Content       string `json:"content"`
	Outcome       string `json:"outcome"`
	Generalizable bool   `json:"generalizable"`
}

type EvidenceEvent struct {
	ID        string    `json:"id"`
	Timestamp time.Time `json:"timestamp"`
	Category  string    `json:"category"`
	Decision  string    `json:"decision,omitempty"`
	Result    string    `json:"result,omitempty"`
	Reason    string    `json:"reason,omitempty"`
}

// AgentManagerRunProvider adapts the existing lifecycle/audit store. Project
// root matching is exact after absolute-path cleaning; the store itself remains
// the sole owner of the records and audit events.
type AgentManagerRunProvider struct {
	store       *run.Store
	projectID   string
	projectRoot string
}

func NewAgentManagerRunProvider(store *run.Store, projectID, projectRoot string) (*AgentManagerRunProvider, error) {
	if store == nil || strings.TrimSpace(projectID) == "" || strings.TrimSpace(projectRoot) == "" {
		return nil, fmt.Errorf("%w: run store, project identity, and project root are required", ErrInvalidEvidence)
	}
	root, err := filepath.Abs(projectRoot)
	if err != nil {
		return nil, fmt.Errorf("resolve project root: %w", err)
	}
	return &AgentManagerRunProvider{store: store, projectID: projectID, projectRoot: filepath.Clean(root)}, nil
}

func (p *AgentManagerRunProvider) Discover(ctx context.Context, projectID string) ([]RunEvidenceRef, error) {
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	if p == nil || projectID != p.projectID {
		return nil, ErrEvidenceNotFound
	}
	records, err := p.store.List()
	if err != nil {
		return nil, err
	}
	refs := make([]RunEvidenceRef, 0, len(records))
	for _, record := range records {
		if !samePath(record.ProjectRoot, p.projectRoot) || !terminal(record.Status) {
			continue
		}
		events, err := p.store.Events(record.ID)
		if err != nil {
			return nil, err
		}
		evidence := evidenceFromRun(record, events, p.projectID)
		ref, err := referenceFor(evidence)
		if err != nil {
			return nil, err
		}
		refs = append(refs, ref)
	}
	sort.Slice(refs, func(i, j int) bool { return refs[i].ID < refs[j].ID })
	return refs, nil
}

func (p *AgentManagerRunProvider) Read(ctx context.Context, ref RunEvidenceRef) (RunEvidence, error) {
	if err := contextErr(ctx); err != nil {
		return RunEvidence{}, err
	}
	if p == nil || ref.ProjectID != p.projectID || ref.Status != ImportCompleted || ref.NativeRunID == "" {
		return RunEvidence{}, ErrEvidenceNotFound
	}
	record, err := p.store.Get(ref.NativeRunID)
	if err != nil {
		return RunEvidence{}, fmt.Errorf("read source run: %w", err)
	}
	if !samePath(record.ProjectRoot, p.projectRoot) || !terminal(record.Status) {
		return RunEvidence{}, ErrEvidenceNotFound
	}
	events, err := p.store.Events(record.ID)
	if err != nil {
		return RunEvidence{}, err
	}
	evidence := evidenceFromRun(record, events, p.projectID)
	want, err := referenceFor(evidence)
	if err != nil {
		return RunEvidence{}, err
	}
	if want.ID != ref.ID || want.ContentHash != ref.ContentHash || want.SourceRef != ref.SourceRef || want.Runtime != ref.Runtime {
		return RunEvidence{}, ErrEvidenceChanged
	}
	evidence.Ref = want
	return evidence, nil
}

func evidenceFromRun(record run.Record, events []run.AuditRecord, projectID string) RunEvidence {
	controls := append([]policy.Control(nil), record.Policy.Policy.Enforcement.OptionalControls...)
	controlNames := make([]string, 0, len(controls)+3)
	for _, control := range controls {
		controlNames = append(controlNames, string(control))
	}
	if record.Policy.Policy.Network != nil {
		controlNames = append(controlNames, string(policy.ControlNetworkRestriction))
	}
	if record.Policy.Policy.Credentials != nil {
		controlNames = append(controlNames, string(policy.ControlCredentialScope))
	}
	if record.Policy.Policy.Budget != nil {
		controlNames = append(controlNames, "configured_budgets")
	}
	sort.Strings(controlNames)
	controlNames = compactStrings(controlNames)
	result := RunEvidence{
		Ref:    RunEvidenceRef{ProjectID: projectID, Runtime: record.Runtime, NativeRunID: record.ID, SourceRef: "agent-manager://runs/" + record.ID, CreatedAt: record.CreatedAt.UTC(), Status: ImportCompleted},
		Status: string(record.Status), UpdatedAt: record.UpdatedAt.UTC(), PolicyID: record.Policy.PolicyID, PolicyVersion: record.Policy.Version,
		PolicyControls: controlNames, Termination: record.TerminationReason,
		Events: make([]EvidenceEvent, 0, len(events)),
	}
	for _, event := range events {
		result.Events = append(result.Events, EvidenceEvent{ID: event.ID, Timestamp: event.Timestamp.UTC(), Category: string(event.Category), Decision: string(event.Decision), Result: string(event.Result), Reason: string(event.ReasonCode)})
	}
	return result
}

func referenceFor(evidence RunEvidence) (RunEvidenceRef, error) {
	if evidence.Ref.ProjectID == "" || evidence.Ref.Runtime == "" || evidence.Ref.NativeRunID == "" || evidence.Ref.CreatedAt.IsZero() || !terminal(run.Status(evidence.Status)) {
		return RunEvidenceRef{}, ErrInvalidEvidence
	}
	if err := validateEvidenceBounds(evidence); err != nil {
		return RunEvidenceRef{}, err
	}
	canonical, err := normalizeEvidence(evidence)
	if err != nil {
		return RunEvidenceRef{}, err
	}
	if len(canonical) > maxNormalizedEvidence {
		return RunEvidenceRef{}, fmt.Errorf("%w: normalized evidence exceeds %d bytes", ErrInvalidEvidence, maxNormalizedEvidence)
	}
	contentSum := sha256.Sum256(canonical)
	contentHash := "sha256:" + hex.EncodeToString(contentSum[:])
	sourceRef := evidence.Ref.SourceRef
	if sourceRef == "" {
		sourceRef = "agent-manager://runs/" + evidence.Ref.NativeRunID
	}
	ref := RunEvidenceRef{
		ID: evidenceIdentity(evidence.Ref.ProjectID, evidence.Ref.Runtime, evidence.Ref.NativeRunID, contentHash), ProjectID: evidence.Ref.ProjectID,
		Runtime: evidence.Ref.Runtime, NativeRunID: evidence.Ref.NativeRunID,
		SourceRef: sourceRef,
		CreatedAt: evidence.Ref.CreatedAt.UTC(), ContentHash: contentHash, Status: ImportCompleted,
	}
	if err := validateEvidenceRef(ref); err != nil {
		return RunEvidenceRef{}, fmt.Errorf("%w: %v", ErrInvalidEvidence, err)
	}
	return ref, nil
}

// ReferenceFor calculates the canonical immutable reference for a normalized
// provider snapshot. Importers should use this rather than inventing IDs.
func ReferenceFor(evidence RunEvidence) (RunEvidenceRef, error) { return referenceFor(evidence) }

func evidenceIdentity(projectID, runtime, nativeRunID, contentHash string) string {
	identityInput := strings.Join([]string{projectID, runtime, nativeRunID, contentHash}, "\x00")
	identitySum := sha256.Sum256([]byte(identityInput))
	return "ev_" + hex.EncodeToString(identitySum[:16])
}

func normalizeEvidence(evidence RunEvidence) ([]byte, error) {
	// Normalize event order and omit the already-derived Ref fields so repeated
	// discovery has one stable content representation.
	events := append([]EvidenceEvent(nil), evidence.Events...)
	sort.Slice(events, func(i, j int) bool {
		if events[i].Timestamp.Equal(events[j].Timestamp) {
			return events[i].ID < events[j].ID
		}
		return events[i].Timestamp.Before(events[j].Timestamp)
	})
	controls := append([]string(nil), evidence.PolicyControls...)
	sort.Strings(controls)
	controls = compactStrings(controls)
	normalized := struct {
		ProjectID        string              `json:"projectId"`
		Runtime          string              `json:"runtime"`
		NativeRunID      string              `json:"nativeRunId"`
		CreatedAt        time.Time           `json:"createdAt"`
		Status           string              `json:"status"`
		UpdatedAt        time.Time           `json:"updatedAt"`
		SummaryText      string              `json:"summary,omitempty"`
		Decisions        []string            `json:"decisions,omitempty"`
		Discoveries      []string            `json:"discoveries,omitempty"`
		Constraints      []string            `json:"constraints,omitempty"`
		KnownIssues      []string            `json:"knownIssues,omitempty"`
		Commands         []string            `json:"commands,omitempty"`
		FailedApproaches []string            `json:"failedApproaches,omitempty"`
		PolicyID         string              `json:"policyId,omitempty"`
		PolicyVersion    string              `json:"policyVersion,omitempty"`
		PolicyControls   []string            `json:"policyControls,omitempty"`
		Termination      string              `json:"termination,omitempty"`
		SourceDigest     string              `json:"sourceDigest,omitempty"`
		Events           []EvidenceEvent     `json:"events,omitempty"`
		Procedures       []ProcedureEvidence `json:"procedures,omitempty"`
	}{evidence.Ref.ProjectID, evidence.Ref.Runtime, evidence.Ref.NativeRunID, evidence.Ref.CreatedAt.UTC(), evidence.Status, evidence.UpdatedAt.UTC(), strings.TrimSpace(evidence.SummaryText), normalizeTextList(evidence.Decisions), normalizeTextList(evidence.Discoveries), normalizeTextList(evidence.Constraints), normalizeTextList(evidence.KnownIssues), normalizeTextList(evidence.Commands), normalizeTextList(evidence.FailedApproaches), evidence.PolicyID, evidence.PolicyVersion, controls, evidence.Termination, evidence.SourceDigest, events, normalizeProcedures(evidence.Procedures)}
	return json.Marshal(normalized)
}

func validateEvidenceBounds(evidence RunEvidence) error {
	if len(evidence.Events) > maxEvidenceEvents {
		return fmt.Errorf("%w: too many governance events", ErrInvalidEvidence)
	}
	if len(evidence.Procedures) > maxEvidenceItems {
		return fmt.Errorf("%w: too many procedure claims", ErrInvalidEvidence)
	}
	if len(evidence.PolicyControls) > maxEvidenceItems || len(evidence.PolicyID) > maxEvidenceFieldBytes ||
		len(evidence.PolicyVersion) > maxEvidenceFieldBytes || len(evidence.Termination) > maxEvidenceFieldBytes ||
		!utf8.ValidString(evidence.PolicyID) || !utf8.ValidString(evidence.PolicyVersion) || !utf8.ValidString(evidence.Termination) {
		return fmt.Errorf("%w: policy evidence exceeds its byte or item limit", ErrInvalidEvidence)
	}
	for _, control := range evidence.PolicyControls {
		if len(control) > maxEvidenceFieldBytes || !utf8.ValidString(control) {
			return fmt.Errorf("%w: policy control exceeds its byte limit or is not UTF-8", ErrInvalidEvidence)
		}
	}
	for _, event := range evidence.Events {
		for _, value := range []string{event.ID, event.Category, event.Decision, event.Result, event.Reason} {
			if len(value) > maxEvidenceFieldBytes || !utf8.ValidString(value) {
				return fmt.Errorf("%w: event evidence exceeds its byte limit or is not UTF-8", ErrInvalidEvidence)
			}
		}
	}
	for _, procedure := range evidence.Procedures {
		if len(procedure.Key) > maxEvidenceFieldBytes || len(procedure.Content) > maxEvidenceFieldBytes || len(procedure.Outcome) > 128 ||
			!utf8.ValidString(procedure.Key) || !utf8.ValidString(procedure.Content) || !utf8.ValidString(procedure.Outcome) {
			return fmt.Errorf("%w: procedure claim exceeds its byte limit or is not UTF-8", ErrInvalidEvidence)
		}
	}
	fields := [][]string{evidence.Decisions, evidence.Discoveries, evidence.Constraints, evidence.KnownIssues, evidence.Commands, evidence.FailedApproaches}
	count := 0
	for _, values := range fields {
		count += len(values)
		if len(values) > maxEvidenceItems {
			return fmt.Errorf("%w: too many values in one evidence field", ErrInvalidEvidence)
		}
		for _, value := range values {
			if len(value) > maxEvidenceFieldBytes || !utf8.ValidString(value) {
				return fmt.Errorf("%w: evidence field exceeds its byte limit or is not UTF-8", ErrInvalidEvidence)
			}
		}
	}
	if count > maxEvidenceItems || len(evidence.SummaryText) > maxEvidenceFieldBytes || !utf8.ValidString(evidence.SummaryText) ||
		(evidence.SourceDigest != "" && !contentHashPattern.MatchString(evidence.SourceDigest)) {
		return fmt.Errorf("%w: evidence exceeds its item or byte limit", ErrInvalidEvidence)
	}
	return nil
}

func normalizeProcedures(values []ProcedureEvidence) []ProcedureEvidence {
	if len(values) == 0 {
		return nil
	}
	result := append([]ProcedureEvidence(nil), values...)
	for i := range result {
		result[i].Key = strings.ToLower(strings.TrimSpace(result[i].Key))
		result[i].Content = strings.TrimSpace(result[i].Content)
		result[i].Outcome = strings.ToLower(strings.TrimSpace(result[i].Outcome))
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Key != result[j].Key {
			return result[i].Key < result[j].Key
		}
		if result[i].Content != result[j].Content {
			return result[i].Content < result[j].Content
		}
		if result[i].Outcome != result[j].Outcome {
			return result[i].Outcome < result[j].Outcome
		}
		return !result[i].Generalizable && result[j].Generalizable
	})
	return result
}

func normalizeTextList(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	result := make([]string, len(values))
	for i, value := range values {
		result[i] = strings.TrimSpace(value)
	}
	return result
}

func terminal(status run.Status) bool {
	return status == run.Completed || status == run.Failed || status == run.Terminated
}

func samePath(a, b string) bool {
	if !filepath.IsAbs(a) || !filepath.IsAbs(b) {
		return false
	}
	return filepath.Clean(a) == filepath.Clean(b)
}

func compactStrings(values []string) []string {
	if len(values) == 0 {
		return values
	}
	out := values[:1]
	for _, value := range values[1:] {
		if value != out[len(out)-1] {
			out = append(out, value)
		}
	}
	return out
}

func contextErr(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("%w: context is required", ErrInvalidEvidence)
	}
	return ctx.Err()
}
