# Policy Gateway and Authorisation Model

**Status:** Accepted (revised 2026-04-13)  
**Date:** 2026-04-14
**Deciders:** Engineering Team  
**Technical Story:** Defining how authorisation policy is expressed, stored, distributed, and enforced across all principal-to-resource communication on the kora platform — and establishing the architectural model that governs what every principal is permitted to do.

---

## Context

[unified-identity-model](2026-03-05-unified-identity-model.md) established a unified principal model. [agent-execution-flow-patterns-and-delegation](2026-03-05-agent-execution-flow-patterns-and-delegation.md) established delegation tokens and execution context. [universal-gateway-policy-enforcement](2026-03-05-universal-gateway-policy-enforcement.md) established Envoy as the universal data plane and OPA as the policy evaluation engine. [envoy-ext-proc-sidecar-token-injection](2026-03-21-envoy-ext-proc-sidecar-token-injection.md) resolved how the Envoy sidecar injects platform tokens transparently.

What none of those ADRs define is the authorisation model itself: how policy is expressed by tenant administrators, how it is stored, compiled, and distributed to enforcement points, and how the enforcement points make allow/deny decisions for any principal-to-resource interaction on the platform.

### Background

The platform currently has no enforcement layer. Any authenticated principal can reach any resource. The inference gateway explicitly defers model access control pending a policy engine. MCP servers apply no access control. The delegation model carries scope claims that are structurally defined but never evaluated.

kora serves two classes of principal-to-resource communication that policy must govern:

1. **North-south traffic** — agents, humans, and service accounts calling platform services from outside or at the platform boundary
2. **East-west traffic** — service-to-service calls within the platform

Policy enforcement is not a feature of any individual service. It is a platform-wide concern. The inference gateway, MCP servers, the credential outbound gateway, and all future platform services are consumers of the same authorisation system.

### Problem Statement

We need to define:

1. The authorisation model: how policy is expressed and what the unit of policy is
2. The Policy Administration Point (PAP): where policy is authored, stored, and compiled
3. The Policy Decision Point (PDP): how allow/deny decisions are made and by what component
4. The Policy Enforcement Points (PEP): where in the request path decisions are applied
5. How the delegation scope intersection from [agent-execution-flow-patterns-and-delegation](2026-03-05-agent-execution-flow-patterns-and-delegation.md) is enforced as a platform invariant
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
- Credential-aware outbound gateway policy (separate feature, [credential-aware-outbound-gateway](2026-03-05-credential-aware-outbound-gateway.md))

---

## Decision Drivers

* **Security:** Default deny. No request reaches a resource without an explicit grant. Policy is the enforcement boundary, not a convenience layer.
* **Runtime control:** Operational policy changes must not require deployments. Tenant administrators must be able to grant and revoke access through an API.
* **Latency:** Policy evaluation must not add a synchronous network hop to every request. OPA must be co-located at every enforcement point — either as a process-embedded library or as a loopback sidecar — so that evaluation is a local operation with no cross-pod call.
* **Consistency:** The same evaluation model applies to all resource types and at all enforcement points. No per-service policy implementations.
* **Go-native:** The Layer 2 PDP is a standard Go library (`internal/platform/policy`) using existing kora platform packages. Evaluation logic is `go test`-able.
* **Delegation correctness:** The scope intersection invariant from [agent-execution-flow-patterns-and-delegation](2026-03-05-agent-execution-flow-patterns-and-delegation.md) is enforced in the platform layer, not left to individual service implementations.

---

## Considered Options

### Option A: In-application policy enforcement per service

**Description:** Each owned service implements its own authorisation logic using kora-provided middleware. Policy rules are either hardcoded or loaded from a shared configuration store per service.

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
- Rejected in [universal-gateway-policy-enforcement](2026-03-05-universal-gateway-policy-enforcement.md) for exactly these reasons

### Option C: OPA bundle distribution — PAP compiles bundles, PDP evaluates locally

**Description:** A Policy Service (PAP) stores grants in a database and compiles them to OPA bundles on write. A separate ext_authz service (PDP) loads bundles locally and evaluates them in-process. Enforcement points (Envoy and owned services) call the PDP — not the PAP — on the request path.

**Pros:**
- No synchronous dependency on the policy service during request evaluation — bundles are loaded locally
- Policy changes propagate within the polling interval (30 seconds default) — acceptable for operational changes
- The PDP is stateless beyond its cached bundle — horizontally scalable with no coordination
- Consistent with [universal-gateway-policy-enforcement](2026-03-05-universal-gateway-policy-enforcement.md)'s OPA bundle distribution model
- Emergency revocation (compromised token) is handled by the JTI blocklist mechanism ([unified-identity-model](2026-03-05-unified-identity-model.md)) independently of bundle propagation

**Cons:**
- Policy changes are not immediately effective — there is a propagation window equal to the polling interval
- Requires the PAP (bundle compilation, bundle serving) as an additional service

**Chosen.** The propagation window is acceptable for operational grant changes. Emergency revocation is handled by the JTI blocklist independently.

