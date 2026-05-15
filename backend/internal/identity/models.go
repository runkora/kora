package identity

import (
	"errors"
	"fmt"
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
	PrincipalTypePAT     = token.PrincipalTypePAT
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
// Supports three grant types:
//   - "urn:ietf:params:oauth:grant-type:device_code" — device flow poll
//   - "refresh_token"                                — refresh an existing human session
//   - "oidc_id_token"                                — browser presents a verified id_token; backend validates and issues an Orbit session
type SessionRequest struct {
	GrantType    string `json:"grant_type"`
	TenantID     string `json:"tenant_id"`
	DeviceCode   string `json:"device_code,omitempty"`
	RefreshToken string `json:"refresh_token,omitempty"`
	// IDToken is used for the oidc_id_token grant type.
	// The browser OIDC client (oidc-client-ts) performs PKCE in the browser and
	// presents the resulting id_token here. The backend verifies it via JWKS and
	// issues an Orbit platform session token.
	IDToken string `json:"id_token,omitempty"`
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
	// RefreshTokenExpiresIn is the lifetime of the refresh token in seconds.
	// The BFF uses this to align the iron-session cookie maxAge with the actual
	// token lifetime, so the cookie never outlives the refresh token.
	// Omitted for agent sessions (no refresh token issued).
	RefreshTokenExpiresIn int `json:"refresh_token_expires_in,omitempty"`
}

// TenantDiscovery is the response body for GET /identity/tenants/discover.
// It contains the minimum OIDC configuration the browser needs to construct a
// UserManager and initiate PKCE — deliberately excludes the client secret.
// See docs/adr/0031-home-realm-discovery-and-email-based-tenant-routing.md
type TenantDiscovery struct {
	TenantID  string   `json:"tenant_id"`
	IssuerURL string   `json:"issuer_url"`
	ClientID  string   `json:"client_id"`
	Scopes    []string `json:"scopes"`
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

// PatTokenPrefix is prepended to every issued PAT JWT for visual identification.
// Security-significant: auth middleware strips this prefix before JWT validation.
const PatTokenPrefix = "orb_"

// ─── PAT types ───────────────────────────────────────────────────────────────

// PAT is a Personal Access Token record stored in user_pat_tokens.
// The raw JWT is never stored — only its SHA-256 hash.
type PAT struct {
	ID        string     `db:"id"`
	TenantID  string     `db:"tenant_id"`
	UserID    string     `db:"user_id"`
	Name      string     `db:"name"`
	JTI       string     `db:"jti"`
	Scopes    []string   `db:"scopes"`
	GrantID   *string    `db:"grant_id"`
	ExpiresAt time.Time  `db:"expires_at"`
	CreatedAt time.Time  `db:"created_at"`
	RevokedAt *time.Time `db:"revoked_at"`
}

// CreatePATRequest is the HTTP request body for POST /identity/tokens.
type CreatePATRequest struct {
	Name          string   `json:"name"`
	ExpiresInDays int      `json:"expires_in_days,omitempty"`
	Scopes        []string `json:"scopes"`
}

// CreatePATResponse is returned on successful PAT creation.
// Token is the raw JWT prefixed with "orb_" — shown exactly once.
type CreatePATResponse struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Token     string   `json:"token"`
	Scopes    []string `json:"scopes"`
	ExpiresAt string   `json:"expires_at"`
	CreatedAt string   `json:"created_at"`
}

// UpdatePATScopesRequest is the HTTP request body for PATCH /identity/tokens/{id}/scopes.
type UpdatePATScopesRequest struct {
	Scopes []string `json:"scopes"`
}

// PATSummary is a single entry in the list response — raw token is omitted.
type PATSummary struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Scopes    []string `json:"scopes"`
	ExpiresAt string   `json:"expires_at"`
	CreatedAt string   `json:"created_at"`
	RevokedAt *string  `json:"revoked_at"`
}

// ListPATsResponse is returned by GET /identity/tokens.
type ListPATsResponse struct {
	Tokens []PATSummary `json:"tokens"`
}

// ─── PAT attribution types ────────────────────────────────────────────────────

