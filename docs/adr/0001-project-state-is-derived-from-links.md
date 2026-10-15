# Project state is derived from links

Project activations will not be represented by a committed manifest. A project-local absolute soft link into the configured Skill repository is managed when its target is valid. A copied activation is managed only when the project operation journal records its Skill identity, exact source version, materialization method, and matching destination fingerprint. A copy without matching journal evidence is unmanaged. An external link becomes managed only through a separately previewed and confirmed migration to an exact repository version; ordinary resynchronization never adopts it. Each destination entry retains its own version binding, so entries with the same Skill ID may intentionally point to different versions. This keeps the repository clean, lets unmanaged project Skills coexist, and gives managed activations an explicit owner that can be removed or undone safely.

The project journal is local operational state under `.skill-manager/`; it is not committed project configuration. Undo restores the exact recorded pre-operation link, copy, or absent state only while the current destination still matches the operation's recorded after-state.

## Considered Options

- Commit a project activation manifest
- Infer link ownership from canonical repository targets and copy ownership from scoped journal evidence
