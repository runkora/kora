package inferencegw

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
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

func TestPredicate(t *testing.T) {
	clause, args := usageCaller{TenantID: "t1", Subject: "alice"}.where()
	if clause != whereOthers || !slices.Equal(args, []any{"t1", "alice"}) {
		t.Errorf("non-admin: %q %v", clause, args)
	}
	clause, args = usageCaller{TenantID: "t1", Subject: "root", Admin: true}.where()
	if clause != whereAdmin || !slices.Equal(args, []any{"t1"}) {
		t.Errorf("admin: %q %v", clause, args)
	}

	w := usagewindow.For(usagewindow.Period7d, time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC))
	clause, args = usageQuery{Caller: usageCaller{TenantID: "t1", Subject: "alice"}, Window: w, Slug: "coder"}.scope(w.Start, w.End)
	want := whereOthers + " AND created_at >= $3 AND created_at < $4 AND agent_slug = $5"
	if clause != want || len(args) != 5 || args[4] != "coder" {
		t.Errorf("scoped: %q %v", clause, args)
	}
}

// fakeUsageStore returns canned rollups and records the queries it was asked.
type fakeUsageStore struct {
	usage    []AgentTypeUsage
	models   []ModelSpend
	previous float64
	buckets  []float64
	spend    []AgentBudgetSpend
	err      error
	queries  []usageQuery
	spendFor []string
}

func (f *fakeUsageStore) AgentUsage(_ context.Context, q usageQuery) ([]AgentTypeUsage, error) {
	f.queries = append(f.queries, q)
	return f.usage, f.err
}
func (f *fakeUsageStore) AgentSpendByModel(context.Context, usageQuery) ([]ModelSpend, error) {
	return f.models, f.err
}
func (f *fakeUsageStore) AgentSpendBuckets(context.Context, usageQuery) (float64, []float64, error) {
	return f.previous, f.buckets, f.err
}
func (f *fakeUsageStore) AgentBudgetSpend(_ context.Context, _ string, slugs []string, _, _ time.Time) ([]AgentBudgetSpend, error) {
	f.spendFor = slugs
	return f.spend, f.err
}

type fakeBundle struct {
	principals map[string]policy.PrincipalBudget
	err        error
}

func (f fakeBundle) ListBudgetsStrict(context.Context, string) (map[string]policy.PrincipalBudget, map[string]policy.RoleBudget, error) {
	return f.principals, nil, f.err
}

const agentNS = "spiffe://orbit.test/tenant/orbit-tenant-example/agent/"

var testEnd = time.Date(2026, 10, 3, 14, 0, 0, 0, time.UTC) // a Saturday

func newInsights(store *fakeUsageStore, bundle policy.StrictBudgetLister) *AgentInsightsService {
	s := NewAgentInsightsService(store, bundle, logging.NewLoggerWithOutput(io.Discard))
	s.now = func() time.Time { return testEnd }
	return s
}

func TestInsights_BucketsTotalsAndPrevious(t *testing.T) {
	buckets := make([]float64, 7)
	buckets[6] = 5
	store := &fakeUsageStore{
		usage:    []AgentTypeUsage{{AgentSlug: "coder", EstimatedCostUSD: 1.5}, {AgentSlug: "notes", EstimatedCostUSD: 0.5}},
		previous: 3,
		buckets:  buckets,
	}
	caller := usageCaller{TenantID: "t1", Subject: "alice"}

	out, err := newInsights(store, nil).Insights(context.Background(), caller, usagewindow.Period7d, testEnd)
	if err != nil {
		t.Fatalf("Insights: %v", err)
	}
	if out.TotalCostUSD != 2 || out.PreviousTotalUSD != 3 {
		t.Errorf("total %v previous %v, want 2 and 3", out.TotalCostUSD, out.PreviousTotalUSD)
	}
	if len(out.Trend) != 7 || out.Trend[6].CostUSD != 5 || out.BucketSeconds != 86400 {
		t.Errorf("trend = %+v bucket %d", out.Trend, out.BucketSeconds)
	}
	if !out.Trend[0].Start.Equal(out.Start) || !out.End.Equal(testEnd) || out.End.Sub(out.Start) != 7*24*time.Hour {
		t.Errorf("window [%s, %s), first bucket %s", out.Start, out.End, out.Trend[0].Start)
	}
	if len(store.queries) != 1 || store.queries[0].Caller != caller {
		t.Errorf("the rollup must run for the verified caller: %+v", store.queries)
	}
	raw, _ := json.Marshal(out)
	var generic map[string]any
	_ = json.Unmarshal(raw, &generic)
	if generic["models"] == nil || generic["usage"] == nil {
		t.Errorf("models and usage must encode as arrays: %s", raw)
	}
	if _, has := generic["budgets"]; has {
		t.Errorf("no bundle: budgets must be absent: %s", raw)
	}
}

