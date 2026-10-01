// Package policy defines versioned, runtime-neutral agent policies and their
// deterministic event evaluation boundary.
package policy

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/AllenMuu/skill-manager/internal/identity"
	"gopkg.in/yaml.v3"
)

const (
	Version       = "v1"
	Version2      = "v2"
	Kind          = "agent-policy"
	maxPolicySize = 1 << 20
)

type AgentPolicy struct {
	Version     string           `yaml:"version" json:"version"`
	Kind        string           `yaml:"kind" json:"kind"`
	ID          string           `yaml:"id" json:"id"`
	Name        string           `yaml:"name" json:"name"`
	Tools       ToolRules        `yaml:"tools" json:"tools"`
	Identity    *IdentityRules   `yaml:"identity,omitempty" json:"identity,omitempty"`
	Network     *NetworkRules    `yaml:"network,omitempty" json:"network,omitempty"`
	Credentials *CredentialRules `yaml:"credentials,omitempty" json:"credentials,omitempty"`
	Subagents   *SubagentLimits  `yaml:"subagents,omitempty" json:"subagents,omitempty"`
	Budget      *BudgetLimits    `yaml:"budget,omitempty" json:"budget,omitempty"`
	Approval    ApprovalRules    `yaml:"approval,omitempty" json:"approval,omitempty"`
	Termination *Termination     `yaml:"termination,omitempty" json:"termination,omitempty"`
	Enforcement Enforcement      `yaml:"enforcement,omitempty" json:"enforcement,omitempty"`
}

type IdentityRules struct {
	Rules []IdentityRule `yaml:"rules" json:"rules"`
}

type IdentityRule struct {
	ActionID       string   `yaml:"action_id" json:"action_id"`
	ActorKinds     []string `yaml:"actor_kinds" json:"actor_kinds"`
	Roles          []string `yaml:"roles,omitempty" json:"roles,omitempty"`
	RequiredScopes []string `yaml:"required_scopes,omitempty" json:"required_scopes,omitempty"`
}

type ToolRules struct {
	Allow []string `yaml:"allow,omitempty" json:"allow,omitempty"`
	Deny  []string `yaml:"deny,omitempty" json:"deny,omitempty"`
}

type NetworkRules struct {
	AllowedDomains []string `yaml:"allowed_domains,omitempty" json:"allowed_domains,omitempty"`
	DeniedDomains  []string `yaml:"denied_domains,omitempty" json:"denied_domains,omitempty"`
}

type CredentialRules struct {
	AllowedScopes []string `yaml:"allowed_scopes,omitempty" json:"allowed_scopes,omitempty"`
	DeniedScopes  []string `yaml:"denied_scopes,omitempty" json:"denied_scopes,omitempty"`
}

type SubagentLimits struct {
	MaxConcurrent *int64 `yaml:"max_concurrent,omitempty" json:"max_concurrent,omitempty"`
	MaxTotal      *int64 `yaml:"max_total,omitempty" json:"max_total,omitempty"`
}

type BudgetLimits struct {
	MaxDurationSeconds *int64   `yaml:"max_duration_seconds,omitempty" json:"max_duration_seconds,omitempty"`
	MaxCostUSD         *float64 `yaml:"max_cost_usd,omitempty" json:"max_cost_usd,omitempty"`
	MaxToolCalls       *int64   `yaml:"max_tool_calls,omitempty" json:"max_tool_calls,omitempty"`
}

type ApprovalRules struct {
	RequiredFor []string `yaml:"required_for,omitempty" json:"required_for,omitempty"`
}

type Termination struct {
	KillOn []Trigger `yaml:"kill_on,omitempty" json:"kill_on,omitempty"`
}

type Enforcement struct {
	OptionalControls []Control `yaml:"optional_controls,omitempty" json:"optional_controls,omitempty"`
}

type Control string

const (
	ControlToolInterception    Control = "tool_interception"
	ControlApprovalPauseResume Control = "approval_pause_resume"
	ControlDurationBudget      Control = "duration_budget"
	ControlCostBudget          Control = "cost_budget"
	ControlToolCallBudget      Control = "tool_call_budget"
	ControlSubagentLimits      Control = "subagent_limits"
	ControlNetworkRestriction  Control = "network_restriction"
	ControlCredentialScope     Control = "credential_scope"
	ControlRunTermination      Control = "run_termination"
	ControlRuntimeEvents       Control = "runtime_events"
)

type Trigger string

const (
	TriggerDeniedToolCall     Trigger = "denied_tool_call"
	TriggerDeniedDomainAccess Trigger = "denied_domain_access"
	TriggerBudgetExhausted    Trigger = "budget_exhausted"
	TriggerPolicyViolation    Trigger = "policy_violation"
	TriggerUnexpectedExit     Trigger = "unexpected_termination"
)

type Snapshot struct {
	Policy     AgentPolicy `json:"policy" yaml:"policy"`
	PolicyID   string      `json:"policy_id" yaml:"policy_id"`
	Version    string      `json:"version" yaml:"version"`
	Hash       string      `json:"hash" yaml:"hash"`
	ResolvedAt time.Time   `json:"resolved_at" yaml:"resolved_at"`
}

func Load(r io.Reader) (AgentPolicy, error) {
	if r == nil {
		return AgentPolicy{}, fmt.Errorf("policy input is required")
	}
	contents, err := io.ReadAll(io.LimitReader(r, maxPolicySize+1))
	if err != nil {
		return AgentPolicy{}, fmt.Errorf("read policy: %w", err)
	}
	if len(contents) > maxPolicySize {
		return AgentPolicy{}, fmt.Errorf("policy exceeds maximum size of %d bytes", maxPolicySize)
	}
	decoder := yaml.NewDecoder(bytes.NewReader(contents))
	decoder.KnownFields(true)
	var result AgentPolicy
	if err := decoder.Decode(&result); err != nil {
		return AgentPolicy{}, fmt.Errorf("parse policy: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return AgentPolicy{}, fmt.Errorf("policy must contain exactly one YAML document")
		}
		return AgentPolicy{}, fmt.Errorf("parse trailing policy document: %w", err)
	}
	if err := result.Validate(); err != nil {
		return AgentPolicy{}, err
	}
	return result.normalized(), nil
}

