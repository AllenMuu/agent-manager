## Purpose

Let users install explicitly selected Skills from the configured local library into a project through either the CLI or WebUI, including Skills surfaced by project recommendations, while keeping both interfaces on the same guarded and reversible lifecycle.

## ADDED Requirements

### Requirement: Install selected Skills from the local library
The system SHALL provide an installation workflow that accepts one or more explicitly selected eligible Skills from the configured local Skill library and activates them for a selected project and one or more explicitly selected supported target agents. The CLI SHALL expose this workflow as `agent-manager install <skill-id>...`; the existing `add` command SHALL remain available with its current behavior. The WebUI SHALL expose installation from both the Skill catalog and project recommendation results. The workflow SHALL NOT install every recommendation unless each Skill is explicitly selected.

#### Scenario: Install a Skill from the CLI
- **WHEN** an operator runs `agent-manager install go-helper --project . --target codex --yes`
- **THEN** Agent Manager resolves `go-helper` from the configured local Skill library and applies the confirmed project activation plan for Codex

#### Scenario: Existing add command remains available
- **WHEN** an operator uses the existing `agent-manager add` command
- **THEN** it continues to activate Skills with its current arguments and guarded behavior

#### Scenario: Install selected Skills from the WebUI catalog
- **WHEN** a user selects one or more eligible Skills in the WebUI catalog, chooses a project and supported target agents, and confirms the displayed plan
- **THEN** Agent Manager activates the selected Skills for only that project and those target agents

#### Scenario: Install selected project recommendations
- **WHEN** a user selects one or more Skill recommendations for a project and invokes the installation workflow through either interface
- **THEN** the selected Skill identifiers are passed to the guarded installation workflow and no unselected recommendation is activated

#### Scenario: Recommendation remains read-only
- **WHEN** a user runs `agent-manager recommend` without invoking installation
- **THEN** Agent Manager reports recommendations without changing project activations, the Skill library, the operation journal, or the global baseline

### Requirement: Preview and confirm project installation
The system SHALL display an installation plan before changing project files. The plan SHALL identify each selected Skill, target agent, project destination, and applicable compatibility warning or conflict. The CLI SHALL apply no changes unless the invocation includes `--yes`, and SHALL display the plan before applying it; the WebUI SHALL require a separate explicit confirmation after presenting the plan. An undeclared compatibility target SHALL produce a warning and remain installable after confirmation. A conflicting unmanaged destination SHALL be refused unless the user selects a supported replacement strategy and provides force confirmation.

#### Scenario: CLI plan is not confirmed
- **WHEN** an operator invokes `agent-manager install` without `--yes`
- **THEN** the CLI displays the plan, returns without applying it, and leaves project files and the operation journal unchanged

#### Scenario: WebUI plan is confirmed
- **WHEN** a user reviews the complete plan in the WebUI and explicitly confirms it
- **THEN** the selected installation is applied using the same lifecycle rules as the CLI

#### Scenario: Installation inputs change after preview
- **WHEN** a selected Skill, target destination, or conflict state changes after the WebUI displayed the plan but before confirmation is applied
- **THEN** Agent Manager makes no changes under the stale plan, displays the updated plan, and requires a new explicit confirmation

#### Scenario: Content behind a selected Skill symlink changes after preview
- **WHEN** a file reached through a symlink in a selected Skill changes after the WebUI displayed the plan but before confirmation is applied
- **THEN** Agent Manager rejects the stale preview without changing project files or the operation journal

#### Scenario: Skill compatibility is undeclared
- **WHEN** a selected Skill has no compatibility declaration for one of the explicitly selected target agents
- **THEN** the plan shows a warning and the installation may proceed only after explicit confirmation

#### Scenario: Installation conflicts with unmanaged content
- **WHEN** a selected Skill destination contains unmanaged content and no supported replacement strategy with force confirmation was selected
- **THEN** Agent Manager refuses the installation without changing that destination

### Requirement: Preserve guarded and reversible batch behavior
The system SHALL apply a confirmed multi-Skill installation as one guarded operation, record the resulting changes in the project operation journal, and support undo through the existing project undo workflow. If any selected activation fails before the journaled operation is committed, the system SHALL restore every destination changed by that installation. Installation SHALL create project-side absolute soft links to eligible local library Skills; it SHALL NOT copy Skill content into the project, modify the global baseline, execute Skill content, or access external sources.

#### Scenario: Batch installation succeeds
- **WHEN** a user confirms a plan containing multiple Skills and all activations succeed
- **THEN** all selected project links are created and one reversible operation record captures the batch

#### Scenario: Batch installation fails partway through
- **WHEN** an activation in a confirmed batch fails before the journal operation is committed
- **THEN** Agent Manager restores all destinations changed by the batch and does not report a partially successful installation

#### Scenario: A confirmed installation is undone
- **WHEN** a user undoes the latest successful installation operation
- **THEN** Agent Manager restores the project destinations to their pre-installation state and leaves the library Skills unchanged

#### Scenario: Independent installations are confirmed concurrently
- **WHEN** separate CLI or WebUI processes confirm installations for different Skills in the same project at the same time
- **THEN** Agent Manager serializes their mutations and journal commits so each successful installation remains independently undoable

### Requirement: Keep the WebUI install surface local
The WebUI SHALL use a local Agent Manager process and SHALL accept installation mutations only from the local operator's WebUI session. It SHALL NOT expose project installation to non-local network clients or fetch Skill content from an external source.

#### Scenario: Launch WebUI for a project
- **WHEN** an operator runs `agent-manager webui --project <path>` (or omits `--project` to use the current directory)
- **THEN** Agent Manager serves the installation UI for that project from a local-only endpoint and reports the local URL

#### Scenario: Installation is requested from outside the local WebUI session
- **WHEN** a request to preview or apply an installation does not originate from the active local WebUI session
- **THEN** Agent Manager rejects the request without changing project files or the operation journal

#### Scenario: The local service becomes unavailable during a request
- **WHEN** the WebUI cannot reach its local Agent Manager process while loading a preview or confirming an installation
- **THEN** it displays an actionable error and allows the user to retry without reloading the page

### Requirement: Reject invalid selections without side effects
The system SHALL reject an invalid project path, unknown Skill identifier, ineligible library entry, unsupported target agent, or empty selection with an actionable error. Such a rejected installation SHALL NOT change project files, the Skill library, the operation journal, or the global baseline.

#### Scenario: An installation selection is invalid
- **WHEN** an installation request contains an unknown Skill, unsupported target, invalid project, or no selected Skill
- **THEN** Agent Manager reports the invalid input and makes no filesystem changes
