# Logging Package

OpenTelemetry-compatible structured logging for Orbit.

## Overview

This package provides a `Logger` interface with automatic trace context extraction. When OpenTelemetry distributed tracing is active, logs automatically include `trace_id` and `span_id` fields for correlation with traces.

## Quick Start

```go
import "github.com/clear-route/orbit/internal/platform/logging"

// Create logger (inject via dependency injection in production)
logger := logging.NewLogger() // Production: JSON to stdout
// OR
logger := logging.NewDevelopmentLogger() // Development: colorized console

// Basic logging
ctx := context.Background()
logger.Info(ctx, "Server started", 
    logging.Field{Key: "port", Value: 8080})

// With OTEL trace context (automatic correlation)
tracedCtx, span := tracer.Start(ctx, "operation")
defer span.End()

logger.Info(tracedCtx, "Processing request",
    logging.Field{Key: "user_id", Value: "user-123"},
    logging.Field{Key: "action", Value: "create_agent"},
)
// Output includes: trace_id, span_id, trace_sampled fields
```

## Interface

```go
type Logger interface {
    Debug(ctx context.Context, msg string, fields ...Field)
    Info(ctx context.Context, msg string, fields ...Field)
    Warn(ctx context.Context, msg string, fields ...Field)
    Error(ctx context.Context, msg string, fields ...Field)
}

type Field struct {
    Key   string
    Value interface{}
}
```

## Features

- **Context-Aware**: Pass `context.Context` to every log method
- **Auto Trace Correlation**: Automatically extracts OTEL trace/span IDs when present
- **Zero-Allocation Fast Path**: No overhead when tracing is not active
- **Structured Logging**: Attach arbitrary key-value fields to logs
- **Multiple Outputs**: JSON (production) or console (development)

## Production Setup

```go
// In main.go or service initialization
logger := logging.NewLogger()

// Inject into services
agentService := agents.NewService(logger, repo)
policyEngine := policy.NewEngine(logger, opaClient)
```

## Development Setup

```go
// Use human-friendly console output during development
logger := logging.NewDevelopmentLogger()
```

## Log Levels

- **Debug**: Verbose diagnostic information (typically disabled in production)
- **Info**: Routine application events (startup, shutdown, state changes)
- **Warn**: Unexpected but recoverable situations (high memory, slow queries)
- **Error**: Failures the system recovered from (failed API calls, validation errors)

## Testing

```go
import (
    "bytes"
    "testing"
    "github.com/clear-route/orbit/internal/platform/logging"
)

func TestMyService(t *testing.T) {
    var buf bytes.Buffer
    logger := logging.NewLoggerWithOutput(&buf)
    
    service := NewMyService(logger)
    service.DoSomething(context.Background())
    
    // Assert log output
    logOutput := buf.String()
    // ... assertions ...
}
```

## OpenTelemetry Integration

When OTEL tracing is configured (via middleware or manual instrumentation), logs automatically include:

- `trace_id`: Unique identifier for the entire request trace
- `span_id`: Identifier for the current operation within the trace  
- `trace_sampled`: Boolean indicating if trace is sampled for storage

This enables querying logs by trace ID in your observability backend to see all logs for a specific request.

## Performance

- **Zero allocations** when no trace context present
- **Single allocation** for field map when fields provided
- **Inlined span extraction** for minimal overhead
- Zerolog backend provides [high-performance JSON serialization](https://github.com/rs/zerolog#benchmarks)

## Future Enhancements

### High Priority (Ergonomics & Error Handling)
- **Field Helpers**: Add typed field constructors (String, Int, Bool, etc.) for better ergonomics
- **Error Helper**: Add ErrorWithErr(ctx, msg, err, fields...) for structured error logging
- **Fatal Method**: Add Fatal() for unrecoverable errors (currently missing)

### Medium Priority (Advanced Features)
- **WithFields Pattern**: Support pre-attaching fields to logger context for request scoping
- **Map Pooling**: Use sync.Pool for field maps in high-throughput scenarios

### Low Priority (Nice to Have)
- **Custom Levels**: Support custom log levels beyond Debug/Info/Warn/Error
- **Sampling**: Add log sampling for high-volume debug logs

## Architecture Decision

See [dependency-injected-logging](../../../../docs/adr/2026-02-17-dependency-injected-logging.md) for design rationale and trade-offs.
