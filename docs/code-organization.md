# Code Organization

This document defines the canonical code organization for the Orbit backend. It reflects both the **current codebase** and **planned future features**, clearly distinguished. Planned packages do not exist on disk yet but are documented here so engineers and AI agents know exactly where to create them.

---

## Directory Structure

```
orbit/
├── backend/
│   ├── cmd/                              # One subdirectory per deployable binary (ADR-0020)
│   │   ├── orbit/                        # Main API server
│   │   │   ├── main.go                   # Application wiring: init, register, serve
│   │   │   └── config/
│   │   │       ├── config.go             # AppConfig struct (server port, OTLP, sampling)
│   │   │       ├── config_test.go
│   │   │       ├── local.yaml
│   │   │       ├── development.yaml
│   │   │       └── production.yaml
│   │   │
│   │   ├── identity/                     # Standalone identity service binary
│   │   │   ├── main.go
│   │   │   └── config/
│   │   │       ├── config.go
│   │   │       └── local.yaml
│   │   │
│   │   ├── identity-proxy/               # Envoy ext_proc sidecar (ADR-0021)
│   │   │   ├── main.go                   # Wires identityproxy.Server + identity platform client
│   │   │   └── config/
│   │   │       ├── config.go             # IdentityProxyConfig (socket path, SPIFFE socket, service URL)
│   │   │       └── base.yaml
│   │   │
│   │   └── example/                      # Reference binary (not production)
│   │       ├── main.go
│   │       └── config/
│   │           └── config.go
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
│   │   │   ├── clients/                  # Typed service-to-service HTTP clients (ADR-0020)
│   │   │   │   ├── httpclient/
│   │   │   │   │   └── base.go           # Base HTTP client: OTel span, W3C trace injection, structured logging
│   │   │   │   └── identity/
│   │   │   │       └── client.go         # Identity service client — token exchange, sentinel errors
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
│   │   │       ├── local.yaml            # issuerUrl: https://dex.orbit.local.clearroute.io:5556/dex
│   │   │       ├── development.yaml
│   │   │       └── production.yaml
│   │   │
│   │   ├── identityproxy/                # Envoy ext_proc sidecar feature package (implemented, ADR-0021)
│   │   │   ├── server.go                 # ExternalProcessorServer: header inspection, token injection
│   │   │   ├── cache.go                  # Thread-safe in-memory token cache with TTL
│   │   │   ├── refresh.go                # Background token refresh loop
│   │   │   ├── config.go                 # IdentityProxyConfig struct
│   │   │   └── identityproxy_test.go
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

### 5. Service-to-Service Calls via Platform Clients (ADR-0020)

All outbound HTTP calls to other Orbit services go through typed clients in `internal/platform/clients/{service}/client.go`. Each client embeds `httpclient.Base`, which automatically:
- Opens an OTel `SpanKindClient` span
- Injects W3C `traceparent`/`tracestate` headers into every request
- Sets standard semantic convention attributes (`http.request.method`, `url.full`, `http.response.status_code`)
- Captures upstream error bodies in the `error.upstream_body` span attribute
- Emits a structured log line for every transport outcome

```go
// internal/platform/clients/identity/client.go
type Client struct {
    base    *httpclient.Base
    baseURL string
}

func (c *Client) Exchange(ctx context.Context, svid string) (*TokenResponse, error) {
    req, _ := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/v1/identity/exchange", body)
    _, span, resp, err := c.base.Do(ctx, "identity.exchange", req)
    defer span.End()
    // ...
}
```

**Never construct `http.Client{}` directly** in feature or service code. The `noRawHttpClientLiteral` lint rule enforces this.

New service clients follow this layout:
```
internal/platform/clients/
└── {service}/
    └── client.go     # Typed client: struct, constructor, methods, sentinel errors
