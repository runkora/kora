# Extended Trigger Types: Scheduled and Manual

<!-- Filename: 2026-06-20-extended-trigger-types.md -->

**Status:** Proposed
**Date:** 2026-06-20
**Deciders:** Platform Engineering
**Technical Story:** [PRD: Extended Trigger Types](../prd/extended-trigger-types.md) — extend the Triggers framework beyond webhook ingestion to include scheduled (cron) and manual (API-fired) trigger kinds, and extract `internal/triggers/` to the committed `orbit-triggers` binary.

---

## Context

The Triggers v1 ADR (`2026-06-17-agent-triggers-and-webhook-ingestion.md`) shipped webhook ingestion as the only trigger kind and made two explicit commitments for when a second kind lands:

1. **Extract `internal/triggers/` to a dedicated `orbit-triggers` binary.** The v1 hosting inside `identity` was explicitly called a *"v1-only choice"* and the extraction was described in detail in the Notes section of that ADR.
2. **Add scheduled and other trigger kinds.** Scheduled/cron triggers were listed as a non-goal for v1 and as the first follow-on kind.

This ADR fulfils both commitments. It records the decisions for:
- A `kind` discriminant on the `Trigger` model.
- The **scheduled trigger** kind: cron-based firing via an in-process scheduler in `orbit-triggers`.
- The **manual trigger** kind: an explicit API call (`POST /api/v1/triggers/{id}/fire`) that fires a trigger programmatically — enabling agent-to-agent orchestration.
- `{{ var.key }}` variable substitution in prompt templates (used by both new kinds).
- The `orbit-triggers` binary extraction mechanics.

### Background

The `internal/triggers/` package was deliberately structured with no imports of other feature packages (`internal/identity`, `internal/agents`). It reaches the policy service and `orbit-agents` exclusively via typed HTTP clients. The v1 ADR documented the extraction cost as bounded to:
1. A new `cmd/orbit-triggers/` binary wiring the existing package.
2. Replacing the in-process `TokenMinter` with an HTTP call to a new internal mint endpoint on `identity`.
3. Moving trigger migrations to the new binary.
4. Moving the ingress routing rules.

This ADR confirms those mechanics and adds the new-kind decisions on top.

### Problem Statement

1. There is no way to start a run on a time schedule without external cron infrastructure and human credentials.
2. There is no way for an agent to start another agent — agent-to-agent composition requires a human or out-of-band scripting.
3. The triggers package is still hosted in `identity`, which the v1 ADR committed to unwinding.

### Goals

- Add `scheduled` and `manual` trigger kinds to the existing framework.
- The shared dispatch pipeline (template render → identity mint → run start → delivery log) is reused unchanged across all kinds.
- Extract `internal/triggers/` to `orbit-triggers`.
- Preserve the *Who / What / How* pillars for all new kinds.
- Introduce `{{ var.key }}` variable substitution so manual and scheduled triggers can be parameterised.

### Non-Goals

- HA/distributed cron scheduler (single-instance best-effort in v1).
- Email or other ingest-based trigger kinds.
- Caller override of prompt template or agent type at fire time.
- DAG/workflow orchestration across agents.
- Sub-minute cron resolution.

---

## Decision Drivers

* **Extraction commitment** — the v1 ADR binds us to extracting when the second kind lands; deferring further would accrete more webhook-specific coupling inside `identity`.
* **Shared dispatch pipeline** — all three kinds must funnel into the same render/mint/start/log path; the `kind` discriminant must not fork that logic.
* **Identity integrity** — scheduled and manual triggers need the same per-source principal and JIT-mint model as webhooks, with no weakening.
* **Minimal caller surface for manual triggers** — firing a trigger must be a scoped, per-trigger capability; callers must not be able to shape the run's agent, template, or source identity.
* **Convention fit** — typed clients, feature packages don't import each other, controller-based routing, route/scope registration.

---

## Considered Options

### A — Kind discriminant placement

#### A1: `kind` column on the `triggers` table; `schedule_config` JSONB for kind-specific config

A single `kind TEXT NOT NULL` column (values: `'webhook'` | `'scheduled'` | `'manual'`). Kind-specific configuration lives in a `config JSONB` column (null for kinds with no extra config; `{"cron":"0 9 * * 1-5","timezone":"Australia/Sydney"}` for scheduled). The existing `source_id` FK becomes nullable — scheduled and manual triggers have no webhook source.

