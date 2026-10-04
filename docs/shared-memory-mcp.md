# Explicit shared Memory access for Codex and Claude Code

The opt-in stdio MCP delivery client exposes one authorized PROJECT owner through
one Memory Gateway. It does not install native configuration, copy canonical
records, rewrite project instructions, start services, install dependencies, or
execute Skill or Task content. Existing CLI/task-context defaults are unchanged.
`memory status` describes default delivery mappings; `memory integration-status`
describes an explicitly configured MCP session.

## Trusted access configuration

Create an operator-owned access file outside the repository. Each native agent has
its own trusted `agent` attribution, but both use the same PROJECT owner and
provider reference. Owner labels in tool arguments and MCP `clientInfo` never
expand authority. Agent attribution is a startup configuration value, not proof
that the process is a particular native runtime; actual native events must be
verified separately.

```json
{
  "agent": "codex",
  "owner": {"kind": "PROJECT", "projectId": "operator-selected-project-id"},
  "allowWeakWrites": false,
  "provider": {
    "version": "v1",
    "id": "shared-project-memory",
    "provider": "mem0",
    "configuration": {"kind": "file", "name": "/private/operator/mem0.json"},
    "scopes": ["project"],
    "capabilities": ["read", "search"]
  }
}
```

Select the existing registered project identity when granting access to an
existing project; a synthetic demo may use an explicitly selected isolated
PROJECT owner. Do not put `agentId` into the owner merely to distinguish Codex
from Claude: that would partition their data. The `claude-code` access file uses
the same owner/provider and separately chosen write permission. Provider selection
uses the existing [Mem0 opt-in configuration](mem0-memory-provider.md), including the
explicit tested contract and `allowNetwork`. For this server, an external secret
reference of kind `file` resolves a bounded regular file **inside the server
process**, at request construction. Keep that key file private; never put its raw
value in agent configuration, environment, diagnostics, canonical metadata, or
committed files. Ordinary factory selection retains its environment-secret default.

```bash
agent-manager memory integration-status --access /private/operator/codex-access.json
agent-manager memory integration-config --agent codex \
  --binary /absolute/agent-manager --access /private/operator/codex-access.json
agent-manager memory integration-config --agent claude-code \
  --binary /absolute/agent-manager --access /private/operator/claude-access.json
```

These commands print status or access-only native snippets. They do not write
`AGENTS.md`, `CLAUDE.md`, private memory files, or global native configuration.
Remove the explicit native session configuration to disable access without
removing shared records. Run the server directly only as the native stdio child:

```bash
agent-manager memory serve-mcp --access /private/operator/codex-access.json
```

## Exposed operations and confirmation

Read-only sessions expose `memory_get`, `memory_search`, `memory_status`, and
`memory_retained_history` when the selected provider implements retained
snapshots. `memory_get` and history obey the Gateway count/content/full-attribution
budgets; oversized inspection fails rather than silently hiding lineage.
Search returns only current typed records within the same budgets.

A trusted session must explicitly set `allowWeakWrites: true` to register
`memory_preview_weak` and `memory_confirm_weak`. The Gateway independently checks
write authority and weak-policy opt-in. Preview accepts `add` or
`replace-observed-weak`; the latter includes an exact ID and observed version.
It displays the complete immutable content/source/evidence, prior snapshot,
confirmation nonce, and weaker semantics. Confirmation requires `confirmed: true`
and the exact intent ID, owner, and source. A preview is single-use: even an
uncertain attempted write cannot be replayed. Reconcile uncertain outcomes with
read-only inspection; an explicit new attempt requires a new preview.

Mem0 PUT is unconditional. The observed-record recheck rejects a stale snapshot
when it can see an intervening change, but another writer can intervene after
that check. There is **no CAS, operation receipt, atomic supersession, concurrent
overwrite protection, or automatic retry guarantee**. Existing strong Gateway
add/update/forget/supersede/history dispatch is not silently downgraded. Mem0
strong history remains unsupported. Basic provider replacement retains up to
eight prior snapshots, capped at 256 KiB in provider-owned `am_retained_lineage`
metadata; old snapshots have `SUPERSEDED` state and are available only through
explicit retained-history inspection. Earlier snapshots may be evicted. These
are bounded retained snapshots, not complete or atomic backend history. Basic
provider replacement now rejects an oversized retained-metadata payload before
PUT; it does not silently omit prior attribution to fit the limit.

