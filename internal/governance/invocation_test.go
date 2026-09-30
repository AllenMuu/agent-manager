package governance_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AllenMuu/skill-manager/internal/governance"
	"github.com/AllenMuu/skill-manager/internal/identity"
	"github.com/AllenMuu/skill-manager/internal/invocation"
	"github.com/AllenMuu/skill-manager/internal/policy"
	"github.com/AllenMuu/skill-manager/internal/run"
)

func TestLocalIdentityGovernedInvocationCorrelatesMockAndDeniesWrite(t *testing.T) {
	policyYAML := "version: v2\nkind: agent-policy\nid: issue6-local\nname: Issue 6 local policy\ntools:\n  allow: [github.read]\nidentity:\n  rules:\n    - action_id: github.read\n      actor_kinds: [human]\n      roles: [developer]\n      required_scopes: [github:read]\n"
	configured, err := policy.Load(strings.NewReader(policyYAML))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 29, 1, 2, 3, 0, time.UTC)
	snapshot, err := policy.Resolve(configured, now)
	if err != nil {
		t.Fatal(err)
	}
	store, err := run.NewStore(filepath.Join(t.TempDir(), "runs"))
	if err != nil {
		t.Fatal(err)
	}
	actor := identity.ActorIdentity{ID: "allen", Kind: identity.Human, Subject: "allen@local", Roles: []string{"developer"}}
	delegation := identity.Delegation{ID: "issue6-delegation", ActorID: actor.ID, Scopes: []string{"github:read"}, ExpiresAt: now.Add(time.Hour)}
	selection := identity.NamedSelection(actor, delegation)
	capabilities := map[policy.Control]bool{policy.ControlToolInterception: true, policy.ControlRuntimeEvents: true}
	started, _, err := store.Start(snapshot, "mock", t.TempDir(), selection, capabilities, now)
	if err != nil {
		t.Fatal(err)
	}
	manager, err := run.NewManager(store)
	if err != nil {
		t.Fatal(err)
	}
	adapter := invocation.NewMockAdapter(true, "repository read")
	invoker := governance.Invoker{Runs: manager, Adapter: adapter}

	read, err := invoker.Invoke(context.Background(), governance.InvocationRequest{
		RunID:       started.ID,
		Event:       policy.Event{Category: policy.ToolCallRequested, Tool: "github.read", ActionType: "read_repository", ActionID: "github.read"},
		Requirement: policy.IdentityRequirement{ActionID: "github.read", Required: true},
		At:          now.Add(time.Second),
	})
	if err != nil {
		t.Fatal(err)
	}
	if read.Decision.Outcome != policy.Allow || read.CompletionAudit == nil || read.CompletionAudit.Result != policy.Outcome(run.InvocationSucceeded) || read.Result.Output != "repository read" {
		t.Fatalf("read invocation outcome = %#v", read)
	}
	calls := adapter.Calls()
	if len(calls) != 1 {
		t.Fatalf("mock invocation count = %d, want one", len(calls))
	}
	call := calls[0]
	wantLineage := invocation.Lineage{RunID: started.ID, ActorID: actor.ID, DelegationID: started.Identity.Delegation.ID, PolicySnapshotHash: snapshot.Hash}
	if err := call.Context.ValidateFor(wantLineage); err != nil {
		t.Fatalf("mock context did not preserve authorized lineage: %v; context=%#v", err, call.Context)
	}
	if call.Context.TraceID != read.RequestAudit.TraceID || read.CompletionAudit.TraceID != call.Context.TraceID || read.CompletionAudit.RequestAuditID != read.RequestAudit.ID {
		t.Fatalf("invocation audit correlation mismatch: call=%#v request=%#v completion=%#v", call.Context, read.RequestAudit, read.CompletionAudit)
	}

	write, err := invoker.Invoke(context.Background(), governance.InvocationRequest{
		RunID:       started.ID,
		Event:       policy.Event{Category: policy.ToolCallRequested, Tool: "github.write", ActionType: "write_repository", ActionID: "github.write"},
		Requirement: policy.IdentityRequirement{ActionID: "github.write", Required: true},
		At:          now.Add(2 * time.Second),
	})
	if err != nil {
		t.Fatal(err)
	}
	if write.Decision.Outcome != policy.Deny || write.CompletionAudit != nil || len(adapter.Calls()) != 1 {
		t.Fatalf("denied write reached mock: outcome=%#v calls=%d", write, len(adapter.Calls()))
	}
	events, err := store.Events(started.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 4 {
		t.Fatalf("audit event count = %d, want run start, read request/completion, and write request", len(events))
	}
	for _, event := range events[1:] {
		if event.ActorID != actor.ID || event.DelegationID != started.Identity.Delegation.ID || event.PolicyHash != snapshot.Hash {
			t.Fatalf("audit event lost run identity or policy lineage: %#v", event)
		}
	}
	if events[3].ActionID != "github.write" || events[3].Decision != policy.Deny || events[3].TraceID == "" {
		t.Fatalf("write denial lacks structured audit lineage: %#v", events[3])
	}

	failedCall, err := (governance.Invoker{Runs: manager, Adapter: errorInvocationAdapter{}}).Invoke(context.Background(), governance.InvocationRequest{
		RunID:       started.ID,
		Event:       policy.Event{Category: policy.ToolCallRequested, Tool: "github.read", ActionType: "read_repository", ActionID: "github.read"},
		Requirement: policy.IdentityRequirement{ActionID: "github.read", Required: true},
		At:          now.Add(3 * time.Second),
	})
	if err == nil || failedCall.CompletionAudit == nil {
		t.Fatalf("adapter failure was not returned and audited: outcome=%#v err=%v", failedCall, err)
	}
	failedAudit, err := json.Marshal(failedCall.CompletionAudit)
	if err != nil || !strings.Contains(string(failedAudit), `"result":"failed"`) {
		t.Fatalf("adapter failure audit has no failed result: %s err=%v", failedAudit, err)
	}

	blockedAdapter := invocation.NewMockAdapter(false, "must not execute")
	blockedCall, err := (governance.Invoker{Runs: manager, Adapter: blockedAdapter}).Invoke(context.Background(), governance.InvocationRequest{
		RunID:       started.ID,
		Event:       policy.Event{Category: policy.ToolCallRequested, Tool: "github.read", ActionType: "read_repository", ActionID: "github.read"},
		Requirement: policy.IdentityRequirement{ActionID: "github.read", Required: true},
		At:          now.Add(4 * time.Second),
	})
	if err == nil || blockedCall.CompletionAudit == nil || len(blockedAdapter.Calls()) != 0 {
		t.Fatalf("unsupported propagation was not blocked and audited: outcome=%#v err=%v calls=%#v", blockedCall, err, blockedAdapter.Calls())
	}
	blockedAudit, err := json.Marshal(blockedCall.CompletionAudit)
	if err != nil || !strings.Contains(string(blockedAudit), `"result":"blocked"`) {
		t.Fatalf("propagation block audit has no blocked result: %s err=%v", blockedAudit, err)
	}
}

