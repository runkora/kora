package inferencegw

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/clear-route/orbit/internal/platform/logging"
	"github.com/clear-route/orbit/internal/platform/token"
)

// newBudgetService builds a Service with quota components wired.
func newBudgetService(t *testing.T, cache QuotaResolver, counter SpendCounter) *Service {
	t.Helper()
	return &Service{
		usage:        newMockUsageRepo(),
		quotaCache:   cache,
		quotaCounter: counter,
		logger:       logging.NewLoggerWithOutput(io.Discard),
	}
}

func makeHumanPrincipal() principal {
	return principal{
		TenantID:      "tenant1",
		PrincipalID:   "user1",
		PrincipalType: string(token.PrincipalTypeHuman),
	}
}

// ── checkBudget ───────────────────────────────────────────────────────────────

func TestCheckBudget_NilCache_PassesThrough(t *testing.T) {
	t.Parallel()
	svc := &Service{logger: logging.NewLoggerWithOutput(io.Discard)}
	ctx, err := svc.checkBudget(context.Background(), makeHumanPrincipal())
	if err != nil {
		t.Errorf("want nil err, got %v", err)
	}
	if ctx == nil {
		t.Error("want non-nil context")
	}
}

func TestCheckBudget_NilCounter_PassesThrough(t *testing.T) {
	t.Parallel()
	svc := &Service{
		quotaCache: &mockQuotaResolver{budget: 100, hasLimit: true},
		logger:     logging.NewLoggerWithOutput(io.Discard),
	}
	ctx, err := svc.checkBudget(context.Background(), makeHumanPrincipal())
	if err != nil {
		t.Errorf("want nil err, got %v", err)
	}
	if ctx == nil {
		t.Error("want non-nil context")
	}
}

func TestCheckBudget_NoLimit_Passes(t *testing.T) {
	t.Parallel()
	svc := newBudgetService(t, &mockQuotaResolver{hasLimit: false}, &mockSpendCounter{})
	_, err := svc.checkBudget(context.Background(), makeHumanPrincipal())
	if err != nil {
		t.Errorf("want nil err for no-limit principal, got %v", err)
	}
}

func TestCheckBudget_SpendBelowBudget_Passes(t *testing.T) {
	t.Parallel()
	svc := newBudgetService(t,
		&mockQuotaResolver{budget: 100, hasLimit: true},
		&mockSpendCounter{spend: 50},
	)
	_, err := svc.checkBudget(context.Background(), makeHumanPrincipal())
	if err != nil {
		t.Errorf("want nil err when spend < budget, got %v", err)
	}
}

func TestCheckBudget_SpendEqualsBudget_Blocked(t *testing.T) {
	t.Parallel()
	svc := newBudgetService(t,
		&mockQuotaResolver{budget: 50, hasLimit: true},
		&mockSpendCounter{spend: 50},
	)
	_, err := svc.checkBudget(context.Background(), makeHumanPrincipal())
	if !errors.Is(err, ErrBudgetExceeded) {
		t.Errorf("want ErrBudgetExceeded when spend == budget, got %v", err)
	}
}

func TestCheckBudget_SpendExceedsBudget_Blocked(t *testing.T) {
	t.Parallel()
	svc := newBudgetService(t,
		&mockQuotaResolver{budget: 10, hasLimit: true},
		&mockSpendCounter{spend: 99},
	)
	_, err := svc.checkBudget(context.Background(), makeHumanPrincipal())
	if !errors.Is(err, ErrBudgetExceeded) {
		t.Errorf("want ErrBudgetExceeded when spend > budget, got %v", err)
	}
}

func TestCheckBudget_CacheError_FailsOpen(t *testing.T) {
	t.Parallel()
	svc := newBudgetService(t,
		&mockQuotaResolver{err: errTestInternal},
		&mockSpendCounter{},
	)
	_, err := svc.checkBudget(context.Background(), makeHumanPrincipal())
	if err != nil {
		t.Errorf("want nil (fail-open) on cache error, got %v", err)
	}
}

