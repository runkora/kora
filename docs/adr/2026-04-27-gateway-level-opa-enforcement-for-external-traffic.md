# Gateway-Level OPA Enforcement for External Traffic

**Status:** Accepted
**Date:** 2026-04-27
**Deciders:** Platform Engineering
**Technical Story:** Direct API callers using PATs and SDK clients bypass all existing OPA enforcement. [universal-gateway-policy-enforcement](2026-03-05-universal-gateway-policy-enforcement.md) established the universal enforcement principle; this ADR closes the gap for north-south traffic by placing OPA at the Istio IngressGateway rather than per-service sidecars.

---

## Context

### Background

[universal-gateway-policy-enforcement](2026-03-05-universal-gateway-policy-enforcement.md) established that all requests must pass through a policy enforcement point before reaching a service. [service-mesh-and-network-enforcement-infrastructure](2026-03-05-service-mesh-and-network-enforcement-infrastructure.md) resolved that Istio is the sidecar control plane and the Istio IngressGateway is the north-south edge.

The current enforcement topology has a gap:

```
Agent pod (runkora.io/opa-policy: enabled)
  └── Envoy SIDECAR_OUTBOUND
        ├── ext_proc → identity-proxy (token swap)
        └── ext_authz → OPA sidecar localhost:9191   ← enforced ✓

SDK client / PAT caller
  └── Istio IngressGateway
        └── (no ext_authz)                           ← gap ✗
              → inference-gateway service
```

The OPA ext_authz EnvoyFilter (`opa-policy-ext-authz`) targets `SIDECAR_OUTBOUND` on pods labelled `runkora.io/opa-policy: "enabled"`. This covers agent-to-service traffic only. It does not cover:

- Direct API callers using Platform Access Tokens (PATs) via the Anthropic or OpenAI SDK
- Any external caller whose traffic enters via the IngressGateway
- Tenant-deployed services (agents, MCP servers, APIs) exposed via the IngressGateway

BFF-mediated browser traffic is a separate concern: the BFF is a trusted internal service that enforces scope at the application layer using the principal's session permissions. BFF-to-service east-west calls do not pass through the IngressGateway and are not addressed by this ADR — that enforcement gap is explicitly deferred to a follow-on ADR.

### Problem Statement

Direct API callers — PAT-authenticated SDK clients and any client app calling the API directly — have their JWTs verified by `AuthMiddleware` in the application process but face no network-layer scope enforcement before reaching the service. A valid JWT is sufficient to call any authenticated endpoint regardless of the principal's granted scopes, because the route→scope mapping enforced by the OPA sidecar only fires on agent pod outbound traffic.

As the platform grows, every new service exposed via the IngressGateway inherits this gap. Tenant-deployed agents, MCP servers, and APIs that register their own routes have no enforcement at the network layer for external callers.

Additionally, in-process `policy.Check()` calls placed inside service method implementations (Layer 2) are an antipattern: each service must independently implement and maintain scope enforcement, there is no single enforcement point auditable from the control plane, and the pattern does not extend to third-party services.

### Goals

- Enforce scope requirements for all external traffic entering the cluster via the IngressGateway — PAT, SDK, client apps — without application code changes in target services
- Reuse the existing OPA bundle distribution and Rego policy infrastructure
- Provide a single, auditable enforcement point for north-south traffic
- Cover PAT-authenticated inference requests and all future routes — including tenant-deployed services — without per-route application code
- Automatically cover any new service added to the IngressGateway with zero enforcement configuration

### Non-Goals

- Enforcing scope on BFF-to-service east-west calls — the BFF is a trusted internal service; its scope enforcement is at the application layer via session permissions; this is deferred to a follow-on ADR
- Replacing Layer 2 in-process `policy.Evaluator` calls for fine-grained decisions that require request body inspection (e.g. per-model access checks that depend on the `model` field in the POST body)
- Replacing the SIDECAR_OUTBOUND OPA enforcement for agent-to-service east-west traffic
- Removing the application-level `AuthMiddleware` JWT verification — that remains as defence-in-depth
- Defining the full audit event pipeline for gateway enforcement decisions

