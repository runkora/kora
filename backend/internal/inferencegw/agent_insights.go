package inferencegw

import (
	"context"
	"fmt"
	"sort"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/clear-route/orbit/internal/platform/logging"
	"github.com/clear-route/orbit/internal/platform/policy"
	"github.com/clear-route/orbit/internal/platform/token"
	"github.com/clear-route/orbit/internal/platform/usagewindow"
)

// The Agents page rollups cover agent traffic for the whole tenant and carry no
// per-person data. The route scope agents:usage:read decides who may read them.

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

// AgentInsights is what the Agents page reports for one window.
type AgentInsights struct {
	Period           string                 `json:"period"`
	Usage            []AgentTypeUsage       `json:"usage"`
	TotalCostUSD     float64                `json:"total_cost_usd"`
	PreviousTotalUSD float64                `json:"previous_total_usd"`
	Models           []AnalyticsCostByModel `json:"models"`
	Trend            []TrendPoint           `json:"trend"`
	Budgets          []AgentBudgetRow       `json:"budgets,omitempty"`
}

// AgentModelUsage is one agent's usage on one model.
type AgentModelUsage struct {
	AgentTypeUsage
	ModelID string
}

// AgentBudgetSpend is an agent's spend since the start of the current week and month.
type AgentBudgetSpend struct {
	AgentSlug string
	Week      float64
	Month     float64
}

// Insights returns the window's usage, model split, spend trend, previous total
// and budgets. The reads are independent, so they run together.
func (s *Service) Insights(ctx context.Context, tenantID string, period usagewindow.Period, now time.Time) (AgentInsights, error) {
	w := usagewindow.For(period, now)

	var (
		rows     []AgentModelUsage
		previous float64
		buckets  []float64
		budgets  []AgentBudgetRow
	)
	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() (err error) { rows, err = s.usage.AgentUsage(gctx, tenantID, w, ""); return err })
	g.Go(func() (err error) { previous, buckets, err = s.usage.AgentSpendBuckets(gctx, tenantID, w); return err })
	g.Go(func() error {
		// Budgets are an extra: a failure leaves them out and does not fail the page.
		budgets = s.agentBudgets(gctx, tenantID, now)
		return nil
	})
	if err := g.Wait(); err != nil {
		return AgentInsights{}, fmt.Errorf("agent insights: %w", err)
	}

	usage, models := foldUsage(rows)
	out := AgentInsights{
		Period:           string(period),
		Usage:            usage,
		Models:           models,
		PreviousTotalUSD: previous,
		Trend:            make([]TrendPoint, len(buckets)),
		Budgets:          budgets,
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
func foldUsage(rows []AgentModelUsage) ([]AgentTypeUsage, []AnalyticsCostByModel) {
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
	models := []AnalyticsCostByModel{}
	for id, cost := range byModel {
		if cost > 0 {
			models = append(models, AnalyticsCostByModel{ModelID: id, EstimatedCostUSD: cost})
		}
	}
	sort.Slice(models, func(i, j int) bool {
		a, b := models[i], models[j]
		return a.EstimatedCostUSD > b.EstimatedCostUSD || (a.EstimatedCostUSD == b.EstimatedCostUSD && a.ModelID < b.ModelID)
	})
	return usage, models
}

// AgentUsage returns one agent's tokens and spend for the window. An agent with
// no rows reads as zero.
func (s *Service) AgentUsage(ctx context.Context, tenantID string, period usagewindow.Period, slug string, now time.Time) (AgentTypeUsage, error) {
	rows, err := s.usage.AgentUsage(ctx, tenantID, usagewindow.For(period, now), slug)
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
// bundle enforcement evaluates, with the agent's spend in each window. A
// delegated agent's limit is shared by everyone who runs it, and its row is
// included.
func (s *Service) agentBudgets(ctx context.Context, tenantID string, now time.Time) []AgentBudgetRow {
	principals, _, err := s.policy.ListBudgets(ctx, tenantID)
	if err != nil {
		s.logger.Warn(ctx, "agent budgets unavailable", logging.Field{Key: "error", Value: err.Error()})
		return nil
	}
	rows := budgetRowsFromBundle(principals, now)
	if len(rows) == 0 {
		return nil
	}

	slugs := make([]string, len(rows))
	for i, r := range rows {
		slugs[i] = r.AgentSlug
	}
	spend, err := s.usage.AgentBudgetSpend(ctx, tenantID, slugs, periodStart(cadenceWeekly, now), periodStart(cadenceMonthly, now))
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
		// The bundle keys budgets by principal ID alone, so the type is the agent one this page lists.
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
