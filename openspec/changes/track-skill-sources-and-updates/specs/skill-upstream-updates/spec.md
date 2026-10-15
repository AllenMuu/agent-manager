## Purpose

This capability makes Skill origin and local comparison evidence visible, and lets users explicitly check and apply upstream updates when trustworthy install metadata identifies a supported remote source.

## ADDED Requirements

### Requirement: Display Skill source and comparison evidence

The Skill management view SHALL display recorded source information for each library Skill, and SHALL label missing or local provenance as local or unknown instead of inferring a remote publisher from Skill-authored content. The synchronization view SHALL display each candidate's source label, source path, newest local file modification time, and content checksum. Timestamps SHALL be presented as local file metadata, not as a Skill version.

#### Scenario: A library Skill has recorded provenance
- **WHEN** a Skill version has source metadata in the configured library
- **THEN** the Skill management list and detail view show that source

#### Scenario: A synchronized candidate has comparable timestamps
- **WHEN** different-content candidates for one identifier have valid local modification timestamps
- **THEN** each candidate shows its timestamp and the candidate with a strictly newer comparable timestamp is labeled “本机修改时间较新” without being selected automatically

#### Scenario: A current-version choice is selected
- **WHEN** the user chooses one candidate as current for a divergent Skill
- **THEN** the selected candidate is visibly marked “已选为当前版本” and the existing explicit review and confirmation requirement remains in force

#### Scenario: Timestamp evidence is absent or tied
- **WHEN** a candidate has no readable modification time or candidates have equal newest times
- **THEN** the console shows available metadata without asserting that one candidate is newer

### Requirement: Check supported remote Skill updates on request

The synchronization view SHALL provide a user-initiated update check for discovered Skills whose install metadata records a non-local source. It SHALL read supported global and registered-project Skills CLI lock files without accepting caller-selected paths. The first supported remote provider SHALL be GitHub sources with a recorded Skill path and comparable stored content hash. Local sources SHALL not cause network requests. Missing metadata, unsupported providers, missing hashes or paths, and failed checks SHALL be reported distinctly from “up to date.”

Global Skills CLI lock entries use Git tree hashes and MAY include bounded multi-file Skills. Project lock entries use a locale-ordered content hash; until that ordering can be reproduced safely, Agent Manager SHALL check or update project entries only when the Skill has one tracked file. Multi-file project entries SHALL be shown as unsupported rather than reported as locally modified or current.

Network access SHALL occur only after an explicit user action and SHALL be limited to HTTPS requests to the official GitHub API and raw content host, with bounded time and response sizes and redirects rejected. The check SHALL compare upstream content hashes; it SHALL NOT execute Skill content, claim a semantic version when only hashes are available, or modify local files or lock files.

#### Scenario: A supported remote Skill has changed upstream
- **WHEN** the user checks updates and the recorded GitHub Skill hash differs from the current upstream hash
- **THEN** the console reports an available update with its source and current and upstream hashes

#### Scenario: A supported remote Skill is current
- **WHEN** the recorded hash matches the supported upstream Skill hash
- **THEN** the console reports that the Skill is up to date

#### Scenario: A local or unsupported Skill is discovered
- **WHEN** a Skill has local provenance, or its remote provider or tracking metadata is unsupported
- **THEN** the console performs no request for that Skill and explains why it cannot be checked

#### Scenario: An upstream request fails or is incomplete
- **WHEN** the upstream is unavailable, rate-limited, truncated, or returns invalid content
- **THEN** the console reports that the check could not be completed and does not report the Skill as current

### Requirement: Review and apply selected upstream updates reversibly

The synchronization view SHALL let the user prepare either all available updates or a selected subset. Before any local file or lock-file change, Agent Manager SHALL show the exact affected Skills, source, destination paths, and current/upstream hashes, then require explicit confirmation. A confirmed update SHALL replace only safely discovered Skill directories with validated regular files, update the matching lock metadata, and preserve a reversible before-state. Applying a batch SHALL either update every selected Skill and its metadata or restore the transaction-owned changes. Browser requests SHALL provide only discovered update identifiers, never filesystem paths, repository identifiers, URLs, or remote refs.

#### Scenario: The user reviews all available updates
- **WHEN** one or more supported Skills have upstream updates and the user chooses update all
- **THEN** the console prepares a review plan containing all available updates without changing local state

#### Scenario: The user reviews a selected subset
- **WHEN** the user selects specific available Skills and chooses update selected
- **THEN** the review plan contains only those selected updates

#### Scenario: The user confirms a current update plan
- **WHEN** the user confirms the displayed plan and its local Skill directories and lock metadata still match the reviewed state
- **THEN** Agent Manager applies the upstream files, updates tracking metadata, and retains enough before-state to undo the operation

#### Scenario: An update plan is stale or the batch fails
- **WHEN** a target, lock file, or upstream content changes after review, or a filesystem failure interrupts application
- **THEN** Agent Manager makes no partial update, preserves unrelated concurrent state, and requires a fresh review

#### Scenario: A remote Skill contains an unsafe filesystem entry
- **WHEN** an update contains a symlink, submodule, special file, invalid Skill, or content beyond supported limits
- **THEN** Agent Manager refuses that update without replacing the installed Skill

#### Scenario: The user cancels an update review
- **WHEN** the user cancels before confirmation
- **THEN** local Skill directories and lock metadata remain unchanged