// PATOwnerInfo carries the user_id and token name for a single PAT, used
// internally when resolving attribution without a round-trip through profiles.
type PATOwnerInfo struct {
	UserID  string
	PATName string
}

// PATGrantAttribution is a single entry in PATAttributionResponse, bundling
// the token's human-readable name with its owning user's profile.
type PATGrantAttribution struct {
	PATName string               `json:"pat_name"`
	Owner   *UserProfileResponse `json:"owner,omitempty"`
}

// PATAttributionResponse is returned by GET /identity/tokens/attribution.
// The Attribution map key is the PAT UUID.
type PATAttributionResponse struct {
	Attribution map[string]PATGrantAttribution `json:"attribution"`
}

// ─── User profile types ───────────────────────────────────────────────────────

// UserProfile is a persistent, upserted record of a human principal's identity
// attributes. Populated from verified OIDC claims on every successful login.
type UserProfile struct {
	ID          string    `db:"id"`
	TenantID    string    `db:"tenant_id"`
	Email       string    `db:"email"`
	DisplayName string    `db:"display_name"`
	CreatedAt   time.Time `db:"created_at"`
	UpdatedAt   time.Time `db:"updated_at"`
}

// UserProfileResponse is a single user entry in list and detail responses.
type UserProfileResponse struct {
	ID          string `json:"id"`
	Email       string `json:"email"`
	DisplayName string `json:"display_name"`
}

// UserProfilesResponse is returned by GET /identity/users.
type UserProfilesResponse struct {
	Users []UserProfileResponse `json:"users"`
	Total int                   `json:"total"`
	Page  int                   `json:"page"`
	Limit int                   `json:"limit"`
}

// UserProfileFilter is the query model for listing user profiles.
type UserProfileFilter struct {
	IDs    []string // optional; when non-empty, only these IDs are returned
	Search string   // optional; when non-empty, filters by email/display_name ILIKE
	Page   int      // 1-based; defaults to 1
	Limit  int      // defaults to 20, max 100
}

// ─── Sentinel errors ─────────────────────────────────────────────────────────

var (
	ErrSessionNotFound        = errors.New("session not found")
	ErrRefreshTokenNotFound   = errors.New("refresh token not found")
	ErrRefreshTokenExpired    = errors.New("refresh token expired")
	ErrRefreshTokenRevoked    = errors.New("refresh token revoked")
	ErrTokenRevoked           = errors.New("token has been revoked")
	ErrInvalidToken           = errors.New("invalid token")
	ErrOIDCNotConfigured      = errors.New("federated authentication not configured")
	ErrAuthorizationPending   = errors.New("authorization_pending")
	ErrSlowDown               = errors.New("slow_down")
	ErrAccessDenied           = errors.New("access_denied")
	ErrDeviceCodeExpired      = errors.New("device code expired or not found")
	ErrUnsupportedTokenType   = errors.New("unsupported token type")
	ErrIDTokenInvalid         = errors.New("id_token failed verification")
	ErrTenantNotFound         = errors.New("tenant not found for email domain")
	ErrDeviceFlowNotSupported = errors.New("tenant OIDC provider does not support Device Authorization Grant")
	ErrInvalidEmailFormat     = errors.New("invalid email format")
	ErrPATNotFound            = errors.New("PAT not found")
	ErrPATSigningFailed       = errors.New("PAT signing failed")
	ErrPATPersistFailed       = errors.New("PAT persistence failed")
	ErrPATListFailed          = errors.New("PAT list failed")
	ErrPATRevokeFailed        = errors.New("PAT revocation failed")
	ErrPATScopesRequired      = errors.New("at least one scope is required")
	ErrPATUpdateFailed        = errors.New("PAT scope update failed")
	ErrUserProfileNotFound    = errors.New("user profile not found")
)

// ErrPATScopesForbiddenError is returned when the requested scopes exceed the
// issuing user's current effective scopes.
type ErrPATScopesForbiddenError struct {
	Scopes []string
}

func (e *ErrPATScopesForbiddenError) Error() string {
	return fmt.Sprintf("scopes exceed your current access: %v", e.Scopes)
}
