## Purpose

Provide a single, inspectable local catalog of reusable directory skills so people and agents can find appropriate skills before activating them in a project.

## ADDED Requirements

### Requirement: Configured directory-skill library
The system SHALL use `~/.agent-manager/skills` as its default Skill-repository path and SHALL allow the user to configure a different local path. Catalog commands SHALL read only the configured repository. A catalog entry SHALL be eligible for management only when its resolved version directory contains `SKILL.md`.

#### Scenario: Default repository is searched
- **WHEN** no alternate library path has been configured
- **THEN** catalog commands discover eligible Skill versions below `~/.agent-manager/skills` and do not merge `~/.agents/skills`

#### Scenario: Invalid library entry is encountered
- **WHEN** a library child does not contain `SKILL.md`
- **THEN** the system excludes it from managed-skill results and reports it as incompatible when diagnostics are requested

### Requirement: Searchable skill catalog
The system SHALL provide both a scriptable search command and an interactive terminal selector. Searches SHALL match skill identifier, `SKILL.md` name and description metadata, companion tags, and `SKILL.md` body text.

#### Scenario: Search returns matching skills
- **WHEN** a user searches using terms found in a skill description, tag, or body
- **THEN** the system returns that eligible skill with enough metadata to identify it

#### Scenario: Interactive selection confirms choices
- **WHEN** a user starts the interactive selector
- **THEN** the system permits searching and selecting multiple skills before displaying the selected skills and target agents for confirmation

### Requirement: Companion management metadata
The system SHALL read optional `.skill-manager.yaml` files placed beside a directory skill for tags, compatibility declarations, and provenance. The absence of companion metadata SHALL NOT prevent discovery or activation.

#### Scenario: Compatibility is declared
- **WHEN** a discovered skill declares target-agent compatibility in companion metadata
- **THEN** the catalog exposes that declaration to search and activation commands

#### Scenario: Legacy skill has no companion metadata
- **WHEN** a skill contains only `SKILL.md` and optional resources
- **THEN** it remains searchable and eligible for management

### Requirement: Machine-readable command output
The system SHALL offer JSON output for non-interactive catalog commands so an agent can retrieve candidate skills before requesting user confirmation.

#### Scenario: Agent requests structured search output
- **WHEN** search is invoked with JSON output enabled
- **THEN** the system returns structured candidate identifiers, descriptions, tags, and compatibility information without interactive prompts
