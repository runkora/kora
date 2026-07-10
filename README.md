# Orbit

**The agentic engineering platform — from POC to production.**

---

Orbit is the control plane for AI agents. It closes the gap between an agent running on a developer's laptop and a secure, governed agent running in production — the gap where most enterprise AI efforts stall.

It is established platform engineering applied to a new domain: SPIFFE workload attestation, OPA policy-as-code, Istio/Envoy at the edge, all Kubernetes-native. Industry-standard tools, not invented primitives. The problems aren't new — the workloads are.

Maintained by [ClearRoute](https://www.clearroute.io).

## The three questions

Putting an agent into production raises three questions. Orbit answers each one directly.

### Who is this agent?
Every agent gets a machine-verifiable identity the moment it deploys — no human managing secrets or provisioning credentials. Humans and agents share one identity model, so a person can delegate work to an agent with defined scope and full accountability, and the agent can never access more than the person who delegated to it.

### What can it access?
Every action is checked against policy before it executes. Admins define exactly what each agent can do, down to the individual tool, and anything outside that list is rejected and logged. Enforcement happens at the edge inside each pod — not by trusting the agent to behave.

### How does it connect?
Agents never handle credentials. The platform intercepts outbound calls and injects the right credentials just in time, deferring to the upstream system as the authority on what a user can access. A Claude Code or Codex instance drops in without modifying its auth handling.

Every agent action traces back to the human who initiated it and the policy that authorised it. The "how do we know the agent isn't going rogue?" question gets a concrete answer.

## What Orbit is — and isn't

Orbit **is** the control plane for agents — the runtime platform underneath production agentic systems.

It **is not** an agent framework, a model router, or an LLM wrapper. It does not write your agent for you; it provides the identity, policy, and gateway substrate that lets one run safely in your environment.

## Capabilities

### Identity & Authentication
Verified identity for humans and agents under a single model. Provider-agnostic OIDC (Entra, Okta, Google Workspace, Keycloak) with the OAuth 2.0 Device Authorization Grant for headless CLIs. Workload identity via SPIFFE SVIDs exchanged for Orbit platform tokens. Personal Access Tokens for programmatic access. Multi-tenant identity registry. Local development uses [Dex](https://dexidp.io/) — no cloud credentials required.

### Policy Enforcement
Pre-execution authorization. Open Policy Agent (OPA) for the engine; a PDP/PAP (`policy-service`) owns grants, roles, and the route-scope vocabulary; a PEP (`policy-gateway`) answers `check`/`query`/`explain`/`simulate` for owned services; gateway-level OPA enforces external traffic. Admin dashboard ships in the frontend.

### Inference Gateway
A unified, OpenAI/Anthropic-compatible interface across providers (AWS Bedrock, Google Vertex, Azure, OpenAI). Model catalog with capability taxonomy and alias/virtual-model routing, an Anthropic zero-translation path, embeddings and Responses endpoints, plus per-request usage logging and analytics.

### Agent Run Control
Agent type registry, run/session lifecycle, event streaming, BYOC discovery, per-tenant Kubernetes orchestration. `session-manager` runs Claude Code (or your agent of choice) per session in an isolated workspace; the sandbox stack provides Landlock/namespace-isolated command execution.

### Identity-aware egress
The `identity-proxy` Envoy `ext_proc` sidecar exchanges each pod's SPIFFE SVID for an Orbit platform token and injects it as a `Bearer` header on outbound calls. Agents never see credentials.

### JIT credential brokering
The `egress-gateway` brokers scoped, just-in-time external credentials into outbound calls (GitHub App, Kubernetes bearer, and passthrough adapters) so agents never hold raw secrets.

### Governed MCP / tool gateway
The `tool-gateway` (with the `orbit-mcp` server) governs MCP tool access — catalog, consent, OAuth, and policy-checked invocation — so every tool call is authorised before it runs.

### Observability
OpenTelemetry traces and metrics across every service, W3C trace propagation through typed platform HTTP clients, log–trace correlation via the injected structured logger.

### Future
A platform-wide event spine — a general structured event bus for awareness and audit — is designed (ADRs exist) but not yet implemented. See [docs/architecture.md](docs/architecture.md) for the current build status.

## Use Cases

- **Enterprise AI Operations**: Deploy agents with enterprise-grade security and governance
- **Multi-Agent Systems**: Coordinate fleets of agents with shared policy and identity
- **Regulated Environments**: Maintain audit trails and enforce compliance policies
- **Production Automation**: Run autonomous agents safely in real engineering environments
- **AI Platform Teams**: Provide self-service agent infrastructure for internal teams

## Architecture

See [docs/architecture.md](docs/architecture.md) for detailed architecture documentation.

## Getting Started

### Prerequisites

**Tier 1 — Local development (backend + Postgres + Redis + Dex)**
- Go 1.26 or later
- Docker Desktop (or Colima on macOS)
- Make

**Tier 2 — Full cluster (adds Istio + SPIRE + the Orbit services)**
- [Colima](https://github.com/abiosoft/colima) (macOS) or Docker Desktop — minimum 4 CPUs / 8 GiB RAM allocated to the VM
- k3d, kubectl, helm — installed via `make setup`. (Istio and SPIRE are not host tools; the operator installs them into the cluster during `make cluster-operator`.)

### Quick Start

**Tier 1 — First-run setup**

```bash
make setup             # install goose, air, golangci-lint, gitleaks; tidy deps
make env               # copy .env.example → .env
make dev-up            # start Postgres + Redis + Dex (auto-generates signing key + Dex credentials on first run)
make migrate-up        # apply database migrations
make dev               # start Orbit with hot-reload
```

**Authenticate (in a second terminal):**

```bash
make auth-device       # device auth flow → log in as dev@orbit.local.clearroute.io / password
                       # token saved to /tmp/orbit_credentials.json
```

**Tier 2 — Full local cluster** (three ordered steps — `dev-cluster` alone is just the cluster + backing services)
```bash
make dev-cluster       # create the k3d cluster (starts Postgres + Redis + Dex first)
make cluster-operator  # install the Orbit operator → deploys SPIRE + Istio + the Orbit services
make cluster-deploy    # register tenants and deploy tenant workloads
```

For a detailed walkthrough see the **[Local Development Guide](docs/guides/local-development.md)**.

### Development Commands

**Setup & Credentials**
- `make setup` — Install development tools and git hooks
- `make env` — Copy `.env.example` → `.env`
- `make gen-signing-key` — Regenerate RSA signing key at `secrets/signing.key` (auto-run by `dev-up` on first run)
- `make gen-session-secret` — Print a `BFF_SESSION_SECRET` for `.env` (never written to disk)
- `make dex-env` — Bootstrap `config/dex/.env` with the default dev password, no-op if already exists (auto-run by `dev-up` on first run)
- `make dex-hash password=X` — Generate Dex credentials with a custom password

**Backing Services (Tier 1)**
- `make dev-up` — Start Postgres + Redis + Dex (bootstraps signing key + Dex credentials if missing)
- `make dev-down` — Stop all Tier 1 containers

**Database Migrations**
- `make migrate-up` — Apply pending migrations
- `make migrate-down` — Roll back last migration
- `make migrate-status` — Show applied and pending migrations
- `make migrate-create service=X name=Y` — Generate a new Goose migration file for a service (e.g. `service=identity name=add_sessions_index`)

**Development**
- `make dev` — Start the main API (the `identity` service) with hot-reload (`air`)
- `make auth-device` — Authenticate via Device Authorization Grant
- `make build-backend` — Build the Go service binaries to `.bin/`
- `make frontend-dev` — Start the Next.js frontend (see [frontend/README.md](frontend/README.md))

**Testing & Quality**
- `make check` — Full quality gate: tidy, fmt, `go vet`, `golangci-lint`, secret scan, `docs-check` (doc-drift guard), backend unit tests, **and** the frontend checks (lint + type-check + tests). Run from the repo root.
- `make test` — Run unit tests with coverage (no cluster required)
- `make test-integration` — Run the live-cluster authz suite. Requires a full running cluster (`dev-cluster` + `cluster-operator` + `cluster-deploy` + `auth-device-k3d`); **not** run by CI or `make check`.
- `make docs-check` — Guard the docs against drift (broken links, missing index entries, dead make targets, stale binary table, hardcoded counts)

**Cluster (Tier 2)**
- `make dev-cluster` — Create the local k3d cluster (starts backing services first); run `make cluster-operator` next
- `make cluster-operator` — Install the Orbit operator, which deploys SPIRE + Istio + the Orbit services
- `make cluster-deploy` — Register tenants and deploy tenant workloads
- `make dev-cluster-down` — Tear down the local cluster

**Cleanup**
- `make clean` — Remove build artifacts

## Contributing

We welcome contributions! Please see [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

For security vulnerabilities, please see our [Security Policy](SECURITY.md).

## License

Orbit is licensed under the Apache License 2.0. See [LICENSE](LICENSE) for details.

---

**Orbit — the agentic engineering platform.**