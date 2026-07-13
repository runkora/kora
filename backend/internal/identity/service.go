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
	"slices"
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

	"github.com/clear-route/orbit/internal/platform/api"
	clientpolicy "github.com/clear-route/orbit/internal/platform/clients/policy"
	"github.com/clear-route/orbit/internal/platform/logging"
	mw "github.com/clear-route/orbit/internal/platform/middleware"
	"github.com/clear-route/orbit/internal/platform/policy"
	"github.com/clear-route/orbit/internal/platform/token"
)

// ─── Internal OIDC provider state ────────────────────────────────────────────

// oidcProviderMeta holds metadata fetched from the OIDC discovery document
// that is not exposed by go-oidc's Provider type.
type oidcProviderMeta struct {
	AuthorizationEndpoint       string `json:"authorization_endpoint"`
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

// PolicyClient abstracts grant management calls to the policy service.
// Implemented by platform/clients/policy.Client in production.
type PolicyClient interface {
	CreateGrant(ctx context.Context, tenantID, principalType, principalID string, scopes []string, budget *clientpolicy.BudgetAssignment) (string, error)
	DeleteGrant(ctx context.Context, grantID string) error
}

// Service implements the identity business logic: federated OIDC authentication
// via the Device Authorization Grant and oidc_id_token browser flow,
// SPIFFE SVID exchange, JWT issuance, session management, and token revocation.
type Service struct {
	cfg           *Config
	sessions      SessionRepository
	refreshTokens RefreshTokenRepository
	blocklist     JTIBlocklistRepository
	authCodes     AuthCodeRepository
	clients       ClientRepository
	pats          PATRepository
	profiles      UserProfileRepository
	policyClient  PolicyClient
	policyEval    policy.Evaluator
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

	// clientSecrets holds the latest resolved OIDC client secrets, keyed by
	// tenant ID. Populated at startup by initClientSecrets and refreshed live
	// by watchClientSecrets whenever orbit-tenant-oidc-secrets changes.
	// Access only while holding mu (read or write as appropriate).
	clientSecrets map[string]string

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
	authCodes AuthCodeRepository,
	clients ClientRepository,
	pats PATRepository,
	profiles UserProfileRepository,
	policyClient PolicyClient,
	policyEval policy.Evaluator,
	extractor PrincipalExtractor,
	logger logging.Logger,
) (*Service, error) {
	if profiles == nil {
		profiles = noopUserProfileRepository{}
	}
	svc := &Service{
		cfg:           cfg,
		sessions:      sessions,
		refreshTokens: refreshTokens,
		blocklist:     blocklist,
		authCodes:     authCodes,
		clients:       clients,
		pats:          pats,
		profiles:      profiles,
		policyClient:  policyClient,
		policyEval:    policyEval,
		extractor:     extractor,
		logger:        logger,
		tracer:        otel.Tracer("orbit.identity"),
		httpClient:    &http.Client{Timeout: 10 * time.Second},
		oidcTenants:   make(map[string]*tenantOIDCState),
		clientSecrets: make(map[string]string),
	}

	// Load the aggregated client secrets before OIDC discovery so that
	// confidential tenants have their client_secret available on first startup.
	if err := svc.initClientSecrets(ctx); err != nil {
		return nil, fmt.Errorf("identity service: init client secrets: %w", err)
	}

	// Discover OIDC providers for all active tenants in the registry.
	if err := svc.initTenantOIDCProviders(ctx); err != nil {
		return nil, fmt.Errorf("identity service: init tenant oidc providers: %w", err)
	}

	// Start live-reload watchers. Both goroutines exit when ctx is canceled
	// (SIGTERM), so the caller must pass a context tied to the process lifetime.
	registryPath := os.Getenv("ORBIT_TENANTS_CONFIG")
	go svc.watchTenantRegistry(ctx, registryPath)

	secretsPath := clientSecretsPath()
	go svc.watchClientSecrets(ctx, secretsPath)

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

	if ts.meta.DeviceAuthorizationEndpoint == "" {
		return nil, ErrDeviceFlowNotSupported
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
		Sub               string   `json:"sub"`
		Email             string   `json:"email"`
		Name              string   `json:"name"`
		PreferredUsername string   `json:"preferred_username"`
		Roles             []string `json:"roles"`
		Groups            []string `json:"groups"`
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

	displayName := resolveDisplayName(claims.Name, claims.PreferredUsername, claims.Email, claims.Sub)

	principalClaims := map[string]string{
		"email":  claims.Email,
		"issuer": idToken.Issuer,
	}
	// OIDC providers use either "roles" (Auth0, Entra) or "groups" (Dex, Okta)
	// for role-like claims. Merge both into a single roles string.
	allRoles := slices.Concat(claims.Roles, claims.Groups)
	if len(allRoles) > 0 {
		principalClaims["roles"] = strings.Join(allRoles, ",")
	}

	principal := &Principal{
		ID:             claims.Sub,
		Type:           PrincipalTypeHuman,
		TenantID:       req.TenantID,
		DisplayName:    displayName,
		IdentitySource: IdentitySourceFederated,
		Claims:         principalClaims,
	}

	return s.createSession(ctx, principal)
}

// ─── Federated authentication: browser id_token flow ────────────────────────

// resolveDisplayName returns the first non-empty value from the ordered
// candidate fields: name → preferred_username → email → sub. The subject
// identifier is always non-empty and acts as the final fallback.
func resolveDisplayName(name, preferredUsername, email, sub string) string {
	for _, v := range []string{name, preferredUsername, email} {
		if v != "" {
			return v
		}
	}
	return sub
}

// ExchangeIDToken handles the "oidc_id_token" grant type. The browser OIDC
// client (oidc-client-ts) completes PKCE in the browser and hands the resulting
// id_token to the BFF, which forwards it here. The backend verifies the token
// cryptographically via JWKS (go-oidc), extracts standard claims, and issues an
// Orbit platform session token + refresh token.
//
// This is the preferred browser auth path: the OIDC token never leaves the
// browser-to-backend channel, no PKCE state is stored server-side, and the
// Orbit JWT is sealed into an HttpOnly iron-session cookie by the BFF.
func (s *Service) ExchangeIDToken(ctx context.Context, req SessionRequest) (*SessionResponse, error) {
	ctx, span := s.tracer.Start(ctx, "identity.ExchangeIDToken",
		trace.WithAttributes(
			attribute.String("grant_type", "oidc_id_token"),
			attribute.String("tenant_id", req.TenantID),
		),
	)
	defer span.End()

	tenant, err := s.tenantOIDC(req.TenantID)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, ErrOIDCNotConfigured.Error())
		return nil, fmt.Errorf("%w: tenant %q", ErrOIDCNotConfigured, req.TenantID)
	}

	// Verify the id_token signature and standard claims (iss, aud, exp) via JWKS.
	idToken, err := tenant.provider.Verifier(&gooidc.Config{
		ClientID: tenant.cfg.ClientID,
	}).Verify(ctx, req.IDToken)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "id_token verification failed")
		return nil, fmt.Errorf("%w: %w", ErrIDTokenInvalid, err)
	}

	var claims struct {
		Sub               string   `json:"sub"`
		Email             string   `json:"email"`
		Name              string   `json:"name"`
		PreferredUsername string   `json:"preferred_username"`
		Roles             []string `json:"roles"`
		Groups            []string `json:"groups"`
	}
	if err := idToken.Claims(&claims); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "claims extraction failed")
		return nil, fmt.Errorf("extract id_token claims: %w", err)
	}

	span.SetAttributes(
		attribute.String("principal.id", claims.Sub),
		attribute.String("principal.type", string(PrincipalTypeHuman)),
		attribute.String("identity.source", string(IdentitySourceFederated)),
	)

	displayName := resolveDisplayName(claims.Name, claims.PreferredUsername, claims.Email, claims.Sub)

	principalClaims := map[string]string{
		"email":  claims.Email,
		"issuer": idToken.Issuer,
	}
	// OIDC providers use either "roles" (Auth0, Entra) or "groups" (Dex, Okta)
	// for role-like claims. Merge both into a single roles string.
	allRoles := slices.Concat(claims.Roles, claims.Groups)
	if len(allRoles) > 0 {
		principalClaims["roles"] = strings.Join(allRoles, ",")
	}

	principal := &Principal{
		ID:             claims.Sub,
		Type:           PrincipalTypeHuman,
		TenantID:       req.TenantID,
		DisplayName:    displayName,
		IdentitySource: IdentitySourceFederated,
		Claims:         principalClaims,
	}

	span.SetStatus(codes.Ok, "id_token verified, creating session")
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
// forged by the calling workload (docs/adr/2026-03-05-spiffe-spire-workload-attestation.md, docs/adr/2026-03-05-universal-gateway-policy-enforcement.md, docs/adr/2026-03-05-service-mesh-and-network-enforcement-infrastructure.md).
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
	accessToken, err = s.signToken(principal, jti, sessionID, "", now, ttl)
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
// token + signed JWT. For HUMAN/FEDERATED principals it also upserts a
// user_profiles row — failures are non-fatal and logged.
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

	// Upsert user profile for federated human principals so that downstream
	// systems (policy dashboard, audit logs) can resolve user_id → display name.
	if principal.IdentitySource == IdentitySourceFederated {
		profile := userProfileFromPrincipal(principal)
		if upsertErr := s.profiles.Upsert(ctx, profile); upsertErr != nil {
			s.logger.Error(ctx, "failed to upsert user profile",
				logging.Field{Key: "user_id", Value: principal.ID},
				logging.Field{Key: "tenant_id", Value: principal.TenantID},
				logging.Field{Key: "error", Value: upsertErr.Error()},
			)
		}
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

	refreshTTL := time.Duration(s.cfg.Token.RefreshTokenTTLDays) * 24 * time.Hour
	return &SessionResponse{
		AccessToken:           accessToken,
		TokenType:             "Bearer",
		ExpiresIn:             int(ttl.Seconds()),
		RefreshToken:          rawRefresh,
		IssuedAt:              now.Unix(),
		RefreshTokenExpiresIn: int(refreshTTL.Seconds()),
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
// Exactly one of sessionID or patID should be non-empty depending on principal type.
func (s *Service) signToken(p *Principal, jti, sessionID, patID string, now time.Time, ttl time.Duration) (string, error) {
	claims := PlatformClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        jti,
			Subject:   p.ID,
			Audience:  jwt.ClaimStrings{s.cfg.Token.Audience},
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
			Issuer:    token.DefaultIssuer,
		},
		Orbit: OrbitClaims{
			SessionID:      sessionID,
			PrincipalType:  p.Type,
			TenantID:       p.TenantID,
			IdentitySource: p.IdentitySource,
			DisplayName:    p.DisplayName,
			Claims:         p.Claims,
			PatID:          patID,
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = keyID(&s.signingKey.PublicKey)
	return token.SignedString(s.signingKey)
}

// SystemToken mints a short-lived RS256 JWT for identity-service internal calls
// to other platform services (e.g. policy service grant management).
// The SYSTEM principal type allows cross-tenant operations in the policy service.
func (s *Service) SystemToken(ctx context.Context) (string, error) {
	principal := &Principal{
		ID:             "orbit-identity-service",
		Type:           token.PrincipalTypeSystem,
		TenantID:       "",
		IdentitySource: IdentitySourcePlatform,
	}
	return s.signToken(principal, uuid.NewString(), "", "", time.Now().UTC(), 5*time.Minute)
}

// MintServiceToken issues a short-lived RS256 platform JWT for a non-interactive
// SERVICE principal — e.g. a webhook trigger source. Unlike SystemToken, the
// principal is caller-specified and tenant-scoped: its access is bounded by the
// policy grants attached to principalID, so a trigger-initiated run is authorized
// and attributed exactly like any principal. See
// docs/adr/2026-06-17-agent-triggers-and-webhook-ingestion.md.
func (s *Service) MintServiceToken(ctx context.Context, tenantID, principalID, displayName string, ttl time.Duration) (string, error) {
	principal := &Principal{
		ID:             principalID,
		Type:           token.PrincipalTypeService,
		TenantID:       tenantID,
		DisplayName:    displayName,
		IdentitySource: IdentitySourcePlatform,
	}
	return s.signToken(principal, uuid.NewString(), "", "", time.Now().UTC(), ttl)
}

// GetUserProfile returns the profile for a single user, scoped to the caller's tenant.
func (s *Service) GetUserProfile(ctx context.Context, tenantID, userID string) (UserProfile, error) {
	profile, err := s.profiles.GetByID(ctx, tenantID, userID)
	if err != nil {
		return UserProfile{}, err
	}
	return profile, nil
}

// ListUserProfiles returns a paginated list of profiles for a tenant.
// When filter.IDs is non-empty, only those IDs are returned.
// Also returns the total count of matching records.
func (s *Service) ListUserProfiles(ctx context.Context, tenantID string, filter UserProfileFilter) ([]UserProfile, int, error) {
	profiles, total, err := s.profiles.List(ctx, tenantID, filter)
	if err != nil {
		return nil, 0, fmt.Errorf("list user profiles: %w", err)
	}
	return profiles, total, nil
}

// GetPATAttribution resolves a set of PAT UUIDs to their token names and owning user profiles.
// PAT IDs not found in the token table are omitted; owner profile is omitted if not yet recorded.
func (s *Service) GetPATAttribution(ctx context.Context, tenantID string, patIDs []string) (map[string]PATGrantAttribution, error) {
	if len(patIDs) == 0 {
		return map[string]PATGrantAttribution{}, nil
	}

	owners, err := s.pats.GetOwnersByIDs(ctx, tenantID, patIDs)
	if err != nil {
		return nil, fmt.Errorf("get PAT owners: %w", err)
	}
	if len(owners) == 0 {
		return map[string]PATGrantAttribution{}, nil
	}

	// Deduplicate user IDs before querying profiles.
	seen := make(map[string]struct{}, len(owners))
	var userIDs []string
	for _, info := range owners {
		if _, ok := seen[info.UserID]; !ok {
			seen[info.UserID] = struct{}{}
			userIDs = append(userIDs, info.UserID)
		}
	}

	profiles, _, err := s.profiles.List(ctx, tenantID, UserProfileFilter{IDs: userIDs, Page: 1, Limit: len(userIDs)})
	if err != nil {
		return nil, fmt.Errorf("get user profiles for attribution: %w", err)
	}

	byUserID := make(map[string]UserProfile, len(profiles))
	for _, p := range profiles {
		byUserID[p.ID] = p
	}

	result := make(map[string]PATGrantAttribution, len(owners))
	for patID, info := range owners {
		entry := PATGrantAttribution{PATName: info.PATName}
		if p, ok := byUserID[info.UserID]; ok {
			entry.Owner = &UserProfileResponse{
				ID:          p.ID,
				Email:       p.Email,
				DisplayName: p.DisplayName,
			}
		}
		result[patID] = entry
	}
	return result, nil
}

// AuthMiddleware returns a Middleware that verifies a platform JWT on the
// request's Authorization header and stores the verified claims in the context
// via token.WithClaims, identical to mw.AuthMiddleware but using the service's
// in-memory signing key instead of a remote JWKS endpoint.
func (s *Service) AuthMiddleware(logger logging.Logger) mw.Middleware {
	return mw.NewMiddleware(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()

			raw := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
			raw = strings.TrimSpace(raw)
			if raw == "" {
				api.ErrorResponse(ctx, w, logger,
					api.UnauthorizedError(api.ErrCodeUnauthorized, "Authorization header required", nil), nil)
				return
			}

			claims, err := s.parseToken(raw)
			if err != nil {
				logger.Error(ctx, "platform token verification failed",
					logging.Field{Key: "error", Value: err.Error()},
				)
				api.ErrorResponse(ctx, w, logger,
					api.UnauthorizedError(api.ErrCodeUnauthorized, "invalid or expired platform token", nil), nil)
				return
			}

			// Check the JTI blocklist so revoked tokens are rejected immediately
			// rather than remaining valid until natural expiry.
			// On Redis failure we fail open (log and allow) to avoid a cache
			// outage blocking all authentication.
			if blocked, blErr := s.blocklist.IsBlocked(ctx, claims.ID); blErr != nil {
				logger.Error(ctx, "JTI blocklist check failed — allowing request",
					logging.Field{Key: "jti", Value: claims.ID},
					logging.Field{Key: "error", Value: blErr.Error()},
				)
			} else if blocked {
				api.ErrorResponse(ctx, w, logger,
					api.UnauthorizedError(api.ErrCodeUnauthorized, "token has been revoked", nil), nil)
				return
			}

			next.ServeHTTP(w, r.WithContext(token.WithClaims(ctx, claims)))
		})
	})
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

