package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type config struct {
	APIBase    string
	AgentToken string
	AdminToken string
	Debug      bool
	Transport  string
	HTTPAddr   string
	HTTPPath   string
}

type claimNextArgs struct {
	ProjectID    int64    `json:"project_id"`
	AgentID      string   `json:"agent_id"`
	LeaseSeconds int64    `json:"lease_seconds,omitempty"`
	Capabilities []string `json:"capabilities,omitempty"`
}

type getTaskContextArgs struct {
	TaskID int64 `json:"task_id"`
}

type runtimeAlertsArgs struct {
	ProjectID            int64 `json:"project_id"`
	HeartbeatStaleSecond int64 `json:"heartbeat_stale_seconds,omitempty"`
}

type heartbeatArgs struct {
	TaskID       int64  `json:"task_id"`
	AgentID      string `json:"agent_id"`
	ClaimToken   string `json:"claim_token"`
	LeaseSeconds int64  `json:"lease_seconds,omitempty"`
}

type checkpointArgs struct {
	TaskID     int64  `json:"task_id"`
	AgentID    string `json:"agent_id"`
	ClaimToken string `json:"claim_token"`
	Note       string `json:"note"`
}

type resultPayloadV2 struct {
	Summary   string   `json:"summary"`
	Changes   []string `json:"changes"`
	Paths     []string `json:"paths"`
	Commands  []string `json:"commands"`
	Tests     []string `json:"tests"`
	Artifacts []string `json:"artifacts"`
	NextRisks []string `json:"next_risks"`
}

type completeTaskArgs struct {
	TaskID               int64           `json:"task_id"`
	AgentID              string          `json:"agent_id"`
	ClaimToken           string          `json:"claim_token"`
	ResultMD             string          `json:"result_md,omitempty"`
	ResultPayloadVersion string          `json:"result_payload_version,omitempty"`
	ResultPayload        resultPayloadV2 `json:"result_payload"`
}

type failTaskArgs struct {
	TaskID               int64           `json:"task_id"`
	AgentID              string          `json:"agent_id"`
	ClaimToken           string          `json:"claim_token"`
	Reason               string          `json:"reason"`
	ResultMD             string          `json:"result_md,omitempty"`
	ResultPayloadVersion string          `json:"result_payload_version,omitempty"`
	ResultPayload        resultPayloadV2 `json:"result_payload"`
}

type linkGitRefArgs struct {
	TaskID     int64  `json:"task_id"`
	Repo       string `json:"repo,omitempty"`
	Branch     string `json:"branch,omitempty"`
	BaseCommit string `json:"base_commit,omitempty"`
	CommitSHA  string `json:"commit_sha,omitempty"`
}

type createProjectArgs struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

type createTaskArgs struct {
	ProjectID            int64    `json:"project_id"`
	ParentTaskID         *int64   `json:"parent_task_id,omitempty"`
	Title                string   `json:"title"`
	SpecMD               string   `json:"spec_md,omitempty"`
	MaxAttempts          *int64   `json:"max_attempts,omitempty"`
	RequiredCapabilities []string `json:"required_capabilities,omitempty"`
}

type addDependencyArgs struct {
	TaskID            int64 `json:"task_id"`
	PredecessorTaskID int64 `json:"predecessor_task_id"`
}

type setTaskCapabilitiesArgs struct {
	TaskID               int64    `json:"task_id"`
	RequiredCapabilities []string `json:"required_capabilities"`
}

type httpCallError struct {
	Status int
	Body   string
}

func (e *httpCallError) Error() string {
	return fmt.Sprintf("tasq api status=%d body=%s", e.Status, e.Body)
}

func main() {
	log.SetOutput(os.Stderr)
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)

	cfg := loadConfig()
	debugf(cfg, "tasq-mcp starting; api_base=%s agent_token=%t admin_token=%t",
		cfg.APIBase, cfg.AgentToken != "", cfg.AdminToken != "")

	var err error
	switch cfg.Transport {
	case "http":
		err = serveHTTP(cfg)
	default:
		err = serve(os.Stdin, os.Stdout, cfg)
	}
	if err != nil {
		debugf(cfg, "tasq-mcp fatal: %v", err)
		os.Exit(1)
	}
}

