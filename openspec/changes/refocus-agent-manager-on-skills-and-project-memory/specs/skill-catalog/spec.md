## MODIFIED Requirements

### Requirement: Configured directory-skill library
The system SHALL use `~/.agent-manager/skills` as its default configurable Skill-repository path and SHALL treat only the configured repository as authoritative. Catalog search SHALL return one stable Skill identity with its selected current version, addressable version keys, and available provenance. It SHALL NOT merge the legacy `~/.agents/skills` library into results.

#### Scenario: Default repository is searched
- **WHEN** no alternate Skill-library path is configured
- **THEN** catalog commands discover eligible Skills below `~/.agent-manager/skills` and do not include `~/.agents/skills`

#### Scenario: One identity has multiple versions
- **WHEN** a Skill identity has several stored versions
- **THEN** catalog search returns that identity once and exposes version metadata without indexing unselected version content as separate Skills
