package identity

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	jwt "github.com/golang-jwt/jwt/v5"

	clientpolicy "github.com/clear-route/orbit/internal/platform/clients/policy"
	"github.com/clear-route/orbit/internal/platform/logging"
	"github.com/clear-route/orbit/internal/platform/policy"
)

// Compile-time interface satisfaction checks.
var _ SessionRepository = (*mockSessionRepository)(nil)
var _ RefreshTokenRepository = (*mockRefreshTokenRepository)(nil)
var _ JTIBlocklistRepository = (*mockJTIBlocklistRepository)(nil)
var _ PATRepository = (*mockPATRepository)(nil)
var _ UserProfileRepository = (*mockUserProfileRepository)(nil)

// ── mockSessionRepository ─────────────────────────────────────────────────────

type mockSessionRepository struct {
	mu       sync.Mutex
	sessions map[string]*Session
	byJTI    map[string]*Session

	createErr   error
	getByIDErr  error
	getByJTIErr error
	revokeErr   error
}

func newMockSessionRepo() *mockSessionRepository {
	return &mockSessionRepository{
		sessions: make(map[string]*Session),
		byJTI:    make(map[string]*Session),
	}
}

func (m *mockSessionRepository) CreateSession(_ context.Context, s *Session) error {
	if m.createErr != nil {
		return m.createErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *s
	m.sessions[s.ID] = &cp
	m.byJTI[s.JTI] = &cp
	return nil
}

func (m *mockSessionRepository) GetSessionByID(_ context.Context, id string) (*Session, error) {
	if m.getByIDErr != nil {
		return nil, m.getByIDErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	if !ok {
		return nil, ErrSessionNotFound
	}
	return s, nil
}

func (m *mockSessionRepository) GetSessionByJTI(_ context.Context, jti string) (*Session, error) {
	if m.getByJTIErr != nil {
		return nil, m.getByJTIErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.byJTI[jti]
	if !ok {
		return nil, ErrSessionNotFound
	}
	return s, nil
}

func (m *mockSessionRepository) RevokeSession(_ context.Context, id string, revokedAt time.Time) error {
	if m.revokeErr != nil {
		return m.revokeErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	if !ok {
		return ErrSessionNotFound
	}
	s.RevokedAt = &revokedAt
	return nil
}

// ── mockRefreshTokenRepository ────────────────────────────────────────────────

type mockRefreshTokenRepository struct {
	mu     sync.Mutex
	byHash map[string]*RefreshToken
	byID   map[string]*RefreshToken

	createErr    error
	getByHashErr error
	revokeErr    error
	revokeAllErr error
}

func newMockRefreshTokenRepo() *mockRefreshTokenRepository {
	return &mockRefreshTokenRepository{
		byHash: make(map[string]*RefreshToken),
		byID:   make(map[string]*RefreshToken),
	}
}

func (m *mockRefreshTokenRepository) CreateRefreshToken(_ context.Context, rt *RefreshToken) error {
	if m.createErr != nil {
		return m.createErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *rt
	m.byHash[rt.TokenHash] = &cp
	m.byID[rt.ID] = &cp
	return nil
}

func (m *mockRefreshTokenRepository) GetRefreshTokenByHash(_ context.Context, hash string) (*RefreshToken, error) {
	if m.getByHashErr != nil {
		return nil, m.getByHashErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	rt, ok := m.byHash[hash]
	if !ok {
		return nil, ErrRefreshTokenNotFound
	}
	return rt, nil
}

func (m *mockRefreshTokenRepository) RevokeRefreshToken(_ context.Context, id string, revokedAt time.Time) error {
	if m.revokeErr != nil {
		return m.revokeErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	rt, ok := m.byID[id]
	if !ok {
		return ErrRefreshTokenNotFound
	}
	rt.RevokedAt = &revokedAt
	return nil
}

func (m *mockRefreshTokenRepository) RevokeAllForSession(_ context.Context, sessionID string, revokedAt time.Time) error {
	if m.revokeAllErr != nil {
		return m.revokeAllErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, rt := range m.byHash {
		if rt.SessionID == sessionID {
			rt.RevokedAt = &revokedAt
		}
	}
	return nil
}

// ── mockJTIBlocklistRepository ────────────────────────────────────────────────

type mockJTIBlocklistRepository struct {
	mu           sync.Mutex
	blocked      map[string]bool
	blockErr     error
	isBlockedErr error
}

func newMockBlocklistRepo() *mockJTIBlocklistRepository {
	return &mockJTIBlocklistRepository{blocked: make(map[string]bool)}
}

func (m *mockJTIBlocklistRepository) BlockJTI(_ context.Context, jti string, _ time.Duration) error {
	if m.blockErr != nil {
		return m.blockErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.blocked[jti] = true
	return nil
}

// ── mockAuthCodeRepository ────────────────────────────────────────────────────

type mockAuthCodeRepository struct {
	codes map[string]AuthCode
}

func newMockAuthCodeRepo() *mockAuthCodeRepository {
	return &mockAuthCodeRepository{codes: make(map[string]AuthCode)}
}

func (m *mockAuthCodeRepository) Store(_ context.Context, code string, data AuthCode, _ time.Duration) error {
	m.codes[code] = data
	return nil
}

func (m *mockAuthCodeRepository) Consume(_ context.Context, code string) (AuthCode, error) {
	data, ok := m.codes[code]
	if !ok {
		return AuthCode{}, ErrAuthCodeNotFound
	}
	delete(m.codes, code)
	return data, nil
}

func (m *mockJTIBlocklistRepository) IsBlocked(_ context.Context, jti string) (bool, error) {
	if m.isBlockedErr != nil {
		return false, m.isBlockedErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.blocked[jti], nil
}

// ── mockPATRepository ─────────────────────────────────────────────────────────

type mockPATRepository struct {
	mu        sync.Mutex
	pats      map[string]*PAT
	createErr error
	listErr   error
	getErr    error
	revokeErr error
}

func newMockPATRepo() *mockPATRepository {
	return &mockPATRepository{pats: make(map[string]*PAT)}
}

func (m *mockPATRepository) CreatePAT(_ context.Context, pat *PAT, _ string) error {
	if m.createErr != nil {
		return m.createErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *pat
	m.pats[pat.ID] = &cp
	return nil
}

func (m *mockPATRepository) ListByUser(_ context.Context, tenantID, userID string) ([]PAT, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []PAT
	for _, p := range m.pats {
		if p.TenantID == tenantID && p.UserID == userID {
			out = append(out, *p)
		}
	}
	return out, nil
}

func (m *mockPATRepository) GetByID(_ context.Context, tenantID, userID, id string) (*PAT, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.pats[id]
	if !ok || p.TenantID != tenantID || p.UserID != userID {
		return nil, ErrPATNotFound
	}
	cp := *p
	return &cp, nil
}

func (m *mockPATRepository) UpdateScopes(_ context.Context, tenantID, userID, id string, scopes []string, grantID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.pats[id]
	if !ok || p.TenantID != tenantID || p.UserID != userID || p.RevokedAt != nil {
		return ErrPATNotFound
	}
	p.Scopes = scopes
	p.GrantID = &grantID
	return nil
}

func (m *mockPATRepository) Revoke(_ context.Context, tenantID, userID, id string, revokedAt time.Time) error {
	if m.revokeErr != nil {
		return m.revokeErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.pats[id]
	if !ok || p.TenantID != tenantID || p.UserID != userID || p.RevokedAt != nil {
		return ErrPATNotFound
	}
	p.RevokedAt = &revokedAt
	return nil
}

func (m *mockPATRepository) GetOwnersByIDs(_ context.Context, tenantID string, ids []string) (map[string]PATOwnerInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make(map[string]PATOwnerInfo, len(ids))
	for _, id := range ids {
		if p, ok := m.pats[id]; ok && p.TenantID == tenantID {
			result[id] = PATOwnerInfo{UserID: p.UserID, PATName: p.Name}
		}
	}
	return result, nil
}

// ── mockPolicyClient ──────────────────────────────────────────────────────────

type mockPolicyClient struct {
	createGrantErr error
	deleteGrantErr error
}

func (m *mockPolicyClient) CreateGrant(_ context.Context, _, _, _ string, _ []string, _ *clientpolicy.BudgetAssignment) (string, error) {
	if m.createGrantErr != nil {
		return "", m.createGrantErr
	}
	return "mock-grant-id", nil
}

func (m *mockPolicyClient) DeleteGrant(_ context.Context, _ string) error {
	return m.deleteGrantErr
}

// ── mockPolicyEvaluator ───────────────────────────────────────────────────────

type mockPolicyEvaluator struct {
	scopes []string
}

func (m *mockPolicyEvaluator) Check(_ context.Context, _ policy.CheckInput) (policy.Result, error) {
	return policy.Result{Allow: true}, nil
}

func (m *mockPolicyEvaluator) Query(_ context.Context, input policy.QueryInput) ([]string, error) {
	ids := make([]string, len(input.Candidates))
	for i, c := range input.Candidates {
		ids[i] = c.ID
	}
	return ids, nil
}

func (m *mockPolicyEvaluator) Scopes(_ context.Context, _ policy.ScopesInput) ([]string, error) {
	if m.scopes != nil {
		return m.scopes, nil
	}
	return []string{"inference:invoke", "inference:list"}, nil
}

func (m *mockPolicyEvaluator) ScopesExplain(_ context.Context, _ policy.ScopesInput) (*policy.ScopesExplainResult, error) {
	return &policy.ScopesExplainResult{}, nil
}

func (m *mockPolicyEvaluator) CheckBudget(_ context.Context, _ string, _ policy.BudgetCheckRequest) (policy.BudgetResult, error) {
	return policy.BudgetResult{Allow: true}, nil
}

func (m *mockPolicyEvaluator) GetTenantBudget(_ context.Context, _ string) (*policy.TenantBudget, error) {
	return nil, nil
}

func (m *mockPolicyEvaluator) GetBudgetCategories(_ context.Context, _, _ string) ([]policy.BudgetCategory, error) {
	return nil, nil
}

func (m *mockPolicyEvaluator) GetRoleBudgets(_ context.Context, _ string, _ []string) ([]policy.BudgetCategory, error) {
	return nil, nil
}

func (m *mockPolicyEvaluator) ListBudgets(_ context.Context, _ string) (map[string]policy.PrincipalBudget, map[string]policy.RoleBudget, error) {
	return map[string]policy.PrincipalBudget{}, map[string]policy.RoleBudget{}, nil
}

func (m *mockPolicyEvaluator) Start(_ context.Context) {}

// ── OIDC test server ──────────────────────────────────────────────────────────

// newOIDCTestServer starts a minimal OIDC-compliant HTTP server and returns the
// issuer URL along with a setter for the token endpoint response. The server is
// closed automatically via t.Cleanup.
func newOIDCTestServer(t *testing.T) (issuerURL string, setTokenResponse func(status int, body string)) {
	t.Helper()

	var mu sync.Mutex
	tokenStatus := http.StatusBadRequest
	tokenBody := `{"error":"authorization_pending"}`

	setTokenResponse = func(status int, body string) {
		mu.Lock()
		defer mu.Unlock()
		tokenStatus = status
		tokenBody = body
	}

	mux := http.NewServeMux()
	srv := httptest.NewUnstartedServer(mux)
	srv.Start()
	t.Cleanup(srv.Close)

	base := srv.URL

	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                                base,
			"authorization_endpoint":                base + "/authorize",
			"device_authorization_endpoint":         base + "/device",
			"token_endpoint":                        base + "/token",
			"jwks_uri":                              base + "/keys",
			"response_types_supported":              []string{"code"},
			"subject_types_supported":               []string{"public"},
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	})
	mux.HandleFunc("/keys", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []any{}})
	})
	mux.HandleFunc("/device", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(DeviceAuthResponse{
			DeviceCode:      "test-device-code",
			UserCode:        "TEST-1234",
			VerificationURI: base + "/activate",
			ExpiresIn:       300,
			Interval:        5,
		})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		s, b := tokenStatus, tokenBody
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(s)
		fmt.Fprint(w, b)
	})

	return base, setTokenResponse
}

// ── service construction helpers ──────────────────────────────────────────────

// newTestService builds a *Service with no OIDC configured and an ephemeral RSA
// signing key. ORBIT_ALLOW_EPHEMERAL_KEY is set so initSigningKey falls through
// to ephemeral key generation rather than failing with a misconfiguration error.
func newTestService(t *testing.T, sessions SessionRepository, refreshTokens RefreshTokenRepository, blocklist JTIBlocklistRepository, pats *mockPATRepository) *Service {
	t.Helper()
	// Permit ephemeral key generation in tests — never set this in production.
	t.Setenv("ORBIT_ALLOW_EPHEMERAL_KEY", "true")
	// Unset signing key env vars to force ephemeral key generation in tests.
	// This ensures tests don't depend on the local dev signing key file.
	t.Setenv("ORBIT_TOKEN_SIGNING_KEY_FILE", "")
	t.Setenv("ORBIT_TOKEN_SIGNING_KEY", "")
	// Clear tenant registry so the service starts with no OIDC providers —
	// tests that need OIDC use newTestServiceWithOIDC instead.
	t.Setenv("ORBIT_TENANTS_CONFIG", "")
	cfg := &Config{
		Token: TokenConfig{
			AccessTokenTTLMinutes: 60,
			RefreshTokenTTLDays:   30,
			PATDefaultTTLDays:     90,
			PATMaxTTLDays:         365,
			Audience:              "orbit-platform",
		},
	}
	svc, err := NewService(
		context.Background(),
		cfg,
		sessions,
		refreshTokens,
		blocklist,
		newMockAuthCodeRepo(),
		pats,
		nil,
		&mockPolicyClient{},
		&mockPolicyEvaluator{},
		&XFCCPrincipalExtractor{},
		logging.NewLoggerWithOutput(io.Discard),
	)
	if err != nil {
		t.Fatalf("newTestService: %v", err)
	}
	return svc
}

// newTestServiceWithOIDC builds a *Service pointed at a live OIDC test server.
// It writes a temporary tenant registry file with a single tenant "tenant-1"
// configured to use the given issuerURL and sets ORBIT_TENANTS_CONFIG to point
// at it for the duration of the test.
func newTestServiceWithOIDC(
	t *testing.T,
	issuerURL string,
	sessions SessionRepository,
	refreshTokens RefreshTokenRepository,
	blocklist JTIBlocklistRepository,
) *Service {
	t.Helper()
	// Permit ephemeral key generation in tests — never set this in production.
	t.Setenv("ORBIT_ALLOW_EPHEMERAL_KEY", "true")
	// Unset signing key env vars to force ephemeral key generation in tests.
	// This ensures tests don't depend on the local dev signing key file.
	t.Setenv("ORBIT_TOKEN_SIGNING_KEY_FILE", "")
	t.Setenv("ORBIT_TOKEN_SIGNING_KEY", "")

	// Write a minimal tenant registry so the service initialises its per-tenant
	// OIDC provider. Two tenant IDs are registered so both service tests
	// ("tenant-1") and handler tests ("t1") resolve correctly.
	registry := fmt.Sprintf(`version: "v1"
tenants:
  - id: "tenant-1"
    displayName: "Test Tenant"
    state: "active"
    oidc:
      issuerURL: %q
      clientID: "test-client"
      scopes: [openid]
  - id: "t1"
    displayName: "Test Tenant T1"
    state: "active"
    oidc:
      issuerURL: %q
      clientID: "test-client"
      scopes: [openid]
`, issuerURL, issuerURL)
	f, err := os.CreateTemp(t.TempDir(), "tenants-*.yaml")
	if err != nil {
		t.Fatalf("newTestServiceWithOIDC: create temp registry: %v", err)
	}
	if _, err := f.WriteString(registry); err != nil {
		t.Fatalf("newTestServiceWithOIDC: write temp registry: %v", err)
	}
	f.Close()
	t.Setenv("ORBIT_TENANTS_CONFIG", f.Name())

	cfg := &Config{
		Token: TokenConfig{
			AccessTokenTTLMinutes: 60,
			RefreshTokenTTLDays:   30,
			Audience:              "orbit-platform",
		},
	}
	svc, err := NewService(
		context.Background(),
		cfg,
		sessions,
		refreshTokens,
		blocklist,
		newMockAuthCodeRepo(),
		newMockPATRepo(),
		nil,
		&mockPolicyClient{},
		&mockPolicyEvaluator{},
		&XFCCPrincipalExtractor{},
		logging.NewLoggerWithOutput(io.Discard),
	)
	if err != nil {
		t.Fatalf("newTestServiceWithOIDC: %v", err)
	}
	return svc
}

// newFullTestService builds a *Service with all three mock repos and returns
// them for direct state manipulation.
func newFullTestService(t *testing.T) (
	svc *Service,
	sessions *mockSessionRepository,
	refreshTokens *mockRefreshTokenRepository,
	blocklist *mockJTIBlocklistRepository,
	pats *mockPATRepository,
) {
	t.Helper()
	sessions = newMockSessionRepo()
	refreshTokens = newMockRefreshTokenRepo()
	blocklist = newMockBlocklistRepo()
	pats = newMockPATRepo()
	svc = newTestService(t, sessions, refreshTokens, blocklist, pats)
	return
}

// newFullTestServiceWithPolicy is like newFullTestService but also returns the
// mock policy client so tests can inject errors into CreateGrant / DeleteGrant.
func newFullTestServiceWithPolicy(t *testing.T) (
	svc *Service,
	sessions *mockSessionRepository,
	refreshTokens *mockRefreshTokenRepository,
	blocklist *mockJTIBlocklistRepository,
	pats *mockPATRepository,
	policy *mockPolicyClient,
) {
	t.Helper()
	t.Setenv("ORBIT_ALLOW_EPHEMERAL_KEY", "true")
	t.Setenv("ORBIT_TOKEN_SIGNING_KEY_FILE", "")
	t.Setenv("ORBIT_TOKEN_SIGNING_KEY", "")
	t.Setenv("ORBIT_TENANTS_CONFIG", "")
	sessions = newMockSessionRepo()
	refreshTokens = newMockRefreshTokenRepo()
	blocklist = newMockBlocklistRepo()
	pats = newMockPATRepo()
	policy = &mockPolicyClient{}
	cfg := &Config{
		Token: TokenConfig{
			AccessTokenTTLMinutes: 60,
			RefreshTokenTTLDays:   30,
			PATDefaultTTLDays:     90,
			PATMaxTTLDays:         365,
			Audience:              "orbit-platform",
		},
	}
	var err error
	svc, err = NewService(
		context.Background(),
		cfg,
		sessions,
		refreshTokens,
		blocklist,
		newMockAuthCodeRepo(),
		pats,
		nil,
		policy,
		&mockPolicyEvaluator{},
		&XFCCPrincipalExtractor{},
		logging.NewLoggerWithOutput(io.Discard),
	)
	if err != nil {
		t.Fatalf("newFullTestServiceWithPolicy: %v", err)
	}
	return
}

// ── data seeding helpers ──────────────────────────────────────────────────────

// seedSession stores a Session directly into the mock repo and returns a pointer
// to the stored value so callers can mutate it (e.g. set RevokedAt).
func seedSession(sessions *mockSessionRepository, id, jti string) *Session {
	now := time.Now()
	s := &Session{
		ID:             id,
		JTI:            jti,
		PrincipalID:    "principal-1",
		PrincipalType:  PrincipalTypeHuman,
		TenantID:       "tenant-1",
		DisplayName:    "Test User",
		IdentitySource: IdentitySourceFederated,
		CreatedAt:      now.Add(-time.Hour),
		ExpiresAt:      now.Add(time.Hour),
	}
	sessions.mu.Lock()
	sessions.sessions[id] = s
	sessions.byJTI[jti] = s
	sessions.mu.Unlock()
	return s
}

// seedRefreshToken stores a RefreshToken keyed by the hash of rawToken and
// returns a pointer to the stored value.
func seedRefreshToken(tokens *mockRefreshTokenRepository, id, sessionID, rawToken string) *RefreshToken {
	hash := hashToken(rawToken)
	now := time.Now()
	rt := &RefreshToken{
		ID:        id,
		SessionID: sessionID,
		TokenHash: hash,
		CreatedAt: now.Add(-time.Hour),
		ExpiresAt: now.Add(30 * 24 * time.Hour),
	}
	tokens.mu.Lock()
	tokens.byHash[hash] = rt
	tokens.byID[id] = rt
	tokens.mu.Unlock()
	return rt
}

// ── signing OIDC test server ──────────────────────────────────────────────────

// newSigningOIDCTestServer starts a minimal OIDC-compliant HTTP server backed by
// a real RSA key pair. Unlike newOIDCTestServer (which returns an empty JWKS),
// this server serves its public key at /keys so that go-oidc can verify tokens.
// The returned issueIDToken helper mints RS256-signed JWTs suitable for passing
// to ExchangeIDToken.
func newSigningOIDCTestServer(t *testing.T) (
	issuerURL string,
	issueIDToken func(clientID, sub, email, name string) string,
) {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("newSigningOIDCTestServer: generate RSA key: %v", err)
	}

	jwksJSON, err := buildJWKS(&key.PublicKey)
	if err != nil {
		t.Fatalf("newSigningOIDCTestServer: build JWKS: %v", err)
	}

	mux := http.NewServeMux()
	srv := httptest.NewUnstartedServer(mux)
	srv.Start()
	t.Cleanup(srv.Close)

	base := srv.URL

	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                                base,
			"authorization_endpoint":                base + "/authorize",
			"token_endpoint":                        base + "/token",
			"jwks_uri":                              base + "/keys",
			"response_types_supported":              []string{"code"},
			"subject_types_supported":               []string{"public"},
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	})
	mux.HandleFunc("/keys", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(jwksJSON)
	})

	issueIDToken = func(clientID, sub, email, name string) string {
		now := time.Now()
		tok := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
			"iss":   base,
			"aud":   jwt.ClaimStrings{clientID},
			"sub":   sub,
			"exp":   now.Add(5 * time.Minute).Unix(),
			"iat":   now.Unix(),
			"email": email,
			"name":  name,
		})
		signed, sigErr := tok.SignedString(key)
		if sigErr != nil {
			t.Fatalf("newSigningOIDCTestServer: sign id_token: %v", sigErr)
		}
		return signed
	}

	return base, issueIDToken
}

