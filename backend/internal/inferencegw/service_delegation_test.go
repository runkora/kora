package inferencegw

import (
	"context"
	"testing"

	"github.com/runkora/kora/backend/internal/platform/logging"
	"github.com/runkora/kora/backend/internal/platform/policy"
	"github.com/runkora/kora/backend/internal/platform/token"

	"io"
)

// ─── Delegated usage attribution and budget input assembly ───────────────────
//
// docs/adr/2026-07-23-delegated-agent-identity.md, Decision 5: usage records
// carry the delegator from the VERIFIED delegation context, the delegator's
// window counters are read/written alongside the agent's, and CheckBudget
// receives the delegator so budget.rego can apply their ceilings.

// delegatedAgentCtx returns a context carrying AGENT claims plus verified
// delegation claims — the shape DelegationMiddleware produces after
// signature/blocklist checks.
func delegatedAgentCtx(delegatorID string) context.Context {
	claims := &token.PlatformClaims{
		Orbit: token.OrbitClaims{
			TenantID:      "tenant-1",
			PrincipalType: token.PrincipalTypeAgent,
		},
	}
	claims.Subject = "spiffe://orbit.test/tenant/tenant-1/agent/reviewer"
	ctx := token.WithClaims(context.Background(), claims)
	return policy.WithDelegation(ctx, &policy.DelegationClaims{
		Sub:                    delegatorID,
		ActSub:                 claims.Subject,
		DelegatorPrincipalType: "HUMAN",
		TenantID:               "tenant-1",
	})
}

// ── writeUsage stamping ───────────────────────────────────────────────────────

func TestWriteUsage_StampsDelegatorFromVerifiedContext(t *testing.T) {
	t.Parallel()
	repo := newMockUsageRepo()
	svc := newTestUsageService(t, repo)

	if err := svc.writeUsage(delegatedAgentCtx("user-1"), InferenceUsageRecord{ID: "rec-1"}); err != nil {
		t.Fatalf("writeUsage: %v", err)
	}

	if len(repo.records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(repo.records))
	}
	rec := repo.records[0]
	if rec.DelegatorID == nil || *rec.DelegatorID != "user-1" {
		t.Errorf("DelegatorID = %v, want user-1", rec.DelegatorID)
	}
	if rec.DelegatorPrincipalType == nil || *rec.DelegatorPrincipalType != "HUMAN" {
		t.Errorf("DelegatorPrincipalType = %v, want HUMAN", rec.DelegatorPrincipalType)
	}
}

func TestWriteUsage_NoDelegation_NoDelegatorStamp(t *testing.T) {
	t.Parallel()
	repo := newMockUsageRepo()
	svc := newTestUsageService(t, repo)

	ctx := ctxFromClaims(makeClaims("tenant-1", "user-1", token.PrincipalTypeHuman, ""))
	if err := svc.writeUsage(ctx, InferenceUsageRecord{ID: "rec-1"}); err != nil {
		t.Fatalf("writeUsage: %v", err)
	}

	rec := repo.records[0]
	if rec.DelegatorID != nil || rec.DelegatorPrincipalType != nil {
		t.Errorf("non-delegated usage must carry no delegator, got %v/%v", rec.DelegatorID, rec.DelegatorPrincipalType)
	}
}

// ── budget input assembly ─────────────────────────────────────────────────────

func TestCheckBudget_DelegationThreadedIntoBudgetInput(t *testing.T) {
	t.Parallel()
	eval := &stubEvaluator{
		budgetCategoriesByPrincipal: map[string][]policy.BudgetCategory{
			// The delegator carries a weekly window budget.
			"user-1": {{Source: "grant", ID: "g-user", ResetCadence: "weekly", LimitUSD: 25}},
		},
	}
	counter := &mockSpendCounter{spends: policy.BudgetSpends{
		Principal: map[string]float64{},
		Delegator: map[string]float64{"weekly": 3},
	}}
	svc := newBudgetService(eval, counter)

	if _, err := svc.checkBudget(delegatedAgentCtx("user-1"), principal{
		TenantID:      "tenant-1",
		PrincipalID:   "spiffe://orbit.test/tenant/tenant-1/agent/reviewer",
		PrincipalType: "AGENT",
	}); err != nil {
		t.Fatalf("checkBudget: %v", err)
	}

	// The counter was asked for the delegator's window counters…
	if counter.lastScopes.DelegatorID != "user-1" {
		t.Errorf("SpendScopes.DelegatorID = %q, want user-1", counter.lastScopes.DelegatorID)
	}
	if len(counter.lastScopes.DelegatorWindows) != 1 || counter.lastScopes.DelegatorWindows[0] != "weekly" {
		t.Errorf("SpendScopes.DelegatorWindows = %v, want [weekly]", counter.lastScopes.DelegatorWindows)
	}
	// …and CheckBudget received the verified delegator with those spends.
	if eval.lastBudgetReq == nil || eval.lastBudgetReq.DelegatorID != "user-1" {
		t.Fatalf("CheckBudget request = %+v, want DelegatorID user-1", eval.lastBudgetReq)
	}
	if eval.lastBudgetReq.Spends.Delegator["weekly"] != 3 {
		t.Errorf("delegator spends = %v, want weekly 3", eval.lastBudgetReq.Spends.Delegator)
	}
}

