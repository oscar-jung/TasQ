# TasQ Operations Guide (Draft)

## Runtime
- Recommended: Docker Compose (`db`, `migrate`, `api`, `web`)
- Health check:
  - API: `GET /healthz`
  - DB: `pg_isready`

## Startup / Rebuild
```bash
docker compose up -d --build
docker compose ps
docker compose logs --tail=100 api web db
```

Runtime knobs:
- `RECONCILE_INTERVAL_SECONDS` (default `30`, set `0` to disable)
- `HEARTBEAT_STALE_SECONDS` (default `300`)

## Backup / Restore (PostgreSQL)
### Backup
```bash
docker compose exec -T db pg_dump -U tasq -d tasq > tasq_backup.sql
```

### Restore
```bash
cat tasq_backup.sql | docker compose exec -T db psql -U tasq -d tasq
```

## Common Incident Playbook
### 1) UI blank/crash
1. Check browser console.
2. Check `docker compose logs --tail=200 web`.
3. Rebuild web: `docker compose up -d --build web`.

### Realtime notes
- UI realtime is hybrid:
  - SSE stream: `/projects/:id/stream`
  - polling fallback/companion: runtime alerts + periodic validation
- SSE emits:
  - `task-event`: DB-backed task lifecycle/audit events
  - `project-signal`: in-memory broker signals for deletion-oriented updates
- If realtime looks stale but manual refresh works:
  1. verify `/projects/:id/stream` stays connected
  2. check browser network tab for SSE reconnect loops
  3. confirm API logs do not show restarts
  4. rely on polling as fallback while debugging

### 2) Claim stuck / no claimable task
1. Verify task eligibility:
   - parent done
   - dependencies done
   - status planned
2. Check lease state in `task_claims` (active + lease_until).
3. `claim-next` automatically reconciles expired claims for the target project.
4. If needed, force reconcile with `POST /projects/:project_id/claims/reconcile`.
5. Use event timeline APIs:
   - `/tasks/:id/events`
   - `/projects/:id/events`
6. Check runtime alerts:
   - `/projects/:id/runtime-alerts`
7. If `runtime-alerts.exhausted_tasks` is non-empty:
   - the queue will not assign those tasks again
   - treat this as human-action-required, not an auto-retry case
   - fix the task definition, underlying implementation issue, or raise `max_attempts`
8. If a task repeatedly returns to `planned` after running:
   - inspect `GET /tasks/:id/context`
   - check `interrupted_runs` for `reason` and `resume_hint`
   - common reasons:
     - `manual_release`
     - `lease_expired_reconcile`
     - `manual_invalidate`
   - reclaim only after reviewing the existing on-disk progress and latest task result/spec
9. If UI does not reflect status/content changes immediately:
   - verify the corresponding `task-event` type is emitted:
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
     - `task.checkpoint.saved`
     - `task.claim.released`
      - `task.invalidated`
      - `task.completed`
      - `task.failed`
      - `task.reconciled.stale_claim`
   - deletions and explicit reruns use `project-signal` (`task.deleted`, `task.invalidated`, `project.deleted`)

### 2b) Explicit rerun / invalidation
- Use `POST /tasks/:id/invalidate` when upstream specs/results changed and downstream work should be re-executed.
- Scope guidance:
  - `subtree`: rerun the selected task and its child tree
  - `downstream`: rerun dependency successors while keeping the selected task state
  - `both`: rerun the selected task, its tree, and downstream dependency chain
- Prefer branch-first Git recovery:
  - keep old branch history intact
  - create a new branch for the rerun path
  - link new git refs back into TasQ per task
  - use `ref_kind=baseline` for the commit you branched from
  - use `ref_kind=rerun_branch` for the branch marker/ref you want operators to resume on
  - use `ref_kind=produced` for the commit created by the rerun

### 3) Reclaim behavior check
- Run smoke:
```bash
./scripts/lease_reclaim_smoke.sh
```

### 4) Mid-run checkpointing
- Long-running agents should save checkpoint notes during meaningful progress, not just heartbeat.
- Use `POST /tasks/:id/checkpoint` with the active `claim_token`.
- Good checkpoint notes should capture:
  - current file/module being changed
  - what is already complete
  - what remains risky or unfinished
- If a worker is interrupted, the next worker should inspect `task context -> interrupted_runs` before continuing.

## Recommended Metrics (next)
- queue depth (claimable planned tasks)
- claim latency (claim -> complete)
- failure rate by project / agent
- stale active claims

## MCP Runtime Smoke
Validate MCP tool routing without launching external agent sessions:

```bash
./scripts/mcp_runtime_smoke.sh
```

This covers:
- claim -> context -> heartbeat -> complete path
- fail path
- runtime alerts read path (including admin-token fallback)
