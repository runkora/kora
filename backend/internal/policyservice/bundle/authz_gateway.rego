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

# An empty scope claim means "no scopes" (v1 delegation tokens carry none —
# see the fail-closed marker section); leaving it undefined keeps the
# token-scope leg satisfied instead of producing the never-matching [""] split.
_delegation_scopes := scopes if {
	_is_delegated
	raw_scope := _layer1_delegation_claims.scope
	raw_scope != ""
	scopes := split(raw_scope, " ")
}

_delegator_id := _layer1_delegation_claims.sub if _is_delegated

# The delegator's principal type from the delegation token (always HUMAN for
# v1 tokens — PAT-minted delegations normalise to the person at issuance). A
# token without the claim leaves this undefined, and with it the
# delegator-grant leg: fail closed.
_delegator_principal_type := _layer1_delegation_claims.delegator_principal_type if _is_delegated

# ══ DELEGATION RUN BINDING ════════════════════════════════════════════════════
#
# Mirrors authz.rego (docs/adr/2026-07-23-delegated-agent-identity.md,
# Decision 4): a delegation token is bound to one run via its run_id claim.
# Where the request independently carries run identity — the /run/{run_id}
# path prefix or the platform token's run_id claim (per-run sandbox SVIDs) —
# a delegation bound to a different run is refused.

_path_run_id := m[0][1] if {
	m := regex.find_all_string_submatch_n(`^/run/([^/]+)/`, input.attributes.request.http.path, 1)
	count(m) == 1
}

# The `default` is load-bearing — see authz.rego for the full rationale: an
# absent run_id claim would otherwise leave every comparison below undefined,
# and with it every `not _delegation_binding_failed` guard passing.
default _delegation_run_id := ""

_delegation_run_id := _layer1_delegation_claims.run_id

# NO run binding on the token at all: malformed, since every minted delegation
# carries run_id. Distinct from "run identity is not independently knowable
# here", which stays allowed.
_delegation_run_unbound if {
	_is_delegated
	_delegation_run_id == ""
}

_delegation_run_mismatch if {
	_is_delegated
	_delegation_run_id != ""
	_path_run_id != _delegation_run_id
}

_delegation_run_mismatch if {
	_is_delegated
	_delegation_run_id != ""
	_claims.orbit.claims.run_id != _delegation_run_id
}

# Aggregate: the delegation failed a binding check and must not be honoured.
_delegation_binding_failed if _delegation_run_mismatch

_delegation_binding_failed if _delegation_run_unbound

# ══ DELEGATION FAIL-CLOSED MARKER ═════════════════════════════════════════════
#
# Mirrors authz.rego (docs/adr/2026-07-23-delegated-agent-identity.md,
# Decision 2): a grant carrying delegation_required marks its principal as a
# delegated agent whose grant is a ceiling, never a standing entitlement. Every
# autonomous allow path refuses such a principal, so a missing delegation token
# means NO access rather than the agent's standing access.

_principal_requires_delegation if {
	some grant in data.tenants[_tenant_id].grants.grants
	grant.principal_type == _principal_type
	grant.principal_id == _principal_id
	grant.delegation_required == true
}

# The token-scope leg applies only when the delegation context carries scopes —
# v1 delegation tokens carry no scope claim (Decision 3); authority is
# evaluated live from the agent-grant and delegator legs.
_delegation_has_scopes if {
	is_array(_delegation_scopes)
	count(_delegation_scopes) > 0
}

_delegation_scope_satisfied if not _delegation_has_scopes

_delegation_scope_satisfied if delegation_covers_required_scope

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
		# Delegation run-token retrieval: the identity-proxy authenticates by
		# mesh-attested SPIFFE identity (XFCC), not a bearer token — same
		# treatment as /identity/exchange (delegated-agent-identity ADR,
		# Decision 3). Release is gated in-process on the caller's SVID
		# matching the issuance's agent principal.
		"/api/v1/identity/delegation/run/",
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

# ══ DELEGATOR LEG (delegated-agent-identity ADR, Decision 5) ═════════════════
#
# Mirrors authz.rego: the delegator's side of the intersection is the person's
# FULL held access — direct grants plus role-derived scopes, resolved offline
# from the tenant's bundle data — plus the vocabulary `requires` projection for
# scopes a person is never granted (MCP tool scopes).

