## Context

See proposal.md and `specs/memory-gateway-and-cli/spec.md`. Ticket #21 belongs to parent #7. The existing completed `evolve-to-agent-manager` Memory configuration/promotion specs and ADR 0006 remain compatibility context.

Blocked by: #20 (`add-structured-local-memory-store`)

## Goals / Non-Goals

**Goals:** Deliver only the observable behavior in this slice through a neutral boundary, with negative/failure scenarios independently testable.

**Non-Goals:** Other Memory slices, automatic extraction/consolidation, GLOBAL access, graph semantics and arbitrary skill/task execution.

## Decisions

1. Bind owner selection to explicit operator/project context at one Gateway. Register opaque project IDs with path mappings rather than deriving identity from directory basename; relocation updates the mapping explicitly. Treat requested owner labels as input, not authority.

2. Route CLI and task-context queries through the same Gateway. Apply owner restrictions before provider search, then type/current-state filters, relevance policy, stable fallback ranking and result/content budgets. Break equal ranks by stable neutral ID and retain provenance with selected content.

3. Use preview plus explicit confirmation for add/update/supersede/forget and import. Context reads are read-only. Keep configured requests, implemented capabilities and availability separate in status; Memory errors do not block unrelated resource workflows.

## Risks / Trade-offs

Cross-project disclosure through search → constrain authorized owner before provider query. Budget strips evidence → select attributed items within budget, not content without lineage.

## Migration Plan

Add structured CLI paths without changing text promotion defaults. Rollback disables the new Gateway paths while leaving canonical stores readable; no provider database is copied into the Skill operation journal.

## Validation and delivery boundary

Follow tasks.md as the only implementation checklist. Bind ticket acceptance item N to requirement N and task 2.N; run all scenarios of that requirement through confirmed public seams. Planning completion is not implementation completion. Offline fixtures, mock behavior and opt-in real-runtime evidence must remain distinct.

Apply only after blocker delivery is verified and the public test seams are confirmed. Run Go tests/build/vet, related race checks, strict change validation and requirement/scenario verification. Ordinary independent review precedes the configured gpt-6.1-sol/high read-only OCR gate; any actionable OCR finding or unavailable reviewer prevents PR publication. Keep this change active until implementation acceptance and authorized merge, then sync and archive.
