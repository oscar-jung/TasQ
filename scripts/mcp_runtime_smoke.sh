#!/usr/bin/env bash
set -euo pipefail

echo "[mcp] runtime flow smoke"
(
  cd tasq-mcp
  go test ./cmd/server -run 'TestMCPRuntimeFlowClaimContextHeartbeatComplete|TestMCPRuntimeFlowFailAndRuntimeAlerts' -count=1
)

echo "[mcp] PASS"

