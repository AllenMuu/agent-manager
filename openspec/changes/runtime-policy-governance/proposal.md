## Why

Agent Manager can describe role permissions and evaluate supplied responses, but it has no versioned runtime policy, immutable policy snapshot, or common audit boundary for policy decisions. A runtime-neutral governance layer makes those decisions deterministic and inspectable while preserving the existing boundary between resource placement and runtime enforcement.

## What Changes

- Add a versioned `AgentPolicy` model for tool rules, approval categories, budgets, subagent limits, network domains, credential scopes, and policy versioning.
- Add normalized governance events and a deterministic policy engine with stable decision reason codes.
- Persist immutable policy snapshots, canonical approval requests, cross-project run inventory, termination outcomes, and redacted audit events in a user-level local state directory.
- Declare runtime governance capabilities independently from resource placement; reject unsupported mandatory controls and report unsupported optional controls explicitly.
- Let local evaluation cases verify policy and audit events for allowed, denied, approval-required, and capability-mismatch behavior.
- Add policy, run, and approval inspection/validation workflows without launching agents or adding network access.

## Capabilities

### New Capabilities

- `runtime-policy-governance`: Canonical policies, deterministic evaluation, run snapshots, approvals, adapter capability checks, run inventory, termination, and audit events.
- `governance-event-evaluation`: Local deterministic evaluation of policy and governance events using the existing evaluation harness.

### Modified Capabilities

None. Existing adapter and evaluation behavior remains compatible; this change adds the governance contracts alongside it.

## Impact

- Adds domain packages under `internal/policy` and `internal/run`, capability declarations under `internal/adapter`, and CLI workflows under `internal/cli`.
- Extends `internal/eval` with a local policy-event verifier and adds fixtures/documentation for governance evaluations.
- Stores run records and events locally under the user-level Agent Manager state directory; optional snapshot references can be attached to task artifacts and evaluation results. It adds no network, agent invocation, candidate-code execution, or dependency-install behavior.
- Current Claude Code, Codex, and Pi directory adapters continue to report runtime governance enforcement as unsupported until they provide verified execution hooks.
