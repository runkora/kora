# 44. Gateway-Level OPA Enforcement for External Traffic

**Status:** Accepted
**Date:** 2026-04-26
**Deciders:** Platform Engineering
**Technical Story:** Browser-originated requests and direct API callers using PATs bypass all existing OPA enforcement. ADR-0011 established the universal enforcement principle; this ADR closes the gap for north-south traffic by placing OPA at the Istio IngressGateway rather than per-service sidecars.

---

## Context

### Background

ADR-0011 established that all requests must pass through a policy enforcement point before reaching a service. ADR-0015 resolved that Istio is the sidecar control plane and the Istio IngressGateway is the north-south edge.

The current enforcement topology has a gap:

```
Agent pod (orbit.io/opa-policy: enabled)
  └── Envoy SIDECAR_OUTBOUND
        ├── ext_proc → identity-proxy (token swap)
        └── ext_authz → OPA sidecar localhost:9191   ← enforced ✓

Browser / SDK client (PAT)
  └── Istio IngressGateway
        └── (no ext_authz)                           ← gap ✗
              → inference-gateway service
```

The OPA ext_authz EnvoyFilter (`opa-policy-ext-authz`) targets `SIDECAR_OUTBOUND` on pods labelled `orbit.io/opa-policy: "enabled"`. This covers agent-to-service traffic only. It does not cover:

- Browser requests proxied via the BFF (Next.js) to backend services
- Direct API callers using Platform Access Tokens (PATs) via the Anthropic or OpenAI SDK
- Any external caller whose traffic enters via the IngressGateway

### Problem Statement

External callers — browser sessions and PAT-authenticated SDK clients — have their JWTs verified by `AuthMiddleware` in the application process but face no network-layer scope enforcement before reaching the service. A valid JWT is sufficient to call any authenticated endpoint regardless of the principal's granted scopes, because the route→scope mapping enforced by the OPA sidecar only fires on agent pod outbound traffic.

Additionally, in-process `policy.Check()` calls placed inside service method implementations (Layer 2) are an antipattern: each service must independently implement and maintain scope enforcement, there is no single enforcement point auditable from the control plane, and the pattern does not extend to third-party services.

### Goals

- Enforce scope requirements for all traffic entering the cluster via the IngressGateway — browser, PAT, SDK — without application code changes in target services
- Reuse the existing OPA bundle distribution and Rego policy infrastructure
- Provide a single, auditable enforcement point for north-south traffic
- Eliminate per-service `policy.Check()` calls for route-level scope decisions
- Cover PAT-authenticated inference requests, browser analytics requests, and all future routes without per-route application code

### Non-Goals

- Replacing Layer 2 in-process `policy.Evaluator` calls for fine-grained decisions that require request body inspection (e.g. per-model access checks that depend on the `model` field in the POST body)
- Replacing the SIDECAR_OUTBOUND OPA enforcement for agent-to-service east-west traffic
- Removing the application-level `AuthMiddleware` JWT verification — that remains as defence-in-depth
- Defining the full audit event pipeline for gateway enforcement decisions

---

## Decision Drivers

* **ADR-0011 compliance**: The universal enforcement principle requires all traffic to pass a policy enforcement point. Browser and PAT traffic currently violates this.
* **No per-service application code**: Adding `policy.Check()` to every service handler or controller is not scalable. Each new service or route would require the same boilerplate, with no network-layer backstop if it is omitted.
* **Sidecar count**: Adding an OPA sidecar to every service pod that handles external traffic (inference-gateway today, MCP servers and others tomorrow) multiplies the sidecar footprint for a problem that has a single enforcement point solution.
* **Coverage breadth**: PAT-authenticated SDK clients are first-class API users. They receive identical enforcement to browser sessions and agent pods.
* **Consistency with existing infrastructure**: OPA bundle distribution, the policy-service bundle API, and the existing Rego are already production infrastructure. The gateway enforcement reuses all of it.

---

## Considered Options

### Option A: In-process scope enforcement per service (Layer 2)

**Description:** Each service adds scope checks in its controller or service methods using `platform/policy.Evaluator`. Route-level decisions (`platform:analytics:read`, `inference:invoke`) are checked inline before handler logic runs.

**Pros:**
- No new infrastructure
- Works immediately with existing policy evaluator

