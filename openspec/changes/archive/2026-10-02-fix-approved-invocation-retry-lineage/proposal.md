## Why

An approved invocation retry creates a new request audit, while its approval remains bound to the original request. Completion currently looks up approval only by the retry request ID and loses valid approval attribution; the existing cancellation regression test fails on main.

## What Changes

- Resolve explicitly referenced approval evidence against its original request and the current attempt, preserving exact run/action/tool/trace/policy correlation.
- Preserve blocked completions and unused authorization when dispatch never happened, while keeping atomic single-use consumption for dispatched calls.
- Add public-boundary regressions for successful retries and invalid correlation, retaining historical audit compatibility.

## Capabilities

### New Capabilities

- `approved-invocation-completion-lineage`: Correlate an approved retry's completion to both its execution attempt and original approval evidence.

### Modified Capabilities

None. The main spec directory currently contains no published capability files; existing identity and governance change specs remain applicable context.

## Impact

The run completion evaluator and public run/governance tests. No new runtime, network operation, credential field, persisted format version or CLI command. This is the prerequisite regression repair for issue #16, not completion of that parent issue.
