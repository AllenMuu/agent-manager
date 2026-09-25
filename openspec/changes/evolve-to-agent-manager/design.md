## Context

The current Go CLI has a mature Skill-only lifecycle: an authoritative directory library, project-local absolute links, guarded previews, reversible operation journals, and Claude Code/Codex-specific placement rules. Those runtime rules are currently too close to the Skill workflows to support further resource kinds safely. See `proposal.md` for motivation and the delta specifications for behavior.

## Goals / Non-Goals

**Goals:**

- Preserve every existing managed-Skill activation and journal entry while extracting reusable governance boundaries.
- Make agent capabilities and resource lifecycle contracts explicit before adding mutable SubAgent or Memory behavior.
- Keep provider state, agent integration state, and user-owned Memory data separate.
- Roll out in independently testable phases, with compatibility tested before enabling each new mutation path.

**Non-Goals:**

- A distributed workflow engine, autonomous delegation runtime, full conversation synchronization, or a new vector database.
- Declaring semantic portability for resource instructions merely because an adapter can place files.
- Replacing native session/task behavior of Claude Code, Codex, or Pi.

## Decisions

### 1. Introduce a domain core split by resource handler and agent adapter

The core will model `ManagedResource` identity and common provenance/compatibility metadata, then delegate resource validation and lifecycle planning to a `ResourceHandler`. An `AgentAdapter` will declare detection, resource-kind support, locations, capabilities, inspection, planning, and validation for a specific runtime. A coordinator composes the two only after the user selects an operation.

This keeps Skill filesystem lifecycle semantics in `SkillHandler`, and allows `SubAgentHandler` and `MemoryHandler` to use different persistence and safety rules. It also keeps runtime paths out of the domain core. The rejected alternative is a single generic install model: links are appropriate for Skills but cannot accurately represent provider-backed Memory.

### 2. Treat capability declarations as contracts, not inferred behavior

Adapters will report supported resource kinds and named capabilities. Resource definitions will report required capabilities. Planning compares the two and produces a warning or error before any write; it never drops unsupported attributes to obtain a superficially successful installation.

This is preferred over adapters accepting arbitrary resource fields because the latter silently degrades a portable definition. It also preserves the existing distinction between target placement support and semantic instruction compatibility.

### 3. Preserve storage and journal compatibility through versioned readers

Existing `~/.agents/skills`, `.skill-manager.yaml`, project links, configuration, and journal entries remain readable. New generic records will carry a resource kind and schema version; readers identify legacy Skill records and normalize them in memory without automatically rewriting user files. The documented primary binary becomes `agent-manager`, while the old binary delegates to the same command implementation and emits a deprecation notice.

This avoids a destructive mass migration. The alternative of rewriting all local state during upgrade is rejected because project links are intentionally machine-local and users may have untracked or unmanaged content.

### 4. Add SubAgents after adapter extraction and use a canonical source of truth

Canonical SubAgent definitions live under the Agent Manager root, separate from target-native output. Install plans render the definition through a target adapter and use existing conflict, preview, confirmation, journal, and undo machinery whenever output is filesystem based. Initial supported targets are Claude Code, Codex, and Pi, but each adapter exposes only capabilities it can faithfully support.

Storing only target-native files is rejected: it prevents consistent validation, provenance, and cross-agent inspection.

### 5. Integrate shared Memory as configuration and capability control first

`MemoryProvider` is a provider adapter with a configuration reference, capability discovery, and scoped status. An agent integration maps allowed provider capabilities into that runtime's supported mechanism. One provider is selected during implementation after a feasibility check; credentials remain external to repository-managed config. Explicit promotion is required for writes; no transcript or resource content is copied automatically.

Building an internal Memory database is rejected because this issue is a control-plane evolution, not a storage product.

### 6. Ship in dependency order

Phase 1: terminology, CLI alias, generic contracts, existing Skill adapter extraction, Pi capability discovery, inventory/doctor, ADR, and migration tests.

Phase 2: canonical SubAgent registry and adapter-backed lifecycle.

Phase 3: provider model, one shared-Memory provider integration, and cross-agent status.

Issue #3's artifacts and evaluation harness remain follow-on work; this change only establishes the stable resource and agent boundaries it needs.

## Risks / Trade-offs

- [Adapter APIs can overgeneralize prematurely] → Start from current Skill operations and require an adapter capability only when a supported runtime needs it.
- [CLI rename disrupts scripts] → Keep an executable compatibility alias, behavior parity tests, and a clear deprecation notice.
- [Pi's native layouts or SubAgent support differ from assumptions] → Implement only documented/detected capabilities and emit explicit unsupported results.
- [Memory provider credentials leak into local config or journals] → Store only a non-secret configuration reference; redact sensitive values from plans, status, and journal evidence.
- [Generic journal migration breaks undo] → Maintain legacy-reader tests and record new operations in a versioned format only after normalization.
- [Broad scope delays usable output] → Gate Phases 2 and 3 on Phase 1 compatibility acceptance and ship each phase with its own fixtures.

## Migration Plan

1. Add `agent-manager` alongside the existing command and route both entrypoints to shared behavior.
2. Add compatibility readers and fixtures for existing Skill configuration, companion metadata, project links, and journals; make these tests pass before changing default behavior.
3. Extract adapters and handlers behind the existing Skill commands, then introduce inventory and diagnostics as read-only capabilities.
4. Add canonical SubAgent storage and guarded adapter-backed operations only after Phase 1 regression coverage passes.
5. Add a single shared-Memory provider path with explicit configuration and promotion commands; do not import or duplicate existing private agent memory.
6. Roll back by invoking the old command alias for Skill workflows and using the existing journal undo mechanism for reversible operations. New provider configuration is removable without deleting provider-owned data.

## Resolved Provider Boundary

The initial implementation selects a local file-backed Memory provider. It
requires an existing direct regular file, performs read-only discovery without
network access, and reserves writes for explicitly confirmed promotion. Remote
providers such as TencentDB Agent Memory and Graphiti remain unsupported and
are deferred to a follow-on change; selecting one later must not weaken the
provider-ownership, no-network-default, or explicit-promotion boundary.
