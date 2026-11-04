package middleware

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/golang-jwt/jwt/v5"

	"github.com/runkora/kora/backend/internal/platform/clients/routescopes"
	"github.com/runkora/kora/backend/internal/platform/logging"
	"github.com/runkora/kora/backend/internal/platform/policy"
	"github.com/runkora/kora/backend/internal/platform/token"
)

func runCtxAgentClaims(tenantID string) *token.PlatformClaims {
	return &token.PlatformClaims{
		RegisteredClaims: jwt.RegisteredClaims{Subject: "spiffe://runkora.test/tenant/t1/agent/coder"},
		Kora: token.KoraClaims{
			TenantID:      tenantID,
			PrincipalType: token.PrincipalTypeAgent,
		},
	}
}

func runCtxHumanClaims(tenantID string) *token.PlatformClaims {
	return &token.PlatformClaims{
		RegisteredClaims: jwt.RegisteredClaims{Subject: "user-1"},
		Kora: token.KoraClaims{
			TenantID:      tenantID,
			PrincipalType: token.PrincipalTypeHuman,
		},
	}
}

func TestStripRunPrefix_RewritesPathAndStoresRunID(t *testing.T) {
	t.Parallel()
	var gotPath, gotRunID string
	h := StripRunPrefix(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotRunID, _ = r.Context().Value(runIDContextKey{}).(string)
	}))

	req := httptest.NewRequest(http.MethodPost, "/run/run-42/v1/messages", nil)
	h.ServeHTTP(httptest.NewRecorder(), req)

	if gotPath != "/v1/messages" {
		t.Errorf("path = %q, want /v1/messages", gotPath)
	}
	if gotRunID != "run-42" {
		t.Errorf("run id = %q, want run-42", gotRunID)
	}
}

// TestStripRunPrefix_ToolGatewayPath proves the same mechanism serves the MCP
// tool-gateway mount: /run/{run_id}/v1/mcp/{server} rewrites to the gateway's
// normal per-server path with the run identity in context.
func TestStripRunPrefix_ToolGatewayPath(t *testing.T) {
	t.Parallel()
	var gotPath, gotRunID string
	h := StripRunPrefix(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotRunID, _ = r.Context().Value(runIDContextKey{}).(string)
	}))

	req := httptest.NewRequest(http.MethodPost, "/run/run-77/v1/mcp/github", nil)
	h.ServeHTTP(httptest.NewRecorder(), req)

	if gotPath != "/v1/mcp/github" {
		t.Errorf("path = %q, want /v1/mcp/github", gotPath)
	}
	if gotRunID != "run-77" {
		t.Errorf("run id = %q, want run-77", gotRunID)
	}
}

func TestStripRunPrefix_NonRunPathUntouched(t *testing.T) {
	t.Parallel()
	var gotPath string
	var hadRunID bool
	h := StripRunPrefix(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, hadRunID = r.Context().Value(runIDContextKey{}).(string)
	}))

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	h.ServeHTTP(httptest.NewRecorder(), req)

	if gotPath != "/v1/messages" || hadRunID {
		t.Errorf("path = %q hadRunID = %v, want untouched path and no run id", gotPath, hadRunID)
	}
}

// capturingEvaluator records the CheckInput ScopeEnforcementMiddleware builds.
type capturingEvaluator struct {
	got policy.CheckInput
}

func (c *capturingEvaluator) Check(_ context.Context, in policy.CheckInput) (policy.Result, error) {
	c.got = in
	return policy.Result{Allow: true}, nil
}

func (c *capturingEvaluator) Query(context.Context, policy.QueryInput) ([]string, error) {
	return nil, nil
}
func (c *capturingEvaluator) Scopes(context.Context, policy.ScopesInput) ([]string, error) {
	return nil, nil
}

func (c *capturingEvaluator) ScopesExplain(context.Context, policy.ScopesInput) (*policy.ScopesExplainResult, error) {
	return &policy.ScopesExplainResult{}, nil
}

func (c *capturingEvaluator) CheckBudget(context.Context, string, policy.BudgetCheckRequest) (policy.BudgetResult, error) {
	return policy.BudgetResult{Allow: true}, nil
}

func (c *capturingEvaluator) GetBudgetCategories(context.Context, string, string) ([]policy.BudgetCategory, error) {
	return nil, nil
}

func (c *capturingEvaluator) GetRoleBudgets(context.Context, string, []string) ([]policy.BudgetCategory, error) {
	return nil, nil
}

func (c *capturingEvaluator) ListBudgets(context.Context, string) (map[string]policy.PrincipalBudget, map[string]policy.RoleBudget, error) {
	return nil, nil, nil
}

func (c *capturingEvaluator) GetTenantBudget(context.Context, string) (*policy.TenantBudget, error) {
	return nil, nil
}
func (c *capturingEvaluator) Start(context.Context) {}

// TestScopeEnforcement_PopulatesRunID proves the scope-enforcement path feeds
// the stripped run prefix into policy.CheckInput.RunID so the engine can apply
// the delegation run-binding check (delegated-agent-identity ADR, Decision 4).
func TestScopeEnforcement_PopulatesRunID(t *testing.T) {
	t.Parallel()
	eval := &capturingEvaluator{}
	routes := []routescopes.RouteScope{
		{PathPrefix: "/v1/messages", Method: "POST", RequiredScope: "inference:invoke"},
	}
	chain := MiddlewareChain(ScopeEnforcementMiddleware(eval, routes, "", logging.NewLoggerWithOutput(io.Discard)))
	h := StripRunPrefix(chain(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})))

	req := httptest.NewRequest(http.MethodPost, "/run/run-42/v1/messages", nil)
	req = req.WithContext(token.WithClaims(req.Context(), runCtxAgentClaims("t1")))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if eval.got.RunID != "run-42" {
		t.Errorf("CheckInput.RunID = %q, want run-42", eval.got.RunID)
	}
}

func TestRunIDFromContext_AgentOnly(t *testing.T) {
	t.Parallel()
	var agentRunID, humanRunID string

	hAgent := StripRunPrefix(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		agentRunID = RunIDFromContext(r.Context())
	}))
	reqAgent := httptest.NewRequest(http.MethodPost, "/run/run-42/v1/messages", nil)
	reqAgent = reqAgent.WithContext(token.WithClaims(reqAgent.Context(), runCtxAgentClaims("t1")))
	hAgent.ServeHTTP(httptest.NewRecorder(), reqAgent)
	if agentRunID != "run-42" {
		t.Errorf("agent run id = %q, want run-42", agentRunID)
	}

	hHuman := StripRunPrefix(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		humanRunID = RunIDFromContext(r.Context())
	}))
	reqHuman := httptest.NewRequest(http.MethodPost, "/run/run-42/v1/messages", nil)
	reqHuman = reqHuman.WithContext(token.WithClaims(reqHuman.Context(), runCtxHumanClaims("t1")))
	hHuman.ServeHTTP(httptest.NewRecorder(), reqHuman)
	if humanRunID != "" {
		t.Errorf("human run id = %q, want empty (prefix ignored for non-agents)", humanRunID)
	}
}
