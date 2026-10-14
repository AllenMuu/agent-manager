## 1. Delivery prerequisites

- [x] 1.1 Re-read ticket #23, verify delivered prerequisites #21 and #22, and confirm the Codex/Pi public test seams with the user before writing tests; record the selected MCP mechanism and Pi/adapter versions with the baseline.

## 2. Observable behavior via TDD

- [x] 2.1 Implement “Two real agent readers and writers” through confirmed public boundaries, one failing behavior test then minimal implementation at a time; verify every scenario (“Codex writes and Pi reads”; “Real Mem0 two-agent acceptance”) passes with its literal specified outcome and no private-helper assertions.

- [x] 2.2 Implement “Effective version convergence” through confirmed public boundaries, one failing behavior test then minimal implementation at a time; verify every scenario (“Both agents observe replacement”) passes with its literal specified outcome and no private-helper assertions.

- [x] 2.3 Implement “Owner isolation and inspectable provenance” through confirmed public boundaries, one failing behavior test then minimal implementation at a time; verify every scenario (“Cross-project read attempt”) passes with its literal specified outcome and no private-helper assertions.

- [x] 2.4 Implement “Per-agent tested capability status” through confirmed public boundaries, one failing behavior test then minimal implementation at a time; verify every scenario (“One integration lacks write support”) passes with its literal specified outcome and no private-helper assertions.

- [x] 2.5 Implement “One canonical source of truth” through confirmed public boundaries, one failing behavior test then minimal implementation at a time; verify every scenario (“Integration is enabled”) passes with its literal specified outcome and no private-helper assertions.

- [x] 2.6 Implement “Interoperable MCP tool schemas” through the public stdio MCP server, one failing wire-format regression test then the minimal fix; verify Pi lists and calls the registered tools, including `memory_status` with an empty `required` array.

## 3. Verification and publication

- [x] 3.1 Verify every requirement/scenario against implementation and test/live evidence, run `go test ./...`, relevant package race checks, `go vet ./...`, CLI build, `openspec validate share-memory-across-codex-and-pi --strict`, `openspec doctor` and diff checks; record actual results and distinguish fixture/mock from real-service validation.
- [x] 3.2 Obtain ordinary independent review of the complete change, resolve actionable findings and rerun affected behavior tests; retain the final reviewed scope and acceptance matrix.
- [x] 3.3 Complete the final read-only review on all eligible proposed PR files and manually review files excluded by OCR. The configured gpt-6.1-sol/high attempts failed; at the user's explicit direction, `open-code-review delegate` reviewed all 5 eligible files and all 14 excluded files were manually reviewed (19/19 total). The delegate found and the implementation repaired a missing synthetic PROJECT ID guard. The exact gpt-6.1-sol/high review is unavailable and is not claimed; the user-selected fallback found no remaining actionable findings or important coverage gaps.
