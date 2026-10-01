## Why

A neutral API and one CLI client do not demonstrate cross-agent Memory. Issue #7 needs two real integrations observing the same effective project knowledge and provenance.

## What Changes

- Two real agent readers and writers: Codex and Claude Code integrations SHALL explicitly write and read project knowledge through the same authorized Gateway and shared provider using the canonical neutral record identity.
- Effective version convergence: After an explicit update or supersession, both integrations SHALL observe the new effective version and SHALL NOT present a retired record as an equally current fact.
- Owner isolation and inspectable provenance: Both integrations SHALL enforce Gateway ownership checks and preserve inspectable source/evidence references on reads and effective-version results.
- Per-agent tested capability status: Status SHALL identify the read/write/search mechanism actually implemented and tested for each integration and explicitly distinguish fixture-tested behavior from live runtime evidence.
- One canonical source of truth: The shared provider SHALL remain the canonical Memory store. Enabling integrations SHALL NOT automatically duplicate records into agent-private memory or replace authoritative issue, ADR or project instruction files.

## Capabilities

### New Capabilities

- `cross-agent-shared-memory`: Two real Agent integrations read and explicitly write project knowledge through one shared Gateway, observe an updated effective version, and inspect its evidence.

### Modified Capabilities

None in the current main spec directory. Existing completed change specs remain compatibility context; this delta adds a distinct capability and preserves their defaults.

## Impact

Codex and Claude Code Memory exposure through the existing Gateway, integration capability diagnostics and an opt-in two-agent demo with the configured shared provider.

Ticket: [M5 / #23](https://github.com/AllenMuu/agent-manager/issues/23). Parent: [#7](https://github.com/AllenMuu/agent-manager/issues/7). Blocked by: #21, #22
