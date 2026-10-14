## Context

The repository was specification-first when this change was proposed. See [proposal.md](./proposal.md) for its original motivation and the project ADRs for the source-of-truth, adapter, and machine-local activation decisions. The original local collection at `~/.agents/skills` is now the legacy import source; Claude Code, Codex, and Pi accept materialized project Skills in their respective agent-owned directories.

## Goals / Non-Goals

**Goals:**

- Implement a local-first Go CLI whose state is derived from filesystem links plus minimal global configuration and operation history.
- Make normal operations safe around user-owned project files and readable by both humans and agents.
- Keep agent-specific placement isolated behind adapters, beginning with Claude Code and Codex.
- Keep domain and application behavior independent from terminal presentation so later interfaces can reuse it.

**Non-Goals:**

- Bidirectional content synchronization, project activation manifests, or portable cross-machine links.
- Editing, executing, installing dependencies for, or judging the quality of managed skills.
- Pi single-file skills or write support for Trae, OpenCode, and Agy in the first release.
- A Web UI, local HTTP service, Wails desktop application, or any other GUI in the first release.
- Remote access, accounts, multi-user operation, cloud storage, and network integrations.

## Decisions

### Go core and CLI runtime

The first release will use Go for the domain model, filesystem operations, and `skill-manager` CLI. Go provides a single distributable executable, predictable startup, cross-platform path and soft-link handling, structured JSON output, and tests that isolate filesystem fixtures with `t.TempDir`.

A Node.js-only implementation was rejected because installed users would otherwise need a JavaScript runtime and because filesystem management is the product's trusted core. A shell-only implementation was rejected because safe planning, journaling, recovery, and adapter tests require more durable data and error handling.

### Core remains independent from terminal delivery

Commands translate input into application requests, but planning, conflict detection, compatibility warnings, journaling, diagnostics, and filesystem changes live outside the command and prompt packages. The first release does not add an HTTP API, frontend build, or Wails dependency merely to anticipate later clients.

The intended evolution after the CLI release is a React and TypeScript WebUI backed by the same Go application services, followed by a Wails macOS package that reuses the React interface and Go core. Those phases will define their own API, security, packaging, and user-interface specifications when they enter scope.

### Local filesystem is the activation state

The managed project directory is an absolute link to the configured skill-library directory. The Go application layer inspects these links rather than reading a project manifest. A small user-level configuration stores the library location, and a user-level journal stores reversible filesystem transformations.

This aligns with the current shared-library practice and preserves project-owned, unmanaged skills. A manifest was rejected because it turns intentionally machine-local activation into a versioned project concern.

### Directory skills and companion metadata

A library entry is valid when it is a directory containing `SKILL.md`; the entire directory is the unit for copy, adoption, and fork so supporting resources remain intact. Management-only fields live in optional `.skill-manager.yaml` files, while existing frontmatter remains untouched.

This avoids coupling the application to each agent's frontmatter tolerance. A centralized metadata registry was rejected because it would become a second source of truth that does not travel with an adopted skill.

### Command surface separates interactive and automation flows

The CLI exposes `init`, `search`, `select`, `add`, `list`, `remove`, `adopt`, `fork`, `doctor`, `reconcile`, `undo`, and a guarded library-delete operation. `select` provides a terminal multi-select experience; corresponding non-interactive commands accept explicit values and JSON output for agent workflows. No command is named `sync` because project links are not bidirectional synchronization.

### Adapters expose placement, not semantic compatibility

Each adapter identifies project and global placement paths and performs directory-link operations. Claude Code and Codex are initially writable adapters. The catalog surfaces optional compatibility metadata and activation warns, but does not prevent, a deliberate install to a non-declared target.

This keeps support claims honest. A uniform all-agent resource abstraction was rejected because Pi is file-based and other local layouts remain unverified.

### Mutations use a planned transaction model

Every mutating command computes a plan, validates destinations, presents changes, requires confirmation, then applies the filesystem changes. It records enough before/after state in the operation journal to undo the latest reversible operation. Filesystem updates use rename-based staging where feasible so a failed operation cannot leave an intended destination half-replaced.

Conflicts with unmanaged paths or unexpected links stop by default. Force modes require an explicit conflict strategy and an additional confirmation. The CLI never runs code found inside a skill.

### Initialization owns the global baseline

Only `init` verifies the CLI and installs or updates the small Operator skill in supported global directories. All project commands remain project-scoped. The Operator skill uses structured `search` output and directs an agent to obtain human confirmation before a mutation.

## Risks / Trade-offs

- [Absolute links break after a library move or on another machine] → `doctor` detects orphaned links and `reconcile` rebuilds them; Git guidance discourages committing them.
- [Adoption and force replacement can lose user-owned content] → show a plan, require confirmation, preserve reversible state when possible, and reject ambiguous conflicts by default.
- [The CLI cannot discover every project that references a library skill] → library deletion refuses by default and diagnostics accepts explicit scan roots.
- [Premature GUI abstractions could complicate the CLI] → isolate application behavior from terminal delivery, but defer HTTP, React, and Wails abstractions until those phases are specified.
- [Agent directories evolve] → confine paths and detection to adapters, test adapters against real layouts, and mark uncertain agents unsupported.
- [Full-text catalog search may grow expensive] → index only the directory's `SKILL.md` and refresh lazily or on explicit catalog operations; do not traverse reference trees.

## Migration Plan

1. Install the Go CLI without altering existing skills.
2. Run `init` to configure the default library and install the minimal Operator skill after preview and confirmation.
3. Use read-only `doctor`, `search`, and `list` to inspect current projects.
4. Activate skills through confirmed links or adopt project skills one at a time.
5. Roll back individual recent mutations with `undo`; repair moved-library links with `reconcile`.
6. Specify and implement the React WebUI and Wails macOS phases only after the CLI release is stable.
