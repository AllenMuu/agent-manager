## 1. Delivery prerequisites

- [x] 1.1 Re-read ticket #21 and its native blockers, verify prerequisite behavior is delivered, and confirm the new public test seams with the user before writing tests; record the selected boundaries and baseline.

## 2. Observable behavior via TDD

- [x] 2.1 Implement “Truthful provider discovery” through confirmed public boundaries, one failing behavior test then minimal implementation at a time; verify every scenario (“Configured search is not implemented”) passes with its literal specified outcome and no private-helper assertions.

- [x] 2.2 Implement “Confirmed owned mutations” through confirmed public boundaries, one failing behavior test then minimal implementation at a time; verify every scenario (“Mutation is not confirmed”; “Stable project identity”) passes with its literal specified outcome and no private-helper assertions.

- [x] 2.3 Implement “Bounded deterministic retrieval” through confirmed public boundaries, one failing behavior test then minimal implementation at a time; verify every scenario (“Bounded attributed search”; “Wrong-owner query”) passes with its literal specified outcome and no private-helper assertions.

- [x] 2.4 Implement “Shared read-only task-context path” through confirmed public boundaries, one failing behavior test then minimal implementation at a time; verify every scenario (“Context handoff reads knowledge”) passes with its literal specified outcome and no private-helper assertions.

- [x] 2.5 Implement “Independent failure and recovery semantics” through confirmed public boundaries, one failing behavior test then minimal implementation at a time; verify every scenario (“Provider outage”) passes with its literal specified outcome and no private-helper assertions.

## 3. Verification and publication

- [x] 3.1 Verify every requirement/scenario against implementation and test/live evidence, run `go test ./...`, relevant package race checks, `go vet ./...`, CLI build, `openspec validate add-memory-gateway-and-cli --strict`, `openspec doctor` and diff checks; record actual results and distinguish fixture/mock from real-service validation.
- [ ] 3.2 Obtain ordinary independent review of the complete change, resolve actionable findings and rerun affected behavior tests; retain the final reviewed scope and acceptance matrix.
- [ ] 3.3 Run the final gpt-6.1-sol/high read-only OCR review covering all proposed PR files; publish only if no actionable findings and no important coverage gap remain. An unavailable required reviewer stops publication.
