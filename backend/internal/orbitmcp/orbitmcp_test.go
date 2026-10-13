package orbitmcp

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
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
		call("tools/call", map[string]any{"name": "agents.create", "arguments": map[string]any{"name": "Reviewer", "slug": "reviewer", "model_id": "anthropic/claude-sonnet-5"}})
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

// TestGuardrailsForwarded proves agents.create and agents.update pass the
// loop-guard thresholds through, and agents.get returns them as the API does.
func TestGuardrailsForwarded(t *testing.T) {
	var gotBody map[string]any
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody = nil
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"at-1","guardrails":{"repeated_call_warn":3,"repeated_call_pause":5}}`))
	}))
	defer backend.Close()

	srv := NewServer(Backends{Agents: backend.URL}, orbitjson.NewClient(logging.NewLogger()))
	gw := httptest.NewServer(mcpkit.NewController("/mcp", srv).Handler())
	defer gw.Close()
	call := mcpCaller(t, gw.URL)
	call("initialize", map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "t", "version": "0"}})

	thresholds := map[string]any{"repeated_call_warn": 3, "repeated_call_pause": 5}
	for _, tc := range []struct {
		tool string
		args map[string]any
	}{
		{"agents.create", map[string]any{"name": "Reviewer", "slug": "reviewer", "model_id": "m", "guardrails": thresholds}},
		{"agents.update", map[string]any{"agent_type_id": "at-1", "guardrails": thresholds}},
	} {
		call("tools/call", map[string]any{"name": tc.tool, "arguments": tc.args})
		g, _ := gotBody["guardrails"].(map[string]any)
		if g["repeated_call_warn"] != float64(3) || g["repeated_call_pause"] != float64(5) || len(g) != 2 {
			t.Errorf("%s forwarded guardrails %v", tc.tool, gotBody["guardrails"])
		}
	}
	got := call("tools/call", map[string]any{"name": "agents.get", "arguments": map[string]any{"agent_type_id": "at-1"}})
	if raw, _ := json.Marshal(got); !strings.Contains(string(raw), "repeated_call_pause") {
		t.Errorf("agents.get did not return the guardrails: %v", got)
	}
}

// TestTriggerToolsUseTriggersBackend guards the fix for the trigger tools
// 404ing in dev: they must call the orbit-triggers backend, not identity. The
// trigger-sources/triggers/deliveries endpoints moved out of the identity binary
// into orbit-triggers; wiring them to b.Identity sent every trigger tool to a
// service that no longer serves the path (a plain 404).
func TestTriggerToolsUseTriggersBackend(t *testing.T) {
	var gotPath string
	triggers := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"sources":[]}`))
	}))
	defer triggers.Close()

	// Identity fails the test if a trigger tool falls back to it.
	identity := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("trigger tool hit identity backend at %s; must use the triggers backend", r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
	}))
	defer identity.Close()

	srv := NewServer(Backends{Identity: identity.URL, Triggers: triggers.URL}, orbitjson.NewClient(logging.NewLogger()))
	gw := httptest.NewServer(mcpkit.NewController("/mcp", srv).Handler())
	defer gw.Close()
	call := mcpCaller(t, gw.URL)
	call("initialize", map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "t", "version": "0"}})

	call("tools/call", map[string]any{"name": "triggers.sources.list", "arguments": map[string]any{}})

	if gotPath != "/api/v1/trigger-sources" {
		t.Errorf("triggers.sources.list hit %q, want /api/v1/trigger-sources on the triggers backend", gotPath)
	}
}

