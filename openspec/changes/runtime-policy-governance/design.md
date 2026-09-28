## Context

See `proposal.md` for motivation and scope. The current `AgentAdapter` separates resource placement from runtime permissions; current Claude Code, Codex, and Pi adapters only manage directories and return no verified execution capabilities. `role.Bind` is a report-only preflight check, and the local eval harness reads supplied response files without invoking agents. There is no existing run lifecycle. Issue #4 therefore needs an additive governance boundary that can be exercised with deterministic mock runtimes while reporting the existing adapters as unsupported.

## Goals / Non-Goals

**Goals:**

- Make policy evaluation, run identity, approvals, and audit events deterministic and inspectable.
- Keep policy requirements separate from claims about what a runtime can enforce.
- Make run inventory and policy/audit state available across projects for the current user.
- Feed governance evidence into local evaluation and optional task-artifact references.

**Non-Goals:**

- Launching Claude Code, Codex, Pi, or any other agent.
- Installing runtime hooks or claiming enforcement by the current directory adapters.
- Building a distributed policy service, a scheduler, a secrets manager, or an anomaly-detection engine.
- Adding network access, external dependencies, or LLM-based evaluation.

## Decisions

### Keep policy evaluation independent from adapters

`internal/policy` owns the versioned policy model, strict YAML decoding, validation, canonical snapshot hashing, normalized event types, stable reason codes, and deterministic pre/post evaluation. It does not import adapter packages or invoke a runtime. `internal/adapter` declares a separate `GovernanceCapabilities` contract for tool interception, domain/credential restriction, approval pause/resume, budget and subagent control, event reporting, inventory, and termination. `internal/run` joins policy and adapter capability data during run preflight. Current directory adapters report every runtime governance capability as unsupported; their resource-placement capabilities do not imply enforcement.

An `enforcement` section makes capability requirements explicit. Configured controls are mandatory by default; a control listed as optional may continue with an explicit warning. A missing mandatory control blocks run creation. This avoids an implicit distinction between a represented rule and a rule that is actually enforced.

### Use fail-closed policy matching

An explicit deny wins over an allow. A tool is allowed only when listed in `tools.allow`; network hostnames and credential scopes match exactly after canonical normalization. Subdomains must be listed separately. Ordered policy lists are normalized before hashing so the same logical policy produces a stable SHA-256 snapshot identity.

### Persist user-level run records and typed audit events

`internal/run` owns `AgentRun`, immutable policy references, approval transitions, safe local storage, event history, and a `RuntimeController` interface. The default store is under `os.UserConfigDir()/agent-manager/runs`, shared across projects; tests and operators can supply an explicit state directory. Run IDs are validated, symlinked store roots and state files are rejected, and records are published atomically with restrictive file permissions where supported. A cross-process lock protects each complete read-modify-write transaction so separate CLI processes and Store instances do not overwrite each other's records. This state is separate from the reversible filesystem operation journal.

Audit records use typed fields for run, snapshot, event category, action/resource, decision, and reason. Arbitrary adapter metadata, credential scopes, decision details, and untrusted actor text are not persisted. Safe labels and stable reason codes are retained; there is no credential or token value field in an audit record.

The manager's `EvaluateAndRecord` operation is the canonical path for a normalized event: it evaluates against the run's immutable snapshot and persists the decision with matching run and policy identity. A completed tool/network action must reference its request audit; the manager reuses that pre-action decision so post-action budget counters cannot retroactively change the result. An approval must cite that same persisted `REQUIRE_APPROVAL` decision, including its already-evaluated budget outcome. A completion linked to the request is allowed only when the matching approval is approved; pending, rejected, expired, and absent approvals remain violations. An unlinked completion is rejected. A termination request is returned to run control and is not reported as complete unless the controller confirms it. Current adapters have no controller and return an explicit unsupported error. The CLI exposes policy list/show/validate, runs list/show/events/kill, and approvals list/show/approve/reject/expire; it does not expose a fake `runs start` command that could be mistaken for launching an agent. A CLI approval decision records operator intent and audit evidence but does not execute or resume a runtime action. Future execution adapters can call the domain API when they gain verified hooks.

### Preserve artifact and eval compatibility additively

Task artifacts gain an optional policy snapshot reference with run ID, policy ID, version, and hash; existing artifact kinds and files remain valid. `internal/eval` adds a governance-event verifier that reads local versioned event evidence and checks expected decisions, reason codes, and capability failures. Optional policy identity fields are added to new evaluation results without changing legacy result validation or comparison. Fixtures cover allow, deny, approval, and mandatory capability mismatch.

All evaluation inputs are data. The evaluation path remains local, deterministic, and non-executing.

## Risks / Trade-offs

- **Current adapters cannot intercept runtime events** → Declare their governance capabilities unsupported and reject mandatory controls before a future integration can claim execution; verify the policy engine with mock/test controllers.
- **A shared state directory can be replaced, redirected, or concurrently overwritten** → Reject symlinked store roots and state files, serialize cross-process read-modify-write transactions, use atomic writes, and test path replacement, permission failures, and competing Store instances.
- **Audit metadata can contain secrets** → Persist typed event fields and allowlisted evidence only; do not accept arbitrary adapter payloads into audit records.
- **Policy hashes can drift if serialization is unstable** → Hash a normalized typed representation with deterministic field/list ordering and test equivalent policies.
- **Adding fields can affect old artifact/eval consumers** → Keep new snapshot/result metadata optional and retain fixtures for pre-governance artifacts and eval results.

## Migration Plan

The change is additive. Existing resource adapters, task artifacts, evaluation suites/results, and both CLI entrypoints remain readable and retain their current behavior. Policy/run directories are created lazily under the user-level configuration directory. Rollback consists of reverting the binary/code; local run/audit records are left untouched and are not automatically deleted.
