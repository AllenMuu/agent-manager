# Runtime permission proposals: Issue #26

R3 is implemented and verified on `feature/issue26-runtime-permission-proposals`
in `/Users/allenj/.codex/worktrees/b9dc/agent-manager`. This is branch evidence:
ordinary independent specification/quality review and final gpt-6.1-sol/high
read-only OCR have passed at source commit
`737211bc48e422d0379693facca939b57b09874b`. Publication metadata and the exact PR
body still require their supplementary read-only gate. Publication, main
acceptance, synchronization and archival remain pending. Parent #16 and #26
remain open. The active R3 change has 9/9 implementation/review tasks complete.

## Prerequisites and selected boundaries

The starting HEAD was `5068a37dec1ce614c6abe1d422262ba85de8596c`; full baseline
`go test -count=1 ./...` passed with exit 0. Main base is
`27697c56d1754631da1dfe2724b24431d5313019`. Live read-only GitHub checks confirmed
#26 OPEN, no comments, native `blocked_by` #25 CLOSED/completed with all five
criteria checked, and PR #41 MERGED at that main commit. A transient GraphQL EOF
on one #25 read was followed by a successful REST read; no authentication change
was made.

The root confirmed the selected functional ports within the previously approved
R3 design before the first test. Exact exported symbol names and the additive
`network:<hostname>` mapping are implementation selections recorded here, not a
claim of a separate human authentication event. Public seams:

- Existing `Store.Start/Get/Events` and `Manager.EvaluateAndRecord` or
  `EvaluateAndRecordWithIdentity` produce real persisted policy-denial lineage.
- `Manager.RequestPermissionProposal` creates one bounded proposal;
  `Store.GetPermissionProposal` exposes its durable lineage/decision audit.
- A separate trusted-host-configured `PermissionProposalDecider.Decide` and
  `ValidateUse` use `OperatorAuthority`, `CurrentPolicyBoundary`, and an injectable
  clock. Ordinary runtime requests contain no authority-installation or
  decision-identity fields.

The host authority establishes and rechecks an authorized human, independent of
caller kind/roles labels. Receipts bind the exact proposal/run/denial/base/decision.
Run-actor self-approval fails even if its kind is changed to human. Use requires
the original established operator identity/authority to remain authorized. Safe
scope identifiers are retained only for named, action-attributed credential
permission-denial audits; secret-shaped fields are rejected and historical
omission remains compatible. Older missing-scope credential audits are readable
but cannot establish an exact credential proposal.

The base boundary holds its authoritative snapshot stable through the Store
transaction. Lock order is policy boundary → Store transaction → operator
resolution; ports must not re-enter Store. The default uses the persisted
immutable initial run snapshot, read before the transaction. The revision
reference consists of policy ID/version/hash/resolution time. R3 implements no
mutable revision setter or application; R4 must coordinate confirmed revision
application separately. `ValidateUse` is eligibility validation, never a retained
execution/application authorization.

## Literal requirement and scenario evidence

The local verification is a manual requirement/scenario evidence audit against
all five requirements and six scenarios in the active OpenSpec capability,
separate from CLI artifact validation. Tests use package `run_test` and public
interfaces, real local persistence, deterministic clocks and external-boundary
fixtures. They do not assert private helpers or mock internal collaborators.

| #26 criterion / task | Requirement / scenario | Public evidence and literal outcome |
| --- | --- | --- |
| 1 / 2.1 | Denial-bound expiring proposals / Denied network action requests permission | `TestDeniedNetworkProposalPersistsExactLineageWithoutPolicyMutation`: pending proposal survives reopening with original DENY, run, identity/delegation, complete immutable base, exact difference and expiry; run/policy unchanged. `TestProposalRejectsForeignOrDifferentDenialLineage` rejects missing/foreign audit, changed action/destination/category, completion, ALLOW and budget denial. |
| 2 / 2.2 | Trusted human decision authority / Agent spoofs human metadata | `TestAgentHumanLabelsCannotEstablishProposalDecisionAuthority` rejects the initiating agent relabeled human/approver. `TestOperatorDecisionRejectsUnauthorizedOrUnboundAuthorityReceipts` rejects unauthorized humans, agent/service, mismatched proposal/run/denial/base/decision and unsafe receipt. No approved proposal or policy mutation; unauthorized attempts retain safe outcome without invented identity. |
| 2 / 2.2 | Trusted human decision authority / Authorized operator decides | `TestAuthorizedOperatorDecisionPersistsAuthorityAndOriginalProposal` reopens the proposal and verifies established human identity, authority receipt, proposal/denial/base linkage and stable decision ID; run unchanged. This establishes the fixture host contract only. |
| 3 / 2.3 | Expiry and base revision checks / Stale or expired request | `TestProposalDecisionAndUseRejectExpiredOrStaleAuthority` covers decision and use for proposal expiry, delegation expiry, and changed authoritative base, returning explicit typed errors with policy unchanged. Failed authorized decisions retain operator attribution. `TestProposalRechecksClockAfterOperatorBoundaryReturns` covers expiry during authority resolution; `TestProposalDecisionResolvesOperatorInsideStableCurrentBaseBoundary` covers decision/use within the external stable-base contract. |
| 4 / 2.4 | Delegation ceiling / Permission exceeds delegation | `TestNetworkProposalCannotExceedExactDelegationCeiling` rejects subdomain/prefix and ungranted destinations. `TestCredentialProposalBindsExactSafeScopeAndCeiling` accepts an exact delegated credential-denial scope and rejects an above-ceiling scope. `TestToolProposalRequiresProvenExactActionDelegationCeiling` requires a matching action rule and all exact delegated scopes; absent/empty/ungranted ceilings fail closed. `TestNetworkProposalRejectsDestinationThatCannotFitCanonicalScope` rejects an unrepresentable mapped scope. A rejected creation has no persisted proposal to approve; the decision port cannot supply a larger difference/delegation. |
| 5 / 2.5 | Distinct non-mutating proposal decisions / Proposal is rejected or merely approved | `TestProposalRejectionOrApprovalDoesNotAuthorizeRetryOrActionApproval`: rejected and approved outcomes leave run/effective policy unchanged, create no one-action Approval, keep retry DENY, and cannot authorize retry with a proposal ID. No lifecycle resume or policy application is invoked. Existing F0/R2 approval and lifecycle suites pass unchanged. |

