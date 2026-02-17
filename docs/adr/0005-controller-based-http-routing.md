# 0005. Controller-Based HTTP Routing with Stdlib ServeMux

**Status:** Proposed
**Date:** 2026-02-17
**Deciders:** Engineering Team
**Technical Story:** API platform architecture for Orbit control plane

---

## Context

Orbit's control plane requires a robust HTTP routing architecture that can serve multiple feature domains (identity, agents, policies, credentials) while maintaining clean separation of concerns, testability, and long-term maintainability.

### Background

Modern Go web frameworks (gin, chi, echo, gorilla/mux) provide extensive feature sets but introduce third-party dependencies and framework-specific patterns. For a security-critical control plane with a multi-year lifecycle, dependency minimization and stdlib stability are paramount.

The Go 1.22+ stdlib `http.ServeMux` now supports method-based routing and path patterns, making it viable for production APIs without framework overhead.

### Problem Statement

We need an HTTP routing architecture that:
- Organizes routes by feature domain (agents, policies, identity)
- Supports flexible middleware composition at multiple levels
- Maintains zero third-party routing dependencies
- Provides clear conventions for route registration and discovery
- Integrates cleanly with our repository pattern and feature-based code organization

### Goals

- **Zero Dependencies**: Use only Go stdlib for routing and middleware
- **Feature Ownership**: Feature packages own their HTTP routes and handlers
- **Middleware Flexibility**: Support server-level, controller-level, and route-level middleware
- **Convention over Configuration**: Clear patterns for route registration without magic
- **Long-term Stability**: Minimize breaking changes from external framework evolution

### Non-Goals

- **Framework Features**: We're not building a full web framework (validation, binding, etc.)
- **Backward Compatibility**: No need to support pre-1.22 ServeMux patterns
- **Auto-Discovery**: Controllers must be explicitly registered, not auto-scanned

---

## Decision Drivers

* **Dependency Minimization**: Security-critical infrastructure should minimize attack surface
* **Long-term Maintainability**: Stdlib APIs are more stable than third-party frameworks
* **Developer Experience**: Familiar patterns from other ecosystems (Swift Vapor, ASP.NET Core)
* **Feature Independence**: Controllers should be colocated with their feature domain logic
* **Testing**: Standard http.Handler interface enables straightforward testing

---

## Considered Options

### Option 1: Third-Party Framework (gin, chi, echo)

**Description:** Use a mature routing framework like gin or chi

**Pros:**
- Rich middleware ecosystem
- Advanced features (parameter binding, validation)
- Large community and examples
- Performance optimizations

**Cons:**
- External dependency with potential security vulnerabilities
- Framework-specific patterns and idioms
- Breaking changes across major versions
- Larger binary size
- Learning curve for framework-specific features

### Option 2: Stdlib ServeMux + Custom Controller Pattern

**Description:** Build a lightweight controller abstraction on top of Go 1.22+ ServeMux

**Pros:**
- Zero external dependencies
- Full control over architecture and patterns
- Stdlib stability guarantees
- Minimal abstraction layer
- Easy to understand and debug
- Familiar patterns from Swift on the server

**Cons:**
- Need to implement our own conventions
- No built-in parameter binding or validation
- Smaller community for specific patterns
- Initial setup effort

### Option 3: Pure Stdlib with No Abstraction

**Description:** Use ServeMux directly without any wrapper layer

**Pros:**
- Absolutely minimal code
- No custom patterns to learn
- Direct stdlib usage

**Cons:**
- No route grouping or organization
- Middleware composition becomes verbose
- Feature routes scattered across codebase
- Difficult to discover all routes for a feature

---

## Decision

**Chosen Option:** Option 2 - Stdlib ServeMux + Custom Controller Pattern

### Rationale

This option provides the best balance of simplicity, maintainability, and developer experience:

1. **Security First**: Zero external routing dependencies eliminates a significant attack surface in security-critical infrastructure
2. **Stdlib Stability**: Go's compatibility promise ensures our routing layer won't break across Go versions
3. **Feature Ownership**: Controllers colocated with feature packages align with our repository pattern (ADR-0003)
4. **Familiar Patterns**: The controller pattern is well-understood from frameworks like Vapor (Swift), ASP.NET Core, and Spring
5. **Minimal Abstraction**: The wrapper layer is ~100 lines of code - easy to understand, debug, and maintain

The pattern is inspired by Swift on the server frameworks, which have proven successful for similar use cases.

---

## Architecture

### Controller Interface

```go
type Controller interface {
    GroupedBy() string                           // Route prefix (e.g., "/agents")
    Middleware() middleware.MiddlewareHandler    // Controller-level middleware
    Boot(*Mux)                                   // Route registration
}
```

### Middleware Composition (Three Tiers)

1. **Server-Level (Default)**: Applied to all routes
   - Example: Request logging, trace context, panic recovery
2. **Controller-Level**: Applied to all routes within a controller
   - Example: Feature-specific auth checks, rate limiting
3. **Route-Level**: Applied to specific endpoints
   - Example: Admin-only middleware, upload size limits

Execution order: `Default → Controller → Route → Handler`

### Mux Wrapper