**Pros:** Single `triggers` table; no table-per-kind proliferation; aligns with the existing `config JSONB` pattern on `webhook_sources`; simple migration.

**Cons:** `source_id` nullable FK is mildly denormalized; JSONB config is schema-less (mitigated by Go struct validation before insert).

#### A2: Separate tables per kind (`scheduled_triggers`, `manual_triggers`)

**Pros:** Strongly typed per-kind columns; nullable FK avoided.

**Cons:** Fragments what is conceptually one entity; query joining across tables for the "all triggers" list; more migration surface; complicates the shared dispatch path.

**Chosen: A1.** A single table with a `kind` discriminant and a JSONB config column is consistent with the codebase's existing patterns and keeps the shared dispatch pipeline trivial to query.

---

### B — Scheduled trigger: where does the scheduler run?

#### B1: In-process goroutine in `orbit-triggers` using `robfig/cron`

A scheduler goroutine starts when `orbit-triggers` boots, loads all enabled scheduled triggers from the DB, and fires them at each cron tick. The `robfig/cron` library handles standard 5-field cron parsing and tick evaluation. On fire, it follows the same dispatch path as webhook ingest (render template → mint JIT token → start run → record delivery).

**Pros:** Zero additional infrastructure; simple to operate and test; consistent with the single-instance v1 posture; `robfig/cron` is a mature, well-tested library.

**Cons:** Single-instance: if `orbit-triggers` is unavailable at a tick, that tick is missed (documented). Multiple replicas would double-fire without coordination.

#### B2: External Kubernetes CronJob per scheduled trigger

**Pros:** Native k8s primitive; no in-process scheduler to operate.

**Cons:** One CronJob object per trigger is impractical at scale; requires kubectl or operator reconciliation to manage; no unified delivery log; breaks the "trigger is a platform concept with a UI and history" goal entirely.

#### B3: Distributed leader-election scheduler (DB advisory lock or k8s Lease)

**Pros:** Multi-replica safe from day one.

**Cons:** Significantly more complexity; premature for v1 volume; the extraction ADR notes say "add when needed."

**Chosen: B1.** Single-instance in-process scheduler is the right v1 call. The HA path (B3) is tracked as follow-up and architecturally cheap to add: replace `scheduler.Start()` with a lock-guarded `scheduler.Start()` once there is evidence of multi-replica load.

**Dedup guard for B1:** To prevent double-fire on pod restart within the same minute window, the scheduler inserts the delivery row with a unique index on `(trigger_id, scheduled_at_minute)` before starting the run. A duplicate insert fails silently (same dedup pattern as webhook `provider_delivery_id`).

---

### C — Manual trigger: fire endpoint identity model

#### C1: Caller holds `triggers:fire:{trigger-id}`; run executes under trigger's source principal (same as webhook)

The fire endpoint requires the calling principal to hold a `triggers:fire:{id}` scope (a per-trigger, explicit grant). The resulting run executes under the trigger's source principal — not the caller's identity. The caller's principal ID is recorded in the delivery row as `fired_by` for audit.

**Pros:**
- Caller scope is minimal and per-trigger — a caller with one fire grant cannot fire other triggers.
- The run's identity and scope are fully determined by the trigger's source configuration, not by the caller. This closes the "caller shapes the run's capabilities" attack surface completely.
- Consistent with the webhook model (run always executes under the source principal).
- Attribution is preserved: `created_by` = source principal, `fired_by` = calling principal.

**Cons:**
- Granting `triggers:fire` requires an admin action via the grants API (no self-service UI in v1).

#### C2: Run executes under the *caller's* identity

**Pros:** No additional principal provisioning needed.

**Cons:** The caller's scope bleeds into the run — an agent that can fire a trigger would start a run with its own (possibly broader) access, breaking the "run is bounded by the trigger's source" invariant. Also breaks attribution: two different callers firing the same trigger would produce runs under different identities, making the trigger semantics inconsistent.

#### C3: Fire endpoint is unauthenticated (HMAC secret per trigger, like webhooks)

**Pros:** No platform token required from the caller.

**Cons:** HMAC secrets are an awkward mechanism for agent-to-agent calls; the platform token + scope model is the right fit for internal callers. This would make manual triggers look like webhooks without the webhook UX.