---

## Decision Drivers

* **[universal-gateway-policy-enforcement](2026-03-05-universal-gateway-policy-enforcement.md) compliance**: The universal enforcement principle requires all traffic to pass a policy enforcement point. PAT and direct API callers currently violate this.
* **No per-service application code**: Adding `policy.Check()` to every service handler or controller is not scalable. Each new service or route would require the same boilerplate, with no network-layer backstop if it is omitted.
* **Sidecar count**: Adding an OPA sidecar to every service pod that handles external traffic (inference-gateway today, MCP servers and others tomorrow) multiplies the sidecar footprint for a problem that has a single enforcement point solution.
* **Coverage breadth**: PAT-authenticated SDK clients are first-class API users. They receive identical enforcement to agent pods.
* **Consistency with existing infrastructure**: OPA bundle distribution, the policy-service bundle API, and the existing Rego are already production infrastructure. The gateway enforcement reuses all of it.

---

## Considered Options

### Option A: In-process scope enforcement per service (Layer 2)

**Description:** Each service adds scope checks in its controller or service methods using `platform/policy.Evaluator`. Route-level decisions (`platform:analytics:read`, `inference:invoke`) are checked inline before handler logic runs.

**Pros:**
- No new infrastructure
- Works immediately with existing policy evaluator

**Cons:**
- Every service implements enforcement independently — same antipattern as in-app middleware (rejected in [universal-gateway-policy-enforcement](2026-03-05-universal-gateway-policy-enforcement.md))
- No network-layer backstop: if a service omits the check, there is no enforcement
- Does not cover third-party services
- Not auditable from the control plane — decisions happen inside application processes
- Does not cover PAT callers hitting routes that lack the check

### Option B: OPA sidecar on each target service pod (SIDECAR_INBOUND)

**Description:** Add the OPA sidecar and `SIDECAR_INBOUND` ext_authz EnvoyFilter to each service pod that handles external traffic.

**Pros:**
- Zero application code
- Consistent with the agent pod SIDECAR_OUTBOUND pattern
- Loopback call — zero network hop

**Cons:**
- One additional sidecar container per service pod — multiplies as the service count grows
- Requires OPA sidecar injection to be added to each service's deployment template
- Multi-tenant bundle challenge still applies (see below) and must be solved per-pod
- Operational footprint grows linearly with service count

### Option C: OPA enforcement at the Istio IngressGateway

**Description:** Deploy a standalone OPA instance (Kubernetes Deployment + Service) that polls the policy-service for bundles. Wire an EnvoyFilter on the IngressGateway (`context: GATEWAY`) to call this OPA deployment as an ext_authz provider. All traffic entering the cluster is evaluated at one point.

**Pros:**
- Single enforcement point covering all external traffic: PAT, SDK, client apps, tenant-deployed services, any future caller
- Zero application code in target services — no per-service changes for new routes
- No OPA sidecar added to any service pod — sidecar count does not grow with service count
- Reuses existing OPA Envoy plugin, bundle distribution, and Rego without modification
- Consistent with the [universal-gateway-policy-enforcement](2026-03-05-universal-gateway-policy-enforcement.md) universal enforcement principle — the gateway IS the documented north-south enforcement point
- PAT-authenticated inference requests are covered without touching inference-gateway code

**Cons:**
- Small network hop from the IngressGateway's Envoy to the OPA Deployment (~1ms intra-cluster)
- Requires a cross-tenant bundle (see Implementation section) — the gateway handles requests from all tenants simultaneously, so the per-tenant bundle model used by agent pod sidecars must be extended
- The OPA Deployment is a new infrastructure component to operate

### Option D: Policy-service exposes a gRPC ext_authz endpoint

