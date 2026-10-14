## MODIFIED Requirements

### Requirement: Configured directory-skill library
The system SHALL use `~/.agent-manager/skills` as its default configurable Skill-repository path and SHALL treat only the configured repository as authoritative. A catalog entry SHALL be eligible for management only when its resolved version directory contains `SKILL.md`.

#### Scenario: Agent Manager repository is searched
- **WHEN** no alternate library path has been configured after upgrading to Agent Manager
- **THEN** catalog commands discover eligible Skill resources below `~/.agent-manager/skills` without merging `~/.agents/skills`

#### Scenario: Invalid library entry is encountered
- **WHEN** a library child does not contain `SKILL.md`
- **THEN** the system excludes it from managed Skill results and reports it as incompatible when diagnostics are requested

### Requirement: Companion management metadata
The system SHALL continue to read optional `.skill-manager.yaml` files beside a directory skill for tags, compatibility declarations, and provenance. The absence of companion metadata SHALL NOT prevent discovery or activation.

#### Scenario: Legacy skill has no companion metadata
- **WHEN** a skill contains only `SKILL.md` and optional resources
- **THEN** it remains searchable and eligible for management after the product migration
