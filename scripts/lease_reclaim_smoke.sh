#!/usr/bin/env bash
set -euo pipefail

# Verifies lease-expiry reclaim behavior:
# 1) agent-a claims a task with short lease
# 2) agent-b cannot claim while lease is active
# 3) after lease expiry, agent-b can claim the same task
#
# Usage:
#   ./scripts/lease_reclaim_smoke.sh
#
# Optional env:
#   API_BASE=http://localhost:8080
#   AUTH_TOKEN=<bearer token>

API_BASE="${API_BASE:-http://localhost:8080}"
timestamp="$(date +%s)"

if ! command -v jq >/dev/null 2>&1; then
  echo "[error] jq is required"
  exit 1
fi

curl_args=(-sS)
if [[ -n "${AUTH_TOKEN:-}" ]]; then
  curl_args+=(-H "Authorization: Bearer ${AUTH_TOKEN}")
fi

echo "[test] create project"
project_id="$(
  curl "${curl_args[@]}" -H "Content-Type: application/json" -X POST "${API_BASE}/projects" \
    -d "{\"name\":\"Lease Reclaim ${timestamp}\",\"description\":\"lease reclaim smoke\"}" \
  | jq -r '.id'
)"

echo "[test] create root task"
task_id="$(
  curl "${curl_args[@]}" -H "Content-Type: application/json" -X POST "${API_BASE}/projects/${project_id}/tasks" \
    -d '{"title":"LeaseReclaimTask","spec_md":"smoke"}' \
  | jq -r '.id'
)"

echo "[test] agent-a claim with lease=2s"
claim_a="$(
  curl "${curl_args[@]}" -H "Content-Type: application/json" -X POST "${API_BASE}/agents/claim-next" \
    -d "{\"project_id\":${project_id},\"agent_id\":\"agent-a\",\"lease_seconds\":2}"
)"
task_a="$(echo "${claim_a}" | jq -r '.task.id // empty')"
token_a="$(echo "${claim_a}" | jq -r '.claim.token // empty')"
if [[ "${task_a}" != "${task_id}" || -z "${token_a}" ]]; then
  echo "[fail] agent-a did not claim expected task"
  echo "${claim_a}" | jq .
  exit 1
fi

echo "[test] agent-b immediate claim should be empty"
claim_b_early="$(
  curl "${curl_args[@]}" -H "Content-Type: application/json" -X POST "${API_BASE}/agents/claim-next" \
    -d "{\"project_id\":${project_id},\"agent_id\":\"agent-b\",\"lease_seconds\":10}"
)"
if [[ "$(echo "${claim_b_early}" | jq -r '.task.id // empty')" != "" ]]; then
  echo "[fail] agent-b claimed while lease still active"
  echo "${claim_b_early}" | jq .
  exit 1
fi

echo "[test] wait lease expiry"
sleep 3

echo "[test] agent-b claim after expiry should reclaim automatically"
claim_b="$(
  curl "${curl_args[@]}" -H "Content-Type: application/json" -X POST "${API_BASE}/agents/claim-next" \
    -d "{\"project_id\":${project_id},\"agent_id\":\"agent-b\",\"lease_seconds\":30}"
)"
task_b="$(echo "${claim_b}" | jq -r '.task.id // empty')"
token_b="$(echo "${claim_b}" | jq -r '.claim.token // empty')"
attempt_b="$(echo "${claim_b}" | jq -r '.claim.attempt_no // empty')"
if [[ "${task_b}" != "${task_id}" || -z "${token_b}" ]]; then
  echo "[fail] agent-b did not reclaim expected task after lease expiry"
  echo "${claim_b}" | jq .
  exit 1
fi
if [[ "${attempt_b}" != "2" ]]; then
  echo "[fail] unexpected attempt number for reclaim (expected 2, got ${attempt_b})"
  echo "${claim_b}" | jq .
  exit 1
fi

echo "[test] complete reclaimed task"
payload="$(jq -nc \
  --arg s "lease reclaim smoke" \
  '{summary:$s,changes:[$s],paths:[$s],commands:[$s],tests:[$s],artifacts:[$s],next_risks:[$s]}'
)"
curl "${curl_args[@]}" -H "Content-Type: application/json" -X POST "${API_BASE}/tasks/${task_id}/complete" \
  -d "$(jq -nc --arg agent "agent-b" --arg token "${token_b}" --argjson payload "${payload}" \
      '{agent_id:$agent,claim_token:$token,result_md:"done",result_payload_version:"v2",result_payload:$payload}')" \
  | jq .

echo "[pass] lease reclaim smoke passed"
