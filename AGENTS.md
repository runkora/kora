# Orbit Development Guide for AI Agents

## Project Overview

Orbit is the **control plane for AI agents** — not a framework, not a model router, not an LLM wrapper. It provides the coordination, identity, and control infrastructure required to safely scale autonomous agents inside real systems.

**Core Metaphor**: Orbit is architected as a **nervous system** for autonomous agents. This metaphor guides all architectural decisions:
- **Identity & Auth** = Sensory input (who is acting?)
- **Policy Enforcement** = Reflex control (what's allowed?)
- **Credential Broker** = Motor function (how action is executed)
- **Event Spine** = Neural signaling (awareness & feedback)
- **Autonomy Levels** = Brain stem vs cortex (decision hierarchy)

**Development Status**: Active early development. The identity layer, platform infrastructure, local development environment, and identity-proxy sidecar are fully implemented. See `docs/` for ADRs covering all major architectural decisions.

## Product Development Workflow

Orbit follows a structured process for new features and significant changes:

### 1. Product Requirements Document (PRD)
When working on a new feature or significant change:
- Create a PRD in `docs/prd/` following the template in `docs/prd/template.md`
- PRDs define the problem, user needs, and high-level solution approach
- Collaborate with the human to refine requirements and scope

### 2. Architecture Decision Record (ADR)
After PRD approval, create an ADR in `docs/adr/`:
- ADRs document the technical implementation approach
- Work with the human to explore options and trade-offs
- Use the ADR template: Context → Options → Decision → Consequences
- ADRs capture the "why" behind architectural choices

### 3. Implementation
Only after PRD and ADR approval:
- Implement the feature following the documented approach
- Reference the ADR and PRD in commit messages and PR descriptions
- Update documentation as needed

**The nervous system metaphor is central to Orbit's design.** All architectural decisions should align with this principle: coordination over computation, signaling over storage, governance over control.

## Development Setup

**Language**: Go 1.26 (control plane)

For the full local setup walkthrough, see **[docs/guides/local-development.md](docs/guides/local-development.md)**.

**First-run bootstrap (Tier 1 — backend only):**

```bash
make setup             # install goose, air, golangci-lint, gitleaks
make env               # copy .env.example → .env
make dev-up            # start Postgres + Redis + Dex — auto-generates secrets/signing.key
                       # and config/dex/.env on first run if they do not exist
make migrate-up        # apply database migrations
make dev               # start backend with hot-reload (air)
```

> `make gen-signing-key` and `make dex-hash` can be run manually to regenerate
> credentials at any time. `make dev-up` only auto-runs them when the files are absent.

**Authenticate in a second terminal:**

```bash
make auth-device       # Device Authorization Grant against local Dex
                       # browser → dev@orbit.local / password
                       # token saved to /tmp/orbit_credentials.json
```

**Run the full quality gate before committing:**

```bash
make check             # fmt + vet + lint (golangci-lint) + secret scan + tests
```

All PRs must pass `make check` with zero issues.

## Key Conventions

An agent **must** follow these conventions when modifying this codebase. Each convention is enforced by a linting rule — violations will fail `make check`.

### Data & Storage
- **No ORMs**: All database access uses explicit SQL via `*storage.DB` (wraps `pgx/v5` pgxpool.Pool)
- **No `database/sql`**: Use `pgx/v5` exclusively. `errors.Is(err, pgx.ErrNoRows)` for not-found checks.
- **No hand-written migrations**: Generate with `make migrate-create name=X` (Goose format)

### HTTP & Routing
- **No third-party routers**: Routes implement the `api.Controller` interface with `GroupedBy()`, `Middleware()`, and `Boot(*api.Mux)`. No gin, chi, echo, or gorilla/mux.
- **No raw JSON decoding**: Use `api.DecodeJSON(ctx, r, w, logger, &req)` in handlers — never `json.NewDecoder(r.Body).Decode(...)`.
- **No raw response writing**: Use `api.JSON(...)` and `api.ErrorResponse(...)` — never `w.WriteHeader(code)` or `http.Error(w, ...)` directly in handlers.
- **No raw HTTP clients**: Service-to-service HTTP calls must use a typed client in `internal/platform/clients/{service}/client.go` that embeds `httpclient.Base`. Never instantiate `http.Client{}` directly in feature or service code.

### Logging
- **No global loggers**: Inject `*logging.Logger` into structs; all log calls take `ctx context.Context` as first argument.
- **No stdlib log**: Never use `log`, `log/slog`, or `fmt.Print*` for logging. Use the injected `*logging.Logger`.

### Configuration
- **No secrets in YAML**: Secrets load via `os.Getenv` only; YAML configs use `yaml:"-"` for secret fields.
- **Config uses `config.Load[T any]`**: Environment-specific YAML loaded by `APP_ENV` value. No raw viper, no raw yaml library imports outside `platform/config/`.

### Errors & Observability
- **Wrap errors with `%w`**: Always use `fmt.Errorf("context: %w", err)` — never `%s` for error wrapping.
- **Static span status descriptions**: `span.SetStatus(codes.Error, sentinel.Error())` must use a package-level sentinel error string — never `err.Error()` or `ctx.Err().Error()` inline. Use `span.RecordError(err)` for dynamic context.
- **No raw OTel in typed clients**: Platform clients in `internal/platform/clients/` must not import `go.opentelemetry.io/otel/codes` or `go.opentelemetry.io/otel/trace` directly — the `httpclient.Base` handles all span lifecycle.

### Code Structure
- **Cross-cutting code lives in `internal/platform/`**: Feature packages must not import each other.
- **ADR before implementation**: Significant architectural decisions require an ADR in `docs/adr/` before code is written.

## Automated Enforcement

All conventions above are mechanically enforced. Do not add inline `//nolint` directives — exceptions are managed centrally via path-scoped exclusions in `.golangci.yml`.

**Tier 1 — AST rules** (`rules/orbit_rules.go`, enforced via `gocritic/ruleguard`):

| Rule | What it bans |
|------|-------------|
| `spanSetStatusLocalErr` | `span.SetStatus(_, err.Error())` |
| `spanSetStatusCtxErr` | `span.SetStatus(_, ctx.Err().Error())` |
| `spanAttrStringLocalErr` | `attribute.String(_, err.Error())` |
| `spanAttrStringCtxErr` | `attribute.String(_, ctx.Err().Error())` |
| `noRawJsonDecodeRequest` | `json.NewDecoder(r.Body).Decode(...)` in handlers |
| `noFmtErrorfStringErr` | `fmt.Errorf("...: %s", err)` |
| `noRawHttpClientLiteral` | `http.Client{...}` literal construction |
| `noHttpErrorInHandler` | `http.Error(w, ...)` in handlers |
| `noDirectWriteHeader` | `w.WriteHeader(code)` in handlers |

**Tier 2 — Import bans** (`depguard` in `.golangci.yml`):

| Rule | What it bans |
|------|-------------|
| `no-stdlib-log` | `log`, `log/slog` |
| `no-otel-span-mgmt-in-service-clients` | `otel/codes`, `otel/trace` inside typed clients |
| `no-database-sql` | `database/sql` |
| `no-raw-viper` | `github.com/spf13/viper` |
| `no-raw-yaml` | `gopkg.in/yaml.v2`, `gopkg.in/yaml.v3` |
| `no-third-party-routers` | `gin`, `chi`, `echo`, `gorilla/mux` |

See `rules/README.md` for full rationale and exception policy.

## Code Organization

Orbit follows a **feature-based architecture** with the repository pattern for data access and a multi-binary deployment model. See the following documents for complete details:

- **[docs/code-organization.md](docs/code-organization.md)** - Complete directory structure and package organization
- **[docs/adr/0003-repository-pattern-and-code-organization.md](docs/adr/0003-repository-pattern-and-code-organization.md)** - Repository pattern and feature structure
- **[docs/adr/0020-multi-service-backend-deployment-structure.md](docs/adr/0020-multi-service-backend-deployment-structure.md)** - Multi-binary deployment model

### Key Principles

**Multi-binary deployment**: The backend compiles to multiple binaries under `backend/cmd/`. Each binary has its own `main.go` and `config/` directory. All binaries share the `backend/internal/platform/` layer.

| Binary | Path | Purpose |
|--------|------|---------|
| `orbit` | `cmd/orbit/` | Main API server — identity, sessions, future features |
| `identity` | `cmd/identity/` | Standalone identity service |
| `identity-proxy` | `cmd/identity-proxy/` | Envoy ext_proc sidecar — injects platform tokens into agent outbound calls |
| `example` | `cmd/example/` | Reference implementation (not production) |

**Feature Ownership**: Each feature package (`internal/identity/`, `internal/agents/`, etc.) owns its complete stack:
- Repository interface (storage contract)
- PostgreSQL implementation
- Business logic (service layer)
- HTTP handlers
- Feature-specific types

**Platform Clients**: Service-to-service HTTP calls go through typed clients in `internal/platform/clients/{service}/client.go`. Each client embeds `httpclient.Base` which automatically handles W3C trace propagation, OTel span lifecycle, and structured logging. Feature packages never construct raw `http.Client` instances.

**Storage Infrastructure**: The `internal/platform/storage/` package provides database infrastructure (Postgres pool, Redis client, migrations) but never imports feature packages. Dependency flow is always: Features → Platform.

**"DDD-lite" Approach**: We use valuable Domain-Driven Design concepts (bounded contexts, repository pattern, ubiquitous language) while staying idiomatic to Go. We skip strict DDD ceremony (deep layering, aggregate roots, value objects) in favor of flat packages and explicit code.

**Standard Feature Structure**:
```
internal/{feature}/
├── repository.go    # Storage interface
├── postgres.go      # PostgreSQL implementation
├── service.go       # Business logic
├── handlers.go      # HTTP transport
└── models.go        # Feature-specific types
```

## Design Principles

When contributing code or architecture:

1. **Security First**: Every component designed with security as primary concern
2. **Identity-Driven**: All actions tied to verified identities
3. **Policy-Governed**: Policies enforced *before* execution, not after
4. **Observable**: Complete visibility into agent behavior — distributed tracing, structured logging, log-trace correlation
5. **Extensible**: Pluggable integrations for models, tools, identity providers
6. **Production-Grade**: Built for enterprise reliability and scale
7. **Open Standards**: OPA for policy, OIDC/SPIFFE for identity, MCP for tools, OpenTelemetry for observability

## Security Guidelines

Orbit is infrastructure for autonomous agents. Security is not optional.

- **No secrets in code**: Use environment variables or secret management
- **Validate all inputs**: Especially from external agents or tools
- **Principle of least privilege**: Grant minimum necessary permissions
- **Audit everything**: All actions must emit events to the event spine
- **Identity verification**: Never trust, always verify
- **Policy-first**: Don't implement authorization logic in business code

See `SECURITY.md` for vulnerability reporting.

## Contributing Workflow

### For New Features or Significant Changes:

1. **Create PRD**: Start with a Product Requirements Document in `docs/prd/` using the template
   - Define the problem, goals, and user stories
   - Collaborate with the human to refine requirements
   - Get PRD approval before proceeding

2. **Create ADR**: After PRD approval, create an Architecture Decision Record in `docs/adr/`
   - Document the technical approach and trade-offs
   - Work with the human to explore implementation options
   - Use the ADR template to capture decisions and rationale

3. **Implement**: Only after PRD and ADR approval
   - Write tests for all new code
   - Follow Go standards: `gofmt`, idiomatic Go
   - Run `make check` and fix all issues before opening a PR
   - Reference PRD and ADR in commit messages and PR descriptions

### For Bug Fixes or Minor Changes:

1. **Document**: Create an issue describing the problem
2. **Implement**: Fix with tests and documentation updates
3. **Review**: Run `make check`, then open a PR with a clear description

## Testing Strategy

- **Unit tests**: Test individual components in isolation using table-driven tests
- **Integration tests**: Test component interactions (repository implementations against a real Postgres instance)
- **E2E tests**: Test full agent workflows through Orbit
- **Security tests**: Validate policy enforcement and credential isolation
- **No dedicated lint-rule unit tests**: Custom `golangci-lint` rules are validated empirically via `make check` — no `analysistest` harness to keep `backend/go.mod` lean

## Implementation Principles

When implementing Orbit components:

- **Policy Evaluation**: Always evaluate policies before executing actions, not after
- **Event Emission**: Emit structured events for all significant actions and decisions
- **Credential Brokering**: Never expose raw credentials to agents — use scoped, temporary access
- **Identity Context**: Carry identity context through the entire execution chain via `context.Context`
- **Repository Pattern**: Define repository interfaces in feature packages, implement with PostgreSQL in the same package
- **Feature Independence**: Features must not import each other — share only through `internal/platform/`
- **Idiomatic Go**: Favor simplicity, flat packages, and explicit code over deep abstraction hierarchies
- **Sentinel Errors**: Define package-level `var ErrXxx = errors.New("...")` rather than inline string errors; wrap with `%w`

See [ADR-0003](docs/adr/0003-repository-pattern-and-code-organization.md) for detailed architectural patterns and the rationale behind our code organization approach.

## Current Focus Areas

### Implemented

- **Identity layer**: Provider-agnostic OIDC federation (RFC 8628 Device Authorization Grant), SPIFFE SVID exchange endpoint, session management, JWT issuance and revocation (JTI blocklist via Redis)
- **Identity-proxy sidecar**: Envoy ext_proc binary that intercepts outbound agent HTTP calls and injects a platform Bearer token; SPIFFE SVID-triggered cache invalidation; waits for Istio/Envoy xDS readiness before accepting connections
- **Platform infrastructure**: HTTP controller routing, structured logging, OpenTelemetry (tracing + log correlation), PostgreSQL (`pgx/v5`), Redis (`go-redis/v9`), YAML config system
- **Platform HTTP clients**: `httpclient.Base` provides automatic W3C trace propagation, OTel span lifecycle, and structured transport logging for all service-to-service calls
- **Automated lint enforcement**: 9 custom AST rules (`rules/orbit_rules.go`) + 6 import-ban rules (`.golangci.yml`) covering ADRs 0003–0022
- **Local development environment**: Dex OIDC provider via Docker Compose, signing key generation, `make auth-device` CLI flow

### Next Priorities

1. **Policy engine**: OPA integration, RBAC/PBAC enforcement (see ADR-0011)
2. **Agent execution**: Agent identity registration, run orchestration, execution state management
3. **Credential broker**: Scoped just-in-time credential provisioning, Vault integration
4. **Event spine**: Structured event emission and routing

The next ADRs in the sequence are **ADR-0023** onward. Check open issues and project boards for specific tasks.

## Getting Help

- **Architecture questions**: Open a discussion or issue referencing the relevant ADR
- **Security concerns**: See `SECURITY.md` for private reporting
- **General questions**: Use GitHub Discussions

## License

Orbit is licensed under Apache License 2.0. All contributions must be compatible with this license.

---

**Remember**: Orbit is not just infrastructure — it's the nervous system that coordinates autonomous agents. Every component should reflect this: coordination over computation, signaling over storage, governance over control.
