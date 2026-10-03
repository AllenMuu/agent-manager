## Purpose

An operator can use a pinned Mem0 OSS REST contract through the same canonical Memory API, without exposing provider IDs or credentials as the public model. This capability provides a separately verifiable slice of parent issue #7.

## ADDED Requirements

### Requirement: Canonical remote operation mapping
The configured Mem0 provider SHALL map create, get, recall, update, forget and health/status to one pinned OSS REST contract while preserving canonical ownership, neutral IDs, source, evidence and observable versions.

#### Scenario: Remote canonical round trip
- **WHEN** a typed project record is created, fetched, searched and updated through the Mem0 adapter
- **THEN** the same neutral identity and owned provenance are returned, then forget removes it from ordinary recall

### Requirement: Honest remote semantic capabilities
The adapter SHALL report any backend operation or concurrency semantics it cannot guarantee, including atomic conditional update and supersession, and SHALL NOT claim success for an unsupported canonical operation.

#### Scenario: Backend cannot atomically supersede
- **WHEN** the selected server supports independent writes but no atomic supersession
- **THEN** capability status marks the guarantee unsupported and the adapter rejects the atomic operation before a partial mutation

### Requirement: Bounded transport and uncertain writes
Remote operations SHALL honor timeout/cancellation and distinguish authentication failure, service unavailability, conflict and uncertain mutation outcomes. An uncertain write SHALL NOT be automatically replayed.

#### Scenario: Write response times out
- **WHEN** the server may have accepted a mutation but its response is lost
- **THEN** the caller sees outcome unknown and the client does not issue a second write

#### Scenario: Authentication failure
- **WHEN** the server rejects the credential
- **THEN** the caller sees authentication failure rather than an empty successful result

### Requirement: Opaque secret references
Provider credentials SHALL be resolved from explicit secret references at the transport boundary; raw credentials SHALL NOT enter canonical records, configuration journals, audit, CLI status or diagnostics.

#### Scenario: Authenticated request diagnostics
- **WHEN** an authenticated request fails and its status/error is rendered
- **THEN** the credential value is absent from persisted and rendered evidence

### Requirement: Offline contract and opt-in live evidence
The Mem0 adapter SHALL pass pinned offline HTTP contract fixtures; real-service validation SHALL be opt-in and record the exact tested server contract version and environment.

#### Scenario: Ordinary offline test run
- **WHEN** the Go test suite runs without Mem0 installed
- **THEN** fixtures validate the API behavior without fetching dependencies or starting a service

#### Scenario: Explicit live smoke
- **WHEN** an operator enables the real-service smoke against a selected server
- **THEN** the resulting evidence records the tested version and cannot be confused with fixture-only coverage