# Direct grants. Principal type must match the delegation token's
# delegator_principal_type so two principal types sharing an id string can
# never satisfy this leg (closes the former TODO(M2)).
_delegator_holds(scope) if {
	some grant in data.tenants[_tenant_id].grants.grants
	grant.principal_type == _delegator_principal_type
	grant.principal_id == _delegator_id
	some gs in grant.scopes
	scope_covers(gs, scope)
}

# Role-derived access: membership from the bundled user_profiles snapshot (the
# same source PAT role attribution uses), through the tenant's role→scope map.
_delegator_holds(scope) if {
	_delegator_principal_type == "HUMAN"
	some role in data.tenants[_tenant_id].principal_roles.principal_roles[_delegator_id]
	some role_scope in data.tenants[_tenant_id].roles.role_scopes[role]
	scope_covers(role_scope, scope)
}

# Vocabulary requires-adjacency (the vocabulary is shared, bundled top-level).
# graph.reachable only reports vertices that exist as graph KEYS, so every
# requires TARGET gets a leaf entry too — otherwise the expansion would
# silently drop targets and weaken the every-check below.
_requires_graph[scope] := entry.requires if {
	some scope, entry in data.vocabulary.scope_vocabulary
	entry.requires
}

_requires_graph[scope] := [] if {
	some _, entry in data.vocabulary.scope_vocabulary
	some scope in entry.requires
	not data.vocabulary.scope_vocabulary[scope].requires
}

_required_scope_expansion := graph.reachable(_requires_graph, {_required_scope}) - {_required_scope}

# The delegator covers the required scope directly (grant or role)...
delegator_covers_required_scope if _delegator_holds(_required_scope)

# ...or through its vocabulary requires expansion, every scope of which the
# delegator must cover — e.g. mcp:{server}:{tool}:use delegates through to the
# person's connection:* authority.
delegator_covers_required_scope if {
	count(_required_scope_expansion) > 0
	every required in _required_scope_expansion {
		_delegator_holds(required)
	}
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
	not _principal_requires_delegation
	principal_has_required_scope
}

# Authenticated principal — delegated.
allow if {
	_is_delegated
	not _delegation_binding_failed
	principal_has_required_scope
	_delegation_scope_satisfied
	delegator_covers_required_scope
}

# HUMAN principals with an OIDC admin role (grants:read/write, bootstrapping).
# Not available for delegated requests — delegation constrains effective permissions.
allow if {
	not _is_delegated
	not _principal_requires_delegation
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
	not _principal_requires_delegation
	principal_has_required_scope
}

reason := concat("", ["delegation:", matched_grant_scope]) if {
	_is_delegated
	not _delegation_binding_failed
	principal_has_required_scope
	_delegation_scope_satisfied
	delegator_covers_required_scope
}

# Fail-closed marker fired: the principal's grant would have covered the
# request, but the grant requires delegation and none is present. Gated on
# principal_has_required_scope (and the bypass guards) so it can never
# conflict with the other reason rules.
reason := "deny:delegation_required" if {
	not _is_frontend_request
	not _is_platform_internal_path
	not _is_self_enforced_path
	not _is_delegated
	_principal_requires_delegation
	principal_has_required_scope
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
	not _delegation_binding_failed
	principal_has_required_scope
	not _delegation_scope_satisfied
}

reason := "deny:no_delegator_grant" if {
	_is_delegated
	not _delegation_binding_failed
	principal_has_required_scope
	_delegation_scope_satisfied
	not delegator_covers_required_scope
}

reason := "deny:no_agent_grant" if {
	_is_delegated
	not _delegation_binding_failed
	_delegation_scope_satisfied
	delegator_covers_required_scope
	not principal_has_required_scope
}

# A delegation bound to a different run than the one the request asserts (or
# the platform token attests). Guarded out of every other delegated reason
# rule so complete-rule conflicts are impossible.
reason := "deny:delegation_run_mismatch" if {
	not _is_frontend_request
	not _is_platform_internal_path
	not _is_self_enforced_path
	_claims
	_is_delegated
	_delegation_run_mismatch
}

# A delegation carrying no run_id claim at all — malformed, since every minted
# delegation is bound to a run. Mutually exclusive with the mismatch reason
# above (which requires a non-empty run_id).
reason := "deny:delegation_run_unbound" if {
	not _is_frontend_request
	not _is_platform_internal_path
	not _is_self_enforced_path
	_claims
	_is_delegated
	_delegation_run_unbound
}

# An invalid platform token is reported ahead of any delegation-binding
# failure; the binding reasons above require _claims, so the two are mutually
# exclusive.
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
	not _delegation_binding_failed
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
