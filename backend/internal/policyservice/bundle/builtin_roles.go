package bundle

import (
	"slices"

	"github.com/runkora/kora/backend/internal/policyservice/vocabulary"
)

// Built-in role names. Like platform-admin they are reserved: tenants cannot
// store, edit or delete them, and they match IdP groups of the same name.
const (
	ViewerRole   = "kora-viewer"
	UserRole     = "kora-user"
	BuilderRole  = "kora-builder"
	OperatorRole = "kora-operator"
	FinOpsRole   = "kora-finops"
)

// ReservedRoleNames lists every reserved role in the order the roles API shows them.
var ReservedRoleNames = []string{PlatformAdminRole, ViewerRole, UserRole, BuilderRole, OperatorRole, FinOpsRole}

// Each tier holds the tier below it. Model, connection and MCP tool scopes
// stay out: tenants add those as their own roles. egress:connect lets an
// operator give the autonomous agents they create open internet access.
var (
	viewerScopes = []string{
		"agents:types:read",
		"agents:environments:read",
		"triggers:read",
		"inference:list",
		"platform:connections:read",
	}
	userScopes = append(slices.Clone(viewerScopes),
		"inference:invoke",
		"agents:runs:delegated:write",
		"mcp:invoke",
	)
	builderScopes = append(slices.Clone(userScopes),
		"agents:types:delegated:write",
	)
	operatorScopes = append(slices.Clone(builderScopes),
		"agents:types:autonomous:write",
		"egress:connect",
		"triggers:write",
		"trigger:fire",
		"agents:usage:read",
		"platform:analytics:read",
		"platform:budgets:read",
		"identity:users:read",
	)
)

// finOpsScopes is a standalone role for spend oversight. It is not a tier.
var finOpsScopes = []string{
	"platform:analytics:read",
	"agents:usage:read",
	"platform:budgets:read",
	"platform:budgets:write",
	"platform:roles:read",
	"platform:grants:read",
	"identity:users:read",
	"agents:types:read",
	"inference:list",
}

var builtinRoles = map[string][]string{
	ViewerRole:   viewerScopes,
	UserRole:     userScopes,
	BuilderRole:  builderScopes,
	OperatorRole: operatorScopes,
	FinOpsRole:   finOpsScopes,
}

// IsBuiltinRole reports whether role is one of the fixed kora-* roles. Their
// scopes are compiled without requires expansion: none of them is meant to
// confer another scope, and a requires edge on a covered leaf must not widen them.
func IsBuiltinRole(role string) bool {
	_, ok := builtinRoles[role]
	return ok
}

// IsReservedRole reports whether role is platform-admin or a built-in role.
func IsReservedRole(role string) bool {
	return slices.Contains(ReservedRoleNames, role)
}

// BuiltinRoleScopes splits a built-in role's declared scopes into those the
// vocabulary holds and those it does not hold yet. Only held scopes compile,
// which lets a role work before every service has registered its scopes.
func BuiltinRoleScopes(role string, vocab []*vocabulary.ScopeEntry) (held, missing []string) {
	held = []string{}
	for _, sc := range builtinRoles[role] {
		if slices.ContainsFunc(vocab, func(e *vocabulary.ScopeEntry) bool { return e.Scope == sc }) {
			held = append(held, sc)
		} else {
			missing = append(missing, sc)
		}
	}
	return held, missing
}

// ReservedRoles returns the scopes of every reserved role derived from vocab,
// before requires expansion.
func ReservedRoles(vocab []*vocabulary.ScopeEntry) map[string][]string {
	out := map[string][]string{PlatformAdminRole: AdminScopes(vocab)}
	for role := range builtinRoles {
		out[role], _ = BuiltinRoleScopes(role, vocab)
	}
	return out
}
