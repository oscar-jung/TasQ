# TasQ Planner Decomposition Guide

Use this guide when the planner is generating task trees for agent execution.

## Primary goal
- Prefer trees that are easy to rerun, easy to recover, and unlikely to create file-level merge conflicts.

## Decomposition rules
- Keep root tasks few.
  - Good default: `2-4` roots.
- Use roots to represent genuinely separate delivery streams.
  - Example: `CLI surface`, `download engine`, `docs/test harness`
- If two tasks will likely touch the same files or module, do not keep them as wide siblings.
  - Make them parent/child or add an explicit dependency chain.
- Prefer vertical chains when one task naturally refines or unlocks the next task.
  - Example: `Define options` -> `Wire command` -> `Add tests`
- Use wide parallel siblings only when the work is merge-safe.
  - Example: docs vs backend integration fixtures in unrelated paths.

## Capability rules
- Use generic labels first.
  - `go`, `cli`, `integration`, `testing`, `docs`, `frontend`, `backend`, `db`
- Do not over-label planning or documentation tasks.
- Ensure at least one root task is claimable by a generic worker immediately after planning.

## Git-first rules for code projects
- For agent-driven code projects, create the project with `git_policy="required"`.
- For code tasks:
  - workers should link `baseline` before editing
  - workers should link `produced` before completion
  - reruns should usually create a `rerun_branch`

## Good pattern
```text
Root: CLI surface
  └─ Define command/options contract
     └─ Wire Cobra root/download commands
        └─ Add command validation tests
```

## Risky pattern
```text
Root: Project
  ├─ Add flags
  ├─ Add validation
  ├─ Add Cobra command
  ├─ Add tests
  └─ Update help text
```

The risky pattern looks parallel, but most of those tasks will touch the same files and should usually be chained.
