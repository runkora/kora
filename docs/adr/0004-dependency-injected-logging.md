# 0004. OTEL-Compatible Dependency-Injected Logging

**Status:** Accepted
**Date:** 2026-02-16
**Last Updated:** 2026-02-16
**Deciders:** Architecture Team
**Technical Story:** Establishing logging infrastructure for the Orbit backend with OpenTelemetry readiness

---

## Context

The Orbit backend requires a logging strategy that supports modern observability, debugging, and operational monitoring across all features and components. The logging approach must:
- Integrate cleanly with Go idioms and support structured logging
- Facilitate testing without creating tight coupling between components
- Enable automatic log-trace correlation with OpenTelemetry (OTEL)
- Support both stdout logging (for Kubernetes) and future OTEL Collector integration
- Maintain high performance with zero-allocation logging paths

### Background

Logging is a cross-cutting concern that touches every part of the application. Different approaches to logging in Go applications create different trade-offs in terms of:
- Coupling between components and logging libraries
- Testability and ability to mock logging behavior
- Visibility of dependencies and side effects
- Consistency with Go community best practices

### Problem Statement

We need to choose a logging pattern that:
1. Makes dependencies explicit and visible
2. Facilitates testing by allowing logger mocking
3. Prevents tight coupling between application code and specific logging libraries
4. Aligns with Go community best practices and idioms
5. Supports structured logging for modern observability tools
6. **Enables automatic log-trace correlation with OpenTelemetry**
7. **Works gracefully with or without active tracing context**
8. **Supports high-performance, zero-allocation logging**

### Goals

- Establish a consistent logging pattern across all backend features
- Enable easy testing of components that perform logging
- Support structured logging with context-aware log entries
- **Enable automatic extraction and attachment of OTEL trace/span IDs to log entries**
- **Support both stdout (Kubernetes container logs) and future OTEL Collector push**
- Maintain flexibility to change logging implementations without refactoring application code
- Make dependencies explicit in function and constructor signatures
- **Achieve zero-allocation logging in hot paths when possible**

### Non-Goals

- Building a complex custom logging framework
- Supporting multiple logging libraries simultaneously
- Implementing advanced logging features (sampling, dynamic levels, etc.) initially
- Creating a centralized logging service or aggregation layer
- **Implementing distributed tracing itself (that's OTEL's job - we only integrate with it)**
- **Building our own trace context propagation (use OTEL SDK standards)**

---

## Decision Drivers

* **Go community consensus** - Follow established best practices from Go experts
* **Testability** - Must be easy to test components that log
* **Explicitness** - Dependencies should be visible, not hidden in globals
* **Simplicity** - Avoid over-engineering the logging infrastructure
* **Maintainability** - Code should be easy to understand and refactor
* **Performance** - Logging should have minimal overhead (zero-allocation when possible)
* **Observability** - Support structured logging for modern monitoring tools
* **OTEL Compatibility** - Seamless integration with OpenTelemetry tracing
* **Future-Proof** - Design works today without OTEL, scales with it later

---

## Considered Options

### Option 1: Package-Level Global Logger

**Description:** Use a package-level variable initialized at startup that all code references directly.

```go
package myfeature

import "github.com/some/logger"

var log = logger.New("myfeature")

func DoWork() {
    log.Info("doing work")
}
```

**Pros:**
- Simple to use - no need to pass logger around
- Less verbose function signatures
- Common pattern in many codebases

**Cons:**
- Hidden dependency - not visible in function signatures
- Tight coupling to specific logging library
- Difficult to test - requires global state manipulation
- Violates "no package-level vars" Go best practice
- Creates magic/implicit behavior
- Transitive coupling - any package using this feature depends on the logger

**Community Position:** Explicitly discouraged by Go experts (Dave Cheney: "The package level logger anti pattern", Peter Bourgon: "No package level variables")

### Option 2: Context-Aware Dependency Injection (CHOSEN)

**Description:** Pass logger as a dependency through constructors and store in struct fields. Logger methods accept `context.Context` as first parameter to enable automatic OTEL trace correlation.

```go
package myfeature

type Logger interface {
    Info(ctx context.Context, msg string, fields ...any)
    Error(ctx context.Context, msg string, err error, fields ...any)
    Debug(ctx context.Context, msg string, fields ...any)
    Warn(ctx context.Context, msg string, fields ...any)
}

type Service struct {
    logger Logger
    // other fields
}

func NewService(logger Logger) *Service {
    return &Service{logger: logger}
}

func (s *Service) DoWork(ctx context.Context) error {
    // Logger automatically extracts trace/span IDs from ctx if present
    s.logger.Info(ctx, "starting work")
    
    // If ctx has active OTEL span, log entry includes:
    // {"msg":"starting work", "trace_id":"...", "span_id":"..."}
    
    return nil
}
```

