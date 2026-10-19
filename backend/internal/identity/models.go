package identity

import (
	"errors"
	"fmt"
	"time"

	"github.com/runkora/kora/backend/internal/platform/token"
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

// Principal is the unified identity abstraction for all actors in kora.
// Aliased from internal/platform/token — see that package for field documentation.
type Principal = token.Principal

// ─── JWT token claims ────────────────────────────────────────────────────────

// PlatformClaims are the JWT claims carried in every platform session token.
// Aliased from internal/platform/token.
type PlatformClaims = token.PlatformClaims

// KoraClaims are the kora-specific claims embedded in the platform token's
// "orbit" namespace. Aliased from internal/platform/token.
type KoraClaims = token.KoraClaims

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
//   - "oidc_id_token"                                — browser presents a verified id_token; backend validates and issues a kora session
type SessionRequest struct {
	GrantType    string `json:"grant_type"`
	TenantID     string `json:"tenant_id"`
	DeviceCode   string `json:"device_code,omitempty"`
	RefreshToken string `json:"refresh_token,omitempty"`
	// IDToken is used for the oidc_id_token grant type.
	// The browser OIDC client (oidc-client-ts) performs PKCE in the browser and
	// presents the resulting id_token here. The backend verifies it via JWKS and
	// issues a kora platform session token.
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
// See docs/adr/2026-04-01-home-realm-discovery-and-email-based-tenant-routing.md
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
	ID          string `db:"id"`
	TenantID    string `db:"tenant_id"`
	Email       string `db:"email"`
	DisplayName string `db:"display_name"`
	// Roles is the principal's IdP-asserted role membership (merged OIDC
	// roles + groups), snapshotted on every login. It is the single source of
	// truth for role membership — credentials (PATs, agent tokens) never carry
	// roles, so role-budget attribution resolves them from here.
	Roles     []string  `db:"roles"`
	CreatedAt time.Time `db:"created_at"`
	UpdatedAt time.Time `db:"updated_at"`
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

// ─── Delegation types ─────────────────────────────────────────────────────────

// DelegationIssuance is a delegation event record stored in delegation_issuances
// (docs/adr/2026-07-23-delegated-agent-identity.md, Decision 3). The signed
// delegation JWT is never persisted — retrieval re-signs a fresh token from
// this record, keyed by the stable JTI.
type DelegationIssuance struct {
	JTI                    string        `db:"jti"`
	RunID                  string        `db:"run_id"`
	TenantID               string        `db:"tenant_id"`
	DelegatorPrincipalID   string        `db:"delegator_principal_id"`
	DelegatorPrincipalType PrincipalType `db:"delegator_principal_type"`
	// DelegatorPATID carries the PAT UUID when the delegation was minted with a
	// PAT platform token — the lineage that lets PAT revocation reach issuances.
	DelegatorPATID   *string    `db:"delegator_pat_id"`
	AgentPrincipalID string     `db:"agent_principal_id"`
	CreatedAt        time.Time  `db:"created_at"`
	RevokedAt        *time.Time `db:"revoked_at"`
}

// CreateDelegationRequest is the body for POST /identity/delegation.
// Tenant and delegator identity come exclusively from verified platform token
// claims — never from unverified body fields. When SubjectToken is present it
// is fully verified (signature, expiry, JTI blocklist) and the delegator is
// derived from IT; otherwise the delegator is the transport principal.
type CreateDelegationRequest struct {
	// AgentPrincipalID is the SPIFFE URI of the agent being delegated to (act.sub).
	AgentPrincipalID string `json:"agent_principal_id"`
	// RunID binds the delegation to a single run.
	RunID string `json:"run_id"`
	// SubjectToken is the RFC 8693 subject_token: the DELEGATOR's own platform
	// token, carried in the body by a platform service brokering the mint.
	// The Authorization header authenticates the brokering service itself, so
	// the runner's proof of identity travels here, as RFC 8693 token exchange models it.
	SubjectToken string `json:"subject_token,omitempty"`
}

// DelegationIssuanceResponse is the wire form of a DelegationIssuance.
// It never carries a token: the only consumers of the signed delegation JWT
// are identity-proxies retrieving it under SPIFFE attestation.
type DelegationIssuanceResponse struct {
	JTI                    string  `json:"jti"`
	RunID                  string  `json:"run_id"`
	TenantID               string  `json:"tenant_id"`
	DelegatorPrincipalID   string  `json:"delegator_principal_id"`
	DelegatorPrincipalType string  `json:"delegator_principal_type"`
	AgentPrincipalID       string  `json:"agent_principal_id"`
	CreatedAt              string  `json:"created_at"`
	RevokedAt              *string `json:"revoked_at,omitempty"`
}

// ListDelegationsResponse is returned by GET /identity/delegation.
type ListDelegationsResponse struct {
	Delegations []DelegationIssuanceResponse `json:"delegations"`
}

// DelegationTokenResponse is returned by GET /identity/delegation/run/{run_id}
// to a mesh-attested caller: a freshly re-signed delegation JWT for the run.
type DelegationTokenResponse struct {
	DelegationToken string `json:"delegation_token"`
	ExpiresIn       int    `json:"expires_in"` // seconds
	RunID           string `json:"run_id"`
}

// newDelegationIssuanceResponse converts a storage record to its wire form.
func newDelegationIssuanceResponse(d *DelegationIssuance) DelegationIssuanceResponse {
	resp := DelegationIssuanceResponse{
		JTI:                    d.JTI,
		RunID:                  d.RunID,
		TenantID:               d.TenantID,
		DelegatorPrincipalID:   d.DelegatorPrincipalID,
		DelegatorPrincipalType: string(d.DelegatorPrincipalType),
		AgentPrincipalID:       d.AgentPrincipalID,
		CreatedAt:              d.CreatedAt.UTC().Format(time.RFC3339),
	}
	if d.RevokedAt != nil {
		revoked := d.RevokedAt.UTC().Format(time.RFC3339)
		resp.RevokedAt = &revoked
	}
	return resp
}

// ─── Sentinel errors ─────────────────────────────────────────────────────────

var (
	ErrSessionNotFound             = errors.New("session not found")
	ErrRefreshTokenNotFound        = errors.New("refresh token not found")
	ErrRefreshTokenExpired         = errors.New("refresh token expired")
	ErrRefreshTokenRevoked         = errors.New("refresh token revoked")
	ErrTokenRevoked                = errors.New("token has been revoked")
	ErrInvalidToken                = errors.New("invalid token")
	ErrOIDCNotConfigured           = errors.New("federated authentication not configured")
	ErrAuthorizationPending        = errors.New("authorization_pending")
	ErrSlowDown                    = errors.New("slow_down")
	ErrAccessDenied                = errors.New("access_denied")
	ErrDeviceCodeExpired           = errors.New("device code expired or not found")
	ErrUnsupportedTokenType        = errors.New("unsupported token type")
	ErrIDTokenInvalid              = errors.New("id_token failed verification")
	ErrTenantNotFound              = errors.New("tenant not found for email domain")
	ErrDeviceFlowNotSupported      = errors.New("tenant OIDC provider does not support Device Authorization Grant")
	ErrInvalidEmailFormat          = errors.New("invalid email format")
	ErrPATNotFound                 = errors.New("PAT not found")
	ErrPATSigningFailed            = errors.New("PAT signing failed")
	ErrPATPersistFailed            = errors.New("PAT persistence failed")
	ErrPATListFailed               = errors.New("PAT list failed")
	ErrPATRevokeFailed             = errors.New("PAT revocation failed")
	ErrPATScopesRequired           = errors.New("at least one scope is required")
	ErrPATUpdateFailed             = errors.New("PAT scope update failed")
	ErrUserProfileNotFound         = errors.New("user profile not found")
	ErrDelegationNotFound          = errors.New("no active delegation issuance found")
	ErrDelegationRunActive         = errors.New("an active delegation issuance already exists for this run")
	ErrDelegationPrincipalType     = errors.New("delegations can only be minted by HUMAN or PAT principals")
	ErrDelegationSubjectToken      = errors.New("invalid or expired subject_token")
	ErrDelegationAgentMismatch     = errors.New("caller identity does not match the delegation's agent principal")
	ErrDelegationRevokeForbidden   = errors.New("only the delegator or a platform service may revoke a delegation")
	ErrUserPreferencesNotFound     = errors.New("user preferences not found")
	ErrPreferencesDocumentTooLarge = errors.New("preferences document exceeds 64 KB limit")
)

// ErrPATScopesForbiddenError is returned when the requested scopes exceed the
// issuing user's current effective scopes.
type ErrPATScopesForbiddenError struct {
	Scopes []string
}

func (e *ErrPATScopesForbiddenError) Error() string {
	return fmt.Sprintf("scopes exceed your current access: %v", e.Scopes)
}
