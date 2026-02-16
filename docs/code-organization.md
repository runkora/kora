# Code Organization

This document defines the canonical code organization for the Orbit backend. All code must follow this structure to ensure consistency, maintainability, and clear architectural boundaries.

## Directory Structure

```
orbit/
├── backend/
│   ├── cmd/
│   │   └── orbit/                    # Single binary entrypoint
│   │       ├── main.go               # Main entry with component registration
│   │       └── commands/             # Subcommands for different modes
│   │           ├── server.go         # Run full server (default)
│   │           ├── gateway.go        # Run gateway components only
│   │           └── worker.go         # Run background workers only
│   │
│   ├── internal/                     # Private application code
│   │   ├── server/                   # HTTP server setup
│   │   │   ├── server.go             # Server initialization
│   │   │   ├── router.go             # Route registration
│   │   │   └── middleware/           # Shared middleware
│   │   │       ├── auth.go           # Authentication middleware
│   │   │       ├── logging.go        # Request logging
│   │   │       ├── ratelimit.go      # Rate limiting
│   │   │       └── tenant.go         # Tenant isolation
│   │   │
│   │   ├── identity/                 # Identity & Auth feature
│   │   │   ├── service.go            # Identity service logic
│   │   │   ├── handlers.go           # HTTP handlers for identity endpoints
│   │   │   ├── repository.go         # Storage interface
│   │   │   ├── postgres.go           # Postgres implementation of repository
│   │   │   ├── provider.go           # Identity provider interface
│   │   │   ├── providers/            # Provider implementations
│   │   │   │   ├── oidc.go           # OIDC provider
│   │   │   │   ├── saml.go           # SAML provider
│   │   │   │   └── local.go          # Local dev provider
│   │   │   ├── token.go              # Token management
│   │   │   └── models.go             # Identity-specific types
│   │   │
│   │   ├── policy/                   # Policy enforcement feature
│   │   │   ├── service.go            # Policy service logic
│   │   │   ├── handlers.go           # HTTP handlers for policy endpoints
│   │   │   ├── repository.go         # Storage interface
│   │   │   ├── postgres.go           # Postgres implementation of repository
│   │   │   ├── engine.go             # Policy engine (OPA integration)
│   │   │   ├── evaluator.go          # Policy evaluation logic
│   │   │   └── models.go             # Policy types & structures
│   │   │
│   │   ├── credentials/              # Credential broker feature
│   │   │   ├── service.go            # Credential broker service
│   │   │   ├── handlers.go           # HTTP handlers for credential endpoints
│   │   │   ├── repository.go         # Storage interface
│   │   │   ├── postgres.go           # Postgres implementation of repository
│   │   │   ├── provider.go           # Credential provider interface
│   │   │   ├── providers/            # Provider implementations
│   │   │   │   ├── vault.go          # HashiCorp Vault
│   │   │   │   ├── aws.go            # AWS Secrets Manager
│   │   │   │   └── memory.go         # In-memory (dev only)
│   │   │   ├── scope.go              # Credential scoping
│   │   │   └── models.go             # Credential types
│   │   │
│   │   ├── agents/                   # Agent management feature
│   │   │   ├── service.go            # Agent service logic
│   │   │   ├── handlers.go           # HTTP handlers for agent endpoints
│   │   │   ├── repository.go         # Storage interface
│   │   │   ├── postgres.go           # Postgres implementation of repository
│   │   │   └── models.go             # Agent types
│   │   │
│   │   ├── runs/                     # Agent run control plane feature
│   │   │   ├── service.go            # Run service logic
│   │   │   ├── handlers.go           # HTTP handlers for run endpoints
│   │   │   ├── repository.go         # Storage interface
│   │   │   ├── postgres.go           # Postgres implementation of repository
│   │   │   ├── controller.go         # Run orchestration
│   │   │   ├── scheduler.go          # Run scheduling
│   │   │   ├── executor.go           # Run execution
│   │   │   ├── state.go              # State management
│   │   │   └── models.go             # Run types
│   │   │
│   │   ├── modelgateway/             # Model gateway feature
│   │   │   ├── service.go            # Model gateway service
│   │   │   ├── handlers.go           # HTTP handlers for model endpoints
│   │   │   ├── repository.go         # Storage interface
│   │   │   ├── postgres.go           # Postgres implementation of repository
│   │   │   ├── router.go             # Model routing logic
│   │   │   ├── providers/            # Model provider adapters
│   │   │   │   ├── openai.go
│   │   │   │   ├── anthropic.go
│   │   │   │   └── azure.go
│   │   │   ├── cache.go              # Response caching
│   │   │   ├── fallback.go           # Fallback logic
│   │   │   └── models.go             # Model gateway types
│   │   │
│   │   ├── toolgateway/              # Tool gateway feature
│   │   │   ├── service.go            # Tool gateway service
│   │   │   ├── handlers.go           # HTTP handlers for tool endpoints
│   │   │   ├── repository.go         # Storage interface
│   │   │   ├── postgres.go           # Postgres implementation of repository
│   │   │   ├── registry.go           # Tool registration
│   │   │   ├── mcp.go                # MCP protocol handling
│   │   │   ├── executor.go           # Tool execution
│   │   │   ├── validator.go          # Request validation
│   │   │   └── models.go             # Tool gateway types
│   │   │
│   │   ├── events/                   # Event spine
│   │   │   ├── emitter.go            # Event emission
│   │   │   ├── processor.go          # Event processing
│   │   │   ├── router.go             # Event routing
│   │   │   ├── repository.go         # Storage interface
│   │   │   ├── postgres.go           # Postgres implementation of repository
│   │   │   ├── types.go              # Event types
│   │   │   └── sinks/                # Event sink implementations
│   │   │       ├── stdout.go
│   │   │       ├── kafka.go
│   │   │       └── webhook.go
│   │   │
│   │   ├── storage/                  # Storage infrastructure
│   │   │   ├── db.go                 # Database connection & pooling
│   │   │   ├── transaction.go        # Transaction utilities
│   │   │   ├── migrations.go         # Migration runner
│   │   │   └── migrations/           # SQL migration files
│   │   │       ├── 001_users.sql
│   │   │       ├── 002_agents.sql
│   │   │       └── 003_policies.sql
│   │   │
│   │   ├── observability/            # Observability integration
│   │   │   ├── metrics.go            # Prometheus metrics
│   │   │   ├── tracing.go            # OpenTelemetry tracing
│   │   │   └── audit.go              # Audit logging
│   │   │
│   │   ├── config/                   # Configuration management
│   │   │   ├── config.go             # Config loading
│   │   │   ├── defaults.go           # Default values
│   │   │   └── validate.go           # Config validation
│   │   │
│   │   ├── models/                   # Shared domain models
│   │   │   ├── identity.go
│   │   │   ├── agent.go
│   │   │   ├── run.go
│   │   │   ├── policy.go
│   │   │   ├── credential.go
│   │   │   └── event.go
│   │   │
│   │   ├── errors/                   # Error types & handling
│   │   │   ├── codes.go              # Error codes
│   │   │   ├── errors.go             # Error types
│   │   │   └── handling.go           # Error handling utilities
│   │   │
│   │   ├── logging/                  # Logging infrastructure
│   │   │   ├── logger.go             # Logger interface
│   │   │   └── slog.go               # slog implementation
│   │   │
│   │   ├── auth/                     # Authentication utilities
│   │   │   ├── jwt.go                # JWT utilities
│   │   │   ├── context.go            # Auth context
│   │   │   └── claims.go             # Token claims
│   │   │
│   │   └── httputil/                 # HTTP utilities
│   │       ├── client.go             # HTTP client
│   │       ├── response.go           # Response helpers
│   │       └── request.go            # Request helpers
│   │
│   ├── deployments/                  # Deployment configurations
│   │   ├── docker/
│   │   │   ├── Dockerfile.api
│   │   │   ├── Dockerfile.gateway
│   │   │   └── docker-compose.yml
│   │   ├── kubernetes/
│   │   │   ├── api/
│   │   │   ├── gateways/
│   │   │   └── workers/
│   │   └── terraform/                # Infrastructure as code
│   │
│   ├── scripts/                      # Development & ops scripts
│   │   ├── dev-setup.sh
│   │   ├── migrate.sh
│   │   └── seed-data.sh
│   │
│   ├── tests/                        # Integration & E2E tests
│   │   ├── integration/
│   │   └── e2e/
│   │
│   ├── go.mod
│   ├── go.sum
│   ├── Makefile
│   └── README.md
│
├── frontend/                         # Future: Admin UI/Dashboard
│   └── .gitkeep
│
├── docs/
│   ├── adr/
│   ├── prd/
│   ├── api/                          # API documentation
│   │   ├── control-plane.md
│   │   ├── model-gateway.md
│   │   └── tool-gateway.md
│   ├── architecture.md
│   └── code-organization.md          # This file
│
└── README.md
```

