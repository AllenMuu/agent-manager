# Structured local Memory delivery evidence

## Baseline and selected boundaries (task 1.1)

Ticket [#20](https://github.com/AllenMuu/agent-manager/issues/20) has five acceptance requirements and seven scenarios. Native blocker [#19](https://github.com/AllenMuu/agent-manager/issues/19) was re-read and verified CLOSED on 2026-10-02. Main-delivered baseline is `7240f50caea7afdffbefb003548775127e7597cb` (PR #32 and PR #33 merged). The controller verified baseline `go test ./...` before implementation. This branch preserves prerequisite M1 spec sync/archive commit `24b8374159955744d49719835ad3ce35784bd389`; its documentation remains part of the independent review scope.

The user-authorized boundaries are `OpenStructuredStore`, existing `Remember/Get/Recall/Capabilities/Health`, optional conditional `Update/Supersede/Forget/History`, explicitly confirmed `ImportLegacy`, and the external filesystem durability/persistence seam. All new behavior tests are external `package memory_test` tests using these public boundaries. No private-helper assertions or internal collaborator mocks are used. TDD was performed one implementation slice at a time; historical red logs and final green evidence remain outside the repository at `/tmp/agent-manager-issue20-evidence`.

## Acceptance matrix (tasks 2.1–2.5)

All named tests passed in the uncached final behavior run (`final-behaviors.log`).

| Task / requirement | Scenario | Public behavior test | Actual outcome |
| --- | --- | --- | --- |
| 2.1 Durable owner and provenance | Reopen owned record | `TestStructuredStoreReopenOwnedRecord` | Same ID, owner, source, evidence, state, layer and version after reopen; another project gets `ErrNotFound`. |
| 2.2 Conditional serialized updates | Competing version updates | `TestStructuredStoreCompetingProcesses`, `TestStructuredStoreCompetingInstances` | Two separate processes or instances attempting version 1 yield exactly one version 2 and one conflict; unrelated records remain readable. |
| 2.3 Atomic lifecycle and history | Supersede and forget | `TestStructuredStoreSupersedeForgetHistory`, `TestStructuredStoreLifecycleVersionsAndRetries` | New neutral replacement ID, bidirectional lineage, SUPERSEDED old record and DELETED replacement retained in owner-bound version history; current recall excludes both; stale retirement requests conflict. |
| 2.3 Atomic lifecycle and history | Supersession fails before commit | `TestStructuredStoreSupersessionFailsBeforeCommit` | Injected temporary-file sync failure reports `ErrNotCommitted`; reopening returns only the unchanged ACTIVE original and its single history version. |
| 2.4 Recoverable local writes | Failure and retry evidence | `TestStructuredStoreFailureRetryEvidence`, `TestStructuredStoreRetryMustConfirmDurability`, `TestStructuredStoreLifecycleVersionsAndRetries` | Rename followed by injected directory-sync failure reports `ErrOutcomeUnknown`; reopened identical operation ID returns its original result without another increment. Different content/owner intent conflicts. Retried receipts must confirm durability before success. Create, supersede and forget receipts also retain their original results. |
| 2.5 Explicit legacy import | No implicit conversion | `TestStructuredStoreNoImplicitConversion` | Open, health and reopen leave legacy bytes unchanged and recall empty; a text file cannot serve as the structured root. |
| 2.5 Explicit legacy import | Confirmed import | `TestStructuredStoreConfirmedLegacyImport`, `TestStructuredStoreLegacyImportAtomicFailure` | Separate exact owner/source confirmation is mandatory. Nonblank legacy lines become inert records with declared source and no invented evidence. Failed batch preparation imports nothing; retry imports the whole batch once. Legacy bytes remain unchanged. |

Additional passing coverage: `TestStructuredStoreCancellableProcessLock` acquires a real lock in a subprocess and verifies deadline cancellation in a competing caller; `TestStructuredStoreGuardsFileAndDirectoryIdentity` rejects direct root/state/lock symlinks and a replaced root directory; `TestStructuredStoreRejectsForeignFormat` rejects unrelated JSON. These are real temporary-directory/subprocess tests. Sync failures are deliberately injected at the external filesystem boundary while writing and renaming actual files. They are not real-service, remote-provider, power-loss, or hardware fault experiments.

Historical TDD evidence: `2.1-red.log` → `2.1-green.log`, `2.2-red.log` → `2.2-green.log`, `2.3-lifecycle-red.log` → `2.3-lifecycle-green.log`, `2.3-failure-red.log` → `2.3-failure-green.log`, `2.4-red.log` → `2.4-green.log`, `2.5-red.log` → `2.5-green.log`. The import red targets the missing explicit import API; no-implicit-conversion is a regression assertion of the already implemented explicit constructor. Self-review regression evidence: `guard-format-red.log` → `guard-format-green.log` and `retry-confirm-red.log` → `retry-confirm-green.log`. `final-behaviors.log` validates the final code, superseding earlier green snapshots.

## Public API and semantics

```go
OpenStructuredStore(root string, options ...StoreOption) (*StructuredStore, error)
(*StructuredStore).Remember(context.Context, NewRecord) (Record, error)
(*StructuredStore).RememberWithOperation(context.Context, RememberRequest) (Record, error)
(*StructuredStore).Get(context.Context, Owner, RecordID) (Record, error)
(*StructuredStore).Recall(context.Context, Query) ([]Record, error)
(*StructuredStore).Update(context.Context, UpdateRequest) (Record, error)
(*StructuredStore).Supersede(context.Context, UpdateRequest) (Record, error)
(*StructuredStore).Forget(context.Context, MutationRequest) (Record, error)
(*StructuredStore).History(context.Context, Owner, RecordID) ([]Record, error)
(*StructuredStore).ImportLegacy(context.Context, LegacyImportRequest, ImportConfirmation) ([]Record, error)
(*StructuredStore).Capabilities() StructuredCapabilities
(*StructuredStore).Health(context.Context) (HealthStatus, error)
```

Optional interfaces and package dispatch functions expose these operations separately from legacy `Provider/Promote/Searcher`. `UpdateRequest` contains `Owner`, `ID`, mandatory nonzero `ExpectedVersion`, full replacement `Record NewRecord`, and optional `OperationID`. `MutationRequest` contains owner, ID, expected version and operation ID. `RememberRequest` combines `Record NewRecord` and operation ID. Ownership cannot be transferred by update or supersede. `History` returns versions of one exact owned ID in ascending version order, including lifecycle states and replacement links; callers may inspect the linked ID separately.

`Update`/`Supersede` capability flags describe support; `ConditionalUpdate`/`AtomicSupersede` separately advertise the stronger local guarantees. Local dispatch checks both flags. `History` and `ImportLegacy` describe their corresponding optional operations. Future providers may advertise basic update support without claiming CAS semantics; this slice adds no remote update boundary.

All mutations accept caller-supplied operation IDs, generated when omitted. Callers requiring reliable retry must supply and retain an ID. Committed receipts fingerprint the complete intent (operation kind, ownership, content, metadata, target ID and expected version); matching committed receipts are checked before stale-version checks and return the original result. Different intent under the same ID returns `ErrConflict`. Records, version history and receipts are committed in a single canonical JSON batch.

`LegacyImportRequest` contains `Path`, `Owner`, `Source`, `Type`, optional `Layer` and `OperationID`. A separate `ImportConfirmation` must have `Confirmed: true` and the exact proposed owner/source. Import converts each nonblank line into inert content and adds no historical evidence. Owner partitions do not authenticate callers; authorized Gateway enforcement remains the next slice.

The store root must already exist as a direct directory. Opening and health are read-only. No legacy configuration/default or text-provider implementation is changed. Filesystem operations use anchored directory descriptors, direct regular files and no-follow opens. A persistent `memory.lock` inode remains independent of atomically replaced `memory.json`; the full read/modify/replace transaction is locked and acquisition is cancellable. Atomic persistence writes a private temporary file, syncs it, renames it, then syncs the directory before reporting success. `ErrNotCommitted` classifies persistence failures before rename; `ErrOutcomeUnknown` classifies post-rename durability uncertainty. A receipt retry flushes the directory before reporting success.

`WithPersistence(StorePersistence)` selects the external persistence seam. `StorePersistence.Commit(context.Context, *os.File, []byte) error` must atomically commit through the anchored directory; `Confirm(context.Context, *os.File) error` must establish durability for a recognized receipt without repeating the mutation. `AtomicFilePersistence{Syncer: FileSyncer}` is the real local implementation; `FileSyncer.Sync(*os.File) error` defaults to `os.File.Sync` and enables filesystem failure injection. Custom implementations must preserve the documented outcome and no-follow semantics.

Supported OS-lock/anchored-filesystem implementations: Darwin, DragonFly BSD, FreeBSD, Linux, NetBSD and OpenBSD. Other platforms explicitly return an error matching `ErrUnsupported`; they do not claim process-safe storage. Actual runtime validation in this delivery is on Darwin; Windows compilation checks the honest unsupported implementation, not Windows storage execution. The store rewrites a full batch and retains tombstones/history/receipts; no automatic compaction, conversion, extraction or execution is added.

## Initial implementation verification and gates (task 3.1)

This table records the `3616a46` implementation baseline. The ordinary-review repair evidence below supersedes it for the current code.

| Check | Result | External evidence |
| --- | --- | --- |
| `go test ./...` | PASS | `go-test.log` |
| `go test -race ./internal/memory/...` | PASS, including real subprocess competition | `go-race.log` |
| `go vet ./...` | PASS | `go-vet.log` |
| `go build -o /tmp/agent-manager-issue20 ./cmd/agent-manager` | PASS | `go-build.log` |
| `go test -count=1 -v ./internal/memory -run TestStructuredStore` | PASS, 14 top-level behavior tests | `final-behaviors.log` |
| `GOOS=windows GOARCH=amd64 go test -c -o /tmp/agent-manager-memory-windows.test.exe ./internal/memory` | PASS, compilation only | `windows-compile.log` |
| `openspec validate add-structured-local-memory-store --strict` | PASS | `openspec-validate.log` |
| `openspec doctor` | PASS | `openspec-doctor.log` |
| `git diff --check` and staged diff check | PASS | `diff-check.log` |

Self-review checked correctness, completeness and public-seam test focus. It fixed acceptance of foreign empty JSON and retry success before durability confirmation, each with an observed failing test and a passing final test. No network provider, dependency installation, skill/task execution, or real external service is used.

Tasks 3.2 and 3.3 remain pending for independent specification review, code-quality review and the controller's final gpt-6.1-sol/high read-only OCR covering the entire branch, including prerequisite M1 documentation. No push, PR creation, merge or M2 archival is performed by the implementation agent.

## Ordinary specification review repair

The independent specification review of `7240f50` → `3616a46` found one reproducible P2: Go strings containing invalid UTF-8 were accepted and JSON silently replaced their bytes with U+FFFD. This altered canonical content/source/evidence and exact owner partitions, and distinct operation intents could share a fingerprint. The public review report and reproduction are `/tmp/agent-manager-issue20-spec-review.md` and `/tmp/agent-manager-issue20-spec-overlay.log`; those intentionally failing overlay probes describe the pre-fix state.

The repair rejects non-UTF-8 canonical record content/source/evidence and owner/token/operation identifiers with `ErrInvalidInput`, before persistence or receipt lookup. Mutation owner/record-ID validation precedes fingerprinting so an invalid owner cannot replay another owner's normalized receipt. Existing knowledge-type/layer/owner-kind allowlists already reject invalid enum strings. Structured validation remains separate from legacy text-provider token rules and does not change legacy configuration/defaults. Confirmed import rejects non-UTF-8 proposal paths and raw file bytes before fingerprinting or creating any batch record. Raw structured JSON bytes must be valid UTF-8 before decoding; corrupt state returns `ErrUnavailable` and is left unchanged. Valid Unicode, including Chinese owner identifiers, sources/evidence and emoji content, continues to round-trip and retry exactly.

| Behavior / affected task | Public regression test | Red / green evidence under `review-utf8/` |
| --- | --- | --- |
| Exact metadata and distinct raw intent / 2.1, 2.4 | `TestStructuredStoreRejectsNonUTF8RecordMetadata` | `metadata-red.log` → `metadata-green.log` |
| Exact owner/record/operation identifiers / 2.1, 2.4 | `TestStructuredStoreRejectsNonUTF8Tokens` | `tokens-red.log` → `tokens-green.log` |
| Invalid owner must not replay a receipt / 2.1, 2.4 | `TestStructuredStoreInvalidOwnerCannotReplayReceipt` | `owner-replay-red.log` → `owner-replay-green.log` |
| Whole confirmed import rejected without partial records / 2.5 | `TestStructuredStoreRejectsNonUTF8LegacyImport` | `import-red.log` → `import-green.log` |
| Corrupt persisted bytes must not become trusted canonical state / 2.1, 2.4 | `TestStructuredStoreRejectsNonUTF8PersistedState` | `state-red.log` → `state-green.log` |

Permanent `TestStructuredStorePortableContract` now runs `memorytest.Run` against fresh real local stores. Its coverage is PROJECT 10 knowledge types × 4 layers, source/two evidence references, create/get/recall, two-project isolation, stable case-insensitive recall, RAW/no-invented-provenance defaults, and USER canceled/invalid writes. It does not claim every owner kind or standalone caller-mutation coverage. `TestStructuredStoreValidUnicodeRoundTrip` and `TestStructuredStoreInvalidMetadataMutationsLeaveCurrentRecord` add passing regression checks for valid Unicode, rejected Update/Supersede mutations, and reuse of an operation ID after rejection. These supplement the original seven M2 scenarios without modifying the approved capability scope.

Final repair evidence lives at `/tmp/agent-manager-issue20-evidence/review-utf8`: `final-regressions.log`, `final-behaviors.log`, `go-test.log`, `go-race.log`, `go-vet.log`, `go-build.log`, `windows-compile.log`, `openspec-validate.log`, `openspec-doctor.log`, and `diff-check.log`. Full tests, memory race checks, vet, CLI build, Windows compilation, strict validation, doctor and diff checks were rerun after the repair. All passed. The uncached final behavior run contains 22 top-level store tests (14 original and eight additional regressions/contract checks). The roadmap's M2 status now records branch implementation/verification 7/9 and ongoing independent review, without claiming main delivery. Independent review acceptance (3.2) and final OCR (3.3) remain pending for controller verification.

## Ordinary code-quality review repair

The independent ordinary quality review of `7240f50` → `3e82787` found two Important boundary defects and one Minor ADR link defect (`/tmp/agent-manager-issue20-quality-review.md`). The four public probes at `/tmp/agent-manager-issue20-quality-overlay.log` reproduce the pre-fix state: importing the store's own `memory.json` rewrote its source and broke exact retries; unpaired JSON surrogate escapes were normalized by Go's decoder into trusted content/owner labels.

Confirmed import now rejects a source whose anchored parent is the store directory and whose name is the canonical destination, including resolved ancestor aliases. It also compares directly opened source/canonical file identities, rejecting case-insensitive filename aliases and conservatively rejecting any hardlink to the current canonical inode, even outside the store root. This reservation prevents treating live canonical storage as legacy text; distinct legacy files remain importable and their exact requests retry without rewriting source bytes. Identity checks repeat inside the locked transaction before creating records or receipts, avoiding a cooperating writer changing the canonical inode during that check. Rejected sources return `ErrInvalidInput`, retain both source/store bytes and create no import result or receipt. The Darwin case-insensitive alias was actually reproduced; the test only exercises that alias where the filesystem identifies it as the canonical file. Hardlinks are tested directly.

Before decoding canonical JSON, a small additional string-escape scan uses the standard library's JSON syntax validation and rejects unpaired high/low UTF-16 surrogate escapes. It does not replace the JSON parser or normalize/encode canonical input. Corruption returns `ErrUnavailable`, including malformed escapes in content, owners, record IDs and object keys, and preserves the stored bytes. Proper surrogate pairs, ordinary escaped Unicode, Chinese/emoji, literal U+FFFD and inert literal backslash-u strings remain accepted exactly. Prior raw-invalid-UTF-8 rejection remains in place. This is scalar-integrity validation, not authentication or detection of arbitrary semantically valid tampering.

| Boundary / affected task | Public regression | Actual evidence in `review-quality/` |
| --- | --- | --- |
| Import source is canonical destination or ancestor alias / 2.5 | `TestStructuredStoreRejectsImportFromCanonicalDestination` | `import-overlap-red.log` → `import-overlap-green.log`; repeated rejection preserves bytes/records, and the rejected operation ID remains usable for a normal distinct-file import/retry. |
| Canonical case/hardlink aliases / 2.5 | `TestStructuredStoreRejectsCanonicalDestinationFileAliases` | `import-file-alias-red.log` → `import-file-alias-green.log`; real Darwin case alias reproduced, both alias types rejected. |
| Unpaired stored Unicode escapes / 2.1, 2.4 | `TestStructuredStoreRejectsUnpairedJSONSurrogates` | `json-surrogates-red.log` → `json-surrogates-green.log`; high, low, high-followed-by-high, normalized owner and record/object-key cases rejected without rewriting. |
| Legitimate JSON Unicode scalars / 2.1 | `TestStructuredStorePreservesValidJSONUnicodeEscapes` | `final-boundaries.log`; valid upper-case surrogate pair and ordinary BMP escapes reopen to the exact original record, including Chinese, emoji, U+FFFD and literal backslash-u content. |

ADR 0006 now links to the accepted main `openspec/specs/scoped-memory-contracts/spec.md`, whose target exists; the old active-change link was the sole remaining stale reference in ADR/CONTEXT/README/main-spec search.

Final quality-repair evidence: `/tmp/agent-manager-issue20-evidence/review-quality/{final-boundaries,final-behaviors,go-test,go-race,go-vet,go-build,windows-compile,openspec-validate,openspec-doctor,diff-check}.log`. All required checks passed after this repair. The uncached final behavior run has 26 top-level store tests (22 preceding this review plus four new boundary tests); subtests are not counted as additional top-level tests. Windows remains compilation-only; runtime/subprocess evidence is Darwin, with actual local files and injected filesystem sync faults, not external services or hardware/power-loss tests. The previous UTF-8 evidence describes its reviewed baseline; these logs supersede it for the current implementation.

Independent ordinary review acceptance (3.2) and final read-only OCR (3.3) remain pending for controller verification. No publication, merge, issue closure or M2 archival is claimed.

## Controller review acceptance (tasks 3.2 and 3.3)

Ordinary specification re-review PASS: `/tmp/agent-manager-issue20-spec-final.md`. Ordinary code-quality re-review PASS: `/tmp/agent-manager-issue20-quality-rereview.md`. Both cover immutable code head `9771451994db1a638cf27b6224362b6512ba1258`, retaining complete PR context from `7240f50caea7afdffbefb003548775127e7597cb`. Prior actionable findings are resolved with public regressions; ordinary quality additionally ran eleven independent Unicode scalar cases.

Required independent final `open-code-review-delegate` gate used `gpt-6.1-sol/high`, actual OCR v1.12.11 JSON preview/rules, and exact merge base `7240f50caea7afdffbefb003548775127e7597cb`. PASS with no actionable findings: included 12/12 reviewed, excluded 14/14 manually reviewed, aggregate 26/26 changed entries, zero skipped, 100% aggregate coverage. Report and exact identity checklist: `/tmp/agent-manager-issue20-final-ocr/{report.md,checklist.json,checklist.md}`. Excluded tests, docs, archived M1 artifacts and accepted main spec were actually reviewed. Three independently executed public overlay groups passed: all six owner variants and evidence-copy isolation; complete-intent/operation-kind receipt conflicts; empty import and changed-source fingerprinting.

All nine implementation checklist tasks are accepted on this branch. This final gate applies to the immutable code head above; this controller-only task/delivery/roadmap metadata update receives a separate incremental read-only review before publication. No new product behavior or test change is included in that metadata. PR creation, authorized merge, issue closure and postmerge M2 spec sync/archive remain separate delivery states; none is claimed here.
