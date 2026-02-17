# Structured Logging

## Overview

Orbit uses structured logging with automatic trace correlation, enabling developers to seamlessly navigate from logs to traces and vice versa.

## Architecture

### Logging Flow

```
┌─────────────────────────────────────────────────────────────┐
│ Request Context (contains OTEL span from middleware)        │
└────────────────────┬────────────────────────────────────────┘
                     │
                     ▼
┌─────────────────────────────────────────────────────────────┐
│ logger.Info(ctx, "message", logging.Field{...})             │
│ - Logger extracts trace_id/span_id from context             │
│ - Automatically includes in log output                      │
└────────────────────┬────────────────────────────────────────┘
                     │
                     ▼
┌─────────────────────────────────────────────────────────────┐
│ Log Output (JSON):                                           │
│ {                                                            │
│   "message": "...",                                          │
│   "trace_id": "2482aff647d2da4b...",                         │
│   "span_id": "85ad5c6de00215ea",                             │
│   "key": "value"                                             │
│ }                                                            │
└─────────────────────────────────────────────────────────────┘
```

## Implementation

### Logger Creation

```go
// Production logger (JSON to stdout)
logger := logging.NewLogger()

// Development logger (pretty-printed console)
logger := logging.NewDevelopmentLogger()

// Inject into services/controllers
controller := NewController(logger)
```

### Automatic Trace Correlation

The logger automatically extracts trace context from the context parameter:

```go
// From: backend/internal/platform/logging/zerolog.go

func (l *zerologLogger) Info(ctx context.Context, msg string, fields ...Field) {
    // Extract OTEL span from context (if present)
    span := trace.SpanFromContext(ctx)
    spanCtx := span.SpanContext()
    
    event := l.logger.Info()
    
    // Add trace fields if span context is valid
    if spanCtx.IsValid() {
        event = event.
            Str("trace_id", spanCtx.TraceID().String()).
            Str("span_id", spanCtx.SpanID().String())
    }
    
    // Add custom fields and emit
    event.Fields(convertFields(fields)).Msg(msg)
}
```

**Result:** Every log entry automatically includes `trace_id` and `span_id` when the context contains a valid span.

### Log Levels

```go
logger.Debug(ctx, "debug message",
    logging.Field{Key: "key", Value: "value"})   // Verbose diagnostics

logger.Info(ctx, "info message",
    logging.Field{Key: "key", Value: "value"})   // Normal operations

logger.Warn(ctx, "warning message",
    logging.Field{Key: "key", Value: "value"})   // Concerning but not errors

logger.Error(ctx, "error message",
    logging.Field{Key: "error", Value: err.Error()})  // Error conditions
```

**Level Hierarchy:** DEBUG < INFO < WARN < ERROR

**Production Default:** INFO (shows Info, Warn, Error)

## Usage Patterns

### Basic Logging

```go
// Logger is dependency injected into controllers/services
type Controller struct {
    logger logging.Logger
}

func (c *Controller) MyHandler(w http.ResponseWriter, r *http.Request) {
    // Pass context as first parameter - trace context extracted automatically
    c.logger.Info(r.Context(), "processing request",
        logging.Field{Key: "method", Value: r.Method},
        logging.Field{Key: "path", Value: r.URL.Path},
    )
}
```

**Output:**
```json
{
  "time": "2024-01-15T14:23:45Z",
  "level": "INFO",
  "message": "processing request",
  "method": "GET",
  "path": "/api/v1/example/hello/world",
  "trace_id": "2482aff647d2da4b929d57bc10bcfa74",
  "span_id": "85ad5c6de00215ea"
}
```

### Logging with Errors

```go
if err != nil {
    c.logger.Error(ctx, "failed to process request",
        logging.Field{Key: "error", Value: err.Error()},
        logging.Field{Key: "agent_id", Value: agentID},
    )
    return err
}
```

