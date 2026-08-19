package inferencegw

import (
	"context"
	"errors"
	"io"
	"testing"

	jwt "github.com/golang-jwt/jwt/v5"

	"github.com/clear-route/orbit/internal/platform/logging"
	"github.com/clear-route/orbit/internal/platform/token"
)

// ── helpers ───────────────────────────────────────────────────────────────────

// makeClaims builds a minimal *token.PlatformClaims for injection via
// token.WithClaims. Only the fields consumed by principalFromClaims are set.
func makeClaims(tenantID, principalID string, pt token.PrincipalType, sessionID string) *token.PlatformClaims {
	c := &token.PlatformClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject: principalID,
		},
		Orbit: token.OrbitClaims{
			TenantID:      tenantID,
			PrincipalType: pt,
			SessionID:     sessionID,
		},
	}
	if pt == token.PrincipalTypePAT {
		c.Orbit.PatID = sessionID
	}
	return c
}

// ctxFromClaims returns a context carrying the given platform claims.
func ctxFromClaims(claims *token.PlatformClaims) context.Context {
	return token.WithClaims(context.Background(), claims)
}

// newTestService builds a minimal *Service for usage-path tests.
// policy, router, and pricing are nil — they are not reached by ListUsage.
func newTestUsageService(t *testing.T, repo *mockInferenceUsageRepository) *Service {
	t.Helper()
	return &Service{
		usage:  repo,
		logger: logging.NewLoggerWithOutput(io.Discard),
	}
}

// ── principalFromClaims ───────────────────────────────────────────────────────

func TestPrincipalFromClaims_NoClaims_ReturnsError(t *testing.T) {
	t.Parallel()
	_, err := principalFromClaims(context.Background())
	if err == nil {
		t.Fatal("expected error when no claims in context, got nil")
	}
}

