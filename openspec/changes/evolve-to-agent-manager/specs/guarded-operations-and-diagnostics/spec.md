## MODIFIED Requirements

### Requirement: Guarded filesystem mutations
The system SHALL preview file changes and require confirmation before any mutating Skill or SubAgent operation. It SHALL refuse to replace an unmanaged directory, ordinary file, or link to a different destination unless the user explicitly selects a compatible conflict strategy and supplies force confirmation.

#### Scenario: SubAgent installation would replace an unmanaged directory
- **WHEN** a filesystem-based SubAgent installation finds an unmanaged destination
- **THEN** the system displays the conflict and makes no filesystem change without explicit force confirmation

#### Scenario: Confirmation-time state is revalidated before publication
- **GIVEN** a confirmed plan whose source and destination state was inspected while the preview was built
- **WHEN** the source becomes invalid or the destination changes after confirmation but before publication
- **THEN** the system revalidates the source and destination, rejects the stale plan, and leaves the newly observed state untouched

#### Scenario: No-replace publication closes the late-create race
- **GIVEN** the destination is absent at the final pre-publication check and the selected conflict strategy does not permit replacement
- **WHEN** another actor creates a file, directory, or link at that destination before the link is published
- **THEN** publication fails without replacing or removing that late-created path, and the operation journal receives no entry for the failed operation

#### Scenario: Parent replacement cannot redirect publication
- **GIVEN** an adapter-owned project placement whose real directory parents passed final validation
- **WHEN** the project root or resource parent is replaced with a link to an external directory before final publication
- **THEN** publication remains anchored to the validated directory, fails without changing the external directory, preserves any staged original in a recoverable location, and records no operation journal entry

#### Scenario: Force acknowledgement and operation confirmation are both required
- **GIVEN** the destination is an ordinary file or a link to a different target
- **WHEN** the user selects a replacement strategy without force acknowledgement
- **THEN** the system refuses before requesting operation confirmation and leaves the destination unchanged
- **WHEN** the user supplies force acknowledgement but declines the displayed replacement plan
- **THEN** the system leaves the destination unchanged and records no operation
- **WHEN** the user supplies force acknowledgement and confirms the displayed replacement plan
- **THEN** the system replaces the conflict, records the prior state for undo, and undo restores the exact ordinary file or wrong link

#### Scenario: Interrupted multi-path publication is transactional
- **GIVEN** a confirmed operation publishes two or more filesystem paths and the journal may already contain completed entries
- **WHEN** publication is interrupted after one or more paths have been published but before the operation is journaled
- **THEN** the system restores every affected path to its exact pre-operation state, removes staged candidates, and does not append an incomplete journal entry or alter the existing journal entries

### Requirement: Reversible operation journal
The system SHALL record each confirmed reversible mutating resource operation in a local operation journal and SHALL provide undo for the latest reversible operation. Existing Skill journal entries SHALL remain readable.

#### Scenario: User undoes an existing Skill activation
- **WHEN** the user invokes undo after a successful pre-migration Skill activation
- **THEN** the system restores the pre-operation project-link state recorded by the existing journal entry

### Requirement: Local resource diagnostics and reconciliation
The system SHALL diagnose resource compatibility, orphaned managed links, unsupported agent locations, and configured Memory-provider availability. It SHALL reconcile a confirmed managed Skill link to the currently configured library path.

#### Scenario: Diagnostics inspect all configured domains
- **WHEN** the user runs diagnostics for a project
- **THEN** the output reports relevant Skill link health, agent support, and Memory integration availability without mutating any resource

### Requirement: No managed-resource code execution
The system SHALL NOT execute scripts, install dependencies, or fetch external resources contained in Skills or SubAgents while cataloging, validating, installing, diagnosing, reconciling, or undoing operations.

#### Scenario: Managed resource contains an executable script
- **WHEN** the system manages a resource that contains executable files
- **THEN** it performs only filesystem and metadata operations without executing those files

#### Scenario: Executable content remains opaque across filesystem lifecycle paths
- **GIVEN** a managed resource contains an executable file whose execution would leave an observable side effect
- **WHEN** the system validates or catalogs the resource, copies or snapshots it, rolls back an interrupted publication, or restores it through undo
- **THEN** it preserves the executable file as bytes and metadata only, never invokes it, and leaves the observable side effect absent
