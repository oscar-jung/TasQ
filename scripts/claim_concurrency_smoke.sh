#!/usr/bin/env bash
set -euo pipefail

API_BASE="${API_BASE:-http://localhost:8080}"
PARALLEL="${PARALLEL:-8}"

create_project_resp="$(curl -sS -X POST "${API_BASE}/projects" -H 'Content-Type: application/json' -d '{"name":"smoke-queue","description":"claim concurrency smoke"}')"
project_id="$(printf '%s' "$create_project_resp" | sed -n 's/.*"id":\([0-9][0-9]*\).*/\1/p' | head -n1)"

if [[ -z "${project_id}" ]]; then
  echo "[FAIL] failed to create project: ${create_project_resp}"
  exit 1
fi

create_task_resp="$(curl -sS -X POST "${API_BASE}/projects/${project_id}/tasks" -H 'Content-Type: application/json' -d '{"title":"only-task","spec_md":"smoke"}')"
task_id="$(printf '%s' "$create_task_resp" | sed -n 's/.*"id":\([0-9][0-9]*\).*/\1/p' | head -n1)"

if [[ -z "${task_id}" ]]; then
  echo "[FAIL] failed to create task: ${create_task_resp}"
  exit 1
fi

tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT

for i in $(seq 1 "$PARALLEL"); do
  (
    curl -sS -X POST "${API_BASE}/agents/claim-next" \
      -H 'Content-Type: application/json' \
      -d "{\"project_id\":${project_id},\"agent_id\":\"agent-${i}\",\"lease_seconds\":300}" \
      >"${tmp_dir}/claim-${i}.json"
  ) &
done
wait

claimed_count="$(grep -l '"task":{"id":' "${tmp_dir}"/claim-*.json | wc -l | tr -d ' ')"
null_count="$(grep -l '"task":null' "${tmp_dir}"/claim-*.json | wc -l | tr -d ' ')"

if [[ "${claimed_count}" != "1" ]]; then
  echo "[FAIL] expected exactly 1 successful claim, got ${claimed_count}"
  echo "--- claim responses ---"
  sed 's/^/  /' "${tmp_dir}"/claim-*.json
  exit 1
fi

echo "[PASS] concurrency claim check"
echo "project_id=${project_id} task_id=${task_id} successful_claims=${claimed_count} null_claims=${null_count}"
