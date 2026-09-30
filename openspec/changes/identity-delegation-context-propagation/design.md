## Context

See [proposal.md](proposal.md) for Issue #6 motivation and scope. The completed `runtime-policy-governance` change supplies `internal/policy` and `internal/run`; the policy loader currently accepts only strict `AgentPolicy` v1, and the run store currently persists a v1 database envelope. `internal/adapter` is a filesystem resource-placement boundary and its governance event normalization does not invoke tools. Keep those responsibilities distinct. There are no published capability documents under `openspec/specs/` yet.

## Goals / Non-Goals

**Goals:**

- Add identity and delegation as immutable run inputs that the policy evaluator and audit trail can correlate.
- Keep one policy engine and support reading existing policy and run data while introducing identity-aware behavior.
- Make context propagation an explicit invocation-adapter contract, testable locally without launching an agent or calling a real service.
- Keep operational context read-only and independent from mutation authorization.

**Non-Goals:**

- Implement OAuth, a credential broker, organization directory, identity registry, or ambient OS identity discovery.
- Integrate a real runtime/tool/MCP adapter or start an agent process.
- Add a concrete operational-context provider, network access, or automatic policy/run data migration on inspection.
- Treat Akuity as a dependency or copy its product-specific control plane into Agent Manager.

## Decisions

### Store an explicit identity snapshot on each new run

Add runtime-neutral identity types in a domain package, with named and explicit anonymous modes. A named actor stores a stable ID, kind, subject, optional provider, and validated safe roles; delegation stores its ID, exact scopes, expiry, and link to the actor/run. Snapshot these values at run creation. Do not resolve them later from a registry or ambient credentials, so policy and audit evidence remain reproducible. Validate and persist only the typed allowlisted fields; do not accept arbitrary identity claims or credential-shaped metadata.

Alternative considered: resolve actor and delegation IDs from a mutable shared registry on each action. Rejected because a later registry change would alter the apparent authority of an existing run and make offline evaluation nondeterministic.

### Version identity rules in `AgentPolicy` v2

Keep strict decoding and validation, but accept policy versions v1 and v2. Add an identity section only to v2, keyed by exact normalized action ID, with exact actor kinds, roles, and required delegation scopes. An action contract can require identity conditions; for such an action, evaluation must find a matching v2 rule and the run must satisfy every condition. Missing identity, rule, scope, or unexpired delegation denies before invocation. For actions without identity requirements, retain current v1 policy-only behavior. Preserve existing policy `DENY`; after the identity gate passes, preserve the policy engine's `ALLOW` or `REQUIRE_APPROVAL` outcome. Include the policy version and normalized identity rules in the immutable policy snapshot hash.

Alternative considered: add identity requirements to a separate policy engine or external authorization service. Rejected because it would split policy precedence and audit into competing authorities. Alternative considered: silently interpret a v1 policy as having permissive identity rules. Rejected because v1 never declared actor conditions or scopes.

### Read old run stores without rewriting them during inspection

Teach the store decoder to accept database envelope v1 and the new envelope version. A v1 run with no typed identity is represented to authorization as legacy/anonymous; never reconstruct identity from its free-form `Actor` audit string. Read operations leave bytes untouched. On the first successful state mutation, write the new envelope while retaining all existing records and audit events; absent identity remains legacy/anonymous. New runs require an explicit named or anonymous input. Keep each run's policy snapshot as originally resolved rather than recomputing an old hash.

Alternative considered: eagerly rewrite all records on read or store open. Rejected because inspection should remain read-only and a failed read must not mutate local state.

### Attribute approval and audit decisions explicitly

Extend approval transitions to receive the deciding `ActorIdentity` explicitly. Persist stable actor/delegation/run/runtime/action/policy/decision/approval/result/trace references in the structured event fields. Continue to exclude arbitrary metadata, raw event payloads, and credentials. Keep legacy events readable and distinguish missing identity fields from a named actor. The initiator does not become the approver by default.

Alternative considered: use the existing free-form event `Actor` string for all identities. Rejected because it cannot safely carry kind, roles, delegation, and approval attribution as typed evidence.

### Introduce a separate invocation contract

Place canonical `InvocationContext` and invocation adapter contracts in an invocation-focused domain package, separate from `internal/adapter` filesystem placement and `RuntimeController`. The context carries correlation IDs and policy snapshot hash, not credentials or unrestricted claims. Adapters declare whether they preserve this context; required-but-unsupported propagation blocks dispatch. The first implementation uses a deterministic mock adapter and checks lineage before dispatch.

Alternative considered: add invocation methods to the existing filesystem adapter. Rejected because placement capability says nothing about a runtime's ability to preserve identity across a tool/MCP call.

### Keep operational context as an optional read-only port

Define a provider interface and records with source, capture time, and freshness. Return unavailable when no provider is configured. Do not let this port authorize mutations or supply implicit actor identity. No provider implementation is part of this change.

Alternative considered: make a live provider part of the first end-to-end slice. Rejected because it would add external state and network assumptions to a deterministic governance contract.

### Record the reference architecture in an ADR

Add `docs/adr/0007-agent-control-plane-identity-delegation.md` to state which governance concepts are informed by Akuity and which are intentionally narrower in Agent Manager. Keep the ADR and `CONTEXT.md` aligned with the runtime-neutral, local-first design.

## Risks / Trade-offs

- **Adding v2 parsing can accidentally weaken strict policy validation** → retain unknown-field rejection and add version-specific validation for both v1 and v2.
- **A caller may omit an identity requirement when creating an action** → make identity requirements part of the action contract and fail closed when a required action lacks a v2 identity rule.
- **Legacy audit strings may be mistaken for verified identity** → expose them only as historical evidence and represent missing typed identity as legacy/anonymous.
- **A context-capable mock may overstate real adapter support** → capability declarations are adapter-specific; ship no real adapter claim in this change.
- **Operational context can become stale** → always carry source and capture/freshness metadata and leave freshness policy to the consuming read workflow.

## Migration Plan

1. Add policy v2 support while preserving v1 loading, validation, snapshots, and policy-only decisions.
2. Add run-store reads for v1 and the new envelope; verify inspection does not write, then version successful mutations while retaining old records.
3. Require explicit identity mode for newly created runs and capture the policy/identity/delegation snapshot at creation.
4. Add identity-aware policy gates, explicit approver attribution, structured audit lineage, and the mock invocation slice.
5. Add the optional operational-context port and update `CONTEXT.md` and the ADR.

Rollback is code-level: revert the new application version and retain the local store. Because a successful mutation may have written the new envelope, the previous version must not be used to mutate that store; the new versioned reader remains the forward-compatibility path. No network or external migration is involved.
