# tasq-backend

## Endpoints
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
- `GET /tasks/:id/events`
- `GET /projects/:id/events`

## Agent integration contract
- `docs/agent_contract.md`
- Demo worker script: `../scripts/agent_worker_demo.sh`
- Operations playbook: `docs/operations.md`

## Notes
- Tasks are ordered by `display_order ASC` (fallback `created_at ASC`) within a project/parent.
- Dependency graph is validated as DAG at creation time.
- A task can move to `in_progress` or `done` only if all predecessor tasks are `done`.
- Task status includes `failed`, and execution policy includes `max_attempts` (default `5`).
- Queue lease lifecycle supports `claim -> heartbeat -> complete|release`.
- Queue lease lifecycle supports `claim -> heartbeat -> complete|release|fail`.
- Claim lease defaults to `120s` when omitted.
- `complete`/`fail` expect structured `result_payload` JSON with required keys:
  `summary`, `changes`, `paths`, `commands`, `tests`, `artifacts`, `next_risks`.
- Auth is configurable via:
  - `AUTH_MODE`: `off` (default), `optional`, `required`
  - `AUTH_TOKENS`: JSON array of token configs:
    `[{\"token\":\"...\",\"actor_type\":\"human|agent\",\"actor_id\":\"...\",\"scopes\":[...],\"projects\":[\"*\"|\"1\"|...]}]`
- Scope model:
  - `project:read`, `task:claim`, `task:update`, `task:complete`, `task:admin`
- In `task:admin` flows, human actor is required by default.
- Topology-changing operations (`create task`, `add/remove dependency`, `move`, `reorder`) use project-scoped advisory transaction locks.
