## Why

Issue #7 requires the first real external Memory engine behind the canonical contract. Configuration alone cannot prove Mem0 behavior, isolation, provenance or safe failure handling.

## What Changes

- Canonical remote operation mapping: The configured Mem0 provider SHALL map create, get, recall, update, forget and health/status to one pinned OSS REST contract while preserving canonical ownership, neutral IDs, source, evidence and observable versions.
- Honest remote semantic capabilities: The adapter SHALL report any backend operation or concurrency semantics it cannot guarantee, including atomic conditional update and supersession, and SHALL NOT claim success for an unsupported canonical operation.
- Bounded transport and uncertain writes: Remote operations SHALL honor timeout/cancellation and distinguish authentication failure, service unavailability, conflict and uncertain mutation outcomes. An uncertain write SHALL NOT be automatically replayed.
- Opaque secret references: Provider credentials SHALL be resolved from explicit secret references at the transport boundary; raw credentials SHALL NOT enter canonical records, configuration journals, audit, CLI status or diagnostics.
- Offline contract and opt-in live evidence: The Mem0 adapter SHALL pass pinned offline HTTP contract fixtures; real-service validation SHALL be opt-in and record the exact tested server contract version and environment.

## Capabilities

### New Capabilities

- `mem0-memory-provider`: An operator can use a pinned Mem0 OSS REST contract through the same canonical Memory API, without exposing provider IDs or credentials as the public model.

### Modified Capabilities

None in the current main spec directory. Existing completed change specs remain compatibility context; this delta adds a distinct capability and preserves their defaults.

## Impact

An explicitly configured Mem0 OSS REST adapter, backend-neutral ID mapping, secret-reference resolver, HTTP fixture tests and opt-in smoke harness; update ADR 0006 before enabling the network path.

Ticket: [M4 / #22](https://github.com/AllenMuu/agent-manager/issues/22). Parent: [#7](https://github.com/AllenMuu/agent-manager/issues/7). Blocked by: #19
