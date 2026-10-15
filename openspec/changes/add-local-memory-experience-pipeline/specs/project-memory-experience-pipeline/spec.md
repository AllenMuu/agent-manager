## Purpose

Provide a local, auditable path from completed agent-run evidence to derived memory and human-reviewed Project Memory proposals, while keeping runtime data untrusted and canonical memory project-owned.

## ADDED Requirements

### Requirement: Immutable run evidence references
The pipeline SHALL represent completed runs with provider-neutral evidence references containing the owning project, source runtime, native run identifier, source reference, creation time, stable content hash, and import status. It SHALL preserve source evidence by reference where practical, SHALL NOT mutate or become the owner of runtime run history, and SHALL NOT treat imported history as trusted Project Memory.

#### Scenario: Import a completed Agent Manager run
- **WHEN** the pipeline imports a completed Agent Manager run for an explicitly identified project
- **THEN** it stores an immutable evidence reference and stable hash of the available normalized evidence, retains the native run identity, and leaves the source run unchanged

#### Scenario: Import explicitly selected Codex and Pi sessions
- **WHEN** the operator supplies a Codex or Pi JSONL directory for an explicitly registered project
- **THEN** the provider imports only terminal sessions whose recorded working directory resolves to that project and stores a full-source digest without copying prompt or response text

#### Scenario: Reopen native evidence after its source changes
- **WHEN** a Codex or Pi source file is missing, no longer terminal, or its bytes differ from the imported digest
- **THEN** the provider returns a missing or changed-evidence diagnostic and does not extract from the stale reference

#### Scenario: Ignore incomplete runs
- **WHEN** a provider discovers a run that is still active or has no confirmed terminal outcome
- **THEN** the pipeline does not extract or promote it as completed evidence

#### Scenario: Re-import unchanged evidence
- **WHEN** a source run with the same project, runtime, native ID, and content hash is imported again
- **THEN** the pipeline returns the existing reference without duplicating evidence or changing its provenance

### Requirement: Deterministic untrusted extraction
The pipeline SHALL be able to extract structured derived memory from one completed evidence reference without an LLM or network. Each derived memory SHALL retain one or more evidence references, an explicit project owner, a content type, a confidence value, and lifecycle state. Extraction SHALL include only information present in its evidence and SHALL NOT write canonical Project Memory.

#### Scenario: Extract from a deterministic fixture
- **WHEN** a completed evidence fixture contains supported run metadata and events
- **THEN** extraction produces the same summary and structured fields on repeated runs, links them to the evidence reference, and leaves Project Memory unchanged

#### Scenario: Evidence lacks a field
- **WHEN** the source does not contain a decision, command, discovery, or result
- **THEN** extraction leaves that field absent instead of inferring or fabricating it

#### Scenario: Inspect derived memory
- **WHEN** an authorized project reader retrieves an extracted memory
- **THEN** it is marked as derived and untrusted, with source runtime and evidence provenance available

### Requirement: Single-writer consolidation and bounded local search
The pipeline SHALL serialize consolidation for each project. Consolidation SHALL merge duplicate derived memories, mark superseded information, maintain a compact generated project summary, preserve all source provenance, and bound retained derived-memory growth. V1 SHALL support local lexical search without a vector database; retrieval relevance and usage metadata SHALL NOT establish trust.

#### Scenario: Concurrent project consolidation
- **WHEN** two workers attempt consolidation for the same project concurrently
- **THEN** only one serialized consolidation result becomes current and no committed evidence or provenance is lost

#### Scenario: Consolidate duplicate and superseded information
- **WHEN** evidence contains an existing discovery and then a clearly newer replacement
- **THEN** consolidation keeps one effective derived result, marks the old result superseded, and preserves the source lineage for both

#### Scenario: Bound the active derived set
- **WHEN** consolidation exceeds the active derived-memory retention cap
- **THEN** it expires lower-ranked records without claiming they have a replacement, while preserving their evidence lineage

#### Scenario: Search with lexical retrieval
- **WHEN** an authorized caller searches local derived memory in V1
- **THEN** results are project-scoped, deterministically ordered, bounded by configured limits, and returned without requiring embeddings or an external service

### Requirement: Progressive memory retrieval
Retrieval SHALL expose a compact generated summary first, then current canonical Project Memory, then relevant derived memory, and raw source evidence only through an explicit evidence lookup. Default context assembly SHALL NOT inject full run history or every derived record, and SHALL enforce result and byte limits while retaining attribution.

#### Scenario: Default task context
- **WHEN** an agent requests project context without an explicit evidence lookup
- **THEN** it receives only the compact summary and bounded relevant canonical memory, with no raw run history injected

#### Scenario: Disable pipeline context for a handoff
- **WHEN** an operator requests role context with `--disable-memory-pipeline`
- **THEN** the handoff omits the local pipeline summary and derived memory while leaving the local database unchanged

#### Scenario: Escalate to derived memory and evidence
- **WHEN** an authorized caller requests deeper context after an initial retrieval
- **THEN** it can search relevant derived memory and explicitly open the linked source evidence while retaining project ownership checks and provenance

### Requirement: Reviewed Project Memory proposals
Consolidation SHALL be able to create a proposal linked to one or more evidence references. A derived memory or proposal SHALL remain outside canonical Project Memory until an authorized operator explicitly accepts it through the Memory Gateway. Acceptance SHALL preserve source and evidence references in the canonical record; rejection SHALL leave canonical records unchanged. An uncertain publish outcome SHALL remain inspectable and SHALL NOT be blindly retried.

