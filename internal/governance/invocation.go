// Package governance composes run policy, audit, and invocation boundaries for
// a governed tool call. It does not start a runtime or contact external tools.
package governance

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/AllenMuu/skill-manager/internal/invocation"
	"github.com/AllenMuu/skill-manager/internal/policy"
	"github.com/AllenMuu/skill-manager/internal/run"
)

type InvocationRequest struct {
	RunID       string
	Event       policy.Event
	Requirement policy.IdentityRequirement
	Budget      policy.BudgetState
	At          time.Time
}

type InvocationOutcome struct {
	Decision        policy.Decision
	Evaluation      policy.Evaluation
	RequestAudit    run.AuditRecord
	CompletionAudit *run.AuditRecord
	Result          invocation.Result
}

type Invoker struct {
	Runs    *run.Manager
	Adapter invocation.InvocationAdapter
}

// Invoke records a policy decision before dispatch and records a correlated
// completion outcome after adapter success, failure, or capability blocking.
// Non-ALLOW decisions never reach the adapter unless the action was approved.
func (i Invoker) Invoke(ctx context.Context, request InvocationRequest) (InvocationOutcome, error) {
	if i.Runs == nil || i.Adapter == nil {
		return InvocationOutcome{}, errors.New("governed invocation requires a run manager and adapter")
	}
	if ctx == nil {
		return InvocationOutcome{}, errors.New("governed invocation requires a context")
	}
	if err := ctx.Err(); err != nil {
		return InvocationOutcome{}, fmt.Errorf("governed invocation context is canceled: %w", err)
	}
	if request.Event.Category != policy.ToolCallRequested || request.Event.ActionID == "" || !request.Requirement.Required || request.Requirement.ActionID != request.Event.ActionID {
		return InvocationOutcome{}, errors.New("governed invocation requires a tool request with a matching identity-bound action")
	}
	if request.At.IsZero() {
		return InvocationOutcome{}, errors.New("governed invocation requires an evaluation time")
	}
	runRecord, err := i.Runs.Store.Get(request.RunID)
	if err != nil {
		return InvocationOutcome{}, err
	}
	if !runRecord.ExecutionReady() {
		return InvocationOutcome{}, fmt.Errorf("run %q is %s and cannot dispatch an invocation", runRecord.ID, runRecord.Status)
	}
	if request.Event.ApprovalID != "" {
		if err := i.validateApproval(&request); err != nil {
			return InvocationOutcome{}, err
		}
	}
	decision, evaluation, requestAudit, err := i.Runs.EvaluateAndRecordWithIdentity(request.RunID, request.Event, request.Budget, request.Requirement, request.At)
	outcome := InvocationOutcome{Decision: decision, Evaluation: evaluation, RequestAudit: requestAudit}
	if err != nil {
		return outcome, err
	}
	approved := request.Event.ApprovalID != "" && decision.Outcome == policy.RequireApproval
	if decision.Outcome != policy.Allow && decision.Outcome != policy.AllowWithWarning && !approved {
		return outcome, nil
	}
	authorizedDecision := decision.Outcome
	if approved {
		authorizedDecision = policy.Allow
	}
	runRecord, err = i.Runs.Store.Get(request.RunID)
	if err != nil {
		return outcome, err
	}
	if !runRecord.ExecutionReady() {
		return outcome, fmt.Errorf("run %q is %s and cannot dispatch an invocation", runRecord.ID, runRecord.Status)
	}
	lineage := invocation.Lineage{RunID: runRecord.ID, PolicySnapshotHash: runRecord.AppliedPolicy().Hash}
	if runRecord.Identity.Actor != nil && runRecord.Identity.Delegation != nil {
		lineage.ActorID = runRecord.Identity.Actor.ID
		lineage.DelegationID = runRecord.Identity.Delegation.ID
	}
	lineage.ApprovalID = request.Event.ApprovalID
	callContext, err := invocation.NewContext(lineage, requestAudit.TraceID)
	if err != nil {
		return outcome, fmt.Errorf("build invocation context: %w", err)
	}
	dispatchRequest := invocation.Request{
		ActionID: request.Event.ActionID, Tool: request.Event.Tool, RequireContextPropagation: true,
	}
	prepared, err := invocation.PrepareDispatch(i.Adapter, dispatchRequest, lineage, callContext)
	if err != nil {
		status := run.InvocationFailed
		var unsupported *invocation.UnsupportedPropagationError
		if errors.As(err, &unsupported) {
			status = run.InvocationBlocked
		}
		completionAudit, auditErr := i.recordCompletion(request, requestAudit, authorizedDecision, status)
		outcome.CompletionAudit = completionAudit
		if auditErr != nil {
			return outcome, errors.Join(err, fmt.Errorf("record invocation preflight failure: %w", auditErr))
		}
		return outcome, err
	}
	if err := ctx.Err(); err != nil {
		completionAudit, auditErr := i.recordCompletion(request, requestAudit, authorizedDecision, run.InvocationBlocked)
		outcome.CompletionAudit = completionAudit
		if auditErr != nil {
			return outcome, errors.Join(err, fmt.Errorf("record canceled invocation: %w", auditErr))
		}
		return outcome, err
	}
	// Adapter preflight may overlap an independent lifecycle mutation. Authorize
	// dispatch against current durable readiness after it completes; approval
	// consumption below retains its additional atomic readiness/single-use check.
	dispatchRecord, dispatchErr := i.Runs.Store.Get(request.RunID)
	if dispatchErr == nil && (!dispatchRecord.ExecutionReady() || dispatchRecord.AppliedPolicy().Hash != requestAudit.PolicyHash) {
		dispatchErr = fmt.Errorf("run %q execution is unavailable before dispatch", request.RunID)
	}
	if dispatchErr != nil {
		completionAudit, auditErr := i.recordCompletion(request, requestAudit, authorizedDecision, run.InvocationBlocked)
		outcome.CompletionAudit = completionAudit
		if auditErr != nil {
			return outcome, errors.Join(dispatchErr, fmt.Errorf("record unavailable invocation: %w", auditErr))
		}
		return outcome, dispatchErr
	}
	if request.Event.ApprovalID != "" {
		if _, err := i.Runs.Store.ConsumeApprovalForInvocation(request.Event.ApprovalID, request.RunID, requestAudit.ID, request.At); err != nil {
			consumeErr := fmt.Errorf("consume invocation approval: %w", err)
			completionAudit, auditErr := i.recordCompletion(request, requestAudit, authorizedDecision, run.InvocationBlocked)
			outcome.CompletionAudit = completionAudit
			if auditErr != nil {
				return outcome, errors.Join(consumeErr, fmt.Errorf("record blocked invocation: %w", auditErr))
			}
			return outcome, consumeErr
		}
	}
	result, err := prepared.Invoke(ctx)
	if err != nil {
		status := run.InvocationFailed
		dispatchErr := err
		var notDispatched *invocation.NotDispatchedError
		if errors.As(err, &notDispatched) {
			status = run.InvocationBlocked
			if request.Event.ApprovalID != "" {
				if releaseErr := i.Runs.Store.ReleaseApprovalConsumption(request.Event.ApprovalID, request.RunID, requestAudit.ID); releaseErr != nil {
					dispatchErr = errors.Join(dispatchErr, fmt.Errorf("release unused invocation approval: %w", releaseErr))
				}
			}
		}
		var unsupported *invocation.UnsupportedPropagationError
		if errors.As(err, &unsupported) {
			status = run.InvocationBlocked
		}
		failedAudit, auditErr := i.recordCompletion(request, requestAudit, authorizedDecision, status)
		outcome.CompletionAudit = failedAudit
		if auditErr != nil {
			return outcome, errors.Join(dispatchErr, fmt.Errorf("record invocation failure: %w", auditErr))
		}
		return outcome, dispatchErr
	}
	outcome.Result = result
	completionAudit, err := i.recordCompletion(request, requestAudit, authorizedDecision, run.InvocationSucceeded)
	if err != nil {
		return outcome, fmt.Errorf("record invocation completion after adapter returned: %w", err)
	}
	outcome.Decision = policy.Decision{Outcome: policy.Allow}
	outcome.CompletionAudit = completionAudit
	return outcome, nil
}

