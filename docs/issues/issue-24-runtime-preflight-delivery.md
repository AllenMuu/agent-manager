# R1 / Issue #24 branch acceptance evidence

Scope: [Issue #24](https://github.com/AllenMuu/agent-manager/issues/24),
accepted change `define-runtime-enforcement-capabilities`, archived at
`openspec/changes/archive/2026-10-06-define-runtime-enforcement-capabilities/`.
Base: `863ebc26f34b67f94b20d4d34b1f5097328f469a` (`origin/main`).
Branch: `feature/issue24-runtime-preflight`; reviewed implementation source:
`931f57a7f071cef51fc756827df4ce9dfe3e62a0`. Publication metadata is a separate
commit containing this updated document. #24 was read live, OPEN, unassigned, with no blockers.
The root controller verified native dependency state before dispatch.

The user approved this roadmap's development and continuation. The controller
selected neutral permission resolution, capability discovery, preflight and
additive CLI inspection as public seams within that design authorization.
Routine exported names were implementation choices, not separate human approvals.
ADR 0008 was written before enabling the behavior. A clean-base uncached Go
baseline passed before implementation.

## Requirement/scenario matrix

| Ticket criterion / task | Literal scenario | Public behavior evidence |
| --- | --- | --- |
| 1 / 2.1 | Inspect indirect permissions | `TestInspectIndirectPermissions` exposes denied tool separately from shell, network and credential contributions; `TestProviderContributionsAndConflictsRemainAttributed` preserves unknowns, exact path/ref identifiers and conflicting source declarations without mutation or grant union. |
| 2 / 2.2 | Required credential mediation is missing | `TestMandatoryCredentialGapRejectsBeforeExecution` rejects and names only credential when network/tool and legacy governance support are declared; no execution port exists in the API. Built CLI example returns exit 1 with `dimension: credential`, `code: missing_control`. |
| 2 / 2.2 | Optional gap | `TestOptionalGapWarns` returns accepted with an explicit filesystem warning; built CLI example also retains that warning while rejecting mandatory credential. |
| 3 / 2.3 | Static filesystem and dynamic network | `TestStaticFilesystemDynamicNetwork` reports filesystem `recreate-required`, network `live-update` and process `unsupported` independently. |
| 4 / 2.4 | Noop or directory adapter is selected | `TestNoopAndDirectoryCannotClaimProtection`, `TestNoopDeclarationHasNoExecutionProtection`, `TestDirectoryDeclarationCannotEnforceMandatoryControls` reject mandatory noop/directory declarations and expose unsupported support even if supplied claims say verified. |
| 5 / 2.5 | Local preflight suite | `TestOfflinePreflightNormalizesRepeatedChecks` compares normalized results byte-for-byte; source-order and unchanged-input checks use the public resolver. `TestOfflinePreflightCLIReportsMissingCredentialAndInspection` uses local JSON input. No model/sandbox/service/install is launched. |

Additional public regressions verify existing governance controls are not
silently ignored, a legacy network bool does not imply new network protection,
mandatory policy network rules cannot become optional, unknown dimensions get
stable machine-readable diagnostics, and malformed/unknown JSON is rejected.

## TDD and verification

Actual local evidence is under `/tmp/agent-manager-issue24-evidence/`.
Each of 2.1, 2.2 mandatory, 2.2 optional, 2.3, 2.4 and 2.5 has its own
`*-red.log` (actual command exit 1) and `*-green.log` (actual exit 0).
CLI, legacy compatibility, invalid declarations, downgrade prevention and
machine-readable invalid input have additional RED/GREEN evidence.
Some REDs are missing-public-API compile failures; later REDs are assertion
failures against implemented behavior. They are not claimed as runtime evidence.

| Final command | Actual exit / result |
| --- | --- |
| Clean-base `go test -count=1 ./...` | 0 / PASS (`baseline.log`) |
| `go test -count=1 ./...` | 0 / PASS (`full-final.log`) |
| `go test -count=1 -race ./internal/enforcement ./internal/adapter ./internal/policy ./internal/run ./internal/governance ./internal/cli` | 0 / PASS (`race-final.log`) |
| `go vet ./...` | 0 / PASS (`vet-final.log`) |
| `go build -o /tmp/agent-manager-issue24-evidence/agent-manager ./cmd/agent-manager` | 0 / PASS (`build-final.log`) |
| `openspec validate define-runtime-enforcement-capabilities --strict` | 0 / valid (`openspec-final.log`) |
| `openspec doctor` | 0 / root ok, no declared references (`doctor-final.log`) |
| Working and full base-relative diff checks | 0 / PASS; staged check also covers new files |
| Built binary `policies preflight <example> --json` | 1 / expected rejection, explicit credential error and filesystem warning |
| Built binary `policies inspect <example> --json` | 0 / JSON declaration report |

Requirement/scenario verification was a direct evidence audit, not a claim of
running an unavailable OpenSpec verify skill. Strict artifact validation and
implementation acceptance are separate checks. Only the exact eval artifact
files generated by these Go test runs were removed from `internal/cli/.agent-manager`.

## Delivery state at the reviewed prepublication snapshot

Implementation, branch validation and source review are complete; tasks are
10/10. Independent specification and quality reviews each covered 13/13 files
and returned PASS. The final independent `gpt-6.1-sol/high` read-only OCR also
returned PASS: OCR selected 4/4 files and manually reviewed all 9 excluded files,
for complete 13/13 coverage, zero skipped files and no actionable findings.
Actual `ocr delegate preview` and `rule` were executed. Source HEAD stayed
unchanged and the checkout was clean after each review.

Local reports are `/tmp/agent-manager-issue24-spec-review.md`,
`/tmp/agent-manager-issue24-quality-review.md` and
`/tmp/agent-manager-issue24-final-ocr/report.md`. Quality review independently
reran the full uncached Go suite, relevant race tests, vet, build and specification
checks; both ordinary reviewers and final OCR also ran independent built-CLI
probes. These checks remain offline declaration evidence.

The newly prepared task/review/roadmap metadata and exact PR body still require
supplemental read-only review before publication. PR creation, merge, main
acceptance, Issue closure and spec sync/archive are pending. No GitHub write or
service/account change was performed by the implementer. Task completion does
not establish main-branch delivery or complete parent #16.

This is deterministic offline contract/fixture and built-CLI evidence. It does
not validate a live execution provider, sandbox, credential manager or model.
Effective scope remains unknown because source declarations are not reachability
proof; accepted preflight is not execution authority. Existing `run.Manager`
execution wiring is unchanged. R2 owns lifecycle integration. #23/M5 source and
real-service evidence are outside this change. The parent #16 is not completed.

## Accepted main and specification archive

[PR #40](https://github.com/AllenMuu/agent-manager/pull/40) merged on
2026-10-05 at `0b6edffe2cb73b9d5091c9a4bdb65721063a7be9`. The fetched main tree
was byte-identical to accepted publication head `cffa4e18c55285915adb1c7baa1e863df5dc25b4`.
The supplemental required read-only gate passed all 3/3 incremental metadata
files and the exact PR body 1/1, preserving complete 14/14 unique PR file coverage.
#24 is CLOSED; all five acceptance checkboxes were updated with the merged-main
evidence and read back. Parent #16 remains OPEN.

On 2026-10-06 the complete 10/10-task change was synchronously synced and archived.
The new main `runtime-enforcement-preflight` specification retains the accepted
Purpose and all five requirements/six scenarios without changing normative
content. `.openspec.yaml` is retained in the archive. Main-spec strict validation
passed 5/5 after sync. Archive instructions had no optional context/guidance;
the valid specs-instruction snapshot had no artifact rules.

The synchronization/archive and acceptance record entered main through
[PR #41](https://github.com/AllenMuu/agent-manager/pull/41), merge commit
`27697c56d1754631da1dfe2724b24431d5313019`, on 2026-10-06.
They add no Go code or live enforcement evidence. The earlier prepublication
section is retained as the historical review snapshot, not current delivery state.
