package governance

import (
	"context"
	"github.com/AllenMuu/skill-manager/internal/enforcement"
	"github.com/AllenMuu/skill-manager/internal/invocation"
	"github.com/AllenMuu/skill-manager/internal/policy"
	"github.com/AllenMuu/skill-manager/internal/run"
)

// Prepare implements the host-installed tool policy retry boundary. Coordinator
// authorizes after this ordinary invocation preflight and claims the durable
// exact-action retry before Dispatch. Network and credential actions use their
// separately configured exact-action adapter instead of this tool adapter.
func (i Invoker) Prepare(ctx context.Context, event policy.Event, callContext invocation.InvocationContext) (run.PreparedPolicyRetry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if event.Category != policy.ToolCallRequested {
		return nil, enforcement.ErrUnsupported
	}
	lineage := invocation.Lineage{RunID: callContext.RunID, ActorID: callContext.ActorID, DelegationID: callContext.DelegationID, PolicySnapshotHash: callContext.PolicySnapshotHash, ApprovalID: callContext.ApprovalID}
	prepared, err := invocation.PrepareDispatch(i.Adapter, invocation.Request{ActionID: event.ActionID, Tool: event.Tool, RequireContextPropagation: true}, lineage, callContext)
	if err != nil {
		return nil, err
	}
	return policyToolRetry{prepared}, nil
}

type policyToolRetry struct{ prepared invocation.PreparedDispatch }

func (p policyToolRetry) Dispatch(ctx context.Context) error {
	_, err := p.prepared.Invoke(ctx)
	return err
}