**Description:** The policy-service (which already runs OPA internally) adds a gRPC ext_authz server. Registered as an Istio `extensionProvider` in MeshConfig; `AuthorizationPolicy` resources (action: CUSTOM) select which workloads and paths it applies to.

**Pros:**
- No new OPA deployment — reuses the policy-service process
- Istio-native `AuthorizationPolicy` is declarative and visible to the control plane
- Fine-grained path/method selection without a monolithic EnvoyFilter

**Cons:**
- Requires adding a gRPC ext_authz server implementation to the policy-service
- Network hop to the policy-service on every evaluated request — the policy-service becomes a latency dependency for all external traffic
- Contradicts the design principle established in `docs/design/policy-enforcement-redesign.md`: "No enforcement decision should require a network hop" — bundle distribution, not synchronous API calls, is the chosen model
- More implementation work than Option C

---

## Decision

**Chosen Option:** Option C — OPA enforcement at the Istio IngressGateway

### Rationale

Option A is the antipattern this ADR exists to close. It violates [universal-gateway-policy-enforcement](2026-03-05-universal-gateway-policy-enforcement.md) and does not scale.

Option B solves the problem correctly for the enforcement model but multiplies sidecar count linearly with services — the sidecar overhead grows precisely where Option C does not.

Option D is architecturally sound but contradicts the platform's established principle that policy decisions must not require a synchronous network hop to the policy-service. The policy-service is already handling bundle distribution; making it a synchronous dependency for every evaluated request at the gateway would introduce the same latency and availability problem the bundle distribution model was designed to avoid.

Option C is the correct implementation of what [universal-gateway-policy-enforcement](2026-03-05-universal-gateway-policy-enforcement.md) already describes as the north-south enforcement point: a centralised edge deployment that evaluates all inbound traffic. The IngressGateway is that edge. OPA at the gateway is a single deployment regardless of how many backend services exist. The network hop from Envoy to the OPA pod is intra-cluster and expected to be sub-millisecond — acceptable on the request path given that the alternative (Option B) has a loopback call that adds per-pod sidecar complexity at a larger operational cost.

---

## Implementation

### Components

**1. OPA Gateway Deployment**

A Kubernetes Deployment running `openpolicyagent/opa:latest-envoy` in the `kora-system` namespace. This is a standalone pod (not a sidecar) accessible to the IngressGateway via a ClusterIP Service.

```
kora-opa-gateway          Deployment — OPA Envoy plugin, polls policy-service bundles
kora-opa-gateway          Service (ClusterIP) — gRPC port 9191, called by IngressGateway Envoy
kora-opa-gateway-config   ConfigMap — OPA bundle config
```

**Availability requirement:** The Deployment must run a minimum of 2 replicas at all times. A `PodDisruptionBudget` with `minAvailable: 1` is required; without it a single node drain takes down all OPA replicas and fails all external traffic closed. The Deployment must also configure liveness and readiness probes against the OPA health endpoint (`GET /health`) so that Kubernetes restarts unhealthy replicas before they appear in the ext_authz cluster.

**2. Cross-Tenant Bundle**

The current per-tenant bundle model (`GET /v1/policy/bundles/{tenant_id}`) requires knowing the tenant at OPA startup. The IngressGateway OPA instance handles traffic from all tenants simultaneously and cannot be parameterised with a single tenant ID.

The policy-service adds a new `GET /v1/policy/bundles/gateway` endpoint that compiles all active tenants' data into a single bundle. Tenant-specific documents are namespaced under `data.tenants["<tenant-id>"]`. Route registrations are split: SYSTEM-principal registrations (global platform services) go into `data.routes`; SERVICE-principal registrations (tenant-deployed agents, MCP servers, APIs) go into `data.tenants["<tenant-id>"].routes`:

```
data/tenants/<tenant-id>/grants/data.json   → { "grants": [...] }
data/tenants/<tenant-id>/roles/data.json    → { "role_scopes": { ... } }
data/tenants/<tenant-id>/routes/data.json   → { "route_scopes": [...] }
data/routes/data.json                       → { "route_scopes": [...] }  ← global only
data/config/data.json                       → { "jwks_url": ..., "issuer": ..., "audience": ... }
```

