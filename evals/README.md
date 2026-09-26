# Evaluation catalog

Evaluation cases are local, versioned engineering assets. A case uses a
deterministic `rule` verifier in the first harness; an agent runtime can write a
candidate response into a candidate directory without changing the case
definition. The runner never executes candidate content.

The initial fixture suite is `java-backend` and contains four runnable cases.
The following 20-case baseline is the planned regression set, grouped by the
kind of development work it exercises:

1. `java-spring-transaction-boundary`
2. `java-spring-after-commit`
3. `java-sql-index-optimization`
4. `java-sql-pagination-plan`
5. `java-redis-timeout`
6. `java-redis-concurrency`
7. `java-cache-stampede`
8. `java-kafka-retry`
9. `java-rest-error-contract`
10. `java-security-authorization`
11. `architecture-service-boundary`
12. `architecture-data-migration`
13. `github-pr-regression-review`
14. `github-pr-security-review`
15. `repository-package-refactor`
16. `repository-test-fixture-refactor`
17. `agent-agents-md-scope`
18. `agent-skill-compatibility`
19. `agent-memory-promotion-gate`
20. `agent-adapter-capability-gap`

Each promoted case should add explicit required and forbidden points plus an
evidence expectation. A smaller fixture set is intentionally shipped first so
the protocol and result comparison remain useful before an agent-specific
benchmark corpus is approved.
