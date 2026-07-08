package orbit.authz_gateway

import rego.v1

default allow := false

# ══ JWT VALIDATION ════════════════════════════════════════════════════════════
#
# Identical to authz.rego's Layer 1 JWT validation. OPA fetches the JWKS once
# and caches it for 5 minutes. The RSA "cert" workaround (OPA 1.15.x bug) applies
# here too — see authz.rego for the full explanation.

_token := raw if {
	auth := input.attributes.request.http.headers.authorization
	bearer := trim_prefix(auth, "Bearer ")
	# Strip the orbit PAT prefix ("orb_") to get the raw JWT.
	raw := trim_prefix(bearer, "orb_")
}

# The Anthropic SDK sends credentials as x-api-key rather than Authorization:
# Bearer. Support both so SDK clients work without reconfiguration.
_token := raw if {
	not input.attributes.request.http.headers.authorization
	api_key := input.attributes.request.http.headers["x-api-key"]
	raw := trim_prefix(api_key, "orb_")
}

_claims := claims if {
	resp := http.send({
		"url":                          data.config.jwks_url,
		"method":                       "GET",
		"force_cache":                  true,
		"force_cache_duration_seconds": 300,
	})
	jwks := json.unmarshal(resp.raw_body)
	result := io.jwt.decode_verify(_token, {
		"cert": json.marshal(jwks.keys[0]),
		"iss":  data.config.issuer,
		"aud":  data.config.audience,
	})
	result[0] == true
	claims := result[2]
}

# ══ NORMALISED PRINCIPAL ══════════════════════════════════════════════════════

_tenant_id := _claims.orbit.tenant_id

_principal_type := _claims.orbit.principal_type

_principal_id := _claims.orbit.pat_id if {
	_claims.orbit.principal_type == "PAT"
	_claims.orbit.pat_id != ""
}

_principal_id := _claims.sub if {
	_claims.orbit.principal_type != "PAT"
}

_principal_roles := roles if {
	raw := _claims.orbit.claims.roles
	raw != ""
	roles := split(raw, ",")
}

_principal_roles := [] if {
	not _claims.orbit.claims.roles
}

_principal_roles := [] if {
	_claims.orbit.claims.roles == ""
}

# ══ DELEGATION ════════════════════════════════════════════════════════════════
#
# Delegation at the gateway mirrors authz.rego Layer 1: parse the
# X-Orbit-Delegation-Token header without signature verification (signature
# is checked in-process by the delegatee service after OPA allows the request).

_is_delegated if {
	input.attributes.request.http.headers["x-orbit-delegation-token"] != ""
}

_layer1_delegation_claims := claims if {
	_is_delegated
	raw := input.attributes.request.http.headers["x-orbit-delegation-token"]
	decoded := io.jwt.decode(raw)
	claims := decoded[1]
}

_delegation_scopes := scopes if {
	_is_delegated
	raw_scope := _layer1_delegation_claims.scope
	scopes := split(raw_scope, " ")
}

_delegator_id := _layer1_delegation_claims.sub if _is_delegated

# ══ FRONTEND HOST BYPASS ══════════════════════════════════════════════════════
#
# The frontend (Next.js BFF) is accessed by browsers using iron-session cookies.
# OPA cannot evaluate session cookies, so all traffic to the frontend host is
# bypassed here. The BFF calls backend services east-west (internal cluster DNS)
# and those paths are enforced by per-service OPA sidecars or this gateway rule.
#
# The :authority header may include a port (e.g. "orbit.local.clearroute.io:8443"
# in local dev). We strip the port before comparing against data.config.frontend_host
# so the same config value works for both standard (443) and non-standard ports.

_is_frontend_request if {
	authority := input.attributes.request.http.headers[":authority"]
	host := split(authority, ":")[0]
	host == data.config.frontend_host
}

# ══ INFRASTRUCTURE BYPASS ═════════════════════════════════════════════════════
#
# Health checks, OIDC discovery, bundle polling, and inbound webhook delivery
# endpoints are unauthenticated by design. The gateway must allow them through
# without JWT validation.
#
# /api/v1/webhooks/ is the Triggers feature's inbound delivery endpoint
# (docs/adr/2026-06-17-agent-triggers-and-webhook-ingestion.md). External
# providers (e.g. GitHub) POST here with no platform token; the request is
# authenticated by the source adapter's HMAC signature check in-process, so it
# must bypass JWT validation at the gateway.

