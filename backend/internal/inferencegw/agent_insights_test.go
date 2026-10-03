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
	for roles, admin := range map[string]bool{"users, platform-admin,engineer": true, "": false, "users": false, "platform-administrator": false} {
		if got := callerFromClaims(claimsWithRoles("t1", "alice", roles)); got.Admin != admin || got.TenantID != "t1" || got.Subject != "alice" {
			t.Errorf("roles %q: %+v, want admin %v", roles, got, admin)
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
	if len(out.Trend) != 7 || out.Trend[6].CostUSD != 5 || !out.Trend[0].Start.Equal(testEnd.Add(-7*24*time.Hour)) {
		t.Errorf("trend = %+v", out.Trend)
	}
	if len(store.queries) != 1 || store.queries[0].Caller != caller {
		t.Errorf("the rollup must run for the verified caller: %+v", store.queries)
	}

	// An empty window must encode arrays, which the page reads.
	empty, _ := newInsights(&fakeUsageStore{buckets: make([]float64, 7)}, nil).Insights(context.Background(), caller, usagewindow.Period7d)
	raw, _ := json.Marshal(empty)
	if strings.Contains(string(raw), `"usage":null`) || strings.Contains(string(raw), `"models":null`) {
		t.Errorf("empty usage and models must encode as arrays: %s", raw)
	}
}

func TestAgentBudgets(t *testing.T) {
	bundle := fakeBundle{principals: map[string]policy.PrincipalBudget{
		agentNS + "coder":  {EffectiveLimits: map[string]float64{"monthly": 100, "weekly": 30}, EffectivePerRunLimitUSD: 5},
		agentNS + "idle":   {EffectiveLimits: map[string]float64{"monthly": 50}}, // a limit and no usage: a zero bar
		agentNS + "perrun": {EffectivePerRunLimitUSD: 2},                         // no window limit, no row
		agentNS + "zero":   {EffectiveLimits: map[string]float64{"monthly": 0}},  // a zero limit is no limit
		"user-1":           {EffectiveLimits: map[string]float64{"monthly": 9}},  // not an agent
	}}
	store := &fakeUsageStore{buckets: make([]float64, 7), spend: []AgentBudgetSpend{{AgentSlug: "coder", Week: 7, Month: 40}}}
	caller := usageCaller{TenantID: "t1", Subject: "alice"}

	out, err := newInsights(store, bundle).Insights(context.Background(), caller, usagewindow.Period7d)
	if err != nil {
		t.Fatalf("Insights: %v", err)
	}
	if slices.Sort(store.spendFor); !slices.Equal(store.spendFor, []string{"coder", "idle"}) || store.spendBy != caller {
		t.Errorf("spend asked for %v by %+v, want coder and idle by the verified caller", store.spendFor, store.spendBy)
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

func TestAgentUsageHandlers(t *testing.T) {
	c := &Controller{
		insights: newInsights(&fakeUsageStore{buckets: make([]float64, 7)}, nil),
		logger:   logging.NewLoggerWithOutput(io.Discard),
	}
	serve := func(h func(http.ResponseWriter, *http.Request), path string, claims *token.PlatformClaims, slug string) int {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.SetPathValue("slug", slug)
		if claims != nil {
			req = req.WithContext(token.WithClaims(req.Context(), claims))
		}
		w := httptest.NewRecorder()
		h(w, req)
		return w.Code
	}
	alice := claimsWithRoles("t1", "alice", "")

	if got := serve(c.handleAgentInsights, "/usage/agent-insights", claimsWithRoles("", "svc", ""), ""); got != http.StatusUnauthorized {
		t.Errorf("a token with no tenant: status %d, want 401", got)
	}
	if got := serve(c.handleAgentInsights, "/usage/agent-insights?period=1y", alice, ""); got != http.StatusBadRequest {
		t.Errorf("a bad period: status %d, want 400", got)
	}
	for slug, want := range map[string]int{"token-probe": http.StatusOK, "probe%": http.StatusBadRequest, "a/b": http.StatusBadRequest} {
		if got := serve(c.handleAgentUsage, "/usage/agent-type/x", alice, slug); got != want {
			t.Errorf("slug %q: status %d, want %d", slug, got, want)
		}
	}
}