func loadConfig() config {
	transport := getenv("TASQ_MCP_TRANSPORT", "stdio")
	httpAddr := getenv("TASQ_MCP_HTTP_ADDR", "127.0.0.1:8091")
	httpPath := getenv("TASQ_MCP_HTTP_PATH", "/mcp")

	flag.StringVar(&transport, "transport", transport, "mcp transport: stdio or http")
	flag.StringVar(&httpAddr, "http-addr", httpAddr, "http listen address")
	flag.StringVar(&httpPath, "http-path", httpPath, "http rpc path")
	flag.Parse()

	return config{
		APIBase:    getenv("TASQ_API_BASE", "http://localhost:8080"),
		AgentToken: strings.TrimSpace(os.Getenv("TASQ_TOKEN_AGENT")),
		AdminToken: strings.TrimSpace(os.Getenv("TASQ_TOKEN_ADMIN")),
		Debug:      strings.EqualFold(strings.TrimSpace(os.Getenv("TASQ_MCP_DEBUG")), "1"),
		Transport:  strings.ToLower(strings.TrimSpace(transport)),
		HTTPAddr:   strings.TrimSpace(httpAddr),
		HTTPPath:   strings.TrimSpace(httpPath),
	}
}

func serveHTTP(cfg config) error {
	mux := http.NewServeMux()
	mux.HandleFunc(cfg.HTTPPath, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodOptions:
			w.Header().Set("Allow", "POST, OPTIONS")
			w.WriteHeader(http.StatusNoContent)
			return
		case http.MethodPost:
			handleHTTPRPC(w, r, cfg)
			return
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
	})
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	debugf(cfg, "tasq-mcp http listening on %s%s", cfg.HTTPAddr, cfg.HTTPPath)
	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	return server.ListenAndServe()
}

func handleHTTPRPC(w http.ResponseWriter, r *http.Request, cfg config) {
	defer r.Body.Close()

	var req rpcRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeHTTPRPC(w, rpcResponse{
			JSONRPC: "2.0",
			Error: &rpcError{
				Code:    -32700,
				Message: "parse error",
			},
		})
		return
	}
	debugf(cfg, "http recv method=%s id_present=%t", req.Method, len(req.ID) > 0)

	resp := handleRequest(req, cfg)
	if len(req.ID) == 0 {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	resp.JSONRPC = "2.0"
	resp.ID = req.ID
	writeHTTPRPC(w, resp)
}

func writeHTTPRPC(w http.ResponseWriter, resp rpcResponse) {
	data, err := json.Marshal(resp)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(data)
}

func serve(in io.Reader, out io.Writer, cfg config) error {
	r := bufio.NewReader(in)
	w := bufio.NewWriter(out)
	for {
		body, err := readMessage(r)
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}

		var req rpcRequest
		if err := json.Unmarshal(body, &req); err != nil {
			debugf(cfg, "parse error for raw body: %q", string(body))
			_ = writeResponse(w, rpcResponse{
				JSONRPC: "2.0",
				Error: &rpcError{
					Code:    -32700,
					Message: "parse error",
				},
			})
			continue
		}
		debugf(cfg, "recv method=%s id_present=%t", req.Method, len(req.ID) > 0)

		resp := handleRequest(req, cfg)
		if len(req.ID) == 0 {
			continue
		}
		resp.ID = req.ID
		resp.JSONRPC = "2.0"
		if resp.Error != nil {
			debugf(cfg, "send error method=%s code=%d message=%s", req.Method, resp.Error.Code, resp.Error.Message)
		} else {
			debugf(cfg, "send result method=%s", req.Method)
		}
		if err := writeResponse(w, resp); err != nil {
			return err
		}
	}
}

