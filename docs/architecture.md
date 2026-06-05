# Orbit Architecture

Orbit is the **control plane for AI agents** — the agentic engineering platform that closes the gap between an agent running on a developer's laptop and a secure, governed agent running in production. It is **not** an agent framework, a model router, or a wrapper around LLM APIs.

This document describes the system as it exists today. For the code layout see [code-organization.md](code-organization.md); for the catalogue of decisions see the [ADR index](adr/README.md).

## The three questions

Putting an agent into production raises three questions, and the architecture is organised around them.

### Who is this agent?

Every agent gets a machine-verifiable identity at deploy time, with no human managing secrets or provisioning credentials. Humans and agents share one identity model, so a person can delegate to an agent with defined scope and full accountability — and the agent can never access more than the delegator.

| Implemented by | What it does |
|----------------|--------------|
| `identity` service ([Identity, Auth & Tokens ADRs](adr/README.md#identity-auth--tokens)) | OIDC federation, sessions, PATs, multi-tenant registry, JWT issuance/revocation |
| `identity-proxy` Envoy `ext_proc` sidecar | Exchanges each pod's SPIFFE SVID for an Orbit platform token at runtime |
| SPIFFE/SPIRE | Workload attestation in the mesh |
| Frontend BFF | Browser sessions federated to OIDC and exchanged for an Orbit session |

### What can it access?

Every action is checked against policy *before* it executes. Admins define exactly what each agent can do, down to the individual tool, and anything outside that list is rejected and logged. Enforcement happens at the edge inside each pod — not by trusting the agent to behave.

| Implemented by | What it does |
|----------------|--------------|
| `policy-service` (PDP/PAP) | Owns grants, roles, and the route-scope vocabulary; compiles & serves OPA bundles |
| `policy-gateway` (PEP) | Answers `check`/`query`/`explain`/`simulate` for owned services |
| Gateway-level OPA | Authorises external traffic at the edge ([gateway-level-opa-enforcement-for-external-traffic](adr/2026-04-27-gateway-level-opa-enforcement-for-external-traffic.md)) |
| Platform middleware | Enforces route scopes inside every service ([platform-scope-enforcement-middleware](adr/2026-06-04-platform-scope-enforcement-middleware.md)) |
| Policy admin dashboard (frontend) | Manages grants, roles, and policy simulation |

### How does it connect?

Agents never handle credentials. The platform intercepts outbound calls and injects the right credentials just-in-time, deferring to the upstream system as the authority on what a user can access. A Claude Code or Codex instance drops in without modifying its auth handling.

| Implemented by | What it does |
|----------------|--------------|
| `inference-gateway` ([Inference Gateway ADRs](adr/README.md#inference-gateway)) | OpenAI/Anthropic-compatible interface across Bedrock / Vertex / Azure / OpenAI; model catalog, alias routing, embeddings, Responses, usage logs, analytics |
| `identity-proxy` sidecar | Injects platform tokens on outbound calls; agents never see credentials |
| `orbit-agents` + `session-manager` + sandbox stack | Runs and isolates agent workloads (Claude Code today; CLI-agnostic) |
| Operator + Helm/GitOps | Reconciles tenants, mesh, and Orbit services |

> **Designed but not yet built.** Three pieces are in design and have ADRs but their feature packages are still empty stubs: a **general credential broker** (beyond the Orbit-platform-token broker that the `identity-proxy` already provides), a **governed MCP/tool gateway** (the existing `orbit-runtime-mcp` is single-purpose for sandbox provisioning), and a **platform-wide event spine** (agent run events stream inside `orbit-agents` today). See [`internal/credentials/`, `internal/toolgateway/`, `internal/events/`](code-organization.md#feature-packages-backendinternal).

## The technical posture

Orbit is established platform engineering applied to a new domain. The hard problems aren't new — the workloads are. We use industry-standard tools rather than invented primitives:

- **Identity** — OIDC for humans (any standards-compliant provider), SPIFFE/SPIRE for workloads, JWT-based platform tokens.
- **Policy** — Open Policy Agent (OPA) with bundles distributed by `policy-service`; PDP/PAP/PEP split.
- **Mesh & edge** — Istio + Envoy sidecars; the `identity-proxy` plugs in via Envoy `ext_proc`.
- **Deployment** — Kubernetes-native, reconciled by an operator; Helm charts under GitOps.
- **Observability** — OpenTelemetry traces and metrics across every service.

Every agent action traces back to the human who initiated it and the policy that authorised it. The "how do we know the agent isn't going rogue?" question gets a concrete answer.

## Runtime topology

The backend is a **multi-service control plane** ([multi-service-backend-deployment-structure](adr/2026-03-19-multi-service-backend-deployment-structure.md)) — 13 binaries sharing one platform layer, deployed to Kubernetes by the Orbit operator. Two planes:

- **Control plane (platform namespace):** the always-on Orbit services — `identity`, `inference-gateway`, `policy-service`, `policy-gateway`, `orbit-agents`, and the `operator`. Backed by PostgreSQL and Redis.
- **Tenant data plane (per-tenant namespaces):** agent workloads and their `session-manager`, each pod fronted by an Istio Envoy sidecar plus the Orbit `identity-proxy` sidecar that injects platform tokens. SPIRE attests workloads; the operator reconciles each tenant namespace.

```
   Humans                                    Agent workloads (tenant namespaces)
     │                                                   │
     ▼                                                   ▼
 ┌─────────┐  BFF/session                       ┌──────────────────┐
 │ Frontend │ ───────────────┐                  │  Agent pod        │
 │ (Next.js)│                │                  │  ┌─────────────┐  │
 └─────────┘                 ▼                  │  │ identity-proxy│ ext_proc: SVID→platform token
                       ┌────────────┐           │  └─────┬───────┘  │
                       │  identity  │◄──────────┼────────┘ injects Bearer token on egress
                       │  :8080 API │  sessions │  ┌─────────────┐  │
                       └─────┬──────┘  PATs     │  │session-manager│ runs Claude Code
        ┌────────────────────┼───────────┐     │  └─────┬───────┘  │
        ▼                    ▼            ▼     └────────┼──────────┘
 ┌──────────────┐   ┌──────────────┐ ┌──────────┐       │ provision_sandbox (MCP) / shell-proxy
 │ inference-   │   │ policy-      │ │ orbit-   │◄───────┘
 │ gateway :8081│   │ gateway :8084│ │ agents   │        ┌──────────────┐
 └──────┬───────┘   └──────┬───────┘ │ :8080    │        │ sandbox pod  │
        │ providers        │ PEP     └────┬─────┘        │ sandbox-exec │
        ▼                  ▼ PDP/bundles  │              │ :50051 gRPC  │
  Model providers   ┌──────────────┐      ▼              └──────────────┘
  (Bedrock/Vertex/  │policy-service│  Postgres / Redis
   Azure/OpenAI)    │ :8083 + OPA  │
                    └──────────────┘
        operator (reconciles tenant/platform CRDs) · SPIRE (SVIDs) · Istio (mTLS mesh)
```

### How requests flow

- **A human** signs in through the frontend, which runs a Backend-for-Frontend (BFF) that federates to an OIDC provider (Dex locally) and exchanges the result for an Orbit session against the `identity` service ([browser-auth-flow-and-bff-session-architecture](adr/2026-04-01-browser-auth-flow-and-bff-session-architecture.md)).
- **An agent** in a tenant pod makes an outbound call; its Istio sidecar routes it through the Orbit `identity-proxy`, which exchanges the pod's SPIFFE SVID for an Orbit platform token and injects it as a `Bearer` header ([envoy-ext-proc-sidecar-token-injection](adr/2026-03-21-envoy-ext-proc-sidecar-token-injection.md)). The agent never handles credentials.
- **An inference call** hits the `inference-gateway`, which authenticates the platform token, checks policy, resolves the model alias, routes to the right provider, and records usage ([inference-gateway-architecture](adr/2026-04-01-inference-gateway-architecture.md)).
- **Authorization** is enforced before action: services call the `policy-gateway` (PEP) which evaluates against OPA bundles compiled and served by `policy-service` (PDP/PAP) ([policy-gateway-and-authorisation-model](adr/2026-04-14-policy-gateway-and-authorisation-model.md)).
- **An agent run** is orchestrated by `orbit-agents`, which manages run/session lifecycle and drives `session-manager` to run Claude Code in an isolated workspace; the agent provisions a sandbox via `orbit-runtime-mcp` and runs shell commands through `orbit-shell` into a `sandbox-exec` pod ([run-and-session-lifecycle](adr/2026-05-11-run-and-session-lifecycle.md), [sandbox-provisioning-and-shell-proxy](adr/2026-05-11-sandbox-provisioning-and-shell-proxy.md)).

## Capabilities in detail

### Identity & Authentication — ✅ built
Establishes verified identity for humans and agents. Human auth via any OIDC provider (Entra, Okta, Google Workspace, Keycloak; Dex locally) including the Device Authorization Grant for headless CLIs; agent workload identity via SPIFFE SVIDs exchanged for Orbit platform tokens; sessions with JWT issuance, refresh, and revocation (Redis JTI blocklist); PATs for programmatic access; a multi-tenant registry binding tenants to OIDC providers. See the [Identity, Auth & Tokens ADRs](adr/README.md#identity-auth--tokens).

### Policy Enforcement — ✅ built
Governs what principals can do **before** they act. A PDP/PAP (`policy-service`) owns grants, roles, and a route-scope vocabulary, and compiles OPA bundles; a PEP (`policy-gateway`) answers `check`/`query`/`explain`/`simulate` for owned services; gateway-level OPA enforces external traffic ([gateway-level-opa-enforcement-for-external-traffic](adr/2026-04-27-gateway-level-opa-enforcement-for-external-traffic.md)); platform middleware enforces route scopes ([platform-scope-enforcement-middleware](adr/2026-06-04-platform-scope-enforcement-middleware.md)). A policy administration dashboard ships in the frontend.

### Inference Gateway — ✅ built
An OpenAI/Anthropic-compatible interface between agents and model providers (AWS Bedrock, Google Vertex, Azure, OpenAI) under a bring-your-own-provider model. Provides a model catalog with capability taxonomy and alias/virtual-model routing, an Anthropic zero-translation path, embeddings and Responses endpoints, plus usage logging and analytics. See the [Inference Gateway ADRs](adr/README.md#inference-gateway).

### Agent Run Control — ✅ built
`orbit-agents` is the control plane for agent types, runs, and sessions, with event streaming, BYOC discovery, and per-tenant Kubernetes orchestration. `session-manager` runs the agent (Claude Code) per session in an isolated workspace with per-session home isolation; the sandbox stack (`orbit-runtime-mcp`, `orbit-shell`, `sandbox-exec`, `orbit-session-runner`) provides provisioned, Landlock/namespace-isolated command execution. See the [Agents Control Plane ADRs](adr/README.md#agents-control-plane--execution).

### Observability — ✅ built
OpenTelemetry traces and metrics across every service, W3C trace propagation through the typed platform HTTP clients, and log–trace correlation via the injected structured logger ([opentelemetry-observability-discipline](adr/2026-03-21-opentelemetry-observability-discipline.md)). See [docs/observability/](observability/README.md).

### Credential Broker — ⛔ designed, not yet built
Scoped, just-in-time credential provisioning so agents never see raw secrets. Designed in [credential-aware-outbound-gateway](adr/2026-03-05-credential-aware-outbound-gateway.md); `internal/credentials` is an empty stub. Today the `identity-proxy` brokers Orbit *platform* tokens for service-to-service calls, but general external-credential brokering is future work.

### Tool / MCP Gateway — ⛔ designed, not yet built
A governed broker for tool and MCP access with permission enforcement. Designed in [mcp-server-registration-deployment-and-governance](adr/2026-03-05-mcp-server-registration-deployment-and-governance.md) and [scope-gated-tool-exposure-via-mcp](adr/2026-03-05-scope-gated-tool-exposure-via-mcp.md); `internal/toolgateway` is an empty stub. `orbit-runtime-mcp` exists but is a single-purpose MCP server for sandbox provisioning, not the governed gateway.

### Event Spine — ⛔ designed, not yet built
A platform-wide structured event bus for awareness and audit. `internal/events` is an empty stub. Agent run events are currently emitted and streamed within `orbit-agents` ([agent-event-streaming-protocol](adr/2026-05-11-agent-event-streaming-protocol.md)); a general spine is future work.

## Technical stack

| Concern | Technology | Status |
|---------|-----------|--------|
| Language | Go 1.26 (backend), TypeScript / Next.js (frontend) | ✅ |
| HTTP layer | Custom controller-based router (`internal/platform/api`) — [controller-based-http-routing](adr/2026-02-17-controller-based-http-routing.md) | ✅ |
| Identity / Auth | `go-oidc/v3` + `golang.org/x/oauth2`, provider-agnostic OIDC | ✅ |
| Workload identity | SPIFFE/SPIRE SVIDs exchanged for platform tokens — [spiffe-spire-workload-attestation](adr/2026-03-05-spiffe-spire-workload-attestation.md) | ✅ |
| Token injection sidecar | Envoy `ext_proc` gRPC sidecar (`cmd/identity-proxy`) — [envoy-ext-proc-sidecar-token-injection](adr/2026-03-21-envoy-ext-proc-sidecar-token-injection.md) | ✅ |
| Service-to-service | Typed HTTP clients (`internal/platform/clients`) with OTel trace propagation | ✅ |
| Policy engine | Open Policy Agent (OPA) — PDP/PAP + PEP split | ✅ |
| Inference | Multi-provider gateway (Bedrock / Vertex / Azure / OpenAI) | ✅ |
| Agent execution | `orbit-agents` + `session-manager` + sandbox stack on Kubernetes | ✅ |
| Primary storage | PostgreSQL via `pgx/v5` (no ORM) — [database-infrastructure-layer](adr/2026-03-03-database-infrastructure-layer.md) | ✅ |
| Cache / revocation | Redis via `go-redis/v9` | ✅ |
| Service mesh | Istio + Envoy sidecars — [service-mesh-and-network-enforcement-infrastructure](adr/2026-03-05-service-mesh-and-network-enforcement-infrastructure.md) | ✅ |
| Deployment | Kubernetes operator + Helm/GitOps — [orbit-kubernetes-operator](adr/2026-03-31-orbit-kubernetes-operator.md), [helm-chart-distribution-and-gitops-repository-structure](adr/2026-03-31-helm-chart-distribution-and-gitops-repository-structure.md) | ✅ |
| Observability | OpenTelemetry (traces + metrics), OTel HTTP semantic conventions | ✅ |
| Configuration | Per-service YAML via `config.LoadService[T]` + `base.yaml`; secrets via `os.Getenv` — [yaml-file-based-configuration](adr/2026-02-18-yaml-file-based-configuration.md) | ✅ |
| Static analysis | `golangci-lint` with custom `ruleguard` + `depguard` rules enforcing ADRs | ✅ |
| Local dev | Docker Compose (Postgres/Redis/Dex/glauth) + optional k3d cluster (Istio + SPIRE) | ✅ |
| Credential broker / tool gateway / event spine | — | ⛔ designed, not built |

## Design principles

1. **Security first** — every component designed with security as the primary concern.
2. **Identity-driven** — all actions tied to a verified identity.
3. **Policy-governed** — policy enforced *before* execution, not after.
4. **Observable** — distributed tracing, structured logging, log–trace correlation throughout.
5. **Extensible** — pluggable model providers, tools, and identity providers.
6. **Production-grade** — built for enterprise reliability and scale.
7. **Open standards** — OPA for policy, OIDC/SPIFFE for identity, MCP for tools, OpenTelemetry for observability.

## Where to go next

- [code-organization.md](code-organization.md) — the directory layout, binaries, and feature packages
- [adr/README.md](adr/README.md) — every architectural decision, by subsystem
- [guides/local-development.md](guides/local-development.md) — run it locally
- [CONTRIBUTING.md](../CONTRIBUTING.md) — how to contribute

## License

Orbit is licensed under the Apache License 2.0. See [LICENSE](../LICENSE).
