## Purpose

Provides runtime-neutral policy decisions and a local governance boundary for Agent Manager. It distinguishes policy that can be represented from controls a runtime can enforce, so unsupported security controls are never presented as active protection.

## ADDED Requirements

### Requirement: Versioned canonical AgentPolicy
Agent Manager SHALL load and validate a versioned, runtime-neutral `AgentPolicy` containing tool allow/deny rules, approval-required action categories, duration/cost/tool-call budgets, subagent limits, allowed/denied network domains, allowed/denied credential scopes, and termination triggers. It SHALL reject unsupported versions, malformed rules, and unrecognized security-relevant fields instead of silently dropping them. Deny rules SHALL take precedence over allow rules. Tools SHALL be denied unless explicitly allowed. Domain rules SHALL match complete hostnames exactly; a subdomain requires its own entry. Credential scopes SHALL match exact canonical scope identifiers.

#### Scenario: Validate a complete policy
- **WHEN** an operator validates a supported policy containing every control category
- **THEN** Agent Manager returns the normalized policy and its stable identity without discarding a configured control

#### Scenario: Reject an unsupported policy version or field
- **WHEN** a policy uses an unsupported version or contains an unrecognized security-relevant field
- **THEN** validation fails with a field-specific diagnostic and no policy is loaded

#### Scenario: Apply safe allow and domain matching
- **WHEN** a tool or network host is evaluated against a policy
- **THEN** an explicit deny takes precedence, a tool is allowed only by an explicit allow rule, and a domain is allowed only by an exact hostname rule

### Requirement: Deterministic policy evaluation API
The system SHALL expose a runtime-neutral policy evaluation boundary with pre-event decisions and post-event evaluations. It SHALL normalize runtime-specific input into stable event categories, including run start, tool request/completion, network request, credential request, subagent spawn, budget update, run completion/failure/termination, and anomaly candidates such as unexpected tool/network access, retry storms, progress stalls, subagent spawn spikes, budget exhaustion, policy violations, and unexpected termination. Decisions SHALL be `ALLOW`, `DENY`, `REQUIRE_APPROVAL`, or `ALLOW_WITH_WARNING`; every deny and approval decision SHALL contain a stable machine-readable reason code. Evaluation SHALL be deterministic for the same normalized event, policy snapshot, and budget state, and testable without launching an LLM runtime.

#### Scenario: Normalize an adapter event
- **WHEN** an adapter submits a runtime-specific tool request
- **THEN** the policy boundary evaluates its normalized tool event without depending on the adapter's event type

#### Scenario: Repeat the same policy evaluation
- **WHEN** the same normalized event and policy snapshot are evaluated with the same budget state
- **THEN** the decision and reason code are identical

#### Scenario: Deny without an LLM runtime
- **WHEN** a unit test evaluates a denied event
- **THEN** the engine returns `DENY` and its stable reason code without launching a runtime

#### Scenario: Evaluate a completed action against its original decision
- **WHEN** Agent Manager evaluates a completed tool or network action
- **THEN** the completion references the persisted request audit, uses that pre-action decision, and does not re-evaluate the action against post-action budget counters

### Requirement: Immutable policy snapshot per AgentRun
At run creation, Agent Manager SHALL resolve the effective policy and persist a snapshot containing policy ID, version, canonical content hash, and resolution time. Policy edits SHALL apply to new runs by default and SHALL NOT change a historical run's snapshot. Audit and evaluation evidence SHALL be able to identify the snapshot that produced a decision.

#### Scenario: Start a run with a resolved policy
- **WHEN** a run is created with a valid policy
- **THEN** the run record contains the policy ID, version, content hash, and resolution time

#### Scenario: Change a policy after a run starts
- **WHEN** the source policy changes after a run snapshot has been persisted
- **THEN** the historical run retains its original snapshot and a later run resolves the updated policy

#### Scenario: Link decision evidence to a snapshot
- **WHEN** Agent Manager persists a policy decision or evaluation result
- **THEN** the evidence identifies the run and its immutable policy snapshot

