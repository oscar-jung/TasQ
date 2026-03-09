#!/usr/bin/env bash
set -euo pipefail

# Spawn and supervise Codex worker sessions for one TasQ project.
#
# Usage:
#   ./scripts/spawn_workers.sh --spec-file /abs/path/to/workers.json
#
# Spec file shape:
# {
#   "project_id": 2,
#   "workspace": "/abs/path/to/repo",
#   "api_base": "http://localhost:8080",
#   "lease_seconds": 300,
#   "heartbeat_seconds": 60,
#   "poll_seconds": 15,
#   "workers": [
#     {"agent_id": "worker-1", "capabilities": ["go","cli","docs"]},
#     {"agent_id": "worker-2", "capabilities": ["go","integration","testing","docs"]}
#   ]
# }
#
# Strategy:
# - spawn one Codex exec session per worker spec
# - poll TasQ runtime alerts until the queue is drained or blocked
# - stop immediately on exhausted tasks (max_attempts reached)

SPEC_FILE=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --spec-file)
      SPEC_FILE="${2:-}"
      shift 2
      ;;
    *)
      echo "[error] unknown argument: $1"
      exit 1
      ;;
  esac
done

if [[ -z "${SPEC_FILE}" ]]; then
  echo "[error] --spec-file is required"
  exit 1
fi

if ! command -v jq >/dev/null 2>&1; then
  echo "[error] jq is required"
  exit 1
fi

if ! command -v codex >/dev/null 2>&1; then
  echo "[error] codex is required"
  exit 1
fi

if [[ ! -f "${SPEC_FILE}" ]]; then
  echo "[error] spec file not found: ${SPEC_FILE}"
  exit 1
fi

PROJECT_ID="$(jq -r '.project_id // empty' "${SPEC_FILE}")"
WORKSPACE="$(jq -r '.workspace // empty' "${SPEC_FILE}")"
API_BASE="$(jq -r '.api_base // "http://localhost:8080"' "${SPEC_FILE}")"
LEASE_SECONDS="$(jq -r '.lease_seconds // 300' "${SPEC_FILE}")"
HEARTBEAT_SECONDS="$(jq -r '.heartbeat_seconds // 60' "${SPEC_FILE}")"
POLL_SECONDS="$(jq -r '.poll_seconds // 15' "${SPEC_FILE}")"
LOG_DIR="$(jq -r '.log_dir // empty' "${SPEC_FILE}")"

if [[ -z "${PROJECT_ID}" || "${PROJECT_ID}" == "null" ]]; then
  echo "[error] project_id is required in spec file"
  exit 1
fi

if [[ -z "${WORKSPACE}" || "${WORKSPACE}" == "null" ]]; then
  echo "[error] workspace is required in spec file"
  exit 1
fi

if [[ ! -d "${WORKSPACE}" ]]; then
  echo "[error] workspace does not exist: ${WORKSPACE}"
  exit 1
fi

WORKER_COUNT="$(jq '.workers | length' "${SPEC_FILE}")"
if [[ "${WORKER_COUNT}" -eq 0 ]]; then
  echo "[error] at least one worker entry is required"
  exit 1
fi

if [[ -z "${LOG_DIR}" || "${LOG_DIR}" == "null" ]]; then
  LOG_DIR="/tmp/tasq-spawn-${PROJECT_ID}-$(date +%Y%m%d%H%M%S)"
fi
mkdir -p "${LOG_DIR}"

PIDS=()
WORKER_LABELS=()

print_runtime_summary() {
  local runtime_json="$1"
  local total done unfinished claimable blocked exhausted
  total="$(echo "${runtime_json}" | jq -r '.total_tasks')"
  done="$(echo "${runtime_json}" | jq -r '.done_tasks')"
  unfinished="$(echo "${runtime_json}" | jq -r '.unfinished_tasks')"
  claimable="$(echo "${runtime_json}" | jq -r '.claimable_tasks')"
  blocked="$(echo "${runtime_json}" | jq -r '.blocked_planned_tasks')"
  exhausted="$(echo "${runtime_json}" | jq -r '.exhausted_tasks | length')"
  echo "[runtime] total=${total} done=${done} unfinished=${unfinished} claimable=${claimable} blocked=${blocked} exhausted=${exhausted}"
}

fetch_runtime() {
  local attempt output
  for attempt in 1 2 3; do
    if output="$(curl -sS "${API_BASE}/projects/${PROJECT_ID}/runtime-alerts?heartbeat_stale_seconds=${HEARTBEAT_SECONDS}" 2>/dev/null)"; then
      echo "${output}" | jq -c .
      return 0
    fi
    sleep 1
  done
  return 1
}

kill_workers() {
  local pid
  for pid in "${PIDS[@]:-}"; do
    if kill -0 "${pid}" >/dev/null 2>&1; then
      kill "${pid}" >/dev/null 2>&1 || true
    fi
  done
}

cleanup() {
  kill_workers
}
trap cleanup EXIT INT TERM

