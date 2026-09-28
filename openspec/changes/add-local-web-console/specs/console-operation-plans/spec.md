## Purpose

Server-owned operation plans let the browser review a guarded filesystem change across separate requests while preserving Agent Manager's existing confirmation, conflict, journal, rollback, and Undo protections.

## ADDED Requirements

### Requirement: Create a non-mutating operation plan
The system MUST create an opaque, session-bound plan for Skill activation, Skill removal, or Undo without mutating the project, Skill library, journal, or global baseline. A plan MUST contain the exact proposed changes, warnings, creation time, and expiration time. Plan requests MAY identify Skills, target agents, and supported operation choices but MUST NOT select filesystem source or destination paths.

#### Scenario: Preview Skill activation
- **WHEN** the operator requests an activation plan for selected Skill identifiers and supported target agents
- **THEN** the service returns an opaque plan identifier and exact changes without changing the filesystem or operation journal

#### Scenario: Preview Skill removal
- **WHEN** the operator requests a removal plan for a selected Skill identifier and target agent
- **THEN** the service returns the existing guarded lifecycle preview without removing the Skill link

#### Scenario: Preview Undo
- **WHEN** the operator requests an Undo plan
- **THEN** the service returns the exact restoration changes for the latest reversible journal entry without restoring them

#### Scenario: Reject caller-selected paths
- **WHEN** a plan request includes a filesystem source or destination path
- **THEN** the service rejects the request and does not create a plan

### Requirement: Bind plans to the session and expire them
A plan MUST be owned by the current process session and registered project, MUST have a short fixed lifetime of at most ten minutes, and MUST be unusable after expiry, cancellation, successful execution, or process exit. Only one execution of a plan identifier MAY proceed at a time.

#### Scenario: Execute a plan from another session
- **WHEN** a caller submits a plan identifier that was created by a different service session
- **THEN** the service rejects it without mutation

#### Scenario: Execute an expired plan
- **WHEN** the plan lifetime has elapsed before execution
- **THEN** the service rejects it with an expired-plan response and requires a fresh preview

#### Scenario: Execute the same plan concurrently
- **WHEN** multiple requests attempt to execute one plan identifier
- **THEN** at most one request reaches the guarded lifecycle service

#### Scenario: Process exits with pending plans
- **WHEN** the service process exits while plans remain pending
- **THEN** those plans are discarded and cannot be resumed by a later process

### Requirement: Revalidate before execution
The service MUST revalidate the registered project and recompute the requested guarded operation immediately before mutation. If the current plan differs from the reviewed plan, relevant project or library state changed, or the plan is no longer safe, the service MUST reject execution and require a new preview. Force replacement MUST require an additional explicit confirmation captured in the server-owned plan.

#### Scenario: Project changes after preview
- **WHEN** a relevant destination or source changes after the operator reviewed a plan
- **THEN** execution is rejected as stale and no filesystem mutation occurs

#### Scenario: Conflict requires force
- **WHEN** activation encounters unmanaged conflicting content and replacement was not explicitly confirmed
- **THEN** execution is rejected and the conflicting content remains untouched

#### Scenario: Execute a current reviewed plan
- **WHEN** the registered project and current operation still match a reviewed, unexpired plan and all required confirmations are present
- **THEN** the service delegates mutation to the existing guarded lifecycle or journal service

### Requirement: Journal successful changes and roll back failures
A successful activation, removal, or Undo MUST use the existing operation journal and rollback behavior. An unsuccessful or rejected execution MUST NOT be reported as successful and MUST preserve the existing guarded-operation guarantees.

#### Scenario: Successful activation or removal
- **WHEN** a confirmed plan executes successfully
- **THEN** the existing lifecycle service journals the change and the console can read it as the latest reversible operation

#### Scenario: Mutation fails during execution
- **WHEN** a guarded lifecycle operation fails after staging begins
- **THEN** the existing lifecycle service rolls back its partial changes and the API returns a failure response

### Requirement: Confirm Undo and protect current state
Undo MUST be represented as a reviewable plan. It MUST only restore snapshots to console-managed project Skill or SubAgent locations, or the specifically managed project `.gitignore` guidance path; it MUST NOT restore to the shared Skill library or arbitrary project paths. Before restoring state, the system MUST revalidate the latest journal entry and current paths through the existing journal implementation. A successful Undo MUST be recorded according to the existing journal semantics and MUST NOT overwrite state that changed after review.

#### Scenario: Confirm a current Undo plan
- **WHEN** the latest operation and its paths still match the reviewed Undo plan
- **THEN** the service delegates restoration to the existing journal implementation

#### Scenario: Reject a stale Undo plan
- **WHEN** any relevant path or latest journal entry changes after the Undo plan was created
- **THEN** the service rejects Undo, preserves the current state, and requires a fresh plan

#### Scenario: Reject Undo outside console-managed project locations
- **WHEN** the latest journal entry would restore a shared Skill library path or an unmanaged project path
- **THEN** the service declines to create an Undo plan and leaves the target path untouched
