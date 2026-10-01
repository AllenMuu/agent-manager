## Context

See proposal.md and `specs/runtime-enforcement-preflight/spec.md`. Ticket #24 belongs to parent #16. The existing completed `runtime-policy-governance` and `identity-delegation-context-propagation` specs remain compatibility context.

Blocked by: None; M1 and R1 are dependency-frontier tickets.

## Goals / Non-Goals

**Goals:** Deliver only the observable behavior in this slice through a neutral boundary, with negative/failure scenarios independently testable.

**Non-Goals:** Other enforcement slices, container/kernel/network isolation implementation, new secret management, Native Sandbox, formal proving, checkpoint and watchdog.

## Decisions

1. Separate EffectiveCapabilitySet (what effects are permitted/reachable) from EnforcementCapabilities (what controls the runtime can enforce). Resolve tool, filesystem paths, exact network destinations, process functions and opaque credential references/scopes from policy plus provider contributions; unknown contributions stay explicit.

2. Preflight fails closed for mandatory unknown/unsupported controls and reports optional gaps as warnings. Noop declares mandatory protection unsupported. A directory resource adapter and an invocation context-preserving adapter do not prove execution interception.

3. Declare policy update support independently per dimension as live-update, recreate-required or unsupported. A single hot-reload boolean would falsely authorize static filesystem/process changes. Leave formal reachability proof, checkpoint and watchdog as explicit unsupported future capabilities.

## Risks / Trade-offs

False positive safety report → distinguish effective permission from enforceable control and preserve unknown dimensions. Hidden provider permissions → include provider contributions in the effective view.

## Migration Plan

Extend existing policy preflight additively and preserve existing declared governance controls. No execution or persisted run revision migration is part of this ticket; rollback restores the earlier inspection path without altering state.

## Validation and delivery boundary

Follow tasks.md as the only implementation checklist. Bind ticket acceptance item N to requirement N and task 2.N; run all scenarios of that requirement through confirmed public seams. Planning completion is not implementation completion. Offline fixtures, mock behavior and opt-in real-runtime evidence must remain distinct.

Apply only after blocker delivery is verified and the public test seams are confirmed. Run Go tests/build/vet, related race checks, strict change validation and requirement/scenario verification. Ordinary independent review precedes the configured gpt-6.1-sol/high read-only OCR gate; any actionable OCR finding or unavailable reviewer prevents PR publication. Keep this change active until implementation acceptance and authorized merge, then sync and archive.