Splitting routes this way means a tenant-deployed agent registering `/api/agent/v1/tasks` requiring `agent:tasks:invoke` is only enforced for principals from that tenant — not for principals from other tenants who may have a service on the same path.

The gateway bundle is recompiled on every grant, role, or route write. On startup, the policy-service compiles the gateway bundle alongside all per-tenant bundles.

**Note on the `reason` field:** `authz_gateway.rego` computes a `reason` string (e.g. `deny:invalid_token`, `deny:no_route_match`, `grant:inference:invoke`). This value appears in OPA decision logs and is visible in the OPA audit trail, but it is **not** included in the gRPC ext_authz denied-response metadata visible to callers. The OPA Envoy plugin only evaluates the `allow` path for the ext_authz response; `reason` is for observability only.

**Rego: separate gateway policy file.** Rather than modifying `authz.rego`, the gateway bundle ships `policy/authz_gateway.rego`. This file has four responsibilities beyond what `authz.rego` does:

1. **Frontend host bypass** — requests to `orbit.local.clearroute.io` carry session cookies, not Bearer tokens. The BFF handles its own auth. These are passed through without evaluation.
2. **Health check bypass** — `GET /health` is registered outside all middleware on every service. Hardcoded bypass alongside the existing platform-internal paths.
3. **Tenant-scoped route matching** — SERVICE-principal route registrations live under `data.tenants[_tenant_id].routes.route_scopes`. The Rego checks these after global routes.
4. **Cross-tenant data partition** — grants and roles are looked up under `data.tenants[_tenant_id]` rather than `data.grants` / `data.roles`.

```rego
package orbit.authz

import rego.v1

default allow := false

_tenant_id := _layer1_claims.orbit.tenant_id

# ── FRONTEND HOST BYPASS ─────────────────────────────────────────────────────
# The frontend (orbit.local.clearroute.io) uses session cookies, not Bearer
# tokens. The BFF handles its own session auth. Pass all frontend traffic
# through without JWT evaluation.

_is_frontend_host if {
    input.attributes.request.http.headers[":authority"] == data.config.frontend_host
}

allow if _is_frontend_host

# ── HEALTH CHECK BYPASS ───────────────────────────────────────────────────────
# GET /health is registered outside all middleware on every service.

_is_platform_internal_path if {
    some prefix in [
        "/health",
        "/api/v1/identity/exchange",
        "/api/v1/identity/.well-known/",
        "/api/v1/policy/bundles/",
    ]
    startswith(input.attributes.request.http.path, prefix)
}

allow if _is_platform_internal_path

# ── REQUIRED SCOPE: global routes then tenant routes ─────────────────────────

_required_scope := route.required_scope if {
    _is_envoy
    some route in data.routes.route_scopes
    startswith(input.attributes.request.http.path, route.path_prefix)
    route.method == input.attributes.request.http.method
}

_required_scope := route.required_scope if {
    _is_envoy
    not _has_global_route_match
    some route in data.tenants[_tenant_id].routes.route_scopes
    startswith(input.attributes.request.http.path, route.path_prefix)
    route.method == input.attributes.request.http.method
}

_has_global_route_match if {
    some route in data.routes.route_scopes
    startswith(input.attributes.request.http.path, route.path_prefix)
    route.method == input.attributes.request.http.method
}

# ── GRANT EVALUATION: cross-tenant data partition ────────────────────────────

principal_has_required_scope if {
    some grant in data.tenants[_tenant_id].grants.grants
    grant.principal_type == _principal_type
    grant.principal_id == _principal_id
    some granted_scope in grant.scopes
    scope_covers(granted_scope, _required_scope)
}

allow if {
    not _is_delegated
    _principal_type == "HUMAN"
    some role in _principal_roles
    some role_scope in data.tenants[_tenant_id].roles.role_scopes[role]
    scope_covers(role_scope, _required_scope)
}
```

