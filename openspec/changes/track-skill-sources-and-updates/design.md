## Context

See `proposal.md` and `specs/skill-upstream-updates/spec.md`. Synchronization currently discovers supported local directories, hashes Skill trees, and imports reviewed versions into the library. The configured library already records version provenance, but the browser catalog drops most source fields. The Skills CLI records upstream installation evidence in global `.skill-lock.json` and project `skills-lock.json` files; local discovery must continue to work without network access.

## Goals / Non-Goals

**Goals:**

- Reuse lock-file source metadata to label installed candidates and check only sources with verifiable GitHub tracking data.
- Compare local candidates with their upstream content using the hash algorithm represented by their lock entry.
- Stage remote content, present an explicit update plan, atomically replace selected Skill directories and lock metadata, and retain an operation-journal before-state for Undo.
- Keep all user-facing source and status evidence explicit about whether it is a local path, a remote source, unsupported, current, or not checkable.

**Non-Goals:**

- Scheduled or background checks, automatic updates, executing `npx`, installing dependencies, or updating arbitrary Git/HTTP/npm sources.
- Semantic version discovery where upstream records only content hashes.
- Changing the existing import rule that divergent local content needs an explicit current-version choice.

## Decisions

1. **Use Skills CLI lock files as provenance evidence.** Read the global lock from `$XDG_STATE_HOME/skills/.skill-lock.json` when configured, otherwise `~/.agents/.skill-lock.json`; read project-local `skills-lock.json` only from the registered project root. Associate an entry by exact Skill directory identifier and matching scope. Display its declared source/source type and lock timestamps when present. Missing entries remain local/unknown; Skill-authored prose is never treated as provenance.

2. **Start with hash-tracked GitHub sources.** Require `sourceType=github`, a valid `owner/repository`, an explicit Skill path, a supported 40-character Git tree hash or 64-character content hash, and a safe ref. Call only `api.github.com` and `raw.githubusercontent.com` over HTTPS; disable redirects, set deadlines and response/file-count/total-byte bounds, reject truncated repository trees, and never accept a URL or ref from the browser. Compare Git tree hashes for global entries and the Skills CLI content hash for project entries. The project lock hash uses locale-sensitive path ordering, so the first implementation supports only single-file project Skills; multi-file entries remain visible but unsupported. Other origins get an explanatory unsupported/not-checkable result.

3. **Keep checking separate from updates.** Inventory and rendering remain local and offline. A button initiates checks. Available updates can be selected individually or all at once; preparation fetches and validates content but does not touch installed files. The resulting session-bound plan captures source lock state, target checksums, staged files, and expected remote hashes. Confirmation revalidates local state before applying.

4. **Replace installed trees as guarded filesystem operations.** Accept only discovered eligible, real directories beneath supported locations. Remote trees may contain only bounded regular blobs; reject symlinks, submodules, special files, oversized files, invalid Skill metadata, and mismatched identifiers. Stage replacements as siblings on the same filesystem, snapshot the Skill directories and lock file through an update journal, swap targets, and update their lock hashes/timestamps. Roll back targets changed by the transaction if a step fails; check transaction ownership before any rollback. The journal uses a path validator limited to supported global locations, the registered project, the exact lock files, and its own backup root.

5. **Keep current-version choice independent from timestamps.** Discover the newest mtime among each candidate tree's regular files and show it alongside the content hash and every source path. Compare newest mtimes only among divergent variants for the same identifier; mark only a unique non-zero maximum. The marker is advisory and never populates the explicit current-choice selection.

6. **Pass source fields through the Skill catalog.** The library API already reads source/type/publisher/ref metadata from the versioned repository. Extend its response and management UI to show the recorded source in each row and details, falling back to local/unknown labels.

## Risks / Trade-offs

- [GitHub rate limits or transient outages] → Report an indeterminate check, never “up to date”; keep checks user-initiated and bounded.
- [File mtimes can reflect copies, extraction, or clock skew] → Label them as local metadata, mark only a unique comparable maximum, and keep checksum and explicit choice visible.
- [A batch touches several trees and lock files] → Stage first, serialize local operations, revalidate before commit, journal snapshots, and use transaction-scoped rollback checks.
- [Skills CLI lock formats can evolve] → Parse only known fields and versions; preserve unknown JSON fields when updating a lock; return unsupported when a required field is missing.

## Migration Plan

No migration is required. Existing installs without a recognized lock file remain visible as local or unknown and cannot be checked. On update, only the matching lock entries are changed. A confirmed update records a reversible snapshot; the Undo flow restores the pre-update tree and lock file if their current states still match the journal's after-state.
