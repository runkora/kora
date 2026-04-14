# Policy Enforcement Redesign

**Status:** Draft  
**Date:** 2026-04-10  
**Author:** Engineering

---

## Problem

The current implementation has three fundamental flaws:

**1. `evaluation.Engine` is locked inside `internal/policygw/`.**
It cannot be imported by any other service. Any service that needs in-process OPA evaluation — the inference gateway today, MCP servers tomorrow — has no reusable path to it.

**2. `PolicyGatewayEvaluator` makes an HTTP call to the remote policy-gateway on every inference request.**
This is a synchronous network hop on the hot path of every model call. Any policy-gateway disruption cascades directly into inference latency.

**3. Layer 1 coarse-grained enforcement, as originally designed, routes every outbound request through a remote policy-gateway pod.**
A network hop per request defeats the purpose of having a fast enforcement layer. At any meaningful request rate this becomes a latency and availability bottleneck — the remote policy-gateway becomes a hard dependency for every single outbound call from every agent pod.

The correct answer for both layers is the same: OPA must be co-located with the enforcement point. No enforcement decision should require a network hop.

---

## Target Architecture

Two enforcement layers. Neither makes a network hop for policy decisions.

```
┌─────────────────────────────────────────────────────────────────────────┐
│  LAYER 1 — Coarse-grained, Envoy outbound ext_authz                     │
│                                                                          │
│  "Can this principal call inference at all?"                             │
│  "Can this agent call the MCP server at all?"                           │
│                                                                          │
│  Enforcement point: calling pod's Envoy sidecar (SIDECAR_OUTBOUND)      │
│  Policy decision: OPA sidecar on localhost:9191 — zero network hop      │
│  OPA sidecar polls policy-service bundles in the background             │
└─────────────────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────────────────┐
│  LAYER 2 — Fine-grained, in-process via platform/policy.Evaluator       │
│                                                                          │
│  "Which specific models can this principal see?"                         │
│  "Can this principal invoke this specific model?"                        │
│                                                                          │
│  Enforcement point: inside the service process itself                    │
│  Policy decision: in-process OPA evaluation — zero network hop          │
│  OPA embedded in the service process, polls policy-service bundles      │
└─────────────────────────────────────────────────────────────────────────┘
```

### Request flow

```
Agent pod (orbit.io/identity-proxy: enabled)
  ├── identity-proxy sidecar
  ├── opa sidecar                    ← polls policy-service bundles (background)
  └── Envoy sidecar (outbound)
        ├── ext_proc → identity-proxy.sock    (inject Authorization header, loopback)
        └── ext_authz → localhost:9191        (Layer 1: OPA sidecar, loopback)
              ↓ 403 if denied
        → orbit-inference-gateway pod
              └── inference-gateway process
                    ├── platform/policy.Evaluator   ← polls policy-service bundles (background)
                    ├── .Query()   (Layer 2: filter model list, in-process OPA)
                    └── .Check()   (Layer 2: per-request model check, in-process OPA)
```

Every policy decision — at both layers — is a loopback call or pure memory access. Policy-service is only contacted in the background for bundle refreshes, never on the request hot path.

---

## Layer 1: OPA Envoy Plugin Sidecar

The OPA Envoy plugin (`openpolicyagent/opa:latest-envoy`) is a purpose-built OPA image that implements the ext_authz gRPC protocol natively. It runs as an additional sidecar container injected into every agent pod.

### Sidecar injection

OPA is injected via an Istio injection template, following the same pattern as the identity-proxy and SPIRE sidecars. The template is added to the orbit-platform Helm values and referenced by the pod annotation:

```yaml
# Pod template annotation on agent pods:
inject.istio.io/templates: "sidecar,spire,identity-proxy,opa-policy"
```

The `opa-policy` template adds:
- The OPA container (`openpolicyagent/opa:latest-envoy`) with a bundle config
- A ConfigMap volume mounting the OPA config (bundle endpoint, decision log settings)

### OPA sidecar config

```yaml
# ConfigMap: orbit-opa-config
services:
  policy-service:
    url: http://orbit-policy-service.orbit-system.svc/api/v1/policy

bundles:
  authz:
    service: policy-service
    resource: "bundles/{{ .TenantID }}"    # per-tenant bundle path
    polling:
      min_delay_seconds: 30
      max_delay_seconds: 60

plugins:
  envoy_ext_authz_grpc:
    addr: localhost:9191
    path: orbit/authz/allow               # maps to data.orbit.authz.allow in the Rego

decision_logs:
  console: true
```

