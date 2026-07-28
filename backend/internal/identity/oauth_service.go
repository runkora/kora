package identity

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"maps"
	"net/url"
	"time"

	"github.com/clear-route/orbit/internal/platform/token"
)

// OAuth 2.1 authorization-code + PKCE for the MCP profile. The BFF /authorize
// (after its session check) calls CreateAuthCode with the authenticated human;
// the MCP client back-channel exchanges the code at /token via ExchangeAuthCode,
// which mints an ordinary aud=orbit-platform HUMAN token.

const (
	// authCodeTTL bounds the window between /authorize and /token. Short by
	// design — a code is a bearer secret in transit.
	authCodeTTL = 90 * time.Second
	// authCodeBytes is the entropy of an opaque authorization code.
	authCodeBytes = 32
)

// ErrInvalidOAuthRequest is a malformed authorize/token request (RFC 6749
// invalid_request). ErrInvalidOAuthGrant is a bad code/PKCE/binding (invalid_grant).
var (
	ErrInvalidOAuthRequest = errors.New("invalid_request")
	ErrInvalidOAuthGrant   = errors.New("invalid_grant")
	// ErrDCRDisabled is returned by RegisterClient when open dynamic client
	// registration is turned off (RequirePreRegistration).
	ErrDCRDisabled = errors.New("registration_not_allowed")
	// ErrInvalidClientMetadata is a malformed RFC 7591 registration request.
	ErrInvalidClientMetadata = errors.New("invalid_client_metadata")
)

// pkceS256 computes the RFC 7636 S256 code challenge for a verifier:
// BASE64URL(SHA256(verifier)), no padding.
func pkceS256(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// isLoopbackRedirect reports whether a redirect URI targets a loopback address.
// Used by RegisterClient (DCR) to enforce the RFC 8252 loopback-only restriction
// for dynamically-registered clients. Static pre-registered clients bypass this
// check — their redirect URIs are admin-reviewed config entries.
func isLoopbackRedirect(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return false
	}
	switch u.Hostname() {
	case "127.0.0.1", "localhost", "::1":
		return true
	default:
		return false
	}
}

func randomToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate random token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// AuthorizeParams are the OAuth authorize request fields the BFF forwards to
// identity after its session check.
type AuthorizeParams struct {
	ClientID            string
	RedirectURI         string
	CodeChallenge       string
	CodeChallengeMethod string
	Scope               string
}

// CreateAuthCode validates an authorize request and stores a single-use code
// bound to the authenticated human, their PKCE challenge, client, and redirect
// URI. Returns the opaque code the BFF redirects back to the client.
func (s *Service) CreateAuthCode(ctx context.Context, human AuthCode, p AuthorizeParams) (string, error) {
	if p.CodeChallengeMethod != "S256" || p.CodeChallenge == "" {
		return "", fmt.Errorf("%w: PKCE S256 code_challenge is required", ErrInvalidOAuthRequest)
	}
	if p.ClientID == "" {
		return "", fmt.Errorf("%w: client_id is required", ErrInvalidOAuthRequest)
	}
	// The client_id must be registered (via DCR or pre-registration), and the
	// redirect_uri must be one it registered. AllowsRedirect (exact match) is the
	// security gate: it covers both loopback and non-loopback URIs for registered
	// clients. The loopback restriction is enforced at DCR time only, not here —
	// static pre-registered clients may have non-loopback redirect URIs.
	client, err := s.clients.Get(ctx, p.ClientID)
	if errors.Is(err, ErrClientNotFound) {
		return "", fmt.Errorf("%w: unknown client_id", ErrInvalidOAuthRequest)
	}
	if err != nil {
		return "", err
	}
	if !client.AllowsRedirect(p.RedirectURI) {
		return "", fmt.Errorf("%w: redirect_uri not registered for this client", ErrInvalidOAuthRequest)
	}
	code, err := randomToken(authCodeBytes)
	if err != nil {
		return "", err
	}
	human.ClientID = p.ClientID
	human.RedirectURI = p.RedirectURI
	human.CodeChallenge = p.CodeChallenge
	human.Scope = p.Scope
	if err := s.authCodes.Store(ctx, code, human, authCodeTTL); err != nil {
		return "", err
	}
	return code, nil
}

