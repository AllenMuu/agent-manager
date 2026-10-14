## Why

Agent Manager's roadmap has expanded into runtime lifecycle and control-plane work that overlaps with the agents and execution platforms it should support. Refocusing the product on project-aware Skills and shared Project Memory keeps its core around portable project knowledge; the current `~/.agents/skills` default also exposes the whole library to agents that auto-load `.agents`, so the canonical library needs an Agent Manager-specific location and a safe migration path.

## What Changes

- Set the product direction around managing Skills and shared Project Memory across coding agents; new runtime, orchestration, checkpoint, compaction, and provider-management features are outside the core roadmap.
- Establish `~/.agent-manager/skills` as the default local Skill repository and source of truth, with version and provenance metadata; agent-specific directories remain materialized targets.
- Provide an explicit, previewable migration/import from the legacy `~/.agents/skills` library. Require confirmation for each batch; import only selected Skills and leave legacy source files and existing links intact. Any later cleanup is a separate explicit operation.
- Keep Global and Project as the initial Skill scopes. Project activation remains machine-local and derived from managed links; do not add a committed project manifest.
- Reuse the existing deterministic project recommendation and guarded local installation flows. Keep broader Skill health and effective-set work behind the repository foundation, and continue Project Memory expansion after the Skill core; existing Codex/Pi shared-memory work remains available.
- Preserve already-delivered runtime, resource, and identity capabilities where they directly support Skill or Project Memory workflows, while treating unrelated runtime expansion as out of scope.

## Capabilities

### New Capabilities

- `skill-repository`: Define the canonical local Skill library, version/provenance metadata, agent-facing materialization, and explicit migration from `~/.agents/skills`.

### Modified Capabilities

- `skill-catalog`: Resolve one stable identity from the configured versioned repository and expose its selected version and provenance.
- `project-skill-installation`: Allow an install request to select an addressable repository version.
- `project-skill-recommendation`: Identify the current repository version for each read-only recommendation.

## Impact

This change affects the Skill catalog and installation contracts, local configuration and migration behavior, and the existing Claude/Codex/Pi adapter boundaries. The current CLI, recommendation, and guarded installation implementations should be reused where possible. No new dependency or network access is required for local library management. Project Memory behavior is not expanded in this change; its existing cross-agent capability remains a later roadmap phase.
