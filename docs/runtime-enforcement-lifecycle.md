# Recoverable runtime lifecycle

R2 adds an offline-testable neutral lifecycle boundary for [Issue #25](https://github.com/AllenMuu/agent-manager/issues/25). It does not implement a model runtime or sandbox. Directory adapters still provide placement only; invocation context propagation does not establish interception.

`enforcement.Provider` supplies its own identity and R1 declaration plus prepare, start and terminate operations. `PauseResumer` and `Querier` are separate optional ports. The coordinator binds preflight to the selected provider declaration and immutable run policy; a caller-supplied declaration cannot upgrade the provider. A provider lacking the optional pause/resume port cannot satisfy mandatory approval suspension.

`run.Coordinator` persists a provider/run/operation intent before each mutation. Provider receipts must match the operation, run, provider, policy, identity, handle, generation and expected state. Preparation establishes a safe opaque external handle and generation. Each persisted operation carries timestamps, outcome and reconciliation evidence. Raw provider diagnostics and credentials are excluded from persisted operation reasons and returned provider errors.

The external lineage is additive to existing run records. Local-only runs, policy versions and historical audit formats remain readable. The store serializes lifecycle mutations through the existing guarded atomic transaction. Unknown operations cannot be superseded by a new mutation. `ExecutionReady` checks persisted confirmed start and outstanding operations, including after coordinator replacement. Governance and invocation use that gate.

Recovery queries the **original** persisted operation. It never repeats prepare/start to discover whether an uncertain side effect happened. A provider without query support leaves execution blocked and returns unsupported/manual recovery information. A failed local confirmation remains recoverable using the persisted intent and the provider's operation receipt. Termination refusal or timeout preserves the last confirmed state; cleanup is never claimed without acknowledgement.

A manager may reconnect a coordinator with `SetCoordinator` for approval pause/resume and termination. An unconnected manager refuses external control. Direct legacy local confirmation methods refuse managed runs. Existing approval decisions, atomic single-use dispatch and request/completion lineage are retained.

`runs show <id> --json` includes the complete neutral external lineage. Text inspection shows provider, handle, generation, observed state, readiness and operation outcomes. CLI inspection does not register or launch a provider. There is no new CLI runtime launch default.

Run the executable public-boundary example with:

```sh
go test ./internal/run -run ExampleCoordinator -count=1 -v
```

It prepares and starts an offline mock, loses a termination acknowledgement, reopens the local store, and reconciles the same operation. The mock keeps deterministic receipts while its fixture instance is retained; it is not a durable external service. Tests exercise confirmation-storage failure, unknown starts, provider refusal, unsupported ports, invalid receipts, concurrent coordinators, and governed approved invocations. This is fixture evidence, not live filesystem/process/network/credential protection. Policy application, event retrieval, policy revisions, OpenShell, container/kernel isolation, checkpoint and watchdog remain future work.

## Denial-bound permission proposals (R3)

`Manager.RequestPermissionProposal` accepts a run, original denial audit ID, one
`PermissionDifference` and expiry. `Store.GetPermissionProposal` exposes the
persisted denial, initiating identity/delegation, immutable complete base policy
snapshot, exact difference and decision audit. The denial must identify the same
run/actor/delegation/policy/action and exact destination, tool or credential scope.
It must be an original `DENY` caused by the corresponding permission rule;
completion, budget/identity denial, foreign audit and one-action
`REQUIRE_APPROVAL` records cannot substitute for it.

The difference adds one exact capability, subject to the current delegation:

| Kind | Ceiling |
| --- | --- |
| Network | R3 introduces `network:<canonical hostname>`; the entire scope must fit the canonical 128-byte scope limit. No subdomain, prefix, wildcard, URL or IP grant is inferred. |
| Credential | The exact canonical credential scope, using the existing scope syntax. |
| Tool | A matching v2 action identity rule with nonempty `RequiredScopes`; every scope must be delegated and the initiating actor must match the rule's kind/role conditions. An absent or empty rule cannot prove a ceiling. |

No proposal expands actor kinds, roles or delegation. Credential-denial audit
records retain a scope identifier only for a named, action-attributed denial;
secret-shaped/noncanonical values are rejected. Anonymous and ordinary allowed
credential events retain historical omission. Older audits without the scope
remain readable but cannot establish exact credential-proposal lineage.

A trusted delivery host constructs `NewPermissionProposalDecider` with its
`OperatorAuthority`. `Decide` accepts proposal ID and approved/rejected status,
without a caller identity/roles claim. The authority port must establish an
authorized human through trusted operator input, bind its receipt to the exact
proposal/run/denial/base/decision, and return safe identity/authority references.
Self-approval, including a run actor relabeled as human, is rejected. No runtime
request or CLI human flag installs this port. The offline fixtures model this
configured host contract; they do not authenticate a real human.

`Decide` serializes the terminal decision in the existing cross-process Store
transaction and records successful and invalid attempts with stable IDs. An
established operator remains attributed when expiry or base validation rejects
approval; an unauthorized receipt is omitted. Proposal and delegation expiry,
exact scope ceilings, and current base ID/version/hash/resolution time are
rechecked inside the transaction after the authority returns. Canceled decisions
cannot approve a proposal.

An optional `CurrentPolicyBoundary.WithCurrentPolicy` holds the authoritative
base stable throughout its callback. Lock order is that boundary, then Store
transaction, then operator resolution. Neither boundary nor operator resolver
may re-enter Store. Without this port, R3 reads the persisted immutable initial
run snapshot before the transaction; no applied revision setter exists. A later
revision workflow must supply authoritative coordination, rather than treating a
past validation result as an application authorization.

`ValidateUse` re-establishes the original operator identity and authority,
including current authorization, and rechecks expiry/ceiling/base in the same
validation transaction. Success only establishes eligibility for a future
confirmed revision application. Neither approval nor validation applies a
revision, changes effective policy, resumes a run, authorizes retry, or consumes
an existing one-action approval. R4 owns separate confirmed revision application.

Run the public R3 contract tests with:

```sh
go test ./internal/run -run 'Test.*Proposal|Test.*Operator' -count=1
```
