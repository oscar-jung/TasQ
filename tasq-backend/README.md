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
- `PATCH /tasks/:id/capabilities`
- `GET /tasks/:id/capabilities`
- `POST /agents/claim-next`
- `POST /projects/:id/claims/reconcile`
- `GET /projects/:id/runtime-alerts`
- `POST /tasks/:id/heartbeat`
- `POST /tasks/:id/release`
- `POST /tasks/:id/complete`
- `POST /tasks/:id/fail`
- `POST /tasks/:id/git-link`
- `GET /tasks/:id/context`
- `GET /tasks/:id/events`
- `GET /projects/:id/events`
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
- Capability-based claim filtering:
  - task `required_capabilities` can be set by `PATCH /tasks/:id/capabilities`
  - agent `POST /agents/claim-next` may provide `capabilities`; required set must be satisfied
- Queue lease lifecycle supports `claim -> heartbeat -> complete|release`.
- Queue lease lifecycle supports `claim -> heartbeat -> complete|release|fail`.
- Background stale-claim reconciler is enabled by default:
  - `RECONCILE_INTERVAL_SECONDS=30` (set `0` to disable)
- Runtime alert heartbeat threshold:
  - `HEARTBEAT_STALE_SECONDS=300`
- Claim lease defaults to `120s` when omitted.
- `complete`/`fail` expect structured `result_payload` JSON with required keys:
  `summary`, `changes`, `paths`, `commands`, `tests`, `artifacts`, `next_risks`.
  - Default schema `v2`: `summary` string, all other keys are arrays of strings.
  - Legacy schema `v1`: all keys are non-empty strings.
- Auth is configurable via:
  - `AUTH_MODE`: `off` (default), `optional`, `required`
  - `AUTH_TOKENS`: JSON array of token configs:
    `[{\"token\":\"...\",\"actor_type\":\"human|agent\",\"actor_id\":\"...\",\"scopes\":[...],\"projects\":[\"*\"|\"1\"|...]}]`
- Scope model:
  - `project:read`, `task:claim`, `task:update`, `task:complete`, `task:admin`
- In `task:admin` flows, human actor is required by default.
- Topology-changing operations (`create task`, `add/remove dependency`, `move`, `reorder`) use project-scoped advisory transaction locks.
