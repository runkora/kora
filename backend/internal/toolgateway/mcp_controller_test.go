package toolgateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/runkora/kora/backend/internal/platform/logging"
	mcpkit "github.com/runkora/kora/backend/internal/platform/mcp"
	mw "github.com/runkora/kora/backend/internal/platform/middleware"
	"github.com/runkora/kora/backend/internal/platform/policy"
	platformtoken "github.com/runkora/kora/backend/internal/platform/token"
)

// fakeEval is a policy.Evaluator stub: Query returns a configured allowlist and
// Check a configured decision; the rest are unused no-ops.
type fakeEval struct {
	allow    map[string]bool // wire-name → allowed (for Query)
	checkOK  bool
	checkErr error

	lastQuery policy.QueryInput
	lastCheck policy.CheckInput
}

func (f *fakeEval) Query(_ context.Context, in policy.QueryInput) ([]string, error) {
	f.lastQuery = in
	var out []string
	for _, c := range in.Candidates {
		if f.allow[c.ID] {
			out = append(out, c.ID)
		}
	}
	return out, nil
}
func (f *fakeEval) Check(_ context.Context, in policy.CheckInput) (policy.Result, error) {
	f.lastCheck = in
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
	return NewMCPController(nil, eval, cat, reg, NewProxy(NewUpstreamCredentials(""), "test", logging.NewLogger()), nil, "/v1", logging.NewLogger())
}

// doRPC dispatches a JSON-RPC body against the per-server endpoint, setting the
// {server} path value. Returns the recorder (for HTTP status) and the decoded
// JSON-RPC response (empty for non-RPC responses like a 404).
func doRPC(t *testing.T, c *MCPController, server, body string) (*httptest.ResponseRecorder, jsonRPCResponse) {
	t.Helper()
	claims := &platformtoken.PlatformClaims{}
	claims.Kora.TenantID = "acme"
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
			NewProxy(NewUpstreamCredentials(""), "test", logging.NewLogger()), nil, "/v1", logging.NewLogger())
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

// TestSameNameServers_NotShadowed verifies a same-named server owned by another
// tenant cannot shadow the caller's own server, regardless of registry order.
func TestSameNameServers_NotShadowed(t *testing.T) {
	// doRPC's caller is tenant "acme"; "aaa" sorts before it in the registry.
	reg := &Registry{
		servers: []Server{
			{TenantID: "aaa", Name: "s", Endpoint: "http://aaa/mcp"},
			{TenantID: "acme", Name: "s", Endpoint: "http://acme/mcp"},
		},
		loaded: true,
	}
	c := NewMCPController(nil, &fakeEval{checkOK: true}, &memCatalog{}, reg,
		NewProxy(NewUpstreamCredentials(""), "test", logging.NewLogger()), nil, "/v1", logging.NewLogger())

	rec, _ := doRPC(t, c, "s", `{"jsonrpc":"2.0","id":1,"method":"initialize"}`)
	if rec.Code == http.StatusNotFound {
		t.Fatalf("own server shadowed by another tenant's same-named server: got 404")
	}
}

// TestRegistryLookup_SelectsEndpoint asserts which server lookup resolves, by
// endpoint, so a wrong pick cannot pass as "not a 404".
func TestRegistryLookup_SelectsEndpoint(t *testing.T) {
	cases := []struct {
		name         string
		servers      []Server
		wantFound    bool
		wantEndpoint string
	}{
		{"global wins over own, global first", []Server{
			{TenantID: "", Name: "s", Endpoint: "http://global/mcp"},
			{TenantID: "acme", Name: "s", Endpoint: "http://acme/mcp"},
		}, true, "http://global/mcp"},
		{"global wins over own, own first", []Server{
			{TenantID: "acme", Name: "s", Endpoint: "http://acme/mcp"},
			{TenantID: "", Name: "s", Endpoint: "http://global/mcp"},
		}, true, "http://global/mcp"},
		{"own wins over other tenant", []Server{
			{TenantID: "aaa", Name: "s", Endpoint: "http://aaa/mcp"},
			{TenantID: "acme", Name: "s", Endpoint: "http://acme/mcp"},
		}, true, "http://acme/mcp"},
		{"only another tenant's server is not found", []Server{
			{TenantID: "other", Name: "s", Endpoint: "http://other/mcp"},
		}, false, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reg := &Registry{servers: tc.servers, loaded: true}
			got, found := reg.lookup("s", "acme")
			if found != tc.wantFound || got.Endpoint != tc.wantEndpoint {
				t.Fatalf("lookup = (%q, %v), want (%q, %v)", got.Endpoint, found, tc.wantEndpoint, tc.wantFound)
			}
		})
	}
}

func TestEmptyToolName(t *testing.T) {
	eval := &fakeEval{checkOK: true}
	c := testController(eval, &memCatalog{})
	_, resp := doRPC(t, c, "orbit", `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"","arguments":{}}}`)
	if resp.Error == nil || resp.Error.Code != -32602 {
		t.Fatalf("expected -32602 for empty tool name, got %+v", resp.Error)
	}
}

