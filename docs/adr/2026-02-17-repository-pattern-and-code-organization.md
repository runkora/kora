# Repository Pattern and Code Organization

**Status:** Accepted
**Date:** 2026-02-17
**Deciders:** Engineering Team
**Technical Story:** Establishing architectural patterns for data access and code organization as kora transitions from prototype to production system.

---

## Context

As kora grows from initial prototype to a multi-feature control plane, we need to establish clear patterns for code organization and data access that support:

- Multiple feature domains (identity, policy, credentials, agents, runs, gateways, events)
- Shared database infrastructure
- Team autonomy and parallel development
- Testability and maintainability
- Idiomatic Go code

### Background

The kora backend is a Go application serving as a control plane for autonomous agents. It manages multiple concerns including authentication, policy enforcement, credential brokering, agent lifecycle, and gateway routing. Each of these domains has its own business logic and data access needs, but they share common platform infrastructure (database connections, migrations, logging, observability).

### Problem Statement

We need to decide:
1. How to organize code across multiple feature domains
2. Where repository interfaces and implementations should live
3. How to structure the storage layer
4. Whether to follow strict Domain-Driven Design patterns or a more pragmatic approach

The key tension is between:
- **Centralization** (shared repositories, easy to coordinate) vs **Autonomy** (feature-owned repositories, independent development)
- **DDD purity** (strict layering, aggregate roots, value objects) vs **Go idioms** (simplicity, flat packages, concrete types)

### Goals

- Clear ownership of business logic and data access per feature
- Avoid circular dependencies between packages
- Enable parallel development across features
- Maintain testability through interface-based design
- Keep code idiomatic and approachable for Go developers
- Share platform infrastructure without coupling features
- Clear separation between infrastructure (platform) and business logic (features)

### Non-Goals

- Building a reusable framework or library (kora is an application)
- Strict adherence to DDD patterns from other language ecosystems
- Supporting multiple database backends initially (can add later)
- Creating a "clean architecture" with rigid layer separation

---

## Decision Drivers

* **Go idioms** - Go community favors simplicity over abstraction, flat packages over deep hierarchies
* **Feature autonomy** - Teams should be able to work on features independently
* **Avoid circular dependencies** - Common problem when features import each other's types
* **Testability** - Need to mock data access for unit tests
* **Developer experience** - Code should be easy to navigate and understand
* **Shared infrastructure** - Database connections, migrations, transactions should not be duplicated

---

## Considered Options

### Option 1: Centralized Repository Layer

**Description:** All repository interfaces and implementations in a single `internal/repository/` package.

```
internal/
├── repository/
│   ├── user.go
│   ├── agent.go
│   ├── policy.go
│   └── postgres/
│       ├── user.go
│       ├── agent.go
│       └── policy.go
├── identity/
│   ├── service.go
│   └── handlers.go
└── agents/
    ├── service.go
    └── handlers.go
```

**Pros:**
- Single place to find all data access code
- Easy to coordinate database changes
- Clear separation of concerns

**Cons:**
- Creates a bottleneck - all features depend on repository package
- Repository package becomes a "god package" over time
- Features lack autonomy over their data layer
- Not idiomatic Go (mixing concerns across feature boundaries)

### Option 2: Shared Models with Feature Repositories

**Description:** Domain models in `internal/models/`, repositories in each feature package.

```
internal/
├── models/
│   ├── user.go      # Features import these
│   ├── agent.go
│   └── policy.go
├── identity/
│   ├── repository.go
│   ├── postgres.go
│   └── service.go
└── storage/
    └── db.go
```

**Pros:**
- Features own their repositories
- Clear separation between domain and data access

**Cons:**
- **Circular dependency problem:** Features need to import models, but storage implementations (postgres.go) need feature-specific types, creating cycles
- Forces all domain logic into shared models package
- Unclear where validation and business rules belong
- Splits domain knowledge across packages

### Option 3: Strict Domain-Driven Design

**Description:** Separate domain/application/infrastructure layers within each feature.

```
internal/identity/
├── domain/
│   ├── user.go          # Entity
│   ├── token.go         # Value object
│   └── repository.go    # Interface only
├── application/
│   └── service.go       # Use cases
└── infrastructure/
    ├── postgres.go      # Repository impl
    └── http.go          # Handlers
```

**Pros:**
- Clear architectural boundaries
- Follows DDD best practices from literature
- Separates pure domain from technical concerns

