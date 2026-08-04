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

# For PAT principals in Layer 1, use the pat_id claim as the grant lookup key.
# sub holds the issuing user's OIDC subject for attribution; pat_id holds the
# PAT UUID that identifies the grant row.
_principal_id := _layer1_claims.orbit.pat_id if {
	_is_envoy
	_layer1_claims.orbit.principal_type == "PAT"
	_layer1_claims.orbit.pat_id != ""
}

_principal_id := _layer1_claims.sub if {
	_is_envoy
	_layer1_claims.orbit.principal_type != "PAT"
}

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

# Run identity travels as a "/run/{run_id}" path prefix (hierarchical budget ADR);
# the inference gateway strips it (StripRunPrefix) before its own routing. Mirror
# that here so the prefix is transparent to Layer-1 route→scope matching: a
# run-scoped request resolves to the same route (and scope) as the unprefixed
# path. Stripping is safe — the stripped path matches the same route, so no scope
# is bypassed; it only avoids a spurious "no route matched" deny.
_route_path := input.attributes.request.http.path if {
	not startswith(input.attributes.request.http.path, "/run/")
}

_route_path := m[0][1] if {
	startswith(input.attributes.request.http.path, "/run/")
	m := regex.find_all_string_submatch_n(`^/run/[^/]+(/.*)$`, input.attributes.request.http.path, 1)
	count(m) == 1
}

_required_scope := route.required_scope if {
	_is_envoy
	some route in data.routes.route_scopes
	startswith(_route_path, route.path_prefix)
	route.method == input.attributes.request.http.method
}

# Wildcard method match — only applies when no exact method route matches,
# preventing a conflict if both an exact and a wildcard route cover the same path.
_has_exact_method_route if {
	_is_envoy
	some route in data.routes.route_scopes
	startswith(_route_path, route.path_prefix)
	route.method == input.attributes.request.http.method
}

