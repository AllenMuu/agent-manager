## Why

README commands assume an installed CLI although building from source does not put it on PATH. Users want the same prebuilt npm distribution used by AllenMuu/mysql-cli, with no Go toolchain required.

## What Changes

- Add a public scoped npm wrapper for direct npx execution and global npm installation.
- Download exact-version GitHub Release binaries with SHA-256 verification and actionable failures.
- Add cross-platform release builds, gated npm publishing and documented first-publish/retry procedures.
- Promote npm installation in both READMEs while retaining the Go source route.

## Capabilities

### New Capabilities
- `npm-cli-distribution`: prebuilt CLI acquisition, delegation and version-aligned publishing.

### Modified Capabilities
None. Existing CLI subcommands and managed-resource semantics remain unchanged.

## Impact

Adds Node-based distribution files, GitHub Actions and GoReleaser configuration. Initial installation requires network access to npm/GitHub Releases and a system `tar` executable (including Windows 10+ bsdtar). No networking or dependency installation is added to the Go managed-skill lifecycle.
