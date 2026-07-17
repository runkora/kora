# Budget Model Redesign: Inline JSONB on Roles/Grants, MAX Resolution, Per-Category Spend Accumulation

**Status:** Proposed  
**Date:** 2026-06-10  
**Deciders:** Platform Engineering  
**Supersedes:** Decision 5 of [LLM Cost Tracking ADR](2026-06-05-llm-cost-tracking.md)  
**Technical Story:** [PRD: LLM Cost Tracking](../prd/llm-cost-tracking.md)

---

## Context

### Background

[Decision 5 of the LLM Cost Tracking ADR](2026-06-05-llm-cost-tracking.md) chose Option B: three dedicated quota tables (`quota_roles`, `principal_quota_role_memberships`, `principal_quotas`) owned by the policy-service database, with a per-principal Redis counter (`quota:{principal_id}:{YYYY-MM}`) as the fast spend read layer.

During early implementation, this design was reviewed against the existing Orbit data model. The conclusion: the three-table quota model introduces a third identity concept (quota role) that sits alongside the two mechanisms that already govern every principal — OIDC roles (coarse, external, IDP-managed) and individual grants (fine-grained, internal, Orbit-managed). This is redundant. The existing model already satisfies the stated requirements — budget policies can live inline on role mappings and grants, the same two primitives that already carry scope assignments.

### Problem with the prior decision

1. **Introduces a new mid-tier concept.** A `QuotaRole` is not an OIDC role and not a grant — it is a third concept, maintained separately, with its own membership join table. Operators must learn a new primitive and navigate a separate `/policy/quotas` page to configure it.

2. **Requires a separate Quotas page.** Budget configuration sits in a different section from access control, even though both are properties of the same policy primitives (roles and grants). This splits the mental model for operators who must configure both.

3. **SRP concern is organisational, not technical.** The argument for separation is that access control (scopes) and cost governance (budgets) are different concerns. In practice, Orbit is operated by a single platform team — provisioning a role or grant is a single action ("engineer role gets `inference:read` and $50/month"). There is no separate security team and FinOps team requiring independent write paths. The SRP concern is speculative; the complexity cost of three extra tables and a separate UI surface is immediate.

4. **Mid-tier group mechanics do not match the model.** Orbit's grant model is a permission model: grants only add capability, never restrict it. A `QuotaRole` with lower precedence than a `PrincipalQuota` is consistent, but a standalone mid-tier concept that can be neither an OIDC role nor an individual grant creates a new semantic that does not exist anywhere else in Orbit.

5. **Per-principal Redis counter loses per-source attribution.** A single `quota:{principal_id}:{YYYY-MM}` counter cannot tell you whether the spend came from a role-budget category or a grant-budget category. Per-category counters are required for independent reporting, alerting, and reset scheduling.

---

## Decision Drivers

* **Consistency with existing model** — budget policy should live on the same primitives that already govern access (OIDC roles, individual grants), not on a separate object.
* **Operator simplicity** — configuring a role or grant should be a single form, not a two-step process across different pages.
* **MAX semantics are a better fit than precedence** — budget is a permission-like property; grants should only raise the effective limit, never lower it. MAX is consistent with how scopes resolve as UNION.
* **Per-category spend attribution** — each budget source should have its own counter for reporting, alerting, and reset tracking, independent of the enforcement ceiling.
* **Minimal schema footprint** — inline JSONB is cheaper than three tables; budget schema evolves without migrations to new tables.
* **No new infrastructure** — the existing OPA bundle pipeline, Redis stack, and roles/grants UI are all reused.

---

## Decision

### Supersede Decision 5

The three-table quota model (`quota_roles`, `principal_quota_role_memberships`, `principal_quotas`) is replaced with inline budget JSONB on the existing `policy_role_scopes` and `policy_grants` tables.

**Schema change:**

