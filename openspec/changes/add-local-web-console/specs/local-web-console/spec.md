## Purpose

The local Web console gives an operator one readable workspace for inspecting a registered project and using Agent Manager's existing local resource capabilities through a browser.

## ADDED Requirements

### Requirement: Loopback service session
The system MUST serve the console only on the IPv4 loopback interface. Each process session MUST create an unpredictable bearer token, require it for API requests, reject requests from origins other than the exact console origin, and discard the token when the process exits.

#### Scenario: Service starts on loopback
- **WHEN** the operator starts the Web console
- **THEN** the service listens on `127.0.0.1` and reports the local URL and session entry URL without binding to a LAN interface

#### Scenario: API request omits session token
- **WHEN** a caller requests an API route without the current session token
- **THEN** the service rejects the request without returning project or catalog data

#### Scenario: API request has an untrusted origin
- **WHEN** a caller sends an API request with an origin that is not the exact console origin
- **THEN** the service rejects the request and does not grant cross-origin access

#### Scenario: Process session ends
- **WHEN** the service process exits and a prior session token is reused
- **THEN** no later service session accepts that token

### Requirement: Register one canonical project
The system MUST allow the operator to register one existing local directory for the current service session. It MUST canonicalize and validate that directory once, issue a session-local project identifier, and use that identifier for subsequent project requests. The browser MUST NOT be given a general-purpose filesystem read API.

#### Scenario: Register a valid project
- **WHEN** an authenticated operator submits a path to an existing directory
- **THEN** the service canonicalizes the path, returns a project identifier, and makes that project the current project for the session

#### Scenario: Reject an invalid project
- **WHEN** an authenticated operator submits a missing path, a file, or an inaccessible directory
- **THEN** the service returns an actionable validation error and does not register the project

#### Scenario: Reuse the registered project
- **WHEN** a later project API request uses the session-issued project identifier
- **THEN** the service uses the canonical registered path without accepting replacement source or destination paths

#### Scenario: Request arbitrary file contents
- **WHEN** a caller requests file contents outside the supported project inspection operations
- **THEN** the service returns not found or not allowed and reads no caller-selected file

### Requirement: Read-only project workspace
The project workspace MUST aggregate current project information through existing Agent Manager services. It MUST show agent availability and capabilities, detected technologies with marker evidence, ranked Skill recommendations with reasons, and resource inventory classifications for managed, unmanaged, orphaned, and unsupported entries.

#### Scenario: Inspect a project with supported technologies
- **WHEN** the registered project contains recognized static project markers
- **THEN** the workspace reports detected technologies, their evidence paths, and recommendations from the configured Skill library

#### Scenario: Inspect a project with unknown or incomplete evidence
- **WHEN** a project has no recognized markers or scanning is incomplete
- **THEN** the workspace shows an actionable empty or partial state and any scan diagnostics without failing the whole workspace

#### Scenario: Inspect inventory with mixed ownership
- **WHEN** the registered project contains managed, unmanaged, orphaned, and unsupported resources
- **THEN** the workspace labels each entry with its correct ownership or support classification

#### Scenario: Read project state
- **WHEN** the workspace or a read API is loaded
- **THEN** no project file, Skill library entry, journal record, or global baseline is modified

### Requirement: Browse Skills and SubAgents
The console MUST provide Skill search and eligible Skill details from the configured library. It MUST expose canonical SubAgent definitions and validation results as read-only information and MUST NOT install or remove SubAgents.

#### Scenario: Search the Skill library
- **WHEN** the operator searches for a term
- **THEN** the console returns matching eligible Skills with identifiers and descriptions

#### Scenario: Inspect a Skill
- **WHEN** the operator opens a Skill detail
- **THEN** the console displays its supported metadata and readable `SKILL.md` content without executing it

#### Scenario: Inspect SubAgents
- **WHEN** the operator opens the SubAgents view
- **THEN** the console shows canonical definitions, referenced Skills, compatibility, capabilities, and validation diagnostics without mutation controls

### Requirement: Show diagnostics and latest operation
The console MUST expose current read-only doctor findings and the latest journaled operation using existing diagnostic and operation-journal behavior. Undo availability MUST reflect the console's project-scoped restoration boundary.

#### Scenario: View diagnostics
- **WHEN** the operator opens diagnostics
- **THEN** the console displays existing doctor findings and does not reconcile or otherwise modify project state

#### Scenario: View operation history
- **WHEN** an operation journal exists for the registered project
- **THEN** the console identifies the latest operation and whether it can be previewed for Undo

#### Scenario: Latest operation targets an unsupported path
- **WHEN** the latest journal entry includes a path outside console-managed project resource locations, including a shared Skill library path
- **THEN** the console may identify the operation but MUST NOT offer an Undo preview or executable Undo action

#### Scenario: No operation exists
- **WHEN** the project has no operation journal entry
- **THEN** the console shows an empty state and provides no executable Undo action

### Requirement: React console delivery
The Web console MUST provide the specified MVP views using the existing Go application behavior as its source of truth. The interface MUST remain usable at desktop and narrow mobile viewport widths, support keyboard navigation, expose visible focus, and label icon-only controls for assistive technology.

#### Scenario: Open the console on desktop
- **WHEN** the operator opens the local service at a desktop viewport
- **THEN** project navigation, status, recommendations, inventory, diagnostics, and operations are understandable without relying on decorative graphics

#### Scenario: Open the console on a narrow screen
- **WHEN** the operator uses the console at a 375-pixel viewport
- **THEN** content remains readable and actions remain reachable without horizontal page overflow

#### Scenario: Navigate with a keyboard
- **WHEN** the operator uses only a keyboard
- **THEN** every action is reachable in a logical order with visible focus and semantic controls
