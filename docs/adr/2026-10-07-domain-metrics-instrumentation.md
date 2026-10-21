# Domain Metrics Instrumentation per Backend Service

**Status:** Accepted (Amended 2026-10-20)
**Date:** 2026-10-07
**Deciders:** Platform Engineering
**Technical Story:** [Domain Metrics Instrumentation PRD](../prd/domain-metrics-instrumentation.md), stacked on [Prometheus Metrics Endpoints](2026-10-06-prometheus-metrics-endpoints.md)

---

## Context

### Background

The Prometheus Metrics Endpoints ADR shipped a `/metrics` port per service and a uniform HTTP request counter/histogram, but deliberately deferred instrumenting any service's business logic. This ADR is that follow-up, covering identity, orbit-agents, session-manager, policy-gateway, policy-service, inference-gateway, orbit-triggers, egress-gateway, and tool-gateway.

### Problem Statement

Each service's feature package already models the domain events an operator cares about (run state, policy decisions, inference usage/cost, delivery outcomes, credential minting, tool-call decisions) in Go types and enums. None of them are exported as metrics. The work is mechanical per call site, but two cross-cutting design questions recur enough to need a single, binding answer instead of nine ad hoc ones: **which labels are safe**, and **what to do when a service's existing query boundary (e.g. tenant isolation) doesn't line up with what a global gauge needs**.

### Goals

- Instrument the metric catalog below.
- Establish a binding cardinality policy so safety doesn't need re-litigating metric-by-metric.
- Resolve `policyservice.Stats()`'s per-tenant isolation and budget-ceiling scopes that include unbounded IDs.

### Non-Goals

- Any change to `internal/platform/observability/` or the Helm chart.
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
- `run_id`/`principal_id` are effectively unbounded (a run is single-use; a growing tenant has unbounded principals) — explodes series count regardless of tenant scale.

### Option 2: No tenant/identity labels anywhere (safest, least useful)

**Description:** Keep every label enum-backed, never add any tenant or identity dimension.

**Cons:**
- Defeats a real, named use case (per-tenant cost/usage dashboards, operational dashboards filterable by tenant) that DB queries already support per-row — Prometheus just can't show it live.

### Option 3: Selective, scale-aware labeling under a binding cardinality policy (chosen)

**Description:** Default to enum-backed labels; allow `tenant_id`/`agent_id` only where the underlying value set is confirmed bounded (see Cardinality Policy); never allow run/principal/session IDs or free-text names as label values anywhere.

**Cons:**
- Requires a documented, binding policy (this ADR) rather than a single blanket rule.
- Puts tenant-identifying values on an unauthenticated endpoint — see **Known Tension with the Unauthenticated-Endpoint Rule** below.

---

## Decision

**Chosen Option:** Option 3 — selective, scale-aware labeling under a binding cardinality policy.

### Rationale

Tenant count and `AgentType` count are both small and bounded in this platform's actual deployment model (kora is operator-managed, not self-service SaaS — tenants are onboarded one at a time via GitOps/CRD, and an `AgentType` provisions a real Kubernetes StatefulSet through a guided admin builder UI, with no bulk-creation path). That makes `tenant_id` and `agent_id` safe as general-purpose labels under the same reasoning that already applies to `role_pool`: a small, slowly-changing, enumerable set, not a request-driven one. Run IDs and principal IDs don't share that property — a run is single-use, and a growing tenant's principal set is unbounded from Prometheus's perspective — so those stay excluded at every scope.

---

## Cardinality Policy (binding)