func (p AgentPolicy) Validate() error {
	if p.Version != Version && p.Version != Version2 {
		return fmt.Errorf("unsupported policy version %q", p.Version)
	}
	if p.Version == Version && p.Identity != nil {
		return errors.New("identity rules require AgentPolicy v2")
	}
	if p.Kind != Kind {
		return fmt.Errorf("policy kind must be %q", Kind)
	}
	if !policyIDPattern.MatchString(p.ID) {
		return fmt.Errorf("policy id %q must be a lowercase kebab-case identifier", p.ID)
	}
	if strings.TrimSpace(p.Name) == "" || p.Name != strings.TrimSpace(p.Name) {
		return fmt.Errorf("policy name is required and must not have surrounding whitespace")
	}
	if err := validateIdentifiers("tools.allow", p.Tools.Allow, toolPattern, true); err != nil {
		return err
	}
	if err := validateIdentifiers("tools.deny", p.Tools.Deny, toolPattern, true); err != nil {
		return err
	}
	if p.Identity != nil {
		if len(p.Identity.Rules) == 0 {
			return errors.New("identity.rules must contain at least one rule")
		}
		seenActions := make(map[string]struct{}, len(p.Identity.Rules))
		for i, rule := range p.Identity.Rules {
			name := fmt.Sprintf("identity.rules[%d]", i)
			if err := validateIdentifiers(name+".action_id", []string{rule.ActionID}, actionPattern, true); err != nil {
				return err
			}
			if _, duplicate := seenActions[rule.ActionID]; duplicate {
				return fmt.Errorf("identity.rules repeats action %q", rule.ActionID)
			}
			seenActions[rule.ActionID] = struct{}{}
			if err := validateIdentifiers(name+".actor_kinds", rule.ActorKinds, actorKindPattern, true); err != nil {
				return err
			}
			if len(rule.ActorKinds) == 0 {
				return fmt.Errorf("%s.actor_kinds must contain at least one actor kind", name)
			}
			if err := validateSafeIdentityLabels(name+".roles", rule.Roles, identifierPattern, true); err != nil {
				return err
			}
			if err := identity.ValidateScopes(name+".required_scopes", rule.RequiredScopes); err != nil {
				return err
			}
		}
	}
	if err := validateIdentifiers("approval.required_for", p.Approval.RequiredFor, actionPattern, true); err != nil {
		return err
	}
	if p.Network != nil {
		if err := validateDomains("network.allowed_domains", p.Network.AllowedDomains); err != nil {
			return err
		}
		if err := validateDomains("network.denied_domains", p.Network.DeniedDomains); err != nil {
			return err
		}
	}
	if p.Credentials != nil {
		if err := identity.ValidateScopes("credentials.allowed_scopes", p.Credentials.AllowedScopes); err != nil {
			return err
		}
		if err := identity.ValidateScopes("credentials.denied_scopes", p.Credentials.DeniedScopes); err != nil {
			return err
		}
	}
	if p.Subagents != nil {
		if err := validateNonNegative("subagents.max_concurrent", p.Subagents.MaxConcurrent); err != nil {
			return err
		}
		if err := validateNonNegative("subagents.max_total", p.Subagents.MaxTotal); err != nil {
			return err
		}
	}
	if p.Budget != nil {
		if err := validateNonNegative("budget.max_duration_seconds", p.Budget.MaxDurationSeconds); err != nil {
			return err
		}
		if err := validateNonNegative("budget.max_tool_calls", p.Budget.MaxToolCalls); err != nil {
			return err
		}
		if p.Budget.MaxCostUSD != nil && (math.IsNaN(*p.Budget.MaxCostUSD) || math.IsInf(*p.Budget.MaxCostUSD, 0) || *p.Budget.MaxCostUSD < 0) {
			return fmt.Errorf("budget.max_cost_usd must be a finite non-negative number")
		}
	}
	if p.Termination != nil {
		if err := validateUniqueTriggers(p.Termination.KillOn); err != nil {
			return err
		}
	}
	configured := p.configuredControls()
	seenOptional := make(map[Control]struct{}, len(p.Enforcement.OptionalControls))
	for _, control := range p.Enforcement.OptionalControls {
		if !validControl(control) {
			return fmt.Errorf("enforcement.optional_controls contains unsupported control %q", control)
		}
		if _, duplicate := seenOptional[control]; duplicate {
			return fmt.Errorf("enforcement.optional_controls repeats %q", control)
		}
		if _, active := configured[control]; !active {
			return fmt.Errorf("enforcement.optional_controls references unconfigured control %q", control)
		}
		seenOptional[control] = struct{}{}
	}
	return nil
}

func Resolve(p AgentPolicy, at time.Time) (Snapshot, error) {
	if err := p.Validate(); err != nil {
		return Snapshot{}, err
	}
	normalized := p.normalized()
	encoded, err := json.Marshal(normalized)
	if err != nil {
		return Snapshot{}, fmt.Errorf("encode canonical policy: %w", err)
	}
	hash := sha256.Sum256(encoded)
	return Snapshot{
		Policy:     normalized,
		PolicyID:   normalized.ID,
		Version:    normalized.Version,
		Hash:       "sha256:" + hex.EncodeToString(hash[:]),
		ResolvedAt: at.UTC(),
	}, nil
}

