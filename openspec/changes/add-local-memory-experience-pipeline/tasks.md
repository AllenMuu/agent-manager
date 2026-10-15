## 1. Evidence boundary and deterministic extraction (P0)

- [x] 1.1 Define the provider-neutral `RunEvidenceRef` and `RunEvidenceProvider` contracts, project ownership rules, immutable status, and stable identity; verify the package contract with table-driven tests.
- [x] 1.2 Implement read-only adapters for completed Agent Manager runs and explicitly selected Codex/Pi JSONL sessions; verify project identity, terminal-state filtering, source immutability, and stale-source rejection.
- [x] 1.3 Normalize available evidence and calculate stable hashes, including full native source digests; verify unchanged imports are idempotent and changed evidence cannot be reopened as the prior reference.
- [x] 1.4 Implement deterministic fixture-backed extraction for summary, decisions, discoveries, constraints, known issues, commands, and failed approaches; verify repeatability, absent-field behavior, and no model/network use.

## 2. Local derived-memory store and consolidation (P0)

- [x] 2.1 Select and pin a SQLite driver with FTS5 support on supported platforms, add schema versioning and local database initialization, and ignore database/sidecar files; verify FTS5 availability is reported truthfully and existing canonical stores are untouched.
- [x] 2.2 Persist evidence references, derived memories, project summaries, proposals, and lineage in SQLite; verify project isolation, reopen durability, and rejection of unsupported schema versions.
- [x] 2.3 Serialize project consolidation in a write transaction, including deduplication, supersession, summary refresh, and FTS maintenance; verify concurrent consolidators preserve committed provenance and one current result.
- [x] 2.4 Implement bounded lexical search with stable ordering and separate relevance, confidence, recency, and usage fields; verify search requires no embeddings and cannot alter trust or verification state.

## 3. Proposal and canonical promotion (P0)

- [x] 3.1 Create provenance-linked pending proposals during eligible consolidation or through an explicit CLI command without writing canonical Memory; verify each pending proposal retains all source evidence references.
- [x] 3.2 Add operator accept/reject flows through the existing Memory Gateway preview and confirmation boundary; verify only acceptance creates one canonical record with the proposal and evidence references.
- [x] 3.3 Persist a stable Gateway operation ID and reconciliation state before publication; verify unknown write outcomes remain inspectable and are never automatically replayed.
- [x] 3.4 Verify accepted canonical Memory can be read through Codex and Pi role-context handoffs and that rejected/derived content remains absent from canonical search.

## 4. Progressive retrieval and delivery (P0)

- [x] 4.1 Implement summary, current canonical search, derived search, and explicit evidence lookup with exact project authorization; verify authorization is checked before each provider is accessed.
- [x] 4.2 Integrate initialized pipeline summaries and bounded canonical retrieval into role task-context assembly, with derived and raw evidence opened only on demand; verify item/byte budgets, source attribution, and no implicit database initialization.
- [x] 4.3 Add explicit CLI operations for initializing the pipeline, importing/extracting Agent Manager/Codex/Pi evidence, consolidating, searching, and reviewing proposals; verify no import or promotion occurs on ordinary status/read commands.
- [x] 4.4 Add end-to-end CLI coverage from evidence import through proposal acceptance and cross-agent canonical retrieval.

## 5. Usage-aware retention and Skill candidates (P1)

- [x] 5.1 Track creation/update/last-used timestamps, usage counts, confidence, and verification/proposal state independently; verify ranking changes do not mutate trust state.
- [x] 5.2 Detect repeated procedures only across independent completed runs with explicit successful or verified outcomes and no contradictory failed evidence; verify unsupported or project-specific patterns do not become candidates.
- [x] 5.3 Add a previewed, confirmed operator annotation path for bounded procedure claims when built-in run providers lack workflow outcomes; keep claims untrusted and bind them to unchanged imported run references.
- [x] 5.4 Persist and surface `SkillCandidate` records with name, proposed workflow, confidence, project-specific marker, evidence references, and review state through explicit Skill Manager review boundaries; verify no Skill is created, installed, or published automatically.
- [x] 5.5 Convert only an approved candidate into a selected local Skill repository after a separate explicit confirmation; preserve candidate provenance and mark the new version untrusted without installing or activating it.

## 6. Documentation and acceptance

- [x] 6.1 Document local database location, explicit Codex/Pi source directories, evidence provenance, proposal creation/review, retrieval layers, Skill candidate conversion, retention, and rollback in English and Chinese operator documentation; verify documented commands match CLI help.
- [x] 6.2 Run focused and full Go tests, `go build ./cmd/agent-manager`, and `openspec validate add-local-memory-experience-pipeline --strict`; record results for each required scenario.
- [x] 6.3 Verify the first vertical slice end to end: completed run reference, deterministic summary, derived search, explicit proposal acceptance, canonical read through Codex and Pi role handoffs, and no direct trust promotion from raw history.

## 7. Diagnostics (P1)

- [x] 7.1 Extend Doctor with a read-only pending-proposal finding; verify it does not reveal proposal content, initialize absent pipeline state, or accept/reject proposals.
