# Repository Guidelines

## Project Structure & Module Organization

This repository is currently specification-first. `openspec/changes/` contains active proposals, designs, tasks, and capability specs; `openspec/specs/` holds the main specifications. Architectural decisions live in `docs/adr/`, research in `docs/research/`, and proposed GitHub issues in `docs/issues/`. `CONTEXT.md` is the authoritative glossary for domain terms such as “project activation,” “managed skill,” and “operation journal.” Go application code and tests will be added as implementation work begins.

## Build, Test, and Development Commands

The application is implemented in Go. Run the standard checks from the repository root:

```bash
go test ./...                 # Run all package tests
go build ./cmd/agent-manager  # Build the Agent Manager CLI
```

Use OpenSpec to inspect and validate specification changes:

```bash
openspec list                 # List active changes
openspec show <change-name>   # Read a change proposal
openspec validate <change-name> # Validate a change or spec
openspec doctor               # Check OpenSpec relationship health
```

When implementation lands, document the canonical Go commands here and keep them reproducible from a clean checkout.

## Coding Style & Naming Conventions

Use the repository vocabulary from `CONTEXT.md` in code, specs, and user-facing text. Prefer small, cohesive Go packages with standard `gofmt` formatting and idiomatic Go names. Use lowercase, hyphen-separated names for OpenSpec change directories (for example, `create-skill-manager-cli`). Keep capability names stable once published.

## Testing Guidelines

Implementation changes should use Go’s standard `testing` package, with tests beside the package they exercise and fixtures under an explicitly named test-data directory. Cover catalog discovery, guarded filesystem operations, adapters, and failure paths. Add tests for every behavior stated in the relevant OpenSpec capability; do not claim coverage until the complete test suite passes.

## Commit & Pull Request Guidelines

The repository currently has one foundational commit, so no established commit convention is visible. Use concise, imperative commit subjects (for example, `Add catalog validation`). Pull requests should explain the user-visible change, link the relevant issue or OpenSpec change, identify validation commands and results, and call out any filesystem or compatibility impact. Include screenshots only for future UI changes.

## Design and Safety Requirements

Preserve the separation between the Go domain core and delivery clients. Managed-skill operations must remain guarded, reversible, and explicit; they must not execute code contained in skills. Do not add network access or dependency installation to managed-skill workflows without updating the relevant specs and ADRs.
