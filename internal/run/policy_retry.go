package run

import (
	"context"
	"errors"
	"reflect"
	"time"

	"github.com/AllenMuu/skill-manager/internal/enforcement"
	"github.com/AllenMuu/skill-manager/internal/invocation"
	"github.com/AllenMuu/skill-manager/internal/policy"
)

// PolicyRetryAdapter is host configuration. It prepares only the persisted
// denied action; preparation cannot dispatch. Dispatch returns NotDispatchedError
// only if no effect occurred; any other error leaves a consumed unknown action.
type PolicyRetryAdapter interface {
	Prepare(context.Context, policy.Event, invocation.InvocationContext) (PreparedPolicyRetry, error)
}
type PreparedPolicyRetry interface{ Dispatch(context.Context) error }
type PolicyRetry struct {
	ApprovalID        string `json:"approval_id,omitempty"`
	ID                string `json:"id"`
	State             string `json:"state"`
	RequestAuditID    string `json:"request_audit_id,omitempty"`
	CompletionAuditID string `json:"completion_audit_id,omitempty"`
}

// PolicyRetryBinding captures what the host adapter prepared. Both claims compare
// it with persisted authority inside the transaction before making any change.
type PolicyRetryBinding struct {
	Mutation enforcement.PolicyMutation
	Action   policy.Event
	Lineage  invocation.Lineage
}

var ErrPolicyRetryStalePreparation = errors.New("policy retry preparation is stale")

type PolicyRetryStore interface {
	BeginPolicyRetryResume(context.Context, string, policy.BudgetState, *PermissionProposalDecider, PolicyRetryBinding) (Record, bool, error)
	BeginPolicyRetryDispatch(context.Context, string, policy.BudgetState, *PermissionProposalDecider, PolicyRetryBinding) (Record, error)
	FinishPolicyRetry(string, string, string) (Record, error)
}

