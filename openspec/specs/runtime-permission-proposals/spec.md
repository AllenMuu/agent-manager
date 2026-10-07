# runtime-permission-proposals Specification

## Purpose

A denied runtime action creates a run-bound, expiring permission proposal with its base policy revision and requested difference; an authorized human can decide it through a trusted operator boundary. This capability provides a separately verifiable slice of parent issue #16.

## Requirements

### Requirement: Denial-bound expiring proposals
A permission proposal SHALL link the initiating identity/delegation, original denial, run, immutable base policy revision, requested capability difference and expiry.

#### Scenario: Denied network action requests permission
- **WHEN** an agent requests a narrowly scoped network destination after a persisted denial
- **THEN** the pending proposal records all lineage and the exact difference without changing effective policy

### Requirement: Trusted human decision authority
Only an authorized human from the trusted operator decision boundary SHALL approve a permission proposal. Agent self-approval and caller-provided human labels without established authority SHALL be rejected.

#### Scenario: Agent spoofs human metadata
- **WHEN** a governed agent submits a decision claiming human kind or approver roles
- **THEN** the decision is rejected and no approved proposal or policy mutation is created

#### Scenario: Authorized operator decides
- **WHEN** the trusted operator boundary establishes an authorized human and the proposal passes validation
- **THEN** the recorded decision includes the established approver identity and original proposal linkage

### Requirement: Expiry and base revision checks
Decision and use SHALL reject expired proposals, expired delegation and stale base revisions rather than approving a difference against outdated authority.

#### Scenario: Stale or expired request
- **WHEN** the proposal/delegation expires or the applied base revision changes before decision
- **THEN** approval is rejected with an explicit expiry or stale-revision outcome and policy remains unchanged

### Requirement: Delegation ceiling
A proposed permission difference SHALL remain within the run-bound delegation authority. A request above that ceiling SHALL be rejected even when a human requests approval.

#### Scenario: Permission exceeds delegation
- **WHEN** the proposal asks for a credential scope or permission outside the run delegation ceiling
- **THEN** the request is rejected and cannot become an approved mutation through this workflow

### Requirement: Distinct non-mutating proposal decisions
Rejection or expiry SHALL leave policy unchanged. Policy-change approval SHALL remain distinct from existing one-action approval and SHALL require a separate confirmed revision application before execution can resume.

#### Scenario: Proposal is rejected or merely approved
- **WHEN** the operator rejects a proposal or records a valid approval before revision application
- **THEN** rejection leaves policy unchanged and approval alone does not resume or authorize retry
