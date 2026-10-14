## MODIFIED Requirements

### Requirement: Read-only project recommendations identify the selected repository version
A recommendation SHALL resolve each candidate from the configured Skill repository through the catalog boundary and SHALL report the current version and repository key with its deterministic evidence. Recommendation SHALL remain read-only and SHALL NOT modify project activations, the Skill repository, or the operation journal.

#### Scenario: Recommend the current Skill version
- **WHEN** the configured repository contains multiple versions for a matching Skill
- **THEN** the recommendation identifies the current version and key while preserving deterministic ranking and matching evidence

#### Scenario: Recommendation remains read-only
- **WHEN** an operator requests recommendations for a project
- **THEN** project links, repository content, and the operation journal remain unchanged
