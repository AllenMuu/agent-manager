## 1. Delivery prerequisites

- [ ] 1.1 Re-read ticket #27 and its native blockers, verify prerequisite behavior is delivered, and confirm the new public test seams with the user before writing tests; record the selected boundaries and baseline.

## 2. Observable behavior via TDD

- [ ] 2.1 Implement “Immutable historical policy references” through confirmed public boundaries, one failing behavior test then minimal implementation at a time; verify every scenario (“Change policy after prior audit”) passes with its literal specified outcome and no private-helper assertions.

- [ ] 2.2 Implement “Desired versus confirmed applied revision” through confirmed public boundaries, one failing behavior test then minimal implementation at a time; verify every scenario (“Provider only accepts update”; “Exact application is confirmed”) passes with its literal specified outcome and no private-helper assertions.

- [ ] 2.3 Implement “Fail-closed application recovery” through confirmed public boundaries, one failing behavior test then minimal implementation at a time; verify every scenario (“Wrong acknowledgement or timeout”) passes with its literal specified outcome and no private-helper assertions.

- [ ] 2.4 Implement “Reconstructable permission change lineage” through confirmed public boundaries, one failing behavior test then minimal implementation at a time; verify every scenario (“Inspect approved permission change”) passes with its literal specified outcome and no private-helper assertions.

- [ ] 2.5 Implement “Idempotent and source-aware events” through confirmed public boundaries, one failing behavior test then minimal implementation at a time; verify every scenario (“Duplicate stale event”) passes with its literal specified outcome and no private-helper assertions.

## 3. Verification and publication

- [ ] 3.1 Verify every requirement/scenario against implementation and test/live evidence, run `go test ./...`, relevant package race checks, `go vet ./...`, CLI build, `openspec validate apply-confirmed-policy-revisions --strict`, `openspec doctor` and diff checks; record actual results and distinguish fixture/mock from real-service validation.
- [ ] 3.2 Obtain ordinary independent review of the complete change, resolve actionable findings and rerun affected behavior tests; retain the final reviewed scope and acceptance matrix.
- [ ] 3.3 Run the final gpt-6.1-sol/high read-only OCR review covering all proposed PR files; publish only if no actionable findings and no important coverage gap remain. An unavailable required reviewer stops publication.
