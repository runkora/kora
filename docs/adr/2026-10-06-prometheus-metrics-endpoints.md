# Prometheus Metrics Endpoints for Backend Services

<!-- Filename: 2026-10-06-prometheus-metrics-endpoints.md  (decision date + slug; no sequential number) -->

**Status:** Accepted
**Date:** 2026-10-06
**Deciders:** Platform Engineering
**Technical Story:** [Prometheus Metrics Endpoints PRD](../prd/prometheus-metrics-endpoints.md)

---

## Context

`backend/internal/platform/observability/metrics.go` builds an OTel `MeterProvider` whose only reader is an OTLP `PeriodicReader`, attached only when `ORBIT_OTLP_ENDPOINT` is configured; otherwise the provider has no reader and records nothing (`metrics.go:38-45`). The only existing caller is `orbit-triggers` (`cmd/orbit-triggers/main.go:303`), which built its own instrument set in `internal/triggers/metrics.go` on the OTel `metric.Meter` API. No service exposes a pull-based `/metrics` endpoint. No Prometheus exporter dependency is wired for application use — `go.opentelemetry.io/otel/exporters/prometheus` is absent from `go.mod`, and `github.com/prometheus/client_golang` is present only as an indirect transitive dependency (pulled in by OPA/controller-runtime). No ServiceMonitor/PodMonitor CRD, and no Prometheus server, exists anywhere in this repo's dev stack.

Two existing PRDs already specify Prometheus metrics that are unmet: `docs/prd/inference-gateway.md` US-013 and `docs/prd/agent-control-plane.md`'s Observability NFR. `docs/adr/2026-04-01-inference-gateway-architecture.md:1076` already states the intended direction: metrics via the OTel metric API, with no direct `client_golang` dependency required. `docs/adr/2026-03-21-envoy-ext-proc-sidecar-token-injection.md:473` separately flags identity-proxy's missing `/metrics` as a known, deferred gap, distinct from this decision.

This touches *How does it connect?* only incidentally (a new, unauthenticated-by-design scrape port per service) — it does not touch *Who is this agent?* or *What can it access?*.

### Goals

- Every rolled-out service exposes a Prometheus-scrapable `/metrics` endpoint on its own dedicated port.
- The same `go.opentelemetry.io/otel/metric` instrument feeds both the existing OTLP push path and the new Prometheus pull path, with zero duplicate instrumentation.
- Works with zero OTel collector infrastructure — the Prometheus reader is independent of whether an OTLP endpoint is configured.

### Non-Goals

- Deploying a Prometheus/Grafana/Prometheus-Operator server in this repo.
- Guaranteeing an out-of-mesh scraper can reach the endpoint (Istio mTLS is a separate concern from NetworkPolicy — addressed under Consequences, not solved here).
- `identity-proxy` and `session-manager` chart wiring (both lack a chart Deployment for different reasons — follow-ons).

---

## Considered Options

### Option 1: OTel metric API + `otel/exporters/prometheus` bridge (chosen)

**Description:** Keep instrumenting with `go.opentelemetry.io/otel/metric` (the API `triggers/metrics.go` already uses). Add `go.opentelemetry.io/otel/exporters/prometheus` as a second `sdkmetric.Reader` on the same `MeterProvider` that already carries the OTLP `PeriodicReader`. `observability.SetupMetrics` gains a variadic `extraReaders ...sdkmetric.Reader` parameter to carry it.

**Pros:**
- Zero duplicate instrumentation — one counter/histogram call feeds both exporters.
- Matches the existing documented precedent (`2026-04-01-inference-gateway-architecture.md:1076`).
- Smallest possible change to `SetupMetrics`: backward compatible, the sole existing caller (`orbit-triggers`) is unaffected.
- Reader presence is independent per-concern: 0, 1, or 2 readers depending on whether an OTLP endpoint and/or Prometheus are configured — directly satisfies "works without any collector."

**Cons:**
- Requires understanding both the OTel SDK and the Prometheus exposition format when debugging (one more layer of indirection than talking to `client_golang` directly).

### Option 2: Raw `client_golang` instrumentation

**Description:** Register `prometheus.Counter`/`Gauge`/`Histogram` directly via `client_golang` in each feature package, serve with `promhttp.Handler()`.

**Pros:**
- Most literally "a Prometheus package" — no OTel SDK indirection.

**Cons:**
- A second, parallel instrumentation API alongside the OTel one already used by `triggers/metrics.go`, duplicating every counter/histogram call site a service wants in both places.
- Contradicts the explicit existing precedent at `2026-04-01-inference-gateway-architecture.md:1076`.

### Option 3: Do nothing

**Description:** Leave metrics OTLP-only.