1. **Default:** every label value comes from a `const`/enum type (status, state, reason, kind, decision) — never a raw user-supplied string, unless the value set is itself a small, bounded, admin-curated catalog (2 and 4 below). Treat "is this label's value set enumerable from a Go `const`/enum, or does it come from a request field?" as a standing design-review question for every new metric.
2. **`tenant_id`** is a default-safe label, given this platform's confirmed scale (single digits to low tens of tenants per cluster). Excluded from the generic cross-cutting `orbit_<service>_http_requests_total`/`_http_request_duration_seconds`, and from `orbit_inference_gateway_request_duration_seconds` (would multiply with provider/model/status), and absent from `orbit_policyservice_grants`/`_roles`/`_bundles_stale`, where `policyservice.Stats()`'s per-tenant isolation is a boundary unrelated to cardinality.
3. **Run IDs, session IDs, and principal IDs never become label values**, at any scope — genuinely unbounded. A breakdown by one of these belongs in traces/logs, or a periodic DB-aggregated export, never a Prometheus label.
4. **`agent_id`** is a bounded label on orbit-agents' run/sandbox metrics, under the same admin-curated-catalog reasoning as `role_pool`. **`agent_name`** is never a label (a rename would churn series identity) — it's carried by `orbit_agents_agent_info`, the standard Prometheus "info metric" pattern (`kube_pod_info` etc.), which Grafana joins on `agent_id`.
5. **`framework`** (bounded, `agents.AgentFramework`: `claude-code`/`codex`/`opencode`) is safe wherever an agent run is involved.
6. Every duration histogram uses an explicit bucket preset from `internal/platform/metrics/buckets` (`Fast` for sub-millisecond work like a warm policy check, `Default` for general seconds-scale work, `Long` for run/session duration spanning seconds to hours) instead of the OTel SDK's millisecond-tuned defaults, which would otherwise collapse nearly every real sample into one bucket.

### Known Tension with the Unauthenticated-Endpoint Rule

The Prometheus Metrics Endpoints ADR established a stricter rule: no tenant, principal, run, or trigger ID ever becomes a label, full stop, because `/metrics` is unauthenticated by design — a label is readable by anything that can reach the port. This ADR's `tenant_id`/`agent_id`/`role_pool` labels are a deliberate, informed exception to that rule, not a reading of it that happens to permit them. The distinction that rule drew (bounded vs. unbounded cardinality) is a different axis from the one it was actually protecting (disclosure on an unauthenticated port) — a human-readable value doesn't change whether it's exposed, only how legible the exposure is.

This platform's tenants are not competitively sensitive to each other in a way that makes enumeration itself harmful, and the set of people who can reach the metrics port is already the same set who administers the cluster — accepted as a deliberate scope decision for this ADR, not a resolution of the underlying rule. If the deployment model changes (self-service tenant signup, or a scraper reachable from outside the operator's own network), revisit this exception before it does.

---

## Consequences

### Positive

- Operators get real-time dashboards/alerting for run health, policy decisions, inference cost, delivery outcomes, credential minting, and tool-call decisions, filterable by tenant and by agent.
- The cardinality policy is written down once, applicable to every future metric addition without re-deriving it.

### Negative

