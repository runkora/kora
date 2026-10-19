// Package token provides shared JWT claim types for kora platform tokens.
// These types are defined here — rather than in internal/identity — so that
// any service binary can import and parse platform tokens without creating a
// cross-feature dependency that violates docs/adr/2026-02-17-repository-pattern-and-code-organization.md.
package token

import (
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
	PrincipalTypePAT     PrincipalType = "PAT"
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

// Principal is the unified identity abstraction for all actors in kora.
// It is the subject of every policy decision, audit event, and delegation chain.
// Downstream components only ever see the Principal carried inside a platform
// session token — they never receive raw OIDC tokens or SVIDs.
type Principal struct {
	// ID is a globally unique, stable identifier.
	// For HUMAN principals this is derived from the upstream IdP subject claim.
	// For AGENT principals this is kora's own agent registry ID.
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
	Kora KoraClaims `json:"orbit"`
}

// KoraClaims are the kora-specific claims embedded in the platform token's
// "orbit" namespace so they do not conflict with registered claim names.
type KoraClaims struct {
	SessionID      string            `json:"session_id"`
	PrincipalType  PrincipalType     `json:"principal_type"`
	TenantID       string            `json:"tenant_id"`
	IdentitySource IdentitySource    `json:"identity_source"`
	DisplayName    string            `json:"display_name"`
	Claims         map[string]string `json:"claims,omitempty"`
	// PatID carries the PAT UUID when PrincipalType == PrincipalTypePAT.
	// It is used as the grant principal_id in OPA evaluation, since sub holds
	// the issuing user's OIDC subject for attribution purposes.
	PatID string `json:"pat_id,omitempty"`
}
