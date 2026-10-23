# Monitor kora

In kora, every service emits structured logs, OpenTelemetry traces and Prometheus metrics. The console shows what runs, models and triggers are doing. kora does not ship dashboards, alerts or a collector. You send its telemetry to the observability stack you already run, and build alerts there.

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
- **`collectorNamespace`:** the namespace of an in-cluster collector. kora opens the network path to it. For a collector outside the cluster, list its address under `networkPolicy.externalServices` instead.

Runs on Claude Code export the harness's own traces, metrics and logs to the same endpoint, tagged with the run and tenant they belong to.

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

The scraper must run in the mesh, in the namespace set by `scraperNamespace`. The kora operator serves its controller metrics on port 8080.

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
| `egress denied by policy` | A sandbox request to an external host was blocked |
| `scope enforcement: denied` | A request was refused for a missing scope |
| `tools/call denied` | An MCP tool call was refused |
| `reaped stale runs` | Runs with no activity for 30 minutes were closed |
| `async provisioning failed` | An agent's workload could not be created |

## Check health

Check the kora services with `kubectl -n kora-system get pods`. Each tenant's agents and sandboxes run in its own namespace, `kora-tenant-<tenant>` by default. An agent runs as a StatefulSet named after its slug. Each sandbox is a pod named `sandbox-<run>`.

The resources platform teams register report whether kora accepted them:

| Resource | Healthy when |
| --- | --- |
| Tenant | Phase is `Ready` |
| `MCPServer` | Phase is `Aggregated` |
| `SandboxImage` | `Valid` is true |
| `InferenceProvider` | Phase is `Ready` |

## Watch runs

A run that stops early records why. `budget_exhausted` is its own state. Errors such as `sandbox_lost` or `loop_detected` appear in the run's events on its page, or through `GET /api/v1/runs/{id}/events`. kora closes a run that has made no progress for 30 minutes.

In the console, **Model Gateway** shows requests, spend, latency and error rates, with a log of every model call. **Triggers** shows every delivery and its outcome. **Policy** → **Evaluate** explains why a principal can or cannot reach something.
