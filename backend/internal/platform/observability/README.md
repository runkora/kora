# Observability Package

OpenTelemetry tracing initialization and configuration for kora.

## Overview

This package provides centralized OTEL setup with environment-based configuration. It handles:
- Tracer provider initialization
- Exporter selection (stdout for dev, OTLP for production)
- Resource attribution (service name, version, environment)
- Graceful shutdown with span flushing

## Quick Start

```go
import "github.com/clear-route/orbit/internal/platform/observability"

func main() {
    ctx := context.Background()
    
    // Service name and version are the binary's; the rest comes from config.
    shutdown, err := observability.Setup(ctx, observability.Config{
        ServiceName:    "my-service",
        ServiceVersion: version,
        Environment:    config.Environment(),
    })
    if err != nil {
        log.Fatalf("Failed to setup observability: %v", err)
    }
    defer shutdown(ctx)
    
    // Now use OTEL tracers and loggers normally
    tracer := otel.Tracer("my-service")
    ctx, span := tracer.Start(ctx, "operation")
    defer span.End()
    
    logger.Info(ctx, "Processing request") // Automatically includes trace_id
}
```

## Configuration

The package loads the `internal/platform/observability` config module: `config/base.yaml`, the `{APP_ENV}.yaml` overlay and the file the chart renders at `/etc/orbit/config/internal/platform/observability.yaml`. `APP_ENV` defaults to `production` and also sets the `deployment.environment` attribute.

*   **`otlpEndpoint`:** The OTLP collector URL (e.g., `http://localhost:4317`). The scheme sets plaintext or TLS.
*   **`samplingRate`:** The fraction of root spans sampled (0.0 to 1.0).
*   **`logFormat`:** `console` or `json`.

`ORBIT_OTLP_ENDPOINT` and `ORBIT_SAMPLING_RATE` override the first two for the session-manager and identity-proxy containers that orbit-agents and the operator build.

Service name and version are not environment variables: each binary passes them in its `Config`.

## Behavior by Environment

### Development Mode (`APP_ENV=local`)
- Uses **stdout exporter** with pretty-printing
- Trace spans printed to console for immediate visibility
- No external dependencies required
- Best for local development and debugging

### Production Mode (`APP_ENV=production`)
- **With OTLP endpoint**: Exports traces to collector via gRPC
- **Without OTLP endpoint**: No-op mode (logs work, traces not exported)
- Allows deployment without observability backend
- Add OTLP endpoint when ready for centralized tracing

## Example: Development

```bash
# Run with stdout trace export (default)
go run ./cmd/identity/main.go

# You'll see:
# 1. Colorized logs with trace_id/span_id fields
# 2. JSON trace spans printed to stdout
# 3. Clear correlation between logs and traces
```

## Example: Production with OTLP Collector

```bash
# Export to a local OTEL collector: set otlpEndpoint: http://localhost:4317
# in /etc/orbit/config/internal/platform/observability.yaml
export APP_ENV=production
go run ./cmd/identity/main.go

# Traces sent to collector via gRPC
# Logs still go to stdout (k8s best practice)
```

## Example: Production without Collector

```bash
# Production mode, no trace export
export APP_ENV=production
go run ./cmd/identity/main.go

# Logs work normally (with trace_id when spans present)
# Traces not exported anywhere
# App works fine without observability backend
```

## Integration with Logging

The observability package works seamlessly with `internal/platform/logging`:

1. **Setup OTEL first** (in main.go)
2. **Create tracers** for your operations
3. **Pass context** through call chains
4. **Logger automatically extracts** trace_id/span_id from context

```go
func main() {
    ctx := context.Background()
    shutdown, _ := observability.SetupFromEnv(ctx)
    defer shutdown(ctx)
    
    logger := logging.NewLogger()
    
    // Create span
    tracer := otel.Tracer("orbit")
    ctx, span := tracer.Start(ctx, "handle-request")
    defer span.End()
    
    // Logger sees span in context, adds trace_id automatically
    logger.Info(ctx, "Processing request", 
        logging.Field{Key: "user_id", Value: "user-123"},
    )
}
```

## Sampling Strategy

Currently uses **AlwaysSample()** - all traces are captured.

For production at scale, consider:
- `TraceIDRatioBased(0.1)` - sample 10% of traces
- `ParentBased(...)` - respect parent span sampling decisions
- Custom sampler based on span attributes

Update in `observability.go`:
```go
sdktrace.WithSampler(sdktrace.TraceIDRatioBased(0.1))
```

## Shutdown Behavior

The `shutdown` function:
- Waits up to 5 seconds for pending spans to flush
- Safe to call multiple times (idempotent)
- Should be deferred in main() to ensure spans export on exit