## Architecture Principles

### 1. Single Binary with Flexible Deployment

The `orbit` binary supports multiple deployment modes through component registration:

```bash
orbit server                    # Run everything (default)
orbit server --api-only        # Run only API server
orbit server --gateways-only   # Run only gateways
orbit worker                   # Run background workers
```

**Benefits:**
- Simple deployment (single binary)
- Easy local development
- Flexible scaling (run components separately when needed)
- Clean migration path (can split to microservices later)

### 2. Feature-Based Organization

Each feature/domain is a self-contained package in `internal/`:
- All related code lives together (service, handlers, storage, models)
- Clear ownership and easier navigation
- No artificial layering between transport and business logic
- Promotes high cohesion and low coupling

**Core features:**
- `internal/identity/` - Identity & authentication
- `internal/policy/` - Policy enforcement
- `internal/credentials/` - Credential broker
- `internal/agents/` - Agent management
- `internal/runs/` - Run control plane
- `internal/modelgateway/` - Model gateway
- `internal/toolgateway/` - Tool gateway
- `internal/events/` - Event spine

### 3. Everything in `internal/`

All application code lives in `internal/` because Orbit is an application, not a library:
- Prevents external projects from importing our internals
- Keeps refactoring options open
- Forces clear API boundaries
- Shared packages (models, errors, auth) are internal utilities

