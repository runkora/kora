# Domain Metrics Instrumentation per Backend Service - Product Requirements Document

**Status:** Approved
**Created:** 2026-10-07
**Last Updated:** 2026-10-20
**Owner:** Platform Engineering
**Stakeholders:** Platform operators, SRE, finance/billing stakeholders (per-tenant cost visibility)

---

## Executive Summary

[Prometheus Metrics Endpoints](prometheus-metrics-endpoints.md) shipped the plumbing — a `/metrics` port per service and a uniform HTTP request counter/histogram — but explicitly deferred any instrumentation of business logic. This PRD is that follow-up: it instruments the domain events each service's own feature packages already model (run state transitions, policy decisions, inference usage and cost, delivery outcomes, credential minting) as Prometheus-visible OTel counters, histograms, and observable gauges, reusing the exact instrument/meter plumbing the prior PRD built.

---

## Problem Statement

### Current State

Every rolled-out service exposes `/metrics`, but the only series present are the generic `orbit_<service>_http_requests_total`/`_http_request_duration_seconds`. None of the domain-specific questions an operator actually asks — how many agent runs are active, is the policy engine denying more than usual, what's inference spend by tenant/provider, are deliveries failing — have a metric today. Each answer currently requires a direct SQL query or a log search.

### User Pain Points

