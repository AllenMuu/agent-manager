## Context

See `proposal.md` for motivation and `specs/local-web-console/spec.md` plus `specs/console-operation-plans/spec.md` for behavior contracts. The CLI already composes local packages for catalog discovery, static project scanning, recommendation, agent inventory, Skill lifecycle, diagnostics, operation journals, and SubAgent registry reads. There is no HTTP server or frontend in the current tree. Existing lifecycle mutators own placement, confirmation, rollback, and journal rules; the Web delivery layer must call those services rather than reproduce their filesystem decisions.

## Goals / Non-Goals

**Goals:**

- Add one local server session that offers read APIs and reviewed, session-owned plans for one canonical project.
- Keep file mutation, conflict checks, rollback, and Undo inside the existing lifecycle and journal services.
- Keep the console available from the built Agent Manager binary by embedding the locally built frontend assets.
- Keep the UI usable at desktop and narrow mobile widths without adding runtime network dependencies.

**Non-Goals:**

- Multiple registered projects, user accounts, LAN access, cloud APIs, generic file browsing, Wails packaging, Skill downloads, Memory UI, SubAgent installation, or runtime orchestration.
- Replacing the CLI or moving filesystem placement policy into HTTP handlers or TypeScript.

## Decisions

### Delivery boundary

Add `internal/webconsole` as a standard-library `net/http` delivery adapter. It composes the same catalog, search, stack, recommendation, adapter inventory, lifecycle, diagnostic, operation-journal, configuration, and SubAgent registry packages used by the CLI. Add an `agent-manager web` command that binds `tcp4` on `127.0.0.1`; the address is not configurable. The default port is `0` so the OS selects an available port, while an optional port flag supports a fixed local port.

The UI is a Vite-built React/TypeScript single-page app. Vite writes production assets into an embedded `internal/webconsole/ui` directory so `go test` and the built CLI work from a clean checkout without a separate frontend server. Frontend source and lockfile remain in `web/`. Use CSS Modules only, with shared CSS custom properties for tokens. React's current official setup guidance recommends Vite when building from scratch without a full-stack framework, which matches this static local-delivery boundary.

### Session and browser security

Generate 32 random bytes per process and use their hex encoding as the bearer token. The CLI prints an entry URL whose token is in the fragment; the browser copies it into tab-scoped `sessionStorage` and removes the fragment from the address bar. API calls send `Authorization: Bearer …`. The server compares tokens in constant time, validates any supplied Origin against the exact loopback origin, requires that origin on state-changing requests, rejects cross-site Fetch Metadata, emits no CORS permission headers, and sets restrictive content security and cache headers. The token never appears in HTTP request paths or server logs.

Only project registration accepts a path. It requires authentication, canonicalizes the user-submitted directory once, and creates one random project ID for the session. Every other route uses server-held project state and accepts only Skill identifiers, target-agent identifiers, or opaque plan IDs. No route opens an arbitrary browser-supplied path.

### Read-model composition

The handler builds responses from existing Go behavior: `adapter.Inventory`, `stack.Scan` and `recommend.Recommend`, `catalog.Discover` and `search.Match`, `lifecycle.List`, `diagnostic.Scan`, the operation journal, and the SubAgent registry. Skill detail resolves only an eligible catalog identifier and returns its already parsed `SKILL.md` body. SubAgents remain read-only. API DTOs have stable JSON names and represent empty collections as arrays.

### Pure previews and guarded execution

Add pure preview methods to `lifecycle.Service` for activation and removal. Refactor the existing mutation methods to share the same plan construction path so preview and execution cannot drift; previews must not capture journal snapshots or create directories. Add `Journal.PreviewUndo` and have `UndoLatest` use its plan builder.

Keep pending plans in an in-memory store guarded by a mutex. Each record contains an unguessable ID, registered project ID, operation request made only from identifiers, normalized preview, creation/expiry timestamps, and execution state. Expire plans after ten minutes, delete them after use/cancel, and discard them on process exit. Plan creation calls only pure preview methods. Execution marks one plan as in progress, recomputes the preview, compares its normalized JSON hash with the reviewed preview, and returns a `409 stale_plan` when they differ. When current, it invokes the existing lifecycle or journal mutator with a confirmation callback that checks the same plan again. Existing mutators then perform their own immediate path/source validation, atomic publication, journal write, and rollback. Replacement requires a separate `confirmReplacement` field saved in the server-owned request and echoed as a visible warning in the review UI.

Undo plans include the latest journal entry fingerprint. Execution checks that fingerprint before calling `UndoLatest`; that service rechecks current path snapshots before restoring. Failed or stale plans are removed and require a fresh preview.

### UI structure and visual system

The audience is a developer managing agent resources in one local repository. Use a compact app shell with a project switcher/identity and a short left navigation rail; the main pane prioritizes actionable status, evidence, and operation review. The first screen is a working workspace, not a marketing hero. Use lists and dense tables where relationships matter, reserving bounded surfaces for recommendations and plan review rather than repeating generic cards.

Visual thesis: a warm paper operator console with graphite text, quiet separators, and a restrained clay-orange action color. Latin interface text uses the platform system sans stack with CJK fallbacks so the offline app matches native text rendering; paths, identifiers, and timestamps use a system monospace stack. CSS tokens use OKLCH, with semantic success/warning/danger colors distinct from the main action color. Use named radius tiers of 4px, 8px, and 12px. State changes remain immediate; press feedback uses a 120ms scale response and is disabled for reduced-motion preferences.

### Error model and response behavior

Use versioned JSON DTOs and a stable error envelope with `code` and actionable `message`. Map invalid project paths to `400`, authentication/origin failures to `401`/`403`, unknown routes/plans to `404`, stale or conflicting state to `409`, and unexpected internal errors to `500` without stack traces. Reject unknown JSON fields and oversized request bodies. Avoid permissive path, target, action, and conflict fields.

### Verification

Use Go unit and HTTP tests with temporary homes, project directories, Skill libraries, fake clocks, and injected token readers. Test authentication, origin handling, project canonicalization, read-only requests, plan non-mutation, expiry, concurrent execute, stale previews, force confirmation, lifecycle rollback, and Undo. Use frontend tests for token bootstrap, read-model states, review/confirm flows, and failure handling; run TypeScript checks and a production Vite build. Verify rendered screens at 1280px and 375px in a real browser.

## Risks / Trade-offs

- [A local browser can share a machine with unrelated local processes] → Bind only to loopback, use an unguessable session token, reject untrusted origins, and expose no arbitrary file API.
- [Project state can change between preview and execution] → Recompute the exact plan at execution and rely on lifecycle/journal's final path revalidation immediately before mutation.
- [A stale checked-in frontend bundle could diverge from source] → Keep the lockfile and generated embedded bundle in the PR; make the documented build regenerate assets before frontend checks.
- [In-memory plans disappear on restart] → This is intentional; pending operations require a new session and preview.
- [The browser may be opened from a deep-linked token URL] → Keep the token only in the URL fragment until bootstrap, then move it to tab-scoped session storage and replace the address-bar URL.

## Migration Plan

No persistent schema changes are required. Add the `web` command without changing existing CLI commands. Build frontend assets into the embedded directory, then build/test Go as before. Rollback is a code revert; there is no project or user data migration. The server creates no project state until an operator confirms an operation through the existing journaled lifecycle service.