func (s Snapshot) Validate() error {
	if s.PolicyID != s.Policy.ID || s.Version != s.Policy.Version {
		return fmt.Errorf("policy snapshot identity does not match its policy")
	}
	resolved, err := Resolve(s.Policy, s.ResolvedAt)
	if err != nil {
		return fmt.Errorf("validate policy snapshot: %w", err)
	}
	if s.Hash != resolved.Hash {
		return fmt.Errorf("policy snapshot hash does not match its policy")
	}
	return nil
}

type EventCategory string

const (
	AgentRunStarted            EventCategory = "AgentRunStarted"
	ToolCallRequested          EventCategory = "ToolCallRequested"
	ToolCallCompleted          EventCategory = "ToolCallCompleted"
	NetworkAccessRequested     EventCategory = "NetworkAccessRequested"
	NetworkAccessCompleted     EventCategory = "NetworkAccessCompleted"
	CredentialAccessRequested  EventCategory = "CredentialAccessRequested"
	SubAgentSpawnRequested     EventCategory = "SubAgentSpawnRequested"
	BudgetUpdated              EventCategory = "BudgetUpdated"
	AgentRunCompleted          EventCategory = "AgentRunCompleted"
	AgentRunFailed             EventCategory = "AgentRunFailed"
	AgentRunTerminated         EventCategory = "AgentRunTerminated"
	EventUnexpectedToolCall    EventCategory = "UnexpectedToolCall"
	EventUnexpectedNetwork     EventCategory = "UnexpectedNetworkAccess"
	EventCredentialAccess      EventCategory = "CredentialAccess"
	EventRetryStorm            EventCategory = "RetryStorm"
	EventProgressStall         EventCategory = "ProgressStall"
	EventSubagentSpawnSpike    EventCategory = "SubAgentSpawnSpike"
	EventBudgetExhaustion      EventCategory = "BudgetExhaustion"
	EventPolicyViolation       EventCategory = "PolicyViolation"
	EventUnexpectedTermination EventCategory = "UnexpectedTermination"
)

type Event struct {
	ID               string        `json:"id,omitempty" yaml:"id,omitempty"`
	RunID            string        `json:"run_id,omitempty" yaml:"run_id,omitempty"`
	RequestAuditID   string        `json:"request_audit_id,omitempty" yaml:"request_audit_id,omitempty"`
	Timestamp        time.Time     `json:"timestamp,omitempty" yaml:"timestamp,omitempty"`
	Category         EventCategory `json:"category" yaml:"category"`
	Actor            string        `json:"actor,omitempty" yaml:"actor,omitempty"`
	ActionID         string        `json:"action_id,omitempty" yaml:"action_id,omitempty"`
	TraceID          string        `json:"trace_id,omitempty" yaml:"trace_id,omitempty"`
	ApprovalID       string        `json:"approval_id,omitempty" yaml:"approval_id,omitempty"`
	ApproverID       string        `json:"approver_id,omitempty" yaml:"approver_id,omitempty"`
	Runtime          string        `json:"runtime,omitempty" yaml:"runtime,omitempty"`
	Resource         string        `json:"resource,omitempty" yaml:"resource,omitempty"`
	Tool             string        `json:"tool,omitempty" yaml:"tool,omitempty"`
	Domain           string        `json:"domain,omitempty" yaml:"domain,omitempty"`
	CredentialScope  string        `json:"credential_scope,omitempty" yaml:"credential_scope,omitempty"`
	ActionType       string        `json:"action_type,omitempty" yaml:"action_type,omitempty"`
	ObservedDecision Outcome       `json:"observed_decision,omitempty" yaml:"observed_decision,omitempty"`
	ReasonCode       ReasonCode    `json:"reason_code,omitempty" yaml:"reason_code,omitempty"`
}

func (e Event) Validate() error {
	if !validEventCategory(e.Category) {
		return fmt.Errorf("unsupported governance event category %q", e.Category)
	}
	switch e.Category {
	case ToolCallRequested, ToolCallCompleted:
		if !toolPattern.MatchString(e.Tool) {
			return fmt.Errorf("event %s requires a valid tool identifier", e.Category)
		}
		if e.Category == ToolCallCompleted && e.RequestAuditID == "" {
			return errors.New("tool completion requires its persisted request audit id")
		}
	case NetworkAccessRequested, NetworkAccessCompleted, EventUnexpectedNetwork:
		if _, err := normalizeDomain(e.Domain); err != nil {
			return fmt.Errorf("event %s requires a valid domain: %w", e.Category, err)
		}
		if e.Category == NetworkAccessCompleted && e.RequestAuditID == "" {
			return errors.New("network completion requires its persisted request audit id")
		}
	case CredentialAccessRequested, EventCredentialAccess:
		if !identity.IsValidScope(e.CredentialScope) {
			return fmt.Errorf("event %s requires a valid credential scope", e.Category)
		}
	case SubAgentSpawnRequested:
		if e.Resource != "" && !identifierPattern.MatchString(e.Resource) {
			return fmt.Errorf("event %s has an invalid subagent identifier", e.Category)
		}
	}
	if e.ActionType != "" && !actionPattern.MatchString(e.ActionType) {
		return fmt.Errorf("event has invalid action type %q", e.ActionType)
	}
	if e.ActionID != "" && !actionPattern.MatchString(e.ActionID) {
		return errors.New("event has an invalid normalized action id")
	}
	if e.TraceID != "" && (!traceIDPattern.MatchString(e.TraceID) || identity.IsCredentialLike(e.TraceID)) {
		return errors.New("event has an invalid trace id")
	}
	for name, value := range map[string]string{"approval id": e.ApprovalID, "approver id": e.ApproverID} {
		if value != "" && (!correlationIDPattern.MatchString(value) || identity.IsCredentialLike(value)) {
			return fmt.Errorf("event has an invalid %s", name)
		}
	}
	if e.ApproverID != "" && e.ApprovalID == "" {
		return errors.New("event approver id requires an approval id")
	}
	if e.ObservedDecision != "" && !IsKnownOutcome(e.ObservedDecision) {
		return fmt.Errorf("event has invalid observed decision %q", e.ObservedDecision)
	}
	if e.RequestAuditID != "" && !identifierPattern.MatchString(e.RequestAuditID) {
		return fmt.Errorf("event has invalid request audit id %q", e.RequestAuditID)
	}
	if e.RequestAuditID != "" && e.Category != ToolCallCompleted && e.Category != NetworkAccessCompleted {
		return fmt.Errorf("event %s cannot reference a completed-action request audit", e.Category)
	}
	if e.ReasonCode != "" && !IsKnownReasonCode(e.ReasonCode) {
		return fmt.Errorf("event has invalid reason code %q", e.ReasonCode)
	}
	if e.ObservedDecision != "" {
		if err := (Decision{Outcome: e.ObservedDecision, ReasonCode: e.ReasonCode}).Validate(); err != nil {
			return fmt.Errorf("event has invalid observed policy decision: %w", err)
		}
	}
	return nil
}

