# npm CLI Distribution Implementation Plan

> **For agentic workers:** Use executing-plans to implement this plan task-by-task in the current isolated worktree. The user approved the recommended design on 2026-10-07.

**Goal:** Let users run `npx @allenmuu/agent-manager` or install with `npm install -g @allenmuu/agent-manager` without Go or a checkout.

**Architecture:** A small dependency-free Node wrapper downloads the matching version's GitHub Release archive and checksum manifest, verifies SHA-256, and delegates all CLI arguments to the Go executable. GoReleaser builds platform assets before npm publishing; installation networking remains outside managed-resource workflows.

**Tech Stack:** Go, Node built-ins and node:test, npm, GoReleaser, GitHub Actions.

## Tasks

- [x] Define proposal, design, capability scenarios, and ADR for distribution-only networking.
- [x] Write `dist/npm/test/install.test.js`: platform mapping, exact-version URLs, checksum mismatch, extraction failure, successful verified install and cleanup. Run `node --test dist/npm/test/install.test.js` and observe failures before implementing `dist/npm/install.js`.
- [x] Write `dist/npm/test/shim.test.js`: real subprocess argument/cwd/stdin/stdout/stderr/exit-code forwarding, `install` passthrough, missing binary, termination handling. Run `node --test dist/npm/test/shim.test.js` and observe failures before implementing `dist/npm/bin/agent-manager.js`.
- [x] Create `dist/npm/package.json` with a single bin, explicit files, postinstall, tests and public publish config. Keep source version `0.0.0` until release sets it from a stable tag.
- [x] Create `.goreleaser.yml` for macOS/Linux/Windows x64 and arm64, stable names and SHA-256 checksums; put output in `.goreleaser-dist` to preserve npm sources.
- [x] Create `.github/workflows/release.yml`: run Go/npm checks, build assets on stable version tags, then publish the matching npm version with configured credentials. Reject prerelease tags; document retry of npm publishing independently from the release.
- [x] Update both READMEs with npm as the primary installation route and Go as a source-build alternative. Label npm commands as pending the first release. Add `docs/npm-release.md` with authentication, tag/version and first-publication steps.
- [x] Run Node tests, `go test ./...`, `go vet ./...`, `goreleaser check`, `goreleaser release --snapshot --clean`, `openspec validate distribute-cli-via-npm --strict`, and `git diff --check`.
- [x] Pack the wrapper, install it in an isolated npm prefix using locally built release assets through the test harness, then execute via npm/npx. Report local validation separately from actual registry publication.

## Validation results

- Node regressions: 13 passed.
- Go suite and vet: passed.
- GoReleaser config and snapshot: passed; all six archives generated.
- Packed-package smoke: global npm installation and scoped npx execution passed using verified snapshot assets in isolated prefixes. Only four intended package files are included.
- OpenSpec strict validation and actionlint: passed.
- Actual registry publication remains pending: package lookup returned E404 and local npm authentication returned ENEEDAUTH. No release tag or npm version was published during local validation.