### EnvoyFilter

An EnvoyFilter in `istio-system` wires ext_authz to the OPA sidecar on agent pods. The critical ordering requirement: ext_authz must be inserted **after** the identity-proxy ext_proc filter so the platform token is already in the `Authorization` header when OPA receives the `CheckRequest`.

```yaml
apiVersion: networking.istio.io/v1alpha3
kind: EnvoyFilter
metadata:
  name: opa-policy-ext-authz
  namespace: istio-system
spec:
  workloadSelector:
    labels:
      orbit.io/identity-proxy: "enabled"
  configPatches:
    - applyTo: HTTP_FILTER
      match:
        context: SIDECAR_OUTBOUND
        listener:
          filterChain:
            filter:
              name: "envoy.filters.network.http_connection_manager"
              subFilter:
                name: "envoy.filters.http.ext_proc"    # insert AFTER token injection
      patch:
        operation: INSERT_AFTER
        value:
          name: envoy.filters.http.ext_authz
          typed_config:
            "@type": type.googleapis.com/envoy.extensions.filters.http.ext_authz.v3.ExtAuthz
            grpc_service:
              envoy_grpc:
                cluster_name: opa_policy_local
              timeout: 10ms                            # loopback — very tight timeout
            failure_mode_allow: false                  # fail-closed
            transport_api_version: V3

    - applyTo: CLUSTER
      match:
        context: SIDECAR_OUTBOUND
      patch:
        operation: ADD
        value:
          name: opa_policy_local
          type: STATIC
          connect_timeout: 1s
          load_assignment:
            cluster_name: opa_policy_local
            endpoints:
              - lb_endpoints:
                  - endpoint:
                      address:
                        socket_address:
                          address: 127.0.0.1
                          port_value: 9191
          http2_protocol_options: {}
```

`failure_mode_allow: false` — fail-closed. If the OPA sidecar is not ready (startup race), the request is denied. The OPA readiness probe gates pod readiness to prevent traffic before OPA has loaded its bundle.

### What the OPA Envoy plugin receives

OPA receives the full Envoy `CheckRequest` including headers. It evaluates `data.orbit.authz.allow` with an input document constructed from the request context. The existing `authz.rego` policy is unchanged — the same Rego runs at both Layer 1 (OPA sidecar) and Layer 2 (`platform/policy.Evaluator`).