// issueIDTokenWithRoles creates a signed ID token that includes a "roles" claim.
// Used by TestExchangeIDToken_PropagatesRoles.
func issueIDTokenWithRoles(
	t *testing.T,
	issuerURL string,
	clientID, sub, email, name string,
	roles []string,
	key *rsa.PrivateKey,
) string {
	t.Helper()
	now := time.Now()
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"iss":   issuerURL,
		"aud":   jwt.ClaimStrings{clientID},
		"sub":   sub,
		"exp":   now.Add(5 * time.Minute).Unix(),
		"iat":   now.Unix(),
		"email": email,
		"name":  name,
		"roles": roles,
	})
	signed, err := tok.SignedString(key)
	if err != nil {
		t.Fatalf("issueIDTokenWithRoles: sign: %v", err)
	}
	return signed
}

// issueIDTokenWithGroups creates a signed ID token that includes a "groups" claim.
// Used by TestExchangeIDToken_PropagatesGroups.
func issueIDTokenWithGroups(
	t *testing.T,
	issuerURL string,
	clientID, sub, email, name string,
	groups []string,
	key *rsa.PrivateKey,
) string {
	t.Helper()
	now := time.Now()
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"iss":    issuerURL,
		"aud":    jwt.ClaimStrings{clientID},
		"sub":    sub,
		"exp":    now.Add(5 * time.Minute).Unix(),
		"iat":    now.Unix(),
		"email":  email,
		"name":   name,
		"groups": groups,
	})
	signed, err := tok.SignedString(key)
	if err != nil {
		t.Fatalf("issueIDTokenWithGroups: sign: %v", err)
	}
	return signed
}