**Chosen: C1.** Per-trigger `triggers:fire:{id}` scope on the caller, run under the trigger's source principal. This is the cleanest model: minimal caller surface, consistent run identity, full attribution.

---

### D — `orbit-triggers` extraction: `TokenMinter` replacement

The in-process `TokenMinter` is the only coupling between `internal/triggers/` and `identity`. The v1 ADR documented two options for replacing it:

#### D1: New internal HTTP endpoint on `identity` — `POST /internal/v1/triggers/mint-token`

`orbit-triggers` calls this endpoint (over the internal service mesh, mTLS-authenticated via SPIFFE) to mint a short-lived SERVICE JWT for a given source principal. `identity` validates that the source principal exists and belongs to the tenant before minting.

**Pros:** Clean separation; `orbit-triggers` is not the token authority; mirrors how other services get tokens minted (e.g. the session manager).

**Cons:** One additional internal RPC per dispatch; increases coupling between `orbit-triggers` and `identity` at the network level (mitigated: the mesh already enforces this boundary, and the call is on the hot path anyway to start the run).

#### D2: `orbit-triggers` holds a shared signing key and mints tokens in-process

**Pros:** No extra RPC.

**Cons:** Distributes token-signing key material outside the authority; undermines the "identity is the token authority" principle; audit trail for minted tokens becomes harder to centralize.

**Chosen: D1.** `orbit-triggers` calls `identity`'s new internal mint endpoint. The internal route is unauthenticated at the HTTP layer but lives on the internal mesh (not exposed externally); SPIFFE peer identity validates the caller is `orbit-triggers`.

---

## Decision

**Chosen options:** A1 + B1 + C1 + D1.

### Summary

1. **`kind` TEXT column on `triggers`**; existing rows migrate to `'webhook'`; `source_id` becomes nullable (null for scheduled/manual). A new `kind_config JSONB` column carries kind-specific fields (`cron_expression`, `timezone` for scheduled; empty for manual and webhook).

2. **Scheduled triggers: in-process `robfig/cron` scheduler in `orbit-triggers`.** Fires the shared dispatch pipeline at each tick. Dedup via `(trigger_id, scheduled_at_minute)` unique index. Missed ticks skipped (no backfill).

3. **Manual triggers: `POST /api/v1/triggers/{id}/fire`**, guarded by `triggers:fire:{id}` scope. Run executes under the trigger's source principal. Optional `variables` map in request body feeds `{{ var.key }}` substitution. `fired_by` principal ID recorded in the delivery row.

4. **`{{ var.key }}` substitution in `renderTemplate`**: variables passed at fire time (or injected from scheduler context) are merged into the field map under the `var.` namespace, then resolved by the existing `renderTemplate` function. No API change to the templating engine — just an additional field source.

5. **`orbit-triggers` binary**: new `cmd/orbit-triggers/` wrapping `internal/triggers/` unchanged. Webhook ingestion endpoint, management API, scheduler, and fire endpoint all live here. A new internal endpoint `POST /internal/v1/triggers/mint-token` on `identity` replaces the in-process `TokenMinter`.

6. **Delivery records** gain a `kind` column (denormalised from the trigger at insert time, so history survives trigger deletion) and a `fired_by` column (null for webhook and scheduled; caller principal ID for manual).

### Rationale

All four sub-decisions flow from the same principle: extend the framework with minimal deviation from the established patterns. The `kind` column on one table keeps the shared pipeline simple. The in-process scheduler keeps the operational footprint small. The per-trigger fire scope keeps the caller surface minimal and consistent with how the webhook model bounds scope (run always executes under the source principal). The internal mint endpoint is the cleanest way to preserve token-authority locality without distributing signing keys.

---

## Sub-decisions

### 1. `ScheduleConfig` struct and cron dedup

```go
type ScheduleConfig struct {
    CronExpression string `json:"cron_expression"` // 5-field standard cron
    Timezone       string `json:"timezone"`         // IANA timezone name
}
```

Stored in `kind_config` JSONB. The scheduler ticks at the top of each minute; before dispatch it inserts a delivery row with `scheduled_at` = the tick's truncated minute. A `UNIQUE (trigger_id, scheduled_at) WHERE kind = 'scheduled'` partial index prevents double-fire if the scheduler fires twice within the same minute (e.g. on restart).

