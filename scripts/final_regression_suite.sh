#!/usr/bin/env bash
set -euo pipefail

# Runs the current smoke/regression suite in sequence.
# Optional env passthrough:
#   API_BASE, AUTH_TOKEN

echo "[suite] claim_concurrency_smoke"
./scripts/claim_concurrency_smoke.sh

echo "[suite] dependency_cycle_race_smoke"
./scripts/dependency_cycle_race_smoke.sh

echo "[suite] topology_move_reorder_smoke"
./scripts/topology_move_reorder_smoke.sh

echo "[suite] lease_reclaim_smoke"
./scripts/lease_reclaim_smoke.sh

echo "[suite] capability_claim_smoke"
./scripts/capability_claim_smoke.sh

echo "[suite] agent_worker_demo (complete)"
PROJECT_ID="${PROJECT_ID:-1}" ACTION=complete ./scripts/agent_worker_demo.sh

echo "[suite] PASS"

