## MODIFIED Requirements

### Requirement: Preview and apply resynchronization of managed activations

The Web console SHALL let the operator preview resynchronization of existing Agent Manager-managed Skill entries for either the user's Global scope or the registered Project scope. Each entry SHALL be identified by scope, target Agent, and entry path, and SHALL retain its own Skill identity, exact repository version, content checksum, current materialization, and proposed materialization. Entries with the same Skill ID MAY use different repository versions. Each confirmed resynchronization SHALL be atomic within its selected scope, use that scope's reversible operation journal, and refuse unmanaged conflicts. Unmanaged or orphaned resources SHALL NOT be adopted or overwritten by resynchronization.

#### Scenario: Preview a Global resynchronization
- **WHEN** the operator selects Global scope and requests resynchronization
- **THEN** the console previews only currently managed Global entries and their exact destination paths and versions

#### Scenario: Preview a Project resynchronization
- **WHEN** the operator selects Project scope and requests resynchronization
- **THEN** the console previews only currently managed entries for the registered project

#### Scenario: Same Skill ID has different versions at two entries
- **WHEN** two managed entries in one scope share a Skill ID but reference different valid repository versions
- **THEN** the preview keeps each entry bound to its own version and resynchronization can rebuild both without merging or blocking the scope

#### Scenario: Resynchronization is not confirmed
- **WHEN** the operator reviews a resynchronization plan but does not confirm it
- **THEN** all Global and Project destinations remain unchanged

#### Scenario: A destination changed after preview
- **WHEN** a selected managed destination or exact repository version changes before confirmation
- **THEN** Agent Manager rejects the stale plan without changing any destination or journal and requires a fresh preview

#### Scenario: A resync preview expires while waiting for the scope lock
- **WHEN** a confirmed resynchronization waits for another local operation until its preview expires
- **THEN** Agent Manager rejects the resynchronization without changing any destination or journal and requires a fresh preview

#### Scenario: Resynchronization succeeds
- **WHEN** the operator confirms a current resynchronization plan for one scope
- **THEN** each planned entry uses its bound repository version and configured materialization mode, and one reversible journal operation records the batch

#### Scenario: Resynchronization encounters an unmanaged destination
- **WHEN** a planned destination is no longer owned by Agent Manager
- **THEN** Agent Manager refuses the affected batch without replacing that content

#### Scenario: Resynchronization is undone
- **WHEN** the operator confirms Undo for the latest resynchronization and affected destinations still match its recorded after-state
- **THEN** Agent Manager restores each destination to its exact pre-resynchronization state, version, and materialization method and leaves repository versions unchanged

### Requirement: Migrate selected existing Skill references

Agent Manager SHALL provide a separate, explicitly confirmed workflow to migrate only selected existing symbolic-link entries beneath supported Claude Code, Codex, and Pi Global or registered-Project Skill locations. A migration preview SHALL bind each selected entry to its complete current link chain, original link text, source checksum, exact matching repository version, destination state, and configured materialization mode. The browser SHALL submit only discovered entry identifiers and a server-issued plan identifier, never filesystem paths. Migration SHALL preserve source directories and unselected entries, SHALL NOT activate additional Skills, and SHALL NOT upgrade an entry to a different current repository version.

Neutral `.agent` and `.agents` entries, standalone real directories, invalid or modified links, and links whose content cannot be matched to an exact repository version SHALL remain import-only or blocked. A multi-hop source link SHALL migrate directly to the selected immutable repository version. A successfully migrated entry SHALL become a managed activation in its existing scope journal and SHALL record its exact version and actual materialization method.

#### Scenario: Migrate a selected supported link
- **WHEN** the operator confirms a current migration preview for one existing adapter link whose content matches an immutable repository version
- **THEN** only that selected entry is replaced with the previewed link or owned copy and becomes a journal-managed activation bound to that exact version

#### Scenario: Migrate a link after import reused an existing version
- **WHEN** the selected source record refers to a version already present in the repository and current source state still matches the preview
- **THEN** the entry can be migrated to the exact reused version without requiring another import

#### Scenario: Do not migrate a real directory or neutral entry
- **WHEN** the operator requests migration of a real-directory entry or an entry under `.agent/skills` or `.agents/skills`
- **THEN** the service blocks that entry and leaves it unchanged