### 2. `triggers:fire:{id}` scope provisioning

The fire scope follows the same `CreateGrant` pattern as webhook trigger run scopes. When an admin wants to grant a principal the ability to fire a specific manual trigger:

```
POST /api/v1/policy/grants
{
  "principal_type": "service" | "pat",
  "principal_id": "<id>",
  "scopes": ["triggers:fire:<trigger-id>"]
}
```

The fire endpoint's scope enforcement uses the same `ScopeEnforcementMiddleware` as all other protected routes, but with a dynamic scope derived from the trigger ID extracted from the URL. This requires the middleware to support per-request dynamic scopes — which it already does via the route-scope registration pattern (the scope is registered as `triggers:fire:*` with a route param substitution).

### 3. Variable substitution merge

`renderTemplate` currently receives a `fields map[string]string`. For manual and scheduled triggers, the caller- or scheduler-supplied variables are injected under the `var.` namespace before the call:

```go
for k, v := range req.Variables {
    fields["var."+k] = v
}
```

Webhook triggers that supply no variables simply have no `var.*` keys — any `{{ var.key }}` placeholders in their templates render empty, consistent with the existing unknown-key behaviour.

### 4. Source identity for scheduled/manual triggers

Scheduled and manual triggers have no webhook source (no inbound HTTP delivery to authenticate). They do, however, need a principal under which the run executes. **Each scheduled/manual trigger provisions its own `SERVICE` principal** at creation — the same model as webhook sources, but one level down: it is the *trigger*, not the source, that owns the principal. This means:

- `source_id` FK on `triggers` is nullable.
- A new `principal_id` column on `triggers` holds the trigger's own SERVICE principal (set at create time for scheduled/manual; null for webhook, which inherits from its source).
- The dispatch path selects `source.principal_id` for webhook triggers and `trigger.principal_id` for scheduled/manual.

This gives each scheduled/manual trigger its own attributable identity and its own bounded scope, consistent with the webhook model.

### 5. `orbit-triggers` deployment

`orbit-triggers` is added to the Helm chart as a new `Deployment` alongside `orbit-identity`, `orbit-agents`, etc. It needs:
- Database access (same credentials as identity, same schema).
- Internal network access to `orbit-identity` (for the new mint endpoint) and `orbit-agents`.
- The webhook ingestion path (`/api/v1/webhooks/`) moves from identity's ingress route to orbit-triggers' ingress route. A brief migration note is needed in the upgrade guide.

---

## Consequences

### Positive

- Fulfils the v1 ADR's extraction commitment, preventing `identity` from accreting trigger logic.
- Scheduled triggers eliminate the need for external cron infrastructure with human credentials.
- Manual triggers enable agent-to-agent composition as a first-class platform primitive.
- The shared dispatch pipeline means delivery logging, attribution, and run initiation work identically across all three kinds — consistent operational model.
- The `{{ var.key }}` mechanism is general enough to parameterise any trigger without caller-controlled template injection.

### Negative

- `orbit-triggers` is a new binary to deploy and operate (Helm chart, ingress, health checks).
- Webhook ingress route changes from identity to orbit-triggers — operators must update ingress config or the Helm chart handles it.
- Per-trigger principal provisioning for scheduled/manual adds a small amount of policy state per trigger (mitigated: same grant lifecycle pattern as webhook sources).
- Single-instance cron scheduler has a reliability gap for multi-replica deployments (documented; tracked for follow-up).

### Neutral

- The internal mint endpoint on `identity` is a new HTTP surface, but it lives on the internal mesh only and is SPIFFE-attested.
- `{{ var.key }}` variables are passed by the caller verbatim — same injection-deferred posture as webhook payload fields.

---

## Implementation

### High-level steps

1. **DB migration** (identity binary, then migrated to orbit-triggers):
   - Add `kind TEXT NOT NULL DEFAULT 'webhook'` to `triggers`.
   - Add `kind_config JSONB NOT NULL DEFAULT '{}'` to `triggers`.
   - Add `principal_id TEXT` to `triggers` (nullable; set for scheduled/manual).
   - Add `grant_id TEXT` to `triggers` for the trigger's own principal grant (scheduled/manual).
   - Make `source_id` nullable on `triggers`.
   - Add `fired_by TEXT` to `trigger_deliveries`.
   - Add `scheduled_at TIMESTAMPTZ` to `trigger_deliveries` (set for scheduled kind).
   - Add partial unique index `UNIQUE (trigger_id, scheduled_at) WHERE scheduled_at IS NOT NULL`.

