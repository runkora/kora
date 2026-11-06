package inferencegw

import (
	"context"
	"fmt"
	"time"

	"github.com/runkora/kora/backend/internal/platform/events"
	"github.com/runkora/kora/backend/internal/platform/logging"
	"github.com/runkora/kora/backend/internal/platform/policy"
	"github.com/runkora/kora/backend/internal/platform/storage"
)

// budgetLevels are the percentages of a budget whose crossing records an event.
var budgetLevels = []int{50, 80, 100}

// budgetEntity is the budget an event carries.
type budgetEntity struct {
	Scope       string    `json:"scope"`
	ID          string    `json:"id"`
	Window      string    `json:"window"`
	PeriodStart time.Time `json:"period_start"`
	LimitUSD    float64   `json:"limit_usd"`
	SpendUSD    float64   `json:"spend_usd"`
	Level       int       `json:"level,omitempty"`
}

// WithEvents records budget threshold and exceeded events through pool.
func (s *Service) WithEvents(pool *storage.Pool) *Service {
	s.events = pool
	return s
}

// eventBudget reports whether c is a tenant or grant budget. Grant budgets
// apply to their principal. Role pool, run and delegator ceilings record no
// budget event.
func eventBudget(c policy.BudgetCeiling) bool {
	return (c.Scope == "tenant" || c.Scope == "principal") && c.LimitUSD > 0
}

// crossEpsilon absorbs float rounding in before, which is after minus the
// amount. A level it counts twice is deduplicated by PublishOnce.
const crossEpsilon = 1e-9

// crossedLevels lists the levels that spend passed going from before to after.
func crossedLevels(before, after, limit float64) []int {
	var out []int
	for _, level := range budgetLevels {
		mark := limit * float64(level) / 100
		if before < mark+crossEpsilon && after >= mark {
			out = append(out, level)
		}
	}
	return out
}

// recordThresholds records each level of a tenant or grant budget that this
// increment of amount crossed. after holds the counters after the increment.
// Each level records once per budget period.
func (s *Service) recordThresholds(ctx context.Context, p principal, roles []string, scopes SpendScopes, after policy.BudgetSpends, amount float64) {
	if s.events == nil || amount <= 0 || (scopes.TenantWindow == "" && len(scopes.PrincipalWindows) == 0) {
		return
	}
	result, err := s.policy.CheckBudget(ctx, p.TenantID, policy.BudgetCheckRequest{
		PrincipalID: p.PrincipalID, PrincipalType: p.PrincipalType, Roles: roles,
		RunID: scopes.RunID, DelegatorID: scopes.DelegatorID, Spends: after,
	})
	if err != nil {
		return
	}
	now := time.Now().UTC()
	for _, c := range result.Ceilings {
		if !eventBudget(c) {
			continue
		}
		for _, level := range crossedLevels(c.SpendUSD-amount, c.SpendUSD, c.LimitUSD) {
			s.publishBudget(ctx, events.BudgetThresholdCrossed, p.TenantID, c, now, level)
		}
	}
}

// recordExceeded records each exhausted tenant or grant budget that refused a
// request, once per budget period.
func (s *Service) recordExceeded(ctx context.Context, tenantID string, ceilings []policy.BudgetCeiling) {
	if s.events == nil {
		return
	}
	now := time.Now().UTC()
	for _, c := range ceilings {
		if eventBudget(c) && c.Reached() {
			s.publishBudget(ctx, events.BudgetExceeded, tenantID, c, now, 0)
		}
	}
}

// publishBudget records a budget event keyed by the budget, its period start
// and the level, which a repeat in the same period does not record again.
func (s *Service) publishBudget(ctx context.Context, typ events.Type, tenantID string, c policy.BudgetCeiling, now time.Time, level int) {
	start := periodStart(c.Window, now)
	// A person's principal id is the same in every tenant they belong to.
	key := fmt.Sprintf("%s/%s/%s/%s/%s", tenantID, c.Scope, c.ID, c.Window, start.Format(time.DateOnly))
	if level > 0 {
		key += fmt.Sprintf("/%d", level)
	}
	err := events.PublishOnce(ctx, s.events, key, events.Event{
		Type: typ, Tenant: tenantID, Subject: "budgets/" + c.Scope + "/" + c.ID,
		Entity: budgetEntity{
			Scope: c.Scope, ID: c.ID, Window: c.Window, PeriodStart: start,
			LimitUSD: c.LimitUSD, SpendUSD: c.SpendUSD, Level: level,
		},
	})
	if err != nil {
		s.logger.Warn(ctx, "budget.event_failed",
			logging.Field{Key: "type", Value: string(typ)},
			logging.Field{Key: "error", Value: err.Error()},
		)
	}
}
