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
already-existing regular file and reports the configured read, write, and
search capabilities and user/project scopes without creating or mutating the
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

The [scoped Memory contract](../../openspec/changes/define-scoped-memory-contracts/specs/scoped-memory-contracts/spec.md)
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
