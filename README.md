# Agent Manager

**English** · [简体中文](README.zh-CN.md)

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
go test ./...
go vet ./...
openspec validate add-local-web-console --strict
openspec doctor
```

The Web console assets are checked in under `internal/webconsole/ui` so a Go
binary does not need Node.js or network access at runtime. To change the React
console or installation UI, use the pinned dependencies and rebuild those embedded assets:

```text
cd web
npm ci
npm test
npm run build
```

The frontend toolchain follows the pinned Vite/Vitest engine range: Node.js
22.12+ on the 22.x line, 24.x, or 26+.

The build preserves both embedded clients: the console in `internal/webconsole/ui`
and the installation UI in `internal/webui/dist`. Their source entry points are
`web/index.html` and `web/install/index.html`, respectively.

## Configuration

Agent Manager reads a small YAML file with the Skill library location:

```yaml
library: ~/.agents/skills
```

Pass it with `--config <path>`; without it the default `~/.agents/skills` is used.

The legacy Memory foundation optionally accepts one file-backed provider with
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

For owned canonical records, explicitly select `structured-local` and use the
[Memory Gateway guide](docs/memory-gateway.md) for registration, confirmed
lifecycle operations, bounded search and read-only role context. Status separates
requested text search from unsupported implementation; existing text promotion
remains available.

## Getting started

```text
agent-manager init --yes            # verify CLI and install the global Operator skill
agent-manager web --project .      # open the local Web console for this repository
agent-manager search <query>        # find Skills in the library
agent-manager select --project .    # interactive search, multi-select, and activation
agent-manager add <skill> --project . --target codex --yes
agent-manager install go-helper review --project . --target codex --yes
agent-manager webui --project .     # open the local installation UI; stop with Ctrl+C
agent-manager list --project .      # show managed/unmanaged/orphaned Skills
agent-manager agents --project . --json # inspect registered agent adapters
agent-manager subagents list        # list canonical SubAgent definitions
agent-manager subagents install <id> --project . --target claude-code --yes
agent-manager subagents remove <id> --project . --target claude-code --yes
agent-manager memory status         # inspect shared Memory provider availability
agent-manager memory promote --scope user --knowledge "..." --yes
agent-manager memory promote --scope project --lessons .agents/tasks/task-1/lessons.yaml --yes
agent-manager task init --id task-1 --summary "..." --project .
agent-manager artifacts list task-1 --project .
agent-manager artifacts save task-1 ./out/plan.yaml --project . --yes
agent-manager artifacts validate .agents/tasks/task-1/intent.yaml
agent-manager roles --json
agent-manager eval list
agent-manager eval run java-backend --agent-label codex --candidate-dir ./eval-responses/codex
```

`skill-manager` remains a temporary compatibility alias and emits a migration notice. Use `agent-manager` in new automation.

## Commands

| Command | Purpose |
|---|---|
| `init` | Verify the CLI and install or update the minimal global Operator skill (guarded, confirmed) |
| `web` | Start the authenticated local Web console on `127.0.0.1` |
| `search <query>` | Search library skill identifiers, descriptions, bodies, and tags |
| `recommend` | Detect the project stack statically and rank matching skills (read-only) |
| `memory status` | Show configured Memory provider availability and per-agent capability mappings (read-only) |
| `memory promote` | Explicitly append confirmed text or typed lessons to the configured provider |
| `select` | Interactive workflow: search, choose skills, choose targets, confirm |
| `add <skill>` | Activate a library skill for selected target agents |
| `install <skill-id>...` | Preview and install explicitly selected library Skills in one operation |
| `webui` | Serve the local project installation interface on loopback |
| `list` | Inventory project skills with their status |
| `remove <skill>` | Remove one managed project link |
| `adopt <skill>` | Move an eligible project skill into the library and link it back |
| `fork <skill>` | Replace a managed link with an independent project-local copy |
| `doctor` | Report invalid library entries, orphaned links, unsupported agents, and Git tracking state |
| `reconcile` | Repair orphaned managed links after a library move (confirmed) |
| `undo` | Restore the pre-operation state of the latest journaled operation |
| `delete <skill>` | Delete an eligible library skill (requires `--force`, refuses by default) |
| `agents` | Inventory registered and unsupported local agent locations (read-only) |
| `roles` | List agent-neutral planner/implementer/reviewer/verifier contracts |
| `roles bind` | Check a role contract against a declared agent adapter |
| `task init` | Create a task and its initial `intent` artifact |
| `artifacts list` | List the versioned artifacts belonging to a task |
| `artifacts save` | Validate and save a stage artifact into an existing task |
| `artifacts show` | Inspect one artifact as YAML |
| `artifacts validate` | Validate an artifact without executing its content |
| `artifacts render` | Render an artifact as human-readable Markdown |
| `eval list` | List local evaluation suites |
| `eval run` | Run deterministic rule/verifier cases and persist results |
| `eval compare` | Report regressions and improvements between two result files |
| `policies validate/list/show` | Validate and inspect local versioned AgentPolicy files |
| `runs list/show/events/kill` | Inspect local AgentRuns, snapshots, approvals, and audit events; request supported termination |
| `approvals list/show/approve/reject/expire` | Inspect approval requests and record human decisions |
| `subagents list` | List canonical SubAgent definitions (read-only) |
| `subagents show <id>` | Show one canonical SubAgent definition (read-only) |
| `subagents validate [id]` | Validate all or one canonical SubAgent definition (read-only) |
| `subagents install <id>` | Render and install a SubAgent for Claude Code or Codex (guarded, confirmed, journaled) |
| `subagents remove <id>` | Remove a managed SubAgent representation (guarded, confirmed, journaled) |

## Local Web Console

Start a one-project local session with:

```text
agent-manager web --project . --port 0
```

`--project` is optional; without it, register a project from the console. The
default port `0` asks the operating system for an available port. The command
always binds to IPv4 loopback (`127.0.0.1`) and prints an entry URL whose
`#session=` fragment contains a random, process-lifetime token. The browser
moves that token into tab-scoped `sessionStorage` and removes it from the
address bar. API requests send it as a bearer token, require the exact current
host and same origin for mutations, reject cross-site fetches, and grant no
CORS access. Stop the command to discard the session and its pending plans.

