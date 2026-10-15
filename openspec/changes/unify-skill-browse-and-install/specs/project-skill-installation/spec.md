## MODIFIED Requirements

### Requirement: Install selected Skills from the local library
The system SHALL provide an installation workflow that accepts one or more explicitly selected eligible Skills from the configured local Skill library and activates them for a selected project using the existing operation-specific target behavior. The CLI SHALL expose this workflow as `agent-manager install <skill-id>...`; the existing `add` command SHALL remain available with its current behavior. The WebUI SHALL expose installation from both the unified Skill catalog and project recommendation results, sharing selection by Skill identifier. For any non-empty selection, including one Skill, the WebUI SHALL offer the installation preview and apply flow with supported targets explicitly selected. The workflow SHALL NOT install every recommendation unless each Skill is explicitly selected.

#### Scenario: Install a Skill from the CLI
- **WHEN** an operator runs `agent-manager install go-helper --project . --target codex --yes`
- **THEN** Agent Manager resolves `go-helper` from the configured local Skill library and applies the confirmed project activation plan for Codex

#### Scenario: Existing add command remains available
- **WHEN** an operator uses the existing `agent-manager add` command
- **THEN** it continues to activate Skills with its current arguments and guarded behavior

#### Scenario: Install one selected Skill from the unified WebUI
- **WHEN** the user selects one eligible Skill from the catalog or project recommendations and starts its installation flow
- **THEN** the WebUI sends that Skill identifier to the existing installation preview workflow and requires explicit target selection

#### Scenario: Install multiple selected Skills from the unified WebUI
- **WHEN** the user selects two or more eligible Skills across the catalog or project recommendations and invokes batch installation
- **THEN** the WebUI sends only the selected Skill identifiers to the existing batch installation workflow

#### Scenario: Install selected project recommendations
- **WHEN** a user selects one or more Skill recommendations for a project and invokes the installation workflow
- **THEN** only the selected Skill identifiers are passed to the guarded workflow and no unselected recommendation is activated

#### Scenario: Recommendation remains read-only
- **WHEN** a user runs `agent-manager recommend` without invoking installation
- **THEN** Agent Manager reports recommendations without changing project activations, the Skill library, the operation journal, or the global baseline

### Requirement: Preview and confirm project installation
The system SHALL display an installation plan before changing project files. The plan SHALL identify each selected Skill, target agent, project destination, and applicable compatibility warning or conflict. The CLI SHALL apply no changes unless the invocation includes `--yes`, and SHALL display the plan before applying it; the WebUI SHALL require a separate explicit confirmation after presenting the plan. The unified WebUI SHALL use the same installation preview and apply flow for one or more selected Skills and SHALL require explicit target selection for every selection size. A change to the WebUI selection, targets, or conflict setting SHALL clear the installation preview. An unavailable or ineligible selected Skill SHALL block installation until it is deselected.

#### Scenario: CLI plan is not confirmed
- **WHEN** an operator invokes `agent-manager install` without `--yes`
- **THEN** the CLI displays the plan, returns without applying it, and leaves project files and the operation journal unchanged

#### Scenario: WebUI plan is confirmed
- **WHEN** a user reviews the complete installation plan in the WebUI and explicitly confirms it
- **THEN** the selected Skills are installed using the same lifecycle rules as the CLI

#### Scenario: Installation inputs change after preview
- **WHEN** a selected Skill, target destination, or conflict state changes after the WebUI displayed the installation plan but before confirmation is applied
- **THEN** Agent Manager makes no changes under the stale plan, displays the updated plan, and requires a new explicit confirmation

#### Scenario: Unified selection changes after installation preview
- **WHEN** the user changes the selected Skills, targets, or conflict setting while an installation preview is displayed
- **THEN** the WebUI clears that preview and requires the user to request and review a new preview

#### Scenario: Selected Skill becomes unavailable
- **WHEN** a selected Skill is no longer present or eligible in the current catalog
- **THEN** the WebUI marks it unavailable and blocks installation until the user deselects it

#### Scenario: Content behind a selected Skill symlink changes after preview
- **WHEN** a file reached through a symlink in a selected Skill changes after the WebUI displayed the plan but before confirmation is applied
- **THEN** Agent Manager rejects the stale preview without changing project files or the operation journal

#### Scenario: Skill compatibility is undeclared
- **WHEN** a selected Skill has no compatibility declaration for one of the explicitly selected target agents
- **THEN** the plan shows a warning and the installation may proceed only after explicit confirmation

#### Scenario: Installation conflicts with unmanaged content
- **WHEN** a selected Skill destination contains unmanaged content and no supported replacement strategy with force confirmation was selected
- **THEN** Agent Manager refuses the installation without changing that destination

### Requirement: Keep the WebUI install surface local
The WebUI SHALL use a local Agent Manager process and SHALL accept installation mutations only from the local operator's WebUI session. It SHALL NOT expose project installation to non-local network clients or fetch Skill content from an external source. The unified Skills workspace SHALL use the main console React application and its authenticated local session. The `/install/` path SHALL remain a compatibility entry that opens the same console application with Skills selected and SHALL NOT serve a separate embedded installation client.

#### Scenario: Open the unified Skills workspace
- **WHEN** an operator runs `agent-manager web --project <path>` and selects Skills in the console navigation
- **THEN** Agent Manager serves the unified catalog, recommendations, and installation controls through the same local-only endpoint and authenticated session as the management dashboard

#### Scenario: Open the compatibility installation path
- **WHEN** an operator opens `/install/` in the local Web application
- **THEN** Agent Manager serves the console application with Skills selected through the current local-only endpoint and authenticated session

#### Scenario: Installation is requested from outside the local WebUI session
- **WHEN** a request to preview or apply an installation does not originate from the active local WebUI session
- **THEN** Agent Manager rejects the request without changing project files or the operation journal

#### Scenario: The local service becomes unavailable during a request
- **WHEN** the WebUI cannot reach its local Agent Manager process while loading a preview or confirming an installation
- **THEN** it displays an actionable error and allows the user to retry without reloading the page

#### Scenario: Recommendation loading fails
- **WHEN** the recommendation request fails while the catalog is available
- **THEN** the WebUI shows a local retryable recommendation error and leaves the catalog usable

#### Scenario: Catalog loading fails
- **WHEN** the catalog request fails
- **THEN** the WebUI disables installation actions and shows an actionable catalog error

#### Scenario: Cancel or complete an operation
- **WHEN** the user cancels a plan or an installation succeeds
- **THEN** cancellation retains the current selection, while success clears the selection and refreshes project inventory and the latest operation