// TestTriggerProviderToolsAndSenderRestriction covers the provider catalog
// tools and sender_restriction forwarding on trigger create and update.
func TestTriggerProviderToolsAndSenderRestriction(t *testing.T) {
	var gotPath, gotQuery string
	var gotBody map[string]any
	triggers := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotQuery = r.URL.Path, r.URL.RawQuery
		gotBody = nil
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer triggers.Close()

	srv := NewServer(Backends{Triggers: triggers.URL}, orbitjson.NewClient(logging.NewLogger()))
	gw := httptest.NewServer(mcpkit.NewController("/mcp", srv).Handler())
	defer gw.Close()
	call := mcpCaller(t, gw.URL)
	call("initialize", map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "t", "version": "0"}})

	call("tools/call", map[string]any{"name": "triggers.providers.event_types", "arguments": map[string]any{"provider": "github"}})
	if gotPath != "/api/v1/trigger-providers/github/event-types" {
		t.Errorf("event_types hit %q", gotPath)
	}

	call("tools/call", map[string]any{"name": "triggers.providers.fields", "arguments": map[string]any{"provider": "github", "event_type": "issue_comment"}})
	if gotPath != "/api/v1/trigger-providers/github/fields" || gotQuery != "event_type=issue_comment" {
		t.Errorf("fields hit %q?%q", gotPath, gotQuery)
	}

	gotPath = ""
	call("tools/call", map[string]any{"name": "triggers.create", "arguments": map[string]any{"source_id": "src-1", "name": "t"}})
	if gotPath != "" {
		t.Errorf("create without event_type reached the backend at %q", gotPath)
	}

	restriction := map[string]any{"logins": []any{"octocat"}, "associations": []any{"MEMBER"}}
	call("tools/call", map[string]any{"name": "triggers.create", "arguments": map[string]any{
		"source_id": "src-1", "name": "t", "event_type": "issue_comment", "sender_restriction": restriction,
	}})
	if gotPath != "/api/v1/triggers" {
		t.Errorf("create hit %q", gotPath)
	}
	if got, _ := gotBody["sender_restriction"].(map[string]any); got["associations"] == nil || got["logins"] == nil {
		t.Errorf("create did not forward sender_restriction: %v", gotBody["sender_restriction"])
	}

	call("tools/call", map[string]any{"name": "triggers.update", "arguments": map[string]any{"id": "t-1", "sender_restriction": restriction}})
	if got, _ := gotBody["sender_restriction"].(map[string]any); got["logins"] == nil {
		t.Errorf("update did not forward sender_restriction: %v", gotBody["sender_restriction"])
	}

	contains := map[string]any{"comment.body": []any{"@kora-agent"}}
	call("tools/call", map[string]any{"name": "triggers.create", "arguments": map[string]any{
		"source_id": "src-1", "name": "t", "event_type": "issue_comment", "match_contains": contains,
	}})
	if got, _ := gotBody["match_contains"].(map[string]any); got["comment.body"] == nil {
		t.Errorf("create did not forward match_contains: %v", gotBody["match_contains"])
	}
	call("tools/call", map[string]any{"name": "triggers.update", "arguments": map[string]any{"id": "t-1", "match_contains": map[string]any{}}})
	if got, ok := gotBody["match_contains"].(map[string]any); !ok || len(got) != 0 {
		t.Errorf("update did not forward an empty match_contains: %v", gotBody["match_contains"])
	}
}

