// Package run stores AgentRun governance state and its local audit trail.
// It never starts an agent process; runtime control is supplied explicitly by
// an adapter or test controller.
package run

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/AllenMuu/skill-manager/internal/enforcement"
	"github.com/AllenMuu/skill-manager/internal/identity"
	"github.com/AllenMuu/skill-manager/internal/policy"
)

const (
	storeVersion         = "v3"
	previousStoreVersion = "v2"
	legacyStoreVersion   = "v1"
	auditVersion         = "v3"
	previousAuditVersion = "v2"
	legacyAuditVersion   = "v1"
)
const maxStoreSize = 32 << 20

const (
	TerminationOperatorRequested = "operator_requested"
	TerminationPolicyViolation   = "policy_violation"
	TerminationBudgetExhausted   = "budget_exhausted"
	TerminationUnexpected        = "unexpected_termination"
	TerminationRuntimeFailure    = "runtime_failure"
)

type Status string

const (
	Active     Status = "active"
	Pending    Status = "pending"
	Prepared   Status = "prepared"
	Paused     Status = "paused"
	Completed  Status = "completed"
	Failed     Status = "failed"
	Terminated Status = "terminated"
)

type Record struct {
	PolicyRevisions    *PolicyRevisionHistory `json:"policy_revisions,omitempty"`
	ExternalRuntime    *ExternalRuntime       `json:"external_runtime,omitempty"`
	ID                 string                 `json:"id"`
	ProjectRoot        string                 `json:"project_root"`
	Runtime            string                 `json:"runtime"`
	Status             Status                 `json:"status"`
	Policy             policy.Snapshot        `json:"policy"`
	Identity           identity.Selection     `json:"identity"`
	CapabilityWarnings []policy.Control       `json:"capability_warnings,omitempty"`
	CreatedAt          time.Time              `json:"created_at"`
	UpdatedAt          time.Time              `json:"updated_at"`
	TerminationReason  string                 `json:"termination_reason,omitempty"`
}

type ApprovalStatus string

const (
	ApprovalPending  ApprovalStatus = "pending"
	ApprovalApproved ApprovalStatus = "approved"
	ApprovalRejected ApprovalStatus = "rejected"
	ApprovalExpired  ApprovalStatus = "expired"
)

type InvocationStatus string

const (
	InvocationSucceeded InvocationStatus = "succeeded"
	InvocationFailed    InvocationStatus = "failed"
	InvocationBlocked   InvocationStatus = "blocked"
)

type Approval struct {
	ID                string                  `json:"id"`
	RunID             string                  `json:"run_id"`
	RequestAuditID    string                  `json:"request_audit_id,omitempty"`
	Category          policy.EventCategory    `json:"category"`
	ActionType        string                  `json:"action_type"`
	Tool              string                  `json:"tool,omitempty"`
	Domain            string                  `json:"domain,omitempty"`
	ReasonCode        policy.ReasonCode       `json:"reason_code"`
	Status            ApprovalStatus          `json:"status"`
	RequestedAt       time.Time               `json:"requested_at"`
	DecidedAt         time.Time               `json:"decided_at,omitempty"`
	DecisionReason    string                  `json:"decision_reason,omitempty"`
	DecidedBy         *identity.ActorIdentity `json:"decided_by,omitempty"`
	ConsumedAt        time.Time               `json:"consumed_at,omitempty"`
	ConsumedByAuditID string                  `json:"consumed_by_audit_id,omitempty"`
}

// AuditRecord deliberately has no arbitrary metadata or event detail field.
// Persist only the normalized, validated fields needed for governance; raw
// credential/token values and runtime error strings are never copied here.
type AuditRecord struct {
	Version           string               `json:"version"`
	ID                string               `json:"id"`
	RunID             string               `json:"run_id"`
	RequestAuditID    string               `json:"request_audit_id,omitempty"`
	Timestamp         time.Time            `json:"timestamp"`
	Category          policy.EventCategory `json:"category"`
	Actor             string               `json:"actor,omitempty"`
	ActorID           string               `json:"actor_id,omitempty"`
	DelegationID      string               `json:"delegation_id,omitempty"`
	ActionID          string               `json:"action_id,omitempty"`
	ApprovalID        string               `json:"approval_id,omitempty"`
	ApproverID        string               `json:"approver_id,omitempty"`
	TraceID           string               `json:"trace_id,omitempty"`
	Runtime           string               `json:"runtime"`
	Tool              string               `json:"tool,omitempty"`
	ActionType        string               `json:"action_type,omitempty"`
	Domain            string               `json:"domain,omitempty"`
	CredentialScope   string               `json:"credential_scope,omitempty"`
	PolicyID          string               `json:"policy_id"`
	PolicyVersion     string               `json:"policy_version"`
	PolicyHash        string               `json:"policy_hash"`
	PolicyResolvedAt  time.Time            `json:"policy_resolved_at"`
	Decision          policy.Outcome       `json:"decision,omitempty"`
	Result            policy.Outcome       `json:"result,omitempty"`
	ReasonCode        policy.ReasonCode    `json:"reason_code,omitempty"`
	TerminationReason string               `json:"termination_reason,omitempty"`
}

type database struct {
	Version   string                        `json:"version"`
	Runs      map[string]Record             `json:"runs"`
	Approvals map[string]Approval           `json:"approvals"`
	Events    []AuditRecord                 `json:"events"`
	Proposals map[string]PermissionProposal `json:"permission_proposals,omitempty"`
}

type Store struct {
	root string
	mu   sync.Mutex
}

var safeID = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,127}$`)
var safeLabel = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$`)
var policyHashPattern = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)

func DefaultStore() (*Store, error) {
	base, err := os.UserConfigDir()
	if err != nil || strings.TrimSpace(base) == "" {
		if err == nil {
			err = errors.New("user config directory is empty")
		}
		return nil, fmt.Errorf("resolve user config directory: %w", err)
	}
	return NewStore(filepath.Join(base, "agent-manager", "runs"))
}

// NewStore creates a store at root. Tests and embedders may supply a local
// directory; the default location is user-level and shared by all projects.
func NewStore(root string) (*Store, error) {
	if strings.TrimSpace(root) == "" {
		return nil, errors.New("run store root is required")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve run store root: %w", err)
	}
	return &Store{root: abs}, nil
}

func (s *Store) Root() string { return s.root }

func (s *Store) Start(snapshot policy.Snapshot, runtime, project string, identitySelection identity.Selection, capabilities map[policy.Control]bool, now time.Time) (Record, policy.CapabilityReport, error) {
	return s.create(snapshot, runtime, project, identitySelection, capabilities, now, nil)
}

func (s *Store) create(snapshot policy.Snapshot, runtime, project string, identitySelection identity.Selection, capabilities map[policy.Control]bool, now time.Time, external *ExternalRuntime) (Record, policy.CapabilityReport, error) {
	if err := snapshot.Validate(); err != nil {
		return Record{}, policy.CapabilityReport{}, err
	}
	if identitySelection.Mode != identity.ModeNamed && identitySelection.Mode != identity.ModeAnonymous {
		return Record{}, policy.CapabilityReport{}, errors.New("new runs require an explicit named or anonymous identity mode")
	}
	if err := identitySelection.Validate(); err != nil {
		return Record{}, policy.CapabilityReport{}, fmt.Errorf("validate run identity: %w", err)
	}
	if strings.TrimSpace(runtime) == "" {
		return Record{}, policy.CapabilityReport{}, errors.New("runtime is required")
	}
	if !safeLabel.MatchString(runtime) {
		return Record{}, policy.CapabilityReport{}, fmt.Errorf("invalid runtime label %q", runtime)
	}
	if !filepath.IsAbs(project) {
		abs, err := filepath.Abs(project)
		if err != nil {
			return Record{}, policy.CapabilityReport{}, fmt.Errorf("resolve project root: %w", err)
		}
		project = abs
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if identitySelection.Mode == identity.ModeNamed && !now.Before(identitySelection.Delegation.ExpiresAt) {
		return Record{}, policy.CapabilityReport{}, errors.New("delegation is expired at run creation")
	}
	report := policy.CheckCapabilities(snapshot.Policy, capabilities)
	if !report.Ready {
		return Record{}, report, &CapabilityError{Report: report}
	}
	id, err := newID("run")
	if err != nil {
		return Record{}, report, err
	}
	identitySnapshot := identitySelection
	if identitySelection.Mode == identity.ModeNamed {
		actor := identitySelection.Actor.Normalized()
		delegation, err := identitySelection.Delegation.Bind(id, actor)
		if err != nil {
			return Record{}, report, fmt.Errorf("bind delegation to run: %w", err)
		}
		identitySnapshot = identity.NamedSelection(actor, delegation)
	}
	record := Record{ID: id, ProjectRoot: project, Runtime: runtime, Status: Active, Policy: snapshot, Identity: identitySnapshot, CapabilityWarnings: append([]policy.Control(nil), report.Warnings...), CreatedAt: now.UTC(), UpdatedAt: now.UTC()}
	if external != nil {
		record.Status = Pending
		record.ExternalRuntime = external
		external.Operations[0].Request = lifecycleRequest(record, external.Operations[0].ID, "prepare")
	}
	err = s.update(func(db *database) error {
		if _, exists := db.Runs[id]; exists {
			return fmt.Errorf("run %q already exists", id)
		}
		db.Runs[id] = record
		if external != nil {
			return nil
		}
		event := policy.Event{Category: policy.AgentRunStarted, RunID: id, Timestamp: now.UTC()}
		audit, err := auditFor(record, event, policy.Decision{Outcome: policy.Allow}, "")
		if err != nil {
			return err
		}
		db.Events = append(db.Events, audit)
		return nil
	})
	return record, report, err
}

func (s *Store) Get(id string) (Record, error) {
	if err := validateID("run", id); err != nil {
		return Record{}, err
	}
	db, err := s.read()
	if err != nil {
		return Record{}, err
	}
	record, ok := db.Runs[id]
	if !ok {
		return Record{}, fmt.Errorf("run %q not found", id)
	}
	return record, nil
}

func (s *Store) List() ([]Record, error) {
	db, err := s.read()
	if err != nil {
		return nil, err
	}
	result := make([]Record, 0, len(db.Runs))
	for _, record := range db.Runs {
		result = append(result, record)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].CreatedAt.Equal(result[j].CreatedAt) {
			return result[i].ID < result[j].ID
		}
		return result[i].CreatedAt.Before(result[j].CreatedAt)
	})
	return result, nil
}

