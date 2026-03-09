# TasQ

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
- MCP (HTTP): http://localhost:8091/mcp

## Codex MCP setup
For the current Codex CLI environment, prefer the HTTP MCP transport over stdio.

```bash
docker compose up -d --build mcp
codex mcp add tasq-http --url http://localhost:8091/mcp
```

## AI-agent flow references
- Agent API contract: `tasq-backend/docs/agent_contract.md`
- Operations guide: `tasq-backend/docs/operations.md`
- Release checklist: `tasq-backend/docs/release_checklist.md`
- MCP session profiles: `tasq-mcp/docs/session_profiles.md`

## Runtime monitoring
- Project list badge (left panel):
  - number = `stale claims + heartbeat overdue + orphan in-progress + exhausted tasks`
- Task detail panel:
  - `Execution > Runtime Alerts` shows detailed alert lists.
  - You can change heartbeat threshold and refresh manually.
  - Queue summary includes `done/total`, `claimable`, `blocked`, and `exhausted`.

## Worker supervisor
Planner sessions should emit a worker spawn spec JSON file and then launch:

```bash
./scripts/spawn_workers.sh --spec-file /abs/path/to/workers.json
```

Spawner behavior:
- starts one `codex exec` worker session per worker spec
- supervises queue progress via `GET /projects/:id/runtime-alerts`
- respawns workers only while useful work may still progress
- stops immediately when any task has exhausted `max_attempts`

Why stop on `max_attempts` exhaustion:
- TasQ uses at-least-once execution semantics
- a restarted worker may safely re-claim unfinished work
- but once a task reaches `max_attempts`, the queue intentionally refuses further claims
- automatic respawn would only churn without unblocking the project
- exhausted tasks therefore require human review: adjust the task, raise `max_attempts`, or fix the underlying failure

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
./scripts/mcp_runtime_smoke.sh
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
