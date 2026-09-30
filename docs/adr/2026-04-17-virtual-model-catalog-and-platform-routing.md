# Virtual Model Catalog and Platform Routing

**Status:** Accepted  
**Date:** 2026-04-17
**Deciders:** kora platform team  
**Technical Story:** The inference gateway currently surfaces models by querying providers directly — every call to `GET /v1/models` returns whatever Bedrock, Vertex AI, and Azure enumerate at that moment. There is no platform-level control over what models are available, how requests are routed across providers, or what policy scopes are generated. This ADR introduces the Virtual Model as a first-class platform primitive: a named, admin-curated model backed by one or more provider routes, with explicit routing strategy, that becomes the authoritative source for the `/v1/models` API and the policy vocabulary.

---

## Context

### Background

[provider-model-catalog-and-alias-routing](2026-04-01-provider-model-catalog-and-alias-routing.md) established the `ProviderModel` type and alias routing within the routing engine. Provider connectors return enriched model objects; the routing engine builds an in-memory alias table that resolves clean names (e.g. `bedrock/claude-sonnet-4-6`) to canonical provider IDs (e.g. `bedrock/us.anthropic.claude-sonnet-4-6`). Aliases are auto-generated from model IDs — they are not configurable.

[policy-gateway-and-authorisation-model](2026-04-14-policy-gateway-and-authorisation-model.md) and [dynamic-route-scope-registration](2026-04-14-dynamic-route-scope-registration.md) established the policy model: access to inference models is controlled via scopes in the format `inference:{family}:{model}:use`. These scopes are registered dynamically in the vocabulary when provider connectors discover new models. Tenants are granted access to specific scopes via the policy grant system.

The current system has several limitations:

1. **No platform curation.** Every model a provider enumerates is surfaced to the gateway, regardless of whether the platform wants to offer it.
2. **Routing is not configurable.** The routing engine supports failover and lowest-cost strategies, but these are applied uniformly with no per-model override. A platform admin cannot say "for Claude Sonnet, prefer Bedrock; fall back to Vertex AI" or "for Gemini Flash, split traffic 70/30 between model variants."
3. **The failover implementation is naive.** Routes are ordered by a single static priority derived from provider discovery order, not from admin intent.
4. **The `/v1/models` response is unfiltered at the platform level.** Policy filtering applies per-principal, but there is no mechanism for a platform admin to suppress a model entirely — e.g. to disable GPT-4o while a provider contract is under review.
5. **Scope registration is driven by provider discovery.** When a provider returns a new model, it is automatically scoped into the vocabulary. There is no review or approval step.
6. **Provider model IDs leak to consumers.** The current model IDs (`bedrock/us.anthropic.claude-sonnet-4-6`) contain provider-internal implementation details. A stable, provider-agnostic name is not yet enforced.

### Problem Statement

Platform admins have no tools to:
- Control which models are available to tenants
- Configure how requests are routed when multiple providers offer the same model
- Define stable, provider-agnostic model names that tenants and agents use
- Express per-model routing rules (priority, weighted split, cost preference)

### Goals

- Introduce a **Virtual Model** as the authoritative unit of model identity for the platform.
- Virtual models follow the existing naming convention: `{family}/{name}` (e.g. `anthropic/claude-sonnet-4-6`), which maps directly to the existing policy scope format `inference:{family}:{name}:use`.
- Virtual models are persisted in the database and managed exclusively by platform admins.
- `GET /v1/models` returns virtual models, not raw provider models. Policy filtering applies on top.
- Each virtual model carries an ordered set of provider routes and a routing strategy.
- Three routing strategies are supported: **Priority/Failover**, **Weighted Split**, and **Lowest Cost**.
- Platform admins can enable or disable individual provider models, preventing them from being used as route targets.
- Policy scopes are auto-registered when a virtual model is created and auto-deregistered when it is deleted.
- A new **Model Catalog** admin page at `/model-gateway/catalog` provides the management UI, visible only to principals with the `platform:catalog:write` scope.

### Non-Goals

- Per-principal or per-tenant spend controls and budget quotas (deferred — see §Budget Controls Deferral below).
- Adding provider models manually that were not discovered from a live provider connector.
- Changing the per-principal policy grant model — tenants continue to receive access to virtual model scopes via the existing grants/roles system.
- Real-time provider health monitoring or automatic circuit-breaking (a follow-on).
- Pricing integration — `estimated_cost_usd` in usage records remains `$0.00` until a separate pricing ADR is implemented.

---

## Budget Controls Deferral

During design, per-principal and per-tenant spend controls were considered and explicitly deferred from this implementation. The chosen approach for a future ADR is **Option B: a separate Quotas resource**.

