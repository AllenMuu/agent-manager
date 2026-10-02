## 1. Delivery prerequisites

- [ ] 1.1 Re-read ticket #24 and its native blockers, verify prerequisite behavior is delivered, and confirm the new public test seams with the user before writing tests; record the selected boundaries and baseline.
- [ ] 1.2 Document control-plane versus external enforcement ADR before enabling the new behavior; verify the document preserves existing managed-skill non-execution/no-installation rules and links the ticket/spec.

## 2. Observable behavior via TDD

- [ ] 2.1 Implement “Separate effective permissions and controls” through confirmed public boundaries, one failing behavior test then minimal implementation at a time; verify every scenario (“Inspect indirect permissions”) passes with its literal specified outcome and no private-helper assertions.

- [ ] 2.2 Implement “Fail-closed mandatory preflight” through confirmed public boundaries, one failing behavior test then minimal implementation at a time; verify every scenario (“Required credential mediation is missing”; “Optional gap”) passes with its literal specified outcome and no private-helper assertions.

- [ ] 2.3 Implement “Dimension-specific policy update support” through confirmed public boundaries, one failing behavior test then minimal implementation at a time; verify every scenario (“Static filesystem and dynamic network”) passes with its literal specified outcome and no private-helper assertions.

- [ ] 2.4 Implement “Truthful noop and placement boundaries” through confirmed public boundaries, one failing behavior test then minimal implementation at a time; verify every scenario (“Noop or directory adapter is selected”) passes with its literal specified outcome and no private-helper assertions.

- [ ] 2.5 Implement “Deterministic non-executing preflight” through confirmed public boundaries, one failing behavior test then minimal implementation at a time; verify every scenario (“Local preflight suite”) passes with its literal specified outcome and no private-helper assertions.

## 3. Verification and publication

- [ ] 3.1 Verify every requirement/scenario against implementation and test/live evidence, run `go test ./...`, relevant package race checks, `go vet ./...`, CLI build, `openspec validate define-runtime-enforcement-capabilities --strict`, `openspec doctor` and diff checks; record actual results and distinguish fixture/mock from real-service validation.
- [ ] 3.2 Obtain ordinary independent review of the complete change, resolve actionable findings and rerun affected behavior tests; retain the final reviewed scope and acceptance matrix.
- [ ] 3.3 Run the final gpt-6.1-sol/high read-only OCR review covering all proposed PR files; publish only if no actionable findings and no important coverage gap remain. An unavailable required reviewer stops publication.
