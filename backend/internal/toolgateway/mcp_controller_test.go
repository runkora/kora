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
	return NewMCPController(nil, eval, cat, reg, NewProxy("test", logging.NewLogger()), "", logging.NewLogger())
}

func doRPC(t *testing.T, c *MCPController, body string) jsonRPCResponse {
	t.Helper()
	claims := &platformtoken.PlatformClaims{}
	claims.Orbit.TenantID = "acme"
	claims.Subject = "agent-1"
	ctx := platformtoken.WithClaims(context.Background(), claims)
	req := httptest.NewRequest(http.MethodPost, "/v1/mcp", strings.NewReader(body)).WithContext(ctx)
	rec := httptest.NewRecorder()
	c.handleMCP(rec, req)

	var resp jsonRPCResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response %q: %v", rec.Body.String(), err)
	}
	return resp
}

func TestToolsList_FiltersToGranted(t *testing.T) {
	cat := &memCatalog{tools: []CatalogTool{
		{ServerSlug: "orbit", ToolName: "agents.list", Scope: "mcp:orbit:agents:list:use"},
		{ServerSlug: "orbit", ToolName: "agents.create", Scope: "mcp:orbit:agents:create:use"},
	}}
	// Only agents.list granted.
	eval := &fakeEval{allow: map[string]bool{"orbit__agents.list": true}}
	c := testController(eval, cat)

	resp := doRPC(t, c, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	result, _ := json.Marshal(resp.Result)
	var payload struct {
		Tools []mcpTool `json:"tools"`
	}
	_ = json.Unmarshal(result, &payload)
	if len(payload.Tools) != 1 || payload.Tools[0].Name != "orbit__agents.list" {
		t.Errorf("expected only orbit__agents.list, got %+v", payload.Tools)
	}
}

func TestToolsCall_DeniedBeforeUpstream(t *testing.T) {
	// checkOK=false → default-deny. Upstream endpoint is bogus, so if the deny
	// did NOT short-circuit, the proxy call would error differently (or hang);
	// asserting the -32001 deny code proves it never reached the proxy.
	eval := &fakeEval{checkOK: false}
	c := testController(eval, &memCatalog{})

	resp := doRPC(t, c, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"orbit__agents.create","arguments":{}}}`)
	if resp.Error == nil || resp.Error.Code != -32001 {
		t.Fatalf("expected -32001 tool not permitted, got %+v (result=%v)", resp.Error, resp.Result)
	}
}

func TestToolsCall_UnknownServer(t *testing.T) {
	eval := &fakeEval{checkOK: true}
	c := testController(eval, &memCatalog{})

	resp := doRPC(t, c, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"ghost__do","arguments":{}}}`)
	if resp.Error == nil || resp.Error.Code != -32602 {
		t.Fatalf("expected -32602 unknown server, got %+v", resp.Error)
	}
}

func TestBadToolName(t *testing.T) {
	eval := &fakeEval{checkOK: true}
	c := testController(eval, &memCatalog{})
	resp := doRPC(t, c, `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"noseparator","arguments":{}}}`)
	if resp.Error == nil || resp.Error.Code != -32602 {
		t.Fatalf("expected -32602 for bad tool name, got %+v", resp.Error)
	}
}