**Output:**
```json
{
  "time": "2024-01-15T14:23:45Z",
  "level": "ERROR",
  "message": "failed to process request",
  "err": "connection refused",
  "agent_id": "agent-123",
  "trace_id": "2482aff647d2da4b929d57bc10bcfa74",
  "span_id": "85ad5c6de00215ea"
}
```

### Logging Complex Data

```go
c.logger.Info(ctx, "agent executed tool",
    logging.Field{Key: "agent_id", Value: agent.ID},
    logging.Field{Key: "tool_name", Value: tool.Name},
    logging.Field{Key: "duration_ms", Value: elapsed.Milliseconds()},
    logging.Field{Key: "success", Value: result.Success},
)
```

**Best Practice:** Use structured fields rather than string interpolation:

❌ **Don't:**
```go
c.logger.Info(ctx, fmt.Sprintf("Agent %s executed %s in %dms", agent.ID, tool.Name, ms))
```

✅ **Do:**
```go
c.logger.Info(ctx, "agent executed tool",
    logging.Field{Key: "agent_id", Value: agent.ID},
    logging.Field{Key: "tool_name", Value: tool.Name},
    logging.Field{Key: "duration_ms", Value: ms},
)
```

### Logging with Context Propagation

```go
type Service struct {
    logger logging.Logger
}

func (s *Service) ProcessRequest(ctx context.Context, req *Request) error {
    // Log at entry - context contains trace information
    s.logger.Info(ctx, "processing request",
        logging.Field{Key: "request_id", Value: req.ID},
    )
    
    // Pass context to helpers (trace_id/span_id automatically included)
    if err := s.validateRequest(ctx, req); err != nil {
        return err
    }
    
    if err := s.executeRequest(ctx, req); err != nil {
        return err
    }
    
    s.logger.Info(ctx, "request processed successfully")
    return nil
}

func (s *Service) validateRequest(ctx context.Context, req *Request) error {
    // Same logger, same trace_id/span_id from context
    s.logger.Debug(ctx, "validating request")
    // ...
    return nil
}
```

## Log-Trace Correlation

### Finding Traces from Logs

1. **Copy trace_id from log entry:**
   ```
   trace_id: 2482aff647d2da4b929d57bc10bcfa74
   ```

2. **Query observability backend:**
   ```
   trace_id:2482aff647d2da4b929d57bc10bcfa74
   ```

3. **View complete request flow** with timing and spans

### Finding Logs from Traces

1. **Copy trace_id from trace span:**
   ```
   Trace ID: 2482aff647d2da4b929d57bc10bcfa74
   ```

2. **Query log aggregator:**
   ```
   trace_id:"2482aff647d2da4b929d57bc10bcfa74"
   ```

3. **View all logs** for that request

## Configuration

### Log Levels

Set via environment variable:

```bash
LOG_LEVEL=debug   # Development: verbose output
LOG_LEVEL=info    # Production: normal operations
LOG_LEVEL=warn    # Production: warnings and errors only
LOG_LEVEL=error   # Production: errors only
```

### Development vs Production

**Development:**
```go
// Pretty-printed, colorized console output
logger := logging.NewDevelopmentLogger()
```

**Production:**
```go
// JSON format for log aggregation (stdout)
logger := logging.NewLogger()
```

## Log Format

### Development Format

Colorized, human-readable:

```
2:11PM INF Incoming request method=GET path=/api/v1/example/health 
            trace_id=2482aff647d2da4b929d57bc10bcfa74 
            span_id=85ad5c6de00215ea
```

### Production Format

JSON for machine parsing:

```json
{
  "time": "2024-01-15T14:11:00Z",
  "level": "info",
  "message": "Incoming request",
  "method": "GET",
  "path": "/api/v1/example/health",
  "trace_id": "2482aff647d2da4b929d57bc10bcfa74",
  "span_id": "85ad5c6de00215ea"
}
```

## Best Practices

### DO

✅ **Use structured fields** instead of string formatting
✅ **Pass context as first parameter** for automatic trace correlation
✅ **Log at appropriate levels** (Info for operations, Error for failures)
✅ **Add meaningful context** (IDs, durations, status)
✅ **Propagate context** through function calls
✅ **Use consistent field names** across the codebase

