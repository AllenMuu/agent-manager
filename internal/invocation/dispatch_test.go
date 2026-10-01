package invocation_test

import (
	"context"
	"testing"

	"github.com/AllenMuu/skill-manager/internal/invocation"
)

func TestDispatchPassesExactContextToSupportingMock(t *testing.T) {
	lineage := invocation.Lineage{RunID: "run-1", ActorID: "allen", DelegationID: "del-1", PolicySnapshotHash: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", ApprovalID: "approval-1"}
	callContext, err := invocation.NewContext(lineage, "trace-1")
	if err != nil {
		t.Fatal(err)
	}
	adapter := invocation.NewMockAdapter(true, "ok")
	result, err := invocation.Dispatch(context.Background(), adapter, invocation.Request{ActionID: "github.read", Tool: "github.read", RequireContextPropagation: true}, lineage, callContext)
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	calls := adapter.Calls()
	if result.Output != "ok" || len(calls) != 1 || calls[0].Context != callContext {
		t.Fatalf("result=%#v calls=%#v", result, calls)
	}
}

func TestDispatchBlocksUnsupportedRequiredPropagation(t *testing.T) {
	lineage := invocation.Lineage{RunID: "run-1", PolicySnapshotHash: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	callContext, err := invocation.NewContext(lineage, "trace-1")
	if err != nil {
		t.Fatal(err)
	}
	adapter := invocation.NewMockAdapter(false, "should not run")
	_, err = invocation.Dispatch(context.Background(), adapter, invocation.Request{ActionID: "github.read", Tool: "github.read", RequireContextPropagation: true}, lineage, callContext)
	if err == nil || len(adapter.Calls()) != 0 {
		t.Fatalf("unsupported required propagation was dispatched: err=%v calls=%#v", err, adapter.Calls())
	}
}

func TestDispatchWarnsWhenOptionalPropagationIsUnsupported(t *testing.T) {
	lineage := invocation.Lineage{RunID: "run-1", PolicySnapshotHash: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	callContext, err := invocation.NewContext(lineage, "trace-1")
	if err != nil {
		t.Fatal(err)
	}
	adapter := invocation.NewMockAdapter(false, "ok")
	result, err := invocation.Dispatch(context.Background(), adapter, invocation.Request{ActionID: "github.read", Tool: "github.read"}, lineage, callContext)
	if err != nil {
		t.Fatalf("optional propagation call failed: %v", err)
	}
	if len(adapter.Calls()) != 1 || len(result.Warnings) != 1 || result.Warnings[0] != "INVOCATION_CONTEXT_PROPAGATION_UNSUPPORTED" {
		t.Fatalf("optional propagation result=%#v calls=%#v", result, adapter.Calls())
	}
}

func TestDispatchRejectsMismatchedLineageBeforeInvokingAdapter(t *testing.T) {
	expected := invocation.Lineage{RunID: "run-1", ActorID: "allen", DelegationID: "del-1", PolicySnapshotHash: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	callContext, err := invocation.NewContext(expected, "trace-1")
	if err != nil {
		t.Fatal(err)
	}
	expected.ActorID = "other"
	adapter := invocation.NewMockAdapter(true, "ok")
	_, err = invocation.Dispatch(context.Background(), adapter, invocation.Request{ActionID: "github.read", Tool: "github.read"}, expected, callContext)
	if err == nil || len(adapter.Calls()) != 0 {
		t.Fatalf("mismatched context was dispatched: err=%v calls=%#v", err, adapter.Calls())
	}
}

func TestDispatchRejectsNilContextBeforeInvokingAdapter(t *testing.T) {
	lineage := invocation.Lineage{RunID: "run-1", PolicySnapshotHash: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	callContext, err := invocation.NewContext(lineage, "trace-1")
	if err != nil {
		t.Fatal(err)
	}
	adapter := invocation.NewMockAdapter(true, "ok")
	_, err = invocation.Dispatch(nil, adapter, invocation.Request{ActionID: "github.read", Tool: "github.read"}, lineage, callContext)
	if err == nil || len(adapter.Calls()) != 0 {
		t.Fatalf("nil context was dispatched: err=%v calls=%#v", err, adapter.Calls())
	}
}