**Revised enforcement topology (2026-04-10):** The original decision described the PDP as a central multi-replica `policy-gateway` deployment that Envoy and owned services call over the network. This was revised after implementation review: calling a remote pod for every policy decision is a synchronous network hop on every request path — the same latency and availability problem as Option B, just shifted one level. The decision is updated to require OPA co-located at each enforcement point. See the Decision section for the revised topology.

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
  scopes[]         // ["inference:invoke", "inference:anthropic:use", "mcp:gmail:invoke"]
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
inference:invoke                           call the inference endpoints (Layer 1 route gate)
inference:use                              use any model across all providers
inference:anthropic:use                    use any Anthropic model
inference:anthropic:claude-sonnet-4:use    use a specific model by family alias
inference:list                             list the models the caller can use

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

**Inference scope design — two distinct actions:** The inference resource uses two actions to keep the route gate orthogonal to model access:

- **`invoke`** — call the inference endpoints. Only `inference:invoke` and `inference:list` use this action. Required by Layer 1 to reach `/v1/chat/completions`, `/v1/messages`, or `/v1/models`.
- **`use`** — use a model. All model and family-level scopes use this action. Required by Layer 2 to use a specific model in a request body, regardless of provider.

Because `scope_covers` requires matching action (last segment), `inference:invoke` never covers `inference:anthropic:use` — the two checks are entirely independent. A principal needs both to make a successful inference request: `inference:invoke` to pass the route gate, and at least one `inference:*:use` scope that covers the requested model's family.

The scope vocabulary is a finite, registered set stored in the Policy Service and compiled into every OPA bundle as a `scope_vocabulary` data document. Static platform scopes are registered by services at startup via the `routescopes` client ([dynamic-route-scope-registration](2026-04-14-dynamic-route-scope-registration.md)). Inference model and family scopes (`inference:{family}:{model}:use`) are registered **dynamically** by the inference gateway on startup after catalog warm-up, and again whenever `GET /v1/models` refreshes the catalog — no migration or manual registration is required as providers add or remove models.

### Scope Hierarchy

A broader scope **implicitly covers** narrower child scopes. `inference:use` grants access to any model. `inference:anthropic:use` grants access only to Anthropic models.

**Coverage rule:** scope `A` covers scope `B` if and only if:
1. Same `resource_type`
2. Same `action` (last segment)
3. `A`'s path segments are a prefix of `B`'s path segments

```
inference:use               covers  inference:anthropic:use              ✓ (path [] is prefix of [anthropic])
inference:anthropic:use     covers  inference:anthropic:claude-sonnet-4:use  ✓ (prefix match)
inference:anthropic:use     covers  inference:google:use                 ✗ (different path branch)
mcp:invoke                  covers  mcp:gmail:send_email:invoke          ✓ (path [] is prefix of [gmail, send_email])
inference:invoke            covers  inference:anthropic:use              ✗ (different action: invoke ≠ use)
inference:use               covers  mcp:gmail:invoke                     ✗ (different resource_type)
```

This is evaluated in OPA by parsing the scope string structurally — splitting on `:`, comparing the first segment (resource type), the last segment (action), and testing prefix membership on the middle segments. The `scope_vocabulary` data document is used by the Policy Service for grant validation at write time (rejecting unregistered scopes), not for runtime hierarchy resolution. This means parent scopes automatically cover new resource-specific child scopes as they are added to the catalog, with no vocabulary update required.

### Required Scope Derivation

When a check request arrives, the required scope is derived from the resource type, path segments, and action:

**Layer 1** (route gate — from `data.routes.route_scopes` in the bundle):
```
path: /v1/chat/completions, method: POST  →  required_scope: inference:invoke
path: /v1/messages,          method: POST  →  required_scope: inference:invoke
path: /v1/models,            method: GET   →  required_scope: inference:list
```

**Layer 2** (model access — constructed by the inference gateway from the resolved model):
```
family: anthropic, alias: claude-sonnet-4, action: use
→ required_scope: inference:anthropic:claude-sonnet-4:use
```

The calling service resolves the requested model string to its canonical `(family, alias)` via the catalog and calls `platform/policy.Evaluator.Check()`. OPA checks whether any granted scope covers the required scope via the hierarchy rule. A principal with `inference:anthropic:use` passes this check for any Anthropic model; a principal with only `inference:google:use` is denied.

### Delegation Intersection — Platform Invariant

The delegation scope intersection from [agent-execution-flow-patterns-and-delegation](2026-03-05-agent-execution-flow-patterns-and-delegation.md) is a **platform invariant** — it is always enforced when a delegation token is present, and is not configurable by tenant administrators.

```
effective_access = delegation_grant_scopes ∩ delegator_grants ∩ agent_grants
```

