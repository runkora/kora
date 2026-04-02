# 0035. Policy Gateway and Authorisation Model

**Status:** Accepted  
**Date:** 2026-04-02  
**Deciders:** Engineering Team  
**Technical Story:** Defining how authorisation policy is expressed, stored, distributed, and enforced across all principal-to-resource communication on the Orbit platform — and establishing the architectural model that governs what every principal is permitted to do.

---

## Context

ADR-0008 established a unified principal model. ADR-0009 established delegation tokens and execution context. ADR-0011 established Envoy as the universal data plane and OPA as the policy evaluation engine. ADR-0021 resolved how the Envoy sidecar injects platform tokens transparently.

What none of those ADRs define is the authorisation model itself: how policy is expressed by tenant administrators, how it is stored, compiled, and distributed to enforcement points, and how the enforcement points make allow/deny decisions for any principal-to-resource interaction on the platform.

### Background

The platform currently has no enforcement layer. Any authenticated principal can reach any resource. The inference gateway explicitly defers model access control pending a policy engine. MCP servers apply no access control. The delegation model carries scope claims that are structurally defined but never evaluated.

Orbit serves two classes of principal-to-resource communication that policy must govern:

1. **North-south traffic** — agents, humans, and service accounts calling platform services from outside or at the platform boundary
2. **East-west traffic** — service-to-service calls within the platform

Policy enforcement is not a feature of any individual service. It is a platform-wide concern. The inference gateway, MCP servers, the credential outbound gateway, and all future platform services are consumers of the same authorisation system.

### Problem Statement

We need to define:

1. The authorisation model: how policy is expressed and what the unit of policy is
2. The Policy Administration Point (PAP): where policy is authored, stored, and compiled
3. The Policy Decision Point (PDP): how allow/deny decisions are made and by what component
4. The Policy Enforcement Points (PEP): where in the request path decisions are applied
5. How the delegation scope intersection from ADR-0009 is enforced as a platform invariant
6. The API contract between the PDP and consuming services (Envoy and owned services)
7. Failure behaviour when the PDP is unavailable

### Goals

- **Universal enforcement:** Every request to every platform resource — inference models, MCP servers, external services, platform APIs — passes through policy evaluation. No exceptions.
- **Runtime configurability:** Tenant administrators configure permissions through an API with no deployment required. Policy changes propagate within a polling interval.
- **Delegation integrity:** The delegation scope intersection (`delegation_grant ∩ delegator_scopes ∩ agent_ceiling`) is enforced as an invariant on every delegated request.
- **Developer transparency:** Resource listing endpoints return only entries the calling principal has grants for. Agents never receive lists containing resources they cannot invoke.
- **Consistent evaluation model:** The same grant data, Rego rules, and allow/deny semantics apply at every enforcement point regardless of resource type.
- **No implicit trust:** East-west service-to-service traffic is subject to the same policy evaluation as north-south external traffic.

### Non-Goals

- Explicit deny rules (whitelist-only in this release)
- Fine-grained PAT-to-resource policy (deferred)
- Cross-tenant grants (architecturally disallowed)
- Audit event spine publication (deferred pending spine infrastructure)
- JTI blocklist integration in `AuthMiddleware` and ext_authz token validation (deferred — see Future Concerns)
- Platform-level plan-based policy (SaaS concern, out of scope for open-source release)
- Credential-aware outbound gateway policy (separate feature, ADR-0012)

---

## Decision Drivers

* **Security:** Default deny. No request reaches a resource without an explicit grant. Policy is the enforcement boundary, not a convenience layer.
* **Runtime control:** Operational policy changes must not require deployments. Tenant administrators must be able to grant and revoke access through an API.
* **Latency:** Policy evaluation must not add a synchronous network hop to every request. Local bundle evaluation is required.
* **Consistency:** The same evaluation model applies to all resource types and at all enforcement points. No per-service policy implementations.
* **Go-native:** The PDP is a standard Go binary using existing Orbit platform packages. Evaluation logic is `go test`-able.
* **Delegation correctness:** The scope intersection invariant from ADR-0009 is enforced in the platform layer, not left to individual service implementations.

---

## Considered Options

### Option A: In-application policy enforcement per service

