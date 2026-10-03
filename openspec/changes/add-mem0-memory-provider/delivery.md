# Issue #22 implementation evidence

Current status: implemented, validated and independently accepted, 10/10 tasks.
Source HEAD `f07485fdb3d77b3d942a519cb9dea9661b1ddd2a`, integrated base
`6fefae2bf02a4232b7c21d4475b3513bf9ef1401`. Publication metadata receives
separate supplemental review; no PR, merge or Issue closure is claimed here.

Scope: branch `feature/issue22-mem0-memory-provider`, starting HEAD `28b8875`,
main prerequisite `ae1745ac50e4ab6e645d7ef07bad66c61c2d8635`. Native blocker #19
is CLOSED; its canonical contracts are present on main. M3 PR #36 is merged.
Baseline `go test ./... -count=1` passed on 2026-10-03.

Under the user's approved roadmap authorization, the controller selected public
seams as routine implementation choices: configured provider construction,
canonical Remember/Get/Recall/scored recall, explicit weaker Replace/Remove,
and safe capability/status/error output. This does not claim human approval of
individual Go names. Existing conditional Gateway operations remain unsupported
for Mem0; no operation-receipt guarantees are invented.

Public fixture tests exercise an HTTP server matching the pinned OSS REST
contract. Live evidence will separately exercise the Go adapter against Python
Mem0 2.2.1, commit 94c3fe9f238f3dbf29c9ce98643bd71eb13077cd. API schema 1.0.0
is not proof of the installed Python version. Runtime auto-detection is unknown.

Implementation is branch-only. No PR, merge or Issue closure is claimed here.
Ordinary independent review and final read-only OCR are pending root execution.

## Literal requirement/scenario verification

| Requirement | Literal scenario | Observable evidence |
| --- | --- | --- |
| Canonical remote operation mapping | Remote canonical round trip | `TestCanonicalCreateGet`, `TestRemoteCanonicalRoundTrip`, `TestExplicitLiveSmoke`: neutral ID retained, typed project owner/source/evidence retained, version 1 → 2; removal absent from ordinary recall. |
| Honest remote semantic capabilities | Backend cannot atomically supersede | `TestUnsupportedStrongOperationsBeforeAnyRequest`: no conditional update/forget, atomic supersede, history or receipts; strong dispatch rejects before any request. Basic Replace/Remove have distinct capability flags. |
| Bounded transport and uncertain writes | Write response times out | `TestLostWriteIsUnknownAndNeverReplayed`: one accepted/requested write, outcome unknown, no replay. Bounded/canceled resolver and read body have independent tests. |
| Bounded transport and uncertain writes | Authentication failure | `TestAuthenticationFailureKeepsCredentialAtTransport`, `TestMem0CLIStatusKeepsAuthenticationFailureSafe`: lazy resolver, authentication failure instead of empty success, safe CLI category. |
| Opaque secret references | Authenticated request diagnostics | Authentication/CLI tests and `TestNonSecretFileConfigurationAndJournalEvidence`: keys absent from rendered errors/status, reference names absent from journal evidence; no key is injected into canonical metadata. |
| Offline contract and opt-in live evidence | Ordinary offline test run | `TestPinnedOfflineContractFixtures`, round-trip HTTP fixtures and corruption cases: `httptest` only, no download/install/service startup. Normal live harness is skipped. |
| Offline contract and opt-in live evidence | Explicit live smoke | Opt-in `TestExplicitLiveSmoke` executes Go canonical adapter on real OSS server; separately saved `live-runtime-proof.json`, `live-smoke.json`, `live-smoke.log` in external task evidence. |

All five requirements/seven named scenarios are accounted for. Further negative
fixtures cover owner kinds, ambiguous IDs, uint64 precision, unsupported enums,
Unicode corruption, invalid/oversized responses, auth redirect prevention, and
GET-by-ID service failure. Search scores are server-provided and bounded; no
full enumeration, conditional concurrency or canonical lineage claim is made.

## Real service environment and proof

The installed `mem0ai` distribution reports 2.2.1; unmodified official source
reports commit 94c3fe9f238f3dbf29c9ce98643bd71eb13077cd; Python reports 3.12.13.
These independent preflight observations are operator proof, distinct from
adapter runtime version auto-detection (unknown). The task-owned official
uvicorn app binds loopback 18768 and pgvector/pg17 binds loopback 19932, using
image sha256:dff87d579b83320e619aa18104acf45258465d2e5f5078a61ce8416642b50a86.
FastEmbed uses cached local BAAI/bge-small-en-v1.5, 384 dimensions (Qdrant model
commit aa8f8b060edb00e03bfdd08813a2949946c8ba55); literal `infer:false` avoids
LLM extraction. SDK state/model cache and private 0600 credentials remain under
the external preparation directory; no global Docker or agent settings changed.


## Final branch verification (2026-10-03)