func TestCheckBudget_NoDelegation_NoDelegatorInput(t *testing.T) {
	t.Parallel()
	eval := &stubEvaluator{}
	counter := &mockSpendCounter{}
	svc := newBudgetService(eval, counter)

	if _, err := svc.checkBudget(ctxWithClaims(t), principal{
		TenantID: "tenant-1", PrincipalID: "user-1", PrincipalType: "HUMAN",
	}); err != nil {
		t.Fatalf("checkBudget: %v", err)
	}
	if counter.lastScopes.DelegatorID != "" || len(counter.lastScopes.DelegatorWindows) != 0 {
		t.Errorf("non-delegated scopes must carry no delegator, got %+v", counter.lastScopes)
	}
	if eval.lastBudgetReq == nil || eval.lastBudgetReq.DelegatorID != "" {
		t.Errorf("CheckBudget must receive no delegator, got %+v", eval.lastBudgetReq)
	}
}

func TestIncrementSpend_DelegatorCountersThreaded(t *testing.T) {
	t.Parallel()
	eval := &stubEvaluator{
		budgetCategoriesByPrincipal: map[string][]policy.BudgetCategory{
			"user-1": {{Source: "grant", ID: "g-user", ResetCadence: "monthly", LimitUSD: 50}},
		},
	}
	counter := &mockSpendCounter{}
	svc := newBudgetService(eval, counter)

	svc.incrementSpend(delegatedAgentCtx("user-1"), "tenant-1", "spiffe://orbit.test/tenant/tenant-1/agent/reviewer", 1.25)

	if counter.lastIncrScopes.DelegatorID != "user-1" {
		t.Errorf("IncrementSpends DelegatorID = %q, want user-1", counter.lastIncrScopes.DelegatorID)
	}
	if counter.incremented != 1.25 {
		t.Errorf("incremented = %v, want 1.25", counter.incremented)
	}
}

// ── personal views include delegated rows ─────────────────────────────────────

func TestAnalyticsMe_IncludesDelegatedRows(t *testing.T) {
	t.Parallel()
	repo := newMockUsageRepo()
	svc := &Service{usage: repo, logger: logging.NewLoggerWithOutput(io.Discard)}

	ctx := ctxFromClaims(makeClaims("tenant-1", "user-1", token.PrincipalTypeHuman, ""))
	if _, err := svc.AnalyticsMe(ctx, AnalyticsFilter{Days: 30, Granularity: "daily"}); err != nil {
		t.Fatalf("AnalyticsMe: %v", err)
	}

	if !repo.lastAnalytics.IncludeDelegated {
		t.Error("AnalyticsMe must set IncludeDelegated for the personal view")
	}
	if repo.lastAnalytics.PrincipalID == nil || *repo.lastAnalytics.PrincipalID != "user-1" {
		t.Errorf("PrincipalID = %v, want user-1", repo.lastAnalytics.PrincipalID)
	}
}

func TestAnalytics_TenantWide_DoesNotWidenPredicate(t *testing.T) {
	t.Parallel()
	repo := newMockUsageRepo()
	svc := &Service{usage: repo, logger: logging.NewLoggerWithOutput(io.Discard)}

	ctx := ctxFromClaims(makeClaims("tenant-1", "admin-1", token.PrincipalTypeHuman, ""))
	if _, err := svc.Analytics(ctx, AnalyticsFilter{Days: 30, Granularity: "daily"}); err != nil {
		t.Fatalf("Analytics: %v", err)
	}
	if repo.lastAnalytics.IncludeDelegated {
		t.Error("tenant-wide analytics must not set the personal IncludeDelegated widening")
	}
}

func TestListUsage_IncludesDelegatedRows(t *testing.T) {
	t.Parallel()
	repo := newMockUsageRepo()
	svc := newTestUsageService(t, repo)

	ctx := ctxFromClaims(makeClaims("tenant-1", "user-1", token.PrincipalTypeHuman, ""))
	if _, _, err := svc.ListUsage(ctx, InferenceUsageFilter{}); err != nil {
		t.Fatalf("ListUsage: %v", err)
	}
	if !repo.lastFilter.IncludeDelegated {
		t.Error("ListUsage must set IncludeDelegated for the personal view")
	}
}
