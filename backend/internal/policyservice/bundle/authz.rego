package orbit.authz

import rego.v1

default allow := false

# ══ LAYER DETECTION ══════════════════════════════════════════════════════════
#
# Layer 1 — OPA Envoy Plugin sidecar: input.attributes.request.http is present.
#           Receives a raw Envoy CheckRequest; validates the JWT here and derives
#           required_scope from data.routes.route_scopes.
#
# Layer 2 — platform/policy Go library: input.principal is present.
#           The calling service has already validated the JWT and pre-processed
#           all fields. No JWT parsing or route matching required.

_is_envoy if input.attributes.request.http

# ══ LAYER 1: JWT VALIDATION ══════════════════════════════════════════════════
#
# OPA fetches the identity service JWKS on first evaluation and caches the
# response for 5 minutes. Signature, issuer, and audience are all validated.
# The OPA container runs as UID 1337 so its outbound traffic bypasses Envoy's
# iptables redirect — preventing recursive ext_authz calls during JWKS fetch.
#
# NOTE: OPA 1.15.x has a bug where io.jwt.decode_verify with the "jwks"
# constraint silently returns undefined for RSA tokens. We work around this
# by extracting the first key from the JWKS and passing it as a serialised
# JWK via the "cert" constraint, which handles RSA correctly.

_layer1_token := raw if {
	_is_envoy
	auth := input.attributes.request.http.headers.authorization
	raw := trim_prefix(auth, "Bearer ")
}

_layer1_claims := claims if {
	_is_envoy
	resp := http.send({
		"url":                          data.config.jwks_url,
		"method":                       "GET",
		"force_cache":                  true,
		"force_cache_duration_seconds": 300,
	})
	jwks := json.unmarshal(resp.raw_body)
	# io.jwt.decode_verify returns [valid, header, payload]; valid must be true.
	# Use "cert" with the serialised JWK to work around the OPA 1.15.x "jwks" bug.
	# NOTE: jwks.keys[0] assumes a single signing key. If key rotation with two
	# simultaneous keys is needed, match on the JWT header "kid" claim instead —
	# `some key in jwks.keys` does not work reliably with io.jwt.decode_verify
	# in OPA 1.15.x (non-matching keys return undefined, killing the rule).
	result := io.jwt.decode_verify(_layer1_token, {
		"cert": json.marshal(jwks.keys[0]),
		"iss":  data.config.issuer,
		"aud":  data.config.audience,
	})
	result[0] == true
	claims := result[2]
}

# ══ NORMALISED PRINCIPAL ════════════════════════════════════════════════════

_principal_type := input.principal.type if not _is_envoy
_principal_type := _layer1_claims.orbit.principal_type if _is_envoy

_principal_id := input.principal.id if not _is_envoy
_principal_id := _layer1_claims.sub if _is_envoy

# Roles: Layer 2 receives a pre-split []string; Layer 1 splits the
# comma-separated roles string from the orbit.claims.roles JWT field.
_principal_roles := input.principal.roles if not _is_envoy

_principal_roles := roles if {
	_is_envoy
	raw := _layer1_claims.orbit.claims.roles
	raw != ""
	roles := split(raw, ",")
}

_principal_roles := [] if {
	_is_envoy
	not _layer1_claims.orbit.claims.roles
}

_principal_roles := [] if {
	_is_envoy
	_layer1_claims.orbit.claims.roles == ""
}

# ══ NORMALISED REQUIRED SCOPE ════════════════════════════════════════════════
#
# Layer 2: required_scope is pre-computed by buildOPAInput from the resource
#          attributes and action provided by the calling service handler.
#
# Layer 1: derived by matching the request path prefix and HTTP method against
#          data.routes.route_scopes (compiled into every bundle by the policy-service).
#          This is a coarse service-level scope — Layer 2 enforces the resource-
#          specific scope from the request payload.

_required_scope := input.required_scope if not _is_envoy

_required_scope := route.required_scope if {
	_is_envoy
	some route in data.routes.route_scopes
	startswith(input.attributes.request.http.path, route.path_prefix)
	route.method == input.attributes.request.http.method
}

# Wildcard method match — only applies when no exact method route matches,
# preventing a conflict if both an exact and a wildcard route cover the same path.
_has_exact_method_route if {
	_is_envoy
	some route in data.routes.route_scopes
	startswith(input.attributes.request.http.path, route.path_prefix)
	route.method == input.attributes.request.http.method
}

_required_scope := route.required_scope if {
	_is_envoy
	not _has_exact_method_route
	some route in data.routes.route_scopes
	startswith(input.attributes.request.http.path, route.path_prefix)
	route.method == "*"
}

# ══ NORMALISED DELEGATION ════════════════════════════════════════════════════
#
# Layer 2: delegation fields are pre-processed by buildOPAInput; the Go caller
#          has already verified and parsed the delegation token via
#          ParseDelegationToken (signature, issuer, audience, expiry).
#
# Layer 1: delegation token is read from the X-Orbit-Delegation-Token header.
#          Signature verification is handled by the DelegationMiddleware before
#          the request reaches OPA evaluation.

_is_delegated if {
	not _is_envoy
	input.delegation_present
}

_is_delegated if {
	_is_envoy
	input.attributes.request.http.headers["x-orbit-delegation-token"] != ""
}

