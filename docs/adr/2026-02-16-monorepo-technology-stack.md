# Monorepo Technology Stack

**Status:** Accepted
**Date:** 2026-02-16
**Deciders:** Architecture Team
**Technical Story:** Initial technology stack selection for Orbit control plane

---

## Context

Orbit is a control plane for autonomous agents, designed as a "nervous system" that coordinates distributed AI agents through identity management, policy enforcement, and event streaming. The system requires both a robust backend API (for agents to interact with) and a web-based UI (for human operators to monitor and control the system).

### Background

As a greenfield project, we need to establish the foundational technology choices that will guide development for the foreseeable future. The system will be deployed as a control plane, requiring high reliability, clear security boundaries, and good developer experience.

### Problem Statement

We need to select technologies for:
1. Backend API services (core control plane)
2. Frontend web application (operator dashboard)
3. Backend-for-Frontend (BFF) layer (security boundary between browser and API)
4. Monorepo structure to house all components

### Goals

- Fast development iteration cycles
- Clear security boundaries (session cookies → API keys)
- Type safety across the stack
- Simple deployment and development setup
- Good performance for a control plane workload
- Maintainability by a small team

### Non-Goals

- Supporting multiple frontend applications initially
- Microservices architecture with multiple backend services
- Complex build orchestration or caching systems
- Supporting mobile native applications

---

## Decision Drivers

* **Security**: Need proper credential translation (browser cookies → API keys)
* **Developer Experience**: Simple local development setup, hot reload, minimal context switching
* **Performance**: Control plane workload requires efficient backend processing
* **Type Safety**: Reduce runtime errors through compile-time checks
* **Simplicity**: Minimize number of services and complexity during initial development
* **Team Velocity**: Enable fast iteration without over-engineering

---

## Considered Options

### Option 1: Go Backend + Separate Go BFF + Next.js Frontend

**Description:** Three separate services - Go API, Go BFF, Next.js UI

**Pros:**
- Language consistency between backend and BFF (Go)
- Clean separation of concerns
- Maximum performance for BFF layer
- Can share Go types/packages between services

**Cons:**
- Three separate services to run in development
- More complex deployment
- Slower iteration (rebuild Go for BFF changes)
- More configuration and coordination needed

### Option 2: Go Backend + Next.js with API Routes (BFF) + React UI

**Description:** Two-tier architecture - Go backend, Next.js serving UI and acting as BFF via API routes

**Pros:**
- Simple development (single `npm run dev` for frontend stack)
- Fast iteration on BFF logic (no Go rebuild)
- TypeScript end-to-end in frontend stack
- Standard Next.js pattern
- One fewer service to deploy/maintain
- Hot reload for both UI and BFF code

**Cons:**
- Mixed language stack (Go backend, TypeScript BFF)
- Slightly lower performance for BFF (acceptable for control plane UI)
- Cannot share types directly between Go backend and TypeScript BFF

### Option 3: Turborepo Monorepo with Multiple Services

**Description:** Use Turborepo to orchestrate builds across Go backend, separate BFF service, and frontend

**Pros:**
- Advanced caching and task orchestration
- Better suited for multiple frontend apps
- Remote build cache sharing

**Cons:**
- Significant tooling overhead for current needs
- More complexity than needed for single backend + single frontend
- Go already has excellent built-in build caching
- Adds Node.js tooling to manage Go builds

---

## Decision

**Chosen Option:** Option 2 - Go Backend + Next.js with API Routes (BFF) + React UI

### Rationale

We selected this option because:

1. **Simplicity wins at this stage**: Running a single backend service and a single Next.js dev server is far simpler than coordinating three services. This reduces cognitive load and speeds up onboarding.

2. **Next.js API routes are purpose-built for this**: The BFF pattern (cookie → API key translation, session management, response shaping) is exactly what Next.js API routes excel at. We get a secure, server-side execution environment for free.

3. **Performance is sufficient**: While a Go BFF would be faster, the control plane UI is not a high-throughput system. TypeScript API routes provide more than adequate performance for operator dashboards.

4. **Developer experience matters**: Hot reload for both UI and BFF logic means faster iteration cycles. Modifying BFF logic doesn't require waiting for Go compilation.