### DON'T

❌ **Log PII or secrets** (passwords, tokens, API keys)
❌ **Log in hot loops** (use sampling or aggregation)
❌ **Interpolate strings** (use structured fields instead)
❌ **Log without context** (always pass context as first parameter)
❌ **Create multiple loggers** (dependency inject single logger instance)
❌ **Use different field names** for the same concept

### Field Naming Conventions

**Standard Fields:**
- `agent_id` - Agent identifier
- `tool_name` - Tool name
- `policy_id` - Policy identifier
- `request_id` - Request identifier
- `duration_ms` - Duration in milliseconds
- `status_code` - HTTP status code
- `err` - Error message
- `trace_id` - Trace identifier (automatic)
- `span_id` - Span identifier (automatic)

**Naming Rules:**
- Use snake_case for field names
- Use `_id` suffix for identifiers
- Use `_ms` suffix for milliseconds
- Use `_bytes` suffix for byte counts
- Be consistent across the codebase

## Middleware Logging

### HTTP Request Logging

The logging middleware automatically logs all HTTP requests:

```go
// From: backend/internal/platform/middleware/logging.go

func LoggingMiddleware(logger logging.Logger) Middleware {
    return NewMiddleware(func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            start := time.Now()
            
            // Log incoming request (context has trace info from OTEL middleware)
            logger.Info(r.Context(), "Incoming request",
                logging.Field{Key: "method", Value: r.Method},
                logging.Field{Key: "path", Value: r.URL.Path},
                logging.Field{Key: "remote_addr", Value: r.RemoteAddr},
            )
            
            // Wrap response writer to capture status code
            wrapped := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
            
            // Execute handler
            next.ServeHTTP(wrapped, r)
            
            // Log completion with duration
            logger.Info(r.Context(), "Request completed",
                logging.Field{Key: "method", Value: r.Method},
                logging.Field{Key: "path", Value: r.URL.Path},
                logging.Field{Key: "status", Value: wrapped.statusCode},
                logging.Field{Key: "duration_ms", Value: time.Since(start).Milliseconds()},
            )
        })
    })
}
```

**Output includes:**
- Request method, path, remote address
- Response status code
- Request duration
- **Automatic trace_id and span_id**

## Integration with Observability Backends

### Grafana Loki

Query logs with trace correlation:

```logql
{service="orbit"} 
  | json 
  | trace_id="2482aff647d2da4b929d57bc10bcfa74"
```

### Datadog

```
service:orbit trace_id:2482aff647d2da4b929d57bc10bcfa74
```

### Elasticsearch

```json
{
  "query": {
    "term": {
      "trace_id": "2482aff647d2da4b929d57bc10bcfa74"
    }
  }
}
```

## Performance Considerations

### Log Volume

**Development:** DEBUG level, ~1000 logs/request
**Production:** INFO level, ~10-20 logs/request

**Cost Reduction:**
- Use INFO level in production
- Sample high-frequency logs
- Aggregate metrics instead of logging each occurrence

### Overhead

- Structured logging: ~2-5 microseconds per log
- JSON marshaling: ~10-20 microseconds per log
- Context extraction: <1 microsecond

**Total:** ~20-50 microseconds per log entry (negligible)

## Troubleshooting

### No trace_id in logs

**Causes:**
1. Logging before tracing middleware runs
2. Not using `logger.WithContext(ctx)`
3. Context not propagated through call chain

**Solution:**
```go
// Always create logger from context
logger := logging.FromContext(ctx)
```

### Logs missing in production

**Check:**
1. Log level too high (use INFO, not ERROR)
2. Log aggregator not configured
3. Application not writing to stdout/stderr

### Performance degradation

**Solutions:**
1. Reduce log level (INFO instead of DEBUG)
2. Sample high-frequency logs
3. Use async log shipping

## Related Documentation

- [Tracing](tracing.md)
- [Configuration](configuration.md)
