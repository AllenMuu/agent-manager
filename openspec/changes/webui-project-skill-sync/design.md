## Context

See `proposal.md` for motivation and the two user-visible workflows. The current application stores versioned Skills in the configured repository (default `~/.agent-manager/skills`), imports only from a single legacy root through `internal/skillimport`, and activates Skills by creating absolute soft links. Global and project operations already have separate journals. The Web console is a session-authenticated local process; it receives the resolved home, project, and repository paths at startup.

## Goals / Non-Goals

**Goals:**

- Discover supported global and registered-project Skill sources without exposing general filesystem browsing.
- Copy selected sources into the versioned repository with complete source provenance and deterministic duplicate handling.
- Persist a sync mode shared by CLI and Web activation paths, and let the Web console resynchronize one scope at a time.
- Preserve existing preview, confirmation, journaling, rollback, and Undo boundaries.

**Non-Goals:**

- Changing the canonical repository location or adding a repository directory picker. The console displays the configured repository; its default remains `~/.agent-manager/skills`.
- Background watchers, scheduled syncing, network sources, Skill execution, or automatic activation of every imported Skill.
- One transaction spanning Global and Project resynchronization. Each scope uses its existing journal and is reviewed/applied independently.

## Decisions

### Use separate import and activation flows

The source-import workflow gathers Skill directories into the canonical versioned repository. The activation workflow materializes only Skills already selected for an Agent and scope. These remain separate actions because importing everything must not implicitly enable Skills in an Agent that may execute them.

Add a multi-root service in `internal/skillsync` rather than overloading the legacy single-root `internal/skillimport` contract. Derive roots from supported adapters and the neutral `agent`/`agents` locations; accept no paths from the browser. Use the catalog parser for eligibility and repository tree checksums for identity. Resolve and validate every path ancestor, do not traverse symlinks or special files, and exclude the configured repository itself. A source already represented by an Agent Manager-managed link or copy is shown as already present rather than imported again.

### Group by identity and checksum, not scope iteration order

Build one deterministic grouping keyed by Skill identifier and full tree checksum. Identical content from several scopes creates one repository version with every source recorded as provenance. Different checksums remain separate version candidates. If the identifier is new and has divergent variants, the preview requires the user to select which variant becomes current; existing current versions are never silently replaced. The preview shows entries already present by checksum as no-ops.

This prevents duplicate copies while keeping divergent source content reviewable. Source scope and canonical path are explicit repository metadata; they are not inferred from Skill instructions.

### Bind preview and apply to server-side state

Add authenticated local API operations for source inventory, import preview, and import apply. Store opaque, expiring preview state in the console session and bind it to source checksums, repository fingerprints, selected variants, project identity, and the active session. The browser submits identifiers and choices only; it never submits filesystem roots. Apply repeats discovery and validation under the shared import lock and repository lock before publishing, then checks the preview deadline after waiting for either lock.

The repository batch publisher stages all selected version trees and metadata, then publishes them as one operation. On failure, rollback removes or restores only state whose ownership is proven by the transaction; it preserves concurrent replacements. Successful imports add immutable repository versions and preserve all source folders. This workflow does not remove successful imports: activations may exist in projects outside the one currently registered with the console, so it cannot prove that deleting a version is safe.

### Keep synchronization mode machine-local and shared by clients

Add a validated local `sync_mode` setting with `auto`, `symlink-only`, and `copy-only`; missing settings resolve to `auto`. Persist it in Agent Manager's local YAML configuration so CLI and Web activations use the same behavior. When the Web console is started with `--config`, pass that resolved config path to the settings API. When no path is supplied, use `~/.agent-manager/config.yaml`; read it when present and create/update it atomically only after an explicit Save. Preserve other config fields and never return provider secrets to the browser.

`auto` tries the current absolute-link publication first and falls back to a staged copy only for platform/link-privilege failures. It does not hide conflicts, changed sources, or unrelated filesystem errors by copying. `symlink-only` and `copy-only` never change strategy. The successful method is retained in operation metadata so inventory and Undo can distinguish managed copies from unmanaged directories.

### Reuse per-scope journals for resynchronization

Expose Global and registered-Project resynchronization as separate preview/confirm operations. Re-read each managed activation's canonical repository version and compute a plan to rematerialize it using the selected mode. Use the existing global or project operation journal and lock; refuse unmanaged, orphaned, or changed destinations. This keeps one rollback boundary per established scope and avoids a new cross-scope transaction model.

For copy materialization, stage a validated repository tree beside the destination, verify its checksum, then publish it atomically. Journal metadata records the source version and actual method, allowing inventory to recognize copies without a committed project manifest. Undo restores the exact prior link, copy, or absent destination only while the after-state still matches the record.

### Add a Web “Storage & Sync” surface

Add a console view that displays the effective repository path, the three strategy options, and the effect of a mode change on future activations. Include distinct Global and Project source groups with per-item and select-all controls, an import preview and explicit confirmation, and separate “resync now” actions for existing managed activations in each scope. Show import status and per-scope resync Undo availability beside the relevant action. Add Simplified Chinese and English strings through the existing console localization.

## Risks / Trade-offs

- [Copy activations have no link target to prove ownership] → Record their canonical Skill version, materialization method, and before/after fingerprints in the existing scoped journals; treat copies without valid journal evidence as unmanaged.
- [Source scans may encounter symlinks or unsafe trees] → Validate every ancestor, reject symlink/special-file trees, bound traversal and metadata reads, and report skipped entries without following them.
- [An `auto` fallback could conceal a real write error] → Restrict fallback to recognized symlink capability/permission failures; any other error rolls back the batch.
- [A project outside the registered scope may activate an imported version] → Successful imports remain in the central repository; this workflow only rolls back a batch when the import itself fails.
- [Old binaries do not know how to recognize journaled copies] → Keep the default mode compatible with existing symlinks, do not rewrite old activations on setting changes, document the minimum version for copy modes, and never delete copies during downgrade.

## Migration Plan

1. Treat a missing sync-mode setting as `auto`; existing links remain untouched.
2. Add repository provenance for source scope/path and use temporary snapshots only for failed-batch rollback.
3. Add journal-aware copy ownership, strategy-aware activation, and separately journaled resynchronization for Global and Project scopes.
4. Expose source import and strategy/resync controls in the local console.
5. If the change is rolled back, leave repository versions and existing links/copies intact; remove only the unused UI/API path in a later code change. No automatic cleanup or migration is performed.