type Outcome string

const (
	Allow            Outcome = "ALLOW"
	Deny             Outcome = "DENY"
	RequireApproval  Outcome = "REQUIRE_APPROVAL"
	AllowWithWarning Outcome = "ALLOW_WITH_WARNING"
)

type ReasonCode string

const (
	ReasonToolDenied                       ReasonCode = "TOOL_DENIED_BY_POLICY"
	ReasonToolNotAllowlisted               ReasonCode = "TOOL_NOT_ALLOWLISTED"
	ReasonApprovalRequired                 ReasonCode = "ACTION_APPROVAL_REQUIRED"
	ReasonDomainDenied                     ReasonCode = "NETWORK_DOMAIN_DENIED"
	ReasonDomainNotAllowed                 ReasonCode = "NETWORK_DOMAIN_NOT_ALLOWED"
	ReasonCredentialDenied                 ReasonCode = "CREDENTIAL_SCOPE_DENIED"
	ReasonCredentialNotAllowed             ReasonCode = "CREDENTIAL_SCOPE_NOT_ALLOWED"
	ReasonDurationBudgetExceeded           ReasonCode = "DURATION_BUDGET_EXCEEDED"
	ReasonCostBudgetExceeded               ReasonCode = "COST_BUDGET_EXCEEDED"
	ReasonToolCallBudgetExceeded           ReasonCode = "TOOL_CALL_BUDGET_EXCEEDED"
	ReasonConcurrentSubagentsExceeded      ReasonCode = "SUBAGENT_CONCURRENCY_LIMIT_EXCEEDED"
	ReasonTotalSubagentsExceeded           ReasonCode = "SUBAGENT_TOTAL_LIMIT_EXCEEDED"
	ReasonInvalidEvent                     ReasonCode = "INVALID_GOVERNANCE_EVENT"
	ReasonInvalidSnapshot                  ReasonCode = "INVALID_POLICY_SNAPSHOT"
	ReasonInvalidBudgetState               ReasonCode = "INVALID_BUDGET_STATE"
	ReasonUnexpectedTermination            ReasonCode = "UNEXPECTED_TERMINATION"
	ReasonIdentityRequired                 ReasonCode = "IDENTITY_REQUIRED"
	ReasonIdentityPolicyRuleMissing        ReasonCode = "IDENTITY_POLICY_RULE_MISSING"
	ReasonIdentityPolicyUnsupportedVersion ReasonCode = "IDENTITY_POLICY_UNSUPPORTED_VERSION"
	ReasonIdentityPolicyDenied             ReasonCode = "IDENTITY_POLICY_DENIED"
	ReasonDelegationScopeMissing           ReasonCode = "DELEGATION_SCOPE_MISSING"
	ReasonDelegationExpired                ReasonCode = "DELEGATION_EXPIRED"
)

type Decision struct {
	Outcome              Outcome    `json:"decision" yaml:"decision"`
	ReasonCode           ReasonCode `json:"reason_code,omitempty" yaml:"reason_code,omitempty"`
	Detail               string     `json:"detail,omitempty" yaml:"detail,omitempty"`
	Warnings             []string   `json:"warnings,omitempty" yaml:"warnings,omitempty"`
	TerminationRequested bool       `json:"termination_requested,omitempty" yaml:"termination_requested,omitempty"`
}

func (d Decision) Validate() error {
	if !IsKnownOutcome(d.Outcome) {
		return fmt.Errorf("unsupported policy outcome %q", d.Outcome)
	}
	if d.ReasonCode != "" && !IsKnownReasonCode(d.ReasonCode) {
		return fmt.Errorf("unsupported policy reason code %q", d.ReasonCode)
	}
	if (d.Outcome == Deny || d.Outcome == RequireApproval) && d.ReasonCode == "" {
		return fmt.Errorf("policy outcome %s requires a reason code", d.Outcome)
	}
	return nil
}

type BudgetState struct {
	Elapsed             time.Duration
	CostUSD             float64
	ToolCalls           int64
	ConcurrentSubagents int64
	TotalSubagents      int64
}

type IdentityRequirement struct {
	ActionID string
	Required bool
}

type Engine struct{}

