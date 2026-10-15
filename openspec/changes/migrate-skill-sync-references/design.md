## Context

See `proposal.md` for motivation and `specs/` for the user-visible contract. The current source scanner rejects entry symlinks, imports publish immutable versions with process-local rollback, resync groups entries by Skill ID, and scoped operation journals are committed after filesystem publication. Global and Project operations already have separate journals and locks; those boundaries remain.

## Goals / Non-Goals

**Goals:**

- Make source identity, source-reference identity, and activation-entry identity explicit and independently verifiable.
- Add a durable recovery protocol without changing the existing immutable-version or scope-journal ownership model.
- Reuse the existing repository, adapter, lifecycle, session-plan, and localization boundaries.

**Non-Goals:**

- General filesystem browsing or symlink following outside a discovered Skill entry.
- Replacing the existing scoped journals with one cross-scope journal.
- Removing imported versions or source directories during migration or Undo.

## Decisions

### Represent each discovered entry separately from its content candidate

Keep one server-discovered source record per entry, with an opaque entry ID, scope, adapter/root, entry path, entry type, original link text, ordered link hops, resolved real Skill path, checksum, and independent import/migration eligibility. A grouped candidate retains all source records, including multiple entries pointing at one real directory, while the service reads and hashes that real tree once. The candidate's read path is the resolved real path; migration always targets the entry path.

Use a maximum of 16 symbolic-link hops, 4,096 immediate entries per scope, and 2,048 regular files / 64 MiB per Skill tree. This bounds path resolution, response size, and hashing work; the cost is that unusually large Skills require a future configurable limit rather than being partially imported. Keep root/ancestor and destination path checks separate from entry-link resolution. Resolve only a discovered adapter entry, never enumerate the target's siblings, and continue rejecting links and special files inside a Skill tree.

### Store source discovery separately from version provenance

Write one JSON record per confirmed source relationship beneath `~/.agent-manager/skill-sync/discoveries/`, and one recoverable batch record beneath `~/.agent-manager/skill-sync/batches/`. Use a random record ID plus the batch/source identity as an idempotency key. Publish each record by writing a sibling temporary file, syncing it, atomically renaming it, and syncing the parent directory. A later observation creates a new record; recovery of the same batch reuses its record ID. Reusing a repository version never rewrites that version's provenance or trust metadata.

Return an import result containing the batch ID, exact repository entries/versions, and source-discovery record IDs. A migration plan resolves each source record against current filesystem state and checksum; historical evidence is not authority to mutate a changed link.

### Bind lifecycle work to entries, not a Skill-ID Cartesian product

Represent each activation as one binding of scope, target Agent, destination entry path, Skill ID, immutable version key, source checksum, and materialization method. Refactor the resync plan/apply path to materialize an explicit list of bindings, then capture and publish all bindings as one operation within the selected scope. The target path is the ownership key; two entries with the same Skill ID may retain different version bindings. A legacy link derives its version from its canonical `.../versions/<key>` target. A journal-owned copy derives its version from the journal. If either cannot be proven, resync blocks rather than choosing the current version.

Keep normal resync restricted to journal-verified activations. External-link migration uses a separate reviewed operation that binds the exact pre-migration path fingerprint and source chain, then delegates to a guarded per-entry publisher with explicit adoption authority only for that path. It does not weaken ordinary resync, removal, or the project/global journal validators.

### Persist a write-ahead batch beside sync state

Add an operation batch store that records kind, scope, batch ID, resources, backups, before-state, intended after-state, link dependencies, source-record IDs, publication progress, and status (`prepared`, `publishing`, `committed`, or `needs-recovery`). Persist backups and `prepared` before the first target mutation; persist each intended publication before changing its path; persist observed progress afterward. Mark committed only after destination verification, the scope journal, and source records are durable. Keep successful immutable imports after downstream migration failure.

Reuse the existing import lock or scope journal operation lock for the affected resource set. On page entry/startup, list pending batches read-only. Before any overlapping import, activation, update, resync, migration, recovery, or Undo, consult the store and block the write. Recovery preview compares the durable intent and before-state to current files. After a separate user confirmation, it may finish an entirely verified after-state or restore paths still proven to be transaction-owned. Unknown or mixed states remain conflicts with their backups preserved. Retrying recovery uses the same batch and source-record IDs.

The protocol relies on file and directory sync plus atomic same-filesystem replacement. If the host cannot provide these operations for a target, reject mutation before publication. The existing journal remains the user-facing reversible-operation log; the batch record closes the crash window before that log can be committed.

### Validate Undo against a simulated before-state

Persist each migrated entry's original link text, every observed hop, content checksum, and dependency classification. Before Undo, construct a read-only view in which this operation's `Before` snapshots are substituted for its current paths. Resolve the complete restored chains against that view, then validate external dependency nodes against their captured evidence. This accepts a link changed by the same batch while still rejecting a changed outside link. Restore entries in dependency-safe order and keep Global and Project Undo independent; a dependent Project preview is refreshed after a Global operation.

### Keep the browser path-free and expire sibling plans

Expose source references through the existing authenticated sync inventory. Add session-bound server plans for migration and recovery, using opaque entry IDs and plan IDs only. The apply handler reloads the registered Project, obtains the appropriate scope lock, rechecks the full chain/checksum/version/destination/mode, and then executes. A completed operation invalidates all outstanding sync plans so other scopes must be re-previewed.

Add migration, impact, pending-recovery, and dependency-conflict sections to `StorageSyncPage`; render explicit import-only/blocked states and bilingual text. Keep the import confirmation distinct from reference migration confirmation.

## Risks / Trade-offs

- [Large or adversarial source trees can consume disk, CPU, or response memory] → Enforce the documented scan, hop, file-count, and byte limits before hashing or staging; never return partial success as a complete inventory.
- [A process can stop between publishing a path and recording progress] → Persist intent first and classify actual state against exact before/after fingerprints during explicit recovery.
- [A matching after-state can be recreated by another actor] → Hold the applicable local operation lock during publication and recovery; if state cannot be attributed from durable intent and exact path evidence, report a conflict and preserve it.
- [Global references can affect projects outside the registered scan] → Warn in the preview, verify observed content around the switch, preserve old source directories, and avoid claiming those projects are managed.
- [Existing journals lack per-entry dependency evidence] → Continue reading legacy entries; resync only when an exact version can be derived, and require fresh evidence before a new Undo can depend on external links.

## Migration Plan

1. Land the source/link and activation-entry contract while retaining legacy JSON compatibility; absent optional fields continue to decode as empty.
2. Add durable discovery and batch records without changing existing repository versions or activation paths.
3. Enable bounded link discovery and import evidence; keep existing entry links untouched until migration is separately confirmed.
4. Enable per-entry resync and its recovery path, then expose external-reference migration only after exact-version binding and Undo validation are in place.
5. Add authenticated APIs and bilingual UI; incomplete batches remain visible and block overlapping writes.
6. If the UI/API is later removed, retain batch and discovery state for recovery and keep imported immutable versions. Do not delete source entries or versions as a rollback step.