_is_platform_internal_path if {
	some prefix in [
		"/health",
		"/api/v1/identity/exchange",
		"/api/v1/identity/.well-known/",
		"/api/v1/policy/bundles/",
		"/api/v1/webhooks/",
		# Root OAuth discovery metadata (RFC 8414/9728) served at the host root
		# (ADR 2026-07-04) and the public token/registration endpoints.
		"/.well-known/",
		"/api/v1/identity/oauth/token",
		"/api/v1/identity/oauth/register",
	]
	startswith(input.attributes.request.http.path, prefix)
}

# ══ SELF-ENFORCED SERVICES ════════════════════════════════════════════════════
#
# A self-enforced service runs its own authentication and PEP and must own the
# full request/response — the MCP tool-gateway validates the token, issues the
# RFC 9728 401 challenge for an unauthenticated client (bootstrapping OAuth), and
# enforces per-tool scopes in-process. The edge must not pre-empt it: enforcing
# the coarse mcp:invoke route-scope here would deny an unauthenticated discovery
# request before the gateway can challenge it. So the gateway's own paths bypass
# edge scope enforcement (north-south only — the east-west sidecar OPA in
# authz.rego still enforces mcp:invoke on agent traffic).
# Ref ADR 2026-07-04 (edge-enforced vs self-enforced services).
# Exact "/v1/mcp" (the aggregate metadata path) or a per-server subpath
# "/v1/mcp/<server>". A bare prefix would also match "/v1/mcpx" and any future
# sibling route mounted under /v1/mcp*, bypassing edge scope enforcement for it.
_is_self_enforced_path if {
	startswith(input.attributes.request.http.path, "/v1/mcp/")
}

_is_self_enforced_path if {
	input.attributes.request.http.path == "/v1/mcp"
}

# ══ REQUIRED SCOPE ════════════════════════════════════════════════════════════
#
# Two-table route matching: global routes (SYSTEM principal registrations) and
# per-tenant routes (SERVICE principal registrations for tenant-deployed services).
#
# Exact method > wildcard "*", consistent with authz.rego.
#
# C1 — Conflict risk: if a global route and a tenant route both register the
# same exact (path_prefix, method) with *different* required scopes, both
# _required_scope bodies fire → OPA raises a complete-rule conflict →
# _required_scope is undefined → deny:no_route_match. Fail-closed, but the
# route is silently unenforceable with no diagnostic. Avoid registering the
# same exact (path, method) in both the global and tenant tables.
#
# M1 — Cross-table wildcard suppression: _has_exact_method_route checks BOTH
# tables. A global exact match suppresses wildcards in the tenant table (and
# vice-versa). If a global service registers GET /v1/foo (exact) and a tenant
# service registers * /v1/foo (wildcard), the tenant wildcard is suppressed.
# This crosses table boundaries and may be surprising; prefer explicit method
# registrations over wildcards to avoid this interaction.

_has_exact_method_route if {
	some route in data.routes.route_scopes
	startswith(input.attributes.request.http.path, route.path_prefix)
	route.method == input.attributes.request.http.method
}

_has_exact_method_route if {
	some route in data.tenants[_tenant_id].routes.route_scopes
	startswith(input.attributes.request.http.path, route.path_prefix)
	route.method == input.attributes.request.http.method
}

# Global exact match.
_required_scope := route.required_scope if {
	some route in data.routes.route_scopes
	startswith(input.attributes.request.http.path, route.path_prefix)
	route.method == input.attributes.request.http.method
}

# Global wildcard (only when no exact match in either table).
_required_scope := route.required_scope if {
	not _has_exact_method_route
	some route in data.routes.route_scopes
	startswith(input.attributes.request.http.path, route.path_prefix)
	route.method == "*"
}

# Tenant exact match.
_required_scope := route.required_scope if {
	some route in data.tenants[_tenant_id].routes.route_scopes
	startswith(input.attributes.request.http.path, route.path_prefix)
	route.method == input.attributes.request.http.method
}

# Tenant wildcard (only when no exact match in either table).
_required_scope := route.required_scope if {
	not _has_exact_method_route
	some route in data.tenants[_tenant_id].routes.route_scopes
	startswith(input.attributes.request.http.path, route.path_prefix)
	route.method == "*"
}

# ══ GRANT EVALUATION ══════════════════════════════════════════════════════════
#
# Grants and roles are keyed by tenant so data from tenant A never leaks to
# tenant B even though both live in the same cross-tenant bundle.

