## Purpose

Makes the initiating actor and the authority delegated to an AgentRun explicit, inspectable, and available to the existing runtime policy and audit flow.

## ADDED Requirements

### Requirement: Runtime-neutral actor identity
Agent Manager SHALL represent a named actor with a stable ID, kind (`human`, `agent`, or `service`), subject, optional provider, and optional safe role metadata. Identity data SHALL NOT contain raw credentials, tokens, or secrets. Every new governed run SHALL receive an explicit named identity or an explicit anonymous mode; Agent Manager SHALL NOT silently infer an actor from the operating-system user or another ambient credential.

#### Scenario: Start a run with a named actor
- **WHEN** a caller creates a governed run with a valid actor identity
- **THEN** the run records the stable actor identity used for later policy decisions and audit inspection

#### Scenario: Start an explicitly anonymous run
- **WHEN** a caller explicitly selects anonymous mode
- **THEN** the run is marked anonymous and any action with an identity rule fails closed

#### Scenario: Reject secret-bearing identity data
- **WHEN** identity input contains a credential, token, or secret value
- **THEN** Agent Manager rejects or redacts that value before persistence and never exposes it in policy or audit output

### Requirement: Scoped, expiring delegation
Each delegated AgentRun SHALL carry an immutable delegation snapshot linked to its initiating actor and run. The snapshot SHALL contain a stable delegation ID, explicit exact-match scopes, and an expiry time. A delegation scope SHALL grant only the listed scope; Agent Manager SHALL NOT infer broader authority through wildcards, prefixes, or ambient credentials.

#### Scenario: Use a scope granted to the run
- **WHEN** an action requires a scope present in the run's unexpired delegation
- **THEN** the delegation check succeeds and the existing policy decision is evaluated

#### Scenario: Deny a missing or expired scope
- **WHEN** an action requires a scope absent from the delegation or the delegation has expired
- **THEN** the action is denied with a stable identity or delegation reason code before invocation

#### Scenario: Preserve delegation history
- **WHEN** a delegation source is changed after a run starts
- **THEN** the run and its audit evidence retain the original delegated scopes and expiry

### Requirement: Identity-aware policy evaluation
The existing runtime policy engine SHALL evaluate actor kind/roles and required delegation scopes alongside the run's immutable policy snapshot. Identity rules SHALL be represented by `AgentPolicy` v2 and SHALL identify an exact normalized action plus its actor-kind/role conditions and required scopes. An action is identity-bound when its action contract requires identity conditions; its v2 policy SHALL contain a matching exact-action identity rule. An identity-bound action SHALL be allowed only when the existing policy allows it and every identity condition matches. Identity rules SHALL use exact actor-kind, role, and scope identifiers. Missing identity, a missing rule, an expired delegation, a missing scope, an unsupported policy version for an identity-bound action, or a failed identity rule SHALL fail closed with stable machine-readable reason codes: `IDENTITY_REQUIRED`, `IDENTITY_POLICY_RULE_MISSING`, `IDENTITY_POLICY_UNSUPPORTED_VERSION`, `IDENTITY_POLICY_DENIED`, `DELEGATION_SCOPE_MISSING`, and `DELEGATION_EXPIRED`. Identity failures SHALL remain distinguishable from ordinary policy denials and approval requirements. A v1 policy SHALL remain readable with its existing policy-only behavior for actions without identity requirements, but SHALL NOT authorize an action that requires identity conditions.

#### Scenario: Allow a role-scoped delegated action
- **WHEN** the existing policy allows an action, the actor matches its kind/role rule, and the delegation contains every required scope
- **THEN** the policy engine returns `ALLOW` with the run, actor, delegation, and policy snapshot still correlated

#### Scenario: Deny an action when identity requirements do not match
- **WHEN** the existing policy allows an action but the actor role or required delegated scope does not match
- **THEN** the policy engine returns `DENY` with an identity-specific reason code

#### Scenario: Preserve an approval requirement after identity checks
- **WHEN** identity and delegation checks pass but the existing policy requires approval
- **THEN** the policy engine returns `REQUIRE_APPROVAL` and links the decision to the same run and policy snapshot

#### Scenario: Keep v1 policies readable without granting identity authority
- **WHEN** a v1 policy is loaded and the action contract requires identity conditions
- **THEN** the policy remains readable for its existing behavior, but the identity-bound action is denied with `IDENTITY_POLICY_UNSUPPORTED_VERSION`

### Requirement: Actor-attributed approvals and audit lineage
Governance audit records SHALL correlate the initiating actor, delegation, AgentRun, runtime, action, immutable policy snapshot, decision, approval, result, and trace ID. Approval transitions SHALL identify the actor who approved, rejected, or expired the request; the approver SHALL be explicit and SHALL NOT be inferred from the run initiator. Audit and approval records SHALL retain only stable identity references and safe identity metadata, never credential values.

When an approved action completes, its new-format completion audit record SHALL include the explicit approver ID from the linked approval, in addition to its approval ID and request-audit link, so that one structured completion record contains the full action lineage. The store SHALL validate that this approver matches the linked approval decision. Newly written audit records SHALL use format v3; existing v2 audit records without the approver field SHALL remain readable as historical evidence.

#### Scenario: Reconstruct a governed action
- **WHEN** an operator inspects a completed action's structured audit evidence
- **THEN** the evidence identifies who initiated the run, what authority was delegated, which runtime and policy snapshot applied, the decision and approval, the resulting action, and its trace correlation

#### Scenario: Read full lineage from one approved completion record
- **WHEN** an operator inspects the structured completion record for an approved action
- **THEN** that record directly contains the initiator, delegation, approver, approval, action, policy snapshot, result, and trace references

#### Scenario: Attribute an approval decision
- **WHEN** an approval is approved, rejected, or expired
- **THEN** the persisted approval and audit event identify the explicit deciding actor

#### Scenario: Keep audit data compatible with local evaluation
- **WHEN** governance events are supplied to the existing deterministic evaluation harness
- **THEN** identity and delegation references remain structured evidence and no event content is executed or sent over the network

### Requirement: Read legacy run records without inventing identity
The run store SHALL continue to read existing v1 records that lack typed identity. Such records SHALL be presented as legacy/anonymous for authorization purposes without deriving identity from free-form audit text. Read-only inspection SHALL NOT rewrite the store. Actions whose contract requires identity conditions on a legacy/anonymous run SHALL fail closed.

#### Scenario: Inspect a legacy run
- **WHEN** an operator reads a pre-identity run record
- **THEN** Agent Manager preserves its historical policy and audit evidence and reports its identity as legacy/anonymous

#### Scenario: Attempt an identity-bound action on a legacy run
- **WHEN** a policy evaluation requires an actor or delegated scope for a legacy/anonymous run
- **THEN** the action is denied with an identity-required reason and the record is not silently upgraded
