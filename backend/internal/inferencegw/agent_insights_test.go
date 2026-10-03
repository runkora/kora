package inferencegw

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/clear-route/orbit/internal/platform/logging"
	"github.com/clear-route/orbit/internal/platform/policy"
	"github.com/clear-route/orbit/internal/platform/token"
	"github.com/clear-route/orbit/internal/platform/usagewindow"
)

func claimsWithRoles(tenant, sub, roles string) *token.PlatformClaims {
	c := &token.PlatformClaims{
		RegisteredClaims: jwt.RegisteredClaims{Subject: sub},
		Orbit:            token.OrbitClaims{TenantID: tenant, PrincipalType: token.PrincipalTypeHuman},
	}
	if roles != "" {
		c.Orbit.Claims = map[string]string{"roles": roles}
	}
	return c
}

func TestCallerFromClaims(t *testing.T) {
	cases := []struct {
		name  string
		roles string
		admin bool
	}{
		{"admin among roles", "users, platform-admin,engineer", true},
		{"no roles claim, as for a PAT", "", false},
		{"other roles", "users,engineer", false},
		{"a longer role name", "platform-administrator", false},
	}
	for _, tc := range cases {
		got := callerFromClaims(claimsWithRoles("t1", "alice", tc.roles))
		if got.Admin != tc.admin || got.TenantID != "t1" || got.Subject != "alice" {
			t.Errorf("%s: %+v", tc.name, got)
		}
	}
}

// The privacy rule is the SQL text, so the test reads it: a non-admin is held to
// rows without a delegator or their own, and an admin query has no delegator clause.
func TestPredicate(t *testing.T) {
	w := usagewindow.For(usagewindow.Period7d, testEnd)

	clause, args := usageQuery{Caller: usageCaller{TenantID: "t1", Subject: "alice"}, Window: w}.scope(w.Start, w.End)
	for _, want := range []string{"tenant_id = $1", "agent_slug IS NOT NULL", "(delegator_id IS NULL OR delegator_id = $2)", "created_at >= $3 AND created_at < $4"} {
		if !strings.Contains(clause, want) {
			t.Errorf("non-admin clause %q lacks %q", clause, want)
		}
	}
	if len(args) != 4 || args[0] != "t1" || args[1] != "alice" {
		t.Errorf("non-admin args = %v", args)
	}

	clause, args = usageQuery{Caller: usageCaller{TenantID: "t1", Subject: "root", Admin: true}, Window: w, Slug: "coder"}.scope(w.Start, w.End)
	if strings.Contains(clause, "delegator_id") || !strings.Contains(clause, "agent_slug IS NOT NULL") || !strings.Contains(clause, "agent_slug = $4") {
		t.Errorf("admin clause = %q", clause)
	}
	if len(args) != 4 || args[0] != "t1" || args[3] != "coder" {
		t.Errorf("admin args = %v", args)
	}
}

// fakeUsageStore returns canned rollups and records the queries it was asked.
type fakeUsageStore struct {
	rows     []agentUsageRow
	previous float64
	buckets  []float64
	spend    []AgentBudgetSpend
	queries  []usageQuery
	spendFor []string
	spendBy  usageCaller
}

func (f *fakeUsageStore) AgentUsage(_ context.Context, q usageQuery) ([]agentUsageRow, error) {
	f.queries = append(f.queries, q)
	return f.rows, nil
}
func (f *fakeUsageStore) AgentSpendBuckets(context.Context, usageQuery) (float64, []float64, error) {
	return f.previous, f.buckets, nil
}
func (f *fakeUsageStore) AgentBudgetSpend(_ context.Context, caller usageCaller, slugs []string, _, _ time.Time) ([]AgentBudgetSpend, error) {
	f.spendFor, f.spendBy = slugs, caller
	return f.spend, nil
}

// fakeBundle is an evaluator that only lists budgets.
type fakeBundle struct {
	policy.Evaluator
	principals map[string]policy.PrincipalBudget
}

func (f fakeBundle) ListBudgets(context.Context, string) (map[string]policy.PrincipalBudget, map[string]policy.RoleBudget, error) {
	return f.principals, nil, nil
}

const agentNS = "spiffe://orbit.test/tenant/orbit-tenant-example/agent/"

var testEnd = time.Date(2026, 10, 3, 14, 0, 0, 0, time.UTC) // a Saturday

func newInsights(store *fakeUsageStore, bundle policy.Evaluator) *AgentInsightsService {
	if bundle == nil {
		bundle = fakeBundle{}
	}
	s := NewAgentInsightsService(store, bundle, logging.NewLoggerWithOutput(io.Discard))
	s.now = func() time.Time { return testEnd }
	return s
}

