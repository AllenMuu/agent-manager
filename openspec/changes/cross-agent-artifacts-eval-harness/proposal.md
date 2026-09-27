## Why

Agent Manager needs a runtime-neutral way to move task intent, plans, implementation results, verification, and lessons across humans, agents, and workflow stages. Explicit local artifacts and a deterministic evaluation harness provide that shared protocol while keeping agent execution under the caller's control.

## What Changes

- Define and validate versioned task artifacts with stable task relationships, unknown-field preservation, safe local storage, and inspectable CLI commands.
- Define agent-neutral planner, implementer, reviewer, and verifier role contracts with explicit artifact and capability requirements.
- Assemble bounded task context from repository guidance, explicitly selected Skills, role inputs, and optional read-only Memory search; report advisory compatibility mismatches.
- Add a local deterministic evaluation harness with versioned cases/results, candidate response checks, persisted run metadata, and regression comparison.
- Keep artifact writes explicit, Memory promotion separate, and evaluation free of agent invocation, network access, or candidate-code execution.

## Capabilities

### New Capabilities

- `cross-agent-artifact-protocol`: Versioned task artifacts, storage, validation, rendering, and role contracts.
- `task-context-assembly`: Bounded, read-only assembly of repository guidance, selected Skills, Memory results, and task artifacts.
- `local-evaluation-harness`: Local rule-based evaluation cases, persisted results, and deterministic regression comparison.

### Modified Capabilities

None.

## Impact

- Affects `internal/artifact`, `internal/role`, `internal/taskcontext`, `internal/eval`, and related command integrations under `internal/cli`.
- Adds local `evals/` fixtures and documentation; runtime eval results remain local under `.agent-manager/evals/`.
- Uses existing Go dependencies and does not add network, agent-execution, or dependency-install behavior.
