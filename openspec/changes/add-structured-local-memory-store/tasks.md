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
- [x] 3.2 Obtain ordinary independent review of the complete change, resolve actionable findings and rerun affected behavior tests; retain the final reviewed scope and acceptance matrix.
- [x] 3.3 Run the final gpt-6.1-sol/high read-only OCR review covering all proposed PR files; publish only if no actionable findings and no important coverage gap remain. An unavailable required reviewer stops publication.

普通独立规格审查在 `3616a46` 发现非 UTF-8 输入经 JSON 编码后改变所有权/来源和操作意图。已补公共边界 RED→GREEN 回归并拒绝这些输入；corrupted state 不再静默标准化。复核证据见 delivery.md；3.2/3.3 继续等待 controller 验证，不因修复提交而勾选。

普通独立质量审查在 `3e82787` 发现 canonical source/destination 导入重叠、JSON 未配对 Unicode surrogate 转义及 ADR 旧链接。已补公共 RED→GREEN 回归和合法 Unicode 正向验证，拒绝 canonical inode 别名来源、严格验证 JSON scalar escapes 并修复 main spec 链接；证据见 delivery.md。3.2/3.3 仍待 controller 复核和最终 OCR。

Controller acceptance: ordinary specification and quality re-reviews PASS at `9771451994db1a638cf27b6224362b6512ba1258`. Required final independent `gpt-6.1-sol/high` OCR PASS at that same head, 12/12 included plus 14/14 supplemental excluded entries (26/26 aggregate, zero skipped). Publication metadata receives a separate incremental read-only gate before push; implementation acceptance does not claim merge or archive.
