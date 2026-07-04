package identity

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
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
)

// pkceS256 computes the RFC 7636 S256 code challenge for a verifier:
// BASE64URL(SHA256(verifier)), no padding.
func pkceS256(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// isLoopbackRedirect reports whether a redirect URI targets a loopback address —
// the only redirect targets allowed for public native MCP clients until DCR
// registers explicit URIs (RFC 8252). Prevents code exfiltration to a remote host.
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
	if !isLoopbackRedirect(p.RedirectURI) {
		return "", fmt.Errorf("%w: redirect_uri must be a loopback address", ErrInvalidOAuthRequest)
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
}

// TokenResponse is the OAuth token endpoint response.
type TokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
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

	principal := &Principal{
		ID:             data.Subject,
		Type:           token.PrincipalTypeHuman,
		TenantID:       data.TenantID,
		DisplayName:    data.DisplayName,
		IdentitySource: IdentitySourceFederated,
		Claims:         map[string]string{"mcp_client_id": data.ClientID},
	}
	_, accessToken, _, ttl, err := s.buildAndPersistSession(ctx, principal)
	if err != nil {
		return nil, fmt.Errorf("mint token: %w", err)
	}
	return &TokenResponse{AccessToken: accessToken, TokenType: "Bearer", ExpiresIn: int(ttl.Seconds())}, nil
}
