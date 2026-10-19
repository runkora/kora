package inferencegw

import (
	"context"
	"errors"
	"testing"

	"github.com/runkora/kora/backend/internal/platform/token"
)

// ── AnalyticsPrincipalByID ───────────────────────────────────────────────────
//
// Covers the admin drill-down endpoint added alongside
// docs/adr/2026-04-27-inference-analytics.md (amended 2026-07-20): it must
// return the same shape as AnalyticsMe, but for a caller-supplied principal
// rather than the authenticated identity, and must always source TenantID
// from the verified JWT — never from the caller-supplied principal.

func TestAnalyticsPrincipalByID_NoClaims_ReturnsErrTokenParseFailed(t *testing.T) {
	t.Parallel()
	svc := newTestUsageService(t, newMockUsageRepo())
	_, err := svc.AnalyticsPrincipalByID(context.Background(), "target-principal", AnalyticsFilter{Days: 30, Granularity: "daily"})
	if !errors.Is(err, ErrTokenParseFailed) {
		t.Fatalf("expected ErrTokenParseFailed, got %v", err)
	}
}

func TestAnalyticsPrincipalByID_TargetsRequestedPrincipal_NotCaller(t *testing.T) {
	t.Parallel()
	repo := newMockUsageRepo()
	svc := newTestUsageService(t, repo)

	// The caller is "admin-1"; the drill-down target is a different principal.
	ctx := ctxFromClaims(makeClaims("tenant-1", "admin-1", token.PrincipalTypeHuman, "session-uuid"))

	_, err := svc.AnalyticsPrincipalByID(ctx, "target-principal", AnalyticsFilter{Days: 30, Granularity: "daily"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := repo.lastAnalytics
	if got.TenantID != "tenant-1" {
		t.Errorf("TenantID: got %q, want %q (from JWT, not caller-supplied)", got.TenantID, "tenant-1")
	}
	if got.PrincipalID == nil || *got.PrincipalID != "target-principal" {
		t.Errorf("PrincipalID: got %v, want %q (the drill-down target, not the caller's own identity)", got.PrincipalID, "target-principal")
	}
}

func TestAnalyticsPrincipalByID_RepositoryError_IsWrapped(t *testing.T) {
	t.Parallel()
	repo := newMockUsageRepo()
	repo.analyticsErr = errors.New("boom")
	svc := newTestUsageService(t, repo)
	ctx := ctxFromClaims(makeClaims("tenant-1", "admin-1", token.PrincipalTypeHuman, "session-uuid"))

	_, err := svc.AnalyticsPrincipalByID(ctx, "target-principal", AnalyticsFilter{Days: 30, Granularity: "daily"})
	if err == nil {
		t.Fatal("expected error to propagate from repository")
	}
}
