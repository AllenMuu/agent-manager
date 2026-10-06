package governance_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AllenMuu/skill-manager/internal/enforcement"
	"github.com/AllenMuu/skill-manager/internal/governance"
	"github.com/AllenMuu/skill-manager/internal/identity"
	"github.com/AllenMuu/skill-manager/internal/invocation"
	"github.com/AllenMuu/skill-manager/internal/policy"
	"github.com/AllenMuu/skill-manager/internal/run"
)

func TestManagedLifecycleRetainsApprovedInvocationLineage(t *testing.T) {
	policyYAML := "version: v2\nkind: agent-policy\nid: issue6-approval\nname: Issue 6 approval policy\ntools:\n  allow: [github.write]\napproval:\n  required_for: [destructive_write]\nidentity:\n  rules:\n    - action_id: github.write\n      actor_kinds: [human]\n      roles: [developer]\n      required_scopes: [github:write]\n"
	configured, err := policy.Load(strings.NewReader(policyYAML))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
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
	provider := enforcement.NewMockProvider("offline")
	coordinator, _ := run.NewCoordinator(store, provider)
	manager.SetCoordinator(coordinator)
	prepared, err := coordinator.Prepare(context.Background(), snapshot, t.TempDir(), identity.NamedSelection(actor, delegation), enforcement.Request{Version: "v1"})
	if err != nil {
		t.Fatal(err)
	}
	started, err := coordinator.Start(context.Background(), prepared.ID)
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
	if _, err := manager.DecideApproval(context.Background(), approval.ID, run.ApprovalApproved, "approved", approver, now.Add(3*time.Second)); err != nil {
		t.Fatal(err)
	}

	requestEvent.TraceID = first.RequestAudit.TraceID
	requestEvent.ApprovalID = approval.ID
	adapterError := errors.New("adapter failed after dispatch")
	failing := &dispatchedFailureAdapter{delegate: adapter, err: adapterError}
	failed, err := (governance.Invoker{Runs: manager, Adapter: failing}).Invoke(context.Background(), governance.InvocationRequest{
		RunID: started.ID, Event: requestEvent,
		Requirement: policy.IdentityRequirement{ActionID: "github.write", Required: true}, At: now.Add(4 * time.Second),
	})
	if !errors.Is(err, adapterError) || failed.CompletionAudit == nil || len(adapter.Calls()) != 1 {
		t.Fatalf("dispatched failure lacks completion: outcome=%#v err=%v calls=%d", failed, err, len(adapter.Calls()))
	}
	completion := *failed.CompletionAudit
	if completion.Result != policy.Outcome(run.InvocationFailed) || completion.Decision != policy.Allow || completion.RequestAuditID != failed.RequestAudit.ID || completion.ApprovalID != approval.ID || completion.ApproverID != approver.ID || completion.TraceID != first.RequestAudit.TraceID {
		t.Fatalf("failed completion lost approval lineage: %#v", completion)
	}
	reopened, err := run.NewStore(store.Root())
	if err != nil {
		t.Fatal(err)
	}
	consumed, err := reopened.GetApproval(approval.ID)
	if err != nil || consumed.ConsumedAt.IsZero() || consumed.ConsumedByAuditID != failed.RequestAudit.ID || consumed.RequestAuditID != first.RequestAudit.ID {
		t.Fatalf("adapter error released authorization: approval=%#v err=%v", consumed, err)
	}
	events, err := reopened.Events(started.ID)
	if err != nil {
		t.Fatal(err)
	}
	persisted := false
	for _, event := range events {
		if event.ID == completion.ID {
			persisted = event.Result == policy.Outcome(run.InvocationFailed) && event.ApprovalID == approval.ID && event.ApproverID == approver.ID
		}
	}
	if !persisted {
		t.Fatal("failed completion attribution was not persisted")
	}
	replayed, err := (governance.Invoker{Runs: manager, Adapter: failing}).Invoke(context.Background(), governance.InvocationRequest{
		RunID: started.ID, Event: requestEvent,
		Requirement: policy.IdentityRequirement{ActionID: "github.write", Required: true}, At: now.Add(5 * time.Second),
	})
	if err == nil || replayed.CompletionAudit != nil || len(adapter.Calls()) != 1 {
		t.Fatalf("failed dispatch approval was replayed: outcome=%#v err=%v calls=%d", replayed, err, len(adapter.Calls()))
	}
}