func (s *Store) appendEvent(runID string, event policy.Event, decision policy.Decision, invocationStatus InvocationStatus, now time.Time, expected policy.Snapshot) (AuditRecord, error) {
	if err := event.Validate(); err != nil {
		return AuditRecord{}, err
	}
	if err := decision.Validate(); err != nil {
		return AuditRecord{}, fmt.Errorf("invalid policy decision: %w", err)
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	event.RunID, event.Timestamp = runID, now.UTC()
	var result AuditRecord
	err := s.update(func(db *database) error {
		record, ok := db.Runs[runID]
		if !ok {
			return fmt.Errorf("run %q not found", runID)
		}
		if event.RequestAuditID != "" {
			if err := validateRequestAuditLink(db.Events, runID, event, true); err != nil {
				return err
			}
		}
		var err error
		snapshot := record.AppliedPolicy()
		if event.RequestAuditID == "" && !samePolicySnapshot(snapshot, expected) {
			return ErrProposalStaleBase
		}
		if (event.Category == policy.ToolCallRequested || event.Category == policy.NetworkAccessRequested || event.Category == policy.CredentialAccessRequested) && record.policyRevisionBlocked() {
			return enforcement.ErrUnknown
		}
		if event.RequestAuditID != "" {
			request, found := auditRecordByID(db.Events, event.RequestAuditID)
			if !found {
				return errors.New("request audit not found")
			}
			var resolved bool
			snapshot, resolved = record.policySnapshot(request.PolicyHash, request.PolicyResolvedAt)
			if !resolved {
				return errors.New("request revision not found")
			}
		}
		record.Policy = snapshot
		record.PolicyRevisions = nil
		result, err = auditFor(record, event, decision, "")
		if err == nil {
			if invocationStatus != "" {
				result.Result = policy.Outcome(invocationStatus)
			}
			db.Events = append(db.Events, result)
		}
		return err
	})
	return result, err
}

func (s *Store) Events(runID string) ([]AuditRecord, error) {
	if runID != "" {
		if err := validateID("run", runID); err != nil {
			return nil, err
		}
	}
	db, err := s.read()
	if err != nil {
		return nil, err
	}
	result := make([]AuditRecord, 0)
	for _, event := range db.Events {
		if runID == "" || event.RunID == runID {
			result = append(result, event)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Timestamp.Equal(result[j].Timestamp) {
			return result[i].ID < result[j].ID
		}
		return result[i].Timestamp.Before(result[j].Timestamp)
	})
	return result, nil
}

func (s *Store) CreateApproval(request Approval, now time.Time) (Approval, error) {
	if request.RequestAuditID == "" {
		return Approval{}, errors.New("approval request must reference its persisted REQUIRE_APPROVAL audit")
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if request.ID == "" {
		id, err := newID("approval")
		if err != nil {
			return Approval{}, err
		}
		request.ID = id
	}
	if request.Category == "" {
		if request.Tool != "" {
			request.Category = policy.ToolCallRequested
		} else {
			request.Category = policy.EventPolicyViolation
		}
	}
	request.Status, request.RequestedAt = ApprovalPending, now.UTC()
	if err := validateApproval(request); err != nil {
		return Approval{}, err
	}
	err := s.update(func(db *database) error {
		run, ok := db.Runs[request.RunID]
		if !ok {
			return fmt.Errorf("run %q not found", request.RunID)
		}
		if run.Status != Paused {
			return fmt.Errorf("run %q must be paused before an approval request is persisted", run.ID)
		}
		requestAudit, err := validateApprovalRequestAudit(*db, request)
		if err != nil {
			return err
		}
		if _, exists := db.Approvals[request.ID]; exists {
			return fmt.Errorf("approval %q already exists", request.ID)
		}
		for _, existing := range db.Approvals {
			if existing.RunID == request.RunID && existing.RequestAuditID == request.RequestAuditID {
				return fmt.Errorf("request audit %q already has an approval", request.RequestAuditID)
			}
		}
		db.Approvals[request.ID] = request
		event := policy.Event{Category: request.Category, RunID: run.ID, Tool: request.Tool, Domain: request.Domain, ActionType: request.ActionType, ActionID: requestAudit.ActionID, TraceID: requestAudit.TraceID, ApprovalID: request.ID, Timestamp: now.UTC()}
		audit, err := auditFor(run, event, policy.Decision{Outcome: policy.RequireApproval, ReasonCode: request.ReasonCode}, "")
		if err != nil {
			return err
		}
		db.Events = append(db.Events, audit)
		return nil
	})
	return request, err
}

func (s *Store) GetApproval(id string) (Approval, error) {
	if err := validateID("approval", id); err != nil {
		return Approval{}, err
	}
	db, err := s.read()
	if err != nil {
		return Approval{}, err
	}
	request, ok := db.Approvals[id]
	if !ok {
		return Approval{}, fmt.Errorf("approval %q not found", id)
	}
	return request, nil
}

// ConsumeApprovalForInvocation atomically binds an approved request to one
// persisted invocation attempt. This prevents the same approval from authorizing
// concurrent or later calls.
func (s *Store) ConsumeApprovalForInvocation(approvalID, runID, requestAuditID string, now time.Time) (Approval, error) {
	if err := validateID("approval", approvalID); err != nil {
		return Approval{}, err
	}
	if err := validateID("run", runID); err != nil {
		return Approval{}, err
	}
	if err := validateID("audit", requestAuditID); err != nil {
		return Approval{}, err
	}
	if now.IsZero() {
		return Approval{}, errors.New("approval consumption requires a timestamp")
	}
	var consumed Approval
	err := s.update(func(db *database) error {
		approval, ok := db.Approvals[approvalID]
		if !ok {
			return fmt.Errorf("approval %q not found", approvalID)
		}
		if approval.RunID != runID || approval.Status != ApprovalApproved || approval.DecidedBy == nil {
			return errors.New("invocation approval is not approved for this run")
		}
		if !approval.ConsumedAt.IsZero() || approval.ConsumedByAuditID != "" {
			return fmt.Errorf("approval %q has already been consumed", approvalID)
		}
		runRecord, ok := db.Runs[runID]
		if !ok || !runRecord.ExecutionReady() {
			return fmt.Errorf("run %q is not active for invocation", runID)
		}
		approvedRequest, err := validateApprovalRequestAudit(*db, approval)
		if err != nil {
			return err
		}
		var invocationRequest AuditRecord
		found := false
		for _, event := range db.Events {
			if event.ID == requestAuditID {
				invocationRequest, found = event, true
				break
			}
		}
		if !found || invocationRequest.RunID != runID || invocationRequest.Category != policy.ToolCallRequested || invocationRequest.Decision != policy.RequireApproval || invocationRequest.ApprovalID != approvalID || invocationRequest.ActionID != approvedRequest.ActionID || invocationRequest.Tool != approvedRequest.Tool || invocationRequest.ActionType != approvedRequest.ActionType || invocationRequest.TraceID != approvedRequest.TraceID || invocationRequest.PolicyHash != runRecord.AppliedPolicy().Hash || !sameAuditPolicyReference(invocationRequest, approvedRequest) {
			return errors.New("invocation audit does not match the approved action")
		}
		approval.ConsumedAt = now.UTC()
		approval.ConsumedByAuditID = requestAuditID
		db.Approvals[approvalID] = approval
		consumed = approval
		return nil
	})
	return consumed, err
}

// ReleaseApprovalConsumption makes an authorization available again only when
// the invocation adapter was never called and no completion was recorded.
func (s *Store) ReleaseApprovalConsumption(approvalID, runID, requestAuditID string) error {
	if err := validateID("approval", approvalID); err != nil {
		return err
	}
	if err := validateID("run", runID); err != nil {
		return err
	}
	if err := validateID("audit", requestAuditID); err != nil {
		return err
	}
	return s.update(func(db *database) error {
		approval, ok := db.Approvals[approvalID]
		if !ok || approval.RunID != runID || approval.Status != ApprovalApproved {
			return errors.New("invocation approval cannot be released for this run")
		}
		if approval.ConsumedAt.IsZero() || approval.ConsumedByAuditID != requestAuditID {
			return errors.New("invocation approval is not consumed by this request")
		}
		for _, event := range db.Events {
			if (event.Category == policy.ToolCallCompleted || event.Category == policy.NetworkAccessCompleted) && event.RequestAuditID == requestAuditID {
				return errors.New("invocation approval cannot be released after a completion was recorded")
			}
		}
		approval.ConsumedAt = time.Time{}
		approval.ConsumedByAuditID = ""
		db.Approvals[approvalID] = approval
		return nil
	})
}

func (s *Store) ListApprovals(runID string) ([]Approval, error) {
	db, err := s.read()
	if err != nil {
		return nil, err
	}
	result := make([]Approval, 0)
	for _, request := range db.Approvals {
		if runID == "" || request.RunID == runID {
			result = append(result, request)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].RequestedAt.Equal(result[j].RequestedAt) {
			return result[i].ID < result[j].ID
		}
		return result[i].RequestedAt.Before(result[j].RequestedAt)
	})
	return result, nil
}

func (s *Store) requestAuditForApproval(request Approval) (AuditRecord, error) {
	db, err := s.read()
	if err != nil {
		return AuditRecord{}, err
	}
	return validateApprovalRequestAudit(db, request)
}

func (s *Store) approvalForRequest(runID, requestAuditID string) (Approval, bool, error) {
	db, err := s.read()
	if err != nil {
		return Approval{}, false, err
	}
	var result Approval
	found := false
	for _, request := range db.Approvals {
		if request.RunID != runID || request.RequestAuditID != requestAuditID {
			continue
		}
		if found {
			return Approval{}, false, fmt.Errorf("request audit %q has multiple approvals", requestAuditID)
		}
		result, found = request, true
	}
	return result, found, nil
}

func (s *Store) DecideApproval(id string, status ApprovalStatus, reason string, decider identity.ActorIdentity, now time.Time) (Approval, error) {
	if status != ApprovalApproved && status != ApprovalRejected && status != ApprovalExpired {
		return Approval{}, fmt.Errorf("invalid terminal approval status %q", status)
	}
	if err := decider.Validate(); err != nil {
		return Approval{}, fmt.Errorf("validate approval decision actor: %w", err)
	}
	decider = decider.Normalized()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	var result Approval
	err := s.update(func(db *database) error {
		request, ok := db.Approvals[id]
		if !ok {
			return fmt.Errorf("approval %q not found", id)
		}
		if request.Status != ApprovalPending {
			return fmt.Errorf("approval %q is already %s", id, request.Status)
		}
		run := db.Runs[request.RunID]
		deciderSnapshot := decider.Normalized()
		request.Status, request.DecidedAt, request.DecisionReason, request.DecidedBy = status, now.UTC(), sanitizeReason(reason), &deciderSnapshot
		db.Approvals[id] = request
		category := policy.EventPolicyViolation
		if status == ApprovalExpired {
			category = policy.EventPolicyViolation
		}
		requestAudit, err := validateApprovalRequestAudit(*db, request)
		if err != nil {
			return err
		}
		event := policy.Event{Category: category, RunID: run.ID, Tool: request.Tool, Domain: request.Domain, ActionType: request.ActionType, ActionID: requestAudit.ActionID, TraceID: requestAudit.TraceID, ApprovalID: request.ID, ApproverID: decider.ID, Timestamp: now.UTC()}
		run.Policy = snapshotForAudit(run, requestAudit)
		run.PolicyRevisions = nil
		audit, err := auditFor(run, event, policy.Decision{Outcome: outcomeFor(status), ReasonCode: request.ReasonCode}, "approval_"+string(status))
		if err != nil {
			return err
		}
		db.Events = append(db.Events, audit)
		result = request
		return nil
	})
	return result, err
}

func (s *Store) confirmTermination(id, reason string, now time.Time) (Record, error) {
	if !validTerminationReason(reason) {
		return Record{}, errors.New("termination reason must be a supported reason code")
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	var result Record
	err := s.update(func(db *database) error {
		record, ok := db.Runs[id]
		if !ok {
			return fmt.Errorf("run %q not found", id)
		}
		if record.ExternalRuntime != nil {
			return errors.New("external termination requires correlated lifecycle acknowledgement")
		}
		if record.Status != Active && record.Status != Paused {
			return fmt.Errorf("run %q is %s and cannot be terminated", id, record.Status)
		}
		record.Status, record.UpdatedAt, record.TerminationReason = Terminated, now.UTC(), sanitizeReason(reason)
		db.Runs[id] = record
		event := policy.Event{Category: policy.AgentRunTerminated, RunID: id, Timestamp: now.UTC()}
		audit, err := auditFor(record, event, policy.Decision{Outcome: policy.Allow}, record.TerminationReason)
		if err != nil {
			return err
		}
		db.Events = append(db.Events, audit)
		result = record
		return nil
	})
	return result, err
}

func (s *Store) ConfirmApprovalResolution(runID string, now time.Time) error {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	return s.update(func(db *database) error {
		record, ok := db.Runs[runID]
		if !ok {
			return fmt.Errorf("run %q not found", runID)
		}
		if record.ExternalRuntime != nil {
			return errors.New("external resume requires correlated lifecycle acknowledgement")
		}
		if record.Status != Paused {
			return fmt.Errorf("run %q is %s, expected paused", runID, record.Status)
		}
		record.Status, record.UpdatedAt = Active, now.UTC()
		db.Runs[runID] = record
		return nil
	})
}

func (s *Store) confirmPause(runID string, now time.Time) error {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	return s.update(func(db *database) error {
		record, ok := db.Runs[runID]
		if !ok {
			return fmt.Errorf("run %q not found", runID)
		}
		if record.ExternalRuntime != nil {
			return errors.New("external pause requires correlated lifecycle acknowledgement")
		}
		if record.Status != Active {
			return fmt.Errorf("run %q is %s and cannot be paused", runID, record.Status)
		}
		record.Status, record.UpdatedAt = Paused, now.UTC()
		db.Runs[runID] = record
		return nil
	})
}

type CapabilityError struct{ Report policy.CapabilityReport }

func (e *CapabilityError) Error() string {
	return fmt.Sprintf("runtime is missing mandatory governance capabilities: %s", joinControls(e.Report.Missing))
}

// RuntimeController must confirm control operations. Returning confirmed=false
// never updates persisted run termination or releases a pending action.
type RuntimeController interface {
	Capabilities() map[policy.Control]bool
	PauseForApproval(context.Context, string, string) (bool, error)
	ResolveApproval(context.Context, string, string, bool) (bool, error)
	Kill(context.Context, string, string) (bool, error)
}

type Manager struct {
	Store       *Store
	coordinator *Coordinator
	controllers map[string]RuntimeController
	mu          sync.Mutex
}

func NewManager(store *Store) (*Manager, error) {
	if store == nil {
		return nil, errors.New("run store is required")
	}
	return &Manager{Store: store, controllers: map[string]RuntimeController{}}, nil
}

func (m *Manager) Start(snapshot policy.Snapshot, runtime, project string, identitySelection identity.Selection, now time.Time, controller RuntimeController) (Record, policy.CapabilityReport, error) {
	capabilities := map[policy.Control]bool{}
	if controller != nil {
		capabilities = controller.Capabilities()
	}
	record, report, err := m.Store.Start(snapshot, runtime, project, identitySelection, capabilities, now)
	if err != nil {
		return Record{}, report, err
	}
	m.mu.Lock()
	if controller != nil {
		m.controllers[record.ID] = controller
	}
	m.mu.Unlock()
	return record, report, nil
}

// EvaluateAndRecord evaluates one normalized governance event against the
// immutable snapshot for its run, then persists the engine result with that
// run and snapshot identity. Completed actions are evaluated as their
// corresponding request and recorded as violations when policy would deny
// them. The returned termination flag is a run-control request; callers must
// pass it through Kill and only report termination after controller confirmation.
func (m *Manager) EvaluateAndRecord(runID string, event policy.Event, state policy.BudgetState, now time.Time) (policy.Decision, policy.Evaluation, AuditRecord, error) {
	return m.evaluateAndRecord(runID, event, state, policy.IdentityRequirement{}, identity.Selection{}, "", now)
}

// EvaluateAndRecordWithIdentity applies the explicit action identity contract
// from a run snapshot before persisting a decision and its audit evidence.
func (m *Manager) EvaluateAndRecordWithIdentity(runID string, event policy.Event, state policy.BudgetState, requirement policy.IdentityRequirement, now time.Time) (policy.Decision, policy.Evaluation, AuditRecord, error) {
	runRecord, err := m.Store.Get(runID)
	if err != nil {
		return policy.Decision{}, policy.Evaluation{}, AuditRecord{}, err
	}
	return m.evaluateAndRecord(runID, event, state, requirement, runRecord.Identity, "", now)
}

// EvaluateAndRecordInvocation records whether a dispatched tool call succeeded,
// failed, or was blocked by adapter capability preflight.
func (m *Manager) EvaluateAndRecordInvocation(runID string, event policy.Event, state policy.BudgetState, status InvocationStatus, now time.Time) (policy.Decision, policy.Evaluation, AuditRecord, error) {
	if event.Category != policy.ToolCallCompleted && event.Category != policy.NetworkAccessCompleted {
		return policy.Decision{}, policy.Evaluation{}, AuditRecord{}, errors.New("invocation result requires a completed tool or network event")
	}
	if status != InvocationSucceeded && status != InvocationFailed && status != InvocationBlocked {
		return policy.Decision{}, policy.Evaluation{}, AuditRecord{}, errors.New("invocation result status is unsupported")
	}
	return m.evaluateAndRecord(runID, event, state, policy.IdentityRequirement{}, identity.Selection{}, status, now)
}

func (m *Manager) evaluateAndRecord(runID string, event policy.Event, state policy.BudgetState, requirement policy.IdentityRequirement, selection identity.Selection, invocationStatus InvocationStatus, now time.Time) (policy.Decision, policy.Evaluation, AuditRecord, error) {
	if err := event.Validate(); err != nil {
		return policy.Decision{}, policy.Evaluation{}, AuditRecord{}, err
	}
	runRecord, err := m.Store.Get(runID)
	if err != nil {
		return policy.Decision{}, policy.Evaluation{}, AuditRecord{}, err
	}
	if (event.Category == policy.ToolCallRequested || event.Category == policy.NetworkAccessRequested || event.Category == policy.CredentialAccessRequested) && ((runRecord.ExternalRuntime != nil && !runRecord.ExecutionReady()) || runRecord.policyRevisionBlocked()) {
		return policy.Decision{}, policy.Evaluation{}, AuditRecord{}, enforcement.ErrUnknown
	}
	event.RunID = runID
	engine := policy.Engine{}
	decisionEvent := event
	switch event.Category {
	case policy.ToolCallCompleted:
		decisionEvent.Category = policy.ToolCallRequested
	case policy.NetworkAccessCompleted:
		decisionEvent.Category = policy.NetworkAccessRequested
	}
	if event.Category == policy.ToolCallCompleted || event.Category == policy.NetworkAccessCompleted {
		if event.RequestAuditID == "" {
			return policy.Decision{}, policy.Evaluation{}, AuditRecord{}, errors.New("completion event must reference its persisted request audit")
		}
		requestAudit, err := m.requestAudit(runID, event)
		if err != nil {
			return policy.Decision{}, policy.Evaluation{}, AuditRecord{}, err
		}
		event.ObservedDecision, event.ReasonCode = requestAudit.Decision, requestAudit.ReasonCode
		if event.ActionID == "" {
			event.ActionID = requestAudit.ActionID
		}
		if event.TraceID == "" {
			event.TraceID = requestAudit.TraceID
		}
		if requestAudit.Decision == policy.RequireApproval {
			requestedApprovalID := event.ApprovalID
			if requestedApprovalID == "" {
				requestedApprovalID = requestAudit.ApprovalID
			}
			var approval Approval
			var found bool
			if requestedApprovalID != "" {
				approval, err = m.Store.GetApproval(requestedApprovalID)
				found = err == nil && approval.RunID == runID
			} else {
				approval, found, err = m.Store.approvalForRequest(runID, requestAudit.ID)
			}
			if err != nil {
				return policy.Decision{}, policy.Evaluation{}, AuditRecord{}, err
			}
			// A completion is evidence that the action ran, even if its approval
			// is absent, pending, rejected, expired, or unrelated. Only a matching
			// approved request changes the recorded policy decision to ALLOW.
			event.ApprovalID, event.ApproverID = "", ""
			if found && approval.Status == ApprovalApproved &&
				(requestedApprovalID == "" || requestedApprovalID == approval.ID) &&
				approval.DecidedBy != nil {
				approvedRequest, err := m.Store.requestAuditForApproval(approval)
				if err != nil {
					return policy.Decision{}, policy.Evaluation{}, AuditRecord{}, err
				}
				if approvedRequest.ActionID == requestAudit.ActionID && approvedRequest.Tool == requestAudit.Tool &&
					approvedRequest.ActionType == requestAudit.ActionType && approvedRequest.TraceID == requestAudit.TraceID &&
					sameAuditDomain(approvedRequest.Domain, requestAudit.Domain) {
					event.ObservedDecision, event.ReasonCode = policy.Allow, ""
					event.ApprovalID = approval.ID
					event.ApproverID = approval.DecidedBy.ID
				}
			}
		}
	}
	evaluation := engine.EvaluateAfter(runRecord.AppliedPolicy(), event, state)
	decision := engine.EvaluateBefore(runRecord.AppliedPolicy(), decisionEvent, state)
	if requirement.Required {
		if event.ActionID != requirement.ActionID {
			decision = policy.Decision{Outcome: policy.Deny, ReasonCode: policy.ReasonIdentityPolicyDenied}
		} else {
			decision = engine.EvaluateBeforeWithIdentity(runRecord.AppliedPolicy(), decisionEvent, state, requirement, runID, selection, now)
		}
	}
	if event.Category == policy.ToolCallCompleted || event.Category == policy.NetworkAccessCompleted {
		decision = policy.Decision{Outcome: event.ObservedDecision, ReasonCode: event.ReasonCode}
	}
	if event.Category == policy.EventUnexpectedTermination {
		decision = policy.Decision{Outcome: policy.Deny, ReasonCode: policy.ReasonUnexpectedTermination}
	}
	decision.TerminationRequested = decision.TerminationRequested || evaluation.TerminationRequested
	if err := decision.Validate(); err != nil {
		return policy.Decision{}, policy.Evaluation{}, AuditRecord{}, err
	}
	event.ObservedDecision = decision.Outcome
	event.ReasonCode = decision.ReasonCode
	audit, err := m.Store.appendEvent(runID, event, decision, invocationStatus, now, runRecord.AppliedPolicy())
	if err != nil {
		return policy.Decision{}, policy.Evaluation{}, AuditRecord{}, err
	}
	return decision, evaluation, audit, nil
}

func (m *Manager) requestAudit(runID string, completion policy.Event) (AuditRecord, error) {
	events, err := m.Store.Events(runID)
	if err != nil {
		return AuditRecord{}, err
	}
	for _, event := range events {
		if event.ID != completion.RequestAuditID {
			continue
		}
		wantCategory := policy.ToolCallRequested
		if completion.Category == policy.NetworkAccessCompleted {
			wantCategory = policy.NetworkAccessRequested
		}
		if event.Category != wantCategory || event.Tool != completion.Tool || !sameAuditDomain(event.Domain, completion.Domain) || event.ActionType != completion.ActionType || (completion.ActionID != "" && event.ActionID != completion.ActionID) || (completion.TraceID != "" && event.TraceID != completion.TraceID) {
			return AuditRecord{}, errors.New("completion event request audit does not match its action")
		}
		return event, nil
	}
	return AuditRecord{}, fmt.Errorf("request audit %q was not found for run %q", completion.RequestAuditID, runID)
}

func (m *Manager) RequestApproval(ctx context.Context, request Approval, now time.Time) (Approval, error) {
	run, err := m.Store.Get(request.RunID)
	if err != nil {
		return Approval{}, err
	}
	if run.Status != Active {
		return Approval{}, fmt.Errorf("run %q is %s and cannot request approval", run.ID, run.Status)
	}
	if request.Category == "" {
		if request.Tool != "" {
			request.Category = policy.ToolCallRequested
		} else {
			request.Category = policy.EventPolicyViolation
		}
	}
	requestEvent := policy.Event{Category: request.Category, Tool: request.Tool, Domain: request.Domain, ActionType: request.ActionType}
	if err := requestEvent.Validate(); err != nil {
		return Approval{}, err
	}
	requestAudit, err := m.Store.requestAuditForApproval(request)
	if err != nil {
		return Approval{}, err
	}
	if requestAudit.PolicyHash != run.AppliedPolicy().Hash || requestAudit.PolicyID != run.AppliedPolicy().PolicyID {
		return Approval{}, errors.New("approval request references an audit from a different policy snapshot")
	}
	if _, found, err := m.Store.approvalForRequest(run.ID, request.RequestAuditID); err != nil {
		return Approval{}, err
	} else if found {
		return Approval{}, fmt.Errorf("request audit %q already has an approval", request.RequestAuditID)
	}
	if request.ID == "" {
		generated, err := newID("approval")
		if err != nil {
			return Approval{}, err
		}
		request.ID = generated
	}
	if run.policyRevisionBlocked() {
		return Approval{}, enforcement.ErrUnknown
	}
	controller := m.controller(run.ID)
	if controller == nil || !controller.Capabilities()[policy.ControlApprovalPauseResume] {
		return Approval{}, errors.New("runtime does not support safe approval pause/resume; action was not approved")
	}
	confirmed, err := controller.PauseForApproval(ctx, run.ID, request.ID)
	if err != nil {
		return Approval{}, fmt.Errorf("pause runtime for approval: %w", err)
	}
	if !confirmed {
		return Approval{}, errors.New("runtime did not confirm pause; action remains unapproved")
	}
	if run.ExternalRuntime == nil {
		if err := m.Store.confirmPause(run.ID, now); err != nil {
			return Approval{}, fmt.Errorf("runtime paused but run state could not be updated: %w", err)
		}
	}
	request, err = m.Store.CreateApproval(request, now)
	if err != nil {
		return Approval{}, err
	}
	return request, nil
}

func (m *Manager) DecideApproval(ctx context.Context, id string, status ApprovalStatus, reason string, decider identity.ActorIdentity, now time.Time) (Approval, error) {
	if err := decider.Validate(); err != nil {
		return Approval{}, fmt.Errorf("validate approval decision actor: %w", err)
	}
	request, err := m.Store.GetApproval(id)
	if err != nil {
		return Approval{}, err
	}
	if request.Status != ApprovalPending && request.Status != status {
		return Approval{}, fmt.Errorf("approval %q is already %s", id, request.Status)
	}
	run, err := m.Store.Get(request.RunID)
	if err != nil {
		return Approval{}, err
	}
	if request.Status == status && run.Status == Active {
		return request, nil
	}
	if run.policyRevisionBlocked() {
		return Approval{}, enforcement.ErrUnknown
	}
	controller := m.controller(run.ID)
	if controller == nil || !controller.Capabilities()[policy.ControlApprovalPauseResume] {
		return Approval{}, errors.New("runtime cannot resolve approval; request remains pending")
	}
	updated := request
	if request.Status == ApprovalPending {
		updated, err = m.Store.DecideApproval(id, status, reason, decider, now)
		if err != nil {
			return Approval{}, err
		}
	}
	confirmed, controlErr := controller.ResolveApproval(ctx, run.ID, id, status == ApprovalApproved)
	if controlErr != nil {
		return updated, fmt.Errorf("approval recorded as %s but runtime resolution failed: %w", status, controlErr)
	}
	if !confirmed {
		return updated, fmt.Errorf("approval recorded as %s but runtime did not confirm resolution", status)
	}
	if run.ExternalRuntime == nil {
		if err := m.Store.ConfirmApprovalResolution(run.ID, now); err != nil {
			return updated, fmt.Errorf("approval recorded as %s and runtime confirmed, but run state update failed: %w", status, err)
		}
	}
	return updated, nil
}

func (m *Manager) Kill(ctx context.Context, id, reason string, now time.Time) (Record, error) {
	if !validTerminationReason(reason) {
		return Record{}, errors.New("termination reason must be a supported reason code")
	}
	run, err := m.Store.Get(id)
	if err != nil {
		return Record{}, err
	}
	if run.Status != Active && run.Status != Paused {
		return Record{}, fmt.Errorf("run %q is %s and cannot be terminated", id, run.Status)
	}
	controller := m.controller(id)
	if controller == nil || !controller.Capabilities()[policy.ControlRunTermination] {
		return Record{}, errors.New("runtime does not support run termination")
	}
	confirmed, err := controller.Kill(ctx, id, reason)
	if err != nil {
		return Record{}, fmt.Errorf("request run termination: %w", err)
	}
	if !confirmed {
		return Record{}, errors.New("runtime did not confirm termination; run remains active")
	}
	if run.ExternalRuntime != nil {
		return m.Store.Get(id)
	}
	return m.Store.confirmTermination(id, reason, now)
}

func (m *Manager) controller(id string) RuntimeController {
	m.mu.Lock()
	defer m.mu.Unlock()
	record, err := m.Store.Get(id)
	if err == nil && record.ExternalRuntime != nil {
		if m.coordinator == nil {
			return nil
		}
		return lifecycleController{coordinator: m.coordinator}
	}
	return m.controllers[id]
}

func (s *Store) read() (database, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureRoot(false); err != nil {
		return database{}, err
	}
	path := filepath.Join(s.root, "state.json")
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return emptyDatabase(), nil
	}
	if err != nil {
		return database{}, fmt.Errorf("inspect run state: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return database{}, errors.New("run state must be a direct regular file")
	}
	if info.Size() > maxStoreSize {
		return database{}, fmt.Errorf("run state exceeds maximum size of %d bytes", maxStoreSize)
	}
	file, err := os.Open(path)
	if err != nil {
		return database{}, fmt.Errorf("open run state: %w", err)
	}
	defer file.Close()
	return decodeDatabase(file)
}

func (s *Store) update(change func(*database) error) (returnErr error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureRoot(true); err != nil {
		return err
	}
	release, err := acquireStoreLock(s.root)
	if err != nil {
		return err
	}
	defer func() {
		if err := release(); err != nil && returnErr == nil {
			returnErr = fmt.Errorf("release run state lock: %w", err)
		}
	}()
	db, err := s.readUnlocked()
	if err != nil {
		return err
	}
	if err := change(&db); err != nil {
		return err
	}
	db.Version = storeVersion
	if err := validateDatabase(db); err != nil {
		return fmt.Errorf("validate updated run state: %w", err)
	}
	contents, err := json.MarshalIndent(db, "", "  ")
	if err != nil {
		return fmt.Errorf("encode run state: %w", err)
	}
	if len(contents) > maxStoreSize {
		return fmt.Errorf("run state exceeds maximum size of %d bytes", maxStoreSize)
	}
	temp, err := os.CreateTemp(s.root, ".state-*.tmp")
	if err != nil {
		return fmt.Errorf("create run state staging file: %w", err)
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(0o600); err != nil {
		temp.Close()
		return fmt.Errorf("secure run state staging file: %w", err)
	}
	if _, err := temp.Write(contents); err != nil {
		temp.Close()
		return fmt.Errorf("write run state: %w", err)
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return fmt.Errorf("sync run state: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close run state: %w", err)
	}
	if err := os.Rename(tempPath, filepath.Join(s.root, "state.json")); err != nil {
		return fmt.Errorf("publish run state: %w", err)
	}
	return nil
}

func (s *Store) readUnlocked() (database, error) {
	path := filepath.Join(s.root, "state.json")
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return emptyDatabase(), nil
	}
	if err != nil {
		return database{}, fmt.Errorf("inspect run state: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return database{}, errors.New("run state must be a direct regular file")
	}
	if info.Size() > maxStoreSize {
		return database{}, fmt.Errorf("run state exceeds maximum size of %d bytes", maxStoreSize)
	}
	file, err := os.Open(path)
	if err != nil {
		return database{}, fmt.Errorf("open run state: %w", err)
	}
	defer file.Close()
	return decodeDatabase(file)
}

func decodeDatabase(input io.Reader) (database, error) {
	var db database
	decoder := json.NewDecoder(io.LimitReader(input, maxStoreSize+1))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&db); err != nil {
		return database{}, fmt.Errorf("decode run state: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return database{}, errors.New("run state must contain exactly one JSON document")
	}
	if (db.Version != legacyStoreVersion && db.Version != previousStoreVersion && db.Version != storeVersion) || db.Runs == nil || db.Approvals == nil || db.Events == nil {
		return database{}, errors.New("run state has an unsupported or incomplete version")
	}
	for _, record := range db.Runs {
		if db.Version != storeVersion && record.PolicyRevisions != nil {
			return database{}, errors.New("revision mutations require version-aware state")
		}
	}
	if db.Version == legacyStoreVersion {
		for id, record := range db.Runs {
			if record.Identity.Mode != "" || record.Identity.Actor != nil || record.Identity.Delegation != nil {
				return database{}, errors.New("v1 run state cannot contain typed identity")
			}
			record.Identity = identity.Selection{Mode: identity.ModeLegacyAnonymous}
			db.Runs[id] = record
		}
	} else {
		for _, record := range db.Runs {
			if record.Identity.Mode == "" {
				return database{}, errors.New("v2 run state is missing run identity mode")
			}
		}
	}
	if err := validateDatabase(db); err != nil {
		return database{}, fmt.Errorf("validate run state: %w", err)
	}
	return db, nil
}

func (s *Store) ensureRoot(create bool) error {
	if create {
		if err := os.MkdirAll(s.root, 0o700); err != nil {
			return fmt.Errorf("create run store: %w", err)
		}
	}
	info, err := os.Lstat(s.root)
	if os.IsNotExist(err) && !create {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect run store: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return errors.New("run store must be a direct directory")
	}
	if create {
		if err := os.Chmod(s.root, 0o700); err != nil {
			return fmt.Errorf("secure run store: %w", err)
		}
	}
	return nil
}

func emptyDatabase() database {
	return database{Version: storeVersion, Runs: map[string]Record{}, Approvals: map[string]Approval{}, Events: []AuditRecord{}}
}

func validateDatabase(db database) error {
	if err := validateRevisionLineage(db); err != nil {
		return err
	}
	if db.Version != legacyStoreVersion && db.Version != previousStoreVersion && db.Version != storeVersion {
		return errors.New("run state has an unsupported version")
	}
	for id, run := range db.Runs {
		if id != run.ID {
			return fmt.Errorf("run map key does not match record id %q", id)
		}
		if err := validateRecord(run); err != nil {
			return err
		}
	}
	for id, approval := range db.Approvals {
		if id != approval.ID {
			return fmt.Errorf("approval map key does not match request id %q", id)
		}
		if err := validateApproval(approval); err != nil {
			return err
		}
		if _, ok := db.Runs[approval.RunID]; !ok {
			return fmt.Errorf("approval %q references missing run", id)
		}
		if approval.RequestAuditID != "" {
			if _, err := validateApprovalRequestAudit(db, approval); err != nil {
				return fmt.Errorf("approval %q has invalid request audit: %w", id, err)
			}
		}
	}
	for _, event := range db.Events {
		if err := validateAudit(event, db); err != nil {
			return err
		}
		if event.RequestAuditID != "" {
			if err := validateRequestAuditLink(db.Events, event.RunID, policy.Event{Category: event.Category, Tool: event.Tool, Domain: event.Domain, ActionType: event.ActionType, ActionID: event.ActionID, TraceID: event.TraceID, Timestamp: event.Timestamp, RequestAuditID: event.RequestAuditID}, event.Version != legacyAuditVersion); err != nil {
				return fmt.Errorf("audit event %q has invalid request correlation: %w", event.ID, err)
			}
		}
	}
	for id, proposal := range db.Proposals {
		if id != proposal.ID {
			return errors.New("proposal key does not match id")
		}
		if err := validatePermissionProposal(db, proposal); err != nil {
			return err
		}
	}
	for _, approval := range db.Approvals {
		if err := validateApprovalConsumption(db, approval); err != nil {
			return fmt.Errorf("approval %q has invalid consumption: %w", approval.ID, err)
		}
	}
	return nil
}

func validateRecord(record Record) error {
	if err := validatePolicyRevisions(record); err != nil {
		return err
	}
	if err := validateExternal(record); err != nil {
		return err
	}
	if err := validateID("run", record.ID); err != nil {
		return err
	}
	if record.Runtime == "" || !safeLabel.MatchString(record.Runtime) {
		return fmt.Errorf("run %q has invalid runtime label", record.ID)
	}
	if record.Status != Pending && record.Status != Prepared && record.Status != Active && record.Status != Paused && record.Status != Completed && record.Status != Failed && record.Status != Terminated {
		return fmt.Errorf("run %q has invalid status %q", record.ID, record.Status)
	}
	if err := record.Policy.Validate(); err != nil {
		return fmt.Errorf("run %q has invalid policy snapshot: %w", record.ID, err)
	}
	if err := record.Identity.Validate(); err != nil {
		return fmt.Errorf("run %q has invalid identity snapshot: %w", record.ID, err)
	}
	switch record.Identity.Mode {
	case identity.ModeNamed:
		delegation := record.Identity.Delegation
		if record.Identity.Actor == nil || delegation == nil || delegation.RunID != record.ID {
			return fmt.Errorf("run %q has incomplete or mismatched identity lineage", record.ID)
		}
		if _, err := delegation.Bind(record.ID, *record.Identity.Actor); err != nil {
			return fmt.Errorf("run %q has invalid delegation lineage", record.ID)
		}
	case identity.ModeAnonymous, identity.ModeLegacyAnonymous:
	default:
		return fmt.Errorf("run %q has unsupported identity mode", record.ID)
	}
	if record.CreatedAt.IsZero() || record.UpdatedAt.IsZero() {
		return fmt.Errorf("run %q is missing timestamps", record.ID)
	}
	if record.Status == Terminated && record.TerminationReason == "" {
		return fmt.Errorf("terminated run %q lacks a termination reason", record.ID)
	}
	if record.TerminationReason != "" && !validTerminationReason(record.TerminationReason) {
		return fmt.Errorf("run %q has an unsupported termination reason", record.ID)
	}
	return nil
}

func validateApproval(approval Approval) error {
	if err := validateID("approval", approval.ID); err != nil {
		return err
	}
	if err := validateID("run", approval.RunID); err != nil {
		return err
	}
	if approval.RequestAuditID != "" {
		if err := validateID("audit", approval.RequestAuditID); err != nil {
			return err
		}
	}
	if !safeLabel.MatchString(approval.ActionType) {
		return fmt.Errorf("approval %q has invalid action type", approval.ID)
	}
	if approval.Tool != "" && !safeLabel.MatchString(approval.Tool) {
		return fmt.Errorf("approval %q has invalid tool", approval.ID)
	}
	if approval.ReasonCode == "" || approval.RequestedAt.IsZero() {
		return fmt.Errorf("approval %q is missing reason or request time", approval.ID)
	}
	if err := (policy.Event{Category: approval.Category, Tool: approval.Tool, Domain: approval.Domain, ActionType: approval.ActionType}).Validate(); err != nil {
		return fmt.Errorf("approval %q has an invalid action: %w", approval.ID, err)
	}
	if approval.Status != ApprovalPending && approval.Status != ApprovalApproved && approval.Status != ApprovalRejected && approval.Status != ApprovalExpired {
		return fmt.Errorf("approval %q has invalid status %q", approval.ID, approval.Status)
	}
	if approval.Status != ApprovalPending && approval.DecidedAt.IsZero() {
		return fmt.Errorf("terminal approval %q is missing decision time", approval.ID)
	}
	if approval.Status == ApprovalPending && approval.DecidedBy != nil {
		return fmt.Errorf("pending approval %q cannot have a deciding actor", approval.ID)
	}
	if approval.ConsumedAt.IsZero() != (approval.ConsumedByAuditID == "") {
		return fmt.Errorf("approval %q has incomplete consumption metadata", approval.ID)
	}
	if !approval.ConsumedAt.IsZero() {
		if approval.Status != ApprovalApproved || approval.DecidedBy == nil || approval.ConsumedAt.Before(approval.DecidedAt) {
			return fmt.Errorf("approval %q has invalid consumption state", approval.ID)
		}
		if err := validateID("audit", approval.ConsumedByAuditID); err != nil {
			return fmt.Errorf("approval %q has invalid consumption audit reference: %w", approval.ID, err)
		}
	}
	if approval.DecidedBy != nil {
		if err := approval.DecidedBy.Validate(); err != nil {
			return fmt.Errorf("approval %q has an invalid deciding actor: %w", approval.ID, err)
		}
	}
	return nil
}

func validateApprovalConsumption(db database, approval Approval) error {
	if approval.ConsumedAt.IsZero() {
		return nil
	}
	approvedRequest, err := validateApprovalRequestAudit(db, approval)
	if err != nil {
		return err
	}
	for _, event := range db.Events {
		if event.ID != approval.ConsumedByAuditID {
			continue
		}
		if event.RunID != approval.RunID || event.Category != policy.ToolCallRequested || event.Decision != policy.RequireApproval || event.ApprovalID != approval.ID || event.ActionID != approvedRequest.ActionID || event.Tool != approvedRequest.Tool || event.ActionType != approvedRequest.ActionType || event.TraceID != approvedRequest.TraceID {
			return errors.New("consumption audit does not match the approved action")
		}
		return nil
	}
	return fmt.Errorf("consumption audit %q was not found", approval.ConsumedByAuditID)
}

func validateApprovalRequestAudit(db database, request Approval) (AuditRecord, error) {
	if request.RequestAuditID == "" {
		return AuditRecord{}, errors.New("approval request must reference its persisted REQUIRE_APPROVAL audit")
	}
	runRecord, ok := db.Runs[request.RunID]
	if !ok {
		return AuditRecord{}, fmt.Errorf("run %q not found", request.RunID)
	}
	var audit AuditRecord
	found := false
	for _, event := range db.Events {
		if event.ID == request.RequestAuditID {
			audit, found = event, true
			break
		}
	}
	if !found {
		return AuditRecord{}, fmt.Errorf("request audit %q was not found for run %q", request.RequestAuditID, request.RunID)
	}
	if audit.RunID != request.RunID || audit.Category != request.Category || audit.Tool != request.Tool || !sameAuditDomain(audit.Domain, request.Domain) || audit.ActionType != request.ActionType {
		return AuditRecord{}, errors.New("approval request audit does not match its action")
	}
	if !auditResolvesPolicy(runRecord, audit) {
		return AuditRecord{}, errors.New("approval request audit does not match the run policy snapshot")
	}
	if audit.Decision != policy.RequireApproval {
		return AuditRecord{}, fmt.Errorf("approval request must reference a REQUIRE_APPROVAL audit; request recorded %s", audit.Decision)
	}
	if audit.ReasonCode != request.ReasonCode {
		return AuditRecord{}, fmt.Errorf("approval reason %s does not match request audit reason %s", request.ReasonCode, audit.ReasonCode)
	}
	return audit, nil
}

func validateAudit(event AuditRecord, db database) error {
	if err := event.Validate(); err != nil {
		return err
	}
	if !validAuditVersion(event.Version) || !safeID.MatchString(event.ID) {
		return fmt.Errorf("audit event has invalid version or id")
	}
	if _, ok := db.Runs[event.RunID]; !ok {
		return fmt.Errorf("audit event %q references missing run", event.ID)
	}
	if !policy.IsKnownEventCategory(event.Category) || event.Timestamp.IsZero() {
		return fmt.Errorf("audit event %q has invalid category or time", event.ID)
	}
	run := db.Runs[event.RunID]
	if event.Version != legacyAuditVersion {
		if event.TraceID == "" || event.Result == "" {
			return fmt.Errorf("audit event %q is missing result or trace correlation", event.ID)
		}
		if policy.IsKnownOutcome(event.Result) {
			if event.Result != event.Decision {
				return fmt.Errorf("audit event %q result does not match its policy decision", event.ID)
			}
		} else if !isInvocationResult(event.Result) || (event.Category != policy.ToolCallCompleted && event.Category != policy.NetworkAccessCompleted) {
			return fmt.Errorf("audit event %q has an invalid invocation result", event.ID)
		}
		if run.Identity.Mode == identity.ModeNamed {
			if run.Identity.Actor == nil || run.Identity.Delegation == nil || event.ActorID != run.Identity.Actor.ID || event.DelegationID != run.Identity.Delegation.ID {
				return fmt.Errorf("audit event %q identity references do not match run", event.ID)
			}
		} else if event.ActorID != "" || event.DelegationID != "" {
			return fmt.Errorf("audit event %q invents identity for a non-named run", event.ID)
		}
		if event.Version == auditVersion && (event.Category == policy.ToolCallCompleted || event.Category == policy.NetworkAccessCompleted) {
			request, found := auditRecordByID(db.Events, event.RequestAuditID)
			if !found {
				return fmt.Errorf("audit event %q completion has no linked request record", event.ID)
			}
			if request.Decision == policy.RequireApproval && event.Decision == policy.Allow && (event.ApprovalID == "" || event.ApproverID == "") {
				return fmt.Errorf("audit event %q approved completion is missing its approval or approver reference", event.ID)
			}
		}
		if (event.TerminationReason == "approval_approved" || event.TerminationReason == "approval_rejected" || event.TerminationReason == "approval_expired") && event.ApprovalID == "" {
			return fmt.Errorf("audit event %q has an approval transition without an approval reference", event.ID)
		}
		if event.ApprovalID != "" {
			approval, ok := db.Approvals[event.ApprovalID]
			if !ok || approval.RunID != event.RunID {
				return fmt.Errorf("audit event %q references a missing or different-run approval", event.ID)
			}
			request, err := validateApprovalRequestAudit(db, approval)
			if err != nil {
				return fmt.Errorf("audit event %q references an invalid approval: %w", event.ID, err)
			}
			if !sameAuditPolicyReference(event, request) {
				return errors.New("approval audit policy reference differs from its original request")
			}
			if (request.ActionID != "" && event.ActionID != request.ActionID) || (request.TraceID != "" && event.TraceID != request.TraceID) || event.Tool != request.Tool || event.ActionType != request.ActionType {
				return fmt.Errorf("audit event %q approval lineage does not match its request", event.ID)
			}
			if (event.Category == policy.ToolCallCompleted || event.Category == policy.NetworkAccessCompleted) && !completionReferencesApprovalRequest(db, event, approval) {
				return fmt.Errorf("audit event %q completion does not reference a request linked to its approval", event.ID)
			}
			if event.ApproverID != "" && (approval.DecidedBy == nil || event.ApproverID != approval.DecidedBy.ID) {
				return fmt.Errorf("audit event %q approver does not match approval decision", event.ID)
			}
			switch event.TerminationReason {
			case "approval_approved":
				if approval.Status != ApprovalApproved || approval.DecidedBy == nil || event.ApproverID != approval.DecidedBy.ID || event.Decision != policy.Allow {
					return fmt.Errorf("audit event %q lacks the explicit approver for its approval transition", event.ID)
				}
			case "approval_rejected":
				if approval.Status != ApprovalRejected || approval.DecidedBy == nil || event.ApproverID != approval.DecidedBy.ID || event.Decision != policy.Deny {
					return fmt.Errorf("audit event %q lacks the explicit approver for its rejection transition", event.ID)
				}
			case "approval_expired":
				if approval.Status != ApprovalExpired || approval.DecidedBy == nil || event.ApproverID != approval.DecidedBy.ID || event.Decision != policy.Deny {
					return fmt.Errorf("audit event %q lacks the explicit actor for its expiry transition", event.ID)
				}
			}
			if (event.Category == policy.ToolCallCompleted || event.Category == policy.NetworkAccessCompleted) && (approval.Status != ApprovalApproved || event.Decision != policy.Allow) {
				return fmt.Errorf("audit event %q records completion without an approved request", event.ID)
			}
			approvedCompletion := (event.Category == policy.ToolCallCompleted || event.Category == policy.NetworkAccessCompleted) && approval.Status == ApprovalApproved && event.Decision == policy.Allow && completionReferencesApprovalRequest(db, event, approval) && approval.DecidedBy != nil && event.ApproverID == approval.DecidedBy.ID
			if event.Version == auditVersion && (event.Category == policy.ToolCallCompleted || event.Category == policy.NetworkAccessCompleted) && approval.Status == ApprovalApproved && event.Decision == policy.Allow && (approval.DecidedBy == nil || event.ApproverID != approval.DecidedBy.ID) {
				return fmt.Errorf("audit event %q approved completion is missing its explicit approver", event.ID)
			}
			if event.ApproverID != "" && event.TerminationReason != "approval_approved" && event.TerminationReason != "approval_rejected" && event.TerminationReason != "approval_expired" && !approvedCompletion {
				return fmt.Errorf("audit event %q has an approver without an approval transition or approved completion", event.ID)
			}
		} else if event.ApproverID != "" {
			return fmt.Errorf("audit event %q has an approver without an approval", event.ID)
		}
	}
	snapshot, found := run.policySnapshot(event.PolicyHash, event.PolicyResolvedAt)
	if !found || event.PolicyID != snapshot.PolicyID || event.PolicyVersion != snapshot.Version {
		return fmt.Errorf("audit event %q policy reference does not match run snapshot", event.ID)
	}
	return nil
}

func validAuditVersion(version string) bool {
	return version == auditVersion || version == previousAuditVersion || version == legacyAuditVersion
}

func auditRecordByID(events []AuditRecord, id string) (AuditRecord, bool) {
	for _, event := range events {
		if event.ID == id {
			return event, true
		}
	}
	return AuditRecord{}, false
}

func completionReferencesApprovalRequest(db database, completion AuditRecord, approval Approval) bool {
	if completion.RequestAuditID == approval.RequestAuditID {
		return true
	}
	wantCategory := policy.ToolCallRequested
	if completion.Category == policy.NetworkAccessCompleted {
		wantCategory = policy.NetworkAccessRequested
	}
	for _, request := range db.Events {
		if request.ID != completion.RequestAuditID {
			continue
		}
		return request.RunID == completion.RunID && request.Category == wantCategory && request.Decision == policy.RequireApproval && request.ApprovalID == approval.ID && request.Tool == completion.Tool && request.ActionType == completion.ActionType && request.ActionID == completion.ActionID && request.TraceID == completion.TraceID
	}
	return false
}

func isInvocationResult(result policy.Outcome) bool {
	return result == policy.Outcome(InvocationSucceeded) || result == policy.Outcome(InvocationFailed) || result == policy.Outcome(InvocationBlocked)
}

// Validate checks the public, standalone audit event shape used by local eval
// fixtures. Store validation additionally checks it against the referenced run.
func (event AuditRecord) Validate() error {
	if !validAuditVersion(event.Version) || !safeID.MatchString(event.ID) || !safeID.MatchString(event.RunID) {
		return errors.New("audit event has invalid version or identity")
	}
	if event.Timestamp.IsZero() || !policy.IsKnownEventCategory(event.Category) {
		return errors.New("audit event has invalid category or timestamp")
	}
	if !safeLabel.MatchString(event.Runtime) || !safeID.MatchString(event.PolicyID) || (event.PolicyVersion != policy.Version && event.PolicyVersion != policy.Version2) || !policyHashPattern.MatchString(event.PolicyHash) || event.PolicyResolvedAt.IsZero() {
		return errors.New("audit event has invalid runtime or policy snapshot reference")
	}
	if (event.Actor != "" && !safeLabel.MatchString(event.Actor)) || (event.ActionType != "" && !safeLabel.MatchString(event.ActionType)) || (event.TerminationReason != "" && !safeLabel.MatchString(event.TerminationReason)) {
		return errors.New("audit event contains an unsafe label")
	}
	if event.Decision != "" && !policy.IsKnownOutcome(event.Decision) {
		return fmt.Errorf("audit event has invalid decision %q", event.Decision)
	}
	if event.Result != "" && !policy.IsKnownOutcome(event.Result) && !isInvocationResult(event.Result) {
		return fmt.Errorf("audit event has invalid result %q", event.Result)
	}
	if isInvocationResult(event.Result) {
		if event.Category != policy.ToolCallCompleted && event.Category != policy.NetworkAccessCompleted {
			return errors.New("audit invocation result requires a completed tool or network event")
		}
	}
	if event.ReasonCode != "" && !policy.IsKnownReasonCode(event.ReasonCode) {
		return fmt.Errorf("audit event has invalid reason code %q", event.ReasonCode)
	}
	credentialScope := event.CredentialScope
	if credentialScope != "" {
		if err := identity.ValidateScopes("audit credential scope", []string{credentialScope}); err != nil {
			return err
		}
	}
	if (event.Category == policy.CredentialAccessRequested || event.Category == policy.EventCredentialAccess) && credentialScope == "" {
		credentialScope = "redacted"
	}
	if event.ActorID != "" && !identity.IsSafeReference(event.ActorID) || event.DelegationID != "" && !identity.IsSafeReference(event.DelegationID) {
		return errors.New("audit event has unsafe actor or delegation reference")
	}
	return (policy.Event{Category: event.Category, Actor: event.Actor, ActionID: event.ActionID, TraceID: event.TraceID, ApprovalID: event.ApprovalID, ApproverID: event.ApproverID, Runtime: event.Runtime, Tool: event.Tool, Domain: event.Domain, CredentialScope: credentialScope, ActionType: event.ActionType, Timestamp: event.Timestamp, ObservedDecision: event.Decision, ReasonCode: event.ReasonCode, RequestAuditID: event.RequestAuditID}).Validate()
}

func auditFor(run Record, event policy.Event, decision policy.Decision, terminationReason string) (AuditRecord, error) {
	run.Policy = run.AppliedPolicy()
	if err := event.Validate(); err != nil {
		return AuditRecord{}, err
	}
	if err := decision.Validate(); err != nil {
		return AuditRecord{}, fmt.Errorf("invalid policy decision: %w", err)
	}
	if event.CredentialScope != "" {
		if err := identity.ValidateScopes("audit credential scope", []string{event.CredentialScope}); err != nil {
			return AuditRecord{}, err
		}
	}
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now().UTC()
	}
	id, err := newID("evt")
	if err != nil {
		return AuditRecord{}, err
	}
	traceID := event.TraceID
	if traceID == "" {
		traceID, err = newID("trace")
		if err != nil {
			return AuditRecord{}, err
		}
	}
	actorID, delegationID := "", ""
	if run.Identity.Mode == identity.ModeNamed && run.Identity.Actor != nil && run.Identity.Delegation != nil {
		actorID, delegationID = run.Identity.Actor.ID, run.Identity.Delegation.ID
	}
	actor := event.Actor
	if actor != "" && !safeLabel.MatchString(actor) {
		actor = "runtime"
	}
	domain := event.Domain
	if domain != "" {
		if normalized, err := normalizeAuditDomain(domain); err == nil {
			domain = normalized
		} else {
			domain = ""
		}
	}
	reason := sanitizeReason(terminationReason)
	// Retain safe scope identifiers only for attributable permission denials.
	// Ordinary/anonymous credential events keep the historical omission contract.
	credentialScope := ""
	if run.Identity.Mode == identity.ModeNamed && event.Category == policy.CredentialAccessRequested && decision.Outcome == policy.Deny && event.ActionID != "" {
		credentialScope = event.CredentialScope
	}
	return AuditRecord{Version: auditVersion, ID: id, RunID: run.ID, RequestAuditID: event.RequestAuditID, Timestamp: event.Timestamp.UTC(), Category: event.Category, Actor: actor, ActorID: actorID, DelegationID: delegationID, ActionID: event.ActionID, ApprovalID: event.ApprovalID, ApproverID: event.ApproverID, TraceID: traceID, Runtime: run.Runtime, Tool: event.Tool, ActionType: event.ActionType, Domain: domain, CredentialScope: credentialScope, PolicyID: run.Policy.PolicyID, PolicyVersion: run.Policy.Version, PolicyHash: run.Policy.Hash, PolicyResolvedAt: run.Policy.ResolvedAt, Decision: decision.Outcome, Result: decision.Outcome, ReasonCode: decision.ReasonCode, TerminationReason: reason}, nil
}

func validateRequestAuditLink(events []AuditRecord, runID string, event policy.Event, strict bool) error {
	if event.RequestAuditID == "" {
		return errors.New("completion event must reference its persisted request audit")
	}
	wantCategory := policy.ToolCallRequested
	if event.Category == policy.NetworkAccessCompleted {
		wantCategory = policy.NetworkAccessRequested
	}
	for _, prior := range events {
		if prior.ID != event.RequestAuditID {
			continue
		}
		strictCorrelation := strict && prior.Version != legacyAuditVersion
		if prior.RunID != runID || prior.Category != wantCategory || prior.Tool != event.Tool || !sameAuditDomain(prior.Domain, event.Domain) || prior.ActionType != event.ActionType || prior.Timestamp.After(event.Timestamp) || (strictCorrelation && (prior.ActionID != event.ActionID || prior.TraceID != event.TraceID)) {
			return errors.New("completion event request audit does not match its action")
		}
		return nil
	}
	return fmt.Errorf("request audit %q was not found for run %q", event.RequestAuditID, runID)
}

func sameAuditDomain(left, right string) bool {
	if left == "" || right == "" {
		return left == right
	}
	leftCanonical, leftErr := normalizeAuditDomain(left)
	rightCanonical, rightErr := normalizeAuditDomain(right)
	return leftErr == nil && rightErr == nil && leftCanonical == rightCanonical
}

func normalizeAuditDomain(value string) (string, error) {
	host := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(value), "."))
	if strings.ContainsAny(host, ":/@*%") || host == "" {
		return "", errors.New("invalid hostname")
	}
	return host, nil
}
func sanitizeReason(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > 256 {
		value = value[:256]
	}
	if strings.ContainsAny(value, "\r\n\x00") {
		return "operator-requested"
	}
	return value
}

func validTerminationReason(value string) bool {
	switch value {
	case TerminationOperatorRequested, TerminationPolicyViolation, TerminationBudgetExhausted, TerminationUnexpected, TerminationRuntimeFailure:
		return true
	default:
		return false
	}
}
func outcomeFor(status ApprovalStatus) policy.Outcome {
	if status == ApprovalApproved {
		return policy.Allow
	}
	return policy.Deny
}
func validateID(kind, value string) error {
	if value == "" || !safeID.MatchString(value) || value == "." || value == ".." {
		return fmt.Errorf("invalid %s id %q", kind, value)
	}
	return nil
}
func newID(prefix string) (string, error) {
	var random [12]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", fmt.Errorf("generate %s id: %w", prefix, err)
	}
	return prefix + "-" + hex.EncodeToString(random[:]), nil
}
func joinControls(controls []policy.Control) string {
	values := make([]string, len(controls))
	for i, control := range controls {
		values[i] = string(control)
	}
	return strings.Join(values, ", ")
}
