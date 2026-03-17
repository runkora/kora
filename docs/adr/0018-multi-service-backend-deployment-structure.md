# 0018. Multi-Service Backend Deployment Structure

**Status:** Accepted
**Date:** 2026-03-10
**Deciders:** Engineering Team
**Technical Story:** The enforcement architecture defined in ADRs 0010, 0011, and 0015 requires each platform service to run as a separate Kubernetes workload with its own SPIFFE identity, Istio sidecar, and Cilium network policy. The current single-binary backend cannot satisfy this requirement. This ADR defines how the Go monorepo is restructured to produce multiple independently deployable binaries without abandoning the monorepo model.

---

## Context

### Background

ADR-0001 listed "Microservices architecture with multiple backend services" as a non-goal. That decision was correct at the time — it ruled out the premature adoption of microservices as an architectural style: separate repos, independent release cycles, network-first design for all internal calls, per-service operational overhead before any of the core platform is built.

However, the enforcement architecture has since been fully specified (ADRs 0010, 0011, 0015), and it has a structural consequence that ADR-0001 did not anticipate:

- ADR-0010 assigns a distinct SPIFFE ID to each platform service (`spiffe://orbit.local/system/identity`, `spiffe://orbit.local/system/ext-authz`, `spiffe://orbit.local/system/outbound-gateway`). A SPIFFE ID is a workload identity — it is assigned to a Kubernetes pod, not to a process within a pod.
- ADR-0011 places an Envoy sidecar on every service pod. The sidecar performs mTLS using the pod's SVID and enforces `ext_authz` policy on every inbound request. A single pod running all services cannot have independent per-service sidecars.
- ADR-0015 uses Istio's per-namespace sidecar injection and Cilium's pod-level eBPF network policy. Both operate at the pod boundary. A single all-features pod cannot be addressed by these controls at the per-service granularity that the security model requires.

The existing `infrastructure/services/` directory already reflects the required deployment topology: separate Kustomize trees for `identity/`, `ext-authz/`, and `outbound-gateway/`, each with their own `ServiceAccount` and `ClusterSPIFFEID`. The backend code structure does not yet match.

This ADR does not reverse the non-goal in ADR-0001. We are not adopting microservices. We are adopting a **modular monorepo with multiple deployment units** — a single Go module, a single repository, shared platform libraries, but multiple binary entrypoints that can be deployed independently as Kubernetes workloads. The distinction matters: the development model remains monorepo; only the deployment model is multi-binary.

### Problem Statement

We need to define:

1. The `cmd/` directory structure for multiple service binaries within the single Go module
2. How database migrations are organised and run per service, both locally and in production
3. How service-to-service HTTP calls are handled when features that were previously in-process become separate network services
4. How configuration is shared vs differentiated across binaries
5. How the Makefile and local development tooling adapt to multiple binaries
6. The migration path from the current single-binary `cmd/orbit/` structure

### Goals

- Each platform service is independently deployable as a Kubernetes workload with its own pod identity, Istio sidecar, and Cilium network policy
- The Go codebase remains a single module (`backend/go.mod`) — no module splitting, no import path changes to `internal/` packages
- Feature packages in `internal/` are unchanged in structure — service isolation is expressed at the `cmd/` entrypoint level, not by fragmenting the internal package graph
- Schema isolation: each service owns and manages the migrations for its own database tables
- Local development remains a single `make migrate-up` command regardless of how many services exist
- Service-to-service calls are made through typed, versioned HTTP clients — no naked `http.Get` calls between services

### Non-Goals

- Splitting the Go monorepo into multiple modules or repositories
- Adopting microservices patterns (per-service CI pipelines, independent versioning, service meshes as the primary development model)
- Running multiple services as separate OS processes on a single local developer machine — locally, a single binary can still be used for the fast feedback tier (Tier 1)
- Defining the production Kubernetes deployment manifests (Helm charts, ArgoCD apps) — those are infrastructure concerns addressed separately

