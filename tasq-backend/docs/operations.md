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

### 3) Reclaim behavior check
- Run smoke:
```bash
./scripts/lease_reclaim_smoke.sh
```

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
