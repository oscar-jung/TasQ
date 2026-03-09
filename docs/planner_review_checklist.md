# TasQ Planner Review Checklist

Use this checklist before a planner session finishes.

## Tree shape
- Root task count is intentionally small.
  - Good default: `2-4`
- Sibling tasks are truly parallel-safe.
- Tasks that touch the same files or module are chained vertically instead of spread wide.

## Execution readiness
- At least one root task is immediately claimable.
- Dependency edges are only added where execution order is real.
- No task is blocked purely because capabilities were over-specified.

## Capability quality
- Capability labels are generic unless specialization is necessary.
- Planning or documentation tasks are not over-labeled.
- At least one worker profile can claim the first runnable task set.

## Git-first quality
- For agent-driven code projects, project `git_policy` is `required`.
- Workers can clearly tell when to link:
  - `baseline`
  - `rerun_branch`
  - `produced`

## Rerun quality
- If an upstream task changes, the planner tree still makes rerun scope obvious.
- Related implementation tasks are grouped so `subtree` and `both` invalidation remain meaningful.

## Result quality
- Tasks are scoped small enough that a worker can leave a strong handoff result.
- Tasks are not so large that result markdown becomes vague or multi-purpose.
