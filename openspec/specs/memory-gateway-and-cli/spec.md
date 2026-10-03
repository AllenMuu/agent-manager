# memory-gateway-and-cli Specification

## Purpose

An operator can add, inspect, search, update, supersede and forget Memory through the CLI, and the same authorized Gateway supplies bounded, attributed knowledge to a task-context handoff. This capability provides a separately verifiable slice of parent issue #7.

## Requirements

### Requirement: Truthful provider discovery
Provider and agent status SHALL distinguish configured requests, implemented read/write/search capabilities, current availability and explicit gaps without claiming unavailable features as supported.

#### Scenario: Configured search is not implemented
- **WHEN** configuration requests search but the selected provider lacks it
- **THEN** status reports the request and unsupported implementation separately rather than advertising working search

### Requirement: Confirmed owned mutations
CLI add, update, supersede, forget and import SHALL preview the authorized owner and intended change and require explicit confirmation before the Gateway mutates Memory.

#### Scenario: Mutation is not confirmed
- **WHEN** an operator previews a write or deletion but does not confirm it
- **THEN** no provider mutation is performed

#### Scenario: Stable project identity
- **WHEN** two repositories have the same basename or a registered project is relocated
- **THEN** their identities remain distinct and relocation requires an explicit mapping update

### Requirement: Bounded deterministic retrieval
Search SHALL authorize and constrain ownership before provider access, filter type and effective state, enforce a maximum result count, content/context budget and configured relevance threshold, and use stable fallback ordering when reranking is unavailable.

#### Scenario: Bounded attributed search
- **WHEN** search returns more current matches than the result/context budget permits and no reranker is available
- **THEN** selected items use deterministic ranking with stable ID ties, fit both limits and retain source attribution

#### Scenario: Wrong-owner query
- **WHEN** the caller requests a project outside its authorized context
- **THEN** the Gateway rejects it before querying the provider

### Requirement: Shared read-only task-context path
Task context and CLI SHALL use the same Memory Gateway and preserve record identifiers and source metadata. A context handoff SHALL NOT implicitly write or promote Memory.

#### Scenario: Context handoff reads knowledge
- **WHEN** roles/task-context requests project knowledge through the configured provider
- **THEN** it receives bounded attributed records from the same Gateway path as CLI search and no write occurs

### Requirement: Independent failure and recovery semantics
Memory provider failures SHALL report diagnostic error categories without corrupting or blocking unrelated Skill/SubAgent operations. Memory mutations SHALL NOT be represented as reversible Skill filesystem journal operations.

#### Scenario: Provider outage
- **WHEN** a Memory query fails and an unrelated Skill inspection or operation follows
- **THEN** the query reports unavailable and the Skill workflow remains functional; no Memory undo promise is made
