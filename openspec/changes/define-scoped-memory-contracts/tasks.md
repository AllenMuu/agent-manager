## 1. Delivery prerequisites

- [ ] 1.1 Re-read ticket #19 and its native blockers, verify prerequisite behavior is delivered, and confirm the new public test seams with the user before writing tests; record the selected boundaries and baseline.
- [ ] 1.2 Document ADR 0006 canonical/provider separation before enabling the new behavior; verify the document preserves existing managed-skill non-execution/no-installation rules and links the ticket/spec.

## 2. Observable behavior via TDD

- [ ] 2.1 Implement “Explicit owner partitions” through confirmed public boundaries, one failing behavior test then minimal implementation at a time; verify every scenario (“Missing ownership identifiers”; “Two-project isolation”) passes with its literal specified outcome and no private-helper assertions.

- [ ] 2.2 Implement “Canonical typed record round trip” through confirmed public boundaries, one failing behavior test then minimal implementation at a time; verify every scenario (“Remember and recall project knowledge”) passes with its literal specified outcome and no private-helper assertions.

- [ ] 2.3 Implement “Honest optional capabilities and errors” through confirmed public boundaries, one failing behavior test then minimal implementation at a time; verify every scenario (“Unsupported recall versus unavailable storage”; “Canceled operation”) passes with its literal specified outcome and no private-helper assertions.

- [ ] 2.4 Implement “Deterministic neutral contract implementation” through confirmed public boundaries, one failing behavior test then minimal implementation at a time; verify every scenario (“Portable contract suite”) passes with its literal specified outcome and no private-helper assertions.

- [ ] 2.5 Implement “Text and Skill compatibility” through confirmed public boundaries, one failing behavior test then minimal implementation at a time; verify every scenario (“Legacy promotion remains available”) passes with its literal specified outcome and no private-helper assertions.

## 3. Verification and publication

- [ ] 3.1 Verify every requirement/scenario against implementation and test/live evidence, run `go test ./...`, relevant package race checks, `go vet ./...`, CLI build, `openspec validate define-scoped-memory-contracts --strict`, `openspec doctor` and diff checks; record actual results and distinguish fixture/mock from real-service validation.
- [ ] 3.2 Obtain ordinary independent review of the complete change, resolve actionable findings and rerun affected behavior tests; retain the final reviewed scope and acceptance matrix.
- [ ] 3.3 Run the final gpt-6.1-sol/high read-only OCR review covering all proposed PR files; publish only if no actionable findings and no important coverage gap remain. An unavailable required reviewer stops publication.
