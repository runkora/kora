# Observability Configuration

## Environment Variables

Shared observability tunables load from `internal/platform/observability/config/base.yaml` and its `{APP_ENV}.yaml` overlay. A handful of environment variables override those YAML values per-process (they are the ones the code actually reads; see `internal/platform/observability/config.go`). Service identity (name and version) is **not** an environment variable; it comes from each binary's YAML config.

### Service Identity

Service name and version come from the binary's own config, not the environment:

```yaml
# backend/cmd/<binary>/config/base.yaml
app:
  version: "0.1.0"           # → service.version resource attribute
observability:
  serviceName: orbit-identity # → service.name resource attribute
```

The deployment environment comes from `APP_ENV`:

```bash
# Environment selector: picks the config overlay and sets the
# deployment.environment resource attribute.
APP_ENV=production
# Values: "local" | "development" | "production"
# Default: "production" (when APP_ENV is unset or empty)
```

### Trace Export

```bash
# OTLP collector endpoint (gRPC)
ORBIT_OTLP_ENDPOINT=localhost:4317
# Empty = OTLP export disabled
# Default: from YAML (empty in base — no export unless APP_ENV=local)

# Disable TLS on the OTLP connection
ORBIT_OTLP_INSECURE=true
# Values: "true" / "1" enable insecure; anything else keeps TLS on
# Default: false (TLS enabled)
# ⚠️ Only use in development/testing
```

### Sampling & Logging

```bash
# Trace sampling rate (0.0 to 1.0) — fraction of root spans sampled
ORBIT_SAMPLING_RATE=0.1
# Default: from YAML (0.1 in base, 1.0 in local); 0.1 as a code fallback
# Values:
#   1.0 = sample all traces (development/debugging)
#   0.1 = sample 10% (recommended production)
#   0.01 = sample 1% (high-traffic production)

# Log output style
ORBIT_LOG_FORMAT=console
# Values: "console" | "json"
# Default: from YAML ("json" in base, "console" in local)
```

## Configuration Modes

The trace exporter is chosen at startup in priority order: an OTLP endpoint always wins; otherwise `APP_ENV=local` gets the stdout exporter; everything else with no endpoint is no-op.

### Local Mode (stdout)

**Characteristics:**
- Exports traces to stdout with pretty-printing
- Higher default sampling rate (100% via the `local` overlay)
- No external dependencies required
- Best for local development and debugging

**Configuration:**
```bash
APP_ENV=local
ORBIT_SAMPLING_RATE=1.0  # Sample everything in dev (already the local default)
```

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
- Triggered whenever `ORBIT_OTLP_ENDPOINT` is set, in any `APP_ENV`
- Configurable sampling rate
- TLS-secured connections unless `ORBIT_OTLP_INSECURE=true`
- Batched export for efficiency

**Configuration:**
```bash
APP_ENV=production
ORBIT_OTLP_ENDPOINT=tempo.monitoring.svc.cluster.local:4317
ORBIT_SAMPLING_RATE=0.1
```

### No-Op Mode

**Characteristics:**
- Spans created but not exported
- Minimal overhead
- Logs still include trace_id/span_id for correlation
- Fallback when no collector is configured

**Triggered when:**
- `APP_ENV` is not `local` (e.g. `development` or `production`) AND
- `ORBIT_OTLP_ENDPOINT` is not set

## Configuration Best Practices

Service name and version live in each binary's YAML config, so the examples below only set the environment-driven overrides.

### Development

```bash
# .env (local development)
APP_ENV=local
ORBIT_SAMPLING_RATE=1.0
ORBIT_LOG_FORMAT=console
```

Use the stdout exporter to see traces directly in logs. This is perfect for understanding how tracing works and debugging trace propagation.

### Staging

```bash
APP_ENV=production
ORBIT_OTLP_ENDPOINT=jaeger-collector.staging:4317
ORBIT_OTLP_INSECURE=true  # If using internal network
ORBIT_SAMPLING_RATE=0.5
```

Higher sampling rate (50%) to catch issues before production. Can use insecure connection if within trusted network.

### Production

```bash
APP_ENV=production
ORBIT_OTLP_ENDPOINT=tempo.monitoring.svc.cluster.local:4317
ORBIT_SAMPLING_RATE=0.1
```

Lower sampling rate (10%) to balance observability with cost. Always use TLS in production (leave `ORBIT_OTLP_INSECURE` unset).

### High-Traffic Production

```bash
# For very high request volumes
ORBIT_SAMPLING_RATE=0.01  # 1% sampling
```

Consider tail-based sampling (future enhancement) for better trace selection.

## Validation

### Verify Configuration

```bash
# Start a binary and check logs for observability setup
APP_ENV=local go run ./cmd/identity

# Expected output:
# "Observability initialized" with tracer and exporter info
```

### Test Trace Export

```bash
# Local mode - check stdout for trace spans
APP_ENV=local go run ./cmd/identity

# OTLP mode - verify connection to collector
APP_ENV=production \
ORBIT_OTLP_ENDPOINT=localhost:4317 \
ORBIT_OTLP_INSECURE=true \
go run ./cmd/identity
```

## Troubleshooting

### No traces appearing

**Check:**
1. Is `APP_ENV` set correctly (stdout export requires `local`)?
2. For OTLP export, is `ORBIT_OTLP_ENDPOINT` configured?
3. Is the OTLP collector reachable?
4. Check sampling rate (`ORBIT_SAMPLING_RATE`): might be too low

### TLS connection errors

**Solutions:**
1. Verify TLS certificates are valid
2. For internal testing, use `ORBIT_OTLP_INSECURE=true`
3. Check that endpoint uses correct port (4317 for gRPC, 4318 for HTTP)

### High overhead

**Solutions:**
1. Reduce sampling rate: `ORBIT_SAMPLING_RATE=0.01`
2. Verify batch processor settings (code-level, not env vars)
3. Consider tail-based sampling (future)

## Related Documentation

- [Tracing Implementation](tracing.md)
- [Logging Implementation](logging.md)
- [OpenTelemetry Environment Variables](https://opentelemetry.io/docs/specs/otel/configuration/sdk-environment-variables/)
