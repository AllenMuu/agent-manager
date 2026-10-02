## Context

See proposal.md and `specs/confirmed-policy-revisions/spec.md`. Ticket #27 belongs to parent #16. The existing completed `runtime-policy-governance` and `identity-delegation-context-propagation` specs remain compatibility context.

Blocked by: #26 (`add-runtime-permission-proposals`)

## Goals / Non-Goals

**Goals:** Deliver only the observable behavior in this slice through a neutral boundary, with negative/failure scenarios independently testable.

**Non-Goals:** Other enforcement slices, container/kernel/network isolation implementation, new secret management, Native Sandbox, formal proving, checkpoint and watchdog.

## Decisions

1. Keep the original run snapshot and each later canonical revision immutable. Audit references the revision used for that event; resolve historical hashes from the persisted revision chain instead of comparing every audit with the newest snapshot.

2. Persist desired and applied revisions separately with mutation operation ID, proposal/approval references and application state pending/applied/failed/unknown. Provider acceptance of a request is not proof of application. Require exact run/handle/generation/operation/revision confirmation and persist it before resume.

3. On missing/wrong/timeout confirmation keep the waiting action paused and reconcile the provider actual revision. Use operation/event IDs to deduplicate and revalidate delegation at retry. Do not replay unknown external effects when the provider cannot guarantee idempotency.

4. Record event origin agent/runtime/control-plane/infrastructure and the trust established by the adapter boundary. A source string alone is not authentication; stale generations, operations or revisions become diagnostics rather than state transitions.

## Risks / Trade-offs

New snapshot invalidates historical audit → resolve immutable revision references and retain legacy compatibility fixtures. Acknowledgement lost after application → reconcile applied revision and keep paused until exact evidence is persisted.

## Migration Plan

Add version-aware readers/validators for historical fixed-snapshot records and the new revision chain before writing new records. No in-place rewrite of old audit; rollback must reject unrecognized revision-aware mutations instead of discarding history.

## Validation and delivery boundary

Follow tasks.md as the only implementation checklist. Bind ticket acceptance item N to requirement N and task 2.N; run all scenarios of that requirement through confirmed public seams. Planning completion is not implementation completion. Offline fixtures, mock behavior and opt-in real-runtime evidence must remain distinct.

Apply only after blocker delivery is verified and the public test seams are confirmed. Run Go tests/build/vet, related race checks, strict change validation and requirement/scenario verification. Ordinary independent review precedes the configured gpt-6.1-sol/high read-only OCR gate; any actionable OCR finding or unavailable reviewer prevents PR publication. Keep this change active until implementation acceptance and authorized merge, then sync and archive.