---

## Decision Drivers

* **SPIFFE identity requirements**: ADR-0010 assigns per-service SPIFFE IDs — these are pod-level identities and require separate pod deployments
* **Istio sidecar enforcement**: ADR-0011/0015 place Envoy sidecars at the pod boundary for per-service mTLS and ext_authz — this cannot work with a single all-features pod
* **Cilium network policy**: ADR-0015 enforces egress and intra-service network rules at the pod level per service identity
* **Schema isolation (SRP)**: Each service should own and evolve its own database tables without coupling to another service's schema changes
* **Monorepo cohesion**: Shared platform code (`internal/platform/`) must not be duplicated — all binaries consume it from the same module
* **Developer experience**: Local development must not require running five separate binaries for every Go code change — the fast feedback loop must be preserved

---

## Considered Options

### Option A: Single binary, runtime mode flag

**Description:** Keep `cmd/orbit/main.go` as a single binary. Add a `--mode` flag (`--mode=identity`, `--mode=ext-authz`, etc.). In each mode, only the relevant feature controllers are registered. The same binary is deployed multiple times in k8s with different flags.

**Pros:**
- Minimal structural change — one `main.go`, one Docker image
- All features remain wired in the same file

**Cons:**
- A single Docker image means a single build artifact — deploying just the identity service requires re-deploying all services (they share the image tag)
- Binary contains all feature code regardless of mode — a vulnerability in agents code is present in the identity service image
- Flag-based mode selection is not idiomatic Go and is not how the Go ecosystem structures multi-binary projects
- Does not model the actual deployment reality — misleading to engineers reading the code
- SPIFFE/Istio tooling selects workloads by label selectors on pods, not by internal process flags

### Option B: Multiple binaries, single Go module (multiple `cmd/` entrypoints)

**Description:** Each deployable service has its own directory under `backend/cmd/` with its own `main.go`. All binaries share the single `backend/go.mod` module and consume `internal/platform/` and their respective `internal/{feature}/` packages. Each binary produces a separate Docker image.

**Pros:**
- Clean mapping from "service" to "binary" to "Docker image" to "k8s Deployment" — the deployment model is legible from the code structure
- Feature packages consumed by a binary are explicit — `cmd/identity/main.go` imports `internal/identity/`, not `internal/agents/`
- Independent Docker images — each service image contains only the code it needs
- Migration files are co-located with the service entrypoint (`cmd/identity/migrations/`)
- Fully idiomatic Go — the Go toolchain natively supports multiple `main` packages in a single module
- Infrastructure (`infrastructure/services/identity/`) and code (`cmd/identity/`) are structurally aligned

**Cons:**
- `make build` must build multiple binaries — slightly more complex Makefile
- Local Tier 1 development (fast feedback, no cluster) needs a decision on whether to run one binary or many
- Service-to-service calls that were previously in-process function calls become network calls requiring typed HTTP clients

### Option C: Go workspace with per-service modules

**Description:** Use Go workspaces (`go.work`) to split each service into its own Go module under `backend/`. Each module has its own `go.mod` and can be built and versioned independently.

**Pros:**
- True module isolation — services cannot accidentally import each other's internal packages
- Independent `go.mod` per service enables per-service dependency management

**Cons:**
- `internal/platform/` would need to be a separate module published somewhere (local replace directives or a private module registry) — significant operational complexity
- Go workspaces add friction to cross-cutting changes that touch the platform layer and a feature simultaneously
- Import paths become inconsistent — engineers must track which module a package lives in
- No clear benefit over Option B given that `internal/` package visibility already enforces the import boundaries we need

---

## Decision

**Chosen Option:** Option B — Multiple binaries, single Go module (`backend/go.mod`)

### Rationale