```sql
-- Add to policy_role_scopes (OIDC role → scope mappings)
ALTER TABLE policy_role_scopes
    ADD COLUMN budget JSONB NULL;

-- Add to policy_grants (individual principal grants)
ALTER TABLE policy_grants
    ADD COLUMN budget JSONB NULL;

-- Drop the quota tables
DROP TABLE IF EXISTS principal_quota_role_memberships;
DROP TABLE IF EXISTS quota_roles;
DROP TABLE IF EXISTS principal_quotas;
```

The `budget` column holds `null` (no budget for this category) or:

```json
{ "monthly_limit_usd": 50.00, "reset_cadence": "monthly" }
```

`reset_cadence` is `"monthly"` in this release. Additional cadences (`"quarterly"`, `"yearly"`) are reserved for future extension.

---

## Considered Options

### Option A: Separate quota tables (prior Decision 5)

Three tables: `quota_roles`, `principal_quota_role_memberships`, `principal_quotas`. Individual quota takes precedence over role quota.

**Pros:**
- Budget schema evolves independently of the grants/roles tables.
- Clear SRP: scopes and budgets are separate DB records.
- Independent audit trail per budget record (created_at, updated_at per quota).

**Cons:**
- Introduces a third principal-governance concept orthogonal to OIDC roles and grants.
- Requires a separate `/policy/quotas` page and admin UX surface.
- First-match precedence (individual → role → no limit) means an individual quota can reduce a principal's budget below their role budget — violating the principle that grants only add capability.
- Single per-principal Redis counter loses per-source attribution.
- Three extra tables, a join table, and a CRUD API surface that mirrors the existing grants/roles surface for one field.

### Option B: Inline `budget` JSONB on `policy_role_scopes` and `policy_grants` (Chosen)

Budget is a field on the record, parallel to `scopes`. MAX resolution. Per-category Redis counters.

**Pros:**
- No new concept — OIDC roles and individual grants are the only budget carriers, same as they are the only scope carriers.
- Single edit form: role/grant configuration is a single admin action.
- MAX resolution: grants only raise the effective limit, consistent with UNION for scopes.
- Per-category counters: each role-budget and grant-budget category has its own Redis counter, enabling independent reporting and reset tracking.
- Minimal schema change: two `ALTER TABLE ADD COLUMN` statements, no new tables.
- Bundle compiler already iterates over roles and grants; adding `budget` extraction is an incremental change.

**Cons:**
- Budget and scope share the same record — two concerns on one row (the SRP concern).
- No independent per-budget-assignment audit trail; only the role/grant `updated_at` changes when a budget is modified.
- Inline JSONB is less structured than a typed table; schema validation must be enforced at the application layer.

### Option C: Parallel `role_budgets` / `grant_budgets` tables keyed by FK

Separate tables, each with a foreign key to `policy_role_scopes` or `policy_grants`.

**Pros:**
- Clean schema separation; budget evolves without touching the grants/roles tables.
- Independent audit trail per budget assignment.

**Cons:**
- Two records to manage per budget assignment; join on every bundle compile.
- Orphan risk without cascade deletes; more migration surface.
- Adds complexity without solving the SRP concern for the operator (they still configure both on the same UI screen).

---

## Resolution Semantics

### MAX over first-match precedence

**Prior model (Decision 5):** individual quota → role quota → no limit (first match wins; individual quota always takes precedence, even if lower).

**New model:** `effective_limit_usd = MAX(monthly_limit_usd across all applicable assignments)`.

A principal with OIDC role "engineer" ($50/month) and an individual grant ($80/month) has `effective_limit_usd = 80`. A principal with only a role budget has `effective_limit_usd` equal to that role's limit. A principal with no budget assignments has no limit (fail-open).

**Why MAX:** Budget is a permission-like property. Granting a principal a higher limit is additive — consistent with how scope grants work (UNION). A per-principal grant that *lowers* the effective limit below a role budget would violate the additive semantics of the grants model and produce surprising outcomes (adding a grant makes a principal more restricted).

