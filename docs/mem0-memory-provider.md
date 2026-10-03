# Explicit Mem0 OSS Memory provider

`mem0` targets the official OSS Python 2.2.1 server at commit
`94c3fe9f238f3dbf29c9ce98643bd71eb13077cd`. It uses `/memories`,
`/memories/{backend-id}` and `/search`, with no `/v1` prefix. Hosted Mem0 APIs
are outside this contract. [Pinned server source](https://github.com/mem0ai/mem0/blob/94c3fe9f238f3dbf29c9ce98643bd71eb13077cd/server/main.py)
is the fixture authority. OpenAPI `1.0.0` is an API schema version; the adapter
cannot automatically observe the installed Python package/source version.

Select a non-secret configuration reference in the existing CLI configuration:

```yaml
memory:
  version: v1
  id: shared-memory
  provider: mem0
  configuration:
    kind: env
    name: AGENT_MANAGER_MEM0_CONFIG
  scopes: [user, project]
  capabilities: [read, search]
```

The referenced environment variable contains this non-secret JSON (a direct
regular file reference is also supported):

```json
{
  "endpoint": "https://mem0.example.test",
  "contract": "mem0-oss-python-2.2.1-94c3fe9f238f3dbf29c9ce98643bd71eb13077cd",
  "allowNetwork": true,
  "timeoutMilliseconds": 10000,
  "secretReference": {"kind": "env", "name": "MEM0_API_KEY"}
}
```

Provision the referenced secret separately through the operator's environment.
The delivery factory supports environment secret references; embedding code may
supply a context-aware `mem0.SecretResolver` for file/keychain references.
Construction never resolves credentials or contacts the service. Explicit
status, get and search contact the selected endpoint. HTTPS is required except
for numeric loopback HTTP endpoints. URL credentials, query strings, fragments,
path prefixes and redirects are rejected. Requests and responses are limited
to 1 MiB, with a default 10-second request deadline (configured range 1–60000 ms).
Secret resolvers must honor their supplied bounded context. External
configuration is limited to 64 KiB and accepts only the documented fields.
Unknown fields, including raw credential fields, fail closed.

```bash
agent-manager --config ./config.yaml memory status --json
agent-manager --config ./config.yaml memory search --user explicit-user 'Go tests'
```

Project reads use the existing explicit project registration and `--project`
boundary. Owner labels partition data; they do not grant caller authority. The
Gateway applies its exact trusted owner allowlist before retrieval. Mem0
requests filter the exact owner first and validate returned ownership, neutral
ID, canonical enums, Unicode, provenance and lossless uint64 versions. Ambiguous
neutral-ID matches return conflict. Semantic scores originate from the server
and must be finite in `[0,1]`; no lexical score is presented as a semantic score.
Recall returns at most 100 semantic candidates and never claims complete
storage enumeration, including when the query is empty.

The programmatic canonical boundaries are `memory.Remember`, `memory.Get`,
`memory.Recall`, and the optional `RecallScored`. The explicit weaker mutation
boundaries are `memory.Replace(ctx, provider, memory.ReplaceRequest{...})` and
`memory.Remove(ctx, provider, owner, neutralID)`. These accept canonical IDs,
never backend IDs. Replacement increments an observed remote metadata version;
concurrent writers can overwrite one another or reuse a version. Removal is
unconditional. Neither operation supplies compare-and-swap, exact operation
receipts, a confirmation preview, or retained canonical lineage/history.

Basic provider capabilities advertise create, replace and remove independently.
Gateway/CLI status advertises read/search only for Mem0. CLI `add`, `update`,
`forget`, `supersede`, `history` and import remain unsupported for this adapter;
no confirmed Gateway operation is silently downgraded to a weaker mutation.
Embedding callers must establish their own explicit mutation authorization.
Mem0's content-oriented history does not meet canonical provenance/lineage
history requirements. No separate local canonical content mirror is kept.

Authentication failure, conflict, unavailable, canceled and outcome-unknown are
safe `errors.Is` categories. Once a mutation may have reached the server,
timeout, cancellation, invalid acknowledgment or uncertain post-write
verification returns outcome unknown without issuing a second write. Reconcile
explicitly through read-only provider observations and external operation
evidence; the adapter has no durable mutation receipt or blind retry mechanism.
Errors never include raw URLs, headers, backend response text, or credentials.

Ordinary tests require no installed Mem0 or service:

```bash
go test ./internal/memory/mem0 ./internal/memoryprovider ./internal/cli
```

The explicit smoke harness exercises the **Go adapter** against an already
running, operator-selected service. It does not install dependencies or start
services. Prepare a private JSON credential file with `ADMIN_API_KEY` and a
non-secret proof JSON containing independently verified `version: "2.2.1"`,
`commit: "94c3fe9f238f3dbf29c9ce98643bd71eb13077cd"`, `python`, and `environment`.
Verify that proof against the actual service process/package/source before
running; configured assertions alone are not version detection.

```bash
AM_MEM0_LIVE=1 \
AM_MEM0_LIVE_ENDPOINT=http://127.0.0.1:18768 \
AM_MEM0_LIVE_PROOF=/private/task/runtime-proof.json \
AM_MEM0_LIVE_SECRET_FILE=/private/task/service-env.json \
AM_MEM0_LIVE_EVIDENCE=/private/task/live-evidence.json \
go test ./internal/memory/mem0 -run TestExplicitLiveSmoke -count=1 -v
```

The smoke creates synthetic owned records, verifies the full canonical round
trip and a foreign-owner negative read, then removes its record. Saved evidence
labels itself `real-service-Go-adapter` and includes the exact tested pin,
environment, semantic score, canonical versions and result; automatic runtime
version observation remains explicitly unknown. Fixtures and live evidence are
separate. Managed-skill workflows retain offline guarded reversible operations,
never execute skill content, and never install or start Mem0.
