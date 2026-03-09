#!/usr/bin/env bash
set -euo pipefail

# Verifies capability-based claim filtering.
#
# Usage:
#   ./scripts/capability_claim_smoke.sh
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
    -d "{\"name\":\"Capability Claim ${timestamp}\",\"description\":\"capability claim smoke\"}" \
  | jq -r '.id'
)"

echo "[test] create task with required capabilities"
task_id="$(
  curl "${curl_args[@]}" -H "Content-Type: application/json" -X POST "${API_BASE}/projects/${project_id}/tasks" \
    -d '{"title":"CapabilityTask","spec_md":"smoke","required_capabilities":["go","backend"]}' \
  | jq -r '.id'
)"

echo "[test] claim without capabilities should be empty"
claim_none="$(
  curl "${curl_args[@]}" -H "Content-Type: application/json" -X POST "${API_BASE}/agents/claim-next" \
    -d "{\"project_id\":${project_id},\"agent_id\":\"agent-none\",\"lease_seconds\":30}"
)"
if [[ "$(echo "${claim_none}" | jq -r '.task.id // empty')" != "" ]]; then
  echo "[fail] claim should be empty without capabilities"
  echo "${claim_none}" | jq .
  exit 1
fi

echo "[test] claim with partial capabilities should be empty"
claim_partial="$(
  curl "${curl_args[@]}" -H "Content-Type: application/json" -X POST "${API_BASE}/agents/claim-next" \
    -d "{\"project_id\":${project_id},\"agent_id\":\"agent-partial\",\"lease_seconds\":30,\"capabilities\":[\"go\"]}"
)"
if [[ "$(echo "${claim_partial}" | jq -r '.task.id // empty')" != "" ]]; then
  echo "[fail] claim should be empty with partial capabilities"
  echo "${claim_partial}" | jq .
  exit 1
fi

echo "[test] claim with full capabilities should succeed"
claim_full="$(
  curl "${curl_args[@]}" -H "Content-Type: application/json" -X POST "${API_BASE}/agents/claim-next" \
    -d "{\"project_id\":${project_id},\"agent_id\":\"agent-full\",\"lease_seconds\":30,\"capabilities\":[\"go\",\"backend\",\"extra\"]}"
)"
claimed_task="$(echo "${claim_full}" | jq -r '.task.id // empty')"
claim_token="$(echo "${claim_full}" | jq -r '.claim.token // empty')"
if [[ "${claimed_task}" != "${task_id}" || -z "${claim_token}" ]]; then
  echo "[fail] claim with full capabilities did not return expected task"
  echo "${claim_full}" | jq .
  exit 1
fi

echo "[test] complete claimed task"
payload="$(jq -nc \
  --arg s "capability claim smoke" \
  '{summary:$s,changes:$s,paths:$s,commands:$s,tests:$s,artifacts:$s,next_risks:$s}'
)"
curl "${curl_args[@]}" -H "Content-Type: application/json" -X POST "${API_BASE}/tasks/${task_id}/complete" \
  -d "$(jq -nc --arg agent "agent-full" --arg token "${claim_token}" --argjson payload "${payload}" \
      '{agent_id:$agent,claim_token:$token,result_md:"done",result_payload:$payload}')" \
  | jq .

echo "[pass] capability claim smoke passed"

