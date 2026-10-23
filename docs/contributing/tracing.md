# Distributed Tracing

## Overview

kora uses OpenTelemetry for distributed tracing, implementing the W3C TraceContext standard for trace propagation across service boundaries.

## Architecture

### Trace Context Flow

```
┌─────────────────────────────────────────────────────────────┐
│ Incoming HTTP Request                                        │
│ Headers: traceparent, tracestate                            │
└────────────────────┬────────────────────────────────────────┘
                     │
                     ▼
┌─────────────────────────────────────────────────────────────┐
│ Tracing Middleware                                           │
│ 1. Extract trace context from headers                       │
│ 2. Start new span for this request                          │
│ 3. Inject span into Go context                              │
└────────────────────┬────────────────────────────────────────┘
                     │
                     ▼
┌─────────────────────────────────────────────────────────────┐
│ Logging Middleware                                           │
│ - Extracts trace_id/span_id from context                    │
│ - Adds to all log entries                                   │
└────────────────────┬────────────────────────────────────────┘
                     │
                     ▼
┌─────────────────────────────────────────────────────────────┐
│ HTTP Handlers                                                │
│ - Access span via context if needed                         │
│ - Propagate context to downstream services                  │
└────────────────────┬────────────────────────────────────────┘
                     │
                     ▼
┌─────────────────────────────────────────────────────────────┐
│ Span Export                                                  │
│ - Development: stdout                                        │
│ - Production: OTLP collector                                 │
└─────────────────────────────────────────────────────────────┘
```

## Implementation Details

### HTTP Request Tracing

Every HTTP request automatically creates a span via the tracing middleware:

```go
// From: backend/internal/platform/middleware/tracing.go

// Get the global tracer for this service
tracer := otel.Tracer(serviceName)

// Extract trace context from headers, then start span
propagatedCtx := otel.GetTextMapPropagator().Extract(
    r.Context(), 
    propagation.HeaderCarrier(r.Header),
)

// Start span for this request (span name: "{METHOD} {PATH}")
tracedCtx, span := tracer.Start(propagatedCtx, r.Method+" "+r.URL.Path,
    trace.WithSpanKind(trace.SpanKindServer),
    trace.WithAttributes(
        semconv.HTTPRequestMethodKey.String(r.Method),
        semconv.HTTPRouteKey.String(r.URL.Path),
        semconv.URLScheme(schemeFromRequest(r)),
        semconv.URLPath(r.URL.Path),
        semconv.ServerAddress(r.Host),
        semconv.UserAgentOriginal(r.UserAgent()),
    ),
)
defer span.End()

// Inject span into request context for downstream use
r = r.WithContext(tracedCtx)
```

**Key Features:**
- Automatic parent-child relationship via W3C TraceContext
- Semantic conventions for HTTP attributes
- Span marked as error for 4xx/5xx responses
- Response status code recorded

### Trace Propagation

kora uses the W3C TraceContext standard for propagating trace context:

**Headers:**
```
traceparent: 00-{trace-id}-{parent-span-id}-{flags}
tracestate: {vendor-specific-data}
```

**Example:**
```
traceparent: 00-2482aff647d2da4b929d57bc10bcfa74-85ad5c6de00215ea-01
```

This enables distributed tracing across:
- kora → Agent frameworks
- kora → External APIs
- kora → Database queries (future)
- Agent → Tool invocations (future)

### Span Attributes

All HTTP request spans include:

**Required Attributes (HTTP Semantic Conventions):**
- `http.request.method`: HTTP method (GET, POST, etc.)
- `http.route`: Route pattern (e.g., `/api/v1/example/hello/{name}`)
- `url.scheme`: Scheme (http/https)
- `url.path`: Request path
- `server.address`: Host header value
- `user_agent.original`: Client user agent (if present)
- `http.response.status_code`: Response status code (added after handler completes)

**Custom Attributes:**
- Can be added in handlers via `span.SetAttributes()`
- Example: agent identity, policy decisions, tool invocations

### Error Handling

Spans are marked as errors for failed requests:

```go
// From: backend/internal/platform/middleware/tracing.go

// Record response status in span
span.SetAttributes(semconv.HTTPResponseStatusCode(wrapped.statusCode))

// Mark span as error if status code indicates failure
if wrapped.statusCode >= 400 {
    span.SetStatus(codes.Error, "HTTP error")
}
```

This enables error-focused trace analysis in observability backends.

## Usage in Code

### Accessing Current Span

```go
import "go.opentelemetry.io/otel/trace"

func MyHandler(w http.ResponseWriter, r *http.Request) {
    // Get current span from context
    span := trace.SpanFromContext(r.Context())
    
    // Add custom attributes
    span.SetAttributes(
        attribute.String("agent.id", agentID),
        attribute.String("policy.decision", "allow"),
    )
    
    // Add events
    span.AddEvent("policy_evaluated", trace.WithAttributes(
        attribute.String("result", "allowed"),
    ))
}
```

### Creating Child Spans

For long-running operations or complex workflows:

```go
func ProcessAgentRequest(ctx context.Context, req *AgentRequest) error {
    tracer := otel.Tracer("kora")
    
    // Create child span
    ctx, span := tracer.Start(ctx, "process_agent_request")
    defer span.End()
    
    // Step 1: Authenticate
    ctx, authSpan := tracer.Start(ctx, "authenticate_agent")
    err := authenticate(ctx, req.AgentID)
    authSpan.End()
    if err != nil {
        span.RecordError(err)
        return err
    }
    
    // Step 2: Evaluate policy
    ctx, policySpan := tracer.Start(ctx, "evaluate_policy")
    decision, err := evaluatePolicy(ctx, req)
    policySpan.SetAttributes(
        attribute.String("decision", decision),
    )
    policySpan.End()
    
    return nil
}
```

