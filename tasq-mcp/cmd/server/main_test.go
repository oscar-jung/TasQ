package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMCPRuntimeFlowClaimContextHeartbeatComplete(t *testing.T) {
	t.Helper()

	var seen []string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Method+" "+r.URL.String())
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/agents/claim-next":
			writeJSONTest(w, map[string]any{
				"task": map[string]any{
					"id":         101,
					"project_id": 1,
					"title":      "Task A",
					"status":     "in_progress",
				},
				"claim": map[string]any{
					"id":       9001,
					"token":    "claim-token-1",
					"attempts": 1,
				},
			})
			return
		case r.Method == http.MethodGet && r.URL.Path == "/tasks/101/context":
			writeJSONTest(w, map[string]any{
				"task": map[string]any{
					"id":    101,
					"title": "Task A",
				},
				"dependencies": []any{},
				"parent_chain": []any{},
			})
			return
		case r.Method == http.MethodPost && r.URL.Path == "/tasks/101/heartbeat":
			writeJSONTest(w, map[string]any{"ok": true})
			return
		case r.Method == http.MethodPost && r.URL.Path == "/tasks/101/complete":
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode complete payload: %v", err)
			}
			if payload["result_payload_version"] != "v2" {
				t.Fatalf("expected result_payload_version=v2, got %#v", payload["result_payload_version"])
			}
			writeJSONTest(w, map[string]any{"ok": true})
			return
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	cfg := config{APIBase: ts.URL, AgentToken: "agent-token"}

	resp := callTool(t, cfg, "tasq_claim_next", map[string]any{
		"project_id": 1,
		"agent_id":   "worker-1",
	})
	if resp.Error != nil {
		t.Fatalf("claim response error: %+v", resp.Error)
	}

	resp = callTool(t, cfg, "tasq_get_task_context", map[string]any{
		"task_id": 101,
	})
	if resp.Error != nil {
		t.Fatalf("context response error: %+v", resp.Error)
	}

	resp = callTool(t, cfg, "tasq_heartbeat", map[string]any{
		"task_id":      101,
		"agent_id":     "worker-1",
		"claim_token":  "claim-token-1",
		"lease_seconds": 120,
	})
	if resp.Error != nil {
		t.Fatalf("heartbeat response error: %+v", resp.Error)
	}

	resp = callTool(t, cfg, "tasq_complete_task", map[string]any{
		"task_id":     101,
		"agent_id":    "worker-1",
		"claim_token": "claim-token-1",
		"result_md":   "done",
		"result_payload": map[string]any{
			"summary":    "completed",
			"changes":    []string{"implemented API"},
			"paths":      []string{"internal/server/tasks.go"},
			"commands":   []string{"go test ./..."},
			"tests":      []string{"pass"},
			"artifacts":  []string{"binary"},
			"next_risks": []string{"none"},
		},
	})
	if resp.Error != nil {
		t.Fatalf("complete response error: %+v", resp.Error)
	}

	want := []string{
		"POST /agents/claim-next",
		"GET /tasks/101/context",
		"POST /tasks/101/heartbeat",
		"POST /tasks/101/complete",
	}
	if len(seen) != len(want) {
		t.Fatalf("expected %d calls, got %d: %v", len(want), len(seen), seen)
	}
	for i := range want {
		if seen[i] != want[i] {
			t.Fatalf("call[%d] expected %q got %q", i, want[i], seen[i])
		}
	}
}

func TestMCPRuntimeFlowFailAndRuntimeAlerts(t *testing.T) {
	t.Helper()

	var authForAlerts string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/tasks/202/fail":
			if strings.TrimSpace(r.Header.Get("Authorization")) != "Bearer agent-token" {
				http.Error(w, "missing/invalid agent auth", http.StatusUnauthorized)
				return
			}
			writeJSONTest(w, map[string]any{"ok": true})
			return
		case r.Method == http.MethodGet && r.URL.Path == "/projects/1/runtime-alerts":
			authForAlerts = r.Header.Get("Authorization")
			writeJSONTest(w, map[string]any{
				"project_id":               1,
				"stale_claims":             []any{},
				"heartbeat_overdue_claims": []any{},
				"orphan_in_progress": []map[string]any{
					{"task_id": 202},
				},
			})
			return
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	// Intentionally omit agent token to ensure runtime alerts fallback uses admin token.
	cfg := config{APIBase: ts.URL, AdminToken: "admin-token"}

	resp := callTool(t, cfg, "tasq_fail_task", map[string]any{
		"task_id":     202,
		"agent_id":    "worker-2",
		"claim_token": "claim-token-2",
		"reason":      "test failure",
		"result_payload": map[string]any{
			"summary":    "failed",
			"changes":    []string{"none"},
			"paths":      []string{"none"},
			"commands":   []string{"go test ./..."},
			"tests":      []string{"failed"},
			"artifacts":  []string{"report.log"},
			"next_risks": []string{"needs retry"},
		},
	})
	if resp.Error == nil {
		t.Fatalf("expected fail call to require agent token, got success")
	}

	cfg.AgentToken = "agent-token"
	resp = callTool(t, cfg, "tasq_fail_task", map[string]any{
		"task_id":     202,
		"agent_id":    "worker-2",
		"claim_token": "claim-token-2",
		"reason":      "test failure",
		"result_payload": map[string]any{
			"summary":    "failed",
			"changes":    []string{"none"},
			"paths":      []string{"none"},
			"commands":   []string{"go test ./..."},
			"tests":      []string{"failed"},
			"artifacts":  []string{"report.log"},
			"next_risks": []string{"needs retry"},
		},
	})
	if resp.Error != nil {
		t.Fatalf("fail response error: %+v", resp.Error)
	}

	cfg.AgentToken = ""
	resp = callTool(t, cfg, "tasq_runtime_alerts", map[string]any{
		"project_id": 1,
	})
	if resp.Error != nil {
		t.Fatalf("runtime alerts response error: %+v", resp.Error)
	}
	if strings.TrimSpace(authForAlerts) != "Bearer admin-token" {
		t.Fatalf("expected runtime alerts to use admin token fallback, got %q", authForAlerts)
	}
}

func callTool(t *testing.T, cfg config, tool string, args map[string]any) rpcResponse {
	t.Helper()

	params, err := json.Marshal(map[string]any{
		"name":      tool,
		"arguments": args,
	})
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	return handleRequest(rpcRequest{
		JSONRPC: "2.0",
		ID:      json.RawMessage("1"),
		Method:  "tools/call",
		Params:  params,
	}, cfg)
}

func writeJSONTest(w http.ResponseWriter, body map[string]any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(body)
}
