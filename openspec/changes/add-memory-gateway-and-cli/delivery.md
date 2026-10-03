# M3 / Issue #21 delivery evidence

Status: implemented and verified on `feature/issue21-memory-gateway-and-cli`;
7/9 OpenSpec tasks complete. Ordinary review repairs and the explicitly authorized final-OCR F1 repair are
implemented; fresh independent acceptance and final read-only OCR remain
controller tasks 3.2/3.3. No PR, push, merge or Issue close is claimed.
Parent #7, Mem0/M4, shared native agent acceptance/M5 and runtime slices remain
outside this delivery.

## Prerequisite and approved public boundaries

Ticket #21 and its native dependency were re-read from GitHub. Blocker #20 was
CLOSED and its accepted delivery PR #34 was MERGED to main
`b388a67ba79aa60a79106574f0d1d830ec918513`. Existing full-suite baseline passed
(`/tmp/agent-manager-issue21-baseline.log`). This branch began M3 at controller
commit `c544feba85293f3fcd0ec4b4c83b49c5d1acd484`, which preserves the historical
M2 specification sync/archive dated 2026-10-02. M3 does not change that archive.

The user authorized continued execution of the approved roadmap. The controller
recorded these public test boundaries as part of that authorization: Memory
Gateway discovery/read/search and immutable mutation preview/exact confirmation;
explicit public project registration/relocation; CLI
`NewAgentManagerCommand().ExecuteC()` against real temporary config/state; and
`taskcontext.ResolveWithOptions`. The controller confirmed the optional scored
provider boundary as a routine retrieval extension. This records the selected
execution seams, without inventing an earlier user approval of exact Go names.
`tasks.md` is the only implementation checklist.

## Requirement/scenario acceptance matrix

All seven literal scenarios are verified for M3 on this branch. Evidence is
under `/tmp/agent-manager-issue21-evidence/`; named tests remain in the repository.

