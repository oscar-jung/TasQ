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
- `POST /agents/claim-next`
- `POST /tasks/:id/heartbeat`
- `POST /tasks/:id/release`
- `POST /tasks/:id/complete`
- `GET /tasks/:id/context`

## Notes
- Tasks are ordered by `display_order ASC` (fallback `created_at ASC`) within a project/parent.
- Dependency graph is validated as DAG at creation time.
- A task can move to `in_progress` or `done` only if all predecessor tasks are `done`.
- Queue lease lifecycle supports `claim -> heartbeat -> complete|release`.
- Topology-changing operations (`create task`, `add/remove dependency`, `move`, `reorder`) use project-scoped advisory transaction locks.
