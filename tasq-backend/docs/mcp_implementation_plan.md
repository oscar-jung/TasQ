# TasQ MCP Runtime Integration Plan

Branch: `codex/mcp-runtime-integration`

This plan is designed for incremental delivery with clear checkpoints.

## Phase 0: Scope Lock
- Define the MCP tool set:
  - `tasq_claim_next`
  - `tasq_get_task_context`
  - `tasq_heartbeat`
  - `tasq_complete_task`
  - `tasq_fail_task`
  - `tasq_link_git_ref`
  - `tasq_runtime_alerts`
  - (optional admin) `tasq_create_project`, `tasq_create_task`, `tasq_add_dependency`
- Freeze payload contract:
  - use `result_payload_version = v2` by default

Acceptance:
- Tool list and IO contract agreed.

## Phase 1: MCP Server Skeleton
- Create `tasq-mcp/` service (Go or Node).
- Wire MCP transport, health logging, and environment loading:
  - `TASQ_API_BASE`
  - `TASQ_TOKEN_AGENT`
  - `TASQ_TOKEN_ADMIN` (optional)

Acceptance:
- MCP server starts.
- At least one no-op or ping tool responds.

## Phase 2: Runtime Tools (Read/Claim Path)
- Implement:
  - `tasq_claim_next`
  - `tasq_get_task_context`
  - `tasq_runtime_alerts`
- Normalize backend errors to stable MCP error shape.

Acceptance:
- Can claim a task and fetch context from MCP only.

## Phase 3: Runtime Tools (Write Path)
- Implement:
  - `tasq_heartbeat`
  - `tasq_complete_task`
  - `tasq_fail_task`
  - `tasq_link_git_ref`
- Add helper to build v2 payload template.

Acceptance:
- End-to-end claim -> complete/fail works via MCP.

## Phase 4: Planner/Admin Tools (Optional but Recommended)
- Implement admin tools behind admin token:
  - `tasq_create_project`
  - `tasq_create_task`
  - `tasq_add_dependency`
  - `tasq_set_task_capabilities`

Acceptance:
- New project and full task graph can be created from MCP tools.

## Phase 5: Codex CLI Session Profiles
- Define two operational profiles:
  - Planner session prompt template
  - Worker session prompt template
- Document how to run N worker sessions concurrently.

Acceptance:
- Repeatable workflow documented and tested.

## Phase 6: Hardening and Release
- Add smoke script for MCP route:
  - claim -> context -> heartbeat -> complete
- Add failure-path smoke:
  - fail flow and runtime alerts visibility
- Update docs:
  - root README
  - `README.ko.md`
  - operations guide

Acceptance:
- `./scripts/final_regression_suite.sh` + MCP smoke pass.

---

## Suggested Execution Order
1. Phase 1
2. Phase 2
3. Phase 3
4. Phase 5 (minimum usable)
5. Phase 4 (admin convenience)
6. Phase 6

