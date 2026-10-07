## Context

The approved reference is AllenMuu/mysql-cli's npm wrapper over GitHub Release binaries. Agent Manager already owns `install` as a Skill lifecycle command, so the wrapper must never intercept it.

## Goals / Non-Goals

Goals: npx execution without Go, optional global npm installation, six macOS/Linux/Windows amd64/arm64 builds, pinned versions and verified downloads, unchanged CLI argument/stream/cwd contracts.
Non-goals: remote Skill installation, agent execution, automatic shell-profile editing, a separate persistent `install` wrapper subcommand, or changes to domain APIs.

## Decisions

- Use `@allenmuu/agent-manager` with one `agent-manager` bin entry and Node >=22. Keep Node code dependency-free.
- Acquire an exact stable `vMAJOR.MINOR.PATCH` archive and checksums.txt from GitHub Releases. Reject unpublished 0.0.0, unsupported platforms and prereleases. HTTPS downloads have redirect, size and timeout limits. SHA-256 detects corruption against the same release's manifest, not independent publisher authentication.
- Extract only the expected executable into a temporary package-local directory. Require a direct regular file, atomically replace the binary, write a version marker and clean staging files. Failed acquisition exits nonzero and preserves any previous binary/version.
- Download in postinstall; if lifecycle scripts are disabled, acquire on first invocation when the version marker is missing. Normal invocations of an installed binary use no network.
- Delegate every argument (including `install`) without a shell, retaining cwd, environment and standard streams. Forward termination signals to the child and report shell-compatible signal exit status.
- GoReleaser outputs stable archive names and SHA-256 manifests to `.goreleaser-dist`, separate from committed npm package sources.
- On stable tags, checks run before GitHub Release creation; npm publishing depends on verified assets for all six platforms and `NPM_PUBLISH=true`. Configure either first-publish NPM_TOKEN or npm trusted publishing for subsequent releases. Manual dispatch can retry npm publication against an existing release.

## Risks / Trade-offs

Users need network access for initial binary acquisition and a system tar command. npm/GitHub outages fail explicitly; Go source installation remains available. The wrapper relies on trust in the publisher and HTTPS release endpoints. Local Node fixture tests verify acquisition and subprocess behavior; actual npm publication additionally requires account credentials and live release assets.

## Validation

Regression-first Node tests cover matching assets, corrupt/missing checksums, extraction/download failure, delegation, termination and cleanup. Build all six platform archives, install a packed wrapper into an isolated npm prefix using locally built verified assets, execute with npx and global-bin paths, run the full Go suite and vet, and validate the OpenSpec change.