# Layer 1: parse delegation token header (parse only, no signature check)
_layer1_delegation_claims := claims if {
	_is_envoy
	_is_delegated
	raw := input.attributes.request.http.headers["x-orbit-delegation-token"]
	# io.jwt.decode returns [header, payload, sig]; payload is index 1.
	decoded := io.jwt.decode(raw)
	claims := decoded[1]
}

_delegation_scopes := input.delegation_scopes if not _is_envoy

_delegation_scopes := scopes if {
	_is_envoy
	_is_delegated
	raw_scope := _layer1_delegation_claims.scope
	scopes := split(raw_scope, " ")
}

_delegator_id := input.delegator_id if not _is_envoy

_delegator_id := _layer1_delegation_claims.sub if {
	_is_envoy
	_is_delegated
}

# ══ INTERNAL PLATFORM BYPASS ════════════════════════════════════════════════
#
# These paths use mTLS workload identity (SPIFFE) for authentication — they
# carry no user JWT and must not be evaluated against the grant/scope model.
#
# Security: this bypass relies on Istio PeerAuthentication STRICT mode enforcing
# mTLS on all inbound traffic to these services. Without STRICT mode, requests
# from outside the mesh reach these paths without any credential check.
#
# Each entry is an explicit, narrow bypass:
#
#   /api/v1/identity/exchange          — identity-proxy acquires platform token
#   /api/v1/identity/.well-known/      — OPA fetches JWKS to validate JWTs
#   /api/v1/policy/bundles/            — OPA polls bundle from policy service
#
# All other paths are deny-by-default; unregistered paths without a matching
# route in data.routes.route_scopes will produce deny:no_route_match.

_is_platform_internal_path if {
	_is_envoy
	some prefix in [
		"/api/v1/identity/exchange",
		"/api/v1/identity/.well-known/",
		"/api/v1/policy/bundles/",
	]
	startswith(input.attributes.request.http.path, prefix)
}

# ══ ALLOW RULES ══════════════════════════════════════════════════════════════

# Platform-internal infrastructure calls (mTLS authenticated, no user JWT).
allow if _is_platform_internal_path

# All principal types — autonomous (no delegation token)
allow if {
	not _is_delegated
	principal_has_required_scope
}

# All principal types — delegated
allow if {
	_is_delegated
	principal_has_required_scope
	delegation_covers_required_scope
	delegator_covers_required_scope
}

# HUMAN principals with an OIDC admin role (PAP access, bootstrapping).
# Must not fire for delegated requests — delegation constrains the effective
# permissions to the intersection of delegator grants and delegation scopes.
allow if {
	not _is_delegated
	_principal_type == "HUMAN"
	some role in _principal_roles
	some role_scope in data.roles.role_scopes[role]
	scope_covers(role_scope, _required_scope)
}

# ══ GRANT EVALUATION ════════════════════════════════════════════════════════

principal_has_required_scope if {
	some grant in data.grants.grants
	grant.principal_type == _principal_type
	grant.principal_id == _principal_id
	some granted_scope in grant.scopes
	scope_covers(granted_scope, _required_scope)
}

# matched_grant_scopes collects all granted scopes that cover the required scope.
# Using a partial set rule avoids OPA's "complete rules must not produce multiple
# outputs" error when a principal holds overlapping grants.
matched_grant_scopes contains granted_scope if {
	some grant in data.grants.grants
	grant.principal_type == _principal_type
	grant.principal_id == _principal_id
	some granted_scope in grant.scopes
	scope_covers(granted_scope, _required_scope)
}

# matched_grant_scope picks the lexicographically smallest match for a stable reason string.
matched_grant_scope := min(matched_grant_scopes) if count(matched_grant_scopes) > 0

delegation_covers_required_scope if {
	some ds in _delegation_scopes
	scope_covers(ds, _required_scope)
}

delegator_covers_required_scope if {
	some grant in data.grants.grants
	grant.principal_id == _delegator_id
	some gs in grant.scopes
	scope_covers(gs, _required_scope)
}

# ══ REASON RULES ═════════════════════════════════════════════════════════════
#
# reason is for logging only — never returned to callers.
# On allow: which path matched. On deny: which condition failed.

default reason := "deny:no_matching_grant"

reason := concat("", ["grant:", matched_grant_scope]) if {
	not _is_delegated
	principal_has_required_scope
}

reason := concat("", ["delegation:", matched_grant_scope]) if {
	_is_delegated
	principal_has_required_scope
	delegation_covers_required_scope
	delegator_covers_required_scope
}

# matched_role_scopes collects all role+scope pairs that cover the required scope.
# Using a partial set + min() mirrors matched_grant_scope for determinism.
matched_roles contains role_match if {
	_principal_type == "HUMAN"
	some role_match in _principal_roles
	some role_scope in data.roles.role_scopes[role_match]
	scope_covers(role_scope, _required_scope)
}

reason := concat("", ["role:", min(matched_roles)]) if {
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

# Layer 1-specific deny reasons
reason := "deny:invalid_token" if {
	_is_envoy
	not _layer1_claims
}

reason := "deny:no_route_match" if {
	_is_envoy
	_layer1_claims
	not _required_scope
}

# ══ SCOPE HIERARCHY ══════════════════════════════════════════════════════════
#
# A covers B if same resource_type, same action, A's path is a prefix of B's path.
# Scope strings are parsed structurally (split on ":") so parent scopes cover
# resource-specific child scopes without requiring vocabulary registration.

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
