package main

// orbit-runtime-mcp is the MCP server for session-runtime tools.
// Claude Code spawns this binary (via --mcp-config) and communicates over
// stdio using JSON-RPC 2.0. It exposes tools that let Claude interact with
// the session-manager to manage its own execution environment.

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"
)

func main() {
	srv := &mcpServer{
		sessionManagerURL: requireEnv("ORBIT_SESSION_MANAGER_URL"),
		runID:             requireEnv("ORBIT_RUN_ID"),
		// Longer than the session-manager's worst case (5 minute provision budget
		// plus the 90s egress canary), so the tool reports its verdict, not a timeout.
		httpClient: &http.Client{Timeout: 8 * time.Minute},
	}
	if err := srv.serve(os.Stdin, os.Stdout); err != nil && !errors.Is(err, io.EOF) {
		log.Fatalf("orbit-runtime-mcp: %v", err)
	}
}

func requireEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		log.Fatalf("orbit-runtime-mcp: %s is required", key)
	}
	return v
}

// ── JSON-RPC types ────────────────────────────────────────────────────────────

type jsonRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type jsonRPCResponse struct {
	JSONRPC string    `json:"jsonrpc"`
	ID      any       `json:"id,omitempty"`
	Result  any       `json:"result,omitempty"`
	Error   *rpcError `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// ── MCP server ────────────────────────────────────────────────────────────────

type mcpServer struct {
	sessionManagerURL string
	runID             string
	httpClient        *http.Client
}

func (s *mcpServer) serve(r io.Reader, w io.Writer) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 256*1024), 256*1024)
	enc := json.NewEncoder(w)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var req jsonRPCRequest
		if err := json.Unmarshal(line, &req); err != nil {
			continue
		}
		resp := s.handle(req)
		if err := enc.Encode(resp); err != nil {
			return err
		}
	}
	return scanner.Err()
}

func (s *mcpServer) handle(req jsonRPCRequest) jsonRPCResponse {
	switch req.Method {
	case "initialize":
		return s.handleInitialize(req)
	case "notifications/initialized":
		return jsonRPCResponse{} // no response needed for notifications
	case "tools/list":
		return s.handleToolsList(req)
	case "tools/call":
		return s.handleToolsCall(req)
	case "resources/list", "resources/templates/list":
		// This server exposes tools only and says so in its capabilities. Codex
		// probes for resources anyway, and a -32601 sends its model looking for
		// resource URIs instead of calling provision_sandbox.
		return jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  map[string]any{"resources": []any{}, "resourceTemplates": []any{}},
		}
	default:
		return jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &rpcError{Code: -32601, Message: "method not found"},
		}
	}
}

func (s *mcpServer) handleInitialize(req jsonRPCRequest) jsonRPCResponse {
	return jsonRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: map[string]any{
			"protocolVersion": "2024-11-05",
			"serverInfo": map[string]any{
				"name":    "orbit-runtime",
				"version": "1.0.0",
			},
			"capabilities": map[string]any{
				"tools": map[string]any{},
			},
		},
	}
}

func (s *mcpServer) handleToolsList(req jsonRPCRequest) jsonRPCResponse {
	return jsonRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: map[string]any{
			"tools": []map[string]any{
				{
					"name":        "provision_sandbox",
					"description": "Provisions a sandbox execution environment for this session. Call this before making any outbound network call (git clone, curl, npm install, pip install, etc.) or running build commands, compiling code, or starting servers. After this tool returns successfully, all Bash commands will execute inside the sandbox and outbound connections will work. Only call this when you actually need those capabilities — do not provision eagerly.\n\nIMPORTANT — working with git repositories after provisioning:\n- Never run `git init` if `git clone` fails. A locally initialized repo has no history in common with the remote and will produce an unmergeable orphan branch.\n- Retry `git clone` (with backoff) on transient failures such as connection reset by peer — these resolve once the sandbox egress path has fully settled.\n- Before branching, committing, or pushing, verify the clone is real: `git remote get-url origin` must succeed and the base branch must be fetchable. If it is not, retry the clone — do not proceed on a broken working copy.",
					"inputSchema": map[string]any{
						"type":       "object",
						"properties": map[string]any{},
						"required":   []string{},
					},
				},
			},
		},
	}
}

func (s *mcpServer) handleToolsCall(req jsonRPCRequest) jsonRPCResponse {
	var params struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return errResponse(req.ID, -32602, "invalid params")
	}

	switch params.Name {
	case "provision_sandbox":
		if err := s.provisionSandbox(); err != nil {
			// A tool result with isError, not a JSON-RPC error: the model reads the
			// text, which tells it what survived and what to do next.
			return jsonRPCResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Result: map[string]any{
					"content": []map[string]any{{"type": "text", "text": fmt.Sprintf("provision_sandbox failed: %v", err)}},
					"isError": true,
				},
			}
		}
		return jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: map[string]any{
				"content": []map[string]any{
					{"type": "text", "text": "Sandbox provisioned. Bash commands will now execute in the sandbox environment."},
				},
			},
		}
	default:
		return errResponse(req.ID, -32601, fmt.Sprintf("unknown tool: %s", params.Name))
	}
}

func (s *mcpServer) provisionSandbox() error {
	url := fmt.Sprintf("%s/internal/session/%s/provision-sandbox", s.sessionManagerURL, s.runID)
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, url, bytes.NewReader([]byte("{}")))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Orbit-Run-Token", os.Getenv("ORBIT_RUN_TOKEN"))
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		body, _ := io.ReadAll(resp.Body)
		var apiErr struct {
			Message string `json:"message"`
		}
		if json.Unmarshal(body, &apiErr) == nil && apiErr.Message != "" {
			return errors.New(apiErr.Message)
		}
		return fmt.Errorf("session-manager returned %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

func errResponse(id any, code int, msg string) jsonRPCResponse {
	return jsonRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &rpcError{Code: code, Message: msg},
	}
}