func (Engine) EvaluateBefore(snapshot Snapshot, event Event, state BudgetState) Decision {
	if err := snapshot.Validate(); err != nil {
		return Decision{Outcome: Deny, ReasonCode: ReasonInvalidSnapshot, Detail: err.Error()}
	}
	if err := event.Validate(); err != nil {
		return Decision{Outcome: Deny, ReasonCode: ReasonInvalidEvent, Detail: err.Error()}
	}
	if err := state.Validate(); err != nil {
		return Decision{Outcome: Deny, ReasonCode: ReasonInvalidBudgetState, Detail: err.Error()}
	}
	p := snapshot.Policy
	if decision, ok := budgetDecision(p, event, state); ok {
		decision.TerminationRequested = shouldTerminate(p, decision.ReasonCode)
		return decision
	}
	decision := Decision{Outcome: Allow}
	switch event.Category {
	case ToolCallRequested:
		if contains(p.Tools.Deny, event.Tool) {
			decision = Decision{Outcome: Deny, ReasonCode: ReasonToolDenied, Detail: "tool is explicitly denied by policy"}
		} else if !contains(p.Tools.Allow, event.Tool) {
			decision = Decision{Outcome: Deny, ReasonCode: ReasonToolNotAllowlisted, Detail: "tool is not explicitly allowed by policy"}
		} else if contains(p.Approval.RequiredFor, event.ActionType) {
			decision = Decision{Outcome: RequireApproval, ReasonCode: ReasonApprovalRequired, Detail: "policy requires operator approval for this action"}
		}
	case NetworkAccessRequested:
		if p.Network != nil {
			domain, _ := normalizeDomain(event.Domain)
			if contains(p.Network.DeniedDomains, domain) {
				decision = Decision{Outcome: Deny, ReasonCode: ReasonDomainDenied, Detail: "network domain is explicitly denied by policy"}
			} else if !contains(p.Network.AllowedDomains, domain) {
				decision = Decision{Outcome: Deny, ReasonCode: ReasonDomainNotAllowed, Detail: "network domain is not explicitly allowed by policy"}
			}
		}
	case CredentialAccessRequested:
		if p.Credentials != nil {
			scope := normalizeScope(event.CredentialScope)
			if contains(p.Credentials.DeniedScopes, scope) {
				decision = Decision{Outcome: Deny, ReasonCode: ReasonCredentialDenied, Detail: "credential scope is explicitly denied by policy"}
			} else if !contains(p.Credentials.AllowedScopes, scope) {
				decision = Decision{Outcome: Deny, ReasonCode: ReasonCredentialNotAllowed, Detail: "credential scope is not explicitly allowed by policy"}
			}
		}
	case SubAgentSpawnRequested:
		if p.Subagents != nil {
			if p.Subagents.MaxConcurrent != nil && state.ConcurrentSubagents >= *p.Subagents.MaxConcurrent {
				decision = Decision{Outcome: Deny, ReasonCode: ReasonConcurrentSubagentsExceeded, Detail: "concurrent subagent limit is exhausted"}
			} else if p.Subagents.MaxTotal != nil && state.TotalSubagents >= *p.Subagents.MaxTotal {
				decision = Decision{Outcome: Deny, ReasonCode: ReasonTotalSubagentsExceeded, Detail: "total subagent limit is exhausted"}
			}
		}
	}
	decision.TerminationRequested = shouldTerminate(p, decision.ReasonCode)
	return decision
}

type Evaluation struct {
	Violations           []EventCategory `json:"violations,omitempty" yaml:"violations,omitempty"`
	TerminationRequested bool            `json:"termination_requested,omitempty" yaml:"termination_requested,omitempty"`
}

// EvaluateAfter expects state captured before the action. Run Manager callers
// should link completion events to the persisted request audit so the engine
// can use the original decision and avoid re-evaluating post-action budgets.
// EvaluateBeforeWithIdentity applies the existing policy first, then an
// identity gate for actions whose contract explicitly requires it. Ordinary
// policy denials retain precedence; a passing gate preserves ALLOW or
// REQUIRE_APPROVAL from the existing policy engine.
func (e Engine) EvaluateBeforeWithIdentity(snapshot Snapshot, event Event, state BudgetState, requirement IdentityRequirement, runID string, selection identity.Selection, now time.Time) Decision {
	decision := e.EvaluateBefore(snapshot, event, state)
	if !requirement.Required || decision.Outcome == Deny {
		return decision
	}
	if event.ActionID != requirement.ActionID {
		return Decision{Outcome: Deny, ReasonCode: ReasonIdentityPolicyDenied}
	}
	if result := evaluateIdentity(snapshot, requirement, runID, selection, now); result.Outcome == Deny {
		return result
	}
	return decision
}

func evaluateIdentity(snapshot Snapshot, requirement IdentityRequirement, runID string, selection identity.Selection, now time.Time) Decision {
	deny := func(reason ReasonCode) Decision { return Decision{Outcome: Deny, ReasonCode: reason} }
	if !actionPattern.MatchString(requirement.ActionID) {
		return deny(ReasonIdentityPolicyRuleMissing)
	}
	if snapshot.Policy.Version == Version {
		return deny(ReasonIdentityPolicyUnsupportedVersion)
	}
	if snapshot.Policy.Version != Version2 || snapshot.Policy.Identity == nil {
		return deny(ReasonIdentityPolicyRuleMissing)
	}
	var rule *IdentityRule
	for i := range snapshot.Policy.Identity.Rules {
		if snapshot.Policy.Identity.Rules[i].ActionID == requirement.ActionID {
			rule = &snapshot.Policy.Identity.Rules[i]
			break
		}
	}
	if rule == nil {
		return deny(ReasonIdentityPolicyRuleMissing)
	}
	if runID == "" || now.IsZero() || selection.Mode != identity.ModeNamed || selection.Validate() != nil || selection.Actor == nil || selection.Delegation == nil {
		return deny(ReasonIdentityRequired)
	}
	actor, delegation := selection.Actor, selection.Delegation
	if delegation.RunID != runID || delegation.ActorID != actor.ID {
		return deny(ReasonIdentityRequired)
	}
	if !now.Before(delegation.ExpiresAt) {
		return deny(ReasonDelegationExpired)
	}
	if !contains(rule.ActorKinds, string(actor.Kind)) {
		return deny(ReasonIdentityPolicyDenied)
	}
	if len(rule.Roles) > 0 {
		matchedRole := false
		for _, role := range actor.Roles {
			if contains(rule.Roles, role) {
				matchedRole = true
				break
			}
		}
		if !matchedRole {
			return deny(ReasonIdentityPolicyDenied)
		}
	}
	for _, scope := range rule.RequiredScopes {
		if !delegation.HasScope(scope, now) {
			return deny(ReasonDelegationScopeMissing)
		}
	}
	return Decision{Outcome: Allow}
}