// TokenRequest is the token-exchange input (RFC 6749 authorization_code grant).
type TokenRequest struct {
	GrantType    string
	Code         string
	CodeVerifier string
	RedirectURI  string
	ClientID     string
	RefreshToken string // grant_type=refresh_token
}

// TokenResponse is the OAuth token endpoint response.
type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token,omitempty"`
	// RefreshTokenExpiresIn is the refresh token lifetime in seconds.
	RefreshTokenExpiresIn int `json:"refresh_token_expires_in,omitempty"`
}

// ExchangeAuthCode consumes a single-use code, verifies PKCE and the
// redirect_uri/client_id binding, and mints the human's ordinary
// aud=orbit-platform HUMAN token (JTI-revocable, same as any platform token).
func (s *Service) ExchangeAuthCode(ctx context.Context, req TokenRequest) (*TokenResponse, error) {
	if req.GrantType != "authorization_code" {
		return nil, fmt.Errorf("%w: unsupported grant_type", ErrInvalidOAuthRequest)
	}
	if req.Code == "" || req.CodeVerifier == "" {
		return nil, fmt.Errorf("%w: code and code_verifier are required", ErrInvalidOAuthRequest)
	}

	data, err := s.authCodes.Consume(ctx, req.Code)
	if errors.Is(err, ErrAuthCodeNotFound) {
		return nil, fmt.Errorf("%w: invalid or expired code", ErrInvalidOAuthGrant)
	}
	if err != nil {
		return nil, err
	}

	// PKCE: the presented verifier must hash to the challenge the code was bound to.
	if pkceS256(req.CodeVerifier) != data.CodeChallenge {
		return nil, fmt.Errorf("%w: PKCE verification failed", ErrInvalidOAuthGrant)
	}
	// The redirect_uri and client_id must match what the code was issued for.
	if req.RedirectURI != data.RedirectURI || req.ClientID != data.ClientID {
		return nil, fmt.Errorf("%w: redirect_uri/client_id mismatch", ErrInvalidOAuthGrant)
	}

	// Preserve the human's verified claims (roles etc.) so the token carries their
	// permissions, and record which MCP client it was minted for.
	claims := make(map[string]string, len(data.Claims)+1)
	maps.Copy(claims, data.Claims)
	claims["mcp_client_id"] = data.ClientID
	principal := &Principal{
		ID:             data.Subject,
		Type:           token.PrincipalTypeHuman,
		TenantID:       data.TenantID,
		DisplayName:    data.DisplayName,
		IdentitySource: IdentitySourceFederated,
		Claims:         claims,
	}
	// createSession (vs buildAndPersistSession) also mints a rotating refresh token,
	// so the MCP client can refresh silently instead of re-running the consent flow
	// when the short-lived access token expires.
	sess, err := s.createSession(ctx, principal)
	if err != nil {
		return nil, fmt.Errorf("mint token: %w", err)
	}
	return tokenResponseFromSession(sess), nil
}

// RefreshOAuthToken handles the OAuth `grant_type=refresh_token` request: it
// rotates the presented refresh token via the shared session-refresh path
// (validating + revoking the old token and preserving the human's claims,
// including their roles and the mcp_client_id) and returns a fresh token pair.
func (s *Service) RefreshOAuthToken(ctx context.Context, refreshToken string) (*TokenResponse, error) {
	if refreshToken == "" {
		return nil, fmt.Errorf("%w: refresh_token is required", ErrInvalidOAuthRequest)
	}
	sess, err := s.RefreshSession(ctx, SessionRequest{GrantType: "refresh_token", RefreshToken: refreshToken})
	switch {
	case errors.Is(err, ErrRefreshTokenNotFound),
		errors.Is(err, ErrRefreshTokenRevoked),
		errors.Is(err, ErrRefreshTokenExpired),
		errors.Is(err, ErrTokenRevoked),
		errors.Is(err, ErrSessionNotFound):
		// All map to RFC 6749 invalid_grant; the specific cause is on the
		// RefreshSession span (span.RecordError) for observability.
		return nil, fmt.Errorf("%w: refresh token invalid, expired, or revoked", ErrInvalidOAuthGrant)
	case err != nil:
		return nil, fmt.Errorf("refresh token: %w", err)
	}
	return tokenResponseFromSession(sess), nil
}

