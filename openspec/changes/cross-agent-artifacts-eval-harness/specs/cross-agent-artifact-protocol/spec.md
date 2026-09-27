## Purpose

Give humans and agent integrations one versioned, inspectable representation for task intent, decisions, plans, implementation metadata, verification, and lessons across workflow stages.

## ADDED Requirements

### Requirement: Versioned task artifact envelope
Every task artifact SHALL contain a supported schema version, kind, stable identifier, RFC3339 creation time, project root, source actor, and explicit task relationship. The initial kinds SHALL be `intent`, `spec`, `plan`, `implementation`, `verification`, and `lessons`. Unknown fields SHALL survive parse, validation, rendering, and save operations.

#### Scenario: Valid artifact round-trips extension fields
- **WHEN** a valid v1 artifact contains fields unknown to the current Agent Manager version
- **THEN** validation succeeds and inspect or save output retains those fields

#### Scenario: Invalid envelope is rejected
- **WHEN** an artifact omits a required envelope field or uses an unsupported version or kind
- **THEN** validation returns an actionable error and does not save the artifact

### Requirement: Task artifact storage and inspection
The system SHALL store artifacts under a project-owned task directory using stable task and kind names. It SHALL provide explicit task initialization, artifact listing, inspection, validation, and human-readable rendering. Storage operations SHALL reject unsafe identifiers and symlinked task path components, and SHALL NOT execute artifact content.

#### Scenario: Initialize a task
- **WHEN** an operator initializes a task with a valid intent
- **THEN** the system creates the task directory and initial intent artifact without overwriting an existing task

#### Scenario: Save a validated artifact
- **WHEN** an operator confirms saving a valid artifact whose ID matches an existing task
- **THEN** the system writes it to the task's kind-specific artifact path

#### Scenario: Inspect or validate an artifact
- **WHEN** an operator requests list, show, validate, or render for a task artifact
- **THEN** the system reports the stored artifact deterministically without executing its content

### Requirement: Explicit lesson promotion
Lessons artifacts SHALL contain typed items with scope and confidence. Agent Manager SHALL keep lesson storage separate from shared Memory and SHALL write lessons to a Memory provider only through a separate explicit promotion operation.

#### Scenario: Lessons remain local until promoted
- **WHEN** a lessons artifact is created, read, or rendered
- **THEN** no Memory provider write occurs

#### Scenario: Operator promotes lessons
- **WHEN** an operator explicitly confirms promotion of a valid lessons artifact to a configured provider
- **THEN** only its typed lesson content is submitted to that provider

### Requirement: Agent-neutral role contracts
The system SHALL provide planner, implementer, reviewer, and verifier role contracts that declare required and produced artifact kinds plus filesystem, shell, and network permissions. Binding a role to an agent SHALL report unsupported required capabilities explicitly and SHALL NOT silently remove requirements.

#### Scenario: Bind a role to a compatible agent
- **WHEN** a role's required capabilities are declared by the selected agent
- **THEN** the binding reports the role's input and output artifact contract

#### Scenario: Bind a role with unsupported requirements
- **WHEN** a selected agent lacks a capability required by the role
- **THEN** the result identifies the unsupported capability and does not present the binding as fully supported
