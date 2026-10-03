# Issue #22 implementation evidence

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
