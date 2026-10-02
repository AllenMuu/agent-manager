# scoped-memory-contracts Specification

## Purpose

A caller can remember and retrieve a typed, explicitly owned Memory through a provider-neutral API and deterministic in-memory provider, with provenance retained and wrong-owner access rejected. This capability provides a separately verifiable slice of parent issue #7.

## Requirements

### Requirement: Explicit owner partitions
Every structured Memory SHALL have USER, PROJECT, AGENT or SESSION ownership: USER requires a user ID, PROJECT a project ID, AGENT an owning user/project plus agent ID, and SESSION an owning user/project plus session ID; unknown ownership kinds including GLOBAL are rejected; a read or query SHALL bind one explicit owner partition and reject access to records belonging to another partition.

#### Scenario: Missing ownership identifiers
- **WHEN** a caller creates USER without user ID, PROJECT without project ID, AGENT without its owning scope or agent ID, or SESSION without its owning scope or session ID
- **THEN** creation is rejected with invalid-input evidence and no record is stored

#### Scenario: Two-project isolation
- **WHEN** project B gets or recalls a Memory belonging to project A
- **THEN** the record is not returned and the get reports an ownership-safe rejection or not-found result

### Requirement: Canonical typed record round trip
Creation, get and recall SHALL preserve the neutral ID, supported knowledge type, content, source, evidence, version, state and layer metadata without exposing provider object shapes or inventing provenance. Initial knowledge types SHALL include FACT, DECISION, PREFERENCE, CONSTRAINT, EXPERIENCE, PROCEDURE, SKILL, TASK, ERROR_SOLUTION and PROJECT_CONTEXT; RAW, ATOMIC, COMPOSITE and PROFILE SHALL be layer metadata without claiming automatic consolidation.

#### Scenario: Remember and recall project knowledge
- **WHEN** a PROJECT CONSTRAINT with source and two evidence references is remembered then fetched and recalled
- **THEN** all canonical metadata is preserved, its version is one and the same neutral ID is returned

### Requirement: Honest optional capabilities and errors
Providers SHALL distinguish supported operations from configured requests and availability, accept cancellation for I/O, and return distinguishable unsupported, unavailable, invalid, conflict and uncertain-write outcomes.

#### Scenario: Unsupported recall versus unavailable storage
- **WHEN** one provider lacks recall and another supports recall but is unavailable
- **THEN** the first returns unsupported and the second unavailable; neither reports an empty successful search

#### Scenario: Canceled operation
- **WHEN** the operation context is canceled before storing a record
- **THEN** the operation returns canceled and no successful write is reported

### Requirement: Deterministic neutral contract implementation
A deterministic in-memory provider SHALL implement the supported canonical create/get/recall contract and run the same externally observable contract suite without a vendor client, network or model.

#### Scenario: Portable contract suite
- **WHEN** the in-memory provider stores records in two owner partitions and the contract suite runs repeatedly
- **THEN** the owned round trips and ordering are deterministic and no network or model is started

### Requirement: Text and Skill compatibility
Structured Memory SHALL coexist with existing explicit text promotion, provider configuration and Skill workflows; ordinary resource operations SHALL NOT persist Memory, execute its SKILL or TASK content, install dependencies or access a network.

#### Scenario: Legacy promotion remains available
- **WHEN** an existing confirmed text-promotion or managed-skill operation is invoked
- **THEN** its documented behavior is preserved and no structured import, network operation or content execution occurs
