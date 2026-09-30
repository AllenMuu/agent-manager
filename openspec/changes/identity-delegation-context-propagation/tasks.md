## 1. Identity and Policy v2

- [x] 1.1 Add validated `ActorIdentity`, explicit anonymous mode, and immutable `Delegation` domain types; verify named/anonymous inputs, exact scopes, expiry, and secret rejection with unit tests.
- [x] 1.2 Extend strict `AgentPolicy` decoding and validation to accept v1 and v2 with exact-action identity rules; verify v1 compatibility, valid v2 rules, and unknown/invalid fields with policy tests.
- [x] 1.3 Include normalized identity rules and policy version in v2 snapshot hashing while preserving existing v1 hashes; verify equivalent normalized inputs hash consistently and rule changes alter the hash.

## 2. Run Identity and Store Compatibility

- [x] 2.1 Require an explicit named or anonymous identity when creating a new governed run and snapshot its actor and delegation; verify new records retain their creation-time identity and authority.
- [x] 2.2 Read v1 and the new run-store envelope, expose records without typed identity as legacy/anonymous, and preserve their policy snapshots; verify v1 reads do not change store bytes and successful mutations retain old records.
- [x] 2.3 Validate identity and delegation references on run creation and load; verify mismatched run/actor/delegation links are rejected without persisting partial state.

## 3. Policy Decisions, Approvals, and Audit

- [x] 3.1 Add the identity gate for action contracts that require identity conditions, matching exact v2 action rules against actor kind/roles and unexpired exact scopes; verify missing identity/rule/scope, expired delegation, and mismatch return stable identity reason codes before invocation.
- [x] 3.2 Preserve existing policy precedence and outcomes around the identity gate; verify ordinary policy DENY remains DENY and a passing identity gate preserves ALLOW or REQUIRE_APPROVAL.
- [x] 3.3 Require an explicit deciding actor for approval transitions and persist approver attribution; require the existing approval CLI commands to accept approver identity explicitly; verify approval, rejection, and expiry record the supplied approver and never infer the initiator.
- [x] 3.4 Extend structured audit lineage with actor, delegation, action, policy, approval, result, runtime, and trace references while retaining legacy event readability; verify evaluation fixtures consume typed fields and serialized events contain no credentials.

## 4. Invocation Context Propagation

- [x] 4.1 Add canonical `InvocationContext` construction and validation for run, actor, delegation, policy snapshot, optional approval, and trace references; verify mismatched lineage is rejected before dispatch.
- [x] 4.2 Add a distinct invocation-adapter contract with explicit context-propagation capability, separate from filesystem resource adapters; verify placement capabilities cannot satisfy invocation requirements.
- [x] 4.3 Implement the deterministic mock invocation adapter and required-capability preflight; verify supported calls receive unchanged context and unsupported required propagation blocks dispatch.

## 5. Read-Only Operational Context

- [x] 5.1 Add the provider interface and freshness-aware context record with source and capture time; verify retrieval is read-only and missing provider reports unavailable without inventing context.
- [x] 5.2 Verify operational context retrieval is independent from identity, mutation authorization, and invocation by testing that reads never dispatch a mutation tool.

## 6. Local Governed Invocation Slice

- [x] 6.1 Compose the domain API path for the local `allen` actor, `github:read` delegation, and mock invocation without adding a run-start or simulation CLI; verify `github.read` is allowed and the mock receives correlated context.
- [x] 6.2 Exercise `github.write` as denied or approval-required according to policy, then inspect structured audit lineage; verify denied actions never invoke the mock and approval outcomes retain explicit approver attribution.

## 7. Architecture Records and Verification

- [x] 7.1 Add ADR 0007 describing Akuity as an industry reference and documenting Agent Manager's narrower runtime-neutral, local-first boundaries; verify links and terms agree with the approved design.
- [x] 7.2 Update `CONTEXT.md` with ActorIdentity, Delegation, InvocationContext, and OperationalContextProvider terminology; verify definitions match the specs and introduce no credential or ambient-identity semantics.
- [x] 7.3 Run `gofmt` on changed Go files and `go test ./...`; verify the complete Go suite passes.
- [x] 7.4 Run `openspec validate identity-delegation-context-propagation` and review the final diff; verify OpenSpec validation succeeds and every added requirement has implementation coverage.
