# Platform-wide Scope Enforcement Middleware

**Status:** Accepted
**Date:** 2026-06-04
**Deciders:** Platform Engineering
**Technical Story:** A scope-enforcement gap was found in orbit-agents while implementing [orbit-agents-authorization-model](2026-06-04-orbit-agents-authorization-model.md): `POST /api/v1/agent-types` had no `agents:types:write` check, so any authenticated user could create agent types. Investigation showed the same gap exists in policy-service (`POST /grants`, `PUT /roles`) and identity (`POST /pats`) — these handlers only verify JWT validity and tenant equality. The fix needs to be uniform across all platform services, not service-by-service hand-rolling.

---

## Context

### Background

[policy-gateway-and-authorisation-model](2026-04-14-policy-gateway-and-authorisation-model.md) defined the policy-gateway and authorization model. [dynamic-route-scope-registration](2026-04-14-dynamic-route-scope-registration.md) added dynamic route→scope registration so services can declare `(method, pathPrefix) → requiredScope` at startup. [orbit-agents-authorization-model](2026-06-04-orbit-agents-authorization-model.md) added in-process tenant + per-resource enforcement to orbit-agents via `policy.Evaluator`. Together these give the platform:

- A way to register what scope each route requires (`routescopes.Client.Register`).
- A way to ask OPA whether a principal has a scope (`policy.Evaluator.Check`).
- A consistent error path (`platformapi.ForbiddenError`).

What is missing is a piece that **closes the loop**: a uniform middleware that, on every request to a platform service, derives the required scope from the route table and consults the evaluator. Today each service is expected to remember to do this in handler code, and as the recent bug demonstrates, services forget.

### Problem Statement

The platform's enforcement model is split between two layers:

| Layer | Mechanism | Where it runs |
|---|---|---|
| **Layer 1 — Envoy ext_authz → OPA sidecar** | Route-scope check before traffic reaches the application | Sidecar in pod, bypasses application code |
| **Layer 2 — `policy.Evaluator` in service code** | Same Rego, but invoked in-process by the application | Application code |

Layer 1 is correct for **tenant agent pods** (the LLM agent workloads) because we cannot trust an untrusted workload to enforce its own scopes — the sidecar terminates Envoy's ext_authz protocol and denies before the agent ever runs. [spiffe-spire-workload-attestation](2026-03-05-spiffe-spire-workload-attestation.md) enforces this via the `orbit.io/opa-policy: "enabled"` pod label.

Layer 2 is correct for **platform services** because we control their code and adding a sidecar to every service adds memory/CPU per pod for a check we already have an in-process evaluator for.

**The bug is that no platform service runs Layer 1 as a sidecar, and most platform services don't run Layer 2 as Go code either.** They sit between layers — `AuthMiddleware` validates the JWT and then handlers proceed without ever consulting scopes. Tenant equality (`resolveTenantID`) is the only thing that prevents cross-tenant writes; nothing prevents same-tenant unauthorized writes.

The evidence (verified by code inspection on 2026-05-29):

| Service | Has `policy.Evaluator` consult on writes? |
|---|---|
| inference-gateway | ✅ Yes — `service.go:541 checkModelPolicy`, `:606 filterTargetsByPolicy` |
| orbit-agents (post-[orbit-agents-authorization-model](2026-06-04-orbit-agents-authorization-model.md)) | Partial — per-resource read/write checks present, **`POST /agent-types` had no check** until this ADR's middleware |
| policy-service | ❌ No — `handlers_grants.go:11 handleCreateGrant` only verifies JWT + tenant equality |
| identity | ❌ No on most write paths — only uses evaluator to validate requested PAT scopes against caller's effective scopes |

Building Layer 2 in handler code repeatedly is fragile. The route→scope vocabulary is already registered with the policy-service ([dynamic-route-scope-registration](2026-04-14-dynamic-route-scope-registration.md)). What's missing is a single piece of glue that walks the registered table and enforces it.

### Goals

- A single platform middleware that performs Layer 1 enforcement in-process for any service that declares its routes via `[]routescopes.RouteScope`.
- Behavior matches the route matching in `bundle/authz.rego` and `bundle/authz_gateway.rego` exactly: an exact-method route beats a `"*"` route, the longest prefix wins, and a prefix matches on a path segment boundary. Unmatched routes deny-close.
- Zero per-handler code; the only per-service change is one extra middleware in the chain.
- Per-resource fine-grained decisions (`checkRun`, `filterAgentTypes`) remain in service code; this middleware handles the coarse "do you have the scope at all" check.
- Internal endpoints (mTLS-protected callbacks) and liveness probes bypass cleanly.

### Non-Goals

