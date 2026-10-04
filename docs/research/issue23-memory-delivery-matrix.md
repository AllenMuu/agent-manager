# Issue #23 delivery evidence: partial checkpoint

This is branch implementation evidence, not acceptance of #23 or parent #7.
M3/#21 and M4/#22 were delivered before this slice. The branch also carries the
M4 archive documentation commit 7e23997; M5 remains active and unarchived.
The authorized public seams are Gateway explicit weak preview/confirmation,
bounded stdio MCP, CLI access configuration/status and an opt-in native runner.
The checklist is exclusively the change's tasks.md: 1.1 and 2.5 are complete;
2.1–2.4 and 3.1–3.3 remain pending.

| Requirement | Literal scenario | Implemented public evidence | Actual runtime outcome |
| --- | --- | --- | --- |
| Two real agent readers and writers | Codex writes and Claude reads | Mem0 Gateway create/provenance and MCP exact confirmation tests; runner pairs native tool results with trusted server events | Preparatory Codex create/read passed against actual Mem0; Claude Memory calls absent. Pending. |
| Two real agent readers and writers | Real Mem0 two-agent acceptance | Runner pins actual process/package/source and records agent versions; requires all create/read/update/inspection stages | Final runner has syntax/help verification only. Pending configured Claude subscription restoration. |
| Effective version convergence | Both agents observe replacement | ConfirmedWeakReplacementRetainsPriorProvenanceSeparately, stale-preview and bounded/corrupt-lineage regressions; runner requires both v2 reads/recalls and separate v1 inspection | No actual Claude update or both-agent v2 convergence. Pending. |
| Owner isolation and inspectable provenance | Cross-project read attempt | WeakWriteOptInAndForeignOwnerFailBeforeRemoteAccess, MCPStartupOwnerCannotBeBroadenedByGatewayOrClientClaims, trusted file-secret authorization test | Fixture rejection/provenance passed; final two-native negatives pending. |
| Per-agent tested capability status | One integration lacks write support | ReadOnlyMCPExposesBoundedSharedRecordAndTruthfulStatus, MCPStatusDoesNotInventWriteAuthorityFromSessionLabels, CLI read-only status | No final native Claude read-only status call. Pending. Configured labels are not native proof. |
| One canonical source of truth | Integration is enabled | MemoryIntegrationConfigurationContainsOnlySessionAccess, trusted startup event test; CLI prints access-only snippets; canonical snapshots remain in provider metadata | No automatic instruction/private-store writes; task-owned native configuration and prep startup contained access only. Complete configuration behavior; full native acceptance remains separate. |

## Evidence separation

Private task evidence is under `/tmp/agent-manager-issue23-evidence/`, outside the
repository. Twelve observed public RED/GREEN cycles are named
`tdd-01-red.log`/`tdd-01-green.log` through `tdd-12-*`. HTTP fixtures and SDK
protocol clients exercise deterministic failure boundaries, not real agents.
The delivered-base full Go baseline is `/tmp/agent-manager-issue23-baseline.log`.
Final offline checks and resource cleanup are recorded in `checkpoint-checks.json`
and `checkpoint-cleanup.json` in the private evidence directory.

The preparatory native Codex 0.157.1 run completed MCP status, weak preview,
confirmation and get. Its v1 neutral record was
`memory-709d83f92a00ca2e1424f4a89370a4c0`; this is preparation evidence, not
acceptance against the final runner. Its initial denied approval attempt is
retained as a negative. The final runner corrects a preparatory ADR evidence URL;
no prior record was silently rewritten to make the demonstration pass.

Claude Code 2.1.236 reached the strict configured MCP server. Empty setting sources
hid existing auth; preserving the user auth source with session hooks/plugins
restrictions restored startup access. Actual startup inventory was restricted,
but the existing configured inference service returned an inactive-subscription
error before any Memory tool call. Account diagnostics remain private and are
not copied here. The user selected restoring that same service; no restoration
completion or alternate endpoint/provider authorization has been received.

Actual Mem0 preparation used Python package 2.2.1 and unmodified source commit
94c3fe9f238f3dbf29c9ce98643bd71eb13077cd with local cached FastEmbed and infer:false,
loopback-only uvicorn and task-owned pgvector/pg17. OpenAPI's generic version is
not runtime pin evidence. Credentials were resolved inside the server from a
private file reference, not passed as native configuration values. Service and
database are stopped at the checkpoint; the runner requires an already running
pinned service for an authorized resumed demonstration.

## Review and resumption

Offline passing checks do not complete task 3.1, which also requires literal
actual-runtime verification. Independent spec/quality review and the prescribed
read-only final OCR have not run for this checkpoint. No PR publication, merge,
Issue closure, parent acceptance or M5 archive is claimed. Resume real acceptance
only after restoration of the current configured Claude service is confirmed;
use a new private evidence directory and retain failed/preparatory evidence.
