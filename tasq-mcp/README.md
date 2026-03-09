# tasq-mcp

MCP server for TasQ runtime integration. It supports both `stdio` and HTTP transport.

## Current status
- MCP transport:
  - `stdio`
  - HTTP JSON-RPC endpoint for `codex mcp add --url`
- `initialize`
- `tools/list`
- `tools/call` tools:
  - `tasq_ping`
  - `tasq_result_payload_template`
  - `tasq_claim_next`
  - `tasq_get_task_context`
  - `tasq_runtime_alerts`
  - `tasq_heartbeat`
  - `tasq_save_checkpoint`
  - `tasq_complete_task`
  - `tasq_fail_task`
  - `tasq_link_git_ref`
- `tasq_create_project`
  - supports `git_policy`, `execution_mode`, and `plan_state`
  - `tasq_create_task`
  - `tasq_add_dependency`
  - `tasq_set_task_capabilities`

Phase 4 adds optional planner/admin tools.

## Environment
- `TASQ_API_BASE` (default: `http://localhost:8080`)
- `TASQ_TOKEN_AGENT` (used by claim/context tools)
- `TASQ_TOKEN_ADMIN` (required for admin tools, fallback for runtime alerts)
- `TASQ_MCP_TRANSPORT` (`stdio` or `http`, default: `stdio`)
- `TASQ_MCP_HTTP_ADDR` (default: `127.0.0.1:8091`)
- `TASQ_MCP_HTTP_PATH` (default: `/mcp`)

## Run (stdio)
```bash
cd tasq-mcp
go run ./cmd/server
```

## Run (HTTP)
```bash
cd tasq-mcp
TASQ_MCP_TRANSPORT=http TASQ_MCP_HTTP_ADDR=127.0.0.1:8091 go run ./cmd/server
```

Then register it in Codex:
```bash
codex mcp add tasq-http --url http://localhost:8091/mcp
```

With Docker Compose:
```bash
docker compose up -d --build mcp
codex mcp add tasq-http --url http://localhost:8091/mcp
```

Logs are written to stderr only when `TASQ_MCP_DEBUG=1`.

## Smoke test
```bash
./scripts/mcp_runtime_smoke.sh
```
