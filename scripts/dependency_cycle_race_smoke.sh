#!/usr/bin/env bash
set -euo pipefail

API_BASE="${API_BASE:-http://localhost:8080}"

project_resp="$(curl -sS -X POST "${API_BASE}/projects" -H 'Content-Type: application/json' -d '{"name":"smoke-dep-race","description":"dependency race"}')"
project_id="$(printf '%s' "$project_resp" | sed -n 's/.*"id":\([0-9][0-9]*\).*/\1/p' | head -n1)"

if [[ -z "${project_id}" ]]; then
  echo "[FAIL] create project failed: ${project_resp}"
  exit 1
fi

a_resp="$(curl -sS -X POST "${API_BASE}/projects/${project_id}/tasks" -H 'Content-Type: application/json' -d '{"title":"A","spec_md":"a"}')"
b_resp="$(curl -sS -X POST "${API_BASE}/projects/${project_id}/tasks" -H 'Content-Type: application/json' -d '{"title":"B","spec_md":"b"}')"
a_id="$(printf '%s' "$a_resp" | sed -n 's/.*"id":\([0-9][0-9]*\).*/\1/p' | head -n1)"
b_id="$(printf '%s' "$b_resp" | sed -n 's/.*"id":\([0-9][0-9]*\).*/\1/p' | head -n1)"

if [[ -z "${a_id}" || -z "${b_id}" ]]; then
  echo "[FAIL] create tasks failed"
  exit 1
fi

tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT

(
  curl -sS -o "${tmp_dir}/ab.body" -w "%{http_code}" \
    -X POST "${API_BASE}/tasks/${b_id}/dependencies" \
    -H 'Content-Type: application/json' \
    -d "{\"predecessor_task_id\":${a_id}}" >"${tmp_dir}/ab.code"
) &
(
  curl -sS -o "${tmp_dir}/ba.body" -w "%{http_code}" \
    -X POST "${API_BASE}/tasks/${a_id}/dependencies" \
    -H 'Content-Type: application/json' \
    -d "{\"predecessor_task_id\":${b_id}}" >"${tmp_dir}/ba.code"
) &
wait

ab_code="$(cat "${tmp_dir}/ab.code")"
ba_code="$(cat "${tmp_dir}/ba.code")"

successes=0
[[ "${ab_code}" == "201" ]] && successes=$((successes + 1))
[[ "${ba_code}" == "201" ]] && successes=$((successes + 1))

if [[ "${successes}" -ne 1 ]]; then
  echo "[FAIL] expected exactly one success in race, got ${successes} (ab=${ab_code}, ba=${ba_code})"
  echo "ab.body=$(cat "${tmp_dir}/ab.body")"
  echo "ba.body=$(cat "${tmp_dir}/ba.body")"
  exit 1
fi

a_deps="$(curl -sS "${API_BASE}/tasks/${a_id}/dependencies")"
b_deps="$(curl -sS "${API_BASE}/tasks/${b_id}/dependencies")"
predecessor_mentions="$(printf '%s\n%s' "$a_deps" "$b_deps" | grep -o '"direction":"predecessor"' | wc -l | tr -d ' ')"

if [[ "${predecessor_mentions}" -ne 1 ]]; then
  echo "[FAIL] expected exactly one predecessor edge after race, got ${predecessor_mentions}"
  echo "a_deps=${a_deps}"
  echo "b_deps=${b_deps}"
  exit 1
fi

echo "[PASS] dependency race check"
echo "project_id=${project_id} ab_code=${ab_code} ba_code=${ba_code} predecessor_edges=${predecessor_mentions}"
