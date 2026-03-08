#!/usr/bin/env bash
set -euo pipefail

API_BASE="${API_BASE:-http://localhost:8080}"

project_resp="$(curl -sS -X POST "${API_BASE}/projects" -H 'Content-Type: application/json' -d '{"name":"smoke-topology","description":"move reorder"}')"
project_id="$(printf '%s' "$project_resp" | sed -n 's/.*"id":\([0-9][0-9]*\).*/\1/p' | head -n1)"

root_resp="$(curl -sS -X POST "${API_BASE}/projects/${project_id}/tasks" -H 'Content-Type: application/json' -d '{"title":"Root","spec_md":"root"}')"
root_id="$(printf '%s' "$root_resp" | sed -n 's/.*"id":\([0-9][0-9]*\).*/\1/p' | head -n1)"

child_resp="$(curl -sS -X POST "${API_BASE}/projects/${project_id}/tasks" -H 'Content-Type: application/json' -d "{\"title\":\"Child\",\"spec_md\":\"child\",\"parent_task_id\":${root_id}}")"
child_id="$(printf '%s' "$child_resp" | sed -n 's/.*"id":\([0-9][0-9]*\).*/\1/p' | head -n1)"

grand_resp="$(curl -sS -X POST "${API_BASE}/projects/${project_id}/tasks" -H 'Content-Type: application/json' -d "{\"title\":\"Grand\",\"spec_md\":\"grand\",\"parent_task_id\":${child_id}}")"
grand_id="$(printf '%s' "$grand_resp" | sed -n 's/.*"id":\([0-9][0-9]*\).*/\1/p' | head -n1)"

invalid_code="$(curl -sS -o /tmp/tasq-move-invalid.out -w "%{http_code}" -X POST "${API_BASE}/tasks/${root_id}/move" -H 'Content-Type: application/json' -d "{\"new_parent_task_id\":${grand_id}}")"
if [[ "${invalid_code}" != "400" ]]; then
  echo "[FAIL] expected subtree-guard 400, got ${invalid_code}"
  cat /tmp/tasq-move-invalid.out
  exit 1
fi

sibling_resp="$(curl -sS -X POST "${API_BASE}/projects/${project_id}/tasks" -H 'Content-Type: application/json' -d '{"title":"Sibling","spec_md":"sib"}')"
sibling_id="$(printf '%s' "$sibling_resp" | sed -n 's/.*"id":\([0-9][0-9]*\).*/\1/p' | head -n1)"

move_code="$(curl -sS -o /tmp/tasq-move-ok.out -w "%{http_code}" -X POST "${API_BASE}/tasks/${child_id}/move" -H 'Content-Type: application/json' -d '{"new_parent_task_id":null,"new_index":0}')"
if [[ "${move_code}" != "200" ]]; then
  echo "[FAIL] move failed with ${move_code}"
  cat /tmp/tasq-move-ok.out
  exit 1
fi

reorder_code="$(curl -sS -o /tmp/tasq-reorder.out -w "%{http_code}" -X POST "${API_BASE}/tasks/reorder" -H 'Content-Type: application/json' -d "{\"project_id\":${project_id},\"parent_task_id\":null,\"ordered_task_ids\":[${sibling_id},${child_id},${root_id}]}")"
if [[ "${reorder_code}" != "200" ]]; then
  echo "[FAIL] reorder failed with ${reorder_code}"
  cat /tmp/tasq-reorder.out
  exit 1
fi

tasks_json="$(curl -sS "${API_BASE}/projects/${project_id}/tasks")"
order="$(printf '%s' "$tasks_json" | tr '{' '\n' | grep '"parent_task_id":null' | sed -n 's/.*"id":\([0-9][0-9]*\).*/\1/p' | paste -sd ',' -)"

expected="${sibling_id},${child_id},${root_id}"
if [[ "${order}" != "${expected}" ]]; then
  echo "[FAIL] expected root order ${expected}, got ${order}"
  echo "$tasks_json"
  exit 1
fi

echo "[PASS] topology move/reorder check"
echo "project_id=${project_id} root_order=${order}"