- Replacing per-resource decisions. Filtering a list to "what can this principal see", checking "is this run owned by this tenant", checking "does this principal hold the slug-specific grant" — all of those stay in service code where the resource shape is available.
- Adding `opa-policy` sidecars to platform services. Not needed; the existing `policy.Evaluator` does the work in-process and matches Layer 1's Rego.
- Changing the route-scope registration contract (`routescopes.Client`). The middleware consumes the same data already being registered.
- Handling the special case of services that legitimately have no scope-checked routes (frontend, opa-gateway). They simply do not install the middleware.

---

## Decision Drivers

- **Mechanical safety.** "Remember to call `Check` in every handler" produces silent gaps when forgotten. A middleware can't be forgotten — the controller composition itself enforces presence.
- **Uniform pattern.** One way to do it across orbit-agents, policy-service, identity, inference-gateway, and any future platform service. Engineers learn it once.
- **Reuse existing primitives.** `routescopes.RouteScope`, `policy.Evaluator`, `platformapi.ForbiddenError` already exist; the middleware just composes them.
- **Matches Layer 1 semantics.** Tenant agent pods enforce route-scope via Envoy ext_authz; platform services enforce the same data structure in-process. Symmetry simplifies reasoning about the security boundary.
- **Avoids sidecar overhead.** Adding `opa-policy` to every platform pod would double the OPA bundle-poll fan-out (one evaluator per service + one sidecar per service) for an enforcement we already have.

---

## Considered Options

### Option 1: Per-handler `eval.Check` calls

Each handler that performs a scoped action calls `policy.Evaluator.Check(...)` at the top, like `inferencegw/service.go:541 checkModelPolicy` does.

**Pros:**
- Explicit at the call site — easy to read.
- Per-handler granularity if the scope depends on the action (rarely needed).

**Cons:**
- Fragile — the orbit-agents bug exists because this pattern was followed for some handlers and missed for `CreateAgentType`.
- Duplicate boilerplate across every service.
- No central place to enforce "every public route MUST have a scope check".

### Option 2: Add `opa-policy` sidecar to platform service pods

Same mechanism used for tenant agent pods (Envoy ext_authz → OPA sidecar). The route-scope bundle is fetched by the sidecar; Envoy denies unauthorized requests before they reach the application.

**Pros:**
- Zero application code; enforcement is purely infrastructure.
- Identical mechanism on platform and tenant pods — one mental model.
- Defense in depth — enforcement happens before the application process even sees the request.

**Cons:**
- Resource overhead — OPA sidecar per pod (memory ~50–100MB, CPU at policy fetch time) on every platform replica.
- Duplicates effort with existing `policy.Evaluator` instances already running in services like inference-gateway. Each evaluator and each OPA sidecar polls the same bundle endpoint.
- Adds a deploy-time concern (Istio template list) where today there is none for new platform services.
- The OPA sidecar's enforcement is **only as good as the route-scope bundle**, which is data already loaded by `policy.Evaluator` in every platform service — so the sidecar adds no enforcement that an in-process middleware couldn't.

### Option 3: Platform middleware — `ScopeEnforcementMiddleware` (Chosen)

A middleware in `platform/middleware/scope_enforce.go` that takes `policy.Evaluator` and `[]routescopes.RouteScope` and performs Layer-1-style enforcement in code on every request.

**Pros:**
- One implementation, applied uniformly to all platform services that opt in by adding it to `Middleware()`.
- Reuses `policy.Evaluator` and `routescopes.RouteScope` — no new primitives, no new wire formats.
- Behavior maps 1:1 to the Rego `_required_scope` rule, so OPA-side and code-side enforcement are semantically identical.
- Forgotten checks become impossible — the middleware fail-closes on routes not in the table, surfacing missing registrations as 403s during testing rather than silent allow-throughs in production.
- Zero infrastructure changes — no chart edits, no sidecar updates.

**Cons:**
- Per-resource decisions still need service-layer code (this middleware only covers coarse route-scope enforcement).
- One more piece in the controller's `Middleware()` chain to remember when standing up a new service. Mitigated by documentation and PR review checklists.
- If a service forgets to register a new route in `agentRoutes()` (or equivalent), the route fail-closes to 403 in production. This is the correct fail-mode but requires test coverage to catch.

---

## Decision

**Chosen Option:** Option 3 — platform `ScopeEnforcementMiddleware`.

### Rationale

Option 1 is what we have today and it has produced a real security gap. Option 2 is correct for untrusted workloads (tenant agent pods) but pays an infrastructure cost we don't need to pay for code we control. Option 3 closes the gap with one composable piece, reuses every existing primitive, and matches the Rego semantics exactly.

The split — "Layer 1 in middleware for platform services we control; Layer 1 in sidecar for tenant agent pods we don't" — is the principled trust boundary: in-process enforcement is fine where we author the process; sidecar enforcement is required where we don't.

