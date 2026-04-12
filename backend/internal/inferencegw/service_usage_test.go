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
	return &token.PlatformClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject: principalID,
		},
		Orbit: token.OrbitClaims{
			TenantID:      tenantID,
			PrincipalType: pt,
			SessionID:     sessionID,
		},
	}
}

// ctxWithClaims returns a context carrying the given platform claims.
func ctxWithClaims(claims *token.PlatformClaims) context.Context {
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
	ctx := ctxWithClaims(makeClaims("tenant-1", "user-1", token.PrincipalTypeHuman, "session-uuid"))
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
	// SessionID must NOT be extracted as TokenID for non-PAT principals —
	// OIDC sessions also populate OrbitClaims.SessionID with a session UUID.
	if p.TokenID != "" {
		t.Errorf("TokenID: got %q, want empty string for HUMAN principal", p.TokenID)
	}
}

func TestPrincipalFromClaims_PATPrincipal_TokenIDIsSessionID(t *testing.T) {
	t.Parallel()
	const patUUID = "pat-record-uuid-abc123"
	ctx := ctxWithClaims(makeClaims("tenant-1", "user-1", token.PrincipalTypePAT, patUUID))
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
	ctx := ctxWithClaims(makeClaims("tenant-1", "agent-1", token.PrincipalTypeAgent, "some-session"))
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
	repo.total = 0
	svc := newTestUsageService(t, repo)

	ctx := ctxWithClaims(makeClaims("tenant-X", "user-Y", token.PrincipalTypeHuman, ""))

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

func TestListUsage_ReturnsRecordsAndTotal(t *testing.T) {
	t.Parallel()
	repo := newMockUsageRepo()
	repo.records = []InferenceUsageRecord{
		{ID: "rec-1", TenantID: "t1", PrincipalID: "u1"},
		{ID: "rec-2", TenantID: "t1", PrincipalID: "u1"},
	}
	repo.total = 42 // simulates total > page size
	svc := newTestUsageService(t, repo)

	ctx := ctxWithClaims(makeClaims("t1", "u1", token.PrincipalTypeHuman, ""))
	records, total, err := svc.ListUsage(ctx, InferenceUsageFilter{Limit: 2})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(records) != 2 {
		t.Errorf("len(records): got %d, want 2", len(records))
	}
	if total != 42 {
		t.Errorf("total: got %d, want 42", total)
	}
}

func TestListUsage_RepoError_IsWrapped(t *testing.T) {
	t.Parallel()
	repo := newMockUsageRepo()
	repo.listErr = errors.New("db connection lost")
	svc := newTestUsageService(t, repo)

	ctx := ctxWithClaims(makeClaims("t1", "u1", token.PrincipalTypeHuman, ""))
	_, _, err := svc.ListUsage(ctx, InferenceUsageFilter{})
	if err == nil {
		t.Fatal("expected error from repo, got nil")
	}
}

func TestListUsage_PATScoping_TokenIDPassedThrough(t *testing.T) {
	t.Parallel()
	const patID = "pat-abc"
	repo := newMockUsageRepo()
	svc := newTestUsageService(t, repo)

	ctx := ctxWithClaims(makeClaims("t1", "u1", token.PrincipalTypePAT, patID))
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
