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

	"github.com/clear-route/orbit/internal/platform/logging"
	"github.com/clear-route/orbit/internal/platform/policy"
	"github.com/clear-route/orbit/internal/platform/token"
	"github.com/clear-route/orbit/internal/platform/usagewindow"
)

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

func newInsights(repo *mockInferenceUsageRepository, bundle policy.Evaluator) *Service {
	if bundle == nil {
		bundle = fakeBundle{}
	}
	return &Service{usage: repo, policy: bundle, logger: logging.NewLoggerWithOutput(io.Discard)}
}

func row(slug, model string, tokens int64, cost float64) AgentModelUsage {
	return AgentModelUsage{AgentTypeUsage: AgentTypeUsage{AgentSlug: slug, PromptTokens: tokens, TotalTokens: tokens, EstimatedCostUSD: cost}, ModelID: model}
}

func TestInsights_FoldsUsageAndBuckets(t *testing.T) {
	buckets := make([]float64, 7)
	buckets[6] = 5
	repo := &mockInferenceUsageRepository{
		agentRows:     []AgentModelUsage{row("coder", "m1", 10, 1), row("coder", "m2", 20, 0.5), row("notes", "m1", 5, 0.5), row("free", "m3", 1, 0)},
		agentPrevious: 3,
		agentBuckets:  buckets,
	}

	out, err := newInsights(repo, nil).Insights(context.Background(), "t1", usagewindow.Period7d, testEnd)
	if err != nil {
		t.Fatalf("Insights: %v", err)
	}
	if out.TotalCostUSD != 2 || out.PreviousTotalUSD != 3 {
		t.Errorf("total %v previous %v, want 2 and 3", out.TotalCostUSD, out.PreviousTotalUSD)
	}
	if len(out.Usage) != 3 || out.Usage[0].AgentSlug != "coder" || out.Usage[0].TotalTokens != 30 || out.Usage[0].EstimatedCostUSD != 1.5 {
		t.Errorf("usage = %+v, want coder folded across its models", out.Usage)
	}
	if len(out.Models) != 2 || out.Models[0].ModelID != "m1" || out.Models[0].EstimatedCostUSD != 1.5 || out.Models[1].ModelID != "m2" {
		t.Errorf("models = %+v, want m1 then m2 and no free model", out.Models)
	}
	if len(out.Trend) != 7 || out.Trend[6].CostUSD != 5 || !out.Trend[0].Start.Equal(testEnd.Add(-7*24*time.Hour)) {
		t.Errorf("trend = %+v", out.Trend)
	}
	if repo.agentTenant != "t1" {
		t.Errorf("the rollup ran for tenant %q, want t1", repo.agentTenant)
	}

	// An empty window must encode arrays, which the page reads.
	empty, _ := newInsights(&mockInferenceUsageRepository{agentBuckets: make([]float64, 7)}, nil).Insights(context.Background(), "t1", usagewindow.Period7d, testEnd)
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
	repo := &mockInferenceUsageRepository{agentBuckets: make([]float64, 7), agentSpend: []AgentBudgetSpend{{AgentSlug: "coder", Week: 7, Month: 40}}}

	out, err := newInsights(repo, bundle).Insights(context.Background(), "t1", usagewindow.Period7d, testEnd)
	if err != nil {
		t.Fatalf("Insights: %v", err)
	}
	if slices.Sort(repo.agentSpendFor); !slices.Equal(repo.agentSpendFor, []string{"coder", "idle"}) {
		t.Errorf("spend asked for %v, want coder and idle", repo.agentSpendFor)
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
		svc:    newInsights(&mockInferenceUsageRepository{agentBuckets: make([]float64, 7)}, nil),
		logger: logging.NewLoggerWithOutput(io.Discard),
	}
	claims := func(tenant string) *token.PlatformClaims {
		return &token.PlatformClaims{Orbit: token.OrbitClaims{TenantID: tenant, PrincipalType: token.PrincipalTypeHuman}}
	}
	for _, tc := range []struct {
		name    string
		handler func(http.ResponseWriter, *http.Request)
		path    string
		tenant  string
		slug    string
		want    int
	}{
		{"a token with no tenant", c.handleAgentInsights, "/usage/agent-insights", "", "", http.StatusUnauthorized},
		{"a bad period", c.handleAgentInsights, "/usage/agent-insights?period=1y", "t1", "", http.StatusBadRequest},
		{"a valid slug", c.handleAgentUsage, "/usage/agent-type/x", "t1", "token-probe", http.StatusOK},
		{"a slug with a symbol", c.handleAgentUsage, "/usage/agent-type/x", "t1", "probe%", http.StatusBadRequest},
		{"a slug with a path", c.handleAgentUsage, "/usage/agent-type/x", "t1", "a/b", http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			req.SetPathValue("slug", tc.slug)
			req = req.WithContext(token.WithClaims(req.Context(), claims(tc.tenant)))
			w := httptest.NewRecorder()
			tc.handler(w, req)
			if w.Code != tc.want {
				t.Errorf("status %d, want %d", w.Code, tc.want)
			}
		})
	}
}
