## 1. Doctor domain

- [x] 1.1 Add regression coverage that Doctor findings exclude adapter capability inventory while preserving actual project findings; verify `go test ./internal/diagnostic` fails before the implementation change.
- [x] 1.2 Add and implement path-scoped reconciliation so the Web console can preview and repair one eligible orphaned managed link without widening the existing CLI operation; verify focused diagnostic tests cover refusal, exact path scope, and rollback.

## 2. Guarded Web repair API

- [x] 2.1 Add HTTP regression coverage for finding codes, repair eligibility, and manual guidance; keep project-scan notes on the existing recommendations response.
- [x] 2.2 Add project-bound preview and execute routes for scoped reconciliation and exact `.gitignore` updates; verify tests cover no mutation before confirmation, journaled success, forged paths, stale plans, and unsupported findings.

## 3. Web diagnostics experience

- [x] 3.1 Add frontend regression coverage for separate Doctor and project-scan sections, localized manual guidance, and eligible repair actions; verify the focused Vitest suite fails before the UI changes.
- [x] 3.2 Implement repair buttons using the shared plan review and confirmation flow, then refresh Doctor findings, project inventory, and the operation journal; verify TypeScript and frontend tests pass.

## 4. Delivery and validation

- [x] 4.1 Update bilingual documentation for supported Web repairs and manual-only findings; verify the commands and safety limits match the implementation.
- [x] 4.2 Build embedded Web assets and run focused/full Go and frontend checks plus strict OpenSpec validation and diff checks; record actual results.
