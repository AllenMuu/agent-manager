## Why

The Web console currently separates Skill browsing and project recommendations from Skill installation. Users must switch surfaces and rebuild their selection when they move from discovery to activation. A single Skills workspace can keep discovery, selection, and the appropriate guarded operation in one place.

## What Changes

- Consolidate the console's Skills and Skill installation navigation entries into one Skills workspace hosted by the main console React application.
- Show project recommendations before the searchable eligible catalog on desktop, with one Skill detail panel and shared selection by Skill identifier.
- Use the existing single-Skill activation plan flow for exactly one selected Skill and show the existing batch installation flow only when two or more Skills are selected.
- Keep `/install/` as a compatibility route that opens the unified console with Skills selected.
- Define a continuous mobile layout with recommendations, catalog, and inline details, plus a selection action bar pinned to the bottom.
- Preserve the current local-only session protections, recommendation behavior, installation APIs, and guarded lifecycle operations.

## Capabilities

### Modified Capabilities

- `local-web-console`: Provide one Skills navigation entry and a unified responsive workspace for recommendations, catalog browsing, details, and shared selection.
- `project-skill-installation`: Define how the unified workspace dispatches one selected Skill to the existing activation plan and multiple selected Skills to the existing batch preview/apply flow.

## Impact

The change affects the console's React navigation and Skills view, its compatibility route, and the associated user-facing specifications and architecture decision record. It does not require changes to Go domain services, resource lifecycle behavior, recommendation ranking, installation endpoints, journal format, or the CLI.
