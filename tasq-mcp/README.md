# tasq-mcp (Phase 1)

Minimal MCP stdio server skeleton for TasQ integration.

## Current status
- MCP transport (stdio, `Content-Length` framing)
- `initialize`
- `tools/list`
- `tools/call` with one no-op tool:
  - `tasq_ping`

This is the Phase 1 checkpoint. Runtime tools are added in later phases.

## Environment
- `TASQ_API_BASE` (default: `http://localhost:8080`)
- `TASQ_TOKEN_AGENT` (optional in Phase 1)
- `TASQ_TOKEN_ADMIN` (optional in Phase 1)

## Run
```bash
cd tasq-mcp
go run ./cmd/server
```

Logs are written to stderr.

