# Monitor runkora

In runkora, every service emits structured logs, OpenTelemetry traces and Prometheus metrics. The console shows what runs, models and triggers are doing. kora does not ship dashboards, alerts or a collector. You send its telemetry to the observability stack you already run, and build alerts there.

## Export traces and metrics

Point kora at an OpenTelemetry collector with the chart's `global.observability` values:

```yaml
global:
  observability:
    otlpEndpoint: http://otel-collector.observability:4317
    samplingRate: "0.1"
    collectorNamespace: observability
```

- **`otlpEndpoint`:** an OTLP gRPC endpoint. kora does not support OTLP over HTTP. The scheme is required, with `http://` for plaintext and `https://` for TLS. The endpoint receives traces and metrics from every service and agent pod.
- **`samplingRate`:** the share of traces kept, from 0 to 1. Empty keeps the production default of 0.1.
- **`collectorNamespace`:** the namespace of an in-cluster collector. kora opens egress to that namespace on the endpoint's port, and without it the default-deny policy drops the export.

Runs on Claude Code export the harness's own traces, metrics and logs to the same endpoint, tagged with the run and tenant they belong to.

kora propagates W3C `traceparent` headers. Export your mesh's spans to the same collector to see the proxy hop and the application span in one trace.

For a collector outside the cluster, leave `collectorNamespace` empty, add its CIDR and port to `networkPolicy.externalServices`, and register its host with the mesh in a `ServiceEntry`.

The collector is the only external host an agent pod can reach. On a shared SaaS endpoint, a prompt-injected agent could export data to an attacker's account on the same service. To prevent this, run an in-cluster collector that forwards to the external one, and point `otlpEndpoint` at it. See [agent pod egress lockdown](../adr/2026-09-30-agent-pod-egress-lockdown.md).

## Scrape metrics

Each kora service serves Prometheus metrics on port 9464 at `/metrics`. If you run the Prometheus Operator, turn on a `ServiceMonitor` for each service:

```yaml
global:
  metrics:
    enabled: true
    scraperNamespace: monitoring
    serviceMonitor:
      enabled: true
```

The scraper must run in the mesh, in the namespace set by `scraperNamespace`. To scrape kora-triggers, add the scraper's mesh identity to `triggers.authorizationPolicy.metricsScraperPrincipals` as `ns/<namespace>/sa/<serviceaccount>`. If your Prometheus Operator selects ServiceMonitors by label, set the label in `global.metrics.serviceMonitor.additionalLabels`.

The kora operator has no ServiceMonitor. It serves controller metrics on port 8080 at `/metrics`, and its pods carry `prometheus.io/*` annotations for scrapers that discover targets that way.

| Metric | What it measures |
| --- | --- |
| `kora_<service>_http_requests_total` | Requests to each service, by route, method and status |
| `kora_<service>_http_request_duration_seconds` | Request latency for each service, by route |
| `kora_triggers_queue_depth` | Trigger deliveries waiting and claimed |
| `kora_triggers_queue_oldest_unclaimed_age_seconds` | How long the oldest waiting delivery has waited |
| `kora_triggers_auto_pauses_total` | Triggers paused after repeated failures |
| `kora_redis_available` | Whether each service can reach Redis |

Run outcomes, model spend and policy decisions are not metrics yet. They are in the logs, traces and the console.

## Read the logs

kora logs JSON to stdout. A line logged inside a traced request carries the trace's `trace_id`. These messages are worth searching or alerting on:

| Message | Means |
| --- | --- |
| `egress denied by policy` | A request through a connection was refused by policy |
| `scope enforcement: denied` | A request was refused for a missing scope |
| `tools/call denied` | An MCP tool call was refused |
| `reaped stale runs` | Runs with no activity for 30 minutes were closed |
| `async provisioning failed` | An agent's workload could not be created |

## Check health

Check the kora services with `kubectl -n kora-system get pods`. Each tenant's agents and sandboxes run in its own namespace, `kora-tenant-<tenant>` by default. An agent runs as a StatefulSet named after its slug. Each sandbox is a pod named `sandbox-<run>`.

The resources platform teams register report whether kora accepted them:

| Resource | Healthy when |
| --- | --- |
| `KoraTenant` | Phase is `Ready` |
| `MCPServer` | Phase is `Aggregated` |
| `SandboxImage` | `Valid` is true |
| `InferenceProvider` | Phase is `Ready` |
| `Agent`, `Environment`, `PolicyRole`, `Trigger`, `TriggerSource`, `VirtualModel` | `Ready` is true |

## Watch runs

A run that stops early records why. `budget_exhausted` is its own state. Errors such as `sandbox_lost` or `loop_detected` appear in the run's events on its page, or through `GET /api/v1/runs/{id}/events`. kora closes a run that has made no progress for 30 minutes.

In the console, **Model Gateway** shows requests, spend, latency and error rates, with a log of every model call. **Triggers** shows every delivery and its outcome. **Policy** → **Evaluate** explains why a principal can or cannot reach something.
