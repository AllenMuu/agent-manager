# Offline runtime enforcement preflight (R1 / Issue #24)

`policies inspect <request.json> --json` reports a local declaration document.
`policies preflight <request.json> --json` reports the same view and exits nonzero
when mandatory requirements are missing or input is invalid. Neither command
launches a model, sandbox, service, installs dependencies or resolves credentials.
They do not record or start an AgentRun. Existing `run.Manager.Start` is unchanged;
R2 will integrate this contract before runtime prepare/start.

The exported boundaries are `enforcement.ResolvePermissions`,
`enforcement.DiscoverCapabilities`, `enforcement.Preflight`, and strict JSON
`enforcement.LoadRequest`. `adapter.EnforcementDeclaration` exposes truthful
placement-only declarations for the existing target agents;
`enforcement.NoopDeclaration` exposes the canonical noop provider.

The additive request uses `version: v1`; its embedded `policy` remains an existing
AgentPolicy v1/v2. Existing YAML policy commands, snapshots, controller interfaces,
governance booleans, approval lineage and Memory behavior are unchanged. JSON
unknown fields, unknown dimensions, support states or update modes are rejected.
The CLI input limit is 1 MiB.

```json
{
  "version": "v1",
  "policy": {
    "version": "v1", "kind": "agent-policy", "id": "offline",
    "name": "Offline", "tools": {"deny": ["send"]}
  },
  "required": ["network", "credential"],
  "optional": ["filesystem"],
  "contributions": [{
    "provider": "fixture",
    "permissions": {
      "process": {"state": "known", "allow": ["shell"]},
      "network": {"state": "known", "allow": ["api.example:443"]},
      "credential": {"state": "known", "allow": ["ref:mail/send"]}
    }
  }],
  "provider": {
    "name": "fixture", "kind": "execution",
    "governance": {"tool_interception": true, "runtime_events": true},
    "controls": {
      "tool": {"support": "supported", "verified": true, "update": "unsupported"},
      "network": {"support": "supported", "verified": true, "update": "live-update"}
    }
  }
}
```

This example rejects missing credential mediation and warns about optional
filesystem protection. Its execution declaration is an offline fixture. The
`verified` field is the submitting integration's assertion, not verification
performed by the CLI. `accepted` means the submitted declarations satisfy the
checked requirements; it neither proves protection nor authorizes execution.

All five dimensions (`tool`, `filesystem`, `network`, `process`, `credential`)
are always present. Each permission view keeps policy constraints separate from
attributed provider contributions; omitted source declarations become `unknown`.
Tool constraints come from AgentPolicy; additional network destinations,
filesystem paths, process functions and opaque credential references/scopes use
`permissions` with `known`/`unknown` states and exact `allow`/`deny` identifiers.
No prefix, wildcard or automatic union is inferred. Credential entries are
references, never credential values. Overlapping network/credential policy
sources are rejected rather than silently overwriting either one.

The aggregate view remains `unknown`: declarations alone cannot establish actual
runtime reachability or settle conflicts between constraints and contributed
permissions. Known source identifiers remain visible, including a provider's
`send` capability alongside a policy deny. Shell, network and credential
contributions are preserved independently; a tool deny establishes no absence
of indirect access. An empty legacy tool allowlist keeps existing AgentPolicy
semantics, not an invented deny-all meaning.

Capabilities are separate from permission sources. Support is `supported`,
`unsupported` or `unknown`; only supported plus explicitly verified declarations
satisfy mandatory controls. Update mode is independently `live-update`,
`recreate-required` or `unsupported` for each dimension. Missing controls are
unknown with unsupported updates. No general hot-reload claim is exposed.

The existing policy's mandatory tool, network and credential controls imply
corresponding mandatory dimensions. Existing optional policy controls imply
optional dimensions. Additional filesystem/process/network/credential permission
constraints require dimension protection unless explicitly optional. Conflicting
mandatory/optional requirements reject. Existing `policy.CheckCapabilities`
continues separately as the `governance` report: booleans never become evidence
for new dimension enforcement, and budget/approval/runtime-event requirements
cannot disappear from preflight.

Noop, directory and unspecified provider kinds always expose unsupported
protection and no governance support, regardless submitted names or booleans.
Placement or invocation-context propagation cannot upgrade these declarations.
No runtime hooks are implemented in R1. External enforcement, lifecycle binding,
permission revisions, formal reachability proof, checkpoint and watchdog remain
future work; managed-skill workflows remain guarded, reversible and non-executing.