A `Quota` entity will carry: `{ principal_type, principal_id, scope, rpm_limit, tpm_limit, monthly_budget_usd, alert_threshold_pct, hard_cap }`. Quotas say *how much* a principal may use a given virtual model; grants continue to say *whether* they can use it at all. This separation keeps the authorization model clean — grants remain lightweight boolean assertions — and allows quota parameters to change frequently without triggering OPA bundle recompilation.

The spend controls UI section in the Model Catalog admin page is included as a structural placeholder in this release. All spend control fields are non-functional until the Quotas ADR is implemented. The platform-wide virtual model rate limits (RPM/TPM) are similarly deferred — they will be the first concrete application of the Quotas resource.

---

## Decision Drivers

- **Stable consumer-facing IDs.** Agents and tenants should address models by a platform-defined name, not a provider-internal slug. Virtual model names are the stable external identity.
- **Platform admin intent over provider auto-discovery.** What models exist on the platform is a deliberate decision, not an automatic consequence of what providers enumerate.
- **Consistent with the policy naming convention.** `{family}/{name}` maps to `inference:{family}:{name}:use` without any transformation, preserving the existing policy model.
- **Minimal breaking change.** Existing policy grants and roles that reference `inference:{family}:{name}:use` scopes continue to work unchanged, provided the named virtual model exists.
- **Routing correctness over cleverness.** The current ad-hoc failover implementation is replaced with explicit, admin-configured routing strategies.

---

## Considered Options

### Option 1: Extend the alias table with admin-configurable entries

Keep the in-memory alias table from [provider-model-catalog-and-alias-routing](2026-04-01-provider-model-catalog-and-alias-routing.md) but add an API for platform admins to inject custom aliases and routing overrides. Provider discovery continues to populate the base table; admin entries take precedence.

**Pros:** Minimal structural change — the existing alias table mechanism is reused.  
**Cons:** Aliases remain derived from provider discovery; the platform does not own the model inventory. In-memory state is lost on restart (or requires a persistence layer bolted on afterwards). Routing strategy configuration has no clean home in the alias table data model. The `/v1/models` response still reflects provider-enumerated models with admin overlays, not a curated inventory.

### Option 2: A separate Catalog service

Introduce a new `orbit-catalog` microservice responsible for virtual model storage, routing configuration, and the catalog admin API. The inference gateway becomes a consumer of the catalog service.

**Pros:** Clean separation of concerns; the catalog owns its domain entirely.  
**Cons:** A new service adds operational overhead (deploy, health, mTLS, DB connection). The catalog is tightly coupled to the inference gateway's routing decisions — the gateway would need to call the catalog on every request or maintain a local cache that could diverge. For the initial feature set, a separate service adds complexity without enough isolation benefit.

### Option 3: Virtual Models as a first-class resource in the inference gateway (chosen)

Add virtual model persistence (DB table) to the inference gateway. Expose CRUD endpoints on the gateway for platform admin management. Replace the in-memory alias table with a DB-backed virtual model catalog. The routing engine reads from the catalog on startup and via a watch/refresh mechanism.

**Pros:** The inference gateway already owns routing decisions — virtual model configuration is a natural extension of that responsibility. No new service to operate. The routing engine can apply strategy logic at dispatch time using data already in memory (loaded from the DB). Changes to virtual models are reflected immediately with no gateway restart.  
**Cons:** The inference gateway's scope expands slightly. The catalog admin API must be scope-gated to prevent tenants from accessing it.

---

## Decision

**Chosen Option:** Option 3 — Virtual Models as a first-class resource in the inference gateway.

---

## Design

### 1. Data Model

#### `VirtualModel`

```go
type VirtualModel struct {
    ID          string           // UUID, internal primary key
    Family      string           // e.g. "anthropic" — first policy scope segment
    Name        string           // e.g. "claude-sonnet-4-6" — second policy scope segment
    DisplayName string           // e.g. "Claude Sonnet 4.6" — human-readable label
    Description string           // optional prose description
    Strategy    RoutingStrategy  // "priority" | "weighted" | "lowest-cost"
    Routes      []RouteTarget    // ordered; priority derived from slice index for "priority" strategy
    Enabled     bool
    CreatedAt   time.Time
    UpdatedAt   time.Time
}

// FullID returns the canonical consumer-facing model ID: "{family}/{name}"
func (v VirtualModel) FullID() string { return v.Family + "/" + v.Name }

// PolicyScope returns the OPA scope this virtual model controls: "inference:{family}:{name}:use"
func (v VirtualModel) PolicyScope() string {
    return "inference:" + v.Family + ":" + v.Name + ":use"
}
```

