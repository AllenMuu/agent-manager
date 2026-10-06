## 1. Delivery prerequisites

- [x] 1.1 Re-read ticket #26 and its native blockers, verify prerequisite behavior is delivered, and confirm the new public test seams with the user before writing tests; record the selected boundaries and baseline.

## 2. Observable behavior via TDD

- [x] 2.1 Implement “Denial-bound expiring proposals” through confirmed public boundaries, one failing behavior test then minimal implementation at a time; verify every scenario (“Denied network action requests permission”) passes with its literal specified outcome and no private-helper assertions.

- [x] 2.2 Implement “Trusted human decision authority” through confirmed public boundaries, one failing behavior test then minimal implementation at a time; verify every scenario (“Agent spoofs human metadata”; “Authorized operator decides”) passes with its literal specified outcome and no private-helper assertions.

- [x] 2.3 Implement “Expiry and base revision checks” through confirmed public boundaries, one failing behavior test then minimal implementation at a time; verify every scenario (“Stale or expired request”) passes with its literal specified outcome and no private-helper assertions.

- [x] 2.4 Implement “Delegation ceiling” through confirmed public boundaries, one failing behavior test then minimal implementation at a time; verify every scenario (“Permission exceeds delegation”) passes with its literal specified outcome and no private-helper assertions.

- [x] 2.5 Implement “Distinct non-mutating proposal decisions” through confirmed public boundaries, one failing behavior test then minimal implementation at a time; verify every scenario (“Proposal is rejected or merely approved”) passes with its literal specified outcome and no private-helper assertions.

## 3. Verification and publication

- [x] 3.1 Verify every requirement/scenario against implementation and test/live evidence, run `go test ./...`, relevant package race checks, `go vet ./...`, CLI build, `openspec validate add-runtime-permission-proposals --strict`, `openspec doctor` and diff checks; record actual results and distinguish fixture/mock from real-service validation.
- [ ] 3.2 Obtain ordinary independent review of the complete change, resolve actionable findings and rerun affected behavior tests; retain the final reviewed scope and acceptance matrix.
- [ ] 3.3 Run the final gpt-6.1-sol/high read-only OCR review covering all proposed PR files; publish only if no actionable findings and no important coverage gap remain. An unavailable required reviewer stops publication.
