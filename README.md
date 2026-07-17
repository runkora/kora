# Orbit

**The agentic engineering platform: from POC to production.**

---

Orbit is the control plane for AI agents. It closes the gap between an agent running on a developer's laptop and a secure, governed agent running in production, the gap where most enterprise AI efforts stall.

It is established platform engineering applied to a new domain: SPIFFE workload attestation, OPA policy-as-code, Istio/Envoy at the edge, all Kubernetes-native. Industry-standard tools, not invented primitives. The problems aren't new; the workloads are.

Maintained by [ClearRoute](https://www.clearroute.io).

## The three questions

Putting an agent into production raises three questions. Orbit answers each one directly.

### Who is this agent?
Every agent gets a machine-verifiable identity the moment it deploys, with no human managing secrets or provisioning credentials. Humans and agents share one identity model, so a person can delegate work to an agent with defined scope and full accountability, and the agent can never access more than the person who delegated to it.

### What can it access?
Every action is checked against policy before it executes. Admins define exactly what each agent can do, down to the individual tool, and anything outside that list is rejected and logged. Enforcement happens at the edge inside each pod, not by trusting the agent to behave.

### How does it connect?
Agents never handle credentials. The platform intercepts outbound calls and injects the right credentials just in time, deferring to the upstream system as the authority on what a user can access. A Claude Code or Codex instance drops in without modifying its auth handling.

Every agent action traces back to the human who initiated it and the policy that authorised it. The "how do we know the agent isn't going rogue?" question gets a concrete answer.

## What Orbit is, and isn't

Orbit **is** the control plane for agents, the runtime platform underneath production agentic systems.

It **is not** an agent framework, a model router, or an LLM wrapper. It does not write your agent for you; it provides the identity, policy, and gateway substrate that lets one run safely in your environment.

## Capabilities

### Identity & Authentication
Verified identity for humans and agents under a single model. Provider-agnostic OIDC (Entra, Okta, Google Workspace, Keycloak) with the OAuth 2.0 Device Authorization Grant for headless CLIs. Workload identity via SPIFFE SVIDs exchanged for Orbit platform tokens. Personal Access Tokens for programmatic access. Multi-tenant identity registry. Local development uses [Dex](https://dexidp.io/), no cloud credentials required.

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
The `tool-gateway` (with the `orbit-mcp` server) governs MCP tool access (catalog, consent, OAuth, and policy-checked invocation) so every tool call is authorised before it runs.

### Observability
OpenTelemetry traces and metrics across every service, W3C trace propagation through typed platform HTTP clients, log–trace correlation via the injected structured logger.

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

- Go 1.26 or later
- Docker (Docker Desktop or Colima) with at least 4 CPUs and 8 GiB of RAM allocated to the VM
- Node 24 and pnpm 10 (frontend)
- Make

`make setup` installs the rest: k3d, kubectl, helm, and the Go tooling. Istio and SPIRE are not host tools; the operator installs them into the cluster during `make up`.

### Quick Start

New here? [docs/guides/day-one.md](docs/guides/day-one.md) takes you from clone to a running change, backend and frontend.

Local development runs in Kubernetes. Three commands:

```bash
make setup   # once per machine: generate .env, install tools, git hooks, local DNS, mkcert TLS
make up      # backing services, k3d cluster, migrations, operator (SPIRE + Istio + Orbit), tenants, workloads. Idempotent.
make auth    # Device Authorization Grant against the cluster → dev@orbit.local.clearroute.io / password
```

`make up` is idempotent: re-run it whenever the cluster drifts. `make down` tears the cluster and backing services down.

Endpoints once `make up` completes:

- Frontend: `https://orbit.local.clearroute.io:8443`
- Inference gateway: `https://api.orbit.local.clearroute.io:8443/v1`

`make auth` writes the token pair to `/tmp/orbit_credentials.json`. Call an authenticated API by reading the token inline:

```bash
TOKEN=$(jq -r '.access_token' /tmp/orbit_credentials.json) && curl -sk \
  -H "Authorization: Bearer $TOKEN" \
  https://api.orbit.local.clearroute.io:8443/v1/models
```

When the token expires, `make auth-refresh` rotates the pair.

For a detailed walkthrough see the **[Local Development Guide](docs/guides/local-development.md)**. Everything else lives in **[docs/](docs/README.md)**.

### Development Commands

**Environment**
- `make setup`: Once per machine. Generate `.env`, install tools, git hooks, local DNS, mkcert TLS.
- `make up`: Bring up everything: backing services, k3d cluster, migrations, operator (SPIRE + Istio + Orbit services), tenants, workloads. Idempotent.
- `make down`: Tear down the cluster and backing services.

**Inner loop**
- `make redeploy`: Rebuild and roll every service. `SERVICE=<name>` narrows it to one.
- `make redeploy-list`: List every service known to `make redeploy`.
- `make redeploy-status`: Show drift between cluster-deployed images and the local registry.
- `make redeploy-warm`: Pre-build every image to warm BuildKit caches before the first change.

**Credentials**
- `make auth`: Authenticate via Device Authorization Grant against the cluster.
- `make auth-refresh`: Rotate the token pair in `/tmp/orbit_credentials.json`.

**Database Migrations**
- `make migrate-up`: Apply pending migrations.
- `make migrate-down`: Roll back the last migration.
- `make migrate-status`: Show applied and pending migrations.
- `make migrate-create service=X name=Y`: Generate a new Goose migration file for a service (e.g. `service=identity name=add_sessions_index`).

**Testing & Quality**
- `make check`: Full quality gate: tidy, fmt, `go vet`, `golangci-lint`, secret scan, `docs-check` (doc-drift guard), backend unit tests, **and** the frontend checks (lint + type-check + tests). Run from the repo root.
- `make test`: Run unit tests with coverage. No cluster required.
- `make test-integration`: Run the live-cluster authz suite. Requires a running cluster; **not** run by CI or `make check`.
- `make docs-check`: Guard the docs against drift (broken links, missing index entries, dead make targets, stale binary table, hardcoded counts).

**Granular cluster steps** (composed by `make up`)
- `make dev-cluster`: Create the k3d cluster and start backing services.
- `make cluster-operator`: Install the Orbit operator, which deploys SPIRE + Istio + the Orbit services.
- `make cluster-deploy`: Register tenants and deploy tenant workloads.
- `make dev-cluster-down`: Tear down the cluster.

**Cleanup**
- `make clean`: Remove build artifacts.

## Contributing

We welcome contributions! Please see [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

For security vulnerabilities, please see our [Security Policy](SECURITY.md).

## License

Orbit is licensed under the Apache License 2.0. See [LICENSE](LICENSE) for details.

---

**Orbit: the agentic engineering platform.**