func (e Engine) EvaluateAfter(snapshot Snapshot, event Event, state BudgetState) Evaluation {
	result := Evaluation{Violations: []EventCategory{}}
	var request Event
	switch event.Category {
	case ToolCallCompleted:
		request = event
		request.Category = ToolCallRequested
	case NetworkAccessCompleted:
		request = event
		request.Category = NetworkAccessRequested
	default:
		decision := e.EvaluateBefore(snapshot, event, state)
		if isBudgetReason(decision.ReasonCode) {
			result.Violations = append(result.Violations, EventBudgetExhaustion)
		}
		result.TerminationRequested = decision.TerminationRequested || event.Category == EventUnexpectedTermination && shouldTerminateTrigger(snapshot.Policy, TriggerUnexpectedExit)
		if event.Category == EventUnexpectedTermination {
			result.Violations = append(result.Violations, EventUnexpectedTermination)
		}
		return result
	}
	expected := e.EvaluateBefore(snapshot, request, state)
	if event.ObservedDecision != "" {
		expected = Decision{Outcome: event.ObservedDecision, ReasonCode: event.ReasonCode, TerminationRequested: shouldTerminate(snapshot.Policy, event.ReasonCode)}
	}
	if expected.Outcome != Allow {
		if event.Category == ToolCallCompleted {
			result.Violations = append(result.Violations, EventUnexpectedToolCall)
		} else {
			result.Violations = append(result.Violations, EventUnexpectedNetwork)
		}
	}
	result.TerminationRequested = expected.TerminationRequested || (len(result.Violations) > 0 && shouldTerminate(snapshot.Policy, ReasonToolDenied))
	return result
}

func (state BudgetState) Validate() error {
	if state.Elapsed < 0 {
		return fmt.Errorf("elapsed run duration must be non-negative")
	}
	if math.IsNaN(state.CostUSD) || math.IsInf(state.CostUSD, 0) || state.CostUSD < 0 {
		return fmt.Errorf("run cost must be finite and non-negative")
	}
	if state.ToolCalls < 0 || state.ConcurrentSubagents < 0 || state.TotalSubagents < 0 {
		return fmt.Errorf("run counters must be non-negative")
	}
	return nil
}

type CapabilityReport struct {
	Ready    bool      `json:"ready" yaml:"ready"`
	Required []Control `json:"required" yaml:"required"`
	Missing  []Control `json:"missing" yaml:"missing"`
	Warnings []Control `json:"warnings" yaml:"warnings"`
}

func CheckCapabilities(p AgentPolicy, capabilities map[Control]bool) CapabilityReport {
	report := CapabilityReport{Ready: true, Required: []Control{}, Missing: []Control{}, Warnings: []Control{}}
	if err := p.Validate(); err != nil {
		report.Ready = false
		return report
	}
	optional := make(map[Control]struct{}, len(p.Enforcement.OptionalControls))
	for _, control := range p.Enforcement.OptionalControls {
		optional[control] = struct{}{}
	}
	controls := make([]Control, 0, len(p.configuredControls()))
	for control := range p.configuredControls() {
		controls = append(controls, control)
	}
	sort.Slice(controls, func(i, j int) bool { return controls[i] < controls[j] })
	for _, control := range controls {
		if _, isOptional := optional[control]; !isOptional {
			report.Required = append(report.Required, control)
		}
		if capabilities[control] {
			continue
		}
		if _, isOptional := optional[control]; isOptional {
			report.Warnings = append(report.Warnings, control)
		} else {
			report.Missing = append(report.Missing, control)
			report.Ready = false
		}
	}
	return report
}

func (p AgentPolicy) configuredControls() map[Control]struct{} {
	controls := map[Control]struct{}{ControlToolInterception: {}, ControlRuntimeEvents: {}}
	if len(p.Approval.RequiredFor) > 0 {
		controls[ControlApprovalPauseResume] = struct{}{}
	}
	if p.Budget != nil {
		if p.Budget.MaxDurationSeconds != nil {
			controls[ControlDurationBudget] = struct{}{}
		}
		if p.Budget.MaxCostUSD != nil {
			controls[ControlCostBudget] = struct{}{}
		}
		if p.Budget.MaxToolCalls != nil {
			controls[ControlToolCallBudget] = struct{}{}
		}
	}
	if p.Subagents != nil && (p.Subagents.MaxConcurrent != nil || p.Subagents.MaxTotal != nil) {
		controls[ControlSubagentLimits] = struct{}{}
	}
	if p.Network != nil {
		controls[ControlNetworkRestriction] = struct{}{}
	}
	if p.Credentials != nil {
		controls[ControlCredentialScope] = struct{}{}
	}
	if p.Termination != nil && len(p.Termination.KillOn) > 0 {
		controls[ControlRunTermination] = struct{}{}
	}
	return controls
}

