package api

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/runkora/kora/backend/internal/platform/logging"
	"github.com/runkora/kora/backend/internal/platform/policy"
	"github.com/runkora/kora/backend/internal/platform/token"
	"github.com/runkora/kora/backend/internal/policyservice/roles"
)

// oneRoleRepo holds one stored role; every other method is unused here.
type oneRoleRepo struct {
	roles.Repository
	entry *roles.RoleEntry
}

func (r oneRoleRepo) GetByRole(_ context.Context, _, role string) (*roles.RoleEntry, error) {
	if r.entry == nil || r.entry.Role != role {
		return nil, roles.ErrRoleNotFound
	}
	copied := *r.entry
	return &copied, nil
}

func (r oneRoleRepo) UpdateBudget(_ context.Context, _, role string, budget *roles.BudgetAssignment) (*roles.RoleEntry, error) {
	if r.entry == nil || r.entry.Role != role {
		return nil, roles.ErrRoleNotFound
	}
	copied := *r.entry
	copied.Budget = budget
	return &copied, nil
}

// budgetsEval answers every Check with a fixed decision.
type budgetsEval struct {
	policy.Evaluator
	allow bool
}

func (e budgetsEval) Check(context.Context, policy.CheckInput) (policy.Result, error) {
	return policy.Result{Allow: e.allow}, nil
}

func budgetTestController(stored *roles.RoleEntry, holdsBudgetsWrite bool) *Controller {
	svc := &Service{roleRepo: oneRoleRepo{entry: stored}, logger: logging.NewLoggerWithOutput(io.Discard)}
	return &Controller{svc: svc, eval: budgetsEval{allow: holdsBudgetsWrite}, logger: logging.NewLoggerWithOutput(io.Discard)}
}

func TestRoleBudgetChangeAllowed(t *testing.T) {
	monthly := &roles.BudgetAssignment{LimitUSD: 50, ResetCadence: "monthly"}
	stored := &roles.RoleEntry{Role: "engineer", Budget: monthly}
	human := &token.PlatformClaims{Kora: token.KoraClaims{PrincipalType: token.PrincipalTypeHuman, TenantID: "t1"}}

	cases := []struct {
		name         string
		budget       *roles.BudgetAssignment
		budgetsWrite bool
		wantAllowed  bool
	}{
		{"unchanged budget needs only roles write", &roles.BudgetAssignment{LimitUSD: 50, ResetCadence: "monthly"}, false, true},
		{"changed budget without budgets write", &roles.BudgetAssignment{LimitUSD: 500, ResetCadence: "monthly"}, false, false},
		{"cleared budget without budgets write", nil, false, false},
		{"changed budget with budgets write", &roles.BudgetAssignment{LimitUSD: 500, ResetCadence: "monthly"}, true, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := budgetTestController(stored, tc.budgetsWrite)
			w := httptest.NewRecorder()
			if got := c.roleBudgetChangeAllowed(context.Background(), w, human, "t1", "engineer", tc.budget); got != tc.wantAllowed {
				t.Errorf("allowed = %v, want %v (status %d)", got, tc.wantAllowed, w.Code)
			}
		})
	}

	c := budgetTestController(nil, false)
	if !c.roleBudgetChangeAllowed(context.Background(), httptest.NewRecorder(), human, "t1", "new-role", nil) {
		t.Error("a new role without a budget needs no budgets write")
	}
}

func TestHandleUpdateRoleBudget_Refusals(t *testing.T) {
	human := &token.PlatformClaims{Kora: token.KoraClaims{PrincipalType: token.PrincipalTypeHuman, TenantID: "t1"}}
	cases := []struct {
		name      string
		role      string
		body      string
		delegated bool
		want      int
	}{
		{"delegated run", "engineer", `{"budget":null}`, true, http.StatusForbidden},
		{"reserved role", "platform-admin", `{"budget":null}`, false, http.StatusBadRequest},
		{"unknown role", "ghost", `{"budget":null}`, false, http.StatusNotFound},
		{"per-run limit on a role", "engineer", `{"budget":{"limit_usd":10,"per_run_limit_usd":1}}`, false, http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := budgetTestController(&roles.RoleEntry{Role: "engineer"}, true)
			req := httptest.NewRequest(http.MethodPatch, "/roles/"+tc.role+"/budget", bytes.NewBufferString(tc.body))
			req.SetPathValue("role", tc.role)
			ctx := token.WithClaims(req.Context(), human)
			if tc.delegated {
				ctx = policy.WithDelegation(ctx, &policy.DelegationClaims{})
			}
			w := httptest.NewRecorder()
			c.handleUpdateRoleBudget(w, req.WithContext(ctx))
			if w.Code != tc.want {
				t.Errorf("status = %d, want %d (body %s)", w.Code, tc.want, w.Body)
			}
		})
	}
}
