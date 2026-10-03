package inferencegw

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/clear-route/orbit/internal/platform/logging"
	"github.com/clear-route/orbit/internal/platform/policy"
	"github.com/clear-route/orbit/internal/platform/token"
	"github.com/clear-route/orbit/internal/platform/usagewindow"
)

// The Agents page rollups. Usage is agent traffic only (rows with an
// agent_slug), and a delegated agent's rows carry the delegator, so one SQL
// predicate decides what a caller sees: every row without a delegator, the
// caller's own delegated rows, and for a platform admin every row. The route
// scope agents:types:read is a prefix rule, so a caller who reaches these routes
// may read every agent type and no per-agent check is needed.

// rolePlatformAdmin is the reserved admin role. It mirrors the agents service,
// which reads it from the verified roles claim in the same way.
const rolePlatformAdmin = "platform-admin"

// usageCaller is the verified identity a rollup is computed for.
type usageCaller struct {
	TenantID string
	Subject  string
	Admin    bool
}

// callerFromClaims reads the caller from verified claims. Admin comes from the
// roles claim only: a token without one, such as a PAT, is never admin, and the
// gateway's user_profiles role fallback is not consulted.
func callerFromClaims(claims *token.PlatformClaims) usageCaller {
	return usageCaller{
		TenantID: claims.Orbit.TenantID,
		Subject:  claims.Subject,
		Admin:    slices.Contains(extractRolesFromClaims(claims), rolePlatformAdmin),
	}
}

// usageQuery is one rollup read: whose view, which window, and optionally one agent.
type usageQuery struct {
	Caller usageCaller
	Window usagewindow.Window
	Slug   string
}

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

// BudgetWindow is one budget window of an agent: its limit, the spend inside
// the window now, and when it resets.
type BudgetWindow struct {
	Cadence  string    `json:"cadence"`
	LimitUSD float64   `json:"limit_usd"`
	SpendUSD float64   `json:"spend_usd"`
	ResetsAt time.Time `json:"resets_at"`
}

// AgentBudgetRow is an agent's budget: one window per cadence it has a limit
// on, and the per-run limit that has no window. Spend is by agent slug, which
// includes the agent's per-run sandbox principals and can differ from the
// per-principal spend enforcement evaluates.
type AgentBudgetRow struct {
	AgentSlug      string         `json:"agent_slug"`
	PerRunLimitUSD float64        `json:"per_run_limit_usd,omitempty"`
	Windows        []BudgetWindow `json:"windows"`
}

// AgentInsights is what the Agents page reports for one window, over the usage
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
	// AllDelegatedUsage is true when the caller sees every row of a delegated
	// agent, which a platform admin does. Otherwise a delegated agent's usage
	// here is the caller's own, and the page says so.
	AllDelegatedUsage bool `json:"all_delegated_usage"`
	// Budgets is absent when the policy bundle cannot be read, so "unknown"
	// never shows as "no budget".
	Budgets []AgentBudgetRow `json:"budgets,omitempty"`
}

// AgentBudgetSpend is an agent's spend since the start of the current week and
// month. Delegated marks an agent that has delegated rows in that span: its
// budget is a pool shared by every delegator, so it gets no budget row.
type AgentBudgetSpend struct {
	AgentSlug string
	Week      float64
	Month     float64
	Delegated bool
}

// agentUsageStore is the storage the agent rollups read.
type agentUsageStore interface {
	// AgentUsage returns tokens and cost per agent, or for one agent when q.Slug is set.
	AgentUsage(ctx context.Context, q usageQuery) ([]AgentTypeUsage, error)
	// AgentSpendByModel returns the window's spend per model, largest first.
	AgentSpendByModel(ctx context.Context, q usageQuery) ([]ModelSpend, error)
	// AgentSpendBuckets returns the previous window's total and the spend in
	// each bucket of the window.
	AgentSpendBuckets(ctx context.Context, q usageQuery) (previous float64, buckets []float64, err error)
	// AgentBudgetSpend returns each listed agent's spend since the week and month starts.
	AgentBudgetSpend(ctx context.Context, tenantID string, slugs []string, weekStart, monthStart time.Time) ([]AgentBudgetSpend, error)
}

// AgentInsightsService builds the Agents page rollups from the usage log.
type AgentInsightsService struct {
	store   agentUsageStore
	budgets policy.StrictBudgetLister // nil when the evaluator cannot report a bundle failure
	logger  logging.Logger
	now     func() time.Time
}

// NewAgentInsightsService creates the service. budgets may be nil, which leaves
// the budgets part out of every response.
func NewAgentInsightsService(store agentUsageStore, budgets policy.StrictBudgetLister, logger logging.Logger) *AgentInsightsService {
	return &AgentInsightsService{store: store, budgets: budgets, logger: logger, now: time.Now}
}

