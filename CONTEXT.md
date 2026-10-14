# Agent Manager

**Agent Manager**:
The project-aware local manager for reusable Skills and shared Project Memory across AI coding agents. It manages portable project resources and their agent-specific placement; it does not own agent reasoning or execution. The product name does not replace Skill-domain vocabulary.
_Avoid_: Skill Manager (except when referring to the compatibility CLI or pre-migration state), agent runtime

## Language

**Console language**:
The selected language for the Web console's built-in interface and Agent Manager-generated explanations. It does not translate project or Skill library source content.
_Avoid_: project language, content language

**Local source content**:
Names, descriptions, instructions, identifiers, and paths read from a registered project or the configured Skill library and presented as stored.
_Avoid_: translated console copy

**Skill library**:
The independent, authoritative local collection of reusable Skills. It is the source from which Global and Project activations are selected; the default is `~/.agent-manager/skills`, with a configurable location. It is separate from agent-recognized Skill directories.
_Avoid_: global skill folder

**Skill version**:
A distinct, addressable state of one managed Skill whose content and metadata remain available when a later version is added.
_Avoid_: overwrite, latest copy

**Skill provenance**:
Evidence about where a Skill version came from and what trust or verification status is known. Missing evidence is represented as unknown rather than inferred from Skill content.
_Avoid_: publisher claim without evidence

**Legacy Skill library**:
The pre-migration collection at `~/.agents/skills`. It is an explicit import source for selected Skills, not an automatic fallback or a second catalog source.
_Avoid_: default library

**Skill library import**:
The previewed and explicitly confirmed copy of selected Skills from a legacy or alternate library into the canonical Skill library. Import leaves source Skills and existing project activations unchanged.
_Avoid_: automatic migration, adoption

**Global baseline**:
The minimal Operator Skill installed by `init` for safe Agent Manager use. Other reusable Skills can be explicitly activated globally from a selected repository version.
_Avoid_: global library, auto-loaded library

**Global activation**:
The current user's selected managed Skill version represented by an absolute soft link in one target agent's global Skill directory.
_Avoid_: global baseline, global sync

**Project activation**:
The resulting installation of selected managed skills for one repository, represented by their project-local soft links rather than a committed manifest.
_Avoid_: project configuration, project sync

**Managed skill**:
A Skill whose Project or Global activation is controlled by Agent Manager from the Skill library.
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
An agent integration selected to receive Global or Project managed-Skill soft links. Each scope may select multiple target agents.
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

**Canonical Memory record**:
Provider-neutral knowledge with an opaque ID, version, explicit owner partition,
knowledge type, inert content, source/evidence references, state and layer metadata.
RAW/ATOMIC/COMPOSITE/PROFILE are layers, not automatic consolidation behavior.
_Avoid_: provider object, executable Skill or Task

**Memory owner partition**:
An exact USER or PROJECT owner, or an AGENT/SESSION partition within exactly one
owning user or project. The identifiers partition data; they do not authenticate
or authorize a caller. GLOBAL is not currently supported.
_Avoid_: ambient owner, authority inferred from labels

**Permission proposal**:
An expiring request for one exact runtime capability difference, linked to a
persisted permission denial, initiating actor/delegation, run and immutable base
policy snapshot. A proposal decision does not apply a policy revision or resume
execution, and remains separate from one-action approval.

**Network delegation scope**:
R3's additive mapping from an exact canonical hostname to `network:<hostname>`.
The entire identifier must fit the existing canonical scope syntax and 128-byte
limit. Exact equality grants one destination; wildcards, prefixes, URLs and IP
addresses grant no authority through this mapping.

**Trusted operator decision boundary**:
A host-configured port that establishes an authorized human independently of
runtime-supplied identity/kind/roles labels and returns proposal-bound authority
evidence. Runtime requests cannot select or install this port. The domain
contract does not implement live human authentication.