```go
type RouteTarget struct {
    ID               string      // UUID
    VirtualModelID   string      // FK → virtual_models.id
    Provider         ProviderName
    ModelID          string      // canonical provider model ID, e.g. "us.anthropic.claude-sonnet-4-6"
    Priority         int         // 1-based; used for display and "priority" strategy ordering
    Weight           int         // 0–100; only meaningful for "weighted" strategy; must sum to 100 across routes
    InputCostPer1M   float64     // USD per 1M input tokens (informational; used by "lowest-cost" strategy)
    OutputCostPer1M  float64     // USD per 1M output tokens
    Enabled          bool
}

type RoutingStrategy string

const (
    StrategyPriority   RoutingStrategy = "priority"    // ordered failover
    StrategyWeighted   RoutingStrategy = "weighted"    // probabilistic traffic split
    StrategyLowestCost RoutingStrategy = "lowest-cost" // prefer cheapest route; failover on error
)
```

#### `ProviderModelOverride`

Stores platform admin enable/disable decisions for individual provider-enumerated models. If no override exists for a model, it defaults to enabled.

```go
type ProviderModelOverride struct {
    Provider  ProviderName
    ModelID   string
    Enabled   bool
    UpdatedAt time.Time
}
```

#### Database tables

```sql
CREATE TABLE virtual_models (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    family       TEXT NOT NULL,
    name         TEXT NOT NULL,
    display_name TEXT NOT NULL,
    description  TEXT NOT NULL DEFAULT '',
    strategy     TEXT NOT NULL CHECK (strategy IN ('priority','weighted','lowest-cost')),
    enabled      BOOLEAN NOT NULL DEFAULT TRUE,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (family, name)
);

CREATE TABLE virtual_model_routes (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    virtual_model_id  UUID NOT NULL REFERENCES virtual_models(id) ON DELETE CASCADE,
    provider          TEXT NOT NULL,
    model_id          TEXT NOT NULL,
    priority          INT NOT NULL DEFAULT 1,
    weight            INT NOT NULL DEFAULT 0,
    input_cost_per_1m NUMERIC(10,6) NOT NULL DEFAULT 0,
    output_cost_per_1m NUMERIC(10,6) NOT NULL DEFAULT 0,
    enabled           BOOLEAN NOT NULL DEFAULT TRUE,
    UNIQUE (virtual_model_id, provider, model_id)
);

CREATE TABLE provider_model_overrides (
    provider    TEXT NOT NULL,
    model_id    TEXT NOT NULL,
    enabled     BOOLEAN NOT NULL DEFAULT TRUE,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (provider, model_id)
);
```

### 2. Catalog API (Inference Gateway)

