## Context

See proposal.md and `specs/mem0-memory-provider/spec.md`. Ticket #22 belongs to parent #7. The existing completed `evolve-to-agent-manager` Memory configuration/promotion specs and ADR 0006 remain compatibility context.

Blocked by: #19 (`define-scoped-memory-contracts`)

## Goals / Non-Goals

**Goals:** Deliver only the observable behavior in this slice through a neutral boundary, with negative/failure scenarios independently testable.

**Non-Goals:** Other Memory slices, automatic extraction/consolidation, GLOBAL access, graph semantics and arbitrary skill/task execution.

## Decisions

1. Pin one Mem0 OSS REST/OpenAPI version and use its documented endpoints, not hosted Mem0 paths. Store the canonical envelope and neutral/provider ID mapping in provider-owned metadata. Never expose backend IDs as the public Memory identity or keep a second canonical content mirror.

2. Map owner partition filters before remote search and verify returned ownership. Preserve declared provenance and versions; report unsupported atomic conditional-update/supersession semantics honestly if the selected server cannot guarantee them. Do not emulate atomicity with two independent remote writes.

3. Accept context deadlines and classify auth, unavailable, unsupported, conflict, canceled and outcome-unknown errors. Never automatically replay an uncertain mutation. Resolve secret references only at the provider transport boundary and redact request/response diagnostics.

4. Use deterministic local HTTP fixtures for the pinned contract. A separately opted-in real-service smoke records server version/environment; normal tests and Skill commands never install or start Mem0.

## Risks / Trade-offs

Remote write succeeds despite timeout → return outcome unknown and reconcile by operation evidence, never blind retry. Server drops owner metadata → reject the result and fail the provider contract instead of relaxing isolation.

## Migration Plan

Add explicit provider kind/config selection with secret references. Keep local providers as independent options. Rollback disables Mem0 access without changing neutral IDs or rewriting remote content; incompatible backend versions are reported unsupported.

## Validation and delivery boundary

Follow tasks.md as the only implementation checklist. Bind ticket acceptance item N to requirement N and task 2.N; run all scenarios of that requirement through confirmed public seams. Planning completion is not implementation completion. Offline fixtures, mock behavior and opt-in real-runtime evidence must remain distinct.

Apply only after blocker delivery is verified and the public test seams are confirmed. Run Go tests/build/vet, related race checks, strict change validation and requirement/scenario verification. Ordinary independent review precedes the configured gpt-6.1-sol/high read-only OCR gate; any actionable OCR finding or unavailable reviewer prevents PR publication. Keep this change active until implementation acceptance and authorized merge, then sync and archive.
