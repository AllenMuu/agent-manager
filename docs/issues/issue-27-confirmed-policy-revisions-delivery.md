# Issue #27: confirmed policy revisions

R4 is implemented on `feature/issue27-confirmed-policy-revisions`, based on
`9eef5cf8bd241e4aea91fabe0e10afdbde120beb`. Main prerequisite is
`490bff8472d86039b13f83f726e332e66bb9ba81` (PR #42 / #26). On 2026-10-07,
the native blocker endpoint returned only CLOSED #26, its five acceptance items
were checked, PR #42 was MERGED and #27 remained OPEN. No GitHub write, model,
service, authentication, native configuration or dependency change occurred here.

The nine R3 synchronization/archive entries already in `9eef5cf8` are preserved
and belong in the complete future PR review. R4 stays active until acceptance and
merge. The separate M5 branch and unconfirmed existing Claude service restoration
are unchanged. This delivery does not close parent #16 or claim R5 protection.

## Selected public boundaries

The root controller confirmed these functional ports within the previously
approved design before tests; this was not live human authentication:

- Existing Coordinator prepare/start/pause, Manager evaluation/proposal request,
  trusted PermissionProposalDecider decision/use, Store inspection and events.
- Coordinator ApplyPolicyRevision, ReconcilePolicyRevision and host-configured
  ReceivePolicyEvent; optional provider PolicyApplier/PolicyRevisionQuerier.
- Public persistence intent/confirmation/retry/event ports, including injected
  persistence failures through a Store wrapper rather than private hooks.
- Host-configured exact-action Prepare/Dispatch adapter for network/credential
  retry. Governance Invoker implements the tool adapter through the existing
  invocation PrepareDispatch boundary. Adapters are installed once by the host.
- RequestPolicyRetryApproval and an optional exact one-action approval ID on
  RetryPolicyAction retain independent F0 approval and atomic single-use dispatch.

Record.Policy, R2 Operation.Policy, original proposals and trusted decision
receipts remain immutable. New action evaluation and invocation context use the
confirmed applied revision. Historical audit/completion references resolve their
specific snapshot, including old F0 approval records. The default R3 current base
is the applied snapshot checked inside the transaction; custom current boundaries
hold their outer lock and must agree with the durable applied reference. Lock
order remains policy boundary → Store transaction → operator port; operator
ports cannot re-enter Store.

Store v3 reads legacy v1/v2 without rewriting on read and requires v3 for revision
mutations. Old fixed-snapshot readers reject the new envelope instead of dropping
history. Snapshot comparison validates both canonical contents and compares
ID/version/hash/resolved-at; it does not trust a caller's hash alone.

## Requirement and scenario acceptance matrix

All assertions use public seams. Offline provider/authority/action fixtures are
contract evidence, not real sandbox, network, credential or human-auth evidence.

| Ticket item / task | Literal scenario | Exact public behavior proof |
| --- | --- | --- |
| 1 / 2.1 | Change policy after prior audit | TestRevisionRetainsOriginalPolicyAndHistoricalAudit: initial and later snapshots retained; old events keep the original hash, new events use applied hash; reopened Store resolves both; old proposal use rejects stale base |
| 2 / 2.2 | Provider only accepts update | TestProviderAcceptanceLeavesAppliedAndExecutionPaused: desired advances, applied stays initial, pending state persists; Coordinator Resume and direct Store BeginLifecycle cannot bypass pause after restart |
| 2 / 2.2 | Exact application is confirmed | TestConfirmedApplicationNeedsNewlyAuthorizedExactRetry: exact correlated receipt persists applied target while paused; ordinary resume remains blocked; newly authorized exact network retry resumes and dispatches once |
| 3 / 2.3 | Wrong acknowledgement or timeout | TestWrongAcknowledgementAndTimeoutKeepRetryPaused: wrong revision is failed, timeout unknown; applied stays initial and resume is blocked; exact query evidence advances applied without dispatch |
| 4 / 2.4 | Inspect approved permission change | TestInspectPermissionChangeReconstructsFullDenialToRetryChain: inspection links original DENY → proposal → trusted human fixture decision → mutation → confirmation → applied-policy retry request/completion, with desired/applied evidence |
| 5 / 2.5 | Duplicate stale event | TestDuplicateStaleAndUntrustedPolicyEventsNeverDispatch: raw origin/trusted metadata grants nothing; configured adapter established evidence confirms once; duplicate is inert, old generation is diagnosed stale, no extra dispatch |

## Additional boundary evidence

- TestToolRevisionRetryRequiresSeparateSingleUseActionApproval proves that a
  policy approval cannot bypass REQUIRE_APPROVAL. The same denied tool action
  receives a separate F0 approval bound to the applied revision. Dispatch consumes
  both authorities atomically; invocation context and completion contain its
  approval/approver references, and reuse dispatches nothing.
- TestCredentialRevisionRetriesOnlyExactDelegatedScope preserves exact scope and
  uses an offline action fixture without resolving credentials.
- TestRevisionIntentAndConfirmationPersistenceFailuresNeverBlindlyReplay proves
  durable intent precedes external update; failed local confirmation leaves the
  old applied revision and recovery queries the original operation.
- TestUnknownRetryCompletionCannotDispatchAgainAfterRestart retains a durable
  dispatch claim when completion persistence fails; restart cannot replay it.
- TestRetryPreflightAndNotDispatchedDoNotConsumeActualAction preserves retry on
  preparation failure and releases only a known NotDispatched attempt. Earlier
  attempts remain in immutable audit/attempt history; unknown dispatch never
  replays automatically.
- TestRetryRevalidatesAuthorityExpiryAndBudgetAfterPreparation and
  TestPolicyReceiptAllCorrelationsAndCanonicalContentFailClosed cover revoked/
  expired authority, invalid budget, foreign run/handle/generation/operation/base
  and mutated canonical content. Provider arguments do not alias run authority.
- TestConcurrentStoresApplyAndRetryExactActionAtMostOnce exercises independent
  Store/coordinator instances against the same durable path.
- TestOfflineMockPolicyUpdateReconcilesLostAcknowledgementWithoutReplay exercises
  the public offline MockProvider apply/query ports after a committed lost receipt.
- TestHistoricalActionApprovalCannotAuthorizeNewRevisionThroughDirectStore is a
  discovered R4 compatibility regression. Old same-action approval remains
  inspectable, but direct Store/Manager paths cannot attach or consume it for a
  different applied revision. Approval request/dispatch/completion bindings
  compare ID/version/hash/resolved-at to their historical request, never to a
  blanket newest revision for old records.
- TestManagedNetworkPolicyPreparationPreservesValidatedSnapshot is a discovered
  R2 integration regression: JSON omitempty changes empty-list representation.
  Both snapshots must validate before exact semantic reference comparison; all
  other operation/provider/identity/handle/generation/state checks are retained.

## Evidence and validation

Local evidence directory: `/tmp/agent-manager-issue27-evidence`.

Frozen baseline `go test -count=1 ./...` exited 0. Each task scenario has saved
RED/GREEN logs and exact `.exit` files: 2.1 (including behavioral audit RED),
2.2-accept, 2.2-confirm, 2.3, 2.4 and 2.5. Integration network, safe nondispatch,
tool dual approval, public offline mock and historical action approval have
separate RED/GREEN evidence.
The failed intermediate runs are retained; they are not final acceptance claims.
`baseline-source-blobs.txt` binds the frozen source and
`final-source-sha256.txt` binds the final Go source; `final-head.txt` records the
source/docs commit after validation.

Final Go/race/vet/build/strict OpenSpec/doctor and complete base diff results are
recorded in their named logs and `.exit` files. Final results: `go test -count=1 ./...` exit 0;
`go test -race -count=1 ./internal/run ./internal/governance ./internal/invocation ./internal/enforcement`
exit 0; `go vet ./...` exit 0; CLI build exit 0;
`openspec validate apply-confirmed-policy-revisions --strict` exit 0;
`openspec doctor` exit 0. Complete base diff and staged-new-file checks exit 0.
Tasks 1.1, 2.1–2.5 and 3.1 are complete (7/9); review tasks remain unchecked.

Ordinary independent specification/quality review (task 3.2), required final
read-only gpt-6.1-sol/high OCR (task 3.3), PR publication, merge/main acceptance,
Issue closure and R4 synchronization/archive remain pending. The root controller
owns those review and delivery gates; this implementation does not mark them done.