---

## Consequences

### Positive

- The `CreateAgentType` bug class becomes impossible to reproduce — once `ScopeEnforcementMiddleware` is in the chain, every route with a registered scope is checked, and routes without a registered scope deny.
- One implementation, one set of tests, one mental model. Future services adopt by registering routes (which they already do for [dynamic-route-scope-registration](2026-04-14-dynamic-route-scope-registration.md)) and adding one middleware to their chain.
- The `routescopes.RouteScope` definitions become load-bearing in code, not just policy-side metadata. This raises the value of registration discipline.
- Decommissioning sidecars for platform services becomes a non-issue — we never installed them.

### Negative

- Services that have unregistered public routes start returning 403 after this middleware is wired. This is the correct behavior but is a behavior change. We will roll out per-service with explicit smoke tests that exercise every public route.
- The fail-closed behavior makes route registration a hard precondition for shipping a new public endpoint. Forgetting to register a new PATCH route, for example, produces 403s in production unless caught in CI. Mitigated by:
  - A linter rule (deferred) that diffs `mux.Handle(...)` calls against `agentRoutes()` and warns on omissions.
  - Integration tests that exercise every public route end-to-end with a known-scope token.
- The middleware adds one `policy.Evaluator.Check` call per request. Cost is negligible (in-process, no network) but measurable in micro-benchmarks.

### Neutral

- The orbit-agents `Authz.checkAgentTypesAction` helper added on 2026-05-29 becomes redundant once the middleware is wired. It is removed in the same PR that introduces the middleware. Per-resource helpers (`checkRun`, `checkAgentType`, `filterAgentTypes`, `readableRunAgentTypes`) remain — they enforce things the middleware cannot.
- `routescopes.RouteScope` becomes the canonical source for route enforcement, both at registration time ([dynamic-route-scope-registration](2026-04-14-dynamic-route-scope-registration.md)) and at request time (this ADR). This may invite future migration of the registration data into a shared package.

---

## Implementation

### `platform/middleware/scope_enforce.go`

```go
package middleware

import (
    "net/http"
    "strings"

    platformapi "github.com/runkora/kora/backend/internal/platform/api"
    "github.com/runkora/kora/backend/internal/platform/clients/routescopes"
    "github.com/runkora/kora/backend/internal/platform/logging"
    "github.com/runkora/kora/backend/internal/platform/policy"
    "github.com/runkora/kora/backend/internal/platform/token"
)

// ScopeEnforcementMiddleware enforces the route → required-scope binding
// declared by the service via routescopes.Register. Behavior mirrors the
// OPA `_required_scope` rule in bundle/authz.rego:
//
//   1. Match the request path against route.PathPrefix (prefix match).
//   2. Among matching routes, prefer one whose Method equals the request method
//      exactly; otherwise fall through to one with Method == "*".
//   3. If a route matches, call policy.Evaluator.Check with the resolved scope.
//   4. If no route matches, deny (mirrors OPA `deny:no_route_match`).
//
// This middleware MUST run after AuthMiddleware so token.ClaimsFromContext
// returns the verified principal.
func ScopeEnforcementMiddleware(eval policy.Evaluator, routes []routescopes.RouteScope, logger logging.Logger) Middleware {
    return NewMiddleware(func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            ctx := r.Context()
            requiredScope, ok := resolveRequiredScope(routes, r.Method, r.URL.Path)
            if !ok {
                writeForbidden(ctx, w, logger, "no route match")
                return
            }
            claims, _ := token.ClaimsFromContext(ctx)
            result, err := eval.Check(ctx, policy.CheckInput{
                Claims:       claims,
                Delegation:   policy.DelegationFromContext(ctx),
                RequiredScope: requiredScope,
            })
            if err != nil {
                logger.Error(ctx, "scope enforcement evaluator failed",
                    logging.Field{Key: "error", Value: err.Error()},
                    logging.Field{Key: "required_scope", Value: requiredScope},
                )
                writeForbidden(ctx, w, logger, "policy unavailable")
                return
            }
            if !result.Allow {
                writeForbidden(ctx, w, logger, result.Reason)
                return
            }
            next.ServeHTTP(w, r)
        })
    })
}

// resolveRequiredScope mirrors authz.rego's _required_scope derivation.
// Returns the matched scope and true; returns "", false if no route matches.
func resolveRequiredScope(routes []routescopes.RouteScope, method, path string) (string, bool) {
    var wildcard string
    var wildcardMatched bool
    for _, r := range routes {
        if !strings.HasPrefix(path, r.PathPrefix) {
            continue
        }
        if r.Method == method {
            return r.RequiredScope, true // exact match wins immediately
        }
        if r.Method == "*" && !wildcardMatched {
            wildcard = r.RequiredScope
            wildcardMatched = true
        }
    }
    return wildcard, wildcardMatched
}
```