| Ticket item / task | Requirement and literal scenario | Public evidence | Outcome |
| --- | --- | --- | --- |
| 1 / 2.1 | Truthful provider discovery / Configured search is not implemented | `TestConfiguredSearchIsReportedSeparatelyFromImplementation`, `TestMemoryStatusSeparatesUnsupportedTextSearchFromRequestedCapabilities`, `TestConfiguredStructuredDiscoveryIsReadOnlyAndReportsActualOperations`, `TestStructuredCLIStatusReportsImplementedOperations`, `TestAgentStatusDoesNotTurnMissingImplementationIntoAvailabilityGap`; `01`, `09`, `14`, `22` red/green logs | Requests, implemented optional interfaces, unsupported requests and unavailable implemented requests are separate. Text search is unsupported; local structured CAS/atomic supersede are truthful. Config references stay out of output; native agent mechanisms remain unsupported. |
| 2 / 2.2 | Confirmed owned mutations / Mutation is not confirmed | `TestOwnedMutationPreviewRequiresExactConfirmation`, `TestConfirmedLifecycleKeepsInspectableRetiredLineage`, `TestImportConfirmationBindsOwnerSourceAndExactPreviewedContent`, `TestStructuredCLIAddPreviewsOwnedIntentAndRequiresConfirmation`, `TestStructuredCLILifecycleRequiresConfirmationAndPreservesHistory`, `TestStructuredCLIImportAndProjectRelocationAreExplicit`; `02`, `04`, `05`, `10`, `11`, `15` red/green logs | Add/update/supersede/forget/import preview exact owner/intent and perform no provider mutation without confirmation. Import additionally confirms exact owner/source and digest-bound inert bytes; a changed source conflicts before a batch write. Retired lineage remains explicitly inspectable. |
| 2 / 2.2 | Confirmed owned mutations / Stable project identity | `TestProjectIdentityIsDistinctAndRelocationIsExplicit`, `TestProjectMappingConfirmationBindsDisplayedDirectoryIdentity`, `TestStructuredCLIImportAndProjectRelocationAreExplicit`; `03`, `13`, `15` red/green logs | Same basenames receive different opaque IDs. Relocation requires exact confirmed mapping update and retains the ID. A replaced displayed directory is rejected. |
| 3 / 2.3 | Bounded deterministic retrieval / Bounded attributed search | `TestSearchSelectsBoundedAttributedCurrentRecordsDeterministically`, `TestScoredRetrievalKeepsSemanticMatchesAndCountsFullAttribution`; `06`, `08` red/green logs | Owner/type/ACTIVE filters, configured relevance, count, aggregate content and full canonical JSON-array budgets apply. Lexical fallback and explicit normalized provider scores are distinct; equal ranks use neutral IDs. Whole attributed records are selected without mutating provider-owned input. |
| 3 / 2.3 | Bounded deterministic retrieval / Wrong-owner query | `TestWrongOwnerIsRejectedBeforeAnyProviderAccess`; `07-owner-green.log` | Foreign requested owner is rejected before capabilities, health or query can cross the external provider boundary. This behavior was already established by the authorization slice; the verification was green when added, with no invented red log. Returned owners are independently checked by `TestGatewayRejectsForeignAndMalformedProviderRecords` (`21` red/green). |
| 4 / 2.4 | Shared read-only task-context path / Context handoff reads knowledge | `TestTaskContextReadsAttributedKnowledgeThroughSameGateway`, `TestRoleContextUsesConfiguredGatewayWithoutImplicitWrites`; `16`, `17` red/green logs | Same configured provider/Gateway/policy produces the same attributed records as CLI search. Canonical content appears once in `memoryRecords`; legacy text API stays compatible. Handoff has read-only authority and creates no implicit write/promotion. |
| 5 / 2.5 | Independent failure and recovery semantics / Provider outage | `TestMemoryOutageLeavesOtherTaskContextResourcesAvailable`, `TestMemoryOutageReportsUnavailableAndSkillWorkflowStillWorks`; `18`, `19` red/green logs | Query reports unavailable with a safe category. Following real Skill inspection remains functional; role context retains selected Skills/artifacts and reports unavailable Memory. No canonical Memory record is placed in the Skill operation journal or promised filesystem undo. |

The public test corpus adds 35 top-level tests executed on Darwin: 23 Gateway/
registry tests, 10 CLI tests and 2 task-context tests (with additional table cases).
Real structured-local/registry filesystem tests are in supported-Unix build-tagged
files. Portable external-fixture/status tests remain available on other systems.
An additional unsupported-platform discovery test is Windows-compiled, not
executed here.

## Recovery, compatibility and self-review

Self-review checked authorization before provider access, immutable intent
snapshots, full attribution budgets, optional-interface truthfulness, registry
mapping integrity, provider error redaction and independent resource failures.
It added public regressions for malformed UTF-8/JSON-surrogate registry identity,
reserved canonical/lock inode aliases, replaced directory mappings, malformed or
foreign provider records and unavailable-agent capability gaps (`12`, `13`, `20`,
`21`, `22` red/green). Uncertain writes keep `outcome-unknown` even when
cancellation is an underlying cause (`23` red/green).

The controller identified an integration edge during self-review: a persisted
uncertain lifecycle receipt must be reconcilable through a fresh process. The
Gateway now leaves current-state/version CAS to the explicitly confirmed provider
mutation, while keeping authorized target inspection in preview. Identical
caller-supplied operation IDs can reconcile durable update/supersede/forget
receipts after a fresh Gateway opens; a genuinely new stale mutation still
conflicts. `TestFreshGatewayConfirmedRetryReconcilesPersistedUncertainLifecycleReceipt`
observed all three pre-fix failures and passes with real local storage and an
injected directory-sync failure (`24-fresh-receipt-red.log` → green). No automatic
retry is introduced.

