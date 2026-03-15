# Code Organization

This document defines the canonical code organization for the Orbit backend. It reflects both the **current codebase** and **planned future features**, clearly distinguished. Planned packages do not exist on disk yet but are documented here so engineers and AI agents know exactly where to create them.

---

## Directory Structure

```
orbit/
├── backend/
│   ├── cmd/
│   │   └── orbit/                        # Single binary entrypoint
│   │       ├── main.go                   # Application wiring: init, register, serve
│   │       └── config/
│   │           ├── config.go             # AppConfig struct (server port, OTLP, sampling)
│   │           ├── config_test.go
│   │           ├── local.yaml
│   │           ├── development.yaml
│   │           └── production.yaml
│   │
│   ├── internal/
│   │   ├── platform/                     # Cross-cutting infrastructure (never imports features)
│   │   │   ├── api/
│   │   │   │   ├── api.go                # JSON helpers: DecodeJSON, JSON response writer
│   │   │   │   ├── controller.go         # Controller interface definition
│   │   │   │   ├── mux.go                # HTTP multiplexer + route registration
│   │   │   │   ├── error.go              # APIError type + standard error codes
│   │   │   │   └── response.go           # Typed response helpers
│   │   │   │
│   │   │   ├── config/
│   │   │   │   └── config.go             # config.Load[T]() generic YAML loader
│   │   │   │
│   │   │   ├── logging/
│   │   │   │   ├── logger.go             # Logger interface + Field struct
│   │   │   │   ├── zerolog.go            # zerolog implementation
│   │   │   │   └── README.md
│   │   │   │
│   │   │   ├── middleware/
│   │   │   │   ├── middleware.go         # MiddlewareHandler type
│   │   │   │   ├── logging.go            # Structured request logging
│   │   │   │   └── tracing.go            # OpenTelemetry trace injection
│   │   │   │
│   │   │   ├── observability/
│   │   │   │   ├── observability.go      # OTEL TracerProvider + MeterProvider setup
│   │   │   │   └── README.md
│   │   │   │
│   │   │   └── storage/
│   │   │       ├── config.go             # StorageConfig (postgres + redis nested struct)
│   │   │       ├── postgres_config.go    # PostgresConfig + env var injection
│   │   │       ├── postgres_pool.go      # pgxpool.Pool builder + OTel query tracer
│   │   │       ├── db.go                 # DB type wrapping pgxpool.Pool + healthcheck
│   │   │       ├── transaction.go        # WithTransaction helper
│   │   │       ├── redis_config.go       # RedisConfig + env var injection
│   │   │       ├── redis.go              # Redis type wrapping go-redis/v9 client
│   │   │       ├── config/
│   │   │       │   ├── local.yaml
│   │   │       │   ├── development.yaml
│   │   │       │   └── production.yaml
│   │   │       └── migrations/           # Goose SQL migration files
│   │   │           ├── 20260218215737_create_example_tables.sql
│   │   │           └── 20260310000000_create_identity_tables.sql
│   │   │
│   │   ├── identity/                     # Identity & authentication feature (implemented)
│   │   │   ├── models.go                 # Principal, Session, OrbitClaims, request/response types
│   │   │   ├── repository.go             # SessionRepository, RefreshTokenRepository, JTIBlocklistRepository
│   │   │   ├── postgres.go               # pgx implementations of SessionRepository + RefreshTokenRepository
│   │   │   ├── redis.go                  # go-redis implementation of JTIBlocklistRepository
│   │   │   ├── service.go                # Service: OIDC flow, token issuance, session lifecycle
│   │   │   ├── handlers.go               # HTTP handlers: device, session, refresh, exchange, revoke
│   │   │   └── config/
│   │   │       ├── local.yaml            # issuerUrl: http://localhost:5556/dex
│   │   │       ├── development.yaml
│   │   │       └── production.yaml
│   │   │
│   │   ├── agents/                       # (planned) Agent registration, identity binding, autonomy levels
│   │   │   ├── models.go
│   │   │   ├── repository.go
│   │   │   ├── postgres.go
│   │   │   ├── service.go
│   │   │   ├── handlers.go
│   │   │   └── config/
│   │   │
│   │   ├── policy/                       # (planned) OPA integration, RBAC/PBAC evaluation, policy bundles
│   │   │   ├── models.go
│   │   │   ├── repository.go
│   │   │   ├── postgres.go
│   │   │   ├── service.go
│   │   │   ├── handlers.go
│   │   │   └── config/
│   │   │
│   │   ├── credentials/                  # (planned) Credential broker: scoped provisioning, OBO flows, Vault
│   │   │   ├── models.go
│   │   │   ├── repository.go
│   │   │   ├── postgres.go
│   │   │   ├── redis.go                  # Token cache / blocklist
│   │   │   ├── service.go
│   │   │   ├── handlers.go
│   │   │   └── config/
│   │   │
│   │   ├── gateway/                      # (planned) Outbound gateway / ext-authz proxy for agent external calls
│   │   │   ├── models.go
│   │   │   ├── repository.go
│   │   │   ├── postgres.go
│   │   │   ├── service.go
│   │   │   ├── handlers.go
│   │   │   └── config/
│   │   │
│   │   ├── runs/                         # (planned) Agent run lifecycle, execution tracking, audit trail
│   │   │   ├── models.go
│   │   │   ├── repository.go
│   │   │   ├── postgres.go
│   │   │   ├── service.go
│   │   │   ├── handlers.go
│   │   │   └── config/
│   │   │
│   │   ├── events/                       # (planned) Structured event emission and routing (event spine)
│   │   │   ├── models.go
│   │   │   ├── repository.go
│   │   │   ├── postgres.go
│   │   │   ├── service.go
│   │   │   ├── handlers.go
│   │   │   └── config/
│   │   │
│   │   └── example/                      # Reference implementation (not production code)
│   │       ├── controller.go             # Demonstrates Controller interface + route registration
│   │       └── repository.go             # Demonstrates repository pattern with storage.DB
│   │
│   ├── go.mod
│   └── go.sum
│
├── config/
│   └── dex/
│       ├── config.yaml                   # Dex OIDC provider config (static user, orbit-local client)
│       └── .env                          # DEX_USER_PASSWORD_HASH — gitignored, generated by make dex-env
│
├── scripts/
│   └── auth-device.sh                    # Device Authorization Grant CLI flow
│
├── secrets/                              # gitignored — local secrets only
│   └── signing.key                       # RSA private key, generated by make gen-signing-key
│
├── docs/
│   ├── adr/                              # Architecture Decision Records
│   ├── prd/                              # Product Requirements Documents
│   ├── guides/
│   │   └── local-development.md          # Full local setup walkthrough
│   ├── configuration/
│   │   ├── config.md                     # YAML config system reference
│   │   └── secrets.md                    # Secret management reference
│   ├── architecture.md
│   └── code-organization.md              # This file
│
├── docker-compose.yml                    # Tier 1: Postgres + Redis + Dex
├── Makefile
├── .env.example
├── AGENTS.md                             # AI agent coding conventions
├── README.md
└── SECURITY.md
```