**Cons:**
- Every service implements enforcement independently — same antipattern as in-app middleware (rejected in ADR-0011)
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
- Single enforcement point covering all external traffic: browser, PAT, SDK, any future caller
- Zero application code in target services — no per-service changes for new routes
- No OPA sidecar added to any service pod — sidecar count does not grow with service count
- Reuses existing OPA Envoy plugin, bundle distribution, and Rego without modification
- Consistent with the ADR-0011 universal enforcement principle — the gateway IS the documented north-south enforcement point
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

Option A is the antipattern this ADR exists to close. It violates ADR-0011 and does not scale.

Option B solves the problem correctly for the enforcement model but multiplies sidecar count linearly with services — the sidecar overhead grows precisely where Option C does not.

Option D is architecturally sound but contradicts the platform's established principle that policy decisions must not require a synchronous network hop to the policy-service. The policy-service is already handling bundle distribution; making it a synchronous dependency for every evaluated request at the gateway would introduce the same latency and availability problem the bundle distribution model was designed to avoid.

Option C is the correct implementation of what ADR-0011 already describes as the north-south enforcement point: a centralised edge deployment that evaluates all inbound traffic. The IngressGateway is that edge. OPA at the gateway is a single deployment regardless of how many backend services exist. The network hop from Envoy to the OPA pod is intra-cluster and expected to be sub-millisecond — acceptable on the request path given that the alternative (Option B) has a loopback call that adds per-pod sidecar complexity at a larger operational cost.

---

## Implementation

### Components

**1. OPA Gateway Deployment**

A Kubernetes Deployment running `openpolicyagent/opa:latest-envoy` in the `orbit-system` namespace. This is a standalone pod (not a sidecar) accessible to the IngressGateway via a ClusterIP Service.

```
orbit-opa-gateway          Deployment — OPA Envoy plugin, polls policy-service bundles
orbit-opa-gateway          Service (ClusterIP) — gRPC port 9191, called by IngressGateway Envoy
orbit-opa-gateway-config   ConfigMap — OPA bundle config
```

**2. Cross-Tenant Bundle**

The current per-tenant bundle model (`bundles/{{ .TenantID }}`) requires knowing the tenant at OPA startup. The IngressGateway OPA instance handles traffic from all tenants simultaneously.

The policy-service adds a cross-tenant bundle endpoint that compiles all active tenants' grants and role mappings into a single bundle, partitioned by tenant ID under `data.tenants`:

```
data.tenants["<tenant-id>"].grants[...]
data.tenants["<tenant-id>"].role_scopes[...]
data.routes.route_scopes[...]      ← shared across tenants
```

The Rego extracts the tenant ID from the JWT (`claims.orbit.tenant_id`), selects the correct data partition, and evaluates the allow decision. The route→scope mapping remains shared.

This is a breaking change to the bundle format isolated to the policy-service bundle compiler. The per-tenant bundle endpoint used by agent pod OPA sidecars is unchanged — both formats are served in parallel during the transition.

**3. EnvoyFilter — IngressGateway**

An EnvoyFilter in `istio-system` targeting the IngressGateway (`istio: ingressgateway`) with `context: GATEWAY`. It inserts the ext_authz HTTP filter into the IngressGateway's filter chain, pointing at the `orbit-opa-gateway` Service via a STRICT_DNS cluster.

```yaml
apiVersion: networking.istio.io/v1alpha3
kind: EnvoyFilter
metadata:
  name: opa-gateway-ext-authz
  namespace: istio-system
spec:
  workloadSelector:
    labels:
      istio: ingressgateway
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
                          address: orbit-opa-gateway.orbit-system.svc.cluster.local
                          port_value: 9191
          http2_protocol_options: {}
```

`failure_mode_allow: false` — fail-closed. If the OPA deployment is unreachable the request is denied. The OPA deployment must be sized and health-checked appropriately.

**4. Unauthenticated Routes**

Routes that do not require authentication (health checks, OIDC callback paths, public endpoints) must be listed in the bundle's `data.routes.public_prefixes` document. The Rego passes these through without JWT evaluation. All other routes require a valid JWT and a matching scope grant.

**5. Remove Application-Level Route Scope Checks**

Once gateway enforcement is in place, `policy.Check()` calls in service handlers for route-level scope decisions (e.g. `platform:analytics:read` on `GET /v1/analytics`) are removed. Fine-grained body-dependent checks (per-model access via `checkModelPolicy`) remain as Layer 2 — the gateway cannot inspect the request body to determine the requested model.

### Deployment Order