// newSigningOIDCTestServerWithKey is like newSigningOIDCTestServer but also
// returns the private key so callers can issue custom tokens with extra claims.
func newSigningOIDCTestServerWithKey(t *testing.T) (
	issuerURL string,
	issueIDToken func(clientID, sub, email, name string) string,
	privateKey *rsa.PrivateKey,
) {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}

	jwksJSON, err := buildJWKS(&key.PublicKey)
	if err != nil {
		t.Fatalf("build JWKS: %v", err)
	}

	mux := http.NewServeMux()
	srv := httptest.NewUnstartedServer(mux)
	srv.Start()
	t.Cleanup(srv.Close)

	base := srv.URL

	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                                base,
			"authorization_endpoint":                base + "/authorize",
			"token_endpoint":                        base + "/token",
			"jwks_uri":                              base + "/keys",
			"response_types_supported":              []string{"code"},
			"subject_types_supported":               []string{"public"},
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	})
	mux.HandleFunc("/keys", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(jwksJSON)
	})

	issueIDToken = func(clientID, sub, email, name string) string {
		now := time.Now()
		tok := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
			"iss":   base,
			"aud":   jwt.ClaimStrings{clientID},
			"sub":   sub,
			"exp":   now.Add(5 * time.Minute).Unix(),
			"iat":   now.Unix(),
			"email": email,
			"name":  name,
		})
		signed, sigErr := tok.SignedString(key)
		if sigErr != nil {
			t.Fatalf("sign id_token: %v", sigErr)
		}
		return signed
	}

	return base, issueIDToken, key
}

