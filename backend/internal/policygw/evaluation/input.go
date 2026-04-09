package evaluation

import (
	"strings"

	"github.com/clear-route/orbit/internal/platform/token"
)

// Input is the OPA input document for a single policy evaluation.
// It maps directly to the JSON structure consumed by authz.rego.
type Input struct {
	Principal         PrincipalInput `json:"principal"`
	DelegationPresent bool           `json:"delegation_present"`
	DelegationScopes  []string       `json:"delegation_scopes"`
	DelegatorID       string         `json:"delegator_id,omitempty"`
	RequiredScope     string         `json:"required_scope"`
	Resource          ResourceInput  `json:"resource"`
	Action            string         `json:"action"`
}

// PrincipalInput is the principal section of the OPA input document.
type PrincipalInput struct {
	Type     string   `json:"type"`
	TenantID string   `json:"tenant_id"`
	ID       string   `json:"id"`
	Roles    []string `json:"roles"`
}

// ResourceInput is the resource section of the OPA input document.
type ResourceInput struct {
	Type       string            `json:"type"`
	Attributes map[string]string `json:"attributes,omitempty"`
}

// DelegationClaims holds the parsed delegation token claims used to populate
// the delegation fields of the OPA input. See ADR-0009 and ADR-0035.
type DelegationClaims struct {
	// Sub is the delegating human's identifier (the delegator_id in OPA input).
	Sub string
	// ActSub is the agent acting on behalf of the delegator.
	ActSub string
	// Scopes are the space-separated scopes from the delegation token's scope claim.
	Scopes []string
}

// BuildInput constructs the OPA input document from a validated platform token
// and an optional delegation token. requiredScope is derived by the caller
// from the resource attributes and action.
func BuildInput(
	claims *token.PlatformClaims,
	delegation *DelegationClaims,
	requiredScope string,
	resource ResourceInput,
	action string,
) Input {
	roles := extractRoles(claims)

	input := Input{
		Principal: PrincipalInput{
			Type:     string(claims.Orbit.PrincipalType),
			TenantID: claims.Orbit.TenantID,
			ID:       claims.Subject,
			Roles:    roles,
		},
		RequiredScope: requiredScope,
		Resource:      resource,
		Action:        action,
	}

	if delegation != nil {
		input.DelegationPresent = true
		input.DelegationScopes = delegation.Scopes
		input.DelegatorID = delegation.Sub
	}

	return input
}

// extractRoles reads the "roles" key from PlatformClaims.Orbit.Claims and
// splits it on commas. The identity service embeds IdP group/role claims here
// for HUMAN principals (e.g. Entra group membership → "tenant-admin").
func extractRoles(claims *token.PlatformClaims) []string {
	if claims.Orbit.Claims == nil {
		return []string{}
	}
	raw, ok := claims.Orbit.Claims["roles"]
	if !ok || raw == "" {
		return []string{}
	}
	parts := strings.Split(raw, ",")
	roles := make([]string, 0, len(parts))
	for _, p := range parts {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			roles = append(roles, trimmed)
		}
	}
	return roles
}
