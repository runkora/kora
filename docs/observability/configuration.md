# Observability Configuration

## Environment Variables

All observability configuration is controlled via environment variables following OpenTelemetry conventions.

### Service Identity

```bash
# Service name (shown in traces and metrics)
OTEL_SERVICE_NAME=orbit
# Default: "orbit"

# Service version (for version-specific analysis)
OTEL_SERVICE_VERSION=0.1.0
# Default: "dev"

# Deployment environment
ENVIRONMENT=production
# Values: "development" | "production"
# Default: "development"
```

### Trace Export

```bash
# OTLP collector endpoint
OTEL_EXPORTER_OTLP_ENDPOINT=localhost:4317
# Required for production mode
# Default: none (uses stdout in development)

# Use insecure connection (no TLS)
OTEL_EXPORTER_OTLP_INSECURE=true
# Values: "true" | "false"
# Default: "false" (TLS enabled)
# ⚠️ Only use in development/testing
```

### Sampling

```bash
# Trace sampling rate (0.0 to 1.0)
OTEL_TRACE_SAMPLING_RATE=0.1
# Default: 0.1 (10% of traces)
# Values: 
#   1.0 = sample all traces (development/debugging)
#   0.1 = sample 10% (recommended production)
#   0.01 = sample 1% (high-traffic production)
```

## Configuration Modes

### Development Mode

**Characteristics:**
- Exports traces to stdout with pretty-printing
- Higher default sampling rate (10%)
- No external dependencies required
- Best for local development and debugging

**Configuration:**
```bash
ENVIRONMENT=development
OTEL_SERVICE_NAME=orbit
OTEL_TRACE_SAMPLING_RATE=1.0  # Sample everything in dev
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

### Production Mode

**Characteristics:**
- Exports to OTLP collector (Jaeger, Tempo, etc.)
- Configurable sampling rate
- TLS-secured connections
- Batched export for efficiency

**Configuration:**
```bash
ENVIRONMENT=production
OTEL_SERVICE_NAME=orbit
OTEL_SERVICE_VERSION=1.0.0
OTEL_EXPORTER_OTLP_ENDPOINT=tempo.monitoring.svc.cluster.local:4317
OTEL_TRACE_SAMPLING_RATE=0.1
```

### No-Op Mode

**Characteristics:**
- Spans created but not exported
- Minimal overhead
- Logs still include trace_id/span_id for correlation
- Fallback when OTLP endpoint unavailable

**Triggered when:**
- `ENVIRONMENT=production` AND
- `OTEL_EXPORTER_OTLP_ENDPOINT` is not set

## Configuration Best Practices

### Development

```bash
# .env.development
ENVIRONMENT=development
OTEL_SERVICE_NAME=orbit
OTEL_TRACE_SAMPLING_RATE=1.0
```

Use stdout exporter to see traces directly in logs. This is perfect for understanding how tracing works and debugging trace propagation.

### Staging

```bash
# .env.staging
ENVIRONMENT=production
OTEL_SERVICE_NAME=orbit-staging
OTEL_SERVICE_VERSION=1.0.0-rc1
OTEL_EXPORTER_OTLP_ENDPOINT=jaeger-collector.staging:4317
OTEL_EXPORTER_OTLP_INSECURE=true  # If using internal network
OTEL_TRACE_SAMPLING_RATE=0.5
```

Higher sampling rate (50%) to catch issues before production. Can use insecure connection if within trusted network.

### Production

```bash
# .env.production
ENVIRONMENT=production
OTEL_SERVICE_NAME=orbit
OTEL_SERVICE_VERSION=1.0.0
OTEL_EXPORTER_OTLP_ENDPOINT=tempo.monitoring.svc.cluster.local:4317
OTEL_TRACE_SAMPLING_RATE=0.1
```

Lower sampling rate (10%) to balance observability with cost. Always use TLS in production.

### High-Traffic Production

```bash
# For very high request volumes
OTEL_TRACE_SAMPLING_RATE=0.01  # 1% sampling
```

Consider tail-based sampling (future enhancement) for better trace selection.

## Validation

### Verify Configuration

```bash
# Start Orbit and check logs for observability setup
./orbit

# Expected output:
# "Observability initialized" with tracer and exporter info
```

### Test Trace Export

```bash
# Development mode - check stdout for trace spans
ENVIRONMENT=development ./orbit

# Production mode - verify connection to collector
ENVIRONMENT=production \
OTEL_EXPORTER_OTLP_ENDPOINT=localhost:4317 \
OTEL_EXPORTER_OTLP_INSECURE=true \
./orbit
```

## Troubleshooting

### No traces appearing

**Check:**
1. Is `ENVIRONMENT` set correctly?
2. In production, is `OTEL_EXPORTER_OTLP_ENDPOINT` configured?
3. Is the OTLP collector reachable?
4. Check sampling rate - might be too low

### TLS connection errors

**Solutions:**
1. Verify TLS certificates are valid
2. For internal testing, use `OTEL_EXPORTER_OTLP_INSECURE=true`
3. Check that endpoint uses correct port (4317 for gRPC, 4318 for HTTP)

### High overhead

**Solutions:**
1. Reduce sampling rate: `OTEL_TRACE_SAMPLING_RATE=0.01`
2. Verify batch processor settings (code-level, not env vars)
3. Consider tail-based sampling (future)

## Related Documentation

- [Tracing Implementation](tracing.md)
- [Logging Implementation](logging.md)
- [OpenTelemetry Environment Variables](https://opentelemetry.io/docs/specs/otel/configuration/sdk-environment-variables/)
