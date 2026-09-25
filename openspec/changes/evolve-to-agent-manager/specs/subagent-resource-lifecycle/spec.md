## Purpose

Define portable, inspectable SubAgent resources that can be validated and installed through supported coding-agent adapters without silently losing their declared role or capabilities.

## ADDED Requirements

### Requirement: Canonical SubAgent definition
The system SHALL store SubAgent definitions in an agent-neutral canonical form containing an identifier, name, role, instructions, referenced skills, compatibility declaration, and declared capability requirements.

#### Scenario: Canonical SubAgent is listed
- **WHEN** the user lists SubAgent resources
- **THEN** the system returns the canonical definition and compatibility information independently of any installed agent representation

### Requirement: Capability-aware validation and preview
The system SHALL validate a SubAgent definition and provide a target-specific preview before installation. Validation SHALL report unsupported fields and missing referenced resources explicitly.

#### Scenario: SubAgent has an unsupported target field
- **WHEN** validation evaluates a SubAgent against a target that cannot express one of its required capabilities
- **THEN** the result identifies the field and target as unsupported

### Requirement: Guarded SubAgent installation
The system SHALL install a validated SubAgent only through a selected supported adapter and only after preview and confirmation. Filesystem-based installations SHALL be journaled and reversible where the adapter can restore the previous state.

#### Scenario: User confirms SubAgent installation
- **WHEN** the user confirms a valid target-specific installation preview
- **THEN** the system installs the adapter representation and records reversible operation evidence when applicable

#### Scenario: Installation would overwrite an unmanaged definition
- **WHEN** an installation destination contains an unmanaged definition
- **THEN** the system refuses to overwrite it without an explicit supported conflict strategy and force confirmation

#### Scenario: Target has no verified SubAgent representation
- **WHEN** a canonical SubAgent is targeted at Pi
- **THEN** the system reports the unsupported target representation before confirmation and does not create or modify a target-native file
