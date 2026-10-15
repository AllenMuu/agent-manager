## MODIFIED Requirements

### Requirement: Browse Skills and SubAgents
The console MUST provide one Skills navigation entry that opens a unified workspace for project recommendations and the full searchable eligible Skill catalog. Recommendations MUST appear before the catalog on desktop. The workspace MUST display one Skill detail panel and MUST share selection by canonical Skill identifier across recommendations and catalog results; a Skill shown in both MUST be one logical selection. It MUST expose canonical SubAgent definitions and validation results as read-only information and MUST NOT install or remove SubAgents.

#### Scenario: Open the unified Skills workspace
- **WHEN** the operator selects Skills in the console navigation
- **THEN** the console shows project recommendations before the searchable eligible Skill catalog and does not show a separate Skill installation navigation entry

#### Scenario: Search the Skill library
- **WHEN** the operator searches for a term
- **THEN** the console returns matching eligible Skills with identifiers and descriptions

#### Scenario: Select the same Skill from recommendations and catalog
- **WHEN** a Skill appears in both project recommendations and catalog results and the operator selects it in either section
- **THEN** both appearances reflect one selection keyed by that Skill's identifier

#### Scenario: Inspect a Skill
- **WHEN** the operator opens a Skill detail
- **THEN** the console displays its supported metadata and readable `SKILL.md` content without executing it

#### Scenario: Inspect SubAgents
- **WHEN** the operator opens the SubAgents view
- **THEN** the console shows canonical definitions, referenced Skills, compatibility, capabilities, and validation diagnostics without mutation controls

### Requirement: React console delivery
The Web console MUST provide the specified MVP views using the existing Go application behavior as its source of truth. The interface MUST remain usable at desktop and narrow mobile viewport widths, support keyboard navigation, expose visible focus, and label icon-only controls for assistive technology. The unified Skills workspace MUST present recommendations, catalog, and details in a continuous vertical flow on mobile, with details inline after the catalog and a selection action bar pinned to the bottom. It MUST NOT introduce horizontal page overflow.

#### Scenario: Open the console on desktop
- **WHEN** the operator opens the local service at a desktop viewport
- **THEN** project navigation, status, recommendations, inventory, diagnostics, and operations are understandable without relying on decorative graphics

#### Scenario: Open the unified Skills workspace on a narrow screen
- **WHEN** the operator opens Skills at a 375-pixel viewport
- **THEN** recommendations, catalog, and inline details appear in that order, selection actions remain reachable from the bottom-pinned bar, and the page has no horizontal overflow

#### Scenario: Navigate with a keyboard
- **WHEN** the operator uses only a keyboard
- **THEN** every action is reachable in a logical order with visible focus and semantic controls
