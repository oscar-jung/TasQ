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
WORKER_SPECS_JSON="$(jq -c '.workers' "${SPEC_FILE}")"

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

worker_can_cover_caps() {
  local required_caps_json="$1"
  echo "${WORKER_SPECS_JSON}" | jq -e --argjson required "${required_caps_json}" '
    any(.[]; (($required | length) == 0) or ($required - (.capabilities // [])) | length == 0)
  ' >/dev/null
}

diagnose_blockage() {
  local tasks_json
  if ! tasks_json="$(curl -sS "${API_BASE}/projects/${PROJECT_ID}/tasks" 2>/dev/null)"; then
    echo "[diag] unable to fetch project tasks for blockage diagnosis"
    return
  fi

  echo "[diag] blockage diagnosis for unfinished tasks:"
  echo "${tasks_json}" | jq -r '.[] | select(.status != "done") | "- T#\(.id) \(.title) [\(.status)]"' | sed -n '1,20p'

  while IFS= read -r task_id; do
    [[ -z "${task_id}" ]] && continue

    local task_json task_title task_status parent_id parent_status caps_json deps_json parent_blockers dep_blockers attempts_used max_attempts
    task_json="$(echo "${tasks_json}" | jq -c --argjson id "${task_id}" '.[] | select(.id == $id)')"
    task_title="$(echo "${task_json}" | jq -r '.title')"
    task_status="$(echo "${task_json}" | jq -r '.status')"
    parent_id="$(echo "${task_json}" | jq -r '.parent_task_id // empty')"
    max_attempts="$(echo "${task_json}" | jq -r '.max_attempts')"
    attempts_used="$(curl -sS "${API_BASE}/projects/${PROJECT_ID}/runtime-alerts?heartbeat_stale_seconds=${HEARTBEAT_SECONDS}" 2>/dev/null | jq -r --argjson id "${task_id}" '([.exhausted_tasks[] | select(.task_id == $id) | .attempts_used][0] // 0)')"
    caps_json="$(curl -sS "${API_BASE}/tasks/${task_id}/capabilities" 2>/dev/null | jq -c '.required_capabilities // []')"
    deps_json="$(curl -sS "${API_BASE}/tasks/${task_id}/dependencies" 2>/dev/null | jq -c '[.[] | select(.direction == "predecessor")]')"

    parent_blockers=""
    if [[ -n "${parent_id}" ]]; then
      parent_status="$(echo "${tasks_json}" | jq -r --argjson id "${parent_id}" '.[] | select(.id == $id) | .status')"
      if [[ "${parent_status}" != "done" ]]; then
        parent_blockers="parent T#${parent_id} is ${parent_status}"
      fi
    fi

    dep_blockers="$(echo "${deps_json}" | jq -r '[.[] | select(.status != "done") | "T#\(.task_id) is \(.status)"] | join(", ")')"

    if [[ -n "${parent_blockers}" || -n "${dep_blockers}" ]]; then
      echo "  - T#${task_id} ${task_title}: blocked by ${parent_blockers}${parent_blockers:+${dep_blockers:+; }}${dep_blockers}"
      continue
    fi

    if [[ "${task_status}" == "planned" ]]; then
      if worker_can_cover_caps "${caps_json}"; then
        echo "  - T#${task_id} ${task_title}: structurally ready; likely waiting on claim/reconcile timing"
      else
        echo "  - T#${task_id} ${task_title}: capability mismatch; requires $(echo "${caps_json}" | jq -r 'join(", ")')"
      fi
      continue
    fi

    if [[ "${task_status}" == "failed" ]]; then
      echo "  - T#${task_id} ${task_title}: task is failed; manual retry or spec change required"
      continue
    fi

    if [[ "${attempts_used}" != "0" && "${attempts_used}" -ge "${max_attempts}" ]]; then
      echo "  - T#${task_id} ${task_title}: attempts exhausted (${attempts_used}/${max_attempts})"
      continue
    fi

    echo "  - T#${task_id} ${task_title}: status=${task_status}; inspect task events and runtime alerts"
  done < <(echo "${tasks_json}" | jq -r '.[] | select(.status != "done") | .id')
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
- Save a tasq_save_checkpoint note after meaningful progress and before risky edits or long tests.
- If task context returns interrupted_runs, read the latest reason, resume hint, and checkpoint before editing.
- If this session restarts after interruption, do not assume the prior claim is still valid. Claim fresh work from TasQ.
- If claim returns no task, exit cleanly.
- Keep the result payload factual and concise.
- When linking git refs, classify them explicitly:
  - baseline
  - rerun_branch
  - produced
- Required capabilities for this worker: ${capabilities_csv}

Loop:
1) Call tasq_claim_next with project_id=${PROJECT_ID}, agent_id=${agent_id}, lease_seconds=${LEASE_SECONDS}, capabilities=${capabilities_json}.
2) If no task is returned, stop.
3) Call tasq_get_task_context for the claimed task.
4) Implement exactly what the task spec requires in the current workspace.
5) Run verification or tests that match the task scope.
6) Send tasq_heartbeat periodically while work is in progress.
7) Save tasq_save_checkpoint notes when progress reaches a meaningful checkpoint.
8) If success, call tasq_complete_task with result_md and result_payload_version="v2".
9) If failure, call tasq_fail_task with reason and result_payload_version="v2".
10) For reruns, prefer a fresh branch and call tasq_link_git_ref with explicit ref_kind values:
    - baseline for the branch point commit
    - rerun_branch for the rerun branch marker
    - produced for the commit created by this task
11) Repeat until no claimable task remains.

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
      diagnose_blockage
      echo "${runtime_json}" | jq .
      exit 3
    fi
  else
    idle_cycles=0
  fi
done
