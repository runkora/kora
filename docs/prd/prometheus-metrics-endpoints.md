# Prometheus Metrics Endpoints - Product Requirements Document

**Status:** Approved
**Created:** 2026-10-06
**Last Updated:** 2026-10-07
**Owner:** Platform Engineering
**Stakeholders:** Platform operators, SRE, customers running kora without a managed OTel collector

---

## Executive Summary

kora's backend services have OTel tracing everywhere, but metrics are push-only to an OTLP collector, and only one service (`orbit-triggers`) actually emits any. There is no pull-based `/metrics` endpoint anywhere in the platform. This PRD adds a shared Go package and per-service wiring so every backend service can expose a configurable, Prometheus-scrapable `/metrics` endpoint on its own dedicated port, fed by the same OTel instruments already used for OTLP, so instrumenting a counter or histogram once gets both export paths for free.

---

## Problem Statement

### Current State

`backend/internal/platform/observability/metrics.go` builds an OTel `MeterProvider` that only ever has a reader when `KORA_OTLP_ENDPOINT` is configured; otherwise it records nothing. Only `orbit-triggers` calls it today. There is no Prometheus exporter dependency wired for application use (`github.com/prometheus/client_golang` is present only as an indirect transitive dependency), no ServiceMonitor/PodMonitor CRDs, and no Prometheus server anywhere in this repo's local dev stack (`docker-compose.yml`, `make up`).

### User Pain Points

- A platform operator running kora without a standing OTel collector gets **zero** metrics from any service — no active-run counts, no request rates, no error rates — even though two existing PRDs already promise them.
- `docs/prd/inference-gateway.md` (US-013) already specifies "Prometheus metrics endpoint exposes request rate, error rate, latency percentiles..." — unmet.
- `docs/prd/agent-control-plane.md`'s Observability NFR already specifies "`orbit-agents` exposes Prometheus metrics: active runs, active sessions, events/sec, session routing latency" — unmet.
- `docs/adr/2026-03-21-envoy-ext-proc-sidecar-token-injection.md` separately flags the identity-proxy sidecar's missing `/metrics` as a known, deferred gap.

### Business Impact

- Operators can't answer basic platform-health questions (how many agents are running, how many sandboxes are active, is the policy engine denying more than usual) without standing up a full OTel collector pipeline first.
- Existing PRD commitments (inference-gateway, agent-control-plane) are unfulfilled.

---

## Goals and Objectives

### Primary Goals

1. A shared Go package (`internal/platform/metrics`) any service can use to spin up a configurable, pull-based Prometheus `/metrics` endpoint on its own dedicated port.
2. The same `go.opentelemetry.io/otel/metric` instrument (counter/histogram/gauge) feeds both the existing OTLP push path (when a collector is configured) and the new Prometheus pull path (when metrics are enabled) — zero duplicate instrumentation.
3. Works with **zero OTel collector infrastructure**: a customer with no collector still gets working Prometheus scraping, because the Prometheus reader is attached independently of whether an OTLP endpoint is configured.
4. Helm chart wiring so the port is reachable: container port, Service port, `prometheus.io/*` scrape annotations, an opt-in `ServiceMonitor` template, and the NetworkPolicy rule needed to actually let a scraper through the namespace's default-deny posture.
5. A uniform HTTP request counter and duration histogram (`orbit_<service>_http_requests_total`, `orbit_<service>_http_request_duration_seconds`) on every rolled-out service, labeled by method/route/status, as the first metric every service gets for free. The companion ADR's Implementation section catalogs the domain-specific metrics identified for each service (run states, session lifecycles, policy decisions, inference usage, delivery outcomes, credential minting) as the target shape for follow-up PRs — this PRD ships the plumbing they'll be built on, not the domain instruments themselves (see Non-Goals).

### Success Metrics

- `curl :9464/metrics` on every rolled-out service returns live OTel-fed series, with `KORA_OTLP_ENDPOINT` unset.
- `make check` passes with the new dependency and no new lint exceptions beyond what's already scoped.
- `docs-check` passes with the new PRD/ADR indexed and the binary table updated.

### Non-Goals