The input document for Layer 1 is constructed by OPA from the Envoy request attributes:
- `Authorization` header → platform JWT → principal claims (OPA Envoy plugin handles JWT parsing via built-in functions)
- HTTP method + path → required scope (via a mapping table embedded in the Rego or in the bundle's `data/routes` document)

This means the `defaultRouteScopes` mapping currently in `grpc.go` moves into the OPA bundle itself as a data document (`routes/data.json`), compiled by the policy-service. The Rego references `data.routes.route_scopes` to derive the required scope from method and path.

### Fate of the standalone policy-gateway

The standalone policy-gateway deployment (`orbit-policy-gateway`) **no longer serves as a Layer 1 enforcement point**. Its gRPC ext_authz server (`grpc.go`) and the `AuthorizationServer` are removed.

The policy-gateway deployment is retained only to host the HTTP `Check`/`Query` API (`handlers.go`) for:
- Non-Go consumers that cannot embed `platform/policy`
- The frontend BFF filtering UI elements
- Operator tooling and ad-hoc testing

If these use cases do not materialise, the policy-gateway deployment can be dropped entirely in a future iteration, leaving only the policy-service.

---

## Layer 2: `internal/platform/policy` — New Package

This is the single reusable entry point for any Go service that needs fine-grained in-process policy evaluation. It owns the OPA lifecycle: bundle fetching, compilation, caching, and background polling. Callers know nothing about OPA.

### Interface

```go
package policy

// Evaluator provides in-process OPA policy evaluation with automatic
// bundle lifecycle management. Zero network hops on the evaluation hot path.
//
// Instantiate with NewEvaluator and call Start before first use.
type Evaluator interface {
    // Check returns whether the principal may perform action on resource.
    // Lazily loads the tenant bundle on first call; subsequent calls are
    // pure in-process OPA evaluation.
    Check(ctx context.Context, input CheckInput) (Result, error)

    // Query filters candidates to those the principal may access.
    // Uses the same in-process bundle; no OPA round-trip per candidate.
    Query(ctx context.Context, input QueryInput) ([]string, error)

    // Start begins background bundle polling. Call once at service startup.
    Start(ctx context.Context)
}

// NewEvaluator returns an Evaluator backed by in-process OPA.
// bundleEndpoint is the base URL of the policy-service bundle API,
// e.g. http://orbit-policy-service.orbit-system.svc/api/v1/policy/bundles
func NewEvaluator(
    bundleEndpoint string,
    pollInterval   time.Duration,
    logger         logging.Logger,
) Evaluator
```

### Input types

```go
// CheckInput is the input to a single allow/deny decision.
type CheckInput struct {
    Claims       *token.PlatformClaims // validated platform JWT claims
    Delegation   *DelegationClaims     // nil if no delegation token present
    ResourceType string                // e.g. "inference"
    PathSegments []string              // e.g. ["anthropic", "claude-sonnet-4-6"]
    Action       string                // e.g. "invoke"
    Attributes   map[string]string     // optional resource metadata
}

// QueryInput is the input to a batch allow/deny across a resource catalog.
type QueryInput struct {
    Claims       *token.PlatformClaims
    Delegation   *DelegationClaims
    ResourceType string
    Action       string
    Candidates   []Candidate
}

// Candidate is a single resource in a Query catalog.
type Candidate struct {
    ID           string            // opaque identifier returned in allowed list
    PathSegments []string
    Attributes   map[string]string
}

// Result is the outcome of a Check call.
type Result struct {
    Allow  bool
    Reason string // "allow" or deny reason — log only, never return to caller
}

// DelegationClaims holds parsed delegation token fields.
type DelegationClaims struct {
    Sub    string   // delegating human (delegator_id in OPA input)
    ActSub string   // agent acting on behalf of delegator
    Scopes []string // granted scopes for this delegation session
}
```

### Usage in any service

```go
// cmd/inference-gateway/main.go
eval := policy.NewEvaluator(
    cfg.Policy.BundleEndpoint,
    cfg.Policy.BundlePollInterval(),
    logger,
)
eval.Start(ctx)

// In a handler:
result, err := eval.Check(ctx, policy.CheckInput{
    Claims:       claims,
    ResourceType: "inference",
    PathSegments: []string{string(provider), modelID},
    Action:       "invoke",
})
if err != nil || !result.Allow {
    // deny
}
```

No knowledge of OPA, bundles, or polling required by the caller.

---

## Package Structure Changes

### Delete

| Path | Reason |
|------|--------|
| `internal/policygw/evaluation/` | Moved to `internal/platform/policy/` |
| `internal/policygw/grpc.go` | ext_authz server removed — Layer 1 is now OPA sidecar |
| `internal/policygw/grpc_auth.go` | No longer needed without gRPC server |
| `internal/inferencegw/policy_gateway_evaluator.go` | Remote HTTP call replaced by `platform/policy` |
| `internal/inferencegw/policy.go` | Interface moves to `platform/policy` |

### Move / Rename

| From | To | Notes |
|------|-----|-------|
| `internal/policygw/evaluation/engine.go` | `internal/platform/policy/engine.go` | Package `evaluation` → `policy` |
| `internal/policygw/evaluation/input.go` | `internal/platform/policy/input.go` | Package renamed |
| `internal/policygw/evaluation/hierarchy.go` | `internal/platform/policy/hierarchy.go` | Package renamed |
| `internal/policygw/token.go` | `internal/platform/policy/delegation.go` | `parseDelegationToken` is platform logic |

### Modify

**`internal/policygw/handlers.go`** and **`internal/policygw/models.go`**  
Remain as the HTTP `Check`/`Query` utility API. Rewired to use `platform/policy.Evaluator` instead of `policygw/evaluation.Engine`.

**`cmd/policy-gateway/main.go`**  
Remove gRPC server setup. Instantiate `policy.NewEvaluator(...)` for the HTTP handlers only.

**`cmd/inference-gateway/main.go`**  
Instantiate `policy.NewEvaluator(...)` from `platform/policy`. Remove `PolicyGatewayEvaluator` wiring.

**`cmd/inference-gateway/config/config.go`** and **`base.yaml`**  
Add `Policy` struct with `BundleEndpoint` and `BundlePollIntervalSecs` fields.

### Add

| Path | What |
|------|------|
| `internal/platform/policy/evaluator.go` | `Evaluator` interface + `NewEvaluator` constructor |
| `internal/platform/policy/delegation.go` | `parseDelegationToken`, `DelegationClaims` |
| `charts/orbit/templates/policy-gateway/envoyfilter.yaml` | OPA ext_authz EnvoyFilter for agent pods |
| `charts/orbit-platform/templates/opa-injection-template.yaml` | Istio injection template for OPA sidecar |
| `internal/policyservice/bundle/` routes data | `routes/data.json` in bundle — method+path → scope mapping |

### What does NOT change

- `internal/policyservice/` — untouched
- `internal/policyservice/bundle/authz.rego` — untouched (same Rego at both layers)
- All policy-service Helm resources — untouched
- Policy-gateway Deployment and Service (HTTP port only — gRPC port removed)

---

## Bundle additions for Layer 1

The OPA Envoy plugin at Layer 1 needs to derive the required scope from the request's HTTP method and path. This mapping currently lives as Go code in `grpc.go`'s `defaultRouteScopes`. It moves into the bundle as a data document so that the same policy-service compilation step owns all policy data.

The policy-service adds `routes/data.json` to every tenant bundle:

```json
{
  "route_scopes": [
    { "prefix": "/v1/inference/", "method": "POST", "scope": "inference:invoke" },
    { "prefix": "/v1/inference/", "method": "GET",  "scope": "inference:list"   },
    { "prefix": "/v1/mcp/",       "method": "POST", "scope": "mcp:invoke"       },
    { "prefix": "/v1/mcp/",       "method": "GET",  "scope": "mcp:list"         }
  ]
}
```

The Rego gains a helper rule that looks up the required scope from `data.routes.route_scopes` using the request path and method from the Envoy input document. This keeps the Go code entirely free of route knowledge.

---

## Inference Gateway: Before vs After

**Before:**
```
model request → inference-gateway handler
  → PolicyGatewayEvaluator.EvaluateInferenceRequest()
      → POST http://orbit-policy-gateway.../v1/policy/check   ← network hop
  ← allow or 403
```

**After (Layer 1 — agent pod outbound):**
```
agent outbound → Envoy ext_authz → localhost:9191 (OPA sidecar)   ← loopback
  ← 403 if denied before request even leaves the pod
```

**After (Layer 2 — inference-gateway process):**
```
model request → inference-gateway handler
  → policy.Evaluator.Check(ctx, CheckInput{...})   ← pure in-process
  ← allow or 403
```

---

## Open Questions

1. **OPA sidecar per tenant bundle** — OPA's bundle plugin supports a single bundle endpoint. For multi-tenant, the bundle path must be parameterised by tenant ID. The OPA Envoy plugin receives the full request including the JWT from which the tenant can be extracted. The Rego must extract the tenant from the JWT and select the correct data partition within the bundle. The simplest approach: compile a single cross-tenant bundle in the policy-service (one bundle containing all tenants' grants under separate keys) rather than per-tenant bundles. This is a breaking change to the bundle format. Requires decision before implementation.

2. **OPA sidecar injection scope** — which pods get the OPA sidecar? Only agent pods (`orbit.io/identity-proxy: "enabled"`) for Layer 1. Service pods (inference-gateway) get `platform/policy.Evaluator` embedded for Layer 2 — they do not need the OPA sidecar. Confirm this is the intended split.

3. **Layer 1 failure mode** — `failure_mode_allow: false` and a 10ms timeout. OPA sidecar startup race (pod starts, OPA has not yet loaded bundle) will cause request denials until OPA is ready. OPA's readiness probe must gate pod readiness. Confirm acceptable.

4. **Token re-verification at Layer 2** — the inference gateway re-verifies the platform JWT for Layer 2. By this point Layer 1 has already validated it. This is intentional defence-in-depth but adds one JWKS cache lookup per request. Note as accepted cost.