// MCP notifications expect no response, so the 202 carries no body at all.
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

// A delegated run's tool decisions carry the verified delegation, and tools/call
// carries the run prefix for the engine's run binding check.
func TestToolDecisions_CarryDelegationAndRun(t *testing.T) {
	delegation := &policy.DelegationClaims{Sub: "person-1", DelegatorPrincipalType: "HUMAN", RunID: "run-1", ActSub: "agent-1"}
	cases := []struct {
		name       string
		delegation *policy.DelegationClaims
		body       string
		wantRunID  string
	}{
		{"tools/list delegated", delegation, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`, ""},
		{"tools/call delegated", delegation, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"agents.list"}}`, "run-1"},
		{"tools/call autonomous", nil, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"agents.list"}}`, "run-1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			eval := &fakeEval{}
			cat := &memCatalog{tools: []CatalogTool{{ServerSlug: "orbit", ToolName: "agents.list", Scope: "mcp:orbit:agents:list:use"}}}
			c := testController(eval, cat)

			claims := &platformtoken.PlatformClaims{}
			claims.Kora.TenantID = "acme"
			claims.Kora.PrincipalType = platformtoken.PrincipalTypeAgent
			claims.Subject = "agent-1"
			ctx := platformtoken.WithClaims(context.Background(), claims)
			if tc.delegation != nil {
				ctx = policy.WithDelegation(ctx, tc.delegation)
			}
			req := httptest.NewRequest(http.MethodPost, "/run/run-1/v1/mcp/orbit", strings.NewReader(tc.body)).WithContext(ctx)
			req.SetPathValue("server", "orbit")
			mw.StripRunPrefix(http.HandlerFunc(c.handleMCP)).ServeHTTP(httptest.NewRecorder(), req)

			got := eval.lastCheck.Delegation
			if strings.HasPrefix(tc.name, "tools/list") {
				got = eval.lastQuery.Delegation
			}
			if got != tc.delegation {
				t.Errorf("delegation = %+v, want %+v", got, tc.delegation)
			}
			if eval.lastCheck.RunID != tc.wantRunID {
				t.Errorf("check run id = %q, want %q", eval.lastCheck.RunID, tc.wantRunID)
			}
		})
	}
}

// The caller's bearer and delegation reach a global forward-identity server only.
// External and tenant-scoped servers use their own credentials and see neither.
func TestToolsCall_ForwardsDelegationToForwardIdentityOnly(t *testing.T) {
	cases := []struct {
		name           string
		tenantID       string
		callerIdentity string
		wantAuth       string
		wantDelegation string
	}{
		{"global forward", "", callerIdentityForward, "Bearer agent-token", "delegation-token"},
		{"global external", "", "", "", ""},
		{"tenant forward", "acme", callerIdentityForward, "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var gotAuth, gotDelegation string
			upstream := mcpkit.NewServer("upstream", "v0")
			upstream.AddTool(&sdk.Tool{Name: "agents.list", InputSchema: json.RawMessage(`{"type":"object"}`)},
				func(context.Context, *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
					return &sdk.CallToolResult{}, nil
				})
			handler := mcpkit.NewController("/mcp", upstream).Handler()
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.Contains(r.Header.Get("Content-Type"), "json") {
					gotAuth, gotDelegation = r.Header.Get("Authorization"), r.Header.Get(mw.DelegationTokenHeader)
				}
				handler.ServeHTTP(w, r)
			}))
			defer ts.Close()

			reg := &Registry{
				servers: []Server{{TenantID: tc.tenantID, Name: "orbit", Endpoint: ts.URL + "/mcp", CallerIdentity: tc.callerIdentity}},
				loaded:  true,
			}
			c := NewMCPController(nil, &fakeEval{checkOK: true}, &memCatalog{}, reg, NewProxy(NewUpstreamCredentials(""), "test", logging.NewLogger()), nil, "/v1", logging.NewLogger())

			claims := &platformtoken.PlatformClaims{}
			claims.Kora.TenantID = "acme"
			claims.Subject = "agent-1"
			body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"agents.list","arguments":{}}}`
			req := httptest.NewRequest(http.MethodPost, "/v1/mcp/orbit", strings.NewReader(body)).WithContext(platformtoken.WithClaims(context.Background(), claims))
			req.SetPathValue("server", "orbit")
			req.Header.Set("Authorization", "Bearer agent-token")
			req.Header.Set(mw.DelegationTokenHeader, "delegation-token")
			rec := httptest.NewRecorder()
			c.handleMCP(rec, req)

			var resp jsonRPCResponse
			_ = json.Unmarshal(rec.Body.Bytes(), &resp)
			if resp.Error != nil {
				t.Fatalf("unexpected error: %+v", resp.Error)
			}
			if gotAuth != tc.wantAuth {
				t.Errorf("upstream Authorization = %q, want %q", gotAuth, tc.wantAuth)
			}
			if gotDelegation != tc.wantDelegation {
				t.Errorf("upstream delegation = %q, want %q", gotDelegation, tc.wantDelegation)
			}
		})
	}
}
