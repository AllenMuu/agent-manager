## ADDED Requirements

### Requirement: Prebuilt CLI acquisition
The npm package SHALL provide a single agent-manager executable entry and acquire the exact stable package version's matching prebuilt binary over HTTPS, with SHA-256 verification before extraction and installation. It SHALL support macOS, Linux and Windows on amd64 and arm64 and fail explicitly on unsupported platforms, failed downloads, missing checksums or invalid archives.

#### Scenario: Verified initial acquisition
- **WHEN** a supported platform installs a released npm package
- **THEN** the matching archive is verified and its executable is installed with a matching version marker

#### Scenario: Corrupt or missing release data
- **WHEN** acquisition fails or the archive does not match exactly one checksum entry
- **THEN** installation fails nonzero, leaves no new runnable binary or success marker and cleans temporary files

#### Scenario: Lifecycle scripts disabled
- **WHEN** the package's CLI is invoked without a matching installed binary and version marker
- **THEN** the wrapper attempts the same verified acquisition before executing any command

#### Scenario: Installed CLI execution
- **WHEN** the matching binary and marker already exist
- **THEN** normal CLI execution performs no distribution download

### Requirement: CLI delegation
The npm wrapper SHALL forward every argument, cwd, environment and standard stream to the Go CLI without shell interpretation, and preserve normal exit codes and shell-compatible signal exits.

#### Scenario: Existing Skill install command
- **WHEN** a user invokes npx with `install go-helper --target codex --yes`
- **THEN** those arguments reach the Go Skill installation command without interception by the wrapper

#### Scenario: Termination
- **WHEN** the wrapper receives a supported termination signal while the child runs
- **THEN** it forwards that signal and reports a shell-compatible termination exit status after the child closes

### Requirement: Version-aligned release publishing
Release automation SHALL validate stable nonzero version tags, build all supported platform archives and a SHA-256 manifest, and verify available release assets before publishing the matching npm version. npm publishing SHALL require explicit repository enablement and configured publisher credentials.

#### Scenario: Stable release
- **WHEN** a stable version tag is released and npm publishing is enabled and authorized
- **THEN** the npm package version matches the GitHub Release tag and every supported archive has a verified checksum

#### Scenario: Retry npm publication
- **WHEN** a maintainer dispatches publication for an existing stable release
- **THEN** checks and asset verification precede npm publishing without recreating the GitHub Release
