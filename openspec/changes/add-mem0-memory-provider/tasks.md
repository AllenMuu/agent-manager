## 1. Delivery prerequisites

- [x] 1.1 Re-read ticket #22 and its native blockers, verify prerequisite behavior is delivered, and confirm the new public test seams with the user before writing tests; record the selected boundaries and baseline.
- [x] 1.2 Document ADR 0006 explicit Mem0 network opt-in and secret-reference boundary before enabling the new behavior; verify the document preserves existing managed-skill non-execution/no-installation rules and links the ticket/spec.

## 2. Observable behavior via TDD

- [x] 2.1 Implement “Canonical remote operation mapping” through confirmed public boundaries, one failing behavior test then minimal implementation at a time; verify every scenario (“Remote canonical round trip”) passes with its literal specified outcome and no private-helper assertions.

- [x] 2.2 Implement “Honest remote semantic capabilities” through confirmed public boundaries, one failing behavior test then minimal implementation at a time; verify every scenario (“Backend cannot atomically supersede”) passes with its literal specified outcome and no private-helper assertions.

- [x] 2.3 Implement “Bounded transport and uncertain writes” through confirmed public boundaries, one failing behavior test then minimal implementation at a time; verify every scenario (“Write response times out”; “Authentication failure”) passes with its literal specified outcome and no private-helper assertions.

- [x] 2.4 Implement “Opaque secret references” through confirmed public boundaries, one failing behavior test then minimal implementation at a time; verify every scenario (“Authenticated request diagnostics”) passes with its literal specified outcome and no private-helper assertions.

- [x] 2.5 Implement “Offline contract and opt-in live evidence” through confirmed public boundaries, one failing behavior test then minimal implementation at a time; verify every scenario (“Ordinary offline test run”; “Explicit live smoke”) passes with its literal specified outcome and no private-helper assertions.

## 3. Verification and publication

- [x] 3.1 Verify every requirement/scenario against implementation and test/live evidence, run `go test ./...`, relevant package race checks, `go vet ./...`, CLI build, `openspec validate add-mem0-memory-provider --strict`, `openspec doctor` and diff checks; record actual results and distinguish fixture/mock from real-service validation.
- [x] 3.2 Obtain ordinary independent review of the complete change, resolve actionable findings and rerun affected behavior tests; retain the final reviewed scope and acceptance matrix.
- [x] 3.3 Run the final gpt-6.1-sol/high read-only OCR review covering all proposed PR files; publish only if no actionable findings and no important coverage gap remain. An unavailable required reviewer stops publication.
