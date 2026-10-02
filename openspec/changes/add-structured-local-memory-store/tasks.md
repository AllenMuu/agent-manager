## 1. Delivery prerequisites

- [x] 1.1 Re-read ticket #20 and its native blockers, verify prerequisite behavior is delivered, and confirm the new public test seams with the user before writing tests; record the selected boundaries and baseline.

## 2. Observable behavior via TDD

- [x] 2.1 Implement “Durable owner and provenance” through confirmed public boundaries, one failing behavior test then minimal implementation at a time; verify every scenario (“Reopen owned record”) passes with its literal specified outcome and no private-helper assertions.

- [x] 2.2 Implement “Conditional serialized updates” through confirmed public boundaries, one failing behavior test then minimal implementation at a time; verify every scenario (“Competing version updates”) passes with its literal specified outcome and no private-helper assertions.

- [x] 2.3 Implement “Atomic lifecycle and history” through confirmed public boundaries, one failing behavior test then minimal implementation at a time; verify every scenario (“Supersede and forget”; “Supersession fails before commit”) passes with its literal specified outcome and no private-helper assertions.

- [x] 2.4 Implement “Recoverable local writes” through confirmed public boundaries, one failing behavior test then minimal implementation at a time; verify every scenario (“Failure and retry evidence”) passes with its literal specified outcome and no private-helper assertions.

- [x] 2.5 Implement “Explicit legacy import” through confirmed public boundaries, one failing behavior test then minimal implementation at a time; verify every scenario (“No implicit conversion”; “Confirmed import”) passes with its literal specified outcome and no private-helper assertions.

## 3. Verification and publication

- [x] 3.1 Verify every requirement/scenario against implementation and test/live evidence, run `go test ./...`, relevant package race checks, `go vet ./...`, CLI build, `openspec validate add-structured-local-memory-store --strict`, `openspec doctor` and diff checks; record actual results and distinguish fixture/mock from real-service validation.
- [ ] 3.2 Obtain ordinary independent review of the complete change, resolve actionable findings and rerun affected behavior tests; retain the final reviewed scope and acceptance matrix.
- [ ] 3.3 Run the final gpt-6.1-sol/high read-only OCR review covering all proposed PR files; publish only if no actionable findings and no important coverage gap remain. An unavailable required reviewer stops publication.

普通独立规格审查在 `3616a46` 发现非 UTF-8 输入经 JSON 编码后改变所有权/来源和操作意图。已补公共边界 RED→GREEN 回归并拒绝这些输入；corrupted state 不再静默标准化。复核证据见 delivery.md；3.2/3.3 继续等待 controller 验证，不因修复提交而勾选。