**Description:** Each owned service implements its own authorisation logic using Orbit-provided middleware. Policy rules are either hardcoded or loaded from a shared configuration store per service.

**Pros:**
- No additional infrastructure components
- Service-specific logic can be highly tailored

**Cons:**
- Third-party services (MCP servers, open-source tooling) cannot be instrumented — no consistent enforcement guarantee
- Every owned service must correctly implement and maintain policy logic — misconfiguration has no backstop
- Policy changes require coordinated redeployment across multiple services
- Delegation intersection must be re-implemented in every service — divergence risk
- No unified enforcement model across the platform

### Option B: Synchronous policy API — enforcement points call policy service per request

**Description:** A centralised policy service exposes a check API. Every enforcement point calls it synchronously on every request. No local bundle distribution.

**Pros:**
- Policy changes are immediately effective — no propagation delay
- Single evaluation engine, single policy store

**Cons:**
- Adds a synchronous network hop to every request — the policy service becomes a hard latency and availability dependency for all platform traffic
- At any meaningful scale the policy API becomes a performance bottleneck
- A policy service outage takes down all platform traffic (the same failure mode exists either way — see failure mode decision below — but with this option there is no grace period from a locally cached bundle)
- Rejected in ADR-0011 for exactly these reasons

### Option C: OPA bundle distribution — PAP compiles bundles, PDP evaluates locally

**Description:** A Policy Service (PAP) stores grants in a database and compiles them to OPA bundles on write. A separate ext_authz service (PDP) loads bundles locally and evaluates them in-process. Enforcement points (Envoy and owned services) call the PDP — not the PAP — on the request path.

**Pros:**
- No synchronous dependency on the policy service during request evaluation — bundles are loaded locally
- Policy changes propagate within the polling interval (30 seconds default) — acceptable for operational changes
- The PDP is stateless beyond its cached bundle — horizontally scalable with no coordination
- Consistent with ADR-0011's OPA bundle distribution model
- Emergency revocation (compromised token) is handled by the JTI blocklist mechanism (ADR-0008) independently of bundle propagation

**Cons:**
- Policy changes are not immediately effective — there is a propagation window equal to the polling interval
- Requires the PAP (bundle compilation, bundle serving) as an additional service

**Chosen.** The propagation window is acceptable for operational grant changes. Emergency revocation is handled by the JTI blocklist independently.

---

## The Authorisation Model

### Grant — The Unit of Policy

A **grant** asserts that a named principal within a tenant holds a set of OAuth-style permission scopes. The default posture is **deny** — access is allowed if and only if a matching grant exists. There are no explicit deny rules in this release.

Grants cover all principal types — `AGENT`, `HUMAN`, and `SERVICE`. Services that do not require OPA enforcement simply have no enforcement point attached at deployment; they do not require grants.

```
Grant {
  tenant_id        // tenant scope — cross-tenant grants do not exist
  principal_type   // AGENT | HUMAN | SERVICE
  principal_id     // agent ID, user ID, or service SPIFFE ID
  scopes[]         // ["inference:anthropic:invoke", "mcp:gmail:invoke"]
}
```

Multiple grants for the same principal combine with **OR** semantics — a request is allowed if any granted scope covers the required scope. There is no update operation on grants — modifications are expressed as delete followed by create. This is a deliberate choice: the atomic unit of change is a grant, and partial mutation of a scope list is avoided to prevent ambiguous intermediate states.

### Scope Vocabulary

Permissions are expressed as OAuth-style scope strings following the format:

```
{resource_type}:{path_segment*}:{action}
```

Where `path_segments` are zero or more colon-separated segments that narrow the resource type (provider, server, tool, etc.), and `action` is always the final segment. Examples:

```
inference:invoke                      all inference models, invoke
inference:anthropic:invoke            Anthropic models only, invoke
inference:anthropic:claude:invoke     Claude models only, invoke
inference:list                        all inference models, list

mcp:invoke                            all MCP tools, invoke
mcp:gmail:invoke                      Gmail MCP tools, invoke
mcp:gmail:send_email:invoke           specific tool, invoke
mcp:list                              all MCP servers, list

external:msgraph:mail:read            MS Graph mail, read
external:msgraph:mail:write           MS Graph mail, write
external:msgraph:invoke               all MS Graph, invoke

platform:grants:read                  read grants
platform:grants:write                 create/delete grants
platform:agents:read                  read agent registrations
```

