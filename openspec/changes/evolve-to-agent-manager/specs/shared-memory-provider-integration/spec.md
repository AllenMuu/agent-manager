## Purpose

Provide a local control layer for user-owned shared Memory providers so compatible agents can be configured and diagnosed without duplicating knowledge into agent-private stores by default.

## ADDED Requirements

### Requirement: Provider-independent Memory configuration
The system SHALL represent a Memory provider independently from agent adapters, including provider identity, non-secret configuration reference, supported capabilities, and supported scopes.

#### Scenario: Provider is configured
- **WHEN** the user configures a supported Memory provider
- **THEN** the system preserves provider configuration separately from every individual agent integration

### Requirement: Scoped capability diagnostics
The system SHALL report whether each supported agent integration can access configured Memory capabilities, including read, write, search, and available user or project scopes.

#### Scenario: User checks Memory status
- **WHEN** the user requests Memory status
- **THEN** the output identifies the configured provider and each agent's available and unavailable capabilities

### Requirement: Explicit shared-Memory promotion
The system SHALL require an explicit user action to persist managed knowledge to a shared Memory provider and SHALL NOT automatically copy resource content or agent-private conversation data into the provider.

#### Scenario: No promotion was requested
- **WHEN** the user configures an agent or manages a resource without requesting Memory persistence
- **THEN** the system does not write resource content or conversation history to the provider

### Requirement: Local provider and no-network boundary
The initial supported provider SHALL be a local file-backed provider. For that provider, `configuration.kind` SHALL be `file` and the reference SHALL name an already-existing absolute direct regular file without embedding secrets. Other non-secret reference kinds may be represented for providers without a local adapter, which status reports as unsupported. Discovery SHALL be read-only, SHALL reject symlink or non-regular paths, and SHALL NOT fetch, start, or configure a network provider. Missing or inaccessible files and configured provider types without a local adapter SHALL produce actionable unavailable or unsupported status.

#### Scenario: Local provider discovery is available
- **WHEN** the configured file reference points to an existing direct regular file
- **THEN** status reports the declared capabilities and scopes without creating or modifying the file

#### Scenario: Provider is unavailable or unsupported
- **WHEN** the file reference is missing, inaccessible, replaced, or symlinked, or the configured provider has no local adapter
- **THEN** status reports unavailable or unsupported with an actionable reason and performs no write or network operation

#### Scenario: Explicit promotion is outside the filesystem journal
- **WHEN** the user confirms `memory promote` for a supported scope and write capability
- **THEN** the provider owns the append and its recovery/retention semantics; Agent Manager does not add a filesystem operation-journal entry or make it reversible through `undo`