**Cons:**
- **Not idiomatic Go** - Go community prefers flat packages
- Deep nesting makes navigation harder
- Verbose for a Go codebase
- Many Go interfaces would have single implementations
- Over-engineered for our needs

### Option 4: Feature-Owned Repositories (Pragmatic Go Approach)

**Description:** Each feature owns its complete stack - interfaces, implementations, models, service, handlers. Storage package provides only infrastructure.

```
internal/
├── platform/            # Infrastructure & cross-cutting concerns
│   ├── storage/         # Database infrastructure
│   │   ├── db.go        # Connection pooling
│   │   ├── transaction.go
│   │   └── migrations/  # SQL files
│   ├── logging/         # Logging infrastructure
│   ├── observability/   # OTEL tracing
│   └── middleware/      # Shared HTTP middleware
├── identity/            # Feature: Identity & Auth
│   ├── repository.go    # Interface
│   ├── postgres.go      # Implementation
│   ├── service.go       # Business logic
│   ├── handlers.go      # HTTP
│   └── models.go        # Types
└── agents/              # Feature: Agent management
    ├── repository.go
    ├── postgres.go
    ├── service.go
    ├── handlers.go
    └── models.go
```

**Pros:**
- **Complete feature autonomy** - each package is self-contained
- **No circular dependencies** - platform never imports features
- **Clear separation** - platform is infrastructure, features are business logic
- **Idiomatic Go** - flat package structure, interfaces when needed
- **Easy navigation** - everything for a feature is in one place
- **Natural testing boundaries** - mock repository interface per feature
- **Shared infrastructure** - database/logging/observability code not duplicated

**Cons:**
- Repository pattern appears in multiple packages (acceptable Go repetition)
- Models package not shared (but keeps features decoupled)
- Some duplication of patterns (but explicit is better than clever)

---

## Decision

**Chosen Option:** Option 4 - Feature-Owned Repositories (Pragmatic Go Approach)

### Rationale

This option best balances **Go idioms** with **DDD principles** while avoiding the pitfalls of both strict centralization and strict DDD layering.

**Why not strict DDD (Option 3)?**
The Go community has consistently shown that deep package hierarchies and heavy abstraction feel unnatural in Go. Projects like Kubernetes, Docker, and Hashicorp tools succeed with flatter structures. The language itself (no inheritance, simple error handling, prefer concrete types) pushes against DDD patterns from Java/C# ecosystems.

**Why not centralized repositories (Option 1)?**
Creates coupling and a bottleneck. Features should own their full stack to enable parallel development and clear ownership.

**Why not shared models (Option 2)?**
The circular dependency problem is real and forces awkward compromises. If `internal/models/user.go` exists and `internal/identity/postgres.go` needs to work with users, where do User methods live? This splits domain knowledge unnaturally.

**Why feature-owned repositories (Option 4)?**
- ✅ Each feature is a **bounded context** (DDD concept we keep)
- ✅ Features are **self-contained** (Go idiom - flat, focused packages)
- ✅ Repository pattern provides **testability** (DDD concept we keep)
- ✅ No circular dependencies (clean dependency graph)
- ✅ Storage package is **pure infrastructure** (clear separation)
- ✅ Some repetition is acceptable (Go proverb: "A little copying is better than a little dependency")

This is **"DDD-lite"** or **"Pragmatic Feature-Based Architecture"** - we keep the valuable DDD concepts (bounded contexts, ubiquitous language, repository pattern) but skip the ceremony (aggregate roots, value objects, deep layering) that doesn't fit Go.

---

## Consequences

### Positive

- **Feature teams can work independently** - no shared repository bottleneck
- **Clear ownership** - identity team owns everything in `internal/identity/`
- **Easy to navigate** - everything related to a feature is in one place
- **Testable** - mock the repository interface for each feature
- **No circular dependencies** - clean, acyclic import graph
- **Idiomatic Go** - feels natural to Go developers
- **Shared infrastructure works well** - `internal/platform/storage/` provides DB without coupling
- **Can refactor freely** - internal packages can be reorganized without breaking external contracts

### Negative

- **Repository pattern appears multiple times** - each feature defines similar interfaces
  - *Mitigation:* This is acceptable repetition in Go. Each feature's needs differ slightly, and explicit is better than generic.
