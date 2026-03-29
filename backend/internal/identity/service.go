package identity

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	gooidc "github.com/coreos/go-oidc/v3/oidc"
	"github.com/fsnotify/fsnotify"
	jwt "github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/clear-route/orbit/internal/platform/config"
	"github.com/clear-route/orbit/internal/platform/logging"
)

// ─── Internal OIDC provider state ────────────────────────────────────────────

// oidcProviderMeta holds metadata fetched from the OIDC discovery document
// that is not exposed by go-oidc's Provider type.
type oidcProviderMeta struct {
	DeviceAuthorizationEndpoint string `json:"device_authorization_endpoint"`
	TokenEndpoint               string `json:"token_endpoint"`
}

// tenantOIDCState bundles the resolved go-oidc provider and its discovery
// metadata for a single tenant.
type tenantOIDCState struct {
	cfg      TenantOIDCConfig
	provider *gooidc.Provider
	meta     *oidcProviderMeta
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
	tracer        trace.Tracer

	// httpClient is used for all outbound OIDC requests. It is set to a
	// sensible default in NewService and can be replaced in tests.
	httpClient *http.Client

	// mu protects oidcTenants — readers acquire RLock, the file-watcher
	// goroutine acquires Lock when it swaps in a freshly loaded map.
	mu sync.RWMutex

	// oidcTenants holds one resolved OIDC state per active tenant, keyed by
	// tenant ID.  Populated at startup by initTenantOIDCProviders and refreshed
	// live by watchTenantRegistry whenever the tenant registry file changes.
	// Access only while holding mu (read or write as appropriate).
	oidcTenants map[string]*tenantOIDCState

	// extractor derives a Principal from the inbound request (reads the
	// Envoy-injected XFCC header in production; a stub header locally).
	extractor PrincipalExtractor

	// signingKey is the RSA private key used to sign platform tokens (RS256).
	signingKey *rsa.PrivateKey

	// jwksJSON is the pre-serialized JWKS JSON for GET /.well-known/jwks.json.
	jwksJSON []byte
}

