# 0018. OpenTelemetry Observability Pipeline

**Status:** Proposed
**Date:** 2026-03-16
**Last Updated:** 2026-03-16
**Deciders:** Engineering Team
**Technical Story:** Establish a production-grade observability foundation for Orbit across traces, metrics, and logs using OpenTelemetry conventions and a Kubernetes-native backend stack.

---

## Context

Orbit is the control plane for autonomous agents. That makes observability part of the product's safety model, not just an operations concern. The platform must be able to answer:

- Which identity initiated an action?
- Which policy decision allowed or denied it?
- Which downstream system was called?
- Where did latency, retries, and failures occur?
- How do operators correlate logs, metrics, and traces for a single agent run?

The current codebase already established one important part of the foundation in [ADR-0004](0004-dependency-injected-logging.md): all services use dependency-injected structured logging with `context.Context` so trace and span identifiers can be attached automatically. What is still missing is the platform-level decision for how telemetry is collected, transported, stored, correlated, and visualised in Kubernetes.

### Background

Orbit is being built as Kubernetes-based control-plane infrastructure with identity, policy, and execution flows that cross service boundaries. That architecture increases the operational cost of weak observability:

- Requests will move through HTTP services, policy engines, service mesh infrastructure, and eventually agent execution runtimes
- AuthN/AuthZ failures must be diagnosable without reading several unrelated log streams by hand
- Multi-tenant workloads require consistent telemetry labels and strong discipline around sensitive data
- The platform should remain aligned with open standards rather than being shaped around a proprietary monitoring vendor

The team is currently leaning toward:

- **Prometheus** for metrics
- **Loki** for logs
- **Tempo** for traces
- **Grafana** for dashboards, exploration, and correlation

That stack is a strong fit for Kubernetes, but the architectural question is broader than backend selection. We also need to decide whether Orbit services export directly to backends, whether we standardise on OpenTelemetry, and what the collection topology should be.

### Problem Statement

We need an observability architecture that:

1. Standardises telemetry instrumentation across Orbit services
2. Preserves trace propagation and cross-signal correlation
3. Works cleanly in Kubernetes and local development environments
4. Minimises vendor lock-in
5. Supports security and tenancy constraints around labels, attributes, and log content
6. Can evolve without forcing widespread application rewrites

### Goals

- Standardise distributed tracing on OpenTelemetry across all Orbit services
- Define a single collection layer that decouples application instrumentation from backend storage
- Use Prometheus, Loki, Tempo, and Grafana as the default self-hosted backend stack
- Preserve the structured stdout logging model established in ADR-0004
- Ensure logs, metrics, and traces can be correlated for a request, agent run, or policy decision
- Keep local development and Kubernetes deployment models aligned where practical

### Non-Goals

- Selecting a commercial SaaS observability vendor
- Designing every Grafana dashboard up front
- Capturing full request or response payloads by default
- Replacing existing structured logging with a custom logging framework
- Defining SLOs and alert thresholds for every service in this ADR

---

## Decision Drivers

* **Open standards first**: Orbit should align with OpenTelemetry and Prometheus conventions rather than vendor-specific SDKs
* **Cross-signal correlation**: traces, logs, and metrics must be navigable together for agent workflows and policy decisions
* **Kubernetes fit**: the solution must work naturally with cluster-native deployment patterns
* **Security and privacy**: telemetry must avoid leaking secrets, tokens, and sensitive payloads
* **Operational flexibility**: backend choices may change later; service instrumentation should not
* **Incremental adoption**: the platform needs a path that works now and scales with future services
* **Performance**: instrumentation overhead must stay bounded on hot paths

---

## Considered Options

### Option A: OpenTelemetry SDKs + OpenTelemetry Collector + Grafana stack (CHOSEN)

**Description:** Standardise on OpenTelemetry for tracing and context propagation. Applications export telemetry to an OpenTelemetry Collector layer. Tempo stores traces, Loki stores logs, Prometheus stores metrics, and Grafana provides visualisation and correlation. Logs remain structured JSON on stdout and are collected at the node level before forwarding to Loki.

**Pros:**
- Decouples Orbit services from storage backends through the Collector
- Preserves flexibility to swap or add backends later without rewriting service code
- Gives a single place for batching, filtering, sampling, enrichment, and tenant-safe attribute processing
- Fits naturally with ADR-0004's context-aware logging model
- Supports standard OTLP transport for traces and future metrics/log pipelines
- Works well in Kubernetes with agent and gateway collector topologies

**Cons:**
- Adds another control-plane component to operate
- Requires clear conventions for resource attributes and log labels
- Creates some upfront design work around sampling, cardinality, and redaction

### Option B: Signal-specific native tooling without an OpenTelemetry Collector

**Description:** Use Prometheus scraping directly for metrics, Promtail or Fluent Bit for logs, and direct Tempo exporters for traces, with each signal managed independently.

**Pros:**
- Simpler initial mental model
- Fewer moving parts in the earliest stages
- Uses familiar per-signal tools from the Kubernetes ecosystem