`data.config.frontend_host` is compiled into the gateway bundle's config document alongside `jwks_url`, `issuer`, and `audience` — making the frontend hostname configurable without a Rego change.

All shared rules (`_layer1_token`, `_layer1_claims`, `_principal_type`, `_principal_id`, `_principal_roles`, delegation rules, `scope_covers`, the main `allow` rules) are duplicated from `authz.rego` into `authz_gateway.rego`. A CI diff check is required to catch unintended divergence between the two files.

The per-tenant bundle endpoint (`GET /v1/policy/bundles/{tenant_id}`) and `authz.rego` are unchanged.

**3. EnvoyFilter — IngressGateway**

An EnvoyFilter in `istio-system` targeting the IngressGateway (`istio: ingressgateway`) with `context: GATEWAY`. It inserts the ext_authz HTTP filter into the IngressGateway's filter chain, pointing at the `kora-opa-gateway` Service via a STRICT_DNS cluster.

```yaml
apiVersion: networking.istio.io/v1alpha3
kind: EnvoyFilter
metadata:
  name: opa-gateway-ext-authz
  namespace: istio-system
spec:
  workloadSelector:
    labels:
      app: gateway
  configPatches:
    - applyTo: HTTP_FILTER
      match:
        context: GATEWAY
        listener:
          filterChain:
            filter:
              name: "envoy.filters.network.http_connection_manager"
              subFilter:
                name: "envoy.filters.http.router"
      patch:
        operation: INSERT_BEFORE
        value:
          name: envoy.filters.http.ext_authz
          typed_config:
            "@type": type.googleapis.com/envoy.extensions.filters.http.ext_authz.v3.ExtAuthz
            grpc_service:
              envoy_grpc:
                cluster_name: opa_gateway_authz
              timeout: 500ms
            failure_mode_allow: false
            transport_api_version: V3

    - applyTo: CLUSTER
      match:
        context: GATEWAY
      patch:
        operation: ADD
        value:
          name: opa_gateway_authz
          type: STRICT_DNS
          connect_timeout: 1s
          load_assignment:
            cluster_name: opa_gateway_authz
            endpoints:
              - lb_endpoints:
                  - endpoint:
                      address:
                        socket_address:
                          address: kora-opa-gateway.kora-system.svc.cluster.local
                          port_value: 9191
          http2_protocol_options: {}
```

`failure_mode_allow: false` — fail-closed. If the OPA deployment is unreachable the request is denied. The OPA deployment must be sized and health-checked appropriately.

**4. Unauthenticated Routes**

Two bypass categories are handled directly in `authz_gateway.rego` with no registration mechanism required:

- **Frontend host** — all traffic to `orbit.local.clearroute.io` is passed through. The BFF is a trusted internal service managing its own session auth.
- **Health checks and platform infrastructure** — `GET /health` (registered outside all middleware on every service) and the existing platform-internal paths (`/api/v1/identity/exchange`, `/.well-known/`, `/api/v1/policy/bundles/`) are hardcoded in the `_is_platform_internal_path` rule.

All other paths without a matching route registration receive `deny:no_route_match`. Services with additional unauthenticated paths beyond `/health` must be added to the `_is_platform_internal_path` list in `authz_gateway.rego` via a pull request — this is expected to be rare and intentionally requires an explicit code change rather than a runtime registration.

**5. Application-Level Scope Checks**

Route-level `policy.Check()` calls in service handlers have already been removed — scope enforcement for registered routes is handled entirely by the Rego route matching against `data.routes.route_scopes`. The only remaining `policy.Check()` calls are Layer 2 body-dependent checks (`checkModelPolicy` in the inference gateway), which inspect the `model` field from the request body. These are not removable at the gateway layer and remain in place.

BFF scope checks (`session.permissions`) are already UX-only — they control nav visibility and are not enforced as security gates. No BFF changes are required.

**6. Workload Identity for the OPA Gateway Pod**