**Note:** If we later build an official Orbit Go SDK, we would create `pkg/sdk/` explicitly for that purpose.

### 4. Repository Pattern for Data Access

Each feature owns its complete data layer:

```
internal/{feature}/
├── repository.go   # Interface definition (contract)
├── postgres.go     # PostgreSQL implementation
├── service.go      # Business logic
├── handlers.go     # HTTP handlers
└── models.go       # Feature-specific types
```

**Dependency flow:**
```
Feature packages → internal/storage (for DB connection)
                 → internal/models (for shared types)

internal/storage → Does NOT import features
internal/models  → Does NOT import features
```

The `internal/storage/` package provides only infrastructure:
- Database connection & pooling (`db.go`)
- Transaction utilities (`transaction.go`)
- Migration runner (`migrations.go`)
- SQL migration files (`migrations/`)

**Benefits:**
- Features have complete autonomy over their data
- No circular dependencies
- Easy to test with mock repositories
- Clear separation between business logic and data access
- Shared database infrastructure prevents duplication

### 5. Pluggable Providers

Each integration point uses the provider pattern for extensibility:

- **Identity providers** (`internal/identity/providers/`)
  - OIDC, SAML, local dev
- **Credential providers** (`internal/credentials/providers/`)
  - Vault, AWS Secrets Manager, in-memory
- **Model providers** (`internal/modelgateway/providers/`)
  - OpenAI, Anthropic, Azure OpenAI
- **Event sinks** (`internal/events/sinks/`)
  - stdout, Kafka, webhooks

## Standard Feature Package Structure

Every feature follows this consistent structure:

```
internal/{feature}/
├── service.go      # Business logic and orchestration
├── handlers.go     # HTTP request/response handling
├── repository.go   # Storage interface (defines contract)
├── postgres.go     # PostgreSQL implementation
├── models.go       # Feature-specific domain types
├── provider.go     # Provider interface (if applicable)
└── providers/      # Provider implementations (if applicable)
    ├── impl1.go
    └── impl2.go
```

**Responsibilities:**

- **service.go**: Core business logic, orchestrates between repository and providers
- **handlers.go**: HTTP transport layer, request validation, response formatting
- **repository.go**: Defines data access contract (interface)
- **postgres.go**: Implements repository interface for PostgreSQL
- **models.go**: Feature-specific types and domain models
- **provider.go**: Defines external integration contract (interface)
- **providers/**: Implements provider interface for specific backends

## Shared Infrastructure

### Storage (`internal/storage/`)

Provides database infrastructure used by all features:

```go
// db.go - Database connection
type DB struct {
    *sql.DB
    // connection pooling, health checks
}

// transaction.go - Transaction utilities
func WithTransaction(ctx context.Context, db *DB, fn func(*sql.Tx) error) error

// migrations.go - Migration runner
func RunMigrations(db *DB) error
```

### Models (`internal/models/`)

Shared domain types used across multiple features. Keep minimal - prefer feature-specific models when possible.

### Server (`internal/server/`)

HTTP server setup and shared middleware:
- Server initialization and lifecycle
- Route registration
- Authentication, logging, rate limiting, tenant isolation

### Logging (`internal/logging/`)

Provides logging infrastructure using dependency injection pattern:
- Logger interface for feature packages to depend on
- Concrete implementation using Go's stdlib slog
- Passed to services via constructors (no global loggers)

### Observability (`internal/observability/`)

Metrics, tracing, and audit logging shared across all features.

## Implementation Guidelines

### Adding a New Feature

1. Create feature package in `internal/{feature}/`
2. Define domain models in `models.go`
3. Define repository interface in `repository.go`
4. Implement repository in `postgres.go`
5. Implement business logic in `service.go`
6. Add HTTP handlers in `handlers.go`
7. Register routes in `internal/server/router.go`
8. Add database migrations in `internal/storage/migrations/`

### Adding Storage to a Feature

```go
// repository.go - Define contract
package identity

type Repository interface {
    GetUserByID(ctx context.Context, id string) (*User, error)
    CreateUser(ctx context.Context, user *User) error
}

// postgres.go - Implement for PostgreSQL
package identity

import "github.com/clear-route/orbit/internal/storage"

type postgresRepository struct {
    db *storage.DB
}

func NewPostgresRepository(db *storage.DB) Repository {
    return &postgresRepository{db: db}
}

func (r *postgresRepository) GetUserByID(ctx context.Context, id string) (*User, error) {
    // Implementation using r.db
}
```

### Testing

- Unit tests alongside implementation files: `service_test.go`, `handlers_test.go`
- Integration tests in `tests/integration/`
- E2E tests in `tests/e2e/`
- Mock repositories for testing business logic

## References

See [architecture.md](architecture.md) for high-level architectural decisions and patterns.