### Requirement: Reference policy snapshots from task artifacts
Task artifacts SHALL be able to carry an optional stable reference to the AgentRun and its policy ID, version, and snapshot hash. Existing artifacts without a policy reference SHALL remain valid, and artifact validation, rendering, and round trips SHALL preserve a supplied reference.

#### Scenario: Attach a policy snapshot reference to verification
- **WHEN** an operator saves a verification artifact associated with a governed run
- **THEN** the artifact can retain the run ID and policy snapshot identity without embedding or mutating the run record

#### Scenario: Read an artifact without a policy reference
- **WHEN** an existing task artifact has no governance metadata
- **THEN** it remains valid and renders as before

### Requirement: Canonical approval lifecycle
`REQUIRE_APPROVAL` SHALL produce a serializable approval request linked to an AgentRun, the requested action, and the persisted request audit that recorded the decision. Approval creation SHALL verify that the referenced audit records `REQUIRE_APPROVAL`, preserving any budget evaluation made with the original request. An approval SHALL transition from pending to approved, rejected, or expired; only a matching approved request permits a completion event, while rejected, expired, pending, or absent approval SHALL be treated as a violation. An adapter with native pause/resume support MAY map that behavior to the canonical request. A runtime without that support SHALL fail safely and report a capability mismatch. Approval creation and transitions SHALL be auditable.

#### Scenario: Require approval for a sensitive action
- **WHEN** a policy requires approval for an action
- **THEN** Agent Manager persists a pending canonical approval request before the action can proceed

#### Scenario: Resolve a pending approval
- **WHEN** an operator approves, rejects, or expires a pending request
- **THEN** the request records the terminal status and decision time, and only an approved request may continue

#### Scenario: Preserve the original budget decision during approval
- **WHEN** an approval request references an audit that denied the action because a configured budget was exhausted
- **THEN** Agent Manager rejects the approval request without pausing the runtime or creating an approval

#### Scenario: Runtime cannot suspend for approval
- **WHEN** approval is mandatory and the selected runtime cannot pause and resume
- **THEN** preflight fails with an explicit capability mismatch and the action is not executed

### Requirement: Explicit runtime governance capabilities
Runtime governance capabilities SHALL be declared separately from resource placement and role permissions. Before a run, Agent Manager SHALL compare configured policy requirements with the selected adapter's declared enforcement capabilities. Unsupported mandatory controls SHALL fail closed; unsupported optional controls SHALL produce explicit warnings. Adapter translation SHALL NOT silently omit a security-relevant policy field. Existing directory adapters SHALL report runtime enforcement as unsupported until they expose verified execution hooks.

#### Scenario: Bind a policy to an unsupported runtime
- **WHEN** a policy requires a control that the selected adapter does not declare
- **THEN** preflight fails before execution and names the missing control

#### Scenario: Continue with an optional unsupported control
- **WHEN** an optional policy control cannot be enforced by the selected adapter
- **THEN** preflight reports an explicit warning and identifies the unsupported control

#### Scenario: Inspect existing directory adapters
- **WHEN** an operator validates a policy against a current Claude Code, Codex, or Pi directory adapter
- **THEN** resource placement support is not treated as runtime enforcement support

### Requirement: Enforce budgets and subagent limits
The policy engine SHALL evaluate elapsed duration, accumulated cost, tool-call count, concurrent subagents, and total spawned subagents against the AgentRun snapshot. A request that exceeds a configured limit SHALL be denied with a stable reason code and SHALL emit a budget or subagent governance event. Configured termination triggers SHALL produce a termination request through the run control boundary.

#### Scenario: Exceed a tool-call or time budget
- **WHEN** a run requests an action after its configured duration or tool-call budget is exhausted
- **THEN** the engine denies the action with a stable budget reason code

#### Scenario: Exceed cost or subagent limits
- **WHEN** a budget update or subagent request would exceed the configured cost, concurrent, or total limit
- **THEN** the engine rejects the request and records the corresponding governance event

### Requirement: Local run inventory and confirmed termination
Agent Manager SHALL provide one run inventory abstraction with list and inspect operations. Run records, policy snapshots, approvals, and audit events SHALL be persisted locally in a user-level state directory shared across projects. Updates to that shared state SHALL serialize the complete read-modify-write transaction across processes so concurrent CLI processes or Store instances cannot lose records. Termination SHALL be capability-aware: unsupported termination SHALL fail explicitly, and Agent Manager SHALL report a run as terminated only after the underlying runtime confirms termination. A confirmed termination reason SHALL be persisted and audited.

