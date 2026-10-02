package run_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AllenMuu/skill-manager/internal/identity"
	"github.com/AllenMuu/skill-manager/internal/policy"
	"github.com/AllenMuu/skill-manager/internal/run"
)

type retryFixture struct {
	store    *run.Store
	manager  *run.Manager
	record   run.Record
	original run.AuditRecord
	approval run.Approval
	action   policy.Event
	at       time.Time
}

func prepareApprovedRetry(t *testing.T) retryFixture {
	t.Helper()
	return prepareRetryApproval(t, run.ApprovalApproved)
}

func prepareRetryApproval(t *testing.T, status run.ApprovalStatus) retryFixture {
	t.Helper()
	store, err := run.NewStore(filepath.Join(t.TempDir(), "runs"))
	if err != nil {
		t.Fatal(err)
	}
	manager, err := run.NewManager(store)
	if err != nil {
		t.Fatal(err)
	}
	controller := &mockController{capabilities: map[policy.Control]bool{
		policy.ControlToolInterception: true, policy.ControlRuntimeEvents: true, policy.ControlApprovalPauseResume: true,
	}, confirmPause: true, confirmResolve: true}
	now := time.Now().UTC()
	configured, err := policy.Load(strings.NewReader("version: v1\nkind: agent-policy\nid: retry-policy\nname: Retry Policy\ntools:\n  allow: [github.update_file, github.delete_file]\napproval:\n  required_for: [destructive_write, delete_repository]\n"))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := policy.Resolve(configured, now)
	if err != nil {
		t.Fatal(err)
	}
	record, _, err := manager.Start(snapshot, "mock", t.TempDir(), identity.AnonymousSelection(), now, controller)
	if err != nil {
		t.Fatal(err)
	}
	action := policy.Event{Category: policy.ToolCallRequested, Tool: "github.update_file", ActionType: "destructive_write", ActionID: "github.write", TraceID: "trace-approved-retry"}
	decision, _, original, err := manager.EvaluateAndRecord(record.ID, action, policy.BudgetState{}, now.Add(time.Second))
	if err != nil || decision.Outcome != policy.RequireApproval {
		t.Fatalf("request approval: decision=%#v err=%v", decision, err)
	}
	approval, err := manager.RequestApproval(context.Background(), run.Approval{
		RunID: record.ID, RequestAuditID: original.ID, Tool: action.Tool, ActionType: action.ActionType, ReasonCode: decision.ReasonCode,
	}, now.Add(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if status != run.ApprovalPending {
		approval, err = manager.DecideApproval(context.Background(), approval.ID, status, "test decision", testApprover(), now.Add(3*time.Second))
		if err != nil {
			t.Fatal(err)
		}
	}
	action.ApprovalID = approval.ID
	return retryFixture{store: store, manager: manager, record: record, original: original, approval: approval, action: action, at: now.Add(4 * time.Second)}
}

func TestApprovedRetryCompletionPreservesPersistedLineage(t *testing.T) {
	f := prepareApprovedRetry(t)
	_, _, attempt, err := f.manager.EvaluateAndRecord(f.record.ID, f.action, policy.BudgetState{}, f.at)
	if err != nil {
		t.Fatal(err)
	}
	if attempt.ID == f.original.ID {
		t.Fatal("retry must have an independent request audit")
	}
	completion := f.action
	completion.Category, completion.RequestAuditID = policy.ToolCallCompleted, attempt.ID
	decision, evaluation, audit, err := f.manager.EvaluateAndRecordInvocation(f.record.ID, completion, policy.BudgetState{}, run.InvocationSucceeded, f.at.Add(time.Second))
	if err != nil || decision.Outcome != policy.Allow || len(evaluation.Violations) != 0 || audit.Result != policy.Outcome(run.InvocationSucceeded) {
		t.Fatalf("approved retry completion: decision=%#v evaluation=%#v audit=%#v err=%v", decision, evaluation, audit, err)
	}
	reopened, err := run.NewStore(f.store.Root())
	if err != nil {
		t.Fatal(err)
	}
	approval, err := reopened.GetApproval(f.approval.ID)
	if err != nil || approval.RequestAuditID != f.original.ID {
		t.Fatalf("approval lost original request: approval=%#v err=%v", approval, err)
	}
	events, err := reopened.Events(f.record.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range events {
		if event.ID != audit.ID {
			continue
		}
		if event.RequestAuditID != attempt.ID || event.ApprovalID != approval.ID || event.ApproverID != testApprover().ID || event.TraceID != f.original.TraceID || event.PolicyHash != f.record.Policy.Hash {
			t.Fatalf("persisted retry completion lost lineage: %#v", event)
		}
		return
	}
	t.Fatal("retry completion is absent after reopening the store")
}

func TestApprovedRetryCompletionSeparatesPolicyFromExecutionResult(t *testing.T) {
	for _, status := range []run.InvocationStatus{run.InvocationBlocked, run.InvocationFailed} {
		t.Run(string(status), func(t *testing.T) {
			f := prepareApprovedRetry(t)
			_, _, attempt, err := f.manager.EvaluateAndRecord(f.record.ID, f.action, policy.BudgetState{}, f.at)
			if err != nil {
				t.Fatal(err)
			}
			completion := f.action
			completion.Category, completion.RequestAuditID = policy.ToolCallCompleted, attempt.ID
			if status == run.InvocationFailed {
				completion.ApprovalID = "" // Recover the explicit approval from the retry request.
			}
			decision, evaluation, audit, err := f.manager.EvaluateAndRecordInvocation(f.record.ID, completion, policy.BudgetState{}, status, f.at.Add(time.Second))
			if err != nil || decision.Outcome != policy.Allow || len(evaluation.Violations) != 0 || audit.Result != policy.Outcome(status) || audit.ApprovalID != f.approval.ID || audit.ApproverID != testApprover().ID {
				t.Fatalf("authorized completion: decision=%#v evaluation=%#v audit=%#v err=%v", decision, evaluation, audit, err)
			}
			if _, err := run.NewStore(f.store.Root()); err != nil {
				t.Fatalf("completion is unreadable after reopen: %v", err)
			}
		})
	}
}

func TestRetryCompletionDoesNotAuthorizeUnrelatedApproval(t *testing.T) {
	for _, dimension := range []string{"run", "action", "tool", "action_type", "trace", "domain"} {
		t.Run(dimension, func(t *testing.T) {
			f := prepareApprovedRetry(t)
			action := f.action
			action.ApprovalID = ""
			runID := f.record.ID
			switch dimension {
			case "run":
				other, _, err := f.store.Start(f.record.Policy, "mock", t.TempDir(), identity.AnonymousSelection(), map[policy.Control]bool{policy.ControlToolInterception: true, policy.ControlRuntimeEvents: true, policy.ControlApprovalPauseResume: true}, f.at)
				if err != nil {
					t.Fatal(err)
				}
				runID = other.ID
			case "action":
				action.ActionID = "github.other_write"
			case "tool":
				action.Tool = "github.delete_file"
			case "action_type":
				action.ActionType = "delete_repository"
			case "trace":
				action.TraceID = "trace-other-attempt"
			case "domain":
				action.Domain = "other.example.com"
				action.ApprovalID = f.approval.ID
			}
			decision, _, attempt, err := f.manager.EvaluateAndRecord(runID, action, policy.BudgetState{}, f.at)
			if err != nil || decision.Outcome != policy.RequireApproval {
				t.Fatalf("independent attempt: decision=%#v err=%v", decision, err)
			}
			completion := action
			completion.Category, completion.RequestAuditID, completion.ApprovalID = policy.ToolCallCompleted, attempt.ID, f.approval.ID
			decision, evaluation, audit, err := f.manager.EvaluateAndRecordInvocation(runID, completion, policy.BudgetState{}, run.InvocationSucceeded, f.at.Add(time.Second))
			if err != nil || decision.Outcome != policy.RequireApproval || len(evaluation.Violations) == 0 || audit.ApprovalID != "" || audit.ApproverID != "" {
				t.Fatalf("unrelated approval authorized completion: decision=%#v evaluation=%#v audit=%#v err=%v", decision, evaluation, audit, err)
			}
			if _, err := run.NewStore(f.store.Root()); err != nil {
				t.Fatalf("unauthorized evidence is unreadable: %v", err)
			}
		})
	}
}

func TestRetryCompletionRejectsMissingApproval(t *testing.T) {
	f := prepareApprovedRetry(t)
	_, _, attempt, err := f.manager.EvaluateAndRecord(f.record.ID, f.action, policy.BudgetState{}, f.at)
	if err != nil {
		t.Fatal(err)
	}
	completion := f.action
	completion.Category, completion.RequestAuditID, completion.ApprovalID = policy.ToolCallCompleted, attempt.ID, "approval-missing"
	decision, _, audit, err := f.manager.EvaluateAndRecordInvocation(f.record.ID, completion, policy.BudgetState{}, run.InvocationSucceeded, f.at.Add(time.Second))
	if err == nil || decision.Outcome == policy.Allow || audit.ID != "" {
		t.Fatalf("missing approval accepted: decision=%#v audit=%#v err=%v", decision, audit, err)
	}
}

func TestRetryCompletionDoesNotAuthorizeNonApprovedDecision(t *testing.T) {
	for _, status := range []run.ApprovalStatus{run.ApprovalPending, run.ApprovalRejected, run.ApprovalExpired} {
		t.Run(string(status), func(t *testing.T) {
			f := prepareRetryApproval(t, status)
			_, _, attempt, err := f.manager.EvaluateAndRecord(f.record.ID, f.action, policy.BudgetState{}, f.at)
			if err != nil {
				t.Fatal(err)
			}
			completion := f.action
			completion.Category, completion.RequestAuditID = policy.ToolCallCompleted, attempt.ID
			decision, evaluation, audit, err := f.manager.EvaluateAndRecordInvocation(f.record.ID, completion, policy.BudgetState{}, run.InvocationSucceeded, f.at.Add(time.Second))
			if err != nil || decision.Outcome != policy.RequireApproval || len(evaluation.Violations) == 0 || audit.ApprovalID != "" || audit.ApproverID != "" {
				t.Fatalf("non-approved decision authorized completion: decision=%#v evaluation=%#v audit=%#v err=%v", decision, evaluation, audit, err)
			}
		})
	}
}
