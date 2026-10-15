## 1. Configuration and Materialization

- [x] 1.1 Add validated `sync_mode` configuration with `auto` default, explicit-config and default-path persistence, and atomic updates; verify config tests cover default, supported/unsupported values, and preservation of other settings.
- [x] 1.2 Implement symlink-only, copy-only, and auto symlink-first materialization for Global and Project activations; verify lifecycle tests cover fallback classification, staged copy, failure rollback, and unchanged source versions.
- [x] 1.3 Record materialization method and source version in scoped journals, recognize journal-owned copies in inventory, and preserve exact-state Undo; verify tests reject unmanaged copies and changed after-state.
- [x] 1.4 Update ADR-0001 and the project activation capability contract for journal-owned copies without a committed Project manifest; verify the updated OpenSpec change validates.

## 2. Global and Project Source Import

- [x] 2.1 Add safe multi-root discovery for supported global and registered-project Skill directories; verify tests cover absent roots, all documented adapters/neutral roots, symlink ancestors, special files, and repository exclusion.
- [x] 2.2 Group discovered Skills deterministically by identifier and full tree checksum, preserving all source provenance and retaining divergent variants; verify cross-scope duplicate and variant tests are order-independent.
- [x] 2.3 Add import preview, explicit selection, stale-plan revalidation, and atomic repository publication; verify tests cover no-op existing versions, current-version choice, injected batch failure, and later repository changes. Successful imports remain durable because unregistered projects may activate them.

## 3. Scoped Resynchronization

- [x] 3.1 Add Global and Project resync previews from journal-verified managed activations; verify previews exclude unmanaged/orphaned entries and show target paths and proposed methods.
- [x] 3.2 Apply each confirmed resync as one operation in its existing scope journal; verify tests cover stale destination/source state, atomic rollback, concurrent operations, and exact Undo for prior links/copies.

## 4. Web Console and UI

- [x] 4.1 Add authenticated APIs for settings, source inventory/import preview/apply, and scoped resync preview/apply/Undo; verify API tests reject missing sessions, untrusted origins, arbitrary paths, and stale tokens.
- [x] 4.2 Add the Storage & Sync console view with configured repository path, three mode choices, distinct Global/Project source lists, select-all, review/confirm, scoped resync, and Undo; verify keyboard and narrow-screen behavior with frontend tests.
- [x] 4.3 Add Simplified Chinese and English labels, diagnostics, warnings, and completion states; verify each visible new string has both translations.
- [x] 4.4 Rebuild embedded console assets and run the focused Go, frontend, and repository-wide verification commands; record exact results in the change delivery notes.