// NewService constructs and initializes the identity Service.
//
// cfg must be the value returned by LoadConfig — env-var overrides and secret
// fields (ClientID, ClientSecret) are resolved there.
//
// OIDC provider discovery is performed eagerly at construction time for
// every active tenant found in the tenant registry (ORBIT_TENANTS_CONFIG).
// Tenants without a configured OIDC issuer URL are skipped.
//
// The RSA signing key is read from the ORBIT_TOKEN_SIGNING_KEY_FILE or
// ORBIT_TOKEN_SIGNING_KEY environment variables (via initSigningKey).
// In local development, if neither is set, a throwaway key is generated
// and a prominent warning is logged. Never use auto-generated keys in production.
func NewService(
	ctx context.Context,
	cfg *Config,
	sessions SessionRepository,
	refreshTokens RefreshTokenRepository,
	blocklist JTIBlocklistRepository,
	extractor PrincipalExtractor,
	logger logging.Logger,
) (*Service, error) {
	svc := &Service{
		cfg:           cfg,
		sessions:      sessions,
		refreshTokens: refreshTokens,
		blocklist:     blocklist,
		extractor:     extractor,
		logger:        logger,
		tracer:        otel.Tracer("orbit.identity"),
		httpClient:    &http.Client{Timeout: 10 * time.Second},
		oidcTenants:   make(map[string]*tenantOIDCState),
	}

	// Discover OIDC providers for all active tenants in the registry.
	if err := svc.initTenantOIDCProviders(ctx); err != nil {
		return nil, fmt.Errorf("identity service: init tenant oidc providers: %w", err)
	}

	// Start the live-reload watcher.  The goroutine exits when ctx is
	// canceled (SIGTERM), so the caller must pass a context that is
	// tied to the process lifetime.
	registryPath := os.Getenv("ORBIT_TENANTS_CONFIG")
	go svc.watchTenantRegistry(ctx, registryPath)

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
// OIDC provider configured for req.TenantID and returns the device code +
// user instructions.
func (s *Service) InitiateDeviceAuth(ctx context.Context, req DeviceAuthRequest) (*DeviceAuthResponse, error) {
	ts, err := s.tenantOIDC(req.TenantID)
	if err != nil {
		return nil, err
	}

	scopes := ts.cfg.Scopes
	if len(scopes) == 0 {
		scopes = []string{"openid", "profile", "email"}
	}

	formVals := url.Values{
		"client_id": {ts.cfg.ClientID},
		"scope":     {strings.Join(scopes, " ")},
	}

	resp, err := s.postForm(ctx, ts.meta.DeviceAuthorizationEndpoint, formVals)
	if err != nil {
		return nil, fmt.Errorf("device auth request to %s: %w", ts.meta.DeviceAuthorizationEndpoint, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read device auth response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		oauthErr := parseDeviceTokenError(body)
		if oauthErr != nil {
			// If it's an OAuthError, include the error code
			var oe *OAuthError
			if errors.As(oauthErr, &oe) {
				return nil, fmt.Errorf("device auth endpoint %s returned %d (%s): %w",
					ts.meta.DeviceAuthorizationEndpoint, resp.StatusCode, oe.Code, oauthErr)
			}
			// Sentinel error (ErrAuthorizationPending, etc.)
			return nil, fmt.Errorf("device auth endpoint %s returned %d: %w",
				ts.meta.DeviceAuthorizationEndpoint, resp.StatusCode, oauthErr)
		}
		return nil, fmt.Errorf("device auth endpoint %s returned %d: %s",
			ts.meta.DeviceAuthorizationEndpoint, resp.StatusCode, body)
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
// package-level sentinel errors. For recognized OAuth2 device-flow codes
// (RFC 8628 §3.5) it returns a typed sentinel; for any other non-empty error
// code it returns an *OAuthError so callers can still inspect the raw code.
// Returns nil only when the body cannot be parsed as an OAuth2 error object.
func parseDeviceTokenError(body []byte) error {
	var oe struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(body, &oe) != nil || oe.Error == "" {
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
	default:
		return &OAuthError{Code: oe.Error}
	}
}

// PollDeviceAuth performs a single, non-blocking poll of the OIDC token
// endpoint for a device_code grant. It maps provider error codes to sentinel
// errors so the caller can decide whether to retry or surface the failure.
func (s *Service) PollDeviceAuth(ctx context.Context, req SessionRequest) (*SessionResponse, error) {
	ctx, span := s.tracer.Start(ctx, "identity.PollDeviceAuth",
		trace.WithAttributes(
			attribute.String("grant_type", "urn:ietf:params:oauth:grant-type:device_code"),
			attribute.String("tenant_id", req.TenantID),
		),
	)
	defer span.End()

	ts, err := s.tenantOIDC(req.TenantID)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "OIDC not configured for tenant")
		return nil, err
	}

	formVals := url.Values{
		"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
		"device_code": {req.DeviceCode},
		"client_id":   {ts.cfg.ClientID},
	}
	if ts.cfg.ClientSecret != "" {
		formVals.Set("client_secret", ts.cfg.ClientSecret)
	}

	resp, err := s.postForm(ctx, ts.meta.TokenEndpoint, formVals)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "token endpoint request failed")
		return nil, fmt.Errorf("poll device auth token endpoint %s: %w", ts.meta.TokenEndpoint, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read token response body: %w", err)
	}

	// OAuth2 error responses use 4xx status codes.
	if resp.StatusCode != http.StatusOK {
		span.SetAttributes(attribute.Int("http.status_code", resp.StatusCode))
		if sentinelErr := parseDeviceTokenError(body); sentinelErr != nil {
			recordDeviceAuthError(span, sentinelErr)
			return nil, sentinelErr
		}
		unexpectedErr := fmt.Errorf("token endpoint returned %d: %s", resp.StatusCode, body)
		span.RecordError(unexpectedErr)
		span.SetStatus(codes.Error, "unexpected status code")
		return nil, unexpectedErr
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
	idToken, err := ts.provider.Verifier(&gooidc.Config{
		ClientID: ts.cfg.ClientID,
	}).Verify(ctx, tokenResp.IDToken)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "id_token verification failed")
		return nil, fmt.Errorf("verify id token: %w", err)
	}

	var claims struct {
		Sub               string `json:"sub"`
		Email             string `json:"email"`
		Name              string `json:"name"`
		PreferredUsername string `json:"preferred_username"`
	}
	if err := idToken.Claims(&claims); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "claims extraction failed")
		return nil, fmt.Errorf("extract id token claims: %w", err)
	}

	span.SetAttributes(
		attribute.String("principal.id", claims.Sub),
		attribute.String("principal.type", string(PrincipalTypeHuman)),
		attribute.String("identity.source", string(IdentitySourceFederated)),
	)

	displayName := claims.Name
	if displayName == "" {
		displayName = claims.PreferredUsername
	}
	if displayName == "" {
		displayName = claims.Email
	}
	if displayName == "" {
		displayName = claims.Sub
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
	ctx, span := s.tracer.Start(ctx, "identity.RefreshSession",
		trace.WithAttributes(
			attribute.String("grant_type", "refresh_token"),
		),
	)
	defer span.End()

	if req.RefreshToken == "" {
		err := fmt.Errorf("refresh_token is required")
		span.RecordError(err)
		span.SetStatus(codes.Error, "missing refresh_token")
		return nil, err
	}

	hash := hashToken(req.RefreshToken)

	rt, err := s.refreshTokens.GetRefreshTokenByHash(ctx, hash)
	if err != nil {
		if errors.Is(err, ErrRefreshTokenNotFound) {
			span.RecordError(err)
			span.SetStatus(codes.Error, "refresh_token not found")
			return nil, ErrRefreshTokenNotFound
		}
		span.RecordError(err)
		span.SetStatus(codes.Error, "database error")
		return nil, fmt.Errorf("get refresh token: %w", err)
	}

	if rt.RevokedAt != nil {
		span.RecordError(ErrRefreshTokenRevoked)
		span.SetStatus(codes.Error, "refresh_token revoked")
		return nil, ErrRefreshTokenRevoked
	}
	if time.Now().After(rt.ExpiresAt) {
		span.RecordError(ErrRefreshTokenExpired)
		span.SetStatus(codes.Error, "refresh_token expired")
		return nil, ErrRefreshTokenExpired
	}

	session, err := s.sessions.GetSessionByID(ctx, rt.SessionID)
	if err != nil {
		if errors.Is(err, ErrSessionNotFound) {
			span.RecordError(err)
			span.SetStatus(codes.Error, "session not found")
			return nil, ErrSessionNotFound
		}
		span.RecordError(err)
		span.SetStatus(codes.Error, "database error")
		return nil, fmt.Errorf("get session: %w", err)
	}
	if session.RevokedAt != nil {
		span.RecordError(ErrTokenRevoked)
		span.SetStatus(codes.Error, "session revoked")
		return nil, ErrTokenRevoked
	}

	span.SetAttributes(
		attribute.String("session.id", session.ID),
		attribute.String("principal.id", session.PrincipalID),
		attribute.String("principal.type", string(session.PrincipalType)),
		attribute.String("tenant_id", session.TenantID),
		attribute.String("identity.source", string(session.IdentitySource)),
	)

	// Revoke the used refresh token (rotation).
	if err := s.refreshTokens.RevokeRefreshToken(ctx, rt.ID, time.Now()); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "refresh token rotation failed")
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