func (p AgentPolicy) normalized() AgentPolicy {
	result := p
	result.Tools.Allow = sortedCopy(p.Tools.Allow)
	result.Tools.Deny = sortedCopy(p.Tools.Deny)
	result.Approval.RequiredFor = sortedCopy(p.Approval.RequiredFor)
	if p.Identity != nil {
		identityRules := &IdentityRules{Rules: append([]IdentityRule(nil), p.Identity.Rules...)}
		for i := range identityRules.Rules {
			identityRules.Rules[i].ActorKinds = sortedCopy(identityRules.Rules[i].ActorKinds)
			identityRules.Rules[i].Roles = sortedCopy(identityRules.Rules[i].Roles)
			identityRules.Rules[i].RequiredScopes = normalizeScopes(identityRules.Rules[i].RequiredScopes)
		}
		sort.Slice(identityRules.Rules, func(i, j int) bool {
			return identityRules.Rules[i].ActionID < identityRules.Rules[j].ActionID
		})
		result.Identity = identityRules
	}
	result.Enforcement.OptionalControls = append([]Control(nil), p.Enforcement.OptionalControls...)
	sort.Slice(result.Enforcement.OptionalControls, func(i, j int) bool {
		return result.Enforcement.OptionalControls[i] < result.Enforcement.OptionalControls[j]
	})
	if p.Network != nil {
		network := *p.Network
		network.AllowedDomains = normalizeDomains(p.Network.AllowedDomains)
		network.DeniedDomains = normalizeDomains(p.Network.DeniedDomains)
		result.Network = &network
	}
	if p.Credentials != nil {
		credentials := *p.Credentials
		credentials.AllowedScopes = normalizeScopes(p.Credentials.AllowedScopes)
		credentials.DeniedScopes = normalizeScopes(p.Credentials.DeniedScopes)
		result.Credentials = &credentials
	}
	if p.Subagents != nil {
		subagents := *p.Subagents
		subagents.MaxConcurrent = cloneInt64(p.Subagents.MaxConcurrent)
		subagents.MaxTotal = cloneInt64(p.Subagents.MaxTotal)
		result.Subagents = &subagents
	}
	if p.Budget != nil {
		budget := *p.Budget
		budget.MaxDurationSeconds = cloneInt64(p.Budget.MaxDurationSeconds)
		budget.MaxCostUSD = cloneFloat64(p.Budget.MaxCostUSD)
		budget.MaxToolCalls = cloneInt64(p.Budget.MaxToolCalls)
		result.Budget = &budget
	}
	if p.Termination != nil {
		termination := *p.Termination
		termination.KillOn = append([]Trigger(nil), p.Termination.KillOn...)
		sort.Slice(termination.KillOn, func(i, j int) bool { return termination.KillOn[i] < termination.KillOn[j] })
		result.Termination = &termination
	}
	return result
}

func budgetDecision(p AgentPolicy, event Event, state BudgetState) (Decision, bool) {
	if p.Budget == nil {
		return Decision{}, false
	}
	if p.Budget.MaxDurationSeconds != nil && *p.Budget.MaxDurationSeconds <= math.MaxInt64/int64(time.Second) && state.Elapsed >= time.Duration(*p.Budget.MaxDurationSeconds)*time.Second {
		return Decision{Outcome: Deny, ReasonCode: ReasonDurationBudgetExceeded, Detail: "run duration budget is exhausted"}, true
	}
	if p.Budget.MaxCostUSD != nil && state.CostUSD >= *p.Budget.MaxCostUSD {
		return Decision{Outcome: Deny, ReasonCode: ReasonCostBudgetExceeded, Detail: "run cost budget is exhausted"}, true
	}
	if event.Category == ToolCallRequested && p.Budget.MaxToolCalls != nil && state.ToolCalls >= *p.Budget.MaxToolCalls {
		return Decision{Outcome: Deny, ReasonCode: ReasonToolCallBudgetExceeded, Detail: "run tool-call budget is exhausted"}, true
	}
	return Decision{}, false
}

func shouldTerminate(p AgentPolicy, reason ReasonCode) bool {
	if p.Termination == nil || reason == "" {
		return false
	}
	var trigger Trigger
	switch reason {
	case ReasonToolDenied, ReasonToolNotAllowlisted:
		trigger = TriggerDeniedToolCall
	case ReasonDomainDenied, ReasonDomainNotAllowed:
		trigger = TriggerDeniedDomainAccess
	case ReasonDurationBudgetExceeded, ReasonCostBudgetExceeded, ReasonToolCallBudgetExceeded, ReasonConcurrentSubagentsExceeded, ReasonTotalSubagentsExceeded:
		trigger = TriggerBudgetExhausted
	}
	return trigger != "" && shouldTerminateTrigger(p, trigger)
}

func shouldTerminateTrigger(p AgentPolicy, trigger Trigger) bool {
	if p.Termination == nil || trigger == "" {
		return false
	}
	for _, candidate := range p.Termination.KillOn {
		if candidate == trigger || candidate == TriggerPolicyViolation {
			return true
		}
	}
	return false
}

func isBudgetReason(reason ReasonCode) bool {
	switch reason {
	case ReasonDurationBudgetExceeded, ReasonCostBudgetExceeded, ReasonToolCallBudgetExceeded, ReasonConcurrentSubagentsExceeded, ReasonTotalSubagentsExceeded:
		return true
	default:
		return false
	}
}

var (
	policyIDPattern      = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
	identifierPattern    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$`)
	correlationIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:@/-]{0,255}$`)
	traceIDPattern       = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$`)
	toolPattern          = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:/-]{0,127}$`)
	actionPattern        = regexp.MustCompile(`^[a-z][a-z0-9_.:/-]{0,127}$`)
	actorKindPattern     = regexp.MustCompile(`^(human|agent|service)$`)
)

func validateSafeIdentityLabels(name string, values []string, pattern *regexp.Regexp, caseSensitive bool) error {
	if err := validateIdentifiers(name, values, pattern, caseSensitive); err != nil {
		return err
	}
	for _, value := range values {
		if identity.IsCredentialLike(value) {
			return fmt.Errorf("%s contains a credential-like identifier", name)
		}
	}
	return nil
}

