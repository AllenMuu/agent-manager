## Why

Mock governance proves coordination but cannot establish real enforcement coverage. An opt-in pinned OpenShell experiment should validate the neutral boundary with one actual sandbox and a complete denied-action approval flow.

## What Changes

- Opt-in neutral OpenShell adapter: The experimental adapter SHALL require explicit enablement and a selected tested version; core APIs SHALL contain no OpenShell-specific types and ordinary Agent Manager workflows SHALL NOT require OpenShell.
- Tested enforcement and credential diagnostics: Doctor SHALL distinguish tested enforcement support, missing controls, credential mediation and weaker environment-injected isolation. Credential values SHALL NOT be persisted or displayed.
- Static policy change handling: The adapter SHALL reject static filesystem/process changes or require an explicit correlated sandbox recreation; dynamic support SHALL be declared separately per dimension.
- Real sandbox approval smoke evidence: An opt-in real sandbox smoke SHALL demonstrate preflight, creation, a denied network action, permission proposal, authorized human approval, exact confirmed revision, retry, audit correlation and confirmed termination, recording version and environment.
- No implicit external setup: Normal managed-skill, status and offline test workflows SHALL NOT install dependencies, launch a model or automatically start an external sandbox. Reference documentation SHALL distinguish adopted control-plane concepts from externally integrated enforcement.

## Capabilities

### New Capabilities

- `experimental-openshell-integration`: An explicitly enabled, version-pinned OpenShell adapter demonstrates one real sandbox with network denial, human approval, confirmed policy update, retry and termination, and exposes tested coverage through doctor.

### Modified Capabilities

None in the current main spec directory. Existing completed change specs remain compatibility context; this delta adds a distinct capability and preserves their defaults.

## Impact

An experimental external OpenShell adapter, reference/ADR, doctor enforcement coverage and separately enabled integration smoke; no default runtime installation or sandbox engine in Agent Manager.

Ticket: [R5 / #28](https://github.com/AllenMuu/agent-manager/issues/28). Parent: [#16](https://github.com/AllenMuu/agent-manager/issues/16). Blocked by: #27
