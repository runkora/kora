package inferencegw

import (
	"context"
	"fmt"
	"time"

	"github.com/runkora/kora/backend/internal/platform/policy"
	"github.com/runkora/kora/backend/internal/platform/token"
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
	result, err := s.evaluateBudget(ctx, p, rp.DelegatorID, runID)
	if err != nil {
		return RunBudget{}, err
	}
	now := time.Now().UTC()
	for _, c := range result.Ceilings {
		if !c.Reached() {
			continue
		}
		b := RunBudget{Scope: c.Scope, Owner: c.ID, Window: c.Window, LimitUSD: c.LimitUSD}
		if b.OwnerName, err = s.ownerName(ctx, tenantID, rp, c); err != nil {
			return RunBudget{}, err
		}
		if c.Window != cadenceRun {
			resets := periodEnd(c.Window, now)
			b.ResetsAt = &resets
		}
		return b, nil
	}
	return RunBudget{}, ErrNoReachedBudget
}

// scopeTenant is the scope of the tenant cap. Its owner reads as "tenant".
const scopeTenant = "tenant"

// ownerName is the name a person reads for the ceiling's owner. A person is
// named from their profile. An agent principal is left empty: kora-agents
// knows the agent's name. A per-run ceiling belongs to no one.
func (s *Service) ownerName(ctx context.Context, tenantID string, rp RunPrincipal, c policy.BudgetCeiling) (string, error) {
	switch c.Scope {
	case "role_pool":
		return c.ID, nil
	case scopeTenant:
		return scopeTenant, nil
	case "delegator":
		return s.usage.GetPersonName(ctx, tenantID, c.ID)
	case "principal":
		if rp.PrincipalType != string(token.PrincipalTypeAgent) {
			return s.usage.GetPersonName(ctx, tenantID, c.ID)
		}
	}
	return "", nil
}
