## Purpose

Let users collect reusable Skills already present in supported agent-wide and registered-project locations into Agent Manager's configured versioned Skill repository through the local Web console, while preserving sources and requiring review before changes.

## ADDED Requirements

### Requirement: Discover global and registered-project Skill sources

The Web console SHALL inspect the current user's supported global Skill locations and the registered project's supported Skill locations. Supported locations SHALL include the Claude Code, Codex, and Pi adapter Skill directories, plus neutral `agent` and `agents` Skill directories (`~/.agent/skills`, `~/.agents/skills`, `<project>/.agent/skills`, and `<project>/.agents/skills`). It SHALL label each discovered Skill with its scope, source location, and eligibility. Discovery SHALL read Skill metadata and content only; it MUST NOT execute Skill content, access the network, accept caller-selected filesystem roots, or include the configured repository as a source.

#### Scenario: View Skills from both scopes
- **WHEN** the registered project and user home contain eligible Skills in supported locations
- **THEN** the console lists global and project Skills in distinct, source-labeled groups

#### Scenario: Source locations are absent
- **WHEN** one or more supported Skill directories do not exist
- **THEN** discovery treats those locations as empty and reports other sources normally

#### Scenario: A source contains unsafe content
- **WHEN** a Skill directory or one of its ancestors is a symlink, or its tree contains a symlink or special file
- **THEN** discovery marks that Skill ineligible, does not follow the unsafe path, and reports an actionable diagnostic

#### Scenario: A browser supplies an arbitrary source path
- **WHEN** a request attempts to select a source root outside the registered project and supported user-home locations
- **THEN** the service rejects the request without reading or changing that path

### Requirement: Review and select a synchronization plan

The console SHALL provide a read-only preview that lists selected Skills, their source scopes and paths, destination identifiers and versions in the configured repository, duplicate handling, and conflicts. The default repository SHALL be `~/.agent-manager/skills` unless another local repository is configured. A user SHALL be able to select individual eligible Skills or select all eligible Skills. Previewing SHALL NOT modify sources, repository contents, or operation history.

#### Scenario: Select all eligible Skills
- **WHEN** the user requests synchronization of all eligible Skills
- **THEN** the preview includes every eligible discovered Skill and leaves all filesystem state unchanged

#### Scenario: Identical content appears in multiple sources
- **WHEN** Skills with the same identifier and tree checksum are discovered from multiple source locations
- **THEN** the preview represents them as one destination version and retains all discovered source labels for provenance

#### Scenario: An identifier has divergent source content
- **WHEN** Skills with the same identifier have different tree checksums
- **THEN** the preview shows each content variant as a distinct version candidate and requires an explicit current-version choice when no repository current version already exists

#### Scenario: A version already exists in the repository
- **WHEN** a discovered Skill matches an existing repository version by identifier and checksum
- **THEN** the preview identifies it as already present and does not plan to rewrite it

#### Scenario: A destination conflict prevents safe import
- **WHEN** the repository contains an unsafe or inconsistent destination for a selected Skill
- **THEN** the preview reports the conflict and the service refuses to apply the affected selection until it is resolved

### Requirement: Confirm and atomically import synchronization sources

The service SHALL require explicit confirmation bound to the displayed preview before copying Skills into the configured repository. Before applying, it SHALL revalidate the registered project, source trees, repository state, and preview token; a stale plan SHALL make no changes. A confirmed batch SHALL be atomic and preserve all source locations. A failed batch SHALL roll back only filesystem state proven to belong to that transaction. Successful imports SHALL remain in the versioned repository because projects outside the currently registered scope may activate them; this workflow SHALL NOT offer to remove an imported version.

#### Scenario: Preview is not confirmed
- **WHEN** the user reviews a synchronization preview but does not confirm it
- **THEN** the source directories, repository, and operation journals remain unchanged

#### Scenario: The preview becomes stale
- **WHEN** a selected source tree, repository destination, or registered project changes after preview
- **THEN** the service rejects the confirmation without changing any source or repository entry and requires a fresh preview

#### Scenario: A confirmed batch succeeds
- **WHEN** the user confirms a current preview containing one or more eligible Skills
- **THEN** Agent Manager copies the selected new versions into the configured repository and preserves their source directories and provenance

#### Scenario: A confirmed batch fails partway through
- **WHEN** a filesystem failure interrupts synchronization
- **THEN** Agent Manager restores transaction-owned destination changes, preserves unrelated concurrent destination state and all sources, and leaves no versions from the failed batch

#### Scenario: An import preview expires while waiting for another local import
- **WHEN** a confirmed import waits for the shared import lock until its preview expires
- **THEN** Agent Manager rejects the import without changing the repository and requires a fresh preview

#### Scenario: A project outside the registered scope may activate an imported version
- **WHEN** an imported version is available to projects not registered with the current console session
- **THEN** Agent Manager keeps that version in the central repository and provides no import-removal action in this workflow

### Requirement: Keep synchronization within the authenticated local console

Synchronization SHALL use the existing Web console session and registered project. State-changing requests SHALL require the active bearer session, exact console origin, and existing local-console request protections. Browser requests SHALL NOT provide repository or source filesystem paths.

#### Scenario: Synchronization is requested outside the active session
- **WHEN** a preview or apply request lacks the active session authorization or trusted console origin
- **THEN** the service rejects the request without reading arbitrary paths or changing repository state

#### Scenario: Synchronization has no eligible Skills
- **WHEN** the user opens synchronization and no eligible source Skills are available
- **THEN** the console shows an empty state with scan diagnostics and offers no apply action
