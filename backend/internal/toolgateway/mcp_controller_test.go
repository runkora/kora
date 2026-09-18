package toolgateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/clear-route/orbit/internal/platform/logging"
	"github.com/clear-route/orbit/internal/platform/policy"
	platformtoken "github.com/clear-route/orbit/internal/platform/token"
)

// fakeEval is a policy.Evaluator stub: Query returns a configured allowlist and
// Check a configured decision; the rest are unused no-ops.
type fakeEval struct {
	allow    map[string]bool // wire-name → allowed (for Query)
	checkOK  bool
	checkErr error
}

func (f *fakeEval) Query(_ context.Context, in policy.QueryInput) ([]string, error) {
	var out []string
	for _, c := range in.Candidates {
		if f.allow[c.ID] {
			out = append(out, c.ID)
		}
	}
	return out, nil
}
func (f *fakeEval) Check(_ context.Context, _ policy.CheckInput) (policy.Result, error) {
	return policy.Result{Allow: f.checkOK, Reason: "test"}, f.checkErr
}
func (f *fakeEval) Scopes(context.Context, policy.ScopesInput) ([]string, error) { return nil, nil }
func (f *fakeEval) ScopesExplain(context.Context, policy.ScopesInput) (*policy.ScopesExplainResult, error) {
	return nil, nil
}
func (f *fakeEval) CheckBudget(context.Context, string, policy.BudgetCheckRequest) (policy.BudgetResult, error) {
	return policy.BudgetResult{}, nil
}
func (f *fakeEval) GetBudgetCategories(context.Context, string, string) ([]policy.BudgetCategory, error) {
	return nil, nil
}
func (f *fakeEval) GetRoleBudgets(context.Context, string, []string) ([]policy.BudgetCategory, error) {
	return nil, nil
}
func (f *fakeEval) ListBudgets(context.Context, string) (map[string]policy.PrincipalBudget, map[string]policy.RoleBudget, error) {
	return nil, nil, nil
}
func (f *fakeEval) GetTenantBudget(context.Context, string) (*policy.TenantBudget, error) {
	return nil, nil
}
func (f *fakeEval) Start(context.Context) {}

// memCatalog is an in-memory CatalogRepository.
type memCatalog struct{ tools []CatalogTool }

func (m *memCatalog) Replace(context.Context, []CatalogTool) error { return nil }
func (m *memCatalog) ListByTenant(context.Context, string) ([]CatalogTool, error) {
	return m.tools, nil
}

func testController(eval policy.Evaluator, cat CatalogRepository) *MCPController {
	reg := &Registry{
		servers: []Server{{TenantID: "", Name: "orbit", Endpoint: "http://orbit/mcp", CallerIdentity: "forward"}},
		loaded:  true,
	}
	return NewMCPController(nil, eval, cat, reg, NewProxy("test", logging.NewLogger()), "/v1", logging.NewLogger())
}

// doRPC dispatches a JSON-RPC body against the per-server endpoint, setting the
// {server} path value. Returns the recorder (for HTTP status) and the decoded
// JSON-RPC response (empty for non-RPC responses like a 404).
func doRPC(t *testing.T, c *MCPController, server, body string) (*httptest.ResponseRecorder, jsonRPCResponse) {
	t.Helper()
	claims := &platformtoken.PlatformClaims{}
	claims.Orbit.TenantID = "acme"
	claims.Subject = "agent-1"
	ctx := platformtoken.WithClaims(context.Background(), claims)
	req := httptest.NewRequest(http.MethodPost, "/v1/mcp/"+server, strings.NewReader(body)).WithContext(ctx)
	req.SetPathValue("server", server)
	rec := httptest.NewRecorder()
	c.handleMCP(rec, req)

	var resp jsonRPCResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp) // non-RPC responses (404) decode to empty
	return rec, resp
}

