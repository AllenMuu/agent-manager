# Owned local Memory

M3 adds an explicitly selected `structured-local` provider. The legacy `file`
provider and confirmed `memory promote` remain available. Neither discovery nor
an unconfirmed plan creates a store directory or canonical state. Memory content,
including SKILL and TASK knowledge, is inert.

Select an existing direct directory using an absolute external reference:

```yaml
memory:
  version: v1
  id: local-memory
  provider: structured-local
  configuration:
    kind: file
    name: /Users/me/.local/share/agent-manager/structured-memory
  scopes: [user, project]
  capabilities: [read, write, search]
  retrieval:
    maxResults: 10
    contentBytes: 32768
    contextBytes: 131072
    minRelevance: 0
```

`capabilities` records requested operations. `memory status --json` separately
reports implemented capabilities, unsupported requests, current availability and
unavailable implemented requests. The local structured provider advertises its
conditional updates and atomic supersede semantics. Legacy text implements
confirmed append, so a configured request for text search is reported as
unsupported. Claude Code, Codex and Pi native Memory integrations remain
unsupported until verified runtime mechanisms are delivered.

The CLI operator explicitly selects exactly one `--user <stable-id>` or
`--project <registered-directory>` context. An optional `--agent-id` or
`--session-id` selects a partition within that owner. These labels select local
partitions; they do not authenticate a remote caller. Embedded clients must bind
trusted exact read/write allowlists when constructing a Gateway. Read authority
never grants write authority.

Register a project deliberately, then add and read its knowledge:

```text
agent-manager --config config.yaml memory project register --project /path/to/repo --yes
agent-manager --config config.yaml memory add --project /path/to/repo --type FACT --content 'Use Go tests' --source issue:21 --evidence review:21 --yes
agent-manager --config config.yaml memory search 'Go tests' --project /path/to/repo --type FACT
agent-manager --config config.yaml memory inspect <record-id> --project /path/to/repo
```

Registration stores an opaque project ID and a canonical directory mapping in
`<config-path>.projects.json`. `--registry <existing-parent>/projects.json`
selects another registry. Same basenames create distinct identities; a moved
project keeps its ID only through explicit `memory project relocate <project-id>
--project <new-directory> --yes`. The mapping preview binds the displayed path
and ID to the observed directory identity and prior mapping. The registry is
separate from canonical state and its persistent lock. A stored path whose final
component or ancestor becomes a symlink grants no owner lookup at the moved
target; explicit confirmed relocation repairs the stale mapping. Real case
aliases for the same direct directory share one identity.

Every add, update, supersede, forget or import prints its exact owner and intent
before accepting interactive `y` or `--yes`. Without confirmation, no provider
mutation occurs. Update, supersede and forget require `--expected-version`; stale
versions return a conflict. `inspect --history` reads retired lineage explicitly.
Add/update/supersede accept full record metadata. Import accepts only its declared
type/source/layer, owner, operation ID and confirmation; `--content` and
`--evidence` are rejected before a plan is printed. Forget accepts owner,
expected version, operation ID and confirmation, and rejects record metadata
flags. Import and forget plans contain no ineffective record body. Embedded
Gateway previews likewise reject fields unused by the selected operation and
fingerprint the effective immutable plan. This narrows the new M3 commands before
their first accepted release; the existing M2 import evidence contract is unchanged.

```text
agent-manager --config config.yaml memory update <record-id> --project /path/to/repo --expected-version 1 --type FACT --content 'Use scoped tests' --source issue:21 --yes
agent-manager --config config.yaml memory supersede <record-id> --project /path/to/repo --expected-version 2 --type FACT --content 'Use full Go tests' --source issue:21 --yes
agent-manager --config config.yaml memory forget <replacement-id> --project /path/to/repo --expected-version 1 --yes
agent-manager --config config.yaml memory import /path/to/legacy.txt --project /path/to/repo --type EXPERIENCE --source operator-declared --yes
```

Import separately binds confirmation to its exact owner and source, plus a
SHA-256 digest of previewed inert bytes. Changed source bytes cause a conflict
before a batch is written; the source is left unchanged. No text conversion or
import runs automatically. `--operation-id` retains local provider receipts for
explicit retries of the same intent. Conflicts and uncertain outcomes are
reported; the CLI never retries a write automatically.

Search authorizes one owner before any provider access, verifies returned
ownership and canonical metadata, selects ACTIVE records and an optional
knowledge type, then applies relevance and budgets. Without a scored provider,
local relevance is the fraction of unique lowercase query tokens present in the
content. It is lexical token coverage, not semantic reranking. An optional
scored-recall provider must explicitly implement finite normalized scores in
`[0,1]`. Equal scores use neutral record IDs in ascending order. Zero-match
nonempty queries are omitted; an empty query lists current owned records.

`contentBytes` bounds the aggregate content bytes. `contextBytes` measures the
complete JSON array of selected canonical records, including IDs, owner, source,
evidence, version, state, layer, escaping, brackets and separators. Oversized
records are omitted whole, preserving attribution. Defaults are 10 results,
32 KiB content and 128 KiB attributed context. Supported limits are 1–100
results, 1 byte–1 MiB content, 2 bytes–1 MiB context and relevance 0–1.

`roles context` resolves configured project knowledge through the same Gateway
and retrieval policy as `memory search`. `--memory-registry` selects a custom
mapping registry. Canonical records appear once in `memoryRecords`, preserving
IDs/source/evidence; legacy text `memory` remains source-compatible without
invented provenance. Handoffs only have read authority and never promote or
write Memory. A provider outage retains other artifacts and Skills with an
output-safe `memoryDiagnostic` category and an unavailable Memory state. Missing,
regular-file and symlink provider roots do not prevent reading a valid owner
registry for that handoff. Inspectable canonical/lock inode aliases and malformed
registry state still fail closed. Registry writes repeat strict root and alias
checks before mutation, including first registration; existing invalid roots
are rejected. A missing optional provider root has no state inode to alias and
does not implicitly create provider storage.

Memory mutations use provider lifecycle semantics. They are outside the Skill
filesystem operation journal and carry no filesystem-undo promise. Memory
outages do not block separately invoked Skill/SubAgent workflows. References
and raw backend errors are omitted from Memory diagnostics.