// DiscoverTenantByEmail resolves the tenant for a given email address by matching
// the email's domain suffix against the EmailDomains list of each active tenant.
// Returns ErrTenantNotFound if no tenant claims the domain.
// The returned TenantDiscovery intentionally excludes the client secret.
func (s *Service) DiscoverTenantByEmail(ctx context.Context, email string) (*TenantDiscovery, error) {
	_, span := otel.Tracer("identity").Start(ctx, "identity.DiscoverTenantByEmail")
	defer span.End()

	at := strings.LastIndex(email, "@")
	if at < 0 {
		return nil, fmt.Errorf("%w: missing '@' sign", ErrInvalidEmailFormat)
	}
	domain := strings.ToLower(email[at+1:])
	if domain == "" {
		return nil, fmt.Errorf("%w: empty domain", ErrInvalidEmailFormat)
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	for tenantID, ts := range s.oidcTenants {
		for _, d := range ts.cfg.EmailDomains {
			if strings.ToLower(d) == domain {
				return &TenantDiscovery{
					TenantID:  tenantID,
					IssuerURL: ts.cfg.IssuerURL,
					ClientID:  ts.cfg.ClientID,
					Scopes:    ts.cfg.Scopes,
				}, nil
			}
		}
	}

	span.RecordError(ErrTenantNotFound)
	span.SetStatus(codes.Error, ErrTenantNotFound.Error())
	return nil, ErrTenantNotFound
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

	// go-oidc exposes token_endpoint and authorization_endpoint via Endpoint().
	// Use those as fallbacks for values not in the raw claims JSON.
	if meta.TokenEndpoint == "" {
		meta.TokenEndpoint = provider.Endpoint().TokenURL
	}
	if meta.AuthorizationEndpoint == "" {
		meta.AuthorizationEndpoint = provider.Endpoint().AuthURL
	}
	if meta.TokenEndpoint == "" {
		return nil, fmt.Errorf("oidc provider for tenant %q does not advertise token_endpoint", tenantID)
	}
	if meta.AuthorizationEndpoint == "" {
		return nil, fmt.Errorf("oidc provider for tenant %q does not advertise authorization_endpoint", tenantID)
	}
	if meta.DeviceAuthorizationEndpoint == "" {
		// Not all IdPs support the Device Authorization Grant (RFC 8628).
		// Web-only providers (e.g. Okta orgs with device flow disabled) omit
		// this endpoint — log a warning but allow startup to proceed.
		// InitiateDeviceAuth will return ErrDeviceFlowNotSupported at call time.
		s.logger.Warn(ctx, "OIDC provider does not advertise device_authorization_endpoint — Device Flow disabled for tenant",
			logging.Field{Key: "tenant_id", Value: tenantID},
		)
	}

	return &tenantOIDCState{cfg: cfg, provider: provider, meta: &meta}, nil
}

// initTenantOIDCProviders loads the tenant registry and performs OIDC discovery
// for every active tenant that has an issuer URL configured. Tenants without
// an issuer URL are skipped with an info log (federated auth disabled for that
// tenant). A discovery failure for any tenant is fatal once retries
// (discoverTenantOIDCWithRetry) are exhausted — misconfigured providers should
// still be caught at startup, not at first login, but a transient blip must
// not crash the service on its very first attempt.
func (s *Service) initTenantOIDCProviders(ctx context.Context) error {
	tenants, err := loadTenantRegistry(s.clientSecrets)
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

		state, err := s.discoverTenantOIDCWithRetry(ctx, tenantID, cfg)
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

// discoverTenantOIDCWithRetry retries discoverTenantOIDC with exponential
// backoff, bounded by cfg.OIDCDiscovery.ReadyTimeout — the same posture
// identity-proxy's waitForInitialToken already uses for its own startup
// dependency (initial token exchange, cmd/identity-proxy). Confirmed live: a
// dev-environment vcluster's CoreDNS can still be rolling out a custom
// split-horizon zone (see the operator's inVClusterCoreDNSRolloutComplete
// gate) for a few seconds after this service starts; a single failed attempt
// must not permanently crash-loop the whole binary.
func (s *Service) discoverTenantOIDCWithRetry(ctx context.Context, tenantID string, cfg TenantOIDCConfig) (*tenantOIDCState, error) {
	deadline := time.Now().Add(s.cfg.OIDCDiscovery.ReadyTimeout())
	backoff := s.cfg.OIDCDiscovery.BackoffInitial()

	for {
		state, err := s.discoverTenantOIDC(ctx, tenantID, cfg)
		if err == nil {
			return state, nil
		}

		if time.Now().After(deadline) {
			return nil, fmt.Errorf("timed out after %s retrying oidc discovery for tenant %q: %w",
				s.cfg.OIDCDiscovery.ReadyTimeout(), tenantID, err)
		}

		s.logger.Warn(ctx, "oidc discovery failed, retrying",
			logging.Field{Key: "tenant_id", Value: tenantID},
			logging.Field{Key: "error", Value: err.Error()},
			logging.Field{Key: "backoff_ms", Value: backoff.Milliseconds()},
		)

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(backoff):
		}

		backoff *= 2
		if backoff > s.cfg.OIDCDiscovery.BackoffMax() {
			backoff = s.cfg.OIDCDiscovery.BackoffMax()
		}
	}
}

// reloadTenantOIDCProviders re-reads the tenant registry file and performs OIDC
// discovery for any tenant whose issuer URL has changed or that is newly active.
// Existing providers whose issuer URL is unchanged are reused — this avoids
// unnecessary outbound discovery calls on every file-change event.
//
// The updated map is swapped in atomically under a write lock so in-flight
// requests using the old map are not interrupted.
func (s *Service) reloadTenantOIDCProviders(ctx context.Context) {
	// Snapshot both maps under a single read lock to keep them consistent.
	s.mu.RLock()
	current := s.oidcTenants
	clientSecrets := s.clientSecrets
	s.mu.RUnlock()

	tenants, err := loadTenantRegistry(clientSecrets)
	if err != nil {
		s.logger.Error(ctx, "tenant registry reload failed — keeping current config",
			logging.Field{Key: "error", Value: err.Error()},
		)
		return
	}

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
//
//  1. ORBIT_TOKEN_SIGNING_KEY_FILE — path to a PEM file on disk (recommended
//     for local dev: avoids multiline env-var problems entirely).
//  2. ORBIT_TOKEN_SIGNING_KEY — PEM value inline. Real newlines and literal \n
//     escape sequences are both accepted.
//  3. Auto-generate a throwaway 2048-bit key — permitted only when
//     ORBIT_ALLOW_EPHEMERAL_KEY=true. Any other case fails fast with a clear
//     error; silently accepting a missing signing key would be a security flaw.
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

	// 3. Auto-generate a throwaway key — only when ORBIT_ALLOW_EPHEMERAL_KEY=true.
	// A missing signing key is always a misconfiguration in production; silently
	// papering over it with an ephemeral key would be a security flaw. Tests and
	// local dev without a key file set ORBIT_ALLOW_EPHEMERAL_KEY=true explicitly.
	if os.Getenv("ORBIT_ALLOW_EPHEMERAL_KEY") != "true" {
		return fmt.Errorf(
			"no RSA signing key configured: set ORBIT_TOKEN_SIGNING_KEY_FILE or ORBIT_TOKEN_SIGNING_KEY before starting the server (or ORBIT_ALLOW_EPHEMERAL_KEY=true for local dev only)",
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