func (i Invoker) validateApproval(request *InvocationRequest) error {
	approval, err := i.Runs.Store.GetApproval(request.Event.ApprovalID)
	if err != nil {
		return err
	}
	if approval.RunID != request.RunID || approval.Status != run.ApprovalApproved || approval.DecidedBy == nil {
		return errors.New("invocation approval is not an approved decision for this run")
	}
	if !approval.ConsumedAt.IsZero() || approval.ConsumedByAuditID != "" {
		return fmt.Errorf("invocation approval %q has already been consumed", approval.ID)
	}
	runRecord, err := i.Runs.Store.Get(request.RunID)
	if err != nil {
		return err
	}
	if !runRecord.ExecutionReady() {
		return fmt.Errorf("run %q is %s and cannot dispatch an approved invocation", runRecord.ID, runRecord.Status)
	}
	events, err := i.Runs.Store.Events(request.RunID)
	if err != nil {
		return err
	}
	for _, event := range events {
		if event.ID != approval.RequestAuditID {
			continue
		}
		if event.Category != policy.ToolCallRequested || event.Decision != policy.RequireApproval || event.ActionID != request.Event.ActionID || event.Tool != request.Event.Tool || event.ActionType != request.Event.ActionType || event.PolicyHash != runRecord.AppliedPolicy().Hash || event.TraceID == "" {
			return errors.New("invocation approval does not match its persisted action and policy request")
		}
		if request.Event.TraceID != "" && request.Event.TraceID != event.TraceID {
			return errors.New("invocation trace does not match the approved request")
		}
		request.Event.TraceID = event.TraceID
		return nil
	}
	return fmt.Errorf("approval request audit %q was not found for run %q", approval.RequestAuditID, request.RunID)
}

func (i Invoker) recordCompletion(request InvocationRequest, requestAudit run.AuditRecord, decision policy.Outcome, status run.InvocationStatus) (*run.AuditRecord, error) {
	completedAt := request.At.Add(time.Nanosecond)
	completion := policy.Event{
		Category: policy.ToolCallCompleted, Tool: request.Event.Tool, ActionType: request.Event.ActionType,
		ActionID: request.Event.ActionID, TraceID: requestAudit.TraceID, ApprovalID: request.Event.ApprovalID,
		RequestAuditID: requestAudit.ID, ObservedDecision: decision,
	}
	completionDecision, _, completionAudit, err := i.Runs.EvaluateAndRecordInvocation(request.RunID, completion, request.Budget, status, completedAt)
	if err != nil {
		return nil, err
	}
	if completionDecision.Outcome != decision {
		return nil, fmt.Errorf("completion recorded %s, want %s", completionDecision.Outcome, decision)
	}
	return &completionAudit, nil
}
