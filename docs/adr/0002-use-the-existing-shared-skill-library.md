# Use the Agent Manager-owned versioned Skill repository

This ADR supersedes the original decision to use `~/.agents/skills` as the default library.

The canonical local Skill repository is `~/.agent-manager/skills`. Agent-recognized directories such as `~/.agents` are target locations, not the default source of truth: some agents load `.agents` content automatically, so storing the entire reusable library there can expose every Skill to every project.

The repository remains local and configurable. Each stable Skill identity stores immutable versions beneath `versions/<version-key>/` and a schema-versioned `metadata.json` record. Catalog and installation workflows resolve only the configured repository and materialize only the selected version into a target agent's Global or Project location.

The former `~/.agents/skills` library is a legacy import source. Agent Manager may inspect it in a read-only preview, then copy an explicitly selected batch after confirmation. Import preserves source files and every existing activation link. A separate migration action may replace only an explicitly selected existing Claude, Codex, or Pi adapter link after revalidating its source chain and matching its content to an exact immutable repository version. Neutral `.agent` and `.agents` entries and standalone directories remain import-only. Migration never deletes the old source or implicitly updates unrelated links.

## Considered Options

- Keep `~/.agents/skills` as the default, which overlaps with an agent auto-discovery namespace.
- Silently search both old and new locations, which leaves source-of-truth and version selection ambiguous.
- Use `~/.agent-manager/skills` as the canonical versioned repository and provide an explicit confirmed import from the old path.

The third option is selected. Project activations continue to use machine-local absolute soft links. The links themselves remain authoritative project state; no project manifest is committed, in accordance with [ADR-0001](0001-project-state-is-derived-from-links.md) and [ADR-0004](0004-project-activation-is-machine-local.md).
