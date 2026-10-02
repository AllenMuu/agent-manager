## Context

See proposal.md. Approval P references original request A, while Invoker creates retry B. Completion lookup by B alone misses P. The baseline regression reports `completion recorded REQUIRE_APPROVAL, want ALLOW` after late cancellation.

## Goals / Non-Goals

**Goals:** Correlate P/A/B safely, return blocked and successful completion evidence, preserve single-use consumption and historical reads.

**Non-Goals:** Runtime execution, permission escalation, human-authority authentication, policy revisions, new store schema, unrelated package refactoring.

## Decisions

When completion carries an explicit approval identifier (or its request carries one), look up that approval directly, then validate run and original-request correlation before applying approved evidence. Without an explicit identifier, retain approval lookup by the original request. Do not rebind the approval's immutable RequestAuditID to a retry.

Keep approval consumption in Invoker/Store as the dispatch gate; the completion evaluator records observed execution and authorization, rather than dispatching actions. Blocked results can retain approved policy evidence without implying execution. Existing store validation continues checking decider, policy and request linkage.

Test through confirmed public seams: Invoker.Invoke, Manager.EvaluateAndRecordInvocation, Store.GetApproval and Store.Events. Use the existing deterministic context cancellation and external invocation adapter boundaries. Do not test private lookup helpers or inspect database JSON as the new regression seam.

The existing end-to-end test is the first red signal. Add an independent run-boundary retry test so the successful completion assertion is reachable independently of cancellation. Separate mismatched lineage and dispatched-error scenarios into independent public tests.

## Risks / Trade-offs

- [Explicit ID mistaken for authorization] → require approved status, decider and exact original-request/attempt correlation; keep invalid completion evidence unauthorized.
- [Fix changes single-use dispatch] → leave consumption and release code unchanged; run the existing concurrent/replay scenario and race checks.
- [Successful invocation appears failed due to persistence error] → preserve existing explicit error outcome; do not introduce automatic retries.
- [Historical compatibility regression] → run complete run/governance tests, including legacy audit reads.

## Migration Plan

No on-disk migration or dependency change. Apply the focused evaluator change and behavioral regressions; roll back code if verification fails. Refs issue #18; parent #16 remains open.