- `orbit_agents_run_transitions_total`'s `framework`/`agent_id` labels cost an extra `GetAgentType` DB lookup — `Transition` runs on every state change, including the idle↔active turn boundary, one of the most frequent transitions a run makes, not just terminal ones. Mitigated with an in-process `sync.Map[runID]runMetricLabels` cache on `RunService`: a run's framework/tenant/agent type never change after creation, so the lookup runs at most once per run's lifetime instead of once per transition, with the entry evicted on every path that can end a run (`Transition`'s own terminal branch, the reaper, a failed start, explicit termination). A residual leak remains for a run cached on one replica but terminated on another — accepted as bounded by run volume, not unbounded time, rather than built out into an LRU/TTL/shared store.
- `orbit_policyservice_grants`/`_roles`/`_bundles_stale` ship as tenant-aggregate gauges only, because `policyservice.Stats()` is deliberately scoped to one tenant per call to avoid leaking cross-tenant counts through that API. A tenant-aggregate gauge needed a new, lightweight "sum across all tenants" query path in `grantRepo`/`roleRepo` that didn't exist before — the smallest delta that respects the existing isolation boundary.
- `orbit_inference_gateway_budget_utilization_ratio` is computed at each budget-check call site, not via an async observable-gauge callback, because no cached registry of "all current ceilings" exists to poll — the spend/limit pairing lives in a per-request result. The gauge only updates when a budget check actually runs, acceptable since one runs on every inference request.
- Every observable-gauge callback that touches Postgres (orbit-agents' run-count query, identity's active-sessions gauge, policy-service's `MetricsSnapshot`) now runs under an explicit `context.WithTimeout`, since the OTel exporter collects with a bare `context.TODO()` with no deadline — without one, a slow database would hang an entire scrape.

### Neutral

- `egress-gateway`'s credential-mint metrics are instrumented at the `egressgw` service-binary call site, not inside the shared `internal/credentials` package where `Connector.Mint` is actually defined — keeps the metric owned by the binary that emits it, rather than making a shared package depend on metrics for a label set only one caller uses.
- A standardized `internal/platform/metrics.RegisterDomain[T]` helper and consistent `Close()` wiring replaced nine services' worth of ad hoc, inconsistently-fatal metrics-registration boilerplate: every service now logs and continues uninstrumented on a registration failure (a service refusing to start because a gauge failed to register has turned its own observability into an outage), and every domain `Metrics` type's observable-gauge callback is unregistered on shutdown.

---

## Implementation

| Service | Metric | Type | Labels | Instrumentation point |
|---|---|---|---|---|
| identity | `orbit_identity_sessions_active` | gauge (observable) | `tenant_id` | `SessionRepository.CountActiveSessions`, grouped by tenant |
| identity | `orbit_identity_delegations_issued_total`/`_revoked_total` | counter | `tenant_id` | `delegation_service.go` (`MintDelegation`/`RevokeDelegation`), `claims.Kora.TenantID` |
| identity | `orbit_identity_auth_failures_total` | counter | `reason,tenant_id` | sentinel-error return sites in `service.go` (`ErrRefreshTokenNotFound`, `ErrRefreshTokenRevoked`, `ErrRefreshTokenExpired`, `ErrTokenRevoked`, `ErrInvalidToken`); `tenant_id` is `""` where a failure occurs before a tenant is resolved (e.g. a malformed token) — a real, bounded value, not an omission |
| orbit-agents | `orbit_agents_run_transitions_total` | counter | `from_state,to_state,framework,tenant_id,agent_id` | every path that writes a run's state: `Transition`/`TransitionRunState`, `AssignPod`, a failed-start cleanup, and the reaper |
| orbit-agents | `orbit_agents_runs_active` | gauge (observable) | `state,framework,tenant_id,agent_id` | callback over the run store, grouped by state/framework/tenant/agent type |
| orbit-agents | `orbit_agents_run_duration_seconds` | histogram (`buckets.Long`) | `tenant_id,agent_id` | `CompletedAt - StartedAt` on every terminal write, including a run ending via the reaper (previously missing, which skewed the histogram toward runs that ended cleanly through `Transition`) |
| orbit-agents | `orbit_agents_runs_reaped_total` | counter | `outcome,tenant_id,agent_id` | the reaper's per-run cleanup |
| orbit-agents | `orbit_agents_sessions_active` | gauge (observable) | `tenant_id` | `PubSub.ActiveSubscriptions()` — the browser `Session` store this was originally specified against has no writers anywhere in the codebase, so this tracks a browser's live SSE subscription to a run instead, the closest real analog that executes today |
| orbit-agents | `orbit_agents_session_duration_seconds` | histogram (`buckets.Long`) | `tenant_id` | `PubSub.Subscribe`'s returned `cancel` closure, guarded by a `sync.Once` so a repeat call can't double-record or double-close the channel |
| orbit-agents | `orbit_agents_sandbox_provision_duration_seconds` | histogram (`buckets.Default`) | `phase,tenant_id,agent_id` | `session.go: ProvisionSandbox` (`SandboxPhase*` consts), every phase including the two early-return failure paths (session not found, a malformed run ID) — those two record the metric directly without pushing a durable event into the run's stream, since there's no real session/run to attach one to |
| orbit-agents | `orbit_agents_sandbox_provision_failures_total` | counter | `tenant_id,agent_id` | same call sites; no `phase` label — this counter only ever increments on the failed phase, so the label carried no information |
| orbit-agents | `orbit_agents_guardrail_warnings_total` | counter | `kind,tenant_id,agent_id` | `guardrails.go: loopGuard` — no tool-name label; an agent/MCP tool name is caller-defined and unbounded |
| orbit-agents | `orbit_agents_agent_info` | gauge (observable, value always `1`) | `agent_id,agent_name,tenant_id,framework` | callback over the agent-type store, one row per `AgentType` — see Cardinality Policy item 4 |
| session-manager | `orbit_session_manager_sessions_active` | gauge (observable) | `tenant_id` | `sessionmanager.Registry.ActiveCount`-equivalent, grouped by tenant |
| policy-gateway | `orbit_policygw_checks_total` / `_check_duration_seconds` | counter / histogram (`buckets.Fast`) | `allow ∈ {"true","false","error"},resource_type,tenant_id` | `policygw/service.go: Check`; `resource_type` is coarsened against a fixed allow-list (`inference`, `platform`, `agents`, `triggers`, `trigger`, `mcp`, `connection`, `egress`) with anything else mapped to `"other"` — `CheckRequest.Resource.Type` is caller-supplied and otherwise unbounded. `allow="error"` covers an evaluator failure (a broken bundle, an OPA error), previously invisible on this counter entirely |
| policy-gateway | `orbit_policygw_denies_total` | counter | `reason,tenant_id` | same call site, on a policy refusal; `reason` is bucketed to the prefix before its first colon (e.g. a `role:<tenant-defined-name>` OPA reason becomes `role`) — this alone bounds the label without also maintaining a parallel hardcoded list of known reason strings that would drift the first time a new OPA rule is added |
| policy-service | `orbit_policyservice_grants` / `_roles` | gauge (observable) | `principal_type` / `kind` — no `tenant_id`, by design | a tenant-aggregate query in `grantRepo`/`roleRepo`, distinct from the existing per-tenant `Stats()` path, which is unchanged |
| policy-service | `orbit_policyservice_bundles_stale` | gauge (observable) | — no `tenant_id`, same reason | aggregate count of tenants whose bundle is stale; no `_total` suffix — it's a gauge, not a counter |
| inference-gateway | `orbit_inference_gateway_usage_total` | counter | `status,provider,model,tenant_id` | `service.go: writeUsage`, fed by `InferenceUsageRecord`; recorded only after the record persists successfully, not before — otherwise an exported total could run ahead of the system of record on a failed write |
| inference-gateway | `orbit_inference_gateway_tokens_total` | counter | `provider,model,kind,tenant_id` | same call site; `kind ∈ {prompt,completion,cache_read,cache_creation}` — the aggregate token count is never separately emitted, which would double-count |
| inference-gateway | `orbit_inference_gateway_cost_usd_total` | counter | `provider,model,tenant_id` | same call site; unit declared as an OTel annotation (`{USD}`), which the Prometheus exporter drops from the rendered name, rather than a literal unit string it would otherwise append |
| inference-gateway | `orbit_inference_gateway_request_duration_seconds` | histogram (`buckets.Default`) | `provider,model,status` — no `tenant_id`, would multiply with the other three | same call site |
| inference-gateway | `orbit_inference_gateway_budget_utilization_ratio` | gauge (set synchronously at call time, not observable) | `scope ∈ {tenant, role_pool} only`,`window`,`tenant_id` (always), `role_pool` (only when `scope == "role_pool"`) | `SpendUSD/LimitUSD` from each budget ceiling consumed at a budget check; ceilings scoped to `run` or `principal` are skipped entirely — unbounded cardinality, and the only two scopes this ADR's policy never permits regardless of label name |
| orbit-triggers | *(pre-existing instruments: auto-pauses, purged payloads, scanned triggers, tick duration, queue depth/age, purge freshness)* | — | — | `tick_duration` now uses `buckets.Default`; otherwise unchanged |
| orbit-triggers | `orbit_triggers_delivery_outcomes_total` | counter | `outcome,tenant_id` | recorded exactly once per delivery, on the first derive that finds nothing outstanding in the pending queue, tracked by a durable `trigger_deliveries.outcome_counted` flag rather than inferred from a status change (the dispatch paths pre-write the status before deriving, so a change-based guard never fires for them). A delivery with several triggers can pass through more than one intermediate status before it's done, and only the final one counts. `duplicate`/`missed` bypass this derivation entirely and are recorded directly at their own successful-insertion points |
| orbit-triggers | `orbit_triggers_admission_outcomes_total` | counter | `outcome ∈ {enqueued,superseded,dropped,paused,error},tenant_id` | `admission.go: Admit`. `error` is a genuine database/infrastructure failure, distinct from `dropped` (a legitimate ceiling or backlog-full refusal) — conflating the two would make a database outage read as normal overflow handling. Each fire displaced by a replace, trimmed from a full backlog, or evicted by `Drain` (a trigger being deleted/disabled) gets its own recorded outcome at the point it's evicted, independent of what `Admit` returns for the fire actually being admitted |
| egress-gateway | `orbit_egress_gateway_decisions_total` | counter | `outcome ∈ {allow,deny,passthrough,error},adapter,tenant_id` | `Broker.Authorize`'s full outcome taxonomy: a real credential minted, a policy refusal, falling through to the anonymous/passthrough adapter (no registered connection — not a failure, and not a mint), or a genuine problem upstream of classification (an unsupported adapter, a config that can't be opened) |
| egress-gateway | `orbit_egress_gateway_credentials_minted_total` / `_mint_duration_seconds` | counter / histogram (`buckets.Default`) | `adapter,action,tenant_id` | only around the real `connector.Mint` call — `action` is the action `evaluatePolicy` actually resolves and sends to `Mint`, not the pre-authorization classification, which can differ when policy grants an alternate action. The passthrough path does not increment this; it's a `decisions_total` outcome, not a mint |
| egress-gateway | `orbit_egress_gateway_mint_failures_total` | counter | `reason,tenant_id` | only on a genuine failure of the real `Mint` call itself (e.g. the upstream rejecting the requested scope) — failures upstream of classification (an unresolvable connection, an unsupported adapter) are `decisions_total{outcome="error"}` instead, never a zero-duration mint sample that would otherwise drag the real mint-duration histogram toward zero |
| tool-gateway | `orbit_tool_gateway_invocations_total` / `_invocation_duration_seconds` | counter / histogram (`buckets.Default`) | `server,decision ∈ {allow,deny,error},tenant_id` | `mcp_controller.go: handleToolsCall`; the timer spans the full request including the upstream MCP call for an allowed invocation, not just the policy decision, so the duration reflects what a caller actually waited for. The two early-return paths (malformed params, a missing tool name) record as `decision="error"` instead of being invisible to this metric. No tool-name label — a tool name is caller-supplied JSON-RPC input with no catalog available on this hot path to bound it against |
| tool-gateway | `orbit_tool_gateway_servers` | gauge (observable) | `state ∈ {pending,ok,degraded}` — no `tenant_id`, servers are cluster-wide | `introspector.go: Snapshot` |

