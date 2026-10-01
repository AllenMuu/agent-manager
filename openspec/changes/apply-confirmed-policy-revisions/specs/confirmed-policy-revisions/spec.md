## Purpose

A approved permission proposal creates an immutable policy revision and a correlated update operation; the run resumes or retries only after the provider confirms that exact applied revision. This capability provides a separately verifiable slice of parent issue #16.

## ADDED Requirements

### Requirement: Immutable historical policy references
Approved changes SHALL create immutable revisions while retaining the original run snapshot. Historical audit SHALL remain readable and resolve the exact policy revision used by each event.

#### Scenario: Change policy after prior audit
- **WHEN** a run applies revision two after events recorded under revision one
- **THEN** both snapshots remain unchanged and old and new events resolve their respective revisions

### Requirement: Desired versus confirmed applied revision
The system SHALL track desired and applied revisions separately and SHALL authorize resume/retry only after the provider confirms and local state records the exact correlated applied revision.

#### Scenario: Provider only accepts update
- **WHEN** the provider accepts an update request but has not confirmed the applied revision
- **THEN** desired advances while applied remains unchanged and the action stays paused

#### Scenario: Exact application is confirmed
- **WHEN** the expected run/handle/generation/operation and target revision are confirmed and persisted
- **THEN** applied advances and only a newly revalidated authorized retry may resume

### Requirement: Fail-closed application recovery
Failed, timed-out, missing or wrong-version acknowledgements SHALL keep execution paused until reconciliation proves the actual applied revision and persists that evidence.

#### Scenario: Wrong acknowledgement or timeout
- **WHEN** an acknowledgement names a different revision or the expected confirmation times out
- **THEN** the mutation is failed/unknown as appropriate and no resume is issued

### Requirement: Reconstructable permission change lineage
Audit SHALL reconstruct original denial, proposal, trusted human decision, mutation, confirmed revision and retry through neutral correlated identifiers without exposing credentials.

#### Scenario: Inspect approved permission change
- **WHEN** an authorized proposal is applied and its denied action retried
- **THEN** inspection follows the complete denial-to-retry chain and identifies both desired/applied evidence

### Requirement: Idempotent and source-aware events
Duplicate or stale events and retries SHALL NOT repeat external side effects. Event origin and established trust SHALL be explicit; untrusted or stale evidence SHALL NOT advance confirmed state.

#### Scenario: Duplicate stale event
- **WHEN** the same acknowledgement is delivered twice or an old handle generation sends an event
- **THEN** the confirmed transition occurs at most once and stale evidence is diagnosed without dispatching another action
