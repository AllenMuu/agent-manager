## Why

The CLI currently exposes promotion/status while task-context Memory retrieval is not wired to the configured provider. Operators and task handoffs need one authorized, bounded access path with inspectable provenance.

## What Changes

- Truthful provider discovery: Provider and agent status SHALL distinguish configured requests, implemented read/write/search capabilities, current availability and explicit gaps without claiming unavailable features as supported.
- Confirmed owned mutations: CLI add, update, supersede, forget and import SHALL preview the authorized owner and intended change and require explicit confirmation before the Gateway mutates Memory.
- Bounded deterministic retrieval: Search SHALL authorize and constrain ownership before provider access, filter type and effective state, enforce a maximum result count, content/context budget and configured relevance threshold, and use stable fallback ordering when reranking is unavailable.
- Shared read-only task-context path: Task context and CLI SHALL use the same Memory Gateway and preserve record identifiers and source metadata. A context handoff SHALL NOT implicitly write or promote Memory.
- Independent failure and recovery semantics: Memory provider failures SHALL report diagnostic error categories without corrupting or blocking unrelated Skill/SubAgent operations. Memory mutations SHALL NOT be represented as reversible Skill filesystem journal operations.

## Capabilities

### New Capabilities

- `memory-gateway-and-cli`: An operator can add, inspect, search, update, supersede and forget Memory through the CLI, and the same authorized Gateway supplies bounded, attributed knowledge to a task-context handoff.

### Modified Capabilities

None in the current main spec directory. Existing completed change specs remain compatibility context; this delta adds a distinct capability and preserves their defaults.

## Impact

Memory Gateway, Memory CLI, provider discovery and task-context/roles handoff. ADR ownership rules and stable local project identity mapping; unrelated Skill operations stay independent.

Ticket: [M3 / #21](https://github.com/AllenMuu/agent-manager/issues/21). Parent: [#7](https://github.com/AllenMuu/agent-manager/issues/7). Blocked by: #20
