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

// The Agents page rollups cover agent traffic only. One SQL predicate decides
// what a caller sees (see where in agent_insights_postgres.go). The route scope
// agents:types:read is a prefix rule, so a caller who passes it may read every
// agent and no per-agent check is needed.

// rolePlatformAdmin is the reserved admin role, read from the roles claim as the agents service does.
const rolePlatformAdmin = "platform-admin"

// usageCaller is the verified identity a rollup is computed for.
type usageCaller struct {
	TenantID string
	Subject  string
	Admin    bool
}

// callerFromClaims reads the caller from verified claims. Admin comes from the
// roles claim alone, so a PAT is never admin and the user_profiles fallback is not used.
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

// AgentBudgetRow is an agent's budget: a window per cadence it has a limit on,
// and the per-run limit, which has no window. Spend is by agent slug, which
// includes the agent's per-run sandbox principals, so it can differ slightly
// from the per-principal spend the limit check counts.
type AgentBudgetRow struct {
	AgentSlug      string         `json:"agent_slug"`
	PerRunLimitUSD float64        `json:"per_run_limit_usd,omitempty"`
	Windows        []BudgetWindow `json:"windows"`
}

// AgentInsights is what the Agents page reports for one window, over the usage
// the caller may see.
type AgentInsights struct {
	Period           string           `json:"period"`
	Usage            []AgentTypeUsage `json:"usage"`
	TotalCostUSD     float64          `json:"total_cost_usd"`
	PreviousTotalUSD float64          `json:"previous_total_usd"`
	Models           []ModelSpend     `json:"models"`
	Trend            []TrendPoint     `json:"trend"`
	// AllDelegatedUsage is true when the caller sees every row of a delegated
	// agent, as a platform admin does. Otherwise it is the caller's own.
	AllDelegatedUsage bool             `json:"all_delegated_usage"`
	Budgets           []AgentBudgetRow `json:"budgets,omitempty"`
}

// agentUsageRow is one agent's usage on one model.
type agentUsageRow struct {
	AgentTypeUsage
	ModelID string
}

// AgentBudgetSpend is an agent's spend since the start of the current week and month.
type AgentBudgetSpend struct {
	AgentSlug string
	Week      float64
	Month     float64
}

// agentUsageStore is the storage the agent rollups read.
type agentUsageStore interface {
	// AgentUsage returns usage per agent and model, or for one agent when q.Slug is set.
	AgentUsage(ctx context.Context, q usageQuery) ([]agentUsageRow, error)
	// AgentSpendBuckets returns the previous window's total and the spend in each bucket of the window.
	AgentSpendBuckets(ctx context.Context, q usageQuery) (previous float64, buckets []float64, err error)
	// AgentBudgetSpend returns each listed agent's spend since the week and month starts.
	AgentBudgetSpend(ctx context.Context, caller usageCaller, slugs []string, weekStart, monthStart time.Time) ([]AgentBudgetSpend, error)
}

// AgentInsightsService builds the Agents page rollups from the usage log.
type AgentInsightsService struct {
	store  agentUsageStore
	policy policy.Evaluator
	logger logging.Logger
	now    func() time.Time
}

// NewAgentInsightsService creates the service.
func NewAgentInsightsService(store agentUsageStore, pol policy.Evaluator, logger logging.Logger) *AgentInsightsService {
	return &AgentInsightsService{store: store, policy: pol, logger: logger, now: time.Now}
}

// Insights returns the window's usage, model split, spend trend, previous total
// and budgets. The reads are independent, so they run together.
func (s *AgentInsightsService) Insights(ctx context.Context, caller usageCaller, period usagewindow.Period) (AgentInsights, error) {
	w := usagewindow.For(period, s.now())
	q := usageQuery{Caller: caller, Window: w}

	var (
		rows     []agentUsageRow
		previous float64
		buckets  []float64
		budgets  []AgentBudgetRow
	)
	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() (err error) { rows, err = s.store.AgentUsage(gctx, q); return err })
	g.Go(func() (err error) { previous, buckets, err = s.store.AgentSpendBuckets(gctx, q); return err })
	g.Go(func() error {
		// Budgets are an extra: a failure leaves them out and does not fail the page.
		budgets = s.agentBudgets(gctx, caller)
		return nil
	})
	if err := g.Wait(); err != nil {
		return AgentInsights{}, fmt.Errorf("agent insights: %w", err)
	}

	usage, models := foldUsage(rows)
	out := AgentInsights{
		Period:            string(period),
		Usage:             usage,
		Models:            models,
		PreviousTotalUSD:  previous,
		Trend:             make([]TrendPoint, len(buckets)),
		AllDelegatedUsage: caller.Admin,
		Budgets:           budgets,
	}
	for _, u := range usage {
		out.TotalCostUSD += u.EstimatedCostUSD
	}
	for i, cost := range buckets {
		out.Trend[i] = TrendPoint{Start: w.Start.Add(time.Duration(i) * w.Bucket), CostUSD: cost}
	}
	return out, nil
}