### Spend accumulation against all categories

Spend is written to every applicable category counter on each inference request, not just the effective category. A principal with an OIDC role budget and an individual grant budget has two counters written on every request:

```
budget:{principal_id}:role:{role_name}:{period}
budget:{principal_id}:grant:{grant_id}:{period}
```

**Why:** Each category has its own reporting, alerting, and reset schedule. The effective budget (`MAX`) determines when to block; per-category counters record where the money was attributed and who is accountable for it. Without per-category counters, it is impossible to report "how much spend was covered by the engineer role budget vs the individual override."

---

## OPA Bundle Shape

The bundle compiler replaces `data/quotas/data.json` with a `data/budgets/data.json` document. `effective_limit_usd` is pre-computed at compile time — OPA evaluates a comparison, not a resolution.

```json
{
  "budgets": {
    "usr_abc123": {
      "categories": [
        { "source": "role",  "id": "engineer",  "monthly_limit_usd": 50, "reset_cadence": "monthly" },
        { "source": "grant", "id": "grant_xyz", "monthly_limit_usd": 80, "reset_cadence": "monthly" }
      ],
      "effective_limit_usd": 80
    }
  }
}
```

The `categories` array is included so that the inference gateway can construct the correct per-category Redis counter keys from bundle data without needing a separate API call.

### Rego rule

```rego
package orbit.budget

import rego.v1

default budget_allow := true  # fail-open: no budget = allow

budget_allow := false if {
    principal := data.budgets[input.principal.id]
    input.current_spend_usd >= principal.effective_limit_usd
}
```

The input document for budget checks:

```json
{
  "principal": { "id": "usr_abc123", "tenant_id": "tenant_1" },
  "current_spend_usd": 75.00
}
```

`current_spend_usd` is the total of all per-category Redis counters for the current period, read by the inference gateway immediately before this call.

---

## Redis Key Shape

```
budget:{principal_id}:role:{role_name}:{period}
budget:{principal_id}:grant:{grant_id}:{period}
```

- **Type:** Redis string (float, USD).
- **Period format:** cadence-dependent — `2026-06` (monthly), `2026-Q2` (quarterly), `2026` (yearly).
- **TTL:** 35 days from last write for monthly cadence — outlives the calendar month by a safe margin.
- **Increment:** `INCRBYFLOAT` on each `inference_usage` write, best-effort after the DB write. Redis failure does not roll back the DB write; the counter self-corrects on next rehydration.
- **Rehydration:** on cache miss, `SELECT SUM(estimated_cost_usd) FROM inference_usage WHERE principal_id = $1 AND DATE_TRUNC('month', created_at AT TIME ZONE 'UTC') = DATE_TRUNC('month', NOW() AT TIME ZONE 'UTC')`.
- **On cadence change:** the old key is invalidated; a new key for the new period is seeded from `inference_usage`.

The inference gateway reads all applicable category counters (derived from `data.budgets[principal_id].categories`), sums them, and injects the total as `current_spend_usd` into the OPA input. OPA compares this single value against `effective_limit_usd`.

---

## Migration Path

Migration `20260609000001_create_quota_tables.sql` is modified in place (single developer, no shared state — this migration has not been applied to any shared environment).

The migration is rewritten to:

1. Drop `quota_roles`, `principal_quota_role_memberships`, `principal_quotas` (if they exist from any partial prior run).
2. `ALTER TABLE policy_role_scopes ADD COLUMN budget JSONB NULL`.
3. `ALTER TABLE policy_grants ADD COLUMN budget JSONB NULL`.

No data migration is required — no quota data has been written in production.

---

## Consequences

### Positive