**Pros:**
- Explicit dependencies - visible in signatures
- Easy to test - can inject mock logger
- No coupling to specific logger library
- Follows Go best practices (Cheney, Bourgon)
- Clear dependency graph
- Supports interface segregation (each package defines what it needs)
- **Automatic OTEL trace/span ID extraction from context**
- **Works with or without OTEL - graceful degradation**
- **Zero-allocation path when no tracing active**
- **Natural fit with Go's context propagation patterns**

**Cons:**
- Requires ctx parameter in logging methods
- Requires passing logger through construction chain
- Slightly more boilerplate than global logger

**Community Position:** Strongly recommended by Go community leaders. Context-first parameters align with Go stdlib conventions (database/sql, net/http).

### Option 3: Logger Stored in Context

**Description:** Store logger in context.Context and extract when needed.

```go
package myfeature

func DoWork(ctx context.Context) {
    log := logger.FromContext(ctx)
    log.Info("doing work")
}
```

**Pros:**
- Doesn't clutter function signatures with logger parameter
- Can attach request-scoped fields to logger
- Works well with middleware

**Cons:**
- **Hidden dependency - not clear from signature that logging occurs**
- **Context pollution - context used for non-cancellation purpose**
- **Requires runtime extraction with potential for nil panics**
- **Testing still requires context manipulation**
- **Not standard Go practice for dependency management**
- **Violates Go proverb: "A little copying is better than a little dependency"**

**Community Position:** Generally discouraged. Go context is for cancellation and request-scoped values, not dependency injection.

---

## Decision

**Chosen Option:** Option 2 - Context-Aware Dependency Injection with Logger Interface

### Rationale

We chose context-aware dependency injection because it combines the best of explicit dependencies with OTEL-native trace correlation:

1. **Community Consensus**: Dave Cheney and Peter Bourgon advocate for DI over global loggers. Context-first parameters align with Go stdlib conventions (database/sql, net/http).

2. **Explicitness Over Magic**: Logger is a visible struct field. The `ctx` parameter makes trace propagation explicit - no hidden globals.

3. **OTEL Integration Without Coupling**: Logger implementation extracts trace context automatically, but feature code has zero coupling to OTEL. Features just pass context through - standard Go practice.

4. **Testability**: Injecting a mock logger is trivial. Context manipulation in tests is also standard Go testing practice.

5. **No Library Lock-in**: By defining the logging interface in consuming packages, we avoid coupling application code to any specific logging library OR tracing library. We can change implementations without touching feature code.

6. **Graceful Degradation**: Works perfectly without OTEL installed. When context has no trace, logging continues normally. Zero-allocation fast path when tracing disabled.

7. **Performance**: Zerolog provides zero-allocation logging. OTEL trace extraction only happens when tracing is active, keeping hot paths fast.

8. **Future-Proof**: Supports stdout logging today (Kubernetes container logs). Can add OTEL Collector exporter later without changing application code.

The `ctx context.Context` parameter is standard Go practice and enables automatic trace correlation - a critical observability feature. This small addition to method signatures provides enormous value for debugging distributed systems.

---

## Consequences

### Positive

- Clear visibility of which components perform logging
- Easy to test components in isolation with mock loggers
- No coupling between application code and logging library OR OTEL
- Freedom to change logging implementation without refactoring features
- Follows Go community best practices and idioms
- Natural fit with the repository pattern already in use
- Supports structured logging through interface design
- **Automatic log-trace correlation when OTEL span exists in context**
- **Zero-allocation logging path when tracing disabled**
- **Works with stdout (K8s) today, OTEL Collector tomorrow**
- **Context propagation aligns with Go stdlib patterns**

### Negative

- Function signatures require `ctx context.Context` parameter
- Requires discipline to pass logger through construction chains and context through call chains
- Initial setup requires more boilerplate than global logger
- Team must understand and follow the pattern consistently

### Neutral

- Each package defines its own logger interface (typically: Info, Error, Debug, Warn with ctx first param)
- Logger is a required dependency for most service constructors
- Testing requires explicit logger mock creation and context passing
- **Trace extraction overhead only occurs when OTEL span active in context**

---

## Implementation

### Logger Interface Design

The logger interface is defined in `internal/logging/logger.go`:

```go
package logging

import "context"

// Field represents a structured key-value pair for log entries
type Field struct {
    Key   string
    Value interface{}
}

// Logger defines the application-wide logging interface with OTEL support
type Logger interface {
    // Debug logs debug-level messages (disabled in production)
    Debug(ctx context.Context, msg string, fields ...Field)
    
    // Info logs informational messages
    Info(ctx context.Context, msg string, fields ...Field)
    
    // Warn logs warning messages
    Warn(ctx context.Context, msg string, fields ...Field)
    
    // Error logs error messages
    Error(ctx context.Context, msg string, fields ...Field)
}
```