#### Scenario: Source changes after preview
- **WHEN** the link chain, source checksum, destination, repository version, or sync setting changes after preview
- **THEN** Agent Manager rejects the stale plan before writing and requires a new preview

#### Scenario: Several references share one source
- **WHEN** multiple entries point to the same source and the operator selects only one of them
- **THEN** only the selected entry is migrated and every unselected reference and source directory remains unchanged

#### Scenario: A Global link has unregistered project dependents
- **WHEN** a Global entry selected for migration may be used indirectly by projects outside the registered scan
- **THEN** the preview warns that those projects will pass through the new target, verifies the observed content before and after, and does not claim those projects are registered or pinned

#### Scenario: A project directly links to the old source
- **WHEN** an unscanned project directly references the original source directory
- **THEN** migration preserves the source directory and that project continues to resolve its original reference

### Requirement: Persist and recover interrupted Skill mutations

Before the first target mutation in an import, migration, resynchronization, or Undo batch, Agent Manager SHALL durably persist the batch identity, scope, affected resources, before-state, required recovery data, and prepared status. Before each publication it SHALL persist the intended after-state and verifiable transaction evidence; after publication it SHALL persist progress. Completion SHALL be recorded only after all writes are verified and the relevant success journal and source-discovery records are durable. If the filesystem cannot provide the required persistence guarantee, the operation SHALL be blocked before mutation.

On startup and when opening Storage & Sync, Agent Manager SHALL inspect incomplete batches without changing files. An incomplete batch SHALL be shown as pending recovery and SHALL block new writes that overlap its resources. The operator SHALL review and confirm a recovery plan before Agent Manager completes or rolls back the batch. Recovery SHALL be repeatable; it SHALL preserve backups and current state whenever ownership, content, or dependencies cannot be verified, and SHALL NOT overwrite concurrent user changes.

#### Scenario: Prepare before the first write
- **WHEN** the required backup or prepared record cannot be durably stored
- **THEN** Agent Manager blocks the operation without modifying any destination

#### Scenario: Process stops around publication
- **WHEN** the process stops before or after a file publication but before the batch is committed
- **THEN** the next recovery preview compares durable intent, backups, and actual files and reports whether completion, safe rollback, or conflict is possible

#### Scenario: Pending batch overlaps a new write
- **WHEN** a new operation would modify a resource owned by an incomplete batch
- **THEN** Agent Manager blocks that write until the pending batch is recovered

#### Scenario: Recovery is interrupted and retried
- **WHEN** the process stops during a confirmed recovery and the operator opens recovery again
- **THEN** the same batch can be safely previewed and completed or rolled back without duplicating a journal entry or overwriting unrelated state

#### Scenario: Current state cannot be attributed to the batch
- **WHEN** a destination or dependency no longer matches either the recorded before-state or the intended transaction-owned after-state
- **THEN** recovery reports a conflict and preserves the current path and all needed backups

### Requirement: Validate Undo against the planned restored dependency view

Before Undo, Agent Manager SHALL validate the filesystem as it would exist after applying this batch's recorded before-states, rather than following only the current link chain. It SHALL distinguish entries restored by the same batch from external dependency nodes, verify external nodes against persisted link-chain and content evidence, restore the original link text, and refuse Undo if a batch-external dependency has changed, become invalid, or resolves to different content. Global and Project operations SHALL remain independently undoable and SHALL NOT be automatically undone together.

#### Scenario: Undo a same-batch link chain
- **WHEN** two entries in one batch form a link chain and Undo restores the upstream entry to the downstream entry's pre-batch state
- **THEN** validation evaluates the simulated restored chain, accepts verified batch-owned changes, and restores the original links in a safe order

#### Scenario: External dependency changed
- **WHEN** an external link or source differs from its persisted before-state evidence
- **THEN** Agent Manager blocks Undo, reports the dependency conflict, and leaves the current entries unchanged

#### Scenario: Restore a relative source link
- **WHEN** a migrated entry's recorded original reference used relative link text and all dependencies still match
- **THEN** Undo restores that exact relative link text and preserves the imported repository version

#### Scenario: Project and Global operations depend on each other
- **WHEN** a Project migration was previewed after a Global migration and the operator attempts to undo Global first
- **THEN** the Project undo preview is invalidated or blocked with the dependency reason, and Agent Manager recommends undoing the Project operation first without chaining the undo automatically
