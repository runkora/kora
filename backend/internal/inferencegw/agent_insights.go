package inferencegw

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"golang.org/x/sync/errgroup"

	agentsclient "github.com/clear-route/orbit/internal/platform/clients/agents"
	"github.com/clear-route/orbit/internal/platform/usagewindow"
)

// ErrAgentVisibilityUnavailable is returned when the gateway cannot learn which
// agents the caller may see. The rollups fail closed: no answer is better than
// one that might include another person's delegated agent.
var ErrAgentVisibilityUnavailable = errors.New("agent visibility unavailable")

// AgentUsageScope selects the usage rows an agent rollup may read: every row of
// an AllRows agent, and the rows of an OwnRows agent whose delegator is Subject.
type AgentUsageScope struct {
	TenantID string
	AllRows  []string
	OwnRows  []string
	Subject  string
}

// empty reports whether the scope can match no row at all.
func (s AgentUsageScope) empty() bool { return len(s.AllRows) == 0 && len(s.OwnRows) == 0 }

// ModelSpend is the spend on one model.
type ModelSpend struct {
	ModelID string  `json:"model_id"`
	CostUSD float64 `json:"cost_usd"`
}

// TrendPoint is the spend in one chart bucket, identified by where it starts.
type TrendPoint struct {
	Start   time.Time `json:"start"`
	CostUSD float64   `json:"cost_usd"`
}

// SpendBuckets is a window's spend cut into chart buckets, with the total for
// the window before it.
type SpendBuckets struct {
	PreviousUSD float64
	Buckets     []float64
}

// AgentInsights is what the Agents page reports for one window, over the agents
// the caller may see.
type AgentInsights struct {
	Period           string           `json:"period"`
	Start            time.Time        `json:"start"`
	End              time.Time        `json:"end"`
	BucketSeconds    int64            `json:"bucket_seconds"`
	Usage            []AgentTypeUsage `json:"usage"`
	TotalCostUSD     float64          `json:"total_cost_usd"`
	PreviousTotalUSD float64          `json:"previous_total_usd"`
	Models           []ModelSpend     `json:"models"`
	Trend            []TrendPoint     `json:"trend"`
}

// AgentBudgetSpend is an agent's spend in the current budget windows, counted
// the way budget enforcement counts them: an ISO week from Monday and a
// calendar month, both in UTC.
type AgentBudgetSpend struct {
	AgentSlug      string  `json:"agent_slug"`
	WeekToDateUSD  float64 `json:"week_to_date_usd"`
	MonthToDateUSD float64 `json:"month_to_date_usd"`
}

// AgentBudgets is the budget-window spend of the agents whose spend the caller
// may see in full.
type AgentBudgets struct {
	WeekStart  time.Time          `json:"week_start"`
	MonthStart time.Time          `json:"month_start"`
	Agents     []AgentBudgetSpend `json:"agents"`
}

// cadenceMonthly is the budget reset cadence that periodStart treats as the default.
const cadenceMonthly = "monthly"

// agentUsageStore is the storage the agent rollups read.
type agentUsageStore interface {
	// AgentUsage returns tokens and cost per agent for the window.
	AgentUsage(ctx context.Context, scope AgentUsageScope, w usagewindow.Window) ([]AgentTypeUsage, error)
	// AgentSpendByModel returns the window's spend per model, largest first.
	AgentSpendByModel(ctx context.Context, scope AgentUsageScope, w usagewindow.Window) ([]ModelSpend, error)
	// AgentSpendBuckets returns the window's spend per chart bucket and the
	// previous window's total.
	AgentSpendBuckets(ctx context.Context, scope AgentUsageScope, w usagewindow.Window) (SpendBuckets, error)
	// AgentBudgetSpend returns each listed agent's spend since the start of the
	// current week and month.
	AgentBudgetSpend(ctx context.Context, tenantID string, slugs []string, weekStart, monthStart time.Time) ([]AgentBudgetSpend, error)
}

// visibleAgents is the cache the rollups ask for the caller's agents.
type visibleAgents interface {
	Visible(ctx context.Context, tenantID, bearer string) (agentsclient.Visibility, error)
}