func TestApprovedInvocationPropagatesApprovalContext(t *testing.T) {
	policyYAML := "version: v2\nkind: agent-policy\nid: issue6-approval\nname: Issue 6 approval policy\ntools:\n  allow: [github.write]\napproval:\n  required_for: [destructive_write]\nidentity:\n  rules:\n    - action_id: github.write\n      actor_kinds: [human]\n      roles: [developer]\n      required_scopes: [github:write]\n"
	configured, err := policy.Load(strings.NewReader(policyYAML))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 29, 2, 3, 4, 0, time.UTC)
	snapshot, err := policy.Resolve(configured, now)
	if err != nil {
		t.Fatal(err)
	}
	store, err := run.NewStore(filepath.Join(t.TempDir(), "runs"))
	if err != nil {
		t.Fatal(err)
	}
	manager, err := run.NewManager(store)
	if err != nil {
		t.Fatal(err)
	}
	actor := identity.ActorIdentity{ID: "allen", Kind: identity.Human, Subject: "allen@local", Roles: []string{"developer"}}
	delegation := identity.Delegation{ID: "issue6-write", ActorID: actor.ID, Scopes: []string{"github:write"}, ExpiresAt: now.Add(time.Hour)}
	controller := approvalRuntimeController{}
	started, _, err := manager.Start(snapshot, "mock", t.TempDir(), identity.NamedSelection(actor, delegation), now, controller)
	if err != nil {
		t.Fatal(err)
	}
	adapter := invocation.NewMockAdapter(true, "write completed")
	invoker := governance.Invoker{Runs: manager, Adapter: adapter}
	requestEvent := policy.Event{Category: policy.ToolCallRequested, Tool: "github.write", ActionType: "destructive_write", ActionID: "github.write"}
	first, err := invoker.Invoke(context.Background(), governance.InvocationRequest{
		RunID: started.ID, Event: requestEvent,
		Requirement: policy.IdentityRequirement{ActionID: "github.write", Required: true}, At: now.Add(time.Second),
	})
	if err != nil || first.Decision.Outcome != policy.RequireApproval || len(adapter.Calls()) != 0 {
		t.Fatalf("write was not held for approval: outcome=%#v err=%v calls=%d", first, err, len(adapter.Calls()))
	}
	approval, err := manager.RequestApproval(context.Background(), run.Approval{
		RunID: started.ID, RequestAuditID: first.RequestAudit.ID, Category: requestEvent.Category,
		Tool: requestEvent.Tool, ActionType: requestEvent.ActionType, ReasonCode: first.Decision.ReasonCode,
	}, now.Add(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	approver := identity.ActorIdentity{ID: "reviewer", Kind: identity.Human, Subject: "reviewer@local", Roles: []string{"approver"}}
	if _, err := store.DecideApproval(approval.ID, run.ApprovalApproved, "approved", approver, now.Add(3*time.Second)); err != nil {
		t.Fatal(err)
	}

	requestEvent.TraceID = first.RequestAudit.TraceID
	requestEvent.ApprovalID = approval.ID
	paused, err := invoker.Invoke(context.Background(), governance.InvocationRequest{
		RunID: started.ID, Event: requestEvent,
		Requirement: policy.IdentityRequirement{ActionID: "github.write", Required: true}, At: now.Add(3500 * time.Millisecond),
	})
	if err == nil || paused.CompletionAudit != nil || len(adapter.Calls()) != 0 {
		t.Fatalf("approved invocation executed while the run was paused: outcome=%#v err=%v calls=%d", paused, err, len(adapter.Calls()))
	}
	if _, err := manager.DecideApproval(context.Background(), approval.ID, run.ApprovalApproved, "approved", approver, now.Add(3750*time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	canceledContext, cancel := context.WithCancel(context.Background())
	cancel()
	canceled, err := invoker.Invoke(canceledContext, governance.InvocationRequest{
		RunID: started.ID, Event: requestEvent,
		Requirement: policy.IdentityRequirement{ActionID: "github.write", Required: true}, At: now.Add(3800 * time.Millisecond),
	})
	if err == nil || canceled.CompletionAudit != nil || len(adapter.Calls()) != 0 {
		t.Fatalf("pre-canceled invocation was not rejected before dispatch: outcome=%#v err=%v calls=%d", canceled, err, len(adapter.Calls()))
	}
	approvalAfterCancel, err := store.GetApproval(approval.ID)
	if err != nil || !approvalAfterCancel.ConsumedAt.IsZero() {
		t.Fatalf("pre-canceled invocation consumed approval: approval=%#v err=%v", approvalAfterCancel, err)
	}
	lateCanceled, err := invoker.Invoke(&cancelAfterErrChecksContext{Context: context.Background()}, governance.InvocationRequest{
		RunID: started.ID, Event: requestEvent,
		Requirement: policy.IdentityRequirement{ActionID: "github.write", Required: true}, At: now.Add(3850 * time.Millisecond),
	})
	if err == nil || lateCanceled.CompletionAudit == nil || lateCanceled.CompletionAudit.Result != policy.Outcome(run.InvocationBlocked) || len(adapter.Calls()) != 0 {
		t.Fatalf("cancellation before adapter dispatch was not recorded as blocked: outcome=%#v err=%v calls=%d", lateCanceled, err, len(adapter.Calls()))
	}
	approvalAfterLateCancel, err := store.GetApproval(approval.ID)
	if err != nil || !approvalAfterLateCancel.ConsumedAt.IsZero() {
		t.Fatalf("canceled pre-dispatch attempt did not release its approval: approval=%#v err=%v", approvalAfterLateCancel, err)
	}
	capabilityBlocked, err := (governance.Invoker{Runs: manager, Adapter: invocation.NewMockAdapter(false, "must not execute")}).Invoke(context.Background(), governance.InvocationRequest{
		RunID: started.ID, Event: requestEvent,
		Requirement: policy.IdentityRequirement{ActionID: "github.write", Required: true}, At: now.Add(3900 * time.Millisecond),
	})
	if err == nil || capabilityBlocked.CompletionAudit == nil || capabilityBlocked.CompletionAudit.Result != policy.Outcome(run.InvocationBlocked) {
		t.Fatalf("unsupported propagation was not audited as blocked: outcome=%#v err=%v", capabilityBlocked, err)
	}
	approvalAfterBlock, err := store.GetApproval(approval.ID)
	if err != nil || !approvalAfterBlock.ConsumedAt.IsZero() {
		t.Fatalf("capability preflight consumed an approval without dispatch: approval=%#v err=%v", approvalAfterBlock, err)
	}
	type invocationAttempt struct {
		outcome governance.InvocationOutcome
		err     error
	}
	secondStore, err := run.NewStore(store.Root())
	if err != nil {
		t.Fatal(err)
	}
	secondManager, err := run.NewManager(secondStore)
	if err != nil {
		t.Fatal(err)
	}
	barrierAdapter := &capabilityBarrierAdapter{delegate: adapter, arrived: make(chan struct{}, 2), release: make(chan struct{})}
	invokers := []governance.Invoker{{Runs: manager, Adapter: barrierAdapter}, {Runs: secondManager, Adapter: barrierAdapter}}
	start := make(chan struct{})
	attempts := make(chan invocationAttempt, 2)
	for index, at := range []time.Time{now.Add(4 * time.Second), now.Add(4100 * time.Millisecond)} {
		invoker := invokers[index]
		go func(invoker governance.Invoker, at time.Time) {
			<-start
			outcome, err := invoker.Invoke(context.Background(), governance.InvocationRequest{
				RunID: started.ID, Event: requestEvent,
				Requirement: policy.IdentityRequirement{ActionID: "github.write", Required: true}, At: at,
			})
			attempts <- invocationAttempt{outcome: outcome, err: err}
		}(invoker, at)
	}
	close(start)
	<-barrierAdapter.arrived
	<-barrierAdapter.arrived
	close(barrierAdapter.release)
	var resumed governance.InvocationOutcome
	var successfulAttempts, failedAttempts int
	for range 2 {
		attempt := <-attempts
		if attempt.err != nil {
			failedAttempts++
			if attempt.outcome.CompletionAudit == nil || attempt.outcome.CompletionAudit.Result != policy.Outcome(run.InvocationBlocked) {
				t.Fatalf("concurrent approval replay lacks blocked completion audit: %#v", attempt.outcome)
			}
			continue
		}
		successfulAttempts++
		resumed = attempt.outcome
	}
	if successfulAttempts != 1 || failedAttempts != 1 || resumed.Decision.Outcome != policy.Allow || resumed.CompletionAudit == nil {
		t.Fatalf("approval did not authorize exactly one concurrent invocation: success=%d failed=%d outcome=%#v", successfulAttempts, failedAttempts, resumed)
	}
	calls := adapter.Calls()
	if len(calls) != 1 || calls[0].Context.ApprovalID != approval.ID {
		t.Fatalf("approved invocation context omitted approval reference: %#v", calls)
	}
	if resumed.CompletionAudit.ApprovalID != approval.ID || resumed.CompletionAudit.TraceID != first.RequestAudit.TraceID {
		t.Fatalf("approved invocation audit lost approval lineage: %#v", resumed.CompletionAudit)
	}
	replayed, err := invoker.Invoke(context.Background(), governance.InvocationRequest{
		RunID: started.ID, Event: requestEvent,
		Requirement: policy.IdentityRequirement{ActionID: "github.write", Required: true}, At: now.Add(5 * time.Second),
	})
	if err == nil || replayed.CompletionAudit != nil || len(adapter.Calls()) != 1 {
		t.Fatalf("approval replay executed more than once: outcome=%#v err=%v calls=%d", replayed, err, len(adapter.Calls()))
	}
}

type errorInvocationAdapter struct{}

func (errorInvocationAdapter) Capabilities() invocation.Capabilities {
	return invocation.Capabilities{PreservesContext: true}
}

func (errorInvocationAdapter) Invoke(context.Context, invocation.Request, invocation.InvocationContext) (invocation.Result, error) {
	return invocation.Result{}, context.DeadlineExceeded
}

type approvalRuntimeController struct{}

type capabilityBarrierAdapter struct {
	delegate invocation.InvocationAdapter
	arrived  chan struct{}
	release  chan struct{}
}

type cancelAfterErrChecksContext struct {
	context.Context
	errChecks atomic.Int32
}

func (c *cancelAfterErrChecksContext) Err() error {
	if c.errChecks.Add(1) >= 3 {
		return context.Canceled
	}
	return nil
}

func (a *capabilityBarrierAdapter) Capabilities() invocation.Capabilities {
	a.arrived <- struct{}{}
	<-a.release
	return a.delegate.Capabilities()
}

func (a *capabilityBarrierAdapter) Invoke(ctx context.Context, request invocation.Request, callContext invocation.InvocationContext) (invocation.Result, error) {
	return a.delegate.Invoke(ctx, request, callContext)
}

func (approvalRuntimeController) Capabilities() map[policy.Control]bool {
	return map[policy.Control]bool{policy.ControlToolInterception: true, policy.ControlRuntimeEvents: true, policy.ControlApprovalPauseResume: true}
}

func (approvalRuntimeController) PauseForApproval(context.Context, string, string) (bool, error) {
	return true, nil
}

func (approvalRuntimeController) ResolveApproval(context.Context, string, string, bool) (bool, error) {
	return true, nil
}

func (approvalRuntimeController) Kill(context.Context, string, string) (bool, error) {
	return true, nil
}
