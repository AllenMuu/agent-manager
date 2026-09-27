## Context

The repository already contains Go packages for versioned task artifacts, canonical roles, context resolution, and deterministic evaluations, plus local fixtures and CLI commands. This change formalizes their behavior and closes a validation gap in the common artifact envelope. See `proposal.md` and the capability specs for motivation and the observable contract.

The system is local-first. Task artifacts are project-owned files, eval cases are versioned engineering inputs, eval results are local runtime state, and Memory remains provider-owned. The harness must not become an agent execution runtime.

## Goals / Non-Goals

**Goals:**

- Keep artifact parsing independent of agent adapters while retaining unknown fields for forward compatibility.
- Keep filesystem access bounded to the selected project/task, suite, candidate, and result paths, with explicit path checks at storage boundaries.
- Keep the context resolver injectable and read-only so Skills, role inputs, and Memory can be assembled without coupling provider storage to an agent runtime.
- Make evaluation results deterministic and comparable by stable case identifiers and evidence.

**Non-Goals:**

- Spawning agents, executing candidate content, or interpreting native session formats.
- Automatically promoting lessons to Memory or automatically creating a baseline benchmark corpus beyond the documented starter set.
- Guaranteeing semantic quality from simple substring rule verifiers.

## Decisions

### Preserve artifact documents as YAML mappings

Parse artifacts into a map-backed document and validate the common envelope plus kind-specific fields. This preserves unknown fields through inspect, render, and save while allowing the protocol to evolve. Typed structs that discard unrecognized fields were rejected. Require project ownership, actor provenance, and a task relationship at validation time so a valid artifact is attributable and can be joined to a task.

### Keep storage operations behind a task store

The task store owns the deterministic `.agents/tasks/<task-id>/<kind>.yaml` layout and validates identifiers and path components before reads or writes. The CLI remains responsible for presenting and confirming an artifact-save plan. Reusing the Skill operation journal was rejected because task artifacts are explicit workflow inputs, not managed-resource activations, and their save contract is separate from Skill lifecycle undo.

### Model roles as contracts and inject integrations

Role definitions declare artifact inputs/outputs and permission requirements. Adapter binding compares requirements with verified runtime capabilities, separate from resource-placement capabilities, and returns unsupported details rather than rewriting a role. Context resolution receives Skill and Memory interfaces as inputs; it assembles bounded content but does not invoke agents or write to providers. Explicitly selected Skills are retained with a warning when advisory compatibility metadata omits the target. Embedding runtime-specific paths or provider storage in role definitions was rejected to keep the protocol agent-neutral.

### Run evaluations against supplied responses

The initial runner applies deterministic rule checks to externally supplied candidate response files and persists versioned result data. It requires a candidate directory so bundled examples cannot produce a misleading pass. `--agent-label` identifies the external producer and does not invoke that agent. An in-process agent runner was rejected because it would introduce network, credentials, hidden runtime state, and adapter lifecycle into a local regression harness. LLM-only judging was rejected because results need deterministic evidence and reproducibility.

### Compare by case identity and retain result evidence

Comparison uses stable case IDs, status, and score to classify regressions, improvements, additions, and unchanged cases. Candidate omissions are reported as regressions. Raw aggregate score alone was rejected because it hides which behavior changed; run records retain per-case evidence and configuration labels.

## Risks / Trade-offs

- [Substring rule checks can miss semantic regressions] → Keep evidence visible, describe the limits of the first verifier, and allow future verifier kinds without requiring them now.
- [Unknown fields can carry malformed extension values] → Preserve them as data while validating all fields that the current protocol owns; never execute artifact content.
- [Task or eval paths can be redirected through symlinks] → Validate direct files/directories and path components at each storage boundary.
- [Four starter cases are too small for meaningful benchmark claims] → Document the 20-case planned baseline and report additions separately from improvements.

## Migration Plan

1. Preserve the existing task artifact layout, role contracts, context bundle, case format, and result format as the initial v1 behavior.
2. Tighten envelope validation so all newly saved or inspected artifacts identify project root, actor, and task relationship; update fixtures to include those fields.
3. Keep existing v1 eval suites and result files readable. New v2 cases require canonical intent inputs and evidence points; generated results remain in ignored local state.
4. Roll back by removing the additive commands and packages; no Skill activation or provider-owned Memory data needs migration.