// ── mockUserProfileRepository ─────────────────────────────────────────────────

type mockUserProfileRepository struct {
	mu       sync.Mutex
	profiles map[string]*UserProfile // keyed by id+":"+tenantID

	upsertErr error
	getErr    error
	listErr   error
}

func newMockProfileRepo() *mockUserProfileRepository {
	return &mockUserProfileRepository{profiles: make(map[string]*UserProfile)}
}

func (m *mockUserProfileRepository) key(id, tenantID string) string { return id + ":" + tenantID }

func (m *mockUserProfileRepository) Upsert(_ context.Context, p UserProfile) error {
	if m.upsertErr != nil {
		return m.upsertErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := p
	m.profiles[m.key(p.ID, p.TenantID)] = &cp
	return nil
}

func (m *mockUserProfileRepository) GetByID(_ context.Context, tenantID, id string) (UserProfile, error) {
	if m.getErr != nil {
		return UserProfile{}, m.getErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.profiles[m.key(id, tenantID)]
	if !ok {
		return UserProfile{}, ErrUserProfileNotFound
	}
	return *p, nil
}

func (m *mockUserProfileRepository) List(_ context.Context, tenantID string, filter UserProfileFilter) ([]UserProfile, int, error) {
	if m.listErr != nil {
		return nil, 0, m.listErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	idSet := make(map[string]struct{}, len(filter.IDs))
	for _, id := range filter.IDs {
		idSet[id] = struct{}{}
	}

	var out []UserProfile
	for _, p := range m.profiles {
		if p.TenantID != tenantID {
			continue
		}
		if len(idSet) > 0 {
			if _, ok := idSet[p.ID]; !ok {
				continue
			}
		}
		if filter.Search != "" {
			q := strings.ToLower(filter.Search)
			if !strings.Contains(strings.ToLower(p.Email), q) && !strings.Contains(strings.ToLower(p.DisplayName), q) {
				continue
			}
		}
		out = append(out, *p)
	}

	total := len(out)
	limit := filter.Limit
	if limit <= 0 {
		limit = 20
	}
	page := filter.Page
	if page <= 0 {
		page = 1
	}
	offset := (page - 1) * limit
	if offset >= len(out) {
		return []UserProfile{}, total, nil
	}
	end := min(offset+limit, len(out))
	return out[offset:end], total, nil
}

// newTestControllerWithProfiles wires up a Controller with a seeded profile repo.
// Use for handler tests that exercise /users and /tokens/attribution.
func newTestControllerWithProfiles(t *testing.T) (
	c *Controller,
	pats *mockPATRepository,
	profiles *mockUserProfileRepository,
) {
	t.Helper()
	t.Setenv("ORBIT_ALLOW_EPHEMERAL_KEY", "true")
	t.Setenv("ORBIT_TOKEN_SIGNING_KEY_FILE", "")
	t.Setenv("ORBIT_TOKEN_SIGNING_KEY", "")
	t.Setenv("ORBIT_TENANTS_CONFIG", "")
	pats = newMockPATRepo()
	profiles = newMockProfileRepo()
	cfg := &Config{
		Token: TokenConfig{
			AccessTokenTTLMinutes: 60,
			RefreshTokenTTLDays:   30,
			PATDefaultTTLDays:     90,
			PATMaxTTLDays:         365,
			Audience:              "orbit-platform",
		},
	}
	svc, err := NewService(
		context.Background(),
		cfg,
		newMockSessionRepo(),
		newMockRefreshTokenRepo(),
		newMockBlocklistRepo(),
		newMockAuthCodeRepo(),
		pats,
		profiles,
		&mockPolicyClient{},
		&mockPolicyEvaluator{},
		&XFCCPrincipalExtractor{},
		logging.NewLoggerWithOutput(io.Discard),
	)
	if err != nil {
		t.Fatalf("newTestControllerWithProfiles: %v", err)
	}
	c = NewController(svc, nil, nil, "", logging.NewLoggerWithOutput(io.Discard))
	return
}

// newTestServiceWithDomains builds a *Service with a single active tenant
// configured with the given issuerURL and emailDomains. Use for
// DiscoverTenantByEmail tests that need email routing to be populated.
func newTestServiceWithDomains(t *testing.T, issuerURL string, emailDomains []string) *Service {
	t.Helper()
	t.Setenv("ORBIT_ALLOW_EPHEMERAL_KEY", "true")
	t.Setenv("ORBIT_TOKEN_SIGNING_KEY_FILE", "")
	t.Setenv("ORBIT_TOKEN_SIGNING_KEY", "")

	domainsJSON, _ := json.Marshal(emailDomains)
	registry := fmt.Sprintf(`version: "v1"
tenants:
  - id: "tenant-1"
    displayName: "Test Tenant"
    state: "active"
    oidc:
      issuerURL: %q
      clientID: "test-client"
      scopes: [openid]
      emailDomains: %s
`, issuerURL, string(domainsJSON))

	f, err := os.CreateTemp(t.TempDir(), "tenants-*.yaml")
	if err != nil {
		t.Fatalf("newTestServiceWithDomains: create temp registry: %v", err)
	}
	if _, err := f.WriteString(registry); err != nil {
		t.Fatalf("newTestServiceWithDomains: write temp registry: %v", err)
	}
	f.Close()
	t.Setenv("ORBIT_TENANTS_CONFIG", f.Name())

	cfg := &Config{
		Token: TokenConfig{
			AccessTokenTTLMinutes: 60,
			RefreshTokenTTLDays:   30,
			Audience:              "orbit-platform",
		},
	}
	svc, err := NewService(
		context.Background(),
		cfg,
		newMockSessionRepo(),
		newMockRefreshTokenRepo(),
		newMockBlocklistRepo(),
		newMockAuthCodeRepo(),
		newMockPATRepo(),
		nil,
		&mockPolicyClient{},
		&mockPolicyEvaluator{},
		&XFCCPrincipalExtractor{},
		logging.NewLoggerWithOutput(io.Discard),
	)
	if err != nil {
		t.Fatalf("newTestServiceWithDomains: %v", err)
	}
	return svc
}