2. **`internal/triggers/models.go`**: add `Kind`, `KindConfig` (`ScheduleConfig`), `PrincipalID` to `Trigger`. Add `FiredBy`, `ScheduledAt` to `Delivery`. Add new request/response DTOs for fire endpoint and scheduled config.

3. **`internal/triggers/service.go`**: extend `CreateTrigger` to branch on kind; add `FireTrigger(ctx, tenantID, triggerID, callerID, vars)` for manual kind; add `TickScheduled(ctx, trigger)` called by the scheduler.

4. **`internal/triggers/scheduler.go`** (new file): cron scheduler using `robfig/cron`. Loads enabled scheduled triggers at start; fires `TickScheduled` at each tick. Graceful shutdown.

5. **`internal/triggers/server.go`**: add `POST /api/v1/triggers/{id}/fire` handler. Register `triggers:fire:*` scope pattern.

6. **`cmd/orbit-triggers/`** (new binary): wire `internal/triggers/` with a `TokenMinterClient` (HTTP to identity's internal mint endpoint) instead of the in-process minter.

7. **`cmd/identity/`**: add `POST /internal/v1/triggers/mint-token` handler (internal-only, SPIFFE-attested); remove the triggers package wiring (it moves to orbit-triggers).

8. **Frontend**: add `kind` field to `TriggerRow` and `CreateTriggerRequest`. Update form modal with kind selector and kind-specific fields. Add `FiredBy` to delivery row display.

9. **Helm**: add `orbit-triggers` deployment; update ingress to route `/api/v1/webhooks/` to orbit-triggers.

### Migration Path

- Existing `webhook` triggers: `kind = 'webhook'`; `kind_config = '{}'`; `principal_id = NULL` (source-owned principal is unchanged).
- No data loss; no behavioral change for existing triggers.
- The ingress route change for `/api/v1/webhooks/` is the only breaking operational change; documented in the upgrade notes.

---

## Validation

### Success Metrics

- A scheduled trigger fires within 60 s of each cron tick; delivery log shows each tick.
- A manual trigger fired by an agent with `triggers:fire:{id}` starts a run and returns the run ID.
- A caller without `triggers:fire:{id}` receives 403.
- Existing webhook triggers continue to function identically after extraction.
- Double-fire is not observed on scheduler restart within the same minute window.

### Monitoring

- `trigger_deliveries` is the queryable audit surface for all kinds.
- Structured log entries for each scheduler tick and each manual fire, correlated by trigger ID and delivery ID.

---

## Related Decisions

- [Agent Triggers and Webhook Ingestion](2026-06-17-agent-triggers-and-webhook-ingestion.md) — v1 ADR; this ADR fulfils its extraction commitment and adds two new trigger kinds.
- [Run and Session Lifecycle](2026-05-11-run-and-session-lifecycle.md)
- [Agent Identity and Policy Provisioning](2026-05-11-agent-identity-and-policy-provisioning.md)
- [PAT Scope Assignment via Explicit Grants](2026-04-19-pat-scope-assignment-via-explicit-grants.md)
- [Dynamic Route-Scope Registration](2026-04-14-dynamic-route-scope-registration.md)
- [Platform-wide Scope Enforcement Middleware](2026-06-04-platform-scope-enforcement-middleware.md)
- [JIT Credential Brokering via a Per-Tenant Egress Gateway](2026-06-06-jit-credential-brokering-egress-gateway.md)

---

## References

- [PRD: Extended Trigger Types](../prd/extended-trigger-types.md)
- [robfig/cron](https://github.com/robfig/cron) — Go cron library used for the scheduler.

---

## Notes

The single-instance scheduler is a deliberate v1 constraint. When `orbit-triggers` runs with >1 replica, a distributed lock (Kubernetes `Lease` or DB advisory lock) must be acquired before the scheduler starts. The scheduler is structured to make this cheap: it is a single `scheduler.Start()` / `scheduler.Stop()` call, and wrapping it with lock acquisition/release is a contained change.

**Last Updated:** 2026-06-20
