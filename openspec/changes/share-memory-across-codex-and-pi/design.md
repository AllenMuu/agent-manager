## Context

See proposal.md and `specs/cross-agent-shared-memory/spec.md`. Ticket #23 belongs to parent #7. The existing completed `evolve-to-agent-manager` Memory configuration/promotion specs and ADR 0006 remain compatibility context.

Prerequisites #21 (`add-memory-gateway-and-cli`) and #22 (`add-mem0-memory-provider`) are delivered.

## Goals / Non-Goals

**Goals:** Deliver only the observable behavior in this slice through a neutral boundary, with negative/failure scenarios independently testable.

**Non-Goals:** Other Memory slices, automatic extraction/consolidation, GLOBAL access, graph semantics and arbitrary skill/task execution.

## Decisions

1. Expose one Gateway through a supported integration mechanism for each agent. Agent adapters expose access mechanisms, not a second canonical schema. The user confirmed CLI configuration/status plus the public stdio MCP server as test seams, with separate native Codex and Pi sessions. The current Pi 0.85.1 installation exposes no `pi mcp` command, so select `pi-mcp-adapter` 5.3.0 through the standard project `.mcp.json` path; the package is installed only in a task-owned validation profile. Pi's generated server entry exposes only the six Memory tools, uses the `mcp` tool prefix and keeps the server alive for a session. The native harness uses a task-owned HOME containing only the adapter's project-server trust policy and a private scratch `.mcp.json`; it does not load the user's other MCP servers. On Pi versions with built-in MCP, use built-in MCP when it meets the required behavior; the adapter replaces built-in MCP for those sessions, so select one path and record versions. Installation or configuration alone is not runtime acceptance.

This #23 slice demonstrates the actual M4 Mem0 provider with both real integrations, recording Mem0/agent versions, updated effective knowledge and provenance. Offline HTTP fixtures plus a local-provider two-agent demo do not satisfy this ticket's acceptance. Before native runs, the operator starts the pinned Mem0 service from a clean checkout with bytecode writes disabled, isolated Python mode and an exact `server/main.py` app directory. The runner verifies that import location and that `mem0`/`uvicorn` resolve from the selected pinned source or virtualenv before accepting process evidence.

2. Reuse M3 authorization, query budgeting and mutation confirmation rather than agent-private file copies. Integration tests exercise each actual runtime mechanism; two CLI calls or two mock clients do not count as the two-agent acceptance demo.

3. Show effective record/version and source/evidence in each integration. Updating the same ID increments version; supersession returns the new ID and identifies the retired record. Report available mechanisms per agent from actual validation evidence.

## Risks / Trade-offs

Mock demo mislabeled real integration → keep offline protocol fixtures separate from opt-in runtime evidence. Private store divergence → canonical data stays in the selected shared provider.

## Migration Plan

Enable each integration explicitly. Disable integration configuration to roll back without deleting shared records or mutating native project instructions; do not automatically populate private memory stores.

## Validation and delivery boundary

Follow tasks.md as the only implementation checklist. Bind ticket acceptance item N to requirement N and task 2.N; run all scenarios of that requirement through confirmed public seams. Planning completion is not implementation completion. Offline fixtures, mock behavior and opt-in real-runtime evidence must remain distinct.

Apply only after prerequisite delivery is verified and the Codex/Pi public test seams are confirmed. Run Go tests/build/vet, related race checks, strict change validation and requirement/scenario verification. Ordinary independent review precedes the configured gpt-6.1-sol/high read-only OCR gate. If that reviewer is unavailable, publication stays blocked unless the user explicitly selects a fallback; record the actual path and coverage, and never claim the configured model passed. Any actionable finding or important coverage gap blocks publication. Keep this change active until implementation acceptance and authorized merge, then sync and archive.