Where:
- `agent_grants` — the scopes granted to the agent principal (from the OPA bundle)
- `delegator_grants` — the scopes granted to the delegating human principal (from the OPA bundle, looked up by `delegator_id`)
- `delegation_grant_scopes` — the scopes the human explicitly granted to the agent for this session (from the validated delegation token's `scope` claim, split on spaces per RFC 6749)

All three are in the same OAuth scope format. The intersection is evaluated entirely in OPA — for a delegated request, the required scope must be covered by at least one scope from each of the three sets.

**Delegation token scope mapping (relationship to [agent-execution-flow-patterns-and-delegation](2026-03-05-agent-execution-flow-patterns-and-delegation.md)):** [agent-execution-flow-patterns-and-delegation](2026-03-05-agent-execution-flow-patterns-and-delegation.md) defines the delegation token structure as: `sub` = the human who delegated, `act.sub` = the agent acting on their behalf, `scope` = space-separated scopes granted for this session (RFC 6749). The ext_authz service splits `scope` on spaces to produce `delegation_scopes[]` for the OPA input. The `sub` claim provides the `delegator_id` used to look up the human's grants in the bundle. `act.sub` is the agent — already identified as `principal.id` from the platform token. [agent-execution-flow-patterns-and-delegation](2026-03-05-agent-execution-flow-patterns-and-delegation.md) should be updated to specify that delegation token scopes use the kora OAuth scope vocabulary format defined here.

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
│  • Serves GET /api/v1/policy/bundles/{tenant_id}         │
└────────────────────────┬─────────────────────────────────┘
                         │ per-tenant bundle polling (≤ 30s, background)
          ┌──────────────┼──────────────────────┐
          ▼              ▼                       ▼
┌──────────────────┐  ┌───────────────────────────────────┐
│  OPA Envoy       │  │  internal/platform/policy         │
│  Plugin sidecar  │  │  (PDP — Go library)               │
│  (Layer 1 PDP)   │  │  (Layer 2 PDP)                    │
│                  │  │                                   │
│  • OPA in-proc   │  │  • OPA in-process                 │
│  • ext_authz     │  │  • Evaluator interface            │
│    gRPC on       │  │  • Check() / Query()              │
│    localhost:9191│  │  • token.Verifier                 │
│  • polls bundles │  │  • polls bundles directly         │
│    directly      │  │  • embedded in service process    │
└────────┬─────────┘  └──────────────┬────────────────────┘
         │ loopback                   │ in-process call
         ▼                            ▼
 Envoy sidecar              Inference GW / MCP Server
 (SIDECAR_OUTBOUND)         (owned service process)
 ext_authz filter
```

**Layer 1 (coarse-grained, Envoy):** The OPA Envoy plugin runs as a sidecar container injected into every agent pod. Envoy's outbound ext_authz filter calls the OPA sidecar on `localhost:9191` — a same-pod loopback call with no cross-pod network hop. The OPA sidecar polls the policy-service bundle endpoint directly in the background. This applies to all outbound requests from agent pods regardless of destination service.

**Layer 2 (fine-grained, in-process):** `internal/platform/policy` is a Go library that any owned service imports. It embeds OPA in the service process, polls the policy-service bundle endpoint in the background, and exposes `Check()` and `Query()` methods. Evaluation is a pure in-memory operation with zero network involvement on the hot path. Used by the inference gateway for model-level filtering and per-request resource checks, and by any future owned service requiring payload-aware policy decisions.

### Policy Service (PAP)

The Policy Service is a standard kora Go service (following [multi-service-backend-deployment-structure](2026-03-19-multi-service-backend-deployment-structure.md)) with:

- A Postgres-backed grant store — grants are scoped by `tenant_id` and are never shared across tenants
- A scope vocabulary registry — the finite set of valid OAuth scope strings and their hierarchy metadata
- A REST API for grant CRUD and vocabulary management
- Bundle compilation triggered on every grant write — the affected tenant's bundle is recompiled immediately
- A per-tenant bundle polling endpoint: `GET /v1/policy/bundles/{tenant_id}`

**PAP Authorization — OIDC Role Bootstrap:** The grant management API is itself a platform resource (`platform:grants:write`). To avoid a circular dependency (you need a grant to create grants), access to the PAP API is bootstrapped via OIDC role claims rather than grants. When the identity service federates an OIDC token, role or group claims from the IdP (e.g. an Entra group membership) are embedded in `PlatformClaims.Kora.Claims["roles"]`. The ext_authz Rego evaluates a `role_scopes` mapping (compiled into the bundle) that translates the `tenant-admin` role to `platform:grants:write`. This is the only documented exception to "grants govern all access" — it is explicit, inspectable in the bundle, and constrained to the `platform:` resource type.

### Layer 1 PDP — OPA Envoy Plugin Sidecar

The OPA Envoy plugin (`openpolicyagent/opa:latest-envoy`) is injected as a sidecar container into every agent pod via an Istio injection template, following the same pattern as the identity-proxy and SPIRE sidecars. The template is referenced by the pod annotation:

```yaml
inject.istio.io/templates: "sidecar,spire,identity-proxy,opa-policy"
```

The OPA sidecar:
- Runs the OPA Envoy plugin which natively implements the `envoy.service.auth.v3.Authorization` gRPC protocol
- Listens on `localhost:9191` — same-pod loopback only, not exposed to the mesh
- Polls the policy-service bundle endpoint directly in the background
- Evaluates `data.orbit.authz.allow` against the locally loaded bundle on every Check call

An EnvoyFilter in `istio-system` wires the outbound ext_authz filter on agent pods to `localhost:9191`. It is inserted **after** the identity-proxy ext_proc filter so the platform token is already present in the `Authorization` header when OPA receives the CheckRequest:

```yaml
workloadSelector:
  labels:
    orbit.io/identity-proxy: "enabled"
configPatches:
  - applyTo: HTTP_FILTER
    match:
      context: SIDECAR_OUTBOUND
      subFilter:
        name: "envoy.filters.http.ext_proc"    # after token injection
    patch:
      operation: INSERT_AFTER
      value:
        name: envoy.filters.http.ext_authz
        typed_config:
          grpc_service:
            envoy_grpc:
              cluster_name: opa_policy_local   # static STATIC cluster → 127.0.0.1:9191
          timeout: 10ms                        # loopback — tight budget
          failure_mode_allow: false            # fail-closed
```

### Agent Pod Injection — MutatingAdmissionWebhook

The identity-proxy and OPA sidecars are a **coupled pair** for Layer 1 enforcement and must be injected together:

- **identity-proxy** intercepts outbound requests and injects the `Authorization: Bearer <platform-token>` header using the pod's SPIFFE SVID
- **OPA sidecar** receives the Envoy `CheckRequest` and validates the now-present JWT against the tenant's grant bundle

Without identity-proxy, the `Authorization` header is absent and OPA denies all requests with `deny:invalid_token`. Without OPA, the ext_authz filter has no handler and requests are blocked (fail-closed). They must always be present together.

The operator registers a `MutatingWebhookConfiguration` that automatically injects the required labels and annotations onto every pod created in a tenant namespace (any namespace labelled `orbit.io/role: "tenant"`). This ensures Layer 1 enforcement is universal — no per-pod opt-in, no accidental gaps.

**Injected by the webhook on every tenant pod:**

| Field | Value | Purpose |
|---|---|---|
| label `orbit.io/identity-proxy` | `"enabled"` | Activates the identity-proxy EnvoyFilter (ext_proc) |
| label `orbit.io/opa-policy` | `"enabled"` | Activates the OPA ext_authz EnvoyFilter |
| annotation `orbit.io/tenant-id` | `<tenant-name>` | Tells the OPA sidecar which bundle to fetch from the policy service |
| annotation `inject.istio.io/templates` | `"sidecar,spire,identity-proxy,opa-policy"` | Injects all four Istio sidecars; merged with any templates already set |

The tenant name is read from the namespace label `orbit.io/tenant`, which is set by the operator's `TenantReconciler` at namespace provisioning time.

**Failure policy: `Ignore`**

If the webhook is temporarily unavailable (e.g. operator restart), pods start without the enforcement labels and Layer 1 sidecars are not injected. Layer 2 (in-process OPA in owned services) continues to enforce independently and is the authoritative check — so no security gap exists at the Layer 2 boundary. The Layer 1 gap is bounded by the webhook's recovery time, which is covered by the operator's readiness probe.

**SPIFFE identity prerequisite**

The identity-proxy exchanges the pod's SPIFFE SVID for a platform token. For this to succeed, the pod must match a `ClusterSPIFFEID` selector — either `spiffe.io/agent: <name>` (autonomous agents) or `spiffe.io/service: <name>` (tenant-deployed services). Without a SPIFFE identity, the identity-proxy fails its readiness probe and the pod never becomes Ready. This is the correct fail-closed behaviour: an agent without a declared identity cannot reach platform services.

**Implementation:** `internal/operator/pod_webhook.go` — `PodMutator` registered with the controller-runtime webhook server on port 9443. TLS for the webhook server is provisioned by the Helm chart (`charts/orbit-operator/templates/webhook.yaml`) using Helm's `genCA`/`genSignedCert` with `lookup` to prevent cert regeneration on upgrade.

### Layer 2 PDP — `internal/platform/policy` Go Library

`internal/platform/policy` is the single reusable entry point for any Go service requiring fine-grained in-process policy evaluation. It is not a separate binary — it is a library embedded in the calling service's process.

```go
type Evaluator interface {
    Check(ctx context.Context, input CheckInput) (Result, error)
    Query(ctx context.Context, input QueryInput) ([]string, error)
    Start(ctx context.Context) // begins background bundle polling
}

func NewEvaluator(bundleEndpoint string, pollInterval time.Duration, logger logging.Logger) Evaluator
```

The `Evaluator` implementation:
- Embeds OPA in-process using the OPA Go library
- Polls the policy-service bundle endpoint in the background on the configured interval
- Caches a compiled `PreparedEvalQuery` per tenant
- `Check()` and `Query()` are pure in-memory operations — no network call on the hot path

`CheckInput` carries `*token.PlatformClaims` (already validated by the service's auth middleware), an optional `*DelegationClaims`, and the resource attributes. The library constructs the OPA input document internally. Callers have no knowledge of OPA, bundle format, or polling mechanics.

**`cmd/policy-gateway` utility server (non-enforcement):**  
The `policy-gateway` binary continues to exist but is no longer on any enforcement hot path. It hosts the HTTP `Check`/`Query` API as a utility interface for non-Go consumers (services in other languages, the frontend BFF, operator tooling). It also embeds `internal/platform/policy` for its own evaluation. It is not a dependency of any Go platform service for enforcement purposes.

### Token Validation

**Layer 1 (OPA sidecar):** OPA's built-in JWT functions parse and validate the `Authorization` header token within the Rego evaluation itself. The JWKS endpoint is configured in the OPA sidecar config and cached after first fetch. No call to the identity service is made on the request path.

**Layer 2 (`platform/policy`):** The calling service's auth middleware has already validated the platform token before calling `Check()` or `Query()`. `CheckInput` accepts `*token.PlatformClaims` — the pre-validated claims — not a raw token string. No re-verification of the JWT signature occurs in the evaluator. This is intentional: the service boundary (auth middleware) is the verification point; the evaluator receives already-trusted claims.

### OPA Bundle Structure

Bundles are served **per tenant** — each tenant's enforcement points poll `GET /v1/policy/bundles/{tenant_id}` and receive a bundle containing only that tenant's grants. Cross-tenant data is never present in any bundle. This is enforced at the bundle compilation step, not by Rego rules.

```
{tenant_id}_bundle.tar.gz
├── .manifest
├── policy/
│   └── authz.rego
├── grants/
│   └── data.json           # this tenant's grants only  → data.grants
├── vocabulary/
│   └── data.json           # scope vocabulary           → data.vocabulary
├── roles/
│   └── data.json           # OIDC role → scope mappings → data.roles
└── routes/
    └── data.json           # path+method → scope map    → data.routes
```

Note: data files at `<dir>/data.json` are loaded into `data.<dir>` in OPA's namespace. The `data/` prefix must not be used — `data/grants/data.json` would load into `data.data.grants`, breaking all Rego references to `data.grants.*`.

`data/grants/data.json`:
```json
{
  "grants": [
    {
      "principal_type": "AGENT",
      "principal_id": "email-summarizer",
      "scopes": ["inference:invoke", "inference:anthropic:use", "mcp:gmail:invoke"]
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
    "inference:invoke":                    { "resource_type": "inference", "path": [],                               "action": "invoke" },
    "inference:use":                       { "resource_type": "inference", "path": [],                               "action": "use"    },
    "inference:anthropic:use":             { "resource_type": "inference", "path": ["anthropic"],                    "action": "use"    },
    "inference:anthropic:claude-sonnet-4:use": { "resource_type": "inference", "path": ["anthropic","claude-sonnet-4"], "action": "use" },
    "mcp:invoke":                          { "resource_type": "mcp",       "path": [],                               "action": "invoke" },
    "mcp:gmail:invoke":                    { "resource_type": "mcp",       "path": ["gmail"],                        "action": "invoke" },
    "mcp:gmail:send_email:invoke":         { "resource_type": "mcp",       "path": ["gmail","send_email"],           "action": "invoke" }
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
  "delegation_scopes": ["mcp:gmail:invoke", "inference:invoke", "inference:anthropic:use"],
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

`required_scope` is derived by the ext_authz service from the resource attributes and action provided by the calling service. `delegation_scopes` is the delegation token's `scope` claim split on spaces. `delegator_id` is the delegation token's `sub` claim (the human who delegated). The `act.sub` claim is the agent acting on the delegator's behalf — which is the calling principal, already identified in `principal.id`. `roles` is populated from the `PlatformClaims.Kora.Claims` map for HUMAN principals.

### Failure Mode — Fail-Closed

If the ext_authz service is unavailable, Envoy **rejects the request** (503). No request is forwarded without policy evaluation.

This is a deliberate security decision: the policy layer is the enforcement boundary, not a convenience layer. Failing open when the enforcement boundary is unavailable would leave the platform without access control for the duration of the outage. The operational consequence — all platform traffic is blocked during an ext_authz outage — is accepted and mitigated by running ext_authz as a multi-replica deployment with fast startup.

This contrasts with the identityproxy (`failure_mode_allow: true` in [envoy-ext-proc-sidecar-token-injection](2026-03-21-envoy-ext-proc-sidecar-token-injection.md)) where fail-open is correct because token injection is not the sole enforcement point — destination services enforce authentication independently. The ext_authz service is the sole policy enforcement boundary; there is no backstop.

### Layer 1 `required_scope` Derivation — Route-to-Scope Mapping

Envoy's `CheckRequest` carries the HTTP method, path, and headers — it does not carry the request body. Layer 1 therefore cannot derive the fine-grained scope a specific resource requires (e.g. which model is being called). Instead, Layer 1 enforces a **service-level minimum scope** — the broadest scope required to call the service at all.

The route-to-scope mapping is compiled into every OPA bundle by the policy-service as `routes/data.json` (accessible in Rego as `data.routes.route_scopes`). Services register their own routes at startup via the `routescopes` client ([dynamic-route-scope-registration](2026-04-14-dynamic-route-scope-registration.md)) — no policy-service code change is required to add a new service or route.

Inference gateway routes as registered in production:

```json
{
  "route_scopes": [
    { "path_prefix": "/v1/chat/completions", "method": "POST", "required_scope": "inference:invoke" },
    { "path_prefix": "/v1/messages",         "method": "POST", "required_scope": "inference:invoke" },
    { "path_prefix": "/v1/models",           "method": "GET",  "required_scope": "inference:list"  }
  ]
}
```

The Rego for Layer 1 derives `required_scope` from `data.routes.route_scopes` using the `input.parsed_path` and `input.parsed_body.method` values provided by the OPA Envoy plugin from the Envoy `CheckRequest`. A principal that passes Layer 1 has at least the service-level scope — Layer 2 then enforces the resource-specific scope from the request body. If no route mapping matches, the default deny applies.

### Layer 1 and Layer 2 Enforcement

**Layer 1 (Envoy outbound, OPA sidecar):** Handles path-level decisions — can this principal reach this service at this path? The OPA Envoy plugin sidecar receives the Envoy `CheckRequest` on `localhost:9191` and evaluates `data.orbit.authz.allow` against the locally loaded bundle. Returns 403 before the request leaves the calling pod. Applies to all outbound requests from agent pods, covering all destination services — owned and third-party — with no application code changes required at the destination.

**Layer 2 (in-process, owned services only):** Handles payload-aware decisions — can this principal use this specific resource identified in the request body? Owned services call `platform/policy.Evaluator.Check()` or `.Query()` inline in their handler code. Evaluation is pure in-memory against the service's own locally loaded bundle. Listing endpoints call `.Query()` to filter their response sets, passing the full resource catalog as candidates. The service owns the catalog; the policy system owns the grants.

The two layers are additive. Layer 1 is the universal baseline — no cross-pod hop, enforced for all traffic. Layer 2 is applied by owned services that have resource-level detail the proxy cannot inspect — also no cross-pod hop, in-process. Third-party services receive Layer 1 enforcement only.

---

## Consequences

### Positive

- All platform resources — regardless of service ownership — receive a consistent enforcement guarantee
- Tenant administrators have runtime control over agent access without deployments
- The delegation scope intersection is enforced uniformly across the platform — not left to individual services
- Resource listing endpoints across all services can return accurate, principal-scoped views via `Evaluator.Query()`
- Zero network hops at both enforcement layers — Layer 1 is a same-pod loopback to the OPA sidecar; Layer 2 is a pure in-process call
- The OPA sidecar and the `platform/policy` library are both stateless beyond their bundle cache — independently scalable with no coordination
- Policy evaluation latency is sub-millisecond at both layers with no cross-pod dependency on the hot path

### Negative

- Policy changes propagate on the polling interval (default 30 seconds), not immediately. Grant deletions take effect within one interval.
- Fail-closed at Layer 1 means OPA sidecar unavailability (e.g. startup race before bundle is loaded) blocks outbound requests from agent pods. The OPA readiness probe must gate pod readiness — OPA must have loaded at least one bundle before the pod accepts traffic.
- Every agent pod carries an additional OPA sidecar container — additional memory footprint per pod (OPA is lightweight, typically ~50MB).
- Per-tenant bundle compilation on every grant write must remain fast (target: ≤ 500ms). As grant volume per tenant grows, incremental compilation may be required.

### Neutral

- The Policy Service (PAP) becomes a first-class platform component that must be designed for high availability
- The `policy-gateway` binary continues to exist as a utility HTTP API but is no longer on any enforcement hot path — it is not a hard availability dependency
- Owned services must integrate `internal/platform/policy` for Layer 2 enforcement — an explicit integration step per service, but no runtime dependency on any external component

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

Every policy decision (allow and deny) should be published as a structured event to the kora event spine. The event contract — principal, resource, outcome, reason, timestamp, delegation context — should be defined alongside the spine infrastructure ADR.

---

## Implementation Notes

### Component Responsibilities

**Policy Service (`cmd/policy-service`) — PAP**  
Handles infrequent administrative writes and bundle compilation. Owns the database. Not on any request hot path. Single operational concern: keep bundles fresh on grant changes and serve them reliably to pollers.

**Platform Policy Library (`internal/platform/policy`) — Layer 2 PDP**  
An importable Go library, not a binary. Embedded directly in owned service processes (inference-gateway, MCP servers, etc.). Owns the OPA lifecycle for Layer 2: bundle polling, compilation, caching, and in-process evaluation. No network hop on the evaluation hot path.

**OPA Envoy Plugin (sidecar injection) — Layer 1 PDP**  
A purpose-built OPA image injected as a sidecar into agent pods via an Istio injection template. Not a kora-owned binary. Polls the policy-service bundle endpoint directly. Serves the ext_authz gRPC protocol on `localhost:9191`.

**Policy Gateway (`cmd/policy-gateway`) — utility HTTP API only**  
A thin HTTP server wrapping `internal/platform/policy`. Provides `POST /v1/policy/check` and `POST /v1/policy/query` for non-Go consumers (frontend BFF, future non-Go services, operator tooling). Not a dependency of any Go platform service for enforcement. Not on any hot path.

### Package Structure

```
backend/cmd/policy-service/
└── main.go                         # PAP: config, grant store, vocabulary, bundle compiler + server

backend/cmd/policy-gateway/
└── main.go                         # Utility HTTP API only — not an enforcement hot path

backend/internal/policyservice/
├── grants/
│   └── repository.go               # Postgres-backed grant CRUD
├── vocabulary/
│   └── repository.go               # Scope vocabulary store and registration
├── bundle/
│   ├── compiler.go                 # OPA bundle compilation (per-tenant)
│   └── server.go                   # GET /api/v1/policy/bundles/{tenant_id}
└── api/
    └── handlers.go                 # Grant + vocabulary CRUD HTTP handlers

backend/internal/platform/policy/   # Layer 2 PDP — reusable library
├── evaluator.go                    # Evaluator interface + NewEvaluator constructor
├── engine.go                       # OPA in-process evaluation, per-tenant bundle loading
├── input.go                        # OPA input document construction
├── hierarchy.go                    # Scope hierarchy resolution helpers (ScopeCovers, DeriveRequiredScope)
└── delegation.go                   # Delegation token parsing, DelegationClaims

backend/internal/policygw/
├── handlers.go                     # POST /v1/policy/check, /v1/policy/query (utility API)
└── models.go                       # Request/response types for utility HTTP API

charts/orbit/templates/
└── policy-gateway/
    └── envoyfilter.yaml            # OPA ext_authz EnvoyFilter for agent pods (SIDECAR_OUTBOUND)

charts/orbit-platform/templates/
└── opa-injection-template.yaml     # Istio sidecar injection template for OPA Envoy plugin
```

### Key Dependencies

- `github.com/open-policy-agent/opa` — OPA Go library for in-process bundle loading and policy evaluation
- `github.com/envoyproxy/go-control-plane/envoy v1.37.0` — already in `go.mod`; provides `envoy.service.auth.v3` proto types
- `github.com/jackc/pgx/v5` — already in `go.mod`; Postgres grant store
- `internal/platform/token` — `token.Verifier` and `PlatformClaims` for token validation and principal extraction
- `internal/platform/logging`, `internal/platform/observability` — standard kora logging and OTel integration

### Migration Path

No migration required. The policy service is additive. Existing services continue to operate unchanged. Layer 2 integration in the inference gateway and MCP servers is implemented as a follow-on integration step per service. Until a service integrates Layer 2, Layer 1 (Envoy) provides the enforcement baseline.

---

## Validation

### Success Metrics

- 100% of outbound requests from agent pods pass through the OPA sidecar ext_authz evaluation point
- `GET /v1/models` and MCP tool listings return only resources matching the calling principal's grants
- Grant deletion propagates to all enforcement points within the configured polling interval
- No cross-tenant grant evaluation — validated by bundle partitioning tests and penetration testing
- Layer 1 policy evaluation p99 latency ≤ 5ms (same-pod loopback to OPA sidecar)
- Layer 2 policy evaluation p99 latency ≤ 1ms (in-process OPA, pure memory)

### Monitoring

- `policygw_bundle_compile_duration_seconds` — histogram: bundle compilation latency per tenant
- `policygw_bundle_last_sync_timestamp` — gauge per enforcement point: last successful bundle load
- `policygw_eval_duration_seconds` — histogram: OPA evaluation latency per decision
- `policygw_decisions_total{outcome="allow|deny"}` — counter: allow/deny rate
- `policygw_token_validation_errors_total` — counter: token validation failures at ext_authz

---

## Related Decisions

- [unified-identity-model](2026-03-05-unified-identity-model.md) — Unified principal model and platform session token validated by ext_authz
- [agent-execution-flow-patterns-and-delegation](2026-03-05-agent-execution-flow-patterns-and-delegation.md) — Delegation tokens; the intersection invariant enforced by this ADR
- [universal-gateway-policy-enforcement](2026-03-05-universal-gateway-policy-enforcement.md) — Establishes Envoy as the data plane and OPA bundle distribution as the evaluation model; this ADR defines the authorisation model and PDP/PAP components
- [envoy-ext-proc-sidecar-token-injection](2026-03-21-envoy-ext-proc-sidecar-token-injection.md) — Identity proxy ext_proc sidecar; the fail-open vs fail-closed contrast is documented there
- [inference-gateway-architecture](2026-04-01-inference-gateway-architecture.md) — Inference gateway; integrates ext_authz HTTP API for Layer 2 model-level enforcement
- [mcp-server-registration-deployment-and-governance](2026-03-05-mcp-server-registration-deployment-and-governance.md) — MCP server governance
- [scope-gated-tool-exposure-via-mcp](2026-03-05-scope-gated-tool-exposure-via-mcp.md) — MCP tool filtering; the `query` API defined here is the mechanism

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

Scope strings use a colon-separated hierarchy (`inference:anthropic:use`). No wildcards appear in scope strings — breadth is expressed by granting a higher-level scope. Parent-child coverage is evaluated in OPA via the `scope_vocabulary` data document compiled into each bundle. The vocabulary is the authoritative finite set of valid scopes; grants referencing unregistered scopes are rejected at write time by the Policy Service.

The delegation intersection is enforced as a platform invariant in Rego — three parallel coverage checks (delegation token scopes, delegator grants, agent grants) that must all pass. It is not configurable by tenants and cannot be bypassed.

---

## Amendment — 2026-06-30: Grant-administration anti-escalation (creation-time scope ceiling)

**Status:** Accepted — adds an invariant to **The Authorisation Model**.

### Context

The **Delegation Intersection** invariant above is a *runtime* check: when a delegated agent makes a request, OPA intersects `delegation_scopes ∩ delegator_grants ∩ agent_grants`. It bounds what an acting principal may **do**.

It says nothing about grant **administration**. Today `POST /policy/grants` (and `PUT /policy/roles`) validate the principal type, check each scope against the vocabulary, and tenant-scope the write — but they never compare the requested scopes against the **caller's own** scopes. So any principal holding the coarse `platform:grants:write` scope can mint a grant — for any principal in its tenant, including itself — carrying **any** vocabulary scope. That is a privilege-escalation primitive: `platform:grants:write` is effectively "grant yourself anything."

This gap was flagged in [orbit-mcp](2026-06-30-orbit-mcp.md) ("the platform's **missing** anti-escalation rule … fix is a separate future platform ADR") and is the blocker for exposing grant/role **write** tools to agents through the kora MCP — an agent must not be able to escalate itself by writing a grant.

### Decision

Enforce a **creation-time scope ceiling** on grant and role administration, as a platform invariant alongside the delegation intersection:

> A non-SYSTEM caller may create or modify a grant or role only if **every** scope it assigns is **covered by the caller's own effective scopes** — the same `ScopeCovers` hierarchy used everywhere else (A covers B if same resource + action and A's path is a prefix of B's path).

- **Where.** In the Policy Service (PAP) write path — `CreateGrant` and the role upsert — before persistence. Compute the caller's effective scopes (`EffectiveScopes`), and reject with `403` if any requested scope is not covered by `AnyScopeCovers(callerScopes, requested)`, naming the offending scopes. This is a new, separate check from the existing vocabulary validation.
- **SYSTEM exemption.** SYSTEM principals are exempt. Trusted platform components (e.g. `orbit-agents` provisioning an agent's grant) legitimately assign scopes they do not personally hold, and already bypass tenant-scoping on these routes (`PrincipalType != SYSTEM` guards). The ceiling therefore applies to **HUMAN** and **AGENT** callers only — exactly the principals that could escalate.
- **Agent grants.** `orbit-agents` provisions an agent's grant as SYSTEM. Before it does, it applies the same ceiling to the caller creating or changing an autonomous agent, or editing an environment attached to one. Every scope the grant would hold, except the egress a connector derives (`egress:<host>:connector`), must be covered by the caller's effective scopes, read from `GET /policy/effective-scopes` with the caller's own token. For a delegated agent only its own egress (`egress:connect`, `egress:<host>:connect`) is checked. Egress is evaluated on the agent alone under delegation.
- **Roles too.** `PUT /policy/roles` gets the same ceiling: a role bundles scopes, and assigning a role is an indirect grant, so an unbounded role upsert would be the same escalation by another door.
- **Budget-only updates unaffected.** `PATCH /policy/grants/{id}` changes only the budget; scopes are immutable on a grant, so it needs no ceiling check.

### Consequences

- **Closes the escalation primitive** and makes grant/role **write** tools safe to expose to agents (the kora MCP `grants.create` / `roles.upsert` surface). Enforced once in the PAP, not re-implemented per client.
- **A caller can no longer hand out a scope it does not itself hold** — by design. Tenant bootstrapping is unaffected: the OIDC admin role / bootstrap path already grants the broad scopes an admin then distributes (see the bootstrapping rule in the OPA bundle).
- **Cost:** one effective-scopes read added to the grant/role write path (already cheap; the data is in the bundle/DB).
- **The SYSTEM carve-out is a trust boundary.** SYSTEM tokens are platform-minted (not tenant-issued) and must stay tightly controlled — they always have been; this amendment makes that property load-bearing for anti-escalation.

### Implementation pointer

In `internal/policyservice/api/service.go`, before persisting in `CreateGrant` (and the role upsert), when the caller is non-SYSTEM: load the caller's effective scopes (`EffectiveScopes`) and verify each requested scope with `policy.AnyScopeCovers` (`internal/platform/policy/hierarchy.go`); return a `403` naming the rejected scopes. The caller's principal type/id/roles come from the validated claims already on the context. Once shipped, the kora MCP grant/role write tools can be exposed safely.

---

**Last Updated:** 2026-06-30
