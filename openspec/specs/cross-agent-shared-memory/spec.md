# cross-agent-shared-memory Specification

## Purpose

Two real Agent integrations read and explicitly write project knowledge through one shared Gateway, observe an updated effective version, and inspect its evidence. This capability provides a separately verifiable slice of parent issue #7.

## Requirements

### Requirement: Two real agent readers and writers
Codex and Pi integrations SHALL explicitly write and read project knowledge through the same authorized Gateway and shared provider using the canonical neutral record identity. Issue #23 SHALL verify these integrations against the same actual version-pinned Mem0 provider through the Gateway; offline Mem0 fixtures combined with a local-provider two-agent demo do not satisfy this ticket. This evidence supports the P1 cross-agent slice of #7 without adding a Mem0 requirement to the parent issue's P0 implementation.

#### Scenario: Codex writes and Pi reads
- **WHEN** Codex explicitly writes a PROJECT CONSTRAINT through its supported integration and Pi queries the project through its supported integration
- **THEN** Pi receives the same canonical record and provenance; the evidence identifies both actual integration mechanisms

#### Scenario: Real Mem0 two-agent acceptance
- **WHEN** the explicitly enabled integration demo runs Codex and Pi against the same real version-pinned Mem0 service through the Gateway
- **THEN** Codex writes a project constraint, Pi reads the same neutral record, an explicit update or supersession becomes effective for both agents, and inspection preserves source/evidence; the demo records the actual Mem0 and agent versions/environment, satisfies issue #23's real-provider acceptance and supplies evidence for the parent #7 P1 cross-agent slice

### Requirement: Interoperable MCP tool schemas
The shared Memory MCP server SHALL advertise each registered tool with a valid object input schema. A schema's `required` field SHALL be an array, including an empty array for tools that take no arguments.

#### Scenario: Pi lists and calls shared Memory tools
- **WHEN** Pi connects through the documented `pi-mcp-adapter` and lists the configured Memory tools
- **THEN** Pi accepts each advertised input schema, lists `memory_status` with no required arguments, and can call the configured tools directly without falling back to an unconfigured proxy operation

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
- **WHEN** an operator configures Codex and Pi access to shared Memory
- **THEN** only access configuration is added; canonical records are not copied into private stores
