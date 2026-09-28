# Runtime Policy and Governance

Agent Manager exposes a runtime-neutral policy engine and run-governance API. It does not launch an agent. Claude Code, Codex, and Pi directory adapters currently report governance enforcement as unsupported, so a run that requires their unsupported controls is rejected during preflight.

## AgentPolicy v1

Policy files use strict YAML. Unknown fields, duplicate rules, unsupported versions, and non-canonical hostnames are errors.

```yaml
version: v1
kind: agent-policy
id: safe-default
name: Safe default
tools:
  allow: [read_file, search]
  deny: [delete_repository]
network:
  allowed_domains: [github.com, api.github.com]
  denied_domains: []
credentials:
  allowed_scopes: [github:read]
  denied_scopes: [production:admin]
subagents:
  max_concurrent: 3
  max_total: 10
budget:
  max_duration_seconds: 3600
  max_cost_usd: 5.0
  max_tool_calls: 200
approval:
  required_for: [destructive_write, external_publish]
termination:
  kill_on: [denied_tool_call, denied_domain_access]
```

Tool matching is default-deny, explicit deny takes precedence, and network hostnames and credential scopes match exactly. The policy package can resolve a policy into a canonical SHA-256 snapshot and evaluate normalized events without a runtime process.

Validate a file with `agent-manager policies validate ./policy.yaml`. Policies for `list` and `show` live in `os.UserConfigDir()/agent-manager/policies/`.

## Run state and controls

Run state is stored in `os.UserConfigDir()/agent-manager/runs/state.json`, shared across projects. Each run stores its immutable effective policy snapshot, runtime label, status, and capability warnings. State files use restrictive permissions and atomic replacement.

The Go API accepts a `RuntimeController` explicitly. The controller must confirm pause/resume and termination operations before persisted run state changes. The current directory adapters do not implement that control interface. `agent-manager runs kill` therefore reports an unsupported capability for those adapters. There is intentionally no `runs start` command that could imply an agent was launched.

`agent-manager runs list`, `runs show <run-id>`, and `runs events <run-id>` inspect the shared local inventory. `agent-manager approvals list|show|approve|reject|expire` records human decisions and linked audit events. Approval creation must cite the persisted `REQUIRE_APPROVAL` request audit; its recorded decision preserves the original budget evaluation. A completion linked to that request is allowed only after its matching approval is approved. A CLI decision records the operator's choice only; it does not execute an action or resume a runtime.

Audit records contain the run and policy snapshot identity, event category, safe action labels, decision, and stable reason code. Raw event metadata, credential scopes, decision details, and untrusted actor text are not copied into the audit trail.

## Governance evaluation

Eval case version `v3` reads local `events.json` evidence and verifies expected event decisions/reason codes or capability reports. It uses the same deterministic rule evaluator as response-file suites and does not invoke an LLM judge.

Run a governance suite with fixture evidence:

```bash
agent-manager eval run ./evals/governance --json
```

Use a recorded local run as the event source:

```bash
agent-manager eval --governance-run-id <run-id> run ./evals/governance --json
```

Eval results include policy snapshot references for the audit events used by each case. Task artifacts can carry an optional `policy_snapshot` reference through `artifact.Document.SetPolicySnapshot`.
