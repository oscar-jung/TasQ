# Tasq Backend Endpoints (Quick Reference)

This is a concise map of HTTP endpoints to handler functions under `internal/server`.

## Health
- `GET /healthz`
  - Handler: `(*server).healthz` in `internal/server/main.go`
  - Purpose: liveness check.

## Projects
- `GET /projects`
  - Handler: `(*server).listProjects` in `internal/server/projects.go`
  - Purpose: list all projects.

- `POST /projects`
  - Handler: `(*server).createProject` in `internal/server/projects.go`
  - Purpose: create a new project.

- `PATCH /projects/:project_id`
  - Handler: `(*server).updateProject` in `internal/server/projects.go`
  - Purpose: rename/update project metadata.

## Project Task Views
- `GET /projects/:project_id/tasks`
  - Handler: `(*server).listProjectTasks` in `internal/server/projects.go`
  - Purpose: list project tasks as a flat list.

- `GET /projects/:project_id/tasks/tree`
  - Handler: `(*server).getProjectTaskTree` in `internal/server/projects.go`
  - Purpose: return hierarchical task tree.

- `POST /projects/:project_id/tasks`
  - Handler: `(*server).createTask` in `internal/server/tasks.go`
  - Purpose: create a root/child task.

- `POST /projects/:project_id/tasks/validate`
  - Handler: `(*server).validateProjectTree` in `internal/server/tree_guard.go`
  - Purpose: validate tree integrity and optionally cleanse invalid statuses.

- `GET /projects/:project_id/events?limit=50`
  - Handler: `(*server).listProjectEvents` in `internal/server/events.go`
  - Purpose: list project-level audit timeline events (newest first).

- `POST /projects/:project_id/claims/reconcile`
  - Handler: `(*server).reconcileProjectClaims` in `internal/server/reconcile.go`
  - Purpose: manually release expired active claims and restore stale in-progress tasks.

## Task Dependencies
- `POST /tasks/:task_id/dependencies`
  - Handler: `(*server).addDependency` in `internal/server/tasks.go`
  - Purpose: add predecessor dependency (DAG-safe).

- `GET /tasks/:task_id/dependencies`
  - Handler: `(*server).listDependencies` in `internal/server/tasks.go`
  - Purpose: list predecessor/successor edges for a task.

- `DELETE /tasks/:task_id/dependencies/:predecessor_id`
  - Handler: `(*server).removeDependency` in `internal/server/tasks.go`
  - Purpose: remove a dependency edge.

## Task Content/Lifecycle
- `PATCH /tasks/:task_id/status`
  - Handler: `(*server).updateTaskStatus` in `internal/server/tasks.go`
  - Purpose: update status (`planned`, `in_progress`, `done`, `failed`) with dependency checks.

- `PATCH /tasks/:task_id/execution-policy`
  - Handler: `(*server).updateTaskExecutionPolicy` in `internal/server/tasks.go`
  - Purpose: update task execution policy (`max_attempts`).

- `PATCH /tasks/:task_id/capabilities`
  - Handler: `(*server).updateTaskCapabilities` in `internal/server/tasks.go`
  - Purpose: update required capability labels for claim filtering.

- `PATCH /tasks/:task_id/content`
  - Handler: `(*server).updateTaskContent` in `internal/server/tasks.go`
  - Purpose: update title/spec/result fields.

- `POST /tasks/:task_id/delete`
  - Handler: `(*server).deleteTask` in `internal/server/tasks.go`
  - Purpose: delete task by strategy (`delete_subtree`, `promote_children`, `delete_if_leaf`).

- `POST /tasks/reorder`
  - Handler: `(*server).reorderTasks` in `internal/server/tasks.go`
  - Purpose: reorder sibling tasks.

- `POST /tasks/:task_id/move`
  - Handler: `(*server).moveTask` in `internal/server/tasks.go`
  - Purpose: move task under a new parent/index.

- `GET /tasks/:task_id/context`
  - Handler: `(*server).getTaskContext` in `internal/server/agents.go`
  - Purpose: get task + dependencies + parent chain context.

- `GET /tasks/:task_id/events?limit=50`
  - Handler: `(*server).listTaskEvents` in `internal/server/events.go`
  - Purpose: list task-level lifecycle/audit events (newest first).

## Agent Queue/Claims
- `POST /agents/claim-next`
  - Handler: `(*server).claimNext` in `internal/server/agents.go`
  - Purpose: atomically claim the next executable task.
  - Includes auto stale-claim reconciliation for the target project before selection.
  - Optional request field: `capabilities` string array.
    - If task has non-empty `required_capabilities`, all required values must be included in `capabilities`.

- `POST /tasks/:task_id/heartbeat`
  - Handler: `(*server).heartbeatTaskClaim` in `internal/server/agents.go`
  - Purpose: extend claim lease.

- `POST /tasks/:task_id/release`
  - Handler: `(*server).releaseTaskClaim` in `internal/server/agents.go`
  - Purpose: release active claim and optionally reset status.

- `POST /tasks/:task_id/complete`
  - Handler: `(*server).completeTask` in `internal/server/agents.go`
  - Purpose: complete claimed task and finalize result.
  - Required body field: `result_payload` JSON object with keys
    `summary`, `changes`, `paths`, `commands`, `tests`, `artifacts`, `next_risks`.

- `POST /tasks/:task_id/fail`
  - Handler: `(*server).failTask` in `internal/server/agents.go`
  - Purpose: mark claimed task as failed and finalize run payload.
  - Required body field: `result_payload` JSON object with keys
    `summary`, `changes`, `paths`, `commands`, `tests`, `artifacts`, `next_risks`.

- `POST /tasks/:task_id/git-link`
  - Handler: `(*server).linkTaskGitRef` in `internal/server/tasks.go`
  - Purpose: link git metadata (`repo`, `branch`, `base_commit`, `commit_sha`) to task history.

## Notes
- Tree guard mode is controlled by env `TREE_GUARD_MODE`:
  - `off` (default), `validate`, `cleanse`.
- Topology writes use project advisory lock via `lockProjectTopology`.
- Claim lease defaults to 120 seconds when omitted.
- Claim lifecycle now returns and accepts `claim_token` for heartbeat/release/complete/fail validation.
- Auth/RBAC:
  - `AUTH_MODE`: `off`, `optional`, `required`
  - `AUTH_TOKENS`: token JSON with `actor_type`, `actor_id`, `scopes`, and project scopes
  - `task:admin` flows require `human` actor.
