# Code Organization

The canonical code organization and package layout for the kora backend.

See [multi-service-backend-deployment-structure](adr/2026-03-19-multi-service-backend-deployment-structure.md) for the multi-binary deployment model and [repository-pattern-and-code-organization](adr/2026-02-17-repository-pattern-and-code-organization.md) for the feature/repository pattern.

---

## The big picture

The backend compiles to a set of small binaries under `backend/cmd/`, all sharing one `backend/internal/platform/` layer. Each is listed in the [binary table below](#binaries-backendcmd). There is **no single "orbit" binary**: the main control-plane API is the `identity` service. Feature logic lives in `backend/internal/{feature}/` packages; cross-cutting infrastructure lives in `backend/internal/platform/`. Dependency flow is strictly one-directional: **features → platform**, and features never import each other.

### Binaries (`backend/cmd/`)

| Binary | Role | Local port | Owns tables |
|--------|------|------------|-------------|
| `identity` | **Main control-plane API**: identity, sessions, PATs, user profiles, tenant registry, JWKS | `8080` (`/api/v1`) | ✅ |
| `inference-gateway` | OpenAI/Anthropic-compatible LLM gateway: chat/completions, embeddings, responses, model catalog, usage logs, analytics | `8081` (`/v1`) | ✅ |
| `policy-service` | Authorization data plane (PDP/PAP): grants, roles, vocabulary, route-scope store; compiles & serves OPA bundles | `8083` (`/api/v1`) | ✅ |
| `policy-gateway` | In-app authorization API (PEP): `/policy/check`, `/query`, `/scopes`, `/explain`, `/simulate` | `8084` (`/v1`) | none |
| `egress-gateway` | JIT credential broker: intercepts agent outbound calls and injects scoped external credentials (GitHub App, Kubernetes bearer); backed by `internal/credentials` | `8085` | none |
| `orbit-mcp` | Platform-management MCP server: exposes kora's own APIs as governed MCP tools | `8095` | none |
| `tool-gateway` | Governed MCP/tool gateway: server catalog, per-tool scope enforcement, OAuth + consent, introspection; backed by `internal/toolgateway` (a `tool-gateway-publisher` deployment runs the same image in `publisher` mode) | `8096` (`/v1`) | ✅ |
| `orbit-agents` | Agent control plane: agent-type registry, run/session lifecycle, event streaming, k8s orchestration, BYOC discovery | `8080` (in-cluster) | ✅ |
| `orbit-triggers` | Triggers: inbound webhook ingestion, trigger management API, cron scheduler, manual fire | `8097` | ✅ |
| `skills-service` | Read-only Agent Marketplace: browses and serves skills from GitHub-hosted repositories; cache-aside via Redis | `8089` | none |
| `session-manager` | Per-tenant StatefulSet that runs the Claude Code subprocess per session/run; workspace + idle lifecycle | `8090` | none |
| `operator` | Kubernetes operator: reconciles Tenant/Platform CRDs, renders Helm/Istio, mutating pod webhook (injects the identity-proxy sidecar) | health `8081`, webhook `9443` | none |
| `identity-proxy` | Envoy `ext_proc` gRPC sidecar: injects platform Bearer tokens into agent outbound calls (SPIFFE→JWT exchange) | unix socket; health `9090` | none |
| `sandbox-exec` | gRPC exec server inside a sandbox pod: streams stdout/stderr/exit for commands | gRPC `50051` | none |
| `orbit-runtime-mcp` | MCP server (stdio) spawned by the agent CLI: exposes the `list_environments`, `provision_sandbox` and `replace_environment` tools | stdio | none |
| `orbit-shell` | `$SHELL` shim in the agent container: execs local bash or streams `-c` commands to the sandbox pod | none | none |
| `orbit-session-runner` | Wrapper that applies Landlock, then `exec()`s the agent CLI | none | none |
| `sandbox-mount` | Privileged node agent (DaemonSet): exports each sandbox's local working folder with the kernel NFS server and mounts it into the session-manager pod at `/workspaces/<run>/sandbox` | NFS `20490` (host network, `sandboxMount.port`) | none |
| `migrate` | Schema migrations: embeds every service's `migrations/` and runs them as the chart's pre-install/pre-upgrade hook Job | none | none |
| `example` | Reference implementation demonstrating the patterns (not production) | `8081` | ✅ |

> The sandbox, shell, MCP and session-runner binaries form the agent execution stack. `session-manager` runs Claude Code through `orbit-session-runner`, and Claude calls `orbit-runtime-mcp` to `list_environments`, `provision_sandbox` and `replace_environment`. The sandbox pod runs `sandbox-exec`, and Claude's `Bash` goes through `orbit-shell` to that pod. With `sandboxMount.enabled`, `sandbox-mount` shares the sandbox's local folder with the session-manager pod ([sandbox-workspace-mount](adr/2026-10-08-sandbox-workspace-mount.md)). See [sandbox-provisioning-and-shell-proxy](adr/2026-05-11-sandbox-provisioning-and-shell-proxy.md).

### Feature packages (`backend/internal/`)

| Package | Owns |
|---------|------|
| `identity` | Sessions, refresh tokens, PATs, user profiles, JTI blocklist, tenant registry, principal extraction |
| `inferencegw` | Inference router, providers, catalog, usage, analytics, virtual models; OpenAI/Anthropic/embeddings/responses handlers |
| `agents` | Agent control plane: types, runs, sessions, pubsub, k8s, authz; sub-packages `sandbox/`, `sessionmanager/` |
| `policyservice` | Authz data plane; sub-packages `api/`, `bundle/`, `grants/`, `roles/`, `routes/`, `vocabulary/` |
| `policygw` | Policy-gateway HTTP transport (delegates evaluation to policy-service) |
| `operator` | K8s reconciliation: tenant/platform controllers, Helm render, Istio templates, pod webhook |
| `identityproxy` | `ext_proc` server, token cache, refresh loop |
| `example` | Reference implementation, not production. Full feature layout (`models`, `repository`, `postgres`, `service`, `handlers`) with tests, demonstrating the pattern |
| `credentials` | JIT credential broker behind `egress-gateway`; `adapters/` (`githubapp`, `k8sbearer`, and the non-registrable `passthrough`) + `resolvers/` ([jit-credential-brokering-egress-gateway](adr/2026-06-06-jit-credential-brokering-egress-gateway.md)) |
| `toolgateway` | Governed MCP/tool gateway behind `tool-gateway`: server catalog, MCP controller, OAuth controller, introspector ([MCP / Tool Gateway ADRs](adr/README.md#mcp--tool-gateway)) |
| `errors` | Nothing yet: an empty placeholder directory. |

> `internal/errors` is an intentional empty placeholder (a `git`-tracked directory with no Go files).

---

## Directory structure

```
orbit/
├── backend/
│   ├── cmd/                          # One subdirectory per deployable binary
│   │   ├── identity/                 # Main control-plane API (port 8080)
│   │   │   ├── main.go
│   │   │   ├── config/               # base.yaml + config.go (+ optional env overlays)
│   │   │   └── migrations/           # Goose SQL migrations (per-binary)
│   │   ├── inference-gateway/        # ... same shape; owns tables (migrations/)
│   │   ├── policy-service/           # ... owns tables (migrations/)
│   │   ├── policy-gateway/           # config only (no DB)
│   │   ├── egress-gateway/           # JIT credential broker (no DB)
│   │   ├── orbit-mcp/                # platform-management MCP server (no DB)
│   │   ├── tool-gateway/             # governed MCP/tool gateway; owns tables (migrations/)
│   │   ├── orbit-agents/             # ... owns tables (migrations/)
│   │   ├── orbit-triggers/           # ... owns tables (migrations/)
│   │   ├── session-manager/          # config only
│   │   ├── operator/                 # K8s operator (controller-runtime)
│   │   ├── identity-proxy/           # ext_proc sidecar
│   │   ├── sandbox-exec/             # gRPC exec server (no config/)
│   │   ├── orbit-runtime-mcp/        # stdio MCP server (no config/)
│   │   ├── orbit-shell/              # $SHELL shim (no config/)
│   │   ├── orbit-session-runner/     # isolation wrapper (no config/)
│   │   ├── sandbox-mount/            # node agent for sandbox working folders
│   │   └── example/                  # Reference binary
│   │
│   ├── internal/
│   │   ├── platform/                 # Cross-cutting infrastructure — never imports features
│   │   │   ├── api/                  # Mux, Controller interface, JSON/error helpers
│   │   │   ├── clients/              # Typed service-to-service HTTP clients (embed httpclient.Base)
│   │   │   ├── config/               # Generic + per-service YAML loaders
│   │   │   ├── logging/              # Logger interface + implementation (injected, never global)
│   │   │   ├── middleware/           # Request logging, tracing, scope enforcement
│   │   │   ├── observability/        # OTel TracerProvider + MeterProvider setup
│   │   │   ├── policy/               # OPA evaluation helpers / policy client glue
│   │   │   ├── storage/              # Postgres (storage.DB) + Redis (storage.RedisClient)
│   │   │   └── token/                # JWT signing / verification primitives
│   │   │
│   │   ├── identity/                 # ✅ feature: identity & auth
│   │   ├── inferencegw/              # ✅ feature: inference gateway (+ providers/ subtree)
│   │   ├── agents/                   # ✅ feature: agent control plane (+ sandbox/, sessionmanager/)
│   │   ├── policyservice/            # ✅ feature: authz data plane (subpackage-per-concern)
│   │   ├── policygw/                 # ✅ feature: policy-gateway transport
│   │   ├── operator/                 # ✅ feature: k8s reconciliation
│   │   ├── identityproxy/            # ✅ feature: ext_proc sidecar
│   │   ├── credentials/              # ✅ feature: JIT credential broker (egress-gateway)
│   │   ├── toolgateway/              # ✅ feature: governed MCP/tool gateway (tool-gateway)
│   │   ├── example/                  # reference feature
│   │   └── errors/                   # empty placeholder
│   │
│   ├── go.mod
│   └── go.sum
│
├── frontend/                         # Next.js app + Hades design system (see frontend/README.md)
├── charts/                           # Helm charts (platform + tenant)
├── config/dex/                       # Local Dex OIDC provider config
├── scripts/                          # auth-device.sh, setup helpers
├── secrets/                          # gitignored — local signing key etc.
├── docs/                             # ADRs, PRDs, guides, this file (see docs/README.md)
├── docker-compose.yml                # Backing services: Postgres + Redis + Dex + glauth + port-proxy
├── Makefile
├── .env.example
├── AGENTS.md                         # AI-agent + contributor conventions
├── CONTRIBUTING.md
└── README.md
```

> **Migrations are per-binary**, under `backend/cmd/{service}/migrations/`, not in a single shared directory. Only binaries that own tables carry a `migrations/` directory (the "Owns tables" column in the [binary table](#binaries-backendcmd) marks which); the rest have none.

---

## Platform vs features

### Platform layer (`internal/platform/`)

Infrastructure shared by all features. Platform packages **never import feature packages**; the dependency flow is strictly one-directional.

| Package | Purpose |
|---------|---------|
| `api` | HTTP `Mux`, `Controller` interface, `DecodeJSON`/`JSON`/`ErrorResponse` helpers, typed `APIError` |
| `clients` | Typed service-to-service HTTP clients; `httpclient.Base` owns OTel spans + W3C trace propagation + structured logging |
| `config` | Generic YAML loaders: `Load[T]` / `MustLoad[T]` and the service variants `LoadService[T]` / `MustLoadService[T]`, keyed by `APP_ENV`, with the chart-rendered file merged last |
| `logging` | `Logger` interface + implementation; injected via constructors, never global |
| `middleware` | Request logging, OTel trace injection, platform scope enforcement |
| `observability` | OTel `TracerProvider` + `MeterProvider` + OTLP exporter setup |
| `policy` | OPA evaluation helpers / policy-gateway client glue used by owned services |
| `storage` | PostgreSQL (`*pgxpool.Pool` via `storage.DB`) + Redis (`storage.RedisClient`): config, pool builders, transactions |
| `token` | JWT signing / verification primitives |

### Feature layer (`internal/`)

Each feature owns a vertical slice (storage contract → implementation → service → HTTP transport → types). See the feature table above for the full list. Smaller features follow the flat file layout below; larger ones (`inferencegw`, `agents`, `policyservice`) split into multiple files and sub-packages. That is expected and idiomatic, not a deviation.

---

## Standard feature structure

A feature package follows this layout (the canonical small-feature shape; larger features expand it):

```
internal/{feature}/
├── models.go       # Domain types, request/response structs, sentinel errors
├── repository.go   # Storage interface(s) — the contract, not the implementation
├── postgres.go     # PostgreSQL implementation (explicit SQL via *storage.DB)
├── redis.go        # Redis implementation (if the feature uses Redis directly)
├── service.go      # Business logic; orchestrates repositories + external clients
└── handlers.go     # HTTP transport: api.Controller, decode → service → respond
```

Per-binary configuration lives under `backend/cmd/{binary}/config/` (a `base.yaml` plus a `config.go` that defines the typed struct; environment overlays are added as `{env}.yaml` when needed). Migrations live under `backend/cmd/{binary}/migrations/`.

---

## Key patterns

### 1. HTTP routing via `api.Controller`

There is no central router file. Each feature registers its own routes by implementing `api.Controller`:

```go
// internal/platform/api/controller.go
type Controller interface {
    GroupedBy() string                      // URL prefix, e.g. "/identity"
    Middleware() middleware.MiddlewareHandler
    Boot(mux *api.Mux)                       // Register routes on the mux
}
```

A binary's `main.go` builds each controller and registers it on the `Mux`. Adding a feature never requires touching existing route files:

```go
// backend/cmd/identity/main.go (abridged)
controllers := []api.Controller{
    identity.NewController(svc, policyEval, identityRoutes(), basePath, logger),
    // ...
}
```

### 2. Dependency-injected logger

Global loggers are forbidden. Every struct that logs accepts a `*logging.Logger` (or the `logging.Logger` interface) in its constructor, and every log call takes `ctx` first for OTel trace correlation:

```go
logger := logging.NewLogger()                 // production JSON logger
// logging.NewDevelopmentLogger()             // human-friendly local logger
s.logger.Info(ctx, "session created", logging.Field{Key: "session_id", Value: id})
```

The `Logger` interface lives in `internal/platform/logging/logger.go`. Features depend on the interface, not the implementation.

### 3. Database access via `storage.DB`

All database access uses explicit SQL, no ORM. `storage.DB` wraps a `*pgxpool.Pool`:

```go
type DB struct { Pool *pgxpool.Pool }

// internal/identity/postgres.go
func (r *postgresSessionRepository) Create(ctx context.Context, s *Session) error {
    _, err := r.db.Pool.Exec(ctx, `INSERT INTO identity_sessions (...) VALUES (...)`, ...)
    return err
}
```

Use `errors.Is(err, pgx.ErrNoRows)` for not-found checks. `storage.WithTransaction(ctx, db, fn)` wraps a transaction.

### 4. Redis access via `storage.RedisClient`

Redis is a first-class storage primitive. `storage.RedisClient` wraps a `*redis.Client`; features that need it receive `*storage.RedisClient` via their constructor (the identity service uses it for the JTI blocklist).

### 5. Service-to-service calls via platform clients

All outbound HTTP to other kora services goes through a typed client in `internal/platform/clients/{service}/client.go` that embeds `httpclient.Base`, which automatically opens an OTel `SpanKindClient` span, injects W3C `traceparent`/`tracestate`, sets HTTP semantic-convention attributes, captures upstream error bodies, and emits a structured log line per request.

**Never construct `http.Client{}` directly** in feature or service code (`noRawHttpClientLiteral` enforces this). Typed clients must **not** import `otel/codes` or `otel/trace` directly: `httpclient.Base` owns the span lifecycle.

### 6. Configuration via `config.LoadService[T]` + `base.yaml`

Each binary has a typed config struct loaded at startup from `backend/cmd/{binary}/config/base.yaml`, an optional `{APP_ENV}.yaml` overlay (`APP_ENV` defaults to `production`) and the file the chart renders at `/etc/orbit/config/cmd/{binary}.yaml`:

```go
// Per-service config loader (most binaries)
cfg := config.MustLoadService[AppConfig]("cmd/identity", config.Environment())
// Generic loader (module-relative) is also available: config.Load[T](module, env)
```

**Secrets never appear in YAML.** Secret fields are tagged `yaml:"-"` and loaded via `os.Getenv`. See [docs/configuration/config.md](configuration/config.md) and [docs/configuration/secrets.md](configuration/secrets.md).

### 7. Migrations via Goose (per-binary)

SQL migrations live under `backend/cmd/{service}/migrations/` and use Goose annotations. Each directory is also a Go package exposing an `embed.FS`; `cmd/migrate` embeds all of them and runs them as one set, as the chart's pre-install and pre-upgrade hook Job ([database migrations ADR](adr/2026-09-10-database-migrations.md)). **Never hand-write migration files**, always generate them, specifying the owning service:

```bash
make migrate-create service=identity name=add_sessions_index
```

`make migrate-lint` (part of `make check`) enforces the content rule: DDL only, own tables only, an `UPDATE` only beside an `ALTER TABLE` on the same table, no row changes in `Down`. Inspect with `make migrate-status`; re-run the hook locally with `make redeploy SERVICE=migrate`.

---

## Application entry points

Each binary under `backend/cmd/` has its own `main.go` and is otherwise independent; all share `internal/platform/`.

- **`cmd/identity`**: the main API (port 8080). Loads its config via `MustLoadService`, initializes observability, builds `*storage.DB` + `*storage.RedisClient` (`initStorage`), wires feature services and their `api.Controller`s (`initControllers`), and serves HTTP. Runs in the cluster; roll a change with `make redeploy SERVICE=identity`.
- **`cmd/inference-gateway`**: the LLM gateway (port 8081); provider backends under `internal/inferencegw/providers/`.
- **`cmd/policy-service`** / **`cmd/policy-gateway`**: the PDP/PAP (8083) and PEP (8084) split; see [policy-gateway-and-authorisation-model](adr/2026-04-14-policy-gateway-and-authorisation-model.md).
- **`cmd/orbit-agents`**: the agent control plane; see [orbit-agents-service-architecture](adr/2026-05-11-orbit-agents-service-architecture.md).
- **`cmd/orbit-triggers`**: the platform's front door for starting work; see [extended-trigger-types](adr/2026-06-20-extended-trigger-types.md).
- **`cmd/identity-proxy`**: the `ext_proc` sidecar; see [envoy-ext-proc-sidecar-token-injection](adr/2026-03-21-envoy-ext-proc-sidecar-token-injection.md).
- **`cmd/operator`**: the Kubernetes operator; see [orbit-kubernetes-operator](adr/2026-03-31-orbit-kubernetes-operator.md).

### Adding a new binary

Create `cmd/{name}/` with its own `main.go` and (if it needs config) a `config/base.yaml` + `config.go`. Import only from `internal/platform/` and the feature packages the binary owns. Cross-cutting concerns belong in `internal/platform/`, never shared across binary entry points.

---

## Adding a new feature

1. **Create the package** at `internal/{feature}/`.
2. **Define models** in `models.go`: domain types, request/response structs, sentinel errors.
3. **Define repository interfaces** in `repository.go`: small, focused storage contracts.
4. **Implement PostgreSQL** in `postgres.go`: explicit SQL, inject `*storage.DB`.
5. **Implement Redis** in `redis.go`: if needed, inject `*storage.RedisClient`.
6. **Write business logic** in `service.go`: call repositories, platform clients, emit events.
7. **Write HTTP handlers** in `handlers.go`: implement `api.Controller`, register routes in `Boot(*api.Mux)`.
8. **Add config** at `backend/cmd/{binary}/config/base.yaml` for the binary that mounts the feature.
9. **Generate migrations** with `make migrate-create service={binary} name=create_{feature}_tables`.
10. **Register in `main.go`**: construct the controller and add it to the binary's controller list.

`internal/example/` demonstrates the repository + controller pattern with working code. Read it before implementing the first route in a new feature.

---

## What this codebase is NOT

- **No ORM**: `*pgxpool.Pool` + explicit SQL only; no GORM, Ent, sqlx.
- **No `database/sql`**: `pgx/v5` exclusively.
- **No third-party router**: no Gin, Chi, Echo, Gorilla; routing is `api.Mux` + the `Controller` interface.
- **No global logger**: no `log`/`log/slog`/`fmt.Print*`; inject `*logging.Logger`.
- **No `cmd/orbit` binary**: the main API is `cmd/identity`.
- **No shared `internal/models/` package**: each feature owns its types.
- **No central `router.go`**: features self-register via `Controller.Boot()`.
- **No `providers/` in every feature**: external integrations live in `service.go` (the inference gateway's `providers/` subtree is the deliberate exception for multi-provider routing).
- **No hand-written migrations**: always `make migrate-create service=X name=Y`.
- **No secrets in YAML**: `yaml:"-"` + `os.Getenv`.

---

## References

- [repository-pattern-and-code-organization](adr/2026-02-17-repository-pattern-and-code-organization.md): Repository pattern and feature structure
- [dependency-injected-logging](adr/2026-02-17-dependency-injected-logging.md): Dependency-injected logging
- [controller-based-http-routing](adr/2026-02-17-controller-based-http-routing.md): Controller-based HTTP routing
- [yaml-file-based-configuration](adr/2026-02-18-yaml-file-based-configuration.md): YAML configuration system
- [database-infrastructure-layer](adr/2026-03-03-database-infrastructure-layer.md): Database infrastructure layer
- [multi-service-backend-deployment-structure](adr/2026-03-19-multi-service-backend-deployment-structure.md): Multi-binary deployment model and typed platform clients
- [opentelemetry-observability-discipline](adr/2026-03-21-opentelemetry-observability-discipline.md): OpenTelemetry observability discipline
- [docs/configuration/config.md](configuration/config.md) · [docs/configuration/secrets.md](configuration/secrets.md): Configuration & secret-management reference
- [docs/guides/local-development.md](guides/local-development.md): Local setup walkthrough