- No new database tables, no new concept in the admin model.
- Budget configuration is part of role/grant management — single form, single page, single mental model for operators.
- MAX resolution is consistent with UNION for scopes — grants only add capability, never restrict.
- Per-category counters enable independent reporting and reset scheduling per budget source.
- `effective_limit_usd` is pre-computed at bundle compile time — OPA evaluation is a single comparison, not a resolution algorithm.
- Bundle manifest change: replace `"quotas"` root with `"budgets"` root.
- `UpsertRole` must be updated to explicitly preserve `budget` when only scopes are being modified — an existing gap that this change surfaces.

### Negative

- Budget and scopes share a record — the SRP concern exists at the data layer even if it is manageable operationally.
- No independent per-budget-assignment audit trail — budget changes show only in the role/grant `updated_at` timestamp. If budget limit history becomes a requirement, a dedicated `budget_history` event table will need to be added.
- Inline JSONB requires application-layer validation of the budget schema; no DB-level schema constraint.

### Neutral

- All files named `quota*` in surviving code are renamed to `budget*` as part of this change. Files being deleted are not renamed first. Full file-by-file detail in `BUDGET-REDESIGN.md`.
- Scope strings `platform:quotas:read` and `platform:quotas:write` become `platform:budgets:read` and `platform:budgets:write`. Built-in role scope assignments in `bundle/compiler.go` are updated accordingly.
- The `data/quotas/data.json` bundle path is replaced by `data/budgets/data.json`; OPA bundle manifest `Roots` updated (`"quotas"` → `"budgets"`).

---

## Known Limitations

- **No retroactive per-category spend attribution.** There is no audit trail of budget limit changes (no history table, no event emission). If a budget limit changes mid-period, the previous limit cannot be reconstructed after the fact. Spend is fully auditable via `inference_usage` (every request is recorded), but per-category limit history is not. This is acceptable — enforcement and current-period reporting remain accurate. Retroactive per-category attribution is a future concern if historical budget reporting becomes a requirement.

---

## Related Decisions

- [LLM Cost Tracking ADR](2026-06-05-llm-cost-tracking.md) — Decisions 1–4 remain accepted. Decision 5 is superseded by this ADR.
- [policy-gateway-and-authorisation-model](2026-04-14-policy-gateway-and-authorisation-model.md) — Policy model; `platform:budgets:*` scopes follow the established vocabulary pattern.
- [inference-usage-logs](2026-04-17-inference-usage-logs.md) — `inference_usage` is the source of truth for spend aggregation and Redis rehydration.

---

## References

- [PRD: LLM Cost Tracking](../prd/llm-cost-tracking.md)
- BUDGET-REDESIGN.md — Working notes, lineage diagrams, removal & rename plan

---

## Notes

**Why not Option C (parallel FK tables)?** The SRP concern is real at the schema level, but Option C does not solve it for operators — they still configure budget and scopes in the same UI form. It adds a join on every bundle compile, an FK constraint, and an orphan-management problem. Inline JSONB achieves the same operational simplicity with less schema surface. If budget ownership ever diverges from grant ownership (e.g. a FinOps team needs independent write access to budgets without touching scopes), extraction into a parallel table is a straightforward additive migration; the inline approach does not prevent that evolution.

**Why pre-compute `effective_limit_usd` in the bundle compiler rather than in Rego?** OPA is an evaluation engine, not a calculation engine. Pre-computing MAX at compile time keeps the Rego rule trivially simple (`current_spend >= effective_limit`) and ensures the enforcement hot path is a single comparison. It also means the `categories` array is available for Redis key construction in the inference gateway without requiring a separate API call to the policy service.

**Why sum all per-category counters at enforcement time rather than maintain a single aggregate counter?** A single aggregate counter loses per-category attribution permanently. The cost of summing N counters (where N is the number of applicable role + grant budget categories, typically 1–3) at enforcement time is negligible compared to the value of retaining per-source spend data for reporting and alerting. The `categories` array in `data.budgets` provides the key templates; the gateway iterates over them in a single Redis MGET.
