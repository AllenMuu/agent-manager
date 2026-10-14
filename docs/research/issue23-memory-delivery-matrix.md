# Issue #23 delivery evidence: Codex/Pi acceptance passed

This records the staged M5 implementation and its live native acceptance on
2026-10-14. M3/#21 and M4/#22 were delivered before this slice; M5 remains
unarchived. Issue #23 is still OPEN; no PR, merge or issue closure is claimed.
Parent #7 also remains OPEN with a broader acceptance checklist. Its current
Definition of Done names Codex, Pi, or another coding agent; #7 says to add
Claude/Hermes adapters incrementally after #23. This slice provides the real
Mem0 Codex/Pi cross-agent evidence for #7 without claiming the parent issue is
complete.

The confirmed public seams are Gateway weak preview/confirmation, the bounded
stdio MCP service, CLI integration configuration/status, and independent native
Codex/Pi sessions. The checklist is exclusively the change's tasks.md.

## Runtime baseline and evidence

- Live #21 and #22 are CLOSED; live #23 remains OPEN and names Codex + Pi.
- Codex CLI 0.157.1; Pi 0.85.1; `pi-mcp-adapter` 5.3.0. Pi does not provide a
  built-in `pi mcp` command, so the adapter was installed only in a task-owned
  `PI_CODING_AGENT_DIR`; global Pi packages and settings were not changed.
- Mem0 OSS Python 2.2.1 from clean source commit
  `94c3fe9f238f3dbf29c9ce98643bd71eb13077cd`, Python 3.12.13, FastEmbed 0.8.1
  and local model `BAAI/bge-small-en-v1.5`. The service used the existing
  task-owned `pgvector/pgvector:pg17` database container, loopback-only endpoint
  `http://127.0.0.1:8765`, and a dedicated collection
  `agent_manager_issue23_codex_pi_20261014`. Mem0 inference was disabled by the
  provider requests; telemetry was disabled. No real Claude or external LLM API
  key was used; a dummy no-network Mem0 server setting prevented model calls.
- The final run selected Pi model `gpt-5.6-luna`. The runner bound the service
  endpoint to the actual process, exact Python virtualenv, clean pinned source,
  server module, Mem0 package import and Uvicorn import. The rerun after
  environment allowlisting passed with native Codex profile authentication
  available and API-key environment variables excluded from both child agents.
- Final private native evidence is stored at
  `/private/tmp/agent-manager-issue23-final/native-evidence-env-sanitized-final2-20261014/`.
  It includes environment/version proof, stage-specific native results and
  metadata-only server events. The logs and synthetic record content remain
  private and are not committed.

| Requirement | Literal scenario | Implemented public evidence | Actual runtime outcome |
| --- | --- | --- | --- |
| Two real agent readers and writers | Codex writes and Pi reads | CLI access configuration, public stdio MCP and native runner pair actual tool results with trusted server events | Passed: Codex created v1 through a weak preview and one confirmation; Pi read the same neutral ID and provenance from real Mem0. |
| Two real agent readers and writers | Real Mem0 two-agent acceptance | Runner binds both sessions to the same real pinned Mem0 endpoint and records source/package/process and agent versions | Passed: Codex and Pi completed independent native sessions against Mem0 2.2.1 at the pinned commit. |
| Effective version convergence | Both agents observe replacement | Replacement preview binds the observed v1; runner requires v2 reads/recalls and separate v1 retained-history inspection | Passed: Pi explicitly replaced v1 with v2; Pi and Codex both recalled active v2, and both separately inspected v1 as superseded. |
| Owner isolation and inspectable provenance | Cross-project read attempt | Gateway enforces the configured PROJECT owner and returns source/evidence with the canonical record | Passed: Pi and Codex each received `ownership-denied` for a foreign project, with no foreign record returned. |
| Per-agent tested capability status | One integration lacks write support | Runner reopens Pi with write permission disabled and checks native `memory_status` | Passed: Pi reported read/search enabled and write disabled in the read-only session. |
| One canonical source of truth | Integration is enabled | Both clients use the same provider and owner; record IDs and retained snapshots are Mem0-backed | Passed: both sessions observed the same Mem0 record and lineage; no agent-private copy was created. |
| Interoperable MCP tool schemas | Pi lists and calls shared Memory tools | Supporting task 2.6: MCP server normalizes empty `required` fields to JSON arrays; Go regression covers every registered tool | Passed: Pi adapter listed the tools and called them directly without proxy fallback. |

## Failed attempts and regression fixes

Two earlier private runs are retained as failed evidence. The first completed
Codex creation, then Pi rejected `memory_status` because its MCP schema encoded
an empty `required` field as `null`. A Go regression test reproduced the wire
payload; the MCP server now encodes an empty array. The second run completed
Pi's reads and the server returned the expected cross-project denial, but the
adapter prefixed the error text with `Error: ` and the runner compared the
un-normalized text. A Python regression now normalizes that prefix only for
failed native tool events and requires Pi's denial event to be an error. Both
failed runs used separate synthetic PROJECT owners and stopped before Pi update.

During the environment-allowlist rerun, one setup attempt was also blocked
before any native client ran because the service working directory did not
match the pinned source tree. The final run started the service from the exact
pinned server directory and completed all native stages with a fresh synthetic
PROJECT owner.

The final successful run used a fresh synthetic PROJECT owner in the dedicated
collection. No user project memory was read. The prepared Codex/Pi access files,
Mem0 service environment, venv, source checkout and output directories are all
outside the repository and have private permissions. The Mem0 source checkout
remained clean.

The user reports Claude API quota is unavailable, so this slice used no Claude
session. Claude/Hermes adapters are follow-on work after #23 in the current #7
body; they are not prerequisites for this Codex/Pi acceptance.

## Review and verification

The live Pi run exposed the JSON-schema interoperability issue above; affected
code now has a regression test. Full checks passed after the final code fix:
`go test ./...`, race checks for `internal/cli` and `internal/memoryintegration`,
`go vet ./...`, CLI build, strict OpenSpec validation, `openspec doctor`, Python
runner regression tests (24 passed after review repair), and staged/unstaged
diff checks. The
final standards review confirmed the Codex/Pi format switch, environment
allowlist and aligned #7 scope, with no remaining actionable findings. The spec
review also required an explicit task for the MCP schema scenario; supporting
task 2.6 is now mapped in this matrix and the issue flow. The final Python
suite includes regression coverage for synthetic-owner scope. Final ordinary
review is complete and task 3.2 is checked. The configured
gpt-6.1-sol/high OCR attempts failed; at the user's explicit direction, the
read-only `open-code-review delegate` review covered all 5 OCR-reviewable files
and all 14 excluded files were manually reviewed (19/19 total). It found that the native
acceptance runner did not require an isolated synthetic PROJECT ID; the runner
now requires the `issue23-` prefix, has regression coverage, and its previous
native evidence uses that prefix. Following the user's explicit selection,
task 3.3 records the delegated review as the final review path; the exact
gpt-6.1-sol/high review is unavailable and is not claimed. No PR publication or
issue closure is recorded here. Fixture and SDK protocol tests remain separate
from the real native acceptance recorded above.