**Cons:**
- `docs/prd/inference-gateway.md` US-013 and `docs/prd/agent-control-plane.md`'s Observability NFR remain permanently unmet for any deployment without a running OTel collector.

---

## Decision

**Chosen Option:** Option 1 — OTel metric API + `otel/exporters/prometheus` bridge.

### Rationale

This is the only option that gets both export paths from one instrument, matches the house style already established by `triggers/metrics.go`, and directly satisfies the "no collector required" requirement by construction: the `MeterProvider`'s reader set is built additively from independent conditions (OTLP endpoint configured? Prometheus metrics enabled?), so either, both, or neither can be true without special-casing.

---

## Consequences

### Positive

- One instrumentation API across the whole backend, for both push and pull metrics.
- New services opt in with a config block and two function calls, not a new dependency per feature package.
- `orbit-triggers`' existing instruments become Prometheus-visible for free, with no re-instrumentation.

### Negative

- New direct dependency `go.opentelemetry.io/otel/exporters/prometheus`; `github.com/prometheus/client_golang` promoted indirect → direct.
- One new container port + Service port per rolled-out service.
- `identity-proxy` and `session-manager` remain gaps, deferred to follow-up work.

### Neutral

- `docs/code-organization.md`'s binary table and the ADR/PRD index files need updating to keep `scripts/docs/check-docs.sh` green.

---

## Implementation

- New package `backend/internal/platform/metrics/` (`Config`, `Exporter`), parallel to `internal/platform/observability/`.
- `observability.SetupMetrics(ctx, cfg, extraReaders ...sdkmetric.Reader)` — additive signature change.
- New `backend/internal/platform/middleware/metrics.go`: `MetricsMiddleware(meter, serviceName)`, added to each rolled-out service's existing `mw.MiddlewareChain(...)` call.
- Per-service `main.go` wiring: a dedicated `*http.Server` for `/metrics`, started/stopped beside the existing app server, using the port `http-metrics` (named per Istio's `http-`/`tcp-`/`grpc-` prefix requirement for Envoy listener generation, documented in `egress-gateway/service.yaml:11-31`), defaulting to port `9464` — not `9090`, which `charts/orbit/templates/platform/netpols.yaml`'s `allow-mesh-sidecar` rule already uses for policy-gateway's ext_authz port.
- Helm: `global.metrics.{enabled,port}` default block (mirroring `global.observability`), per-service override via the existing `orbit.serviceConfig` helper, container + Service port entries, hardcoded `prometheus.io/*` scrape annotations (scrape correctness is chart-owned, unlike `orbit.podAnnotations`, which is for arbitrary user annotations).
- A new NetworkPolicy ingress rule in `netpols.yaml`, gated on a new `global.metrics.scraperNamespace` value (empty by default, matching the existing `collectorNamespace` precedent — nothing opens until an operator names where their scraper runs).
- An opt-in `ServiceMonitor` template (`charts/orbit/templates/platform/servicemonitors.yaml`), gated on both a values flag and a live `Capabilities.APIVersions.Has "monitoring.coreos.com/v1"` check, so enabling the flag without the CRD installed cannot break `helm template`/`install`.
- Rollout order: identity, orbit-agents, inference-gateway, policy-gateway, policy-service, orbit-triggers, egress-gateway, tool-gateway, orbit-mcp, session-manager (Go-side only). `operator` gets chart-only wiring (it already has controller-runtime's own Prometheus metrics). `migrate`, the sandbox/shell/MCP/session-runner stack, and `identity-proxy` are excluded.

**Istio mTLS note** (documented here, not solved by this ADR): this mesh enforces mTLS east-west (`docs/adr/2026-03-05-service-mesh-and-network-enforcement-infrastructure.md`). NetworkPolicy and mTLS are independent enforcement layers — passing the NetworkPolicy rule above does not mean a plain-HTTP, out-of-mesh scraper can complete a TLS handshake with the target's Envoy sidecar. The supported path is running the scraper *inside* the mesh (sidecar-injected), where Istio transparently upgrades its plain-HTTP request to mTLS sidecar-to-sidecar with no scraper-side changes. A per-port `PeerAuthentication` with `portLevelMtls` set to `PERMISSIVE` on the metrics port is a documented but **not built** alternative for scrapers that genuinely cannot join the mesh.

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
- [Envoy ext_proc Sidecar for Transparent Platform Token Injection](2026-03-21-envoy-ext-proc-sidecar-token-injection.md) — identity-proxy's deferred `/metrics` gap
- [Service Mesh and Network Enforcement Infrastructure](2026-03-05-service-mesh-and-network-enforcement-infrastructure.md) — the mTLS posture this ADR's Consequences section accounts for

**Last Updated:** 2026-10-06