### Propagating to Downstream Services

**Never manually inject trace headers or construct `http.Client{}`.** All service-to-service calls must use a typed client in `internal/platform/clients/{service}/client.go` that embeds `httpclient.Base`. The base client automatically:
- Injects `traceparent` / `tracestate` W3C headers via `otel.GetTextMapPropagator().Inject(...)`
- Opens a `SpanKindClient` child span with the correct OTel HTTP semantic convention attributes
- Records upstream error bodies in the `error.upstream_body` span attribute

```go
// CORRECT — use a typed platform client
type MyServiceClient struct {
    base    *httpclient.Base
    baseURL string
}

func (c *MyServiceClient) Fetch(ctx context.Context, id string) (*Result, error) {
    req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/resource/"+id, nil)
    if err != nil {
        return nil, fmt.Errorf("build request: %w", err)
    }
    _, span, resp, err := c.base.Do(ctx, "myservice.fetch", req)
    defer span.End()
    if err != nil {
        return nil, err
    }
    var result Result
    if err := c.base.DecodeJSON(span, resp, &result); err != nil {
        return nil, err
    }
    return &result, nil
}

// WRONG — banned by noRawHttpClientLiteral lint rule
func CallExternalAPI(ctx context.Context, url string) error {
    req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
    otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(req.Header))
    resp, err := http.DefaultClient.Do(req)
    _ = resp
    return err
}
```

## Trace Analysis

### Example Trace Hierarchy

```
Trace ID: 2482aff647d2da4b929d57bc10bcfa74
│
└─ GET /api/v1/agents/execute [200ms]
   │
   ├─ authenticate_agent [5ms]
   │
   ├─ evaluate_policy [10ms]
   │  ├─ load_policy [3ms]
   │  └─ opa_evaluate [7ms]
   │
   ├─ execute_tool [180ms]
   │  ├─ get_credentials [5ms]
   │  └─ http_call_to_tool [175ms]
   │
   └─ record_audit_event [5ms]
```

### Query Patterns

**Find slow requests:**
```
http.status_code:200 AND duration > 1s
```

**Find policy denials:**
```
policy.decision:deny
```

**Find agent-specific traces:**
```
agent.id:agent-xyz
```

**Find errors:**
```
status.code:error
```

## Performance Considerations

### Sampling

- **Development**: 100% sampling (all traces)
- **Production**: 10% sampling (configurable)
- **High-traffic**: 1% sampling or tail-based

### Overhead

OpenTelemetry's overhead is minimal:
- Span creation: ~1-5 microseconds
- Attribute addition: ~100 nanoseconds
- Context propagation: negligible

### Batching

Spans are batched before export:
- Default: 512 spans per batch
- Max delay: 5 seconds
- Reduces network overhead

## Integration Points

### Current

✅ **HTTP Requests**: Automatic `SpanKindServer` span per inbound request via tracing middleware
✅ **Logging**: Automatic `trace_id`/`span_id` correlation on every log line
✅ **W3C TraceContext**: Standard `traceparent`/`tracestate` propagation on all inbound and outbound calls
✅ **Service-to-service clients**: `httpclient.Base.Do()` opens a `SpanKindClient` child span and injects W3C headers automatically
✅ **Upstream error bodies**: Non-2xx responses from upstream services are captured in the `error.upstream_body` span attribute

### Planned

🔄 **Database Queries**: Span per query with SQL text
🔄 **gRPC Services**: Automatic gRPC tracing
🔄 **Agent Tool Invocations**: Trace agent → tool calls
🔄 **Policy Evaluations**: Detailed OPA evaluation spans
🔄 **Credential Brokering**: Track secret retrieval and usage

## Best Practices

### DO

✅ Use semantic conventions for standard operations (HTTP, DB, RPC)
✅ Add meaningful attributes that aid debugging
✅ Create child spans for complex operations (>100ms)
✅ Record errors with `span.RecordError(err)`
✅ Propagate context to all downstream calls

### DON'T

❌ Create spans for trivial operations (<1ms)
❌ Add PII or secrets as span attributes
❌ Create deeply nested span hierarchies (>10 levels)
❌ Forget to call `span.End()` (use defer)
❌ Sample 100% in production (too expensive)
❌ Use `err.Error()` or `ctx.Err().Error()` inside `span.SetStatus(codes.Error, ...)`; use a package-level sentinel string ([opentelemetry-observability-discipline](../adr/2026-03-21-opentelemetry-observability-discipline.md), enforced by `spanSetStatusLocalErr` lint rule)
❌ Use `err.Error()` inside `attribute.String(...)`; use `span.RecordError(err)` for dynamic error context instead ([opentelemetry-observability-discipline](../adr/2026-03-21-opentelemetry-observability-discipline.md), enforced by `spanAttrStringLocalErr` lint rule)
❌ Manually call `otel.GetTextMapPropagator().Inject(...)` on outbound requests; use `httpclient.Base.Do()` ([multi-service-backend-deployment-structure](../adr/2026-03-19-multi-service-backend-deployment-structure.md))
❌ Import `go.opentelemetry.io/otel/codes` or `go.opentelemetry.io/otel/trace` in typed service clients: the base client owns that lifecycle ([opentelemetry-observability-discipline](../adr/2026-03-21-opentelemetry-observability-discipline.md), enforced by `no-otel-span-mgmt-in-service-clients`)

## Related Documentation

- [Configuration](../reference/telemetry.md)
- [Logging](logging.md)
- [OpenTelemetry Tracing Spec](https://opentelemetry.io/docs/specs/otel/trace/api/)
- [OpenTelemetry Semantic Conventions](https://opentelemetry.io/docs/specs/semconv/)