_required_scope := route.required_scope if {
	_is_envoy
	not _has_exact_method_route
	some route in data.routes.route_scopes
	startswith(_route_path, route.path_prefix)
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

# An empty scope claim means "no scopes" (v1 tokens carry none — see the
# fail-closed marker section); leaving it undefined here keeps the token-scope
# leg satisfied instead of producing the never-matching [""] split.
_delegation_scopes := scopes if {
	_is_envoy
	_is_delegated
	raw_scope := _layer1_delegation_claims.scope
	raw_scope != ""
	scopes := split(raw_scope, " ")
}

_delegator_id := input.delegator_id if not _is_envoy

_delegator_id := _layer1_delegation_claims.sub if {
	_is_envoy
	_is_delegated
}

# The delegator's principal type, from the delegation token's
# delegator_principal_type claim (delegated-agent-identity ADR, Decision 3).
# Always HUMAN for v1 tokens — PAT-minted delegations normalise to the person
# at issuance. A token without the claim leaves this undefined, and with it
# the delegator-grant leg: fail closed.
_delegator_principal_type := input.delegator_principal_type if not _is_envoy

_delegator_principal_type := _layer1_delegation_claims.delegator_principal_type if {
	_is_envoy
	_is_delegated
}

# ══ DELEGATION RUN BINDING (Layer 1) ═════════════════════════════════════════
#
# A delegation token is bound to one run via its run_id claim
# (docs/adr/2026-07-23-delegated-agent-identity.md, Decision 4). Where the
# request independently carries run identity — the /run/{run_id} path prefix,
# or the run_id claim in a per-run sandbox pod's platform token — a delegation
# bound to a DIFFERENT run is refused: a token lifted from one run is useless
# anywhere run identity is independently known. Layer 2 receives the identical
# checks from the Go engine (checkDelegationBinding in platform/policy), which
# runs before rego evaluation, so no Layer-2 input plumbing is needed here.

_path_run_id := m[0][1] if {
	_is_envoy
	m := regex.find_all_string_submatch_n(`^/run/([^/]+)/`, input.attributes.request.http.path, 1)
	count(m) == 1
}

# The `default` is load-bearing. Without it, a delegation token carrying no
# run_id claim leaves this rule UNDEFINED, and `undefined != ""` is itself
# undefined rather than true — so both mismatch rules below go undefined,
# _delegation_binding_failed goes undefined (falsy), and every
# `not _delegation_binding_failed` guard passes. An unbound token would be
# accepted on every run. Defaulting to "" makes the absence a definite value
# the rules below can test.
default _delegation_run_id := ""

_delegation_run_id := _layer1_delegation_claims.run_id

# NO run binding on the token at all. Every delegation the identity service
# mints carries run_id (required at mint; NOT NULL on the issuance row), so an
# absent claim means a malformed or hand-rolled token: enforce, never bypass.
#
# This is deliberately NOT the same condition as "run identity is not
# independently knowable here" — a well-formed token on a request with no
# /run/{run_id} prefix and no platform-token run_id claim stays allowed, per
# ADR Decision 4 ("wherever run identity is independently attested or
# asserted"). Conflating the two would deny legitimate unprefixed traffic.
_delegation_run_unbound if {
	_is_envoy
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
	_layer1_claims.orbit.claims.run_id != _delegation_run_id
}

# Aggregate: the delegation failed a binding check and must not be honoured.
# Every delegated allow path and every delegated reason rule guards on this.
_delegation_binding_failed if _delegation_run_mismatch

_delegation_binding_failed if _delegation_run_unbound

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
#   /api/v1/identity/delegation/run/   — identity-proxy retrieves a run's delegation token
#   /api/v1/identity/.well-known/      — OPA fetches JWKS to validate JWTs
#   /api/v1/policy/bundles/            — OPA polls bundle from policy service
#
# All other paths are deny-by-default; unregistered paths without a matching
# route in data.routes.route_scopes will produce deny:no_route_match.

_is_platform_internal_path if {
	_is_envoy
	some prefix in [
		"/api/v1/identity/exchange",
		# Delegation run-token retrieval: XFCC/SVID-authenticated like
		# /identity/exchange; release is gated in-process on the caller's SVID
		# matching the issuance's agent principal (delegated-agent-identity
		# ADR, Decision 3).
		"/api/v1/identity/delegation/run/",
		"/api/v1/identity/.well-known/",
		"/api/v1/policy/bundles/",
		# session-manager → orbit-agents event/state/sandbox callbacks (mTLS SPIFFE authenticated)
		"/internal/runs/",
	]
	startswith(input.attributes.request.http.path, prefix)
}

# ══ DELEGATION FAIL-CLOSED MARKER ════════════════════════════════════════════
#
# A grant carrying delegation_required marks its principal as a delegated agent
# (docs/adr/2026-07-23-delegated-agent-identity.md, Decision 2): the grant is
# the ceiling half of the delegation intersection, never a standing
# entitlement. Every autonomous allow path refuses such a principal, so a
# missing/stripped/expired/revoked delegation token means NO access — not the
# agent's standing access — regardless of how the token was meant to travel.

_principal_requires_delegation if {
	some grant in data.grants.grants
	grant.principal_type == _principal_type
	grant.principal_id == _principal_id
	grant.delegation_required == true
}

# The token-scope leg of the delegated intersection applies only when the
# delegation context carries scopes. v1 delegation tokens carry no scope claim
# (delegated-agent-identity ADR, Decision 3): authority is evaluated live from
# the agent-grant and delegator legs, so an unscoped delegation satisfies this
# leg by construction.
_delegation_has_scopes if {
	is_array(_delegation_scopes)
	count(_delegation_scopes) > 0
}

_delegation_scope_satisfied if not _delegation_has_scopes

_delegation_scope_satisfied if delegation_covers_required_scope

# ══ ALLOW RULES ══════════════════════════════════════════════════════════════

# Platform-internal infrastructure calls (mTLS authenticated, no user JWT).
allow if _is_platform_internal_path

# All principal types — autonomous (no delegation token)
allow if {
	not _is_delegated
	not _principal_requires_delegation
	principal_has_required_scope
}

# All principal types — delegated
allow if {
	_is_delegated
	not _delegation_binding_failed
	principal_has_required_scope
	_delegation_scope_satisfied
	delegator_covers_required_scope
}

# HUMAN principals with an OIDC admin role (PAP access, bootstrapping).
# Must not fire for delegated requests — delegation constrains the effective
# permissions to the intersection of delegator grants and delegation scopes.
allow if {
	not _is_delegated
	not _principal_requires_delegation
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

# ══ DELEGATOR LEG (delegated-agent-identity ADR, Decision 5) ═════════════════
#
# The delegator's side of the intersection is the person's FULL held access —
# direct grants plus role-derived scopes — resolved offline from bundle data:
#
#   data.grants.grants                    direct grants (id AND principal type)
#   data.principal_roles.principal_roles  user→role membership — the bundled
#                                         user_profiles snapshot, the same
#                                         source PAT role attribution uses
#   data.roles.role_scopes                role→scope mappings
#
# plus the vocabulary `requires` projection: a required scope a person is never
# granted (an MCP tool scope) is satisfied on the delegator leg when the person
# covers every scope in its transitive requires expansion — the same edges
# expandRequires folds into agent grants at bundle compile.

# Direct grants. Principal type must match the delegation token's
# delegator_principal_type so two principal types sharing an id string can
# never satisfy this leg.
_delegator_holds(scope) if {
	some grant in data.grants.grants
	grant.principal_type == _delegator_principal_type
	grant.principal_id == _delegator_id
	some gs in grant.scopes
	scope_covers(gs, scope)
}

# Role-derived access, resolved live from bundle data so revoking a role's
# scopes reaches in-flight delegated runs on the next bundle propagation.
_delegator_holds(scope) if {
	_delegator_principal_type == "HUMAN"
	some role in data.principal_roles.principal_roles[_delegator_id]
	some role_scope in data.roles.role_scopes[role]
	scope_covers(role_scope, scope)
}

# Vocabulary requires-adjacency: scope → the upstream scopes it requires.
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

# Transitive requires expansion of the required scope; empty when the required
# scope has no requires edges (ordinary person-holdable scopes).
_required_scope_expansion := graph.reachable(_requires_graph, {_required_scope}) - {_required_scope}

# The delegator covers the required scope directly (grant or role)...
delegator_covers_required_scope if _delegator_holds(_required_scope)

# ...or the required scope projects through its vocabulary requires expansion
# and the delegator covers EVERY scope in it — e.g. mcp:{server}:{tool}:use
# delegates through to the person's connection:* authority.
delegator_covers_required_scope if {
	count(_required_scope_expansion) > 0
	every required in _required_scope_expansion {
		_delegator_holds(required)
	}
}

# ══ EGRESS CONNECTIVITY POSTURE (sandbox CONNECT) ════════════════════════════
#
# A sandbox tool reaches upstreams via HTTPS_PROXY=<gateway>, so its first
# request on the wire is an HTTP CONNECT to the gateway. The CONNECT exposes
# only the destination host:port and the injected platform token — never the
# connection name (registry-only, resolved at the gateway) nor the action
# (inside the TLS tunnel, visible only after the gateway terminates TLS). So
# OPA cannot evaluate connection:<name>:<action> here; it enforces the
# connectivity POSTURE: may this agent reach this host at all?
#
# Posture is expressed as grants, shape-consistent with connection:<name>:<action>:
#   egress:*:connect              open — any host  (≡ the 2-part egress:connect)
#   egress:<host>:connect         whitelist a host (exact)
#   egress:*.<suffix>:connect     whitelist a host and any subdomain depth
#   (no egress grant)             no egress (default deny)
#
# resourceType (egress) and action (connect) match exactly like every other
# scope; only the host segment globs, via _host_matches. Host matching is
# IPv4/DNS only — IPv6 literals never match and fall through to deny.
# See ADR 2026-06-06-jit-credential-brokering-egress-gateway (Amendment 2026-06-07).

_is_connect if {
	_is_envoy
	input.attributes.request.http.method == "CONNECT"
}

# Destination host from the CONNECT :authority, lowercased, port stripped.
# IPv4/DNS only: split on ":" takes the host; an IPv6 literal would split on its
# own colons and never match a sane egress pattern (→ deny), which is intended.
_connect_host := lower(host) if {
	_is_connect
	host := split(input.attributes.request.http.host, ":")[0]
}

# _host_matches(pattern, host): does an egress grant's host pattern cover host?
_host_matches(pattern, host) if pattern == host # exact

_host_matches(pattern, _host) if pattern == "*" # open — any host

_host_matches(pattern, host) if { # "*.suffix" — the apex itself
	startswith(pattern, "*.")
	host == substring(pattern, 2, -1)
}

_host_matches(pattern, host) if { # "*.suffix" — any subdomain depth
	startswith(pattern, "*.")
	endswith(host, substring(pattern, 1, -1))
}

# Host pattern carried by an egress scope. Two accepted shapes:
#   egress:connect            → "*" (open, no host segment)
#   egress:<pattern>:connect  → <pattern>
# Any non-egress scope (e.g. connection:*) yields undefined and is skipped.
_egress_host_pattern(scope) := "*" if scope == "egress:connect"

_egress_host_pattern(scope) := parts[1] if {
	parts := split(scope, ":")
	count(parts) == 3
	parts[0] == "egress"
	parts[2] == "connect"
}

# The principal holds an egress grant whose host pattern covers the CONNECT host.
_principal_egress_allows if {
	some grant in data.grants.grants
	grant.principal_type == _principal_type
	grant.principal_id == _principal_id
	some scope in grant.scopes
	_host_matches(_egress_host_pattern(scope), _connect_host)
}

# Egress posture allow. Connection-level authorization (connection:<name>:<action>)
# + JIT mint happen at the gateway after TLS termination — not here.
# Autonomous-gated: a delegation-required principal is refused here without a
# valid delegation (the delegated egress rule below).
allow if {
	_is_connect
	not _is_delegated
	not _principal_requires_delegation
	_principal_egress_allows
}

# Delegated egress. Egress is the AGENT's connectivity posture — a property of
# the tool, not an authority a person holds — so the delegator is deliberately
# NOT intersected on egress scopes (delegated-agent-identity ADR, Decision 5);
# every action performed OVER the tunnel (connections, tools, models) still
# intersects with the person at the gateway behind it. The agent's own egress
# grant is required exactly as in the autonomous rule, plus a valid delegation
# whose binding checks pass. With the Decision 2 marker this leaves marked
# agents egress-capable only under valid delegation, and unmarked agents
# exactly as today.
allow if {
	_is_connect
	_is_delegated
	not _delegation_binding_failed
	_principal_egress_allows
}

# Egress is HTTPS-only by design. A plain HTTP request (non-CONNECT) to an
# external host is intentionally NOT matched here, so it falls through to
# `default allow := false` (→ 403). This is deliberate, not an omission:
# plaintext egress has no confidentiality/integrity/server-auth on the
# gateway→upstream hop, and for an agent HTTP response-tampering becomes an
# injection vector. Supporting HTTP/80 is future work (passthrough-only, strip
# the platform JWT at the gateway, distinct per-host opt-in) — see ADR
# 2026-06-06-jit-credential-brokering-egress-gateway (Amendment 2).

# ══ REASON RULES ═════════════════════════════════════════════════════════════
#
# reason is for logging only — never returned to callers.
# On allow: which path matched. On deny: which condition failed.

default reason := "deny:no_matching_grant"

reason := concat("", ["egress:", _connect_host]) if {
	_is_connect
	not _is_delegated
	not _principal_requires_delegation
	_principal_egress_allows
}

reason := concat("", ["egress_delegated:", _connect_host]) if {
	_is_connect
	_is_delegated
	not _delegation_binding_failed
	_principal_egress_allows
}

reason := concat("", ["grant:", matched_grant_scope]) if {
	not _is_delegated
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
# request, but the grant requires delegation and none is present.
# Gated on the would-have-allowed legs so it can never conflict with the
# grant:/role:/egress: reasons or the Layer-1 deny reasons.
reason := "deny:delegation_required" if {
	not _is_delegated
	_principal_requires_delegation
	principal_has_required_scope
}

reason := "deny:delegation_required" if {
	_is_connect
	not _is_delegated
	_principal_requires_delegation
	_principal_egress_allows
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
	not _is_delegated
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
# the sandbox platform token attests). Guarded out of every other delegated
# reason rule so complete-rule conflicts are impossible.
reason := "deny:delegation_run_mismatch" if {
	not _is_platform_internal_path
	_layer1_claims
	_is_delegated
	_delegation_run_mismatch
}

# A delegation carrying no run_id claim at all — malformed, since every minted
# delegation is bound to a run. Mutually exclusive with the mismatch reason
# above (which requires a non-empty run_id), so the two can never conflict.
reason := "deny:delegation_run_unbound" if {
	not _is_platform_internal_path
	_layer1_claims
	_is_delegated
	_delegation_run_unbound
}

# Layer 1-specific deny reasons
# An invalid platform token is reported ahead of any delegation-binding
# failure: nothing downstream was evaluated meaningfully without it. The two
# are mutually exclusive — the binding reasons above require _layer1_claims —
# so neither can conflict with the other.
reason := "deny:invalid_token" if {
	_is_envoy
	not _layer1_claims
}

# CONNECT is authorized by egress posture, not route→scope matching, so
# "no route" is meaningless there — and without this guard the rule co-fires
# with the egress reasons on CONNECT, a complete-rule conflict.
reason := "deny:no_route_match" if {
	_is_envoy
	not _is_connect
	not _delegation_binding_failed
	_layer1_claims
	not _required_scope
}

# ══ PRINCIPAL SCOPE PROJECTION ═══════════════════════════════════════════════
#
# Collects the complete set of scopes a principal holds — used by the policy
# gateway's GET /v1/policy/scopes endpoint to project permissions into the
# frontend session. This never filters by required_scope; it returns everything.

# Scopes from direct grants.
principal_all_scopes contains scope if {
	some grant in data.grants.grants
	grant.principal_type == _principal_type
	grant.principal_id == _principal_id
	some scope in grant.scopes
}

# Scopes from OIDC role → scope mappings (including reserved roles).
principal_all_scopes contains scope if {
	_principal_type == "HUMAN"
	some role in _principal_roles
	some scope in data.roles.role_scopes[role]
}

# ══ PRINCIPAL SCOPE DETAIL (EXPLAIN) ═════════════════════════════════════════
#
# Returns scopes grouped by their source — used by the policy gateway's
# GET /v1/policy/scopes/explain endpoint to show administrators where each
# scope comes from. Each entry includes the grant ID or role name.

# Grants: one entry per matching grant with its ID and scopes.
principal_grant_detail contains {"grant_id": grant.id, "scopes": grant.scopes} if {
	some grant in data.grants.grants
	grant.principal_type == _principal_type
	grant.principal_id == _principal_id
}

# Roles: one entry per matching role with the role name and its resolved scopes.
principal_role_detail contains {"role": role, "scopes": role_scopes} if {
	_principal_type == "HUMAN"
	some role in _principal_roles
	role_scopes := data.roles.role_scopes[role]
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