// tokenResponseFromSession maps an internal SessionResponse to the RFC 6749
// token-endpoint response shape.
func tokenResponseFromSession(sess *SessionResponse) *TokenResponse {
	return &TokenResponse{
		AccessToken:           sess.AccessToken,
		TokenType:             "Bearer",
		ExpiresIn:             sess.ExpiresIn,
		RefreshToken:          sess.RefreshToken,
		RefreshTokenExpiresIn: sess.RefreshTokenExpiresIn,
	}
}

// ── Dynamic Client Registration (RFC 7591) ────────────────────────────────────

// ClientRegistrationRequest is the RFC 7591 client metadata a public MCP client
// submits to register itself.
type ClientRegistrationRequest struct {
	RedirectURIs            []string `json:"redirect_uris"`
	ClientName              string   `json:"client_name,omitempty"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method,omitempty"`
	GrantTypes              []string `json:"grant_types,omitempty"`
	ResponseTypes           []string `json:"response_types,omitempty"`
}

// GetClient returns a registered client by ID (ErrClientNotFound if unknown).
// Used by the consent screen to show the human which application is asking.
func (s *Service) GetClient(ctx context.Context, clientID string) (RegisteredClient, error) {
	return s.clients.Get(ctx, clientID)
}

// RegisterClient validates a dynamic client registration and stores a new public
// client. Public clients only (no secret; PKCE is the proof) with loopback
// redirect URIs. Returns ErrDCRDisabled when open registration is turned off.
func (s *Service) RegisterClient(ctx context.Context, req ClientRegistrationRequest) (RegisteredClient, error) {
	if s.cfg.OAuth.RequirePreRegistration {
		return RegisteredClient{}, ErrDCRDisabled
	}
	if len(req.RedirectURIs) == 0 {
		return RegisteredClient{}, fmt.Errorf("%w: at least one redirect_uri is required", ErrInvalidClientMetadata)
	}
	for _, u := range req.RedirectURIs {
		if !isLoopbackRedirect(u) {
			return RegisteredClient{}, fmt.Errorf("%w: redirect_uris must be loopback addresses", ErrInvalidClientMetadata)
		}
	}
	// Only public clients — the flow relies on PKCE, not a client secret.
	if req.TokenEndpointAuthMethod != "" && req.TokenEndpointAuthMethod != "none" {
		return RegisteredClient{}, fmt.Errorf("%w: only public clients (token_endpoint_auth_method=none) are supported", ErrInvalidClientMetadata)
	}
	clientID, err := randomToken(24)
	if err != nil {
		return RegisteredClient{}, err
	}
	client := RegisteredClient{
		ClientID:     clientID,
		RedirectURIs: req.RedirectURIs,
		ClientName:   req.ClientName,
		CreatedAt:    time.Now().UTC(),
	}
	if err := s.clients.Store(ctx, client, registeredClientTTL); err != nil {
		return RegisteredClient{}, err
	}
	return client, nil
}

// initStaticOAuthClients seeds the config-declared static OAuth clients into the
// client repository. Static clients are re-seeded on every service startup and
// use staticClientTTL (effectively permanent) so they are always present.
// This is called from NewService after the signing key is initialised.
func (s *Service) initStaticOAuthClients(ctx context.Context) error {
	for _, sc := range s.cfg.OAuth.StaticClients {
		if sc.ClientID == "" {
			return fmt.Errorf("static oauth client has empty clientId")
		}
		if len(sc.RedirectURIs) == 0 {
			return fmt.Errorf("static oauth client %q has no redirectUris", sc.ClientID)
		}
		client := RegisteredClient{
			ClientID:     sc.ClientID,
			ClientName:   sc.ClientName,
			RedirectURIs: sc.RedirectURIs,
			CreatedAt:    time.Now().UTC(),
		}
		if err := s.clients.Store(ctx, client, staticClientTTL); err != nil {
			return fmt.Errorf("seed static oauth client %q: %w", sc.ClientID, err)
		}
	}
	return nil
}
