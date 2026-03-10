package identity

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	gooidc "github.com/coreos/go-oidc/v3/oidc"
	jwt "github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/clear-route/orbit/internal/platform/config"
	"github.com/clear-route/orbit/internal/platform/logging"
)

// ─── Configuration ────────────────────────────────────────────────────────────

// Config is the non-secret, YAML-sourced configuration for the identity service.
// Secrets (client credentials, signing key) are loaded from environment variables.
type Config struct {
	OIDC  OIDCConfig  `yaml:"oidc"`
	Token TokenConfig `yaml:"token"`
}

// OIDCConfig holds OIDC provider settings. IssuerURL is not a secret and can
// live in YAML. ClientID and ClientSecret must be supplied via environment
// variables (ORBIT_OIDC_CLIENT_ID, ORBIT_OIDC_CLIENT_SECRET).
type OIDCConfig struct {
	// IssuerURL is the OIDC provider's issuer URL used for discovery.
	// Leave empty to disable federated authentication.
	IssuerURL string `yaml:"issuerUrl"`

	// Scopes is the list of OAuth2 scopes requested during authentication.
	Scopes []string `yaml:"scopes"`

	// ClientID and ClientSecret are loaded from env, not from YAML.
	ClientID     string `yaml:"-"`
	ClientSecret string `yaml:"-"`
}

// TokenConfig controls platform token lifetimes.
type TokenConfig struct {
	// AccessTokenTTLMinutes is the lifetime of a platform session token.
	AccessTokenTTLMinutes int `yaml:"accessTokenTtlMinutes"`

	// RefreshTokenTTLDays is the lifetime of a human refresh token.
	RefreshTokenTTLDays int `yaml:"refreshTokenTtlDays"`
}

// ─── SVID validator interface ─────────────────────────────────────────────────

// SVIDValidator validates a SPIFFE SVID and extracts the embedded principal
// claims. In production this verifies the SVID signature against the SPIRE
// trust bundle. In local development the StubSVIDValidator is used instead.
type SVIDValidator interface {
	Validate(ctx context.Context, svid string) (*Principal, error)
}

// StubSVIDValidator is a test/local-dev stand-in that accepts any SVID string
// in the format "stub:<principal_id>:<tenant_id>" and constructs a principal.
type StubSVIDValidator struct{}

func (s *StubSVIDValidator) Validate(_ context.Context, svid string) (*Principal, error) {
	parts := strings.SplitN(svid, ":", 3)
	if len(parts) != 3 || parts[0] != "stub" {
		return nil, fmt.Errorf("stub validator: expected 'stub:<principal_id>:<tenant_id>', got %q", svid)
	}
	return &Principal{
		ID:             parts[1],
		Type:           PrincipalTypeAgent,
		TenantID:       parts[2],
		DisplayName:    "stub-agent:" + parts[1],
		IdentitySource: IdentitySourceAttested,
		Claims:         map[string]string{"spiffe_id": "spiffe://local/stub/" + parts[1]},
	}, nil
}

// ─── Internal OIDC provider metadata ─────────────────────────────────────────

// oidcProviderMeta holds metadata fetched from the OIDC discovery document
// that is not exposed by go-oidc's Provider type.
type oidcProviderMeta struct {
	DeviceAuthorizationEndpoint string `json:"device_authorization_endpoint"`
	TokenEndpoint               string `json:"token_endpoint"`
}

// ─── Service ──────────────────────────────────────────────────────────────────

// Service implements the identity business logic: federated OIDC authentication
// via the Device Authorization Grant, SPIFFE SVID exchange, JWT issuance,
// session management, and token revocation.
type Service struct {
	cfg           *Config
	sessions      SessionRepository
	refreshTokens RefreshTokenRepository
	blocklist     JTIBlocklistRepository
	logger        logging.Logger

	// OIDC — nil when federated auth is not configured.
	oidcProvider *gooidc.Provider
	oidcMeta     *oidcProviderMeta

	// svid is injected so tests can substitute a stub.
	svid SVIDValidator

	// signingKey is the RSA private key used to sign platform tokens (RS256).
	signingKey *rsa.PrivateKey

	// jwks is the pre-serialized JWKS JSON for GET /.well-known/jwks.json.
	jwksJSON []byte
}