The Go module system's `internal/` visibility rules are sufficient to enforce the package boundaries that matter. A Go workspace is solving a problem we do not have. The mode-flag approach (Option A) papers over the deployment reality rather than modelling it.

Option B produces code structure that directly mirrors the infrastructure structure already established in `infrastructure/services/`. When an engineer looks at `infrastructure/services/identity/base/spiffe-id.yaml` and `backend/cmd/identity/main.go`, the relationship is unambiguous. This alignment reduces cognitive overhead and makes the system legible.

---

## Structure

### `cmd/` Directory Layout

```
backend/
├── cmd/
│   ├── identity/
│   │   ├── main.go          # wires: internal/identity only
│   │   ├── config/          # identity service YAML config (APP_ENV-keyed)
│   │   └── migrations/      # SQL migrations owned by the identity service
│   │       ├── 20260218215737_create_sessions.sql
│   │       └── 20260219100000_add_refresh_tokens.sql
│   │
│   ├── ext-authz/
│   │   ├── main.go          # wires: internal/policy only
│   │   ├── config/
│   │   └── migrations/      # SQL migrations owned by ext-authz (if any)
│   │
│   ├── gateway/
│   │   ├── main.go          # wires: internal/credentials only
│   │   ├── config/
│   │   └── migrations/      # SQL migrations owned by the outbound gateway
│   │
│   └── (future: agents/, run-manager/, mcp-gateway/)
│
├── internal/
│   ├── platform/            # unchanged — consumed by all binaries
│   │   ├── api/
│   │   ├── clients/         # NEW: typed HTTP clients for service-to-service calls
│   │   ├── config/
│   │   ├── logging/
│   │   ├── middleware/
│   │   ├── observability/
│   │   └── storage/
│   ├── identity/            # consumed only by cmd/identity
│   ├── policy/              # consumed only by cmd/ext-authz
│   ├── credentials/         # consumed only by cmd/gateway
│   ├── agents/              # consumed only by cmd/agents (future)
│   ├── runs/                # consumed only by cmd/run-manager (future)
│   └── ...
```

### Service-to-Binary Mapping

| k8s Service | Binary entrypoint | SPIFFE ID (from ADR-0010) | Internal feature package |
|---|---|---|---|
| identity | `cmd/identity/` | `spiffe://orbit.local/system/identity` | `internal/identity/` |
| ext-authz | `cmd/ext-authz/` | `spiffe://orbit.local/system/ext-authz` | `internal/policy/` |
| outbound-gateway | `cmd/gateway/` | `spiffe://orbit.local/system/outbound-gateway` | `internal/credentials/` |
| agents (future) | `cmd/agents/` | `spiffe://orbit.local/system/agents` | `internal/agents/` |
| run-manager (future) | `cmd/run-manager/` | `spiffe://orbit.local/system/run-manager` | `internal/runs/` |

### Migrations: Per-Service Ownership

Each service owns the SQL migration files for the database tables it manages. Migration files live under `cmd/{service}/migrations/` and follow the existing Goose timestamp-based naming convention.

**Why per-service, not centralised:**
- Schema changes to the identity service do not require a migration file in the same directory as credentials schema changes — reducing merge conflicts and ownership ambiguity
- SRP applied to the database layer: the identity team reasons about identity schema; the credentials team reasons about credentials schema
- Goose's timestamp-based ordering is global — files from all services are sortable by timestamp regardless of which directory they originate from

**Local development — migration aggregation:**

The Makefile `migrate-up` target aggregates all service migration files at runtime into a single temporary directory and passes that to Goose. Goose applies them in timestamp order. This is idempotent — subsequent runs are no-ops for already-applied versions.

```makefile
migrate-up:
    @TMP_MIGRATIONS=$$(mktemp -d) && \
    find backend/cmd -path "*/migrations/*.sql" -exec cp {} $$TMP_MIGRATIONS \; && \
    goose -dir $$TMP_MIGRATIONS postgres "$(DATABASE_URL)" up && \
    rm -rf $$TMP_MIGRATIONS
```