New admin endpoints on the inference gateway, mounted under `/api/v1/catalog/`. All require `platform:catalog:write` scope (read endpoints require `platform:catalog:read`).

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/v1/catalog/virtual-models` | List all virtual models with routes |
| `POST` | `/api/v1/catalog/virtual-models` | Create a virtual model; auto-registers policy scope |
| `GET` | `/api/v1/catalog/virtual-models/{id}` | Get a single virtual model |
| `PUT` | `/api/v1/catalog/virtual-models/{id}` | Update virtual model (replaces routes) |
| `DELETE` | `/api/v1/catalog/virtual-models/{id}` | Delete; auto-deregisters policy scope |
| `GET` | `/api/v1/catalog/provider-models` | List all discovered provider models with override status |
| `PATCH` | `/api/v1/catalog/provider-models/{provider}/{model-id}` | Enable or disable a provider model |

Policy scope registration (create) and deregistration (delete) are performed synchronously as part of the virtual model write path. The inference gateway calls the policy service's scope registration endpoint directly — the same mechanism used by [dynamic-route-scope-registration](2026-04-14-dynamic-route-scope-registration.md)'s dynamic route-scope registration.

### 3. Updated `GET /v1/models` Behaviour

The OpenAI-compatible `GET /v1/models` endpoint is updated to return virtual models from the catalog instead of live provider enumeration.

**Current flow:**
```
GET /v1/models → RoutingEngine.ListModels() → connectors.ListModels() [live provider calls] → filter by principal policy → respond
```

**New flow:**
```
GET /v1/models → CatalogStore.ListEnabled() → filter by principal policy (scope: inference:{family}:{name}:use) → respond
```

The response shape is unchanged (`ModelObject` with `id`, `display_name`, `owned_by`, `aliases`, etc.). For virtual models, `id` is the full ID (`anthropic/claude-sonnet-4-6`), `owned_by` is the family (`anthropic`), and `aliases` is empty — the full ID is already the stable name clients should use.

Provider discovery still runs in the background (on a configurable interval) to populate the Provider Catalog view and to detect newly available or removed provider models. It no longer drives the `/v1/models` response.

### 4. Routing Engine Changes

The `RoutingEngine` is updated to load virtual models from the catalog store on startup and refresh them periodically (or on write-triggered invalidation). The in-memory alias table from [provider-model-catalog-and-alias-routing](2026-04-01-provider-model-catalog-and-alias-routing.md) is replaced by a virtual model lookup table keyed on `{family}/{name}`.

#### Dispatch logic per strategy

**Priority / Failover:**  
Routes are sorted by `priority` ascending. The gateway attempts routes in order. If a route returns a provider error (5xx, timeout, or connectivity failure), the next route is tried. Policy or auth errors (4xx from the provider) are not retried — they are returned immediately.

**Weighted Split:**  
At request time, a route is selected probabilistically using the configured weights. The remaining routes, in priority order, serve as the failover chain if the selected route errors.

**Lowest Cost:**  
At request time, enabled routes are sorted by `(input_cost_per_1m + output_cost_per_1m)` ascending. The cheapest is attempted first; on failure, the next cheapest is tried. If costs are equal, priority order is the tiebreaker.

Disabled routes (both at the virtual model route level and via provider model overrides) are excluded from all strategies before dispatch.

#### Catalog load and invalidation

The `CatalogStore` is loaded once at startup and cached in the `RoutingEngine` via an `atomic.Pointer`. An admin write to any catalog endpoint triggers an async reload — the engine swaps the pointer lock-free, so in-flight requests are not blocked. The reload is debounced (100ms) to coalesce rapid consecutive writes.

### 5. BFF Proxy Routes

New BFF route handlers added under `app/api/catalog/`. The inference gateway URL is already in the frontend config as `INFERENCE_GATEWAY_URL`.

| BFF Route | Method(s) | Proxies To | Scope |
|---|---|---|---|
| `/api/catalog/virtual-models` | GET, POST | `/api/v1/catalog/virtual-models` | `platform:catalog:read` / `write` |
| `/api/catalog/virtual-models/[id]` | GET, PUT, DELETE | `/api/v1/catalog/virtual-models/{id}` | `platform:catalog:read` / `write` |
| `/api/catalog/provider-models` | GET | `/api/v1/catalog/provider-models` | `platform:catalog:read` |
| `/api/catalog/provider-models/[provider]/[modelId]` | PATCH | `/api/v1/catalog/provider-models/{provider}/{id}` | `platform:catalog:write` |

`ApiRoute.catalog.*` entries are added to `api-routes.ts` following the established pattern.

### 6. Frontend Catalog Page

Route: `/model-gateway/catalog`  
Nav entry: "Catalog" in the Model Gateway nav group, gated by `platform:catalog:write`.  
The page provides two tabs:

**Virtual Models** — table grouped by family, showing `name` (monospace), display name, routing strategy badge, route count with per-route health dots, spend controls summary (placeholder, non-functional until quotas ADR), and enabled status. Clicking a row opens a right-panel editor with:
- Identity fields: `family`, `name` (together forming the `{family}/{name}` full ID), `display_name`, `description`, and a read-only derived scope field (`inference:{family}:{name}:use`)
- Routes & Strategy tab: strategy selector (Priority/Failover, Weighted Split, Lowest Cost) + ordered route list with provider, canonical model ID, input/output cost, per-route enable toggle, weight input (for weighted strategy), and drag-to-reorder handle
- Spend Controls tab: RPM limit, TPM limit, monthly budget cap, alert threshold, hard cap toggle — all rendered but disabled with a "coming soon" indicator pending the Quotas ADR

**Provider Catalog** — table of all provider-discovered models with enable/disable toggle, capability badges, family label, and "assigned to" column showing which virtual models route through each provider model. Filtered by provider (Bedrock, Vertex AI, Azure) and search.

### 7. Policy Scope Lifecycle

When a virtual model is **created**, the inference gateway calls the policy service's scope registration endpoint to add `inference:{family}:{name}:use` to the vocabulary for the tenant. The scope description is set to the virtual model's `description` field (or a generated default).

When a virtual model is **deleted**, the corresponding scope is removed from the vocabulary. Existing grants or roles that reference the scope are not automatically revoked — the policy service will warn on a best-effort basis that the scope is no longer in the vocabulary, and grant evaluation for that scope will fail safe (deny). Platform admins should revoke affected grants before deleting a virtual model.

When a virtual model is **disabled** (but not deleted), the scope remains in the vocabulary. Policy checks will succeed — the principal has the scope — but the gateway will return a model-unavailable error at request time because no enabled virtual model matches the requested ID.

### 8. Scope Gating

| Action | Required Scope | Enforcement |
|---|---|---|
| View catalog page | `platform:catalog:write` | `requiredScope` in nav entry + page-level `ScopeGuard` |
| Read virtual models | `platform:catalog:read` | BFF proxy + inference gateway |
| Create / update / delete virtual models | `platform:catalog:write` | BFF proxy + inference gateway |
| Enable / disable provider models | `platform:catalog:write` | BFF proxy + inference gateway |

The `platform:catalog:read` and `platform:catalog:write` scopes are added to the policy vocabulary as new platform-tier scopes, following the existing `platform:grants:read/write` and `platform:roles:read/write` pattern.

---

## Implementation Order

### Phase 1: Backend — Data Model and CRUD

1. Add `virtual_models`, `virtual_model_routes`, and `provider_model_overrides` DB tables (migration)
2. Implement `CatalogStore` — CRUD operations, DB-backed
3. Implement catalog HTTP handlers on the inference gateway
4. Register `platform:catalog:read` and `platform:catalog:write` in the platform vocabulary
5. Wire scope auto-registration/deregistration on virtual model create/delete

### Phase 2: Backend — Routing Engine Integration

6. Replace alias table in `RoutingEngine` with `CatalogStore` lookup
7. Implement the three routing strategies (priority, weighted, lowest-cost) with correct failover semantics
8. Update `GET /v1/models` to return virtual models from the catalog
9. Update provider discovery to feed the Provider Catalog view (background refresh, no longer drives `/v1/models`)

### Phase 3: Frontend — BFF and Catalog Page

10. Add `ApiRoute.catalog.*` entries and BFF proxy handlers
11. Wire the Catalog page to live API data (remove mock data from the component)
12. Add nav entry with `platform:catalog:write` scope gate

### Phase 4: Validation

13. End-to-end: create a virtual model → assign policy scope → verify `GET /v1/models` returns it for a principal with the scope
14. Routing: verify priority failover triggers when the first route errors; verify weighted split distributes traffic; verify lowest-cost selects by price
15. Scope lifecycle: verify scope appears in vocabulary on create; verify grants fail safe after delete

---

## Consequences

### Positive

- Platform admins have full control over what models are available — provider enumeration is an input, not the source of truth
- Virtual model names are stable, provider-agnostic identifiers that survive provider migrations or model ID changes
- The `{family}/{name}` naming convention maps cleanly to the existing policy scope format with no translation layer
- Routing strategy is explicit, per-model, and configurable — the ad-hoc failover logic is replaced with something admins can reason about
- Policy scopes are created intentionally (when an admin creates a virtual model) rather than automatically on provider discovery

### Negative

- The inference gateway gains a persistence dependency (DB tables) for the catalog store
- `GET /v1/models` is now only as accurate as the catalog — if a provider removes a model that still has a virtual model pointing to it, requests will fail at dispatch time rather than being absent from the listing
- Deleting a virtual model while grants exist for its scope requires manual grant cleanup; there is no automated cascade

### Neutral

- Existing grants and roles that reference `inference:{family}:{name}:use` scopes continue to work, provided a virtual model with the matching `family/name` is created in the catalog
- Provider discovery continues to run in the background; its role changes from driving the models API to informing the Provider Catalog admin view
- The `estimated_cost_usd` field in usage records remains `$0.00` pending a pricing ADR; the `input_cost_per_1m` / `output_cost_per_1m` fields on `RouteTarget` are stored but not yet used for billing

---

## Related Decisions

- [inference-gateway-architecture](2026-04-01-inference-gateway-architecture.md) — Inference Gateway architecture; this ADR extends the routing engine defined there
- [provider-model-catalog-and-alias-routing](2026-04-01-provider-model-catalog-and-alias-routing.md) — Provider model catalog and alias routing; the alias table introduced there is superseded by the virtual model catalog
- [policy-gateway-and-authorisation-model](2026-04-14-policy-gateway-and-authorisation-model.md) — Policy gateway and authorisation model
- [dynamic-route-scope-registration](2026-04-14-dynamic-route-scope-registration.md) — Dynamic route-scope registration; the same registration mechanism is used for virtual model scope registration
- [model-capability-taxonomy](2026-04-01-model-capability-taxonomy.md) — Model capability taxonomy; virtual model metadata will reference capability types defined there

---

**Last Updated:** 2026-04-16