A precise "active users" metric remains a known gap (no `last_activity_at` column).

---

## Validation

### Success Metrics

- `curl :9464/metrics` on each service shows the new series after driving the corresponding event.
- `make check` and `make test-postgres` green.
- A manual review pass confirms no label in the table above resolves to a raw, unbounded value at runtime.

### Monitoring

- `make redeploy` + `kubectl port-forward` + `curl` smoke test, repeated per service in scope.

---

## Related Decisions

- [Prometheus Metrics Endpoints](2026-10-06-prometheus-metrics-endpoints.md) — the plumbing this ADR instruments against, and the source of the unauthenticated-endpoint rule this ADR partially departs from (see Known Tension, above).
- [Rendered Service Config Files](2026-10-06-rendered-service-config-files.md) — unrelated to this change.

---

## Notes

Cardinality math on the amendment's worst case, `orbit_agents_run_transitions_total` (five label dimensions: `from_state`×`to_state`×`framework`×`tenant_id`×`agent_id`): at confirmed scale (low tens of tenants, low tens of agent types per tenant, ~8 run states, 3 frameworks), the real series count is bounded by *actual* (tenant, agent_type) pairs in use, not the full cross product — each tenant only has its own handful of agent types, so the realistic count is in the low thousands. Acceptable for a single Prometheus instance; revisit if either bound (tenant count or agent types per tenant) grows by an order of magnitude.

**Last Updated:** 2026-10-20
