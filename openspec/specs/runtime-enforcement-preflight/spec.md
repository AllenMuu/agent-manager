# runtime-enforcement-preflight Specification

## Purpose

A caller can inspect effective tool/filesystem/network/process/credential permissions and preflight a run against declared enforcement support before execution. This capability provides a separately verifiable slice of parent issue #16.

## Requirements

### Requirement: Separate effective permissions and controls
The system SHALL expose machine-readable tool, filesystem, network, process and credential permission dimensions separately from the controls a runtime can enforce, including provider-contributed permissions and explicit unknown values.

#### Scenario: Inspect indirect permissions
- **WHEN** a tool is denied but shell, network and credential permissions could still reach its effect
- **THEN** the effective view displays those dimensions independently and does not claim the tool deny eliminates indirect access

### Requirement: Fail-closed mandatory preflight
Before execution, mandatory unknown or unsupported controls SHALL reject the run and identify the missing dimensions. Optional unsupported controls SHALL produce explicit warnings.

#### Scenario: Required credential mediation is missing
- **WHEN** policy requires network policy and credential mediation but the provider confirms only network policy
- **THEN** preflight rejects the run before prepare/start and names missing credential mediation

#### Scenario: Optional gap
- **WHEN** an optional control is unsupported
- **THEN** preflight reports an explicit warning rather than silently omitting it

### Requirement: Dimension-specific policy update support
Capability discovery SHALL distinguish live-update, recreate-required and unsupported policy changes separately for each permission dimension.

#### Scenario: Static filesystem and dynamic network
- **WHEN** a provider allows dynamic network changes but filesystem policy requires recreation
- **THEN** status reports each mode separately and does not claim general hot reload

### Requirement: Truthful noop and placement boundaries
Noop and resource-placement integrations SHALL report mandatory execution protection as unsupported unless their execution hooks have verified enforcement support; invocation metadata propagation alone SHALL NOT establish interception.

#### Scenario: Noop or directory adapter is selected
- **WHEN** mandatory enforcement is required from a noop or directory placement adapter
- **THEN** preflight fails and the adapter is not represented as active protection

### Requirement: Deterministic non-executing preflight
Effective permission resolution and preflight SHALL be testable deterministically without launching a model, sandbox, network service or dependency installation.

#### Scenario: Local preflight suite
- **WHEN** the same normalized policy and provider declarations are checked offline twice
- **THEN** the resulting permission view, errors and warnings match and no external runtime is started
