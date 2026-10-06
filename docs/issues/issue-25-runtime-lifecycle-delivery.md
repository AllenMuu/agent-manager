# R2 / Issue #25 branch evidence

Scope: [Issue #25](https://github.com/AllenMuu/agent-manager/issues/25), active
change `add-runtime-enforcement-lifecycle`. Branch:
`feature/issue25-runtime-lifecycle`; base:
`0b6edffe2cb73b9d5091c9a4bdb65721063a7be9`. The branch also carries the nine
accepted R1 specification/archive/documentation entries from controller commit
`ce523d450fd0e82e32d114787ec9bd7ca8219ec9`; this implementation preserves them.
The implementation commit identity is reported separately, avoiding a recursive
commit hash in this document.

Live read-only Issue checks found #25 OPEN and its declared/native prerequisites
#18 and #24 CLOSED. #18 single-use approval/completion lineage is present in the
source base; #24's declaration boundary and ADR 0008 are present. The controller
selected the neutral coordinator/provider, durable run/operation inspection,
governance/invocation and additive CLI inspection seams within the user-approved
roadmap. Exported names are routine implementation choices, not separately
claimed human approvals.

## Requirement/scenario matrix

| Criterion / task | Literal scenario | Public evidence |
| --- | --- | --- |
| 1 / 2.1 | Inspect a prepared mock run | `TestPreparedRunPersistsNeutralLineage`: reopened Store exposes provider, opaque handle/generation and confirmed preparation operation with the same immutable policy and explicit identity. Named immutable identity and invocation context also pass the governed lifecycle tests. |
| 2 / 2.2 | External prepare succeeds but local confirmation fails | `TestPrepareConfirmationFailureRecoversOriginalHandle`: a public LifecycleStore wrapper fails confirmation; a reopened Store queries the same persisted operation and recovers its original handle with one external resource and one preparation call. |
| 2 / 2.2 | Restart after uncertain start | `TestUncertainStartBlocksGovernanceAfterRestart`: persisted unknown start retains prepared observed state and blocks governance after restart; query stays unknown until explicit correlated outcome evidence arrives, then confirms the same resource without another start. |
| 3 / 2.3 | Termination is not acknowledged | `TestTerminationUnknownAndRefusedKeepConfirmedState`: deadline/refusal retains active confirmed state, no termination reason and inspectable unknown/failed operation. `TestManagedTerminationPreservesSupportedReason` checks a positively acknowledged termination and its supported reason through Manager. |
| 4 / 2.4 | Provider cannot pause | `TestUnsupportedPauseAndQueryStayExplicit`: missing optional ports return unsupported without a paused confirmation; no query support keeps uncertain mutation unavailable. `TestOptionalProviderRejectionRemainsExplicit`: a present port returning unsupported records an explicit unsupported outcome without changing confirmed state. |
| 5 / 2.5 | Mock lifecycle and invocation | `TestMockLifecycleAndApprovedInvocationThenControlReconciliation`: prepares/starts a named mock run, approves and dispatches once, records succeeded/ALLOW request/completion/approver lineage, refuses reuse, reopens the Store and reconciles a lost pause acknowledgement, then resumes and confirms audited termination. `TestManagedLifecycleRetainsApprovedInvocationLineage` additionally retains consumed authorization after a dispatched adapter error. |

Additional public tests cover intent persistence failure before external prepare
or start, concurrent coordinators starting only once, foreign/malformed/stale
provider receipts, provider request alias mutation, caller declarations unable
to upgrade an unsupported provider, privacy-safe provider errors, and legacy
Manager refusing external termination without a reconnected coordinator.

`ExampleCoordinator` is an executable public-boundary offline harness: prepare,
start, lost termination acknowledgement, reopened Store, original-operation
reconciliation, exactly one mock resource. Text `runs show` exposes neutral
lineage and readiness; JSON includes full operation evidence. CLI inspection
registers no provider and has no new model-launch default.

## TDD and verification evidence

Raw local logs are retained in `/tmp/agent-manager-issue25-evidence/`. Actual
RED/GREEN pairs are `01` (missing public API), `02` (unknown-start governance
gate), `04` (manager lifecycle integration), `05` (confirmed termination reason),
`07` (raw provider diagnostics), `09` (nested authority alias), and `10`
(explicit unsupported response). Some initial REDs are compilation failures;
these are not live-runtime evidence. The first `04-green` exposed an expired
fixed-time fixture; `04-green-2` is the passing corrected fixture result.
Recovery/control and additional boundary tests in `03` and `06` also exercise
the shared lifecycle contract; those logs are not claimed as separate REDs.

The initial clean-baseline Go log completed with all package results passing,
but its shell wrapper failed afterward by assigning zsh's read-only `status`
variable. That wrapper is not exit-0 evidence. A separate frozen export of
`ce523d450fd0e82e32d114787ec9bd7ca8219ec9` ran uncached full tests with captured
exit 0 (`baseline-frozen.json` and `baseline-frozen.log`).

The following commands all completed after the final source fixes. Actual exits
are captured in `final-source-*.exit`; logs have matching names. The final Go
source fingerprint is retained separately for binding the unchanged source to
the reported implementation commit. Subsequent edits only add this evidence
record and update the task checkboxes.

| Command | Actual exit / result |
| --- | --- |
| `go test -count=1 ./...` | 0 / PASS |
| `go test -race -count=1 ./internal/run ./internal/governance ./internal/invocation ./internal/enforcement ./internal/cli` | 0 / PASS |
| `go vet ./...` | 0 / PASS |
| `go build -o /tmp/agent-manager-issue25-evidence/agent-manager ./cmd/agent-manager` | 0 / PASS |
| `openspec validate add-runtime-enforcement-lifecycle --strict` | 0 / valid |
| `openspec doctor` | 0 / root OK, no declared references |
| `git diff --check` | 0 / PASS; final staged/base-relative checks include new files |
| `go test ./internal/run -run ExampleCoordinator -count=1 -v` | 0 / offline public harness PASS |

Requirement verification is the direct matrix audit above, separate from strict
artifact validation. Only the three exact task-generated CLI eval YAML files
were removed; the existing CLI fixture now supplies an explicit temporary
project so subsequent tests do not write eval artifacts into the checkout.

## Delivery boundary

Implementation/verification tasks are 7/9. Independent specification/quality
review and required final read-only OCR remain pending under the controller;
3.2 and 3.3 remain unchecked. The change remains active and unarchived. The
implementer made no GitHub writes, pushed nothing, and changed no native agent,
account, Memory provider, DB or service. Publication, merge, main acceptance,
Issue checkbox updates and later specification synchronization/archive remain
controller work. Parent #16 is not completed.

This is deterministic contract/fixture evidence. The mock receipts are retained
by the test provider instance across coordinator replacement, not by a live
external service. No model, network runtime, OpenShell or real sandbox starts.
Directory placement and context propagation do not establish interception. R1
preflight is declaration admission; execution providers remain responsible for
real mediation. Missing query support requires manual recovery rather than a
blind retry. Policy application/revision, event retrieval, kernel/container
isolation, secret management, checkpoint and watchdog remain outside R2.
