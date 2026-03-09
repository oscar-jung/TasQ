# tasq-mcp (Phase 3)

MCP stdio server for TasQ runtime integration.

## Current status
- MCP transport (stdio, `Content-Length` framing)
- `initialize`
- `tools/list`
- `tools/call` tools:
  - `tasq_ping`
  - `tasq_result_payload_template`
  - `tasq_claim_next`
  - `tasq_get_task_context`
  - `tasq_runtime_alerts`
  - `tasq_heartbeat`
  - `tasq_complete_task`
  - `tasq_fail_task`
  - `tasq_link_git_ref`

Phase 3 adds write-path runtime APIs for heartbeat and task finalization.

## Environment
- `TASQ_API_BASE` (default: `http://localhost:8080`)
- `TASQ_TOKEN_AGENT` (used by claim/context tools)
- `TASQ_TOKEN_ADMIN` (fallback for admin-level reads such as runtime alerts)

## Run
```bash
cd tasq-mcp
go run ./cmd/server
```

Logs are written to stderr.
