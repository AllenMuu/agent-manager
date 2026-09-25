# Agent Manager

Agent Manager governs local agent resources across a shared Skill library, agent-wide locations, and individual repositories. Its first managed resource domain is Skills: it discovers Skills from a configurable local library, activates them per project through machine-local soft links, and keeps filesystem mutations guarded, journaled, and reversible.

Agent Manager never executes managed-resource code and never installs dependencies.

## How activation works

- **Skill library** — the authoritative local collection of reusable skills (default `~/.agents/skills`, configurable). Each skill is a directory containing `SKILL.md` and optional companion metadata (`.skill-manager.yaml`).
- **Project activation** — selected skills appear in a repository's agent-recognized skill location (`.claude/skills/<id>`, `.codex/skills/<id>`, or `.pi/skills/<id>`) as absolute soft links into the library. Activation is machine-local by design: it is not a committed manifest and is not portable between machines.
- **Target agents** — Claude Code, Codex, and Pi have registered Skill adapters. Pi supports Skill placement only; its native layout has no verified SubAgent definition format, so SubAgent installation for Pi is reported as unsupported without a write. A project can activate the same Skill for multiple agents.
- **Global baseline** — a deliberately small set of resources installed agent-wide. Only `init` touches global locations; project commands never do.

## Build

```text
go build ./cmd/agent-manager
```

## Configuration

Agent Manager reads a small YAML file with the Skill library location:

```yaml
library: ~/.agents/skills
```

Pass it with `--config <path>`; without it the default `~/.agents/skills` is used.

The local Memory foundation optionally accepts one file-backed provider with
read-only discovery. Only an external reference is stored; the referenced file
is never created or written during discovery, and writes require the explicit
confirmed `memory promote --scope <user|project> --knowledge <text> --yes`
command (or an interactive confirmation). Network providers are not enabled
by default and no management command fetches, starts, or configures one:

```yaml
memory:
  version: v1
  id: local-memory
  provider: file
  configuration:
    kind: file
    name: /Users/me/.local/share/agent-manager/memory.json
  scopes: [user, project]
  capabilities: [read, search]
```

The file must already exist, be an absolute path to a direct regular local file,
and not be a symlink. Discovery rejects missing, replaced, non-regular, or
inaccessible files with an actionable status; it never creates the file.
Configure a non-secret provider reference rather than placing credentials in
this file. The initial release does not provide a native Memory integration for
Claude Code, Codex, or Pi, so `memory status` reports those mappings as
unsupported even when the local provider is available.

## Getting started

```text
agent-manager init                 # verify CLI and install the global Operator skill
agent-manager search <query>        # find Skills in the library
agent-manager select --project .    # interactive search, multi-select, and activation
agent-manager add <skill> --project . --target codex
agent-manager list --project .      # show managed/unmanaged/orphaned Skills
agent-manager agents --project . --json # inspect registered agent adapters
agent-manager subagents list        # list canonical SubAgent definitions
agent-manager subagents install <id> --project . --target claude-code --yes
agent-manager subagents remove <id> --project . --target claude-code --yes
agent-manager memory status         # inspect shared Memory provider availability
agent-manager memory promote --scope user --knowledge "..." --yes
```

`skill-manager` remains a temporary compatibility alias and emits a migration notice. Use `agent-manager` in new automation.

## Commands

| Command | Purpose |
|---|---|
| `init` | Verify the CLI and install or update the minimal global Operator skill (guarded, confirmed) |
| `search <query>` | Search library skill identifiers, descriptions, bodies, and tags |
| `recommend` | Detect the project stack statically and rank matching skills (read-only) |
| `memory status` | Show configured Memory provider availability and per-agent capability mappings (read-only) |
| `memory promote` | Explicitly append confirmed knowledge to the configured provider |
| `select` | Interactive workflow: search, choose skills, choose targets, confirm |
| `add <skill>` | Activate a library skill for selected target agents |
| `list` | Inventory project skills with their status |
| `remove <skill>` | Remove one managed project link |
| `adopt <skill>` | Move an eligible project skill into the library and link it back |
| `fork <skill>` | Replace a managed link with an independent project-local copy |
| `doctor` | Report invalid library entries, orphaned links, unsupported agents, and Git tracking state |
| `reconcile` | Repair orphaned managed links after a library move (confirmed) |
| `undo` | Restore the pre-operation state of the latest journaled operation |
| `delete <skill>` | Delete an eligible library skill (requires `--force`, refuses by default) |
| `agents` | Inventory registered and unsupported local agent locations (read-only) |
| `subagents list` | List canonical SubAgent definitions (read-only) |
| `subagents show <id>` | Show one canonical SubAgent definition (read-only) |
| `subagents validate [id]` | Validate all or one canonical SubAgent definition (read-only) |
| `subagents install <id>` | Render and install a SubAgent for Claude Code or Codex (guarded, confirmed, journaled) |
| `subagents remove <id>` | Remove a managed SubAgent representation (guarded, confirmed, journaled) |

Every filesystem-mutating command previews its plan, requires confirmation (`--yes` or an interactive prompt), records a reversible filesystem operation-journal entry, and leaves unmanaged directories, ordinary files, and unexpected links untouched by default. `memory promote` is the exception: it separately confirms an append to provider-owned data, does not create a filesystem operation-journal entry, and is not reversible through `undo`; recovery and retention semantics are defined by the selected provider. No Skill, SubAgent, status, inventory, or list workflow copies resource content or agent conversation data into Memory.

