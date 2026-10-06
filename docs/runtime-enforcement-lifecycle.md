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
