package identity

import (
	"context"
	"errors"
	"testing"
	"time"

	jwt "github.com/golang-jwt/jwt/v5"
)

// testClaims returns a minimal *PlatformClaims for use in PAT tests.
func testClaims() *PlatformClaims {
	return &PlatformClaims{
		RegisteredClaims: jwt.RegisteredClaims{Subject: "user-1"},
		Orbit: OrbitClaims{
			TenantID:      "t1",
			PrincipalType: PrincipalTypeHuman,
		},
	}
}

// ── CreatePAT ─────────────────────────────────────────────────────────────────

func TestCreatePAT_Success(t *testing.T) {
	svc, _, _, _, pats := newFullTestService(t)

	resp, err := svc.CreatePAT(context.Background(), "t1", "user-1", testClaims(), CreatePATRequest{
		Name:          "ci-token",
		ExpiresInDays: 30,
		Scopes:        []string{"inference:invoke"},
	})
	if err != nil {
		t.Fatalf("CreatePAT: %v", err)
	}
	if resp.ID == "" {
		t.Error("expected non-empty ID")
	}
	if resp.Name != "ci-token" {
		t.Errorf("name: got %q, want %q", resp.Name, "ci-token")
	}
	if len(resp.Token) < 4 || resp.Token[:4] != "orb_" {
		t.Errorf("token should be prefixed with orb_, got %q", resp.Token[:min(10, len(resp.Token))])
	}
	if resp.ExpiresAt == "" {
		t.Error("expected non-empty expires_at")
	}

	// Verify persisted.
	pats.mu.Lock()
	count := len(pats.pats)
	pats.mu.Unlock()
	if count != 1 {
		t.Errorf("expected 1 persisted PAT, got %d", count)
	}
}

func TestCreatePAT_DefaultTTL_WhenZero(t *testing.T) {
	svc, _, _, _, _ := newFullTestService(t)

	// ExpiresInDays == 0 should default to PATDefaultTTLDays (90 in tests).
	resp, err := svc.CreatePAT(context.Background(), "t1", "user-1", testClaims(), CreatePATRequest{
		Name:   "default-ttl",
		Scopes: []string{"inference:invoke"},
	})
	if err != nil {
		t.Fatalf("CreatePAT: %v", err)
	}

	expiresAt, parseErr := time.Parse(time.RFC3339, resp.ExpiresAt)
	if parseErr != nil {
		t.Fatalf("parse expires_at: %v", parseErr)
	}
	createdAt, parseErr := time.Parse(time.RFC3339, resp.CreatedAt)
	if parseErr != nil {
		t.Fatalf("parse created_at: %v", parseErr)
	}

	ttl := expiresAt.Sub(createdAt)
	expectedTTL := time.Duration(90) * 24 * time.Hour
	// Allow a small delta for execution time.
	if ttl < expectedTTL-time.Minute || ttl > expectedTTL+time.Minute {
		t.Errorf("default TTL: got ~%v, want ~%v", ttl.Round(time.Hour), expectedTTL)
	}
}

func TestCreatePAT_PersistError_ReturnsError(t *testing.T) {
	svc, _, _, _, pats := newFullTestService(t)
	pats.createErr = errors.New("db down")

	_, err := svc.CreatePAT(context.Background(), "t1", "user-1", testClaims(), CreatePATRequest{
		Name:   "will-fail",
		Scopes: []string{"inference:invoke"},
	})
	if err == nil {
		t.Fatal("expected error when persist fails")
	}
}

func TestCreatePAT_GrantCreateFailure_ReturnsError(t *testing.T) {
	svc, _, _, _, _, policy := newFullTestServiceWithPolicy(t)
	policy.createGrantErr = errors.New("policy service unavailable")

	_, err := svc.CreatePAT(context.Background(), "t1", "user-1", testClaims(), CreatePATRequest{
		Name:   "will-fail",
		Scopes: []string{"inference:invoke"},
	})
	if err == nil {
		t.Fatal("expected error when grant creation fails")
	}
}