func TestToolsList_FiltersToGranted(t *testing.T) {
	cat := &memCatalog{tools: []CatalogTool{
		{ServerSlug: "orbit", ToolName: "agents.list", Scope: "mcp:orbit:agents:list:use"},
		{ServerSlug: "orbit", ToolName: "agents.create", Scope: "mcp:orbit:agents:create:use"},
		// A tool on a different server must NOT appear on the orbit endpoint.
		{ServerSlug: "github", ToolName: "issues.create", Scope: "mcp:github:issues:create:use"},
	}}
	// Only agents.list granted (plain name — the connection is scoped to orbit).
	eval := &fakeEval{allow: map[string]bool{"agents.list": true}}
	c := testController(eval, cat)

	_, resp := doRPC(t, c, "orbit", `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	result, _ := json.Marshal(resp.Result)
	var payload struct {
		Tools []mcpTool `json:"tools"`
	}
	_ = json.Unmarshal(result, &payload)
	if len(payload.Tools) != 1 || payload.Tools[0].Name != "agents.list" {
		t.Errorf("expected only agents.list, got %+v", payload.Tools)
	}
}

func TestToolsCall_DeniedBeforeUpstream(t *testing.T) {
	// checkOK=false → default-deny. Upstream endpoint is bogus, so if the deny
	// did NOT short-circuit, the proxy call would error differently (or hang);
	// asserting the -32001 deny code proves it never reached the proxy.
	eval := &fakeEval{checkOK: false}
	c := testController(eval, &memCatalog{})

	_, resp := doRPC(t, c, "orbit", `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"agents.create","arguments":{}}}`)
	if resp.Error == nil || resp.Error.Code != -32001 {
		t.Fatalf("expected -32001 tool not permitted, got %+v (result=%v)", resp.Error, resp.Result)
	}
}

func TestUnknownServer_404(t *testing.T) {
	eval := &fakeEval{checkOK: true}
	c := testController(eval, &memCatalog{})

	// A server not in the registry → HTTP 404 before any JSON-RPC dispatch.
	rec, _ := doRPC(t, c, "ghost", `{"jsonrpc":"2.0","id":3,"method":"initialize"}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown server, got %d (%s)", rec.Code, rec.Body.String())
	}
}

// TestTenantOwnershipGuard verifies a tenant can only reach a global server or
// its own; another tenant's server is a 404, indistinguishable from unknown — so
// the tenant-blind scope vocabulary can't be used to reach across tenants.
func TestTenantOwnershipGuard(t *testing.T) {
	// doRPC's caller is tenant "acme".
	mk := func(serverTenant string) *MCPController {
		reg := &Registry{
			servers: []Server{{TenantID: serverTenant, Name: "s", Endpoint: "http://s/mcp"}},
			loaded:  true,
		}
		return NewMCPController(nil, &fakeEval{checkOK: true}, &memCatalog{}, reg,
			NewProxy("test", logging.NewLogger()), "/v1", logging.NewLogger())
	}
	init := `{"jsonrpc":"2.0","id":1,"method":"initialize"}`

	t.Run("another tenant's server → 404", func(t *testing.T) {
		rec, _ := doRPC(t, mk("other-tenant"), "s", init)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("cross-tenant server should be 404, got %d (%s)", rec.Code, rec.Body.String())
		}
	})
	t.Run("global server → reachable", func(t *testing.T) {
		rec, _ := doRPC(t, mk(""), "s", init)
		if rec.Code == http.StatusNotFound {
			t.Fatalf("global server should be reachable, got 404")
		}
	})
	t.Run("own-tenant server → reachable", func(t *testing.T) {
		rec, _ := doRPC(t, mk("acme"), "s", init)
		if rec.Code == http.StatusNotFound {
			t.Fatalf("own-tenant server should be reachable, got 404")
		}
	})
}

func TestEmptyToolName(t *testing.T) {
	eval := &fakeEval{checkOK: true}
	c := testController(eval, &memCatalog{})
	_, resp := doRPC(t, c, "orbit", `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"","arguments":{}}}`)
	if resp.Error == nil || resp.Error.Code != -32602 {
		t.Fatalf("expected -32602 for empty tool name, got %+v", resp.Error)
	}
}

// TestNotification_202EmptyBody verifies MCP notifications (no id, no response
// expected per spec) get a 202 with a truly empty body — not a JSON "null" —
// since a compliant client parses this response as "no content", and any body
// at all deviates from that contract.
func TestNotification_202EmptyBody(t *testing.T) {
	eval := &fakeEval{checkOK: true}
	c := testController(eval, &memCatalog{})
	rec, _ := doRPC(t, c, "orbit", `{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", rec.Code)
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("expected empty body for notification ack, got %q", rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "" {
		t.Errorf("expected no Content-Type header for empty-body response, got %q", ct)
	}
}
