# Observability

OpenTelemetry-based observability for Orbit, providing distributed tracing, structured logging, and automatic log-trace correlation.

## Overview

Orbit implements OpenTelemetry-based distributed tracing with automatic log correlation. This provides complete visibility into agent request execution paths and performance characteristics.

Orbit is a multi-service backend: it compiles to a set of binaries under `backend/cmd/` (the main API is `cmd/identity`; the full set is listed in the [binary table](../code-organization.md#binaries-backendcmd)), all of which share the `internal/platform/observability` layer and emit OTel telemetry. Examples below use `cmd/identity` (the main API, listening on `:8080`), but the same instrumentation applies to every service.

## Documentation Structure

- **[README.md](README.md)** - This file: architecture overview and quick start
- **[configuration.md](configuration.md)** - Environment variables and configuration options
- **[tracing.md](tracing.md)** - Distributed tracing implementation details
- **[logging.md](logging.md)** - Structured logging and log-trace correlation

## Architecture

### Components

1. **Observability Package** (`internal/platform/observability/`)
   - OpenTelemetry setup and configuration
   - OTLP exporter for production (sends to collectors like Jaeger, Tempo, etc.)
   - Stdout exporter for development
   - W3C TraceContext propagation for distributed tracing

2. **Tracing Middleware** (`internal/platform/middleware/tracing.go`)
   - Extracts trace context from incoming HTTP request headers
   - Creates a span for each HTTP request
   - Injects span into request context for downstream use
   - Records HTTP attributes (method, route, status, etc.)
   - Marks spans as error for 4xx/5xx responses

3. **Logging Middleware** (`internal/platform/middleware/logging.go`)
   - Logs incoming requests and responses
   - Automatically includes trace_id and span_id from context
   - Enables log-trace correlation

4. **Structured Logger** (`internal/platform/logging/`)
   - Extracts trace context from Go context
   - Automatically adds trace_id and span_id to log entries
   - Uses zerolog for structured logging

## Configuration

Service identity (name, version) comes from each binary's YAML config via `observability.serviceName` and `app.version`, and the environment is selected by `APP_ENV`. The shared observability tunables load from `internal/platform/observability/config/base.yaml` (plus its `{APP_ENV}.yaml` overlay) and can be overridden per-process with these environment variables:

```bash
# Environment selector — picks which config overlay loads and sets the
# deployment.environment resource attribute.
# Values: "local" | "development" | "production" (default: "production")
APP_ENV=local

# Trace export (override the YAML-loaded platform defaults)
ORBIT_OTLP_ENDPOINT=http://localhost:4317   # Collector URL (empty = export disabled)
                                            # http:// is plaintext, https:// is TLS

# Sampling & logging
ORBIT_SAMPLING_RATE=0.1              # Fraction of root spans to sample (0.0–1.0)
ORBIT_LOG_FORMAT=console             # Log output style: "console" or "json"
```

### Exporter selection

The trace exporter is chosen at startup, in priority order:

1. **OTLP endpoint set** (`ORBIT_OTLP_ENDPOINT` non-empty): export to the OTLP collector (Jaeger, Tempo, …) in *any* environment. The endpoint's scheme sets the transport: `http://` is plaintext, `https://` is TLS. An endpoint without a scheme is rejected at startup.
2. **`APP_ENV=local`, no endpoint**: stdout exporter, for laptop development.
3. **Anything else with no endpoint** is a no-op: spans are created but not exported; logs still carry `trace_id`/`span_id` for correlation. This covers the development cluster until a collector is deployed, and any production misconfiguration.

## Usage

### Automatic HTTP Tracing

All HTTP requests are automatically traced via middleware:

```go
// In cmd/identity/main.go
server := apiServer.Server(
    ctx,
    port,
    basePath,
    mw.MiddlewareChain(
        mw.TracingMiddleware("orbit"),      // Creates spans
        mw.LoggingMiddleware(logger),       // Logs with trace context
    ),
    controllers...,
)
```

### Automatic Log-Trace Correlation

Logs automatically include trace context when using the context-aware logger:

```go
// In any handler or service
logger.Info(ctx, "Processing request", 
    logging.Field{Key: "user_id", Value: userID},
)

// Output includes trace_id and span_id:
// {"level":"info","msg":"Processing request","user_id":"123",
//  "trace_id":"2482aff647d2da4b929d57bc10bcfa74",
//  "span_id":"85ad5c6de00215ea"}
```

### Manual Span Creation

For detailed tracing within services:

```go
import "go.opentelemetry.io/otel"

func (s *Service) ProcessData(ctx context.Context, data Data) error {
    tracer := otel.Tracer("orbit")
    ctx, span := tracer.Start(ctx, "Service.ProcessData")
    defer span.End()
    
    // Add custom attributes
    span.SetAttributes(
        attribute.String("data.type", data.Type),
        attribute.Int("data.size", len(data.Items)),
    )
    
    // Your business logic here
    // Pass ctx to downstream calls for trace propagation
    
    return nil
}
```

See [tracing.md](tracing.md) for detailed usage patterns.

## Trace Propagation

Orbit uses **W3C TraceContext** standard for trace propagation:

1. **Incoming requests**: `TracingMiddleware` extracts trace context from the `traceparent` header and starts a `SpanKindServer` span
2. **Downstream calls**: `httpclient.Base.Do()` automatically injects `traceparent`/`tracestate` headers and opens a `SpanKindClient` child span. Never do this manually
3. **Cross-service**: Trace context flows automatically across all service boundaries via the platform client layer

Example propagation header:
```
traceparent: 00-2482aff647d2da4b929d57bc10bcfa74-85ad5c6de00215ea-01
```

## Observability Stack Recommendations

### Local Development

Use Jaeger all-in-one:

```bash
docker run -d --name jaeger \
  -p 4317:4317 \
  -p 16686:16686 \
  jaegertracing/all-in-one:latest

export ORBIT_OTLP_ENDPOINT=http://localhost:4317
```

Access UI at http://localhost:16686

### Production

Recommended stack:
- **Grafana Tempo**: Trace storage backend
- **Grafana**: Visualization and correlation
- **Loki**: Log aggregation (correlates with traces)
- **Prometheus**: Metrics (future)

All three systems integrate seamlessly with trace_id correlation.

## Benefits for Orbit

As a control plane for autonomous agents, observability is critical:

1. **Agent Request Tracking**: Every agent request gets a unique trace_id
2. **Policy Evaluation Visibility**: See when/why policies are evaluated
3. **Credential Access Auditing**: Track credential broker operations
4. **Performance Analysis**: Identify bottlenecks in agent workflows
5. **Error Investigation**: Quickly find all logs related to a failed request
6. **Distributed Debugging**: Trace requests across multiple services

## Implementation Status

✅ **Completed**:
- OpenTelemetry setup and configuration
- HTTP request tracing via middleware (`SpanKindServer` span per inbound request)
- Automatic log-trace correlation (`trace_id`/`span_id` on every log line)
- W3C TraceContext propagation on all inbound and outbound calls
- Development and production modes
- OTLP exporter support
- Service-to-service client tracing via `httpclient.Base.Do()` (`SpanKindClient` child spans with OTel HTTP semantic conventions)
- Upstream error body capture in `error.upstream_body` span attribute for non-2xx responses
- [opentelemetry-observability-discipline](../adr/2026-03-21-opentelemetry-observability-discipline.md) lint rules enforcing static span status descriptions and correct `RecordError` usage

## Quick Start

### 1. Development Mode (Stdout)

```bash
# Start Orbit with stdout tracing
APP_ENV=local go run cmd/identity/main.go

# Make a request
curl http://localhost:8080/api/v1/example/health

# See trace output in terminal
```

### 2. Production Mode with Jaeger

```bash
# Start Jaeger
docker run -d -p4317:4317 -p16686:16686 jaegertracing/all-in-one:latest

# Configure Orbit
export APP_ENV=production
export ORBIT_OTLP_ENDPOINT=http://localhost:4317

# Start Orbit
go run cmd/identity/main.go

# Make requests
curl http://localhost:8080/api/v1/example/hello/world

# View traces at http://localhost:16686
```

### 3. Exploring Traces and Logs

**Find traces by service:**
- Open Jaeger UI
- Select service: the binary's configured `observability.serviceName` (e.g. `orbit-identity`)
- View recent traces

**Find logs for a trace:**
- Copy `trace_id` from trace span
- Search logs: `grep "trace_id=2482aff647d2da4b..." logs/orbit.log`
- See all logs for that request

**Find traces from logs:**
- Find error in logs with `trace_id`
- Search Jaeger with that trace_id
- View complete request flow

## Related Documentation

- **[configuration.md](configuration.md)** - Complete configuration reference
- **[tracing.md](tracing.md)** - Distributed tracing deep dive
- **[logging.md](logging.md)** - Structured logging guide
- [dependency-injected-logging](../adr/2026-02-17-dependency-injected-logging.md)
- [opentelemetry-observability-discipline](../adr/2026-03-21-opentelemetry-observability-discipline.md)
- [OpenTelemetry Go Documentation](https://opentelemetry.io/docs/instrumentation/go/)
- [W3C TraceContext Specification](https://www.w3.org/TR/trace-context/)