**Production — dedicated migration Job (planned, not yet implemented):**

In the k8s deployment, migrations are run by a dedicated `cmd/migrate/` binary executed as a k8s `Job` before any service `Deployment` rolls out. This Job embeds all migration files at build time via `go:embed`, runs Goose, and exits. The service Deployments proceed only after the Job completes successfully. This pattern is deferred until the first Helm chart is written — the per-service migration directory structure established here is fully compatible with this approach and requires no structural change when the migration Job is introduced.

### Service-to-Service Communication

Services that need to call other services use typed HTTP clients defined in `internal/platform/clients/`. This package is consumed by feature packages that need cross-service data — it is part of the platform layer and follows the existing dependency rule (features import platform; platform does not import features).

```
internal/platform/clients/
├── identity/
│   └── client.go    # typed client for identity service HTTP API
└── (future: agents/, policy/, etc.)
```

Each client is:
- A thin wrapper around `net/http` with typed request/response structs — no third-party HTTP client framework
- Injected into feature services that need it (constructor injection, consistent with the existing pattern)
- Configurable via the standard config system (base URL, timeout, retry settings from YAML/env)

In Tier 1 local development (single binary, no cluster), service-to-service calls that cross service boundaries are not exercised. Integration of the full call chain is a Tier 2 concern (local k8s cluster per ADR-0016).

### Configuration

All services share the same YAML config file structure, loaded via `config.Load[T]()` keyed by `APP_ENV`. Differences between deployed services are expressed via environment variables only — not separate YAML files per service. This is consistent with ADR-0006 and avoids maintaining per-service config trees that are largely identical.

Each service's `cmd/{service}/config/` directory contains the standard environment-keyed YAML files (`local.yaml`, `production.yaml`, etc.) for any config values that are genuinely service-specific. Shared infrastructure config (Postgres DSN, Redis address, observability endpoint) lives in environment variables and is not duplicated per service.

---

## Consequences

### Positive

- **Deployment model is legible from the codebase** — `cmd/identity/` maps directly to the identity k8s Deployment, its SPIFFE ID, its Istio sidecar, and its Cilium network policy
- **Schema isolation** — each service's migrations are co-located with the service code; changes to identity schema do not touch credentials migration files
- **Independent Docker images** — each service image contains only its binary and migrations; an unrelated feature's code is not present in the image
- **Correct security surface** — a vulnerability in the agents feature package does not affect the identity service image because the identity binary does not import `internal/agents/`
- **Infrastructure and code are aligned** — `infrastructure/services/identity/` and `backend/cmd/identity/` are structurally parallel; onboarding engineers see consistent naming across both layers

### Negative

- **`make build` produces multiple binaries** — the Makefile must enumerate service targets; a `build-all` target is required
- **Service-to-service calls are now network calls** — what was previously a function call between packages is now an HTTP request with latency, retries, and potential failure modes; this complexity must be handled in the typed client layer
- **Local Tier 1 development is more nuanced** — for the fast feedback loop, engineers typically work on one service at a time; they start only that service's binary locally, meaning cross-service integration is not exercised in Tier 1

### Neutral

- The `cmd/orbit/` entrypoint is retired; `cmd/identity/` is the first replacement. Existing `make build` references must be updated
- The `internal/example/` package (currently wired into `cmd/orbit/main.go`) is removed or migrated to a dedicated example entrypoint; it does not belong in a production service binary
- Future services follow the same pattern: create `cmd/{service}/`, wire only the relevant `internal/{feature}/` package, add a `migrations/` subdirectory if the service owns schema

---

## Implementation

### Migration Path from `cmd/orbit/`