// TestTriggerKindsCreateUpdateAndPreview covers the per-kind create validation
// and request bodies, the schedule/manual config update mapping, and the
// schedule preview tool.
func TestTriggerKindsCreateUpdateAndPreview(t *testing.T) {
	var gotPath, gotMethod string
	var gotBody map[string]any
	previewStatus := http.StatusOK
	triggers := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.Path, r.Method
		gotBody = nil
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/v1/triggers/schedule-preview" && previewStatus != http.StatusOK {
			w.WriteHeader(previewStatus)
			_, _ = w.Write([]byte(`{"code":"validation_error","message":"invalid cron expression"}`))
			return
		}
		_, _ = w.Write([]byte(`{"next_fires":[{"at":"2026-01-01T09:00:00Z","label":"Thu 1 Jan 2026, 09:00"}]}`))
	}))
	defer triggers.Close()

	srv := NewServer(Backends{Triggers: triggers.URL}, orbitjson.NewClient(logging.NewLogger()))
	gw := httptest.NewServer(mcpkit.NewController("/mcp", srv).Handler())
	defer gw.Close()
	call := mcpCaller(t, gw.URL)
	call("initialize", map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "t", "version": "0"}})

	callTool := func(name string, args map[string]any) map[string]any {
		t.Helper()
		gotPath, gotMethod, gotBody = "", "", nil
		resp := call("tools/call", map[string]any{"name": name, "arguments": args})
		result, _ := resp["result"].(map[string]any)
		return result
	}
	isToolError := func(result map[string]any) bool {
		isErr, _ := result["isError"].(bool)
		return isErr
	}
	schedule := map[string]any{"cron_expression": "0 9 * * 1-5", "timezone": "Australia/Sydney", "catch_up": true}

	t.Run("create validation per kind", func(t *testing.T) {
		rejected := map[string]map[string]any{
			"webhook without source_id":   {"name": "t", "event_type": "push"},
			"webhook without event_type":  {"name": "t", "source_id": "src-1"},
			"explicit webhook, no source": {"kind": "webhook", "name": "t", "event_type": "push"},
			"scheduled without schedule":  {"kind": "scheduled", "name": "t"},
			"unknown kind":                {"kind": "cron", "name": "t"},
			"manual without name":         {"kind": "manual"},
			"scheduled without name":      {"kind": "scheduled", "schedule_config": schedule},
		}
		for label, args := range rejected {
			result := callTool("triggers.create", args)
			if gotPath != "" {
				t.Errorf("%s: reached the backend at %q", label, gotPath)
			}
			if !isToolError(result) {
				t.Errorf("%s: want a tool error, got %v", label, result)
			}
		}
	})

	t.Run("webhook is the default kind", func(t *testing.T) {
		callTool("triggers.create", map[string]any{"name": "t", "source_id": "src-1", "event_type": "push"})
		if gotMethod != http.MethodPost || gotPath != "/api/v1/triggers" {
			t.Fatalf("create hit %s %q", gotMethod, gotPath)
		}
		if gotBody["kind"] != "webhook" || gotBody["source_id"] != "src-1" || gotBody["event_type"] != "push" {
			t.Errorf("webhook body = %v", gotBody)
		}
		if _, ok := gotBody["schedule_config"]; ok {
			t.Errorf("webhook body carries schedule_config: %v", gotBody)
		}
	})

	t.Run("scheduled needs no source or event type", func(t *testing.T) {
		result := callTool("triggers.create", map[string]any{
			"kind": "scheduled", "name": "nightly", "agent_slug": "reporter", "schedule_config": schedule,
		})
		if isToolError(result) {
			t.Fatalf("scheduled create failed: %v", result)
		}
		if gotPath != "/api/v1/triggers" || gotBody["kind"] != "scheduled" {
			t.Fatalf("create hit %q with %v", gotPath, gotBody)
		}
		sc, _ := gotBody["schedule_config"].(map[string]any)
		if sc["cron_expression"] != "0 9 * * 1-5" || sc["timezone"] != "Australia/Sydney" || sc["catch_up"] != true {
			t.Errorf("schedule_config = %v", gotBody["schedule_config"])
		}
		if _, ok := gotBody["manual_config"]; ok {
			t.Errorf("scheduled body carries manual_config: %v", gotBody)
		}
	})

	t.Run("manual forwards its inputs", func(t *testing.T) {
		manual := map[string]any{"inputs": []any{
			map[string]any{"name": "ticket", "type": "text", "required": true},
			map[string]any{"name": "env", "type": "choice", "choices": []any{"dev", "prod"}, "default": "dev"},
		}}
		result := callTool("triggers.create", map[string]any{
			"kind": "manual", "name": "triage", "agent_slug": "triager",
			"prompt_template": "Triage {{ var.ticket }} in {{ var.env }}", "manual_config": manual,
		})
		if isToolError(result) {
			t.Fatalf("manual create failed: %v", result)
		}
		if gotBody["kind"] != "manual" {
			t.Fatalf("kind = %v", gotBody["kind"])
		}
		mc, _ := gotBody["manual_config"].(map[string]any)
		inputs, _ := mc["inputs"].([]any)
		if len(inputs) != 2 {
			t.Fatalf("manual_config = %v", gotBody["manual_config"])
		}
		env, _ := inputs[1].(map[string]any)
		if env["name"] != "env" || env["default"] != "dev" || len(env["choices"].([]any)) != 2 {
			t.Errorf("second input = %v", env)
		}
		if _, ok := gotBody["schedule_config"]; ok {
			t.Errorf("manual body carries schedule_config: %v", gotBody)
		}
	})

	t.Run("update maps schedule and manual config", func(t *testing.T) {
		callTool("triggers.update", map[string]any{"id": "t-1", "schedule_config": map[string]any{
			"cron_expression": "30 6 * * *", "timezone": "Europe/London",
		}})
		if gotMethod != http.MethodPatch || gotPath != "/api/v1/triggers/t-1" {
			t.Fatalf("update hit %s %q", gotMethod, gotPath)
		}
		sc, _ := gotBody["schedule_config"].(map[string]any)
		if sc["cron_expression"] != "30 6 * * *" || sc["timezone"] != "Europe/London" {
			t.Errorf("schedule_config = %v", gotBody["schedule_config"])
		}
		if _, ok := gotBody["manual_config"]; ok {
			t.Errorf("update carries an unsupplied manual_config: %v", gotBody)
		}

		callTool("triggers.update", map[string]any{"id": "t-2", "manual_config": map[string]any{
			"inputs": []any{map[string]any{"name": "ticket", "type": "text"}},
		}})
		if _, ok := gotBody["manual_config"].(map[string]any); !ok {
			t.Errorf("update did not forward manual_config: %v", gotBody)
		}
		if _, ok := gotBody["schedule_config"]; ok {
			t.Errorf("update carries an unsupplied schedule_config: %v", gotBody)
		}
	})

	t.Run("update body omits unsupplied configs", func(t *testing.T) {
		name := "renamed"
		body := updateTriggerBody(updateTriggerArgs{ID: "t-1", Name: &name})
		if len(body) != 1 || body["name"] != name {
			t.Errorf("body = %v, want only name", body)
		}
	})

	t.Run("schedule preview", func(t *testing.T) {
		result := callTool("triggers.schedule_preview", map[string]any{"schedule_config": schedule})
		if isToolError(result) {
			t.Fatalf("preview failed: %v", result)
		}
		if gotMethod != http.MethodPost || gotPath != "/api/v1/triggers/schedule-preview" {
			t.Fatalf("preview hit %s %q", gotMethod, gotPath)
		}
		sc, _ := gotBody["schedule_config"].(map[string]any)
		if sc["cron_expression"] != "0 9 * * 1-5" || sc["timezone"] != "Australia/Sydney" {
			t.Errorf("preview body = %v", gotBody)
		}

		result = callTool("triggers.schedule_preview", map[string]any{})
		if gotPath != "" || !isToolError(result) {
			t.Errorf("preview without schedule_config: path %q, result %v", gotPath, result)
		}

		previewStatus = http.StatusBadRequest
		result = callTool("triggers.schedule_preview", map[string]any{"schedule_config": map[string]any{
			"cron_expression": "not a cron", "timezone": "UTC",
		}})
		if !isToolError(result) {
			t.Errorf("invalid expression: want a tool error, got %v", result)
		}
	})
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

