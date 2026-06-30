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

	call := func(method string, params any) map[string]any {
		t.Helper()
		body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
		req, _ := http.NewRequest(http.MethodPost, gw.URL+"/mcp", bytes.NewReader(body))
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
}
