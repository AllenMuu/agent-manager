## Why

Current run validation assumes one fixed snapshot for every audit. Runtime permission changes need an immutable revision history and evidence that the provider applied the exact revision before retry/resume.

## What Changes

- Immutable historical policy references: Approved changes SHALL create immutable revisions while retaining the original run snapshot. Historical audit SHALL remain readable and resolve the exact policy revision used by each event.
- Desired versus confirmed applied revision: The system SHALL track desired and applied revisions separately and SHALL authorize resume/retry only after the provider confirms and local state records the exact correlated applied revision.
- Fail-closed application recovery: Failed, timed-out, missing or wrong-version acknowledgements SHALL keep execution paused until reconciliation proves the actual applied revision and persists that evidence.
- Reconstructable permission change lineage: Audit SHALL reconstruct original denial, proposal, trusted human decision, mutation, confirmed revision and retry through neutral correlated identifiers without exposing credentials.
- Idempotent and source-aware events: Duplicate or stale events and retries SHALL NOT repeat external side effects. Event origin and established trust SHALL be explicit; untrusted or stale evidence SHALL NOT advance confirmed state.

## Capabilities

### New Capabilities

- `confirmed-policy-revisions`: A approved permission proposal creates an immutable policy revision and a correlated update operation; the run resumes or retries only after the provider confirms that exact applied revision.

### Modified Capabilities

None in the current main spec directory. Existing completed change specs remain compatibility context; this delta adds a distinct capability and preserves their defaults.

## Impact

Run policy revision/mutation coordination, compatible audit validators, desired/applied references, provider acknowledgement/event handling and deterministic recovery tests.

Ticket: [R4 / #27](https://github.com/AllenMuu/agent-manager/issues/27). Parent: [#16](https://github.com/AllenMuu/agent-manager/issues/16). Blocked by: #26
