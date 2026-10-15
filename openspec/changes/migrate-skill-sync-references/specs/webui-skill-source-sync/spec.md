## MODIFIED Requirements

### Requirement: Discover global and registered-project Skill sources

The Web console SHALL inspect the current user's supported global Skill locations and the registered project's supported Skill locations. Supported locations SHALL include the Claude Code, Codex, and Pi adapter Skill directories, plus neutral `agent` and `agents` Skill directories (`~/.agent/skills`, `~/.agents/skills`, `<project>/.agent/skills`, and `<project>/.agents/skills`). Each discovered entry SHALL identify its scope, source location, entry type, resolved Skill directory, content checksum, and separate import and migration eligibility. Discovery SHALL read Skill metadata and content only; it MUST NOT execute Skill content, access the network, accept caller-selected filesystem roots, include the configured repository as an ordinary source, or recurse into siblings of an external link target.

Each supported source root and its ancestors SHALL be verified as real directories. A child entry MAY be a real directory or a symbolic link. For an entry link, the console SHALL preserve the original link text and resolve relative and multi-hop links up to 16 hops. The final target MAY be outside the source root, but only that Skill tree SHALL be inspected. The source tree itself SHALL contain only regular files and real directories; internal symbolic links and special files SHALL make that source ineligible. A source target or tree that overlaps the configured repository SHALL be classified as managed or blocked and SHALL NOT be copied into itself.

Discovery SHALL inspect at most 4,096 immediate entries per scope and at most 2,048 regular files or 64 MiB per Skill tree. Exceeding a limit, encountering a cycle, dangling link, inaccessible path, or unsupported entry SHALL produce an actionable diagnostic and mark the inventory incomplete where entries may have been missed. Only entries under the Claude Code, Codex, or Pi adapter roots SHALL be migration-eligible, and only when the entry already exists as a symbolic link. Neutral roots and real-directory entries SHALL remain import-only.

#### Scenario: View Skills from both scopes
- **WHEN** the registered project and user home contain eligible Skills in supported locations
- **THEN** the console lists global and project Skills in distinct, source-labeled groups and shows each entry's import and migration eligibility

#### Scenario: Source locations are absent
- **WHEN** one or more supported Skill directories do not exist
- **THEN** discovery treats those locations as empty and reports other sources normally

#### Scenario: Resolve a supported entry link
- **WHEN** a supported adapter entry is a relative or multi-hop symbolic link whose chain is valid and no longer than 16 hops
- **THEN** discovery shows the original link text, every link hop, the final Skill directory and checksum, and inspects only that Skill tree

#### Scenario: Link target is outside the scanned root
- **WHEN** a supported entry link resolves to a valid Skill directory outside its source root
- **THEN** discovery may import that Skill tree, displays the external target, and does not scan sibling directories

#### Scenario: A source contains unsafe content
- **WHEN** a source root or its ancestor is a symbolic link, or the Skill tree contains an internal symbolic link or special file
- **THEN** discovery marks that source ineligible, does not follow the unsafe path, and reports an actionable diagnostic

#### Scenario: A link is invalid or exceeds a bound
- **WHEN** an entry link is dangling, cyclic, inaccessible, deeper than 16 hops, or a scan/content limit is exceeded
- **THEN** discovery reports the reason, marks the affected source ineligible, and reports an incomplete inventory when candidates may have been omitted

#### Scenario: Neutral or real-directory entry is selected for migration
- **WHEN** a user selects a neutral-root entry or an adapter entry that is a real directory for migration
- **THEN** the service refuses the migration while leaving that source available for import

#### Scenario: Multiple entries point to the same real Skill directory
- **WHEN** two or more supported entries resolve to the same real Skill directory
- **THEN** discovery reads and hashes the content once, preserves every entry and link-chain relationship, and groups them as one content candidate

#### Scenario: A browser supplies an arbitrary source path
- **WHEN** a request attempts to select a source root or entry path outside the server-discovered inventory
- **THEN** the service rejects the request without reading or changing that path

### Requirement: Record confirmed source discovery relationships

For each confirmed import selection, Agent Manager SHALL persist a local source-discovery record separately from immutable Skill-version provenance. The record SHALL contain a unique record ID, import batch ID, observation and confirmation times, scope, discovered entry, original link text and resolved link chain when present, final directory, content checksum, Skill identity, exact repository version, and whether the version was added or reused. Unknown fields SHALL be represented as unknown. A retry of the same batch and source SHALL be idempotent. Recording a newly observed source for an existing version SHALL NOT rewrite that version's provenance or trust state.

#### Scenario: Confirm an import that adds a version
- **WHEN** a confirmed import publishes a new immutable version and its source record is durably committed
- **THEN** the result identifies the exact version and source record, and the record reflects the observed source relationship

#### Scenario: Confirm an import that reuses a version
- **WHEN** a confirmed import finds that the selected content already exists in the repository
- **THEN** Agent Manager records the new source relationship and exact reused version without rewriting version provenance or trust state

#### Scenario: Retry source-record persistence
- **WHEN** recovery retries source-record persistence for the same import batch and source
- **THEN** it completes the existing record idempotently without creating a duplicate record

#### Scenario: Source evidence changes after import
- **WHEN** a later migration uses a stored source-discovery record
- **THEN** Agent Manager revalidates the current entry, link chain, and content against the migration preview instead of trusting the historical record alone

### Requirement: Recover interrupted source imports

Before publishing an imported version, Agent Manager SHALL durably persist a prepared batch record and the evidence needed to determine whether publication and source-record persistence completed. A confirmed import SHALL NOT be reported as complete until its versions and source-discovery records are committed. If a process stops between publication and commit, Agent Manager SHALL retain published immutable versions, backups, and pending evidence; it SHALL expose a recovery preview and SHALL NOT start reference migration for that batch until recovery is confirmed.

#### Scenario: Import is interrupted before publication
- **WHEN** the process stops after preparing an import but before publishing any version
- **THEN** the next recovery preview identifies the pending batch and offers safe completion or rollback without changing source entries

#### Scenario: Source-record commit fails after version publication
- **WHEN** an immutable version is published but the source-discovery record cannot be committed
- **THEN** the version remains, the batch is reported as pending recovery, and migration remains unavailable until the record is safely completed

#### Scenario: Import is interrupted during publication
- **WHEN** the process stops while publishing a confirmed batch
- **THEN** recovery uses durable intent and actual repository state to identify transaction-owned changes, preserves unrelated changes, and requires explicit confirmation before completion or rollback
