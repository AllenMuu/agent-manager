## Context

See proposal.md and `specs/experimental-openshell-integration/spec.md`. Ticket #28 belongs to parent #16. The existing completed `runtime-policy-governance` and `identity-delegation-context-propagation` specs remain compatibility context.

Blocked by: #27 (`apply-confirmed-policy-revisions`)

## Goals / Non-Goals

**Goals:** Deliver only the observable behavior in this slice through a neutral boundary, with negative/failure scenarios independently testable.

**Non-Goals:** Other enforcement slices, container/kernel/network isolation implementation, new secret management, Native Sandbox, formal proving, checkpoint and watchdog.

## Decisions

1. Keep OpenShell-specific transport and types inside its adapter and explicitly select a tested version. Core APIs consume only neutral permission/lifecycle/revision/event contracts. Do not add OpenShell as a required ordinary CLI dependency.

2. Pin and test the provider support matrix. Treat filesystem/process isolation policy as static unless the tested version proves otherwise; reject or explicitly recreate for static changes. Network live update support does not imply every dimension can hot reload.

3. Execute one real network denial → trusted human proposal decision → exact policy application → retry → termination smoke with neutral audit correlation. Offline adapter fixtures and a real sandbox smoke are separate evidence; record actual version/environment.

4. Document adopt/integrate/do-not-build boundaries and opaque credential mediation. Doctor reports actual tested support, missing controls and weaker credential injection isolation; raw secrets stay outside control-plane state and audit.

## Risks / Trade-offs

Version drift overstates support → pin transport contract and retain version-tagged smoke evidence. Recreating static policy loses lineage → require explicit recreation with correlated new handle and confirmation rather than silent reload.

## Migration Plan

Ship behind explicit experimental selection. No automatic OpenShell install/start. Disable the adapter to roll back; cleanup existing external handles only through a confirmed lifecycle operation. Document source/version evidence in the reference.

## Validation and delivery boundary

Follow tasks.md as the only implementation checklist. Bind ticket acceptance item N to requirement N and task 2.N; run all scenarios of that requirement through confirmed public seams. Planning completion is not implementation completion. Offline fixtures, mock behavior and opt-in real-runtime evidence must remain distinct.

Apply only after blocker delivery is verified and the public test seams are confirmed. Run Go tests/build/vet, related race checks, strict change validation and requirement/scenario verification. Ordinary independent review precedes the configured gpt-6.1-sol/high read-only OCR gate; any actionable OCR finding or unavailable reviewer prevents PR publication. Keep this change active until implementation acceptance and authorized merge, then sync and archive.