// Insights returns the window's usage, model split, spend trend, previous
// total and budgets. The queries are independent, so they run together.
func (s *AgentInsightsService) Insights(ctx context.Context, caller usageCaller, period usagewindow.Period, end time.Time) (AgentInsights, error) {
	w := usagewindow.For(period, end)
	q := usageQuery{Caller: caller, Window: w}

	var (
		usage    []AgentTypeUsage
		models   []ModelSpend
		previous float64
		buckets  []float64
		budgets  []AgentBudgetRow
	)
	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() (err error) { usage, err = s.store.AgentUsage(gctx, q); return err })
	g.Go(func() (err error) { models, err = s.store.AgentSpendByModel(gctx, q); return err })
	g.Go(func() (err error) { previous, buckets, err = s.store.AgentSpendBuckets(gctx, q); return err })
	g.Go(func() error {
		// A budget failure leaves the budgets out and does not fail the page.
		budgets = s.agentBudgets(gctx, caller.TenantID)
		return nil
	})
	if err := g.Wait(); err != nil {
		return AgentInsights{}, fmt.Errorf("agent insights: %w", err)
	}

	n := w.Buckets()
	out := AgentInsights{
		Period:            string(period),
		Start:             w.Start,
		End:               w.End,
		BucketSeconds:     int64(w.Bucket / time.Second),
		Usage:             orEmpty(usage),
		Models:            orEmpty(models),
		Trend:             make([]TrendPoint, n),
		Budgets:           budgets,
		AllDelegatedUsage: caller.Admin,
	}
	for _, u := range usage {
		out.TotalCostUSD += u.EstimatedCostUSD
	}
	for i := range out.Trend {
		out.Trend[i] = TrendPoint{Start: w.Start.Add(time.Duration(i) * w.Bucket), CostUSD: buckets[i]}
	}
	out.PreviousTotalUSD = previous
	return out, nil
}

// AgentUsage returns one agent's tokens and spend for the window, under the
// same predicate as Insights. An agent the caller sees no rows of reads as zero.
func (s *AgentInsightsService) AgentUsage(ctx context.Context, caller usageCaller, period usagewindow.Period, end time.Time, slug string) (AgentTypeUsage, error) {
	rows, err := s.store.AgentUsage(ctx, usageQuery{Caller: caller, Window: usagewindow.For(period, end), Slug: slug})
	if err != nil {
		return AgentTypeUsage{}, fmt.Errorf("agent usage: %w", err)
	}
	if len(rows) == 0 {
		return AgentTypeUsage{AgentSlug: slug}, nil
	}
	return rows[0], nil
}

func orEmpty[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

// agentBudgets builds the budget rows from the policy bundle, the same compiled
// data enforcement evaluates, and the agents' spend in each window. It returns
// nil when the bundle cannot be read. An agent with no window limit has no row.
// A delegated agent has no row either: its limit is shared by every person who
// runs it, so a bar of what one person spent against it misleads. The gateway
// cannot read an agent's identity mode, so an agent counts as delegated when it
// has delegated usage rows in the current week or month.
func (s *AgentInsightsService) agentBudgets(ctx context.Context, tenantID string) []AgentBudgetRow {
	if s.budgets == nil {
		return nil
	}
	principals, _, err := s.budgets.ListBudgetsStrict(ctx, tenantID)
	if err != nil {
		s.logger.Warn(ctx, "agent budgets unavailable: policy bundle not readable",
			logging.Field{Key: "error", Value: err.Error()})
		return nil
	}

	now := s.now().UTC()
	rows := budgetRowsFromBundle(principals, now)
	if len(rows) == 0 {
		return nil
	}

	slugs := make([]string, len(rows))
	for i, r := range rows {
		slugs[i] = r.AgentSlug
	}
	spend, err := s.store.AgentBudgetSpend(ctx, tenantID, slugs, periodStart(cadenceWeekly, now), periodStart(cadenceMonthly, now))
	if err != nil {
		s.logger.Warn(ctx, "agent budget spend unavailable", logging.Field{Key: "error", Value: err.Error()})
		return nil
	}
	spendBySlug := make(map[string]AgentBudgetSpend, len(spend))
	for _, sp := range spend {
		spendBySlug[sp.AgentSlug] = sp
	}

	out := make([]AgentBudgetRow, 0, len(rows))
	for _, row := range rows {
		sp := spendBySlug[row.AgentSlug]
		if sp.Delegated {
			continue
		}
		for i := range row.Windows {
			if row.Windows[i].Cadence == cadenceWeekly {
				row.Windows[i].SpendUSD = sp.Week
			} else {
				row.Windows[i].SpendUSD = sp.Month
			}
		}
		out = append(out, row)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].AgentSlug < out[j].AgentSlug })
	return out
}

// budgetRowsFromBundle turns the bundle's per-principal budgets into rows for
// agent principals, with the windows that have a limit and no spend yet.
func budgetRowsFromBundle(principals map[string]policy.PrincipalBudget, now time.Time) []AgentBudgetRow {
	rows := make([]AgentBudgetRow, 0)
	for pid, pb := range principals {
		slug := agentSlugFromPrincipal(string(token.PrincipalTypeAgent), pid)
		if slug == "" {
			continue
		}
		row := AgentBudgetRow{AgentSlug: slug, PerRunLimitUSD: pb.EffectivePerRunLimitUSD}
		for _, cadence := range []string{cadenceMonthly, cadenceWeekly} {
			// A zero limit is no limit: the bundle drops it, and so does this.
			if limit := pb.EffectiveLimits[cadence]; limit > 0 {
				row.Windows = append(row.Windows, BudgetWindow{Cadence: cadence, LimitUSD: limit, ResetsAt: periodEnd(cadence, now)})
			}
		}
		if len(row.Windows) > 0 {
			rows = append(rows, row)
		}
	}
	return rows
}

// cadenceMonthly is the budget reset cadence that periodStart treats as the default.
const cadenceMonthly = "monthly"

// periodEnd is when the budget window that contains t resets.
func periodEnd(cadence string, t time.Time) time.Time {
	start := periodStart(cadence, t)
	if cadence == cadenceWeekly {
		return start.AddDate(0, 0, 7)
	}
	return start.AddDate(0, 1, 0)
}
