package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
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
}

func main() {
	log.SetOutput(os.Stderr)
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)

	cfg := loadConfig()
	log.Printf(
		"tasq-mcp starting; api_base=%s agent_token=%t admin_token=%t",
		cfg.APIBase, cfg.AgentToken != "", cfg.AdminToken != "",
	)

	if err := serve(os.Stdin, os.Stdout, cfg); err != nil {
		log.Fatalf("tasq-mcp fatal: %v", err)
	}
}

func loadConfig() config {
	return config{
		APIBase:    getenv("TASQ_API_BASE", "http://localhost:8080"),
		AgentToken: strings.TrimSpace(os.Getenv("TASQ_TOKEN_AGENT")),
		AdminToken: strings.TrimSpace(os.Getenv("TASQ_TOKEN_ADMIN")),
	}
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
			_ = writeResponse(w, rpcResponse{
				JSONRPC: "2.0",
				Error: &rpcError{
					Code:    -32700,
					Message: "parse error",
				},
			})
			continue
		}

		resp := handleRequest(req, cfg)
		if len(req.ID) == 0 {
			continue
		}
		resp.ID = req.ID
		resp.JSONRPC = "2.0"
		if err := writeResponse(w, resp); err != nil {
			return err
		}
	}
}

func handleRequest(req rpcRequest, cfg config) rpcResponse {
	switch req.Method {
	case "initialize":
		return rpcResponse{
			Result: map[string]any{
				"protocolVersion": "2024-11-05",
				"capabilities": map[string]any{
					"tools": map[string]any{
						"listChanged": false,
					},
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
		lower := strings.ToLower(line)
		if strings.HasPrefix(lower, "content-length:") {
			v := strings.TrimSpace(line[len("Content-Length:"):])
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

func getenv(key, fallback string) string {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	return v
}

