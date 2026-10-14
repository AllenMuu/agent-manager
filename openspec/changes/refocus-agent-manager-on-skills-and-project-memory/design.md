## Context

See `proposal.md` for the product direction and `specs/skill-repository/spec.md` for the new behavior contract. Existing Skill catalog and installation changes assume `~/.agents/skills`; ADR-0002 records the same default. Project activation is already represented by machine-local absolute soft links, with no committed manifest (ADR-0001 and ADR-0004). The deterministic project recommendation and guarded install flows are separate capabilities and should continue to consume the configured repository.

The existing cross-agent Project Memory capability covers Codex and Pi. This change leaves its contract intact and does not expand Memory behavior.

## Goals / Non-Goals

**Goals:**

- Give the Skill catalog one canonical, Agent Manager-owned local repository and preserve distinct, addressable Skill versions and provenance.
- Move existing libraries through read-only preview and explicit batch import while keeping legacy content and project links usable.
- Adapt existing catalog, recommendation, installation, and agent adapters to resolve selected versions from the repository.
- Keep project activation local, explicit, and derived from links.

**Non-Goals:**

- Implement or expand agent execution, checkpoint/resume, compaction, workflow orchestration, sandboxing, model/provider management, or coding-agent installation.
- Expand shared Project Memory in this change; continue to treat it as the next roadmap phase after the Skill core.
- Discover and rewrite every project link on a machine, add a committed project manifest, or automatically prune the legacy library.
- Fetch remote Skills or execute Skill content during catalog, import, install, or recommendation workflows.

## Decisions

### Use `~/.agent-manager/skills` as the canonical repository

The repository root is independent from agent-owned `.agents` directories, which some agents scan automatically. Catalog, install, and recommendation resolve from the configured path only; the old path is a migration source, not an implicit fallback. Retain the existing local path override for users who need to place the repository elsewhere.

Alternatives considered: keep `~/.agents/skills`, which preserves current links but keeps the library inside an auto-discovered namespace; use an OS-specific application-data directory, which adds portability/configuration work without improving the user-selected `.agent-manager` convention.

### Store immutable versions beneath a stable Skill identity

Use `~/.agent-manager/skills/<skill-id>/versions/<version-key>/` for Skill content and `<skill-id>/metadata.json` for the identity, selected version, provenance, and trust state. Metadata records include a schema version and support source/source type, publisher, immutable ref or checksum, timestamps, and trust/verification state. Keep a legacy Skill with no declared version as `unknown` in metadata and use a repository-generated, non-semantic storage key; do not invent a published version. Existing identifiers resolve through the repository API to a specific version directory; adapters receive that resolved directory rather than scanning the whole repository. Import existing `.skill-manager.yaml` fields such as tags and compatibility into the repository metadata model without treating them as provenance proof.

Alternatives considered: overwrite a single directory on update, which loses rollback and provenance; keep each version as an unrelated top-level catalog entry, which fragments Skill identity and complicates recommendations and activation; use `.skill-manager.yaml` as the only repository index, which does not naturally model a Skill with multiple versions. Retain `.skill-manager.yaml` as compatible per-version metadata input where present, while `metadata.json` is the canonical repository index.

### Import legacy Skills by copy, not implicit relocation

Preview validates eligible `SKILL.md` directories, proposes stable destination identifiers, reports missing metadata and collisions, and is read-only. A user-confirmed batch copies only selected entries into the new repository. Conflicts never overwrite either side, and a failed batch rolls back its new writes. The importer does not rewrite project links or delete legacy content; the existing absolute links continue to resolve until the user separately activates the new repository version and explicitly handles cleanup.

Alternatives considered: make the new library silently search the old path, which leaves source-of-truth ambiguous; move directories and rewrite links automatically, which cannot safely account for arbitrary project locations without a committed project manifest or a complete machine-wide link index.

### Preserve Global and Project activation as machine-local links

Keep the current scope model. Global and Project activations are explicit links to a selected repository version. Project inventory and reconciliation derive state from those links; do not add a committed manifest. A migration import does not change active links. Users can apply the normal guarded activation workflow to move a project to the new repository after reviewing its plan.

Alternatives considered: store portable project activation state in Git, which would encode machine-specific paths; automatically scan all of a user's projects during import, which would be incomplete and surprising.

### Ship the repository foundation before later roadmap phases

The first implementation slice covers the canonical repository, metadata/version resolution, explicit legacy import, and path reconciliation across existing Skill specs and ADRs. Reuse the current project recommendation and guarded install behavior. Follow with broader Skill health/effective-set capabilities, then expand Project Memory across agents. Runtime/control-plane capabilities already delivered remain available when they directly support these workflows; unrelated additions are not core roadmap work.

## Risks / Trade-offs

- [Existing specs and ADR-0002 disagree with the selected default] → Update the active Skill catalog and installation contracts and supersede ADR-0002 in the implementation slice; keep the old path only as an explicit migration source.
- [Two library copies can coexist during migration] → Show canonical source and version in catalog output; do not merge the legacy path implicitly; make cleanup a separate confirmed operation.
- [Legacy Skills may lack trustworthy version or source data] → Record them as legacy/unknown and never synthesize publisher, source, or trust claims from content.
- [Versioned storage changes the path shape expected by current adapters] → Centralize identifier/version resolution in the Skill repository boundary and pass the resolved version directory to existing lifecycle and adapter code.
- [A user may have active project links unknown to the importer] → Leave existing links and sources untouched during import; use the existing per-project preview/confirm activation workflow for each transition.
- [Skill files may contain symlinks or unsafe paths] → Validate destinations remain within the repository, reject escaping links for managed writes, and preserve the existing no-execution/no-network boundary.

## Migration Plan

1. Reconcile the configured-library contract and ADR-0002 to make `~/.agent-manager/skills` canonical; retain explicit custom-path configuration.
2. Initialize the versioned repository without altering `~/.agents/skills`.
3. Preview the legacy library, select a batch, resolve conflicts, and confirm a copy into the new repository. Preserve the source library and existing project links.
4. Activate selected versions through the existing guarded Global or Project workflow. Record project state through the resulting local links.
5. Offer any legacy cleanup only as a later, separate previewed and explicitly confirmed operation after users have reviewed their remaining links.

Rollback before import confirmation is a no-op. A failed confirmed import restores only files created by that batch. Since the source and existing links are not changed, rollback does not need to reconstruct the old library.

## Open Questions

None. The repository location, migration safety boundary, activation model, and roadmap ordering were settled during the planning discussion.
