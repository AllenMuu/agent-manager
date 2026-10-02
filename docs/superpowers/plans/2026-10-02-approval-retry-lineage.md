# Approval Retry Lineage Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox syntax. The user selected github-oss-flow; its independent review and read-only OCR gates apply before PR publication.

**Goal:** Complete approved retries with inspectable approval attribution without weakening single-use dispatch.

**Architecture:** Keep original request, approval and attempt identifiers distinct. Completion resolves explicit approval evidence and checks it against the attempt; existing Invoker consumption remains the dispatch gate.

**Tech Stack:** Go standard testing, existing run/governance/invocation packages, OpenSpec, GitHub tracker.

**Ticket:** #18. **Base:** 71ee231. **Public seams confirmed:** Invoker.Invoke, Manager.EvaluateAndRecordInvocation, Store.GetApproval/Events.

## Task 1: Isolate the failing retry behavior

**Create:** `internal/run/approval_retry_test.go` (test package run_test, public API only).

- [x] Add the following complete independent test and fixture:

```go
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

```

- [x] Run `go test ./internal/run -run '^TestApprovedRetryCompletionPreservesPersistedLineage$' -count=1`. Baseline must FAIL with REQUIRE_APPROVAL instead of ALLOW.
- [x] Run `go test ./internal/governance -run '^TestApprovedInvocationPropagatesApprovalContext$' -count=1`. Baseline must FAIL with missing blocked completion.

## Task 2: Resolve approval evidence for each attempt

**Modify:** `internal/run/run.go`, completion evaluation's REQUIRE_APPROVAL branch.

- [x] Replace request-only lookup with this block:

```go
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
```

- [x] Retain approved-status/decider/action/tool/action-type/trace checks and extend the correlation condition with `sameAuditDomain(approvedRequest.Domain, requestAudit.Domain)`.
- [x] Run the two Task 1 commands. Both must PASS; the original invocation test includes cancellation, capability blocking, concurrent consumption and replay.

## Task 3: Validate other observable completion outcomes

**Modify:** `internal/run/approval_retry_test.go` and, if necessary, add an independent governance error-path test.

- [x] Reuse the fixture through the public completion API to verify blocked and failed results retain ALLOW policy evidence and persisted approval attribution.
- [x] Change one attempt lineage dimension at a time (run/action/tool/action type/trace/domain); the literal expected decision is REQUIRE_APPROVAL or a validation rejection, never ALLOW. Preserve original-request completion without an explicit ID and historical compatibility through existing run tests.
- [x] For dispatched adapter errors, use an external invocation adapter that returns a known error, then observe failed completion and consumed approval through the public interfaces. Never release authorization after ambiguous external side effects.

## Task 4: Validate and deliver

- [x] Run `go test ./...`, `go test -race ./internal/run ./internal/governance ./internal/invocation`, `go vet ./...`, and `go build -o /tmp/agent-manager-issue18 ./cmd/agent-manager`; all must exit zero.
- [x] Run `openspec validate fix-approved-invocation-retry-lineage --strict`, `openspec doctor`, and `git diff --check`; all must pass.
- [x] Have an independent reviewer inspect the complete diff and acceptance matrix; fix actionable findings and rerun affected checks.
- [x] Have the prescribed gpt-6.1-sol/high independent OCR subagent review the final scope using the actual delegation skill. Any actionable finding stops publication without a repair in that phase.
- [x] Stage only `internal/run/run.go`, `internal/run/approval_retry_test.go`, `internal/governance/approval_retry_test.go` and this change's OpenSpec/plan files. Commit with `Fix approval lineage for invocation retries` after inspecting staged filenames and diff checks.
- [x] After successful OCR, push the feature branch, create a PR with `Closes #18` and `Refs #16`, and attach it to this chat. Do not merge or close parent issues.

Exact OpenSpec requirements and executable checklist are in `openspec/changes/fix-approved-invocation-retry-lineage/`; record completed validation and review evidence in that change's delivery record. This plan does not implement Memory or sandbox features.

Implemented Task 3 tests: `TestApprovedRetryCompletionSeparatesPolicyFromExecutionResult`, `TestRetryCompletionDoesNotAuthorizeUnrelatedApproval`, `TestRetryCompletionRejectsMissingApproval`, `TestRetryCompletionDoesNotAuthorizeNonApprovedDecision`, and `TestApprovedAdapterFailureRetainsConsumedAuthorization`. The failed result also exercises approval lookup from its retry request when completion omits the ID. The domain case retains the explicit approval on the retry to exercise domain validation before persistence.