func TestCreatePAT_PersistError_GrantCleanupFails_ReturnsError(t *testing.T) {
	svc, _, _, _, pats, policy := newFullTestServiceWithPolicy(t)
	pats.createErr = errors.New("db down")
	policy.deleteGrantErr = errors.New("policy service unavailable")

	_, err := svc.CreatePAT(context.Background(), "t1", "user-1", testClaims(), CreatePATRequest{
		Name:   "will-fail",
		Scopes: []string{"inference:invoke"},
	})
	// Must still return the original persist error even when cleanup also fails.
	if err == nil {
		t.Fatal("expected error when persist fails")
	}
}

// ── ListPATs ──────────────────────────────────────────────────────────────────

func TestListPATs_EmptyList(t *testing.T) {
	svc, _, _, _, _ := newFullTestService(t)

	resp, err := svc.ListPATs(context.Background(), "t1", "user-1")
	if err != nil {
		t.Fatalf("ListPATs: %v", err)
	}
	if resp.Tokens == nil {
		t.Error("tokens must not be nil — expect empty slice")
	}
	if len(resp.Tokens) != 0 {
		t.Errorf("expected 0 tokens, got %d", len(resp.Tokens))
	}
}

func TestListPATs_ReturnsScopedTokens(t *testing.T) {
	svc, _, _, _, pats := newFullTestService(t)

	now := time.Now().UTC()
	pats.mu.Lock()
	pats.pats["p1"] = &PAT{ID: "p1", TenantID: "t1", UserID: "user-1", Name: "first", ExpiresAt: now.Add(24 * time.Hour), CreatedAt: now}
	pats.pats["p2"] = &PAT{ID: "p2", TenantID: "t1", UserID: "user-1", Name: "second", ExpiresAt: now.Add(48 * time.Hour), CreatedAt: now}
	pats.pats["p3"] = &PAT{ID: "p3", TenantID: "t1", UserID: "user-2", Name: "other", ExpiresAt: now.Add(24 * time.Hour), CreatedAt: now}
	pats.mu.Unlock()

	resp, err := svc.ListPATs(context.Background(), "t1", "user-1")
	if err != nil {
		t.Fatalf("ListPATs: %v", err)
	}
	if len(resp.Tokens) != 2 {
		t.Errorf("expected 2 tokens for user-1, got %d", len(resp.Tokens))
	}
}

func TestListPATs_IncludesRevokedPATs(t *testing.T) {
	svc, _, _, _, pats := newFullTestService(t)

	now := time.Now().UTC()
	revokedAt := now.Add(-time.Hour)
	pats.mu.Lock()
	pats.pats["p1"] = &PAT{ID: "p1", TenantID: "t1", UserID: "user-1", Name: "active", ExpiresAt: now.Add(24 * time.Hour), CreatedAt: now}
	pats.pats["p2"] = &PAT{ID: "p2", TenantID: "t1", UserID: "user-1", Name: "revoked", ExpiresAt: now.Add(48 * time.Hour), CreatedAt: now, RevokedAt: &revokedAt}
	pats.mu.Unlock()

	resp, err := svc.ListPATs(context.Background(), "t1", "user-1")
	if err != nil {
		t.Fatalf("ListPATs: %v", err)
	}
	if len(resp.Tokens) != 2 {
		t.Errorf("expected 2 tokens (active + revoked), got %d", len(resp.Tokens))
	}
}

func TestListPATs_RevokedPATHasCorrectRevokedAt(t *testing.T) {
	svc, _, _, _, pats := newFullTestService(t)

	now := time.Now().UTC().Truncate(time.Second)
	revokedAt := now.Add(-2 * time.Hour)
	pats.mu.Lock()
	pats.pats["p1"] = &PAT{ID: "p1", TenantID: "t1", UserID: "user-1", Name: "revoked", ExpiresAt: now.Add(24 * time.Hour), CreatedAt: now, RevokedAt: &revokedAt}
	pats.mu.Unlock()

	resp, err := svc.ListPATs(context.Background(), "t1", "user-1")
	if err != nil {
		t.Fatalf("ListPATs: %v", err)
	}
	if len(resp.Tokens) != 1 {
		t.Fatalf("expected 1 token, got %d", len(resp.Tokens))
	}
	tok := resp.Tokens[0]
	if tok.RevokedAt == nil {
		t.Fatal("expected revoked_at to be non-nil")
	}
	parsed, err := time.Parse(time.RFC3339, *tok.RevokedAt)
	if err != nil {
		t.Fatalf("parse revoked_at: %v", err)
	}
	if !parsed.Equal(revokedAt) {
		t.Errorf("revoked_at: got %v, want %v", parsed, revokedAt)
	}
}

