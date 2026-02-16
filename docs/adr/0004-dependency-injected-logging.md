# 0004. Dependency-Injected Logging Pattern

**Status:** Accepted
**Date:** 2026-02-16
**Deciders:** Architecture Team
**Technical Story:** Establishing logging infrastructure for the Orbit backend

---

## Context

The Orbit backend requires a logging strategy that supports observability, debugging, and operational monitoring across all features and components. The logging approach must integrate cleanly with Go idioms, support structured logging, and facilitate testing without creating tight coupling between components.

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

### Goals

- Establish a consistent logging pattern across all backend features
- Enable easy testing of components that perform logging
- Support structured logging with context-aware log entries
- Maintain flexibility to change logging implementations without refactoring application code
- Make dependencies explicit in function and constructor signatures

### Non-Goals

- Building a custom logging framework or wrapper
- Supporting multiple logging libraries simultaneously
- Implementing advanced logging features (sampling, dynamic levels, etc.) initially
- Creating a centralized logging service or aggregation layer

---

## Decision Drivers

* **Go community consensus** - Follow established best practices from Go experts
* **Testability** - Must be easy to test components that log
* **Explicitness** - Dependencies should be visible, not hidden in globals
* **Simplicity** - Avoid over-engineering the logging infrastructure
* **Maintainability** - Code should be easy to understand and refactor
* **Performance** - Logging should have minimal overhead
* **Observability** - Support structured logging for modern monitoring tools

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

### Option 2: Dependency Injection with Logger Interface

**Description:** Pass logger as a dependency through constructors and store in struct fields. Define logging interface in consuming packages.

```go
package myfeature

type Logger interface {
    Info(msg string, fields ...any)
    Error(msg string, err error, fields ...any)
}

type Service struct {
    logger Logger
    // other fields
}

func NewService(logger Logger) *Service {
    return &Service{logger: logger}
}

func (s *Service) DoWork() {
    s.logger.Info("doing work")
}
```

**Pros:**
- Explicit dependencies - visible in signatures
- Easy to test - can inject mock logger
- No coupling to specific logger library
- Follows Go best practices (Cheney, Bourgon)
- Clear dependency graph
- Supports interface segregation (each package defines what it needs)

**Cons:**
- More verbose function signatures
- Requires passing logger through construction chain
- Slightly more boilerplate code

**Community Position:** Strongly recommended by Go community leaders

### Option 3: Context-Based Logger

**Description:** Store logger in context.Context and extract when needed.

```go
package myfeature

func DoWork(ctx context.Context) {
    log := logger.FromContext(ctx)
    log.Info("doing work")
}
```

**Pros:**
- Doesn't clutter function signatures
- Can attach request-scoped fields to logger
- Works well with middleware

**Cons:**
- Hidden dependency - not clear from signature
- Context pollution - context used for non-cancellation purpose
- Requires runtime extraction with potential for nil panics
- Testing still requires context manipulation
- Not standard Go practice for dependency management

---

## Decision

**Chosen Option:** Option 2 - Dependency Injection with Logger Interface

### Rationale

We chose dependency injection because it aligns with Go community best practices and provides the clearest, most maintainable code:

1. **Community Consensus**: Dave Cheney and Peter Bourgon, respected Go community members, explicitly advocate for this pattern and against global loggers.

2. **Explicitness Over Magic**: When a function signature includes a logger parameter or when a struct has a logger field, readers can immediately see that logging will occur. This makes code easier to understand and reason about.

3. **Testability**: Injecting a mock logger is trivial compared to manipulating global state or context values. This leads to cleaner, more reliable tests.

4. **No Library Lock-in**: By defining the logging interface in consuming packages, we avoid coupling application code to any specific logging library. We can change implementations without touching feature code.

5. **Clear Dependency Graph**: Dependencies flow in one direction - features depend on the logging interface, not the other way around. This prevents circular dependencies and makes the codebase easier to navigate.

6. **Proven at Scale**: Projects like Go kit have used this pattern successfully, demonstrating its viability for production systems.

The verbosity argument against this approach is outweighed by the benefits of clarity and maintainability. Writing `logger Logger` in a few constructors is a small price for explicit, testable, maintainable code.

---

## Consequences

### Positive

- Clear visibility of which components perform logging
- Easy to test components in isolation with mock loggers
- No coupling between application code and logging library
- Freedom to change logging implementation without refactoring features
- Follows Go community best practices and idioms
- Natural fit with the repository pattern already in use
- Supports structured logging through interface design

### Negative

- Function signatures and constructors are slightly more verbose
- Requires discipline to pass logger through construction chains
- Initial setup requires more boilerplate than global logger
- Team must understand and follow the pattern consistently

### Neutral

- Each package defines its own logger interface (typically small: Info, Error, Debug)
- Logger is a required dependency for most service constructors
- Testing requires explicit logger mock creation

---

## Implementation

**Note:** Implementation details will be completed in a subsequent phase. This ADR establishes the architectural decision.

### High-Level Approach

1. Create `backend/internal/logging` package
2. Define common logger interface
3. Implement concrete logger using Go stdlib `slog`
4. Update service constructors to accept logger parameter
5. Update all feature packages to use injected logger

### Migration Path

Since this is a greenfield project, no migration is needed. All new code will follow this pattern from the start.

---

## Validation

### Success Metrics

- All feature packages use dependency-injected loggers
- Zero package-level logger variables in application code
- All services can be instantiated with mock loggers in tests
- No direct imports of concrete logging libraries in feature packages

### Monitoring

- Code reviews will verify new code follows the pattern
- Linting rules can detect package-level variables
- Test coverage will validate mock logger usage

---

## Related Decisions

- [ADR-0003](0003-repository-pattern-and-code-organization.md) - Repository pattern (similar dependency injection approach)

---

## References

- [Dave Cheney - The package level logger anti pattern](https://dave.cheney.net/2017/01/23/the-package-level-logger-anti-pattern)
- [Peter Bourgon - A theory of modern Go](https://peter.bourgon.org/blog/2017/06/09/theory-of-modern-go.html)
- [Go blog - Structured Logging with slog](https://go.dev/blog/slog)
- [Go kit - Logging design](https://gokit.io/) (uses dependency injection)

---

## Notes

This decision was informed by research into Go community best practices and examination of production Go systems. The pattern is well-established and has proven successful at scale.

**Last Updated:** 2026-02-16
