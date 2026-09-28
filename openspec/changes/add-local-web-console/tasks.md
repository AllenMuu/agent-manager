## 1. Go delivery and safe project context

- [x] 1.1 Add side-effect-free activation and removal preview methods to `lifecycle.Service`, sharing plan construction with the existing mutators; verify previews leave project, library, and journal trees byte-for-byte unchanged.
- [x] 1.2 Add `Journal.PreviewUndo` and share its plan construction with `UndoLatest`; verify preview does not rewrite the journal or restore paths.
- [x] 1.3 Create `internal/webconsole` server composition with embedded assets, crypto-random session token, strict loopback origin, bearer authentication, security headers, bounded JSON decoding, stable errors, and SPA fallback; verify auth and origin behavior through HTTP tests.
- [x] 1.4 Implement one-project session registration with canonical path validation and opaque project ID; verify invalid paths and repeated registration fail without exposing arbitrary file reads.
- [x] 1.5 Implement authenticated system, current project, agent inventory, technology/recommendation, and managed-resource inventory read endpoints using existing packages; verify stable JSON and no mutation.
- [x] 1.6 Implement Skill search/detail, read-only SubAgent list/detail/validation, diagnostics, and latest-operation endpoints; verify only eligible catalog entries and canonical definitions are exposed.
- [x] 1.7 Implement the in-memory operation-plan store with opaque IDs, ten-minute expiry, cancellation, single execution, and process-lifetime ownership; verify expired, unknown, cancelled, and concurrent plans are rejected.
- [x] 1.8 Implement activation/removal plan creation and execution using lifecycle preview/mutation APIs; verify caller-supplied paths are rejected and stale/conflicting plans do not mutate state.
- [x] 1.9 Implement Undo plan preview/execution using journal fingerprints and existing Undo safeguards; verify stale paths and changed journal state are preserved.
- [x] 1.10 Add the `agent-manager web` command with optional initial project and port, fixed `127.0.0.1` binding, clear startup URL/token fragment, and graceful shutdown; verify requested non-loopback binding is impossible.

## 2. React console

- [x] 2.1 Add a pinned Vite React/TypeScript frontend, package lock, strict TypeScript configuration, and CSS Modules-only styles; build production assets into the Go embedded UI directory.
- [x] 2.2 Add the nine-section visual guide in `web/DESIGN.md` and implement the responsive app shell, navigation, project registration, and session-token bootstrap.
- [x] 2.3 Implement the project workspace for agent status, technology evidence, Skill recommendations, inventory classifications, and loading/empty/error states.
- [x] 2.4 Implement searchable Skills and eligible Skill detail, read-only SubAgent definitions and validation, diagnostics, and latest-operation views.
- [x] 2.5 Implement activation/removal review, separate replacement confirmation, stale-plan recovery, Undo preview/confirmation, and success/failure feedback.
- [x] 2.6 Verify keyboard access, visible focus, semantic controls, 40px touch targets, reduced-motion behavior, and no horizontal overflow at 375px.

## 3. Verification and documentation

- [x] 3.1 Add HTTP tests for loopback/session security, read routes, unsafe project paths, two-phase plans, stale state, conflicts, rollback, cancellation, and Undo.
- [x] 3.2 Add frontend tests for token bootstrap, view states, plan review/confirmation, and API error handling; verify TypeScript and production build commands pass.
- [x] 3.3 Update README with local setup, `agent-manager web` usage, token/session behavior, API security limits, and frontend verification commands.
- [x] 3.4 Run formatting, static checks, full Go tests, frontend tests/build, OpenSpec validation, and visual review at 1280px and 375px; verify clean-checkout commands are documented and pass.