// AgentInsightsService builds the Agents page rollups from the usage log,
// limited to the agents the caller may see.
type AgentInsightsService struct {
	store      agentUsageStore
	visibility visibleAgents
	now        func() time.Time
}

// NewAgentInsightsService creates the service. src is called at most once per
// token every few seconds (see visibilityTTL).
func NewAgentInsightsService(store agentUsageStore, src agentVisibilitySource) *AgentInsightsService {
	return &AgentInsightsService{store: store, visibility: newVisibilityCache(src, visibilityTTL), now: time.Now}
}

func (s *AgentInsightsService) scope(ctx context.Context, tenantID, subject, bearer string) (AgentUsageScope, error) {
	v, err := s.visibility.Visible(ctx, tenantID, bearer)
	if err != nil {
		return AgentUsageScope{}, fmt.Errorf("%w: %w", ErrAgentVisibilityUnavailable, err)
	}
	return AgentUsageScope{TenantID: tenantID, AllRows: v.AllRows, OwnRows: v.OwnRows, Subject: subject}, nil
}

// Insights returns the window's usage, model split, spend trend and previous
// total. The three queries are independent once visibility is known, so they
// run together.
func (s *AgentInsightsService) Insights(ctx context.Context, tenantID, subject, bearer string, period usagewindow.Period) (AgentInsights, error) {
	scope, err := s.scope(ctx, tenantID, subject, bearer)
	if err != nil {
		return AgentInsights{}, err
	}
	w := usagewindow.For(period, s.now())
	out := AgentInsights{
		Period:        string(period),
		Start:         w.Start,
		End:           w.End,
		BucketSeconds: int64(w.Bucket / time.Second),
		Usage:         []AgentTypeUsage{},
		Models:        []ModelSpend{},
		Trend:         make([]TrendPoint, w.Buckets()),
	}
	for i := range out.Trend {
		out.Trend[i].Start = w.Origin.Add(time.Duration(i) * w.Bucket)
	}
	if scope.empty() {
		return out, nil
	}

	var (
		usage   []AgentTypeUsage
		models  []ModelSpend
		buckets SpendBuckets
	)
	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() (err error) {
		usage, err = s.store.AgentUsage(gctx, scope, w)
		return err
	})
	g.Go(func() (err error) {
		models, err = s.store.AgentSpendByModel(gctx, scope, w)
		return err
	})
	g.Go(func() (err error) {
		buckets, err = s.store.AgentSpendBuckets(gctx, scope, w)
		return err
	})
	if err := g.Wait(); err != nil {
		return AgentInsights{}, fmt.Errorf("agent insights: %w", err)
	}

	out.Usage = usage
	for _, u := range usage {
		out.TotalCostUSD += u.EstimatedCostUSD
	}
	if models != nil {
		out.Models = models
	}
	out.PreviousTotalUSD = buckets.PreviousUSD
	for i := range out.Trend {
		if i < len(buckets.Buckets) {
			out.Trend[i].CostUSD = buckets.Buckets[i]
		}
	}
	return out, nil
}

// Budgets returns the budget-window spend of the agents whose usage the caller
// may see in full. A delegated agent seen only through the caller's own runs is
// left out: its limit applies to everyone's runs together, so the caller's own
// share would read as the whole and understate the spend.
func (s *AgentInsightsService) Budgets(ctx context.Context, tenantID, subject, bearer string) (AgentBudgets, error) {
	scope, err := s.scope(ctx, tenantID, subject, bearer)
	if err != nil {
		return AgentBudgets{}, err
	}
	now := s.now()
	out := AgentBudgets{
		WeekStart:  periodStart(cadenceWeekly, now),
		MonthStart: periodStart(cadenceMonthly, now),
		Agents:     []AgentBudgetSpend{},
	}
	if len(scope.AllRows) == 0 {
		return out, nil
	}
	rows, err := s.store.AgentBudgetSpend(ctx, tenantID, scope.AllRows, out.WeekStart, out.MonthStart)
	if err != nil {
		return AgentBudgets{}, fmt.Errorf("agent budgets: %w", err)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].AgentSlug < rows[j].AgentSlug })
	if rows != nil {
		out.Agents = rows
	}
	return out, nil
}
