## Why

The Web console currently shows Doctor findings without a way to safely repair the cases Agent Manager already knows how to fix. Its dashboard also labels project-scan notes as Doctor findings, while adapter capability metadata appears among diagnostic results even though it is not a problem.

## What Changes

- Add guarded Web repair plans for eligible orphaned managed links and managed links that Git would otherwise track. Require review and confirmation, preserve stale-plan checks and journaling, then refresh diagnostics.
- Keep findings that cannot be repaired safely in the Web console as guidance-only; do not edit arbitrary Skill content or ambiguous links.
- Remove adapter capability inventory from Doctor findings and keep it in the adapter inventory view.
- Present project traversal notes separately from Doctor findings.

## Capabilities

### New Capabilities
- `web-diagnostic-repair`: Web Doctor findings, safe repair previews and application, guidance for findings that need manual work, and distinct project-scan notes.

### Modified Capabilities

## Impact

- `internal/diagnostic`: remove adapter capability inventory from findings and support scoped reconciliation previews.
- `internal/webconsole`: expose finding codes, repair eligibility, guarded repair plans, and execution.
- `web/src`: add repair actions, manual guidance, and separate project-scan diagnostics from Doctor findings.
- `openspec/changes/web-diagnostics-repair`: define the repair and presentation contract.