All checks below returned exit 0 on the final source tree:

- `go test ./... -count=1` (12.935 s)
- `go test -race ./internal/memory/... ./internal/memoryprovider ./internal/cli ./internal/taskcontext -count=1` (7.744 s)
- `go vet ./...`
- `go build -o /tmp/agent-manager-issue22-evidence/agent-manager ./cmd/agent-manager`
- `openspec validate add-mem0-memory-provider --strict`
- `openspec doctor`
- `git diff --check`
- Explicit `TestExplicitLiveSmoke` rerun on final source (0.22 s test body; PASS).

External logs and exact commands/exit codes are retained under
`/tmp/agent-manager-issue22-evidence/check-exit-status.json` and named check logs.
The two exact timestamped CLI evaluation artifacts created by the test/race runs
were inspected and removed individually; no unrelated paths were deleted.
The task-owned uvicorn service was terminated and loopback 18768 has no listener;
`agent-manager-mem0-acceptance-db` is stopped. Cleanup handles/results are saved
in `cleanup.json`; the prepared private environment/cache remain reusable for
later authorized work. No global process/configuration was changed.

Tasks are 8/10 complete: implementation and verification are complete on this
branch, ordinary independent review and final OCR remain pending. No main
acceptance, PR publication or Issue closure is inferred from this checklist.

## Ordinary specification review F1 repair

Independent specification review at `290db940eeb1ece927d82a8e17664321cbf577a9`
returned REQUEST CHANGES for one Low configuration-policy mismatch: unknown
nested fields in `secretReference`, including synthetic `apiKey`, were silently
ignored. No credential disclosure was observed or claimed. The historical report
is retained at `/tmp/agent-manager-issue22-spec-review.md`.

The external Mem0 factory decoder now rejects unknown nested fields and trailing
input, while preserving canonical Unicode checks, exact top-level field policy
and existing documented opaque references. This is confined to external factory
configuration; global `memory.ConfigReference` compatibility and remote adapter
operations are unchanged. Public regression
`TestExternalMem0ConfigurationRejectsNestedUnknownFieldsBeforeAccess` reproduced
both nested raw-key and unrelated-field acceptance (RED), then passed (GREEN),
including valid documented-reference compatibility, trailing-input rejection and
zero service/auth/network requests during invalid construction.

Repair evidence is retained separately under
`/tmp/agent-manager-issue22-evidence/spec-repair/`. The remote provider and live
harness source are byte-identical to the original live-validated commit; no live
service was restarted for this factory-only decoder repair. Existing actual
Go-adapter live smoke evidence remains applicable to that unchanged adapter;
ordinary specification re-review, quality review and final OCR remain pending.

After the final factory edit, affected factory/CLI tests, full
`go test ./... -count=1`, relevant Memory/factory/CLI/task-context race checks,
vet, CLI build, strict OpenSpec validation, doctor and diff checks all returned
exit 0. Exact commands/logs are in `spec-repair/check-exit-status.json`. The three
specific evaluation artifacts created by affected/full/race checks were inspected
and removed individually. Task-owned Mem0 service/container remain stopped.
Tasks 3.2/3.3 remain pending root review; no publication or merge is claimed.

## Ordinary quality review F1 repair

Independent quality review at `79b888467220000b6d191b2e64b0556affeacad8`
returned REQUEST CHANGES for one Medium lifecycle-preview dispatch defect:
unsupported Mem0 update/supersede/forget looked up targets through remote health
before checking strong operation support, allowing auth/unavailable errors to
mask unsupported. The historical report/probe remain unchanged at
`/tmp/agent-manager-issue22-quality-review.md` and its external probe directory.
No mutation or credential disclosure was observed.

A shared pure strong lifecycle predicate now intersects the actual optional
interface with its declared operation and exact guarantee flags. Update requires
ConditionalUpdate; supersede requires AtomicSupersede; forget requires its
conditional RecordForgetter contract. Preview applies this after owner/input/
operation-ID validation and before target Get/Health, using the same predicate
as commit dispatch. Add/import draft-plan compatibility is unchanged; Mem0
basic Replace/Remove are neither downgraded nor routed through the Gateway.

Public RED→GREEN tests reproduce all three CLI operations against 401 and 503
endpoints and now assert unsupported, zero requests and no confirmation plan.
Direct exported Preview tests cover declarations without interfaces, false
operation declarations, missing CAS/atomic guarantees, and authorization/input
validation before capability access. The original independent CLI overlay probe
now passes. Local exact confirmation, lifecycle/history, fresh uncertain receipt
reconciliation and effective-flag plans pass their focused compatibility tests.
This extends the honest semantic capability scenario with actual CLI preview
behavior, not only direct strong dispatch.

