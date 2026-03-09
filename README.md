# tasq

Dependency-aware task management service for self-hosting.

## Stack
- Backend: Go (`net/http`) + PostgreSQL
- Frontend: React + Vite
- Runtime: Docker Compose

## Domain rules implemented
- Task tree (`parent_task_id`) is separate from execution dependencies (`task_dependencies`).
- Dependency graph is validated as DAG during dependency creation.
- Task can move to `in_progress` or `done` only when all predecessor tasks are `done`.
- Task claim queue returns the oldest claimable planned task.
- Task result/spec are stored as Markdown text (`spec_md`, `result_md`).
- Topology-changing operations use project-scoped transactional advisory locks.

## Quick start
```bash
docker compose up --build
```

- Web: http://localhost:5173
- API: http://localhost:8080
- DB: localhost:5432

## API summary
- `GET /healthz`
- `GET /projects`
- `POST /projects`
- `POST /projects/:id/tasks`
- `GET /projects/:id/tasks`
- `GET /projects/:id/tasks/tree`
- `POST /tasks/:id/dependencies`
- `GET /tasks/:id/dependencies`
- `DELETE /tasks/:id/dependencies/:predecessorTaskId`
- `POST /tasks/:id/move`
- `POST /tasks/reorder`
- `PATCH /tasks/:id/content`
- `POST /tasks/:id/delete`
- `PATCH /tasks/:id/status`
- `PATCH /tasks/:id/execution-policy`
- `POST /agents/claim-next`
- `POST /tasks/:id/heartbeat`
- `POST /tasks/:id/release`
- `POST /tasks/:id/complete`
- `POST /tasks/:id/fail`
- `POST /tasks/:id/git-link`
- `GET /tasks/:id/context`

## Smoke tests
```bash
./scripts/claim_concurrency_smoke.sh
./scripts/dependency_cycle_race_smoke.sh
./scripts/topology_move_reorder_smoke.sh
```

## Next implementation targets
- Agent capability-based claim filters
- Dependency graph visualization

## Auth (backend)
- `AUTH_MODE`: `off` (default), `optional`, `required`
- `AUTH_TOKENS`: JSON array token map with actor/scopes/project scopes
