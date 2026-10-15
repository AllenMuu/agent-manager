## Why

Project Memory can already be shared through the canonical Memory Gateway, but completed agent work has no bounded path to become reviewable, provenance-linked proposals. This change adds a local-first pipeline that treats run records as untrusted evidence and keeps human-confirmed Project Memory as the only canonical state.

## What Changes

- Add provider-neutral, read-only references to completed Agent Manager, Codex, and Pi runs, with stable source identity and source-content hashes; do not add or own generic run/session persistence.
- Extract deterministic summaries into a separate SQLite-backed derived-memory store with lexical search and a single project-level consolidation boundary.
- Keep derived memories and proposals untrusted until an operator explicitly accepts a proposal through the existing Memory Gateway confirmation path.
- Add progressive retrieval from a compact summary to canonical and derived memory, opening source evidence only on demand.
- Track usage and recency separately from trust; emit reviewed Skill candidates for repeated validated procedures and convert only after explicit approval into an untrusted local Skill version.
- Keep native adapters opt-in by explicit source directory; do not scan global runtime stores implicitly or copy transcript contents into the derived database.
- Defer Claude Code and Hermes native session adapters.

## Capabilities

### New Capabilities
- `project-memory-experience-pipeline`: References completed run evidence, derives and consolidates local memory, proposes verified Project Memory, and retrieves progressively.

### Modified Capabilities

## Impact

- Affected code: `internal/run` read-only integration, new `internal/memorypipeline` package, `internal/memory` Gateway boundary, and CLI/context retrieval entry points.
- Storage: a separate project-local SQLite database for evidence references, derived memories, proposals, and FTS5 indexes; the existing canonical Memory provider/store remains unchanged.
- Dependencies: a SQLite driver may be required; pin and document it during implementation. No network or model is required for deterministic extraction.
- Trust boundary: source runs, extracted summaries, and proposals remain untrusted. Only explicit operator confirmation promotes a proposal into canonical Project Memory, retaining evidence references.
