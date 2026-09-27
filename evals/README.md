# Evaluation catalog

Evaluation cases are local, versioned engineering assets. New `v2` cases
contain a canonical `intent.yaml`, explicit required/forbidden points, and an
evidence point. Generate responses with the agent and configuration being
tested, then save them as `<case-id>.md` in a candidate directory. The rule
runner scores only that directory. It neither invokes an agent nor executes
candidate content. Existing `v1` cases remain runnable with their original
field requirements; they do not need an intent file or evidence point.

For example, generate one response for each case under two directories, then
run and compare them:

```bash
agent-manager eval run java-backend --candidate-dir ./responses/baseline --agent-label codex --config-version baseline
agent-manager eval run java-backend --candidate-dir ./responses/candidate --agent-label codex --config-version candidate
agent-manager eval compare .agent-manager/evals/<baseline-run>.yaml .agent-manager/evals/<candidate-run>.yaml
```

The initial fixture suite is `java-backend` and contains four runnable cases.
The following 20-case baseline is planned. Each row records the behavior and
evidence that a future runnable case must test. The first, third, fifth, and
thirteenth themes are represented by the current four-case suite.

| Planned case | Required behavior | Evidence to demand |
| --- | --- | --- |
| `java-spring-transaction-boundary` | Identify the transaction boundary and after-commit event delivery | Commit and rollback tests |
| `java-spring-after-commit` | Separate database commit from external side effects | Failure and retry trace |
| `java-sql-index-optimization` | Choose a composite index from predicate and sort order | Before and after query plans |
| `java-sql-pagination-plan` | Explain stable ordering and deep-page cost | Plan and latency at two offsets |
| `java-redis-timeout` | Bound total request time and define fallback | Slow dependency test |
| `java-redis-concurrency` | Preserve atomicity across competing writers | Concurrent interleaving test |
| `java-cache-stampede` | Limit simultaneous refreshes | Load test with cache expiry |
| `java-kafka-retry` | Distinguish retriable failures from poison messages | Redelivery and dead-letter test |
| `java-rest-error-contract` | Preserve status codes and response shape | Contract test for each error class |
| `java-security-authorization` | Check resource ownership at the service boundary | Authorized and unauthorized tests |
| `architecture-service-boundary` | State ownership and coupling tradeoffs | Dependency diagram and failure path |
| `architecture-data-migration` | Plan reversible rollout and compatibility | Expand/migrate/contract checks |
| `github-pr-regression-review` | Identify a reproducible changed-line regression | Reproduction and regression test |
| `github-pr-security-review` | Trace a concrete trust-boundary failure | Exploit path and guarded test |
| `repository-package-refactor` | Preserve public behavior while moving code | Before and after test results |
| `repository-test-fixture-refactor` | Keep fixtures isolated and deterministic | Repeated test run |
| `agent-agents-md-scope` | Respect nearest repository instructions | Scoped instruction resolution example |
| `agent-skill-compatibility` | Explain an advisory mismatch for explicit selection | Selected Skill and warning output |
| `agent-memory-promotion-gate` | Require explicit review before long-term write | Rejected and confirmed promotion paths |
| `agent-adapter-capability-gap` | Surface unsupported runtime permissions | Binding report naming each gap |

Every promoted case must add its own intent artifact, assertions, forbidden
advice, and evidence point. The current suite is a small runnable fixture set;
the planned rows are not included in scores until promoted.
