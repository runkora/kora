# Hierarchical Budget Enforcement: Windows, Pools, Tenant Caps, and Per-Run Limits

**Status:** Proposed
**Date:** 2026-06-10
**Deciders:** Platform Engineering
**Extends:** [Budget Enforcement Corrections ADR](2026-06-10-budget-enforcement-corrections-and-budgets-page.md)
**Technical Story:** [PRD: Hierarchical Budgets](../prd/hierarchical-budgets.md)

---

## Context

The budget system enforces exactly one ceiling type: per-principal, monthly. The PRD adds four more: weekly windows, role pools (shared pots), tenant caps (rollup, blocking agents/services only), and per-run limits for agents. All configuration must live in the policy-service ("one place") and compile into the OPA bundle; nothing budget-shaped is carried in tokens.

Constraints discovered during design:

1. **Agent tokens carry no run identifier** (`token.OrbitClaims` has session/tenant/display-name/SPIFFE claims only; run binding happens at the orbit-agents service layer by tenant+slug). Run identity must reach the gateway another way.
2. **Role membership is request-time knowledge** (IDP-asserted JWT roles), so pool counters cannot be rebuilt from the database unless usage rows record the caller's roles.
3. A weekly and a monthly assignment can apply to the same principal simultaneously; their limits cannot be merged by MAX (different windows).

---

## Decision

### 1. Generalised composition: MAX within (scope, window), AND across ceilings

A budget assignment is `(scope, limit_usd, window, pooled?)`. Per-member assignments targeting the same `(scope, window)` resolve with MAX (grants only raise limits). Every resulting ceiling — principal-weekly, principal-monthly, each held role pool, tenant cap, run cap — is an independent constraint; a request is denied if **any** applicable ceiling is met. Fail-open semantics are unchanged (no configuration ⇒ allow; infrastructure errors ⇒ allow).

### 2. Schema: one assignment shape

```json
{ "limit_usd": 50.0, "reset_cadence": "weekly" | "monthly", "pooled": false, "per_run_limit_usd": 5.0 }
```

- `limit_usd` replaces `monthly_limit_usd` (which is a misnomer once cadence varies). The migration rewrites existing JSONB in place — single-developer environment, no compatibility shim.
- `pooled` is meaningful on **roles** only: `true` = one shared pot for all members; `false` (default) = per-member allowance. Pooled budgets do not participate in the per-principal MAX.
- `per_run_limit_usd` is meaningful on **grants** only (AGENT/SERVICE principals); windowless — the run is the window.
- An assignment carries one cadence. A role or grant wanting both weekly and monthly ceilings uses two grants (roles: one cadence per role this iteration).
- New **tenant budget** record (one row per tenant, policy-service DB): `{ limit_usd, reset_cadence }`, managed via `PUT/GET/DELETE /policy/tenant-budget`, gated by `platform:budgets:read/write`, recompiling the bundle on write.

### 3. Counter key layout

```
budget:{principal_id}:{period}                       canonical per-principal, per window
budget:tenant:{tenant_id}:{period}                   tenant rollup (all principals' spend)
budget:rolepool:{tenant_id}:{role}:{period}          shared pool (role names are tenant-scoped, hence the tenant segment)
budget:run:{run_id}                                  per-run, TTL 7 days refreshed on write
budget:{principal_id}:{source}:{id}:{period}         attribution (existing, unchanged)
```

Period strings are window-specific and collision-free: `2026-06` (monthly), `2026-W24` (ISO week, UTC — matches Postgres `date_trunc('week', …)` Monday start). The existing monthly canonical key is unchanged. Increment writes every applicable canonical counter + attribution counters; reads are per-ceiling. Rehydration on miss is window-aware:

- principal: `SUM(estimated_cost_usd) WHERE tenant_id, principal_id, created_at >= period_start`
- tenant: same without principal filter
- pool: `… WHERE tenant_id AND role = ANY(principal_roles) AND created_at >= period_start`
- run: `… WHERE run_id = $1`

### 4. `inference_usage` gains `run_id` and `principal_roles`

Nullable columns recorded at write time from request context. They make run and pool counters rebuildable (the DB stays the source of truth) and give per-run / per-role cost audit.

### 5. Run identity: per-run base URL path prefix

The agent process performs no header or identity injection of any kind, and run identity must be fully transparent to the workload — for every supported runtime (Claude Code, Codex, OpenCode, …).

A pod-level sidecar cannot supply run identity (the session-manager pod hosts multiple concurrent runs, so its Envoy/identity-proxy sidecar cannot attribute an egress connection to a run), and the only thing the runner legitimately controls about the subprocess is its environment — which it already populates per run (`WORKSPACE`, `HOME`, `TMPDIR`).

**Run identity travels in the URL.** The runner sets the runtime's base-URL env per subprocess, carrying the run as a path prefix:

```
ANTHROPIC_BASE_URL=http://orbit-inference-gateway.../run/{run_id}            (Claude Code)
OPENAI_BASE_URL=http://orbit-inference-gateway.../run/{run_id}/v1            (Codex-style runtimes)
```

