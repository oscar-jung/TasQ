# tasq

[한국어 README](./README.ko.md)

TasQ is a self-hosted task orchestration tool for human + AI execution.
It is built for workflows where you plan once, split into actionable tasks, and run tasks concurrently with AI agents.

## Who this is for
- You use tools like Codex CLI / Gemini CLI and want visible task-level progress.
- You want to intervene safely while agents are working in parallel.
- You want recovery tools for stuck/expired jobs and a clear audit trail.

## Core idea
TasQ separates:
- Task structure (tree): parent/child planning hierarchy.
- Task execution constraints (dependencies): prerequisite edges.
- Task runtime lifecycle: claim, heartbeat, complete/fail/release.

This separation allows safe parallel execution while keeping the plan editable.

## Typical usage scenario (easy mode)
Use this when you are not deeply technical but want control.

1. Plan with AI in chat.
Ask AI to define scope, milestones, and completion criteria.

2. Create project and task tree in TasQ.
AI (or you) creates projects/tasks/dependencies via UI or API.

3. Let agents run concurrently.
Agents claim available tasks and work in parallel.

4. Intervene only at control gates.
You review and decide when to split, retry, fail, or redirect.

5. Final check before merge/release.
Run regression scripts and release checklist.

## When users should intervene
- Before execution starts:
  - Approve task granularity.
  - Confirm dependencies and capabilities.
- During execution:
  - Watch project badges in the left panel.
  - Open `Task Detail > Execution > Runtime Alerts` for details.
  - Handle `failed`, stale, or orphan tasks.
- Before final merge:
  - Run final regression suite.
  - Review release checklist.

## Quick start
```bash
docker compose up --build
```

- Web: http://localhost:5173
- API: http://localhost:8080
- DB: localhost:5432

## AI-agent flow references
- Agent API contract: `tasq-backend/docs/agent_contract.md`
- Operations guide: `tasq-backend/docs/operations.md`
- Release checklist: `tasq-backend/docs/release_checklist.md`

## Runtime monitoring
- Project list badge (left panel):
  - number = `stale claims + heartbeat overdue + orphan in-progress`
- Task detail panel:
  - `Execution > Runtime Alerts` shows detailed alert lists.
  - You can change heartbeat threshold and refresh manually.

## Agent runtime demo
```bash
PROJECT_ID=1 ./scripts/agent_worker_demo.sh
PROJECT_ID=1 ACTION=fail ./scripts/agent_worker_demo.sh
PROJECT_ID=1 ACTION=release ./scripts/agent_worker_demo.sh
```

## Smoke and regression tests
```bash
./scripts/claim_concurrency_smoke.sh
./scripts/dependency_cycle_race_smoke.sh
./scripts/topology_move_reorder_smoke.sh
./scripts/lease_reclaim_smoke.sh
./scripts/capability_claim_smoke.sh
./scripts/final_regression_suite.sh
```

## Tech stack
- Backend: Go (`net/http`) + PostgreSQL
- Frontend: React + Vite
- Runtime: Docker Compose

## Auth (backend)
- `AUTH_MODE`: `off` (default), `optional`, `required`
- `AUTH_TOKENS`: JSON array token map with actor/scopes/project scopes

## Next implementation targets
- Dependency graph visualization
- Dashboard-level runtime alert summaries
