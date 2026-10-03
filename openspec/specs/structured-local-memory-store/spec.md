# structured-local-memory-store Specification

## Purpose

A caller can persist owned Memory locally, reopen it, conditionally update or supersede it, and forget it without losing concurrent writes or automatically rewriting existing text stores. This capability provides a separately verifiable slice of parent issue #7.

## Requirements

### Requirement: Durable owner and provenance
Stored records SHALL preserve neutral identifiers, ownership, source, evidence, state and versions across closing and reopening the structured provider.

#### Scenario: Reopen owned record
- **WHEN** a record is written and a new provider instance opens the same store
- **THEN** get returns the same owner, ID, provenance and version and another project cannot retrieve it

### Requirement: Conditional serialized updates
Concurrent updates SHALL compare an explicit expected version, serialize the full storage transaction across store instances and processes, and reject stale versions without losing other committed records.

#### Scenario: Competing version updates
- **WHEN** two independent store instances update the same record from version one
- **THEN** one commits version two and the other reports conflict; unrelated committed records remain present

### Requirement: Atomic lifecycle and history
Supersession SHALL atomically create a new neutral record and retire the old one; normal recall SHALL exclude SUPERSEDED and DELETED records, while explicit authorized history inspection SHALL retain their lineage.

#### Scenario: Supersede and forget
- **WHEN** an ACTIVE record is superseded and the replacement is subsequently forgotten
- **THEN** current recall returns neither retired record; history explains the supersedes relationship and deletion tombstone

#### Scenario: Supersession fails before commit
- **WHEN** storage fails while preparing a replacement
- **THEN** the old record remains ACTIVE and no partially current replacement is reported

### Requirement: Recoverable local writes
A failed write SHALL preserve prior valid data and report its confirmed or uncertain outcome. Duplicate operation identifiers SHALL NOT repeat committed mutations or accept a different intent.

#### Scenario: Failure and retry evidence
- **WHEN** a write fails during persistence and the caller retries a previously committed operation ID
- **THEN** existing data remains readable and a committed operation is recognized without another version increment

### Requirement: Explicit legacy import
Legacy text import SHALL require a separately confirmed owner and source; opening or discovering a structured provider SHALL NOT automatically convert existing text stores.

#### Scenario: No implicit conversion
- **WHEN** status or reopen sees a legacy text file
- **THEN** the file is unchanged and import is not attempted

#### Scenario: Confirmed import
- **WHEN** the operator confirms an import with explicit owner and source
- **THEN** the created canonical records retain that declared source without claiming undocumented historical evidence
