# Existing Skill references migrate through explicit recoverable operations

Importing a Skill and changing an Agent-visible entry are separate actions. Source discovery is read-only and limited to supported Global and registered-Project roots. An entry link may be resolved through a bounded chain for inspection, but only an explicitly selected existing Claude, Codex, or Pi adapter link can be migrated. Neutral directories and standalone real directories remain import-only. The source directory and all unselected links remain in place.

Every managed activation is bound by scope, target Agent, destination entry path, Skill identity, immutable version, checksum, and actual materialization method. Resynchronization reads that binding per entry; it does not merge entries by Skill ID or substitute the library's current version. The existing separate Global and Project operation journals and locks remain the transaction boundaries.

Before mutation, a durable batch record stores the before-state, required backups, dependencies, and intended writes. Each publication has durable intent. A pending batch is inspected without mutation and requires an explicit recovery preview and confirmation. Undo validates a simulated view of the batch's restored before-state, allowing dependencies changed by the same batch while requiring external dependencies to match recorded evidence. Source-discovery records are local operational evidence separate from immutable version provenance.

## Considered Options

- Automatically replace every discovered source link during import, which would implicitly activate Skills and could change unregistered projects.
- Keep sources read-only and never offer migration, which leaves selected Agent entries unmanaged after import.
- Require a separate reviewed migration per selected supported link and keep source content, unselected entries, scope logs, and recovery evidence.

The third option is selected. It keeps adoption explicit, limits writes to adapter-owned entries, and makes crash recovery and Undo evidence part of the local operation contract.