// ConfigurePolicyRevisions installs trusted delivery ports once. Runtime request
// fields cannot replace these ports. The adapters must not re-enter Store while
// operator authority is resolving inside its transaction.
func (c *Coordinator) ConfigurePolicyRevisions(d *PermissionProposalDecider, event PolicyEventAdapter, retry PolicyRetryAdapter) error {
	c.policyMu.Lock()
	defer c.policyMu.Unlock()
	if c.policyConfigured {
		return errors.New("policy ports already configured")
	}
	if d == nil {
		return errors.New("trusted proposal decider required")
	}
	c.policyDecider = d
	c.policyEvents = event
	c.policyRetry = retry
	c.policyConfigured = true
	return nil
}
func policyRetryEvent(p PermissionProposal) policy.Event {
	return policy.Event{Category: p.Denial.Category, ActionID: p.Denial.ActionID, ActionType: p.Denial.ActionType, Tool: p.Denial.Tool, Domain: p.Denial.Domain, CredentialScope: p.Denial.CredentialScope, TraceID: p.Denial.TraceID, RunID: p.RunID}
}
func (d *PermissionProposalDecider) validateRetryLocked(ctx context.Context, db *database, r Record, budget policy.BudgetState, approvalID string, requestingApproval bool) (PermissionProposal, policy.Event, error) {
	if r.PolicyRevisions == nil {
		return PermissionProposal{}, policy.Event{}, errors.New("revision missing")
	}
	m := r.PolicyRevisions.Mutations[len(r.PolicyRevisions.Mutations)-1]
	p := db.Proposals[m.Request.ProposalID]
	if m.State != "applied" || !samePolicySnapshot(r.AppliedPolicy(), m.Request.Target) || !samePolicySnapshot(r.PolicyRevisions.Desired, m.Request.Target) || !samePolicySnapshot(p.BasePolicy, m.Request.Base) || p.Status != ProposalApproved {
		return p, policy.Event{}, enforcement.ErrUnknown
	}
	decision := p.Decisions[len(p.Decisions)-1]
	if decision.ID != m.Request.DecisionID || decision.Operator == nil || decision.Operator.ReceiptID != m.Request.ReceiptID {
		return p, policy.Event{}, ErrOperatorUnauthorized
	}
	receipt, err := d.authority.ResolveOperator(ctx, operatorRequest(p, ProposalApproved))
	if err != nil || validateOperator(receipt, p, ProposalApproved) != nil || receipt.AuthorityID != decision.Operator.AuthorityID || !reflect.DeepEqual(receipt.Actor.Normalized(), decision.Operator.Actor.Normalized()) {
		return p, policy.Event{}, ErrOperatorUnauthorized
	}
	now := d.clock().UTC()
	if now.IsZero() || now.Before(p.CreatedAt) {
		return p, policy.Event{}, errors.New("invalid retry clock")
	}
	if err = validateLiveProposal(p, r, m.Request.Base, now); err != nil {
		return p, policy.Event{}, err
	}
	if err = ctx.Err(); err != nil {
		return p, policy.Event{}, err
	}
	event := policyRetryEvent(p)
	event.Timestamp = now
	engine := policy.Engine{}
	verdict := engine.EvaluateBefore(r.AppliedPolicy(), event, budget)
	if event.Category == policy.ToolCallRequested {
		verdict = engine.EvaluateBeforeWithIdentity(r.AppliedPolicy(), event, budget, policy.IdentityRequirement{Required: true, ActionID: event.ActionID}, r.ID, r.Identity, now)
	}
	event.ObservedDecision = verdict.Outcome
	event.ReasonCode = verdict.ReasonCode
	if verdict.Outcome == policy.RequireApproval {
		if requestingApproval {
			return p, event, nil
		}
		approval, ok := db.Approvals[approvalID]
		if !ok || approval.RunID != r.ID || approval.Status != ApprovalApproved || approval.DecidedBy == nil || !approval.ConsumedAt.IsZero() {
			return p, event, errors.New("retry requires separate unconsumed action approval")
		}
		request, err := validateApprovalRequestAudit(*db, approval)
		if err != nil || request.PolicyHash != r.AppliedPolicy().Hash || request.Category != event.Category || request.ActionID != event.ActionID || request.ActionType != event.ActionType || request.Tool != event.Tool || request.Domain != event.Domain || request.TraceID != event.TraceID {
			return p, event, errors.New("retry approval does not match exact action and applied revision")
		}
		event.ApprovalID = approvalID
		return p, event, nil
	}
	if approvalID != "" || requestingApproval {
		return p, event, errors.New("one-action approval not required for this retry")
	}
	if verdict.Outcome != policy.Allow && verdict.Outcome != policy.AllowWithWarning {
		return p, event, errors.New("retry requires a fresh allowed decision; one-action approval remains separate")
	}
	return p, event, nil
}
func (s *Store) withRetryAuthority(ctx context.Context, id string, budget policy.BudgetState, d *PermissionProposalDecider, approvalID string, requestingApproval bool, binding *PolicyRetryBinding, apply func(*database, *Record, policy.Event) error) (Record, error) {
	if d == nil || d.store != s {
		return Record{}, errors.New("trusted retry decider does not own store")
	}
	var result Record
	transaction := func(external *policy.Snapshot) error {
		return s.update(func(db *database) error {
			r, ok := db.Runs[id]
			if !ok {
				return errors.New("run not found")
			}
			result = r
			if external != nil && !samePolicySnapshot(*external, r.AppliedPolicy()) {
				return ErrProposalStaleBase
			}
			if binding != nil {
				if r.PolicyRevisions == nil || len(r.PolicyRevisions.Mutations) == 0 {
					return ErrPolicyRetryStalePreparation
				}
				m := r.PolicyRevisions.Mutations[len(r.PolicyRevisions.Mutations)-1]
				p, found := db.Proposals[m.Request.ProposalID]
				lineage := invocation.Lineage{ApprovalID: approvalID, RunID: r.ID, ActorID: r.Identity.Actor.ID, DelegationID: r.Identity.Delegation.ID, PolicySnapshotHash: r.AppliedPolicy().Hash}
				if !found || !samePolicyMutation(binding.Mutation, m.Request) || !samePolicySnapshot(binding.Mutation.Target, r.AppliedPolicy()) || !reflect.DeepEqual(binding.Action, policyRetryEvent(p)) || !reflect.DeepEqual(binding.Lineage, lineage) {
					return ErrPolicyRetryStalePreparation
				}
			}
			_, event, err := d.validateRetryLocked(ctx, db, r, budget, approvalID, requestingApproval)
			if err != nil {
				return err
			}
			if err = apply(db, &r, event); err != nil {
				return err
			}
			db.Runs[id] = r
			result = r
			return nil
		})
	}
	var err error
	if d.current != nil {
		err = d.current.WithCurrentPolicy(ctx, id, func(base policy.Snapshot) error { return transaction(&base) })
	} else {
		err = transaction(nil)
	}
	return result, err
}
func (s *Store) BeginPolicyRetryResume(ctx context.Context, id string, budget policy.BudgetState, d *PermissionProposalDecider, binding PolicyRetryBinding) (Record, bool, error) {
	approvalID := binding.Lineage.ApprovalID
	created := false
	r, err := s.withRetryAuthority(ctx, id, budget, d, approvalID, false, &binding, func(db *database, r *Record, event policy.Event) error {
		m := &r.PolicyRevisions.Mutations[len(r.PolicyRevisions.Mutations)-1]
		if m.Retry != nil {
			if m.Retry.State == "not_dispatched" && r.Status == Active && !unresolved(*r) {
				m.RetryAttempts = append(m.RetryAttempts, *m.Retry)
				m.Retry = &PolicyRetry{ID: m.Request.ID + "-retry", State: "resuming", ApprovalID: approvalID}
				return nil
			}
			if m.Retry.ApprovalID != approvalID {
				return errors.New("retry action approval differs from resume authorization")
			}
			if m.Retry.State != "resuming" {
				return errors.New("policy retry already consumed")
			}
			if unresolved(*r) {
				return enforcement.ErrUnknown
			}
			if r.Status != Active {
				return enforcement.ErrUnknown
			}
			return nil
		}
		if r.Status != Paused || r.ExternalRuntime == nil || unresolved(*r) {
			return errors.New("retry requires confirmed paused runtime")
		}
		retryID := m.Request.ID + "-retry"
		opID := retryID + "-resume"
		now := time.Now().UTC()
		m.Retry = &PolicyRetry{ID: retryID, State: "resuming", ApprovalID: approvalID}
		r.ExternalRuntime.Operations = append(r.ExternalRuntime.Operations, LifecycleOperation{ID: opID, Request: lifecycleRequest(*r, opID, "resume"), Outcome: "pending", CreatedAt: now, UpdatedAt: now})
		r.UpdatedAt = now
		created = true
		return nil
	})
	return r, created, err
}
func (s *Store) BeginPolicyRetryDispatch(ctx context.Context, id string, budget policy.BudgetState, d *PermissionProposalDecider, binding PolicyRetryBinding) (Record, error) {
	approvalID := binding.Lineage.ApprovalID
	return s.withRetryAuthority(ctx, id, budget, d, approvalID, false, &binding, func(db *database, r *Record, event policy.Event) error {
		m := &r.PolicyRevisions.Mutations[len(r.PolicyRevisions.Mutations)-1]
		if m.Retry == nil || m.Retry.State != "resuming" || m.Retry.ApprovalID != approvalID || r.Status != Active || r.ExternalRuntime.ObservedState != "active" || unresolved(*r) {
			return enforcement.ErrUnknown
		}
		audit, err := auditFor(*r, event, policy.Decision{Outcome: event.ObservedDecision, ReasonCode: event.ReasonCode}, "")
		if err != nil {
			return err
		}
		if event.Category == policy.CredentialAccessRequested {
			audit.CredentialScope = event.CredentialScope
		}
		db.Events = append(db.Events, audit)
		if approvalID != "" {
			approval := db.Approvals[approvalID]
			approval.ConsumedAt = event.Timestamp
			approval.ConsumedByAuditID = audit.ID
			db.Approvals[approvalID] = approval
		}
		m.Retry.State = "dispatching"
		m.Retry.RequestAuditID = audit.ID
		return nil
	})
}
func (s *Store) FinishPolicyRetry(id, retryID, state string) (Record, error) {
	var result Record
	err := s.update(func(db *database) error {
		r, ok := db.Runs[id]
		if !ok || r.PolicyRevisions == nil {
			return errors.New("retry not found")
		}
		m := &r.PolicyRevisions.Mutations[len(r.PolicyRevisions.Mutations)-1]
		if m.Retry == nil || m.Retry.ID != retryID || m.Retry.State != "dispatching" {
			return errors.New("retry dispatch was not claimed")
		}
		if state != "succeeded" && state != "unknown" && state != "not_dispatched" {
			return errors.New("invalid retry result")
		}
		p := db.Proposals[m.Request.ProposalID]
		event := policyRetryEvent(p)
		if m.Retry.ApprovalID != "" {
			approval := db.Approvals[m.Retry.ApprovalID]
			event.ApprovalID = approval.ID
			event.ApproverID = approval.DecidedBy.ID
			if state == "not_dispatched" {
				approval.ConsumedAt = time.Time{}
				approval.ConsumedByAuditID = ""
				db.Approvals[approval.ID] = approval
			}
		}
		event.Timestamp = time.Now().UTC()
		request, found := auditRecordByID(db.Events, m.Retry.RequestAuditID)
		if !found {
			return errors.New("retry request missing")
		}
		if event.Timestamp.Before(request.Timestamp) {
			event.Timestamp = request.Timestamp.Add(time.Nanosecond)
		}
		switch event.Category {
		case policy.ToolCallRequested:
			event.Category = policy.ToolCallCompleted
		case policy.NetworkAccessRequested:
			event.Category = policy.NetworkAccessCompleted
		case policy.CredentialAccessRequested:
			event.Category = policy.EventCredentialAccess
		}
		if event.Category == policy.ToolCallCompleted || event.Category == policy.NetworkAccessCompleted {
			event.RequestAuditID = request.ID
		}
		audit, err := auditFor(r, event, policy.Decision{Outcome: policy.Allow}, "")
		if err != nil {
			return err
		}
		if event.Category == policy.ToolCallCompleted || event.Category == policy.NetworkAccessCompleted {
			audit.Result = policy.Outcome(InvocationSucceeded)
			if state != "succeeded" {
				audit.Result = policy.Outcome(InvocationFailed)
				if state == "not_dispatched" {
					audit.Result = policy.Outcome(InvocationBlocked)
				}
			}
		}
		if event.Category == policy.EventCredentialAccess {
			audit.CredentialScope = event.CredentialScope
		}
		db.Events = append(db.Events, audit)
		m.Retry.State = state
		m.Retry.CompletionAuditID = audit.ID
		db.Runs[id] = r
		result = r
		return nil
	})
	return result, err
}
func (c *Coordinator) RetryPolicyAction(ctx context.Context, id string, budget policy.BudgetState, approvalIDs ...string) (Record, error) {
	if err := checkContext(ctx); err != nil {
		return Record{}, err
	}
	c.policyMu.Lock()
	d, adapter := c.policyDecider, c.policyRetry
	c.policyMu.Unlock()
	if d == nil || adapter == nil {
		return Record{}, enforcement.ErrUnsupported
	}
	store, ok := c.store.(PolicyRetryStore)
	if !ok {
		return Record{}, enforcement.ErrUnsupported
	}
	r, err := c.store.Get(id)
	if err != nil {
		return r, err
	}
	if r.PolicyRevisions == nil {
		return r, errors.New("revision missing")
	}
	m := r.PolicyRevisions.Mutations[len(r.PolicyRevisions.Mutations)-1]
	p, err := d.store.GetPermissionProposal(m.Request.ProposalID)
	if err != nil {
		return r, err
	}
	event := policyRetryEvent(p)
	approvalID, err := policyRetryApprovalID(approvalIDs)
	if err != nil {
		return r, err
	}
	lineage := invocation.Lineage{ApprovalID: approvalID, RunID: r.ID, ActorID: r.Identity.Actor.ID, DelegationID: r.Identity.Delegation.ID, PolicySnapshotHash: r.AppliedPolicy().Hash}
	callContext, err := invocation.NewContext(lineage, event.TraceID)
	if err != nil {
		return r, err
	}
	mutation, err := clonePolicyMutation(m.Request)
	if err != nil {
		return r, err
	}
	binding := PolicyRetryBinding{Mutation: mutation, Action: event, Lineage: lineage}
	prepared, err := adapter.Prepare(ctx, event, callContext)
	if err != nil {
		return r, err
	}
	if prepared == nil {
		return r, errors.New("retry adapter returned no prepared action")
	}
	if err = ctx.Err(); err != nil {
		return r, err
	}
	provider, err := c.provider(r)
	if err != nil {
		return r, err
	}
	if _, ok := provider.(enforcement.PauseResumer); !ok {
		return r, enforcement.ErrUnsupported
	}
	r, created, err := store.BeginPolicyRetryResume(ctx, id, budget, d, binding)
	if err != nil {
		return r, err
	}
	if created {
		r, err = c.mutate(ctx, provider, r)
		if err != nil {
			return r, err
		}
	}
	r, err = store.BeginPolicyRetryDispatch(ctx, id, budget, d, binding)
	if err != nil {
		return r, err
	}
	retry := r.PolicyRevisions.Mutations[len(r.PolicyRevisions.Mutations)-1].Retry
	dispatchErr := prepared.Dispatch(ctx)
	state := "succeeded"
	if dispatchErr != nil {
		state = "unknown"
		var notDispatched *invocation.NotDispatchedError
		if errors.As(dispatchErr, &notDispatched) {
			state = "not_dispatched"
		}
	}
	r, err = store.FinishPolicyRetry(id, retry.ID, state)
	if err != nil {
		return r, errors.Join(dispatchErr, err)
	}
	if dispatchErr != nil {
		return r, enforcement.ErrUnknown
	}
	return r, nil
}

