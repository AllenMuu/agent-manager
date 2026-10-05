## Why

Existing policy preflight has governance booleans but no complete representation of effective filesystem, process, network and credential permissions. Directory placement or context propagation must not imply actual enforcement.

## What Changes

- Separate effective permissions and controls: The system SHALL expose machine-readable tool, filesystem, network, process and credential permission dimensions separately from the controls a runtime can enforce, including provider-contributed permissions and explicit unknown values.
- Fail-closed mandatory preflight: Before execution, mandatory unknown or unsupported controls SHALL reject the run and identify the missing dimensions. Optional unsupported controls SHALL produce explicit warnings.
- Dimension-specific policy update support: Capability discovery SHALL distinguish live-update, recreate-required and unsupported policy changes separately for each permission dimension.
- Truthful noop and placement boundaries: Noop and resource-placement integrations SHALL report mandatory execution protection as unsupported unless their execution hooks have verified enforcement support; invocation metadata propagation alone SHALL NOT establish interception.
- Deterministic non-executing preflight: Effective permission resolution and preflight SHALL be testable deterministically without launching a model, sandbox, network service or dependency installation.

## Capabilities

### New Capabilities

- `runtime-enforcement-preflight`: A caller can inspect effective tool/filesystem/network/process/credential permissions and preflight a run against declared enforcement support before execution.

### Modified Capabilities

None in the current main spec directory. Existing completed change specs remain compatibility context; this delta adds a distinct capability and preserves their defaults.

## Impact

Runtime-neutral effective permission and enforcement capability contracts, preflight/diagnostics mapping and mock/noop provider declarations; add a control-plane versus enforcement ADR without launching a runtime.

Ticket: [R1 / #24](https://github.com/AllenMuu/agent-manager/issues/24). Parent: [#16](https://github.com/AllenMuu/agent-manager/issues/16). Blocked by: None (planning ready does not imply implementation approval or delivery).