- No real-time visibility into agent run/session counts, state transitions, or sandbox provisioning health (`docs/prd/agent-control-plane.md`'s Observability NFR is still unmet beyond request rate).
- No real-time per-tenant inference cost/usage/budget visibility (`docs/prd/llm-cost-tracking.md`'s operational visibility is DB-query-only today).
- No visibility into policy-gateway allow/deny rates or policy-service bundle staleness without hitting `GET /policy/stats` by hand, per tenant.
- No visibility into trigger delivery/admission outcomes beyond the existing queue-depth gauges.
- No visibility into credential-minting failures or MCP tool-call allow/deny/error rates.

### Business Impact

- Operators can't build alerting or dashboards for the platform's actual failure modes (denied policy checks spiking, sandbox provisioning failing, budget ceilings approaching) without first writing bespoke SQL-polling exporters.
- Per-tenant cost/usage visibility matters for both the platform's own ops and for a design partner's own observability needs once this runs outside a single tenant.

---

## Goals and Objectives

### Primary Goals

1. Instrument the domain events already identified in the companion ADR's (`2026-10-07-domain-metrics-instrumentation.md`) Implementation section, across all four rollout tiers, in one stacked PR on top of #408.
2. Keep every new label low-cardinality by construction: enum-backed values only, never a raw user-supplied string, except `tenant_id` (default-safe at this platform's confirmed scale) and `agent_id` (a bounded, admin-curated catalog — see the cardinality policy below) — and never a per-run, per-session, or per-principal ID at any scope.
3. Reuse the exact `meter`/`MetricsMiddleware` wiring the prior PRD built; no new ports, no new config surface, no chart changes.

### Success Metrics

- `curl :9464/metrics` on every service in scope shows the new domain series after driving the corresponding business event (a run transition, a policy denial, an inference request, etc.).
- `make check` and `make test-postgres` green with no new lint exceptions.
- No metric in this PRD has unbounded cardinality: every label's value set is enumerable from a `const`/enum type, or is `tenant_id`/`agent_id` — both confirmed bounded at this platform's actual deployment scale (see Cardinality Policy).

### Non-Goals

- `orbit-mcp`: excluded. It is a pure passthrough tool catalog with no independent allow/deny or health-state logic of its own (`backend/internal/orbitmcp/orbitmcp.go:7-9`: "This server holds no authority"). Every tool call it forwards is already mediated and labeled by `tool-gateway`'s `handleToolsCall`.
- A precise "active users" metric — still blocked on the missing `last_activity_at` column, unchanged from the prior PRD.
- Per-tenant or per-principal breakdowns of `orbit_policyservice_grants`/`_roles` — `policyservice.Stats()` is deliberately scoped to one tenant per call to avoid leaking cross-tenant counts through the query path (`policyservice/api/service.go:360-362`); this PRD ships tenant-aggregate gauges only (see Implementation). Per-tenant breakdowns are a distinct follow-up requiring a new all-tenant-safe aggregate query.
- Per-run or per-principal budget-utilization series. `policy.BudgetCeiling.Scope` includes `"run"` and `"principal"`, both unbounded/high-cardinality (a run ID is single-use; a tenant can have many principals) — this PRD only emits `orbit_inference_gateway_budget_utilization_ratio` for `scope ∈ {tenant, role_pool}`, which are bounded, slowly-changing sets.
- Any change to `charts/orbit/`, `internal/platform/metrics/`, or `internal/platform/observability/` — all plumbing is already in place from PR #408.

---

## User Stories

**As a** platform operator,
**I want** `orbit-agents` to expose run/session state and sandbox provisioning health as metrics,
**So that** I can alert on stuck runs, reaper activity, or sandbox provisioning failures without querying Postgres directly.

**Acceptance Criteria:**
- [ ] `orbit_agents_run_transitions_total{from_state,to_state,framework,tenant_id,agent_id}` increments on every `Transition`/`TransitionRunState` call.
- [ ] `orbit_agents_runs_active{state,framework,tenant_id,agent_id}` and `orbit_agents_sessions_active{tenant_id}` are live observable gauges.
- [ ] `orbit_agents_sandbox_provision_duration_seconds{phase,tenant_id,agent_id}`/`_failures_total{phase,tenant_id,agent_id}` record on every `ProvisionSandbox` phase.
- [ ] `orbit_agents_agent_info{agent_id,agent_name,tenant_id,framework}` lets Grafana resolve a name for any `agent_id` on the metrics above without that name ever being a label itself.

**As a** finance/billing stakeholder,
**I want** per-tenant inference usage, token, and cost counters plus a bounded budget-utilization gauge,
**So that** I can build a real-time per-tenant spend dashboard without waiting on batch DB rollups.

**Acceptance Criteria:**
- [ ] `orbit_inference_gateway_{usage,tokens,cost_usd}_total` all carry `tenant_id` alongside provider/model/status/kind labels.
- [ ] `orbit_inference_gateway_budget_utilization_ratio{scope,window}` is emitted only for `scope ∈ {tenant, role_pool}`, never `run` or `principal`.

**As a** platform operator,
**I want** policy-gateway/policy-service/triggers/egress-gateway/tool-gateway domain outcomes as metrics,
**So that** I can alert on anomalous deny rates, bundle staleness, delivery failures, credential-mint failures, or MCP tool-call errors.

**Acceptance Criteria:**
- [ ] `orbit_policygw_checks_total{allow,resource_type,tenant_id}`/`_check_duration_seconds`/`_denies_total{reason,tenant_id}` present.
- [ ] `orbit_policyservice_grants{principal_type}`/`_roles{kind}` are tenant-aggregate gauges (no `tenant_id` label — see Non-Goals; unaffected by the 2026-10-20 amendment).
- [ ] `orbit_triggers_delivery_outcomes_total{outcome,tenant_id}`/`_admission_outcomes_total{outcome,tenant_id}` present, on the existing `orbit-triggers` meter.
- [ ] `orbit_egress_gateway_credentials_minted_total{adapter,action,tenant_id}`/`_mint_duration_seconds`/`_mint_failures_total{reason,tenant_id}` present, instrumented at the `egressgw/service.go` call site (the `Connector.Mint` implementation itself lives in the shared `internal/credentials` package and is not modified).
- [ ] `orbit_tool_gateway_invocations_total{server,decision,tenant_id}`/`_invocation_duration_seconds` and `orbit_tool_gateway_servers{state}` present. (`tool` is not a label — see Cardinality Policy correction in the ADR: the proxy binary has no cheap, bounded tool catalog to validate against on the hot path.)
- [ ] `orbit_session_manager_sessions_active{tenant_id}` present (meter already wired in `cmd/session-manager/main.go` from PR #408; this just adds the observable callback).

---

## Proposed Solution

### High-Level Approach

No new packages, ports, or chart changes. Each service's `main.go` already constructs a `metric.Meter` (from PR #408); this work adds `Int64Counter`/`Float64Histogram`/observable-gauge instruments next to each identified business-logic call site, following the exact label-cardinality discipline below.

### Cardinality Policy (binding for this PRD and future metric additions; revised 2026-10-20)

1. **Default:** every label value must come from a `const`/enum type (status, state, reason, kind, decision) — never a raw user-supplied string, ID, or name, unless that value set is itself a small, bounded, admin-curated catalog (see 2 and 4 below).
2. **Tenant ID is a default-safe label**, since confirmed deployment scale is single digits to low tens of tenants per cluster (operator-managed, not self-service SaaS — see the ADR's 2026-10-20 amendment). Add it to any metric unless explicitly excluded below. Still excluded from the generic cross-cutting `orbit_<service>_http_requests_total`/`_http_request_duration_seconds` (unchanged from #408) and from `orbit_inference_gateway_request_duration_seconds` (already multiplies with provider/model/status) — and deliberately absent from `orbit_policyservice_grants`/`_roles`/`_bundle_stale_total`, where `policyservice.Stats()`'s per-tenant isolation is a boundary unrelated to cardinality.
3. **Run IDs, session IDs, and principal IDs never become label values**, at any scope — these are genuinely unbounded (single-use or per-user growth). Where a breakdown by one of these would be valuable, it belongs in traces/logs (unbounded cardinality is free there) or a periodic DB-aggregated export — not a Prometheus label.
4. **Agent ID is a bounded label on `orbit-agents` run/sandbox metrics.** Confirmed: `AgentType` is a small, admin-curated catalog (each provisions a real Kubernetes StatefulSet, created through a guided builder UI, no bulk-creation path) — structurally identical to the already-accepted `role_pool` precedent, not the unbounded free-text case (MCP tool names) that review caught twice. **Agent *name* is still never a label** (a rename would churn series identity) — it's carried instead by a dedicated low-cardinality info metric (`orbit_agents_agent_info{agent_id,agent_name,tenant_id,framework}`, the standard Prometheus "info metric" pattern) that Grafana joins on `agent_id`.
5. **Agent framework** (`claude-code`/`codex`/`opencode`, from `agents.AgentFramework`) is a safe, bounded label — add it to `orbit-agents` run-state metrics.

### Key Components

No new Go packages. Changes are additive instrumentation inside each service's existing feature package, per the ADR's Implementation table.

---

## Scope

### In Scope (all four tiers, one stacked PR)

- **Tier 1:** identity, orbit-agents, inference-gateway.
- **Tier 2:** policy-gateway, policy-service, orbit-triggers.
- **Tier 3:** egress-gateway, tool-gateway.
- **Tier 4:** session-manager.

### Out of Scope

- `orbit-mcp` (see Non-Goals).
- `operator` (no domain metrics beyond its existing controller-runtime instruments).
- Any new DB column (e.g. `last_activity_at`) or new cross-tenant aggregate query beyond the minimal tenant-aggregate additions to `policyservice.Stats()`'s call path needed for the two gauges in Tier 2.

---

## Requirements

### Functional Requirements

**Must Have (P0):** see User Stories' acceptance criteria above — all of it, across all four tiers, per the user's explicit direction to ship this as a single stacked PR rather than incremental per-tier PRs (deviating from the prior PRD's own stated preference for incremental follow-ups — noted, not re-litigated here).

**Should Have (P1):** none remaining — the one deferred item (a `tenant_id`-labeled `orbit_agents_runs_active` breakdown) is superseded by the 2026-10-20 amendment's `{state,framework,tenant_id,agent_id}` version, which is now a P0 (see User Stories above).

### Non-Functional Requirements

**Observability:**
- No metric introduced by this PRD may have unbounded or practically-unbounded cardinality. Any PR review finding a counterexample blocks merge until the offending label is removed or bucketed.

---

## Dependencies

### Internal Dependencies

- [Prometheus Metrics Endpoints](prometheus-metrics-endpoints.md) / [its ADR](../adr/2026-10-06-prometheus-metrics-endpoints.md) — all plumbing this PRD builds on, Implemented (PR #408, merged 2026-10-18).

### External Dependencies

None new.

---

## Risks and Mitigations

| Risk | Impact | Probability | Mitigation |
|------|--------|-------------|------------|
| A label silently becomes unbounded later (e.g. someone adds a run ID "just for debugging") | High (Prometheus/alerting degradation) | Low | Cardinality Policy section above is binding; call out explicitly in code review |
| `policyservice.Stats()`'s per-tenant isolation gets weakened to support a gauge callback | Medium (cross-tenant data exposure) | Low | This PRD ships tenant-aggregate-only gauges; per-tenant breakdown is explicitly out of scope, not implemented as a shortcut |
| Extra `GetAgentType` DB lookup per run transition for the `framework`/`agent_id` labels adds load | Low | Low | PR #418 review correctly flagged that `Transition` runs on every state change, including the frequent idle↔active boundary, not just terminal ones. Mitigated with an in-process `sync.Map[runID]AgentFramework` cache on `RunService`: one DB read per run's lifetime, not per transition |
| Tenant count or agent-types-per-tenant grows by an order of magnitude after this ADR's 2026-10-20 amendment, making the now-broadly-applied `tenant_id`/`agent_id` labels a real cardinality problem | Medium | Low | Both bounds are re-derivable from deployment data (`SELECT count(*) FROM tenants`, `SELECT tenant_id, count(*) FROM agent_types GROUP BY tenant_id`); revisit the amendment if either grows an order of magnitude past "low tens" |

---

## Appendix

### Related Documents

- [Domain Metrics Instrumentation ADR](../adr/2026-10-07-domain-metrics-instrumentation.md)
- [Prometheus Metrics Endpoints PRD](prometheus-metrics-endpoints.md)
- [Agent Control Plane PRD](agent-control-plane.md)
- [LLM Cost Tracking PRD](llm-cost-tracking.md)

**Last Updated:** 2026-10-20
