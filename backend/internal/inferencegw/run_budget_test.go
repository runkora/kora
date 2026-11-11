package inferencegw

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/runkora/kora/backend/internal/platform/logging"
	"github.com/runkora/kora/backend/internal/platform/policy"
	"github.com/runkora/kora/backend/internal/platform/token"
)

const budgetRunID = "6f1c2a1e-6a59-4c55-9a43-0f1d3c1b2e7a"

func runBudgetController(eval *stubEvaluator, counter *mockSpendCounter, rp *RunPrincipal) *Controller {
	svc := &Service{policy: eval, usage: &mockInferenceUsageRepository{runPrincipal: rp, runTenant: "t1", personNames: map[string]string{"user-1": "Dev User"}}, budgetCounter: counter, logger: logging.NewLoggerWithOutput(io.Discard)}
	return &Controller{svc: svc, logger: logging.NewLoggerWithOutput(io.Discard)}
}

func getRunBudget(c *Controller, claims *token.PlatformClaims, runID, tenant string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/budgets/runs/"+runID+"?tenant_id="+tenant, nil)
	req.SetPathValue("runID", runID)
	req = req.WithContext(token.WithClaims(req.Context(), claims))
	w := httptest.NewRecorder()
	c.handleGetRunBudget(w, req)
	return w
}

func TestHandleGetRunBudget(t *testing.T) {
	agent := &RunPrincipal{PrincipalID: "spiffe://runkora.test/tenant/t1/agent/coder", PrincipalType: "AGENT", DelegatorID: "user-1"}
	system := makeClaims("", "spiffe://runkora.test/system/kora-agents", token.PrincipalTypeSystem, "")
	system.Kora.DisplayName = "kora-agents"
	otherSystem := makeClaims("", "spiffe://runkora.test/system/kora-triggers", token.PrincipalTypeSystem, "")
	otherSystem.Kora.DisplayName = "kora-triggers"
	reached := []policy.BudgetCeiling{
		{Scope: "principal", ID: agent.PrincipalID, Window: "monthly", LimitUSD: 100, SpendUSD: 10},
		{Scope: "delegator", ID: "user-1", Window: "weekly", LimitUSD: 5, SpendUSD: 5.2},
	}
	tests := []struct {
		name     string
		claims   *token.PlatformClaims
		runID    string
		tenant   string
		ceilings []policy.BudgetCeiling
		rp       *RunPrincipal
		want     int
		wantBody *RunBudget
	}{
		{name: "the reached ceiling", claims: system, runID: budgetRunID, ceilings: reached, rp: agent, want: http.StatusOK,
			wantBody: &RunBudget{Scope: "delegator", Owner: "user-1", OwnerName: "Dev User", Window: "weekly", LimitUSD: 5}},
		{name: "a per-run ceiling never resets", claims: system, runID: budgetRunID, rp: agent, want: http.StatusOK,
			ceilings: []policy.BudgetCeiling{{Scope: "run", ID: budgetRunID, Window: "run", LimitUSD: 1, SpendUSD: 1}},
			wantBody: &RunBudget{Scope: "run", Owner: budgetRunID, Window: "run", LimitUSD: 1}},
		{name: "nothing reached", claims: system, runID: budgetRunID, rp: agent, ceilings: reached[:1], want: http.StatusNotFound},
		{name: "a run without usage", claims: system, runID: budgetRunID, want: http.StatusNotFound},
		{name: "another system component may not ask", claims: otherSystem, runID: budgetRunID, rp: agent, ceilings: reached, want: http.StatusForbidden},
		{name: "a run outside the tenant", claims: system, runID: budgetRunID, tenant: "t2", rp: agent, ceilings: reached, want: http.StatusNotFound},
		{name: "a person may not ask", claims: makeClaims("t1", "user-1", token.PrincipalTypeHuman, ""), runID: budgetRunID, rp: agent, ceilings: reached, want: http.StatusForbidden},
		{name: "a run id that is not a UUID", claims: system, runID: "stats", rp: agent, want: http.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			eval := &stubEvaluator{budgetCeilings: tt.ceilings}
			counter := &mockSpendCounter{}
			tenant := tt.tenant
			if tenant == "" {
				tenant = "t1"
			}
			w := getRunBudget(runBudgetController(eval, counter, tt.rp), tt.claims, tt.runID, tenant)
			if w.Code != tt.want {
				t.Fatalf("status = %d, want %d; body %s", w.Code, tt.want, w.Body)
			}
			if tt.wantBody == nil {
				return
			}
			var got RunBudget
			if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if tt.wantBody.Window == "run" {
				if got.ResetsAt != nil {
					t.Errorf("resets_at = %v, want none for a per-run ceiling", got.ResetsAt)
				}
			} else if got.ResetsAt == nil || !got.ResetsAt.Equal(periodEnd(tt.wantBody.Window, time.Now())) {
				t.Errorf("resets_at = %v, want the end of the %s window", got.ResetsAt, tt.wantBody.Window)
			}
			got.ResetsAt = nil
			if got != *tt.wantBody {
				t.Errorf("budget = %+v, want %+v", got, *tt.wantBody)
			}
			// The run's own principal, delegator and run are what the policy evaluated.
			if r := eval.lastBudgetReq; r == nil || r.PrincipalID != agent.PrincipalID || r.DelegatorID != "user-1" || r.RunID != budgetRunID {
				t.Errorf("CheckBudget request = %+v, want the run's principal, delegator and run", eval.lastBudgetReq)
			}
			if counter.lastScopes.RunID != budgetRunID || counter.lastScopes.DelegatorID != "user-1" {
				t.Errorf("counter scopes = %+v, want the run and the delegator", counter.lastScopes)
			}
		})
	}
}

// Each ceiling scope names its owner for a person, except an agent and a run.
func TestReachedRunBudget_OwnerName(t *testing.T) {
	agent := &RunPrincipal{PrincipalID: "spiffe://runkora.test/tenant/t1/agent/coder", PrincipalType: "AGENT"}
	person := &RunPrincipal{PrincipalID: "user-1", PrincipalType: "HUMAN"}
	tests := []struct {
		name    string
		rp      *RunPrincipal
		ceiling policy.BudgetCeiling
		want    string
	}{
		{"an agent principal is left to kora-agents", agent, policy.BudgetCeiling{Scope: "principal", ID: agent.PrincipalID, Window: "monthly"}, ""},
		{"a person principal is named", person, policy.BudgetCeiling{Scope: "principal", ID: "user-1", Window: "monthly"}, "Dev User"},
		{"a delegator is named", agent, policy.BudgetCeiling{Scope: "delegator", ID: "user-1", Window: "weekly"}, "Dev User"},
		{"a role pool is its role", agent, policy.BudgetCeiling{Scope: "role_pool", ID: "engineers", Window: "monthly"}, "engineers"},
		{"a tenant cap is the tenant", agent, policy.BudgetCeiling{Scope: "tenant", ID: "t1", Window: "monthly"}, "tenant"},
		{"a run ceiling has no owner name", agent, policy.BudgetCeiling{Scope: "run", ID: budgetRunID, Window: "run"}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.ceiling.LimitUSD, tt.ceiling.SpendUSD = 1, 1
			c := runBudgetController(&stubEvaluator{budgetCeilings: []policy.BudgetCeiling{tt.ceiling}}, &mockSpendCounter{}, tt.rp)
			got, err := c.svc.ReachedRunBudget(context.Background(), "t1", budgetRunID)
			if err != nil || got.OwnerName != tt.want {
				t.Errorf("owner_name = %q, %v; want %q", got.OwnerName, err, tt.want)
			}
		})
	}
}
