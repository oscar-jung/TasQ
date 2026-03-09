#!/usr/bin/env bash
set -euo pipefail

# Minimal agent runtime demo:
# claim -> heartbeat -> complete|fail|release
#
# Usage:
#   PROJECT_ID=1 ./scripts/agent_worker_demo.sh
#   PROJECT_ID=1 ACTION=fail ./scripts/agent_worker_demo.sh
#   PROJECT_ID=1 ACTION=release ./scripts/agent_worker_demo.sh
#
# Optional env:
#   API_BASE=http://localhost:8080
#   AGENT_ID=agent-demo
#   LEASE_SECONDS=120
#   AUTH_TOKEN=<bearer token>
#   ACTION=complete|fail|release (default: complete)

API_BASE="${API_BASE:-http://localhost:8080}"
PROJECT_ID="${PROJECT_ID:-}"
AGENT_ID="${AGENT_ID:-agent-demo}"
LEASE_SECONDS="${LEASE_SECONDS:-120}"
ACTION="${ACTION:-complete}"

if [[ -z "${PROJECT_ID}" ]]; then
  echo "[error] PROJECT_ID is required"
  exit 1
fi

if ! command -v jq >/dev/null 2>&1; then
  echo "[error] jq is required"
  exit 1
fi

curl_args=(-sS)
if [[ -n "${AUTH_TOKEN:-}" ]]; then
  curl_args+=(-H "Authorization: Bearer ${AUTH_TOKEN}")
fi

echo "[info] claim-next: project=${PROJECT_ID} agent=${AGENT_ID}"
claim_res="$(
  curl "${curl_args[@]}" \
    -H "Content-Type: application/json" \
    -X POST "${API_BASE}/agents/claim-next" \
    -d "{\"project_id\":${PROJECT_ID},\"agent_id\":\"${AGENT_ID}\",\"lease_seconds\":${LEASE_SECONDS}}"
)"

task_id="$(echo "${claim_res}" | jq -r '.task.id // empty')"
claim_token="$(echo "${claim_res}" | jq -r '.claim.token // empty')"

if [[ -z "${task_id}" || -z "${claim_token}" ]]; then
  echo "[info] no claimable task"
  echo "${claim_res}" | jq .
  exit 0
fi

echo "[info] claimed task_id=${task_id}"

echo "[info] heartbeat"
curl "${curl_args[@]}" \
  -H "Content-Type: application/json" \
  -X POST "${API_BASE}/tasks/${task_id}/heartbeat" \
  -d "{\"agent_id\":\"${AGENT_ID}\",\"claim_token\":\"${claim_token}\",\"lease_seconds\":${LEASE_SECONDS}}" \
  | jq .

now="$(date -u +"%Y-%m-%dT%H:%M:%SZ")"
result_md=$(
  cat <<EOF
## Agent Runtime Report

- action: ${ACTION}
- executed_at: ${now}
- task_id: ${task_id}
- agent_id: ${AGENT_ID}
EOF
)

result_payload=$(
  jq -nc \
    --arg summary "Action ${ACTION} executed at ${now}" \
    --arg changes "See result_md report." \
    --arg paths "N/A" \
    --arg commands "agent_worker_demo.sh" \
    --arg tests "N/A" \
    --arg artifacts "N/A" \
    --arg next_risks "N/A" \
    '{summary:$summary,changes:$changes,paths:$paths,commands:$commands,tests:$tests,artifacts:$artifacts,next_risks:$next_risks}'
)

case "${ACTION}" in
  complete)
    echo "[info] complete task"
    curl "${curl_args[@]}" \
      -H "Content-Type: application/json" \
      -X POST "${API_BASE}/tasks/${task_id}/complete" \
      -d "$(jq -nc --arg agent_id "${AGENT_ID}" --arg claim_token "${claim_token}" --arg result_md "${result_md}" --argjson result_payload "${result_payload}" '{agent_id:$agent_id,claim_token:$claim_token,result_md:$result_md,result_payload:$result_payload}')" \
      | jq .
    ;;
  fail)
    echo "[info] fail task"
    curl "${curl_args[@]}" \
      -H "Content-Type: application/json" \
      -X POST "${API_BASE}/tasks/${task_id}/fail" \
      -d "$(jq -nc --arg agent_id "${AGENT_ID}" --arg claim_token "${claim_token}" --arg reason "demo failure" --arg result_md "${result_md}" --argjson result_payload "${result_payload}" '{agent_id:$agent_id,claim_token:$claim_token,reason:$reason,result_md:$result_md,result_payload:$result_payload}')" \
      | jq .
    ;;
  release)
    echo "[info] release task"
    curl "${curl_args[@]}" \
      -H "Content-Type: application/json" \
      -X POST "${API_BASE}/tasks/${task_id}/release" \
      -d "$(jq -nc --arg agent_id "${AGENT_ID}" --arg claim_token "${claim_token}" '{agent_id:$agent_id,claim_token:$claim_token,to_status:"planned"}')" \
      | jq .
    ;;
  *)
    echo "[error] invalid ACTION=${ACTION}, expected complete|fail|release"
    exit 1
    ;;
esac

echo "[info] task events"
curl "${curl_args[@]}" "${API_BASE}/tasks/${task_id}/events?limit=5" | jq .
