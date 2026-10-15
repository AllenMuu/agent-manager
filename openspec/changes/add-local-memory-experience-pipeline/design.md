## Context

See `proposal.md` for motivation and `specs/project-memory-experience-pipeline/spec.md` for behavior.

The canonical `memory.Record` already carries project ownership, source, evidence, state, and versions through a guarded Gateway. Its selected local store is transactional JSON, not SQLite. `internal/run.Record` is a governance/lifecycle record containing project, runtime, state, policy, identity, and timestamps; it does not contain a conversation transcript or general agent output. Issue #35's direction update leaves execution persistence to agent runtimes, while retaining provenance and trust boundaries in Project Memory.

## Goals / Non-Goals

**Goals:**

- Add read-only evidence adapters over completed Agent Manager runs and explicitly selected Codex/Pi JSONL sessions, without changing their lifecycle or persisting another copy of run/session history.
- Store derived data locally with serialized consolidation, stable provenance, lexical search, and bounded progressive retrieval.
- Publish only operator-accepted proposals through the existing authorized Memory Gateway.

**Non-Goals:**

- Replacing or migrating the existing canonical Memory provider/store.
- Implementing a generic AgentRun, checkpoint, resume, timer, or execution journal system.
- Importing Claude Code or Hermes sessions, automatically scanning runtime homes, or persisting native transcript content in this slice.
- Treating a run summary, confidence, recency, or retrieval score as trusted Project Memory.
- Requiring an LLM, vector database, network service, automatic Skill creation, or installation.

## Decisions

### 1. Keep the derived store separate from canonical Memory

Use a project-local SQLite database at `.agent-manager/memory.db` for evidence references, derived memories, proposals, generated summary state, usage metadata, and FTS5 indexes. Keep the existing canonical store and its provider configuration unchanged. This follows the issue's SQLite requirement for derived memory without migrating a working canonical provider or changing #7's source of truth. Add SQLite sidecar files and generated pipeline state to `.gitignore`; do not store raw runs in this database.

Alternative considered: move all canonical records into SQLite. Rejected because it changes #7's provider behavior and risks breaking existing JSON stores and configured providers.

### 2. Read Agent Manager, Codex, and Pi evidence through provider boundaries

Define a `RunEvidenceProvider` with project-scoped discovery, retrieval by native run ID, and deterministic summarization. The Agent Manager adapter reads completed records and available governance evidence from `internal/run`. Codex and Pi adapters read only explicitly named JSONL directories, require the session's recorded working directory to match the registered project, and require a terminal session marker. They retain a digest of the full native file so changed evidence is rejected, but store only a bounded metadata summary and opaque native reference. No prompt or response text is copied into the derived database.

Alternative considered: copy runtime databases or build a new run archive. Rejected because it creates a second owner for execution history and conflicts with #35's runtime-owned execution boundary.

### 3. Make extraction deterministic and evidence-bounded

Use a deterministic extractor over normalized provider summaries for V1. It emits only fields supported by the source, with missing decisions, commands, or outcomes left absent. Generated records are always derived/untrusted. The extraction contract can later accept an optional local or cloud model, but no model request is part of this change.

Alternative considered: require an LLM for useful summaries. Rejected because it adds provider authorization, data egress, and nondeterministic behavior before the provenance and review pipeline is proven.

### 4. Serialize consolidation in SQLite transactions

Use one write transaction per project, backed by SQLite's cross-connection/process write lock, to deduplicate derived records, retire explicitly superseded records, expire low-ranked records beyond the active-set cap, update provenance links, refresh the compact summary, and maintain FTS5 indexes together. Expiration does not claim that a replacement exists. Assign immutable evidence IDs using project, runtime, native run ID, and normalized content hash; a changed hash creates new evidence instead of rewriting old evidence. Version the schema and reject unsupported database versions without silently replacing data.

Alternative considered: concurrent agent-side consolidation into Markdown. Rejected because it recreates mutable shared-file races and loses atomic provenance updates.

### 5. Treat proposal acceptance as the only pipeline promotion path

Persist a proposal before publishing it. On acceptance, require an explicitly authorized operator, preview/confirm through `memory.Gateway`, and create a canonical `memory.Record` with the proposal ID as source and all source evidence references. Store the Gateway operation ID with the pending publication so a restart can reconcile; an unknown remote result remains inspectable and is never blindly retried. A rejected proposal only changes the derived store.

The proposal ledger is the pipeline's verification record. This change does not claim that retrieval or extraction establishes trust, and it does not add unrelated trust fields to every existing canonical Memory record.

Alternative considered: write directly from the consolidator or add another canonical provider. Rejected because agents/consolidation must not own canonical state and #7 already supplies the authorized write boundary.

### 6. Keep retrieval layered and ranking dimensions separate

Expose summary, canonical search, derived search, and evidence lookup as separate calls. Existing bounded Gateway retrieval remains the canonical path. Derived search uses FTS5 lexical relevance and stable ID tie-breaking, and may include recency/usage in a separate ranking breakdown. Context assembly starts with the generated summary and current canonical results; deeper derived and raw evidence retrieval requires relevance or an explicit call. No retrieval score changes trust state.

Alternative considered: inject all summaries and history into every agent. Rejected because it grows context without a need and weakens evidence traceability.

### 7. Emit candidates without creating Skills

In the P1 stage, detect repeated procedures only from explicit provider claims or operator-attested claims attached to an unchanged imported run reference. Operator claims require a preview and confirmation, remain untrusted local metadata, and advance candidate-input generation. Do not infer workflows or outcomes from governance logs or transcript text. Require independent completed runs and explicit successful/verified outcomes, and disqualify candidates when any imported claim reports failure or project-specific scope. Persist a `SkillCandidate` with provenance and review state, then hand it to existing Skill Manager workflows.

An approved candidate can be explicitly converted into one local `internal/skillrepo` version after a separate confirmation. The converted version keeps the candidate ID as provenance and is marked untrusted. Conversion does not install or activate the Skill.

Alternative considered: create Skills automatically during consolidation. Rejected because Skill scope, trust, and installation remain explicit operator decisions.

## Risks / Trade-offs

- [Current `internal/run` records omit prompts, commands, and agent results] → V1 extraction is intentionally limited to recorded governance/lifecycle evidence; do not infer missing fields. Richer run evidence requires a later adapter to a runtime-owned export.
- [SQLite driver or target platform lacks FTS5] → select a pinned driver that includes FTS5 on supported platforms, check FTS5 at store open, and report unavailable rather than silently substituting different search behavior.
- [SQLite and the canonical Memory provider cannot share one transaction] → persist proposal intent and a stable Gateway operation ID first; reconcile confirmed outcomes before marking accepted, and never retry an unknown write automatically.
- [Project-local evidence references may expose local paths or identities] → prefer opaque native references, keep the database local and ignored by Git, and redact paths/identity values from ordinary diagnostics.
- [Deterministic summaries can still mislead when evidence is incomplete] → retain source hashes and evidence links, label all derived content untrusted, and require operator review before canonical publication.
- [A compact summary can grow or become stale] → regenerate it transactionally from bounded active derived memories and preserve the full lineage in SQLite.

## Migration Plan

1. Create the database only after an explicit local pipeline enable/init operation; do not import runs merely by opening Agent Manager.
2. Import references to completed runs idempotently, then derive and search summaries locally.
3. Keep proposal publication opt-in and use the configured Memory Gateway and exact project owner.
4. Roll back by disabling pipeline retrieval/publication. Preserve the SQLite database for inspection or explicit export; never delete it automatically, and leave canonical Memory untouched.
