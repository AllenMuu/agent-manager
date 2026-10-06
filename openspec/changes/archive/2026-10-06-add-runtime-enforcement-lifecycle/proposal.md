## Why

Runtime controllers are currently associated in memory and local run records do not coordinate external preparation/start recovery. A neutral lifecycle must preserve confirmed state and reconcile partial external operations.

## What Changes

- Persisted neutral external lineage: Managed runs SHALL record provider identity, external handle/generation and lifecycle operation correlation without importing runtime-specific types into the domain.
- Recoverable preparation and start: External preparation/start followed by persistence failure or restart SHALL be reconciled or cleaned up through the original operation evidence without duplicate launch.
- Confirmed lifecycle transitions: Pause, resume and termination SHALL change confirmed run state only after the provider acknowledges the correlated transition; failed or unknown outcomes SHALL remain inspectable.
- Honest optional lifecycle functions: Unsupported optional provider functions SHALL return explicit capability outcomes and SHALL NOT be simulated as successful lifecycle transitions.
- Deterministic lifecycle evidence: Prepare/start/control/restart scenarios SHALL be testable with a deterministic mock without launching a real model. Governed tool invocations SHALL retain the single-use approval dispatch and completion lineage from the prerequisite repair.

## Capabilities

### New Capabilities

- `runtime-enforcement-lifecycle`: A caller can prepare, start, pause and terminate a mock-governed run through a neutral enforcement provider and reconcile external handles after partial failure or restart.

### Modified Capabilities

None in the current main spec directory. Existing completed change specs remain compatibility context; this delta adds a distinct capability and preserves their defaults.

## Impact

Enforcement provider boundary and optional lifecycle interfaces, run coordinator/persistence references, deterministic mock provider and recovery/race tests; no OpenShell dependency.

Ticket: [R2 / #25](https://github.com/AllenMuu/agent-manager/issues/25). Parent: [#16](https://github.com/AllenMuu/agent-manager/issues/16). Blocked by: #24, #18