1. Create `backend/cmd/identity/` with `main.go` that wires only `internal/identity/`
2. Move the SQL migration files from `internal/platform/storage/migrations/` to `cmd/identity/migrations/` (identity owns those tables — sessions, refresh tokens, JTI blocklist config)
3. Update `make build` to target `cmd/identity/` (and subsequent services as they are created)
4. Update `make migrate-up` to use the aggregation approach described above
5. Retire `cmd/orbit/main.go` once `cmd/identity/` is validated
6. Create `internal/platform/clients/identity/` as the first typed inter-service client when the first consumer service needs it
7. Update `docs/code-organization.md` to reflect the multi-binary structure

### New Services

When a new service is introduced:
1. Create `backend/cmd/{service}/main.go` — wire only the feature packages the service owns
2. Create `backend/cmd/{service}/migrations/` — add SQL files for tables the service owns
3. Create `infrastructure/services/{service}/` — add `ServiceAccount`, `ClusterSPIFFEID`, and Kustomization following the existing pattern
4. Update `make build-all` to include the new binary target

---

## Validation

### Success Metrics

- Each service binary compiles independently via `go build ./cmd/{service}/...` with no other service's feature packages in its import graph
- `make migrate-up` applies all service migrations in timestamp order and is idempotent — passes in CI with no manual steps
- The identity service runs as a standalone binary in the local k3d cluster (Tier 2) with its SPIFFE ID attested and its Istio sidecar injected
- No `internal/{feature}/` package imports another feature package (enforced by import cycle check in CI)

### Monitoring

- CI import cycle check: `go vet ./...` and a custom import graph check confirm no feature-to-feature imports
- Docker image size: each service image should not contain feature packages outside its own scope (verified by inspecting the binary's import graph at build time)

---

## Related Decisions

- [ADR-0001](0001-monorepo-technology-stack.md) — Established the monorepo and listed "microservices" as a non-goal; this ADR introduces multiple deployment units within the monorepo without reversing the non-goal
- [ADR-0003](0003-repository-pattern-and-code-organization.md) — Feature-owned repository pattern; each service binary wires exactly the feature packages it owns
- [ADR-0006](0006-yaml-file-based-configuration.md) — Config loading; per-service YAML directories follow the same `config.Load[T]()` mechanism
- [ADR-0010](0010-spiffe-spire-workload-attestation.md) — Per-service SPIFFE IDs; the primary driver for separate pod deployments
- [ADR-0011](0011-universal-gateway-policy-enforcement.md) — Per-pod Envoy sidecar enforcement; requires separate pod deployments
- [ADR-0015](0015-service-mesh-and-network-enforcement-infrastructure.md) — Istio sidecar injection and Cilium pod-level network policy; requires separate pod deployments
- [ADR-0016](0016-local-development-environment.md) — Two-tier local dev strategy; Tier 1 single-binary fast feedback, Tier 2 full cluster with separate service deployments

---

## References

- [Go Modules Reference — Multiple main packages in one module](https://go.dev/ref/mod)
- [Goose — Timestamp-based migration versioning](https://github.com/pressly/goose)
- [Kubernetes Jobs — run-to-completion workloads](https://kubernetes.io/docs/concepts/workloads/controllers/job/)
- [ClusterSPIFFEID spec — infrastructure/services/identity/base/spiffe-id.yaml](../../infrastructure/services/identity/base/spiffe-id.yaml)

---

## Notes

**On the "non-microservices" framing:**

ADR-0001's non-goal was ruling out microservices as an *architectural style* — independent teams, independent release cycles, network-first internal design, per-service operational overhead from day one. That remains correct.

What this ADR introduces is multiple *deployment units* within a monorepo — a different concept. The development model is unchanged: single module, single repository, shared platform code, atomic commits across services. The only thing that changes is that the deployment model reflects the security architecture: each service gets its own pod, its own SPIFFE identity, its own Istio sidecar. The enforcement model from ADRs 0010/0011/0015 requires this; it is not an architectural preference but a security requirement.

**Last Updated:** 2026-03-10
