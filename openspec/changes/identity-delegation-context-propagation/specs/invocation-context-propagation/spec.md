## Purpose

Carries stable run, actor, delegation, policy, approval, and trace references through supported tool or MCP invocation adapters without coupling those semantics to filesystem resource placement.

## ADDED Requirements

### Requirement: Canonical invocation context
Agent Manager SHALL provide a runtime-neutral `InvocationContext` containing the run ID, actor ID, delegation ID, policy snapshot hash, optional approval ID, and trace ID for a governed invocation. The context SHALL contain correlation metadata only and SHALL NOT contain credentials, tokens, or unrestricted identity claims.

#### Scenario: Build context for a governed action
- **WHEN** an action is prepared for invocation from a governed run
- **THEN** its invocation context identifies the run, initiating actor, delegation, immutable policy snapshot, and trace, plus any approval authorizing the action

#### Scenario: Reject a context with mismatched lineage
- **WHEN** an invocation context references a run, actor, delegation, policy, or approval that does not belong to the same authorized action
- **THEN** Agent Manager rejects the context before invoking the downstream tool

### Requirement: Explicit adapter context-propagation capability
Tool or MCP invocation adapters SHALL declare whether they can preserve the canonical invocation context end to end. A required but unsupported propagation capability SHALL block the invocation; optional unsupported propagation SHALL produce an explicit warning. Filesystem resource adapters SHALL NOT be treated as invocation adapters or claim propagation support from placement capability alone.

#### Scenario: Invoke through an adapter that preserves context
- **WHEN** the selected invocation adapter declares verified context-propagation support
- **THEN** it passes the canonical correlation fields through its supported transport and retains the same IDs across the call

#### Scenario: Block unsupported required propagation
- **WHEN** an action requires propagation but the selected adapter does not declare support
- **THEN** Agent Manager reports the missing capability and does not invoke the tool

#### Scenario: Validate the contract with a mock adapter
- **WHEN** the deterministic local slice invokes a mock tool adapter
- **THEN** the mock receives the exact canonical context and the resulting audit event is correlated to that invocation
