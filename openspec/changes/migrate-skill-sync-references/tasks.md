## 1. Contracts and architecture

- [x] 1.1 Update source-sync and installation specifications and affected ADRs; verify with `openspec validate migrate-skill-sync-references`.

## 2. Bounded source and reference model

- [x] 2.1 Add failing tests for valid relative/multi-hop links, cycles, missing targets, depth limits, and external targets; implement bounded entry-link resolution and verify with `go test ./internal/skillsync`.
- [x] 2.2 Add tests for source-root/ancestor symlinks, internal tree symlinks, special files, repository overlap, import-only neutral/real-directory entries, and scan limits; implement eligibility and diagnostics and verify with `go test ./internal/skillsync`.
- [x] 2.3 Preserve all references while reading and hashing one real Skill tree once; verify same-content multi-entry and divergent-content cases with `go test ./internal/skillsync`.

## 3. Durable records and import publication

- [x] 3.1 Add failing tests for durable prepared batch records, atomic persistence, and resource-overlap blocking; implement the shared batch store and verify with `go test ./internal/operation`.
- [x] 3.2 Add failing tests for durable, idempotent source-discovery records on both new and reused versions; implement the record store and verify with `go test ./internal/skillsync`.
- [x] 3.3 Add write-ahead import-batch tests for interruption before/after version publication and source-record commit failure; implement recovery evidence without deleting a successfully published immutable version and verify with `go test ./internal/skillsync ./internal/operation ./internal/skillrepo`.

## 4. Per-entry managed resynchronization

- [x] 4.1 Add a failing resync test where one scope contains the same Skill ID at two entries bound to different versions; refactor planning to retain entry bindings and verify with `go test ./internal/lifecycle`.
- [x] 4.2 Apply a confirmed per-entry plan as one scoped operation and preserve each entry's version and materialization through Undo; verify with `go test ./internal/lifecycle ./internal/operation`.

## 5. Durable recovery and dependency-aware Undo

- [x] 5.1 Extend durable batch records with per-publication intent and recovery classifications for lifecycle operations; verify with `go test ./internal/operation ./internal/lifecycle`.
- [x] 5.2 Integrate write-ahead batches with import, activation, resync, and Undo; block writes that overlap pending resources and verify failure/interruption cases with `go test ./internal/operation ./internal/skillsync ./internal/lifecycle`.
- [x] 5.3 Persist link-chain dependencies and test Undo against a simulated restored before-state, including same-batch chains and changed external dependencies; verify with `go test ./internal/operation ./internal/webconsole`.

## 6. Explicit reference migration service

- [x] 6.1 Add failing preview/apply tests for exact-version matching, selected-entry-only writes, sync-mode materialization, stale state, and preservation of old sources; implement guarded migration and verify with `go test ./internal/skillsync ./internal/lifecycle`.
- [x] 6.2 Test indirect Global impact, direct external references, dependency conflicts, per-scope atomicity, and migration Undo; verify with `go test ./internal/skillsync ./internal/operation ./internal/lifecycle`.

## 7. Local API

- [x] 7.1 Add API tests for session/origin protection, server-side entry and plan IDs, migration/recovery previews, and path rejection; implement endpoints and verify with `go test ./internal/webconsole`.
- [x] 7.2 Invalidate sibling scope plans after an operation and block pending-batch overlaps; verify with `go test ./internal/webconsole`.

## 8. Web UI and final verification

- [x] 8.1 Add frontend tests for source chains, import-only states, migration selection, exact versions, and impact warnings; update Storage & Sync and bilingual strings and verify with the frontend test command.
- [x] 8.2 Add pending-recovery and dependency-aware Undo flows with tests for conflicts and confirmation; verify with the frontend test command.
- [x] 8.3 Rebuild embedded assets and run `go test ./...`, `go build ./cmd/agent-manager`, frontend tests/build, `openspec validate migrate-skill-sync-references`, and `openspec doctor`; record exact results.
