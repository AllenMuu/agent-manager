# R2 / Issue #25 accepted delivery and archive

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
| 5 / 2.5 | Mock lifecycle and invocation | `TestMockLifecycleAndApprovedInvocationThenControlReconciliation`: prepares/starts a named mock run, approves and dispatches once, records succeeded/ALLOW request/completion/approver lineage, refuses reuse, reopens the Store and reconciles a lost pause acknowledgement, then resumes and confirms audited termination. `TestManagedLifecycleRetainsApprovedInvocationLineage` additionally retains consumed authorization after a dispatched adapter error. `TestLifecycleUnknownDuringPreflightBlocksDispatch` holds adapter preflight while an independent pause becomes durably unknown; both ordinary ALLOW and approved attempts remain undispatched with blocked completion lineage and unused approval. |

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

The following table records the original implementation-source checks for
`e4ac8e163c9f7b22a816072f3681c481a6534a4c`. Actual exits are captured in
`final-source-*.exit`; logs have matching names. That historical fingerprint
still binds the original implementation, not the later Q1 source repair.
The ordinary-review repair section below records the new final-source checks.

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

## Ordinary-review repair Q1

Ordinary specification review covered 26/26 entries and accepted all five
requirements/six scenarios; its roadmap documentation finding D1 was corrected
in doc-only commit `e8926019df152d7206476d0e4a4b380a7d5908fd`. Ordinary quality
review at that head found Q1: a durable unknown lifecycle transition occurring
inside adapter capability preflight could still be followed by ordinary ALLOW
dispatch. This is a control-plane dispatch authorization boundary, not a claim
of sandbox interception.

`TestLifecycleUnknownDuringPreflightBlocksDispatch` was added first through
public provider/coordinator/Invoker seams. `11-q1-red.log` captured actual exit 1:
the ordinary ALLOW subcase dispatched one call despite persisted unknown pause;
the approved subcase already blocked through atomic approval consumption.
`11-q1-green.log` captured exit 0 after the minimal common post-preflight durable
readiness check. Both paths now leave a correlated blocked completion, zero
adapter calls, and the approved path's authority remains unconsumed. The
existing atomic approval-consumption check is retained. The gate authorizes
dispatch at its current durable read; it does not promise interception of a
later external transition after dispatch has been authorized.

After the final source edit, full `go test -count=1 ./...`, the same five-package
race command above, vet, CLI build, strict change validation, doctor, working and
full base-relative diff checks all captured exit 0 in `q1-final-*.exit` and
matching logs. The focused regression and prior managed invocation lineage
tests also pass (`q1-final-focused.log`). `q1-source-fingerprint.json` binds the
unchanged Go source to the separately reported repair commit; the frozen-head
metadata is `q1-final-head.json`. Only this delivery record changes after those
source checks. At this implementation checkpoint, tasks remained 7/9 and ordinary rereviews/final OCR were pending. The subsequent review outcome is recorded below.

## Historical prepublication delivery boundary

Implementation and review tasks are now 9/9. Ordinary specification and quality
rereviews of Q1 passed, followed by the required independent final read-only OCR
using `gpt-6.1-sol` / `high` at source commit
`64a02b31c482e8e5ea0f3bd8442ef7ce1225d373`. The final gate reviewed all 26 unique
entries: nine OCR-selected and 17 excluded entries manually supplemented, with
zero skipped and no actionable findings. Tasks 3.2 and 3.3 are checked on that
evidence. Publication metadata and the exact proposed PR body still require
the supplementary read-only gate before publication. The change remains active and unarchived. The
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


## Independent review completion

The ordinary specification review covered 26/26 entries and all five requirements
and six literal scenarios. D1 was corrected and its doc-only rereview passed.
After Q1, the specification rereview covered the three changed paths and retained
unchanged full coverage; fresh regression and affected race checks passed. The
quality rereview independently copied its original probe byte-for-byte into a
frozen source export and observed exit 0, with no remaining actionable findings.

The final OCR used actual CLI 1.12.12 preview/rules, an exact source export, and
fresh full Go tests, five-package race checks, vet, CLI build, strict active-change
and all five main-spec validation, doctor and diff checks; all exited 0. It also
reran the original Q1 probe and independently tested lost start acknowledgement
and external start success followed by local confirmation failure. Both recover
the original operation/handle/generation without relaunch or duplicate start
audit. The five carried R1 archive pairs are byte-identical and the synchronized
main spec is normatively equivalent. No real model or runtime starts.

Local reports: `/tmp/agent-manager-issue25-spec-review.md`,
`/tmp/agent-manager-issue25-spec-rereview.md`,
`/tmp/agent-manager-issue25-spec-q1-rereview.md`,
`/tmp/agent-manager-issue25-quality-review.md`,
`/tmp/agent-manager-issue25-quality-q1-rereview.md`, and
`/tmp/agent-manager-issue25-final-ocr/report.md`. Their source/head and scope
records distinguish historical findings from accepted rereviews. This metadata
update changes no Go source, capability requirements or scenario content.

R1 archive documentation is included in this proposed delivery. R2 remains active
until its authorized merge and actual main acceptance; #25 and parent #16 remain
OPEN at this snapshot. M5 remains on its separate local branch awaiting confirmed
restoration of the current Claude service subscription.


## Accepted main and specification archive

[PR #41](https://github.com/AllenMuu/agent-manager/pull/41) merged on
2026-10-06 at `27697c56d1754631da1dfe2724b24431d5313019`. The fetched main tree
is byte-identical to accepted publication head
`9a31958843788a13d55515189f280b65ea32007b`; its Go source remains identical to
reviewed source `64a02b31c482e8e5ea0f3bd8442ef7ce1225d373`. The supplementary
read-only OCR gate passed 3/3 metadata paths and the exact PR body 1/1, preserving
full 26/26 unique coverage with zero skipped and no actionable findings.
#25 is CLOSED; all five acceptance items were updated with main delivery evidence
and read back. Parent #16 remains OPEN, with #26 the next dependent increment.

On 2026-10-06 the complete 9/9-task change was synchronously synced and archived
at `openspec/changes/archive/2026-10-06-add-runtime-enforcement-lifecycle/`.
The new [main lifecycle specification](../../openspec/specs/runtime-enforcement-lifecycle/spec.md)
retains the accepted Purpose and all five requirements/six scenarios, changing
only the title and delta heading. All five original artifacts, including
`.openspec.yaml`, are preserved. Main-spec strict validation passed 6/6 after
sync. Archive instructions contained no optional context/guidance; the valid
specs instruction snapshot had no artifact rules.

This synchronization/archive and current delivery-state documentation are local
on the #26 branch until its separately reviewed PR is merged. No Go source or
live enforcement evidence is added. The prepublication sections above describe
the historical frozen review snapshots; they do not override this accepted
state. M5 service restoration remains unconfirmed and its separate branch is
preserved.