func validateIdentifiers(name string, values []string, pattern *regexp.Regexp, caseSensitive bool) error {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		canonical := strings.TrimSpace(value)
		if !caseSensitive {
			canonical = strings.ToLower(canonical)
		}
		if value != canonical || !pattern.MatchString(canonical) {
			return fmt.Errorf("%s contains invalid identifier %q", name, value)
		}
		if _, duplicate := seen[canonical]; duplicate {
			return fmt.Errorf("%s repeats identifier %q", name, value)
		}
		seen[canonical] = struct{}{}
	}
	return nil
}

func validateDomains(name string, values []string) error {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		domain, err := normalizeDomain(value)
		if err != nil || domain != value {
			return fmt.Errorf("%s contains invalid or non-canonical hostname %q", name, value)
		}
		if _, duplicate := seen[domain]; duplicate {
			return fmt.Errorf("%s repeats hostname %q", name, value)
		}
		seen[domain] = struct{}{}
	}
	return nil
}

func normalizeDomain(value string) (string, error) {
	domain := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(value), "."))
	if domain == "" || len(domain) > 253 || net.ParseIP(domain) != nil || strings.ContainsAny(domain, ":/@*%") {
		return "", fmt.Errorf("invalid hostname")
	}
	for _, label := range strings.Split(domain, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return "", fmt.Errorf("invalid hostname")
		}
		for _, r := range label {
			if !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9') && r != '-' {
				return "", fmt.Errorf("invalid hostname")
			}
		}
	}
	return domain, nil
}

func normalizeDomains(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		normalized, _ := normalizeDomain(value)
		result = append(result, normalized)
	}
	sort.Strings(result)
	return result
}

func normalizeScopes(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		result = append(result, strings.ToLower(value))
	}
	sort.Strings(result)
	return result
}

func normalizeScope(value string) string { return strings.ToLower(strings.TrimSpace(value)) }

func validateNonNegative(name string, value *int64) error {
	if value != nil && *value < 0 {
		return fmt.Errorf("%s must be non-negative", name)
	}
	return nil
}

func validateUniqueTriggers(values []Trigger) error {
	seen := make(map[Trigger]struct{}, len(values))
	for _, trigger := range values {
		if !validTrigger(trigger) {
			return fmt.Errorf("termination.kill_on contains unsupported trigger %q", trigger)
		}
		if _, duplicate := seen[trigger]; duplicate {
			return fmt.Errorf("termination.kill_on repeats trigger %q", trigger)
		}
		seen[trigger] = struct{}{}
	}
	return nil
}

func validTrigger(trigger Trigger) bool {
	switch trigger {
	case TriggerDeniedToolCall, TriggerDeniedDomainAccess, TriggerBudgetExhausted, TriggerPolicyViolation, TriggerUnexpectedExit:
		return true
	default:
		return false
	}
}

func validControl(control Control) bool {
	switch control {
	case ControlToolInterception, ControlApprovalPauseResume, ControlDurationBudget, ControlCostBudget, ControlToolCallBudget, ControlSubagentLimits, ControlNetworkRestriction, ControlCredentialScope, ControlRunTermination, ControlRuntimeEvents:
		return true
	default:
		return false
	}
}

func validEventCategory(category EventCategory) bool {
	switch category {
	case AgentRunStarted, ToolCallRequested, ToolCallCompleted, NetworkAccessRequested, NetworkAccessCompleted, CredentialAccessRequested, SubAgentSpawnRequested, BudgetUpdated, AgentRunCompleted, AgentRunFailed, AgentRunTerminated, EventUnexpectedToolCall, EventUnexpectedNetwork, EventCredentialAccess, EventRetryStorm, EventProgressStall, EventSubagentSpawnSpike, EventBudgetExhaustion, EventPolicyViolation, EventUnexpectedTermination:
		return true
	default:
		return false
	}
}

func contains[T comparable](values []T, target T) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func sortedCopy(values []string) []string {
	result := append([]string(nil), values...)
	sort.Strings(result)
	return result
}

func cloneInt64(value *int64) *int64 {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func cloneFloat64(value *float64) *float64 {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func (p AgentPolicy) configuredControlList() []Control {
	controls := make([]Control, 0, len(p.configuredControls()))
	for control := range p.configuredControls() {
		controls = append(controls, control)
	}
	sort.Slice(controls, func(i, j int) bool { return controls[i] < controls[j] })
	return controls
}

func IsKnownEventCategory(category EventCategory) bool { return validEventCategory(category) }

func IsKnownControl(control Control) bool { return validControl(control) }

func IsKnownOutcome(outcome Outcome) bool {
	switch outcome {
	case Allow, Deny, RequireApproval, AllowWithWarning:
		return true
	default:
		return false
	}
}

func IsKnownReasonCode(reason ReasonCode) bool {
	switch reason {
	case ReasonToolDenied, ReasonToolNotAllowlisted, ReasonApprovalRequired,
		ReasonDomainDenied, ReasonDomainNotAllowed, ReasonCredentialDenied,
		ReasonCredentialNotAllowed, ReasonDurationBudgetExceeded, ReasonCostBudgetExceeded,
		ReasonToolCallBudgetExceeded, ReasonConcurrentSubagentsExceeded,
		ReasonTotalSubagentsExceeded, ReasonInvalidEvent, ReasonInvalidSnapshot, ReasonInvalidBudgetState,
		ReasonUnexpectedTermination, ReasonIdentityRequired, ReasonIdentityPolicyRuleMissing,
		ReasonIdentityPolicyUnsupportedVersion, ReasonIdentityPolicyDenied, ReasonDelegationScopeMissing,
		ReasonDelegationExpired:
		return true
	default:
		return false
	}
}