---

## Platform vs Features

### Platform Layer (`internal/platform/`)

Infrastructure packages shared by all features. Platform packages:
- **never import feature packages** — dependency flow is strictly one-directional
- provide building blocks that features wire themselves to via constructors
- are fully tested in isolation

| Package | Purpose |
|---|---|
| `api` | HTTP multiplexer, `Controller` interface, JSON helpers, typed errors |
| `config` | Generic YAML loader `config.Load[T any]()` keyed by `APP_ENV` |
| `logging` | `Logger` interface + zerolog implementation; injected, never global |
| `middleware` | Request logging, OTel trace injection; composed in the `Mux` |
| `observability` | OTEL `TracerProvider` + `MeterProvider` setup, OTLP exporter |
| `storage` | PostgreSQL (`*pgxpool.Pool` via `storage.DB`) + Redis (`storage.Redis`); config, pool builder, migrations |

### Feature Layer (`internal/`)

Domain packages owning a complete vertical slice:

| Package | Status | Description |
|---|---|---|
| `identity` | ✅ Implemented | OIDC federation, Device Authorization Grant, session lifecycle, JWT issuance, revocation |
| `agents` | 🔜 Planned | Agent registration, identity binding, autonomy level management |
| `policy` | 🔜 Planned | OPA integration, RBAC/PBAC evaluation, policy bundle management |
| `credentials` | 🔜 Planned | Credential broker: scoped provisioning, OBO flows, Vault integration (ADR-0012) |
| `gateway` | 🔜 Planned | Outbound gateway / ext-authz proxy for agent external service calls (ADR-0011, ADR-0012) |
| `runs` | 🔜 Planned | Agent run lifecycle, execution tracking, audit trail |
| `events` | 🔜 Planned | Structured event emission and routing (event spine) |