// ─── SPIFFE exchange ──────────────────────────────────────────────────────────

// ExchangeSVID extracts the caller's SPIFFE identity from the Envoy-injected
// x-forwarded-client-cert (XFCC) header and issues a platform session token for
// the attested agent. Agent sessions have no refresh token — their identity
// lifecycle is managed by SPIRE workload attestation.
//
// The XFCC header is written by Envoy after mTLS termination and cannot be
// forged by the calling workload (ADR-0010, ADR-0011, ADR-0015).
func (s *Service) ExchangeSVID(ctx context.Context, r *http.Request) (*SessionResponse, error) {
	ctx, span := s.tracer.Start(ctx, "identity.ExchangeSVID")
	defer span.End()

	principal, err := s.extractor.Extract(r)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "principal extraction failed")
		return nil, fmt.Errorf("extract principal: %w", err)
	}

	span.SetAttributes(
		attribute.String("principal.id", principal.ID),
		attribute.String("principal.type", string(principal.Type)),
		attribute.String("tenant_id", principal.TenantID),
		attribute.String("identity.source", string(principal.IdentitySource)),
	)

	// tenant_id is extracted exclusively from the SPIFFE URI in the XFCC header —
	// the value attested by SPIRE and injected by Envoy after mTLS termination.
	// Agent-supplied request body values cannot override the attested identity.
	return s.createAgentSession(ctx, principal)
}