#### Scenario: Create a provenance-linked proposal
- **WHEN** consolidation identifies a candidate project fact
- **THEN** it creates a pending proposal with evidence references and does not mutate canonical Project Memory

#### Scenario: Accept a proposal
- **WHEN** an authorized operator explicitly accepts a pending proposal and the Memory Gateway confirms the write
- **THEN** one canonical Project Memory record is created with the proposal's source and evidence references, and the proposal records the canonical record identity

#### Scenario: Reject a proposal
- **WHEN** an authorized operator rejects a pending proposal
- **THEN** the proposal is retained as rejected and no canonical Memory mutation occurs

#### Scenario: Canonical write outcome is uncertain
- **WHEN** proposal publication times out or its result cannot be confirmed
- **THEN** the proposal remains in an inspectable reconciliation state and the pipeline does not repeat the write automatically

### Requirement: Explicit local pipeline commands
The CLI SHALL expose explicit project-scoped operations to initialize the local pipeline, import and extract run evidence, consolidate, search derived memory, inspect summaries/evidence, and review proposals. Ordinary status and canonical read commands SHALL NOT initialize the pipeline, import evidence, or promote content.

#### Scenario: Read without enabling the pipeline
- **WHEN** an operator runs ordinary status or canonical Memory read commands for a project without an initialized pipeline
- **THEN** no pipeline database is created and no run evidence is imported

#### Scenario: Review a proposal from the CLI
- **WHEN** an operator accepts or rejects a pending proposal
- **THEN** the CLI displays the proposal and requires explicit confirmation, and only acceptance publishes through the authorized Memory Gateway

### Requirement: Usage-aware retention without trust conflation
The pipeline SHALL record derived-memory usage count, last-used time, creation/update time, confidence, and verification or proposal state independently. Any ranking function SHALL keep trust/verification separate from relevance, recency, usage, and project match.

#### Scenario: Rank two derived memories
- **WHEN** local retrieval ranks memories using relevance, recency, or usage
- **THEN** the returned score does not change either memory's trust or verification state

### Requirement: Reviewed Skill candidates
The pipeline SHALL emit a SkillCandidate only for the same explicitly generalizable procedure in at least two independent completed runs with successful or verified outcomes and no contradictory failed evidence. Procedure claims SHALL come from an evidence provider or from a previewed, operator-confirmed local annotation linked to an unchanged imported run. The pipeline SHALL NOT infer procedures or outcomes from governance logs or transcript text. A candidate SHALL expose a name, proposed workflow, confidence, project-specific marker, evidence references, and review status. A project-specific or unsupported procedure SHALL NOT become a candidate. Candidate review SHALL remain separate from repository creation and installation; the pipeline SHALL NOT automatically create, install, or publish a Skill.

#### Scenario: Record an operator-attested procedure claim
- **WHEN** an operator previews and confirms a procedure claim against an unchanged imported run
- **THEN** the bounded claim is stored as untrusted local metadata and prior candidate review becomes stale

#### Scenario: Repeated validated procedure
- **WHEN** the same stable procedure appears in multiple independent completed runs with successful or verified outcomes
- **THEN** the pipeline emits a reviewable SkillCandidate with the supporting evidence references and confidence

#### Scenario: Procedure is project-specific or unverified
- **WHEN** a procedure has no independent repetition or no explicit successful/verified result
- **THEN** the pipeline does not emit it as a SkillCandidate

#### Scenario: Successful procedure has contradictory failure evidence
- **WHEN** multiple runs report a successful or verified procedure but any imported run reports that same procedure failed
- **THEN** the pipeline does not emit the procedure as a SkillCandidate and invalidates any previously approved candidate while retaining the contradictory evidence reference

#### Scenario: Candidate evidence changes after detection
- **WHEN** new run evidence or a procedure claim is added after a SkillCandidate was detected
- **THEN** candidate review and conversion are blocked until detection re-reads the current evidence set

#### Scenario: Review a SkillCandidate
- **WHEN** a SkillCandidate is surfaced to an operator
- **THEN** it remains a candidate and the existing Skill Manager workflow controls review, scope, repository creation, and installation

#### Scenario: Explicitly convert an approved SkillCandidate
- **WHEN** an operator converts an approved candidate and confirms the displayed identifier, version, repository, and evidence
- **THEN** the repository receives one provenance-linked untrusted Skill version and no installation or activation occurs

#### Scenario: Mark generated summaries as untrusted
- **WHEN** the pipeline summary is returned to a CLI or role-context caller
- **THEN** the summary explicitly identifies its trust level as untrusted and remains separate from canonical Memory

### Requirement: Memory pipeline diagnostics
Doctor SHALL report outstanding project-memory proposals without exposing their contents or mutating proposal state. It SHALL NOT initialize an absent pipeline database.

#### Scenario: Report a pending proposal
- **WHEN** Doctor scans a project with pending, publishing, or reconciliation-required proposals
- **THEN** it reports each proposal's identifier and state for operator review, without accepting or rejecting it

#### Scenario: Scan before pipeline initialization
- **WHEN** Doctor scans a project without an initialized pipeline database
- **THEN** it returns normal diagnostics and leaves the pipeline uninitialized
