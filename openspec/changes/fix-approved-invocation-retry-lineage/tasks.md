## 1. Repair approved retry completion

- [x] 1.1 Reproduce the existing approved-invocation cancellation regression through Invoker.Invoke; verify the baseline test fails with completion decision mismatch.
- [x] 1.2 Resolve explicit approvals against their original requests and validate attempt correlation; verify the existing approved-invocation test passes including cancellation, concurrent consumption and replay.

## 2. Independent behavior regressions

- [x] 2.1 Add an independent public run-boundary test for a new retry request and persisted approval/approver lineage; verify it fails against the baseline evaluator and passes with the fix after store reopen.
- [x] 2.2 Independently cover successful, blocked and failed completion outcomes plus unrelated approval correlation; verify existing unauthorized and historical completion tests still pass.

## 3. Validate and review

- [x] 3.1 Run all Go tests, related package race tests, vet, CLI build, strict OpenSpec validation and diff checks; record actual results.
- [x] 3.2 Complete independent review and resolve actionable findings with behavior tests; preserve the review evidence for the final diff.
- [ ] 3.3 Run the prescribed read-only OCR subagent review on the final PR scope; publish a PR only if there are no actionable findings and coverage is complete.