// ─── Revocation ───────────────────────────────────────────────────────────────

// RevokeToken revokes an access token or refresh token.
//
//   - "access_token": the JTI is added to the Redis blocklist; the backing
//     session and all its refresh tokens are also revoked in PostgreSQL.
//   - "refresh_token": only the specific refresh token record is revoked.
func (s *Service) RevokeToken(ctx context.Context, req RevokeRequest) error {
	ctx, span := s.tracer.Start(ctx, "identity.RevokeToken",
		trace.WithAttributes(
			attribute.String("token_type", req.TokenType),
		),
	)
	defer span.End()

	switch req.TokenType {
	case "access_token":
		err := s.revokeAccessToken(ctx, req.Token)
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, "access_token revocation failed")
		} else {
			span.SetStatus(codes.Ok, "access_token revoked")
		}
		return err
	case "refresh_token":
		err := s.revokeRefreshToken(ctx, req.Token)
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, "refresh_token revocation failed")
		} else {
			span.SetStatus(codes.Ok, "refresh_token revoked")
		}
		return err
	default:
		err := fmt.Errorf("unsupported token_type %q: %w", req.TokenType, ErrUnsupportedTokenType)
		span.RecordError(err)
		span.SetStatus(codes.Error, "unsupported token_type")
		return err
	}
}

func (s *Service) revokeAccessToken(ctx context.Context, token string) error {
	ctx, span := s.tracer.Start(ctx, "identity.revokeAccessToken")
	defer span.End()

	claims, err := s.parseToken(token)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "invalid token")
		return ErrInvalidToken
	}

	jti := claims.ID
	if jti == "" {
		span.RecordError(ErrInvalidToken)
		span.SetStatus(codes.Error, "missing jti")
		return ErrInvalidToken
	}

	span.SetAttributes(attribute.String("jti", jti))

	expiry, err := claims.GetExpirationTime()
	if err == nil && expiry != nil {
		ttl := time.Until(expiry.Time)
		if ttl > 0 {
			if blockErr := s.blocklist.BlockJTI(ctx, jti, ttl); blockErr != nil {
				span.RecordError(blockErr)
				span.SetStatus(codes.Error, "blocklist operation failed")
				return fmt.Errorf("block jti: %w", blockErr)
			}
			span.SetAttributes(attribute.Int64("jti.ttl_seconds", int64(ttl.Seconds())))
		}
	}

	// Revoke the backing session and all associated refresh tokens.
	session, err := s.sessions.GetSessionByJTI(ctx, jti)
	if err != nil {
		if errors.Is(err, ErrSessionNotFound) {
			// Session already gone — blocklist entry is sufficient.
			span.SetStatus(codes.Ok, "jti blocked (session already revoked)")
			return nil
		}
		span.RecordError(err)
		span.SetStatus(codes.Error, "database error")
		return fmt.Errorf("get session by jti: %w", err)
	}

	span.SetAttributes(attribute.String("session.id", session.ID))

	now := time.Now()
	if rErr := s.refreshTokens.RevokeAllForSession(ctx, session.ID, now); rErr != nil {
		span.RecordError(rErr)
		span.SetStatus(codes.Error, "refresh token revocation failed")
		return fmt.Errorf("revoke refresh tokens for session: %w", rErr)
	}
	if rErr := s.sessions.RevokeSession(ctx, session.ID, now); rErr != nil {
		if !errors.Is(rErr, ErrSessionNotFound) {
			span.RecordError(rErr)
			span.SetStatus(codes.Error, "session revocation failed")
			return fmt.Errorf("revoke session: %w", rErr)
		}
	}

	span.SetStatus(codes.Ok, "access_token revoked")
	return nil
}

