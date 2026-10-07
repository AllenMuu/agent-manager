# ADR 0009: Distribute the CLI through an npm wrapper

Status: Accepted (user approved 2026-10-07).

## Context

CLI examples fail with `command not found` if users only build a local binary. The user requested the npm distribution pattern of AllenMuu/mysql-cli.

## Decision

A dependency-free Node package downloads an exact-version, SHA-256-verified Go executable from GitHub Releases and exposes it through npx or global npm installation. npm package acquisition and binary downloading are explicit distribution operations, separate from the Go domain core and delivery clients. The wrapper delegates all CLI arguments; `install` remains the existing Skill installation command.

Normal invocations use the installed executable without network access. First invocation may acquire it if npm lifecycle scripts were disabled. Download failures are fatal with actionable guidance. The wrapper does not install, fetch or execute code contained in managed Skills, edit shell profiles or start agents. Existing managed-resource network/dependency restrictions remain in force.

## Consequences

End users require Node >=22, npm, system tar and initial access to npm/GitHub Releases, but do not need Go. Publishers maintain six platform archives and matching npm versions. The checksum manifest verifies corruption against the release; it is not an independent signature. Source installation remains available when distribution endpoints cannot be reached.