#### Scenario: List and inspect runs across projects
- **WHEN** an operator lists or inspects runs from any project
- **THEN** the user-level inventory returns matching run records with their runtime, policy snapshot, status, approvals, and violations

#### Scenario: Request termination from an unsupported adapter
- **WHEN** an operator requests termination but the adapter does not support run control
- **THEN** Agent Manager returns an explicit unsupported-capability error and does not mark the run terminated

#### Scenario: Runtime confirms or refuses termination
- **WHEN** a runtime control reports a termination outcome
- **THEN** Agent Manager records `terminated` only for a confirmed outcome and persists the supplied reason

### Requirement: Structured privacy-safe governance audit
Policy-relevant events SHALL be stored locally as structured machine-readable records linked to run ID and policy snapshot. Records SHALL identify timestamp, normalized category, actor/runtime, resource, decision, and reason code where applicable. Credential and token values SHALL never be stored; event metadata SHALL be restricted or redacted before persistence. Audit data SHALL remain available as deterministic input to local evaluation.

#### Scenario: Persist a policy decision
- **WHEN** the engine allows, denies, warns, or requests approval for an event
- **THEN** it persists a structured record linked to the run and policy snapshot

#### Scenario: Record an event containing sensitive metadata
- **WHEN** event metadata contains a credential or token value
- **THEN** persistence omits or redacts that value while retaining safe decision evidence

#### Scenario: Read an audit record
- **WHEN** an operator inspects a run's audit history
- **THEN** records are returned in deterministic order without executing event content or exposing secrets

### Requirement: Normalized anomaly-event vocabulary
The governance event model SHALL leave room for anomaly evaluation with stable categories for unexpected tool use, unexpected network access, credential access, retry storms, progress stalls, subagent spawn spikes, budget exhaustion, policy violations, and unexpected termination. P0/P1 enforcement and evaluation SHALL NOT require an LLM-as-judge.

#### Scenario: Classify a governance anomaly candidate
- **WHEN** an adapter or evaluator reports one of the defined anomaly candidates
- **THEN** Agent Manager stores or evaluates it as a normalized event linked to the run

#### Scenario: Run governance evaluation locally
- **WHEN** a governance event is evaluated
- **THEN** deterministic local rules are used without an LLM-as-judge or network access

### Requirement: Inspect policies, runs, and approvals through the CLI
The CLI SHALL provide policy list/show/validate, run list/show/kill, and approval inspection/decision workflows. Validation and inspection SHALL be read-only. Approval and termination commands SHALL use the same capability and audit rules as the domain API and SHALL report unsupported operations explicitly.

#### Scenario: Validate and inspect a policy
- **WHEN** an operator lists, shows, or validates a policy file
- **THEN** the CLI reports stable policy identity, validation diagnostics, and enforcement capability findings without changing the policy

#### Scenario: Inspect or decide an approval
- **WHEN** an operator lists or resolves a pending approval
- **THEN** the CLI displays the linked run/action and records the decision through the canonical approval lifecycle

#### Scenario: Kill an inventoried run
- **WHEN** an operator runs the kill command for a run
- **THEN** the CLI reports confirmed termination or an explicit capability/runtime failure and never reports an unconfirmed kill as successful

### Requirement: Preserve non-executing, backward-compatible operation
Policy loading, evaluation, run inspection, audit inspection, and evaluation SHALL NOT launch an agent, execute policy/event/candidate content, access a network service, or install dependencies. Existing Skill Manager resource behavior and compatibility CLI behavior SHALL remain unchanged.

#### Scenario: Inspect untrusted policy or event data
- **WHEN** Agent Manager loads or renders a policy or event record
- **THEN** it treats the content as data and performs no execution or network access

#### Scenario: Use an existing Skill Manager workflow
- **WHEN** an operator invokes an existing resource workflow through either CLI entrypoint
- **THEN** the behavior remains compatible with the existing implementation