**Cons:**
- Correlation and signal governance become fragmented
- Traces couple more directly to a specific backend choice
- No central place for telemetry policy, enrichment, redaction, or routing
- Different services are more likely to drift into inconsistent instrumentation patterns

### Option C: Managed vendor platform for all telemetry

**Description:** Instrument Orbit with a vendor-specific or vendor-hosted telemetry stack and defer most collection and storage concerns to that provider.

**Pros:**
- Fastest path to a polished UI and mature operational tooling
- Lower self-hosting burden for storage backends
- Often includes advanced alerting and analytics out of the box

**Cons:**
- High lock-in risk at the exact layer where Orbit should prefer open standards
- Cost scales with ingest volume and retention
- Harder to keep local, CI, and production environments conceptually aligned
- Less control over tenant boundaries and sensitive telemetry handling

---

## Decision

**Chosen Option:** Option A - OpenTelemetry SDKs + OpenTelemetry Collector + Grafana stack

### Rationale

Orbit should standardise on **OpenTelemetry as the instrumentation and transport standard**, not on any single backend product. The right abstraction boundary is:

- **Applications and platform components speak OpenTelemetry**
- **Collectors own telemetry transport, shaping, and routing**
- **Prometheus, Loki, and Tempo are replaceable storage backends behind that layer**

This is the most robust fit for Orbit's architecture.

First, Orbit is a control plane with identity, policy, and execution paths that cross service boundaries. Distributed tracing is not optional. OpenTelemetry gives us the standard propagation model, semantic conventions, and SDK support needed to trace those flows end to end.

Second, the Collector gives Orbit a central enforcement point for telemetry handling. That matters for security as much as operations. Redaction, attribute filtering, tenant enrichment, sampling policy, and backend routing should not be implemented independently inside every service.

Third, the Grafana stack matches the team's current preference and fits the Kubernetes deployment model well:

- **Tempo** is the system of record for traces
- **Loki** is the system of record for logs
- **Prometheus** is the system of record for metrics
- **Grafana** is the common UI for dashboards and trace-log-metric correlation

Fourth, this decision preserves the logging model already chosen in ADR-0004. Orbit services will continue to log structured JSON to stdout with trace and span identifiers included when a span is active. In Kubernetes, log collection is an infrastructure concern handled by the collector layer, not by application code.

This gives Orbit a practical path now without foreclosing future changes. If the team later adopts managed backends, changes retention systems, or introduces tenant-specific routing, those changes happen primarily in collectors and backend configuration rather than across every Go service.

---

## Consequences

### Positive

- Orbit services share one observability standard for trace propagation and instrumentation
- Backend storage remains replaceable because applications target the Collector, not Tempo/Loki/Prometheus directly
- Logs, metrics, and traces can be correlated consistently in Grafana
- Structured stdout logging from ADR-0004 remains valid and Kubernetes-friendly
- The Collector becomes a single place to enforce sampling, redaction, attribute normalisation, and routing
- The design scales from local development to clustered environments with the same conceptual model

### Negative

- The platform now depends on collector operations and configuration quality
- Telemetry schema discipline becomes mandatory; poor label and attribute hygiene will create cost and performance problems
- Teams must learn OpenTelemetry concepts, especially spans, resource attributes, propagation, and cardinality controls
- Some backend-specific capabilities may still require translation or compromise at the collector layer

### Neutral

- Metrics remain Prometheus-oriented even though OpenTelemetry is the instrumentation standard; that hybrid model is intentional and common
- Logs are still emitted to stdout by applications rather than pushed directly from application code to Loki
- Sampling strategy, retention periods, and dashboard design remain follow-on implementation decisions

---

## Implementation

### Target Architecture

```
┌──────────────────────────────────────────────────────────────┐
│ Orbit services and platform components                      │
│ OTel tracing + context propagation                          │
│ Structured JSON logs to stdout                              │
│ Prometheus-style service metrics and/or OTel metrics        │
└──────────────────────────────────────────────────────────────┘
                             │
                             ▼
┌──────────────────────────────────────────────────────────────┐
│ OpenTelemetry Collector                                     │
│ Agent/DaemonSet for node log collection                     │
│ Gateway/Deployment for OTLP ingest, processing, routing     │
└──────────────────────────────────────────────────────────────┘
              │                       │                    │
              ▼                       ▼                    ▼
        ┌───────────┐           ┌───────────┐        ┌───────────┐
        │   Tempo   │           │   Loki    │        │ Prometheus│
        │  traces   │           │   logs    │        │  metrics  │
        └───────────┘           └───────────┘        └───────────┘
                             \         |         /
                              \        |        /
                               ▼       ▼       ▼
                                 ┌─────────┐
                                 │ Grafana │
                                 └─────────┘
```

### Signal Strategy

**Traces**

- All Go services instrument inbound and outbound HTTP flows with the OpenTelemetry Go SDK
- Trace context propagates across internal service calls and gateway boundaries
- Services export traces over OTLP to the Collector gateway
- Tempo stores traces and Grafana is the primary trace exploration UI

