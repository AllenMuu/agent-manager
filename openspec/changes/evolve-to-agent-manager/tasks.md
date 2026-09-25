## 1. Compatibility baseline and product migration

- [x] 1.1 Inventory the current Skill CLI entrypoints, configuration, companion metadata, project links, and journal formats; add legacy fixtures and verify they reproduce existing `go test ./...` behavior.
- [x] 1.2 Introduce `agent-manager` as the primary binary and route `skill-manager` through the same command implementation with a deprecation notice; verify existing command arguments retain equivalent outcomes.
- [x] 1.3 Update `CONTEXT.md`, README command examples, and user-facing diagnostics to use Agent Manager for the product and Skill for the resource domain; verify terminology preserves the documented glossary distinctions.
- [x] 1.4 Add versioned readers that normalize legacy Skill configuration and journal entries without rewriting them; verify a pre-migration activation can be listed and undone.
- [x] 1.5 Write an ADR for core resource contracts, agent adapters, provider boundaries, and compatibility guarantees; verify it records rejected alternatives from `design.md`.

## 2. Agent and resource governance core

- [x] 2.1 Define managed-resource identity, kind, provenance, compatibility, and capability requirement models; verify unit tests cover valid and invalid resource contracts.
- [x] 2.2 Define `ResourceHandler` and `AgentAdapter` contracts for detection, inspection, validation, planning, placement, and capabilities; verify domain tests do not require runtime-specific paths.
- [x] 2.3 Extract existing Skill catalog and lifecycle behavior into the Skill handler and adapter-backed placement flow; verify all existing catalog, activation, adoption, fork, reconciliation, and undo tests pass unchanged.
- [x] 2.4 Implement capability comparison and explicit unsupported-capability results; verify no operation plan is generated when a required target capability is absent.
- [x] 2.5 Implement read-only agent and resource inventory commands with human-readable and JSON output; verify supported, unavailable, and unsupported agent fixtures are distinguishable.
- [x] 2.6 Implement or register the Pi adapter with only verified locations and declared capabilities; verify unsupported resource kinds are reported without writes.
- [x] 2.7 Extend `doctor` to report adapter capabilities, unmanaged resources, orphaned links, and unsupported agent locations; verify diagnostics are read-only.

## 3. Guarded generic operations

- [x] 3.1 Generalize operation plans and journal records with resource kind and schema version while preserving legacy readers; verify previews and undo work for old and new Skill records.
- [x] 3.2 Apply existing conflict-strategy, force-confirmation, rollback, and no-code-execution protections to filesystem-backed resource handlers; verify unmanaged files and executable resource content remain untouched.
- [x] 3.3 Add migration and end-to-end tests for Claude Code, Codex, and Pi project Skill activations across both CLI entrypoints; verify links and journal recovery are identical where adapter support exists.

## 4. Canonical SubAgent lifecycle

- [x] 4.1 Define versioned canonical SubAgent storage, schema validation, and registry discovery under the Agent Manager data root; verify malformed definitions and missing Skill references produce actionable errors.
- [x] 4.2 Add SubAgent list, show, and validate command surfaces with JSON output; verify canonical details remain independent of target-native renderings.
- [x] 4.3 Implement target-specific SubAgent inspection and render plans through Claude Code, Codex, and Pi adapters; verify every unsupported field or capability is surfaced explicitly.
- [x] 4.4 Implement confirmed, conflict-protected SubAgent installation and removal for adapters with a filesystem representation; verify a successful operation is journaled, undoable, and never overwrites unmanaged content without force confirmation.
- [x] 4.5 Add fixtures and end-to-end tests for successful installation, unsupported capabilities, missing referenced Skills, conflicts, rollback, and undo; verify `go test ./...` passes.

## 5. Shared Memory provider foundation

- [x] 5.1 Define versioned Memory provider configuration, non-secret configuration references, user/project scopes, and read/write/search capability models; verify sensitive values are redacted from output and journal evidence.
- [x] 5.2 Select one feasible provider adapter after documenting local integration constraints; implement provider capability discovery without enabling network access by default, and verify an unavailable provider yields actionable status.
- [x] 5.3 Implement explicit provider configuration and `memory status` output with per-agent capability mapping; verify it distinguishes configured, available, unavailable, and unsupported states.
- [x] 5.4 Implement explicit knowledge-promotion flow with scope and confirmation requirements; verify ordinary resource operations and agent conversation data never trigger provider writes.
- [x] 5.5 Add provider, adapter-mapping, and no-implicit-write tests using a fake or local test provider; verify `go test ./...` passes without external credentials or network access.

## 6. Release verification

- [x] 6.1 Run formatting, static checks, and the complete Go test suite; verify all commands succeed from a clean checkout without new mandatory dependencies.
- [x] 6.2 Exercise documented migration, inventory, Skill lifecycle, SubAgent, and Memory-status workflows in end-to-end fixtures; verify all mutations remain previewed, confirmed, journaled, and reversible where applicable.
- [x] 6.3 Review every delta specification and the README against the shipped CLI behavior; verify backward-compatibility limits, Pi capability caveats, provider prerequisites, and the Issue #3 handoff boundary are documented.
