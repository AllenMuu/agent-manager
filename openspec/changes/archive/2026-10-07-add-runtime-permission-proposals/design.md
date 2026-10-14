## Context

See proposal.md and `specs/runtime-permission-proposals/spec.md`. Ticket #26 belongs to parent #16. The existing completed `runtime-policy-governance` and `identity-delegation-context-propagation` specs remain compatibility context.

Blocked by: #25 (`add-runtime-enforcement-lifecycle`)

## Goals / Non-Goals

**Goals:** Deliver only the observable behavior in this slice through a neutral boundary, with negative/failure scenarios independently testable.

**Non-Goals:** Other enforcement slices, container/kernel/network isolation implementation, new secret management, Native Sandbox, formal proving, checkpoint and watchdog.

## Decisions

1. Bind each proposal to run, initiating actor/delegation, original denied audit, base policy revision/hash, bounded capability difference and expiry. Proposal approval authorizes a future mutation; it does not immediately allow the denied action or overwrite policy.

2. Resolve local operator identity/approval authority through a trusted operator boundary separate from runtime event input. Runtime-submitted human/roles strings are not authenticated authority. Agents cannot decide their own proposals.

3. At decision time, recheck proposal expiry, live delegation expiry/scope ceiling and current base revision. Reject requests above the existing delegation ceiling; a larger delegation is a separate design. Keep action approvals immutable and singly consumed under their existing contract.

## Risks / Trade-offs

Spoofed human labels enable escalation → do not trust labels from runtime/caller data; use the operator authority boundary. Stale proposals race policy changes → compare immutable base revision at decision.

## Migration Plan

Introduce permission proposals as a distinct type/lifecycle; do not reinterpret historical one-action approvals as policy-change approvals. Rollback disables new proposals while retaining audit inspection of existing records.

## Validation and delivery boundary

Follow tasks.md as the only implementation checklist. Bind ticket acceptance item N to requirement N and task 2.N; run all scenarios of that requirement through confirmed public seams. Planning completion is not implementation completion. Offline fixtures, mock behavior and opt-in real-runtime evidence must remain distinct.

Apply only after blocker delivery is verified and the public test seams are confirmed. Run Go tests/build/vet, related race checks, strict change validation and requirement/scenario verification. Ordinary independent review precedes the configured gpt-6.1-sol/high read-only OCR gate; any actionable OCR finding or unavailable reviewer prevents PR publication. Keep this change active until implementation acceptance and authorized merge, then sync and archive.
