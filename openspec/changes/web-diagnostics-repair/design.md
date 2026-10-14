## Context

See `proposal.md` and `specs/web-diagnostic-repair/spec.md`. The Go Doctor currently emits both real findings and adapter capability metadata. The Web console renders Doctor findings as a read-only list, while a separate project marker scan supplies the dashboard's scan-note count. The operation-plan API already provides expiring, single-use, project-bound previews, confirmation, journaling, stale-state rejection, and Undo.

## Goals / Non-Goals

**Goals:**
- Reuse existing guarded Doctor repair operations in the Web console.
- Limit each repair to the exact finding selected by the operator.
- Keep project-scan notes and Doctor findings visually distinct.
- Keep adapter capability inventory in the existing agent inventory view.

**Non-Goals:**
- General file editing or arbitrary path mutations through the Web API.
- Automatically editing malformed Skill content, unmanaged resources, ambiguous links, unsupported locations, or the Git index.
- Changing the existing CLI reconciliation scope or its confirmation behavior.

## Decisions

- Remove adapter capability records from `diagnostic.Scan`; those values are already served by the agent inventory API and rendered in the workspace.
- Include a stable finding code plus a bounded repair action or guidance reference in Web Doctor DTOs. Derive repair eligibility from a fresh server-side scan rather than trusting client-provided codes or targets.
- Add a scoped reconciliation entry point for one selected link while keeping the existing all-eligible-links `Reconcile` entry point for CLI use. The Web API accepts a project finding path, then verifies it is a current `doctor.orphaned_managed_link` before planning.
- Reuse `AddGitignore` for a finding with status `would be tracked`. Do not offer that action for an already tracked link because `.gitignore` cannot remove it from the index; avoid implicit staging changes.
- Build each preview with the existing operation plan callback before any mutation. Fingerprint the finding and complete plan, then re-evaluate at execution and compare again in the domain confirmation callback immediately before publication. Treat changed or empty plans as stale.
- Store Doctor repairs in the existing expiring, single-use operation-plan store. On confirmation, journal with the registered project's journal and use the existing shared plan review and Undo view.
- Render project scan notes in a separate section fed by the existing recommendations response. Rename the workspace summary so its count describes only those notes.

## Risks / Trade-offs

- A managed link may lose eligibility between preview and execution → re-scan and compare plans, then rely on the domain operation's final ownership and placement checks before publishing.
- Some findings are not safely repairable in-product → display explicit guidance and withhold repair controls; do not promise a universal one-click fix.
- The existing Git-ignore repair only appends a scoped rule and does not untrack staged paths → offer it only for paths Git would otherwise track, and explain that already tracked paths require manual index review.
- Per-finding preview invokes filesystem and Git inspection → keep it limited to the current registered project and the paths being displayed.