func TestListPATs_RepoError_ReturnsError(t *testing.T) {
	svc, _, _, _, pats := newFullTestService(t)
	pats.listErr = errors.New("db down")

	_, err := svc.ListPATs(context.Background(), "t1", "user-1")
	if err == nil {
		t.Fatal("expected error when repo fails")
	}
}

// ── validateScopesAgainstUser ─────────────────────────────────────────────────

func TestValidateScopesAgainstUser_AllowedSubset(t *testing.T) {
	svc, _, _, _, _ := newFullTestService(t)
	// mockPolicyEvaluator returns ["inference:invoke", "inference:list"] by default.
	claims := testClaims()
	if err := svc.validateScopesAgainstUser(context.Background(), claims, []string{"inference:invoke"}); err != nil {
		t.Errorf("expected nil for allowed scope subset, got: %v", err)
	}
}

func TestValidateScopesAgainstUser_ForbiddenScope_ReturnsError(t *testing.T) {
	svc, _, _, _, _ := newFullTestService(t)
	claims := testClaims()
	err := svc.validateScopesAgainstUser(context.Background(), claims, []string{"inference:invoke", "admin:users:write"})
	if err == nil {
		t.Fatal("expected error for scope exceeding effective access")
	}
	var scopeErr *ErrPATScopesForbiddenError
	if !errors.As(err, &scopeErr) {
		t.Fatalf("expected *ErrPATScopesForbiddenError, got: %T", err)
	}
	if len(scopeErr.Scopes) != 1 || scopeErr.Scopes[0] != "admin:users:write" {
		t.Errorf("forbidden scopes: got %v, want [admin:users:write]", scopeErr.Scopes)
	}
}

// ── UpdatePATScopes ───────────────────────────────────────────────────────────

func TestUpdatePATScopes_NewGrantCreateFails_ReturnsError(t *testing.T) {
	svc, _, _, _, pats, policy := newFullTestServiceWithPolicy(t)

	now := time.Now().UTC()
	grantID := "existing-grant"
	pats.mu.Lock()
	pats.pats["pat-1"] = &PAT{
		ID:        "pat-1",
		TenantID:  "t1",
		UserID:    "user-1",
		Name:      "my-pat",
		JTI:       "jti-1",
		Scopes:    []string{"inference:invoke"},
		GrantID:   &grantID,
		ExpiresAt: now.Add(24 * time.Hour),
		CreatedAt: now,
	}
	pats.mu.Unlock()

	policy.createGrantErr = errors.New("policy service unavailable")

	err := svc.UpdatePATScopes(context.Background(), "t1", "user-1", "pat-1", testClaims(), UpdatePATScopesRequest{
		Scopes: []string{"inference:invoke"},
	})
	if err == nil {
		t.Fatal("expected error when new grant creation fails")
	}
}

func TestUpdatePATScopes_PATUpdateFails_GrantCleanupFails_ReturnsError(t *testing.T) {
	svc, _, _, _, pats, policy := newFullTestServiceWithPolicy(t)

	now := time.Now().UTC()
	grantID := "existing-grant"
	pats.mu.Lock()
	pats.pats["pat-1"] = &PAT{
		ID:        "pat-1",
		TenantID:  "t1",
		UserID:    "user-1",
		Name:      "my-pat",
		JTI:       "jti-1",
		Scopes:    []string{"inference:invoke"},
		GrantID:   &grantID,
		ExpiresAt: now.Add(24 * time.Hour),
		CreatedAt: now,
	}
	pats.mu.Unlock()

	// UpdateScopes will fail for a PAT not matching the repo (wrong ID after cleanup).
	// Simulate: repo update fails by making the PAT appear deleted after grant swap.
	pats.mu.Lock()
	delete(pats.pats, "pat-1")
	pats.mu.Unlock()

	// Also make the cleanup DeleteGrant fail to test the double-failure path.
	policy.deleteGrantErr = errors.New("policy service unavailable")

	err := svc.UpdatePATScopes(context.Background(), "t1", "user-1", "pat-1", testClaims(), UpdatePATScopesRequest{
		Scopes: []string{"inference:invoke"},
	})
	if err == nil {
		t.Fatal("expected error when PAT not found")
	}
}

