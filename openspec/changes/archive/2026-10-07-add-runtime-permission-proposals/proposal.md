## Why

One-action approval is insufficient for changing runtime permissions and caller-supplied human labels do not establish approval authority. A narrow permission proposal must bind denial, delegation and current policy before any revision is created.

## What Changes

- Denial-bound expiring proposals: A permission proposal SHALL link the initiating identity/delegation, original denial, run, immutable base policy revision, requested capability difference and expiry.
- Trusted human decision authority: Only an authorized human from the trusted operator decision boundary SHALL approve a permission proposal. Agent self-approval and caller-provided human labels without established authority SHALL be rejected.
- Expiry and base revision checks: Decision and use SHALL reject expired proposals, expired delegation and stale base revisions rather than approving a difference against outdated authority.
- Delegation ceiling: A proposed permission difference SHALL remain within the run-bound delegation authority. A request above that ceiling SHALL be rejected even when a human requests approval.
- Distinct non-mutating proposal decisions: Rejection or expiry SHALL leave policy unchanged. Policy-change approval SHALL remain distinct from existing one-action approval and SHALL require a separate confirmed revision application before execution can resume.

## Capabilities

### New Capabilities

- `runtime-permission-proposals`: A denied runtime action creates a run-bound, expiring permission proposal with its base policy revision and requested difference; an authorized human can decide it through a trusted operator boundary.

### Modified Capabilities

None in the current main spec directory. Existing completed change specs remain compatibility context; this delta adds a distinct capability and preserves their defaults.

## Impact

Permission proposal model/service, trusted local operator decision boundary, delegation/base-revision validation and audit tests; existing action approval semantics remain distinct.

Ticket: [R3 / #26](https://github.com/AllenMuu/agent-manager/issues/26). Parent: [#16](https://github.com/AllenMuu/agent-manager/issues/16). Blocked by: #25
