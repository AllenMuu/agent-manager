## Why

The Web console can browse the managed repository and inspect project Skills, but users cannot gather existing global and project Skills into that repository or choose how managed Skills are synchronized into agent directories. This leaves reusable Skills scattered across locations and forces users to accept the current symlink-only behavior.

## What Changes

- Add a Web console workflow that discovers eligible Skills in supported global and registered-project locations, previews import into the configured versioned repository (default `~/.agent-manager/skills`), and preserves source copies and provenance.
- Add a local synchronization setting with `auto` (symlink first, copy fallback), `symlink-only`, and `copy-only` modes. New activations and updates use the setting; changing it does not rewrite existing activations.
- Add a reviewable “resync now” operation for already managed global or current-project Skills, with per-scope atomic apply and Undo.
- Show the configured repository, current synchronization mode, source scopes, outcomes, and safety diagnostics in bilingual Web UI.
- Update the activation ownership contract so journaled copies remain identifiable and reversible without adding a committed project manifest.

## Capabilities

### New Capabilities

- `webui-skill-source-sync`: Discover, preview, and explicitly import eligible global and project Skills into the configured versioned repository through the local Web console.

### Modified Capabilities

- `project-skill-installation`: Support configurable link/copy materialization for future activations and updates, plus guarded resynchronization of existing global or project activations.

## Impact

- `internal/config`, `internal/adapter`, `internal/lifecycle`, `internal/operation`, `internal/skillrepo`, and `internal/webconsole`.
- The React console under `web/src`, localization, and embedded UI assets.
- The project activation ownership decision in `docs/adr/0001-project-state-is-derived-from-links.md` and the related OpenSpec capability.
- No new dependencies, network access, or Skill execution.
