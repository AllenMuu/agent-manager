## 1. Delivery prerequisites

- [ ] 1.1 Re-read ticket #23 and its native blockers, verify prerequisite behavior is delivered, and confirm the new public test seams with the user before writing tests; record the selected boundaries and baseline.

## 2. Observable behavior via TDD

- [ ] 2.1 Implement “Two real agent readers and writers” through confirmed public boundaries, one failing behavior test then minimal implementation at a time; verify every scenario (“Codex writes and Claude reads”; “Real Mem0 two-agent acceptance”) passes with its literal specified outcome and no private-helper assertions.

- [ ] 2.2 Implement “Effective version convergence” through confirmed public boundaries, one failing behavior test then minimal implementation at a time; verify every scenario (“Both agents observe replacement”) passes with its literal specified outcome and no private-helper assertions.

- [ ] 2.3 Implement “Owner isolation and inspectable provenance” through confirmed public boundaries, one failing behavior test then minimal implementation at a time; verify every scenario (“Cross-project read attempt”) passes with its literal specified outcome and no private-helper assertions.

- [ ] 2.4 Implement “Per-agent tested capability status” through confirmed public boundaries, one failing behavior test then minimal implementation at a time; verify every scenario (“One integration lacks write support”) passes with its literal specified outcome and no private-helper assertions.

- [ ] 2.5 Implement “One canonical source of truth” through confirmed public boundaries, one failing behavior test then minimal implementation at a time; verify every scenario (“Integration is enabled”) passes with its literal specified outcome and no private-helper assertions.

## 3. Verification and publication

- [ ] 3.1 Verify every requirement/scenario against implementation and test/live evidence, run `go test ./...`, relevant package race checks, `go vet ./...`, CLI build, `openspec validate share-memory-across-codex-and-claude --strict`, `openspec doctor` and diff checks; record actual results and distinguish fixture/mock from real-service validation.
- [ ] 3.2 Obtain ordinary independent review of the complete change, resolve actionable findings and rerun affected behavior tests; retain the final reviewed scope and acceptance matrix.
- [ ] 3.3 Run the final gpt-6.1-sol/high read-only OCR review covering all proposed PR files; publish only if no actionable findings and no important coverage gap remain. An unavailable required reviewer stops publication.
