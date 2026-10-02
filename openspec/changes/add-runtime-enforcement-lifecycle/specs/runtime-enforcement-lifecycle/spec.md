## Purpose

A caller can prepare, start, pause and terminate a mock-governed run through a neutral enforcement provider and reconcile external handles after partial failure or restart. This capability provides a separately verifiable slice of parent issue #16.

## ADDED Requirements

### Requirement: Persisted neutral external lineage
Managed runs SHALL record provider identity, external handle/generation and lifecycle operation correlation without importing runtime-specific types into the domain.

#### Scenario: Inspect a prepared mock run
- **WHEN** a neutral provider confirms preparation
- **THEN** inspection exposes provider/handle/operation references with the immutable run policy and identity context

### Requirement: Recoverable preparation and start
External preparation/start followed by persistence failure or restart SHALL be reconciled or cleaned up through the original operation evidence without duplicate launch.

#### Scenario: External prepare succeeds but local confirmation fails
- **WHEN** the provider created a handle and local persistence fails before confirmation
- **THEN** recovery queries or cleans up that same operation/handle and does not create a second external runtime

#### Scenario: Restart after uncertain start
- **WHEN** the coordinator restarts with a persisted start intent but no success acknowledgement
- **THEN** it reconciles the existing operation and keeps execution unavailable until the real outcome is known

### Requirement: Confirmed lifecycle transitions
Pause, resume and termination SHALL change confirmed run state only after the provider acknowledges the correlated transition; failed or unknown outcomes SHALL remain inspectable.

#### Scenario: Termination is not acknowledged
- **WHEN** termination times out or the provider reports refusal
- **THEN** the run is not marked terminated and its uncertain/failed operation remains available for reconciliation

### Requirement: Honest optional lifecycle functions
Unsupported optional provider functions SHALL return explicit capability outcomes and SHALL NOT be simulated as successful lifecycle transitions.

#### Scenario: Provider cannot pause
- **WHEN** a caller requests pause from a provider without pause support
- **THEN** the operation is rejected as unsupported and no paused confirmation is recorded

### Requirement: Deterministic lifecycle evidence
Prepare/start/control/restart scenarios SHALL be testable with a deterministic mock without launching a real model. Governed tool invocations SHALL retain the single-use approval dispatch and completion lineage from the prerequisite repair.

#### Scenario: Mock lifecycle and invocation
- **WHEN** offline tests prepare/start a mock run, perform an approved invocation and reconcile control operations
- **THEN** state and audit reflect confirmed operations, the approval dispatches at most once and no model or external sandbox starts
