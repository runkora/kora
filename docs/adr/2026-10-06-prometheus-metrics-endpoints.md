# Prometheus Metrics Endpoints for Backend Services

<!-- Filename: 2026-10-06-prometheus-metrics-endpoints.md  (decision date + slug; no sequential number) -->

**Status:** Accepted
**Date:** 2026-10-06
**Deciders:** Platform Engineering
**Technical Story:** [Prometheus Metrics Endpoints PRD](../prd/prometheus-metrics-endpoints.md)

---

## Context

`backend/internal/platform/observability/metrics.go` builds an OTel `MeterProvider` whose only reader is an OTLP `PeriodicReader`, attached only when `ORBIT_OTLP_ENDPOINT` is configured; otherwise the provider has no reader and records nothing. The only existing caller is `orbit-triggers`, which built its own instrument set in `internal/triggers/metrics.go` on the OTel `metric.Meter` API. No service exposes a pull-based `/metrics` endpoint. No Prometheus exporter dependency is wired for application use: `go.opentelemetry.io/otel/exporters/prometheus` is absent from `go.mod`, and `github.com/prometheus/client_golang` is present only as an indirect transitive dependency (pulled in by OPA/controller-runtime). No ServiceMonitor/PodMonitor CRD, and no Prometheus server, exists anywhere in this repo's dev stack.

Two existing PRDs already specify Prometheus metrics that are unmet: `docs/prd/inference-gateway.md` US-013 and `docs/prd/agent-control-plane.md`'s Observability NFR. `docs/adr/2026-04-01-inference-gateway-architecture.md` already states the intended direction: metrics via the OTel metric API, with no direct `client_golang` dependency required. `docs/adr/2026-03-21-envoy-ext-proc-sidecar-token-injection.md` separately flags identity-proxy's missing `/metrics` as a known, deferred gap, distinct from this decision.

This touches *How does it connect?* only incidentally (a new, unauthenticated-by-design scrape port per service); it does not touch *Who is this agent?* or *What can it access?*.

### Goals

- Every rolled-out service exposes a Prometheus-scrapable `/metrics` endpoint on its own dedicated port.
- The same `go.opentelemetry.io/otel/metric` instrument feeds both the existing OTLP push path and the new Prometheus pull path, with zero duplicate instrumentation.
- Works with zero OTel collector infrastructure: the Prometheus reader is independent of whether an OTLP endpoint is configured.

### Non-Goals

- Deploying a Prometheus/Grafana/Prometheus-Operator server in this repo.
- Guaranteeing an out-of-mesh scraper can reach the endpoint. Istio mTLS and NetworkPolicy are independent concerns; see Decision's "Scrape path".
- `identity-proxy` and `session-manager` chart wiring. Both lack a chart Deployment for different reasons; both are follow-ons.
- The per-service domain metrics catalogued under Implementation below (run/session states, policy decisions, inference usage, delivery outcomes, credential minting). This decision covers the plumbing they'll be built on, not the instrumentation itself; see the companion PRD's Non-Goals for why that's follow-up work, not this PR.

---

## Considered Options

### Option 1: OTel metric API + `otel/exporters/prometheus` bridge (chosen)

**Description:** Keep instrumenting with `go.opentelemetry.io/otel/metric` (the API `triggers/metrics.go` already uses). Add `go.opentelemetry.io/otel/exporters/prometheus` as a second `sdkmetric.Reader` on the same `MeterProvider` that already carries the OTLP `PeriodicReader`. `observability.SetupMetrics` gains a variadic `extraReaders ...sdkmetric.Reader` parameter to carry it.

**Pros:**
- Zero duplicate instrumentation: one counter/histogram call feeds both exporters.
- Matches the existing documented precedent (`2026-04-01-inference-gateway-architecture.md`).
- Smallest possible change to `SetupMetrics`: backward compatible, the sole existing caller (`orbit-triggers`) is unaffected.
- Reader presence is independent per-concern: 0, 1, or 2 readers depending on whether an OTLP endpoint and/or Prometheus are configured. Directly satisfies "works without any collector."

**Cons:**
- Requires understanding both the OTel SDK and the Prometheus exposition format when debugging (one more layer of indirection than talking to `client_golang` directly).

### Option 2: Raw `client_golang` instrumentation

**Description:** Register `prometheus.Counter`/`Gauge`/`Histogram` directly via `client_golang` in each feature package, serve with `promhttp.Handler()`.

**Pros:**
- Most literally "a Prometheus package": no OTel SDK indirection.

