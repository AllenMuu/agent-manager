## Purpose

Preserve verifiable authorization and execution evidence when an approved invocation creates a new request audit for a retry, without weakening correlation or single-use dispatch guarantees.

## ADDED Requirements

### Requirement: Approved retries retain original authorization lineage
The system SHALL preserve an approval's original request reference while recording a retry completion against its own attempt request. Explicit approval evidence SHALL authorize the completion only when it is approved, has an explicit decider, and matches the attempt's run, action, tool, action type, trace, policy snapshot and applicable domain. A missing or unrelated approval SHALL NOT change a required-approval decision to ALLOW.

#### Scenario: Approved retry completes against a new request
- **WHEN** an approved action creates a new request for its retry and completes successfully
- **THEN** the completion records ALLOW and succeeded with the retry request, original approval, decider, trace and policy references

#### Scenario: Approval is unrelated to the attempt
- **WHEN** a completion cites approval evidence with a different run, action, tool, action type, trace or domain
- **THEN** it is not recorded as an authorized completion

#### Scenario: Original request completes without an explicit approval identifier
- **WHEN** an original required-approval request completes after its own matching approval was approved
- **THEN** the completion retains its original-request correlation and explicit approver attribution

### Requirement: Unused authorization survives blocked attempts
The system SHALL distinguish authorized policy decisions from execution results. A canceled or unsupported attempt that never reaches the adapter SHALL leave a blocked completion after a request has been recorded, without permanently consuming its approval. A pre-canceled invocation SHALL create no new request or completion.

#### Scenario: Cancellation occurs immediately before dispatch
- **WHEN** an approved attempt is canceled after preparation but before the adapter is invoked
- **THEN** its blocked completion is inspectable and its unused approval remains available

#### Scenario: Required propagation is unsupported
- **WHEN** an approved attempt cannot preserve required invocation context
- **THEN** the adapter is not invoked and a blocked completion is recorded without consuming approval

### Requirement: Dispatched authorization remains single use and readable
The system SHALL retain atomic single-use consumption for dispatched invocations and SHALL NOT release authorization after an adapter error that could follow side effects. Historical audit formats SHALL remain readable without a rewrite or new persisted format version.

#### Scenario: Two attempts compete for one approval
- **WHEN** independent callers concurrently retry the same approved action
- **THEN** at most one reaches the adapter and the blocked attempt has inspectable completion evidence

#### Scenario: Dispatched adapter returns an error
- **WHEN** the adapter has been invoked and returns an error
- **THEN** a failed completion retains the approval lineage and authorization remains consumed

#### Scenario: Store is reopened
- **WHEN** a store with retry completions and existing historical records is reopened
- **THEN** approved completion lineage and prior records remain readable
