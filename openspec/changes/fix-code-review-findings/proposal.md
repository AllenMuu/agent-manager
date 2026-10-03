## Why

The full-code report at commit 2296136f identified nine defects in guarded Skill operations and read-only recommendations. Reconcile each against refreshed main b388a67, repair remaining defects with test-first changes, and preserve protections already delivered since the review.

## What Changes

- Preserve concurrent user files when initialization rolls back.
- Generate literal, scoped Git ignore rules for managed links, rejecting names that cannot be represented in one rule.
- Expand the documented home-relative library configuration.
- Preserve nested non-boundary marker evidence inside its nearest project scope.
- Verify the original batch rollback, placement, journal serialization, source revalidation and Pi cases against the current baseline.

## Capabilities

### New Capabilities

- `guarded-skill-maintenance`: Regression guarantees for safe initialization, precise tracking guidance, documented library paths, and scoped recommendation evidence.

### Modified Capabilities

None. Existing Skill lifecycle and adapter contracts remain unchanged.

## Impact

Focused Go changes in initcmd, diagnostic, config and stack, with regression tests at their existing public interfaces and CLI integration. No managed Skill execution, runtime integration, dependency installation, or managed-workflow network behavior is added.
