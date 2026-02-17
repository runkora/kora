# Observability

OpenTelemetry-based observability for Orbit, providing distributed tracing, structured logging, and automatic log-trace correlation.

## Overview

Orbit implements OpenTelemetry-based distributed tracing with automatic log correlation. This provides complete visibility into agent request execution paths and performance characteristics.

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

Observability is configured via environment variables:

```bash
# Service identification
OTEL_SERVICE_NAME=orbit                    # Service name (default: "orbit")
OTEL_SERVICE_VERSION=0.1.0                 # Service version (default: "dev")
ENVIRONMENT=production                     # Environment (default: "development")

# Trace export
OTEL_EXPORTER_OTLP_ENDPOINT=localhost:4317 # OTLP collector endpoint (optional)
OTEL_EXPORTER_OTLP_INSECURE=true          # Use insecure connection (dev only)

# Sampling
OTEL_TRACE_SAMPLING_RATE=0.1              # Sample 10% of traces (default: 0.1)
```

### Environment Modes

**Development** (`ENVIRONMENT=development`):
- Exports traces to stdout with pretty-printing
- High sampling rate (10% default)
- Useful for local debugging

**Production** (`ENVIRONMENT=production`):
- Requires `OTEL_EXPORTER_OTLP_ENDPOINT` to be set
- Exports to OTLP collector (Jaeger, Tempo, etc.)
- Uses TLS by default
- Configurable sampling rate

**Production without OTLP** (no endpoint configured):
- No-op mode: spans are created but not exported
- Logs still include trace_id/span_id for correlation
- Minimal overhead

## Usage

### Automatic HTTP Tracing

All HTTP requests are automatically traced via middleware:

```go
// In cmd/orbit/main.go
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

1. **Incoming requests**: TracingMiddleware extracts trace context from `traceparent` header
2. **Downstream calls**: Use the request context when making HTTP calls to propagate traces
3. **Cross-service**: Trace context flows automatically across service boundaries

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

export OTEL_EXPORTER_OTLP_ENDPOINT=localhost:4317
export OTEL_EXPORTER_OTLP_INSECURE=true
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
- HTTP request tracing via middleware
- Automatic log-trace correlation
- W3C TraceContext propagation
- Development and production modes
- OTLP exporter support

🚧 **Future Enhancements**:
- Database query tracing
- gRPC service tracing
- Agent tool invocation tracing
- Policy evaluation detailed spans
- Metrics integration (OpenTelemetry Metrics)
- Tail-based sampling for intelligent trace selection

## Quick Start

### 1. Development Mode (Stdout)

```bash
# Start Orbit with stdout tracing
ENVIRONMENT=development go run cmd/orbit/main.go

# Make a request
curl http://localhost:6069/api/v1/example/health

# See trace output in terminal
```

### 2. Production Mode with Jaeger

```bash
# Start Jaeger
docker run -d -p4317:4317 -p16686:16686 jaegertracing/all-in-one:latest

# Configure Orbit
export ENVIRONMENT=production
export OTEL_EXPORTER_OTLP_ENDPOINT=localhost:4317
export OTEL_EXPORTER_OTLP_INSECURE=true

# Start Orbit
go run cmd/orbit/main.go

# Make requests
curl http://localhost:6069/api/v1/example/hello/world

# View traces at http://localhost:16686
```

### 3. Exploring Traces and Logs

**Find traces by service:**
- Open Jaeger UI
- Select service: `orbit`
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
- [ADR-0004: OTEL-Compatible Dependency-Injected Logging](../adr/0004-dependency-injected-logging.md)
- [OpenTelemetry Go Documentation](https://opentelemetry.io/docs/instrumentation/go/)
- [W3C TraceContext Specification](https://www.w3.org/TR/trace-context/)
