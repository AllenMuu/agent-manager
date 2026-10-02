## 1. Delivery prerequisites

- [ ] 1.1 Re-read ticket #28 and its native blockers, verify prerequisite behavior is delivered, and confirm the new public test seams with the user before writing tests; record the selected boundaries and baseline.
- [ ] 1.2 Document OpenShell reference with adopt/integrate/do-not-build decisions and pinned contract before enabling the new behavior; verify the document preserves existing managed-skill non-execution/no-installation rules and links the ticket/spec.

## 2. Observable behavior via TDD

- [ ] 2.1 Implement “Opt-in neutral OpenShell adapter” through confirmed public boundaries, one failing behavior test then minimal implementation at a time; verify every scenario (“Adapter is not selected”) passes with its literal specified outcome and no private-helper assertions.

- [ ] 2.2 Implement “Tested enforcement and credential diagnostics” through confirmed public boundaries, one failing behavior test then minimal implementation at a time; verify every scenario (“Inspect weaker credential isolation”) passes with its literal specified outcome and no private-helper assertions.

- [ ] 2.3 Implement “Static policy change handling” through confirmed public boundaries, one failing behavior test then minimal implementation at a time; verify every scenario (“Filesystem change after start”) passes with its literal specified outcome and no private-helper assertions.

- [ ] 2.4 Implement “Real sandbox approval smoke evidence” through confirmed public boundaries, one failing behavior test then minimal implementation at a time; verify every scenario (“Real denied network retry”) passes with its literal specified outcome and no private-helper assertions.

- [ ] 2.5 Implement “No implicit external setup” through confirmed public boundaries, one failing behavior test then minimal implementation at a time; verify every scenario (“Offline workflows”) passes with its literal specified outcome and no private-helper assertions.

## 3. Verification and publication

- [ ] 3.1 Verify every requirement/scenario against implementation and test/live evidence, run `go test ./...`, relevant package race checks, `go vet ./...`, CLI build, `openspec validate integrate-experimental-openshell --strict`, `openspec doctor` and diff checks; record actual results and distinguish fixture/mock from real-service validation.
- [ ] 3.2 Obtain ordinary independent review of the complete change, resolve actionable findings and rerun affected behavior tests; retain the final reviewed scope and acceptance matrix.
- [ ] 3.3 Run the final gpt-6.1-sol/high read-only OCR review covering all proposed PR files; publish only if no actionable findings and no important coverage gap remain. An unavailable required reviewer stops publication.
