## Purpose

Assemble a bounded, read-only context bundle so an agent workflow can receive repository guidance, selected Skills, required task artifacts, and relevant shared Memory through one agent-neutral request.

## ADDED Requirements

### Requirement: Bounded task context assembly
The system SHALL assemble context only from explicitly requested task inputs, repository guidance files, explicitly selected Skills, and optional results from an injected read/search-capable Memory provider. It SHALL warn when a selected Skill's advisory compatibility declaration omits the requested agent, apply configured size bounds, and SHALL NOT invoke an agent or write to Memory.

#### Scenario: Assemble role inputs
- **WHEN** a task and role with existing required artifacts are resolved for an agent
- **THEN** the context result includes the bounded repository guidance and requested role input artifacts

#### Scenario: Include selected Skills with an advisory mismatch
- **WHEN** a Skill is explicitly requested for an agent omitted from its compatibility declaration
- **THEN** that Skill is included with a compatibility warning and no unrequested Skills are included

#### Scenario: No Skills are requested
- **WHEN** no Skill identifiers are supplied to context resolution
- **THEN** the context result contains no Skills from the library

#### Scenario: Include optional Memory search results
- **WHEN** Memory search is requested and an injected provider declares read and search capabilities
- **THEN** matching results are included without writing to the provider

#### Scenario: Provider lacks required read capability
- **WHEN** Memory search is requested from a provider without read/search capability
- **THEN** the resolver reports the limitation and performs no provider write