func (s *Service) revokeRefreshToken(ctx context.Context, token string) error {
	ctx, span := s.tracer.Start(ctx, "identity.revokeRefreshToken")
	defer span.End()

	hash := hashToken(token)
	rt, err := s.refreshTokens.GetRefreshTokenByHash(ctx, hash)
	if err != nil {
		if errors.Is(err, ErrRefreshTokenNotFound) {
			// Token not found means it was never issued or already revoked.
			// RFC 7009 §2.1 requires idempotent revocation — treat as success.
			span.SetStatus(codes.Ok, "refresh_token not found (already revoked or never issued)")
			return nil
		}
		span.RecordError(err)
		span.SetStatus(codes.Error, "database error")
		return fmt.Errorf("get refresh token for revocation: %w", err)
	}

	span.SetAttributes(attribute.String("refresh_token.id", rt.ID))

	if err := s.refreshTokens.RevokeRefreshToken(ctx, rt.ID, time.Now()); err != nil {
		if errors.Is(err, ErrRefreshTokenNotFound) {
			// Concurrent revocation — already gone, still a success per RFC 7009.
			span.SetStatus(codes.Ok, "refresh_token concurrently revoked")
			return nil
		}
		span.RecordError(err)
		span.SetStatus(codes.Error, "revocation failed")
		return err
	}

	span.SetStatus(codes.Ok, "refresh_token revoked")
	return nil
}

// ─── JWKS ─────────────────────────────────────────────────────────────────────

// JWKSJSON returns the pre-serialized JWKS JSON for the public key endpoint.
func (s *Service) JWKSJSON() []byte {
	return s.jwksJSON
}

// ─── Internal helpers ─────────────────────────────────────────────────────────

// buildAndPersistSession generates IDs, computes TTL, persists the Session
// record to PostgreSQL, and signs the platform JWT. It is shared by
// createSession (human, with refresh token) and createAgentSession (no refresh
// token). Returns the session ID, signed access token, issue time, and TTL.
func (s *Service) buildAndPersistSession(ctx context.Context, principal *Principal) (sessionID, accessToken string, now time.Time, ttl time.Duration, err error) {
	ctx, span := s.tracer.Start(ctx, "identity.buildAndPersistSession",
		trace.WithAttributes(
			attribute.String("principal.id", principal.ID),
			attribute.String("principal.type", string(principal.Type)),
			attribute.String("tenant_id", principal.TenantID),
			attribute.String("identity.source", string(principal.IdentitySource)),
		),
	)
	defer span.End()

	sessionID = uuid.NewString()
	jti := uuid.NewString()
	now = time.Now().UTC()

	ttl = time.Duration(s.cfg.Token.AccessTokenTTLMinutes) * time.Minute

	span.SetAttributes(
		attribute.String("session.id", sessionID),
		attribute.String("jti", jti),
		attribute.Int64("ttl_minutes", int64(s.cfg.Token.AccessTokenTTLMinutes)),
	)

	// Sign before persisting — if signing fails nothing is written to the DB.
	accessToken, err = s.signToken(principal, jti, sessionID, now, ttl)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "token signing failed")
		err = fmt.Errorf("sign access token: %w", err)
		return
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
	if err = s.sessions.CreateSession(ctx, session); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "session persistence failed")
		err = fmt.Errorf("persist session: %w", err)
		return
	}

	span.SetStatus(codes.Ok, "session created")
	return
}