```

Wire the client into the binary's `main.go` via the constructor — never share raw HTTP clients across packages.

### 6. Configuration via `config.Load[T any]()`

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

### 7. Storage Config is Unified

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

### 8. Migrations via Goose

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

## Application Entry Points

The backend compiles to multiple binaries (ADR-0020). Each binary under `backend/cmd/` has its own `main.go` wiring file. All binaries share `backend/internal/platform/` but are otherwise fully independent.

### `cmd/orbit` — Main API server

1. Loads `AppConfig` (server port, OTLP endpoint, sampling rate)
2. Initializes `observability` (TracerProvider, MeterProvider)
3. Calls `initStorage()` — loads `StorageConfig`, creates `*storage.DB` and `*storage.Redis`
4. Runs Goose migrations against the DB
5. Initializes each feature service with its dependencies
6. Constructs the `api.Mux`, registers each feature's `Controller`
7. Starts the HTTP server

### `cmd/identity-proxy` — Envoy ext_proc sidecar (ADR-0021)

1. Loads `IdentityProxyConfig` (Unix socket path, SPIFFE socket, identity service URL)
2. Initializes `observability`
3. Builds an `identity.Client` (platform typed HTTP client) backed by `httpclient.Base`
4. Creates the `identityproxy.Server` — a gRPC ext_proc server that intercepts outbound agent calls and injects a platform Bearer token
5. Polls `http://localhost:15021/healthz/ready` until Envoy/Istio xDS configuration is fully received
6. Watches SPIFFE SVID rotations and invalidates the token cache on each new SVID
7. Serves the gRPC ext_proc stream on a Unix domain socket

### `cmd/identity` — Standalone identity service

Runs the identity feature as a standalone HTTP binary for deployments where the main `orbit` binary is split by topology.

### Adding a new binary

Create `cmd/{name}/` with its own `main.go` and `config/` subdirectory. Import only from `internal/platform/` and the feature packages the binary owns. Never import across binary entry-point boundaries — cross-cutting concerns belong in `internal/platform/`.

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

// JSON response writer — writes status + JSON body, records OTel span outcome, structured log
func JSON(ctx context.Context, w http.ResponseWriter, logger logging.Logger, status int, body any)

// Request decoder — decodes JSON body into dest; writes 400 + returns false on failure
func DecodeJSON(ctx context.Context, r *http.Request, w http.ResponseWriter, logger logging.Logger, dest any) bool

// Error response — writes JSON error envelope, sets span status, structured log
func ErrorResponse(ctx context.Context, w http.ResponseWriter, logger logging.Logger, apiErr *APIError, underlyingErr error)

// APIError constructors — all accept optional details map for structured context
func NotFoundError(code, message string, details map[string]any) *APIError
func ValidationError(code, message string, details map[string]any) *APIError
func BadRequestError(code, message string, details map[string]any) *APIError
func ConflictError(code, message string, details map[string]any) *APIError
func UnauthorizedError(code, message string, details map[string]any) *APIError
func ForbiddenError(code, message string, details map[string]any) *APIError
func InternalError(code, message string, details map[string]any) *APIError
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

### `internal/platform/clients/httpclient`

Base HTTP client used by all typed service-to-service clients (ADR-0020, ADR-0022):

```go
// Construct a base client — tracerName scopes OTel spans, timeout applied per-request
func New(tracerName string, logger logging.Logger, timeout time.Duration) *Base

// Execute an outbound request — opens SpanKindClient span, injects W3C traceparent/tracestate,
// sets http.request.method + url.full attributes, returns span for the caller to close
func (b *Base) Do(ctx context.Context, spanName string, req *http.Request) (context.Context, trace.Span, *http.Response, error)

// Encode a value as JSON into an io.Reader for use as a request body
func (b *Base) EncodeJSON(body any) (io.Reader, error)

// Decode a 2xx response body into dest; records error on span if decode fails
func (b *Base) DecodeJSON(span trace.Span, resp *http.Response, dest any) error

// Handle a non-2xx response — reads body (≤512 bytes), sets error.upstream_body span attribute,
// records error on span, returns wrapped sentinel
func (b *Base) HandleErrorResponse(span trace.Span, resp *http.Response, sentinel error) error

// Record an application-level error on the span with a static sentinel description
func (b *Base) RecordError(span trace.Span, sentinel error) error
```

Typed clients in `internal/platform/clients/{service}/client.go` embed `*httpclient.Base` and call `b.Do` for all outbound requests. They define their own sentinel errors and wire types. They must **not** import `go.opentelemetry.io/otel/codes` or `go.opentelemetry.io/otel/trace` directly — span lifecycle is owned by `httpclient.Base`.

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
- **No subcommands inside a binary** — each `cmd/` entry point runs exactly one mode; multi-mode deployment is achieved via separate binaries under `cmd/`, not subcommands
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
- [ADR-0020](adr/0020-multi-service-backend-deployment-structure.md) — Multi-binary deployment model and typed platform clients
- [ADR-0021](adr/0021-envoy-ext-proc-sidecar-token-injection.md) — Envoy ext_proc sidecar for agent token injection
- [ADR-0022](adr/0022-opentelemetry-observability-discipline.md) — OpenTelemetry observability discipline
- [docs/configuration/secrets.md](configuration/secrets.md) — Secret management reference
- [docs/guides/local-development.md](guides/local-development.md) — Local setup walkthrough