func TestInsights_StoreErrorSurfaces(t *testing.T) {
	boom := errors.New("db down")
	_, err := newInsights(&fakeUsageStore{err: boom, buckets: make([]float64, 7)}, nil).
		Insights(context.Background(), usageCaller{TenantID: "t1"}, usagewindow.Period7d, testEnd)
	if !errors.Is(err, boom) {
		t.Errorf("err = %v, want %v", err, boom)
	}
}

func TestAgentBudgets(t *testing.T) {
	bundle := fakeBundle{principals: map[string]policy.PrincipalBudget{
		// Both windows, a per-run limit.
		agentNS + "coder": {EffectiveLimits: map[string]float64{"monthly": 100, "weekly": 30}, EffectivePerRunLimitUSD: 5},
		// A limit and no usage this month: a zero bar.
		agentNS + "idle": {EffectiveLimits: map[string]float64{"monthly": 50}},
		// Delegated, found from its rows: no row for anyone.
		agentNS + "notes": {EffectiveLimits: map[string]float64{"monthly": 10}},
		// A per-run limit only, and a zero limit: no window, no row.
		agentNS + "perrun": {EffectivePerRunLimitUSD: 2},
		agentNS + "zero":   {EffectiveLimits: map[string]float64{"monthly": 0}},
		// A sandbox principal is not an agent budget key; a human is not an agent.
		"user-1":             {EffectiveLimits: map[string]float64{"monthly": 9}},
		"trigger-source:abc": {EffectiveLimits: map[string]float64{"monthly": 9}},
	}}
	store := &fakeUsageStore{buckets: make([]float64, 7), spend: []AgentBudgetSpend{
		{AgentSlug: "coder", Week: 7, Month: 40},
		{AgentSlug: "notes", Week: 1, Month: 2, Delegated: true},
	}}

	out, err := newInsights(store, bundle).Insights(context.Background(), usageCaller{TenantID: "t1"}, usagewindow.Period7d, testEnd)
	if err != nil {
		t.Fatalf("Insights: %v", err)
	}
	if want := []string{"coder", "idle", "notes"}; !slices.Equal(sortedCopy(store.spendFor), want) {
		t.Errorf("spend asked for %v, want %v", store.spendFor, want)
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

func TestAgentBudgets_BundleFailureLeavesThemOut(t *testing.T) {
	store := &fakeUsageStore{buckets: make([]float64, 7)}
	out, err := newInsights(store, fakeBundle{err: errors.New("bundle not loaded")}).
		Insights(context.Background(), usageCaller{TenantID: "t1"}, usagewindow.Period7d, testEnd)
	if err != nil {
		t.Fatalf("a budget failure must not fail the page: %v", err)
	}
	if out.Budgets != nil || store.spendFor != nil {
		t.Errorf("budgets %+v spend asked %v: nothing may be shown as no budget", out.Budgets, store.spendFor)
	}
}

func TestAgentUsage(t *testing.T) {
	store := &fakeUsageStore{}
	svc := newInsights(store, nil)
	caller := usageCaller{TenantID: "t1", Subject: "alice"}

	got, err := svc.AgentUsage(context.Background(), caller, usagewindow.Period30d, testEnd, "notes")
	if err != nil || got.AgentSlug != "notes" || got.TotalTokens != 0 {
		t.Errorf("no rows = %+v, %v; want zero for the slug", got, err)
	}
	if q := store.queries[0]; q.Slug != "notes" || q.Caller != caller || q.Window.Period != usagewindow.Period30d {
		t.Errorf("query = %+v", q)
	}

	store.usage = []AgentTypeUsage{{AgentSlug: "notes", TotalTokens: 5}}
	if got, _ := svc.AgentUsage(context.Background(), caller, usagewindow.Period7d, testEnd, "notes"); got.TotalTokens != 5 {
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
	store := &fakeUsageStore{buckets: make([]float64, 15), usage: []AgentTypeUsage{{AgentSlug: "coder", EstimatedCostUSD: 1}}}
	svc := newInsights(store, nil)
	claims := claimsWithRoles("t1", "alice", "")
	wired := (&Controller{logger: log}).WithAgentInsights(svc)

	code := func(c *Controller, h func(http.ResponseWriter, *http.Request), req *http.Request) int {
		w := httptest.NewRecorder()
		h(w, req)
		return w.Code
	}

	t.Run("insights", func(t *testing.T) {
		w := httptest.NewRecorder()
		wired.handleAgentInsights(w, agentUsageRequest("/usage/agent-insights?period=90d&end=2026-10-03T14:00:00Z", claims))
		var out AgentInsights
		if err := json.NewDecoder(w.Body).Decode(&out); w.Code != http.StatusOK || err != nil || out.Period != "90d" || len(out.Trend) != 15 || !out.End.Equal(testEnd) {
			t.Errorf("status %d out %+v err %v", w.Code, out, err)
		}
	})
	t.Run("defaults to 7d", func(t *testing.T) {
		w := httptest.NewRecorder()
		wired.handleAgentInsights(w, agentUsageRequest("/usage/agent-insights", claims))
		var out AgentInsights
		_ = json.NewDecoder(w.Body).Decode(&out)
		if w.Code != http.StatusOK || out.Period != "7d" {
			t.Errorf("status %d period %q", w.Code, out.Period)
		}
	})
	t.Run("bad period or end", func(t *testing.T) {
		for _, p := range []string{"/usage/agent-insights?period=1y", "/usage/agent-insights?end=yesterday"} {
			if got := code(wired, wired.handleAgentInsights, agentUsageRequest(p, claims)); got != http.StatusBadRequest {
				t.Errorf("%s: status %d, want 400", p, got)
			}
		}
	})
	t.Run("no claims or no tenant", func(t *testing.T) {
		if got := code(wired, wired.handleAgentInsights, agentUsageRequest("/usage/agent-insights", nil)); got != http.StatusUnauthorized {
			t.Errorf("no claims: status %d, want 401", got)
		}
		if got := code(wired, wired.handleAgentInsights, agentUsageRequest("/usage/agent-insights", claimsWithRoles("", "svc", ""))); got != http.StatusUnauthorized {
			t.Errorf("no tenant: status %d, want 401", got)
		}
	})
	t.Run("not wired", func(t *testing.T) {
		c := &Controller{logger: log}
		if got := code(c, c.handleAgentInsights, agentUsageRequest("/usage/agent-insights", claims)); got != http.StatusServiceUnavailable {
			t.Errorf("status %d, want 503", got)
		}
	})
	t.Run("one agent validates its slug", func(t *testing.T) {
		for slug, want := range map[string]int{"token-probe": http.StatusOK, "": http.StatusBadRequest, "probe%": http.StatusBadRequest, "a/b": http.StatusBadRequest} {
			req := agentUsageRequest("/usage/agent-type/x", claims)
			req.SetPathValue("slug", slug)
			if got := code(wired, wired.handleAgentUsage, req); got != want {
				t.Errorf("slug %q: status %d, want %d", slug, got, want)
			}
		}
	})
}
