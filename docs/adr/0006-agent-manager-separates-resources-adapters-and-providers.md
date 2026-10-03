# Separate Agent Manager resources, adapters, and providers

Agent Manager SHALL model a managed resource independently from both an agent runtime and a storage provider. A resource handler owns resource-kind validation and lifecycle rules; an agent adapter owns runtime detection, declared capabilities, inspection, and target-specific placement; a Memory provider owns provider configuration and capability discovery.

Existing Skill Manager state remains a first-class Skill resource domain. The `agent-manager` CLI is primary while `skill-manager` is a compatibility alias. Legacy configuration, companion metadata, project links, and journal entries are read and normalized in memory without automatic rewrites.

## Considered Options

- Preserve a Skill-only domain and add agent-specific paths directly to each new command.
- Use one generic filesystem install abstraction for every resource kind.
- Split resource lifecycle rules, agent runtime adapters, and Memory providers behind explicit capability contracts.

The third option is selected. It preserves guarded and reversible Skill link operations, exposes unsupported runtime capabilities instead of silently discarding data, and permits provider-backed Memory without treating it as a filesystem resource. A generic filesystem abstraction was rejected because links, canonical SubAgent definitions, and provider-backed Memory do not share safe mutation semantics.

## Local provider selection

For the initial provider boundary, Agent Manager selects a local file-backed
provider as the feasible implementation. Capability discovery validates an
already-existing regular file. Configured capability requests are separate
from its implemented confirmed-append operation and user/project scopes without creating or mutating the
file during discovery; writes are reserved for an explicit confirmed
promotion. Network-backed providers are intentionally unavailable by default: the
configuration stores only a non-secret `file` reference, and an unavailable or
misconfigured path returns an actionable local status.

On platforms without a portable no-follow open primitive (including Plan 9,
Solaris, and illumos), discovery uses `Lstat`, an ordinary read-only open, and
post-open file-identity verification. This rejects direct symlink references
and ordinary replacement races, but has a weaker guarantee than the Unix
`O_NOFOLLOW` or Windows reparse-point paths if a platform permits a symlink
swap during the open itself; such a mismatch is rejected after opening.

## Structured Memory contract (Issue #19)

The [scoped Memory contract](../../openspec/specs/scoped-memory-contracts/spec.md)
adds a canonical record boundary separately from the legacy text Provider,
Promote and Searcher interfaces. Agent Manager owns neutral record IDs, versions,
knowledge types, owner partitions, state, layer, source and evidence references;
a provider maps its own objects to that shape without inventing provenance.
Capabilities describe implemented operations; health separately describes current
availability. Optional operations return unsupported rather than pretending an
empty result is success. Context cancellation and uncertain writes have distinct
error categories.

USER and PROJECT identify one owner; AGENT and SESSION refine exactly one owning
user or project. Owner labels establish partitions, not authenticated authority;
the authorized Gateway is a later slice. An in-memory implementation is only a
deterministic contract implementation: it does not enable durable storage,
external providers, automatic extraction, conversion of legacy text, or a new
CLI default. SKILL and TASK record content is inert knowledge, never executable.
Managed-skill operations retain their guarded reversible lifecycle and do not
write Memory, execute skill-provided code, install dependencies or access a
network through this contract. See [ticket #19](https://github.com/AllenMuu/agent-manager/issues/19).


## Authorized Memory Gateway (Issue #21)

CLI and task context share a Gateway with trusted exact owner allowlists and
separate read/write authority. Requested partition labels do not grant access.
The local operator explicitly chooses a user identity or registered project;
project identities are opaque IDs with explicit canonical directory mappings,
not directory basenames. Registration and relocation preview the exact mapping
and require confirmation. Relocation preserves the ID; no path is inferred or
automatically adopted.

`structured-local` explicitly selects an existing direct directory through the
existing non-secret configuration reference. Discovery is read-only. Structured
store writes require an immutable owner/content/version/operation preview plus
exact confirmation; legacy import also binds declared source and previewed
content bytes. Configured requests, implemented optional interfaces and current
availability remain separate. Legacy text promotion/defaults are preserved.

Gateway retrieval authorizes before provider access and verifies provider output.
It filters current records and knowledge type, uses documented lexical token
coverage unless a provider explicitly offers finite normalized scores, breaks
ties by neutral IDs and bounds count, content bytes and the full attributed JSON
array. Task context includes canonical records once and retains provenance;
provider outages leave independent context resources and resource workflows
available with safe diagnostic categories. Memory writes use provider lifecycle
semantics and are outside the reversible Skill filesystem operation journal.
No network provider, extraction/consolidation, executable knowledge, dependency
installation or native agent Memory mechanism is introduced by this slice.

## Explicit Mem0 OSS opt-in (Issue #22)

The separately selected `mem0` provider enables network requests only when its
non-secret external configuration explicitly sets `allowNetwork: true` and the
tested OSS contract pin. Local defaults and managed-skill workflows remain
offline: no skill execution, dependency installation, service startup or network
access is added to their lifecycle. See [ticket #22](https://github.com/AllenMuu/agent-manager/issues/22)
and [Mem0 capability](../../openspec/changes/add-mem0-memory-provider/specs/mem0-memory-provider/spec.md).

Credentials are opaque references resolved only while building authenticated
transport requests. Status, journals, canonical metadata and errors contain no
credential values or raw HTTP diagnostics. Requests have bounded deadlines and
bodies, reject redirects, and never automatically replay uncertain writes.

The pinned OSS server does not guarantee conditional mutations, operation
receipts, atomic supersession or canonical lineage history. Basic provider
create and explicitly unconditional replacement/removal remain distinct from
confirmed Gateway mutations; the CLI Gateway advertises read/search only.
Versions are observable metadata counters, not compare-and-swap guarantees.
No second canonical content mirror is maintained.
