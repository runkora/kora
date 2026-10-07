# Domain Metrics Instrumentation per Backend Service

**Status:** Accepted
**Date:** 2026-10-07
**Deciders:** Platform Engineering
**Technical Story:** [Domain Metrics Instrumentation PRD](../prd/domain-metrics-instrumentation.md), stacked on [Prometheus Metrics Endpoints](2026-10-06-prometheus-metrics-endpoints.md) (PR #408)

---

## Context

### Background

PR #408 shipped a Prometheus `/metrics` endpoint per service and a uniform HTTP request counter/histogram, but deliberately deferred instrumenting any service's business logic — "the per-service domain metrics... are catalogued in the ADR's Implementation section as named follow-up work, not claimed as delivered here" (that ADR's Follow-up section). This ADR is that follow-up, covering all nine in-scope services in one stacked PR per explicit direction, rather than the incremental per-service rollout the prior ADR's own Implementation section recommended.

### Problem Statement

Each service's feature package already models the domain events an operator cares about (run state, policy decisions, inference usage/cost, delivery outcomes, credential minting, tool-call decisions) in Go types and enums. None of them are exported as metrics. The work here is mechanical per call site, but two cross-cutting design questions recur enough to need a single, binding answer instead of nine ad hoc ones: **which labels are safe**, and **what to do when a service's existing query boundary (e.g. tenant isolation) doesn't line up with what a global gauge needs**.

### Goals

- Instrument the full metric catalog below across identity, orbit-agents, inference-gateway, policy-gateway, policy-service, orbit-triggers, egress-gateway, tool-gateway, session-manager.
- Establish a binding cardinality policy so this doesn't need re-litigating metric-by-metric.
- Resolve the two concrete conflicts surfaced during research: `policyservice.Stats()`'s per-tenant isolation, and budget-ceiling scopes that include unbounded IDs.

### Non-Goals

- Any change to `internal/platform/metrics/`, `internal/platform/observability/`, or the Helm chart — all already shipped.
- `orbit-mcp` — pure passthrough, no independent decision points (see PRD Non-Goals).

---

## Decision Drivers

* Cardinality safety — a label mistake here degrades Prometheus/alerting for every service, not just one.
* Reuse of existing enums/sentinel errors as label sources, to avoid inventing a parallel taxonomy.
* Respecting existing tenant-isolation boundaries (`policyservice.Stats()`) rather than quietly weakening them to make a gauge convenient.
* Minimal new DB access patterns — prefer instrumenting at existing call sites over adding new queries, except where a gauge genuinely requires one.

---

## Considered Options

### Option 1: Per-request/per-tenant labels everywhere (maximum dimensionality)

**Description:** Add `tenant_id`, `principal_id`, or `run_id` to every metric that has one available, for maximum drill-down.

**Cons:**
- Explodes series count: `tenant_id` alone on the cross-cutting HTTP counter multiplies by tenant count on top of route×status×method. `run_id` or `principal_id` anywhere is effectively unbounded (a run is single-use; a growing tenant has unbounded principals).
- Directly contradicts the existing cardinality guidance already baked into PR #408's metrics middleware (route *pattern*, not raw path).

### Option 2: No tenant/identity labels anywhere (safest, least useful)

**Description:** Keep every label enum-backed, never add any tenant or identity dimension, even where billing/budget use cases would benefit.

**Cons:**
- Defeats a real, named use case (per-tenant cost/usage dashboards, `docs/prd/llm-cost-tracking.md`) that the business already wants and that DB queries already support per-row — Prometheus just can't show it live.

### Option 3: Selective tenant labeling, binding cardinality policy (chosen)

**Description:** Default to enum-backed labels only; allow `tenant_id` exclusively on metrics explicitly scoped to tenant-level billing/budget visibility (inference-gateway's usage/token/cost counters, and the budget-utilization gauge restricted to `scope ∈ {tenant, role_pool}`); never allow run/principal/session/agent-name as label values anywhere.

**Cons:**
- Requires a documented, binding policy (this ADR) rather than a single blanket rule — slightly more to communicate in review.

---

## Decision

**Chosen Option:** Option 3 — selective tenant labeling under a binding cardinality policy.

### Rationale

Tenant count is bounded and slow-growing — the same property that already makes `global.metrics.scraperNamespace`-style per-tenant config safe elsewhere in this codebase. Run IDs and principal IDs are not: a `policy.BudgetCeiling` with `Scope == "run"` is single-use, and a growing tenant's principal set is effectively unbounded from Prometheus's perspective. Drawing the line at "tenant or role-pool scope only, never run or principal" captures the real billing/budget use case while keeping every new series set bounded by a quantity (tenants, role pools) the platform already treats as enumerable and slow-changing.

---

## Consequences

### Positive

- Operators get real-time dashboards/alerting for run health, policy decisions, inference cost, delivery outcomes, credential minting, and tool-call decisions — the actual gap this PR's PRD identifies.
- The cardinality policy is now written down once, applicable to every future metric addition without re-deriving it.

### Negative

- `orbit_agents_run_transitions_total`/`_runs_active`'s `framework` label costs an extra `GetAgentType` DB lookup per call (`run_service.go`'s `Transition`/`StartReaper` don't have an `AgentType` loaded in scope today) — accepted as low-impact, since run transitions aren't a hot per-request path, and the lookup pattern (`s.atRepo.GetAgentType`) already exists elsewhere in the same file (lines 309, 390, 745).
- `orbit_policyservice_grants`/`_roles` ship as tenant-aggregate gauges only in this PR, not per-tenant, because `policyservice.Stats()` is deliberately scoped to one tenant per call (`policyservice/api/service.go:360-362`, to avoid leaking cross-tenant counts through that API). A tenant-aggregate gauge needs a new, lightweight "sum across all tenants" query path in `grantRepo`/`roleRepo` that doesn't exist today — smallest delta that respects the existing isolation boundary.
- `orbit_inference_gateway_budget_utilization_ratio` is computed at each budget-check call site, not via an async observable-gauge callback, because no cached registry of "all current ceilings" exists to poll (`RedisBudgetCounter.GetSpends` returns raw spend only; the spend/limit pairing lives in a per-request `policy.BudgetCheckResult.Ceilings`, `evaluator.go:255`). This means the gauge only updates when a budget check actually runs, not continuously — acceptable, since budget checks run on every inference request.

### Neutral

- `egress-gateway`'s credential-mint metrics are instrumented at the `egressgw/service.go:170` call site, not inside the shared `internal/credentials` package where `Connector.Mint` is actually defined (`credentials/adapter.go:81`) — keeps the metric owned by the service binary that emits it, consistent with every other service in this catalog, rather than making the shared `credentials` package depend on metrics for a label set only one caller uses today.

---

## Implementation

Binding cardinality policy (see PRD for the full statement): enum-backed labels by default; `tenant_id` only on inference-gateway's billing/budget metrics; never a run/principal/session/agent-name ID as a label value, at any scope; `framework` (bounded, `agents.AgentFramework`) is safe wherever an agent run is involved.

| Service | Metric | Type | Labels | Instrumentation point |
|---|---|---|---|---|
| identity | `orbit_identity_sessions_active` | gauge (observable) | — | new `SessionRepository.CountActiveSessions` (`session_repository.go`, alongside `CreateSession`/`GetSessionByID`/`GetSessionByJTI`/`RevokeSession` at lines 47/77/86/95) — `WHERE revoked_at IS NULL AND expires_at > now()`; no such query exists today |
| identity | `orbit_identity_delegations_issued_total`/`_revoked_total` | counter | — | `delegation_service.go:64` (`MintDelegation`) / `:176` (`RevokeDelegation`) |
| identity | `orbit_identity_auth_failures_total{reason}` | counter | `reason` | `service.go:568` (`ErrRefreshTokenNotFound`), `:578` (`ErrRefreshTokenRevoked`), `:583` (`ErrRefreshTokenExpired`), `:600` (`ErrTokenRevoked`), `:713`/`:720` (`ErrInvalidToken`); sentinels at `models.go:373-403` |
| orbit-agents | `orbit_agents_run_transitions_total{from_state,to_state,framework}` | counter | `from_state,to_state,framework` | `run_service.go:463` (`Transition`), `:527` (`TransitionRunState`); `framework` resolved via `s.atRepo.GetAgentType(ctx, run.AgentTypeID)`, same pattern as lines 309/390/745 |
| orbit-agents | `orbit_agents_runs_active{state,framework}` | gauge (observable) | `state,framework` | callback over run store (existing `AllRunStates()`-style read) |
| orbit-agents | `orbit_agents_run_duration_seconds` | histogram | — | `CompletedAt - StartedAt`, same call site as the terminal-state branch in `Transition` (`run_service.go:503-516`) |
| orbit-agents | `orbit_agents_runs_reaped_total{outcome}` | counter | `outcome` | `run_service.go:1000` (`StartReaper`), `:1016` (`ReapStaleRuns`) |
| orbit-agents | `orbit_agents_sessions_active` | gauge (observable) | — | browser `Session` store, `DisconnectedAt == nil` |
| orbit-agents | `orbit_agents_session_duration_seconds` | histogram | — | where `Session.DisconnectedAt` is set |
| orbit-agents | `orbit_agents_sandbox_provision_duration_seconds{phase}`/`_failures_total{phase}` | histogram/counter | `phase` | `session.go: ProvisionSandbox` (`SandboxPhase*` consts) |
| orbit-agents | `orbit_agents_guardrail_warnings_total{kind,tool}` | counter | `kind,tool` | `guardrails.go: loopGuard`, `GuardrailWarningPayload` |
| orbit-agents (P1) | `orbit_agents_runs_active{state,tenant_id}` | gauge (observable) | `state,tenant_id` | optional — bounded by tenant count, same callback as above with an added dimension; include if time allows |
| session-manager | `orbit_session_manager_sessions_active` | gauge (observable) | — | `sessionmanager/session.go:332` (`Registry.ActiveCount`); meter already constructed in `cmd/session-manager/main.go` (`initMeter:65`, used by `mw.MetricsMiddleware` at `:152`) — this just registers the callback, no new plumbing |
| policy-gateway | `orbit_policygw_checks_total{allow,resource_type}`/`_check_duration_seconds{allow,resource_type}` | counter/histogram | `allow,resource_type` | `policygw/service.go:26` (`Check`); `resourceType` param already a logged field `resource_type` at `:50` |
| policy-gateway | `orbit_policygw_denies_total{reason}` | counter | `reason` | same call site, on `!result.Allow`; `policy.Result{Allow,Reason}` defined `platform/policy/evaluator.go:208-211` |
| policy-service | `orbit_policyservice_grants{principal_type}`/`_roles{kind}` | gauge (observable) | `principal_type`/`kind` | new tenant-aggregate query (sum across all tenants) in `grantRepo`/`roleRepo`, distinct from the existing per-tenant `Stats()` path (`policyservice/api/service.go:334-399`) which stays as-is for its own endpoint |
| policy-service | `orbit_policyservice_bundle_stale_total` | gauge (observable) | — | aggregate count of tenants whose bundle `Status == "stale"`, same new aggregate query; **not** per-tenant (`tenantBundleStatus.TenantID` stays internal to `Stats()`'s existing per-tenant response) |
| inference-gateway | `orbit_inference_gateway_usage_total{status,provider,model,tenant_id}` | counter | `status,provider,model,tenant_id` | `service.go: writeUsage` (`:725-738`), fed by `InferenceUsageRecord` (`models.go:145-192`): `Status` (169), `Provider` (153, nil-safe default `"unknown"`), `ModelID` (154), `TenantID` (148) |
| inference-gateway | `orbit_inference_gateway_tokens_total{provider,model,kind,tenant_id}` | counter | `provider,model,kind,tenant_id` | same call site; `kind ∈ {prompt,completion,cache_read,cache_creation}` from `PromptTokens`/`CompletionTokens`/`CacheReadInputTokens`/`CacheCreationInputTokens` (models.go:158-162) — `TotalTokens` not separately emitted (would double-count) |
| inference-gateway | `orbit_inference_gateway_cost_usd_total{provider,model,tenant_id}` | counter | `provider,model,tenant_id` | same call site, `EstimatedCostUSD` (models.go:163) |
| inference-gateway | `orbit_inference_gateway_request_duration_seconds{provider,model,status}` | histogram | `provider,model,status` (no tenant_id — would multiply with provider/model/status) | same call site, `LatencyMS` (models.go:173) |
| inference-gateway | `orbit_inference_gateway_budget_utilization_ratio{scope,window}` | gauge (set at call time, not observable) | `scope ∈ {tenant, role_pool} only`, `window` | computed as `SpendUSD/LimitUSD` from each `policy.BudgetCeiling` in `BudgetCheckResult.Ceilings` (`evaluator.go:140-149,255`) wherever that result is consumed; ceilings with `Scope ∈ {"run","principal"}` are skipped — unbounded cardinality |
| orbit-triggers | *(existing instruments)* | — | — | `metrics.go:68-125` already Prometheus-visible via PR #408's reader attach; no changes |
| orbit-triggers | `orbit_triggers_delivery_outcomes_total{outcome}` | counter | `outcome ∈ StatusVerified/Dispatched/Skipped/Failed/Duplicate/Missed` | `outcomes.go: derive` → `DeriveDeliveryStatus` (`postgres.go:641-663`); enum `models.go:50-55` |
| orbit-triggers | `orbit_triggers_admission_outcomes_total{outcome}` | counter | `outcome ∈ AdmitEnqueued/Superseded/Dropped/Paused` | `admission.go:167-234` (`Admit`), enum `admission.go:49-63` |
| egress-gateway | `orbit_egress_gateway_credentials_minted_total{adapter,action}`/`_mint_duration_seconds{adapter,action}` | counter/histogram | `adapter,action` | `egressgw/service.go:170` (`connector.Mint` call site); `adapter` from `Adapter.Type()` (`credentials/adapter.go:42`, values `models.go:27-33`), `action` from `Classification.Action` (`adapter.go:104`) |
| egress-gateway | `orbit_egress_gateway_mint_failures_total{reason}` | counter | `reason` | same call site; sentinels `credentials/models.go:83-106` (`ErrConnectionNotFound`, `ErrUnsupportedAdapter`, `ErrInvalidConfig`, `ErrCredentialScopeRejected`) |
| tool-gateway | `orbit_tool_gateway_invocations_total{server,tool,decision}`/`_invocation_duration_seconds{server,tool,decision}` | counter/histogram | `server,tool,decision ∈ {allow,deny,error}` | `mcp_controller.go:236` (`handleToolsCall`); `server`/`tool` params already span-tagged (`:251-297`), `decision` already set via `mcp.decision` span attribute (`:264,269,284`) |
| tool-gateway | `orbit_tool_gateway_servers{state}` | gauge (observable) | `state ∈ pending/ok/degraded` | `introspector.go:240` (`Snapshot`); `ServerState` enum `models.go:48-59` |

A precise "active users" metric remains a known gap (no `last_activity_at` column), unchanged from the prior ADR.

---

## Validation

### Success Metrics

- `curl :9464/metrics` on each service shows the new series after driving the corresponding event.
- `make check` and `make test-postgres` green.
- A manual review pass confirms no label in the table above resolves to a raw ID at runtime (spot-check `tenant_id` usages against the cardinality policy).

### Monitoring

- Same `make redeploy` + `kubectl port-forward` + `curl` smoke test pattern as the prior ADR, repeated per service in scope.

---

## Related Decisions

- [Prometheus Metrics Endpoints](2026-10-06-prometheus-metrics-endpoints.md) — the plumbing this ADR instruments against.
- [Rendered Service Config Files](2026-10-06-rendered-service-config-files.md) — unrelated to this change, no config schema additions here.

---

## Notes

Shipped as a single stacked PR covering all four tiers, per explicit direction, rather than the incremental per-service rollout the prior ADR's own Implementation section recommended ("one or a few services at a time, so each lands with its own review"). Recorded here, not re-litigated.

**Last Updated:** 2026-10-07
