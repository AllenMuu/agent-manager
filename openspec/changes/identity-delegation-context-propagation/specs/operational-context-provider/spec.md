## Purpose

Defines a narrow read-only boundary for supplying agents with freshness-aware operational context under an exact-key read policy that is independent from mutation-tool authorization.

## ADDED Requirements

### Requirement: Read-only operational context records
An operational context provider SHALL return structured context records with a source, capture timestamp, and freshness information. Retrieval SHALL be read-only and SHALL be authorized by a separate exact-key access policy, independent from mutation-tool authorization and the mutation policy engine. Every read query SHALL name one or more keys, and every requested key SHALL be explicitly allowlisted before the provider is called. A provider response SHALL NOT expose keys outside the authorized query. Missing or empty read policy SHALL fail closed. This change SHALL define the provider contract without requiring a concrete provider implementation.

#### Scenario: Read context with source and freshness
- **WHEN** a consumer requests operational context from a configured provider
- **THEN** each returned record identifies its source and capture time and communicates whether it is still fresh

#### Scenario: Keep context retrieval separate from mutations
- **WHEN** a consumer retrieves operational context
- **THEN** the separate context-read policy decides access before the provider is called, and no mutation tool is invoked

#### Scenario: Deny an unlisted context key before retrieval
- **WHEN** a query requests a key that is not explicitly allowed by the context-read policy, or no read policy is configured
- **THEN** the read is denied and the provider is not called

#### Scenario: Allow a specifically authorized context query
- **WHEN** a query names only keys explicitly allowed by the context-read policy
- **THEN** the provider may be called for those keys without evaluating mutation-tool authorization

#### Scenario: Reject context outside the authorized query
- **WHEN** a provider returns a record for a key that the authorized query did not request
- **THEN** the service rejects the provider response without exposing the extra record

#### Scenario: Define the boundary without a provider
- **WHEN** Agent Manager is built without a concrete operational context provider
- **THEN** an authorized read reports the provider as unavailable rather than inventing context, and identity, policy, invocation, and audit workflows remain usable
