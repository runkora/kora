package inferencegw

import (
	"context"
	"fmt"
	"time"

	"github.com/runkora/kora/backend/internal/platform/policy"
)

// ReachedRunBudget returns the spending ceiling that stops a run: the first
// ceiling its principal has reached, evaluated the way checkBudget evaluates a
// request of that run. The principal and delegator are read from the run's
// usage rows, which record who the gateway charged.
func (s *Service) ReachedRunBudget(ctx context.Context, tenantID, runID string) (RunBudget, error) {
	if s.budgetCounter == nil {
		return RunBudget{}, ErrNoReachedBudget
	}
	rp, err := s.usage.GetRunPrincipal(ctx, tenantID, runID)
	if err != nil {
		return RunBudget{}, fmt.Errorf("find the run's principal: %w", err)
	}
	p := principal{TenantID: tenantID, PrincipalID: rp.PrincipalID, PrincipalType: rp.PrincipalType}
	// The usage row records the delegator of the verified delegation the run's requests carried.
	scopes, _, roles := s.budgetScopesFor(ctx, p, rp.DelegatorID)
	// budgetScopesFor reads the run from an agent's request; this caller names it.
	scopes.RunID = runID

	spends, err := s.budgetCounter.GetSpends(ctx, scopes)
	if err != nil {
		return RunBudget{}, fmt.Errorf("read spend counters: %w", err)
	}
	result, err := s.policy.CheckBudget(ctx, tenantID, policy.BudgetCheckRequest{
		PrincipalID:   p.PrincipalID,
		PrincipalType: p.PrincipalType,
		Roles:         roles,
		RunID:         runID,
		DelegatorID:   rp.DelegatorID,
		Spends:        spends,
	})
	if err != nil {
		return RunBudget{}, fmt.Errorf("evaluate budget: %w", err)
	}
	now := time.Now().UTC()
	for _, c := range result.Ceilings {
		if !c.Reached() {
			continue
		}
		b := RunBudget{Scope: c.Scope, Owner: c.ID, Window: c.Window, LimitUSD: c.LimitUSD}
		if c.Window != "run" {
			resets := periodEnd(c.Window, now)
			b.ResetsAt = &resets
		}
		return b, nil
	}
	return RunBudget{}, ErrNoReachedBudget
}