// createSession creates a full human session: PostgreSQL session + refresh
// token + signed JWT.
func (s *Service) createSession(ctx context.Context, principal *Principal) (*SessionResponse, error) {
	ctx, span := s.tracer.Start(ctx, "identity.createSession",
		trace.WithAttributes(
			attribute.String("principal.type", string(principal.Type)),
		),
	)
	defer span.End()

	sessionID, accessToken, now, ttl, err := s.buildAndPersistSession(ctx, principal)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "session creation failed")
		return nil, err
	}

	// Issue refresh token (opaque random bytes; only the hash is stored).
	rawRefresh, err := generateOpaqueToken()
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "refresh token generation failed")
		return nil, fmt.Errorf("generate refresh token: %w", err)
	}

	rt := &RefreshToken{
		ID:        uuid.NewString(),
		SessionID: sessionID,
		TokenHash: hashToken(rawRefresh),
		CreatedAt: now,
		ExpiresAt: now.Add(time.Duration(s.cfg.Token.RefreshTokenTTLDays) * 24 * time.Hour),
	}
	if err := s.refreshTokens.CreateRefreshToken(ctx, rt); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "refresh token persistence failed")
		return nil, fmt.Errorf("persist refresh token: %w", err)
	}

	span.SetAttributes(
		attribute.String("refresh_token.id", rt.ID),
		attribute.Int64("refresh_token.ttl_days", int64(s.cfg.Token.RefreshTokenTTLDays)),
	)
	span.SetStatus(codes.Ok, "session with refresh token created")

	return &SessionResponse{
		AccessToken:  accessToken,
		TokenType:    "Bearer",
		ExpiresIn:    int(ttl.Seconds()),
		RefreshToken: rawRefresh,
		IssuedAt:     now.Unix(),
	}, nil
}

// createAgentSession creates an agent session with no refresh token.
// Agent identity lifecycle is managed entirely by SPIRE workload attestation.
func (s *Service) createAgentSession(ctx context.Context, principal *Principal) (*SessionResponse, error) {
	ctx, span := s.tracer.Start(ctx, "identity.createAgentSession",
		trace.WithAttributes(
			attribute.String("principal.type", string(principal.Type)),
		),
	)
	defer span.End()

	_, accessToken, now, ttl, err := s.buildAndPersistSession(ctx, principal)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "agent session creation failed")
		return nil, err
	}

	span.SetStatus(codes.Ok, "agent session created (no refresh token)")

	return &SessionResponse{
		AccessToken: accessToken,
		TokenType:   "Bearer",
		ExpiresIn:   int(ttl.Seconds()),
		IssuedAt:    now.Unix(),
	}, nil
}

// signToken constructs and signs an RS256 platform JWT.
func (s *Service) signToken(p *Principal, jti, sessionID string, now time.Time, ttl time.Duration) (string, error) {
	claims := PlatformClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        jti,
			Subject:   p.ID,
			Audience:  jwt.ClaimStrings{s.cfg.Token.Audience},
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
	}, jwt.WithAudience(s.cfg.Token.Audience))
	if err != nil {
		return nil, err
	}
	return &claims, nil
}

// tenantOIDC returns the resolved OIDC state for a given tenant ID.
// Returns ErrOIDCNotConfigured if the tenant is unknown or has no OIDC provider.
func (s *Service) tenantOIDC(tenantID string) (*tenantOIDCState, error) {
	s.mu.RLock()
	ts, ok := s.oidcTenants[tenantID]
	s.mu.RUnlock()
	if ok {
		return ts, nil
	}
	return nil, ErrOIDCNotConfigured
}

// recordDeviceAuthError records span error details for device authorization flow errors.
// ErrAuthorizationPending is intentionally not recorded as an error - it's expected polling behavior.
func recordDeviceAuthError(span trace.Span, err error) {
	if errors.Is(err, ErrAuthorizationPending) {
		return
	}
	span.RecordError(err)
	switch {
	case errors.Is(err, ErrSlowDown):
		span.SetStatus(codes.Error, "slow_down")
	case errors.Is(err, ErrAccessDenied):
		span.SetStatus(codes.Error, "access_denied")
	case errors.Is(err, ErrDeviceCodeExpired):
		span.SetStatus(codes.Error, "expired_token")
	default:
		var oe *OAuthError
		if errors.As(err, &oe) {
			span.SetAttributes(attribute.String("oauth.error", oe.Code))
			span.SetStatus(codes.Error, oe.Code)
		}
	}
}