// TestRunToolsForwardMethodAndPath covers the runs lifecycle: each tool hits the
// right method + path under /api/v1/runs and forwards its body.
func TestRunToolsForwardMethodAndPath(t *testing.T) {
	var gotMethod, gotPath string
	var gotQuery url.Values
	var gotBody map[string]any
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath, gotQuery = r.Method, r.URL.Path, r.URL.Query()
		gotBody = nil
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"run-1"}`))
	}))
	defer backend.Close()

	srv := NewServer(Backends{Agents: backend.URL}, orbitjson.NewClient(logging.NewLogger()))
	gw := httptest.NewServer(mcpkit.NewController("/mcp", srv).Handler())
	defer gw.Close()
	call := mcpCaller(t, gw.URL)
	call("initialize", map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "t", "version": "0"}})

	t.Run("start POSTs agent_type_id", func(t *testing.T) {
		call("tools/call", map[string]any{"name": "runs.start", "arguments": map[string]any{"agent_type_id": "at-9", "initial_message": "hi"}})
		if gotMethod != http.MethodPost || gotPath != "/api/v1/runs" {
			t.Errorf("start hit %s %s, want POST /api/v1/runs", gotMethod, gotPath)
		}
		if gotBody["agent_type_id"] != "at-9" || gotBody["initial_message"] != "hi" {
			t.Errorf("start body not forwarded: %v", gotBody)
		}
	})

	t.Run("send_message POSTs content to /messages", func(t *testing.T) {
		call("tools/call", map[string]any{"name": "runs.send_message", "arguments": map[string]any{"run_id": "run-1", "content": "go"}})
		if gotMethod != http.MethodPost || gotPath != "/api/v1/runs/run-1/messages" {
			t.Errorf("send_message hit %s %s", gotMethod, gotPath)
		}
		if gotBody["content"] != "go" {
			t.Errorf("send_message content not forwarded: %v", gotBody)
		}
	})

	t.Run("interrupt POSTs to /interrupt", func(t *testing.T) {
		call("tools/call", map[string]any{"name": "runs.interrupt", "arguments": map[string]any{"run_id": "run-1"}})
		if gotMethod != http.MethodPost || gotPath != "/api/v1/runs/run-1/interrupt" {
			t.Errorf("interrupt hit %s %s", gotMethod, gotPath)
		}
	})

	t.Run("terminate DELETEs the run", func(t *testing.T) {
		call("tools/call", map[string]any{"name": "runs.terminate", "arguments": map[string]any{"run_id": "run-1"}})
		if gotMethod != http.MethodDelete || gotPath != "/api/v1/runs/run-1" {
			t.Errorf("terminate hit %s %s", gotMethod, gotPath)
		}
	})

	t.Run("list forwards filters and the page position", func(t *testing.T) {
		call("tools/call", map[string]any{"name": "runs.list", "arguments": map[string]any{
			"agent_type_id": "at-9", "state": "pending,active", "cursor": "abc", "limit": 20,
		}})
		if gotMethod != http.MethodGet || gotPath != "/api/v1/runs" {
			t.Errorf("list hit %s %s", gotMethod, gotPath)
		}
		want := url.Values{"agent_type_id": {"at-9"}, "state": {"pending,active"}, "cursor": {"abc"}, "limit": {"20"}}
		if !reflect.DeepEqual(gotQuery, want) {
			t.Errorf("list query = %v, want %v", gotQuery, want)
		}
	})

	t.Run("events GETs the paginated event log", func(t *testing.T) {
		call("tools/call", map[string]any{"name": "runs.events", "arguments": map[string]any{"run_id": "run-1", "after_seq": 12, "limit": 25}})
		if gotMethod != http.MethodGet || gotPath != "/api/v1/runs/run-1/events" {
			t.Errorf("events hit %s %s", gotMethod, gotPath)
		}
		if gotQuery.Get("after_seq") != "12" || gotQuery.Get("limit") != "25" {
			t.Errorf("events pagination not forwarded: %v", gotQuery)
		}
	})
}

// TestPolicyWriteToolsForwardMethodAndPath covers the policy grant/role writes:
// grants.create POSTs to /grants, roles.upsert PUTs to /roles, and the grant
// principal_type enum is surfaced.
func TestPolicyWriteToolsForwardMethodAndPath(t *testing.T) {
	var gotMethod, gotPath string
	var gotBody map[string]any
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		gotBody = nil
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"grant-1"}`))
	}))
	defer backend.Close()

	srv := NewServer(Backends{Policy: backend.URL}, orbitjson.NewClient(logging.NewLogger()))
	gw := httptest.NewServer(mcpkit.NewController("/mcp", srv).Handler())
	defer gw.Close()
	call := mcpCaller(t, gw.URL)
	call("initialize", map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "t", "version": "0"}})

	t.Run("grants.create POSTs to /grants", func(t *testing.T) {
		call("tools/call", map[string]any{"name": "policy.grants.create", "arguments": map[string]any{
			"principal_type": "AGENT", "principal_id": "spiffe://x", "scopes": []any{"inference:invoke"},
		}})
		if gotMethod != http.MethodPost || gotPath != "/api/v1/policy/grants" {
			t.Errorf("grants.create hit %s %s", gotMethod, gotPath)
		}
		if gotBody["principal_type"] != "AGENT" {
			t.Errorf("principal_type not forwarded: %v", gotBody)
		}
	})

	t.Run("grants.create rejects an invalid principal_type via the enum", func(t *testing.T) {
		got := call("tools/call", map[string]any{"name": "policy.grants.create", "arguments": map[string]any{
			"principal_type": "ROOT", "principal_id": "x", "scopes": []any{"inference:invoke"},
		}})
		res, _ := got["result"].(map[string]any)
		text, _ := res["content"].([]any)[0].(map[string]any)["text"].(string)
		if !strings.Contains(text, "enum") {
			t.Errorf("expected an enum validation error for principal_type=ROOT, got %q", text)
		}
	})

	t.Run("roles.upsert PUTs to /roles", func(t *testing.T) {
		call("tools/call", map[string]any{"name": "policy.roles.upsert", "arguments": map[string]any{
			"role": "analyst", "scopes": []any{"agents:types:read"},
		}})
		if gotMethod != http.MethodPut || gotPath != "/api/v1/policy/roles" {
			t.Errorf("roles.upsert hit %s %s, want PUT /api/v1/policy/roles", gotMethod, gotPath)
		}
	})
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

