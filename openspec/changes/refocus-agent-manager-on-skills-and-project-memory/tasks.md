## 1. Canonical Skill repository

- [x] 1.1 Change the default library resolver to `~/.agent-manager/skills`, preserve an explicit local path override, and remove implicit fallback/search of `~/.agents/skills`; verify default, override, and legacy-path isolation with config and CLI tests.
- [x] 1.2 Add versioned Skill storage and a schema-versioned `metadata.json` identity record, including unknown legacy version/provenance values and conversion of existing `.skill-manager.yaml` metadata; verify version retention, metadata round trips, and path-containment behavior with repository tests.
- [x] 1.3 Add repository resolution by stable Skill identifier and selected version, returning only the resolved version directory to callers; verify that catalog consumers cannot resolve or expose unselected repository versions.

## 2. Safe legacy import

- [x] 2.1 Add a read-only legacy-library preview that reports eligible entries, destination identifiers, metadata gaps, and conflicts without changing either library or project links; verify every preview scenario with filesystem tests.
- [x] 2.2 Add explicit batch selection and confirmation for importing selected legacy Skills by copy; verify that unconfirmed and unselected entries remain unchanged and that confirmed imports preserve legacy sources and activation links.
- [x] 2.3 Make identifier conflicts non-overwriting and confirmed batch writes recoverable; verify conflict refusal, partial-failure rollback, and unchanged sources with filesystem tests.

## 3. Integrate existing Skill workflows

- [x] 3.1 Adapt catalog search/list and machine-readable output to represent one stable Skill identity with addressable versions and provenance; verify configured-path behavior and metadata visibility with catalog and CLI tests.
- [x] 3.2 Adapt guarded Global and Project installation plus Claude/Codex/Pi adapters to materialize only explicitly selected repository versions; verify selected targets, link-derived activation state, removal safety, and retained repository versions with lifecycle tests.
- [x] 3.3 Keep project recommendations read-only and resolve candidates from the configured repository through the catalog boundary; verify deterministic recommendation output and that recommendation does not mutate activations or the journal.

## 4. Reconcile contracts and documentation

- [x] 4.1 Reconcile active Skill catalog and installation OpenSpec artifacts with the new repository contract, update ADR-0002 to supersede the `~/.agents/skills` default, and preserve ADR-0001/0004's no-manifest machine-local activation model; verify no active normative spec claims the old default.
- [x] 4.2 Update English and Chinese quick-start/configuration docs and the product roadmap to document `~/.agent-manager/skills`, confirmed import, Global/Project scopes, Skill-first sequencing, Project Memory as the following phase, and runtime/control-plane boundaries; verify both README versions contain matching commands and defaults.

## 5. Integrated verification

- [x] 5.1 Add regression coverage for default-path isolation, explicit migration confirmation, version/provenance preservation, and cross-agent materialization; verify the relevant package and CLI test suites pass.
- [x] 5.2 Run `go test ./...` and `go build ./cmd/agent-manager`; record the exact outcomes and any environment limitations before considering the implementation slice complete.
