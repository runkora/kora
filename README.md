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
Establish verified identity for humans and agents. Integration with enterprise SSO (OIDC, SAML, Entra).

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

Orbit is in **early development**. We are actively building the core platform components.

## Getting Started

### Prerequisites

- Go 1.21 or later
- Docker and Docker Compose (for PostgreSQL)
- Make

### Quick Start

```bash
# Bootstrap: install tools, start database, run migrations
make bootstrap

# Start Orbit in development mode
make dev
```

### Development Commands

Run `make help` to see all available commands. Key workflows:

**Setup & Installation**
- `make setup` - Complete project setup
- `make install-tools` - Install development tools
- `make tidy` - Tidy dependencies

**Database Management**
- `make db-up` - Start PostgreSQL
- `make db-down` - Stop PostgreSQL
- `make db-reset` - Reset database
- `make migrate-up` - Run migrations
- `make migrate-status` - Check migration status
- `make migrate-create name=<name>` - Create new migration

**Development**
- `make dev` - Run with auto-reload
- `make build` - Build binary to `.bin/orbit`
- `make run` - Run Orbit

**Testing & Quality**
- `make test` - Run tests with coverage
- `make test-verbose` - Verbose test output
- `make lint` - Run linters
- `make check` - Run all checks (test + lint)

**Cleanup**
- `make clean` - Remove build artifacts
- `make clean-all` - Full cleanup (artifacts + database)

## Contributing

We welcome contributions! Please see [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

For security vulnerabilities, please see our [Security Policy](SECURITY.md).

## License

Orbit is licensed under the Apache License 2.0. See [LICENSE](LICENSE) for details.

---

**Orbit: Scale intelligence — without losing control.**