The `kora-opa-gateway` Deployment is assigned a Kubernetes ServiceAccount in `kora-system`. The policy-service bundle endpoint (`GET /v1/policy/bundles/gateway`) is accessible without a bearer token — it is a cluster-internal polling endpoint, analogous to the per-tenant bundle endpoints.

The `kora-opa-gateway` pod is labelled `spiffe.io/component: opa-gateway`, which selects it for the `kora-system-component` ClusterSPIFFEID. SPIRE issues it the identity `spiffe://orbit.test/system/opa-gateway`. The Istio sidecar presents this SVID when OPA's bundle fetch reaches the policy-service, satisfying the Istio DENY AuthorizationPolicy that restricts the gateway bundle endpoint to verified `kora-system` sources.

The pod runs as UID 1000 (not UID 1337). An earlier draft used UID 1337 on the assumption that it would prevent a recursive ext_authz loop on bundle fetches. This was incorrect: the `opa-gateway-ext-authz` EnvoyFilter targets the IngressGateway workload only (`app: gateway`); OPA's bundle fetch goes pod-to-pod within the cluster and never passes through the IngressGateway, so no loop is possible. Using UID 1337 had the opposite effect — it caused OPA's outbound traffic to bypass the Istio iptables redirect, making bundle fetches plain HTTP. Without an SVID in the TLS handshake, Istio's DENY policy treated OPA as an unverified source and blocked the fetch with 403.

### Deployment Order

1. Add `GET /v1/policy/bundles/gateway` endpoint to the policy-service; validate cross-tenant bundle compiles correctly including both global and tenant-scoped route registrations
2. Write `authz_gateway.rego` and validate it evaluates correctly in unit tests: frontend host bypass, health check bypass, global route matching, tenant route matching, grant evaluation
3. Deploy `kora-opa-gateway` Deployment (2 replicas), Service, PodDisruptionBudget, and ServiceAccount in `kora-system`
4. Configure `kora-opa-gateway` to poll the cross-tenant bundle endpoint; validate bundle sync and allow/deny decisions for principals across at least two tenants, including a tenant-registered service
5. Apply the `opa-gateway-ext-authz` EnvoyFilter — from this point, all external traffic is evaluated at the gateway
6. Validate: PAT-authenticated request without `inference:invoke` → 403 at gateway; PAT with correct scope → 200; frontend traffic unaffected; agent pod traffic unaffected

### Helm Chart Additions

```
charts/kora/templates/opa-gateway/deployment.yaml
charts/kora/templates/opa-gateway/service.yaml
charts/kora/templates/opa-gateway/configmap.yaml
charts/kora/templates/opa-gateway/envoyfilter.yaml
charts/kora/templates/opa-gateway/poddisruptionbudget.yaml
```

---

## Consequences

### Positive

- All traffic entering via the IngressGateway — PAT, SDK, client apps, tenant-deployed agents and APIs — passes OPA enforcement without any application code in target services
- Any new service exposed via the IngressGateway is automatically covered; no enforcement configuration needed beyond registering its routes with the policy-service
- Service implementations are scope-agnostic for route-level decisions; they read verified claims from context and trust that access was already enforced at the edge
- PAT-authenticated inference callers receive the same enforcement as agent pods (which go through SIDECAR_OUTBOUND OPA), closing the direct-API-caller coverage gap
- Sidecar count does not grow as new backend services are added
- The enforcement point is visible to the control plane — the IngressGateway EnvoyFilter is a Kubernetes resource, not code buried in a service

### Negative

