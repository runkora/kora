# Hierarchical Budgets - Product Requirements Document

**Status:** Draft
**Created:** 2026-06-10
**Last Updated:** 2026-06-10
**Owner:** Platform Engineering
**Stakeholders:** Platform operators, tenant admins, FinOps, agent operators

---

## Executive Summary

Extend the budget model from a single type (per-principal monthly) to a small hierarchy of independent spending ceilings: **per-run** (an agent's maximum spend in a single run, refreshed every run), **per-principal monthly** (existing), and **tenant monthly cap** (a rollup ceiling across all principals in a tenant). A request must pass every ceiling that applies. All budget configuration lives in the policy-service — one place — and compiles into the OPA bundle like every other policy primitive.

---

## Problem Statement

### Current State

Budgets exist at exactly one scope: per-principal per-month, carried on roles and grants. This forces a false choice for agents: either hard-cap an agent's month (and it stops doing its job on day 20) or give it no budget at all (and a runaway loop can burn unbounded spend in an afternoon).

### User Pain Points

- An agent cannot be given "room to work" while still contributing to an overall cost ceiling — there is no rollup.
- A runaway agent run has no per-run blast-radius limit; the only backstop is a monthly number that is either too tight or too loose.
- A tenant has no aggregate cost guarantee; the provider bill is the sum of whatever individual budgets happen to exist.

### Business Impact

- Cost incidents are bounded only by monthly per-principal numbers — a single bad run can consume a month's budget.
- Operators cannot give finance a per-tenant cost ceiling.

---

## Goals and Objectives

### Primary Goals

1. **Tenant cap** — every request in the tenant accumulates to one ceiling; agents and services are blocked at the cap, humans are not.
2. **Per-run budgets** — a maximum spend per agent run, configured in policy on the agent's grant, refreshed implicitly by each new run.
3. **Weekly windows** — any budget assignment (role, grant, tenant) may reset weekly (ISO week, UTC) instead of monthly.
4. **Role budget pools** — a role budget may be a shared pot across all members ("engineering shares $500/week") instead of a per-member allowance ("each engineer gets $50").
5. **One configuration home** — all limits live in the policy-service and compile into the OPA bundle; no budget configuration is carried in tokens.

### Success Metrics

- A runaway agent run is blocked at its per-run limit within one request of crossing it.
- An agent with no monthly budget keeps working all month yet its spend appears in (and is bounded by) the tenant cap.
- Tenant cap utilisation is visible on the Budgets page alongside principal utilisation.

### Non-Goals

- Monitor-only (non-blocking) budgets — enforce-only this iteration.
- Quarterly/yearly windows — remain reserved.
- Platform-wide (cross-tenant) caps, per-PAT budgets, per-model budgets, burn-rate ($/hour) limits, delegation-aware attribution (charging the delegating human for an agent's spend) — documented as future scopes of the same model, not built now.

---

## The Budget Model

Every budget is **(scope, limit, window, pooled?)**, enforcement always blocking:

| Scope | Window | Carrier (policy-service) | Counter |
|---|---|---|---|
| **run** | the run itself | `per_run_limit_usd` on the agent's grant budget | `budget:run:{run_id}` |
| **principal** | weekly or monthly | `limit_usd` + `reset_cadence` on roles (per-member) and grants | `budget:{principal_id}:{period}` |
| **role pool** | weekly or monthly | `limit_usd` + `pooled: true` on a role | `budget:rolepool:{tenant_id}:{role}:{period}` |
| **tenant** | weekly or monthly | tenant budget record (new) | `budget:tenant:{tenant_id}:{period}` |

Period strings are window-specific and collision-free: `2026-06` (monthly), `2026-W24` (ISO week, UTC, Monday start).

### Composition rules

- **Within (scope, window)**, multiple per-member assignments resolve with MAX (unchanged): a grant can only raise a principal's effective limit *for that window*.
- **Across ceilings**, AND: a request must pass every configured, applicable ceiling — run AND principal-weekly AND principal-monthly AND each held role pool AND tenant. No precedence between ceilings.
- A **pooled** role budget is its own ceiling (one shared counter for all members), never merged into the per-principal MAX.

### Applicability

- **Run budgets** apply only to requests carrying run identity. The *limit* comes from the compiled bundle (the agent's grant); the *run identity* travels as a base-URL path prefix: the session-manager runner sets the runtime's base-URL env per subprocess (`ANTHROPIC_BASE_URL=…/run/{run_id}`, `OPENAI_BASE_URL=…/run/{run_id}/v1`), and the gateway strips the prefix into request context. Runtime-agnostic (Claude Code, Codex, OpenCode), fully transparent to the agent process, no new components; sidecar token injection unchanged. The gateway honours the prefix only for AGENT principals; the principal and tenant ceilings remain hard backstops.
- **Principal budgets** apply to all principals (unchanged).
- **Role pools** apply to every principal whose verified JWT asserts the role.
- **Tenant cap** accumulates spend from *all* principals (humans included), but **blocks only AGENT and SERVICE principals** at the limit. Humans continue under their individual budgets — the cap protects the bill without locking people out mid-month.

### Reset semantics

- Run: none — a new run is a new counter. Counter TTL 7 days, refreshed on write (long runs stay alive; abandoned counters expire).
- Weekly: ISO week, UTC (matches Postgres `date_trunc('week', …)`).
- Monthly: calendar month, UTC (unchanged). Quarterly/yearly remain reserved.

### Pool rehydration requires durable role attribution

A pool counter can only be rebuilt after a Redis loss if usage rows record which roles the caller held. `inference_usage` therefore gains a `principal_roles` column (alongside `run_id`); pool rehydration is `SUM(estimated_cost_usd) WHERE tenant_id = $1 AND $2 = ANY(principal_roles) AND created_at >= period_start`.

---

## User Stories

1. As a **tenant admin**, I set a $2,000/month tenant cap; finance gets a number that cannot be exceeded by agents, and I see its utilisation on the Budgets page.
2. As an **agent operator**, I set `$5 per run` on the deploy agent's grant; a runaway loop is stopped at $5, and the next run starts fresh with $5.
3. As an **agent operator**, my agent has a per-run budget and *no* monthly budget; it works all month and its spend rolls up to the tenant cap.
4. As a **developer (human)**, the tenant cap being reached does not block my requests; only my own budget does.
5. As an **operator**, a run that hits its budget ends in a clean `budget_exhausted` state, not a crash loop.

---

## Proposed Solution

### Policy-service (configuration + bundle)

- `BudgetAssignment` becomes `{ "limit_usd": 50, "reset_cadence": "weekly"|"monthly", "pooled": bool (roles only), "per_run_limit_usd": number (grants only, optional) }`. Existing rows (`monthly_limit_usd`) are rewritten in the migration (single-dev environment, no compatibility shim).
- New tenant budget record (one per tenant): `{ "limit_usd": 2000, "reset_cadence": "monthly" }`. CRUD via `PUT/GET/DELETE /policy/tenant-budget` (scopes `platform:budgets:read/write`), bundle recompile on write.
- Bundle: `data/tenant_budget/data.json`; `data.role_budgets` entries gain `pooled` + cadence; `data.budgets[pid]` carries per-window effective limits and `effective_per_run_limit_usd`.

### Enforcement (inference gateway + Rego)

- Gateway resolves run identity from `X-Orbit-Run-Id` (AGENT principals only), gathers the windows/pools applicable to the caller from bundle data + JWT roles, reads the corresponding canonical counters (rehydrating window-aware from `inference_usage` on miss), and passes a spends document to OPA: `{ run, principal: {weekly?, monthly?}, role_pools: {role: spend}, tenant: {weekly?, monthly?} }`.
- `budget.rego` denies when ANY applicable ceiling is met:
  - principal per window: spend(window) ≥ MAX of (grant ∪ held-role per-member) limits for that window.
  - role pool: pool spend ≥ pool limit, for each held pooled role.
  - run: run identity present AND per-run limit configured AND run spend ≥ limit.
  - tenant: principal type ∈ {AGENT, SERVICE} AND cap configured AND tenant spend(cap window) ≥ cap.
- `incrementSpend` writes every applicable canonical counter (principal per configured window, pools, tenant, run) plus the existing attribution counters.
- `inference_usage` gains nullable `run_id` and `principal_roles` columns — run/pool rehydration and audit.

### Agent control plane (orbit-agents + session-manager)

- Session-manager runner sets the per-subprocess base-URL env with the `/run/{run_id}` path prefix (replacing the inherited pod-level value, same strip-and-replace pattern as `HOME`). Transparent to the agent runtime; sidecar token injection unchanged.
- On `BUDGET_EXCEEDED` (gateway 403 — non-retryable so runtimes fail fast instead of retry-storming) surfacing from the subprocess, emit `run_error` with code `budget_exhausted`; the session closes immediately and orbit-agents transitions the run to a terminal `budget_exhausted` state instead of `failed`.

### UI (Budgets page)

- **Tenant cap card** at the top of the Principals tab: limit, cadence, period spend (all principals), utilisation bar, edit (write-scoped).
- Budget forms (Budgets page + Policy roles/grants modals): limit + cadence select (weekly/monthly); **Pooled** toggle on roles; **Per-run limit** field on grants.
- Principals tab chips show window and per-run limits; Roles tab shows pool utilisation; `GET /v1/budgets/principals` extended accordingly.
- My Usage banner renders one utilisation bar per applicable ceiling.

---

## Scope

### In Scope

- Tenant monthly cap (record, API, bundle, Rego, counters, UI card) with agent/service-only blocking.
- Per-run limits on grant budgets (schema, bundle, Rego, run counter, UI field) enforced via request run context.
- `run_id` on `inference_usage`; run-state handling of `BUDGET_EXCEEDED` in orbit-agents.

### Out of Scope (future work, same model)

- Monitor-only budgets; role pools; platform-wide cap; per-PAT, per-model, burn-rate budgets; delegation-aware attribution; alerting/notifications.

---

## References

- [ADR: Budget Enforcement Corrections](../adr/2026-06-10-budget-enforcement-corrections-and-budgets-page.md) — canonical-counter and MAX/AND semantics this PRD builds on.
- [ADR: Budget Model Redesign](../adr/2026-06-10-budget-model-redesign.md) — inline budget JSONB on roles/grants.
- [PRD: Budgets Page](budgets-page.md) — the management surface these types appear on.
