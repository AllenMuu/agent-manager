## Why

Typed Memory needs durable, owner-preserving storage with a safe update lifecycle. Existing append-only text files cannot provide versions, current-state filtering or atomic supersession.

## What Changes

- Durable owner and provenance: Stored records SHALL preserve neutral identifiers, ownership, source, evidence, state and versions across closing and reopening the structured provider.
- Conditional serialized updates: Concurrent updates SHALL compare an explicit expected version, serialize the full storage transaction across store instances and processes, and reject stale versions without losing other committed records.
- Atomic lifecycle and history: Supersession SHALL atomically create a new neutral record and retire the old one; normal recall SHALL exclude SUPERSEDED and DELETED records, while explicit authorized history inspection SHALL retain their lineage.
- Recoverable local writes: A failed write SHALL preserve prior valid data and report its confirmed or uncertain outcome. Duplicate operation identifiers SHALL NOT repeat committed mutations or accept a different intent.
- Explicit legacy import: Legacy text import SHALL require a separately confirmed owner and source; opening or discovering a structured provider SHALL NOT automatically convert existing text stores.

## Capabilities

### New Capabilities

- `structured-local-memory-store`: A caller can persist owned Memory locally, reopen it, conditionally update or supersede it, and forget it without losing concurrent writes or automatically rewriting existing text stores.

### Modified Capabilities

None in the current main spec directory. Existing completed change specs remain compatibility context; this delta adds a distinct capability and preserves their defaults.

## Impact

A separately selected structured local Memory provider, local storage and contract/race tests. No rewrite of existing text files or operation-journal semantics.

Ticket: [M2 / #20](https://github.com/AllenMuu/agent-manager/issues/20). Parent: [#7](https://github.com/AllenMuu/agent-manager/issues/7). Blocked by: #19
