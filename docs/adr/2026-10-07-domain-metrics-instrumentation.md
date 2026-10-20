# Domain Metrics Instrumentation per Backend Service

**Status:** Accepted (Amended 2026-10-20)
**Date:** 2026-10-07
**Deciders:** Platform Engineering
**Technical Story:** [Domain Metrics Instrumentation PRD](../prd/domain-metrics-instrumentation.md), stacked on [Prometheus Metrics Endpoints](2026-10-06-prometheus-metrics-endpoints.md) (PR #408, merged 2026-10-18)

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

- `orbit_agents_run_transitions_total`/`_runs_active`'s `framework` label costs an extra `GetAgentType` DB lookup per call (`run_service.go`'s `Transition`/`StartReaper` don't have an `AgentType` loaded in scope today) — the lookup pattern (`s.atRepo.GetAgentType`) already exists elsewhere in the same file (lines 309, 390, 745). PR #418 review correctly flagged that `Transition` runs on every state change, including the idle↔active turn boundary (one of the most frequent transitions a run makes), not just terminal ones, so "not a hot per-request path" undersold the cost. Mitigated with an in-process `sync.Map[runID]AgentFramework` cache on `RunService`: a run's framework never changes after creation, so `GetRun`+`GetAgentType` run at most once per run's lifetime (first transition after start) instead of once per transition, and the entry is evicted on terminal state. The remaining cost — one `GetRun` call on every terminal transition, to read `StartedAt`/`CompletedAt` for the duration histogram and to deprovision the sandbox — is unavoidable and unchanged from before this PR.
- `orbit_policyservice_grants`/`_roles` ship as tenant-aggregate gauges only in this PR, not per-tenant, because `policyservice.Stats()` is deliberately scoped to one tenant per call (`policyservice/api/service.go:360-362`, to avoid leaking cross-tenant counts through that API). A tenant-aggregate gauge needs a new, lightweight "sum across all tenants" query path in `grantRepo`/`roleRepo` that doesn't exist today — smallest delta that respects the existing isolation boundary.
- `orbit_inference_gateway_budget_utilization_ratio` is computed at each budget-check call site, not via an async observable-gauge callback, because no cached registry of "all current ceilings" exists to poll (`RedisBudgetCounter.GetSpends` returns raw spend only; the spend/limit pairing lives in a per-request `policy.BudgetCheckResult.Ceilings`, `evaluator.go:255`). This means the gauge only updates when a budget check actually runs, not continuously — acceptable, since budget checks run on every inference request.

### Neutral

- `egress-gateway`'s credential-mint metrics are instrumented at the `egressgw/service.go:170` call site, not inside the shared `internal/credentials` package where `Connector.Mint` is actually defined (`credentials/adapter.go:81`) — keeps the metric owned by the service binary that emits it, consistent with every other service in this catalog, rather than making the shared `credentials` package depend on metrics for a label set only one caller uses today.

---

## Amendment (2026-10-20): broader `tenant_id`, and a bounded `agent_id`

The original Cardinality Policy restricted `tenant_id` to inference-gateway's billing metrics alone, reasoning that tenant count was merely "bounded" without pinning down the actual scale. Revisited for a Grafana use case (filter one dashboard by tenant, see agents/runs/sandboxes together): research into this deployment's actual shape found tenant count is small — single digits to low tens per cluster, since kora is operator-managed, not self-service SaaS (tenants are onboarded via GitOps/CRD, `docs/adr/2026-03-31-tenant-registration-and-oidc-provider-binding.md:61`, `2026-03-31-orbit-kubernetes-operator.md:378-379`). That changes the cost-benefit: `tenant_id` is safe as a general-purpose label, not just a special case for one service.

The same research also found `AgentType` (an agent definition) is a bounded, admin-curated catalog, not a bulk-creatable resource: each one provisions a real Kubernetes StatefulSet (`docs/adr/2026-05-11-agent-execution-infrastructure.md:22`), is created through a guided admin builder UI (`docs/prd/agent-builder.md:88`), and has no bulk-creation path — structurally identical to the `role_pool` precedent already accepted in this ADR, not the unbounded free-text case (`tool` names from dynamically-registered MCP servers) that review caught twice. Low tens of agent types per tenant, times low tens of tenants, is still a small number.

**Revised policy:**
1. `tenant_id` is now a default-safe label on any metric below, not just inference-gateway's. Still excluded from the generic cross-cutting `orbit_<service>_http_requests_total`/`_http_request_duration_seconds` (unchanged from #408) and from `orbit_inference_gateway_request_duration_seconds` (would multiply with provider/model/status) — revisit those separately if wanted, not as part of this amendment.
2. `agent_id` (the `AgentType.ID`, stable across renames) is now a safe label on orbit-agents' run/sandbox metrics, under the same "bounded admin-curated catalog" reasoning as `role_pool`.
3. `agent_name` is **not** a label — a tenant can rename an agent, which would otherwise churn a metric's series identity for no operational reason. Instead, a new low-cardinality "info metric" (`orbit_agents_agent_info{agent_id,agent_name,tenant_id,framework}`, value always `1`) carries the current name, so Grafana resolves `agent_id` → `agent_name` for display via a label join (`* on (agent_id) group_left(agent_name) orbit_agents_agent_info`) instead of every series carrying a renamable string. This is the standard Prometheus pattern (`kube_pod_info` etc.) for attaching mutable metadata to a stable ID.
4. Everything else in the original Cardinality Policy is unchanged: run/session/principal IDs are still never label values, at any scope, for the same unbounded reason as before.

Policy-service's two tenant-aggregate gauges (`orbit_policyservice_grants`/`_roles`) are explicitly **not** revisited by this amendment — `policyservice.Stats()`'s per-tenant isolation (`policyservice/api/service.go:360-362`) is a deliberate boundary unrelated to cardinality, and this amendment doesn't touch it.

---

## Implementation

Binding cardinality policy (see PRD for the full statement, as amended 2026-10-20): enum-backed labels by default; `tenant_id` is now a default-safe label on any metric except the generic HTTP request counter/histogram and inference-gateway's request-duration histogram; `agent_id` is safe on orbit-agents' run/sandbox metrics (bounded admin-curated catalog, same precedent as `role_pool`); `agent_name` is never a label, only available via the `orbit_agents_agent_info` info metric; run/session/principal IDs and free-text names (e.g. MCP tool names) are never label values, at any scope; `framework` (bounded, `agents.AgentFramework`) is safe wherever an agent run is involved.

| Service | Metric | Type | Labels | Instrumentation point |
|---|---|---|---|---|
| identity | `orbit_identity_sessions_active{tenant_id}` | gauge (observable) | `tenant_id` | `SessionRepository.CountActiveSessions`, changed to `GROUP BY tenant_id` (was a single global count) |
| identity | `orbit_identity_delegations_issued_total{tenant_id}`/`_revoked_total{tenant_id}` | counter | `tenant_id` | `delegation_service.go:64` (`MintDelegation`) / `:176` (`RevokeDelegation`); `claims.Kora.TenantID` already in scope at both call sites |
| identity | `orbit_identity_auth_failures_total{reason,tenant_id}` | counter | `reason,tenant_id` | `service.go:568` (`ErrRefreshTokenNotFound`), `:578` (`ErrRefreshTokenRevoked`), `:583` (`ErrRefreshTokenExpired`), `:600` (`ErrTokenRevoked`), `:713`/`:720` (`ErrInvalidToken`); sentinels at `models.go:373-403`. `tenant_id` is `""` where a failure occurs before a tenant is resolved (e.g. a malformed token) — a real, bounded value, not an omission |
| orbit-agents | `orbit_agents_run_transitions_total{from_state,to_state,framework,tenant_id,agent_id}` | counter | `from_state,to_state,framework,tenant_id,agent_id` | `run_service.go:463` (`Transition`), `:527` (`TransitionRunState`); `framework`/`agent_id` resolved via `s.atRepo.GetAgentType(ctx, run.AgentTypeID)` (cached per run, see Negative consequences), `tenant_id` from `run.TenantID` |
| orbit-agents | `orbit_agents_runs_active{state,framework,tenant_id,agent_id}` | gauge (observable) | `state,framework,tenant_id,agent_id` | callback over run store, grouped by state/framework/tenant/agent type — supersedes the originally-floated, now-dropped `{state,tenant_id}` P1 variant |
| orbit-agents | `orbit_agents_run_duration_seconds{tenant_id,agent_id}` | histogram | `tenant_id,agent_id` | `CompletedAt - StartedAt`, same call site as the terminal-state branch in `Transition` (`run_service.go:503-516`). **Correction (PR #418 review):** `PostgresRepository.SetRunState` originally stamped `completed_at` only for `completed`/`failed`, so a run terminating in `budget_exhausted` had a nil `CompletedAt` and never reached this histogram. `budget_exhausted` now stamps `completed_at` too |
| orbit-agents | `orbit_agents_runs_reaped_total{outcome,tenant_id,agent_id}` | counter | `outcome,tenant_id,agent_id` | `run_service.go:1000` (`StartReaper`), `:1016` (`ReapStaleRuns`) |
| orbit-agents | `orbit_agents_sessions_active{tenant_id}` | gauge (observable) | `tenant_id` | **Correction (PR #418 review):** the `agents.Session` ("browser session") table has zero writers anywhere in the codebase — no `INSERT`/`UPDATE` against it, no repo method. Instrumented against `PubSub.ActiveSubscriptions()` instead (`pubsub.go`) — the actual, already-running connect/disconnect lifecycle for a browser's live SSE subscription to a run. `tenant_id` requires threading the run's tenant into `Subscribe`, which only takes a `runID` today |
| orbit-agents | `orbit_agents_session_duration_seconds{tenant_id}` | histogram | `tenant_id` | same correction; recorded in `PubSub.Subscribe`'s returned `cancel` closure (`pubsub.go`), guarded by a `sync.Once` so a second `cancel()` call can't double-record or double-close the channel |
| orbit-agents | `orbit_agents_sandbox_provision_duration_seconds{phase,tenant_id,agent_id}`/`_failures_total{phase,tenant_id,agent_id}` | histogram/counter | `phase,tenant_id,agent_id` | `session.go: ProvisionSandbox` (`SandboxPhase*` consts) |
| orbit-agents | `orbit_agents_guardrail_warnings_total{kind,tenant_id,agent_id}` | counter | `kind,tenant_id,agent_id` | `guardrails.go: loopGuard`, `GuardrailWarningPayload`. **Correction (PR #418 review):** the original `{kind,tool}` label set used `ToolUsePayload.Name` (an MCP/agent tool name) as `tool` — unbounded, since custom tools register dynamically. Dropped `tool`; `kind` is enum-backed |
| orbit-agents | `orbit_agents_agent_info{agent_id,agent_name,tenant_id,framework}` | gauge (observable, value always `1`) | `agent_id,agent_name,tenant_id,framework` | **New in the 2026-10-20 amendment.** `agent_name` is never a label on the metrics above — a rename would otherwise churn their series identity. This info metric (the standard Prometheus "info" pattern, e.g. `kube_pod_info`) carries the current name against the stable `agent_id`, so Grafana resolves it via `* on (agent_id) group_left(agent_name) orbit_agents_agent_info`. Callback over the agent-type store, one row per `AgentType` |
| session-manager | `orbit_session_manager_sessions_active{tenant_id}` | gauge (observable) | `tenant_id` | `sessionmanager/session.go:332` (`Registry.ActiveCount`), changed to group by tenant; meter already constructed in `cmd/session-manager/main.go` (`initMeter:65`, used by `mw.MetricsMiddleware` at `:152`) |
| policy-gateway | `orbit_policygw_checks_total{allow,resource_type,tenant_id}`/`_check_duration_seconds{allow,resource_type,tenant_id}` | counter/histogram | `allow,resource_type,tenant_id` | `policygw/service.go:26` (`Check`); `resourceType` param already a logged field `resource_type` at `:50`, `tenant_id` from `claims.Kora.TenantID`. **Correction (PR #418 review):** `resourceType` comes straight from `CheckRequest.Resource.Type`, caller-supplied and unvalidated — any authenticated caller could mint unbounded series. `policygw/metrics.go` now coarsens it against a fixed allow-list (`inference`, `platform`, `agents`, `triggers`, `trigger`, `mcp`, `connection`), mapping anything else to `"other"` via a plain in-memory map lookup — no new DB/cache dependency on the hot auth-check path |
| policy-gateway | `orbit_policygw_denies_total{reason,tenant_id}` | counter | `reason,tenant_id` | same call site, on `!result.Allow`; `policy.Result{Allow,Reason}` defined `platform/policy/evaluator.go:208-211`. One OPA deny rule (`authz_gateway.rego`) embeds a tenant-defined role name into `Reason` (`"role:<name>"`) — coarsened to the prefix before the colon (e.g. `"role"`), same cardinality discipline as the resource-type fix above |
| policy-service | `orbit_policyservice_grants{principal_type}`/`_roles{kind}` | gauge (observable) | `principal_type`/`kind` — **no `tenant_id`, by design, unaffected by this amendment** | new tenant-aggregate query (sum across all tenants) in `grantRepo`/`roleRepo`, distinct from the existing per-tenant `Stats()` path (`policyservice/api/service.go:334-399`) which stays as-is for its own endpoint. `policyservice.Stats()`'s per-tenant isolation (`:360-362`) is a deliberate boundary this amendment does not touch |
| policy-service | `orbit_policyservice_bundle_stale_total` | gauge (observable) | — (no `tenant_id`, same reason as above) | aggregate count of tenants whose bundle `Status == "stale"`, same new aggregate query; **not** per-tenant (`tenantBundleStatus.TenantID` stays internal to `Stats()`'s existing per-tenant response) |
| inference-gateway | `orbit_inference_gateway_usage_total{status,provider,model,tenant_id}` | counter | `status,provider,model,tenant_id` | `service.go: writeUsage` (`:725-738`), fed by `InferenceUsageRecord` (`models.go:145-192`): `Status` (169), `Provider` (153, nil-safe default `"unknown"`), `ModelID` (154), `TenantID` (148) |
| inference-gateway | `orbit_inference_gateway_tokens_total{provider,model,kind,tenant_id}` | counter | `provider,model,kind,tenant_id` | same call site; `kind ∈ {prompt,completion,cache_read,cache_creation}` from `PromptTokens`/`CompletionTokens`/`CacheReadInputTokens`/`CacheCreationInputTokens` (models.go:158-162) — `TotalTokens` not separately emitted (would double-count) |
| inference-gateway | `orbit_inference_gateway_cost_usd_total{provider,model,tenant_id}` | counter | `provider,model,tenant_id` | same call site, `EstimatedCostUSD` (models.go:163) |
| inference-gateway | `orbit_inference_gateway_request_duration_seconds{provider,model,status}` | histogram | `provider,model,status` (no `tenant_id` — unchanged by this amendment, would multiply with provider/model/status on the one histogram in the catalog; revisit separately if wanted) | same call site, `LatencyMS` (models.go:173) |
| inference-gateway | `orbit_inference_gateway_budget_utilization_ratio{scope,window,tenant_id,role_pool}` | gauge (set at call time, not observable) | `scope ∈ {tenant, role_pool} only`, `window`, `tenant_id` (always), `role_pool` (the OIDC role name, `BudgetCeiling.ID`, only when `scope == "role_pool"`; empty for `scope == "tenant"`) | computed as `SpendUSD/LimitUSD` from each `policy.BudgetCeiling` in `BudgetCheckResult.Ceilings` (`evaluator.go:140-149,255`) wherever that result is consumed; ceilings with `Scope ∈ {"run","principal"}` are skipped — unbounded cardinality. **Correction (PR #418 review):** the original `{scope,window}` label set let every tenant's and every role pool's ceiling collide onto the same series within a `(scope, window)`, so the exported value was whichever request recorded last, not meaningful utilization |
| orbit-triggers | *(existing instruments)* | — | — | `metrics.go:68-125` already Prometheus-visible via PR #408's reader attach; no changes |
| orbit-triggers | `orbit_triggers_delivery_outcomes_total{outcome,tenant_id}` | counter | `outcome,tenant_id` | `outcomes.go: derive` → `DeriveDeliveryStatus` (`postgres.go`); enum `models.go:50-55`, `tenant_id` from `Delivery.TenantID`. **Correction (PR #418 review):** the original `UPDATE ... RETURNING d.status` returned a row on every call regardless of whether the computed status actually changed, so one multi-trigger delivery double-counted (e.g. `dispatched` once per completed trigger). Added a change-detecting `WHERE d.status IS DISTINCT FROM <computed>` guard so `RETURNING` only yields a row on a genuine transition. `duplicate`/`missed` bypass `derive()` entirely (`recordDuplicate`/`RecordMissedTick` set those statuses directly) — recorded at their own successful-insertion points instead |
| orbit-triggers | `orbit_triggers_admission_outcomes_total{outcome,tenant_id}` | counter | `outcome,tenant_id` | `admission.go:167-234` (`Admit`), enum `admission.go:49-63`, `tenant_id` from `Trigger.TenantID`. **Correction (PR #418 review):** `Admit`'s own return value only ever described the new fire's outcome (never `AdmitSuperseded`, since a successful replace returns `AdmitEnqueued` for the new item) — the displaced/evicted fire's own outcome was never counted. Each displaced fire in the `OverflowReplace` branch now gets its own `recordAdmissionOutcome(ctx, AdmitSuperseded)` call, independent of what `Admit` returns |
| egress-gateway | `orbit_egress_gateway_credentials_minted_total{adapter,action,tenant_id}`/`_mint_duration_seconds{adapter,action,tenant_id}` | counter/histogram | `adapter,action,tenant_id` | `egressgw/service.go` (`connector.Mint` call site); `adapter` from `Adapter.Type()` (`credentials/adapter.go:42`, values `models.go:27-33`), `action` from the `action` variable `evaluatePolicy` resolves — **correction (PR #418 review):** originally passed the pre-authorization `cls.Action`, which can differ from what's actually minted when policy grants an alternate action. `tenant_id` from `claims.Kora.TenantID` |
| egress-gateway | `orbit_egress_gateway_mint_failures_total{reason,tenant_id}` | counter | `reason,tenant_id` | sentinels `credentials/models.go:83-106` (`ErrConnectionNotFound`, `ErrUnsupportedAdapter`, `ErrInvalidConfig`, `ErrCredentialScopeRejected`). **Correction (PR #418 review):** `ErrConnectionNotFound` and `ErrInvalidConfig` return from `resolveConnection`/`adapter.Open`, both before `connector.Mint` is ever called — the original single-call-site instrumentation made those two reasons unreachable. Added mint-failure recording at those two earlier call sites too (using `"unknown"` for `adapter`/`action` where not yet resolved at that point) so all four reasons are observable |
| tool-gateway | `orbit_tool_gateway_invocations_total{server,decision,tenant_id}`/`_invocation_duration_seconds{server,decision,tenant_id}` | counter/histogram | `server,decision ∈ {allow,deny,error},tenant_id` | `mcp_controller.go:236` (`handleToolsCall`); `server` already span-tagged (`:251-297`), `decision` already set via `mcp.decision` span attribute (`:264,269,284`), `tenant_id` from the caller's claims. **Correction (PR #418 review):** the original label set also included `tool`, taken from the caller-supplied JSON-RPC `params.name` before any catalog-membership check — unbounded, since a caller can submit any name. The proxy binary (where `handleToolsCall` runs) never constructs an `Introspector`, so there's no cheap, bounded tool catalog to validate against on this hot path; dropped `tool` entirely. Tool name stays available on the span (`mcp.tool`) and in allow/deny log lines |
| tool-gateway | `orbit_tool_gateway_servers{state}` | gauge (observable) | `state ∈ pending/ok/degraded` (no `tenant_id` — servers are cluster-wide, not tenant-scoped) | `introspector.go:240` (`Snapshot`); `ServerState` enum `models.go:48-59` |

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

PR #418's review surfaced three genuine unbounded-cardinality violations (guardrail `tool`, policy-gateway `resource_type`, tool-gateway `tool`) that this ADR's own binding policy should have caught at design time but didn't, because the design pass didn't check whether each label's value set was actually code-defined versus caller-supplied. All three are corrected in the Implementation table above. Treat "is this label's value set enumerable from a Go `const`/enum, or does it come from a request field?" as a standing design-review question for every future metric, not just something to catch in review.

On the 2026-10-20 amendment's cardinality: worst case is `orbit_agents_run_transitions_total`, with five label dimensions (`from_state`×`to_state`×`framework`×`tenant_id`×`agent_id`). At confirmed scale (low tens of tenants, low tens of agent types per tenant, ~8 run states, 3 frameworks) that's on the order of 8×8×3×20×20 ≈ 77,000 possible combinations in the worst theoretical case, but the real series count is bounded by *actual* (tenant, agent_type) pairs in use, not the cross product — each tenant only has its own handful of agent types, so the realistic count is closer to (tenants × agent-types-per-tenant × states-actually-reached × frameworks-in-use), typically low thousands. Acceptable for a single Prometheus instance; revisit if either bound (tenant count or agent types per tenant) grows by an order of magnitude.

**Last Updated:** 2026-10-20
