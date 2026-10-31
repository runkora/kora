package main

// kora-runtime-mcp is the MCP server for session-runtime tools.
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
	"strings"
	"time"
)

// provisionTimeout sits above the session-manager's worst case for a provision:
// its retry budget (the chart caps agents.guardrails.sandboxProvisionBudgetSecs
// at 600) plus the egress canary (90s, egressCanaryMaxWait). Timing out first
// reports a sandbox that the session-manager then goes on to hand over.
const provisionTimeout = 720 * time.Second

func main() {
	srv := &mcpServer{
		sessionManagerURL: requireEnv("KORA_SESSION_MANAGER_URL"),
		runID:             requireEnv("KORA_RUN_ID"),
		httpClient:        &http.Client{Timeout: provisionTimeout},
	}
	if err := srv.serve(os.Stdin, os.Stdout); err != nil && !errors.Is(err, io.EOF) {
		log.Fatalf("kora-runtime-mcp: %v", err)
	}
}

func requireEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		log.Fatalf("kora-runtime-mcp: %s is required", key)
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
				"name":    "kora-runtime",
				"version": "1.0.0",
			},
			"capabilities": map[string]any{
				"tools": map[string]any{},
			},
		},
	}
}

// environmentProperty is the optional or required environment argument.
var environmentProperty = map[string]any{
	"type":        "string",
	"description": "An environment slug from list_environments.",
}

func (s *mcpServer) handleToolsList(req jsonRPCRequest) jsonRPCResponse {
	return jsonRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: map[string]any{
			"tools": []map[string]any{
				{
					"name":        "list_environments",
					"description": "Lists the environments this session can use with their slug, name, description, image and size, and marks the default and the current sandbox's environment.",
					"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}, "required": []string{}},
				},
				{
					"name":        "provision_sandbox",
					"description": "Starts a sandbox with network access and the build toolchain for this session. Commands run only in a sandbox, so provision one before the first command. `environment` selects one from list_environments, and the agent's default applies when it is omitted.",
					"inputSchema": map[string]any{
						"type":       "object",
						"properties": map[string]any{"environment": environmentProperty},
						"required":   []string{},
					},
				},
				{
					"name":        "replace_environment",
					"description": "Replaces the running sandbox with one in another environment. Files in the workspace stay, and running commands, build caches and installed tools are lost.",
					"inputSchema": map[string]any{
						"type":       "object",
						"properties": map[string]any{"environment": environmentProperty},
						"required":   []string{"environment"},
					},
				},
			},
		},
	}
}

func (s *mcpServer) handleToolsCall(req jsonRPCRequest) jsonRPCResponse {
	var params struct {
		Name      string `json:"name"`
		Arguments struct {
			Environment string `json:"environment"`
		} `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return errResponse(req.ID, -32602, "invalid params")
	}
	env := params.Arguments.Environment

	var text string
	var err error
	switch params.Name {
	case "list_environments":
		text, err = s.listEnvironments()
	case "provision_sandbox":
		err = s.post("provision-sandbox", env)
		text = "Sandbox provisioned. Bash commands will now execute in the sandbox environment."
	case "replace_environment":
		err = s.post("replace-environment", env)
		text = fmt.Sprintf("Sandbox replaced. Bash commands now execute in the %s environment.", env)
	default:
		return errResponse(req.ID, -32601, fmt.Sprintf("unknown tool: %s", params.Name))
	}
	if err != nil {
		// The failure is a tool result with isError. Its text tells the model
		// what survived and what to do next.
		return jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: map[string]any{
				"content": []map[string]any{{"type": "text", "text": fmt.Sprintf("%s failed: %v", params.Name, err)}},
				"isError": true,
			},
		}
	}
	return jsonRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  map[string]any{"content": []map[string]any{{"type": "text", "text": text}}},
	}
}

// post sends a sandbox request for this run to the session-manager.
func (s *mcpServer) post(action, environment string) error {
	body, err := json.Marshal(map[string]string{"environment": environment})
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}
	_, err = s.call(http.MethodPost, action, bytes.NewReader(body))
	return err
}

// environment is one entry of the session-manager's environment list.
type environment struct {
	Slug        string `json:"slug"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Image       string `json:"image"`
	Size        struct {
		CPU    string `json:"cpu"`
		Memory string `json:"memory"`
		Disk   string `json:"disk"`
	} `json:"size"`
	Default bool `json:"default"`
	Current bool `json:"current"`
}

// listEnvironments renders the run's environments as one line each.
func (s *mcpServer) listEnvironments() (string, error) {
	raw, err := s.call(http.MethodGet, "environments", nil)
	if err != nil {
		return "", err
	}
	var list struct {
		Environments []environment `json:"environments"`
	}
	if err := json.Unmarshal(raw, &list); err != nil {
		return "", fmt.Errorf("decode environments: %w", err)
	}
	var b strings.Builder
	for _, e := range list.Environments {
		var marks []string
		if e.Default {
			marks = append(marks, "default")
		}
		if e.Current {
			marks = append(marks, "current sandbox")
		}
		// The previous release's kora-agents sends only name, which is then the slug.
		slug := e.Slug
		if slug == "" {
			slug = e.Name
		}
		fmt.Fprintf(&b, "- %s", slug)
		if name := strings.TrimSpace(e.Name); name != "" && e.Slug != "" {
			fmt.Fprintf(&b, " %q", name)
		}
		if len(marks) > 0 {
			fmt.Fprintf(&b, " (%s)", strings.Join(marks, ", "))
		}
		b.WriteString(":")
		if desc := strings.TrimSuffix(strings.TrimSpace(e.Description), "."); desc != "" {
			fmt.Fprintf(&b, " %s.", desc)
		}
		fmt.Fprintf(&b, " Image %s. %s CPU, %s memory, %s local disk.\n", e.Image, e.Size.CPU, e.Size.Memory, e.Size.Disk)
	}
	return b.String(), nil
}

// call sends one request for this run to the session-manager and returns the
// response body. A non-2xx answer returns an error with its message.
func (s *mcpServer) call(method, action string, body io.Reader) ([]byte, error) {
	url := fmt.Sprintf("%s/internal/session/%s/%s", s.sessionManagerURL, s.runID, action)
	req, err := http.NewRequestWithContext(context.Background(), method, url, body)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Kora-Run-Token", os.Getenv("KORA_RUN_TOKEN"))
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		var apiErr struct {
			Message string `json:"message"`
		}
		if json.Unmarshal(raw, &apiErr) == nil && apiErr.Message != "" {
			return nil, errors.New(apiErr.Message)
		}
		return nil, fmt.Errorf("session-manager returned %d: %s", resp.StatusCode, string(raw))
	}
	return raw, nil
}

func errResponse(id any, code int, msg string) jsonRPCResponse {
	return jsonRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &rpcError{Code: code, Message: msg},
	}
}
