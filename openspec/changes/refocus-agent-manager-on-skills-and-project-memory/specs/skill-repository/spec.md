## Purpose

Provide a dedicated local source of truth for reusable Skills that does not collide with agent directories that automatically discover `.agents`, while preserving a deliberate, reviewable path for existing libraries and project activations.

## ADDED Requirements

### Requirement: Agent Manager-specific default Skill library
The system SHALL use `~/.agent-manager/skills` as the default local Skill library and SHALL continue to honor an explicitly configured local library path. The system SHALL NOT silently merge or fall back to `~/.agents/skills` when resolving the configured library.

#### Scenario: Default library is used
- **WHEN** no alternate Skill library path is configured
- **THEN** catalog, install, and recommendation workflows resolve Skills from `~/.agent-manager/skills`

#### Scenario: Default library has not been created yet
- **WHEN** a catalog or recommendation workflow discovers a configured repository root that does not exist
- **THEN** it reports an empty repository without creating the root or failing discovery

#### Scenario: Alternate local library is configured
- **WHEN** the operator configures a different local Skill library path
- **THEN** Skill workflows use that configured path without also searching the default or legacy path

#### Scenario: Legacy library exists without migration selection
- **WHEN** `~/.agents/skills` exists and no migration has been previewed and confirmed
- **THEN** its entries are not included in the managed catalog or activated implicitly

### Requirement: Versioned Skill identity and provenance
The system SHALL associate each repository-managed Skill version with a stable Skill identifier and version record, and SHALL record provenance and trust information when known. Missing legacy metadata SHALL be represented as unknown rather than inferred from Skill content. Adding a version SHALL preserve previously stored versions.

#### Scenario: A known-source Skill is imported
- **WHEN** a Skill is imported with source and immutable revision or checksum information
- **THEN** the repository records those values with the Skill identifier and version

#### Scenario: Legacy Skill has incomplete metadata
- **WHEN** an imported Skill has no version or source metadata
- **THEN** the repository marks the missing values as unknown and does not reject otherwise eligible Skill content solely for that reason

#### Scenario: A Skill receives a new version
- **WHEN** a new version is added for an existing Skill identifier
- **THEN** the new version is separately addressable and the previously stored version remains available

#### Scenario: Stored version content no longer matches its checksum
- **WHEN** a repository version contains content that differs from its recorded checksum
- **THEN** the repository refuses to resolve that version as the recorded immutable content

#### Scenario: A catalog search reports one identity and addressable versions
- **WHEN** a Skill identity has multiple stored versions
- **THEN** catalog output reports one Skill identity, its selected current version, each addressable version key, and available provenance without exposing the unselected version content as another catalog entry

#### Scenario: Installation selects a retained version
- **WHEN** the operator selects a Skill version by declared version or repository key in a guarded install request
- **THEN** the plan and agent-facing link use that exact repository version directory, and other stored versions remain unchanged

### Requirement: Materialize only selected Skills for agents
Agent-facing Skill directories SHALL expose only Skills selected for the applicable Global or Project scope and target agent. The system SHALL NOT configure an agent to discover the entire Agent Manager repository as its Skill directory.

#### Scenario: A project selects a subset of repository Skills
- **WHEN** the repository contains more Skills than the project has activated
- **THEN** each selected target agent sees only the Skills activated for that project and target

#### Scenario: A Skill is removed from a project
- **WHEN** the user removes a managed project activation
- **THEN** the agent-facing link is removed while the canonical repository version remains available

### Requirement: Preview and confirm legacy-library migration
The system SHALL provide a migration/import workflow for eligible Skills in `~/.agents/skills` that previews source entries, destination identifiers, metadata gaps, and conflicts before mutation. The user SHALL explicitly select the batch and confirm its displayed plan. A confirmed import SHALL copy only the selected Skills into the new repository while leaving legacy source entries and existing activation links intact. This workflow SHALL NOT delete legacy entries or rewrite project links; any later cleanup requires a separate explicit operation. Unselected or unconfirmed entries SHALL remain unchanged.

#### Scenario: Migration is previewed without confirmation
- **WHEN** the operator previews a batch from `~/.agents/skills` but does not confirm it
- **THEN** the system reports the planned destinations and leaves the legacy library, new library, and existing activation links unchanged

#### Scenario: A selected batch is imported
- **WHEN** the operator confirms a previewed import batch
- **THEN** only the selected eligible Skills are added to `~/.agent-manager/skills`, and the legacy source entries and existing activation links remain intact

#### Scenario: Import confirmation is bound to the displayed preview
- **WHEN** a source Skill or destination state changes after preview and the operator submits the earlier confirmation token
- **THEN** the import is rejected as stale and no destination entry is written

#### Scenario: Migration finds an identifier conflict
- **WHEN** a selected legacy Skill maps to an identifier that already exists in the destination repository
- **THEN** the system reports the conflict and makes no change to either Skill until the operator selects and confirms a supported conflict strategy

#### Scenario: A confirmed batch fails partway through
- **WHEN** a filesystem error prevents a confirmed batch migration from completing
- **THEN** the system removes every destination entry created by that batch, leaves legacy sources and existing activation links unchanged, and reports the failure

#### Scenario: A legacy Skill contains a symlink or special file
- **WHEN** the legacy preview encounters a Skill tree containing a symlink or special file
- **THEN** the entry is reported as ineligible for copying and confirmation cannot import it
