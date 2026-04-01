package identity

import (
	"errors"
	"time"

	"github.com/clear-route/orbit/internal/platform/token"
)

// ─── Principal ───────────────────────────────────────────────────────────────

// PrincipalType identifies the category of actor.
// Defined in internal/platform/token and aliased here for ergonomic use within
// the identity package without requiring a package qualifier.
type PrincipalType = token.PrincipalType

const (
	PrincipalTypeHuman   = token.PrincipalTypeHuman
	PrincipalTypeAgent   = token.PrincipalTypeAgent
	PrincipalTypeService = token.PrincipalTypeService
	PrincipalTypeSystem  = token.PrincipalTypeSystem
)

// IdentitySource records how the principal's identity was established.
type IdentitySource = token.IdentitySource

const (
	// IdentitySourceFederated — OIDC/SAML via an external provider (Entra, Okta, …).
	IdentitySourceFederated = token.IdentitySourceFederated
	// IdentitySourceAttested — SPIFFE/SPIRE workload attestation.
	IdentitySourceAttested = token.IdentitySourceAttested
	// IdentitySourcePlatform — internally issued (SYSTEM principals).
	IdentitySourcePlatform = token.IdentitySourcePlatform
)

// Principal is the unified identity abstraction for all actors in Orbit.
// Aliased from internal/platform/token — see that package for field documentation.
type Principal = token.Principal

// ─── JWT token claims ────────────────────────────────────────────────────────

// PlatformClaims are the JWT claims carried in every platform session token.
// Aliased from internal/platform/token.
type PlatformClaims = token.PlatformClaims

// OrbitClaims are the Orbit-specific claims embedded in the platform token's
// "orbit" namespace. Aliased from internal/platform/token.
type OrbitClaims = token.OrbitClaims

// ─── Storage records ─────────────────────────────────────────────────────────

// Session is an active platform session stored in PostgreSQL.
// The session record holds enough principal context to reconstruct the
// Principal on refresh without a round-trip to the upstream IdP.
type Session struct {
	ID             string            `db:"id"`
	JTI            string            `db:"jti"`
	PrincipalID    string            `db:"principal_id"`
	PrincipalType  PrincipalType     `db:"principal_type"`
	TenantID       string            `db:"tenant_id"`
	DisplayName    string            `db:"display_name"`
	IdentitySource IdentitySource    `db:"identity_source"`
	Claims         map[string]string `db:"claims"` // stored as JSONB
	CreatedAt      time.Time         `db:"created_at"`
	ExpiresAt      time.Time         `db:"expires_at"`
	RevokedAt      *time.Time        `db:"revoked_at"`
}

// RefreshToken is a server-side refresh token record.
// Only the SHA-256 hash of the token is persisted — the raw value is returned
// to the client exactly once and never stored.
type RefreshToken struct {
	ID        string     `db:"id"`
	SessionID string     `db:"session_id"` // foreign key → sessions.id
	TokenHash string     `db:"token_hash"`
	CreatedAt time.Time  `db:"created_at"`
	ExpiresAt time.Time  `db:"expires_at"`
	RevokedAt *time.Time `db:"revoked_at"`
}

// ─── Request / response types ────────────────────────────────────────────────

// DeviceAuthRequest is the body for POST /identity/device.
type DeviceAuthRequest struct {
	TenantID string `json:"tenant_id"`
}

// DeviceAuthResponse is returned to the client when a device flow is initiated.
// The client displays UserCode at VerificationURI and then polls
// POST /identity/session with grant_type=device_code.
type DeviceAuthResponse struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"`
}

// SessionRequest is the body for POST /identity/session.
// Supports two grant types:
//   - "urn:ietf:params:oauth:grant-type:device_code" — device flow poll
//   - "refresh_token" — refresh an existing human session
type SessionRequest struct {
	GrantType    string `json:"grant_type"`
	TenantID     string `json:"tenant_id"`
	DeviceCode   string `json:"device_code,omitempty"`
	RefreshToken string `json:"refresh_token,omitempty"`
}

// SessionResponse is returned on successful session creation.
// IssuedAt is expressed as Unix seconds (integer) per RFC 6749 / RFC 7519
// convention — not an RFC 3339 string.
type SessionResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token,omitempty"`
	IssuedAt     int64  `json:"issued_at"` // Unix seconds
}

// RevokeRequest is the parsed form body for POST /identity/revoke (RFC 7009 §2.1).
// The HTTP body must be application/x-www-form-urlencoded.
// Field names match RFC 7009: "token" and "token_type_hint".
type RevokeRequest struct {
	Token     string // form field: token
	TokenType string // form field: token_type_hint — "access_token" or "refresh_token"
}

// ─── OAuthError ──────────────────────────────────────────────────────────────

// OAuthError carries a raw OAuth2 error code returned by a provider that does
// not map to one of the package-level sentinel errors. Callers can type-assert
// to inspect the code, or simply treat it as an opaque error.
type OAuthError struct {
	Code string
}

func (e *OAuthError) Error() string {
	return "oauth2 error: " + e.Code
}

// ─── Sentinel errors ─────────────────────────────────────────────────────────

var (
	ErrSessionNotFound      = errors.New("session not found")
	ErrRefreshTokenNotFound = errors.New("refresh token not found")
	ErrRefreshTokenExpired  = errors.New("refresh token expired")
	ErrRefreshTokenRevoked  = errors.New("refresh token revoked")
	ErrTokenRevoked         = errors.New("token has been revoked")
	ErrInvalidToken         = errors.New("invalid token")
	ErrOIDCNotConfigured    = errors.New("federated authentication not configured")
	ErrAuthorizationPending = errors.New("authorization_pending")
	ErrSlowDown             = errors.New("slow_down")
	ErrAccessDenied         = errors.New("access_denied")
	ErrDeviceCodeExpired    = errors.New("device code expired or not found")
	ErrUnsupportedTokenType = errors.New("unsupported token type")
)
