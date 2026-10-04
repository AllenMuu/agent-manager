## Context

See proposal.md and `specs/cross-agent-shared-memory/spec.md`. Ticket #23 belongs to parent #7. The existing completed `evolve-to-agent-manager` Memory configuration/promotion specs and ADR 0006 remain compatibility context.

Blocked by: #21 (`add-memory-gateway-and-cli`), #22 (`add-mem0-memory-provider`)

## Goals / Non-Goals

**Goals:** Deliver only the observable behavior in this slice through a neutral boundary, with negative/failure scenarios independently testable.

**Non-Goals:** Other Memory slices, automatic extraction/consolidation, GLOBAL access, graph semantics and arbitrary skill/task execution.

## Decisions

1. Expose one Gateway through a supported integration mechanism for each agent, preferably a shared MCP read/write/search surface where both runtimes support it. Agent adapters expose access mechanisms, not a second canonical schema.

The final parent #7 demonstration uses the actual M4 Mem0 provider and both real integrations, recording Mem0/agent versions, updated effective knowledge and provenance. Offline HTTP fixtures plus a local-provider two-agent demo do not satisfy this acceptance.

2. Reuse M3 authorization, query budgeting and mutation confirmation rather than agent-private file copies. Integration tests exercise each actual runtime mechanism; two CLI calls or two mock clients do not count as the two-agent acceptance demo.

3. Show effective record/version and source/evidence in each integration. Updating the same ID increments version; supersession returns the new ID and identifies the retired record. Report available mechanisms per agent from actual validation evidence.

## Risks / Trade-offs

Mock demo mislabeled real integration → keep offline protocol fixtures separate from opt-in runtime evidence. Private store divergence → canonical data stays in the selected shared provider.

## Migration Plan

Enable each integration explicitly. Disable integration configuration to roll back without deleting shared records or mutating AGENTS.md/CLAUDE.md; do not automatically populate private memory stores.

## Validation and delivery boundary

Follow tasks.md as the only implementation checklist. Bind ticket acceptance item N to requirement N and task 2.N; run all scenarios of that requirement through confirmed public seams. Planning completion is not implementation completion. Offline fixtures, mock behavior and opt-in real-runtime evidence must remain distinct.

Apply only after blocker delivery is verified and the public test seams are confirmed. Run Go tests/build/vet, related race checks, strict change validation and requirement/scenario verification. Ordinary independent review precedes the configured gpt-6.1-sol/high read-only OCR gate; any actionable OCR finding or unavailable reviewer prevents PR publication. Keep this change active until implementation acceptance and authorized merge, then sync and archive.

## Implemented partial checkpoint (Issue #23)

The selected public seams are Gateway weak preview/confirmation, bounded stdio
MCP tools, CLI access snippets/status, and an opt-in native acceptance runner.
These routine seams are within the approved roadmap. Baseline tests passed on
7e23997, which also carries the M4 archive documentation; no existing strong
mutation policy is weakened. The pinned Mem0 backend cannot satisfy strong
confirmed mutation contracts, so a separately named `replace-observed-weak`
policy requires explicit startup write authority, immutable preview and exact
owner/source/intent confirmation. Each preview permits one attempted write;
uncertain outcomes require read-only reconciliation rather than automatic replay.
An observed snapshot check can reject an already stale version but is not CAS.

Prior canonical snapshots remain only in provider-owned Mem0 metadata, capped at
eight versions and 256 KiB. Separate retained-history inspection preserves full
owner, neutral ID, source and evidence validation; ordinary recall exposes only
the effective current record. Existing strong history remains unsupported.

The Go 1.24-compatible official MCP SDK v1.4.0 handles negotiated lifecycle,
JSON-RPC IDs/errors and cancellation. Delivery requires initialization completion,
bounds frames/arguments/results/calls/previews and registers only implemented
Memory tools. Trusted startup attribution and exact PROJECT owner are separate:
Codex and Claude share the data owner; client claims grant no authority. Private
file-secret references resolve within server transport construction only.
Configuration output adds access information without copying canonical content.
Status separates implemented fixture-tested capabilities from native evidence.

The resumable runner is opt-in and never starts services or changes authentication,
provider selection, global settings or subscription state. Preparatory Codex and
real Mem0 create/read succeeded. Claude connected to MCP but its configured
inference service rejected requests due to inactive subscription; no Claude
Memory tool completed. The user selected restoration of that same service, with
restoration still pending. Final runner syntax/help checks are not native
acceptance. The literal five requirements/six scenarios remain mapped in the
[delivery matrix](../../../docs/research/issue23-memory-delivery-matrix.md).
