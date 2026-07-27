# Orbit Development Guide for AI Agents

## Project Overview

Orbit is **the control plane for AI agents**, the agentic engineering platform that closes the gap between an agent running on a developer's laptop and a secure, governed agent running in production. It is *not* an agent framework, a model router, or an LLM wrapper.

The platform answers three questions for every agent that goes into production, and the architecture is organised around them:

- **Who is this agent?** Every agent gets a machine-verifiable identity at deploy time. Humans and agents share one identity model; a person can delegate to an agent with defined scope and full accountability, and the agent can never access more than the delegator.
- **What can it access?** Every action is checked against policy *before* it executes. Admins define what each agent can do down to the individual tool; everything outside that list is rejected and logged. Enforcement happens at the edge inside each pod. The agent is never trusted to behave.
- **How does it connect?** Agents never handle credentials. The platform intercepts outbound calls and injects credentials just-in-time, deferring to the upstream system as the authority on what a user can access. A Claude Code or Codex instance drops in without modifying its auth handling.

Underneath, Orbit is established platform engineering applied to a new domain: SPIFFE workload attestation, OPA policy-as-code, Istio/Envoy at the edge, all Kubernetes-native and standards-based. Every agent action traces back to the human who initiated it and the policy that authorised it.

**These three questions guide architectural decisions.** When you propose a change, be explicit about which one it touches and how it preserves the others.

Build status lives in [docs/architecture.md](docs/architecture.md), the binary and port map in [docs/code-organization.md](docs/code-organization.md), and the decision history in the [ADR index](docs/adr/README.md). Check them before assuming a capability exists.

## Development Workflow

Orbit follows a **PRD → ADR → implementation** process for new features and significant changes: write a PRD (`docs/prd/`, from `docs/prd/template.md`) defining the problem and solution; then, once it's agreed, an ADR (`docs/adr/`, Context → Options → Decision → Consequences) capturing the technical approach and the "why"; then implement against the agreed docs, referencing them in commits and the PR. Bug fixes and minor changes skip the PRD/ADR. **An ADR before significant implementation is a hard rule**: architectural decisions are captured in an ADR the PR references, never inline in a PR.

