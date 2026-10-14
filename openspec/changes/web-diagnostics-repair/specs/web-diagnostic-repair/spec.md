## Purpose

The Web console helps operators identify project and Skill-library problems, safely repair supported cases, and understand which findings require manual work.

## ADDED Requirements

### Requirement: Doctor findings contain diagnostic information only
The Doctor view SHALL report Skill catalog and project integration problems without including static adapter capability inventory. Adapter capabilities SHALL remain available in the agent inventory view.

#### Scenario: Inspect a healthy project
- **WHEN** the operator runs Doctor and no catalog or project integration problem exists
- **THEN** the result contains no adapter capability entries

#### Scenario: Inspect a project with a diagnostic finding
- **WHEN** a catalog or project integration problem is found
- **THEN** the result identifies the finding code, relevant path, and any available repair action or manual guidance

### Requirement: Repair supported findings through guarded operation plans
The Web console SHALL offer a reviewed repair plan for an eligible orphaned managed Skill link and for a managed absolute link that Git would otherwise track. A repair SHALL be bound to the registered project and the exact finding, revalidated before publication, explicitly confirmed, and journaled for Undo.

#### Scenario: Preview reconciliation for one orphaned managed link
- **WHEN** the operator requests repair for an orphaned managed link with an eligible replacement in the configured library
- **THEN** the console previews only that link's replacement, leaves project state unchanged, and offers confirmation

#### Scenario: Apply a confirmed reconciliation
- **WHEN** the operator confirms a current reconciliation plan
- **THEN** the console repoints the selected managed link, records the operation, and refreshes Doctor findings

#### Scenario: Preview a Git ignore rule
- **WHEN** the operator requests repair for a managed link that would be tracked and is not already ignored
- **THEN** the console previews the exact project-relative ignore rule and leaves project and journal state unchanged until confirmation

#### Scenario: Apply a confirmed Git ignore rule
- **WHEN** the operator confirms a current Git ignore plan
- **THEN** the console appends only the exact managed-link rule, journals the change, and refreshes Doctor findings

#### Scenario: Repair plan becomes stale
- **WHEN** the selected finding, project, library, journal, or planned path changes after preview
- **THEN** execution is rejected without applying the reviewed mutation and the operator is asked to rescan

### Requirement: Findings without safe automatic repair include guidance
The Web console SHALL explain when a finding requires manual review and SHALL NOT automatically alter unmanaged resources, ambiguous dangling links, invalid Skill content, unsupported agent locations, or Git index state.

#### Scenario: Ambiguous dangling link
- **WHEN** a dangling link cannot be proven to belong to Agent Manager
- **THEN** the console provides the path and manual ownership guidance without offering reconciliation

#### Scenario: Already tracked managed link
- **WHEN** a managed absolute link is already present in the Git index
- **THEN** the console explains that an ignore rule alone does not untrack it and does not stage or remove the path from the index

#### Scenario: Invalid catalog entry or unmanaged resource
- **WHEN** Doctor reports invalid Skill data or a resource that Agent Manager does not own
- **THEN** the console explains the required manual review and offers no automatic file edit

### Requirement: Project scan notes are distinct from Doctor findings
The workspace SHALL label project traversal notes separately from Doctor findings, and the Diagnostics view SHALL present the two sources in separate sections.

#### Scenario: View project scan notes
- **WHEN** the static project scan is partial or has unreadable paths
- **THEN** the workspace count refers to those scan notes and the Diagnostics view displays them separately from Doctor findings

#### Scenario: No project scan notes
- **WHEN** the static project scan completes without notes
- **THEN** the workspace reports zero project scan notes without implying that Doctor found zero issues