**Cons:**
- A second, parallel instrumentation API alongside the OTel one already used by `triggers/metrics.go`, duplicating every counter/histogram call site a service wants in both places.
- Contradicts the explicit existing precedent at `2026-04-01-inference-gateway-architecture.md`.

### Option 3: Do nothing

**Description:** Leave metrics OTLP-only.

**Cons:**
- `docs/prd/inference-gateway.md` US-013 and `docs/prd/agent-control-plane.md`'s Observability NFR remain permanently unmet for any deployment without a running OTel collector.

---

## Decision

**Chosen Option:** Option 1: OTel metric API + `otel/exporters/prometheus` bridge.

### Rationale

This is the only option that gets both export paths from one instrument, matches the house style already established by `triggers/metrics.go`, and directly satisfies the "no collector required" requirement by construction: the `MeterProvider`'s reader set is built additively from independent conditions (OTLP endpoint configured? Prometheus metrics enabled?), so either, both, or neither can be true without special-casing.

### Scrape path

Two ways to find a scrape target exist in Kubernetes: `prometheus.io/*` pod annotations (a scraper's own `kubernetes_sd_configs` relabels them) and a Prometheus Operator `ServiceMonitor` CRD. This mesh's Istio sidecar rewrites `prometheus.io/port` to `15020` (`/stats/prometheus`) on every injected pod (metrics-merge, on by default), so the annotation path never reaches the port it names on any mesh-injected service. The chosen path is therefore the `ServiceMonitor` (`servicemonitors.yaml`), scraping `http-metrics`/`9464` directly over mTLS; the `prometheus.io/*` annotations are not rendered for the 9 mesh-injected, `ServiceMonitor`-covered services. The two exceptions keep them: `operator` (sidecar injection disabled, so the annotation path is the only one that reaches it; no `ServiceMonitor` either, since Prometheus Operator discovers it by annotation) and `session-manager` (no chart wiring either way: Go-side only, nothing scrapes it yet).

A direct `9464` scrape still has to clear each target's own Istio `AuthorizationPolicy`, which is a service-level allow-list, not a port-level one: `orbit-triggers`'s denies every caller outside its three named principals, metrics scrape included. The fix is `triggers.authorizationPolicy.metricsScraperPrincipals`: a port-scoped `ALLOW` rule, empty by default, so a scrape target is reachable only once an operator names the scraper's own mesh identity. The scraper itself must run inside the mesh (sidecar-injected) for its plain-HTTP request to be transparently upgraded to mTLS; a `PeerAuthentication` with `portLevelMtls: PERMISSIVE` on `9464` is a documented but **not built** alternative for a scraper that genuinely cannot join the mesh.

No metric emitted under this ADR carries a tenant, principal, run or trigger ID as a label. The `/metrics` endpoint is unauthenticated by design (Non-Goals): a label is readable by anything that can reach the port, so an identifier belongs in a trace, a log line, or a database row (surfaces this platform already authenticates and audits), never in a Prometheus label. This rule also applies to every per-service metric listed under Implementation below.

---

## Consequences

### Positive

- One instrumentation API across the whole backend, for both push and pull metrics.
- New services opt in with a config block and `platformmetrics.Setup`, not a new dependency per feature package.
- `orbit-triggers`' existing instruments become Prometheus-visible for free, with no re-instrumentation.

### Negative

- New direct dependency `go.opentelemetry.io/otel/exporters/prometheus`; `github.com/prometheus/client_golang` promoted indirect to direct.
- One new container port + Service port per rolled-out service.
- `identity-proxy` and `session-manager` remain gaps, deferred to follow-up work.
- NetworkPolicy and Istio mTLS are independent enforcement layers: a `ServiceMonitor` and the scraper's own mesh identity are both required for a scrape to actually complete; see Decision's "Scrape path" for the mechanism and `docs/adr/2026-03-05-service-mesh-and-network-enforcement-infrastructure.md` for the mesh's mTLS posture generally.

### Neutral

- `docs/code-organization.md`'s binary table and the ADR/PRD index files need updating to keep `scripts/docs/check-docs.sh` green.

---

## Implementation

- New package `backend/internal/platform/metrics/` (`Config`, `Exporter`, `Bootstrap`), parallel to `internal/platform/observability/`. `Bootstrap` is the one entry point each `main.go` calls: `Setup` builds the OTel pipeline and the per-route middleware, `Meter`/`Middleware` expose them, and `Start`/`StopServer`/`Shutdown` own the dedicated metrics server's lifecycle, so the OTLP-reader-plus-Prometheus-reader plumbing is written once rather than copied into every binary.
- `observability.SetupMetrics(ctx, cfg, extraReaders ...sdkmetric.Reader)`: additive signature change.
- New `backend/internal/platform/middleware/metrics.go`: `MetricsMiddleware(meter, serviceName)`. It must run *inside* `api.Mux`'s per-route dispatch, after `RoutePattern` has set the route on the request context, not in a binary's global `mw.MiddlewareChain(...)`, which wraps the Mux from outside, so a context value set inside the Mux's own dispatch is never visible to it. `api.Server` and `api.Mux` both take it via a `SetMetricsMiddleware` method.
- Per-service `main.go` wiring goes through `platformmetrics.Bootstrap`: a dedicated `*http.Server` for `/metrics`, started/stopped beside the existing app server, using the port name `http-metrics` (per Istio's `http-`/`tcp-`/`grpc-` prefix requirement for Envoy listener generation), defaulting to port `9464`, not `9090`, which `charts/orbit/templates/platform/netpols.yaml`'s `allow-mesh-sidecar` rule already uses for policy-gateway's ext_authz port. Shutdown uses `cfg.Metrics.ShutdownTimeout()`, independent of the service's own general shutdown timeout.
- Helm: `global.metrics.{enabled,port,scraperNamespace,serviceMonitor}` default block (mirroring `global.observability`), rendered into each service's own `cmd/<service>` config module (`docs/adr/2026-10-06-rendered-service-config-files.md`) rather than an env var fan-out; per-service override is the generic `configOverrides` escape hatch that ADR already provides, not a dedicated chart key. Container + Service port entries are gated on `global.metrics.enabled`, so disabling it removes the advertised scrape surface along with the Go-side listener.
- Two NetworkPolicy ingress rules in `netpols.yaml`, both gated on a new `global.metrics.scraperNamespace` value (empty by default, matching the existing `collectorNamespace` precedent): one scoped to `podSelector: {}` on port `9464`, rendered only when `global.metrics.enabled` is true, and one scoped to `app: orbit-operator` on port `8080`, rendered unconditionally since the operator's controller-runtime metrics server isn't part of this feature's enable/disable switch. The two are separate rules, not one shared port list, because `8080` is also identity's and orbit-agents' real API port: a single `podSelector: {}` rule naming it would have opened their application traffic to the scraper namespace too.
- An opt-in `ServiceMonitor` template (`charts/orbit/templates/platform/servicemonitors.yaml`), gated on a values flag, a live `Capabilities.APIVersions.Has "monitoring.coreos.com/v1"` check, and `global.metrics.enabled`, so enabling the flag without the CRD installed cannot break `helm template`/`install`, and disabling metrics takes the ServiceMonitor down with it. `prometheus.io/*` scrape annotations are rendered only for `operator` (sidecar injection disabled, so it has no other scrape path) and are omitted everywhere else; see Decision's "Scrape path" for why.
- `triggers.authorizationPolicy.metricsScraperPrincipals`: a port-scoped `ALLOW` rule on `orbit-triggers`'s `AuthorizationPolicy`, empty by default, naming the scraper's own mesh identity. Without it, `orbit-triggers`'s existing allow-list (three named caller principals) rejects a metrics scrape the same as it rejects any other uninvited caller.
- Rollout order: identity, orbit-agents, inference-gateway, policy-gateway, policy-service, orbit-triggers, egress-gateway, tool-gateway, orbit-mcp, session-manager (Go-side only, disabled by default: no chart wiring scrapes it yet). `operator` gets chart-only wiring (it already has controller-runtime's own Prometheus metrics, always on, independent of `global.metrics.enabled`). `migrate`, the sandbox/shell/MCP/session-runner stack, and `identity-proxy` are excluded.
- Instrument names normalize a leading `orbit-` out of the configured service name before building `orbit.<name>....`. Some services configure `orbit-<name>` (e.g. `orbit-identity`), others just `<name>` (e.g. `policy-gateway`); without normalizing, the former would double up into `orbit_orbit_identity_...`.
- The HTTP request duration histogram uses explicit seconds-scale bucket boundaries (Prometheus client_golang's conventional `DefBuckets`) rather than the OTel SDK's default, millisecond-tuned boundaries, which would otherwise collapse nearly every sub-5-second request into one bucket.

### Follow-up: per-service domain metrics (not implemented in this PR)

This decision ships the plumbing; it does not instrument any feature's business logic beyond the generic HTTP request counter/histogram every service gets automatically. The per-service metrics below are the target shape for follow-up PRs, one or a few services at a time, identified from each service's existing domain model (see the PRD's Non-Goals for why they're deferred rather than bundled here):

| Service | Metric (shape) | Instrumentation point |
|---|---|---|
| identity | `orbit_identity_sessions_active` (gauge), `orbit_identity_delegations_issued_total`/`_revoked_total` (counter), `orbit_identity_auth_failures_total{reason}` (counter) | `SessionRepository` (new `ActiveCount` query), `delegation_service.go` |
| orbit-agents | `orbit_agents_run_transitions_total{from_state,to_state}`, `orbit_agents_runs_active{state}` (gauge), `orbit_agents_run_duration_seconds`, `orbit_agents_runs_reaped_total{outcome}`, `orbit_agents_sessions_active` (gauge), `orbit_agents_session_duration_seconds`, `orbit_agents_sandbox_provision_duration_seconds`/`_failures_total{phase}`, `orbit_agents_guardrail_warnings_total{kind,tool}` | `run_service.go: Transition`/`TransitionRunState`/`StartReaper`, browser `Session` store, `session.go: ProvisionSandbox`, `guardrails.go: loopGuard` |
| session-manager | `orbit_session_manager_sessions_active` (gauge) | `sessionmanager.Registry.ActiveCount()`, already a ready-made read |
| policy-gateway | `orbit_policygw_checks_total{allow,resource_type}`, `orbit_policygw_check_duration_seconds`, `orbit_policygw_denies_total{reason}` | `policygw/service.go: Check` |
| policy-service | `orbit_policyservice_grants{principal_type}`/`_roles{kind}` (gauge), `orbit_policyservice_bundle_compiled_time{status}` (gauge) | existing `GET /policy/stats` query |
| inference-gateway | `orbit_inference_gateway_usage_total{status,provider}`, `_tokens_total{provider,kind}`, `_cost_usd_total{provider}`, `_request_duration_seconds{provider,status}`, `_budget_utilization_ratio{scope,window}` (gauge) | `InferenceUsageRecord` persistence path, `RedisBudgetCounter`'s live ledger |
| orbit-triggers | `orbit_triggers_delivery_outcomes_total{outcome}`, `orbit_triggers_admission_outcomes_total{outcome}` | `Delivery`/`DeliveryRun` status finalization, `admission.go` |
| egress-gateway | `orbit_egress_gateway_credentials_minted_total{adapter,action}`/`_mint_duration_seconds`/`_mint_failures_total{reason}` | `adapter.go: Connector.Mint` |
| tool-gateway | `orbit_tool_gateway_invocations_total{server,tool,decision}`/`_invocation_duration_seconds`, `orbit_tool_gateway_servers{state}` (gauge) | `mcp_controller.go: handleToolsCall`, `introspector.go: Snapshot()` |

A precise "active users" metric is a known, separate gap: no `last_seen`/`last_activity` column exists on `identity.UserProfile` or `Session` today, so it needs a small follow-up migration rather than a metric fabricated from `UpdatedAt`.

---

## Validation

### Success Metrics

- `curl :9464/metrics` on every rolled-out service returns live OTel-fed series with `ORBIT_OTLP_ENDPOINT` unset.
- `make check` green with the new dependency.
- `helm template` with `global.metrics.serviceMonitor.enabled=true` and the CRD absent renders no `ServiceMonitor` resources (guard holds).

### Monitoring

- Standard `make redeploy` + `kubectl port-forward` + `curl` smoke test per service, documented in the PRD's acceptance criteria.

---

## Related Decisions

- [OpenTelemetry Observability Discipline](2026-03-21-opentelemetry-observability-discipline.md)
- [Inference Gateway Architecture](2026-04-01-inference-gateway-architecture.md)
- [Telemetry Collector as a Declared Platform Destination](2026-09-18-telemetry-collector-platform-destination.md)
- [Envoy ext_proc Sidecar for Transparent Platform Token Injection](2026-03-21-envoy-ext-proc-sidecar-token-injection.md): identity-proxy's deferred `/metrics` gap
- [Service Mesh and Network Enforcement Infrastructure](2026-03-05-service-mesh-and-network-enforcement-infrastructure.md): the mTLS posture Decision's "Scrape path" accounts for

**Last Updated:** 2026-10-14