The console registers one canonical project per process and exposes only the
supported inventory, catalog, diagnostic, and operation APIs; it has no
caller-selected file-read endpoint. Activation, removal, and available Undo
actions each show a server-owned plan that expires after ten minutes. Undo is
limited to console-managed project locations; an operation that targets the
shared Skill library or another unmanaged path remains visible without an Undo
action. The operator must confirm execution separately, and replacement
requires an additional explicit force confirmation. All successful mutations
use the existing lifecycle or journal service.

Every Skill/resource filesystem-mutating command previews its plan, requires confirmation (`--yes` or an interactive prompt), records a reversible filesystem operation-journal entry, and leaves unmanaged directories, ordinary files, and unexpected links untouched by default. `artifacts save` separately confirms a validated task-artifact write; `memory promote` separately confirms an append to provider-owned data. Neither operation creates a filesystem operation-journal entry or is reversible through `undo`. No Skill, SubAgent, status, inventory, or list workflow copies resource content or agent conversation data into Memory.

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

When activation would replace an existing unmanaged path, Agent Manager refuses by default. To replace it, select the conflict strategy and supply force confirmation:

```text
agent-manager add <skill> --project . --target codex --conflict replace --force --yes
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

Managed links are machine-local. Committing them would make activation portable, which conflicts with the local-only model, so Agent Manager reports their Git tracking state:

```text
agent-manager doctor --project .                    # show tracked/ignored/would-be-tracked links
agent-manager doctor --project . --update-gitignore --yes  # add exact scoped ignores
```

`doctor` only ever appends the exact paths of managed links it owns; it never rewrites unrelated tracking rules.

## Recovery

- `agent-manager undo --project . --yes` — revert the latest journaled project operation.
- `agent-manager doctor` — diagnose invalid library entries, orphaned links, and unsupported agents.
- `agent-manager reconcile --project . --yes` — relink orphaned managed links after the library moved (uses journal ownership and rolls back on failure).

## Recommend

`recommend` is a read-only command that detects the current project's technology stack from static markers and ranks matching skills from the library. It never executes project code, installs dependencies, uses the network, or modifies any file:

```text
agent-manager recommend                 # evaluate the current directory
agent-manager recommend --project ../service
agent-manager recommend --project . --json
```

Detected technologies come from the documented marker vocabulary: `go.mod`, `package.json`, `tsconfig.json`, `pom.xml`, `build.gradle`, `build.gradle.kts`, `pyproject.toml`, `Cargo.toml`, `Gemfile`, `Dockerfile`, Compose files, and the `.claude`, `.codex`, and `.agents` directories. The technology vocabulary covers Go, Node.js, TypeScript, Maven, Gradle, Python, Rust, Ruby, Docker, Compose, Express, NestJS, React, Next.js, Spring Boot, Django, FastAPI, Rails, PostgreSQL, MySQL, Redis, MongoDB, and the supported agents. Matching skills are ranked by exact companion tags first, then identifier and description text, then body text; each recommendation explains why it matched. Traversal skips dependency and build directories and directory soft links, reads at most 1 MiB per marker, and stops after 10,000 entries with `scanComplete: false`.

Monorepos report each independently marked sub-project as its own scope, so unrelated services never blend into one stack. Scopes without recognized markers return `insufficient_evidence` with a pointer to catalog search; stacks with no matching skill return `no_catalog_match` with guidance to add technology tags to companion metadata.

## Cross-agent artifacts and evaluation

Agent Manager stores task artifacts under `.agents/tasks/<task-id>/`:

```text
intent.yaml -> spec.yaml -> plan.yaml -> implementation.yaml
                                      -> verification.yaml
                                      -> lessons.yaml
