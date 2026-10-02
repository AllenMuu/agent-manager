## Purpose

Two real Agent integrations read and explicitly write project knowledge through one shared Gateway, observe an updated effective version, and inspect its evidence. This capability provides a separately verifiable slice of parent issue #7.

## ADDED Requirements

### Requirement: Two real agent readers and writers
Codex and Claude Code integrations SHALL explicitly write and read project knowledge through the same authorized Gateway and shared provider using the canonical neutral record identity. Parent issue #7 acceptance SHALL additionally require the actual Mem0 provider path, rather than combining offline Mem0 fixtures with a local-provider agent demonstration.

#### Scenario: Codex writes and Claude reads
- **WHEN** Codex explicitly writes a PROJECT CONSTRAINT through its supported integration and Claude Code queries the project through its supported integration
- **THEN** Claude receives the same canonical record and provenance; the evidence identifies both actual integration mechanisms

#### Scenario: Real Mem0 two-agent acceptance
- **WHEN** the explicitly enabled integration demo runs Codex and Claude Code against the same real version-pinned Mem0 service through the Gateway
- **THEN** Codex writes a project constraint, Claude reads the same neutral record, an explicit update or supersession becomes effective for both agents, and inspection preserves source/evidence; the demo records the actual Mem0 and agent versions/environment and is required before closing parent #7

### Requirement: Effective version convergence
After an explicit update or supersession, both integrations SHALL observe the new effective version and SHALL NOT present a retired record as an equally current fact.

#### Scenario: Both agents observe replacement
- **WHEN** one integration confirms an update or supersession and both integrations repeat recall
- **THEN** both see the new effective version/record and can inspect the prior lineage separately

### Requirement: Owner isolation and inspectable provenance
Both integrations SHALL enforce Gateway ownership checks and preserve inspectable source/evidence references on reads and effective-version results.

#### Scenario: Cross-project read attempt
- **WHEN** either integration requests another project outside its authorized context
- **THEN** the Gateway rejects access and no foreign record is returned

### Requirement: Per-agent tested capability status
Status SHALL identify the read/write/search mechanism actually implemented and tested for each integration and explicitly distinguish fixture-tested behavior from live runtime evidence.

#### Scenario: One integration lacks write support
- **WHEN** a runtime exposes only the configured read mechanism
- **THEN** status reports read-only access and does not advertise confirmed write support

### Requirement: One canonical source of truth
The shared provider SHALL remain the canonical Memory store. Enabling integrations SHALL NOT automatically duplicate records into agent-private memory or replace authoritative issue, ADR or project instruction files.

#### Scenario: Integration is enabled
- **WHEN** an operator configures Codex and Claude Code access to shared Memory
- **THEN** only access configuration is added; canonical records are not copied into private stores
