## Purpose

Let teams compare changes to Skills, prompts, context rules, role definitions, and adapter behavior against local versioned cases without requiring an agent runtime or network service.

## ADDED Requirements

### Requirement: Versioned local evaluation cases
The system SHALL load deterministic local evaluation suites containing versioned cases and a supported verifier type. New v2 cases SHALL provide a canonical intent input, explicit required and evidence points, and optional forbidden points. Existing v1 cases SHALL remain runnable with their original fields. Invalid or unsafe cases SHALL produce actionable diagnostics and SHALL NOT execute case or candidate content.

#### Scenario: List and load a valid suite
- **WHEN** an operator lists suites or runs a valid suite
- **THEN** the system reports its cases in deterministic order

#### Scenario: Reject an invalid case
- **WHEN** a suite contains malformed, unsupported, or unsafe case data
- **THEN** the runner reports the invalid case and does not execute its content

### Requirement: Deterministic evaluation results
The system SHALL evaluate responses supplied in an explicit candidate directory using the declared local rule verifier and persist versioned results containing case identifiers, an external agent label and configuration label when supplied, pass/fail/partial status, score, evidence, run timestamp, and measured duration. Missing cases within that candidate directory SHALL be represented as partial results.

#### Scenario: Run a suite against candidates
- **WHEN** an operator runs a suite with candidate responses
- **THEN** the runner records deterministic rule evidence and persists a result that identifies the suite and configuration

#### Scenario: Run without a candidate response
- **WHEN** a case has no supplied candidate response
- **THEN** its result is partial and explains that the response was missing

### Requirement: Compare evaluation runs
The system SHALL compare validated baseline and candidate results by case identifier, report regressions, improvements, new cases, and unchanged cases separately, and order each result list deterministically.

#### Scenario: Compare two evaluation runs
- **WHEN** an operator compares two valid persisted runs
- **THEN** the output identifies case regressions, improvements, additions, and unchanged cases

#### Scenario: Candidate run omits a baseline case
- **WHEN** a baseline case is absent from the candidate result
- **THEN** comparison reports that case as a regression with a missing-case reason

### Requirement: Evaluation remains local and non-executing
Evaluation SHALL NOT invoke an agent, execute candidate or fixture content, access the network, or install dependencies. Persisted runtime results SHALL be kept in local Agent Manager state rather than added to the versioned case definitions.

#### Scenario: Evaluate a candidate file
- **WHEN** the runner reads candidate content for rule checks
- **THEN** it treats the content as data and performs no code execution or network access
