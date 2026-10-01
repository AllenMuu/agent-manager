## Purpose

An explicitly enabled, version-pinned OpenShell adapter demonstrates one real sandbox with network denial, human approval, confirmed policy update, retry and termination, and exposes tested coverage through doctor. This capability provides a separately verifiable slice of parent issue #16.

## ADDED Requirements

### Requirement: Opt-in neutral OpenShell adapter
The experimental adapter SHALL require explicit enablement and a selected tested version; core APIs SHALL contain no OpenShell-specific types and ordinary Agent Manager workflows SHALL NOT require OpenShell.

#### Scenario: Adapter is not selected
- **WHEN** an operator performs ordinary CLI or Skill work without enabling OpenShell
- **THEN** the operation runs without importing runtime-specific domain types, installing OpenShell or creating a sandbox

### Requirement: Tested enforcement and credential diagnostics
Doctor SHALL distinguish tested enforcement support, missing controls, credential mediation and weaker environment-injected isolation. Credential values SHALL NOT be persisted or displayed.

#### Scenario: Inspect weaker credential isolation
- **WHEN** the selected runtime injects credentials instead of mediating downstream requests
- **THEN** doctor reports the weaker isolation and audit/status contains only safe references and scopes

### Requirement: Static policy change handling
The adapter SHALL reject static filesystem/process changes or require an explicit correlated sandbox recreation; dynamic support SHALL be declared separately per dimension.

#### Scenario: Filesystem change after start
- **WHEN** a change requires a control fixed at sandbox creation
- **THEN** the adapter reports recreate-required or unsupported and does not claim a live update succeeded

### Requirement: Real sandbox approval smoke evidence
An opt-in real sandbox smoke SHALL demonstrate preflight, creation, a denied network action, permission proposal, authorized human approval, exact confirmed revision, retry, audit correlation and confirmed termination, recording version and environment.

#### Scenario: Real denied network retry
- **WHEN** the operator enables the pinned smoke and approves a valid narrow permission proposal
- **THEN** one real sandbox denies then permits the revalidated action only after confirmed policy application, records full lineage and confirms termination

### Requirement: No implicit external setup
Normal managed-skill, status and offline test workflows SHALL NOT install dependencies, launch a model or automatically start an external sandbox. Reference documentation SHALL distinguish adopted control-plane concepts from externally integrated enforcement.

#### Scenario: Offline workflows
- **WHEN** ordinary tests, Skill management or capability inspection run with no external runtime installed
- **THEN** they use deterministic fixtures or report unavailable support without network installation or sandbox start
