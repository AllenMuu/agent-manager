# Explicit shared Memory access for Codex and Pi

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
PROJECT owner. The Issue #23 native acceptance runner additionally requires
its synthetic `projectId` to begin with `issue23-`; choose a fresh isolated
value for each run. This naming check is a fail-closed scope guard, not an
authorization mechanism. Do not put `agentId` into the owner merely to
distinguish Codex or Pi: that would partition their data. Each agent access file uses
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
agent-manager memory integration-config --agent pi \
  --binary /absolute/agent-manager --access /private/operator/pi-access.json
```

These commands print status or access-only native snippets. They do not write
project instructions, private memory files, or global native configuration.
Pi's snippet is a project `.mcp.json` for [`pi-mcp-adapter`](https://github.com/nicobailon/pi-mcp-adapter):
it limits direct tools to the six Memory operations, uses the `mcp` prefix and
keeps the configured server alive for the session. On Pi versions without built-in
MCP, install the adapter once with `pi install npm:pi-mcp-adapter`. The CLI does
not install extensions or dependencies.
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

The opt-in #23 runner requires an **already running** pinned Mem0 service, actual
Codex/Pi binaries, preserved native authentication, and two trusted access files.
It never starts/installs services or changes account/configuration state. This
delivery covers Codex and Pi only. Parent #7 remains open with its broader
acceptance checklist; its current Definition of Done names Codex, Pi, or another
coding agent, and it schedules Claude/Hermes adapters incrementally after #23.
Run only after explicitly authorizing synthetic weak writes:

```bash
python3 scripts/memory-two-agent-demo.py \
  --binary /absolute/agent-manager \
  --codex-access /private/operator/codex-access.json \
  --pi-access /private/operator/pi-access.json \
  --pi-binary /absolute/pi \
  --pi-agent-dir /private/operator/pi-validation-profile \
  --pi-model gpt-5.6-luna \
  --mem0-source /task-owned/pinned-mem0-source \
  --mem0-python /task-owned/mem0-venv/bin/python \
  --mem0-pid <actual-running-uvicorn-pid> \
  --output /new/private/task-evidence-directory
```

The runner requires both access files to resolve to the same pinned, numeric
loopback Mem0 endpoint and file-backed provider configuration. Start the service
manually from a clean checkout of the pinned source, using the selected virtual
environment and the exact server application path:

```bash
cd "$MEM0_SOURCE/server"
"$MEM0_VENV/bin/python" -B -I -m uvicorn main:app \
  --app-dir "$MEM0_SOURCE/server" --host 127.0.0.1 --port 8765
```

Use absolute paths and set `MEM0_SOURCE` to the resolved checkout path; keep the
virtualenv and acceptance output directory outside that checkout. `-B` prevents
Python bytecode from dirtying the pinned source while the server runs.

The runner checks that the pinned Git source has no tracked, untracked or ignored
changes; the process disables bytecode writes and uses isolated Python mode, the
exact selected virtualenv and `server/main.py`; `uvicorn` resolves from that
virtualenv; and `mem0` resolves from that virtualenv or the pinned source tree.
It checks the configured endpoint against the exact listener owned by the
supplied process ID and reads the process argument vector without flattening
argument boundaries, so paths containing spaces remain verifiable. It records
the requested and resolved Python executable paths, resolved application/package
import paths, package version, source commit, binary hash and agent versions.
This rejects a different environment that happens to share the same base
interpreter or an alternate Uvicorn app directory.
It then requires actual native tool results paired with server events: Codex create, Pi reads the same ID and
explicitly replaces it, both read v2 and separately inspect v1 provenance, both
reject cross-project access, and Pi reports its configured read-only status. A
failed stage stops without write replay or service/model substitution. The output
directory must be new. Full native logs remain private. Source/package/listener
checks are independent runtime evidence, not automatic Mem0 API version detection.

Codex uses ephemeral read-only sessions with shell/private memories disabled,
exact enabled tools and session-only per-tool approval overrides. The tested
0.157.1 schema supports `tools.<tool>.approval_mode="approve"`. Pi runs in JSON
mode without session persistence, built-in tools, discovered extensions, Skills
or context files. The runner loads only `pi-mcp-adapter` and a task-owned guard,
uses an isolated HOME plus a temporary project `.mcp.json`, and refuses proxy
operations or direct tools outside the configured Memory allowlist. It records
Pi, adapter and model versions. The runner verifies actual tool/server events;
flags and configuration alone are not proof.

The native runner passed the Codex/Pi two-agent acceptance on 2026-10-14 against
Mem0 OSS Python 2.2.1 at commit
`94c3fe9f238f3dbf29c9ce98643bd71eb13077cd`. It verified Codex creation, Pi read
and explicit replacement, both agents' v2 recall and separate v1 provenance
inspection, foreign-project denial for both agents, and Pi's native read-only
status. The recorded environment includes actual process/import/listener binding
and both native client versions. Final private evidence, including the rerun
with API-key environment variables excluded from both child agents, is at
`/private/tmp/agent-manager-issue23-final/native-evidence-env-sanitized-final2-20261014/`;
failed attempts and the fixes they exposed are described in the
[M5 delivery matrix](research/issue23-memory-delivery-matrix.md). This completes
the #23 Codex/Pi acceptance slice. It provides the real Mem0 cross-agent
evidence described by #7, while the parent issue remains open for its broader
acceptance checklist. Claude/Hermes adapters are follow-on work after #23.

Primary protocol sources: [stdio](https://modelcontextprotocol.io/specification/2025-11-25/basic/transports),
[lifecycle](https://modelcontextprotocol.io/specification/2025-11-25/basic/lifecycle),
[official SDK pin](https://github.com/modelcontextprotocol/go-sdk/tree/v1.4.0),
[Codex pinned schema](https://github.com/openai/codex/blob/rust-v0.157.1/codex-rs/core/config.schema.json),
[Pi MCP adapter](https://github.com/nicobailon/pi-mcp-adapter).