- **No shared domain models** - types are duplicated across features
  - *Mitigation:* Actually a positive - keeps features decoupled. Only share if truly necessary.
- **Learning curve for DDD practitioners** - may feel "wrong" to developers from Java/C# backgrounds
  - *Mitigation:* Document our reasoning. This is idiomatic Go, even if it's not strict DDD.

### Neutral

- **Feature packages are larger** - each contains repository, service, handlers, models
  - This is fine in Go. Better than artificial splitting across layers.
- **Some patterns feel repetitive** - repository.go appears in 8 packages
  - Go values explicit over clever. This is expected.

---

## Implementation

### Standard Feature Package Structure

Every feature follows this pattern:

```
internal/{feature}/
├── repository.go    # Storage interface (contract)
├── postgres.go      # PostgreSQL implementation  
├── service.go       # Business logic
├── handlers.go      # HTTP transport
└── models.go        # Feature-specific types
```

### Platform Package Responsibilities

The `internal/platform/` packages provide **infrastructure only**:

```
internal/platform/
├── storage/         # Database infrastructure
│   ├── db.go        # Database connection & pooling
│   ├── transaction.go
│   ├── migrations.go
│   └── migrations/  # SQL migration files
├── logging/         # Logging infrastructure
│   └── logger.go    # Logger interface & impl
├── observability/   # OTEL tracing & metrics
│   └── observability.go
└── middleware/      # Shared HTTP middleware
    └── auth.go
```

**Critical:** Platform packages never import features. Dependency flow is one-way:
```
Features → Platform (for infrastructure)
Platform ✗ Features (never)
```

### Example Implementation

```go
// internal/identity/repository.go
package identity

type Repository interface {
    GetUserByID(ctx context.Context, id string) (*User, error)
    CreateUser(ctx context.Context, user *User) error
}

// internal/identity/postgres.go
package identity

import "github.com/runkora/kora/backend/internal/platform/storage"

type postgresRepository struct {
    db *storage.DB
}

func NewPostgresRepository(db *storage.DB) Repository {
    return &postgresRepository{db: db}
}

func (r *postgresRepository) GetUserByID(ctx context.Context, id string) (*User, error) {
    // SQL query implementation
}

// internal/identity/service.go
package identity

type Service struct {
    repo Repository
}

func NewService(repo Repository) *Service {
    return &Service{repo: repo}
}

// internal/identity/models.go
package identity

type User struct {
    ID    string
    Email string
    // ... identity-specific fields
}
```

## Validation

### Success Metrics

- **Feature independence:** Can we develop features in parallel without merge conflicts in shared code?
- **Test coverage:** Can we achieve >80% coverage with unit tests using mocked repositories?
- **Onboarding time:** Can new developers find and understand feature code within their first week?
- **Refactoring ease:** Can we refactor a feature's data layer without touching other features?

### Monitoring

- Track import cycles in CI (should be zero)
- Measure test execution time (mocked tests should be fast)
- Survey team on code navigation and ownership clarity

---

## Related Decisions

- [language-and-framework-selection](2026-02-16-language-and-framework-selection.md) - Go language selection (influences this decision)
- See [code-organization.md](../code-organization.md) for complete structure documentation

---

## References

- [Domain-Driven Design by Eric Evans](https://www.domainlanguage.com/ddd/)
- [Go Proverbs](https://go-proverbs.github.io/) - "A little copying is better than a little dependency"
- [Effective Go](https://go.dev/doc/effective_go) - Package organization guidelines
- [Standard Go Project Layout](https://github.com/golang-standards/project-layout) - `internal/` package convention
- [Package Oriented Design by William Kennedy](https://www.ardanlabs.com/blog/2017/02/package-oriented-design.html)

---

## Notes

**On DDD in Go:**

Domain-Driven Design concepts are valuable (bounded contexts, ubiquitous language, repository pattern), but strict DDD patterns from Java/C# feel unnatural in Go. The Go community has evolved its own pragmatic approach:

- **Keep:** Bounded contexts (feature packages), repository pattern, clear domain models
- **Skip:** Deep layering (domain/application/infrastructure), aggregate roots as formal pattern, value objects as distinct from structs
- **Go Way:** Flat packages, interfaces when needed (not everywhere), explicit over clever, some repetition is fine

This ADR embraces "Go with DDD influence" rather than "DDD in Go."

**Last Updated:** 2026-02-17