func row(slug, model string, tokens int64, cost float64) agentUsageRow {
	return agentUsageRow{AgentTypeUsage: AgentTypeUsage{AgentSlug: slug, PromptTokens: tokens, TotalTokens: tokens, EstimatedCostUSD: cost}, ModelID: model}
}

func TestInsights_FoldsUsageAndBuckets(t *testing.T) {
	buckets := make([]float64, 7)
	buckets[6] = 5
	store := &fakeUsageStore{
		rows:     []agentUsageRow{row("coder", "m1", 10, 1), row("coder", "m2", 20, 0.5), row("notes", "m1", 5, 0.5), row("free", "m3", 1, 0)},
		previous: 3,
		buckets:  buckets,
	}
	caller := usageCaller{TenantID: "t1", Subject: "alice"}

	out, err := newInsights(store, nil).Insights(context.Background(), caller, usagewindow.Period7d)
	if err != nil {
		t.Fatalf("Insights: %v", err)
	}
	if out.TotalCostUSD != 2 || out.PreviousTotalUSD != 3 {
		t.Errorf("total %v previous %v, want 2 and 3", out.TotalCostUSD, out.PreviousTotalUSD)
	}
	if len(out.Usage) != 3 || out.Usage[0].AgentSlug != "coder" || out.Usage[0].TotalTokens != 30 || out.Usage[0].EstimatedCostUSD != 1.5 {
		t.Errorf("usage = %+v, want coder folded across its models", out.Usage)
	}
	if len(out.Models) != 2 || out.Models[0].ModelID != "m1" || out.Models[0].CostUSD != 1.5 || out.Models[1].ModelID != "m2" {
		t.Errorf("models = %+v, want m1 then m2 and no free model", out.Models)
	}
	if len(out.Trend) != 7 || out.Trend[6].CostUSD != 5 {
		t.Errorf("trend = %+v", out.Trend)
	}
	if want := testEnd.Add(-7 * 24 * time.Hour); !out.Trend[0].Start.Equal(want) {
		t.Errorf("first bucket starts %s, want %s", out.Trend[0].Start, want)
	}
	if len(store.queries) != 1 || store.queries[0].Caller != caller {
		t.Errorf("the rollup must run for the verified caller: %+v", store.queries)
	}
	raw, _ := json.Marshal(out)
	var generic map[string]any
	_ = json.Unmarshal(raw, &generic)
	if _, has := generic["budgets"]; has {
		t.Errorf("no budgets: the field must be absent: %s", raw)
	}
}

func TestInsights_NoUsageEncodesEmptyArrays(t *testing.T) {
	out, err := newInsights(&fakeUsageStore{buckets: make([]float64, 7)}, nil).Insights(context.Background(), usageCaller{TenantID: "t1"}, usagewindow.Period7d)
	if err != nil {
		t.Fatalf("Insights: %v", err)
	}
	raw, _ := json.Marshal(out)
	var generic map[string]any
	_ = json.Unmarshal(raw, &generic)
	if generic["models"] == nil || generic["usage"] == nil {
		t.Errorf("models and usage must encode as arrays: %s", raw)
	}
}

func TestAgentBudgets(t *testing.T) {
	bundle := fakeBundle{principals: map[string]policy.PrincipalBudget{
		// Both windows, a per-run limit.
		agentNS + "coder": {EffectiveLimits: map[string]float64{"monthly": 100, "weekly": 30}, EffectivePerRunLimitUSD: 5},
		// A limit and no usage this month: a zero bar.
		agentNS + "idle": {EffectiveLimits: map[string]float64{"monthly": 50}},
		// A per-run limit only, and a zero limit: no window, no row.
		agentNS + "perrun": {EffectivePerRunLimitUSD: 2},
		agentNS + "zero":   {EffectiveLimits: map[string]float64{"monthly": 0}},
		// Not agent principals.
		"user-1":             {EffectiveLimits: map[string]float64{"monthly": 9}},
		"trigger-source:abc": {EffectiveLimits: map[string]float64{"monthly": 9}},
	}}
	store := &fakeUsageStore{buckets: make([]float64, 7), spend: []AgentBudgetSpend{{AgentSlug: "coder", Week: 7, Month: 40}}}
	caller := usageCaller{TenantID: "t1", Subject: "alice"}

	out, err := newInsights(store, bundle).Insights(context.Background(), caller, usagewindow.Period7d)
	if err != nil {
		t.Fatalf("Insights: %v", err)
	}
	if want := []string{"coder", "idle"}; !slices.Equal(sortedCopy(store.spendFor), want) || store.spendBy != caller {
		t.Errorf("spend asked for %v by %+v, want %v by the verified caller", store.spendFor, store.spendBy, want)
	}
	if len(out.Budgets) != 2 || out.Budgets[0].AgentSlug != "coder" || out.Budgets[1].AgentSlug != "idle" {
		t.Fatalf("budgets = %+v, want coder and idle only", out.Budgets)
	}
	coder := out.Budgets[0]
	if coder.PerRunLimitUSD != 5 || len(coder.Windows) != 2 {
		t.Fatalf("coder = %+v", coder)
	}
	monthly, weekly := coder.Windows[0], coder.Windows[1]
	if monthly.Cadence != "monthly" || monthly.LimitUSD != 100 || monthly.SpendUSD != 40 || !monthly.ResetsAt.Equal(time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("monthly = %+v", monthly)
	}
	// 3 Oct 2026 is a Saturday, so the week resets on Monday 5 Oct.
	if weekly.Cadence != "weekly" || weekly.LimitUSD != 30 || weekly.SpendUSD != 7 || !weekly.ResetsAt.Equal(time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("weekly = %+v", weekly)
	}
	if idle := out.Budgets[1]; len(idle.Windows) != 1 || idle.Windows[0].SpendUSD != 0 {
		t.Errorf("idle = %+v, want one window at zero spend", idle)
	}
}

