# Orbit Documentation

Orbit is the **control plane for AI agents**: the identity, policy, and coordination infrastructure for running autonomous agents safely at scale. This is the documentation home; start here.

## Start here

New to the repo? Read these in order:

1. **[guides/day-one.md](guides/day-one.md)**: clone to a running change, backend and frontend
2. **[../README.md](../README.md)**: what Orbit is, and the quick start
3. **[architecture.md](architecture.md)**: the runtime topology and what's built vs designed
4. **[code-organization.md](code-organization.md)**: how the backend is laid out (binaries, feature packages, conventions)
5. **[guides/backend-golden-path.md](guides/backend-golden-path.md)**: add a backend feature end to end
6. **[guides/local-development.md](guides/local-development.md)**: the full local environment reference
7. **[../CONTRIBUTING.md](../CONTRIBUTING.md)**: the PRD → ADR → implement workflow and the quality gate
8. **[../frontend/README.md](../frontend/README.md)**: run and develop the Next.js frontend

## Map

| Area | Docs |
|------|------|
| **Architecture & design** | [architecture.md](architecture.md) · [code-organization.md](code-organization.md) · [spiffe.md](spiffe.md) (SPIFFE/SPIRE in the mesh) |
| **Decisions** | [Architecture Decision Records](adr/README.md) (the *why* behind everything) · [Product Requirements](prd/README.md) |
| **Guides** | [Day one](guides/day-one.md) · [Backend golden path](guides/backend-golden-path.md) · [Local development](guides/local-development.md) · [Policy & roles](guides/policy-and-roles.md) · [Frontend testing](guides/frontend-testing.md) |
| **Configuration** | [Config system](configuration/config.md) · [Secrets](configuration/secrets.md) |
| **Observability** | [Overview](observability/README.md) · [Logging](observability/logging.md) · [Tracing](observability/tracing.md) · [Configuration](observability/configuration.md) |
| **Frontend** | [frontend/README.md](../frontend/README.md) · [Terra & component docs](../frontend/docs/README.md) |

## By subsystem

Each subsystem is described in [architecture.md](architecture.md); the design history lives in the ADR index.

| Subsystem | What it is | Start with |
|-----------|------------|------------|
| **Identity & Auth** | OIDC federation, SPIFFE exchange, sessions, PATs, tenant registry | [architecture.md](architecture.md#the-pillars-in-detail) · [ADRs](adr/README.md#identity-auth--tokens) |
| **Policy & Authorization** | OPA-based PDP/PAP + PEP, route scopes, admin dashboard | [guides/policy-and-roles.md](guides/policy-and-roles.md) · [ADRs](adr/README.md#policy--authorization) |
| **Inference Gateway** | Multi-provider, OpenAI/Anthropic-compatible LLM gateway | [Inference Gateway PRD](prd/inference-gateway.md) · [ADRs](adr/README.md#inference-gateway) |
| **Agents Control Plane** | Agent runs, sessions, sandboxing, event streaming | [Agent Control Plane PRD](prd/agent-control-plane.md) · [ADRs](adr/README.md#agents-control-plane--execution) |
| **Operator & Deployment** | Kubernetes operator, Helm/GitOps, service mesh | [ADRs](adr/README.md#service-mesh-operator--deployment) |
| **Frontend** | Next.js app, BFF, Terra design system | [frontend/README.md](../frontend/README.md) · [ADRs](adr/README.md#frontend--design-system) |

## Conventions

- **ADRs** are named `YYYY-MM-DD-kebab-slug.md` (decision date + slug, no sequential number) and cited by slug; see the [ADR index](adr/README.md) and [`adr/template.md`](adr/template.md).
- **PRDs** precede ADRs for new features; see the [PRD index](prd/README.md) and [`prd/template.md`](prd/template.md).
- The full set of code conventions (enforced by lint) lives in [../AGENTS.md](../AGENTS.md).

## Archive

Superseded and point-in-time working documents (retired PRDs, old design scratch, shipped implementation plans) live in [archive/](archive/), kept for history, not current guidance.