## Canonical SubAgents

SubAgent definitions are read from `<agent-manager-root>/subagents` as versioned,
agent-neutral YAML. By default, the root is `~/.agents`; use `--root <path>` to
inspect another root. Skill references are checked against the configured Skill
library (the default `~/.agents/skills`, or `--config <path>`/`--library <path>`).
These inspection commands never render or modify target-native agent files.

The machine-readable `subagents list --json` response is an envelope so registry
diagnostics are not lost:

```json
{
  "definitions": [],
  "diagnostics": [
    {"id": "reviewer", "path": "/.../subagents/reviewer.yaml", "message": "..."}
  ]
}
```

`definitions` and `diagnostics` are always arrays. `subagents validate --json`
returns `{ "valid": true|false, "diagnostics": [...] }`; validation diagnostics
include `id` when it can be recovered from an invalid definition.
Each canonical definition always includes `skills` and `requiredCapabilities`
arrays, plus `compatibility: {"agents": [...]}`; empty values are encoded as
empty arrays rather than omitted or `null`.

SubAgent installations render the canonical definition through the selected
adapter. Claude Code and Codex have verified filesystem representations;
Pi reports an explicit unsupported result because no native SubAgent format is
verified. Canonical definitions remain the source of truth and are not
rewritten into target-native files during inspection.

## Conflict handling

When activation would replace an existing unmanaged path, Skill Manager refuses by default. To replace it, select the conflict strategy and supply force confirmation:

```text
agent-manager add <skill> --project . --target codex --conflict replace --force
```

The replacement is journaled like any other operation, so `undo` restores the previous content.

## Compatibility warnings

Skills may declare target-agent compatibility in `.skill-manager.yaml`:

```yaml
compatibility:
  - codex
```

Installing for an undeclared target produces a warning in the plan preview. A warning never blocks an explicit installation.

## Git guidance

Managed links are machine-local. Committing them would make activation portable, which conflicts with the local-only model, so Skill Manager reports their Git tracking state:

```text
agent-manager doctor --project .                    # show tracked/ignored/would-be-tracked links
agent-manager doctor --project . --update-gitignore  # offer exact scoped ignores after confirmation
```

`doctor` only ever appends the exact paths of managed links it owns; it never rewrites unrelated tracking rules.

## Recovery

- `agent-manager undo` — revert the latest journaled operation.
- `agent-manager doctor` — diagnose invalid library entries, orphaned links, and unsupported agents.
- `agent-manager reconcile` — relink orphaned managed links after the library moved (uses journal ownership, confirmed, rolled back on failure).

## Recommend

`recommend` is a read-only command that detects the current project's technology stack from static markers and ranks matching skills from the library. It never executes project code, installs dependencies, uses the network, or modifies any file:

```text
agent-manager recommend                 # evaluate the current directory
agent-manager recommend --project ../service
agent-manager recommend --project . --json
```

Detected technologies come from the documented marker vocabulary: `go.mod`, `package.json`, `tsconfig.json`, `pom.xml`, `build.gradle`, `build.gradle.kts`, `pyproject.toml`, `Cargo.toml`, `Gemfile`, `Dockerfile`, Compose files, and the `.claude`, `.codex`, and `.agents` directories. The technology vocabulary covers Go, Node.js, TypeScript, Maven, Gradle, Python, Rust, Ruby, Docker, Compose, Express, NestJS, React, Next.js, Spring Boot, Django, FastAPI, Rails, PostgreSQL, MySQL, Redis, MongoDB, and the supported agents. Matching skills are ranked by exact companion tags first, then identifier and description text, then body text; each recommendation explains why it matched. Traversal skips dependency and build directories and directory soft links, reads at most 1 MiB per marker, and stops after 10,000 entries with `scanComplete: false`.

Monorepos report each independently marked sub-project as its own scope, so unrelated services never blend into one stack. Scopes without recognized markers return `insufficient_evidence` with a pointer to catalog search; stacks with no matching skill return `no_catalog_match` with guidance to add technology tags to companion metadata.

## Compatibility and Issue #3 boundary

`skill-manager` remains a compatibility alias for existing Skill workflows and
prints a migration notice; it is not a separate implementation. Versioned
configuration, companion metadata, project links, and legacy operation journals
are read and normalized in memory without an automatic rewrite. Project links
remain machine-local and are not a portable project manifest. New SubAgent and
Memory workflows are documented under `agent-manager` and should not be assumed
to have legacy-script argument compatibility.

This release hands Issue #3 a stable local control-plane boundary: canonical
resources, capability-declaring adapters, guarded filesystem placement, and an
explicit local Memory provider/status/promotion API. Issue #3's autonomous
delegation workflow, conversation synchronization, remote/network provider
adapters, vector-database behavior, and evaluation-harness/artifact work remain
deferred. They must consume these boundaries without weakening confirmation,
no-code-execution, no-network-default, journal, or provider-ownership rules.

## Roadmap

- **Issue #3 follow-on** — autonomous delegation, conversation synchronization,
  remote Memory adapters, and evaluation artifacts over the shipped control
  plane (see the compatibility boundary above).
- **React WebUI** — a post-CLI web interface over the same local services.
- **Wails** — desktop packaging of the WebUI.
