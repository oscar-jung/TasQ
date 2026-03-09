# TasQ Release Checklist (Agent-Support Branch)

## 1) Build and Runtime
- [ ] `go test ./...` passes in `tasq-backend`
- [ ] `npm run build` passes in `tasq-app`
- [ ] `docker compose up -d --build` succeeds
- [ ] `GET /healthz` returns `ok`

## 2) Core Functional Flows
- [ ] Project/task create/edit/delete flows work in UI
- [ ] Tree move/promote/delete strategies work as expected
- [ ] Task status constraints are enforced (parent/dependency rules)
- [ ] Tree validation and cleanse behavior is correct

## 3) Agent Runtime
- [ ] Claim lifecycle works end-to-end:
  - claim -> heartbeat -> complete/fail/release
- [ ] `result_payload` validation uses schema `v2` by default
- [ ] capability-based claim filter works
- [ ] stale-claim auto reconcile works (lease expiry recovery)

## 4) Observability and Operations
- [ ] Task/project event timelines load in UI
- [ ] Runtime alerts API returns expected fields
- [ ] Project list badges reflect runtime alert counts
- [ ] Operations guide reflects current behavior/knobs

## 5) Security and Access
- [ ] AUTH required mode validated with representative human/agent tokens
- [ ] scope checks verified for read/claim/update/complete/admin

## 6) Regression Suite
Run:
```bash
./scripts/final_regression_suite.sh
```

Expected:
- all smoke scripts complete with `[pass]` and non-zero exits are absent.

