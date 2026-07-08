package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestCallerTokenPassthrough proves the load-bearing claim of the kit: the
// caller's Authorization bearer reaches a tool handler's ctx via CallerToken,
// so handlers can forward it to downstream clients. It exercises the real HTTP
// + Streamable-HTTP path, not a shortcut.
func TestCallerTokenPassthrough(t *testing.T) {
	srv := NewServer("test", "v0.0.1")
	// A tool that echoes back whatever token the kit surfaced in its context.
	srv.AddTool(&sdk.Tool{Name: "whoami", InputSchema: json.RawMessage(`{"type":"object"}`)},
		func(ctx context.Context, _ *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
			return &sdk.CallToolResult{
				Content: []sdk.Content{&sdk.TextContent{Text: "token=" + CallerToken(ctx)}},
			}, nil
		})

	ts := httptest.NewServer(NewController("/mcp", srv).Handler())
	defer ts.Close()

	call := func(method string, params any) map[string]any {
		t.Helper()
		body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
		req, _ := http.NewRequest(http.MethodPost, ts.URL+"/mcp", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		req.Header.Set("Authorization", "Bearer caller-token-xyz")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s: %v", method, err)
		}
		defer func() { _ = resp.Body.Close() }()
		buf := new(bytes.Buffer)
		_, _ = buf.ReadFrom(resp.Body)
		return decodeRPC(t, method, buf.String())
	}

	// Stateless Streamable HTTP still requires initialize to open the temp session.
	if got := call("initialize", map[string]any{
		"protocolVersion": "2025-06-18",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "test", "version": "0"},
	}); got["result"] == nil {
		t.Fatalf("initialize returned no result: %v", got)
	}

	got := call("tools/call", map[string]any{"name": "whoami", "arguments": map[string]any{}})
	result, _ := got["result"].(map[string]any)
	if result == nil {
		t.Fatalf("tools/call returned no result: %v", got)
	}
	content, _ := result["content"].([]any)
	if len(content) == 0 {
		t.Fatalf("no content in result: %v", result)
	}
	text, _ := content[0].(map[string]any)["text"].(string)
	if text != "token=caller-token-xyz" {
		t.Fatalf("caller token did not propagate to the tool handler; got %q", text)
	}
}

// decodeRPC parses a Streamable HTTP response body, which is either a bare JSON
// object or an SSE stream of `data: {json}` frames.
func decodeRPC(t *testing.T, method, body string) map[string]any {
	t.Helper()
	body = strings.TrimSpace(body)
	if strings.HasPrefix(body, "{") {
		var m map[string]any
		if err := json.Unmarshal([]byte(body), &m); err != nil {
			t.Fatalf("%s: bad json body %q: %v", method, body, err)
		}
		return m
	}
	for line := range strings.SplitSeq(body, "\n") {
		if data, ok := strings.CutPrefix(strings.TrimSpace(line), "data:"); ok {
			var m map[string]any
			if json.Unmarshal([]byte(strings.TrimSpace(data)), &m) == nil && m["id"] != nil {
				return m
			}
		}
	}
	t.Fatalf("%s: could not decode response body %q", method, body)
	return nil
}

func TestScopePath(t *testing.T) {
	for name, want := range map[string][]string{
		"agents.create":        {"agents", "create"},
		"policy.grants.create": {"policy", "grants", "create"},
		"models":               {"models"},
	} {
		if got := ScopePath(name); !reflect.DeepEqual(got, want) {
			t.Errorf("ScopePath(%q) = %v, want %v", name, got, want)
		}
	}
}