```

Every artifact uses the `v1` envelope (`version`, `kind`, `id`, `created_at`,
project/source/links metadata). Parsing is independent of an agent adapter and
unknown future fields are retained during validation, rendering, and writes.
The six canonical kinds are `intent`, `spec`, `plan`, `implementation`,
`verification`, and `lessons`. Lessons have typed `decision`, `constraint`,
`lesson`, and `known_issue` items with scope and confidence. They are never
written to Memory automatically; `memory promote --lessons <path>` explicitly
formats and confirms a lessons artifact before appending it to the configured
provider.

Role contracts are agent-neutral and declare required input artifacts, output
artifacts, filesystem/shell/network permissions, and adapter capability gaps.
The built-in roles are planner, implementer, reviewer, and verifier. `roles
bind` reports unsupported runtime permissions until a runtime integration can
verify them; resource placement support does not prove execution permission.

The task context resolver assembles bounded root `AGENTS.md`/`CLAUDE.md`
guidance, selected Skills, role input artifacts, and optionally
search results from an injected read/search-capable Memory provider. It does
not invoke an agent or write to Memory. An explicitly selected Skill with an
advisory compatibility mismatch is included with a warning. The `roles context`
command accepts a task ID, role, and agent, then emits a JSON handoff containing the
role's canonical input artifacts and its binding gaps. An external agent or
human can produce the next artifact and save it with `artifacts save`.

The local workflow is `intent` → `spec` → planner context → `plan` →
implementer context → `implementation` → verifier context → `verification`.
The CLI test exercises each handoff and save step without launching an agent.

The local eval harness stores cases under `evals/<suite>/cases/<case-id>/` and
results under `.agent-manager/evals/` (ignored local runtime state). The first
runner is deterministic and rule-based: `eval run <suite> --candidate-dir <dir>`
scores supplied `<case-id>.md` responses; a missing response is partial. The
directory is required, and `--agent-label` and `--config-version` label externally
generated responses rather than launching an agent. Each case provides a
canonical intent artifact for producing those responses. The harness captures case
status, score, evidence, agent label, configuration version, timestamp, and
duration. `eval compare` reports regressions, improvements, and newly added
cases instead of relying on an LLM judge alone. The
planned baseline is documented in [evals/README.md](evals/README.md), with a
small runnable `java-backend` fixture suite included.

## Runtime Policy and Governance

Versioned `AgentPolicy` files describe tool rules, network and credential
scopes, SubAgent limits, budgets, approval requirements, and termination
conditions. Validate a policy with:

```sh
agent-manager policies validate ./policy.yaml
```

User-level policies for `list` and `show` live under
`os.UserConfigDir()/agent-manager/policies/`.

`agent-manager runs` inspects local run snapshots and audit events, while
`agent-manager approvals` records manual approval decisions. Current directory
adapters do not enforce runtime policies or implement runtime control, so
unsupported requirements and termination requests are reported explicitly.
Approval commands record the operator's decision; they do not execute an action
or resume an agent. Agent Manager does not launch agents. See
[Runtime Policy and Governance](docs/runtime-policy-governance.md) for the
policy and audit contract.

## Compatibility and migration boundary

`skill-manager` remains a compatibility alias for existing Skill workflows and
prints a migration notice; it is not a separate implementation. Versioned
configuration, companion metadata, project links, and legacy operation journals
are read and normalized in memory without an automatic rewrite. Project links
remain machine-local and are not a portable project manifest. New SubAgent and
Memory workflows are documented under `agent-manager` and should not be assumed
to have legacy-script argument compatibility.

The artifact and eval primitives are deliberately local and do not spawn
autonomous agents, synchronize full conversations, fetch remote data, or act as
a distributed workflow engine. They consume the existing guarded resource and
Memory boundaries without weakening confirmation, no-code-execution,
no-network-default, journal, or provider-ownership rules.

## Roadmap

- **Autonomous delegation** — future work can consume the versioned artifact
  and role contracts without inventing a runtime-specific task-state format.
- **React WebUI** — a post-CLI web interface over the same local services.
- **Wails** — desktop packaging of the WebUI.
