package orbitmcp

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/clear-route/orbit/internal/platform/clients/orbitjson"
	"github.com/clear-route/orbit/internal/platform/logging"
	mcpkit "github.com/clear-route/orbit/internal/platform/mcp"
)

// TestAgentWriteToolsForwardMethodAndBody exercises the write vertical: each
// write tool must hit the right HTTP method + path, carry the caller's token,
// and forward the typed input as the JSON body.
func TestAgentWriteToolsForwardMethodAndBody(t *testing.T) {
	var gotMethod, gotPath, gotAuth string
	var gotBody map[string]any
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath, gotAuth = r.Method, r.URL.Path, r.Header.Get("Authorization")
		gotBody = nil
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"at-9","slug":"reviewer"}`))
	}))
	defer backend.Close()

	srv := NewServer(Backends{Agents: backend.URL}, orbitjson.NewClient(logging.NewLogger()))
	gw := httptest.NewServer(mcpkit.NewController("/mcp", srv).Handler())
	defer gw.Close()
	call := mcpCaller(t, gw.URL)
	call("initialize", map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "t", "version": "0"}})

	t.Run("create POSTs the typed body", func(t *testing.T) {
		call("tools/call", map[string]any{"name": "agents.create", "arguments": map[string]any{"name": "Reviewer", "slug": "reviewer"}})
		if gotMethod != http.MethodPost || gotPath != "/api/v1/agent-types" {
			t.Errorf("create hit %s %s, want POST /api/v1/agent-types", gotMethod, gotPath)
		}
		if gotAuth != "Bearer caller-abc" {
			t.Errorf("create did not forward the caller token; got %q", gotAuth)
		}
		if gotBody["name"] != "Reviewer" || gotBody["slug"] != "reviewer" {
			t.Errorf("create body not forwarded: %v", gotBody)
		}
	})

	t.Run("update PATCHes by id", func(t *testing.T) {
		desc := "now does reviews"
		call("tools/call", map[string]any{"name": "agents.update", "arguments": map[string]any{"agent_type_id": "at-9", "description": desc}})
		if gotMethod != http.MethodPatch || gotPath != "/api/v1/agent-types/at-9" {
			t.Errorf("update hit %s %s, want PATCH /api/v1/agent-types/at-9", gotMethod, gotPath)
		}
		if gotBody["description"] != desc {
			t.Errorf("update body not forwarded: %v", gotBody)
		}
	})

	t.Run("delete DELETEs by id", func(t *testing.T) {
		call("tools/call", map[string]any{"name": "agents.delete", "arguments": map[string]any{"agent_type_id": "at-9"}})
		if gotMethod != http.MethodDelete || gotPath != "/api/v1/agent-types/at-9" {
			t.Errorf("delete hit %s %s, want DELETE /api/v1/agent-types/at-9", gotMethod, gotPath)
		}
	})
}

// TestCreateToolSchemaSurfacesEnumAndModelHint asserts the agents.create input
// schema (a) constrains framework to the closed enum and (b) points model_id at
// the models.list tool — so a client/agent sees valid options up front.
func TestCreateToolSchemaSurfacesEnumAndModelHint(t *testing.T) {
	srv := NewServer(Backends{}, orbitjson.NewClient(logging.NewLogger()))
	gw := httptest.NewServer(mcpkit.NewController("/mcp", srv).Handler())
	defer gw.Close()
	call := mcpCaller(t, gw.URL)
	call("initialize", map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "t", "version": "0"}})

	list := call("tools/list", map[string]any{})
	tools, _ := list["result"].(map[string]any)["tools"].([]any)
	var create map[string]any
	for _, tt := range tools {
		if m, _ := tt.(map[string]any); m["name"] == "agents.create" {
			create = m
		}
	}
	if create == nil {
		t.Fatal("agents.create not in tools/list")
	}
	props, _ := create["inputSchema"].(map[string]any)["properties"].(map[string]any)

	enum, _ := props["framework"].(map[string]any)["enum"].([]any)
	got := make([]string, 0, len(enum))
	for _, v := range enum {
		got = append(got, v.(string))
	}
	if want := "claude-code,codex,opencode"; strings.Join(got, ",") != want {
		t.Errorf("framework enum = %v, want [%s]", got, want)
	}

	desc, _ := props["model_id"].(map[string]any)["description"].(string)
	if !strings.Contains(desc, "models.list") {
		t.Errorf("model_id description should point at models.list; got %q", desc)
	}
}

// TestDeriveGrantScopes covers the BFF-mirrored grant-scope derivation.
// TestCreateForwardsModelWithoutDerivingScopes proves the MCP no longer derives
// grant_scopes: agents.create forwards model_id and does NOT inject grant_scopes —
// the agents service is the single source of truth (2026-06-30 amendment).
func TestCreateForwardsModelWithoutDerivingScopes(t *testing.T) {
	var gotBody map[string]any
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody = nil
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"at-1"}`))
	}))
	defer backend.Close()

	srv := NewServer(Backends{Agents: backend.URL}, orbitjson.NewClient(logging.NewLogger()))
	gw := httptest.NewServer(mcpkit.NewController("/mcp", srv).Handler())
	defer gw.Close()
	call := mcpCaller(t, gw.URL)
	call("initialize", map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "t", "version": "0"}})

	call("tools/call", map[string]any{"name": "agents.create", "arguments": map[string]any{
		"name": "Reviewer", "slug": "reviewer", "model_id": "anthropic/claude-sonnet-4-6",
	}})

	if gotBody["model_id"] != "anthropic/claude-sonnet-4-6" {
		t.Errorf("model_id not forwarded: %v", gotBody["model_id"])
	}
	if _, ok := gotBody["grant_scopes"]; ok {
		t.Errorf("MCP must not derive grant_scopes; got %v", gotBody["grant_scopes"])
	}
}

