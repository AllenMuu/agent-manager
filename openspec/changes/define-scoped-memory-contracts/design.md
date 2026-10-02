## Context

See proposal.md and `specs/scoped-memory-contracts/spec.md`. Ticket #19 belongs to parent #7. The existing completed `evolve-to-agent-manager` Memory configuration/promotion specs and ADR 0006 remain compatibility context.

Blocked by: None; M1 and R1 are dependency-frontier tickets.

## Goals / Non-Goals

**Goals:** Deliver only the observable behavior in this slice through a neutral boundary, with negative/failure scenarios independently testable.

**Non-Goals:** Other Memory slices, automatic extraction/consolidation, GLOBAL access, graph semantics and arbitrary skill/task execution.

## Decisions

1. Keep the existing text Provider contract compatible; add structured records and separate optional reader, recall, writer, conditional updater and lifecycle capabilities. Replacing the legacy interface would unnecessarily break promotion and task-context callers.

2. Use a neutral opaque ID, monotonic integer version, typed ownership, source/evidence references, explicit state and layer metadata. USER requires user ID; PROJECT requires project ID; AGENT and SESSION carry an owning user/project plus agent/session ID. Reject GLOBAL until its access rules are specified. Scope does not establish truth precedence.

3. Read/get operations take an explicit owner and queries require the same owner partition before search. In-memory storage validates the stored owner against the requested owner. This proves partitioning; authenticated caller authorization is added at the Gateway in M3, not inferred from caller labels.

4. Use context-aware operations and typed invalid-input, not-found, ownership-denied, unsupported, unavailable, conflict, canceled and outcome-unknown errors. Do not require every backend to fake future extraction, graph or atomic supersession.

5. Classify FACT, DECISION, PREFERENCE, CONSTRAINT, EXPERIENCE, PROCEDURE, SKILL, TASK, ERROR_SOLUTION and PROJECT_CONTEXT. RAW/ATOMIC/COMPOSITE/PROFILE are metadata; no extraction or consolidation is implied. Preserve references rather than inventing evidence.

## Risks / Trade-offs

Owner strings mistaken for authority → keep partition validation separate from Gateway authorization. Legacy interface expansion → keep structured APIs distinct and run compatibility tests.

## Migration Plan

No automatic store conversion or configuration rewrite. The in-memory provider is a deterministic implementation for contract tests, not durable production storage. Document the canonical/provider boundary in ADR 0006 before external integration.

## Validation and delivery boundary

Follow tasks.md as the only implementation checklist. Bind ticket acceptance item N to requirement N and task 2.N; run all scenarios of that requirement through confirmed public seams. Planning completion is not implementation completion. Offline fixtures, mock behavior and opt-in real-runtime evidence must remain distinct.

Apply only after blocker delivery is verified and the public test seams are confirmed. Run Go tests/build/vet, related race checks, strict change validation and requirement/scenario verification. Ordinary independent review precedes the configured gpt-6.1-sol/high read-only OCR gate; any actionable OCR finding or unavailable reviewer prevents PR publication. Keep this change active until implementation acceptance and authorized merge, then sync and archive.