The `example` package is a reference implementation demonstrating the correct patterns. It is not a production feature and will be removed when the first post-identity feature is added.

---

## Standard Feature Structure

Every feature follows this exact file layout:

```
internal/{feature}/
├── models.go       # Domain types, request/response structs, errors
├── repository.go   # Storage interface(s) — the contract, not the implementation
├── postgres.go     # PostgreSQL implementation of repository interfaces
├── redis.go        # Redis implementation (if the feature uses Redis directly)
├── service.go      # Business logic; orchestrates repositories and external clients
├── handlers.go     # HTTP transport: decodes requests, calls service, writes responses
└── config/
    ├── local.yaml
    ├── development.yaml
    └── production.yaml
```

Features that require Redis (e.g. for caching or blocklists) add a `redis.go` alongside `postgres.go`. There is no `providers/` subdirectory — OIDC and other external integrations live directly in `service.go` or as unexported helpers in the same package.

---

## Key Patterns

### 1. HTTP Routing via `api.Controller`

There is no central router file. Each feature registers its own routes by implementing the `api.Controller` interface:

```go
// internal/platform/api/controller.go
type Controller interface {
    GroupedBy() string                      // URL prefix, e.g. "/identity"
    Middleware() middleware.MiddlewareHandler
    Boot(mux *api.Mux)                     // Register routes on the mux
}
```

`main.go` instantiates each feature's controller and calls `mux.Register(ctrl)`. Adding a new feature never requires touching existing files — only `main.go` needs updating.

```go
// backend/cmd/orbit/main.go
mux.Register(identity.NewController(identitySvc))
mux.Register(example.NewController(db))
// future: mux.Register(agents.NewController(agentsSvc))
```

### 2. Dependency-Injected Logger

Global loggers are forbidden. Every struct that logs accepts a `*logging.Logger` in its constructor:

```go
type Service struct {
    logger *logging.Logger
    // ...
}

// All log calls take ctx as first argument for OTel trace extraction
s.logger.Info(ctx, "session created", logging.Field{Key: "session_id", Value: id})
```

The `Logger` interface is defined in `internal/platform/logging/logger.go`. The zerolog implementation is in `zerolog.go`. Features depend on the interface, not the implementation.

### 3. Database Access via `storage.DB`

All database access uses explicit SQL. ORMs are not used. The `storage.DB` type wraps a `pgxpool.Pool`:

```go
// internal/platform/storage/db.go
type DB struct {
    Pool *pgxpool.Pool
}
```

Feature repository implementations receive `*storage.DB` via constructor injection:

```go
// internal/identity/postgres.go
type postgresSessionRepository struct {
    db *storage.DB
}

func (r *postgresSessionRepository) Create(ctx context.Context, s *Session) error {
    _, err := r.db.Pool.Exec(ctx,
        `INSERT INTO identity_sessions (id, principal_id, ...) VALUES ($1, $2, ...)`,
        s.ID, s.PrincipalID, ...)
    return err
}
```

### 4. Redis Access via `storage.Redis`

Redis is a first-class storage primitive alongside PostgreSQL. The `storage.Redis` type wraps a `*redis.Client`:

```go
// internal/platform/storage/redis.go
type Redis struct {
    Client *redis.Client
}
```

Features that need Redis receive `*storage.Redis` via constructor injection. The identity service uses it for the JTI blocklist (`orbit:jti:blocklist:{jti}`):

```go
// internal/identity/redis.go
type redisJTIBlocklistRepository struct {
    redis *storage.Redis
}
```

### 5. Configuration via `config.Load[T any]()`

Configuration is environment-aware YAML, loaded at startup. Each component has its own config directory with one file per environment:

```
internal/{feature}/config/
├── local.yaml
├── development.yaml
└── production.yaml
```

The `config.Load[T any]()` function reads the file matching `APP_ENV` (defaults to `local`):

```go
// Load the identity service config for the current environment
cfg, err := config.Load[identity.Config]("internal/identity/config", config.Environment())
```

**Secrets never appear in YAML.** Any secret field on a config struct must be tagged `yaml:"-"` and loaded separately via `os.Getenv`. See [docs/configuration/secrets.md](configuration/secrets.md) for the full reference.

### 6. Storage Config is Unified

Storage configuration uses a single two-level YAML structure — both PostgreSQL and Redis live in the same environment file:

```yaml
# internal/platform/storage/config/local.yaml
postgres:
  host: localhost
  port: 5432
  ...

redis:
  addr: localhost:6379
  ...
```

`storage.LoadStorageConfig()` parses the unified config and returns a `StorageConfig` with both `Postgres` and `Redis` sub-configs. Environment variable overrides (`DATABASE_URL`, `REDIS_ADDR`, etc.) are applied after YAML parsing inside `loadPostgresConfig` and `loadRedisConfig`.

### 7. Migrations via Goose

SQL migrations live in `internal/platform/storage/migrations/` and use Goose annotations:

```sql
-- +goose Up
-- +goose StatementBegin
CREATE TABLE ...;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE ...;
-- +goose StatementEnd
```

**Never write migration files by hand.** Always generate them:

```bash
make migrate-create name=create_agents_table
```

This ensures correct timestamp ordering and Goose-compatible format.

---

## Application Entry Point

`backend/cmd/orbit/main.go` is the only wiring file. It:

1. Loads `AppConfig` (server port, OTLP endpoint, sampling rate)
2. Initializes `observability` (TracerProvider, MeterProvider)
3. Calls `initStorage()` — loads `StorageConfig`, creates `*storage.DB` and `*storage.Redis`
4. Runs Goose migrations against the DB
5. Initializes each feature service with its dependencies
6. Constructs the `api.Mux`, registers each feature's `Controller`
7. Starts the HTTP server

There are no subcommands (`server`, `worker`, `gateway`). The binary runs a single mode. Deployment topology (running components separately) is a future concern addressed when the scale justification exists.

---

## Adding a New Feature

Follow these steps to add a new feature package:

1. **Create the package** at `internal/{feature}/`
2. **Define models** in `models.go` — domain types, request/response structs, feature-specific errors
3. **Define repository interfaces** in `repository.go` — storage contracts; keep methods small and focused
4. **Implement PostgreSQL** in `postgres.go` — explicit SQL only, inject `*storage.DB`
5. **Implement Redis** in `redis.go` — if the feature needs Redis, inject `*storage.Redis`
6. **Write business logic** in `service.go` — call repositories, external clients, issue events
7. **Write HTTP handlers** in `handlers.go` — implement `api.Controller`, register routes via `Boot(*api.Mux)`
8. **Add config files** in `config/{local,development,production}.yaml`
9. **Generate migrations** with `make migrate-create name=create_{feature}_tables`
10. **Register in `main.go`** — instantiate the service and call `mux.Register(feature.NewController(svc))`

The `internal/example/` package demonstrates steps 3–8 with working code. Read it before implementing the first route in a new feature.

---

## Platform Infrastructure Reference

### `internal/platform/api`

The HTTP layer abstraction. Key types:

```go
// Controller — implement this in every feature
type Controller interface {
    GroupedBy() string
    Middleware() middleware.MiddlewareHandler
    Boot(mux *Mux)
}

// Mux — constructed in main.go, passed to each Controller.Boot()
type Mux struct { ... }
func (m *Mux) Register(c Controller)
func (m *Mux) ServeHTTP(w http.ResponseWriter, r *http.Request)

// JSON helpers — used in handlers
func JSON(w http.ResponseWriter, status int, v any) error
func DecodeJSON(r *http.Request, v any) error

// Errors — return these from handlers
func BadRequest(code, message string) *APIError
func Unauthorized(code, message string) *APIError
func NotFound(code, message string) *APIError
func InternalError() *APIError
```

### `internal/platform/logging`

```go
type Logger interface {
    Debug(ctx context.Context, msg string, fields ...Field)
    Info(ctx context.Context, msg string, fields ...Field)
    Warn(ctx context.Context, msg string, fields ...Field)
    Error(ctx context.Context, msg string, fields ...Field)
    With(fields ...Field) Logger
}

type Field struct {
    Key   string
    Value interface{}
}
```

Construct a logger in `main.go` and inject it into every service:

```go
logger := logging.NewZerolog(os.Stdout, logging.LevelInfo)
svc := identity.NewService(db, redis, logger, cfg)
```

### `internal/platform/storage`

```go
// PostgreSQL — wraps pgxpool.Pool
type DB struct {
    Pool *pgxpool.Pool
}
func (db *DB) HealthCheck(ctx context.Context) error

// Redis — wraps go-redis/v9 client
type Redis struct {
    Client *redis.Client
}
func (r *Redis) HealthCheck(ctx context.Context) error

// Transaction helper
func WithTransaction(ctx context.Context, db *DB, fn func(pgx.Tx) error) error

// Unified config loader
func LoadStorageConfig() (*StorageConfig, error)
type StorageConfig struct {
    Postgres PostgresConfig
    Redis    RedisConfig
}
```

### `internal/platform/config`

```go
// Load YAML config for the current APP_ENV
// path is relative to the backend/ directory
func Load[T any](path string, env string) (T, error)

// Returns APP_ENV value (defaults to "local")
func Environment() string
```

---

## What This Codebase Is Not

To prevent common mistakes:

- **No ORM** — `*pgxpool.Pool` and explicit SQL only; no GORM, Ent, sqlx, or similar
- **No third-party router** — no Gorilla Mux, Chi, Gin, Echo, or Fiber; routing goes through `api.Mux` and the `Controller` interface
- **No global logger** — no `log.Println`, no package-level `var logger = ...`; inject `*logging.Logger` everywhere
- **No `internal/models/` package** — shared domain types do not exist; each feature owns its models
- **No `internal/server/router.go`** — there is no central route registry; features self-register via `Controller.Boot()`
- **No `commands/` subdir in `cmd/orbit/`** — no subcommands; `main.go` runs one mode
- **No `providers/` subdirectory in features** — OIDC and external integrations live in `service.go`
- **No hand-written migration files** — always use `make migrate-create name=X`
- **No secrets in YAML** — use `yaml:"-"` tags and `os.Getenv`; see `docs/configuration/secrets.md`

---

## References

- [ADR-0003](adr/0003-repository-pattern-and-code-organization.md) — Repository pattern and code organization rationale
- [ADR-0004](adr/0004-dependency-injected-logging.md) — Dependency-injected logging
- [ADR-0005](adr/0005-controller-based-http-routing.md) — Controller-based HTTP routing
- [ADR-0006](adr/0006-yaml-file-based-configuration.md) — YAML configuration system
- [ADR-0007](adr/0007-database-infrastructure-layer.md) — Database infrastructure layer
- [ADR-0017](adr/0017-oidc-provider-abstraction-and-local-identity-provider.md) — OIDC abstraction and local Dex setup
- [docs/configuration/secrets.md](configuration/secrets.md) — Secret management reference
- [docs/guides/local-development.md](guides/local-development.md) — Local setup walkthrough