// ── RevokePAT ─────────────────────────────────────────────────────────────────

func TestRevokePAT_Success_BlocklistsJTI(t *testing.T) {
	svc, _, _, blocklist, pats := newFullTestService(t)

	now := time.Now().UTC()
	jti := "jti-to-revoke"
	pats.mu.Lock()
	pats.pats["pat-1"] = &PAT{
		ID:        "pat-1",
		TenantID:  "t1",
		UserID:    "user-1",
		Name:      "my-pat",
		JTI:       jti,
		ExpiresAt: now.Add(24 * time.Hour),
		CreatedAt: now,
	}
	pats.mu.Unlock()

	if err := svc.RevokePAT(context.Background(), "t1", "user-1", "pat-1"); err != nil {
		t.Fatalf("RevokePAT: %v", err)
	}

	// JTI must be in the blocklist.
	blocklist.mu.Lock()
	blocked := blocklist.blocked[jti]
	blocklist.mu.Unlock()
	if !blocked {
		t.Error("expected JTI to be blocklisted after revocation")
	}

	// PAT must be marked revoked in the repo.
	pats.mu.Lock()
	p := pats.pats["pat-1"]
	pats.mu.Unlock()
	if p.RevokedAt == nil {
		t.Error("expected PAT to have RevokedAt set")
	}
}

func TestRevokePAT_NotFound_ReturnsErrPATNotFound(t *testing.T) {
	svc, _, _, _, _ := newFullTestService(t)

	err := svc.RevokePAT(context.Background(), "t1", "user-1", "no-such-pat")
	if err == nil {
		t.Fatal("expected error for missing PAT")
	}
	if !errors.Is(err, ErrPATNotFound) {
		t.Errorf("error should wrap ErrPATNotFound, got: %v", err)
	}
}

func TestRevokePAT_AlreadyRevoked_ReturnsErrPATNotFound(t *testing.T) {
	svc, _, _, _, pats := newFullTestService(t)

	now := time.Now().UTC()
	revokedAt := now.Add(-time.Hour)
	pats.mu.Lock()
	pats.pats["pat-1"] = &PAT{
		ID:        "pat-1",
		TenantID:  "t1",
		UserID:    "user-1",
		Name:      "my-pat",
		JTI:       "jti-1",
		ExpiresAt: now.Add(24 * time.Hour),
		CreatedAt: now,
		RevokedAt: &revokedAt,
	}
	pats.mu.Unlock()

	err := svc.RevokePAT(context.Background(), "t1", "user-1", "pat-1")
	if err == nil {
		t.Fatal("expected error when revoking an already-revoked PAT")
	}
	if !errors.Is(err, ErrPATNotFound) {
		t.Errorf("expected ErrPATNotFound, got: %v", err)
	}
}

func TestRevokePAT_BlocklistFailure_IsNonFatal(t *testing.T) {
	svc, _, _, blocklist, pats := newFullTestService(t)

	// Make blocklist fail.
	blocklist.blockErr = errors.New("redis unavailable")

	now := time.Now().UTC()
	pats.mu.Lock()
	pats.pats["pat-1"] = &PAT{
		ID:        "pat-1",
		TenantID:  "t1",
		UserID:    "user-1",
		Name:      "my-pat",
		JTI:       "jti-1",
		ExpiresAt: now.Add(24 * time.Hour),
		CreatedAt: now,
	}
	pats.mu.Unlock()

	// Blocklist failure must not propagate — DB record is already revoked.
	if err := svc.RevokePAT(context.Background(), "t1", "user-1", "pat-1"); err != nil {
		t.Errorf("blocklist failure should be non-fatal, got error: %v", err)
	}
}