// NewService constructs and initializes the identity Service.
//
// If cfg.OIDC.IssuerURL is non-empty, OIDC provider discovery is performed
// eagerly. OIDC client credentials are read from ORBIT_OIDC_CLIENT_ID and
// ORBIT_OIDC_CLIENT_SECRET environment variables.
//
// The RSA signing key is read from the ORBIT_TOKEN_SIGNING_KEY environment
// variable (PEM-encoded PKCS#8 or PKCS#1 RSA private key). In local
// development, if the variable is not set, a throwaway key is generated and
// a prominent warning is logged. Never use auto-generated keys in production.
func NewService(
	ctx context.Context,
	cfg *Config,
	sessions SessionRepository,
	refreshTokens RefreshTokenRepository,
	blocklist JTIBlocklistRepository,
	svidValidator SVIDValidator,
	logger logging.Logger,
) (*Service, error) {
	svc := &Service{
		cfg:           cfg,
		sessions:      sessions,
		refreshTokens: refreshTokens,
		blocklist:     blocklist,
		svid:          svidValidator,
		logger:        logger,
	}

	// Populate secret fields from environment variables.
	cfg.OIDC.ClientID = os.Getenv("ORBIT_OIDC_CLIENT_ID")
	cfg.OIDC.ClientSecret = os.Getenv("ORBIT_OIDC_CLIENT_SECRET")

	// Allow the issuer URL to be overridden via environment variable without
	// editing YAML files — useful for local dev and CI where you want to point
	// at a real provider (e.g. Entra) without committing the URL to config.
	if envIssuer := os.Getenv("ORBIT_OIDC_ISSUER_URL"); envIssuer != "" {
		cfg.OIDC.IssuerURL = envIssuer
	}

	// Initialize OIDC provider if an issuer URL has been configured.
	if cfg.OIDC.IssuerURL != "" {
		if err := svc.initOIDCProvider(ctx); err != nil {
			return nil, fmt.Errorf("identity service: init oidc provider: %w", err)
		}
		logger.Info(ctx, "OIDC provider initialized",
			logging.Field{Key: "issuer", Value: cfg.OIDC.IssuerURL},
		)
	} else {
		logger.Info(ctx, "OIDC provider not configured — federated authentication disabled")
	}

	// Load or generate the RSA signing key.
	if err := svc.initSigningKey(ctx); err != nil {
		return nil, fmt.Errorf("identity service: init signing key: %w", err)
	}

	// Pre-serialize JWKS for the public key endpoint.
	jwks, err := buildJWKS(&svc.signingKey.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("identity service: build jwks: %w", err)
	}
	svc.jwksJSON = jwks

	return svc, nil
}

// ─── Federated authentication: device authorization grant ────────────────────

