## Why

Users cannot tell where a Skill came from or whether a remotely installed Skill has changed upstream. When several local copies diverge, the synchronization view also lacks enough evidence to compare them, even though it correctly requires an explicit current-version choice.

## What Changes

- Show recorded source metadata in the Skill library/management view and source labels in synchronization candidates.
- Show each synchronization candidate's newest local file modification time, source path, and content checksum; identify the candidate with the newest comparable local timestamp without treating it as a version number or choosing it automatically.
- Add an explicit upstream update check for Skills with supported non-local provenance, plus actions to prepare all available updates or a selected subset for review and confirmation.
- Apply confirmed updates reversibly, preserving update metadata and never executing Skill content. Local or unsupported origins remain visible and explain why they cannot be checked.
- Keep local source discovery offline; only a user-initiated update check or confirmed update may access the supported upstream service.

## Capabilities

### New Capabilities

- `skill-upstream-updates`: source attribution, candidate comparison evidence, and explicit upstream update checking and application.

### Modified Capabilities

- None. Existing synchronization discovery remains local and offline; the new capability adds a separate explicit network action.

## Impact

- Affected code: `internal/skillsync`, `internal/webconsole/sync_api.go`, `internal/webconsole` catalog API, and the Web console Skill and Storage & Sync pages.
- Affected systems: Vercel Skills CLI global `.skill-lock.json` and project `skills-lock.json` metadata, supported GitHub repositories, and a local reversible update journal.
- No new dependency. Outbound requests are limited to the official GitHub API and raw content host, with bounded requests and responses.
- Update checks are read-only. Applying an update requires a preview and explicit confirmation; only regular files under a safely discovered Skill directory are eligible.