func TestMockLifecycleAndApprovedInvocationThenControlReconciliation(t *testing.T) {
	policyYAML := "version: v2\nkind: agent-policy\nid: issue6-approval\nname: Issue 6 approval policy\ntools:\n  allow: [github.write]\napproval:\n  required_for: [destructive_write]\nidentity:\n  rules:\n    - action_id: github.write\n      actor_kinds: [human]\n      roles: [developer]\n      required_scopes: [github:write]\n"
	configured, err := policy.Load(strings.NewReader(policyYAML))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
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
	provider := enforcement.NewMockProvider("offline")
	coordinator, _ := run.NewCoordinator(store, provider)
	manager.SetCoordinator(coordinator)
	prepared, err := coordinator.Prepare(context.Background(), snapshot, t.TempDir(), identity.NamedSelection(actor, delegation), enforcement.Request{Version: "v1"})
	if err != nil {
		t.Fatal(err)
	}
	started, err := coordinator.Start(context.Background(), prepared.ID)
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
	if _, err := manager.DecideApproval(context.Background(), approval.ID, run.ApprovalApproved, "approved", approver, now.Add(3*time.Second)); err != nil {
		t.Fatal(err)
	}

	requestEvent.TraceID = first.RequestAudit.TraceID
	requestEvent.ApprovalID = approval.ID

	outcome, err := invoker.Invoke(context.Background(), governance.InvocationRequest{RunID: started.ID, Event: requestEvent, Requirement: policy.IdentityRequirement{ActionID: "github.write", Required: true}, At: now.Add(4 * time.Second)})
	if err != nil || outcome.CompletionAudit == nil || outcome.CompletionAudit.Result != policy.Outcome(run.InvocationSucceeded) || outcome.CompletionAudit.ApprovalID != approval.ID || outcome.CompletionAudit.ApproverID != approver.ID || outcome.CompletionAudit.RequestAuditID != outcome.RequestAudit.ID {
		t.Fatalf("confirmed invocation: %#v %v", outcome, err)
	}
	if _, err = invoker.Invoke(context.Background(), governance.InvocationRequest{RunID: started.ID, Event: requestEvent, Requirement: policy.IdentityRequirement{ActionID: "github.write", Required: true}, At: now.Add(5 * time.Second)}); err == nil {
		t.Fatal("approval reused")
	}
	if len(adapter.Calls()) != 1 {
		t.Fatal("approval dispatched more than once")
	}
	provider.FailNext("pause", enforcement.ErrUnknown, true)
	if _, err = coordinator.Pause(context.Background(), started.ID); err == nil {
		t.Fatal("lost pause acknowledgement ignored")
	}
	reopened, _ := run.NewStore(store.Root())
	restarted, _ := run.NewCoordinator(reopened, provider)
	got, err := restarted.Reconcile(context.Background(), started.ID)
	if err != nil || got.Status != run.Paused || !got.ExternalRuntime.Operations[len(got.ExternalRuntime.Operations)-1].Reconciled {
		t.Fatalf("control reconciliation: %#v %v", got, err)
	}
	if _, err = restarted.Resume(context.Background(), started.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = restarted.Terminate(context.Background(), started.ID); err != nil {
		t.Fatal(err)
	}
	events, err := reopened.Events(started.ID)
	if err != nil {
		t.Fatal(err)
	}
	terminatedAudit := false
	for _, event := range events {
		if event.Category == policy.AgentRunTerminated {
			terminatedAudit = true
		}
	}
	if !terminatedAudit {
		t.Fatal("confirmed termination audit missing")
	}
	if provider.ResourceCount() != 1 {
		t.Fatal("offline lifecycle duplicated a resource")
	}
}
