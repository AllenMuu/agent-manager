## Purpose

Defines a narrow read-only boundary for supplying agents with freshness-aware operational context independently from policy enforcement and mutation tools.

## ADDED Requirements

### Requirement: Read-only operational context records
An operational context provider SHALL return structured context records with a source, capture timestamp, and freshness information. Retrieval SHALL be read-only by default and SHALL remain separate from mutation-tool authorization and policy evaluation. This change SHALL define the provider contract without requiring a concrete provider implementation.

#### Scenario: Read context with source and freshness
- **WHEN** a consumer requests operational context from a configured provider
- **THEN** each returned record identifies its source and capture time and communicates whether it is still fresh

#### Scenario: Keep context retrieval separate from mutations
- **WHEN** a consumer retrieves operational context
- **THEN** no mutation tool is invoked and the read operation is governed independently from mutation authorization

#### Scenario: Define the boundary without a provider
- **WHEN** Agent Manager is built without a concrete operational context provider
- **THEN** identity, policy, invocation, and audit workflows remain usable and report the provider as unavailable rather than inventing context
