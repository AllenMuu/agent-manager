## Context

See proposal.md and `specs/structured-local-memory-store/spec.md`. Ticket #20 belongs to parent #7. The existing completed `evolve-to-agent-manager` Memory configuration/promotion specs and ADR 0006 remain compatibility context.

Blocked by: #19 (`define-scoped-memory-contracts`)

## Goals / Non-Goals

**Goals:** Deliver only the observable behavior in this slice through a neutral boundary, with negative/failure scenarios independently testable.

**Non-Goals:** Other Memory slices, automatic extraction/consolidation, GLOBAL access, graph semantics and arbitrary skill/task execution.

## Decisions

1. Persist canonical records, ownership, operation IDs and metadata in a new explicit structured format with guarded file access, atomic replacement and serialized read-modify-write across store instances/processes. Append-only text and last-writer-wins updates were rejected because they cannot preserve lifecycle invariants.

2. Update the same neutral ID only with a matching expected version and increment exactly once. Supersede creates a new ID and atomically records the old record as SUPERSEDED with the replacement relationship. Reuse operation IDs for duplicate detection; reject an ID reused with different intent.

3. Keep DELETED tombstones and explicit history inspection, with current recall returning only ACTIVE records. Forget is provider-managed retention, not filesystem undo. Import legacy text only through an explicit ownership/source proposal; do not fabricate original provenance.

## Risks / Trade-offs

Concurrent processes lose updates → serialize the complete transaction and check expected version. Partial writes corrupt valid state → atomic replacement and failpoint tests preserve the prior readable state.

## Migration Plan

Select the structured provider explicitly. Existing text providers remain unchanged. Legacy import is opt-in with confirmed owner/source; rollback leaves the structured data inspectable and does not downgrade it into plain text.

## Validation and delivery boundary

Follow tasks.md as the only implementation checklist. Bind ticket acceptance item N to requirement N and task 2.N; run all scenarios of that requirement through confirmed public seams. Planning completion is not implementation completion. Offline fixtures, mock behavior and opt-in real-runtime evidence must remain distinct.

Apply only after blocker delivery is verified and the public test seams are confirmed. Run Go tests/build/vet, related race checks, strict change validation and requirement/scenario verification. Ordinary independent review precedes the configured gpt-6.1-sol/high read-only OCR gate; any actionable OCR finding or unavailable reviewer prevents PR publication. Keep this change active until implementation acceptance and authorized merge, then sync and archive.
