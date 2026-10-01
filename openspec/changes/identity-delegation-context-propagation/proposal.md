## Why

The completed runtime-policy-governance change records policy decisions, approvals, runs, and audit events, but does not carry a typed initiating identity or scoped delegation through a governed action. [Issue #6](https://github.com/AllenMuu/agent-manager/issues/6) closes that lineage gap so a local-first Agent Manager can explain who acted, under whose authority, and under which policy without relying on shared privileged credentials.

## What Changes

- Add runtime-neutral `ActorIdentity` and expiring, explicitly scoped `Delegation` models; snapshot them on each governed run and make missing identity explicit.
- Extend the existing policy engine with an `AgentPolicy` v2 identity section for actor kind/roles and exact delegated scopes; keep v1 policies readable and fail closed when an identity-bound action is requested without v2 identity rules.
- Add a canonical `InvocationContext` for run, actor, delegation, policy, approval, and trace correlation. Define a context-propagation adapter contract separate from filesystem resource adapters; verify it with a mock invocation path.
- Extend approval and audit lineage to retain the initiating actor, delegated authority, explicit approver, runtime, policy snapshot, action, result, and trace correlation without storing credentials or tokens.
- Put the explicit approver reference on the completed-action audit record as well as the approval transition record, so one structured record carries the full action lineage.
- Define a read-only `OperationalContextProvider` boundary with source and freshness metadata and a separate exact-key read access policy; do not add a concrete provider in the initial slice.
- Add an ADR that identifies Akuity as an industry reference and records Agent Manager's narrower local-first, runtime-neutral scope.
- Keep existing run-store records readable. Treat records without typed identity as explicitly legacy/anonymous, fail closed for sensitive actions, and avoid rewriting them during inspection.

The first implementation slice is deterministic and local: a typed local actor, a scoped run, allow/deny/approval policy outcomes, a mock invocation adapter, and structured lineage evidence. It does not require OAuth, a real runtime integration, or an agent-launch command.

## Capabilities

### New Capabilities

- `actor-identity-and-delegation`: Typed actors, scoped and expiring delegation, identity-aware policy evaluation, explicit anonymity, and actor-attributed approval/audit lineage.
- `invocation-context-propagation`: Canonical invocation metadata and declared adapter support for preserving it across tool or MCP calls.
- `operational-context-provider`: A read-only provider boundary and freshness-aware context records, without a concrete provider in this change.

### Modified Capabilities

None. The repository has no main capability files under `openspec/specs/` yet. The new identity capability extends the completed `runtime-policy-governance` behavior from #4 in place; it does not introduce a second policy engine or run store.

## Impact

- Extends the existing `internal/policy` and `internal/run` contracts and local store; adds runtime-neutral identity and invocation-context domain types.
- Adds a distinct invocation-adapter contract and capability validation without changing filesystem resource-adapter responsibilities.
- Extends structured run/audit inspection and deterministic tests; adds `docs/adr/0007-agent-control-plane-identity-delegation.md` as an implementation artifact.
- Adds no network dependency, credential storage, agent execution, or mandatory provider integration.