principal_has_required_scope if {
	some grant in data.tenants[_tenant_id].grants.grants
	grant.principal_type == _principal_type
	grant.principal_id == _principal_id
	some granted_scope in grant.scopes
	scope_covers(granted_scope, _required_scope)
}

matched_grant_scopes contains granted_scope if {
	some grant in data.tenants[_tenant_id].grants.grants
	grant.principal_type == _principal_type
	grant.principal_id == _principal_id
	some granted_scope in grant.scopes
	scope_covers(granted_scope, _required_scope)
}

# min() provides a deterministic reason string; the choice is lexicographic, not semantic.
matched_grant_scope := min(matched_grant_scopes) if count(matched_grant_scopes) > 0

delegation_covers_required_scope if {
	some ds in _delegation_scopes
	scope_covers(ds, _required_scope)
}

# TODO(M2): also check grant.principal_type against the delegator's principal type.
# Two principal types sharing the same principal_id string would incorrectly satisfy
# this check. Requires adding delegator_principal_type to the delegation token spec.
delegator_covers_required_scope if {
	some grant in data.tenants[_tenant_id].grants.grants
	grant.principal_id == _delegator_id
	some gs in grant.scopes
	scope_covers(gs, _required_scope)
}

# ══ ALLOW RULES ═══════════════════════════════════════════════════════════════

# Frontend traffic: browser UI with session cookies — bypass JWT enforcement.
allow if _is_frontend_request

# Infrastructure paths: health, OIDC discovery, bundle polling.
allow if _is_platform_internal_path

# Self-enforced services (e.g. the MCP tool-gateway) own authn + PEP in-process.
allow if _is_self_enforced_path

# Authenticated principal — autonomous (no delegation).
allow if {
	not _is_delegated
	principal_has_required_scope
}

# Authenticated principal — delegated.
allow if {
	_is_delegated
	principal_has_required_scope
	delegation_covers_required_scope
	delegator_covers_required_scope
}

# HUMAN principals with an OIDC admin role (grants:read/write, bootstrapping).
# Not available for delegated requests — delegation constrains effective permissions.
allow if {
	not _is_delegated
	_principal_type == "HUMAN"
	some role in _principal_roles
	some role_scope in data.tenants[_tenant_id].roles.role_scopes[role]
	scope_covers(role_scope, _required_scope)
}

# ══ REASON RULES ══════════════════════════════════════════════════════════════

default reason := "deny:no_matching_grant"

reason := "allow:frontend_bypass" if _is_frontend_request

reason := "allow:platform_internal_bypass" if _is_platform_internal_path

reason := "allow:self_enforced" if _is_self_enforced_path

reason := concat("", ["grant:", matched_grant_scope]) if {
	not _is_delegated
	not _is_self_enforced_path
	principal_has_required_scope
}

reason := concat("", ["delegation:", matched_grant_scope]) if {
	_is_delegated
	principal_has_required_scope
	delegation_covers_required_scope
	delegator_covers_required_scope
}

matched_roles contains role_match if {
	_principal_type == "HUMAN"
	some role_match in _principal_roles
	some role_scope in data.tenants[_tenant_id].roles.role_scopes[role_match]
	scope_covers(role_scope, _required_scope)
}

reason := concat("", ["role:", min(matched_roles)]) if {
	not _is_delegated
	not _is_self_enforced_path
	not principal_has_required_scope
	count(matched_roles) > 0
}

reason := "deny:no_delegation_scope" if {
	_is_delegated
	principal_has_required_scope
	not delegation_covers_required_scope
}

reason := "deny:no_delegator_grant" if {
	_is_delegated
	principal_has_required_scope
	delegation_covers_required_scope
	not delegator_covers_required_scope
}

reason := "deny:no_agent_grant" if {
	_is_delegated
	delegation_covers_required_scope
	delegator_covers_required_scope
	not principal_has_required_scope
}

reason := "deny:invalid_token" if {
	not _is_frontend_request
	not _is_platform_internal_path
	not _is_self_enforced_path
	not _claims
}

reason := "deny:no_route_match" if {
	not _is_frontend_request
	not _is_platform_internal_path
	not _is_self_enforced_path
	_claims
	not _required_scope
}

# ══ SCOPE HIERARCHY ═══════════════════════════════════════════════════════════
#
# Identical to authz.rego — a covers b if same resource_type and action, and
# a's path is a prefix of b's path.

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
