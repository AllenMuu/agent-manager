## Context

See [proposal.md](./proposal.md) for motivation and [project-skill-installation/spec.md](./specs/project-skill-installation/spec.md) for the behavior contract. The Go CLI already has catalog discovery, read-only project recommendations, and a lifecycle service whose `AddMany` path plans a batch, confirms it, rolls back partial writes, and records one journal entry. The existing architecture ADR describes React WebUI as a later phase and excludes an HTTP service from the CLI foundation; this change introduces only the local bridge needed for the requested WebUI workflow.

## Goals / Non-Goals

**Goals:**

- Give CLI and WebUI one application-level installation path over the current Skill lifecycle.
- Keep selection, planning, confirmation, conflict handling, journaling, and undo consistent across both interfaces.
- Serve the minimal React installation UI from a loopback-only Agent Manager process.

**Non-Goals:**

- Remote Skill registries, URL or Git source downloads, or a general-purpose remote API.
- A complete Agent Manager dashboard, Wails packaging, or global-baseline installation.
- Changes to recommendation ranking or automatic installation of all recommendations.

## Decisions

### 1. Put install orchestration in a shared Go application service

Add an application-facing installation service that accepts a project path, explicit Skill identifiers, target agents, and conflict options. It resolves identifiers through the configured catalog and calls the existing lifecycle batch operation. The CLI `install` command and WebUI bridge both call this service; `add` and `select` remain compatible entry points.

The current `AddMany` flow combines plan creation, confirmation, and application. Extract its side-effect-free plan construction so the application service can expose preview and apply operations without duplicating safety rules. Keep the existing command paths as wrappers over the same lifecycle logic. A separate WebUI filesystem implementation was rejected because it would split path validation, conflict handling, and rollback behavior.

### 2. Host the minimal WebUI on loopback

`agent-manager webui --project <path>` starts a local process for the requested project and reports a URL bound to `127.0.0.1` on an available port. The process serves the compiled React assets and a narrow API for catalog search, project recommendations, installation preview, and installation confirmation. Embed built assets in the Go binary so running the WebUI does not require a Node runtime. The API is not a general remote management interface.

The server rejects non-loopback binds, validates the browser `Origin`, and requires a per-session token for mutation requests. Static Skill content stays in the local library and is never fetched from a remote source. Update ADR 0005 to record this local WebUI phase and its loopback-only bridge; do not add a public listener or remote API.

A Wails-only bridge was rejected because it would make this WebUI require the later desktop packaging phase. A standalone React app that directly manipulates files was rejected because browser code cannot safely replace the Go lifecycle core.

### 3. Make preview and apply a two-phase guarded operation

The application service returns an immutable preview containing selected Skill identifiers, target paths, warnings, and conflicts. For WebUI previews, the server associates the preview with the active session and a short-lived opaque token. On confirmation it rebuilds the plan from current catalog and filesystem state and compares it with the preview. If the plan changed, it performs no writes and returns a replacement preview that requires another confirmation. If it matches, the server invokes the existing guarded batch mutation and journal path. The CLI displays the plan in the same invocation and applies only when `--yes` is present.

The service does not trust a plan sent back by the browser. Revalidation at apply time and the lifecycle's existing destination checks protect against changes between preview and confirmation. A persistent preview database was rejected because previews are short-lived interaction state and should not create project files.

The preview fingerprint includes resolved content for symlinked files and directories inside selected Skills. Missing and existing directory ancestors are treated alike when checking destination safety, so another installation creating a shared parent does not invalidate an unchanged selection. Apply holds a project-scoped operating-system lock through the journal commit; journal read-modify-write operations use a separate cross-process lock so simultaneous CLI and WebUI operations cannot overwrite each other's entries.

### 4. Keep catalog and recommendation behavior read-only

The React UI reads the configured Skill catalog and existing project recommendation service. A user explicitly selects one or more catalog or recommendation entries and then starts installation. Recommendation itself remains read-only, and the CLI install command accepts explicit Skill identifiers rather than implicitly selecting all recommendations.

## Risks / Trade-offs

- [A browser-origin bug could expose local filesystem mutations] → Bind only to loopback, validate `Origin`, require an in-memory session token, scope each server process to one selected project, and test rejection of remote requests.
- [Filesystem state may change after preview] → Rebuild and compare the plan before apply; if it differs, make no writes and require fresh confirmation.
- [CLI and WebUI behavior could drift] → Keep catalog resolution and mutations in one Go application service and exercise both surfaces against the same fixtures.
- [Embedded frontend assets may not match the source tree] → Add a reproducible frontend build step and verify the embedded asset set as part of the documented build workflow.

## Migration Plan

1. Extract reusable batch planning and application from the current lifecycle path without changing `add`, `select`, journal compatibility, or undo behavior.
2. Add `install` as a batch-oriented CLI entry point and retain existing commands.
3. Add the loopback WebUI command, narrow local API, and embedded React assets; update ADR 0005 and usage documentation.
4. Verify that failed or rejected previews leave project files and journals unchanged, and that successful operations use existing undo behavior.

Rollback removes the `install` and `webui` entry points and the embedded frontend/API. No existing configuration or journal format needs migration; operations already committed through the shared lifecycle remain recoverable with `undo`.