`integration-status` reports the configured fixture-tested mechanism and exact
implemented operations (`weakAdd`, `weakReplace`, `retainedHistory`). It does not
probe native runtimes or infer live evidence from configured names/versions.
`memory_status` additionally reports successful tool observations in that server
session. `liveIdentity: unverified-client-claims` deliberately leaves native
identity verification to actual CLI startup/tool logs. A read-only session does
not advertise or register confirmed writes.

## Protocol and evidence boundaries

The delivery package pins official Go MCP SDK v1.4.0 (compatible with Go 1.24),
negotiates its supported versions, requires initialize then
`notifications/initialized`, and uses newline-delimited UTF-8 JSON-RPC. Frames
are bounded at 1 MiB before SDK decoding; tool arguments at 64 KiB; a session at
64 tool calls and 16 outstanding previews; each tool call has a 15-second
context deadline. SDK cancellation propagates to the provider. Stdout contains
protocol only. Tool execution errors use `isError` and safe diagnostic categories.
Protocol/lifecycle errors remain JSON-RPC errors. No arbitrary command tool is
registered. Canonical content and source URLs are inert claims, never authority.

An optional `serve-mcp --events /new/task-owned/file.jsonl` creates a private,
exclusive, bounded metadata-only event file. It records startup agent attribution,
negotiated protocol, tool outcomes and neutral record/version/source/evidence;
it does not store record content or backend IDs. Native JSONL outputs are private
acceptance evidence, not a canonical store or private Memory feature.

## Actual two-agent acceptance

The opt-in runner requires an **already running** pinned Mem0 service, actual
Codex/Claude binaries, preserved native authentication, and two trusted access
files. It never starts/installs services or changes account/configuration state.
Run only after explicitly authorizing synthetic weak writes:

```bash
python3 scripts/memory-two-agent-demo.py \
  --binary /absolute/agent-manager \
  --codex-access /private/operator/codex-access.json \
  --claude-access /private/operator/claude-access.json \
  --mem0-source /task-owned/pinned-mem0-source \
  --mem0-python /task-owned/mem0-venv/bin/python \
  --mem0-pid <actual-running-uvicorn-pid> \
  --output /new/private/task-evidence-directory
```

The runner verifies process/package/source pin and records binary hash/versions,
then requires actual native tool results paired with server events: Codex create,
Claude read the same ID, Claude explicitly replace, both read v2 and separately
inspect v1 provenance, cross-project denial, and a native read-only status. A
failed stage stops without write replay or service/model substitution. The output
directory must be new. Full native logs remain private. Source/package checking
is independent process evidence, not automatic server version detection.

Codex uses ephemeral read-only sessions with shell/private memories disabled,
exact enabled tools and session-only per-tool approval overrides. The tested
0.157.1 schema supports `tools.<tool>.approval_mode="approve"`. Claude uses strict
MCP configuration, no builtin tools/private memory/session persistence/chrome or
Skills, exact allowed MCP names, and session-only disabled hooks/plugins. On the
tested 2.1.236 installation, empty `--setting-sources` hides existing auth, so the
runner preserves `user` authentication sources and disables discovered enabled
plugins via session overrides. Managed settings may override isolation choices;
the runner verifies actual startup/tool/hook evidence rather than treating flags
as proof. It refuses unexpected tools/plugins/Skills/hooks.

At the initial #23 checkpoint, Codex and real Mem0 completed a preparatory native
create/read. Claude reached the configured MCP server but its existing inference
service rejected requests because its subscription was inactive. No actual
Claude Memory tool ran. The user chose to restore that same service; this is
pending, and no provider switch is authorized. The final runner has only help
and syntax verification so far, not native acceptance. The
[M5 delivery matrix](research/issue23-memory-delivery-matrix.md) preserves this
literal gap. Neither #23 nor parent #7 is accepted by offline fixtures alone.

Primary protocol sources: [stdio](https://modelcontextprotocol.io/specification/2025-11-25/basic/transports),
[lifecycle](https://modelcontextprotocol.io/specification/2025-11-25/basic/lifecycle),
[official SDK pin](https://github.com/modelcontextprotocol/go-sdk/tree/v1.4.0),
[Codex pinned schema](https://github.com/openai/codex/blob/rust-v0.157.1/codex-rs/core/config.schema.json),
[Claude CLI](https://code.claude.com/docs/en/cli-reference),
[Claude hooks](https://code.claude.com/docs/en/hooks), and
[auto-memory environment control](https://code.claude.com/docs/en/env-vars).
