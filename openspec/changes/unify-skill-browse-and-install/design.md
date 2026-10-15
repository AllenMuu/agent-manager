## Context

The local Web console has a Skills view for catalog search, details, and single-Skill activation. A second React application at `/install/` shows project recommendations and the eligible catalog, then uses the existing batch installation preview and apply endpoints. Both run in the same authenticated local process, but the iframe and separate navigation entry split discovery and action.

The unified workspace is a presentation and interaction change. The Go services, session security, recommendation results, and both guarded mutation paths remain authoritative.

## Goals / Non-Goals

**Goals:**

- Give Skills one entry point in the main console and preserve guarded browse-to-install operations.
- Let a selection span recommendations and the catalog without duplicate selection for the same Skill identifier.
- Use one installation action for any non-empty selection, with explicit target selection and preview.
- Keep the workspace usable on desktop and mobile.

**Non-Goals:**

- Change Skill eligibility, recommendation ranking, target compatibility, installation semantics, operation journaling, or undo behavior.
- Add or alter API endpoints, CLI commands, network access, or Skill execution.
- Change the installation flow's explicit target selection or guarded confirmation behavior.

## Decisions

### 1. Host one Skills workspace in the main console

The main console React application owns the unified workspace and its selection state. The sidebar exposes one Skills entry. The `/install/` path remains available for compatibility and opens the same console application with Skills selected; it no longer serves a separate embedded installation client. Recommendations and catalog results share selection by Skill identifier, so the same Skill appearing in both sections remains one logical selection.

On desktop, recommendations appear first, followed by the searchable eligible catalog, with one detail panel. On mobile, these sections form a continuous vertical flow in the order recommendations, catalog, and inline details. A selection action bar stays pinned to the bottom on mobile, and the page must not overflow horizontally.

The console host is selected because it already owns project context, authenticated requests, navigation, and the local session used by installation previews. Keeping the second install application as host would preserve two navigation models, while embedding both as panes would retain duplicated browsing surfaces and complicate shared selection.

### 2. Use the installation preview for every non-empty selection

With one or more Skills selected, the workspace offers the installation flow backed by `/api/install/preview` and `/api/install/apply`. The operator explicitly chooses supported target agents, reviews the plan, and separately confirms installation. With no selection, no mutation action is available.

Changing the selection, installation targets, or conflict setting while a preview is open clears that preview. Canceling a preview leaves the selection intact; a successful installation clears selection and refreshes project inventory and the latest operation.

The installation service accepts one or more selected Skills and already provides guarded planning and confirmation, so the workspace uses the same path regardless of selection count.

### 3. Keep loading and stale-state errors local to the workspace

If recommendations fail, the workspace shows a local error and retry while keeping the catalog usable. If catalog loading fails, mutation actions remain disabled. If a selected Skill is no longer eligible or present in the catalog, mark it unavailable and require the user to deselect it before mutation.

Changing selected Skills, targets, or the conflict setting clears any stale installation preview. If the server rejects a stale preview, show the updated preview and require a new review and confirmation. These states do not change server-side authorization or apply-time revalidation.

## Risks / Trade-offs

- Shared selection can make an item selected in one section appear selected in another; use one identifier-keyed state and show the selection consistently in both places.
- Explicit targets are required even for one selected Skill; keep the target controls visible before preview.
- The `/install/` compatibility route can drift from the console's initial state; cover the route as part of the console navigation behavior.

## Migration Plan

1. Move the unified Skills workspace into the console React application and consolidate its sidebar entry.
2. Route `/install/` to the console's Skills view and stop serving the separate install client there.
3. Reuse the existing installation preview/apply interaction for one or more selected Skills.
4. Remove the obsolete embedded install application after its behavior is represented in the unified workspace.

Rollback restores the separate install client route and its navigation entry. No project files, Skill library content, or operation journals require migration.
