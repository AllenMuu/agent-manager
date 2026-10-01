# Agent Manager

**Agent Manager**:
The local control plane that governs agent resources across a shared Skill library, agent-wide locations, and individual repositories. Skills are the first managed resource kind; the product name does not replace Skill-domain vocabulary.
_Avoid_: Skill Manager (except when referring to the compatibility CLI or pre-migration state)

## Language

**Skill library**:
The independent, authoritative local collection of reusable skills. It is the source from which project skills are selected and installed; the initial library is `~/.agents/skills`, with a configurable location.
_Avoid_: skill repository, global skill folder

**Global baseline**:
The intentionally small set of agent resources kept available in an agent's global location, including the Agent Manager CLI usage skill.
_Avoid_: global skills, root skills

**Project activation**:
The resulting installation of selected managed skills for one repository, represented by their project-local soft links rather than a committed manifest.
_Avoid_: project configuration, project sync

**Managed skill**:
A Skill whose presence in a project or global baseline is controlled by Agent Manager from the Skill library.
_Avoid_: copied skill, local skill

**Project skill**:
A skill placed in a repository's agent-recognized skill location. It may be unmanaged and remain independent, or be converted into a managed skill.
_Avoid_: local skill

**Adoption**:
The explicit conversion of an existing project skill into a managed skill: its content is stored in the skill library and its former project location becomes a soft link.
_Avoid_: import, migration

**Fork**:
The explicit conversion of one managed project skill into an independent project skill, ending its shared-library relationship without changing other projects.
_Avoid_: detach, local override

**Target agent**:
An agent integration selected to receive a project's managed-skill soft links. A project may have multiple target agents.
_Avoid_: platform, provider

**Operator skill**:
A small global-baseline Skill that teaches an agent to use Agent Manager: search the Skill library, explain a recommendation, request confirmation, then invoke the CLI.
_Avoid_: CLI prompt, manager prompt

**Directory skill**:
A skill represented by a directory containing `SKILL.md` and any companion resources. It is the only resource shape Skill Manager manages in its first release.
_Avoid_: skill file, Markdown skill

**Compatibility declaration**:
Optional skill metadata that names the target agents for which a skill's instructions are intended. It informs installation warnings but does not prevent an explicit installation.
_Avoid_: installation support, adapter support

**Operation journal**:
The local, reversible record of a confirmed Agent Manager file operation, used to preview changes and support undo.
_Avoid_: audit log, sync history

**Actor identity**:
A typed, runtime-neutral reference to the human, agent, or service that initiated a governed AgentRun. It contains a stable ID and safe identity metadata, never credentials; identity must be explicit rather than inferred from ambient machine state.
_Avoid_: shared admin identity, OS-derived actor

**Delegation**:
The immutable, run-bound authority snapshot granted to an AgentRun, represented by exact scopes and an expiry time. A scope grants only its exact identifier and does not imply prefix or wildcard access.
_Avoid_: ambient permission, inherited wildcard scope

**Invocation context**:
The correlation metadata carried through a governed tool or MCP call: run, actor, delegation, policy snapshot, optional approval, and trace references. It carries identifiers, not credentials or unrestricted identity claims.
_Avoid_: runtime credential bundle, prompt context

**Operational context provider**:
An optional read-only boundary that returns operational records with source and freshness metadata. A separate exact-key access policy authorizes each read. The provider does not authorize mutations or establish actor identity.

**Operational context access policy**:
An exact allowlist of operational context keys that may be read. It is evaluated before the provider is called and remains separate from mutation-tool policy.
_Avoid_: policy provider, mutation adapter

**Initialization**:
The explicit global operation that verifies the CLI and installs or updates Operator skills. Project operations do not implicitly alter the global baseline.
_Avoid_: setup, bootstrap

**Environment activation**:
The machine-local presence of managed-skill soft links in a project. It is intentionally not a portable or Git-versioned project declaration.
_Avoid_: checked-in activation, project state

**Reconciliation**:
The repair of a project's managed-skill soft links so they point to the currently configured skill library.
_Avoid_: synchronization, migration

**Orphaned link**:
A managed-skill soft link whose library target no longer exists. It is reported by diagnostics and may be repaired by reconciliation.
_Avoid_: missing skill, broken installation

**Companion metadata**:
Optional `.skill-manager.yaml` data stored beside a directory skill, containing management-specific fields such as tags, target-agent compatibility, and provenance without changing `SKILL.md` semantics.
_Avoid_: skill frontmatter, registry entry

**Guarded operation**:
A management operation that previews its changes, requires confirmation, records a reversible journal entry, and never executes skill-provided code.
_Avoid_: automatic sync, unsafe update
