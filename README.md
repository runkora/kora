# Orbit

**The Nervous System for Scalable AI Agents**

---

Agents don't fail because they're unintelligent.  
They fail because they lack **coordination, identity, and control**.

Orbit provides the signaling, policy, and identity infrastructure required to safely scale autonomous agents inside real systems.

From a single agent to an intelligent fleet — Orbit keeps them connected, governed, and observable.

**Scale intelligence — without losing control.**

---

## The Problem

Agents can think.  
They can act.  
They can decide.

But without coordination, they are isolated reflexes.

- Without **identity**, they are unsafe.
- Without **policy**, they are unpredictable.
- Without **signaling**, they are blind.

Orbit is the nervous system that connects intelligence to action.

## What Orbit Does

Orbit ensures that:
- Every decision is **contextual**
- Every action is **authorized**
- Every interaction is **observable**
- Every agent is **accountable**

Scaling intelligence requires coordination.  
**Orbit makes coordination possible.**

## The Nervous System Metaphor

Just as a biological nervous system coordinates complex organisms, Orbit coordinates autonomous agents:

| Component | Nervous System Function |
|-----------|------------------------|
| **Identity** | Sensory input — who is acting? |
| **Policy** | Reflex control — what's allowed? |
| **Token Broker** | Motor function — how action is executed |
| **Event Spine** | Neural signaling — awareness & feedback |
| **Autonomy Levels** | Brain stem vs cortex — decision hierarchy |
| **Observability** | Feedback loop — learning & adjustment |

## What Orbit Is Not

Orbit is **not**:
- ❌ A framework
- ❌ A model router
- ❌ A wrapper around LLM APIs

Orbit **is**:
- ✅ The control plane for agents
- ✅ The runtime platform for autonomous systems
- ✅ Infrastructure for production-grade AI agents

## Core Capabilities

### Identity & Authentication
Establish verified identity for humans and agents. Provider-agnostic OIDC integration — works with Entra, Okta, Google Workspace, Keycloak, or any standards-compliant provider. Supports the OAuth 2.0 Device Authorization Grant (RFC 8628) for headless CLI authentication. Local development uses [Dex](https://dexidp.io/) — no cloud credentials required.

### Policy Enforcement
Govern what agents can do **before** they do it. OPA-compatible policy engine with RBAC and PBAC.

### Credential Brokering
Securely provide agents with scoped credentials without direct access. Just-in-time provisioning with full audit trails.

### Agent Run Control
Orchestrate agent lifecycle, manage execution state, coordinate multi-agent workflows.

### Model Gateway
Route model calls, enforce quotas, track costs, provide fallback and caching.

### Tool/MCP Gateway
Broker tool access with permission enforcement. Full integration with Model Context Protocol (MCP).

### Event Spine
Emit structured events for complete observability and audit trails of all agent decisions and actions.

## Use Cases

- **Enterprise AI Operations**: Deploy agents with enterprise-grade security and governance
- **Multi-Agent Systems**: Coordinate fleets of agents with shared policy and identity
- **Regulated Environments**: Maintain audit trails and enforce compliance policies
- **Production Automation**: Run autonomous agents safely in real engineering environments
- **AI Platform Teams**: Provide self-service agent infrastructure for internal teams

## Architecture

See [docs/architecture.md](docs/architecture.md) for detailed architecture documentation.

## Development Status

Orbit is in **active early development**. The following are fully implemented: identity layer (OIDC federation, Device Authorization Grant, SPIFFE SVID exchange, session management, JWT issuance and revocation), the Envoy ext_proc token injection sidecar (`cmd/identity-proxy`), service-to-service typed HTTP clients with W3C trace propagation, platform infrastructure (HTTP controller routing, structured logging, OpenTelemetry, PostgreSQL, Redis), and the local development environment. Policy enforcement, agent execution, credential brokering, and the event spine are designed and under active development.

## Getting Started

### Prerequisites

**Tier 1 — Local development (backend + Postgres + Redis + Dex)**
- Go 1.26 or later
- Docker Desktop (or Colima on macOS)
- Make

**Tier 2 — Full cluster (adds k3d + Istio + SPIRE)**
- [Colima](https://github.com/abiosoft/colima) (macOS) or Docker Desktop — minimum 4 CPUs / 8 GiB RAM allocated to the VM
- k3d, kubectl, istioctl, helm — installed via `make setup`

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
make auth-device       # device auth flow → log in as dev@orbit.test / password
                       # token saved to /tmp/orbit_credentials.json
```

**Tier 2 — Full local cluster**
```bash
make dev-cluster       # provisions k3d cluster + SPIRE + Istio (~2 min)
```

For a detailed walkthrough see the **[Local Development Guide](docs/guides/local-development.md)**.

### Development Commands

**Setup & Credentials**
- `make setup` — Install development tools and git hooks
- `make env` — Copy `.env.example` → `.env`
- `make gen-signing-key` — Regenerate RSA signing key at `secrets/signing.key` (auto-run by `dev-up` on first run)
- `make dex-env` — Bootstrap `config/dex/.env` with the default dev password, no-op if already exists (auto-run by `dev-up` on first run)
- `make dex-hash password=X` — Generate Dex credentials with a custom password

**Backing Services (Tier 1)**
- `make dev-up` — Start Postgres + Redis + Dex (bootstraps signing key + Dex credentials if missing)
- `make dev-down` — Stop all Tier 1 containers

**Database Migrations**
- `make migrate-up` — Apply pending migrations
- `make migrate-down` — Roll back last migration
- `make migrate-status` — Show applied and pending migrations
- `make migrate-create name=X` — Generate a new Goose migration file

**Development**
- `make dev` — Start backend with hot-reload
- `make auth-device` — Authenticate via Device Authorization Grant
- `make build` — Build all service binaries (`orbit`, `identity`, `identity-proxy`) to `.bin/`

**Testing & Quality**
- `make check` — Run `go vet`, `golangci-lint`, `gitleaks`, and tests
- `make test` — Run unit tests with coverage
- `make test-integration` — Run integration tests (requires Tier 2 cluster)

**Cluster (Tier 2)**
- `make dev-cluster` — Provision k3d cluster with Istio + SPIRE
- `make dev-cluster-down` — Tear down the local cluster

**Cleanup**
- `make clean` — Remove build artifacts

## Contributing

We welcome contributions! Please see [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

For security vulnerabilities, please see our [Security Policy](SECURITY.md).

## License

Orbit is licensed under the Apache License 2.0. See [LICENSE](LICENSE) for details.

---

**Orbit: Scale intelligence — without losing control.**