Evidence is separate under `/tmp/agent-manager-issue22-evidence/quality-repair/`.
The remote adapter and live harness remain byte-identical to the original
real-service-validated source; no wire behavior changed or live service restarted.
The task service/container remain stopped. Ordinary spec re-review, quality
re-review and prescribed final read-only OCR remain pending root execution;
tasks 3.2/3.3 are not marked complete and no delivery to main is claimed.

After the final source edit, full `go test ./... -count=1`, related
Memory/factory/CLI/task-context race tests, vet, CLI build, strict OpenSpec
validation, doctor and diff checks all returned exit 0. Final focused preview/
dispatch parity tests and the original reviewer probe passed. Exact commands,
exit codes and logs are in `quality-repair/check-exit-status.json`; two generated
test/race evaluation artifacts were inspected and removed individually. No code
changed after these final checks. Existing original real smoke is retained as
executed evidence for the unchanged remote wire implementation, not a new live
run of the repaired Gateway.

## Final OCR F1 STOP and explicit authorized repair

The prescribed final gpt-6.1-sol/high read-only OCR at
`302a548e1cf7dbcecaed9ae5eb7173faa8c9f867` reviewed all 31 entries and returned
STOP for one actionable Low authentication diagnostic projection gap. No repair
or publication was performed in that review. Its original report/probe/log remain
unchanged at `/tmp/agent-manager-issue22-final-ocr/`.

The user subsequently explicitly authorized repair with “帮我修复”. Before this
repair, the controller refreshed and cleanly merged latest main
`6fefae2bf02a4232b7c21d4475b3513bf9ef1401` (PR #37) into the branch, producing
clean merge HEAD `e7f3012e239b7666cb57c8e4aab8dd7a361805fb`. This new integration
base does not rewrite the historical OCR range or accepted M3 archive history.

`DiagnosticCategory` now explicitly returns `authentication` when SafeError
preserves ErrAuthentication. Public direct/wrapped-error tests and the real Mem0
adapter + httptest401 injected into exported taskcontext.ResolveWithOptions
reproduced the previous `unavailable` projection (RED), then passed (GREEN).
The task-context regression retains intent artifacts, repository guidance and
selected Skills, checks safe serialized output, and verifies exactly one
transport credential resolution/request without credential disclosure. The
original independent public OCR overlay probe now passes unchanged. Existing
canceled/unavailable/unsupported/ownership/conflict/invalid/receipt categories
remain unchanged. This is an embedding task-context diagnostic fix; automatic
CLI role selection is still structured-local and no native agent/M5 reachability
is claimed.

New repair evidence is separately retained under
`/tmp/agent-manager-issue22-evidence/ocr-repair/`. The remote adapter and live
harness wire source remain identical to the actual live-validated original
commit; the historical Go smoke applies to those unchanged operations, not a
fresh whole-head live run. No service/container restart is necessary or claimed.
Tasks remain 8/10, with ordinary spec then quality re-review and fresh prescribed
final read-only OCR pending controller execution. No publication, main delivery
or Issue closure is inferred from the authorized repair.

After final source edits on the refreshed-main integration, affected Memory/
task-context suites, full `go test ./... -count=1` (including PR #37 integration
coverage), Memory/factory/CLI/task-context races, vet, CLI build, strict OpenSpec
validation, doctor, working-tree diff and complete-PR diff against new main all
returned exit 0. Exact commands/exit codes/logs are in
`ocr-repair/check-exit-status.json`. The two exact generated CLI evaluation
artifacts were inspected and removed individually. No source changed after these
checks. Current source identity/clean HEAD/base/complete scope and stopped backend
observations are retained in `ocr-repair/manifest.json` for fresh review.

## Repaired-source independent acceptance

At source HEAD `f07485fdb3d77b3d942a519cb9dea9661b1ddd2a` on refreshed base
`6fefae2bf02a4232b7c21d4475b3513bf9ef1401`, the required sequence completed:

- Specification PASS: `/tmp/agent-manager-issue22-spec-ocr-repair-review.md`,
  33/33 entries and all five requirements / seven scenarios.
- Quality PASS: `/tmp/agent-manager-issue22-quality-ocr-repair-review.md`,
  33/33 entries; original authentication probe and compatibility regressions pass.
- Prescribed independent `gpt-6.1-sol` / `high` read-only OCR PASS:
  `/tmp/agent-manager-issue22-final-ocr-rereview/report.md`, 33/33 entries
  (11 selected, 22 manually supplemented), zero skipped and no actionable finding.
  The original STOP and subsequent explicit repair authorization remain preserved.

Tasks 3.2/3.3 now record those completed source gates. Earlier pending-task
statements above describe their historical heads, not current acceptance.
This three-file acceptance-metadata update and proposed PR body receive a
supplemental read-only gate before publication. Go source is unchanged from the
validated and reviewed source head. Mem0 wire/live-harness evidence retains its
precise original tested-source scope. Parent #7 remains open for M5.
