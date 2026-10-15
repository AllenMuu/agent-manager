## Why

Users can import a Skill while existing global and project links continue to point at unmanaged source directories. Switching those links safely requires exact source evidence, per-entry version ownership, and recovery that survives process termination; the current in-memory rollback and post-write journal are insufficient.

## What Changes

- Discover supported Skill entry links, resolve bounded relative and multi-hop chains, and show source, reference, link-chain, and eligibility evidence.
- Keep source import separate from explicitly reviewed migration of selected existing Claude, Codex, or Pi links; neutral directories and real directories remain import-only.
- Persist source-discovery records independently from immutable version provenance, including when an existing version is reused.
- Bind each managed activation to its scope, agent entry, Skill identity, exact version, checksum, and materialization method so same-ID entries can retain different versions.
- Add durable prepared batches, explicit recovery previews, transaction-owned rollback, and dependency-aware Undo while retaining separate Global and Project locks and journals.
- Add authenticated local API and bilingual Storage & Sync workflows for migration, recovery, impact, and Undo.

## Capabilities

### New Capabilities

None. This change extends the existing source-sync and project-installation capabilities.

### Modified Capabilities

- `webui-skill-source-sync`: resolve supported entry links safely, preserve source relationships, and persist confirmed discovery evidence.
- `project-skill-installation`: bind materialization to individual entries and support explicit reference migration, durable recovery, and dependency-aware Undo.

## Impact

- Affected code: `internal/skillsync`, `internal/lifecycle`, `internal/operation`, `internal/skillrepo`, `internal/webconsole`, and `web/src` plus embedded console assets.
- Affected local state: `~/.agent-manager/skill-sync` gains source-discovery and recoverable-batch records; existing repository versions and scoped operation journals remain authoritative.
- Affected architecture decisions: project activations remain machine-local; imports preserve sources, while a separate confirmed migration may replace only selected supported links.
- No new network access, Skill execution, project manifest, or cross-scope transaction is introduced.