### Zerolog Implementation with OTEL Integration

The concrete implementation uses zerolog with automatic OTEL trace extraction:

```go
package logging

import (
    "context"
    "os"

    "github.com/rs/zerolog"
    "go.opentelemetry.io/otel/trace"
)

type zerologLogger struct {
    logger zerolog.Logger
}

// NewLogger creates a production logger with JSON output and OTEL support
func NewLogger() Logger {
    zlog := zerolog.New(os.Stdout).With().Timestamp().Logger()
    return &zerologLogger{logger: zlog}
}

// NewDevelopmentLogger creates a human-friendly console logger
func NewDevelopmentLogger() Logger {
    zlog := zerolog.New(zerolog.ConsoleWriter{Out: os.Stdout}).
        With().
        Timestamp().
        Logger()
    return &zerologLogger{logger: zlog}
}

func (l *zerologLogger) Info(ctx context.Context, msg string, fields ...Field) {
    l.logWithContext(ctx, l.logger.Info(), msg, fields)
}

func (l *zerologLogger) Error(ctx context.Context, msg string, fields ...Field) {
    l.logWithContext(ctx, l.logger.Error(), msg, fields)
}

func (l *zerologLogger) Debug(ctx context.Context, msg string, fields ...Field) {
    l.logWithContext(ctx, l.logger.Debug(), msg, fields)
}

func (l *zerologLogger) Warn(ctx context.Context, msg string, fields ...Field) {
    l.logWithContext(ctx, l.logger.Warn(), msg, fields)
}

// logWithContext extracts OTEL trace context and attaches it to the log entry
func (l *zerologLogger) logWithContext(
    ctx context.Context,
    event *zerolog.Event,
    msg string,
    fields []Field,
) {
    // Extract OTEL span from context (if present)
    span := trace.SpanFromContext(ctx)
    if span.SpanContext().IsValid() {
        spanCtx := span.SpanContext()
        event = event.
            Str("trace_id", spanCtx.TraceID().String()).
            Str("span_id", spanCtx.SpanID().String())
        
        // Include trace flags if trace is sampled
        if spanCtx.TraceFlags().IsSampled() {
            event = event.Bool("trace_sampled", true)
        }
    }
    
    // Add custom fields
    if len(fields) > 0 {
        event = event.Fields(convertFields(fields))
    }
    
    event.Msg(msg)
}

func convertFields(fields []Field) map[string]interface{} {
    m := make(map[string]interface{}, len(fields))
    for _, f := range fields {
        m[f.Key] = f.Value
    }
    return m
}
```

### Usage in Feature Packages

Feature packages define minimal logger interfaces with only methods they need:

```go
// internal/identity/service.go
package identity

import "context"

// Logger defines logging methods needed by identity service
type Logger interface {
    Info(ctx context.Context, msg string, fields ...logging.Field)
    Error(ctx context.Context, msg string, fields ...logging.Field)
}

type Service struct {
    logger Logger
    repo   Repository
}

func NewService(logger Logger, repo Repository) *Service {
    return &Service{
        logger: logger,
        repo:   repo,
    }
}

func (s *Service) CreateAgent(ctx context.Context, req CreateAgentRequest) (*Agent, error) {
    s.logger.Info(ctx, "creating agent", logging.Field{Key: "name", Value: req.Name})
    
    agent, err := s.repo.Create(ctx, req)
    if err != nil {
        s.logger.Error(ctx, "failed to create agent", 
            logging.Field{Key: "error", Value: err},
            logging.Field{Key: "name", Value: req.Name},
        )
        return nil, err
    }
    
    // If ctx has active OTEL span, these logs automatically include trace_id/span_id
    s.logger.Info(ctx, "agent created successfully", 
        logging.Field{Key: "agent_id", Value: agent.ID},
    )
    
    return agent, nil
}
```

### OTEL Setup (Future)

When ready to enable distributed tracing, initialize OTEL in main.go:

```go
// cmd/server/main.go
package main

import (
    "context"
    
    "go.opentelemetry.io/otel"
    "go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
    "go.opentelemetry.io/otel/sdk/trace"
)

func initOTEL(ctx context.Context) (*trace.TracerProvider, error) {
    exporter, err := otlptracegrpc.New(ctx,
        otlptracegrpc.WithEndpoint("otel-collector:4317"),
        otlptracegrpc.WithInsecure(),
    )
    if err != nil {
        return nil, err
    }
    
    tp := trace.NewTracerProvider(
        trace.WithBatcher(exporter),
        trace.WithSampler(trace.AlwaysSample()),
    )
    
    otel.SetTracerProvider(tp)
    return tp, nil
}
```

