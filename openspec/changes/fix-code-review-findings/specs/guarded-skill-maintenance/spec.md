## ADDED Requirements

### Requirement: Ownership-preserving initialization rollback
Initialization SHALL roll back only paths it published and still owns. It SHALL preserve a concurrently created or replaced unmanaged file, while restoring unaffected published targets after later failure.

#### Scenario: Unmanaged file appears before publication
- **WHEN** an unmanaged Operator file appears after confirmation but before initialization publishes that target
- **THEN** initialization refuses it and preserves the file during rollback

#### Scenario: Later target fails after earlier publication
- **WHEN** initialization has published an earlier target and a later target fails
- **THEN** the earlier owned publication is restored without deleting any concurrent replacement

### Requirement: Literal managed-link tracking guidance
Git guidance SHALL ignore only each selected managed-link path. It SHALL escape Git pattern metacharacters and SHALL reject path names containing line separators before confirmation or filesystem mutation.

#### Scenario: Managed link contains a wildcard character
- **WHEN** a managed Skill directory name includes a Git wildcard
- **THEN** its exact path is ignored and neighboring unmanaged paths remain visible

#### Scenario: Managed link contains a newline
- **WHEN** a requested ignore rule would contain a newline or carriage return
- **THEN** the operation refuses the path without writing Git guidance or a journal entry

### Requirement: Home-relative library configuration
Configuration SHALL resolve a library beginning with ~/ against the current user's home. Ordinary relative libraries SHALL remain relative to the configuration file directory.

#### Scenario: README configuration is loaded
- **WHEN** the library is ~/.agents/skills
- **THEN** the configured library resolves to the current user's .agents/skills directory

### Requirement: Non-boundary evidence belongs to its enclosing scope
Technology evidence from Docker, Compose and agent markers in non-scope directories SHALL be attributed to the nearest containing project scope with scope-relative marker paths. Evidence SHALL NOT cross an independently marked scope boundary.

#### Scenario: Deployment markers are below the project root
- **WHEN** a root Go scope contains deploy/compose.yaml specifying PostgreSQL
- **THEN** its evidence includes deploy/compose.yaml and the Compose, Docker and PostgreSQL technologies

#### Scenario: Deployment markers are below a child scope
- **WHEN** an independently marked child contains nested deployment markers
- **THEN** their evidence belongs only to that child scope