// TestToolRequiresKeysAreRegisteredTools guards the toolRequires map against
// typos: every key must name a real registered tool. A key with no matching tool
// is dead (its scope is never emitted as _meta); a tool that needs a scope but is
// missing from the map would silently 403 when granted — so this also spot-checks
// that a representative new tool carries its _meta["orbit/requires"].
func TestToolRequiresKeysAreRegisteredTools(t *testing.T) {
	srv := NewServer(Backends{}, orbitjson.NewClient(logging.NewLogger()))
	gw := httptest.NewServer(mcpkit.NewController("/mcp", srv).Handler())
	defer gw.Close()
	call := mcpCaller(t, gw.URL)
	call("initialize", map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "t", "version": "0"}})

	// List every tool, following the cursor so pagination can't hide any.
	names := map[string]bool{}
	meta := map[string]any{}
	cursor := ""
	for {
		params := map[string]any{}
		if cursor != "" {
			params["cursor"] = cursor
		}
		resp := call("tools/list", params)
		result, _ := resp["result"].(map[string]any)
		for _, rt := range asSlice(result["tools"]) {
			tm, _ := rt.(map[string]any)
			name, _ := tm["name"].(string)
			if name != "" {
				names[name] = true
				meta[name], _ = tm["_meta"]
			}
		}
		cursor, _ = result["nextCursor"].(string)
		if cursor == "" {
			break
		}
	}

	if len(names) < 50 {
		t.Fatalf("expected the full tool surface (50+ tools), got %d", len(names))
	}
	for name := range toolRequires {
		if !names[name] {
			t.Errorf("toolRequires has %q but no such tool is registered (typo in scope mapping?)", name)
		}
	}
	// The _meta path works end to end for a new catalog tool.
	m, _ := meta["models.pricing.set_override"].(map[string]any)
	req, _ := m[metaRequiresKey].([]any)
	if len(req) != 1 || req[0] != "platform:catalog:write" {
		t.Errorf("models.pricing.set_override should carry _meta requires=[platform:catalog:write], got %v", m)
	}
}

