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

## Realtime updates
TasQ uses a hybrid approach:
- SSE for event-driven UI refresh
- polling for time-based runtime alerts such as stale leases and overdue heartbeats

SSE endpoint:
- `GET /projects/:project_id/stream`

SSE event types:
- `task-event`
  - payload is a task event row
  - currently used for:
    - `task.created`
    - `task.content.updated`
    - `task.execution_policy.updated`
    - `task.capabilities.updated`
    - `task.status.updated`
    - `task.git.linked`
    - `task.reordered`
    - `task.moved`
    - `task.claimed`
    - `task.claim.heartbeat`
    - `task.claim.released`
    - `task.completed`
    - `task.failed`
    - `task.reconciled.stale_claim`
- `project-signal`
  - lightweight broker signal for cases that are awkward to keep in `task_events`
  - currently used for:
    - `task.deleted`
    - `project.deleted`

UI refresh behavior:
- tree refresh:
  - `task.created`, `task.moved`, `task.reordered`, `task.status.updated`, `task.completed`, `task.failed`, `task.claimed`, `task.claim.released`, `task.reconciled.stale_claim`, `task.deleted`
- selected task detail / observability refresh:
  - `task.content.updated`, `task.execution_policy.updated`, `task.capabilities.updated`, `task.git.linked`, `task.status.updated`, `task.completed`, `task.failed`, `task.claimed`, `task.claim.heartbeat`, `task.claim.released`, `task.reconciled.stale_claim`
- runtime alerts refresh:
  - claim / heartbeat / completion / failure / reconcile / delete flows

Design note:
- deletion uses `project-signal` because `task_events` rows cascade with the deleted task and are not a reliable transport for post-delete UI updates

## Worker supervisor
Planner sessions should emit a worker spawn spec JSON file and then launch:

```bash
cp ./examples/workers.sample.json /abs/path/to/workers.json
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

## Copy-paste prompts you can try
### 1) Planner prompt
Use this in a fresh Codex CLI session after `tasq-http` MCP is registered:

- Template file: [templates/planner_prompt_tasq.txt](/Users/jung-yeon-woo/Development/Projects/tasq/templates/planner_prompt_tasq.txt)
- Worker spec sample: [examples/workers.sample.json](/Users/jung-yeon-woo/Development/Projects/tasq/examples/workers.sample.json)
- Worker spec schema: [schemas/workers.schema.json](/Users/jung-yeon-woo/Development/Projects/tasq/schemas/workers.schema.json)

```text
You are a planning agent working with TasQ via MCP tools.

Create a project and actionable task tree for:
"Go-based YouTube Downloader CLI using Cobra, supporting video/audio download and quality options."

Rules:
- Use TasQ MCP tools to create one project, root tasks, child tasks, and dependency edges.
- Keep tasks small enough for one focused worker run.
- Use required_capabilities sparingly.
- Keep at least one root task immediately claimable by a generic worker.
- Prefer vertical decomposition over wide sibling trees when tasks will touch the same files or modules.
- Keep root tasks few and meaningful; parallelize only when the work is clearly merge-safe.
- Prefer generic capability labels: go, cli, integration, testing, docs.
- Do not implement code in this session.
- At the end, report the numeric project_id.
- Also create a workers.json file in the current workspace with this shape:
  {
    "project_id": <numeric id>,
    "workspace": "<absolute workspace path>",
    "api_base": "http://127.0.0.1:8080",
    "lease_seconds": 300,
    "heartbeat_seconds": 60,
    "poll_seconds": 15,
    "workers": [
      {"agent_id": "worker-1", "capabilities": ["go","cli","docs"]},
      {"agent_id": "worker-2", "capabilities": ["go","integration","testing","docs"]}
    ]
  }

At the end, print:
- project_id
- concise tree summary
- dependency summary
- path to workers.json
```

### 2) Start workers automatically
```bash
cp ./examples/workers.sample.json /abs/path/to/workers.json
# edit project_id and workspace in workers.json first
./scripts/spawn_workers.sh --spec-file /abs/path/to/workers.json
```

### 3) Manual worker prompt
If you want to run one worker manually instead of the spawner:

```text
You are a TasQ worker agent executing tasks from queue through MCP.

Use:
- project_id=<numeric project id>
- agent_id=worker-1
- capabilities=["go","cli","integration","testing","docs"]
- lease_seconds=300
- heartbeat_seconds=60

Rules:
- project_id must remain numeric.
- Claim only through TasQ MCP tools.
- Immediately fetch task context after claim.
- If work may take longer than 60 seconds, send tasq_heartbeat before lease expiry.
- Save a checkpoint note when you finish a meaningful sub-step or before risky edits/tests.
- If the task context reports `interrupted_runs`, read the latest interruption reason and resume hint before editing.
- If the task context reports `effective_git_policy="required"`, link a `baseline` git ref before editing and do not complete until a new `produced` ref exists for the current attempt.
- If claim returns no task, stop cleanly.
- Finish with tasq_complete_task or tasq_fail_task.
- Include result_payload_version="v2" with summary, changes, paths, commands, tests, artifacts, next_risks.
- `result_md` must be handoff quality and include:
  - `## Summary`
  - `## Changes`
  - `## Verification`
  - `## Risks`

For reruns, prefer a branch-first workflow:
- create a fresh branch for the rerun path
- continue implementation there
- link the branch/base commit/produced commit back to the task
- mark linked refs as:
  - `baseline`
  - `rerun_branch`
  - `produced`
```

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
