# Issue #19 implementation evidence

Baseline: `a98adf6933f35141fe7c4d468a3963ee0bd9aad7` (main after PR #30 and #31).
Branch: `feature/issue19-scoped-memory-contracts`; existing isolated worktree:
`/Users/allenj/.codex/worktrees/b9dc/agent-manager`.
Issue #19 has no native blockers and is assigned to AllenMuu; #7 remains open.

The user instructed “帮我合并PR，然后继续后续#19” after being shown the pending
#19 test-boundary confirmation. This continues the proposed Remember/Get/Recall
and Capabilities/Health boundaries: owned canonical round trips, wrong-owner
rejection, cancellation and unsupported versus unavailable outcomes. Existing
Promote/resource tests remain compatibility evidence; no private-helper assertions
are added. New tests use `memory_test` and exercise public provider boundaries.

Baseline `go test ./...` passed. ADR 0006 records canonical/provider separation,
inert SKILL/TASK content, no implicit conversion/network/install and unchanged
legacy Provider/Promote/Searcher compatibility before implementation.

## Requirement / scenario evidence

| Task | Requirement / scenario | Public evidence | Result |
| --- | --- | --- | --- |
| 2.1 | Missing ownership identifiers | `TestStructuredMemoryRejectsIncompleteOwners`, `TestStructuredMemoryRejectsInvalidRecordsWithoutStoringThem`; unknown/GLOBAL/ambiguous owners rejected | Passed |
| 2.1 | Two-project isolation | `TestStructuredMemoryRoundTripAndProjectIsolation`, `TestStructuredMemoryAllOwnerPartitionsAreExact`; exact USER/PROJECT/AGENT/SESSION partitions, safe not-found on cross-owner Get | Passed |
| 2.2 | Remember and recall project knowledge | `TestStructuredMemoryRoundTripAndProjectIsolation`; literal CONSTRAINT/ATOMIC/source/two evidence references/neutral ID/version one/ACTIVE round trip | Passed |
| 2.2 | Metadata integrity | `TestStructuredMemoryRetainsMetadataDespiteCallerMutation`; input, create/get/recall result changes cannot rewrite stored evidence | Passed |
| 2.3 | Unsupported recall versus unavailable storage | `TestStructuredMemoryDistinguishesUnsupportedFromUnavailable`; discovery-only external boundary returns unsupported, uninitialized supported provider returns unavailable, health separate from capabilities | Passed |
| 2.3 | Canceled operation | `TestStructuredMemoryCanceledWriteStoresNothing`; errors.Is detects canceled and context.Canceled, later recall is empty | Passed |
| 2.4 | Portable contract suite | `TestInMemoryProviderContract` / `memorytest.Run`; all 10 knowledge types and 4 layers, two projects, repeated stable owned recall, canceled/invalid writes | Passed |
| 2.4 | Concurrent writes | `TestStructuredMemoryConcurrentWritesRetainEveryRecord`; 24 records retained with unique IDs; related race check | Passed |
| 2.5 | Legacy promotion remains available | Existing `TestFileProviderPromoteAppendsExplicitKnowledge`, `TestPromoteLessonsRequiresExplicitConfirmation`, lifecycle/resource/CLI suite | Passed |

## Red / green and implementation boundary

Sequential cycles observed missing-public-contract compile failures first, then
actual metadata-aliasing, canceled-write, invalid-record, concurrent data race and
empty-ID failures before each corresponding implementation. Original red/green
logs are outside the repository at `/tmp/agent-manager-issue19-evidence`.
The reusable suite validates already implemented behavior rather than bulk-writing
imagined behavior before implementation.

Storage is process-local and deterministic, with context-aware create/get/recall,
copy-safe evidence, exact partitions and serialized mutations. IDs are opaque and
instance-local, version starts at one, state ACTIVE and default layer RAW. Recall
uses case-insensitive content substring matching in creation order for this
implementation. No durable production store, authenticated Gateway, Mem0, CLI
rewrite, automatic extraction/import or execution of SKILL/TASK content is delivered.
Update/forget/supersede are unsupported capability declarations, not implemented
mutations. Legacy Provider/Promote/Searcher and configuration are unchanged.

## Verification

The optional openspec-verify-change skill is not installed. The matrix above is
an explicit requirement/scenario/code/test evidence check, separate from CLI
structural validation. Completed checks:

- `go test ./...`
- `go test -race ./internal/memory/... ./internal/taskcontext ./internal/lifecycle ./internal/governance`
- `go vet ./...`
- `go build -o /tmp/agent-manager-issue19 ./cmd/agent-manager`
- `openspec validate define-scoped-memory-contracts --strict`
- `openspec doctor`
- `git diff --check`

Ordinary independent review found a portability issue: the reusable contract
suite required insertion order beyond the specified deterministic ordering.
It now checks the complete record set and repeatable order. The reviewer reran
its stable reverse-order provider overlay successfully and reported no remaining
actionable findings. Design clarifies future mutation capabilities and provider-
instance IDs. Memory tests/race passed again after these corrections.

All evidence is offline in-memory or existing local fixtures; none is a real
external Memory integration claim. Final read-only OCR and PR publication remain
pending; merge/sync/archive will follow only accepted authorized delivery. #7
remains open and all other Memory ticket implementation tasks remain pending.