func asSlice(v any) []any { s, _ := v.([]any); return s }

// TestToolRequiresMatchDownstreamRouteScopes cross-references every scoped tool's
// declared _meta["orbit/requires"] against the platform scope its downstream route
// actually enforces — mirrored from the route→scope lists in cmd/{service}/main.go.
// A mismatch is a broken grant (the tool 403s via its own scope) and/or an
// over-broad conferral (the grant confers more than the route needs), e.g. the
// set_budget mismatch (platform:grants:write declared vs platform:budgets:write
// enforced). Update wantRequires when a downstream route's RequiredScope changes.
func TestToolRequiresMatchDownstreamRouteScopes(t *testing.T) {
	wantRequires := map[string]string{
		// orbit-agents  /api/v1/agent-types  → agents:types:{read,write}
		"agents.list": "agents:types:read", "agents.get": "agents:types:read",
		"agents.create": "agents:types:write",
		"agents.update": "agents:types:write", "agents.delete": "agents:types:write",
		"agents.connectors": "platform:connections:read",
		// orbit-agents  /api/v1/environments  → agents:environments:{read,write}
		"agents.environments.list":    "agents:environments:read",
		"agents.environments.get":     "agents:environments:read",
		"agents.environments.create":  "agents:environments:write",
		"agents.environments.replace": "agents:environments:write",
		"agents.environments.delete":  "agents:environments:write",
		"agents.sandbox_images.list":  "agents:environments:read",
		"agents.environments.sizes":   "agents:environments:read",
		// orbit-agents  /api/v1/runs  → agents:runs:{read,write}
		"runs.list": "agents:runs:read", "runs.get": "agents:runs:read", "runs.events": "agents:runs:read",
		"runs.start": "agents:runs:write", "runs.send_message": "agents:runs:write",
		"runs.interrupt": "agents:runs:write", "runs.terminate": "agents:runs:write",
		// inference-gateway  /v1/models  → inference:list ;  /v1/catalog  → platform:catalog:{read,write}
		"models.list":         "inference:list",
		"models.virtual.list": "platform:catalog:read", "models.virtual.get": "platform:catalog:read",
		"models.virtual.create": "platform:catalog:write", "models.virtual.update": "platform:catalog:write",
		"models.virtual.delete": "platform:catalog:write", "models.providers.list": "platform:catalog:read", "models.providers.status": "platform:catalog:read",
		"models.providers.refresh": "platform:catalog:write", "models.providers.set_enabled": "platform:catalog:write",
		"models.pricing.list": "platform:catalog:read", "models.pricing.set_override": "platform:catalog:write",
		"models.pricing.clear_override": "platform:catalog:write",
		// policy-service  /api/v1/policy  → grants/roles/budgets/vocabulary
		"policy.grants.list": "platform:grants:read", "policy.grants.get": "platform:grants:read",
		"policy.grants.create": "platform:grants:write", "policy.grants.delete": "platform:grants:write",
		"policy.grants.set_budget": "platform:budgets:write", // PATCH /grants → budgets:write, not grants:write
		"policy.roles.list":        "platform:roles:read", "policy.roles.get": "platform:roles:read",
		"policy.roles.upsert": "platform:roles:write", "policy.roles.delete": "platform:roles:write",
		"policy.vocabulary.list": "platform:grants:read", "policy.vocabulary.get": "platform:grants:read",
		"budgets.tenant.get": "platform:budgets:read", "budgets.tenant.set": "platform:budgets:write",
		"budgets.tenant.delete": "platform:budgets:write", "budgets.principals.status": "platform:budgets:read",
		// triggers  /api/v1/{trigger-sources,triggers,trigger-deliveries}  → triggers:{read,write}
		"triggers.sources.list": "triggers:read", "triggers.sources.get": "triggers:read",
		"triggers.sources.create": "triggers:write", "triggers.sources.update": "triggers:write",
		"triggers.sources.delete": "triggers:write", "triggers.list": "triggers:read",
		"triggers.get": "triggers:read", "triggers.create": "triggers:write",
		"triggers.update": "triggers:write", "triggers.delete": "triggers:write",
		"triggers.deliveries.list":       "triggers:read",
		"triggers.providers.event_types": "triggers:read", "triggers.providers.fields": "triggers:read",
		"triggers.schedule_preview": "triggers:write", // POST /triggers/schedule-preview → triggers:write
	}

	for tool, want := range wantRequires {
		got, ok := toolRequires[tool]
		if !ok {
			t.Errorf("%s: documented here but missing from toolRequires", tool)
			continue
		}
		if len(got) != 1 || got[0] != want {
			t.Errorf("%s: toolRequires=%v but its downstream route enforces %q", tool, got, want)
		}
	}
	for tool := range toolRequires {
		if _, ok := wantRequires[tool]; !ok {
			t.Errorf("%s: in toolRequires but not in wantRequires — add it with the scope its downstream route enforces", tool)
		}
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

// TestListToolsMapTimeWindowAndPayloadParams checks runs.list and
// triggers.deliveries.list forward their time bounds and payload option.
func TestListToolsMapTimeWindowAndPayloadParams(t *testing.T) {
	var gotPath string
	var gotQuery url.Values
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotQuery = r.URL.Path, r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer backend.Close()

	srv := NewServer(Backends{Agents: backend.URL, Triggers: backend.URL}, orbitjson.NewClient(logging.NewLogger()))
	gw := httptest.NewServer(mcpkit.NewController("/mcp", srv).Handler())
	defer gw.Close()
	call := mcpCaller(t, gw.URL)
	call("initialize", map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "t", "version": "0"}})

	call("tools/call", map[string]any{"name": "runs.list", "arguments": map[string]any{
		"created_after": "2026-01-01T00:00:00Z", "created_before": "2026-01-02T00:00:00Z",
	}})
	if gotPath != "/api/v1/runs" || gotQuery.Get("created_after") != "2026-01-01T00:00:00Z" || gotQuery.Get("created_before") != "2026-01-02T00:00:00Z" {
		t.Errorf("runs.list hit %q?%v", gotPath, gotQuery)
	}

	call("tools/call", map[string]any{"name": "triggers.deliveries.list", "arguments": map[string]any{
		"received_after": "2026-01-01T00:00:00Z", "received_before": "2026-01-02T00:00:00Z", "omit_payload": true,
	}})
	if gotPath != "/api/v1/trigger-deliveries" || gotQuery.Get("received_after") != "2026-01-01T00:00:00Z" ||
		gotQuery.Get("received_before") != "2026-01-02T00:00:00Z" || gotQuery.Get("omit_payload") != "true" {
		t.Errorf("triggers.deliveries.list hit %q?%v", gotPath, gotQuery)
	}

	call("tools/call", map[string]any{"name": "triggers.deliveries.list", "arguments": map[string]any{}})
	if gotQuery.Has("omit_payload") || gotQuery.Has("received_after") {
		t.Errorf("unset options were forwarded: %v", gotQuery)
	}
}