func policyRetryApprovalID(ids []string) (string, error) {
	if len(ids) > 1 {
		return "", errors.New("one exact action approval may be selected")
	}
	if len(ids) == 0 || (len(ids) == 1 && ids[0] == "") {
		return "", nil
	}
	if err := validateID("approval", ids[0]); err != nil {
		return "", err
	}
	return ids[0], nil
}
func (c *Coordinator) RequestPolicyRetryApproval(ctx context.Context, id string, budget policy.BudgetState) (Approval, error) {
	if err := checkContext(ctx); err != nil {
		return Approval{}, err
	}
	c.policyMu.Lock()
	d := c.policyDecider
	c.policyMu.Unlock()
	if d == nil {
		return Approval{}, enforcement.ErrUnsupported
	}
	var result Approval
	_, err := d.store.withRetryAuthority(ctx, id, budget, d, "", true, nil, func(db *database, r *Record, event policy.Event) error {
		if r.Status != Paused || unresolved(*r) {
			return errors.New("approval requires confirmed paused retry")
		}
		for _, a := range db.Approvals {
			if a.RunID != r.ID {
				continue
			}
			request, found := auditRecordByID(db.Events, a.RequestAuditID)
			if found && request.PolicyHash == r.AppliedPolicy().Hash && request.ActionID == event.ActionID && request.Tool == event.Tool && request.ActionType == event.ActionType && request.Category == event.Category && request.Domain == event.Domain && request.TraceID == event.TraceID {
				result = a
				return nil
			}
		}
		audit, err := auditFor(*r, event, policy.Decision{Outcome: policy.RequireApproval, ReasonCode: policy.ReasonApprovalRequired}, "")
		if err != nil {
			return err
		}
		approvalID, err := newID("approval")
		if err != nil {
			return err
		}
		result = Approval{ID: approvalID, RunID: r.ID, RequestAuditID: audit.ID, Category: event.Category, Tool: event.Tool, Domain: event.Domain, ActionType: event.ActionType, ReasonCode: policy.ReasonApprovalRequired, Status: ApprovalPending, RequestedAt: event.Timestamp}
		db.Events = append(db.Events, audit)
		db.Approvals[approvalID] = result
		return nil
	})
	return result, err
}