worker_prompt() {
  local agent_id="$1"
  local capabilities_json="$2"
  local capabilities_csv
  capabilities_csv="$(echo "${capabilities_json}" | jq -r 'join(", ")')"
  cat <<EOF
You are a TasQ worker agent executing tasks from queue through MCP.

Use:
- project_id=${PROJECT_ID}
- agent_id=${agent_id}
- capabilities=${capabilities_json}
- lease_seconds=${LEASE_SECONDS}
- heartbeat_seconds=${HEARTBEAT_SECONDS}

Rules:
- project_id must remain numeric.
- Use TasQ MCP tools only for queue lifecycle: claim, context, heartbeat, complete, fail, git ref.
- Immediately fetch task context after every claim.
- If execution or tests may take longer than ${HEARTBEAT_SECONDS} seconds, send tasq_heartbeat before the lease goes stale.
- If this session restarts after interruption, do not assume the prior claim is still valid. Claim fresh work from TasQ.
- If claim returns no task, exit cleanly.
- Keep the result payload factual and concise.
- Required capabilities for this worker: ${capabilities_csv}

Loop:
1) Call tasq_claim_next with project_id=${PROJECT_ID}, agent_id=${agent_id}, lease_seconds=${LEASE_SECONDS}, capabilities=${capabilities_json}.
2) If no task is returned, stop.
3) Call tasq_get_task_context for the claimed task.
4) Implement exactly what the task spec requires in the current workspace.
5) Run verification or tests that match the task scope.
6) Send tasq_heartbeat periodically while work is in progress.
7) If success, call tasq_complete_task with result_md and result_payload_version="v2".
8) If failure, call tasq_fail_task with reason and result_payload_version="v2".
9) If a git commit was created, call tasq_link_git_ref.
10) Repeat until no claimable task remains.

Result payload v2 must include:
- summary
- changes
- paths
- commands
- tests
- artifacts
- next_risks
EOF
}

spawn_worker() {
  local index="$1"
  local agent_id capabilities_json prompt log_file pid
  agent_id="$(jq -r ".workers[${index}].agent_id" "${SPEC_FILE}")"
  capabilities_json="$(jq -c ".workers[${index}].capabilities" "${SPEC_FILE}")"

  if [[ -z "${agent_id}" || "${agent_id}" == "null" ]]; then
    echo "[error] workers[${index}].agent_id is required"
    exit 1
  fi

  log_file="${LOG_DIR}/${agent_id}.log"
  prompt="$(worker_prompt "${agent_id}" "${capabilities_json}")"

  echo "[spawn] ${agent_id} -> ${log_file}"
  codex exec --full-auto --ephemeral -C "${WORKSPACE}" "${prompt}" >"${log_file}" 2>&1 &
  pid=$!
  PIDS+=("${pid}")
  WORKER_LABELS+=("${agent_id}")
}

all_workers_exited() {
  local pid
  for pid in "${PIDS[@]:-}"; do
    if kill -0 "${pid}" >/dev/null 2>&1; then
      return 1
    fi
  done
  return 0
}

for ((i = 0; i < WORKER_COUNT; i += 1)); do
  spawn_worker "${i}"
done

idle_cycles=0
api_failures=0
while true; do
  sleep "${POLL_SECONDS}"
  if ! runtime_json="$(fetch_runtime)"; then
    api_failures=$((api_failures + 1))
    if [[ "${api_failures}" -ge 5 ]]; then
      echo "[stop] TasQ API is unreachable after repeated retries"
      exit 4
    fi
    echo "[warn] TasQ API temporarily unreachable; retrying"
    continue
  fi
  api_failures=0
  print_runtime_summary "${runtime_json}"

  exhausted_count="$(echo "${runtime_json}" | jq -r '.exhausted_tasks | length')"
  unfinished_count="$(echo "${runtime_json}" | jq -r '.unfinished_tasks')"
  claimable_count="$(echo "${runtime_json}" | jq -r '.claimable_tasks')"
  stale_count="$(echo "${runtime_json}" | jq -r '.stale_active_claims | length')"
  orphan_count="$(echo "${runtime_json}" | jq -r '.orphan_in_progress_tasks | length')"

  if [[ "${exhausted_count}" -gt 0 ]]; then
    echo "[stop] max_attempts exhausted. Human intervention required."
    echo "${runtime_json}" | jq -r '.exhausted_tasks[] | "- T#\(.task_id) \(.title) (\(.attempts_used)/\(.max_attempts))"'
    exit 2
  fi

  if [[ "${unfinished_count}" -eq 0 ]]; then
    echo "[done] project queue drained"
    exit 0
  fi

  if all_workers_exited; then
    if [[ "${claimable_count}" -gt 0 || "${stale_count}" -gt 0 || "${orphan_count}" -gt 0 ]]; then
      echo "[respawn] workers exited while tasks remain; restarting worker set"
      PIDS=()
      WORKER_LABELS=()
      for ((i = 0; i < WORKER_COUNT; i += 1)); do
        spawn_worker "${i}"
      done
      idle_cycles=0
      continue
    fi

    idle_cycles=$((idle_cycles + 1))
    if [[ "${idle_cycles}" -ge 3 ]]; then
      echo "[stop] unfinished tasks remain but no worker is alive and nothing is claimable."
      echo "[hint] check capability routing, dependency blockage, or manual intervention requirements."
      echo "${runtime_json}" | jq .
      exit 3
    fi
  else
    idle_cycles=0
  fi
done