// InitiateDeviceAuth starts a Device Authorization Grant flow with the
// configured OIDC provider and returns the device code + user instructions.
func (s *Service) InitiateDeviceAuth(ctx context.Context, req DeviceAuthRequest) (*DeviceAuthResponse, error) {
	if err := s.requireOIDC(); err != nil {
		return nil, err
	}

	scopes := s.cfg.OIDC.Scopes
	if len(scopes) == 0 {
		scopes = []string{"openid", "profile", "email"}
	}

	formVals := url.Values{
		"client_id": {s.cfg.OIDC.ClientID},
		"scope":     {strings.Join(scopes, " ")},
	}

	resp, err := http.PostForm(s.oidcMeta.DeviceAuthorizationEndpoint, formVals) //nolint:noctx
	if err != nil {
		return nil, fmt.Errorf("device auth request: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("device auth endpoint returned %d: %s", resp.StatusCode, body)
	}

	var dar struct {
		DeviceCode              string `json:"device_code"`
		UserCode                string `json:"user_code"`
		VerificationURI         string `json:"verification_uri"`
		VerificationURIComplete string `json:"verification_uri_complete"`
		ExpiresIn               int    `json:"expires_in"`
		Interval                int    `json:"interval"`
	}
	if err := json.Unmarshal(body, &dar); err != nil {
		return nil, fmt.Errorf("decode device auth response: %w", err)
	}

	verifyURI := dar.VerificationURIComplete
	if verifyURI == "" {
		verifyURI = dar.VerificationURI
	}
	interval := dar.Interval
	if interval == 0 {
		interval = 5
	}

	return &DeviceAuthResponse{
		DeviceCode:      dar.DeviceCode,
		UserCode:        dar.UserCode,
		VerificationURI: verifyURI,
		ExpiresIn:       dar.ExpiresIn,
		Interval:        interval,
	}, nil
}

// parseDeviceTokenError maps an OAuth2 error-response body to one of the
// package-level sentinel errors. Returns nil when no recognized error is found.
func parseDeviceTokenError(body []byte) error {
	var oe struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(body, &oe) != nil {
		return nil
	}
	switch oe.Error {
	case "authorization_pending":
		return ErrAuthorizationPending
	case "slow_down":
		return ErrSlowDown
	case "access_denied":
		return ErrAccessDenied
	case "expired_token":
		return ErrDeviceCodeExpired
	}
	return nil
}

// PollDeviceAuth performs a single, non-blocking poll of the OIDC token
// endpoint for a device_code grant. It maps provider error codes to sentinel
// errors so the caller can decide whether to retry or surface the failure.
func (s *Service) PollDeviceAuth(ctx context.Context, req SessionRequest) (*SessionResponse, error) {
	if err := s.requireOIDC(); err != nil {
		return nil, err
	}

	formVals := url.Values{
		"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
		"device_code": {req.DeviceCode},
		"client_id":   {s.cfg.OIDC.ClientID},
	}
	if s.cfg.OIDC.ClientSecret != "" {
		formVals.Set("client_secret", s.cfg.OIDC.ClientSecret)
	}

	resp, err := http.PostForm(s.oidcMeta.TokenEndpoint, formVals) //nolint:noctx
	if err != nil {
		return nil, fmt.Errorf("poll device auth: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	// OAuth2 error responses use 4xx status codes.
	if resp.StatusCode != http.StatusOK {
		if sentinelErr := parseDeviceTokenError(body); sentinelErr != nil {
			return nil, sentinelErr
		}
		return nil, fmt.Errorf("token endpoint returned %d: %s", resp.StatusCode, body)
	}

	var tokenResp struct {
		IDToken string `json:"id_token"`
	}
	if unmarshalErr := json.Unmarshal(body, &tokenResp); unmarshalErr != nil {
		return nil, fmt.Errorf("decode token response: %w", unmarshalErr)
	}
	if tokenResp.IDToken == "" {
		return nil, fmt.Errorf("token endpoint response missing id_token")
	}

	// Verify the ID token signature + claims with go-oidc.
	idToken, err := s.oidcProvider.Verifier(&gooidc.Config{
		ClientID: s.cfg.OIDC.ClientID,
	}).Verify(ctx, tokenResp.IDToken)
	if err != nil {
		return nil, fmt.Errorf("verify id token: %w", err)
	}

	var claims struct {
		Sub               string `json:"sub"`
		Email             string `json:"email"`
		Name              string `json:"name"`
		PreferredUsername string `json:"preferred_username"`
	}
	if err := idToken.Claims(&claims); err != nil {
		return nil, fmt.Errorf("extract id token claims: %w", err)
	}

	displayName := claims.Name
	if displayName == "" {
		displayName = claims.PreferredUsername
	}
	if displayName == "" {
		displayName = claims.Email
	}

	principal := &Principal{
		ID:             claims.Sub,
		Type:           PrincipalTypeHuman,
		TenantID:       req.TenantID,
		DisplayName:    displayName,
		IdentitySource: IdentitySourceFederated,
		Claims: map[string]string{
			"email":  claims.Email,
			"issuer": idToken.Issuer,
		},
	}

	return s.createSession(ctx, principal)
}

// RefreshSession exchanges a valid refresh token for a new access token and
// rotates the refresh token (old token is revoked, new token issued).
func (s *Service) RefreshSession(ctx context.Context, req SessionRequest) (*SessionResponse, error) {
	if req.RefreshToken == "" {
		return nil, fmt.Errorf("refresh_token is required")
	}

	hash := hashToken(req.RefreshToken)

	rt, err := s.refreshTokens.GetRefreshTokenByHash(ctx, hash)
	if err != nil {
		return nil, ErrRefreshTokenNotFound
	}

	if rt.RevokedAt != nil {
		return nil, ErrRefreshTokenRevoked
	}
	if time.Now().After(rt.ExpiresAt) {
		return nil, ErrRefreshTokenExpired
	}

	session, err := s.sessions.GetSessionByID(ctx, rt.SessionID)
	if err != nil {
		return nil, ErrSessionNotFound
	}
	if session.RevokedAt != nil {
		return nil, ErrTokenRevoked
	}

	// Revoke the used refresh token (rotation).
	if err := s.refreshTokens.RevokeRefreshToken(ctx, rt.ID, time.Now()); err != nil {
		return nil, fmt.Errorf("rotate refresh token: %w", err)
	}

	principal := &Principal{
		ID:             session.PrincipalID,
		Type:           session.PrincipalType,
		TenantID:       session.TenantID,
		DisplayName:    session.DisplayName,
		IdentitySource: session.IdentitySource,
		Claims:         session.Claims,
	}

	return s.createSession(ctx, principal)
}

// ─── SVID exchange ────────────────────────────────────────────────────────────

// ExchangeSVID validates a SPIFFE SVID and issues a platform session token for
// the attested agent. Agent sessions have no refresh token — their identity
// lifecycle is managed by SPIRE workload attestation.
func (s *Service) ExchangeSVID(ctx context.Context, req ExchangeRequest) (*SessionResponse, error) {
	principal, err := s.svid.Validate(ctx, req.SVID)
	if err != nil {
		return nil, fmt.Errorf("validate svid: %w", err)
	}
	if req.TenantID != "" {
		principal.TenantID = req.TenantID
	}

	return s.createAgentSession(ctx, principal)
}

// ─── Revocation ───────────────────────────────────────────────────────────────

// RevokeToken revokes an access token or refresh token.
//
//   - "access_token": the JTI is added to the Redis blocklist; the backing
//     session and all its refresh tokens are also revoked in PostgreSQL.
//   - "refresh_token": only the specific refresh token record is revoked.
func (s *Service) RevokeToken(ctx context.Context, req RevokeRequest) error {
	switch req.TokenType {
	case "access_token":
		return s.revokeAccessToken(ctx, req.Token)
	case "refresh_token":
		return s.revokeRefreshToken(ctx, req.Token)
	default:
		return fmt.Errorf("unsupported token_type %q: expected access_token or refresh_token", req.TokenType)
	}
}

func (s *Service) revokeAccessToken(ctx context.Context, token string) error {
	claims, err := s.parseToken(token)
	if err != nil {
		return ErrInvalidToken
	}

	jti := claims.ID
	if jti == "" {
		return ErrInvalidToken
	}

	expiry, err := claims.GetExpirationTime()
	if err == nil && expiry != nil {
		ttl := time.Until(expiry.Time)
		if ttl > 0 {
			if blockErr := s.blocklist.BlockJTI(ctx, jti, ttl); blockErr != nil {
				return fmt.Errorf("block jti: %w", blockErr)
			}
		}
	}

	// Revoke the backing session and all associated refresh tokens.
	session, err := s.sessions.GetSessionByJTI(ctx, jti)
	if err != nil {
		// Session already gone or not found — blocklist entry is sufficient.
		return nil
	}

	now := time.Now()
	if rErr := s.refreshTokens.RevokeAllForSession(ctx, session.ID, now); rErr != nil {
		s.logger.Error(ctx, "failed to revoke refresh tokens for session",
			logging.Field{Key: "session_id", Value: session.ID},
			logging.Field{Key: "error", Value: rErr.Error()},
		)
	}
	if rErr := s.sessions.RevokeSession(ctx, session.ID, now); rErr != nil {
		// Not found is acceptable — it may have already been revoked.
		if rErr != ErrSessionNotFound {
			return fmt.Errorf("revoke session: %w", rErr)
		}
	}

	return nil
}

func (s *Service) revokeRefreshToken(ctx context.Context, token string) error {
	hash := hashToken(token)
	rt, err := s.refreshTokens.GetRefreshTokenByHash(ctx, hash)
	if err != nil {
		return ErrRefreshTokenNotFound
	}
	return s.refreshTokens.RevokeRefreshToken(ctx, rt.ID, time.Now())
}

// ─── JWKS ─────────────────────────────────────────────────────────────────────

// JWKSJson returns the pre-serialized JWKS JSON for the public key endpoint.
func (s *Service) JWKSJson() []byte {
	return s.jwksJSON
}

// ─── Internal helpers ─────────────────────────────────────────────────────────

// createSession creates a full human session: PostgreSQL session + refresh token + signed JWT.
func (s *Service) createSession(ctx context.Context, principal *Principal) (*SessionResponse, error) {
	sessionID := uuid.NewString()
	jti := uuid.NewString()
	now := time.Now().UTC()

	ttl := time.Duration(s.cfg.Token.AccessTokenTTLMinutes) * time.Minute
	if ttl == 0 {
		ttl = 60 * time.Minute // safe default
	}

	session := &Session{
		ID:             sessionID,
		JTI:            jti,
		PrincipalID:    principal.ID,
		PrincipalType:  principal.Type,
		TenantID:       principal.TenantID,
		DisplayName:    principal.DisplayName,
		IdentitySource: principal.IdentitySource,
		Claims:         principal.Claims,
		CreatedAt:      now,
		ExpiresAt:      now.Add(ttl),
	}
	if err := s.sessions.CreateSession(ctx, session); err != nil {
		return nil, fmt.Errorf("persist session: %w", err)
	}

	accessToken, err := s.signToken(principal, jti, sessionID, now, ttl)
	if err != nil {
		return nil, fmt.Errorf("sign access token: %w", err)
	}

	// Issue refresh token (opaque random bytes, only the hash is stored).
	rawRefresh, err := generateOpaqueToken()
	if err != nil {
		return nil, fmt.Errorf("generate refresh token: %w", err)
	}

	refreshTTLDays := s.cfg.Token.RefreshTokenTTLDays
	if refreshTTLDays == 0 {
		refreshTTLDays = 30
	}

	rt := &RefreshToken{
		ID:        uuid.NewString(),
		SessionID: sessionID,
		TokenHash: hashToken(rawRefresh),
		CreatedAt: now,
		ExpiresAt: now.Add(time.Duration(refreshTTLDays) * 24 * time.Hour),
	}
	if err := s.refreshTokens.CreateRefreshToken(ctx, rt); err != nil {
		return nil, fmt.Errorf("persist refresh token: %w", err)
	}

	return &SessionResponse{
		AccessToken:  accessToken,
		TokenType:    "Bearer",
		ExpiresIn:    int(ttl.Seconds()),
		RefreshToken: rawRefresh,
		IssuedAt:     now,
	}, nil
}

// createAgentSession creates an agent session with no refresh token.
// Agent identity lifecycle is managed entirely by SPIRE workload attestation.
func (s *Service) createAgentSession(ctx context.Context, principal *Principal) (*SessionResponse, error) {
	sessionID := uuid.NewString()
	jti := uuid.NewString()
	now := time.Now().UTC()

	ttl := time.Duration(s.cfg.Token.AccessTokenTTLMinutes) * time.Minute
	if ttl == 0 {
		ttl = 60 * time.Minute
	}

	session := &Session{
		ID:             sessionID,
		JTI:            jti,
		PrincipalID:    principal.ID,
		PrincipalType:  principal.Type,
		TenantID:       principal.TenantID,
		DisplayName:    principal.DisplayName,
		IdentitySource: principal.IdentitySource,
		Claims:         principal.Claims,
		CreatedAt:      now,
		ExpiresAt:      now.Add(ttl),
	}
	if err := s.sessions.CreateSession(ctx, session); err != nil {
		return nil, fmt.Errorf("persist agent session: %w", err)
	}

	accessToken, err := s.signToken(principal, jti, sessionID, now, ttl)
	if err != nil {
		return nil, fmt.Errorf("sign agent access token: %w", err)
	}

	return &SessionResponse{
		AccessToken: accessToken,
		TokenType:   "Bearer",
		ExpiresIn:   int(ttl.Seconds()),
		IssuedAt:    now,
	}, nil
}

// signToken constructs and signs an RS256 platform JWT.
func (s *Service) signToken(p *Principal, jti, sessionID string, now time.Time, ttl time.Duration) (string, error) {
	claims := PlatformClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        jti,
			Subject:   p.ID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
			Issuer:    "orbit",
		},
		Orbit: OrbitClaims{
			SessionID:      sessionID,
			PrincipalType:  p.Type,
			TenantID:       p.TenantID,
			IdentitySource: p.IdentitySource,
			DisplayName:    p.DisplayName,
			Claims:         p.Claims,
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = keyID(&s.signingKey.PublicKey)
	return token.SignedString(s.signingKey)
}

// parseToken parses and validates an RS256 platform JWT, returning its claims.
func (s *Service) parseToken(tokenString string) (*PlatformClaims, error) {
	var claims PlatformClaims
	_, err := jwt.ParseWithClaims(tokenString, &claims, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodRSA); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return &s.signingKey.PublicKey, nil
	})
	if err != nil {
		return nil, err
	}
	return &claims, nil
}

// requireOIDC returns ErrOIDCNotConfigured if no OIDC provider is configured.
func (s *Service) requireOIDC() error {
	if s.oidcProvider == nil {
		return ErrOIDCNotConfigured
	}
	return nil
}

// initOIDCProvider performs OIDC discovery and populates oidcProvider + oidcMeta.
func (s *Service) initOIDCProvider(ctx context.Context) error {
	provider, err := gooidc.NewProvider(ctx, s.cfg.OIDC.IssuerURL)
	if err != nil {
		return fmt.Errorf("oidc discovery: %w", err)
	}

	var meta oidcProviderMeta
	if err := provider.Claims(&meta); err != nil {
		return fmt.Errorf("extract provider metadata: %w", err)
	}
	if meta.TokenEndpoint == "" {
		// go-oidc exposes token_endpoint via Endpoint().TokenURL — use that as fallback.
		meta.TokenEndpoint = provider.Endpoint().TokenURL
	}
	if meta.DeviceAuthorizationEndpoint == "" {
		return fmt.Errorf("provider does not advertise device_authorization_endpoint")
	}

	s.oidcProvider = provider
	s.oidcMeta = &meta
	return nil
}

// initSigningKey loads the RSA private key from one of three sources, in order:
//
//  1. ORBIT_TOKEN_SIGNING_KEY_FILE — path to a PEM file on disk (recommended
//     for local dev: avoids multiline env-var problems entirely).
//  2. ORBIT_TOKEN_SIGNING_KEY — PEM value inline. Real newlines and literal \n
//     escape sequences are both accepted.
//  3. Auto-generate a throwaway 2048-bit key — permitted only when APP_ENV is
//     "local". Any other environment fails fast with a clear error; silently
//     accepting a missing signing key in production would be a security flaw.
func (s *Service) initSigningKey(ctx context.Context) error {
	// 1. File path takes priority — cleanest for local dev.
	if path := os.Getenv("ORBIT_TOKEN_SIGNING_KEY_FILE"); path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read ORBIT_TOKEN_SIGNING_KEY_FILE %q: %w", path, err)
		}
		key, err := parseRSAPrivateKey(string(data))
		if err != nil {
			return fmt.Errorf("parse ORBIT_TOKEN_SIGNING_KEY_FILE %q: %w", path, err)
		}
		s.signingKey = key
		s.logger.Info(ctx, "RSA signing key loaded from file", logging.Field{Key: "path", Value: path})
		return nil
	}

	// 2. Inline PEM value — used in CI / production secret injection.
	if pemData := os.Getenv("ORBIT_TOKEN_SIGNING_KEY"); pemData != "" {
		key, err := parseRSAPrivateKey(pemData)
		if err != nil {
			return fmt.Errorf("parse ORBIT_TOKEN_SIGNING_KEY: %w", err)
		}
		s.signingKey = key
		s.logger.Info(ctx, "RSA signing key loaded from environment")
		return nil
	}

	// 3. Auto-generate a throwaway key — strictly forbidden outside local dev.
	// In any non-local environment a missing signing key is a misconfiguration,
	// not something we should silently paper over with an ephemeral key.
	if env := config.Environment(); env != "local" {
		return fmt.Errorf(
			"no RSA signing key configured in environment %q: set ORBIT_TOKEN_SIGNING_KEY_FILE or ORBIT_TOKEN_SIGNING_KEY before starting the server",
			env,
		)
	}

	s.logger.Warn(ctx, "no signing key configured — generating ephemeral RSA key; tokens will not survive a restart (local dev only)")
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return fmt.Errorf("generate ephemeral rsa key: %w", err)
	}
	s.signingKey = key
	return nil
}