**Logs**

- Application logs continue to be emitted as structured JSON to stdout, per ADR-0004
- Log entries must include trace and span identifiers when present in `context.Context`
- A Collector agent running per node collects container logs and forwards them to Loki
- Application code does not push logs directly to Loki

**Metrics**

- Prometheus remains the metrics backend of record
- Services expose operational metrics using Prometheus-compatible conventions
- The Collector may enrich or forward metrics where useful, but Prometheus scraping remains the baseline interoperability model
- High-cardinality business identifiers must not be used as metric labels

### Collector Topology

Orbit should adopt a **two-tier collector model** in Kubernetes:

- **Collector agent (DaemonSet)** for node-local log collection and optional host-level telemetry
- **Collector gateway (Deployment)** as the central OTLP ingest, processing, sampling, and routing layer

This split keeps application traffic off node-local backend clients and centralises policy in the gateway while still handling Kubernetes log collection efficiently.

### Telemetry Conventions

All Orbit services must follow a shared telemetry schema:

- `service.name`, `service.version`, and `deployment.environment` set consistently
- Tenant, workspace, agent, and run identifiers recorded as span/log attributes only where cardinality is safe
- Identity and policy decision metadata included as structured attributes when it materially improves diagnosis
- Secrets, bearer tokens, raw credentials, and full sensitive payloads are never emitted

### Security Requirements

- No access tokens, refresh tokens, client secrets, or raw brokered credentials in logs, spans, or metric labels
- Request and response body capture is opt-in and must be redacted before export
- Sampling must not create a path where denied or security-relevant operations become invisible; security events should always be logged even when traces are sampled
- Multi-tenant metadata must be reviewed for both sensitivity and label cardinality before standardisation

### Local Development Alignment

- Tier 1 local development may continue without the full collector stack where fast feedback matters most
- Tier 2 cluster development should include the collector and Grafana stack so trace propagation and correlation can be validated end to end
- Local and cluster environments should share the same OTLP configuration model even when specific backends differ

### Migration Path

1. Keep ADR-0004 logging as-is and ensure all log records include trace/span IDs when available
2. Add OpenTelemetry tracing to the backend HTTP server, HTTP clients, database calls, and key internal workflows
3. Deploy an OpenTelemetry Collector in the local cluster and Kubernetes environments
4. Connect Collector outputs to Tempo, Loki, and Prometheus-compatible metrics flow
5. Create initial Grafana dashboards for API latency, auth flows, policy decisions, and agent execution paths
6. Introduce telemetry schema linting or review checks to prevent label and attribute drift

### Timeline

- **Phase 1:** tracing instrumentation for backend services and Collector deployment
- **Phase 2:** log pipeline into Loki with Grafana trace-log correlation
- **Phase 3:** metrics standardisation, dashboards, and alert baselines
- **Phase 4:** sampling, retention, and multi-tenant telemetry governance hardening

---

## Validation

### Success Metrics

- A single request can be followed in Grafana from ingress to downstream service calls using trace correlation
- Authentication, authorisation, and policy-evaluation failures can be diagnosed from correlated traces and logs without ad hoc shell access to pods
- New services can adopt the observability stack without bespoke backend-specific code
- Telemetry pipelines do not emit secrets or high-risk sensitive data in routine operation
- Collector and backend failure modes are visible and alertable

### Monitoring

- Collector ingest rate, queue depth, dropped telemetry count, and export failure rate
- Tempo trace ingest and query latency
- Loki log ingest errors and label-cardinality pressure
- Prometheus scrape health and time-series churn
- End-to-end trace coverage for critical flows such as login, token exchange, policy checks, and agent run orchestration

---

## Related Decisions

- [ADR-0004](0004-dependency-injected-logging.md) - Defines the structured logging pattern and log-trace correlation model this ADR builds on
- [ADR-0011](0011-universal-gateway-policy-enforcement.md) - Policy enforcement flows require end-to-end telemetry and decision visibility
- [ADR-0015](0015-service-mesh-and-network-enforcement-infrastructure.md) - Kubernetes traffic topology affects where telemetry is collected and correlated
- [ADR-0016](0016-local-development-environment.md) - Local environment strategy should be extended to include observability validation in the cluster tier

---

## References

- [OpenTelemetry](https://opentelemetry.io/)
- [OpenTelemetry Collector](https://opentelemetry.io/docs/collector/)
- [Prometheus](https://prometheus.io/)
- [Loki](https://grafana.com/oss/loki/)
- [Tempo](https://grafana.com/oss/tempo/)
- [Grafana](https://grafana.com/)

---

## Notes

This ADR intentionally selects a **collector-centric architecture** rather than binding Orbit directly to the Grafana stack. The Grafana stack is the default backend choice, but the lasting decision is that Orbit emits standard telemetry and delegates transport, processing, and backend routing to OpenTelemetry Collector infrastructure.
