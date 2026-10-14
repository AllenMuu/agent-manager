## Purpose

Let a project activate only the skills it needs through local links while preserving and safely transitioning any independent skills already owned by that project.

## ADDED Requirements

### Requirement: Explicit multi-target project activation
The system SHALL activate a selected library skill only for user-selected target agents, creating an absolute soft link in each target agent's recognized project skill location. It SHALL support an explicit option to select all detected supported target agents.

#### Scenario: Skill is activated for selected agents
- **WHEN** the user confirms activation of a library skill for Claude Code and Codex
- **THEN** the system creates one absolute soft link per selected target in that project's corresponding skill location

#### Scenario: Compatibility is not declared for a target
- **WHEN** the user selects a target agent not included in a skill's compatibility declaration
- **THEN** the system displays a compatibility warning and permits the explicitly confirmed activation

### Requirement: Project skill inventory
The system SHALL list project skills by target agent and distinguish managed skills, unmanaged directory skills, and orphaned links.

#### Scenario: Project contains both managed and unmanaged skills
- **WHEN** the user lists skills in a project containing a library link and an independent directory skill
- **THEN** the output labels the linked skill as managed and the independent directory as unmanaged

### Requirement: Safe managed-skill removal
The system SHALL remove only the selected project-side managed soft link and SHALL NOT delete its corresponding library skill. It SHALL refuse to remove an unmanaged project skill through the managed-skill removal command.

#### Scenario: Managed skill is removed from one project
- **WHEN** the user confirms removal of a managed skill from a project
- **THEN** the project soft link is removed and the library directory remains available

#### Scenario: Unmanaged skill is selected for removal
- **WHEN** the user requests managed-skill removal for an independent project directory
- **THEN** the system refuses the operation without changing that directory

### Requirement: Adoption of an existing project skill
The system SHALL allow an existing eligible project directory skill to be adopted into the versioned Skill repository. Adoption SHALL create a version record with unknown version and provenance where they are not available, then replace the former project directory with a managed soft link to that exact version only after the user confirms the displayed plan.

#### Scenario: Existing skill is adopted
- **WHEN** the user confirms adoption of an eligible independent project skill whose library identifier is unused
- **THEN** the skill is stored under its versioned repository identity and its former project location becomes a link to that exact version directory

#### Scenario: Adoption has a conflicting library identifier
- **WHEN** the library already contains a skill with the proposed identifier
- **THEN** the system refuses to overwrite either skill until the user explicitly chooses a supported conflict strategy

### Requirement: Forking a managed project skill
The system SHALL allow a managed project skill to be forked into an independent project directory without modifying the library skill or other projects that link to it.

#### Scenario: Managed skill is forked
- **WHEN** the user confirms a fork of a managed project skill
- **THEN** the project link is replaced with an independent copy and the library skill remains unchanged