// parseRSAPrivateKey parses a PEM-encoded RSA private key (PKCS#1 or PKCS#8).
// It accepts both real newlines and the literal two-character sequence \n so
// that keys stored as single-line env-var values in .env files load correctly.
func parseRSAPrivateKey(pemData string) (*rsa.PrivateKey, error) {
	// Unescape literal \n sequences produced by tools like `awk '{printf "%s\\n"}'`.
	pemData = strings.ReplaceAll(pemData, `\n`, "\n")
	block, _ := pem.Decode([]byte(pemData))
	if block == nil {
		return nil, fmt.Errorf("no PEM block found")
	}

	switch block.Type {
	case "RSA PRIVATE KEY":
		return x509.ParsePKCS1PrivateKey(block.Bytes)
	case "PRIVATE KEY":
		key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return nil, err
		}
		rsaKey, ok := key.(*rsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("PKCS#8 key is not RSA")
		}
		return rsaKey, nil
	default:
		return nil, fmt.Errorf("unsupported PEM block type %q", block.Type)
	}
}

// ─── JWKS helpers ─────────────────────────────────────────────────────────────

// buildJWKS serializes an RSA public key as a JWK Set.
func buildJWKS(pub *rsa.PublicKey) ([]byte, error) {
	n := base64.RawURLEncoding.EncodeToString(pub.N.Bytes())
	e := base64.RawURLEncoding.EncodeToString(intToBytes(pub.E))

	jwk := map[string]any{
		"kty": "RSA",
		"use": "sig",
		"alg": "RS256",
		"kid": keyID(pub),
		"n":   n,
		"e":   e,
	}
	set := map[string]any{
		"keys": []any{jwk},
	}
	return json.Marshal(set)
}

