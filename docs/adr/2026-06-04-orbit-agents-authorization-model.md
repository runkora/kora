# Service-Layer Authorization for orbit-agents

**Status:** Accepted
**Date:** 2026-06-04
**Deciders:** Platform Engineering
**Technical Story:** A merge-readiness audit of `feat/agents-frontend` (see `agents-merge-audit.md`) found that the orbit-agents controller registers no middleware and trusts `tenant_id` from request body and query string. There is no authentication or authorization in the Go layer. This ADR defines how orbit-agents enforces authorization in-process, reusing the existing `platform/policy.Evaluator` pattern adopted by the inference gateway.

---

## Update — 2026-06-03: Internal callback endpoints must be authenticated in-process

> This addendum corrects the security posture for the `/internal/runs/*` callback
> endpoints (state transition, event push, sandbox provisioning). The original
> sections below state these are "authenticated by SPIFFE/mTLS at the Envoy
> layer", "gated by Istio AuthorizationPolicy", and "bypassed in Rego". A
> post-merge review of PR #61 found that claim to be both **unimplemented and
> insufficient**, and was empirically validated in the `k3d-orbit-local` cluster.

**Findings:**

1. **No mesh control existed.** `Controller.Middleware` skipped the entire auth
   chain for any `/internal/` path. No Istio `AuthorizationPolicy` and no
   `PeerAuthentication` selected orbit-agents (only a policy-service
   AuthorizationPolicy and an orbit-agents client-side `DestinationRule` ship).
   With no ALLOW policy selecting the workload, Istio allows all traffic, and the
   `allow-tenant-mesh-ingress` NetworkPolicy lets any tenant-namespace pod reach
   orbit-agents on :8080. Reproduced: an unrelated tenant pod, with no token,
   POSTed `/internal/runs/{id}/events` (204), `/state` (409 — CAS executed), and
   `/provision-sandbox` (404) — all unauthenticated.

2. **A mesh policy alone is insufficient anyway.** The legitimate caller (the
   session-manager) shares the agent pod's SPIFFE identity
   `spiffe://<td>/tenant/<ns>/agent/<slug>`, which is indistinguishable in shape
   from every other agent pod. An AuthorizationPolicy restricting `/internal/*`
   to `…/agent/*` principals would not stop one agent pod from forging callbacks
   for another agent's (or tenant's) runs.

