## Context

See proposal.md and `specs/runtime-enforcement-lifecycle/spec.md`. Ticket #25 belongs to parent #16. The existing completed `runtime-policy-governance` and `identity-delegation-context-propagation` specs remain compatibility context.

Blocked by: #24 (`define-runtime-enforcement-capabilities`), #18 (`fix-approved-invocation-retry-lineage`)

## Goals / Non-Goals

**Goals:** Deliver only the observable behavior in this slice through a neutral boundary, with negative/failure scenarios independently testable.

**Non-Goals:** Other enforcement slices, container/kernel/network isolation implementation, new secret management, Native Sandbox, formal proving, checkpoint and watchdog.

## Decisions

1. Keep the domain provider neutral and split optional pause/resume, policy application and event retrieval interfaces. Use a deterministic mock with operation evidence and failpoints; do not require every runtime to fake every optional function.

2. Persist intent with provider ID, operation ID and run correlation before prepare/start. Persist external handle and generation after acknowledgement. Reconcile by stable operation ID and external handle after a crash; if the provider cannot deduplicate/query an uncertain start, block and expose manual recovery rather than relaunch.

3. Only provider-confirmed start/pause/resume/terminate outcomes change confirmed run state. Separate desired transition from observed state and record reconciliation/cleanup outcomes. Reuse fixed policy/identity/invocation context and retain original approval single-use guarantees from F0.

## Risks / Trade-offs

External start succeeds but persistence fails → stable intent and query/reconciliation, never unconditional duplicate launch. In-memory controller map lost → reconnect with persisted provider identity/handle/generation.

## Migration Plan

Add optional runtime operation/handle fields with a compatible store reader, preserving historical local-only runs. Existing runs without handles remain inspectable; rollback never marks external resources cleaned up without confirmation.

## Validation and delivery boundary

Follow tasks.md as the only implementation checklist. Bind ticket acceptance item N to requirement N and task 2.N; run all scenarios of that requirement through confirmed public seams. Planning completion is not implementation completion. Offline fixtures, mock behavior and opt-in real-runtime evidence must remain distinct.

Apply only after blocker delivery is verified and the public test seams are confirmed. Run Go tests/build/vet, related race checks, strict change validation and requirement/scenario verification. Ordinary independent review precedes the configured gpt-6.1-sol/high read-only OCR gate; any actionable OCR finding or unavailable reviewer prevents PR publication. Keep this change active until implementation acceptance and authorized merge, then sync and archive.