// keyID derives a stable key identifier from the public key.
func keyID(pub *rsa.PublicKey) string {
	h := sha256.Sum256(pub.N.Bytes())
	return base64.RawURLEncoding.EncodeToString(h[:8])
}

// intToBytes converts a small integer (e.g. RSA exponent) to big-endian bytes.
func intToBytes(i int) []byte {
	b := make([]byte, 4)
	b[0] = byte(i >> 24)
	b[1] = byte(i >> 16)
	b[2] = byte(i >> 8)
	b[3] = byte(i)
	// Trim leading zero bytes.
	for len(b) > 1 && b[0] == 0 {
		b = b[1:]
	}
	return b
}

// hashToken returns the hex-encoded SHA-256 hash of a token string.
func hashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return fmt.Sprintf("%x", h)
}

// generateOpaqueToken produces a 32-byte cryptographically random token
// encoded as base64 URL (no padding). Used for refresh tokens.
func generateOpaqueToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("read random bytes: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// ─── Configuration loader ─────────────────────────────────────────────────────

// LoadConfig loads the identity service configuration from
// internal/identity/config/{environment}.yaml.
// Secrets (OIDC client credentials, token signing key) are never in YAML —
// they are injected at service construction time via environment variables.
func LoadConfig() (*Config, error) {
	cfg, err := config.Load[Config]("internal/identity", config.Environment())
	if err != nil {
		return nil, fmt.Errorf("load identity config: %w", err)
	}
	return cfg, nil
}