M2 `LegacyImportRequest` gains only an optional expected-content digest; older
callers omitting it retain their existing import/receipt behavior. Existing M2
public import, canonical-isolation, Unicode and persistence/retry tests all pass
in the complete suite. Platform support constants only inform truthful M3
discovery; M2 storage behavior is unchanged. Legacy file promotion/defaults,
legacy task-context Searcher and unrelated Skill/SubAgent commands stay compatible.

## Ordinary review repairs

The controller's independent ordinary review at `4674e94` inspected all 38/38
complete-PR entries and found three Important defects. Its report and original
negative probes are `/tmp/agent-manager-issue21-quality-review.md` and
`/tmp/agent-manager-issue21-quality-cli-overlay-test.go`. All three repairs use
the already authorized public seams and actual temporary filesystem/CLI/store
behavior. Logs below are in `review-repairs/` under the evidence directory.

| Finding | Repair and public regression | Actual evidence |
| --- | --- | --- |
| I1 / stable project identity | Anchored no-follow traversal validates stored mappings before directory equivalence; stale mappings grant no owner lookup and allow explicit repair. `TestMovedRegisteredProjectCannotAcquireIdentityThroughSymlinkSubstitution` checks direct and ancestor substitutions, denied reads, unconfirmed zero mapping writes and confirmed relocation retaining records/ID. `TestProjectRegistryRecognizesRealCaseInsensitiveDirectoryAlias` preserves genuine filesystem case aliases. | `i1-symlink-red.log` → `i1-symlink-green.log`; real case alias PASS in `i2-first-write-and-guards-green.log` (compatibility check was green, no invented red). |
| I2 / independent outage recovery | Registry reads separate optional provider health from trustworthy owner lookup; inspectable aliases remain denied. Strict writes repeat guard checks even before the first registry file exists. `TestUnavailableProviderRootRetainsTrustedOwnerAndIndependentRoleResources` covers missing/file/symlink roots and independent artifacts/Skills; `TestRegistryFirstMutationChecksUnavailableProviderRoots` covers file/symlink first-write guards. Existing Unicode/reserved-name/canonical-state/persistent-lock hardlink checks remain green. | `i2-root-health-red.log` → green; `i2-first-write-red.log` → `i2-first-write-and-guards-green.log`. |
| I3 / faithful mutation intent | CLI exposes only effective flags; import content/evidence and forget record metadata are rejected before plan/provider mutation. Import type/source/layer survive. Gateway rejects unused direct inputs; import/forget plans omit record bodies and fingerprints bind the effective copied plan. Explicit import receipt IDs/content constraints are honored; contradictory IDs and blank sources are rejected. `TestStructuredMutationFlagsDescribeOnlyAppliedIntent`, `TestGatewayRejectsIneffectiveMutationInputBeforePreview`, `TestImportPreviewHonorsExplicitReceiptAndContentConstraints`. | `i3-cli-intent-red.log`, `i3-gateway-intent-red.log`, `i3-import-constraints-red.log` → respective green logs. |

I3 intentionally rejects import `--evidence` rather than extending M2. The original
reviewer's evidence-preservation probe therefore now fails at flag parsing as
required by the controller-selected correction; it is not an all-green acceptance
probe for this revised boundary. New repository regressions verify explicit
rejection and zero writes, then supported import metadata. New M3 commands have
no accepted release compatibility obligation yet. Fresh lifecycle receipt
reconciliation still passes after forget inputs are restricted to effective fields.

## Final OCR stop and explicitly authorized F1 repair

The prescribed independent `gpt-6.1-sol` / `high` final read-only OCR at
`5e4abd33da32b686c7ccab798f3bf49e162475f3` inspected all 38/38 changed entries
and returned **STOP**, with one actionable Medium finding F1: Gateway write
status tested `RecordWriter`, while confirmed add dispatches
`RecordOperationWriter`. The original report/probes remain unchanged under
`/tmp/agent-manager-issue21-final-ocr/`; that result did not authorize publication.
The user subsequently explicitly instructed “根据报告修复”, authorizing this
bounded repair. Fresh independent spec/quality acceptance and a new prescribed
final read-only OCR are still required before publication.