// TestConnectorsDiscoveryAndForwarding covers the connectors surface: the
// agents.connectors discovery tool hits the connections catalog, and agents.create
// forwards the nested connectors + connectivity intent for the backend to compile.
func TestConnectorsDiscoveryAndForwarding(t *testing.T) {
	var gotPath string
	var gotBody map[string]any
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotBody = nil
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"connections":[{"name":"github"}]}`))
	}))
	defer backend.Close()

	srv := NewServer(Backends{Agents: backend.URL}, orbitjson.NewClient(logging.NewLogger()))
	gw := httptest.NewServer(mcpkit.NewController("/mcp", srv).Handler())
	defer gw.Close()
	call := mcpCaller(t, gw.URL)
	call("initialize", map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "t", "version": "0"}})

	call("tools/call", map[string]any{"name": "agents.connectors", "arguments": map[string]any{}})
	if gotPath != "/api/v1/connections" {
		t.Errorf("agents.connectors hit %q, want /api/v1/connections", gotPath)
	}

	call("tools/call", map[string]any{"name": "agents.create", "arguments": map[string]any{
		"name": "C", "slug": "c", "model_id": "anthropic/claude-sonnet-4-6",
		"connectors":   []any{map[string]any{"connection": "github", "actions": []any{"repo:read"}}},
		"connectivity": map[string]any{"mode": "whitelist", "hosts": []any{"api.github.com"}},
	}})
	if gotPath != "/api/v1/agent-types" {
		t.Fatalf("create hit %q, want /api/v1/agent-types", gotPath)
	}
	conns, _ := gotBody["connectors"].([]any)
	if len(conns) != 1 || conns[0].(map[string]any)["connection"] != "github" {
		t.Errorf("connectors not forwarded: %v", gotBody["connectors"])
	}
	if c, _ := gotBody["connectivity"].(map[string]any); c["mode"] != "whitelist" {
		t.Errorf("connectivity not forwarded: %v", gotBody["connectivity"])
	}
}

// TestToolForwardsCallerTokenAndReturnsJSON exercises the whole Phase 1 vertical:
// a tools/call over Streamable HTTP → the kit's caller-token passthrough → the
// orbitjson forwarder → a backend, asserting (a) the backend received the
// caller's bearer, (b) it was the right path, and (c) the backend JSON is
// returned to the caller verbatim.
func TestToolForwardsCallerTokenAndReturnsJSON(t *testing.T) {
	var gotAuth, gotPath string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"agent_types":[{"slug":"reviewer"}]}`))
	}))
	defer backend.Close()

	srv := NewServer(Backends{Agents: backend.URL}, orbitjson.NewClient(logging.NewLogger()))
	gw := httptest.NewServer(mcpkit.NewController("/mcp", srv).Handler())
	defer gw.Close()

	call := mcpCaller(t, gw.URL)
	call("initialize", map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "t", "version": "0"}})
	got := call("tools/call", map[string]any{"name": "agents.list", "arguments": map[string]any{}})

	if gotAuth != "Bearer caller-abc" {
		t.Errorf("backend did not receive the caller's token; got Authorization %q", gotAuth)
	}
	if gotPath != "/api/v1/agent-types" {
		t.Errorf("tool called wrong backend path: %q", gotPath)
	}
	result, _ := got["result"].(map[string]any)
	content, _ := result["content"].([]any)
	if len(content) == 0 {
		t.Fatalf("no content in tool result: %v", got)
	}
	text, _ := content[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, `"reviewer"`) {
		t.Errorf("backend JSON not returned to caller; got %q", text)
	}
	// The payload must live in text content only. If the tool emits a
	// structuredContent object, clients that prefer it (e.g. Claude Code) surface
	// that instead of the text — which for our text-only tools would be empty.
	if sc, ok := result["structuredContent"]; ok {
		t.Errorf("result must not carry structuredContent (clients surface it over text); got %v", sc)
	}
}

// mcpCaller returns a function that issues a JSON-RPC request to the MCP gateway
// over Streamable HTTP with a fixed caller token, parsing the SSE `data:` frame
// (or a bare JSON body) into the response map.
func mcpCaller(t *testing.T, baseURL string) func(method string, params any) map[string]any {
	return func(method string, params any) map[string]any {
		t.Helper()
		body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
		req, _ := http.NewRequest(http.MethodPost, baseURL+"/mcp", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		req.Header.Set("Authorization", "Bearer caller-abc")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s: %v", method, err)
		}
		defer func() { _ = resp.Body.Close() }()
		buf := new(bytes.Buffer)
		_, _ = buf.ReadFrom(resp.Body)
		for line := range strings.SplitSeq(buf.String(), "\n") {
			if data, ok := strings.CutPrefix(strings.TrimSpace(line), "data:"); ok {
				var m map[string]any
				if json.Unmarshal([]byte(strings.TrimSpace(data)), &m) == nil && m["id"] != nil {
					return m
				}
			}
		}
		var m map[string]any
		_ = json.Unmarshal(buf.Bytes(), &m)
		return m
	}
}
