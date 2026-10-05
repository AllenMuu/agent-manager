# Runtime preflight is control-plane metadata

Status: Accepted for R1 / [Issue #24](https://github.com/AllenMuu/agent-manager/issues/24).

Agent Manager resolves declared permission sources separately from verified
execution controls. Tool denial does not remove filesystem, process, network or
credential reachability. Provider contributions and policy restrictions remain
attributed separately; conflicting or incomplete declarations cannot establish a
final effective scope. Unknown is explicit and mandatory protection fails closed.

A versioned offline preflight request embeds an existing AgentPolicy without
changing v1/v2 policy schemas or run snapshots. Public inspection/preflight ports
accept values, not execution hooks. Each dimension declares enforcement support
and live-update, recreate-required or unsupported update support separately.
Directory placement and invocation context propagation establish no interception.
Noop and directory declarations therefore expose unsupported execution protection.

A successful preflight means the submitted declarations satisfy mandatory
requirements. It does not attest a live sandbox or authorize execution. R2 owns
prepare/start integration; external runtimes own actual filesystem, process,
network and credential mediation. Kernel/container isolation, formal reachability
proof, checkpoint and watchdog remain future unsupported capabilities.

Existing governance booleans, approval lineage, identity, Memory and diagnostics
remain compatible. Managed-skill operations remain guarded, reversible and
non-executing; this metadata boundary adds no network access, dependency
installation, credential resolution or service startup to Skill workflows.

See [runtime-enforcement-preflight](../../openspec/specs/runtime-enforcement-preflight/spec.md).
