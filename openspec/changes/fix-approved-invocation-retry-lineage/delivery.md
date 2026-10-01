# Ticket #18 verification record

Parent: [#16](https://github.com/AllenMuu/agent-manager/issues/16). Slice: [#18](https://github.com/AllenMuu/agent-manager/issues/18). Reproduction baseline: `71ee2312d6eb417027d1a4cbcab665fe402e51f1`. Current PR base after updating to main: `1c743868a1e485fa3d2b24e8b840e78b5a0992e0` (merged #29; no run/governance changes).

## Scope and verification method

This change repairs approval evidence on retry completions. It does not deliver the enforcement provider or close parent #16. The immutable original request remains on the approval; the completion references its retry attempt.

The project has no installed `openspec-verify-change` skill. Verification below directly compares the requirements/scenarios, implementation and executable public-boundary evidence; CLI validation is reported separately.

| Requirement / scenario | Tasks | Evidence | Result |
| --- | --- | --- | --- |
| Retry success preserves original approval and links current request | 1.1–2.1 | `TestApprovedRetryCompletionPreservesPersistedLineage` uses Manager completion, GetApproval and Events after reopen | Passed |
| Approved policy decision remains distinct from blocked/failed result | 1.2, 2.2 | `TestApprovedRetryCompletionSeparatesPolicyFromExecutionResult`; failed case also recovers the ID from the retry request | Passed |
| Unrelated evidence cannot authorize completion | 1.2, 2.2 | `TestRetryCompletionDoesNotAuthorizeUnrelatedApproval`: different run, action, tool, action type, trace and domain | Passed |
| Missing/non-approved evidence cannot authorize completion | 2.2 | `TestRetryCompletionRejectsMissingApproval`, `TestRetryCompletionDoesNotAuthorizeNonApprovedDecision`: pending/rejected/expired | Passed |
| Original completion without an explicit ID still works | 2.2 | Existing `TestManagerEvaluatesApprovalAndWritesLinkedAudit` | Passed |
| Pre-canceled call is rejected before dispatch; late cancellation/capability block preserves unused approval | 1.1–1.2 | Existing `TestApprovedInvocationPropagatesApprovalContext`, now runs beyond the formerly failing cancellation assertion | Passed |
| Concurrent stores consume one approval at most once; replay cannot dispatch again | 1.2, 2.2 | Existing `TestApprovedInvocationPropagatesApprovalContext`; related package race run | Passed |
| Dispatched adapter error retains failed evidence and consumed approval | 2.2 | Independent `TestApprovedAdapterFailureRetainsConsumedAuthorization`: one observed adapter call, reopened approval/audit, rejected replay | Passed |
| Prior format compatibility and persisted lineage | 2.1–2.2 | Existing v1 store and v2 completion audit tests plus new store reopen test | Passed |

Policy snapshot correlation remains enforced by the existing immutable run snapshot and Store request/approval/audit validators; no format version changes were introduced.

## Red / green evidence

- On the baseline evaluator, the existing approved-invocation test failed with `completion recorded REQUIRE_APPROVAL, want ALLOW`.
- The independent success test was run before the repair and failed with REQUIRE_APPROVAL instead of ALLOW.
- A Go overlay of the baseline evaluator also reproduced failures for blocked/failed retry completions and the dispatched adapter-error regression, while retaining the current test files.
- Each regression passes with the repaired evaluator. Removing only the added domain check makes the domain correlation regression fail; the final implementation preserves unauthorized observed evidence rather than attempting authorized persistence.

## Validation

All commands below completed successfully after the final test additions and again after rebasing onto the current PR base (Go reused valid package caches where applicable):

- `go test ./...`
- `go test -race ./internal/run ./internal/governance ./internal/invocation`
- `go vet ./...`
- `go build -o /tmp/agent-manager-issue18 ./cmd/agent-manager`
- `openspec validate fix-approved-invocation-retry-lineage --strict`
- `openspec doctor`
- `git diff --check`

The CLI suite generated an untracked eval YAML beneath `internal/cli/.agent-manager/`; the identified test output was removed and is not part of the change.

## Review and lifecycle

Ordinary independent review covered the evaluator, Invoker, Store validators, all new tests, OpenSpec artifacts and the implementation plan. It independently reran `go test ./internal/run ./internal/governance -count=1` and `git diff --check`; no code defect was confirmed. Its staging-list documentation omission was corrected to include the governance regression.

Final gpt-6.1-sol/high read-only OCR review was attempted, but the subagent returned “You’ve hit your usage limit. Try again later.” before providing coverage or findings. Its publication gate remains incomplete in tasks.md; no claim of OCR execution/coverage or PR readiness is made. Review findings or incomplete coverage stop publication without fixing in that phase. Review report and reviewed refs are recorded with the task before publication. This change remains active pending review, PR acceptance and authorized merge; specification synchronization and archive follow merge.
