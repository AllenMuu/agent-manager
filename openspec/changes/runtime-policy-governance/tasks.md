## 1. Canonical policy model

- [x] 1.1 Implement tool allow/deny policy; verify explicit allow, default deny, and deny precedence in unit tests.
- [x] 1.2 Implement approval-required action categories; verify each configured category yields `REQUIRE_APPROVAL` with a stable reason code.
- [x] 1.3 Implement duration, cost, and tool-call budgets; verify limit boundaries and over-budget denials in unit tests.
- [x] 1.4 Implement concurrent and total subagent limits; verify boundary and exceeded cases in unit tests.
- [x] 1.5 Implement canonical allowed/denied network domains with exact-host matching; verify subdomains require separate entries.
- [x] 1.6 Implement canonical allowed/denied credential scopes; verify exact scope matching and deny precedence.
- [x] 1.7 Implement versioned policy decoding, validation, and deterministic hashing; verify unsupported versions/fields fail without dropping controls.

## 2. Evaluation boundary and run snapshots

- [x] 2.1 Keep core policy evaluation independent of Claude/Codex/Pi event types; verify policy package tests use only normalized events.
- [x] 2.2 Normalize adapter-specific events before evaluation; verify equivalent runtime events produce the same normalized record.
- [x] 2.3 Make policy decisions deterministic for the same normalized event and snapshot; verify repeated inputs return identical decisions.
- [x] 2.4 Give every deny and approval decision a machine-readable reason code; verify all decision branches expose stable codes.
- [x] 2.5 Make policy evaluation testable without launching a real LLM runtime; verify the test suite uses no runtime process or network.
- [x] 2.6 Persist a resolved policy snapshot for every AgentRun; verify run creation stores policy ID, version, hash, and resolution time.
- [x] 2.7 Make snapshot identity/hash available to task artifacts and evaluation results from #3; verify both references round-trip.
- [x] 2.8 Apply policy changes to new runs without changing historical runs; verify an existing snapshot remains byte-stable after source policy edits.
- [x] 2.9 Make audit output explain the policy version that produced a decision; verify run audit inspection includes snapshot identity.

## 3. Approval and capability enforcement

- [x] 3.1 Make `REQUIRE_APPROVAL` produce a serializable approval request; verify request validation and persistence.
- [x] 3.2 Support approved, rejected, and expired approval states; verify valid transitions and reject transitions from terminal states.
- [x] 3.3 Let adapters map native approval/pause behavior into the canonical model; verify the generic contract with a mock controller.
- [x] 3.4 Make runtimes without native suspend/resume support fail safely and report capability mismatch; verify the action remains unexecuted.
- [x] 3.5 Make approval creation and decisions auditable; verify pending and terminal transitions produce linked audit events.
- [x] 3.6 Require adapters to declare governance capabilities; verify Claude Code, Codex, and Pi directory adapters explicitly declare unsupported controls.
- [x] 3.7 Validate policy requirements against adapter capabilities before execution; verify preflight reports missing required capabilities.
- [x] 3.8 Fail closed for configured unsupported mandatory controls; verify run creation is refused before any action can execute.
- [x] 3.9 Emit explicit warnings for unsupported optional controls; verify warnings identify each unfulfilled control.
- [x] 3.10 Prevent silent loss of security-relevant fields during adapter translation; verify unknown or unrepresentable fields fail validation.

## 4. Run inventory, audit, and anomaly events

- [x] 4.1 Provide active run inventory through one Agent Manager abstraction; verify list/show reads the shared user-level store across project roots.
- [x] 4.2 Make run kill capability-aware; verify a supported mock controller receives the request with the run ID and reason.
- [x] 4.3 Fail explicitly for unsupported kill operations; verify the run remains active and the CLI returns a capability error.
- [x] 4.4 Persist and audit termination reasons; verify a confirmed termination records the reason with run and snapshot identity.
- [x] 4.5 Prevent adapters from reporting successful termination without runtime confirmation; verify an unconfirmed response does not mark a run terminated.
- [x] 4.6 Store structured machine-readable audit records; verify versioned event records can be loaded and listed deterministically.
- [x] 4.7 Ensure credential/token values are never stored in audit events; verify sensitive metadata is omitted or redacted before persistence.
- [x] 4.8 Link policy decisions to the run and policy snapshot; verify event records contain the matching IDs and hash.
- [x] 4.9 Keep policy events available to feed the eval harness from #3; verify stored events load through the local evidence reader.
- [x] 4.10 Keep audit storage local-first; verify default state uses a user-level local directory and no network provider.
- [x] 4.11 Leave room in the event model for anomaly evaluators; verify all listed candidate event categories validate.
- [x] 4.12 Let the local eval harness consume policy/audit events from #3; verify a suite loads events from local fixtures.
- [x] 4.13 Keep P0/P1 enforcement/evaluation independent of an LLM-as-judge; verify evaluation tests run with deterministic local rules only.

## 5. Governance event evaluation

- [x] 5.1 Add an eval fixture that verifies an allowed action; verify the expected decision and evidence pass.
- [x] 5.2 Add an eval fixture that verifies a denied action; verify decision and reason-code mismatch fails.
- [x] 5.3 Add an eval fixture that verifies an approval-required action; verify the pending approval evidence passes.
- [x] 5.4 Add an eval fixture that verifies an adapter capability mismatch; verify mandatory mismatch evidence fails closed.

## 6. Issue #4 acceptance criteria

- [x] 6.1 Deliver a versioned canonical `AgentPolicy` model; verify policy fixtures cover supported and rejected versions.
- [x] 6.2 Make tool allow/deny and approval rules executable, not prompt-only documentation; verify policy engine outcomes in unit tests.
- [x] 6.3 Record an immutable effective policy snapshot/hash on each AgentRun; verify policy edits do not mutate prior run records.
- [x] 6.4 Expose stable machine-readable policy decision reason codes; verify deny and approval outcomes serialize them.
- [x] 6.5 Make adapter governance capabilities explicit and validate them before execution; verify preflight runs before any runtime action.
- [x] 6.6 Prevent unsupported mandatory enforcement from being silently ignored; verify missing required controls fail closed.
- [x] 6.7 Provide a canonical runtime-neutral approval request; verify it round-trips through storage and CLI output.
- [x] 6.8 Inventory active runs through Agent Manager; verify CLI list/show returns persisted run state.
- [x] 6.9 Make kill/termination capability-aware and auditable; verify only confirmed termination updates state and creates audit evidence.
- [x] 6.10 Make structured governance events consumable by the eval layer from #3; verify event fixtures and result metadata.
- [x] 6.11 Cover allow, deny, approval, capability mismatch, and budget-limit behavior with unit tests; verify the focused and full Go suites pass.
- [x] 6.12 Preserve existing Skill Manager behavior; verify existing CLI/resource regression tests pass.