**Decision:** authenticate `/internal/*` with the agent's platform JWT — already
injected on every agent-pod outbound request by the identity-proxy ext_proc
sidecar (`cmd/orbit-agents` k8s.go: *"actual auth is the identity-proxy-issued
JWT"*) — and bind the verified identity to the target run in-process:

- Run `AuthMiddleware` on `/internal/*` (verify + populate claims). Do **not** run
  `ScopeEnforcementMiddleware` there: the route→scope table only covers the
  public `/api/v1` surface and agent principals hold no route scopes.
- Each internal handler calls `RunService.AuthorizeAgentCallback`, which requires
  `PrincipalType == AGENT`, `claims.tenant_id == run.TenantID`, and the agent
  slug (from `display_name` / the SPIFFE path in claims) `== run's agent-type
  slug`. Deny → 403; unknown run → 404 (no existence leak).

This makes the JWT the authoritative control and needs no SPIFFE/XFCC parsing. A
mesh `AuthorizationPolicy` + `PeerAuthentication` STRICT remain worthwhile as
defense-in-depth but are no longer load-bearing and are tracked separately.

**Validated in-cluster (2026-06-03):** no-token `/internal/*` → 401 (was 204);
authenticated-but-unowned run → 404; public route unchanged at 403. Cross-tenant
and cross-slug denial (403) covered by unit tests in `internal/agents`.

---

## Context

### Background

[orbit-agents-route-scope-registration](2026-05-11-orbit-agents-route-scope-registration.md) registered orbit-agents' route → scope vocabulary with the policy-service (`agents:types:read|write`, `agents:runs:read|write`, plus per-slug variants). That gave the policy-service the inputs it needs to compile bundles, but it did not wire any enforcement code inside orbit-agents itself.

The platform already has a complete in-process authorization stack:

- **`platform/middleware.AuthMiddleware`** — validates the platform JWT, stores `*token.PlatformClaims` in `context.Context`. Used today by `identity`, `policy-service`, and `inference-gateway` controllers.
- **`platform/policy.Evaluator`** — fetches the OPA bundle from `policy-service` in the background and evaluates allow/deny decisions in-process (zero network hops on the hot path). Exposes:
  - `Check(ctx, CheckInput) (Result, error)` — single allow/deny.
  - `Query(ctx, QueryInput) ([]string, error)` — batch filter; returns the subset of candidates the principal may access.
  - `Scopes`, `ScopesExplain` — projection used by the policy gateway.
- **`platform/policy.DelegationFromContext`** — reads delegation claims set by `DelegationMiddleware`, so any authorization call can honour delegated scopes without per-handler code.
- **OPA scope hierarchy (`scope_covers` in `bundle/authz.rego:394-409`)** — a parent scope automatically covers all child resource scopes. `agents:runs` covers `agents:runs:tenant-foo:slug-bar:read`, so admin/tenant/per-resource decisions share one code path with no Go branching.

The inference-gateway uses this stack in two characteristic ways:

- `service.go:541-568 checkModelPolicy` — single-resource check before a chat completion.
- `service.go:576-628 filterTargetsByPolicy` — batch filter of routing candidates against the principal's grants.

kora-agents needs the same two shapes: single-resource decisions for `GET/PATCH/DELETE /agent-types/{id}` and friends, batch filtering for `GET /agent-types` and `GET /runs`.

### Problem Statement

Today:

1. `internal/agents/server.go:27 Middleware() returns nil` — no JWT verification, no claims in context.
2. `handleListAgentTypes`, `handleListRuns`, `handleCreateAgentType`, `handleStartRun` all read `tenant_id` from the request body/query string (`server.go:94, 71, 181, 204`). The Go code never compares it to the caller's identity.
3. `handleGetRun`, `handleTerminateRun`, `handleSendMessage`, `handleInterruptRun`, `handleStreamEvents` look up by `r.PathValue("id")` with no ownership check at all.

Consequences in production:

- A token holder for tenant A can read, mutate, terminate, and stream events from tenant B's runs by passing tenant B's id in the body.
- Per-agent-type grants registered with the policy service (`agents:runs:{slug}:read`) are not consulted anywhere in orbit-agents.
- The list endpoints cannot filter by per-agent-type grants — a user authorised for `slug-foo` but not `slug-bar` would see both, because there is no Go-side filter.
- The cross-tenant case (platform admin operating on any tenant) cannot be modelled without inventing a new mechanism.

### Goals

- Replace request-body `tenant_id` trust with token-derived identity and policy-based authorization.
- Reuse the existing `platform/policy.Evaluator` — no new authorization primitive.
- Single decision path for all principal types: tenant user, per-agent-type grant holder, platform admin, delegated agent.
- List endpoints filter automatically by what the principal may read — no separate admin vs user code path.
- Keep handlers handler-shaped: no per-handler boilerplate beyond the service call already there.
- Cross-tenant access works by virtue of a coarser scope grant, not a special case.
- Internal session-manager callbacks (`/internal/runs/...`) continue to authenticate via mTLS/SPIFFE at the Envoy layer (already covered by the Rego `_is_platform_internal_path` rule).

### Non-Goals

- Authentication itself is not redesigned — we are adopting the existing `AuthMiddleware`.
- The route→scope vocabulary ([orbit-agents-route-scope-registration](2026-05-11-orbit-agents-route-scope-registration.md)) is not changing — it is consumed as-is.
- The session-manager's outbound notifier (orbit-agents internal endpoints) is not changing — those are gated by Istio AuthorizationPolicy. A future ADR may move them to typed clients (see merge audit §1.4) but that is orthogonal.
- The BFF (`frontend/src/app/api/agents/`) does not change — it already forwards the user's bearer token; the change is purely backend.

---

## Decision Drivers

- **Consistency with inference-gateway.** Engineers already understand the `policy.Evaluator` pattern. Adopting it here means one mental model, not two.
- **Single hot-path decision.** `Evaluator.Check` and `Evaluator.Query` are pure in-process OPA calls — no per-request network round-trip.
- **Scope hierarchy is the cross-tenant story.** Platform admins do not need a special branch in Go. They have a coarser scope grant; `scope_covers` handles the rest. The same code path works for them, a tenant user, and a per-slug grant holder.
- **List filtering is non-negotiable.** Users with per-agent-type grants must see only their grants reflected in list responses. A single `Query` call replaces both the admin/non-admin branch and the filter loop.
- **Audit traceability.** Every Allow/Deny carries a `Reason` string (`grant:scope`, `role:role-name`, `delegation:scope`, `deny:no_matching_grant`). Logging this attaches durable rationale to every authorization decision.

---

## Considered Options

### Option 1: Per-handler `Allow` checks

Each handler calls `c.authz.Allow(ctx, scope, tenantID)` at the top, after reading `tenant_id` from the request as today.

**Pros:**
- Minimal abstraction; obvious where the decision lives.
- Handlers stay self-contained.

**Cons:**
- 14 handlers × ~5 lines of boilerplate each.
- One forgotten handler is a silent tenant leak — a fragile policy.
- List endpoints still need a separate filter pass (no `Query` reuse) — duplicates the call.
- No place to centralise the resource-shape (path segments) that goes into `CheckInput`.

### Option 2: Repository-layer tenant scoping from `ctx`

The PostgreSQL repository reads `claims.Orbit.TenantID` from context and silently appends `AND tenant_id = $N` to every query.

**Pros:**
- Forgotten tenant checks become mechanically impossible — the SQL enforces.
- Handler code shrinks (no tenant params at all).

**Cons:**
- Breaks platform admins who must read/write across tenants.
- Breaks future cross-tenant collaborator grants — the SQL filter has no notion of a multi-tenant principal.
- Internal endpoints, called by session-manager with no user JWT, would fail (no claims in ctx).
- Couples the data layer to the auth subsystem — repos should be pure data access.
- Cannot express per-agent-type grants — the filter only knows tenant.

### Option 3: Service-layer `authz` helper backed by `policy.Evaluator` (Chosen)

A thin `authz` type lives inside `internal/agents/`, wrapping `platform/policy.Evaluator`. It exposes a small surface (`checkAgentType`, `filterAgentTypes`, `checkRun`, `readableRunAgentTypes`) that hides the `CheckInput`/`QueryInput` plumbing. Service methods call into it before returning data; handlers stay handler-shaped.

**Pros:**
- One pattern matches inference-gateway exactly — no new abstraction to learn.
- `Query` gives automatic list filtering — admin, tenant, and per-slug grants share one code path.
- Scope hierarchy in Rego handles admin/tenant/granular in OPA, not in Go.
- Handlers do not call `authz` directly — services do, so the abstraction is enforced by API shape (you cannot get data out of the service without going through `authz`).
- Delegation is honoured for free via `DelegationFromContext`.
- Internal endpoints bypass cleanly via the existing `_is_platform_internal_path` Rego rule — no Go-side branching.

**Cons:**
- Adds a dependency from `internal/agents` to `internal/platform/policy`. (Already the case for inference-gateway; not a new precedent.)
- Service methods must have claims in `ctx`. Code paths that bypass `AuthMiddleware` (tests, internal callbacks) need to inject claims or call lower-level repo methods directly.
- Bundle must be loaded before serving traffic — a brief startup window where `Check` returns "bundle not loaded" must be handled gracefully (fail closed).

---

## Decision

**Chosen Option:** Option 3 — service-layer `authz` helper backed by `policy.Evaluator`.

### Rationale

The pattern is already established by inference-gateway and is the smallest delta from how the platform thinks about authorization today. It is the only option that:

1. Models platform admins, tenant users, and per-agent-type grants on one code path (the scope hierarchy in Rego does the work).
2. Filters list responses by per-resource grants automatically via `Query`, with no Go-side branching.
3. Keeps handlers free of authorization boilerplate while still making the enforcement point obvious (every service method that returns data calls `authz` before returning).

Option 1 fails on fragility (forgotten checks). Option 2 fails on the platform-admin / cross-tenant / internal-endpoint cases. Option 3 inherits the lessons already encoded in `bundle/authz.rego` and `platform/policy.Evaluator`.

---

## Consequences

### Positive

- One authorization model for orbit-agents and inference-gateway; engineers do not learn a new abstraction.
- Per-agent-type grants registered via [orbit-agents-route-scope-registration](2026-05-11-orbit-agents-route-scope-registration.md)'s vocabulary become enforceable end-to-end.
- List endpoints filter automatically — a user with `agents:runs:slug-foo:read` and no other grant sees only `slug-foo` runs in `GET /runs`.
- Platform admins work without special-case Go code — they hold a coarser scope (`agents:*` or similar), which `scope_covers` propagates over all sub-resources.
- Internal `/internal/runs/*` endpoints are unaffected — they are already authenticated by SPIFFE/mTLS and bypassed in Rego.
- Delegation tokens flow into authorization decisions via `DelegationFromContext` with no per-handler code.
- Every decision carries a `Reason` string suitable for structured logging and audit.

### Negative

- `internal/agents` gains a runtime dependency on `internal/platform/policy`. Tests now need to provide either a real evaluator (with a stub bundle) or a fake.
- Service methods grow a `claims` precondition. Test setup needs to construct `*token.PlatformClaims` and put it in `ctx` via `token.WithClaims`. (A test helper will be added.)
- Bundle warmup window: the first ~100ms of process lifetime, `Check` may return "bundle not loaded". Code must `Evaluator.Start` early and either block readiness on first successful load or fail closed for requests in that window. We choose to fail closed and document this in the readiness probe.
- The pre-existing `tenant_id` request body / query fields on the public API become ignored. The BFF currently sends them anyway — no contract change for clients, but the handler will derive tenant from claims and ignore the body field.

### Neutral

- The `RunService` and `AgentTypeService` method signatures change to drop `tenantID` parameters in favour of reading from `ctx`. Call sites inside `cmd/orbit-agents/main.go` (the reaper, scope-registration hook) need claims-bearing contexts. The reaper is internal and operates on all tenants — it will use a privileged service context (covered in Implementation).

---

## Implementation

### High-level shape

```
internal/agents/
├── authz.go              ← NEW: thin wrapper around policy.Evaluator
├── server.go             ← Middleware() now returns mw.AuthMiddleware
├── service.go            ← AgentTypeService takes *authz
├── run_service.go        ← RunService takes *authz; methods drop tenantID args
├── agent_type_service.go ← drops tenantID args; reads from claims
└── ...

cmd/orbit-agents/main.go  ← constructs policy.Evaluator; passes to services
```

### `authz.go` — the new file

```go
package agents

import (
    "context"
    "errors"
    "fmt"

    "github.com/runkora/kora/backend/internal/platform/policy"
    "github.com/runkora/kora/backend/internal/platform/token"
)

// ErrForbidden is returned when the principal lacks the required scope.
// Handlers translate this to either 403 or 404 depending on whether the
// resource's existence is itself a secret.
var ErrForbidden = errors.New("forbidden")

type authz struct {
    eval policy.Evaluator
}

func newAuthz(eval policy.Evaluator) *authz { return &authz{eval: eval} }

func (a *authz) checkAgentType(ctx context.Context, at AgentType, action string) error {
    r, err := a.eval.Check(ctx, policy.CheckInput{
        Claims:       claimsFromCtx(ctx),
        Delegation:   policy.DelegationFromContext(ctx),
        ResourceType: "agents",
        PathSegments: []string{"types", at.Slug},
        Action:       action,
    })
    if err != nil {
        return fmt.Errorf("policy check: %w", err)
    }
    if !r.Allow {
        return ErrForbidden
    }
    return nil
}

func (a *authz) filterAgentTypes(ctx context.Context, ats []AgentType) ([]AgentType, error) {
    if len(ats) == 0 {
        return ats, nil
    }
    candidates := make([]policy.Candidate, len(ats))
    for i, at := range ats {
        candidates[i] = policy.Candidate{
            ID:           at.ID,
            PathSegments: []string{"types", at.Slug},
        }
    }
    allowed, err := a.eval.Query(ctx, policy.QueryInput{
        Claims:       claimsFromCtx(ctx),
        Delegation:   policy.DelegationFromContext(ctx),
        ResourceType: "agents",
        Action:       "read",
        Candidates:   candidates,
    })
    if err != nil {
        return nil, fmt.Errorf("policy query: %w", err)
    }
    set := make(map[string]struct{}, len(allowed))
    for _, id := range allowed {
        set[id] = struct{}{}
    }
    out := ats[:0]
    for _, at := range ats {
        if _, ok := set[at.ID]; ok {
            out = append(out, at)
        }
    }
    return out, nil
}

// Equivalent checkRun / readableRunAgentTypes operate on path segments ["runs", agentSlug].
// Tenant is implicit in the per-tenant policy bundle; cross-tenant isolation
// is provided by bundle scoping, not by encoding tenantID in path segments.

func claimsFromCtx(ctx context.Context) *token.PlatformClaims {
    c, _ := token.ClaimsFromContext(ctx)
    return c
}
```

### Handler shape after the change

```go
func (c *Controller) handleListRuns(w http.ResponseWriter, r *http.Request) {
    ctx := r.Context()
    agentTypeID := r.URL.Query().Get("agent_type_id")
    runs, err := c.runs.ListRuns(ctx, agentTypeID)  // tenant gone from signature
    // ... error response / api.JSON unchanged
}

func (c *Controller) handleGetRun(w http.ResponseWriter, r *http.Request) {
    ctx := r.Context()
    run, err := c.runs.GetRun(ctx, r.PathValue("id"))
    if errors.Is(err, ErrForbidden) || errors.Is(err, ErrNotFound) {
        // 404 to avoid leaking existence
        api.ErrorResponse(ctx, w, c.logger, api.NotFoundError(...), nil)
        return
    }
    // ...
}
```

### Service shape after the change

```go
func (s *RunService) GetRun(ctx context.Context, id string) (*Run, error) {
    run, err := s.repo.GetRun(ctx, id)
    if err != nil {
        return nil, err
    }
    at, err := s.atRepo.GetAgentType(ctx, run.AgentTypeID)
    if err != nil {
        return nil, err
    }
    if err := s.authz.checkRun(ctx, *run, *at, "read"); err != nil {
        return nil, err  // ErrForbidden — handler maps to 404
    }
    return run, nil
}

func (s *RunService) ListRuns(ctx context.Context, tenantID string, q RunListQuery) ([]Run, error) {
    ats, err := s.atRepo.ListAgentTypes(ctx, tenantID)
    if err != nil {
        return nil, err
    }
    // One policy query over the agent types; the repository filters on the result in SQL.
    readable, err := s.authz.readableRunAgentTypes(ctx, ats)
    if err != nil {
        return nil, err
    }
    vis, err := s.runVisibility(ctx) // delegated runs: starter-only without agents:delegated:read
    if err != nil {
        return nil, err
    }
    q.Scope = RunScope{AgentTypeIDs: readable, Vis: vis}
    return s.repo.ListRuns(ctx, tenantID, q)
}
```

### Wiring in `cmd/orbit-agents/main.go`

```go
verifier := token.NewVerifierFromConfig(ctx, token.LoadConfig())

evalLogger := logger // child logger ok
eval := policy.NewEvaluator(cfg.Platform.PolicyServiceURL+"/api/v1/policy/bundles",
                            cfg.Policy.BundlePollInterval,
                            evalLogger)
eval.Start(ctx)  // background bundle polling

authz := agents.NewAuthz(eval)

atSvc := agents.NewAgentTypeService(repo, repo, k8s, authz, ..., logger)
runSvc := agents.NewRunService(repo, repo, sm, k8s, pubsub, authz, ..., logger)

controller := agents.NewController(atSvc, runSvc, verifier, logger)
```

Controller `Middleware()` now returns the auth chain (matches inferencegw):

```go
func (c *Controller) Middleware() mw.MiddlewareHandler {
    return mw.MiddlewareChain(
        mw.AuthMiddleware(c.verifier, c.logger),
        mw.DelegationMiddleware(c.logger, c.verifier),
    )
}
```

### Privileged internal contexts

Two call sites operate outside a user request:

1. **The reaper goroutine (`run_service.go:507 StartReaper`)** — runs on `ctx` from `realMain`. It calls `repo.ReapStaleRuns` and `repo.GetRun` directly (no auth). Its publishes are server-internal `pubsub.Publish` calls. We will not push the reaper through `authz`; it operates on the repository layer below the service boundary. This is consistent with the inference-gateway's reapers and the identity service's housekeeping jobs.

2. **The scope-registration hook (`main.go:163 initScopeRegistration`)** — calls `repo.ListAllAgentTypeSlugs` to feed the policy-service. Same logic: data-layer access below the auth boundary.

Neither is reachable from external traffic; both touch the database directly, never the HTTP API. This keeps the rule simple: **anything serving HTTP goes through `authz`; anything internal goes through the repository.**

### Error-to-status mapping

| Service error | Handler response |
|---|---|
| `ErrForbidden` on read of an existing resource | 404 — existence is itself information |
| `ErrForbidden` on write where the caller may read | 403 |
| `ErrForbidden` on write where the caller may not read | 404 |
| `ErrNotFound` | 404 |

The "read before write" distinction is handled by an `authz.canRead` shortcut so handlers do not need to think about it.

### Bundle warmup

`policy.Evaluator.Check` and `Query` return a sentinel error if called before the bundle has loaded. The readiness probe (`/health`) will:

1. Always return 200 if the process is up (liveness).
2. A separate `/ready` will return 503 until the bundle has been successfully loaded at least once.

Kubernetes uses `/ready` for traffic admission, so no request reaches handlers before the bundle is loaded under normal operation. If the bundle endpoint is unavailable at startup, the pod stays out of service.

### Migration Path

Phase 1 — wire (this PR or sibling):
- Add `verifier` and `eval` construction in `main.go`.
- Add `authz.go`.
- Change `Controller.Middleware()` to return the auth chain.
- Add per-handler claim extraction; drop `tenant_id` from body/query reads.
- Service signatures change in-place (no backwards-compat shim — orbit-agents has no external Go API consumers).

Phase 2 — list filtering:
- Add `Query`-backed filter calls in `ListAgentTypes`, `ListRuns`.
- Drop repository pre-filtering by tenant ID where present (now redundant with the policy filter; keep it as a performance hint at the SQL layer).

Phase 3 — BFF cleanup (separate PR, non-blocking):
- Stop sending `tenant_id` in request bodies; the backend ignores it anyway. This reduces ambiguity but is not security-critical once Phase 1 lands.

### Timeline

Phases 1 and 2 land together in the PR that addresses the merge-readiness audit blocker §1.1. Estimated effort: 1–2 days including tests. Phase 3 is a follow-up frontend change of similar size.

---

## Validation

### Success Metrics

- **Cross-tenant access blocked:** A test creates two tenants, issues a token for tenant A, attempts every public path with tenant B's resource IDs. All return 404.
- **Per-agent-type filtering works:** A user with `agents:runs:slug-foo:read` and no other grant calls `GET /runs`. Response includes only `slug-foo` runs.
- **Platform admin still works:** A principal with `agents:*` operates on any tenant, sees all runs in list responses.
- **Internal callbacks unchanged:** Session-manager `POST /internal/runs/{id}/state` continues to succeed (it does not present a user JWT; the Rego internal-path bypass covers it).
- **Bundle warmup:** Pod readiness lags startup by the bundle load time, no requests are served against an empty bundle.

### Monitoring

- New OTel attributes on every authorization decision: `policy.allow`, `policy.reason`, `policy.scope`, `principal.id`, `principal.tenant`.
- Counter metric `orbit_agents_authz_denied_total{reason="..."}` for visibility into deny patterns.
- Existing `policy.Evaluator` already emits bundle-load metrics; we will alert on prolonged "bundle stale" conditions.

---

## Related Decisions

- [orbit-agents-route-scope-registration](2026-05-11-orbit-agents-route-scope-registration.md) — registers the scope vocabulary this ADR consumes.
- [agent-identity-and-policy-provisioning](2026-05-11-agent-identity-and-policy-provisioning.md) — provisions per-agent-type SPIFFE identities and policy grants.
- [dynamic-route-scope-registration](2026-04-14-dynamic-route-scope-registration.md) — the route→scope registration pattern reused here.
- ADR-XXXX (TBD) — wraps the session-manager → orbit-agents HTTP notifier in a typed `httpclient.Base`-backed client (audit §1.4, deferred follow-up).

---

## References

- `backend/internal/platform/policy/evaluator.go` — `Evaluator` interface.
- `backend/internal/platform/policy/engine.go` — in-process OPA engine implementation.
- `backend/internal/inferencegw/service.go:541-628` — reference implementation of `Check` and `Query` from inference-gateway.
- `backend/internal/policyservice/bundle/authz.rego` — the Rego policy (scope hierarchy, internal-path bypass).
- `backend/internal/platform/middleware/auth.go` — `AuthMiddleware` definition.
- `agents-merge-audit.md` §1.1 — the audit finding this ADR addresses.

---

**Last Updated:** 2026-05-29