Additional public regressions cover original-operator revocation/replacement
(`TestProposalUseRechecksOriginalOperatorAuthority`), cancellation
(`TestCanceledOperatorDecisionCannotApproveProposal`), secret-shaped credential
scope rejection (`TestCredentialScopeAuditNeverPersistsSecretShapedValues`), and
one terminal decision under twelve concurrent deciders using separate Stores
(`TestConcurrentProposalDecisionsAcrossStoresHaveOneTerminalDecision`).

## TDD and final validation

Actual RED → minimal GREEN cycles were recorded for 2.1; both 2.2 scenarios;
2.3; network, credential and tool ceiling slices of 2.4; and 2.5. Initial additive
API tests failed to compile on missing public ports/fields; later tool/rejection
cycles failed on literal behavior. Self-review reproduced and repaired changed
operator identity on use, canceled approvals, and lost attribution of authorized
failed decisions. The first full-suite run exposed the existing
`TestAuditOmitsUntrustedActorText` privacy regression; the fix retained historical
scope omission outside attributable named permission denials, and that existing
regression plus new scope tests passed. These are distinct recorded failures,
not claims that every RED was an executable runtime failure.

Evidence logs reside in `/tmp/agent-manager-issue26-evidence`. All final commands
below completed with exit 0 after the final Go source edit:

| Command | Final evidence |
| --- | --- |
| `go test -count=1 ./...` | `full-tests-final.log` |
| `go test -race -count=1 ./internal/run ./internal/governance ./internal/cli ./internal/enforcement ./internal/adapter` | `races-final.log` |
| `go vet ./...` | `vet-final.log` |
| `go build -o /tmp/agent-manager-issue26-evidence/agent-manager ./cmd/agent-manager` | `build-final.log` |
| `/tmp/agent-manager-issue26-evidence/agent-manager --help` | `cli-help.log` |
| `openspec validate add-runtime-permission-proposals --strict` | `openspec-strict.log` |
| `openspec doctor` | `openspec-doctor.log`; nearest repository root healthy |
| `git diff --check` and staged/full-base diff checks | `diff-check.log`, `full-base-inventory.log` |

`source-fingerprint.json` covers all 188 staged/tracked Go paths in sorted order,
with path and file bytes separated by NUL. Aggregate SHA-256:
`90a11bda10e40525129e3e1778e171f8e13820478ff3454408c7da1005fc582d`.
The committed-source comparison is recorded in `committed-source-check.log`.

## Scope and remaining gates

The authority, current-policy and clock fixtures are offline contract evidence.
No real human authentication, model, provider policy change, OpenShell, native
sandbox, kernel/container isolation, credential resolution, network dependency
or managed-skill execution was introduced. Services, subscriptions, models,
account/authentication and native runtime configuration were not changed. M5
remains separate and pending restored current subscription evidence.

The prerequisite R2 carry from `5068a37` is preserved: five exact archive renames,
one synchronized main capability, and the three prior delivery/roadmap documents.
The whole proposed PR inventory includes those entries alongside R3 and must be
covered by the completed independent ordinary review and final OCR. Tasks 3.2
and 3.3 are checked on that evidence. No PR, push, GitHub mutation, merge, review acceptance or main delivery
is claimed by this implementation record. R3 remains active until acceptance and
authorized merge; this slice does not close parent #16.


## Independent review completion

The ordinary specification report `/tmp/agent-manager-issue26-spec-review.md`
passed all 17 entries, five requirements and six scenarios with no findings.
Fresh full tests, related races, vet/build, strict OpenSpec/doctor/diff checks,
archive equivalence and the 188-Go-file fingerprint passed. Four extra independent
probes verified cancellation during authority resolution, changed authority ID
on use, historical missing credential scope remaining unusable, and eligibility
validation leaving the denied retry unchanged.

The ordinary quality report `/tmp/agent-manager-issue26-quality-review.md`
passed 17/17 entries with no actionable findings. Independent alias-mutation,
sanitized authority-error/replay and publication-failure probes also passed.
The required final OCR report `/tmp/agent-manager-issue26-final-ocr/report.md`
used actual CLI 1.12.12 preview/rules: four selected and 13 excluded entries
manually supplemented, full 17/17 unique coverage, zero skipped and no actionable
findings or important gaps. Fresh full Go/race/vet/build/help, strict active and
main specs, doctor/diff/gofmt checks, two new boundary probe functions with seven
subcases, and replayed independent prior probes all passed. All 418 tracked bytes
and the reviewed source remained unchanged during that gate.

This metadata changes no Go source or normative capability content. The exact
publication head/body require the supplementary read-only gate; review-task
completion does not establish main acceptance, Issue closure or live operator
authentication. The accepted R2 specification/archive carry will enter main
through this separately reviewed delivery.
