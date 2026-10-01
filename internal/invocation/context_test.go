package invocation_test

import (
	"context"
	"testing"

	"github.com/AllenMuu/skill-manager/internal/invocation"
)

type invocationContractStub struct{}

func (invocationContractStub) Capabilities() invocation.Capabilities {
	return invocation.Capabilities{PreservesContext: true}
}

func (invocationContractStub) Invoke(context.Context, invocation.Request, invocation.InvocationContext) (invocation.Result, error) {
	return invocation.Result{}, nil
}

var _ invocation.InvocationAdapter = invocationContractStub{}

func TestInvocationAdapterDeclaresItsOwnPropagationCapability(t *testing.T) {
	adapter := invocationContractStub{}
	if !adapter.Capabilities().PreservesContext {
		t.Fatal("invocation adapter failed to declare context propagation")
	}
}

func TestBuildsAndValidatesCanonicalInvocationContext(t *testing.T) {
	lineage := invocation.Lineage{RunID: "run-1", ActorID: "allen", DelegationID: "del-1", PolicySnapshotHash: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", ApprovalID: "approval-1"}
	context, err := invocation.NewContext(lineage, "trace-1")
	if err != nil {
		t.Fatalf("build invocation context: %v", err)
	}
	if err := context.Validate(); err != nil {
		t.Fatalf("validate invocation context: %v", err)
	}
	if err := context.ValidateFor(lineage); err != nil {
		t.Fatalf("validate matching lineage: %v", err)
	}

	for _, changed := range []invocation.Lineage{
		{RunID: "run-2", ActorID: lineage.ActorID, DelegationID: lineage.DelegationID, PolicySnapshotHash: lineage.PolicySnapshotHash, ApprovalID: lineage.ApprovalID},
		{RunID: lineage.RunID, ActorID: "someone-else", DelegationID: lineage.DelegationID, PolicySnapshotHash: lineage.PolicySnapshotHash, ApprovalID: lineage.ApprovalID},
		{RunID: lineage.RunID, ActorID: lineage.ActorID, DelegationID: "del-other", PolicySnapshotHash: lineage.PolicySnapshotHash, ApprovalID: lineage.ApprovalID},
		{RunID: lineage.RunID, ActorID: lineage.ActorID, DelegationID: lineage.DelegationID, PolicySnapshotHash: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", ApprovalID: lineage.ApprovalID},
		{RunID: lineage.RunID, ActorID: lineage.ActorID, DelegationID: lineage.DelegationID, PolicySnapshotHash: lineage.PolicySnapshotHash, ApprovalID: "approval-other"},
	} {
		if err := context.ValidateFor(changed); err == nil {
			t.Errorf("mismatched lineage was accepted: %#v", changed)
		}
	}
}

func TestAnonymousInvocationContextOmitsActorAndDelegation(t *testing.T) {
	lineage := invocation.Lineage{RunID: "run-1", PolicySnapshotHash: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	context, err := invocation.NewContext(lineage, "trace-1")
	if err != nil {
		t.Fatalf("anonymous context rejected: %v", err)
	}
	if err := context.ValidateFor(lineage); err != nil {
		t.Fatalf("anonymous context lineage rejected: %v", err)
	}
	lineage.ActorID = "allen"
	if err := context.ValidateFor(lineage); err == nil {
		t.Fatal("anonymous context matched a named actor")
	}
}

func TestInvocationContextRequiresCompleteSafeCorrelationFields(t *testing.T) {
	base := invocation.Lineage{RunID: "run-1", ActorID: "allen", DelegationID: "del-1", PolicySnapshotHash: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	partial := base
	partial.DelegationID = ""
	if _, err := invocation.NewContext(partial, "trace-1"); err == nil {
		t.Fatal("context with actor but no delegation was accepted")
	}
	if _, err := invocation.NewContext(base, ""); err == nil {
		t.Fatal("context without a trace ID was accepted")
	}
	base.ActorID = "ghp_012345678901234567890123456789"
	if _, err := invocation.NewContext(base, "trace-1"); err == nil {
		t.Fatal("credential-like identity reference was accepted")
	}
}