func TestCheckBudget_CounterError_FailsOpen(t *testing.T) {
	t.Parallel()
	svc := newBudgetService(t,
		&mockQuotaResolver{budget: 100, hasLimit: true},
		&mockSpendCounter{getSpendErr: errTestInternal},
	)
	_, err := svc.checkBudget(context.Background(), makeHumanPrincipal())
	if err != nil {
		t.Errorf("want nil (fail-open) on counter error, got %v", err)
	}
}

// ── GetPersonalQuota ──────────────────────────────────────────────────────────

func TestGetPersonalQuota_NilComponents_ReturnsZero(t *testing.T) {
	t.Parallel()
	svc := &Service{logger: logging.NewLoggerWithOutput(io.Discard)}
	status, err := svc.GetPersonalQuota(context.Background(), "t1", "u1")
	if err != nil {
		t.Fatalf("want nil err, got %v", err)
	}
	if status.HasLimit || status.SpendLimitReached || status.MonthlyBudgetUSD != 0 {
		t.Errorf("want zero status, got %+v", status)
	}
}

func TestGetPersonalQuota_NoLimit(t *testing.T) {
	t.Parallel()
	svc := newBudgetService(t, &mockQuotaResolver{hasLimit: false}, &mockSpendCounter{})
	status, err := svc.GetPersonalQuota(context.Background(), "t1", "u1")
	if err != nil {
		t.Fatalf("want nil err, got %v", err)
	}
	if status.HasLimit {
		t.Error("HasLimit = true, want false")
	}
	if status.CurrentSpendUSD != 0 {
		t.Errorf("CurrentSpendUSD = %v, want 0 (counter not consulted when no limit)", status.CurrentSpendUSD)
	}
}

func TestGetPersonalQuota_WithLimit(t *testing.T) {
	t.Parallel()
	svc := newBudgetService(t,
		&mockQuotaResolver{budget: 200, hasLimit: true},
		&mockSpendCounter{spend: 75},
	)
	status, err := svc.GetPersonalQuota(context.Background(), "t1", "u1")
	if err != nil {
		t.Fatalf("want nil err, got %v", err)
	}
	if !status.HasLimit {
		t.Error("HasLimit = false, want true")
	}
	if status.MonthlyBudgetUSD != 200 {
		t.Errorf("MonthlyBudgetUSD = %v, want 200", status.MonthlyBudgetUSD)
	}
	if status.CurrentSpendUSD != 75 {
		t.Errorf("CurrentSpendUSD = %v, want 75", status.CurrentSpendUSD)
	}
	if status.SpendLimitReached {
		t.Error("SpendLimitReached = true, want false (75 < 200)")
	}
}

func TestGetPersonalQuota_LimitReached(t *testing.T) {
	t.Parallel()
	svc := newBudgetService(t,
		&mockQuotaResolver{budget: 100, hasLimit: true},
		&mockSpendCounter{spend: 100},
	)
	status, err := svc.GetPersonalQuota(context.Background(), "t1", "u1")
	if err != nil {
		t.Fatalf("want nil err, got %v", err)
	}
	if !status.SpendLimitReached {
		t.Error("SpendLimitReached = false, want true (spend == budget)")
	}
}

func TestGetPersonalQuota_CacheError(t *testing.T) {
	t.Parallel()
	svc := newBudgetService(t, &mockQuotaResolver{err: errTestInternal}, &mockSpendCounter{})
	_, err := svc.GetPersonalQuota(context.Background(), "t1", "u1")
	if err == nil {
		t.Error("want error on cache failure, got nil")
	}
}

func TestGetPersonalQuota_CounterError(t *testing.T) {
	t.Parallel()
	svc := newBudgetService(t,
		&mockQuotaResolver{budget: 100, hasLimit: true},
		&mockSpendCounter{getSpendErr: errTestInternal},
	)
	_, err := svc.GetPersonalQuota(context.Background(), "t1", "u1")
	if err == nil {
		t.Error("want error on counter failure, got nil")
	}
}