Every runtime that supports a custom base URL preserves its path component (OpenAI-style base URLs conventionally end in `/v1`, which SDKs must preserve), so this is runtime-agnostic by construction — the same property the gateway's dialect support already relies on. The gateway's root handler recognises the `/run/{run_id}` prefix, stores the run ID in request context, strips the prefix, and processing continues on the normal paths. No new components, no data-path hops, sidecar token injection unchanged.

The gateway honours the run prefix **only for AGENT principals** (verified claims) and records the run ID on the usage row.

**Trust statement:** the prefix is configuration set by platform-owned runner code; the agent neither sets nor needs to know it. A hostile subprocess could read its env and address a forged run prefix — and is then caught by the principal and tenant ceilings, which key on verified identity. Hardening (per-run network namespacing or token-bound run identity) is additive and changes nothing about this design.

### 6. Rego shape

OPA input: `{ principal: { id, tenant_id, type, roles, run_id? }, spends: { run?, principal: {window: usd}, role_pools: {role: usd}, tenant: {window: usd} } }`. The gateway determines which counters to read from bundle data (its own grant categories + tenant budget) and JWT roles (per-member and pooled role budgets) before evaluation.

Deny rules (any):
- some window w: `spends.principal[w] >= max(grant limits for w ∪ held per-member role limits for w)`
- some held role r with pooled budget: `spends.role_pools[r] >= pool limit`
- `principal.type ∈ {AGENT, SERVICE}` and tenant cap configured and `spends.tenant[cap.window] >= cap.limit`
- `principal.run_id` present and per-run limit configured and `spends.run >= limit`

`effective_budget_usd`/`has_limit` outputs generalise to a `ceilings` list `{scope, window, limit_usd, spend_usd, reached}` consumed by the personal endpoint and the Budgets page.

### 7. Run lifecycle on exhaustion

When the subprocess surfaces `BUDGET_EXCEEDED` (gateway **403**), the runner emits `run_error` with code `budget_exhausted`; the session-manager closes the session immediately and orbit-agents transitions the run to a new terminal state `budget_exhausted` (distinct from `failed` — the run did nothing wrong; its budget ended).

> **Why 403, not 429.** Budget exhaustion is not transient: the ceiling holds until the window resets or an admin raises it. Agent runtimes (Claude Code, Codex, OpenCode) auto-retry 429 with minutes of exponential backoff — observed live as a ~3-minute storm of doomed calls before the runtime gave up, sometimes reporting the truncated turn as a *success*, which left the run idle instead of `budget_exhausted`. None of the runtimes retry 403, so the first rejection is final: the turn fails immediately with the budget message in the error result, and classification is deterministic.

---

## Considered Alternatives

- **Run identity in the platform token** — rejected: tokens are per-agent SPIFFE identities by design; minting run-scoped tokens couples the identity plane to run lifecycle and contradicts the "nothing run/budget-shaped in tokens" decision.
- **Agent-side header injection (`ANTHROPIC_CUSTOM_HEADERS`)** — rejected: the agent process must not participate in identity or header injection; also runtime-specific, breaking Codex/OpenCode support.
- **Sidecar-stamped run header** — not possible today: the session-manager pod multiplexes concurrent runs, so its sidecar cannot attribute an egress connection to a run.
- **Per-run loopback reverse proxy in the session-manager** — rejected: adds a second userspace hop in the LLM data path (streaming, failure modes, per-run lifecycle) duplicating Envoy's role as the egress interception point, when an env-var-only mechanism achieves the same trust level.
- **Control-plane (session-manager) run-budget enforcement** — rejected: enforcement based on agent-self-reported usage violates "the agent is never trusted"; the gateway is the policy enforcement point with authoritative usage data.
- **Sum-of-attribution-counters for pools** — rejected: attribution counters are best-effort Redis state; pools need DB-rebuildable truth, hence `principal_roles` on usage rows.
- **`monthly_limit_usd` kept with a `weekly_limit_usd` sibling** — rejected: cadence is a property of the assignment, not a new field per window; one `(limit_usd, reset_cadence)` pair scales to reserved cadences without schema growth.

---

## Consequences

### Positive

- One mental model covers every ceiling: MAX within (scope, window), AND across ceilings, fail-open throughout.
- Agents get blast-radius control (per-run) and tenants get a bill guarantee (cap) without hard-capping agents monthly.
- Pools and weekly windows reuse the existing counter/rehydration machinery; no new infrastructure.

### Negative

- The OPA input and gateway pre-read logic grow: the gateway must derive the applicable ceiling set before each check (bounded: ≤ 2 principal windows + held pools + tenant + run).
- Per-run enforcement is only as strong as the runner-stamped header until sidecar hardening lands.
- One more Redis write per request per additional configured ceiling.

### Neutral

- `BudgetStatus` becomes a ceilings list; the My Usage banner renders one bar per ceiling.
- The budgets listing endpoint reports tenant cap and pool utilisation alongside principals.

---

## Related Decisions

- [Budget Enforcement Corrections](2026-06-10-budget-enforcement-corrections-and-budgets-page.md) — canonical-counter model this extends.
- [Budget Model Redesign](2026-06-10-budget-model-redesign.md) — inline JSONB carriers (shape revised here).
- [Envoy ext_proc sidecar token injection](2026-03-21-envoy-ext-proc-sidecar-token-injection.md) — the future home of hardened run identity.
