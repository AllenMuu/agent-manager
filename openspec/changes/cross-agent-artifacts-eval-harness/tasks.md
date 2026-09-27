## 1. Artifact Protocol and Roles

- [x] 1.1 Require the v1 artifact envelope fields and preserve unknown fields; verify validation and round-trip coverage in `internal/artifact` tests.
- [x] 1.2 Keep task storage paths ID/path-validated and artifact-save writes explicitly confirmed; verify init/save/list/show/validate/render coverage in CLI and store tests.
- [x] 1.3 Define planner, implementer, reviewer, and verifier contracts with explicit artifact and permission requirements; verify capability binding reports unsupported requirements.
- [x] 1.4 Keep lessons promotion explicit and typed; verify artifact use alone does not write to Memory.

## 2. Task Context Assembly

- [x] 2.1 Assemble bounded repository guidance, requested compatible Skills, role input artifacts, and optional read/search Memory results; verify resolver tests cover bounds and no provider writes.

## 3. Local Evaluation Harness

- [x] 3.1 Load versioned local cases and statically read candidate responses without executing content; verify malformed-case, missing-candidate, and filesystem-boundary coverage.
- [x] 3.2 Persist versioned evaluation results with per-case evidence and runtime/configuration labels; verify result validation and deterministic case ordering.
- [x] 3.3 Compare results by stable case ID and report regressions, improvements, additions, and missing baseline cases; verify comparison tests cover every category.

## 4. CLI, Fixtures, and Documentation

- [x] 4.1 Expose task, artifact, role, and eval workflows through the CLI; verify command help and end-to-end fixtures describe the same behavior.
- [x] 4.2 Document the artifact lifecycle and planned evaluation baseline; verify the README examples and eval catalog match the implemented commands and fixtures.
