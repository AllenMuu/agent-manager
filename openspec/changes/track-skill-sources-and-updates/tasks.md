## 1. Provenance and candidate evidence

- [x] 1.1 Read the supported Skills CLI global and registered-project lock files, attach source metadata to discovered candidates, and safely retain unknown/local labels.
- [x] 1.2 Track newest regular-file modification time per discovered Skill and expose it with path/checksum evidence; mark a unique newer divergent candidate without changing explicit current-choice rules.
- [x] 1.3 Expose repository version source metadata in the Skill management API and UI.

## 2. Upstream checking and updates

- [x] 2.1 Implement bounded, redirect-free GitHub tree and raw-content access with deterministic local/project hash comparison and clear current/update/unsupported/error states.
- [x] 2.2 Add session-bound update preparation and confirmation APIs; validate remote Skill trees, revalidate discovered local state, apply all/selected updates transactionally, update lock metadata, and support guarded Undo.
- [x] 2.3 Add the Sync page check, update-all/update-selected review, source display, and update Undo controls; retain explicit current-version selection and confirmation.

## 3. Documentation and validation

- [x] 3.1 Update ADR 0006 with the explicit, user-initiated upstream-update exception and its network/safety boundaries.
- [x] 3.2 Add Go and Web regression coverage for source attribution, mtime/current-choice evidence, update statuses, safe remote parsing, guarded apply/Undo, API authorization, and UI selection/confirmation.
- [x] 3.3 Run focused package and Web checks, then the repository's standard Go test/build and Web build checks; validate the OpenSpec change.
