## 1. Shared installation application service

- [x] 1.1 Extract side-effect-free batch planning from `AddMany` and preserve `AddMany` as a compatibility wrapper; verify existing lifecycle and undo tests continue to pass.
- [x] 1.2 Add an application service that resolves explicit Skill identifiers from the configured local library and validates project, targets, and conflict options; verify unknown, ineligible, duplicate, or empty selections produce no writes.
- [x] 1.3 Add preview and apply operations that revalidate the current plan before mutation and reject stale previews; verify changed targets or conflicts produce an updated preview without changing files or the journal.
- [x] 1.4 Verify successful multi-Skill installation uses one journaled operation and rolls back every published destination when a pre-commit write fails.
- [x] 1.5 Serialize confirmed installations and journal updates across processes; verify concurrent installs remain independently undoable.
- [x] 1.6 Include symlink target content in preview fingerprints; verify changed content is stale while unrelated parent-directory creation is not.

## 2. CLI install workflow

- [x] 2.1 Add `agent-manager install <skill-id>...` with project, target, conflict, force, and confirmation flags; verify one command can preview and activate multiple selected Skills.
- [x] 2.2 Keep `add` and `select` behavior available through the shared application service; verify their existing command tests and CLI help remain compatible.
- [x] 2.3 Verify installation from identifiers shown by `search` or `recommend` never selects additional Skills and never changes files without `--yes`.

## 3. Local WebUI and install bridge

- [x] 3.1 Create the minimal React UI and reproducible asset build, then embed its output in the Go executable; verify a clean build serves the embedded UI without a Node runtime.
- [x] 3.2 Add `agent-manager webui --project <path>` with a loopback-only local server and per-session request protection; verify the server rejects non-loopback access and requests outside the active session.
- [x] 3.3 Add read-only catalog search and project recommendation endpoints backed by existing Go services; verify the WebUI displays only eligible local-library Skills and recommendations for its selected project.
- [x] 3.4 Add WebUI selection, target selection, plan preview, and confirmation flows backed by the shared installation service; verify confirmation applies only the selected Skills and targets.
- [x] 3.5 Verify a stale WebUI plan is rejected and refreshed, compatibility warnings remain visible, conflicts require the supported force path, and failed batches leave no partial activation.
- [x] 3.6 Report local service network failures and release the UI busy state so users can retry without reloading.

## 4. Documentation and integration

- [x] 4.1 Update ADR 0005, README command examples, and build instructions to describe the loopback WebUI and `install` command; verify documented commands match the shipped help output.
- [x] 4.2 Run OpenSpec validation, the complete Go test suite, and the frontend build/test workflow; verify the CLI and WebUI share the same guarded behavior and the change validates cleanly.
- [x] 4.3 Re-run review-driven concurrency, rollback, symlink-fingerprint, and WebUI network-failure regression checks.
