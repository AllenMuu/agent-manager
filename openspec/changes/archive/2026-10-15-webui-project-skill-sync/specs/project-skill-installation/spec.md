## ADDED Requirements

### Requirement: Configure managed Skill synchronization mode

Agent Manager SHALL store a machine-local materialization mode for managed Skill activations: `auto`, `symlink-only`, or `copy-only`. The default SHALL be `auto`. The Web console SHALL show the configured repository path and provide controls for these three modes with a Save action. The selected mode SHALL apply to future Global and Project activations and updates through both CLI and Web console. Changing the mode SHALL NOT alter existing activations until the operator explicitly requests resynchronization.

#### Scenario: No mode is configured
- **WHEN** Agent Manager loads a configuration without a synchronization mode
- **THEN** it uses `auto` for later activation and update operations

#### Scenario: A supported mode is configured
- **WHEN** the operator selects `auto`, `symlink-only`, or `copy-only`
- **THEN** later activation previews identify the selected mode and apply that mode after confirmation

#### Scenario: The operator saves a mode in the Web console
- **WHEN** the operator selects a supported mode and saves the synchronization setting
- **THEN** the setting is persisted locally, the console shows the current repository path and selected mode, and existing activations remain unchanged

#### Scenario: An unsupported mode is configured
- **WHEN** the configuration contains an unknown synchronization mode
- **THEN** Agent Manager reports the invalid setting and performs no activation or resynchronization

#### Scenario: The operator changes the mode
- **WHEN** the operator changes the configured mode while managed Skills are already active
- **THEN** the existing destinations remain unchanged until a separately previewed resynchronization is confirmed

### Requirement: Preview and apply resynchronization of managed activations

The Web console SHALL let the operator preview resynchronization of existing Agent Manager-managed Skills for either the user's Global scope or the registered Project scope. The preview SHALL identify affected Skills, targets, destination paths, current state, and proposed materialization mode. Each confirmed resynchronization SHALL be atomic within its selected scope, use that scope's reversible operation journal, and refuse unmanaged conflicts. Unmanaged or orphaned resources SHALL NOT be adopted or overwritten by resynchronization.

#### Scenario: Preview a Global resynchronization
- **WHEN** the operator selects Global scope and requests resynchronization
- **THEN** the console previews only currently managed Global Skill activations and their destinations

#### Scenario: Preview a Project resynchronization
- **WHEN** the operator selects Project scope and requests resynchronization
- **THEN** the console previews only currently managed activations for the registered project

#### Scenario: Resynchronization is not confirmed
- **WHEN** the operator reviews a resynchronization plan but does not confirm it
- **THEN** all Global and Project destinations remain unchanged

#### Scenario: A destination changed after preview
- **WHEN** a selected managed destination or repository version changes before confirmation
- **THEN** Agent Manager rejects the stale plan without changing any destination or journal and requires a fresh preview

#### Scenario: A resync preview expires while waiting for the scope lock
- **WHEN** a confirmed resynchronization waits for another local operation until its preview expires
- **THEN** Agent Manager rejects the resynchronization without changing any destination or journal and requires a fresh preview

#### Scenario: Resynchronization succeeds
- **WHEN** the operator confirms a current resynchronization plan for one scope
- **THEN** all planned managed activations in that scope use the configured materialization mode and one reversible journal operation records the batch

#### Scenario: Resynchronization encounters an unmanaged destination
- **WHEN** a planned destination is no longer owned by Agent Manager
- **THEN** Agent Manager refuses the affected batch without replacing that content

#### Scenario: Resynchronization is undone
- **WHEN** the operator confirms Undo for the latest resynchronization and affected destinations still match its recorded after-state
- **THEN** Agent Manager restores each destination to its exact pre-resynchronization state and leaves repository versions unchanged

### Requirement: Preserve guarded and reversible batch behavior

The system SHALL apply a confirmed multi-Skill installation as one guarded operation, record the resulting changes in the applicable Global or Project operation journal, and support Undo through the existing scoped undo workflow. If any selected activation fails before the journaled operation is committed, the system SHALL restore every destination changed by that installation while preserving state not owned by the transaction. The configured materialization mode SHALL determine how each eligible repository Skill is made available in an Agent directory: `symlink-only` SHALL create an absolute soft link, `copy-only` SHALL copy the validated Skill tree, and `auto` SHALL try an absolute soft link first and fall back to copying only when the platform cannot create that link. Copies SHALL be identifiable as managed through reversible operation records without adding a committed Project manifest. Installation SHALL NOT modify source Skill versions, execute Skill content, or access external sources.

#### Scenario: Batch installation succeeds
- **WHEN** a user confirms a plan containing multiple Skills and every activation succeeds
- **THEN** Agent Manager materializes each selected Skill using the configured mode and records one reversible operation for the batch

#### Scenario: Automatic mode cannot create a soft link
- **WHEN** the platform reports that creating a soft link is unavailable for a selected destination
- **THEN** `auto` copies the validated Skill tree to that destination and reports the resulting materialization method

#### Scenario: Automatic mode encounters an unrelated write failure
- **WHEN** soft-link publication fails for a reason other than platform support or permission
- **THEN** `auto` does not hide the failure by copying and the batch follows the normal atomic rollback path

#### Scenario: Link-only mode cannot create a soft link
- **WHEN** a user confirms an installation in `symlink-only` mode but the platform cannot create a soft link
- **THEN** Agent Manager fails the batch without copying the Skill or leaving a partial activation

#### Scenario: Copy-only mode activates a Skill
- **WHEN** a user confirms an installation in `copy-only` mode
- **THEN** Agent Manager copies the selected repository version into the requested Agent directory without creating a link

#### Scenario: A confirmed installation fails partway through
- **WHEN** an activation in a confirmed batch fails before the operation journal is committed
- **THEN** Agent Manager restores all transaction-owned destinations changed by the batch and preserves unrelated concurrent destination state

#### Scenario: A confirmed installation is undone
- **WHEN** the operator undoes the latest successful installation
- **THEN** Agent Manager restores the affected destinations to their pre-installation link, copy, or absent state and leaves the repository Skills unchanged

#### Scenario: Independent installations are confirmed concurrently
- **WHEN** separate CLI or Web console processes confirm installations for the same scope at the same time
- **THEN** Agent Manager serializes their mutations and journal commits so each successful operation remains independently undoable