func handleRequest(req rpcRequest, cfg config) rpcResponse {
	switch req.Method {
	case "initialize":
		protocolVersion := "2024-11-05"
		var initParams struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		if len(req.Params) > 0 {
			_ = json.Unmarshal(req.Params, &initParams)
			if strings.TrimSpace(initParams.ProtocolVersion) != "" {
				protocolVersion = strings.TrimSpace(initParams.ProtocolVersion)
			}
		}
		return rpcResponse{
			Result: map[string]any{
				"protocolVersion": protocolVersion,
				"capabilities": map[string]any{
					"tools": map[string]any{
						"listChanged": false,
					},
					"logging": map[string]any{},
				},
				"serverInfo": map[string]any{
					"name":    "tasq-mcp",
					"version": "0.1.0",
				},
			},
		}
	case "notifications/initialized":
		return rpcResponse{}
	case "tools/list":
		return rpcResponse{
			Result: map[string]any{
				"tools": []map[string]any{
					{
						"name":        "tasq_ping",
						"description": "Returns MCP server and TasQ API connectivity metadata.",
						"inputSchema": map[string]any{
							"type":       "object",
							"properties": map[string]any{},
						},
					},
					{
						"name":        "tasq_result_payload_template",
						"description": "Return a v2 result_payload template for complete/fail calls.",
						"inputSchema": map[string]any{
							"type":       "object",
							"properties": map[string]any{},
						},
					},
					{
						"name":        "tasq_claim_next",
						"description": "Claim next executable task for an agent.",
						"inputSchema": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"project_id":    map[string]any{"type": "integer"},
								"agent_id":      map[string]any{"type": "string"},
								"lease_seconds": map[string]any{"type": "integer"},
								"capabilities": map[string]any{
									"type":  "array",
									"items": map[string]any{"type": "string"},
								},
							},
							"required": []string{"project_id", "agent_id"},
						},
					},
					{
						"name":        "tasq_get_task_context",
						"description": "Fetch task context payload for execution.",
						"inputSchema": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"task_id": map[string]any{"type": "integer"},
							},
							"required": []string{"task_id"},
						},
					},
					{
						"name":        "tasq_runtime_alerts",
						"description": "Read runtime alert report for a project.",
						"inputSchema": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"project_id":              map[string]any{"type": "integer"},
								"heartbeat_stale_seconds": map[string]any{"type": "integer"},
							},
							"required": []string{"project_id"},
						},
					},
					{
						"name":        "tasq_heartbeat",
						"description": "Extend an active task claim lease.",
						"inputSchema": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"task_id":       map[string]any{"type": "integer"},
								"agent_id":      map[string]any{"type": "string"},
								"claim_token":   map[string]any{"type": "string"},
								"lease_seconds": map[string]any{"type": "integer"},
							},
							"required": []string{"task_id", "agent_id", "claim_token"},
						},
					},
					{
						"name":        "tasq_save_checkpoint",
						"description": "Save a mid-run checkpoint note for the active task claim.",
						"inputSchema": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"task_id":     map[string]any{"type": "integer"},
								"agent_id":    map[string]any{"type": "string"},
								"claim_token": map[string]any{"type": "string"},
								"note":        map[string]any{"type": "string"},
							},
							"required": []string{"task_id", "agent_id", "claim_token", "note"},
						},
					},
					{
						"name":        "tasq_complete_task",
						"description": "Complete claimed task with v2 result payload.",
						"inputSchema": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"task_id":     map[string]any{"type": "integer"},
								"agent_id":    map[string]any{"type": "string"},
								"claim_token": map[string]any{"type": "string"},
								"result_md":   map[string]any{"type": "string"},
								"result_payload_version": map[string]any{
									"type": "string",
									"enum": []string{"v2"},
								},
								"result_payload": map[string]any{
									"type": "object",
									"properties": map[string]any{
										"summary":    map[string]any{"type": "string"},
										"changes":    map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
										"paths":      map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
										"commands":   map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
										"tests":      map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
										"artifacts":  map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
										"next_risks": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
									},
									"required": []string{
										"summary", "changes", "paths", "commands", "tests", "artifacts", "next_risks",
									},
								},
							},
							"required": []string{"task_id", "agent_id", "claim_token", "result_payload"},
						},
					},
					{
						"name":        "tasq_fail_task",
						"description": "Fail claimed task with reason and v2 result payload.",
						"inputSchema": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"task_id":     map[string]any{"type": "integer"},
								"agent_id":    map[string]any{"type": "string"},
								"claim_token": map[string]any{"type": "string"},
								"reason":      map[string]any{"type": "string"},
								"result_md":   map[string]any{"type": "string"},
								"result_payload_version": map[string]any{
									"type": "string",
									"enum": []string{"v2"},
								},
								"result_payload": map[string]any{
									"type": "object",
									"properties": map[string]any{
										"summary":    map[string]any{"type": "string"},
										"changes":    map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
										"paths":      map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
										"commands":   map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
										"tests":      map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
										"artifacts":  map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
										"next_risks": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
									},
									"required": []string{
										"summary", "changes", "paths", "commands", "tests", "artifacts", "next_risks",
									},
								},
							},
							"required": []string{"task_id", "agent_id", "claim_token", "reason", "result_payload"},
						},
					},
					{
						"name":        "tasq_link_git_ref",
						"description": "Link git metadata to task history.",
						"inputSchema": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"task_id":     map[string]any{"type": "integer"},
								"repo":        map[string]any{"type": "string"},
								"branch":      map[string]any{"type": "string"},
								"base_commit": map[string]any{"type": "string"},
								"commit_sha":  map[string]any{"type": "string"},
							},
							"required": []string{"task_id"},
						},
					},
					{
						"name":        "tasq_create_project",
						"description": "Create a project (admin token required).",
						"inputSchema": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"name":        map[string]any{"type": "string"},
								"description": map[string]any{"type": "string"},
							},
							"required": []string{"name"},
						},
					},
					{
						"name":        "tasq_create_task",
						"description": "Create a task (admin token required).",
						"inputSchema": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"project_id":     map[string]any{"type": "integer"},
								"parent_task_id": map[string]any{"type": "integer"},
								"title":          map[string]any{"type": "string"},
								"spec_md":        map[string]any{"type": "string"},
								"max_attempts":   map[string]any{"type": "integer"},
								"required_capabilities": map[string]any{
									"type":  "array",
									"items": map[string]any{"type": "string"},
								},
							},
							"required": []string{"project_id", "title"},
						},
					},
					{
						"name":        "tasq_add_dependency",
						"description": "Add dependency edge (admin token required).",
						"inputSchema": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"task_id":             map[string]any{"type": "integer"},
								"predecessor_task_id": map[string]any{"type": "integer"},
							},
							"required": []string{"task_id", "predecessor_task_id"},
						},
					},
					{
						"name":        "tasq_set_task_capabilities",
						"description": "Set task capability filter (admin token required).",
						"inputSchema": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"task_id": map[string]any{"type": "integer"},
								"required_capabilities": map[string]any{
									"type":  "array",
									"items": map[string]any{"type": "string"},
								},
							},
							"required": []string{"task_id", "required_capabilities"},
						},
					},
				},
			},
		}
	case "tools/call":
		var payload struct {
			Name      string                 `json:"name"`
			Arguments map[string]interface{} `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &payload); err != nil {
			return rpcResponse{Error: &rpcError{Code: -32602, Message: "invalid params"}}
		}
		switch payload.Name {
		case "tasq_ping":
			return rpcResponse{
				Result: map[string]any{
					"content": []map[string]any{
						{
							"type": "text",
							"text": fmt.Sprintf(
								"pong api_base=%s time=%s agent_token=%t admin_token=%t",
								cfg.APIBase, time.Now().UTC().Format(time.RFC3339), cfg.AgentToken != "", cfg.AdminToken != "",
							),
						},
					},
				},
			}
		case "tasq_result_payload_template":
			template := resultPayloadV2{
				Summary:   "What was completed and why.",
				Changes:   []string{"Code changes made."},
				Paths:     []string{"relative/path/to/file.go"},
				Commands:  []string{"go test ./..."},
				Tests:     []string{"unit:passed"},
				Artifacts: []string{"binary-or-report-name"},
				NextRisks: []string{"Open risk or follow-up"},
			}
			b, _ := json.Marshal(template)
			return toolResult(b)
		case "tasq_claim_next":
			var args claimNextArgs
			if err := decodeArgs(payload.Arguments, &args); err != nil {
				return rpcResponse{Error: &rpcError{Code: -32602, Message: err.Error()}}
			}
			if args.ProjectID == 0 || strings.TrimSpace(args.AgentID) == "" {
				return rpcResponse{Error: &rpcError{Code: -32602, Message: "project_id and agent_id are required"}}
			}
			resBody, err := tasqJSON(cfg, http.MethodPost, "/agents/claim-next", args, cfg.AgentToken)
			if err != nil {
				return toolError(err)
			}
			return toolResult(resBody)
		case "tasq_get_task_context":
			var args getTaskContextArgs
			if err := decodeArgs(payload.Arguments, &args); err != nil {
				return rpcResponse{Error: &rpcError{Code: -32602, Message: err.Error()}}
			}
			if args.TaskID == 0 {
				return rpcResponse{Error: &rpcError{Code: -32602, Message: "task_id is required"}}
			}
			resBody, err := tasqJSON(cfg, http.MethodGet, fmt.Sprintf("/tasks/%d/context", args.TaskID), nil, cfg.AgentToken)
			if err != nil {
				return toolError(err)
			}
			return toolResult(resBody)
		case "tasq_runtime_alerts":
			var args runtimeAlertsArgs
			if err := decodeArgs(payload.Arguments, &args); err != nil {
				return rpcResponse{Error: &rpcError{Code: -32602, Message: err.Error()}}
			}
			if args.ProjectID == 0 {
				return rpcResponse{Error: &rpcError{Code: -32602, Message: "project_id is required"}}
			}
			urlPath := fmt.Sprintf("/projects/%d/runtime-alerts", args.ProjectID)
			if args.HeartbeatStaleSecond > 0 {
				urlPath = fmt.Sprintf("%s?heartbeat_stale_seconds=%d", urlPath, args.HeartbeatStaleSecond)
			}
			token := cfg.AgentToken
			if token == "" {
				token = cfg.AdminToken
			}
			resBody, err := tasqJSON(cfg, http.MethodGet, urlPath, nil, token)
			if err != nil {
				return toolError(err)
			}
			return toolResult(resBody)
		case "tasq_heartbeat":
			var args heartbeatArgs
			if err := decodeArgs(payload.Arguments, &args); err != nil {
				return rpcResponse{Error: &rpcError{Code: -32602, Message: err.Error()}}
			}
			if args.TaskID == 0 || strings.TrimSpace(args.AgentID) == "" || strings.TrimSpace(args.ClaimToken) == "" {
				return rpcResponse{Error: &rpcError{Code: -32602, Message: "task_id, agent_id, and claim_token are required"}}
			}
			resBody, err := tasqJSON(cfg, http.MethodPost, fmt.Sprintf("/tasks/%d/heartbeat", args.TaskID), map[string]any{
				"agent_id":      strings.TrimSpace(args.AgentID),
				"claim_token":   strings.TrimSpace(args.ClaimToken),
				"lease_seconds": args.LeaseSeconds,
			}, cfg.AgentToken)
			if err != nil {
				return toolError(err)
			}
			return toolResult(resBody)
		case "tasq_save_checkpoint":
			var args checkpointArgs
			if err := decodeArgs(payload.Arguments, &args); err != nil {
				return rpcResponse{Error: &rpcError{Code: -32602, Message: err.Error()}}
			}
			if args.TaskID == 0 || strings.TrimSpace(args.AgentID) == "" || strings.TrimSpace(args.ClaimToken) == "" || strings.TrimSpace(args.Note) == "" {
				return rpcResponse{Error: &rpcError{Code: -32602, Message: "task_id, agent_id, claim_token, and note are required"}}
			}
			resBody, err := tasqJSON(cfg, http.MethodPost, fmt.Sprintf("/tasks/%d/checkpoint", args.TaskID), map[string]any{
				"agent_id":    strings.TrimSpace(args.AgentID),
				"claim_token": strings.TrimSpace(args.ClaimToken),
				"note":        strings.TrimSpace(args.Note),
			}, cfg.AgentToken)
			if err != nil {
				return toolError(err)
			}
			return toolResult(resBody)
		case "tasq_complete_task":
			var args completeTaskArgs
			if err := decodeArgs(payload.Arguments, &args); err != nil {
				return rpcResponse{Error: &rpcError{Code: -32602, Message: err.Error()}}
			}
			if args.TaskID == 0 || strings.TrimSpace(args.AgentID) == "" || strings.TrimSpace(args.ClaimToken) == "" {
				return rpcResponse{Error: &rpcError{Code: -32602, Message: "task_id, agent_id, and claim_token are required"}}
			}
			if err := validateResultPayloadV2(args.ResultPayload); err != nil {
				return rpcResponse{Error: &rpcError{Code: -32602, Message: err.Error()}}
			}
			version := strings.TrimSpace(args.ResultPayloadVersion)
			if version == "" {
				version = "v2"
			}
			resBody, err := tasqJSON(cfg, http.MethodPost, fmt.Sprintf("/tasks/%d/complete", args.TaskID), map[string]any{
				"agent_id":               strings.TrimSpace(args.AgentID),
				"claim_token":            strings.TrimSpace(args.ClaimToken),
				"result_md":              strings.TrimSpace(args.ResultMD),
				"result_payload_version": version,
				"result_payload":         args.ResultPayload,
			}, cfg.AgentToken)
			if err != nil {
				return toolError(err)
			}
			return toolResult(resBody)
		case "tasq_fail_task":
			var args failTaskArgs
			if err := decodeArgs(payload.Arguments, &args); err != nil {
				return rpcResponse{Error: &rpcError{Code: -32602, Message: err.Error()}}
			}
			if args.TaskID == 0 || strings.TrimSpace(args.AgentID) == "" || strings.TrimSpace(args.ClaimToken) == "" || strings.TrimSpace(args.Reason) == "" {
				return rpcResponse{Error: &rpcError{Code: -32602, Message: "task_id, agent_id, claim_token, and reason are required"}}
			}
			if err := validateResultPayloadV2(args.ResultPayload); err != nil {
				return rpcResponse{Error: &rpcError{Code: -32602, Message: err.Error()}}
			}
			version := strings.TrimSpace(args.ResultPayloadVersion)
			if version == "" {
				version = "v2"
			}
			resBody, err := tasqJSON(cfg, http.MethodPost, fmt.Sprintf("/tasks/%d/fail", args.TaskID), map[string]any{
				"agent_id":               strings.TrimSpace(args.AgentID),
				"claim_token":            strings.TrimSpace(args.ClaimToken),
				"reason":                 strings.TrimSpace(args.Reason),
				"result_md":              strings.TrimSpace(args.ResultMD),
				"result_payload_version": version,
				"result_payload":         args.ResultPayload,
			}, cfg.AgentToken)
			if err != nil {
				return toolError(err)
			}
			return toolResult(resBody)
		case "tasq_link_git_ref":
			var args linkGitRefArgs
			if err := decodeArgs(payload.Arguments, &args); err != nil {
				return rpcResponse{Error: &rpcError{Code: -32602, Message: err.Error()}}
			}
			if args.TaskID == 0 {
				return rpcResponse{Error: &rpcError{Code: -32602, Message: "task_id is required"}}
			}
			resBody, err := tasqJSON(cfg, http.MethodPost, fmt.Sprintf("/tasks/%d/git-link", args.TaskID), map[string]any{
				"repo":        strings.TrimSpace(args.Repo),
				"branch":      strings.TrimSpace(args.Branch),
				"base_commit": strings.TrimSpace(args.BaseCommit),
				"commit_sha":  strings.TrimSpace(args.CommitSHA),
			}, cfg.AgentToken)
			if err != nil {
				return toolError(err)
			}
			return toolResult(resBody)
		case "tasq_create_project":
			var args createProjectArgs
			if err := decodeArgs(payload.Arguments, &args); err != nil {
				return rpcResponse{Error: &rpcError{Code: -32602, Message: err.Error()}}
			}
			if strings.TrimSpace(args.Name) == "" {
				return rpcResponse{Error: &rpcError{Code: -32602, Message: "name is required"}}
			}
			adminToken, err := requireAdminToken(cfg)
			if err != nil {
				return toolError(err)
			}
			resBody, err := tasqJSON(cfg, http.MethodPost, "/projects", map[string]any{
				"name":        strings.TrimSpace(args.Name),
				"description": strings.TrimSpace(args.Description),
			}, adminToken)
			if err != nil {
				return toolError(err)
			}
			return toolResult(resBody)
		case "tasq_create_task":
			var args createTaskArgs
			if err := decodeArgs(payload.Arguments, &args); err != nil {
				return rpcResponse{Error: &rpcError{Code: -32602, Message: err.Error()}}
			}
			if args.ProjectID == 0 || strings.TrimSpace(args.Title) == "" {
				return rpcResponse{Error: &rpcError{Code: -32602, Message: "project_id and title are required"}}
			}
			adminToken, err := requireAdminToken(cfg)
			if err != nil {
				return toolError(err)
			}
			body := map[string]any{
				"parent_task_id":        args.ParentTaskID,
				"title":                 strings.TrimSpace(args.Title),
				"spec_md":               strings.TrimSpace(args.SpecMD),
				"required_capabilities": args.RequiredCapabilities,
			}
			if args.MaxAttempts != nil {
				body["max_attempts"] = *args.MaxAttempts
			}
			resBody, err := tasqJSON(cfg, http.MethodPost, fmt.Sprintf("/projects/%d/tasks", args.ProjectID), body, adminToken)
			if err != nil {
				return toolError(err)
			}
			return toolResult(resBody)
		case "tasq_add_dependency":
			var args addDependencyArgs
			if err := decodeArgs(payload.Arguments, &args); err != nil {
				return rpcResponse{Error: &rpcError{Code: -32602, Message: err.Error()}}
			}
			if args.TaskID == 0 || args.PredecessorTaskID == 0 {
				return rpcResponse{Error: &rpcError{Code: -32602, Message: "task_id and predecessor_task_id are required"}}
			}
			adminToken, err := requireAdminToken(cfg)
			if err != nil {
				return toolError(err)
			}
			resBody, err := tasqJSON(cfg, http.MethodPost, fmt.Sprintf("/tasks/%d/dependencies", args.TaskID), map[string]any{
				"predecessor_task_id": args.PredecessorTaskID,
			}, adminToken)
			if err != nil {
				return toolError(err)
			}
			return toolResult(resBody)
		case "tasq_set_task_capabilities":
			var args setTaskCapabilitiesArgs
			if err := decodeArgs(payload.Arguments, &args); err != nil {
				return rpcResponse{Error: &rpcError{Code: -32602, Message: err.Error()}}
			}
			if args.TaskID == 0 {
				return rpcResponse{Error: &rpcError{Code: -32602, Message: "task_id is required"}}
			}
			adminToken, err := requireAdminToken(cfg)
			if err != nil {
				return toolError(err)
			}
			resBody, err := tasqJSON(cfg, http.MethodPatch, fmt.Sprintf("/tasks/%d/capabilities", args.TaskID), map[string]any{
				"required_capabilities": args.RequiredCapabilities,
			}, adminToken)
			if err != nil {
				return toolError(err)
			}
			return toolResult(resBody)
		default:
			return rpcResponse{Error: &rpcError{Code: -32601, Message: "tool not found"}}
		}
	default:
		return rpcResponse{Error: &rpcError{Code: -32601, Message: "method not found"}}
	}
}

func readMessage(r *bufio.Reader) ([]byte, error) {
	contentLength := -1
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		headerName := strings.ToLower(strings.TrimSpace(parts[0]))
		headerValue := strings.TrimSpace(parts[1])
		if headerName == "content-length" {
			v := headerValue
			n, err := strconv.Atoi(v)
			if err != nil || n < 0 {
				return nil, fmt.Errorf("invalid content-length header: %q", line)
			}
			contentLength = n
		}
	}
	if contentLength < 0 {
		return nil, fmt.Errorf("missing content-length header")
	}
	body := make([]byte, contentLength)
	if _, err := io.ReadFull(r, body); err != nil {
		return nil, err
	}
	return body, nil
}

func writeResponse(w *bufio.Writer, resp rpcResponse) error {
	data, err := json.Marshal(resp)
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	_, _ = fmt.Fprintf(&buf, "Content-Length: %d\r\n\r\n", len(data))
	_, _ = buf.Write(data)
	if _, err := w.Write(buf.Bytes()); err != nil {
		return err
	}
	return w.Flush()
}

func decodeArgs(arguments map[string]any, out any) error {
	raw, err := json.Marshal(arguments)
	if err != nil {
		return fmt.Errorf("invalid arguments")
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("invalid arguments")
	}
	return nil
}

func tasqJSON(cfg config, method, route string, payload any, bearerToken string) (json.RawMessage, error) {
	baseURL, err := url.Parse(strings.TrimSpace(cfg.APIBase))
	if err != nil {
		return nil, fmt.Errorf("invalid TASQ_API_BASE: %w", err)
	}
	if baseURL.Scheme == "" || baseURL.Host == "" {
		return nil, fmt.Errorf("invalid TASQ_API_BASE: missing scheme or host")
	}
	routeURL, err := url.Parse(route)
	if err != nil {
		return nil, fmt.Errorf("invalid route: %w", err)
	}
	targetURL := baseURL.ResolveReference(routeURL)

	var bodyReader io.Reader
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		bodyReader = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, targetURL.String(), bodyReader)
	if err != nil {
		return nil, err
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if strings.TrimSpace(bearerToken) != "" {
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(bearerToken))
	}

	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &httpCallError{Status: resp.StatusCode, Body: strings.TrimSpace(string(body))}
	}
	if len(body) == 0 {
		body = []byte(`{"ok":true}`)
	}
	return json.RawMessage(body), nil
}

func toolResult(body json.RawMessage) rpcResponse {
	pretty := string(body)
	var anyJSON any
	if err := json.Unmarshal(body, &anyJSON); err == nil {
		if b, err := json.MarshalIndent(anyJSON, "", "  "); err == nil {
			pretty = string(b)
		}
	}
	return rpcResponse{
		Result: map[string]any{
			"content": []map[string]any{
				{"type": "text", "text": pretty},
			},
			"structuredContent": json.RawMessage(body),
		},
	}
}

func toolError(err error) rpcResponse {
	var httpErr *httpCallError
	if errors.As(err, &httpErr) {
		return rpcResponse{Error: &rpcError{
			Code:    -32001,
			Message: fmt.Sprintf("tasq api error (%d): %s", httpErr.Status, httpErr.Body),
		}}
	}
	return rpcResponse{Error: &rpcError{
		Code:    -32000,
		Message: err.Error(),
	}}
}

func validateResultPayloadV2(payload resultPayloadV2) error {
	if strings.TrimSpace(payload.Summary) == "" {
		return fmt.Errorf("result_payload.summary is required")
	}
	fields := map[string][]string{
		"changes":    payload.Changes,
		"paths":      payload.Paths,
		"commands":   payload.Commands,
		"tests":      payload.Tests,
		"artifacts":  payload.Artifacts,
		"next_risks": payload.NextRisks,
	}
	for name, values := range fields {
		if len(values) == 0 {
			return fmt.Errorf("result_payload.%s must be a non-empty array", name)
		}
		for i, item := range values {
			if strings.TrimSpace(item) == "" {
				return fmt.Errorf("result_payload.%s[%d] must be non-empty", name, i)
			}
		}
	}
	return nil
}

func requireAdminToken(cfg config) (string, error) {
	token := strings.TrimSpace(cfg.AdminToken)
	if token == "" {
		return "", fmt.Errorf("TASQ_TOKEN_ADMIN is required for this tool")
	}
	return token, nil
}

func getenv(key, fallback string) string {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	return v
}

func debugf(cfg config, format string, args ...any) {
	if !cfg.Debug {
		return
	}
	log.Printf(format, args...)
}
