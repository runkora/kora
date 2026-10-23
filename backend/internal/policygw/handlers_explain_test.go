package policygw

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/runkora/kora/backend/internal/platform/logging"
	"github.com/runkora/kora/backend/internal/platform/policy"
	"github.com/runkora/kora/backend/internal/platform/token"
)

// scopedEvaluator answers Check from a fixed scope set, so a test says what
// the caller holds rather than what the answer should be.
type scopedEvaluator struct {
	held []string
}

func (e *scopedEvaluator) scope(input policy.CheckInput) string {
	parts := append([]string{input.ResourceType}, input.PathSegments...)
	return strings.Join(append(parts, input.Action), ":")
}

func (e *scopedEvaluator) Check(_ context.Context, input policy.CheckInput) (policy.Result, error) {
	if slices.Contains(e.held, e.scope(input)) {
		return policy.Result{Allow: true, Reason: "allow"}, nil
	}
	return policy.Result{Allow: false, Reason: "no_matching_grant"}, nil
}

func (e *scopedEvaluator) Query(_ context.Context, _ policy.QueryInput) ([]string, error) {
	return nil, nil
}

func (e *scopedEvaluator) Scopes(_ context.Context, _ policy.ScopesInput) ([]string, error) {
	return e.held, nil
}

func (e *scopedEvaluator) ScopesExplain(_ context.Context, _ policy.ScopesInput) (*policy.ScopesExplainResult, error) {
	return &policy.ScopesExplainResult{EffectiveScopes: e.held}, nil
}

func (e *scopedEvaluator) CheckBudget(_ context.Context, _ string, _ policy.BudgetCheckRequest) (policy.BudgetResult, error) {
	return policy.BudgetResult{Allow: true}, nil
}

func (e *scopedEvaluator) GetTenantBudget(_ context.Context, _ string) (*policy.TenantBudget, error) {
	return nil, nil
}

func (e *scopedEvaluator) GetBudgetCategories(_ context.Context, _, _ string) ([]policy.BudgetCategory, error) {
	return nil, nil
}

func (e *scopedEvaluator) GetRoleBudgets(_ context.Context, _ string, _ []string) ([]policy.BudgetCategory, error) {
	return nil, nil
}

func (e *scopedEvaluator) ListBudgets(_ context.Context, _ string) (map[string]policy.PrincipalBudget, map[string]policy.RoleBudget, error) {
	return map[string]policy.PrincipalBudget{}, map[string]policy.RoleBudget{}, nil
}

func (e *scopedEvaluator) Start(_ context.Context) {}

func testController(held ...string) *Controller {
	logger := logging.NewLoggerWithOutput(io.Discard)
	return &Controller{
		svc:    NewService(&scopedEvaluator{held: held}, logger),
		logger: logger,
	}
}

// callerClaims is a tenant-scoped human, the principal type the dashboard uses.
func callerClaims() *token.PlatformClaims {
	c := &token.PlatformClaims{}
	c.Subject = "user-1"
	c.Kora.PrincipalType = token.PrincipalTypeHuman
	c.Kora.TenantID = "tenant-1"
	return c
}

func requestWithClaims(method, target string, body string, claims *token.PlatformClaims) *http.Request {
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, target, nil)
	} else {
		r = httptest.NewRequest(method, target, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
	}
	return r.WithContext(token.WithClaims(r.Context(), claims))
}

// TestScopesExplain_OtherPrincipalRequiresGrantsRead covers the authorization
// split the policy-administration ADR specifies: a caller may always read its
// own effective scopes, and needs platform:grants:read to read anyone else's.
func TestScopesExplain_OtherPrincipalRequiresGrantsRead(t *testing.T) {
	cases := []struct {
		name     string
		query    string
		held     []string
		wantCode int
	}{
		{
			name:     "own scopes without the scope",
			query:    "",
			wantCode: http.StatusOK,
		},
		{
			name:     "own identity named explicitly",
			query:    "?principal_type=HUMAN&principal_id=user-1",
			wantCode: http.StatusOK,
		},
		{
			name:     "another principal without the scope",
			query:    "?principal_type=HUMAN&principal_id=user-2",
			wantCode: http.StatusForbidden,
		},
		{
			name:     "another principal with the scope",
			query:    "?principal_type=HUMAN&principal_id=user-2",
			held:     []string{inspectOthersScope},
			wantCode: http.StatusOK,
		},
		{
			name:     "another principal type without the scope",
			query:    "?principal_type=AGENT&principal_id=user-1",
			wantCode: http.StatusForbidden,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := testController(tc.held...)
			w := httptest.NewRecorder()

			c.handleScopesExplain(w, requestWithClaims(http.MethodGet, "/policy/scopes/explain"+tc.query, "", callerClaims()))

			if w.Code != tc.wantCode {
				t.Errorf("status = %d, want %d (body %s)", w.Code, tc.wantCode, w.Body.String())
			}
		})
	}
}

// TestScopesExplain_SystemPrincipalPasses covers the carve-out
// ScopeEnforcementMiddleware makes platform-wide: a SYSTEM principal is a
// platform service authenticated by SPIFFE at the mesh and carries no tenant,
// so a per-tenant bundle lookup would deny it whatever it holds.
func TestScopesExplain_SystemPrincipalPasses(t *testing.T) {
	claims := &token.PlatformClaims{}
	claims.Subject = "kora-agents"
	claims.Kora.PrincipalType = token.PrincipalTypeSystem

	c := testController()
	w := httptest.NewRecorder()

	c.handleScopesExplain(w, requestWithClaims(http.MethodGet, "/policy/scopes/explain?principal_type=HUMAN&principal_id=user-2", "", claims))

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 (body %s)", w.Code, w.Body.String())
	}
}

// TestCheckSimulate_RequiresGrantsRead covers the simulate route, which always
// names a principal other than the caller and so has no self case to exempt.
func TestCheckSimulate_RequiresGrantsRead(t *testing.T) {
	body := `{"principal_type":"AGENT","principal_id":"agent-1","resource":{"type":"inference","path_segments":["anthropic"]},"action":"invoke"}`

	cases := []struct {
		name     string
		held     []string
		wantCode int
	}{
		{name: "without the scope", wantCode: http.StatusForbidden},
		{name: "with the scope", held: []string{inspectOthersScope}, wantCode: http.StatusOK},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := testController(tc.held...)
			w := httptest.NewRecorder()

			c.handleCheckSimulate(w, requestWithClaims(http.MethodPost, "/policy/check/simulate", body, callerClaims()))

			if w.Code != tc.wantCode {
				t.Errorf("status = %d, want %d (body %s)", w.Code, tc.wantCode, w.Body.String())
			}
		})
	}
}