func sortedCopy(s []string) []string {
	out := slices.Clone(s)
	slices.Sort(out)
	return out
}

func TestAgentUsage(t *testing.T) {
	store := &fakeUsageStore{}
	svc := newInsights(store, nil)
	caller := usageCaller{TenantID: "t1", Subject: "alice"}

	got, err := svc.AgentUsage(context.Background(), caller, usagewindow.Period30d, "notes")
	if err != nil || got.AgentSlug != "notes" || got.TotalTokens != 0 {
		t.Errorf("no rows = %+v, %v; want zero for the slug", got, err)
	}
	if q := store.queries[0]; q.Slug != "notes" || q.Caller != caller || q.Window.Period != usagewindow.Period30d {
		t.Errorf("query = %+v", q)
	}

	store.rows = []agentUsageRow{row("notes", "m1", 5, 0.5), row("notes", "m2", 2, 0.2)}
	if got, _ := svc.AgentUsage(context.Background(), caller, usagewindow.Period7d, "notes"); got.TotalTokens != 7 {
		t.Errorf("rows = %+v", got)
	}
}

func agentUsageRequest(path string, claims *token.PlatformClaims) *http.Request {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if claims != nil {
		req = req.WithContext(token.WithClaims(req.Context(), claims))
	}
	return req
}

func TestAgentUsageHandlers(t *testing.T) {
	log := logging.NewLoggerWithOutput(io.Discard)
	store := &fakeUsageStore{buckets: make([]float64, 15), rows: []agentUsageRow{row("coder", "m", 1, 1)}}
	claims := claimsWithRoles("t1", "alice", "")
	wired := (&Controller{logger: log}).WithAgentInsights(newInsights(store, nil))

	code := func(h func(http.ResponseWriter, *http.Request), req *http.Request) int {
		w := httptest.NewRecorder()
		h(w, req)
		return w.Code
	}

	t.Run("insights", func(t *testing.T) {
		w := httptest.NewRecorder()
		wired.handleAgentInsights(w, agentUsageRequest("/usage/agent-insights?period=90d", claims))
		var out AgentInsights
		if err := json.NewDecoder(w.Body).Decode(&out); w.Code != http.StatusOK || err != nil || out.Period != "90d" || len(out.Trend) != 15 {
			t.Errorf("status %d out %+v err %v", w.Code, out, err)
		}
	})
	t.Run("a bad period is refused", func(t *testing.T) {
		if got := code(wired.handleAgentInsights, agentUsageRequest("/usage/agent-insights?period=1y", claims)); got != http.StatusBadRequest {
			t.Errorf("status %d, want 400", got)
		}
	})
	t.Run("no claims or no tenant is refused", func(t *testing.T) {
		if got := code(wired.handleAgentInsights, agentUsageRequest("/usage/agent-insights", nil)); got != http.StatusUnauthorized {
			t.Errorf("no claims: status %d, want 401", got)
		}
		if got := code(wired.handleAgentInsights, agentUsageRequest("/usage/agent-insights", claimsWithRoles("", "svc", ""))); got != http.StatusUnauthorized {
			t.Errorf("no tenant: status %d, want 401", got)
		}
	})
	t.Run("one agent validates its slug", func(t *testing.T) {
		for slug, want := range map[string]int{"token-probe": http.StatusOK, "": http.StatusBadRequest, "probe%": http.StatusBadRequest, "a/b": http.StatusBadRequest} {
			req := agentUsageRequest("/usage/agent-type/x", claims)
			req.SetPathValue("slug", slug)
			if got := code(wired.handleAgentUsage, req); got != want {
				t.Errorf("slug %q: status %d, want %d", slug, got, want)
			}
		}
	})
}
