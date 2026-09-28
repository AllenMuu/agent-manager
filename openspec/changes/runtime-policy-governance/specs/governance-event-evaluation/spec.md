## Purpose

Extends Agent Manager's local deterministic evaluation boundary to verify policy decisions and governance audit evidence without launching an agent. It lets policy behavior and adapter capability mismatches participate in repeatable regression checks.

## ADDED Requirements

### Requirement: Evaluate policy and governance events as local evidence
The evaluation harness SHALL accept structured governance events as candidate evidence and verify expected decisions, reason codes, run IDs, and policy snapshot identity using deterministic local rules. Invalid, missing, or unsafe event evidence SHALL produce actionable failures and SHALL NOT execute its content.

#### Scenario: Evaluate allowed, denied, and approval-required actions
- **WHEN** a policy evaluation suite contains local fixtures for allowed, denied, and approval-required actions
- **THEN** each case passes only when its recorded decision and required reason evidence match the expectation

#### Scenario: Evaluate an adapter capability mismatch
- **WHEN** a case expects a mandatory capability mismatch
- **THEN** the verifier passes only when the evidence identifies the missing capability and fail-closed outcome

#### Scenario: Reject missing or malformed governance evidence
- **WHEN** a case references absent, malformed, or mismatched run/audit evidence
- **THEN** the evaluation reports a deterministic case failure without executing the evidence

### Requirement: Persist policy identity in governance evaluation results
Governance evaluation results SHALL identify the policy ID, version, and snapshot hash associated with each evaluated run. Existing evaluation suites and result files SHALL remain readable when those optional governance fields are absent.

#### Scenario: Persist a governance evaluation
- **WHEN** an operator evaluates valid governance event fixtures
- **THEN** the result records policy identity and snapshot hash with per-case evidence

#### Scenario: Read a pre-governance evaluation result
- **WHEN** the harness loads an existing result without policy metadata
- **THEN** it remains valid and comparison behavior for its existing cases is unchanged

### Requirement: Keep governance evaluation deterministic and local
Governance evaluation SHALL use versioned local fixtures and stable case ordering. It SHALL NOT invoke an agent or judge, execute candidate/event content, access the network, or install dependencies.

#### Scenario: Repeat an evaluation over the same event fixtures
- **WHEN** the same suite and governance evidence are evaluated more than once
- **THEN** case ordering, decisions, reason codes, and scores are identical apart from run timestamps and measured duration

#### Scenario: Evaluate without external services
- **WHEN** an operator runs a governance evaluation
- **THEN** the harness reads local data only and performs no agent or network call