// foldUsage sums the per agent and model rows into usage per agent and spend per
// model, largest first. A model with no spend is left out.
func foldUsage(rows []agentUsageRow) ([]AgentTypeUsage, []ModelSpend) {
	usage := []AgentTypeUsage{}
	byModel := map[string]float64{}
	for _, r := range rows {
		if n := len(usage); n > 0 && usage[n-1].AgentSlug == r.AgentSlug {
			u := &usage[n-1]
			u.PromptTokens += r.PromptTokens
			u.CompletionTokens += r.CompletionTokens
			u.CacheReadTokens += r.CacheReadTokens
			u.CacheCreationTokens += r.CacheCreationTokens
			u.TotalTokens += r.TotalTokens
			u.EstimatedCostUSD += r.EstimatedCostUSD
		} else {
			usage = append(usage, r.AgentTypeUsage)
		}
		byModel[r.ModelID] += r.EstimatedCostUSD
	}
	models := []ModelSpend{}
	for id, cost := range byModel {
		if cost > 0 {
			models = append(models, ModelSpend{ModelID: id, CostUSD: cost})
		}
	}
	sort.Slice(models, func(i, j int) bool {
		return models[i].CostUSD > models[j].CostUSD || (models[i].CostUSD == models[j].CostUSD && models[i].ModelID < models[j].ModelID)
	})
	return usage, models
}

// AgentUsage returns one agent's tokens and spend for the window, under the same
// predicate as Insights. An agent the caller sees no rows of reads as zero.
func (s *AgentInsightsService) AgentUsage(ctx context.Context, caller usageCaller, period usagewindow.Period, slug string) (AgentTypeUsage, error) {
	rows, err := s.store.AgentUsage(ctx, usageQuery{Caller: caller, Window: usagewindow.For(period, s.now()), Slug: slug})
	if err != nil {
		return AgentTypeUsage{}, fmt.Errorf("agent usage: %w", err)
	}
	usage, _ := foldUsage(rows)
	if len(usage) == 0 {
		return AgentTypeUsage{AgentSlug: slug}, nil
	}
	return usage[0], nil
}

// agentBudgets builds a row per agent that has a window limit, from the policy
// bundle enforcement evaluates, with the agent's spend in each window. The page
// drops delegated agents: their limit is shared by everyone who runs them.
func (s *AgentInsightsService) agentBudgets(ctx context.Context, caller usageCaller) []AgentBudgetRow {
	principals, _, err := s.policy.ListBudgets(ctx, caller.TenantID)
	if err != nil {
		s.logger.Warn(ctx, "agent budgets unavailable", logging.Field{Key: "error", Value: err.Error()})
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
	spend, err := s.store.AgentBudgetSpend(ctx, caller, slugs, periodStart(cadenceWeekly, now), periodStart(cadenceMonthly, now))
	if err != nil {
		s.logger.Warn(ctx, "agent budget spend unavailable", logging.Field{Key: "error", Value: err.Error()})
		return nil
	}
	spendBySlug := make(map[string]AgentBudgetSpend, len(spend))
	for _, sp := range spend {
		spendBySlug[sp.AgentSlug] = sp
	}
	for i := range rows {
		sp := spendBySlug[rows[i].AgentSlug]
		for j := range rows[i].Windows {
			if rows[i].Windows[j].Cadence == cadenceWeekly {
				rows[i].Windows[j].SpendUSD = sp.Week
			} else {
				rows[i].Windows[j].SpendUSD = sp.Month
			}
		}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].AgentSlug < rows[j].AgentSlug })
	return rows
}

// budgetRowsFromBundle turns the bundle's per-principal budgets into rows for
// agent principals, keeping the windows that have a limit. A zero limit is no
// limit, as in the bundle.
func budgetRowsFromBundle(principals map[string]policy.PrincipalBudget, now time.Time) []AgentBudgetRow {
	rows := make([]AgentBudgetRow, 0)
	for pid, pb := range principals {
		slug := agentSlugFromPrincipal(string(token.PrincipalTypeAgent), pid)
		if slug == "" {
			continue
		}
		row := AgentBudgetRow{AgentSlug: slug, PerRunLimitUSD: pb.EffectivePerRunLimitUSD}
		for _, cadence := range []string{cadenceMonthly, cadenceWeekly} {
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
