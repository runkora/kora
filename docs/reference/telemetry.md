# Observability Configuration

Shared observability tunables load from the `internal/platform/observability` config module: `config/base.yaml`, its `{APP_ENV}.yaml` overlay and, in a chart install, the rendered `/etc/kora/config/internal/platform/observability.yaml`. *Reference: See `internal/platform/observability/config.go`.*

## Service Identity

Service name and version come from each binary's own config:

```yaml
# backend/cmd/<binary>/config/base.yaml
app:
  version: "0.1.0"           # → service.version resource attribute
observability:
  serviceName: orbit-identity # → service.name resource attribute
```

`APP_ENV` selects the config overlay and sets the `deployment.environment` resource attribute. It is `local` or `production`, and defaults to `production` when unset.

## Config Keys

*   **`otlpEndpoint`:** The OTLP collector's gRPC URL. `http://` is plaintext and `https://` is TLS. An endpoint without a scheme fails startup, and an empty endpoint disables OTLP export.
*   **`samplingRate`:** The fraction of root spans sampled, from 0.0 to 1.0. It is 0.1 in `production.yaml` and 1.0 in `local.yaml`.
*   **`logFormat`:** `console` or `json`. It is `json` in `production.yaml` and `console` in `local.yaml`.

The `kora` chart renders these keys from `global.observability.otlpEndpoint`, `global.observability.samplingRate` and `global.logFormat`:

```yaml
global:
  logFormat: json
  observability:
    otlpEndpoint: https://tempo.monitoring.svc.cluster.local:4317
    samplingRate: "0.1"
```

A single component overrides them through its `configOverrides`:

```yaml
policyService:
  configOverrides:
    internal/platform/observability:
      samplingRate: 1.0
```

The session-manager and the identity-proxy sidecar have no rendered file. kora-agents and the operator pass them the endpoint and the sampling rate as `KORA_OTLP_ENDPOINT` and `KORA_SAMPLING_RATE`.

## Configuration Modes

The trace exporter is chosen at startup in priority order: an OTLP endpoint always wins; otherwise `APP_ENV=local` gets the stdout exporter; everything else with no endpoint is no-op.

### Local Mode (stdout)

**Characteristics:**
- Exports traces to stdout with pretty-printing
- Higher default sampling rate (100% via the `local` overlay)
- No external dependencies required
- Best for local development and debugging

**Configuration:** `APP_ENV=local` with no `otlpEndpoint`.

**Output Example:**
```json
{
  "Name": "GET /api/v1/example/health",
  "SpanContext": {
    "TraceID": "2482aff647d2da4b929d57bc10bcfa74",
    "SpanID": "85ad5c6de00215ea"
  },
  "Attributes": [
    {"Key": "http.method", "Value": "GET"},
    {"Key": "http.route", "Value": "/api/v1/example/health"},
    {"Key": "http.status_code", "Value": 200}
  ]
}
```

### OTLP Mode (any environment)

**Characteristics:**
- Exports to an OTLP collector (Jaeger, Tempo, etc.)
- Triggered whenever `otlpEndpoint` is set, in any `APP_ENV`
- Configurable sampling rate
- TLS when the endpoint is `https://`, plaintext when it is `http://`
- Batched export for efficiency

**Configuration:** Set `global.observability.otlpEndpoint` in the chart values.

### No-Op Mode

**Characteristics:**
- Spans created but not exported
- Minimal overhead
- Logs still include trace_id/span_id for correlation
- Fallback when no collector is configured

**Triggered when:**
- `APP_ENV` is not `local` AND
- `otlpEndpoint` is empty

## Configuration Best Practices

*   **Development:** The `local` overlay samples every trace and logs in the console format.
*   **Staging:** A higher `global.observability.samplingRate` (e.g. `"0.5"`) catches issues before production. `http://` keeps the hop plaintext for a collector reached inside a trusted network or over a service mesh that encrypts it.
*   **Production:** A `samplingRate` of `"0.1"` balances observability with cost. Use `https://` unless a service mesh already encrypts the hop and the collector serves plaintext.
*   **High-traffic production:** A `samplingRate` of `"0.01"` samples 1% of traces.

## Validation

Check a workload's rendered values with `kubectl -n orbit-system get configmap orbit-identity-config -o yaml`, and its startup logs for "Observability initialized".

## Troubleshooting

### No traces appearing

**Check:**
1. Is `APP_ENV` set correctly (stdout export requires `local`)?
2. For OTLP export, is `global.observability.otlpEndpoint` set?
3. Is the OTLP collector reachable?
4. Check the sampling rate (`global.observability.samplingRate`): it might be too low

### TLS connection errors

**Solutions:**
1. Verify TLS certificates are valid
2. For a collector serving plaintext, use an `http://` endpoint
3. Check that endpoint uses correct port (4317 for gRPC, 4318 for HTTP)

### High overhead

**Solutions:**
1. Reduce the sampling rate: `global.observability.samplingRate: "0.01"`
2. Verify batch processor settings (code-level)
3. Consider tail-based sampling (future)

## Related Documentation

- [Tracing Implementation](../contributing/tracing.md)
- [Logging Implementation](../contributing/logging.md)
- [OpenTelemetry Environment Variables](https://opentelemetry.io/docs/specs/otel/configuration/sdk-environment-variables/)
