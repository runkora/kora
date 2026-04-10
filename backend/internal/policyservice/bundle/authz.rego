package orbit.authz

import rego.v1

default allow := false

# All principal types — autonomous (no delegation token)
allow if {
	not input.delegation_present
	principal_has_required_scope
}

# All principal types — delegated
allow if {
	input.delegation_present
	principal_has_required_scope
	delegation_covers_required_scope
	delegator_covers_required_scope
}

# HUMAN principals with an OIDC admin role (PAP access, bootstrapping)
allow if {
	input.principal.type == "HUMAN"
	some role in input.principal.roles
	some role_scope in data.roles.role_scopes[role]
	scope_covers(role_scope, input.required_scope)
}

principal_has_required_scope if {
	some grant in data.grants.grants
	grant.principal_type == input.principal.type
	grant.principal_id == input.principal.id
	some granted_scope in grant.scopes
	scope_covers(granted_scope, input.required_scope)
}

# matched_grant_scope is the specific granted scope that covers the required scope.
matched_grant_scope := granted_scope if {
	some grant in data.grants.grants
	grant.principal_type == input.principal.type
	grant.principal_id == input.principal.id
	some granted_scope in grant.scopes
	scope_covers(granted_scope, input.required_scope)
}

delegation_covers_required_scope if {
	some ds in input.delegation_scopes
	scope_covers(ds, input.required_scope)
}

delegator_covers_required_scope if {
	some grant in data.grants.grants
	grant.principal_id == input.delegator_id
	some gs in grant.scopes
	scope_covers(gs, input.required_scope)
}

# reason explains the outcome of the allow decision.
# On allow: which rule matched. On deny: why it was denied.
default reason := "deny:no_matching_grant"

reason := concat("", ["grant:", matched_grant_scope]) if {
	not input.delegation_present
	principal_has_required_scope
}

reason := concat("", ["delegation:", matched_grant_scope]) if {
	input.delegation_present
	principal_has_required_scope
	delegation_covers_required_scope
	delegator_covers_required_scope
}

reason := concat("", ["role:", role_match]) if {
	input.principal.type == "HUMAN"
	some role_match in input.principal.roles
	some role_scope in data.roles.role_scopes[role_match]
	scope_covers(role_scope, input.required_scope)
}

reason := "deny:no_delegation_scope" if {
	input.delegation_present
	principal_has_required_scope
	not delegation_covers_required_scope
}

reason := "deny:no_delegator_grant" if {
	input.delegation_present
	principal_has_required_scope
	delegation_covers_required_scope
	not delegator_covers_required_scope
}

# Hierarchy: A covers B if same resource_type, same action, A's path is a prefix of B's path.
# Scope strings are parsed structurally (split on ":") so parent scopes cover resource-specific
# child scopes that have not been pre-registered in the vocabulary. The vocabulary is used only
# for grant validation at write time, not for runtime hierarchy resolution.
scope_covers(a, b) if {
	a == b
}

scope_covers(a, b) if {
	parts_a := split(a, ":")
	parts_b := split(b, ":")
	count(parts_a) >= 2
	count(parts_b) >= 2
	parts_a[0] == parts_b[0]
	parts_a[count(parts_a) - 1] == parts_b[count(parts_b) - 1]
	path_a := array.slice(parts_a, 1, count(parts_a) - 1)
	path_b := array.slice(parts_b, 1, count(parts_b) - 1)
	count(path_a) <= count(path_b)
	array.slice(path_b, 0, count(path_a)) == path_a
}