5. **We can always extract later**: If the BFF layer becomes a bottleneck or we need to share logic with the Go backend, we can extract it into a separate Go service. Starting simple doesn't prevent this evolution.

6. **Type safety where it matters**: TypeScript provides excellent type safety within the frontend stack (UI ↔ BFF). The boundary between BFF and Go backend is an API contract anyway, where types are enforced by the API schema.

---

## Consequences

### Positive

- **Faster development velocity**: Single command to start frontend stack, instant feedback on changes
- **Lower operational complexity**: Fewer services to deploy, monitor, and maintain
- **Proven pattern**: Next.js API routes + backend API is a well-established architecture
- **Clear security model**: Session cookies never leave the browser, API keys never reach the browser
- **Good TypeScript ecosystem**: Rich libraries for auth, validation, and API clients in the frontend stack

### Negative

- **Language fragmentation**: Team needs expertise in both Go and TypeScript
- **No type sharing**: Cannot directly share type definitions between Go backend and TypeScript BFF
- **Potential performance ceiling**: If BFF becomes compute-heavy, TypeScript may become a bottleneck
- **Framework lock-in**: Tied to Next.js framework conventions and lifecycle

### Neutral

- **Two separate build pipelines**: Go backend and Next.js frontend built independently
- **Different testing strategies**: Go tests for backend, Jest/React Testing Library for frontend
- **Separate dependency management**: go.mod for backend, package.json for frontend

---

## Implementation

### Structure

```
/
├── backend/              # Go services (control plane)
│   ├── cmd/
│   │   └── orbit/        # Main control plane API entrypoint
│   ├── internal/         # Feature packages (identity, agents, etc.)
│   │   └── platform/     # Shared infrastructure (logging, storage, api, observability)
│   ├── go.mod
│   └── go.sum
├── docs/                 # ADRs, PRDs, and architecture documentation
│   ├── adr/
│   └── prd/
├── infrastructure/       # Declarative Kubernetes manifests (Kustomize)
│   ├── base/             # Shared base resources (SPIRE, NetworkPolicy, namespaces)
│   ├── overlays/         # Environment-specific overlays (local, staging, prod)
│   └── services/         # Per-service Kustomize overlays (identity, ext-authz, outbound-gateway)
├── scripts/              # Operational scripts (not manifests)
│   └── cluster/          # k3d cluster lifecycle (dev-cluster-up.sh, dev-cluster-down.sh)
├── docker-compose.yml    # Tier 1: Postgres + Redis for daily Go development
└── Makefile              # Root-level dev commands (dev-up, dev-cluster, test, etc.)
```

### Migration Path

N/A - This is the initial architecture.

### Timeline

- **Phase 1 (Week 1)**: Scaffold Go backend and Next.js frontend
- **Phase 2 (Week 2-3)**: Implement first BFF endpoints and API integration
- **Phase 3 (Ongoing)**: Iterate on features

---

## Validation

### Success Metrics

- **Development velocity**: Time from idea to deployed feature remains under 1 day for typical UI features
- **Build time**: Frontend rebuild + backend rebuild completes in under 2 minutes
- **Developer onboarding**: New developer can run full stack locally in under 15 minutes
- **API latency**: BFF → Backend API calls complete in under 100ms (p95)

### Monitoring

- Track build times in CI/CD
- Monitor API response times from BFF to backend
- Gather developer feedback on iteration speed and friction points
- Watch for performance bottlenecks in TypeScript BFF layer

---

## Related Decisions

- Future ADR: API schema and contract definition (OpenAPI/GraphQL)
- Future ADR: Authentication and session management implementation

---

## References

- [Next.js API Routes Documentation](https://nextjs.org/docs/app/building-your-application/routing/route-handlers)
- [Backend for Frontend Pattern](https://samnewman.io/patterns/architectural/bff/)

---

## Notes

This decision prioritizes developer experience and simplicity over theoretical performance optimization. We can revisit if:
- BFF layer shows performance problems under load
- We need to add multiple frontend applications
- We want to share significant business logic between BFF and backend

**Last Updated:** 2026-02-16