Application code remains unchanged - logs automatically include trace context.

### Testing with Mock Logger

```go
// internal/identity/service_test.go
package identity

import (
    "context"
    "testing"
    
    "github.com/yourusername/orbit/internal/platform/logging"
)

type mockLogger struct {
    infoCalls  []string
    errorCalls []string
}

func (m *mockLogger) Info(ctx context.Context, msg string, fields ...logging.Field) {
    m.infoCalls = append(m.infoCalls, msg)
}

func (m *mockLogger) Error(ctx context.Context, msg string, fields ...logging.Field) {
    m.errorCalls = append(m.errorCalls, msg)
}

func TestCreateAgent(t *testing.T) {
    logger := &mockLogger{}
    repo := &mockRepository{}
    svc := NewService(logger, repo)
    
    ctx := context.Background()
    agent, err := svc.CreateAgent(ctx, CreateAgentRequest{Name: "test"})
    
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    
    if len(logger.infoCalls) != 2 {
        t.Errorf("expected 2 info logs, got %d", len(logger.infoCalls))
    }
}
```

### Migration Path

Since this is a greenfield project, no migration is needed. All new code will follow this pattern from the start.

**Phased Rollout:**
1. ✅ Phase 1: Implement logging package with zerolog + OTEL trace extraction
2. ✅ Phase 2: Use in all feature packages (stdout logging only)
3. 🔜 Phase 3: Add OTEL SDK and enable distributed tracing
4. 🔜 Phase 4: Add OTEL Collector exporter for centralized log aggregation

---

## Validation

### Success Metrics

- All feature packages use dependency-injected loggers
- Zero package-level logger variables in application code
- All services can be instantiated with mock loggers in tests
- No direct imports of concrete logging libraries in feature packages
- **All logger methods accept `context.Context` as first parameter**
- **Log entries automatically include trace_id/span_id when OTEL span exists**
- **Logging works correctly with and without OTEL tracing enabled**
- **Zero-allocation logging path verified under benchmarking**

### Monitoring

- Code reviews will verify new code follows the pattern
- Linting rules can detect package-level variables
- Test coverage will validate mock logger usage
- **Integration tests verify trace context propagation to logs**
- **Benchmarks ensure zero-allocation fast path**
- **Log sampling in production confirms trace_id presence in traced requests**

---

## Related Decisions

- [ADR-0003](0003-repository-pattern-and-code-organization.md) - Repository pattern (similar dependency injection approach)

---

## References

### Go Best Practices
- [Dave Cheney - The package level logger anti pattern](https://dave.cheney.net/2017/01/23/the-package-level-logger-anti-pattern)
- [Peter Bourgon - A theory of modern Go](https://peter.bourgon.org/blog/2017/06/09/theory-of-modern-go.html)
- [Go Proverbs - A little copying is better than a little dependency](https://go-proverbs.github.io/)

### Logging Libraries
- [Zerolog - Zero Allocation JSON Logger](https://github.com/rs/zerolog)
- [Go blog - Structured Logging with slog](https://go.dev/blog/slog)

### OpenTelemetry
- [OpenTelemetry Go - Trace API](https://pkg.go.dev/go.opentelemetry.io/otel/trace)
- [OTEL Logging Best Practices](https://opentelemetry.io/docs/specs/otel/logs/)
- [OTLP Exporter for Go](https://pkg.go.dev/go.opentelemetry.io/otel/exporters/otlp/otlptrace)

### Production Examples
- [Go kit - Logging design](https://gokit.io/) (uses dependency injection)
- [Uber Zap - Blazing fast, structured, leveled logging](https://github.com/uber-go/zap)

---

## Notes

This decision was informed by research into Go community best practices, OpenTelemetry integration patterns, and examination of production Go systems. The context-aware dependency injection pattern combines established Go idioms with modern observability requirements.

**Key Design Decisions:**
1. **Zerolog over slog**: Chosen for zero-allocation performance and mature OTEL integration examples
2. **Context-first parameters**: Aligns with Go stdlib (database/sql, net/http) and enables automatic trace propagation
3. **Graceful degradation**: System works perfectly without OTEL, making it safe to deploy logging before tracing
4. **Stdout first, OTEL later**: Kubernetes container log collection works today; OTEL Collector is an additive enhancement

**Performance Characteristics:**
- Zero allocations when trace context not present
- Single span context extraction per log call when tracing active
- JSON serialization overhead constant regardless of tracing

**Future Considerations:**
- May add sampling/rate limiting for high-volume debug logs
- Could add dynamic log level adjustment per request
- Consider log aggregation backends (Loki, Elasticsearch) via OTEL Collector
