package identity

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/clear-route/orbit/internal/platform/logging"
)

// Compile-time interface satisfaction checks.
var _ SessionRepository = (*mockSessionRepository)(nil)
var _ RefreshTokenRepository = (*mockRefreshTokenRepository)(nil)
var _ JTIBlocklistRepository = (*mockJTIBlocklistRepository)(nil)

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

func (m *mockJTIBlocklistRepository) IsBlocked(_ context.Context, jti string) (bool, error) {
	if m.isBlockedErr != nil {
		return false, m.isBlockedErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.blocked[jti], nil
}

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
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
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
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"keys": []interface{}{}})
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
// signing key. This is valid because APP_ENV defaults to "local".
func newTestService(t *testing.T, sessions SessionRepository, refreshTokens RefreshTokenRepository, blocklist JTIBlocklistRepository) *Service {
	t.Helper()
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
		&XFCCPrincipalExtractor{},
		logging.NewLoggerWithOutput(io.Discard),
	)
	if err != nil {
		t.Fatalf("newTestService: %v", err)
	}
	return svc
}

// newTestServiceWithOIDC builds a *Service pointed at a live OIDC test server.
func newTestServiceWithOIDC(
	t *testing.T,
	issuerURL string,
	sessions SessionRepository,
	refreshTokens RefreshTokenRepository,
	blocklist JTIBlocklistRepository,
) *Service {
	t.Helper()
	cfg := &Config{
		OIDC: OIDCConfig{
			IssuerURL: issuerURL,
			ClientID:  "test-client",
			Scopes:    []string{"openid"},
		},
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
) {
	t.Helper()
	sessions = newMockSessionRepo()
	refreshTokens = newMockRefreshTokenRepo()
	blocklist = newMockBlocklistRepo()
	svc = newTestService(t, sessions, refreshTokens, blocklist)
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
