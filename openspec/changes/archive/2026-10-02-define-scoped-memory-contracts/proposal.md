## Why

Current Memory APIs describe configuration and append text but cannot round-trip typed, owned knowledge or prove project isolation. A neutral behavioral contract is needed before a persistent store or external engine can be trusted.

## What Changes

- Explicit owner partitions: Every structured Memory SHALL have USER, PROJECT, AGENT or SESSION ownership with the required identifiers; a read or query SHALL bind one explicit owner partition and reject access to records belonging to another partition.
- Canonical typed record round trip: Creation, get and recall SHALL preserve the neutral ID, supported knowledge type, content, source, evidence, version, state and layer metadata without exposing provider object shapes or inventing provenance.
- Honest optional capabilities and errors: Providers SHALL distinguish supported operations from configured requests and availability, accept cancellation for I/O, and return distinguishable unsupported, unavailable, invalid, conflict and uncertain-write outcomes.
- Deterministic neutral contract implementation: A deterministic in-memory provider SHALL implement the supported canonical create/get/recall contract and run the same externally observable contract suite without a vendor client, network or model.
- Text and Skill compatibility: Structured Memory SHALL coexist with existing explicit text promotion, provider configuration and Skill workflows; ordinary resource operations SHALL NOT persist Memory, execute its SKILL or TASK content, install dependencies or access a network.

## Capabilities

### New Capabilities

- `scoped-memory-contracts`: A caller can remember and retrieve a typed, explicitly owned Memory through a provider-neutral API and deterministic in-memory provider, with provenance retained and wrong-owner access rejected.

### Modified Capabilities

None in the current main spec directory. Existing completed change specs remain compatibility context; this delta adds a distinct capability and preserves their defaults.

## Impact

Memory domain and optional provider contracts, deterministic in-memory provider, ADR 0006 and Memory terminology. Preserve existing Provider/Promote and Searcher consumers through a distinct structured contract.

Ticket: [M1 / #19](https://github.com/AllenMuu/agent-manager/issues/19). Parent: [#7](https://github.com/AllenMuu/agent-manager/issues/7). Blocked by: None (planning ready does not imply implementation approval or delivery).
