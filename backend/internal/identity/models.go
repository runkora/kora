package identity

import (
	"errors"
	"time"

	jwt "github.com/golang-jwt/jwt/v5"
)

// ─── Principal ───────────────────────────────────────────────────────────────

// PrincipalType identifies the category of actor.
type PrincipalType string

const (
	PrincipalTypeHuman   PrincipalType = "HUMAN"
	PrincipalTypeAgent   PrincipalType = "AGENT"
	PrincipalTypeService PrincipalType = "SERVICE"
	PrincipalTypeSystem  PrincipalType = "SYSTEM"
)

// IdentitySource records how the principal's identity was established.
type IdentitySource string

const (
	// IdentitySourceFederated — OIDC/SAML via an external provider (Entra, Okta, …).
	IdentitySourceFederated IdentitySource = "FEDERATED"
	// IdentitySourceAttested — SPIFFE/SPIRE workload attestation.
	IdentitySourceAttested IdentitySource = "ATTESTED"
	// IdentitySourcePlatform — internally issued (SYSTEM principals).
	IdentitySourcePlatform IdentitySource = "PLATFORM"
)

// Principal is the unified identity abstraction for all actors in Orbit.
// It is the subject of every policy decision, audit event, and delegation chain.
// Downstream components only ever see the Principal carried inside a platform
// session token — they never receive raw OIDC tokens or SVIDs.
type Principal struct {
	// ID is a globally unique, stable identifier.
	// For HUMAN principals this is derived from the upstream IdP subject claim.
	// For AGENT principals this is Orbit's own agent registry ID.
	ID string `json:"id"`

	// Type discriminates the category of actor.
	Type PrincipalType `json:"type"`

	// TenantID scopes this principal to a specific tenant.
	TenantID string `json:"tenant_id"`

	// DisplayName is a human-readable label for audit and UI.
	DisplayName string `json:"display_name"`

	// IdentitySource records the authentication mechanism used.
	IdentitySource IdentitySource `json:"identity_source"`

	// Claims carries source-specific verified attributes.
	Claims map[string]string `json:"claims"`
}

// ─── JWT token claims ────────────────────────────────────────────────────────

// PlatformClaims are the JWT claims carried in every platform session token.
type PlatformClaims struct {
	jwt.RegisteredClaims
	Orbit OrbitClaims `json:"orbit"`
}

// OrbitClaims are the Orbit-specific claims embedded in the platform token's
// "orbit" namespace so they do not conflict with registered claim names.
type OrbitClaims struct {
	SessionID      string            `json:"session_id"`
	PrincipalType  PrincipalType     `json:"principal_type"`
	TenantID       string            `json:"tenant_id"`
	IdentitySource IdentitySource    `json:"identity_source"`
	DisplayName    string            `json:"display_name"`
	Claims         map[string]string `json:"claims,omitempty"`
}

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

// ExchangeRequest is the body for POST /identity/exchange (SPIFFE → platform token).
// The caller's SPIFFE identity is extracted from the Envoy-injected
// x-forwarded-client-cert (XFCC) header — agents never supply SVID material
// in the request body (ADR-0010, ADR-0011).
type ExchangeRequest struct {
	TenantID string `json:"tenant_id"`
}

// RevokeRequest is the body for DELETE /identity/revoke.
type RevokeRequest struct {
	Token     string `json:"token"`
	TokenType string `json:"token_type"` // "access_token" or "refresh_token"
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
)
