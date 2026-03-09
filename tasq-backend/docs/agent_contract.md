# TasQ Agent API Contract (M7)

This document defines the stable contract for external agents.

## Auth
- `AUTH_MODE=off`: no token required.
- `AUTH_MODE=optional|required`: pass `Authorization: Bearer <token>`.
- Required scopes for agent runtime:
  - `project:read`
  - `task:claim`
  - `task:update`
  - `task:complete`

## Task Eligibility (Claim Queue)
A task is claimable when all conditions are true:
- `status = planned`
- parent is `done` (or no parent)
- all dependency predecessors are `done`
- no active unexpired claim exists
- total claims `< max_attempts`
- if task `required_capabilities` is non-empty, it must be subset of agent request `capabilities`

Ordering:
- `display_order ASC`, then `created_at ASC`

## Runtime Lifecycle
1. Claim next task
2. (Optional) periodic heartbeat
3. Finish by one of:
   - complete
   - fail
   - release

Claim and completion APIs validate `claim_token`.
`claim-next` performs stale-claim reconciliation before task selection.

## Endpoints

### 1) Claim Next
`POST /agents/claim-next`

Request:
```json
{
  "project_id": 1,
  "agent_id": "agent-1",
  "lease_seconds": 120,
  "capabilities": ["go", "backend", "db-migration"]
}
```

Response (claimed):
```json
{
  "task": {
    "id": 20,
    "project_id": 1,
    "parent_task_id": null,
    "title": "Planning",
    "spec_md": "...",
    "result_md": "",
    "status": "in_progress",
    "max_attempts": 5
  },
  "claim": {
    "id": 15,
    "token": "claim-token",
    "attempt_no": 1,
    "task_run_id": 9,
    "lease_seconds": 120
  }
}
```

Response (empty queue):
```json
{
  "task": null,
  "message": "no claimable task"
}
```

### 1a) Manual Reconcile (optional admin control)
`POST /projects/:project_id/claims/reconcile`

### 1b) Update Task Capability Filter (admin)
`PATCH /tasks/:task_id/capabilities`

### 2) Heartbeat
`POST /tasks/:task_id/heartbeat`

Request:
```json
{
  "agent_id": "agent-1",
  "claim_token": "claim-token",
  "lease_seconds": 120
}
```

### 3) Release
`POST /tasks/:task_id/release`

Request:
```json
{
  "agent_id": "agent-1",
  "claim_token": "claim-token",
  "to_status": "planned"
}
```

### 4) Complete
`POST /tasks/:task_id/complete`

Request:
```json
{
  "agent_id": "agent-1",
  "claim_token": "claim-token",
  "result_md": "## Work log ...",
  "result_payload": {
    "summary": "...",
    "changes": "...",
    "paths": "...",
    "commands": "...",
    "tests": "...",
    "artifacts": "...",
    "next_risks": "..."
  }
}
```

### 5) Fail
`POST /tasks/:task_id/fail`

Request:
```json
{
  "agent_id": "agent-1",
  "claim_token": "claim-token",
  "reason": "build failed",
  "result_md": "## Failure details ...",
  "result_payload": {
    "summary": "...",
    "changes": "...",
    "paths": "...",
    "commands": "...",
    "tests": "...",
    "artifacts": "...",
    "next_risks": "..."
  }
}
```

## Context / Observability APIs
- `GET /tasks/:task_id/context`
  - includes `dependencies`, `parent_chain`, `recent_runs`, `git_refs`
- `GET /tasks/:task_id/events?limit=30`
- `GET /projects/:project_id/events?limit=30`

## Delivery Semantics
- Current guarantee: `at-least-once`.
- Agent requirement:
  - make task execution idempotent when possible
  - verify current task state/context before destructive operations
  - include detailed run outputs in `result_md` + `result_payload`

## Error Handling Guide
- `409 active claim not found`: lease expired or token mismatch; re-claim.
- `409 all predecessor tasks must be done`: dependency/parent not ready.
- `400 result_payload.*`: required runtime output fields missing.
- `401/403`: token/scope mismatch.

## Reference Worker
- See `/scripts/agent_worker_demo.sh` for a minimal runtime flow example.