The repair changes only the Gateway implemented-capability projection: confirmed
Remember/write requires the operation-aware interface plus the declared Remember
flag. Generic low-level Remember and provider declarations are unchanged. No
silent basic-write fallback or fabricated receipt guarantee is introduced.

| Public behavior | Actual evidence in `ocr-repair/` |
| --- | --- |
| `TestGatewayDiscoversOperationAwareOnlyConfirmedAdd`: restricted wrapper of real StructuredStore confirms and durably persists add; unconfirmed add leaves zero records; Gateway advertises the callable operation; generic basic Remember stays unsupported for this interface shape. | `01-operation-aware-red.log` → `01-operation-aware-green.log` |
| `TestGatewayDoesNotAdvertiseBasicRememberAsConfirmedAdd`: real InMemoryProvider still supports generic Remember; unconfirmed and unsupported confirmed Gateway adds leave zero records; requested Gateway write is reported unsupported. | `02-basic-only-red.log` → `02-basic-only-green.log` |
| `TestGatewayConfirmedAddDiscoveryRequiresInterfaceAndDeclaration`: real both-interface local store preserves both APIs; operation-aware method without declaration and declaration without interface both remain unsupported with zero writes. Existing local discovery and exact-confirmation regressions remain green. | `03-compatibility-green.log` (existing compatibility behavior; no invented red) |

The operation-aware wrapper forwards actual storage/health and hides only the
separate basic interface; its success path uses real temporary canonical storage,
including reopening the durable record. The basic-only path uses the existing
real in-memory provider. False-interface/declaration wrappers cover unsupported
external shapes. These tests do not validate any future remote integration.

## Final validation (2026-10-03 Asia/Shanghai)

Initial implementation and ordinary-repair checks remain historical evidence in
the evidence root and `review-repairs/`. After the user-authorized F1 source
repair, the final checks below passed on the latest source; their logs and recorded
exit statuses are in `ocr-repair/`. Prior passing checks do not override the
original final OCR STOP. No extra broad sweep is needed without
further source changes. Exit statuses are in `ocr-repair/check-exit-status.json`;
the unchanged original OCR probes also pass in `original-ocr-probes-green.log`.

| Check | Result | Evidence file |
| --- | --- | --- |
| `go test ./... -count=1` | PASS | `go-test.log` |
| `go test -race ./internal/memory/... ./internal/taskcontext ./internal/cli -count=1` | PASS | `go-race.log` |
| `go vet ./...` | PASS | `go-vet.log` |
| `go build -o /tmp/agent-manager-issue21 ./cmd/agent-manager` | PASS | `go-build.log` |
| Windows amd64 `go test -c` for Memory, CLI and taskcontext | PASS, compilation only | `windows-memory-compile.log`, `windows-cli-compile.log`, `windows-taskcontext-compile.log` |
| `openspec validate add-memory-gateway-and-cli --strict` | PASS | `openspec-validate.log` |
| `openspec doctor` | PASS | `openspec-doctor.log` |
| `git diff --check` and staged diff check | PASS | `diff-check.log` |

Validation uses real temporary local directories/configs/canonical stores for
normal CLI/Gateway/context paths. Fixtures are limited to adversarial/unsupported
provider access, optional scored responses, unsafe metadata and external query or
write failures. Filesystem sync failures are injected at the public external
persistence boundary around real canonical writes. No Mem0, external service,
real agent runtime, Windows runtime storage, network fetch or dependency install
was validated or added. The optional scored fixture is not real semantic runtime
acceptance.

Ordinary independent repair acceptance (3.2) and the required independent
`gpt-6.1-sol` / `high` read-only OCR (3.3) remain pending with the controller.
Any actionable final OCR finding stops publication. M3 stays active until accepted
implementation and authorized merge, followed by spec sync/archive.