### Note on `policy.CheckInput`

The current `policy.CheckInput` is shaped for resource-keyed lookups (`ResourceType` + `PathSegments` + `Action`) so callers can also populate `Attributes` for attribute-based decisions in OPA (`input.resource.attributes`). The middleware does not have a resource — it has a pre-built scope string from the route table.

Rather than introduce a parallel "pre-built scope" field on `CheckInput` (which would duplicate the intent and require precedence rules), the middleware inverts `policy.DeriveRequiredScope` with a small `splitScope` helper: it splits the route table's scope string back into the three fields `CheckInput` expects. `DeriveRequiredScope` then rebuilds the same string inside the engine and passes it to OPA as `input.required_scope`. The round-trip is intentional — it lets `CheckInput` keep one canonical shape for both callers (resource-shaped and scope-shaped) without precedence magic.

### Wiring example (orbit-agents)

```go
func (c *Controller) Middleware() mw.MiddlewareHandler {
    authChain := mw.MiddlewareChain(
        mw.AuthMiddleware(c.verifier, c.logger),
        mw.DelegationMiddleware(c.logger, c.verifier),
        mw.ScopeEnforcementMiddleware(c.eval, agentRoutes(), c.logger),  // NEW
    )
    return func(next http.Handler) http.Handler {
        authed := authChain(next)
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            if strings.HasPrefix(r.URL.Path, "/internal/") || r.URL.Path == "/health" {
                next.ServeHTTP(w, r)
                return
            }
            authed.ServeHTTP(w, r)
        })
    }
}
```

The bypass for `/internal/*` and `/health` stays in the controller (those paths must not see auth or scope enforcement). Everything else flows through `Auth → Delegation → ScopeEnforce → handler`.

### Migration Path

Phased per service. Each phase adds the middleware and verifies every public route end-to-end with a token that holds the expected scope.

1. **orbit-agents** (this PR / immediate). The route table (`cmd/kora-agents/main.go:agentRoutes`) needs a PATCH entry for `/api/v1/agent-types/{id}` which is currently missing — adding it is part of this PR.
2. **policy-service** — closes the same gap on `POST /grants`, `PUT /roles`, etc. Route table needs construction.
3. **identity** — closes the same gap on PAT and session admin paths.
4. **inference-gateway** — already does in-process per-resource checks; adding the middleware is defense-in-depth and removes the per-handler boilerplate.

Each phase is its own PR.

### Timeline

Phase 1 (orbit-agents) lands in the PR that addresses the merge-readiness audit. Phases 2–4 follow as separate PRs within the next sprint.

---

## Validation

### Success Metrics

- **Scope checks on every public route in orbit-agents.** Test: send a request with a token holding no `agents:*` grant; assert 403 on every public route, 200/204 on `/health` and `/internal/*`.
- **Unmatched routes deny.** Test: add a `mux.Handle` for a path not in `agentRoutes()`; the test asserts 403.
- **Existing happy paths still work.** Existing handler tests pass unchanged for tokens that hold the expected scope.

### Monitoring

- Counter metric `orbit_scope_enforce_denied_total{service, scope, reason}` emitted on every deny — exposes both legitimate denies and unregistered-route gaps.
- Existing `policy.Evaluator` bundle-load metrics continue to apply (no new evaluator state).

---

## Related Decisions

- [policy-gateway-and-authorisation-model](2026-04-14-policy-gateway-and-authorisation-model.md) — the broader authorization model this fits into.
- [dynamic-route-scope-registration](2026-04-14-dynamic-route-scope-registration.md) — the route-scope registration mechanism this middleware consumes.
- [orbit-agents-route-scope-registration](2026-05-11-orbit-agents-route-scope-registration.md) — orbit-agents' specific route registration; this ADR makes those registrations load-bearing in code.
- [orbit-agents-authorization-model](2026-06-04-orbit-agents-authorization-model.md) — per-resource fine-grained enforcement in orbit-agents; this ADR adds the coarse route-level layer underneath it.

---

## References

- `backend/internal/platform/clients/routescopes/client.go` — `RouteScope` struct definition.
- `backend/internal/platform/policy/evaluator.go` — `Evaluator.Check` shape.
- `backend/internal/policyservice/bundle/authz.rego:113-137` — the OPA `_required_scope` rule this middleware mirrors.
- `backend/internal/inferencegw/service.go:541-628` — the Layer 2 enforcement pattern that motivated this generalization.
- The 2026-05-29 audit conversation that surfaced the `CreateAgentType` gap.

---

**Last Updated:** 2026-05-29