- Cross-tenant bundle is a new bundle format requiring a new policy-service endpoint, a new `authz_gateway.rego`, and careful parallel serving with per-tenant bundles during rollout
- The OPA Gateway Deployment is a new infrastructure component on the critical path for all external traffic: 2-replica minimum, PDB, and health probes are required before the EnvoyFilter is applied
- The `timeout: 500ms` on the ext_authz call is strict. OPA must evaluate the cross-tenant bundle within this budget under load. Before applying the EnvoyFilter in production, the gateway OPA should be benchmarked under realistic load (cross-tenant bundle with N active tenants, concurrent requests) to confirm p99 latency is below 100ms and p100 is below 500ms. If the benchmark fails, either the timeout must be raised or the bundle size must be bounded.
- `authz_gateway.rego` and `authz.rego` will diverge over time if not actively maintained in sync. A CI diff check between the two files is required to catch unintended divergence.
- BFF-to-service east-west calls are not covered by this ADR — enforcement of browser-session-originated requests to backend services is deferred.

### Neutral

- Agent pod SIDECAR_OUTBOUND enforcement is unchanged — agent traffic continues to pass two enforcement points (gateway + sidecar outbound) which is acceptable defence-in-depth
- Application `AuthMiddleware` JWT verification remains — it is still the correct place to populate verified claims into the request context for Layer 2 use
- BFF session scope checks remain as UX helpers; they are not a security gate and are not changed by this ADR

---

## Validation

### Success Metrics

- A PAT-authenticated request to `POST /v1/chat/completions` without `inference:invoke` scope returns 403 at the gateway before reaching the inference-gateway process
- A PAT with correct scope reaches the service and receives a valid response
- A principal from tenant A cannot access a tenant B service even if both are exposed on the same path
- Agent pod traffic is unaffected — SIDECAR_OUTBOUND enforcement and routing continue to function correctly
- Frontend traffic (`orbit.local.clearroute.io`) is unaffected — login, OIDC callback, and all BFF routes pass through
- OPA cross-tenant bundle serves correct allow/deny decisions for principals across at least two distinct tenants including one with tenant-registered routes

### Monitoring

- `envoy_http_ext_authz_ok` / `envoy_http_ext_authz_error` metrics on the IngressGateway — allow/deny rates and evaluation errors
- OPA bundle sync lag — time since last successful cross-tenant bundle fetch
- ext_authz p99 latency on the IngressGateway filter chain — must remain below 100ms under normal load

---

## Related Decisions

- [universal-gateway-policy-enforcement](2026-03-05-universal-gateway-policy-enforcement.md) — Establishes the universal enforcement principle this ADR implements for north-south traffic
- [service-mesh-and-network-enforcement-infrastructure](2026-03-05-service-mesh-and-network-enforcement-infrastructure.md) — Adopts Istio as service mesh; the IngressGateway used here is the Istio IngressGateway
- [envoy-ext-proc-sidecar-token-injection](2026-03-21-envoy-ext-proc-sidecar-token-injection.md) — ext_proc sidecar token injection for agent pods; the IngressGateway EnvoyFilter follows the same ext_authz wiring pattern
- [dynamic-route-scope-registration](2026-04-14-dynamic-route-scope-registration.md) — Route→scope registration; the route_scopes data document in the cross-tenant bundle is the runtime form of this registry

---

## References

- [OPA Envoy Plugin](https://www.openpolicyagent.org/docs/latest/envoy-introduction/)
- [Envoy ext_authz HTTP filter](https://www.envoyproxy.io/docs/envoy/latest/configuration/http/http_filters/ext_authz_filter)
- [OPA Bundle distribution](https://www.openpolicyagent.org/docs/latest/management-bundles/)
- [Istio EnvoyFilter API](https://istio.io/latest/docs/reference/config/networking/envoy-filter/)
- `docs/design/policy-enforcement-redesign.md` — Platform design document establishing the two-layer enforcement model and zero-network-hop principle

---

## Notes

The cross-tenant bundle and `authz_gateway.rego` are the highest-risk components. Both should be fully implemented and unit-tested before the EnvoyFilter is applied. The EnvoyFilter step is the point of no return — once applied, all external traffic fails closed if OPA is unavailable.

BFF-to-service east-west enforcement (browser sessions calling backend services directly) is explicitly out of scope and should be addressed in a follow-on ADR.

**Last Updated:** 2026-04-28