- Deploying an actual Prometheus / Grafana / Prometheus-Operator server in this repo. None exists today in `make up` or `docker-compose.yml`, and none is added by this work.
- Guaranteeing an out-of-mesh scraper can reach the endpoint. This PRD ships the NetworkPolicy rule and the opt-in `ServiceMonitor`, but the mesh runs full Istio mTLS east-west; a scraper must either run inside the mesh (recommended) or a per-port `PeerAuthentication` exception must be added separately — see the companion ADR's Consequences.
- A `/metrics` endpoint for `identity-proxy` (webhook-injected sidecar, no chart Deployment — already a known, separately-flagged gap) or for the `session-manager` Helm chart (it has no Deployment/Service template; it's dynamically provisioned per-session by `orbit-agents`, so its metrics port must be threaded through that provisioning code, not YAML, as a follow-up).
- A precise "active users" metric. No `last_seen`/`last_activity` column exists on `identity.UserProfile` or `Session` today; a trustworthy version needs a small follow-up migration, not a metric fabricated from `UpdatedAt`.
- The per-service domain metrics catalogued in the companion ADR (run/session states, policy decisions, inference usage, delivery outcomes, credential minting). Each requires instrumenting business logic in its own feature package, not the platform plumbing this PRD scopes — tracked as follow-up PRs, one or a few services at a time, so each lands with its own review rather than one PR touching every feature package at once.
- Fixing the `docs/prd/inference-gateway.md` US-013 mismatch where it says `GET /healthz` but the actual repo convention is `GET /health` — noted here, not fixed as part of this work.

---

## User Stories

**As a** platform operator running kora with no OTel collector deployed,
**I want** each backend service to expose a `/metrics` endpoint I can point any Prometheus-compatible scraper at,
**So that** I get request rates, error rates, active-run/session counts, and policy-decision counts without first standing up collector infrastructure.

**Acceptance Criteria:**
- [x] Each rolled-out service listens on a dedicated metrics port, separate from its API port.
- [x] `/metrics` returns Prometheus text-format output with the uniform HTTP request counter/histogram. Service-specific domain metrics are catalogued in the ADR as follow-up work (see Non-Goals), not delivered by this PRD.
- [x] Disabling `KORA_OTLP_ENDPOINT` has no effect on whether `/metrics` populates.

**As a** platform operator who already runs an OTel collector,
**I want** the same instruments to also flow to that collector,
**So that** I don't have to choose between the two or instrument twice.

**Acceptance Criteria:**
- [ ] The same counter/histogram increments are visible via both the OTLP-fed backend and `/metrics`.

**As a** cluster operator with Prometheus Operator installed,
**I want** an opt-in `ServiceMonitor` per service,
**So that** Prometheus Operator can discover scrape targets without me hand-writing scrape configs.

**Acceptance Criteria:**
- [ ] A values flag renders a `ServiceMonitor` per rolled-out service when both the flag is set and the CRD is present on the target cluster.
- [ ] Leaving the flag on without the CRD installed does not break `helm template`/`install`.

---

## Proposed Solution

### High-Level Approach

Extend the existing OTel metrics pipeline (`observability.SetupMetrics`) to optionally carry a second `sdkmetric.Reader` backed by `go.opentelemetry.io/otel/exporters/prometheus`, alongside the existing OTLP `PeriodicReader`. A new `internal/platform/metrics` package owns this Prometheus reader, its own dedicated `prometheus.Registry`, and a bare `*http.Server` serving it — modeled on the existing `/health` bypass precedent in `internal/platform/api/api.go` (health and now metrics intentionally sit outside the app's tracing/logging middleware chain). Each service wires this in next to its existing `observability.Setup` call, and a new `middleware.MetricsMiddleware` adds a uniform HTTP request counter/histogram to every service's middleware chain.

### Key Components

- **`internal/platform/metrics`**: `Config`, `Exporter` (owns registry + reader + HTTP server), nil-safe throughout so a disabled config is a no-op.
- **`observability.SetupMetrics` extension**: variadic `extraReaders ...sdkmetric.Reader` parameter.
- **`middleware.MetricsMiddleware`**: uniform HTTP request counter + duration histogram, labeled by low-cardinality route pattern.
- **Helm chart wiring**: per-service `metricsPort`/`metricsEnabled` values, container + Service port named `http-metrics`, scrape annotations, opt-in `ServiceMonitor`, and a NetworkPolicy ingress rule gated on a configured scraper namespace.

---

## Scope

### In Scope

- `internal/platform/metrics` package; `observability.SetupMetrics` signature change.
- Wiring into: identity, orbit-agents, inference-gateway, policy-gateway, policy-service, orbit-triggers, egress-gateway, tool-gateway, orbit-mcp, session-manager (Go side only for the last one).
- Helm chart wiring for every one of the above except session-manager (no chart Deployment), plus chart-only wiring (Service port + annotations) for `operator` (already has its own controller-runtime metrics).
- Opt-in `ServiceMonitor` template and a NetworkPolicy scrape-ingress rule.
- A uniform HTTP request counter/histogram on every rolled-out service (`orbit_<service>_http_requests_total`/`_http_request_duration_seconds`), and bringing `orbit-triggers`' pre-existing instruments onto the same Prometheus reader.

### Out of Scope

- `migrate` (one-shot Job), `sandbox-exec`/`orbit-runtime-mcp`/`orbit-shell`/`orbit-session-runner` (non-HTTP agent-sandbox stack), `identity-proxy` (sidecar, no chart Deployment).
- Deploying Prometheus/Grafana/Prometheus-Operator itself.
- Per-port `PeerAuthentication` Istio exceptions for out-of-mesh scrapers (documented as an alternative, not built).
- The per-service domain metrics (run/session states, policy decisions, inference usage, delivery outcomes, credential minting) catalogued in the companion ADR. These need instrumentation inside each feature's own business logic, not the platform plumbing this PRD scopes.

### Future Considerations

- `identity-proxy` `/metrics` (needs Go changes in `pod_webhook.go`'s sidecar injection, separate from this chart-driven rollout).
- `session-manager` chart-equivalent wiring threaded through `orbit-agents`' dynamic pod-spec construction.
- A precise "active users" metric backed by a dedicated `last_activity_at` column.
- Per-service domain metrics (see Out of Scope) — one or a few services at a time, as their own reviewable PRs, following the inventory in the companion ADR.

---

## Requirements

### Functional Requirements

**Must Have (P0):**
- [ ] `internal/platform/metrics.New(cfg)` returns a nil-safe `*Exporter` that is `nil` when `cfg.Enabled == false`.
- [ ] `observability.SetupMetrics` accepts additional readers without breaking the existing `orbit-triggers` caller.
- [ ] Each rolled-out service's metrics server bypasses the app's tracing/logging middleware, matching the `/health` precedent.
- [ ] Metrics survive graceful shutdown: the metrics `*http.Server` shuts down alongside the app server using the same shutdown timeout.

**Should Have (P1):**
- [ ] Per-service disable via `{service}.configOverrides: {cmd/{service}: {metrics: {enabled: false}}}` in Helm values (the generic per-service config override mechanism `docs/adr/2026-10-06-rendered-service-config-files.md` introduced) — not a dedicated chart key of its own.
- [ ] Opt-in `ServiceMonitor`, guarded by a values flag, a live CRD-presence check, and the same `global.metrics.enabled` flag.

**Nice to Have (P2):**
- [ ] Go runtime / process collectors (`go_*`, `process_*` series) alongside the OTel-fed series.

### Non-Functional Requirements

**Observability:**
- Metric names follow `orbit_<service>_<noun>_<unit|total>`.
- Labels stay low-cardinality (route *pattern*, not raw path; enum-backed label values, never free-text IDs).

**Security:**
- The metrics endpoint carries no authentication by itself (standard Prometheus convention) — reachability is controlled entirely by NetworkPolicy and Istio mTLS, not application auth. This PRD does not add app-level auth to `/metrics`.

---

## Dependencies

### Internal Dependencies

- `observability.SetupMetrics` (existing, being extended) — Implemented.
- `triggers/metrics.go` — existing OTel instrument-set precedent this work reuses, not replaces.
- `api.Server`'s `/health` bypass precedent (`api/api.go:63-92`) — the pattern this work mirrors for `/metrics`.

### External Dependencies

- `go.opentelemetry.io/otel/exporters/prometheus` — new direct dependency.
- `github.com/prometheus/client_golang` — promoted from indirect to direct.

---

## Risks and Mitigations

| Risk | Impact | Probability | Mitigation |
|------|--------|-------------|------------|
| Out-of-mesh Prometheus can't reach the endpoint due to Istio mTLS | Medium | High (if a scraper is deployed naively) | Document that the scraper must run inside the mesh (sidecar-injected); offer per-port `PeerAuthentication` as a documented alternative, not built by default |
| Reusing a port number already meaningful elsewhere in `netpols.yaml` (e.g. `9090`) causes confusion | Low | Medium | Use `9464` (OTel Prometheus exporter convention), distinct from every port already named in `allow-mesh-sidecar` |
| `docs-check` fails on stale binary table / missing ADR-PRD index entries | Low | High | Update `docs/code-organization.md` and both index files as part of this change |

---

## Open Questions

1. **Exact `app` label per service for the opt-in `ServiceMonitor`'s `selector.matchLabels`.**
   - **Status:** Open — verify against each service's own `orbit.<svc>.selectorLabels` helper in `_helpers.tpl` during implementation rather than assuming a naming pattern.
   - **Owner:** Implementer.

---

## Appendix

### Related Documents

- [Prometheus Metrics Endpoints ADR](../adr/2026-10-06-prometheus-metrics-endpoints.md)
- [Inference Gateway PRD](inference-gateway.md) — US-013
- [Agent Control Plane PRD](agent-control-plane.md) — Observability NFR

### References

- [OpenTelemetry Observability Discipline ADR](../adr/2026-03-21-opentelemetry-observability-discipline.md)
- [Inference Gateway Architecture ADR](../adr/2026-04-01-inference-gateway-architecture.md)
- [Telemetry Collector as a Declared Platform Destination ADR](../adr/2026-09-18-telemetry-collector-platform-destination.md)
- [Envoy ext_proc Sidecar ADR](../adr/2026-03-21-envoy-ext-proc-sidecar-token-injection.md) — identity-proxy's deferred `/metrics` gap

**Last Updated:** 2026-10-06
