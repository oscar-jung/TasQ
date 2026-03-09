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
3. Note: expired lease does not automatically reset task status from `in_progress` to `planned`.
4. Recover by setting task status to `planned` (admin/manual flow), then re-claim.
5. Use event timeline APIs:
   - `/tasks/:id/events`
   - `/projects/:id/events`

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