The `Mux` wrapper around `http.ServeMux` provides:
- Automatic route prefixing based on `GroupedBy()`
- Middleware composition and chaining
- Clean separation between controller routes and server routes

### Example Controller

```go
type AgentsController struct {
    logger  logging.Logger
    service *AgentService
}

func (c *AgentsController) GroupedBy() string {
    return "/agents"
}

func (c *AgentsController) Middleware() middleware.MiddlewareHandler {
    return middleware.MiddlewareChain(
        middleware.RequireAuth(),
        middleware.RateLimit(100),
    )
}

func (c *AgentsController) Boot(mux *api.Mux) {
    mux.Handle("GET /", c.listAgents)
    mux.Handle("POST /", c.createAgent)
    mux.Handle("GET /{id}", c.getAgent)
    mux.Handle("PATCH /{id}", c.updateAgent)
    mux.Handle("DELETE /{id}", c.deleteAgent)
}
```

Routes registered:
- `GET /agents/`
- `POST /agents/`
- `GET /agents/{id}`
- `PATCH /agents/{id}`
- `DELETE /agents/{id}`

---

## Consequences

### Positive

- **Zero External Dependencies**: No third-party routing libraries to audit or update
- **Long-term Stability**: Stdlib compatibility guarantees prevent breaking changes
- **Feature Colocation**: Controllers live with their feature packages, improving discoverability
- **Clear Conventions**: The `Boot()` pattern makes route registration explicit and searchable
- **Flexible Middleware**: Three-tier composition supports diverse security and operational needs
- **Testable**: Standard `http.Handler` interface enables straightforward unit and integration tests
- **Small Surface Area**: The abstraction layer is minimal (~100 LOC) and easy to understand

### Negative

- **Manual Implementation**: We implement our own conventions vs. leveraging framework features
- **No Parameter Binding**: Request parsing and validation must be done manually in handlers
- **Learning Curve**: Developers familiar with gin/chi/echo need to learn our patterns
- **Limited Ecosystem**: Fewer third-party middleware packages compatible with our pattern

### Neutral

- **Convention Enforcement**: Developers must follow the controller pattern - no automatic route discovery
- **Middleware Interface**: We use `func(http.Handler) http.Handler` - compatible with most stdlib middleware
- **Route Registration**: Explicit `Boot()` calls required during server setup

---

## Implementation

### File Structure

```
internal/
├── platform/
│   ├── api/
│   │   ├── api.go          # Server constructor, default middleware
│   │   ├── mux.go          # Mux wrapper with basePath handling
│   │   ├── controller.go   # Controller interface
│   └── middleware/
│       └── middleware.go   # Middleware types and chain builder
├── agents/
│   ├── controller.go       # AgentsController implementation
│   ├── handlers.go         # HTTP handlers for agent operations
│   └── service.go          # Business logic
└── policies/
    ├── controller.go       # PoliciesController implementation
    └── ...
```

### Server Bootstrap

```go
// main.go or cmd/server/main.go
logger := logging.NewLogger()
api := api.NewAPI(logger)

server := api.Server(
    8080,                           // port
    "/api/v1",                      // base path
    customMiddleware,               // server-level custom middleware
    agents.NewController(logger),   // feature controllers
    policies.NewController(logger),
    identity.NewController(logger),
)

server.ListenAndServe()
```

### Migration Path

1. **Phase 1**: Implement base API infrastructure (api.go, mux.go, middleware.go)
2. **Phase 2**: Implement standard middleware (logging, tracing, auth)
3. **Phase 3**: Migrate feature packages to controller pattern one at a time
4. **Phase 4**: Add integration tests for middleware composition and route registration

### Timeline

- **Week 1**: Core API infrastructure
- **Week 2**: Standard middleware suite
- **Week 3-4**: Feature controller migration
- **Week 5**: Integration tests and documentation

---

## Validation

### Success Metrics

- **Zero Routing Dependencies**: No third-party routing libraries in go.mod
- **Route Discovery**: All routes discoverable via code search for `Boot(` or `mux.Handle(`
- **Test Coverage**: >80% coverage for API infrastructure and controllers
- **Performance**: Baseline latency comparable to direct ServeMux usage (<1μs overhead)

### Monitoring

- **Route Registration**: Log all registered routes at startup for visibility
- **Middleware Execution**: Trace middleware chain execution in development
- **Handler Errors**: Monitor panic recovery and error rates per controller

---

## Related Decisions

- [ADR-0003](0003-repository-pattern-and-code-organization.md) - Repository Pattern and Code Organization
- [ADR-0004](0004-dependency-injected-logging.md) - OTEL-Compatible Dependency-Injected Logging

---

## References

- [Go 1.22 ServeMux Enhancements](https://go.dev/blog/routing-enhancements)
- [Swift Vapor Framework](https://docs.vapor.codes/)
- [Effective Go - HTTP Handlers](https://golang.org/doc/effective_go#web_server)

---

## Notes

**Middleware Integration with OTEL:**
The default middleware chain will include trace context extraction and injection (per ADR-0004). Controllers don't need to be aware of tracing - it's handled transparently at the server level.

**Future Enhancements:**
- Auto-generate OpenAPI spec from registered routes
- Route-level metrics collection (per-endpoint latency, error rates)
- Development mode route visualization

**Last Updated:** 2026-02-17
