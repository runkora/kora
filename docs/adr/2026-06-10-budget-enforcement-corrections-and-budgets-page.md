# Budget Enforcement Corrections: Canonical Spend Counter, Request-Time Role Enforcement, All Principal Types, and the Budgets Management Surface

**Status:** Proposed
**Date:** 2026-06-10
**Deciders:** Platform Engineering
**Amends:** [Budget Model Redesign ADR](2026-06-10-budget-model-redesign.md)
**Technical Story:** [PRD: Budgets Page](../prd/budgets-page.md), [PRD: LLM Cost Tracking](../prd/llm-cost-tracking.md)

---

## Context

The [budget model redesign](2026-06-10-budget-model-redesign.md) (inline budget JSONB on roles/grants, MAX resolution, per-category Redis counters) was implemented in commits `b386a7c` and `81cd3dc`. A review of the implementation against its intent found three correctness defects — two of which trace back to the ADR text itself — and a set of management gaps that make budgets effectively unobservable and partially unenforceable:

1. **Role budgets are never enforced.** `data.budgets` in the OPA bundle is compiled from grants only — the policy service cannot resolve IDP-asserted role membership at compile time, so role-only principals are absent from the map and `budget.rego` falls through to its fail-open default. The inference gateway *does* know the caller's roles (from the JWT) but never passes them to OPA. Role budgets currently affect spend attribution counters and nothing else.

2. **Spend is double-counted for multi-source principals.** The prior ADR specifies that spend is written to *every* applicable category counter and that enforcement reads *the sum of all* category counters. Each category counter holds the principal's full spend, so a principal with N budget sources has their spend counted N times against an effective limit computed once. A user with a role budget and a grant budget is blocked at half their effective limit.

3. **Non-human grant budgets are silently dropped.** `buildBudgets` skips grants whose principal type is not `HUMAN`, while the grant creation UI offers the budget field for AGENT and SERVICE principals (and defaults to AGENT). For a platform whose purpose is governing agents, agent budgets must enforce.

4. **Budgets are unmanageable after creation.** There is no endpoint to update a grant's budget (delete + recreate mints a new grant ID and orphans the period's spend attribution), and no surface anywhere that joins limits with live spend ("who is near or over budget?").

---

## Decision Drivers

* **Enforcement must match configuration** — a limit an admin can type into a form must actually block traffic; silent no-ops are worse than no feature.
* **Spend arithmetic must be correct** — a principal's enforced spend is what they actually spent, regardless of how many budget sources apply.
* **Agents are first-class principals** — budget governance applies to humans, agents, and services alike.
* **Preserve the inline model** — budgets stay on roles/grants (no new tables, no third concept); fixes are to evaluation and counting, not the data model.
* **Role membership is request-time knowledge** — the JWT is the only authority on a principal's OIDC roles; enforcement must consume it there.

---

## Decision

### 1. Canonical per-principal spend counter; category counters become attribution-only

The "legacy" single key is promoted to the canonical enforcement counter:

```
budget:{principal_id}:{period}                        ← canonical: read for enforcement, always written
budget:{principal_id}:role:{role_name}:{period}       ← attribution only: written, never summed for enforcement
budget:{principal_id}:grant:{grant_id}:{period}       ← attribution only
```

`IncrementSpend` writes the canonical key **and** each applicable category key. `GetSpend` reads **only** the canonical key, rehydrating from `inference_usage` (`SUM(estimated_cost_usd)` for the period) on a miss. This supersedes the prior ADR's "read all applicable category counters and sum them" — that arithmetic was wrong by construction, since every category carries the full spend.

Category counters retain their purpose from the prior ADR — per-source reporting and alerting — and become the data source for the Budgets page's per-category breakdown. The 35-day TTL-on-write behaviour is unchanged for all keys.

### 2. Role budgets enforced via JWT roles in the OPA input

`BudgetCheckInput` gains the caller's OIDC roles:

```json
{
  "principal": { "id": "usr_abc123", "tenant_id": "tenant_1", "roles": ["engineer"] },
  "current_spend_usd": 75.00
}
```

`budget.rego` computes the effective limit across both sources at evaluation time:

```rego
limits contains l if { l := data.budgets[input.principal.id].effective_limit_usd }
limits contains l if {
    some r in input.principal.roles
    l := data.role_budgets.role_budgets[r].monthly_limit_usd
}

default has_limit := false
has_limit if count(limits) > 0

default effective_budget_usd := 0
effective_budget_usd := max(limits) if count(limits) > 0

default budget_allow := true
budget_allow := false if {
    has_limit
    input.current_spend_usd >= effective_budget_usd
}
```

`data.budgets[principal].effective_limit_usd` remains the compile-time MAX over the principal's *grant* budgets; the role contribution is resolved per request. MAX semantics across the union are preserved exactly as the prior ADR intended. `has_limit` (and therefore the personal budget banner) is now true for role-only principals.

### 3. All principal types carry enforceable grant budgets

`buildBudgets` includes grants for every principal type (HUMAN, AGENT, SERVICE). Agents authenticate with platform tokens carrying their principal ID, so the existing enforcement path applies without modification.

### 4. Grant budget mutation endpoint

`PATCH /policy/grants/{id}` accepts `{ "budget": { "monthly_limit_usd": 50, "reset_cadence": "monthly" } }` or `{ "budget": null }`, updates only the budget column, preserves the grant ID (and therefore its attribution counters), and triggers tenant bundle recompilation. Scopes-and-budget-together updates remain out of scope; scopes are immutable on grants by existing design.

### 5. Budget status listing endpoint

`GET /v1/budgets/principals` on the inference gateway (gated by `platform:budgets:read`) returns per-principal budget status for the current period: budget categories, effective limit, current spend, and utilisation. Sources:

- **Grant budgets** — enumerable from bundle data (`data.budgets`).
- **Role budgets** — role *limits* are enumerable (`data.role_budgets`), but membership is not statically resolvable. Active role-budget holders are discovered from their attribution counters (`budget:*:role:{role}:{period}`); a principal who holds a budgeted role but has made no requests this period does not appear with that role category. This is an accepted v1 limitation: zero spend means zero budget risk, and the next request materialises the counter.
- **Spend** — per-principal period totals from `inference_usage` (authoritative), with category counters supplying the per-source breakdown.

---

## Consequences

### Positive

- Role budgets, agent budgets, and multi-source budgets now enforce exactly as configured.
- Enforced spend equals actual spend; the personal banner and enforcement agree because both read the canonical counter through the same path.
- Grant budgets are editable in place without destroying attribution.
- Operators get a single spend-vs-limit surface (Budgets page) built on `platform:budgets:read/write` — scopes that were reserved but unused until now.

### Negative

- The OPA budget query is no longer a pure function of bundle data — correctness depends on the gateway passing JWT roles faithfully. Mitigated: the gateway already extracts roles for scope evaluation; the same claims source is reused.
- Role-budget principal discovery via Redis key scan is O(keyspace) — acceptable at current scale (keys ≈ principals × categories), revisit if key cardinality grows.
- One extra Redis write per request (canonical + categories instead of categories only).

### Neutral

- Mixed reset cadences remain reserved: the canonical counter is monthly-period; quarterly/yearly enforcement requires per-cadence canonical counters and is deferred until a cadence other than monthly is exposed in the UI.
- The Platform Usage page (a strict subset of Analytics calling the same endpoint) is removed; the Budgets page takes its navigation slot.

---

## Related Decisions

- [Budget Model Redesign](2026-06-10-budget-model-redesign.md) — data model unchanged; spend-summation and role-enforcement sections superseded by this ADR.
- [LLM Cost Tracking ADR](2026-06-05-llm-cost-tracking.md) — Decisions 1–4 unchanged.
- [Inference Usage Logs](2026-04-17-inference-usage-logs.md) — `inference_usage` remains the source of truth for rehydration and the new per-principal spend listing.