1. Deploy `orbit-opa-gateway` Deployment and Service
2. Update policy-service to serve the cross-tenant bundle at a new endpoint
3. Configure `orbit-opa-gateway` to poll the cross-tenant bundle endpoint
4. Validate OPA evaluates correctly for a test tenant and route
5. Apply the IngressGateway EnvoyFilter
6. Remove application-level route-scope `policy.Check()` calls from services
7. Deprecate BFF `withAuthAndScope` session-cache checks (these become UX helpers only, not security gates)

### Helm Chart Additions

```
charts/orbit/templates/opa-gateway/deployment.yaml
charts/orbit/templates/opa-gateway/service.yaml
charts/orbit/templates/opa-gateway/configmap.yaml
charts/orbit/templates/opa-gateway/envoyfilter.yaml
```

---

## Consequences

### Positive

- All external traffic — browser, PAT, SDK — passes OPA enforcement at the IngressGateway without any application code in target services
- Service implementations are scope-agnostic for route-level decisions; they read verified claims from context and trust that access was already enforced
- PAT-authenticated inference callers receive the same coarse-grained enforcement as agent pods (which go through SIDECAR_OUTBOUND OPA), closing the coverage gap
- Sidecar count does not grow as new backend services are added
- The enforcement point is visible to the control plane — the IngressGateway EnvoyFilter is a Kubernetes resource, not code buried in a service

### Negative

- Cross-tenant bundle is a breaking change to the policy-service bundle compiler — requires careful versioning and parallel serving of both formats during migration
- The OPA Gateway Deployment is a new infrastructure component: it must be sized, health-checked, and monitored; its availability is now on the critical path for all external traffic
- The `timeout: 500ms` on the ext_authz call is stricter than the loopback timeout on agent pod sidecars (`timeout: 2s`) — OPA must evaluate within this budget under load

### Neutral

- Agent pod SIDECAR_OUTBOUND enforcement is unchanged — agent traffic continues to pass two enforcement points (gateway + sidecar outbound) which is acceptable defence-in-depth
- Application `AuthMiddleware` JWT verification remains — it is still the correct place to populate verified claims into the request context for Layer 2 use

---

## Validation

### Success Metrics

- A PAT-authenticated request to `POST /v1/chat/completions` without `inference:invoke` scope returns 403 at the gateway before reaching the inference-gateway process
- A browser session without `platform:analytics:read` receives 403 on `GET /v1/analytics` without the inference-gateway service being invoked
- Agent pod traffic is unaffected — SIDECAR_OUTBOUND enforcement and routing continue to function correctly
- OPA cross-tenant bundle serves correct allow/deny decisions for principals across at least two distinct tenants in integration testing

### Monitoring

- `envoy_http_ext_authz_ok` / `envoy_http_ext_authz_error` metrics on the IngressGateway — allow/deny rates and evaluation errors
- OPA bundle sync lag — time since last successful cross-tenant bundle fetch
- ext_authz p99 latency on the IngressGateway filter chain — must remain below 100ms under normal load

---

## Related Decisions

- [ADR-0011](0011-universal-gateway-policy-enforcement.md) — Establishes the universal enforcement principle this ADR implements for north-south traffic
- [ADR-0015](0015-service-mesh-and-network-enforcement-infrastructure.md) — Adopts Istio as service mesh; the IngressGateway used here is the Istio IngressGateway
- [ADR-0021](0021-envoy-ext-proc-sidecar-token-injection.md) — ext_proc sidecar token injection for agent pods; the IngressGateway EnvoyFilter follows the same ext_authz wiring pattern
- [ADR-0036](0036-dynamic-route-scope-registration.md) — Route→scope registration; the route_scopes data document in the cross-tenant bundle is the runtime form of this registry

---

## References

- [OPA Envoy Plugin](https://www.openpolicyagent.org/docs/latest/envoy-introduction/)
- [Envoy ext_authz HTTP filter](https://www.envoyproxy.io/docs/envoy/latest/configuration/http/http_filters/ext_authz_filter)
- [OPA Bundle distribution](https://www.openpolicyagent.org/docs/latest/management-bundles/)
- [Istio EnvoyFilter API](https://istio.io/latest/docs/reference/config/networking/envoy-filter/)
- `docs/design/policy-enforcement-redesign.md` — Platform design document establishing the two-layer enforcement model and zero-network-hop principle

---

## Notes

Implementation is tracked separately from this branch (`feat/robust-inference`). The cross-tenant bundle format change in the policy-service is the highest-risk component and should be prototyped and validated before the EnvoyFilter is applied to production.

**Last Updated:** 2026-04-26