The scope vocabulary is a finite, registered set stored in the Policy Service and compiled into every OPA bundle as a `scope_vocabulary` data document. Adding a new provider or MCP server means registering its scopes in the vocabulary — the grant model itself does not change.

### Scope Hierarchy

A broader scope **implicitly covers** narrower child scopes. `inference:invoke` grants access to any inference resource with any invoke-level action. `inference:anthropic:invoke` grants access only to Anthropic resources.

**Coverage rule:** scope `A` covers scope `B` if and only if:
1. Same `resource_type`
2. Same `action` (last segment)
3. `A`'s path segments are a prefix of `B`'s path segments

```
inference:invoke        covers  inference:anthropic:invoke     ✓ (path [] is prefix of [anthropic])
inference:anthropic:invoke  covers  inference:openai:invoke    ✗ (different path branch)
mcp:invoke              covers  mcp:gmail:send_email:invoke    ✓ (path [] is prefix of [gmail, send_email])
inference:invoke        covers  mcp:gmail:invoke               ✗ (different resource_type)
```

This is evaluated in OPA by parsing the scope string structurally — splitting on `:`, comparing the first segment (resource type), the last segment (action), and testing prefix membership on the middle segments. The `scope_vocabulary` data document is used by the Policy Service for grant validation at write time (rejecting unregistered scopes), not for runtime hierarchy resolution. This means parent scopes automatically cover new resource-specific child scopes as they are added to the catalogue, with no vocabulary update required.

### Required Scope Derivation

When a check request arrives, the ext_authz service derives the `required_scope` from the resource and action provided by the calling service:

```
resource_type: inference, provider: anthropic, model: claude-sonnet-4-6, action: invoke
→ required_scope: inference:anthropic:claude-sonnet-4-6:invoke
```

The calling service provides the resource attributes; the ext_authz service constructs the most specific scope string and passes it to OPA. OPA then checks whether any granted scope covers it via the hierarchy rule. This means the policy system remains decoupled from resource catalogues — new resources are automatically covered by existing parent-level grants.

### Delegation Intersection — Platform Invariant

The delegation scope intersection from ADR-0009 is a **platform invariant** — it is always enforced when a delegation token is present, and is not configurable by tenant administrators.

```
effective_access = delegation_grant_scopes ∩ delegator_grants ∩ agent_grants
```