Setup, conventions, and the quality gate: [CONTRIBUTING.md](CONTRIBUTING.md#2-development-workflow).

**The three questions are the spine of every architectural decision.** Be explicit about which one a change touches (*Who is this agent? What can it access? How does it connect?*) and how it preserves the others.

## Development Setup

**Language**: Go 1.26 (control plane)

Local development runs in Kubernetes. There is one environment. For the full walkthrough see **[docs/guides/local-development.md](docs/guides/local-development.md)**.

**First run:**

```bash
make setup   # once per machine: generate .env, install tools, git hooks, local DNS, mkcert TLS
make up      # backing services, k3d cluster, migrations, operator (SPIRE + Istio + Orbit), tenants, workloads. Idempotent.
make auth    # Device Authorization Grant → dev@orbit.local.clearroute.io / password. Token → /tmp/orbit_credentials.json
```

`make up` is idempotent: re-run it whenever the cluster drifts. `make down` tears it down. The inner loop is `make redeploy` (`SERVICE=<name>` narrows it to one service).

**Calling authenticated Orbit APIs (curl):**

Read the token inline from `/tmp/orbit_credentials.json`; `$ORBIT_TOKEN` is not available in subprocess shells. Use `curl -sk` against the cluster ingress on `:8443`.

```bash
# Standard pattern for any authenticated API call
TOKEN=$(jq -r '.access_token' /tmp/orbit_credentials.json) && curl -sk \
  -H "Authorization: Bearer $TOKEN" \
  https://api.orbit.local.clearroute.io:8443/v1/models

# POST example (inference gateway)
TOKEN=$(jq -r '.access_token' /tmp/orbit_credentials.json) && curl -sk -X POST \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"model":"bedrock/claude-sonnet-4-5-20250929-v1","max_tokens":50,"messages":[{"role":"user","content":"Hello"}]}' \
  https://api.orbit.local.clearroute.io:8443/v1/chat/completions | jq .
```

**Endpoints:** frontend `https://orbit.local.clearroute.io:8443`, inference gateway `https://api.orbit.local.clearroute.io:8443/v1`.

**In-cluster ports** (each service binds these inside the mesh):
| Service | Port |
|---------|------|
| Main API (`identity`) | `8080` |
| Inference Gateway | `8081` |
| Policy Service | `8083` |
| Policy Gateway | `8084` |
| Triggers | `8097` |
| Session Manager | `8090` |

(`orbit-agents` binds `8080` in-cluster; the `operator`, `identity-proxy`, and `sandbox-exec` are not host-facing HTTP. See [code-organization.md](docs/code-organization.md) for the full binary/port map.)

**If a call returns `{"code":"unauthorized","message":"invalid or expired platform token"}` (the token expired):**

```bash
make auth-refresh   # rotate the token pair in /tmp/orbit_credentials.json
```

If the refresh token itself is expired, run `make auth` to start a fresh session. Never refresh with raw `curl`: see `scripts/auth-refresh.sh` for why.

**Run the full quality gate before committing:**

```bash
make check             # tidy + fmt + vet + lint (golangci-lint) + secret scan + docs-check + tests + frontend checks
```

`docs-check` (`scripts/docs/check-docs.sh`) guards the docs against drift: broken links, missing ADR/PRD index entries, dead `make` targets, a stale binary table, and hardcoded counts. All PRs must pass `make check` with zero issues.

## Key Conventions

An agent **must** follow these conventions when modifying this codebase. Each convention is enforced by a linting rule; violations will fail `make check`.

### Data & Storage
- **No ORMs**: All database access uses explicit SQL via `*storage.DB` (wraps `pgx/v5` pgxpool.Pool)
- **No `database/sql`**: Use `pgx/v5` exclusively. `errors.Is(err, pgx.ErrNoRows)` for not-found checks.
- **No hand-written migrations**: Generate with `make migrate-create service=X name=Y` (Goose format); migrations live per-binary under `backend/cmd/{service}/migrations/`

### HTTP & Routing
- **No third-party routers**: Routes implement the `api.Controller` interface with `GroupedBy()`, `Middleware()`, and `Boot(*api.Mux)`. No gin, chi, echo, or gorilla/mux.
- **No raw JSON decoding**: Use `api.DecodeJSON(ctx, r, w, logger, &req)` in handlers, never `json.NewDecoder(r.Body).Decode(...)`.
- **No raw response writing**: Use `api.JSON(...)` and `api.ErrorResponse(...)`, never `w.WriteHeader(code)` or `http.Error(w, ...)` directly in handlers.
- **No raw HTTP clients**: Service-to-service HTTP calls must use a typed client in `internal/platform/clients/{service}/client.go` that embeds `httpclient.Base`. Never instantiate `http.Client{}` directly in feature or service code.

### Logging
- **No global loggers**: Inject `*logging.Logger` into structs; all log calls take `ctx context.Context` as first argument.
- **No stdlib log**: Never use `log`, `log/slog`, or `fmt.Print*` for logging. Use the injected `*logging.Logger`.

### Configuration
- **No secrets in YAML**: Secrets load via `os.Getenv` only; YAML configs use `yaml:"-"` for secret fields.
- **Config uses `config.LoadService[T]` / `config.Load[T]`**: Binaries load a typed config from `backend/cmd/{service}/config/base.yaml` (+ optional `{env}.yaml`) via `MustLoadService[T]`; `APP_ENV` selects the environment. No raw viper, no raw yaml library imports outside `platform/config/`.

### Errors & Observability
- **Wrap errors with `%w`**: Always use `fmt.Errorf("context: %w", err)`, never `%s` for error wrapping.
- **Static span status descriptions**: `span.SetStatus(codes.Error, sentinel.Error())` must use a package-level sentinel error string, never `err.Error()` or `ctx.Err().Error()` inline. Use `span.RecordError(err)` for dynamic context.
- **No raw OTel in typed clients**: Platform clients in `internal/platform/clients/` must not import `go.opentelemetry.io/otel/codes` or `go.opentelemetry.io/otel/trace` directly; the `httpclient.Base` handles all span lifecycle.

### Code Structure
- **Cross-cutting code lives in `internal/platform/`**: Feature packages must not import each other.
- **ADR before implementation**: Significant architectural decisions require an ADR in `docs/adr/` before code is written.

## Automated Enforcement

All conventions above are mechanically enforced. Do not add inline `//nolint` directives; exceptions are managed centrally via path-scoped exclusions in `.golangci.yml`.

**Tier 1: AST rules** (`rules/orbit_rules.go`, enforced via `gocritic/ruleguard`):

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

**Tier 2: Import bans** (`depguard` in `.golangci.yml`):

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
- **[docs/adr/2026-02-17-repository-pattern-and-code-organization.md](docs/adr/2026-02-17-repository-pattern-and-code-organization.md)** - Repository pattern and feature structure
- **[docs/adr/2026-03-19-multi-service-backend-deployment-structure.md](docs/adr/2026-03-19-multi-service-backend-deployment-structure.md)** - Multi-binary deployment model

### Key Principles

**Multi-binary deployment**: The backend compiles to multiple binaries under `backend/cmd/`. Each binary has its own `main.go` and `config/` directory. All binaries share the `backend/internal/platform/` layer.

| Binary | Path | Purpose |
|--------|------|---------|
| `identity` | `cmd/identity/` | **Main control-plane API**: identity, sessions, PATs, tenant registry (port 8080) |
| `inference-gateway` | `cmd/inference-gateway/` | OpenAI/Anthropic-compatible LLM gateway (port 8081) |
| `policy-service` | `cmd/policy-service/` | Authz data plane (PDP/PAP): grants, roles, OPA bundles (port 8083) |
| `policy-gateway` | `cmd/policy-gateway/` | In-app authz API (PEP) (port 8084) |
| `orbit-agents` | `cmd/orbit-agents/` | Agent control plane: runs, sessions, event streaming, k8s orchestration |
| `orbit-triggers` | `cmd/orbit-triggers/` | Triggers: webhook ingestion, trigger API, cron scheduler, manual fire (port 8097) |
| `session-manager` | `cmd/session-manager/` | Per-tenant runner for the agent (Claude Code) subprocess (port 8090) |
| `operator` | `cmd/operator/` | Kubernetes operator: reconciles Tenant/Platform CRDs, injects sidecars |
| `identity-proxy` | `cmd/identity-proxy/` | Envoy ext_proc sidecar: injects platform tokens into agent outbound calls |
| `sandbox-exec`, `orbit-runtime-mcp`, `orbit-shell`, `orbit-session-runner` | `cmd/*/` | Agent sandbox/exec stack (gRPC exec, MCP provisioning, shell shim, isolation wrapper) |
| `example` | `cmd/example/` | Reference implementation (not production) |

> There is **no `cmd/orbit`** binary; the main API is `cmd/identity`. See [docs/code-organization.md](docs/code-organization.md) for the complete map.

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
4. **Observable**: Complete visibility into agent behavior: distributed tracing, structured logging, log-trace correlation
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

## Testing Strategy

This describes what the backend test suite **actually is today**, and (separately and clearly labelled) where it's headed.

**What exists today:**
- **Unit tests**: the bulk of coverage, table-driven, across packages. Run by `make test` (race + coverage) and by `make check`; no cluster or database required.
- **One Postgres-backed repository test**: `backend/internal/triggers/postgres_test.go`, gated on `ORBIT_TEST_DB_URL` and skipped when it's unset. It is currently the *only* test that talks to a real database.
- **One integration suite**: `backend/tests/integration/policy/authz_test.go` (build tag `integration`): a live-cluster authz test run via `make test-integration`. It needs a full running cluster (`dev-cluster` + `cluster-operator` + `cluster-deploy` + `auth-device-k3d`) and is **not** run by CI or by `make check`.
- **No dedicated lint-rule unit tests**: custom `golangci-lint` rules are validated empirically via `make check`; no `analysistest` harness, to keep `backend/go.mod` lean.

Write tests alongside new code; unit tests are the default and must pass in `make check`.

## Implementation Principles

When implementing Orbit components:

- **Policy Evaluation**: Always evaluate policies before executing actions, not after
- **Event Emission**: Emit structured events for all significant actions and decisions
- **Credential Brokering**: Never expose raw credentials to agents; use scoped, temporary access
- **Identity Context**: Carry identity context through the entire execution chain via `context.Context`
- **Repository Pattern**: Define repository interfaces in feature packages, implement with PostgreSQL in the same package
- **Feature Independence**: Features must not import each other; share only through `internal/platform/`
- **Idiomatic Go**: Favor simplicity, flat packages, and explicit code over deep abstraction hierarchies
- **Sentinel Errors**: Define package-level `var ErrXxx = errors.New("...")` rather than inline string errors; wrap with `%w`

See [repository-pattern-and-code-organization](docs/adr/2026-02-17-repository-pattern-and-code-organization.md) for detailed architectural patterns and the rationale behind our code organization approach.

## Frontend Development

All product JSX must use Terra (`@orbit/terra`). Raw HTML primitives are banned by ESLint with **zero escape hatches**. `eslint-disable` comments targeting any Terra rule are themselves a lint error (`local/no-terra-disable-comment`). Violations fail `pnpm lint`.

| Rule | What it bans | Use instead |
| ---- | ------------ | ----------- |
| `local/no-raw-layout-elements` | `<div>`, `<section>`, `<aside>`, `<nav>`, `<header>`, `<footer>`, `<article>`, `<main>` | `Stack`, `Grid`, or `Stack asChild`/`Grid asChild` for semantic elements |
| `local/no-raw-html-text-tags` | `<p>`, `<h1>`–`<h6>` | `Text`, `Heading` |
| `local/no-raw-inline-elements` | `<span>`, `<strong>`, `<b>`, `<em>`, `<i>`, `<code>`, `<kbd>`, all other phrasing-content HTML | `Text`, `Text weight="heavy"`, `Code` |
| `local/no-raw-interactive-elements` | `<button>` | `Button` |
| `local/no-terra-classname` | `className` on any Terra component | Use the component's own props |
| `local/no-direct-lucide-import` | `import { X } from 'lucide-react'` in `src/app/**` or `src/components/**` | `<Icon name="…" />`: **no escape hatch**. Add icons to `src/packages/terra/src/components/icon/registry.ts` only. |
| `local/no-terra-disable-comment` | Any `eslint-disable` targeting a Terra rule | There is no escape hatch; use the correct Terra component |

**Common HTML → Terra replacements:**

| Raw HTML | Terra replacement |
| -------- | ----------------- |
| `<div className="flex flex-col gap-4">` | `<Stack gap="comfortable">` |
| `<div className="flex flex-row gap-2">` | `<Stack direction="horizontal" gap="snug">` |
| `<div className="grid grid-cols-3 gap-4">` | `<Grid cols={3} gap="comfortable">` |
| `<div className="flex items-center justify-between">` | `<Stack direction="horizontal" align="center" justify="between">` |
| `<section>` | `<Section>` |
| `<section className="flex flex-col gap-8 p-6">` | `<Stack asChild gap="relaxed"><section>…</section></Stack>` |
| `<aside className="grid grid-cols-2">` | `<Grid asChild cols={2}><aside>…</aside></Grid>` |
| `<button onClick={fn}>label</button>` | `<Button onClick={fn}>label</Button>` |
| `<h1>…</h1>` | `<Heading variant="title">…</Heading>` |
| `<h2>…</h2>` | `<Heading variant="section">…</Heading>` |
| `<p>Body copy</p>` | `<Text>Body copy</Text>` |
| `<p className="text-sm text-muted">…</p>` | `<Text size="supporting" color="muted">…</Text>` |
| `<span>Label</span>` | `<Text as="span" size="supporting">Label</Text>` (inline) or `<Text>` (block) |
| `<strong>…</strong>` / `<b>…</b>` | `<Text weight="heavy">…</Text>` |
| `<code>value</code>` | `<Code>value</Code>` (prose) or `<Text mono>` (standalone technical value) |
| `import { X } from 'lucide-react'` | `<Icon name="…" />` |

**Missing Terra component:** If no Terra component covers the use case:

1. Do not write the raw element, or `pnpm lint` will fail.
2. Write a short proposal: element needed, suggested name and minimal API, Radix/ShadCN base if one exists.
3. Wait for approval from a human engineer before writing any code in `src/packages/terra/`.
4. Once approved: implement with `cva` + `cn`, `React.forwardRef`, `.displayName`, `data-slot`. Base on ShadCN/Radix where available. Export from `src/packages/terra/src/index.ts`.
5. Replace the raw element across the codebase.

**Component placement (three-tier model):**

| Tier | Name | Filesystem | Import | Storybook prefix |
| ---- | ---- | ---------- | ------ | ---------------- |
| **1** | Terra | `src/packages/terra/src/components/` | `import { X } from '@orbit/terra'` | `Terra/Atoms/`, `Terra/Molecules/`, `Terra/Layout/`, `Terra/Organisms/` |
| **2** | App-level shared | `src/components/` | `import { X } from '@orbit/components'` | `App/Shared/` |
| **3** | Feature-scoped | `src/app/(dashboard)/[feature]/_components/` | relative import | n/a |

Apply this decision tree in order, stop at the first match, never pre-generalise:

1. Used by exactly one file → keep inline. Extract only if the host file exceeds ~300 lines of JSX.
2. All consumers are within the same feature area → **Tier 3**: `src/app/(dashboard)/[feature]/_components/`, relative import.
3. Consumers span two or more feature areas **and** no Orbit domain knowledge (no status tokens, principal types, model/provider names, or permission vocabulary) → **Tier 1 (Terra)**. Requires human approval before any Terra modification.
4. Consumers span two or more feature areas **and** Orbit-domain-aware → **Tier 2**: `src/components/`, export from `src/components/index.ts`.

Every extracted component file must open with a `@tier` JSDoc comment:

```ts
/** @tier 1 — Zero Orbit domain knowledge. Could be extracted to a standalone library. */

/** @tier 2 — Orbit-domain-aware: uses Orbit status tokens. Used by: dashboard overview, model-gateway. */

/** @tier 3 — model-gateway-scoped. Promote to src/components/ if a second feature needs this. */
```

Promoting Tier 3 → 2 → 1 is easy; splitting an over-generalised component is hard. Always start at the most specific tier.

All `src/` filenames must be kebab-case. Files in `src/app/**` that import React client hooks need `"use client"` as the first line; files in `src/server/**` and Route Handlers need `import "server-only"` as the very first import. Before marking any frontend task complete: `cd frontend && pnpm lint && pnpm tsc --noEmit`; both must pass with zero errors.

**Reference docs:** [`frontend/docs/reference/component-placement.md`](frontend/docs/reference/component-placement.md) (full Terra component inventory, Storybook conventions, authoring rules) · [`frontend/docs/reference/app-components.md`](frontend/docs/reference/app-components.md) (Tier 2 component API: EmptyState, StatusBadge, ConfirmDialog, MetricCard, EventFeedItem) · [`frontend/docs/reference/terra-design-system.md`](frontend/docs/reference/terra-design-system.md) (full Terra usage, props, examples, verification checklist) · [`frontend/docs/golden-paths/frontend-state-pattern.md`](frontend/docs/golden-paths/frontend-state-pattern.md) (fetching, caching, reactive state)

## Getting Help

- **Architecture questions**: Open a discussion or issue referencing the relevant ADR
- **Security concerns**: See `SECURITY.md` for private reporting
- **General questions**: Use GitHub Discussions

## License

Orbit is licensed under Apache License 2.0. All contributions must be compatible with this license.

---

**Remember**: Orbit is the control plane underneath production agentic systems. Every change should answer one or more of: *Who is this agent? What can it access? How does it connect?*, and never weaken the others.
