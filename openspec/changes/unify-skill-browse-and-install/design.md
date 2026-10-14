## Context

The local Web console has a Skills view for catalog search, details, and single-Skill activation. A second React application at `/install/` shows project recommendations and the eligible catalog, then uses the existing batch installation preview and apply endpoints. Both run in the same authenticated local process, but the iframe and separate navigation entry split discovery and action.

The unified workspace is a presentation and interaction change. The Go services, session security, recommendation results, and both guarded mutation paths remain authoritative.

## Goals / Non-Goals

**Goals:**

- Give Skills one entry point in the main console and preserve the existing browse-to-activation operations.
- Let a selection span recommendations and the catalog without duplicate selection for the same Skill identifier.
- Make the single-versus-batch action visible from the current selection count.
- Keep the workspace usable on desktop and mobile.

**Non-Goals:**

- Change Skill eligibility, recommendation ranking, target compatibility, installation semantics, operation journaling, or undo behavior.
- Add or alter API endpoints, CLI commands, network access, or Skill execution.
- Change the default Codex target for the existing single-Skill activation flow or the explicit target selection required by batch installation.

## Decisions

### 1. Host one Skills workspace in the main console

The main console React application owns the unified workspace and its selection state. The sidebar exposes one Skills entry. The `/install/` path remains available for compatibility and opens the same console application with Skills selected; it no longer serves a separate embedded installation client. Recommendations and catalog results share selection by Skill identifier, so the same Skill appearing in both sections remains one logical selection.

On desktop, recommendations appear first, followed by the searchable eligible catalog, with one detail panel. On mobile, these sections form a continuous vertical flow in the order recommendations, catalog, and inline details. A selection action bar stays pinned to the bottom on mobile, and the page must not overflow horizontally.

The console host is selected because it already owns project context, authenticated requests, navigation, and single-Skill activation plans. Keeping the second install application as host would preserve two navigation models, while embedding both as panes would retain duplicated browsing surfaces and complicate shared selection.

### 2. Select the guarded operation from the number of selected Skills

With exactly one Skill selected, the workspace offers the current project activation plan flow, including plan review followed by execute or cancel. It keeps the current single-activation Codex default. With two or more Skills selected, the workspace offers the batch installation action backed by `/api/install/preview` and `/api/install/apply`, with target selection explicit. With no selection, neither mutation action is available.

The batch action is hidden at one selection; the single activation action appears only at one. Changing selection while a batch preview is open clears that preview. Canceling a plan leaves the selection intact; a successful activation or installation clears selection and refreshes project inventory and the latest operation.

This keeps each established lifecycle intact while making the user choose Skills in one shared workspace. A new combined lifecycle was rejected because it would duplicate planning and confirmation behavior already provided by the console and installation services.

### 3. Keep loading and stale-state errors local to the workspace

If recommendations fail, the workspace shows a local error and retry while keeping the catalog usable. If catalog loading fails, mutation actions remain disabled. If a selected Skill is no longer eligible or present in the catalog, mark it unavailable and require the user to deselect it before mutation.

Changing selected Skills, batch targets, or the conflict setting clears any stale batch preview. If the server rejects a stale preview, show the updated preview and require a new review and confirmation. These states do not change server-side authorization or apply-time revalidation.

## Risks / Trade-offs

- Shared selection can make an item selected in one section appear selected in another; use one identifier-keyed state and show the selection consistently in both places.
- Reusing different operation flows by selection count may surprise users; label the action with the number of selected Skills and clearly distinguish activation plan from batch installation preview.
- The `/install/` compatibility route can drift from the console's initial state; cover the route as part of the console navigation behavior.

## Migration Plan

1. Move the unified Skills workspace into the console React application and consolidate its sidebar entry.
2. Route `/install/` to the console's Skills view and stop serving the separate install client there.
3. Reuse the existing single activation-plan and batch preview/apply interactions based on selection count.
4. Remove the obsolete embedded install application after its behavior is represented in the unified workspace.

Rollback restores the separate install client route and its navigation entry. No project files, Skill library content, or operation journals require migration.