Where:
- `agent_grants` — the scopes granted to the agent principal (from the OPA bundle)
- `delegator_grants` — the scopes granted to the delegating human principal (from the OPA bundle, looked up by `delegator_id`)
- `delegation_grant_scopes` — the scopes the human explicitly granted to the agent for this session (from the validated delegation token's `scope` claim, split on spaces per RFC 6749)

All three are in the same OAuth scope format. The intersection is evaluated entirely in OPA — for a delegated request, the required scope must be covered by at least one scope from each of the three sets.

**Delegation token scope mapping (relationship to ADR-0009):** ADR-0009 defines the delegation token structure as: `sub` = the human who delegated, `act.sub` = the agent acting on their behalf, `scope` = space-separated scopes granted for this session (RFC 6749). The ext_authz service splits `scope` on spaces to produce `delegation_scopes[]` for the OPA input. The `sub` claim provides the `delegator_id` used to look up the human's grants in the bundle. `act.sub` is the agent — already identified as `principal.id` from the platform token. ADR-0009 should be updated to specify that delegation token scopes use the Orbit OAuth scope vocabulary format defined here.

When no delegation token is present (autonomous agent), only the agent's own grants apply.

---

## Decision

**Chosen Option:** Option C — OPA bundle distribution with a dedicated Policy Service (PAP) and ext_authz service (PDP).

### Three-Layer Architecture (NIST ABAC)

```
┌──────────────────────────────────────────────────────────┐
│  cmd/policy-service  (PAP)                               │
│  • Postgres-backed grant + vocabulary store              │
│  • REST API for grant CRUD (tenant-scoped)               │
│  • Scope vocabulary registration                         │
│  • Compiles per-tenant OPA bundles on grant write        │
│  • Serves GET /v1/policy/bundles/{tenant_id}             │
└────────────────────────┬─────────────────────────────────┘
                         │ per-tenant bundle polling (≤ 30s)
              ┌──────────┴──────────┐
              ▼                     ▼
┌─────────────────────┐   ┌─────────────────────┐
│  cmd/policy-gateway │   │  cmd/policy-gateway │  (replicated)
│  (PDP)              │   │  (PDP)              │
│  • OPA in-process   │   │  • OPA in-process   │
│  • gRPC (Envoy)     │   │  • HTTP (services)  │
│  • token.Verifier   │   │  • token.Verifier   │
│  • fail-closed      │   │  • fail-closed      │
└──────────┬──────────┘   └──────────┬──────────┘
           │                         │
    ┌──────┴──────┐           ┌──────┴────────────┐
    ▼             ▼           ▼                   ▼
 Edge Envoy   Sidecar    Inference GW         MCP Server
 (Layer 1)    Envoy      (Layer 2)            (Layer 2)
              (Layer 1)
```

### Policy Service (PAP)

The Policy Service is a standard Orbit Go service (following ADR-0020) with:

- A Postgres-backed grant store — grants are scoped by `tenant_id` and are never shared across tenants
- A scope vocabulary registry — the finite set of valid OAuth scope strings and their hierarchy metadata
- A REST API for grant CRUD and vocabulary management
- Bundle compilation triggered on every grant write — the affected tenant's bundle is recompiled immediately
- A per-tenant bundle polling endpoint: `GET /v1/policy/bundles/{tenant_id}`

**PAP Authorization — OIDC Role Bootstrap:** The grant management API is itself a platform resource (`platform:grants:write`). To avoid a circular dependency (you need a grant to create grants), access to the PAP API is bootstrapped via OIDC role claims rather than grants. When the identity service federates an OIDC token, role or group claims from the IdP (e.g. an Entra group membership) are embedded in `PlatformClaims.Orbit.Claims["roles"]`. The ext_authz Rego evaluates a `role_scopes` mapping (compiled into the bundle) that translates the `tenant-admin` role to `platform:grants:write`. This is the only documented exception to "grants govern all access" — it is explicit, inspectable in the bundle, and constrained to the `platform:` resource type.

### ext_authz Service (PDP)

The ext_authz service is a Go binary that exposes two server interfaces:

**gRPC — `envoy.service.auth.v3.Authorization`** (for Envoy)  
Used by the edge Envoy and all Envoy sidecars for Layer 1 path-level decisions. This is the Envoy external authorisation protocol — Envoy calls it on every intercepted request.

**HTTP — check and query API** (for owned services)  
Used by owned services for Layer 2 in-application enforcement where payload inspection is required (model ID in a request body, tool name in an MCP invocation) and for resource listing endpoints.

```
POST /v1/policy/check
Request:  { principal, resource { type, ...attributes }, action }
Response: { allow: bool, reason: string }

POST /v1/policy/query
Request:  { principal, resource_type, candidates: [{ id, ...attributes }, ...] }
Response: { allowed: [resource_id, ...] }
```

The `candidates` field in the query request is the full resource list the calling service holds (e.g. all models in the catalogue, all tools on an MCP server). The ext_authz service evaluates each candidate against the principal's grants and returns only those the principal is permitted to access. Without a candidate list, OPA can only return the raw grant scope patterns — not a filtered resource list — because the bundle contains no resource catalogue. The calling service owns the catalogue; the policy service owns the grants.

Both interfaces evaluate against the same locally loaded OPA bundle. There is no difference in policy semantics between the gRPC and HTTP paths.

### Token Validation in ext_authz

The ext_authz service validates platform tokens using `token.Verifier` from `internal/platform/token`. The verifier fetches the JWKS from the identity service's well-known endpoint, caches keys, handles rotation, and verifies signature and expiry. No call to the identity service is made on the request path.

The validated `PlatformClaims` provide `tenant_id`, `principal_type`, and the principal identifier used to construct the OPA input document. Delegation token claims (if present) are validated by the same verifier and provide `delegation_grant_scopes` and `delegator_scopes` for the intersection computation.

### OPA Bundle Structure

Bundles are served **per tenant** — each tenant's enforcement points poll `GET /v1/policy/bundles/{tenant_id}` and receive a bundle containing only that tenant's grants. Cross-tenant data is never present in any bundle. This is enforced at the bundle compilation step, not by Rego rules.

```
{tenant_id}_bundle.tar.gz
├── .manifest
├── policy/
│   └── authz.rego
└── data/
    ├── grants/
    │   └── data.json       # this tenant's grants only
    ├── vocabulary/
    │   └── data.json       # scope vocabulary (compiled from Policy Service registry)
    └── roles/
        └── data.json       # OIDC role → platform scope mappings
```

`data/grants/data.json`:
```json
{
  "grants": [
    {
      "principal_type": "AGENT",
      "principal_id": "email-summarizer",
      "scopes": ["mcp:gmail:invoke", "inference:anthropic:invoke"]
    },
    {
      "principal_type": "HUMAN",
      "principal_id": "user:sarah@example.com",
      "scopes": ["inference:invoke", "mcp:invoke"]
    }
  ]
}
```

`data/vocabulary/data.json` (excerpt):
```json
{
  "scope_vocabulary": {
    "inference:invoke":           { "resource_type": "inference", "path": [],                      "action": "invoke" },
    "inference:anthropic:invoke": { "resource_type": "inference", "path": ["anthropic"],           "action": "invoke" },
    "mcp:invoke":                 { "resource_type": "mcp",       "path": [],                      "action": "invoke" },
    "mcp:gmail:invoke":           { "resource_type": "mcp",       "path": ["gmail"],               "action": "invoke" },
    "mcp:gmail:send_email:invoke":{ "resource_type": "mcp",       "path": ["gmail","send_email"],  "action": "invoke" }
  }
}
```

`data/roles/data.json`:
```json
{
  "role_scopes": {
    "tenant-admin":   ["platform:grants:read", "platform:grants:write", "platform:agents:read"],
    "platform-admin": ["platform:grants:read", "platform:grants:write", "platform:agents:read", "platform:tenants:manage"]
  }
}
```

Core Rego evaluation:
```rego
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
  grant.principal_id   == input.principal.id
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

# Hierarchy: A covers B if same resource_type, same action, A's path is a prefix of B's path.
# Scope strings are parsed structurally (split on ":") so that parent scopes cover resource-
# specific child scopes that have not been pre-registered in the vocabulary. The vocabulary
# is used only for grant validation at write time, not for runtime hierarchy resolution.
scope_covers(a, b) if { a == b }

scope_covers(a, b) if {
  parts_a := split(a, ":")
  parts_b := split(b, ":")
  count(parts_a) >= 2
  count(parts_b) >= 2
  # same resource_type (first segment) and same action (last segment)
  parts_a[0] == parts_b[0]
  parts_a[count(parts_a) - 1] == parts_b[count(parts_b) - 1]
  # A's path segments are a prefix of B's path segments
  path_a := array.slice(parts_a, 1, count(parts_a) - 1)
  path_b := array.slice(parts_b, 1, count(parts_b) - 1)
  count(path_a) <= count(path_b)
  array.slice(path_b, 0, count(path_a)) == path_a
}
```

### OPA Input Document

```json
{
  "principal": {
    "type": "AGENT",
    "tenant_id": "tenant-abc",
    "id": "email-summarizer",
    "roles": []
  },
  "delegation_present": true,
  "delegation_scopes": ["mcp:gmail:invoke", "inference:anthropic:invoke"],
  "delegator_id": "user:sarah@example.com",
  "required_scope": "mcp:gmail:send_email:invoke",
  "resource": {
    "type": "mcp",
    "server": "gmail",
    "tool": "send_email"
  },
  "action": "invoke"
}
```

`required_scope` is derived by the ext_authz service from the resource attributes and action provided by the calling service. `delegation_scopes` is the delegation token's `scope` claim split on spaces. `delegator_id` is the delegation token's `sub` claim (the human who delegated). The `act.sub` claim is the agent acting on the delegator's behalf — which is the calling principal, already identified in `principal.id`. `roles` is populated from the `PlatformClaims.Orbit.Claims` map for HUMAN principals.

### Failure Mode — Fail-Closed

If the ext_authz service is unavailable, Envoy **rejects the request** (503). No request is forwarded without policy evaluation.

This is a deliberate security decision: the policy layer is the enforcement boundary, not a convenience layer. Failing open when the enforcement boundary is unavailable would leave the platform without access control for the duration of the outage. The operational consequence — all platform traffic is blocked during an ext_authz outage — is accepted and mitigated by running ext_authz as a multi-replica deployment with fast startup.

This contrasts with the identityproxy (`failure_mode_allow: true` in ADR-0021) where fail-open is correct because token injection is not the sole enforcement point — destination services enforce authentication independently. The ext_authz service is the sole policy enforcement boundary; there is no backstop.

### Layer 1 `required_scope` Derivation — Route-to-Scope Mapping

Envoy's `CheckRequest` carries the HTTP method, path, and headers — it does not carry the request body. Layer 1 therefore cannot derive the fine-grained scope a specific resource requires (e.g. `inference:anthropic:claude-sonnet-4-6:invoke`). Instead, Layer 1 enforces a **service-level minimum scope** — the broadest scope required to call the service at all.

A route-to-scope mapping is registered per service in the Policy Service and compiled into every OPA bundle as a `route_scopes` data document:

```json
{
  "route_scopes": [
    { "path_prefix": "/v1/inference/", "method": "POST", "required_scope": "inference:invoke" },
    { "path_prefix": "/v1/inference/", "method": "GET",  "required_scope": "inference:list"   },
    { "path_prefix": "/v1/mcp/",       "method": "POST", "required_scope": "mcp:invoke"       },
    { "path_prefix": "/v1/mcp/",       "method": "GET",  "required_scope": "mcp:list"         },
    { "path_prefix": "/api/v1/policy/","method": "*",    "required_scope": "platform:grants:read" }
  ]
}
```

The gRPC `CheckRequest` handler in `server/grpc.go` matches the request path and method against this table to derive `required_scope` before calling OPA. A principal that passes Layer 1 has at least the service-level scope — Layer 2 then enforces the resource-specific scope from the request body. If no route mapping matches, the default is deny.

### Layer 1 and Layer 2 Enforcement

**Layer 1 (Envoy):** Handles path-level decisions — can this principal reach this service at this path? Envoy calls ext_authz via the gRPC interface on every intercepted request. Returns 401 (authentication failure) or 403 (policy denial) before the destination service receives the request. Applies to all services, owned and third-party, with no application code changes required.

**Layer 2 (in-application, owned services only):** Handles payload-aware decisions — can this principal use this specific resource identified in the request body? Owned services call the ext_authz HTTP `check` API before invoking the resource. Listing endpoints call the `query` API to filter their response sets. The calling service is responsible for providing full resource context (type, ID, and any attributes used in constraint evaluation) in the check request.

The two layers are additive. Layer 1 is the universal baseline. Layer 2 is applied by owned services that have resource-level detail the proxy layer cannot inspect. Third-party services receive Layer 1 enforcement only.

---

## Consequences

### Positive

- All platform resources — regardless of service ownership — receive a consistent enforcement guarantee
- Tenant administrators have runtime control over agent access without deployments
- The delegation scope intersection is enforced uniformly across the platform — not left to individual services
- Resource listing endpoints across all services can return accurate, principal-scoped views by calling the `query` API
- The ext_authz service is stateless beyond its bundle cache — horizontal scaling requires no coordination
- OPA evaluation is in-process — no network hop on the request path

### Negative

- Policy changes propagate on the polling interval (default 30 seconds), not immediately. Grant deletions take effect within one interval.
- Fail-closed means an ext_authz outage blocks all platform traffic. Multi-replica deployment is a hard operational requirement.
- Per-tenant bundle compilation on every grant write must remain fast (target: ≤ 500ms). As grant volume per tenant grows, incremental compilation may be required.

### Neutral

- The Policy Service becomes a first-class platform component that must be designed for high availability alongside the ext_authz service
- Owned services must integrate the ext_authz HTTP API for Layer 2 enforcement and listing filtering — this is an explicit integration step for each service

---

## Future Concerns

### JTI Blocklist Integration and Push-Based Bundle Invalidation (Deferred — Address Together)

These two deferrals are related and compound each other. They should be addressed in sequence, JTI first.

**JTI blocklist (immediate revocation of tokens):** The existing `token.Verifier` validates JWT signature and expiry but does not check the Redis JTI blocklist (`orbit:jti:blocklist:{jti}` — implemented in `internal/identity/jti_blocklist_repository.go`). A revoked token remains valid at the ext_authz layer until natural expiry.

Follow-on task:
1. Lift `JTIBlocklistRepository` from `internal/identity/` to `internal/platform/` to avoid a cross-feature dependency
2. Wire the check into the ext_authz token validation path after `token.Verifier.Verify()`
3. Wire the same check into `AuthMiddleware`

**Push-based bundle invalidation (immediate effect of grant changes):** The polling model means grant deletions take effect within the polling interval (≤ 30s), not immediately. For time-critical revocation of an agent's grants, a push mechanism to invalidate cached bundles is needed.

**Why these compound:** Until JTI is wired, a revoked token bypasses ext_authz for up to `token_ttl` regardless of blocklist state. Until push invalidation is implemented, a deleted grant takes effect within 30s rather than immediately. Both gaps exist independently but the combined window — revoked token + stale grant bundle — represents the platform's worst-case revocation latency. Closing JTI first is the higher priority since token TTLs are typically longer than the bundle polling interval.

### Explicit Deny Rules (Deferred)

The whitelist-only model requires more specific grants when exceptions to broad wildcards are needed. As grant patterns become more complex, the absence of deny rules becomes constraining. A follow-on iteration should extend the grant model with a `deny` flag and define deny-overrides-allow evaluation semantics.

### Fine-Grained PAT Policy (Deferred)

PATs currently inherit the issuing principal's full permission set. A follow-on iteration should support scope selection at PAT issuance time, constraining PATs below the issuing principal's ceiling.

### Audit Event Spine (Deferred)

Every policy decision (allow and deny) should be published as a structured event to the Orbit event spine. The event contract — principal, resource, outcome, reason, timestamp, delegation context — should be defined alongside the spine infrastructure ADR.

---

## Implementation Notes

### Binary Split — PAP and PDP are Separate Services

The Policy Service (PAP) and the ext_authz service (PDP) are **separate binaries** with different operational requirements:

- The **PDP** (`policy-gateway`) is on every request path — multi-replica, fail-closed, fast-starting. It is stateless beyond the cached bundle.
- The **PAP** (`policy-service`) handles infrequent administrative writes and bundle compilation. It owns the database and does not need the same replica count or startup budget as the PDP.

Co-locating them would couple these concerns into a single deployment unit and prevent independent scaling.

### Package Structure

```
backend/cmd/policy-service/
└── main.go                         # PAP: config, grant store, vocabulary, bundle compiler + server

backend/cmd/policy-gateway/
└── main.go                         # PDP: config, OPA engine, gRPC + HTTP eval servers

backend/internal/policyservice/
├── config.go
├── grants/
│   ├── repository.go               # Postgres-backed grant CRUD
│   └── repository_test.go
├── vocabulary/
│   ├── repository.go               # Scope vocabulary store and registration
│   └── repository_test.go
├── bundle/
│   ├── compiler.go                 # OPA bundle compilation (per-tenant)
│   ├── compiler_test.go
│   └── server.go                   # GET /v1/policy/bundles/{tenant_id} polling endpoint
└── api/
    └── handlers.go                 # Grant + vocabulary CRUD HTTP handlers

backend/internal/policygw/
├── config.go
├── evaluation/
│   ├── engine.go                   # OPA in-process evaluation, per-tenant bundle loading
│   ├── engine_test.go
│   ├── input.go                    # OPA input document construction + required_scope derivation
│   └── hierarchy.go                # Scope hierarchy resolution helpers
├── server/
│   ├── grpc.go                     # envoy.service.auth.v3.Authorization gRPC server
│   ├── http.go                     # POST /v1/policy/check and /v1/policy/query
│   └── server_test.go
└── token/
    └── validation.go               # token.Verifier wiring + delegation token parsing
```

### Key Dependencies

- `github.com/open-policy-agent/opa` — OPA Go library for in-process bundle loading and policy evaluation
- `github.com/envoyproxy/go-control-plane/envoy v1.37.0` — already in `go.mod`; provides `envoy.service.auth.v3` proto types
- `github.com/jackc/pgx/v5` — already in `go.mod`; Postgres grant store
- `internal/platform/token` — `token.Verifier` and `PlatformClaims` for token validation and principal extraction
- `internal/platform/logging`, `internal/platform/observability` — standard Orbit logging and OTel integration

### Migration Path

No migration required. The policy service is additive. Existing services continue to operate unchanged. Layer 2 integration in the inference gateway and MCP servers is implemented as a follow-on integration step per service. Until a service integrates Layer 2, Layer 1 (Envoy) provides the enforcement baseline.

---

## Validation

### Success Metrics

- 100% of requests to platform services pass through a Layer 1 ext_authz evaluation point
- `GET /v1/models` and MCP tool listings return only resources matching the calling principal's grants
- Grant deletion propagates to all enforcement points within the configured polling interval
- No cross-tenant grant evaluation — validated by bundle partitioning tests and penetration testing
- Policy evaluation p95 latency ≤ 10ms (local OPA, no network hop)

### Monitoring

- `policygw_bundle_compile_duration_seconds` — histogram: bundle compilation latency per tenant
- `policygw_bundle_last_sync_timestamp` — gauge per enforcement point: last successful bundle load
- `policygw_eval_duration_seconds` — histogram: OPA evaluation latency per decision
- `policygw_decisions_total{outcome="allow|deny"}` — counter: allow/deny rate
- `policygw_token_validation_errors_total` — counter: token validation failures at ext_authz

---

## Related Decisions

- [ADR-0008](0008-unified-identity-model.md) — Unified principal model and platform session token validated by ext_authz
- [ADR-0009](0009-agent-execution-flow-patterns-and-delegation.md) — Delegation tokens; the intersection invariant enforced by this ADR
- [ADR-0011](0011-universal-gateway-policy-enforcement.md) — Establishes Envoy as the data plane and OPA bundle distribution as the evaluation model; this ADR defines the authorisation model and PDP/PAP components
- [ADR-0021](0021-envoy-ext-proc-sidecar-token-injection.md) — Identity proxy ext_proc sidecar; the fail-open vs fail-closed contrast is documented there
- [ADR-0023](0023-inference-gateway-architecture.md) — Inference gateway; integrates ext_authz HTTP API for Layer 2 model-level enforcement
- [ADR-0013](0013-mcp-server-registration-deployment-and-governance.md) — MCP server governance
- [ADR-0014](0014-scope-gated-tool-exposure-via-mcp.md) — MCP tool filtering; the `query` API defined here is the mechanism

---

## References

- [OPA Go API](https://www.openpolicyagent.org/docs/latest/integration/#go-integration)
- [OPA Bundle distribution](https://www.openpolicyagent.org/docs/latest/management-bundles/)
- [Envoy ext_authz filter](https://www.envoyproxy.io/docs/envoy/latest/configuration/http/http_filters/ext_authz_filter)
- [NIST ABAC reference model](https://nvlpubs.nist.gov/nistpubs/specialpublications/nist.sp.800-162.pdf)
- [PRD: Policy Gateway](../prd/policy-gateway.md)

---

## Notes

The policy model is intentionally simple in this release. The grant is the unit of policy, the default is deny, and the evaluation model is "does any granted scope cover the required scope, considering hierarchy?" This is sufficient for all stated use cases. Complexity is deferred to explicit follow-on items (deny rules, PAT policy, audit spine) rather than built speculatively.

Scope strings use a colon-separated hierarchy (`inference:anthropic:invoke`). No wildcards appear in scope strings — breadth is expressed by granting a higher-level scope. Parent-child coverage is evaluated in OPA via the `scope_vocabulary` data document compiled into each bundle. The vocabulary is the authoritative finite set of valid scopes; grants referencing unregistered scopes are rejected at write time by the Policy Service.

The delegation intersection is enforced as a platform invariant in Rego — three parallel coverage checks (delegation token scopes, delegator grants, agent grants) that must all pass. It is not configurable by tenants and cannot be bypassed.

**Last Updated:** 2026-04-02
