## Why

Agent Manager's local CLI now provides the catalog, project inspection, recommendations, diagnostics, guarded Skill lifecycle, operation journal, and SubAgent validation needed for a visual workflow. A loopback Web console will bring those existing capabilities together for one local project while keeping filesystem policy in Go services and every mutation reviewable and reversible.

## What Changes

- Add a local Web console served by Agent Manager through a loopback-only Go HTTP service and a React/TypeScript frontend.
- Register one canonical local project path per running session, then aggregate agent availability, technology evidence, Skill recommendations, inventory, SubAgent definitions, and diagnostics from existing Go packages.
- Add server-owned, expiring operation plans for Skill activation and removal. The browser reviews exact changes and submits only an opaque plan ID to execute; execution revalidates the preview before calling existing guarded lifecycle services.
- Expose the latest reversible operation and guarded Undo through the console.
- Protect the local API with a random session token, strict origin checks, and loopback binding; never expose general-purpose file reads or client-selected mutation paths.
- Add Go HTTP and frontend checks for read flows, plan lifecycle, stale state, unsafe paths, conflicts, rollback, and Undo.

## Capabilities

### New Capabilities
- `local-web-console`: Local project registration, read-only project workspace and resource views, loopback service security, and React/TypeScript delivery.
- `console-operation-plans`: Server-owned two-phase plans for guarded Skill activation/removal and reversible Undo.

### Modified Capabilities

None. The console reuses existing CLI/domain behavior and does not change established Skill lifecycle requirements.

## Impact

- Affected code: `cmd/agent-manager`, new `internal/webconsole` Go HTTP/service composition, and a new `web/` React/TypeScript application.
- APIs: versioned loopback-only `/api/v1` reads and operation-plan endpoints; no general file access endpoint.
- Dependencies: Go HTTP support uses the standard library. Frontend build dependencies are pinned and run locally; the application does not fetch code or project content from the network at runtime.
- Systems: one local project per service session; no Wails packaging, cloud service, Memory provider, Skill download, SubAgent installation, or runtime orchestration.