func TestPrincipalFromClaims_HumanPrincipal_TokenIDEmpty(t *testing.T) {
	t.Parallel()
	ctx := ctxFromClaims(makeClaims("tenant-1", "user-1", token.PrincipalTypeHuman, "session-uuid"))
	p, err := principalFromClaims(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.TenantID != "tenant-1" {
		t.Errorf("TenantID: got %q, want %q", p.TenantID, "tenant-1")
	}
	if p.PrincipalID != "user-1" {
		t.Errorf("PrincipalID: got %q, want %q", p.PrincipalID, "user-1")
	}
	// PatID must NOT be extracted as TokenID for non-PAT principals.
	if p.TokenID != "" {
		t.Errorf("TokenID: got %q, want empty string for HUMAN principal", p.TokenID)
	}
}

func TestPrincipalFromClaims_PATPrincipal_TokenIDIsPatID(t *testing.T) {
	t.Parallel()
	const patUUID = "pat-record-uuid-abc123"
	ctx := ctxFromClaims(makeClaims("tenant-1", "user-1", token.PrincipalTypePAT, patUUID))
	p, err := principalFromClaims(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.TokenID != patUUID {
		t.Errorf("TokenID: got %q, want %q", p.TokenID, patUUID)
	}
}

func TestPrincipalFromClaims_AgentPrincipal_TokenIDEmpty(t *testing.T) {
	t.Parallel()
	ctx := ctxFromClaims(makeClaims("tenant-1", "agent-1", token.PrincipalTypeAgent, "some-session"))
	p, err := principalFromClaims(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.TokenID != "" {
		t.Errorf("TokenID: got %q, want empty string for AGENT principal", p.TokenID)
	}
}

// ── ListUsage ─────────────────────────────────────────────────────────────────

func TestListUsage_NoClaims_ReturnsErrTokenParseFailed(t *testing.T) {
	t.Parallel()
	svc := newTestUsageService(t, newMockUsageRepo())
	_, _, err := svc.ListUsage(context.Background(), InferenceUsageFilter{})
	if !errors.Is(err, ErrTokenParseFailed) {
		t.Errorf("error: got %v, want wrapping ErrTokenParseFailed", err)
	}
}

func TestListUsage_EnforcesTenantScoping(t *testing.T) {
	t.Parallel()
	repo := newMockUsageRepo()
	svc := newTestUsageService(t, repo)

	ctx := ctxFromClaims(makeClaims("tenant-X", "user-Y", token.PrincipalTypeHuman, ""))

	// Caller-supplied values must be overwritten by service.
	callerFilter := InferenceUsageFilter{
		TenantID:    "injected-tenant",
		PrincipalID: new("injected-user"),
	}
	_, _, err := svc.ListUsage(ctx, callerFilter)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if repo.lastFilter.TenantID != "tenant-X" {
		t.Errorf("TenantID passed to repo: got %q, want %q", repo.lastFilter.TenantID, "tenant-X")
	}
	if repo.lastFilter.PrincipalID == nil || *repo.lastFilter.PrincipalID != "user-Y" {
		t.Errorf("PrincipalID passed to repo: got %v, want pointer to %q", repo.lastFilter.PrincipalID, "user-Y")
	}
}

func TestListUsage_ReturnsRecordsAndHasNextPage(t *testing.T) {
	t.Parallel()
	repo := newMockUsageRepo()
	repo.records = []InferenceUsageRecord{
		{ID: "rec-1", TenantID: "t1", PrincipalID: "u1"},
		{ID: "rec-2", TenantID: "t1", PrincipalID: "u1"},
	}
	repo.hasNextPage = true // simulates more pages available
	svc := newTestUsageService(t, repo)

	ctx := ctxFromClaims(makeClaims("t1", "u1", token.PrincipalTypeHuman, ""))
	records, hasNextPage, err := svc.ListUsage(ctx, InferenceUsageFilter{Limit: 2})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(records) != 2 {
		t.Errorf("len(records): got %d, want 2", len(records))
	}
	if !hasNextPage {
		t.Error("hasNextPage: got false, want true")
	}
}

func TestListUsage_RepoError_IsWrapped(t *testing.T) {
	t.Parallel()
	repo := newMockUsageRepo()
	repo.listErr = errors.New("db connection lost")
	svc := newTestUsageService(t, repo)

	ctx := ctxFromClaims(makeClaims("t1", "u1", token.PrincipalTypeHuman, ""))
	_, _, err := svc.ListUsage(ctx, InferenceUsageFilter{})
	if err == nil {
		t.Fatal("expected error from repo, got nil")
	}
}

// ── ListUsageAll ──────────────────────────────────────────────────────────────

func TestListUsageAll_NoClaims_ReturnsErrTokenParseFailed(t *testing.T) {
	t.Parallel()
	svc := newTestUsageService(t, newMockUsageRepo())
	_, _, err := svc.ListUsageAll(context.Background(), InferenceUsageFilter{})
	if !errors.Is(err, ErrTokenParseFailed) {
		t.Errorf("error: got %v, want wrapping ErrTokenParseFailed", err)
	}
}

// TestListUsageAll_ForcesTenantFromClaims_WidensToWholeTenant asserts the admin
// read overwrites any caller-supplied TenantID with the verified claim and, when
// no principal filter is given, leaves PrincipalID nil so the query spans the
// whole tenant. It also pins IncludeDelegated off — an admin sees each row as
// recorded, delegated agent rows attributed to the agent, not the delegator.
func TestListUsageAll_ForcesTenantFromClaims_WidensToWholeTenant(t *testing.T) {
	t.Parallel()
	repo := newMockUsageRepo()
	svc := newTestUsageService(t, repo)

	ctx := ctxFromClaims(makeClaims("tenant-X", "admin-Y", token.PrincipalTypeHuman, ""))

	// A caller-supplied TenantID must be discarded in favour of the claim.
	_, _, err := svc.ListUsageAll(ctx, InferenceUsageFilter{TenantID: "injected-tenant"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if repo.lastFilter.TenantID != "tenant-X" {
		t.Errorf("TenantID passed to repo: got %q, want %q (from claims)", repo.lastFilter.TenantID, "tenant-X")
	}
	if repo.lastFilter.PrincipalID != nil {
		t.Errorf("PrincipalID passed to repo: got %v, want nil (whole tenant)", repo.lastFilter.PrincipalID)
	}
	if repo.lastFilter.IncludeDelegated {
		t.Error("IncludeDelegated: got true, want false (admin sees rows as recorded)")
	}
}

// TestListUsageAll_ForeignPrincipalIDOnlyNarrowsWithinTenant is the security
// invariant the ADR calls out: the caller-supplied principal_id can only narrow
// within the tenant. The tenant predicate is always forced from the claim, so a
// principal_id belonging to another tenant reaches the repository paired with
// the caller's own tenant_id — the AND predicate yields no rows rather than a
// cross-tenant leak.
func TestListUsageAll_ForeignPrincipalIDOnlyNarrowsWithinTenant(t *testing.T) {
	t.Parallel()
	repo := newMockUsageRepo()
	svc := newTestUsageService(t, repo)

	ctx := ctxFromClaims(makeClaims("tenant-X", "admin-Y", token.PrincipalTypeHuman, ""))

	foreign := "principal-in-another-tenant"
	_, _, err := svc.ListUsageAll(ctx, InferenceUsageFilter{PrincipalID: &foreign})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The principal_id is preserved as a narrowing filter, but the tenant is
	// still forced from the claim — so the compound predicate is tenant-safe.
	if repo.lastFilter.TenantID != "tenant-X" {
		t.Errorf("TenantID passed to repo: got %q, want %q (forced from claim)", repo.lastFilter.TenantID, "tenant-X")
	}
	if repo.lastFilter.PrincipalID == nil || *repo.lastFilter.PrincipalID != foreign {
		t.Errorf("PrincipalID passed to repo: got %v, want pointer to %q", repo.lastFilter.PrincipalID, foreign)
	}
}

func TestListUsageAll_RepoError_IsWrapped(t *testing.T) {
	t.Parallel()
	repo := newMockUsageRepo()
	repo.listErr = errors.New("db connection lost")
	svc := newTestUsageService(t, repo)

	ctx := ctxFromClaims(makeClaims("t1", "admin-1", token.PrincipalTypeHuman, ""))
	_, _, err := svc.ListUsageAll(ctx, InferenceUsageFilter{})
	if err == nil {
		t.Fatal("expected error from repo, got nil")
	}
}

// ── UsageStats ────────────────────────────────────────────────────────────────

func TestUsageStats_NoClaims_ReturnsErrTokenParseFailed(t *testing.T) {
	t.Parallel()
	svc := newTestUsageService(t, newMockUsageRepo())
	_, err := svc.UsageStats(context.Background())
	if !errors.Is(err, ErrTokenParseFailed) {
		t.Errorf("error: got %v, want wrapping ErrTokenParseFailed", err)
	}
}

func TestUsageStats_EnforcesTenantAndPrincipalScoping(t *testing.T) {
	t.Parallel()
	repo := newMockUsageRepo()
	repo.statsResult = []TokenUsageStat{
		{TokenID: "pat-1", RequestCount: 5, LastUsed: "2026-04-18T10:00:00Z"},
	}
	svc := newTestUsageService(t, repo)

	ctx := ctxFromClaims(makeClaims("tenant-X", "user-Y", token.PrincipalTypeHuman, ""))
	stats, err := svc.UsageStats(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(stats) != 1 {
		t.Fatalf("stats: got %d entries, want 1", len(stats))
	}
	if stats[0].TokenID != "pat-1" {
		t.Errorf("TokenID: got %q, want %q", stats[0].TokenID, "pat-1")
	}
	// Verify scoping — repo captures tenantID and principalID.
	f := repo.LastFilter()
	if f.TenantID != "tenant-X" {
		t.Errorf("TenantID passed to repo: got %q, want %q", f.TenantID, "tenant-X")
	}
	if f.PrincipalID == nil || *f.PrincipalID != "user-Y" {
		t.Errorf("PrincipalID passed to repo: got %v, want pointer to %q", f.PrincipalID, "user-Y")
	}
}

func TestUsageStats_RepoError_IsWrapped(t *testing.T) {
	t.Parallel()
	repo := newMockUsageRepo()
	repo.statsErr = errors.New("db connection lost")
	svc := newTestUsageService(t, repo)

	ctx := ctxFromClaims(makeClaims("t1", "u1", token.PrincipalTypeHuman, ""))
	_, err := svc.UsageStats(ctx)
	if err == nil {
		t.Fatal("expected error from repo, got nil")
	}
}

func TestUsageStats_EmptyResult_ReturnsNil(t *testing.T) {
	t.Parallel()
	repo := newMockUsageRepo() // statsResult is nil by default
	svc := newTestUsageService(t, repo)

	ctx := ctxFromClaims(makeClaims("t1", "u1", token.PrincipalTypeHuman, ""))
	stats, err := svc.UsageStats(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(stats) != 0 {
		t.Errorf("expected empty stats, got %d entries", len(stats))
	}
}

func TestListUsage_PATScoping_TokenIDPassedThrough(t *testing.T) {
	t.Parallel()
	const patID = "pat-abc"
	repo := newMockUsageRepo()
	svc := newTestUsageService(t, repo)

	ctx := ctxFromClaims(makeClaims("t1", "u1", token.PrincipalTypePAT, patID))
	// Caller requests no token_id filter — service should not inject one.
	// Token attribution happens at write time, not read time.
	_, _, err := svc.ListUsage(ctx, InferenceUsageFilter{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if repo.lastFilter.TokenID != nil {
		t.Errorf("TokenID filter: got %v, want nil (token_id filter is opt-in by caller)", repo.lastFilter.TokenID)
	}
}