// TestTriggerCoalesceForwarding covers coalesce on trigger create and update:
// the setting reaches the triggers backend under the documented names, and an
// empty object, which removes it, is forwarded rather than dropped.
func TestTriggerCoalesceForwarding(t *testing.T) {
	var gotBody map[string]any
	triggers := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody = nil
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer triggers.Close()

	srv := NewServer(Backends{Triggers: triggers.URL}, orbitjson.NewClient(logging.NewLogger()))
	gw := httptest.NewServer(mcpkit.NewController("/mcp", srv).Handler())
	defer gw.Close()
	call := mcpCaller(t, gw.URL)
	call("initialize", map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "t", "version": "0"}})

	coalesce := map[string]any{"key": "{{ repository.full_name }}#{{ pull_request.number }}", "windowSeconds": 30}
	call("tools/call", map[string]any{"name": "triggers.create", "arguments": map[string]any{
		"source_id": "src-1", "name": "t", "event_type": "pull_request", "coalesce": coalesce,
	}})
	got, _ := gotBody["coalesce"].(map[string]any)
	if got["key"] != coalesce["key"] || got["windowSeconds"] != float64(30) {
		t.Errorf("create did not forward coalesce: %v", gotBody["coalesce"])
	}

	call("tools/call", map[string]any{"name": "triggers.update", "arguments": map[string]any{"id": "t-1", "coalesce": coalesce}})
	if got, _ := gotBody["coalesce"].(map[string]any); got["windowSeconds"] != float64(30) {
		t.Errorf("update did not forward coalesce: %v", gotBody["coalesce"])
	}

	call("tools/call", map[string]any{"name": "triggers.update", "arguments": map[string]any{"id": "t-1", "coalesce": map[string]any{}}})
	if _, ok := gotBody["coalesce"]; !ok {
		t.Errorf("an empty coalesce object was dropped from the update, so it could not remove the setting: %v", gotBody)
	}

	call("tools/call", map[string]any{"name": "triggers.update", "arguments": map[string]any{"id": "t-1", "name": "renamed"}})
	if _, ok := gotBody["coalesce"]; ok {
		t.Errorf("an update that did not mention coalesce carried it: %v", gotBody["coalesce"])
	}
}
