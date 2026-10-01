# Use Akuity as an identity and delegation reference

Status: Accepted

Issue [#6](https://github.com/AllenMuu/agent-manager/issues/6) uses Akuity Agentic Control Plane as an industry reference for actor identity, delegated authority, policy evaluation, invocation context, and audit lineage. Agent Manager adopts those governance boundaries in a smaller local-first domain model; Akuity is not a runtime dependency, service integration, or compatibility target.

## Decision

- A governed run receives an explicit typed `ActorIdentity` or explicit anonymous mode. Named identity and exact, expiring delegated scopes are snapshotted on the run; no ambient OS identity, shared privileged token, or mutable identity registry supplies authority.
- The existing Agent Manager policy engine remains authoritative. `AgentPolicy` v2 adds exact-action identity rules, while v1 keeps policy-only behavior for actions whose contract does not require identity conditions. Identity-bound actions fail closed without matching v2 rules and run-bound delegation.
- Tool and MCP invocation use a separate adapter boundary that declares whether it preserves canonical `InvocationContext`. Filesystem resource placement is not evidence of invocation support.
- Approval and audit records identify the explicit deciding actor and preserve run, actor, delegation, policy, action, result, approval, and trace references without storing credentials. New v3 approved completion records carry the approver reference directly; existing v2 records remain readable.
- Operational context is an optional read-only provider port with source and freshness metadata. A separate exact-key access policy governs reads; the provider does not authorize mutations or establish actor identity.
- The initial implementation is local and deterministic: no OAuth, organization directory, agent launch, live runtime adapter, network-backed provider, or Akuity-specific protocol.

## Consequences

This design keeps governance evidence reproducible and inspectable offline, and lets local adapters adopt the contracts incrementally. Existing policy and run records remain readable; records without typed identity are treated as legacy/anonymous and cannot satisfy identity-bound actions. Integrations that cannot preserve required invocation context are blocked before dispatch. Agent Manager does not provide Akuity's broader hosted control-plane capabilities.

## Considered Options

- Integrate directly with an external control plane and adopt its identity or token format. Rejected because it adds network and credential dependencies and would make local governance depend on a vendor protocol.
- Keep actor and authority in free-form audit text. Rejected because the policy engine could not evaluate them safely and legacy text is not verified identity.
- Use Akuity as a pattern reference while defining runtime-neutral local contracts. Selected because it captures the governance boundaries in Issue #6 while preserving Agent Manager's local-first scope.
