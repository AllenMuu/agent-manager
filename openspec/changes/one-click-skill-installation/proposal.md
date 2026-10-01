## Why

Agent Manager can already activate Skills from the local library through separate CLI workflows, while project recommendations are read-only and there is no shared WebUI installation path. A unified install workflow will let users move from a selected library Skill or recommendation to a guarded project activation from either interface.

## What Changes

- Add a project Skill installation workflow to the CLI and a minimal React WebUI for one or more user-selected Skills and target agents.
- Allow recommendation results to provide Skills to this workflow without making recommendation itself mutate project state.
- Route both interfaces through the existing guarded lifecycle service so plans, compatibility warnings, conflicts, confirmation, journaling, rollback, and undo remain consistent.
- Limit this change to Skills already present in the configured local Skill library; do not fetch remote sources or execute Skill content.

## Capabilities

### New Capabilities

- `project-skill-installation`: Install selected local-library Skills into a project through the CLI or WebUI using the shared guarded lifecycle.

### Modified Capabilities

None. The workflow composes the existing project Skill lifecycle and read-only recommendation capabilities without changing their contracts.

## Impact

The CLI command surface, Go application-service boundary, and React WebUI will gain a shared project-install workflow. The WebUI will be served by a loopback-only local process and use existing adapter placement, operation journal, and undo behavior; Skill content will not be fetched externally or executed.