// postForm executes an application/x-www-form-urlencoded POST request using the
// service's HTTP client with the provided context, so that caller deadlines and
// cancellations are propagated to the upstream OIDC provider.
func (s *Service) postForm(ctx context.Context, endpoint string, vals url.Values) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(vals.Encode()))
	if err != nil {
		return nil, fmt.Errorf("build request for %s: %w", endpoint, err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return s.httpClient.Do(req)
}

// discoverTenantOIDC performs OIDC provider discovery for a single tenant
// and returns the resolved state. Returns an error if discovery or endpoint
// validation fails.
func (s *Service) discoverTenantOIDC(ctx context.Context, tenantID string, cfg TenantOIDCConfig) (*tenantOIDCState, error) {
	ctxWithClient := gooidc.ClientContext(ctx, s.httpClient)
	provider, err := gooidc.NewProvider(ctxWithClient, cfg.IssuerURL)
	if err != nil {
		return nil, fmt.Errorf("oidc discovery for tenant %q: %w", tenantID, err)
	}

	var meta oidcProviderMeta
	if err := provider.Claims(&meta); err != nil {
		return nil, fmt.Errorf("extract provider metadata for tenant %q: %w", tenantID, err)
	}

	// go-oidc exposes token_endpoint via Endpoint().TokenURL — use as fallback.
	if meta.TokenEndpoint == "" {
		meta.TokenEndpoint = provider.Endpoint().TokenURL
	}
	if meta.TokenEndpoint == "" {
		return nil, fmt.Errorf("oidc provider for tenant %q does not advertise token_endpoint", tenantID)
	}
	if meta.DeviceAuthorizationEndpoint == "" {
		return nil, fmt.Errorf("oidc provider for tenant %q does not advertise device_authorization_endpoint", tenantID)
	}

	return &tenantOIDCState{cfg: cfg, provider: provider, meta: &meta}, nil
}

// initTenantOIDCProviders loads the tenant registry and performs OIDC discovery
// for every active tenant that has an issuer URL configured. Tenants without
// an issuer URL are skipped with an info log (federated auth disabled for that
// tenant). A discovery failure for any tenant is fatal — misconfigured
// providers should be caught at startup, not at first login.
func (s *Service) initTenantOIDCProviders(ctx context.Context) error {
	tenants, err := loadTenantRegistry()
	if err != nil {
		return fmt.Errorf("load tenant registry: %w", err)
	}
	if len(tenants) == 0 {
		s.logger.Info(ctx, "no tenants in registry — federated authentication disabled")
		return nil
	}

	for tenantID, cfg := range tenants {
		if cfg.IssuerURL == "" {
			s.logger.Info(ctx, "tenant has no OIDC issuer URL — skipping",
				logging.Field{Key: "tenant_id", Value: tenantID},
			)
			continue
		}

		state, err := s.discoverTenantOIDC(ctx, tenantID, cfg)
		if err != nil {
			return err
		}
		s.oidcTenants[tenantID] = state
		s.logger.Info(ctx, "OIDC provider initialized",
			logging.Field{Key: "tenant_id", Value: tenantID},
			logging.Field{Key: "issuer", Value: cfg.IssuerURL},
		)
	}
	return nil
}

// reloadTenantOIDCProviders re-reads the tenant registry file and performs OIDC
// discovery for any tenant whose issuer URL has changed or that is newly active.
// Existing providers whose issuer URL is unchanged are reused — this avoids
// unnecessary outbound discovery calls on every file-change event.
//
// The updated map is swapped in atomically under a write lock so in-flight
// requests using the old map are not interrupted.
func (s *Service) reloadTenantOIDCProviders(ctx context.Context) {
	tenants, err := loadTenantRegistry()
	if err != nil {
		s.logger.Error(ctx, "tenant registry reload failed — keeping current config",
			logging.Field{Key: "error", Value: err.Error()},
		)
		return
	}

	// Snapshot the current map once before the loop — one lock acquisition
	// rather than one per iteration.
	s.mu.RLock()
	current := s.oidcTenants
	s.mu.RUnlock()

	// Build the new map, reusing existing providers where the issuer URL is
	// unchanged. Always take the freshly resolved cfg so that rotated client
	// secrets are picked up without a full OIDC discovery round-trip.
	next := make(map[string]*tenantOIDCState, len(tenants))
	for tenantID, cfg := range tenants {
		if cfg.IssuerURL == "" {
			continue
		}

		if existing, had := current[tenantID]; had && existing.cfg.IssuerURL == cfg.IssuerURL {
			next[tenantID] = &tenantOIDCState{
				cfg:      cfg,
				provider: existing.provider,
				meta:     existing.meta,
			}
			continue
		}

		state, err := s.discoverTenantOIDC(ctx, tenantID, cfg)
		if err != nil {
			s.logger.Error(ctx, "OIDC discovery failed during reload — skipping tenant",
				logging.Field{Key: "tenant_id", Value: tenantID},
				logging.Field{Key: "error", Value: err.Error()},
			)
			continue
		}
		next[tenantID] = state
		s.logger.Info(ctx, "OIDC provider reloaded",
			logging.Field{Key: "tenant_id", Value: tenantID},
			logging.Field{Key: "issuer", Value: cfg.IssuerURL},
		)
	}

	s.mu.Lock()
	s.oidcTenants = next
	s.mu.Unlock()

	s.logger.Info(ctx, "tenant registry reloaded",
		logging.Field{Key: "tenant_count", Value: len(next)},
	)
}

// watchTenantRegistry watches the directory containing the tenant registry
// file for changes and triggers a live reload whenever the file is updated.
//
// Kubernetes writes ConfigMap-backed volume files via an atomic symlink swap
// (the kubelet replaces the ..data symlink), which means fsnotify fires a
// RENAME or REMOVE event rather than a plain WRITE.  Watching the *directory*
// and filtering by filename handles this correctly across both local and
// in-cluster environments.
//
// A 200 ms debounce timer coalesces rapid event bursts (e.g. RENAME + CREATE)
// into a single reload call.
//
// The goroutine exits cleanly when ctx is canceled (SIGTERM).
func (s *Service) watchTenantRegistry(ctx context.Context, registryPath string) {
	if registryPath == "" {
		return
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		s.logger.Error(ctx, "failed to create fsnotify watcher — hot-reload disabled",
			logging.Field{Key: "error", Value: err.Error()},
		)
		return
	}
	defer func() {
		if err := watcher.Close(); err != nil {
			s.logger.Error(ctx, "failed to close fsnotify watcher",
				logging.Field{Key: "error", Value: err.Error()},
			)
		}
	}()

	dir := filepath.Dir(registryPath)
	base := filepath.Base(registryPath)

	if addErr := watcher.Add(dir); addErr != nil {
		s.logger.Error(ctx, "failed to watch tenant registry directory — hot-reload disabled",
			logging.Field{Key: "path", Value: dir},
			logging.Field{Key: "error", Value: addErr.Error()},
		)
		return
	}

	s.logger.Info(ctx, "watching tenant registry for changes",
		logging.Field{Key: "path", Value: registryPath},
	)

	var debounce *time.Timer
	const debounceDelay = 200 * time.Millisecond

	for {
		select {
		case <-ctx.Done():
			if debounce != nil {
				debounce.Stop()
			}
			return

		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			if !isRelevantRegistryEvent(event, base) {
				continue
			}
			if debounce != nil {
				debounce.Stop()
			}
			debounce = time.AfterFunc(debounceDelay, func() {
				s.reloadTenantOIDCProviders(ctx)
			})

		case watchErr, ok := <-watcher.Errors:
			if !ok {
				return
			}
			s.logger.Error(ctx, "tenant registry watcher error",
				logging.Field{Key: "error", Value: watchErr.Error()},
			)
		}
	}
}

// isRelevantRegistryEvent reports whether the fsnotify event should trigger a
// tenant registry reload. It matches only events on the expected filename and
// only for write, create, rename, or remove operations — covering both normal
// file writes and the atomic symlink swaps that Kubernetes uses for ConfigMap
// volume updates.
func isRelevantRegistryEvent(event fsnotify.Event, base string) bool {
	name := filepath.Base(event.Name)
	// Match either the target file directly (plain file writes, local dev)
	// or the "..data" symlink that Kubernetes rotates atomically when a
	// ConfigMap-backed volume is updated.  The symlink swap emits a RENAME
	// event on "..data", not on the individual files inside it.
	if name != base && name != "..data" {
		return false
	}
	return event.Has(fsnotify.Write) ||
		event.Has(fsnotify.Create) ||
		event.Has(fsnotify.Rename) ||
		event.Has(fsnotify.Remove)
}

// initSigningKey loads the RSA private key from one of three sources, in order:

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
	binary.BigEndian.PutUint32(b, uint32(i)) //nolint:gosec // pub.E is always a small positive int (e.g. 65537)
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
