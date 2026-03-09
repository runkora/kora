# 0016. Local Development Environment

**Status:** Accepted
**Date:** 2026-03-03
**Deciders:** Engineering Team
**Technical Story:** Defining the local development environment strategy for Orbit — how engineers run the platform locally for day-to-day Go development and for full-stack integration testing against the SPIFFE/SPIRE, Istio, and Envoy infrastructure established in ADRs 0010, 0011, and 0015.

---

## Context

Orbit's architecture (ADRs 0010, 0011, 0015) requires several infrastructure components that do not exist in a typical Go development setup:

- **SPIRE Server + Agent** — workload attestation and SVID issuance (ADR-0010)
- **Envoy sidecars** — mTLS enforcement and platform token injection (ADR-0011)
- **Istio** — sidecar injection and xDS config distribution (ADR-0015)
- **OPA** — policy bundle evaluation at ext_authz service (ADR-0011)
- **Cilium** — eBPF-level network enforcement (ADR-0015)

Running all of these locally for every Go code change would make the feedback loop unusably slow. At the same time, deferring all infrastructure setup to CI means engineers cannot test the SPIFFE identity flow, ext_authz enforcement, or Envoy sidecar injection locally at all — producing a class of integration bugs that are only discovered in staging.

A two-tier environment strategy resolves this tension: a fast, lightweight baseline for daily Go development, and a full-stack local cluster for integration testing of the identity and enforcement layers.

### Problem Statement

We need to define:

1. The tooling used to run a local Kubernetes cluster for full-stack testing
2. How SPIRE is configured locally (trust domain, attestation plugin, datastore)
3. Whether Cilium is included in the local environment and if not, what substitutes
4. How the SVID exchange endpoint (`POST /identity/exchange`) is handled in unit tests vs full integration tests
5. The `make` targets that provide a consistent entry point for both tiers
6. How Tier 2 cluster nodes reach the Tier 1 postgres instance

### Goals

- **Fast feedback for Go development**: The baseline environment starts in seconds with a single command and provides Postgres, Redis, and OPA — no cluster required
- **Full-stack integration testing**: A local Kubernetes cluster provides SPIRE, Istio, and Envoy for integration testing of the identity and enforcement flows
- **Documented Cilium decision**: The rationale for excluding Cilium locally is explicit and recorded — engineers understand why it is absent and what the CI/staging difference is
- **Reproducible**: Both environment tiers are fully scripted via `make` targets; no manual steps required after initial tool installation
- **SVID exchange testability**: Engineers can test the `POST /identity/exchange` flow locally without a cloud cluster

### Non-Goals

- Defining the CI/staging environment (separate concern)
- Defining production Kubernetes topology (separate concern)
- Providing a complete Istio/Envoy production configuration — local config is intentionally minimal
- Supporting Windows as a development platform

---

## Decision Drivers

* **Developer velocity**: Requiring a full cluster for every code change is not acceptable — the baseline tier must be fast
* **Integration confidence**: The SVID exchange flow, ext_authz gRPC protocol, and sidecar token injection cannot be meaningfully tested without the actual infrastructure running
* **macOS compatibility**: Most engineers develop on macOS; the environment must work on macOS without kernel-level hacks
* **Cilium reality**: Cilium's eBPF enforcement requires a Linux kernel; it is not runnable on macOS via Docker Desktop, k3d, or kind without significant workarounds that make the local environment fragile
* **Simplicity of setup**: New engineers should be able to go from `git clone` to a running environment in under 15 minutes

---

## Considered Options

### Local Cluster Tooling

#### Option A: k3d

**Description:** k3d runs k3s (a lightweight Kubernetes distribution) inside Docker containers. A multi-node cluster is a single CLI command. It integrates cleanly with the existing Docker-based workflow.

**Pros:**
- Runs inside Docker — no VM overhead, no separate hypervisor
- Fast cluster creation (~30 seconds)
- Works well on macOS via Docker Desktop
- Supports multiple nodes for realistic pod scheduling
- LoadBalancer services work natively via k3d's built-in load balancer
- Active project, widely used in local dev workflows

**Cons:**
- k3s is not vanilla Kubernetes — minor API compatibility differences exist (though none relevant to Orbit's usage)
- Cilium is not supported in k3d with full eBPF capability on macOS (this is a Cilium limitation, not a k3d limitation)

#### Option B: kind (Kubernetes IN Docker)

**Description:** kind runs Kubernetes inside Docker containers and is the reference tool for Kubernetes conformance testing.

**Pros:**
- Vanilla Kubernetes — maximum API compatibility
- Widely used in CI pipelines
- Supported by the Kubernetes SIG-testing team

**Cons:**
- Slower cluster creation than k3d (~2–3 minutes vs ~30 seconds)
- LoadBalancer services do not work natively — requires MetalLB or similar
- Less ergonomic for multi-service local dev (port-forwarding is the primary access pattern)

#### Option C: minikube

**Description:** minikube runs a single-node Kubernetes cluster, typically in a VM or Docker container.

**Pros:**
- Long-established tool, extensive documentation
- Supports multiple drivers (Docker, VirtualBox, Hyperkit)

**Cons:**
- Single-node only — cannot test realistic multi-pod scheduling
- Slower and heavier than k3d
- VM-based drivers add significant resource overhead on macOS
- Docker driver has similar Cilium limitations to k3d

---

## Decision

### Tier 1 — `make dev-up` (Daily Go Development)

**Tooling:** Docker Compose (`docker compose`)

`make dev-up` starts:
- **PostgreSQL** — primary data store for all platform services
- **Redis** — JTI blocklist store (session revocation per ADR-0008) and general caching

OPA is **not included** in Tier 1. Its deployment topology (sidecar to the ext_authz service, standalone cluster service, or both) is an open question to be resolved when the ext_authz service is designed. Adding OPA to Docker Compose before that decision is made would presuppose the answer. It will be added to the appropriate tier via a future ADR amendment.

This tier has no Kubernetes dependency. All Go services run directly with `go run` or `air` (hot reload). Unit and repository-level integration tests run against this tier.

The SVID exchange endpoint (`POST /identity/exchange`) is **not available** in this tier. Unit tests for the identity service stub the SPIFFE validation step with a configurable test helper that accepts a pre-signed test SVID. This is explicitly documented in the identity package — the stub is not a permanent workaround, it is the correct unit-test boundary.

### Tier 2 — `make dev-cluster` (Full-Stack Integration Testing)

**Tooling:** k3d

**Decision rationale:** k3d is chosen over kind and minikube. The combination of fast cluster creation, Docker-native operation, and clean LoadBalancer support makes it the most productive choice for the local full-stack tier. The k3s API compatibility delta is not relevant to any Orbit platform usage.

`make dev-cluster` provisions:
- A k3d cluster with 1 server node and 2 agent nodes, joined to the same Docker bridge network as the Tier 1 postgres container (`orbit-network`) so cluster pods can reach `orbit-postgres` by name without additional networking hacks
- **SPIRE Server** — deployed as a Kubernetes StatefulSet with a minimal trust domain (`spiffe://orbit.local`), backed by the same PostgreSQL instance as the rest of the platform (no SQLite3 — SQLite3 is single-writer and inconsistent with the rest of the stack)
- **SPIRE Agent** — deployed as a DaemonSet with the `k8s_psat` attestation plugin (Kubernetes projected service account tokens — works without hardware TPM)
- **Istio** — installed via `istioctl` with sidecar injection enabled for the `orbit-system` namespace
- Standard Kubernetes **NetworkPolicy** resources enforcing the sidecar-or-nothing constraint from ADR-0011

OPA topology for Tier 2 will be defined when the ext_authz service is designed.

**Cilium is not included in the local dev cluster.** See below.

**Tier 2 requires Tier 1 postgres to be running first.** SPIRE Server connects to PostgreSQL at pod startup; if postgres is not healthy when the cluster deploys, SPIRE enters a crash loop with no actionable error. `make dev-cluster` declares `db-up` as a prerequisite — engineers do not need to remember the ordering.

The full `POST /identity/exchange` flow is testable in this tier — a SPIRE Agent running in the cluster issues real SVIDs to attested workloads, and the exchange endpoint receives and validates them.

### Cilium — Explicitly Deferred to CI/Staging

Cilium is not included in the local development environment. This is an explicit, deliberate decision — not an oversight.

**Rationale:**

1. **macOS kernel incompatibility**: Cilium's eBPF enforcement requires Linux kernel 4.9+ with eBPF support. On macOS, Docker Desktop runs a Linux VM with a kernel that Cilium nominally supports, but Cilium's eBPF programs depend on kernel features (kprobes, tc hooks) that are not reliably available or stable in the Docker Desktop VM kernel. In practice, Cilium either fails to start or runs in a degraded mode that does not enforce its network policies.

2. **k3d kernel restrictions**: k3d runs k3s inside Docker containers. The eBPF maps and program loading that Cilium requires cannot be isolated per-container in the way Cilium expects. This is a fundamental architectural mismatch, not a configuration problem.

3. **Local substitute is sufficient**: The architectural constraint from ADR-0011 — "no direct pod-to-pod traffic that bypasses the sidecar" — is enforced locally via standard Kubernetes `NetworkPolicy` resources. These are less sophisticated than Cilium's eBPF enforcement (they are iptables-based and do not provide DNS-aware egress policy), but they are sufficient to verify that the enforcement topology is correctly wired. The distinction matters for production hardening, not for local development correctness.

4. **CI/staging is the right place**: Cilium's eBPF enforcement is a production security property. It should be validated in a CI pipeline running on real Linux nodes (e.g. GitHub Actions with Ubuntu runners) and in staging. This is where Cilium runs reliably and where its network policy enforcement can be meaningfully tested.

**What this means in practice:**
- Engineers cannot locally validate Cilium-specific network policies (DNS-aware egress, per-pod eBPF enforcement)
- Standard NetworkPolicy resources cover the sidecar bypass constraint for local dev
- Any Cilium-specific policy files are validated in CI only
- This limitation is explicitly documented and expected — it is not a gap to be solved

### SVID Exchange Endpoint — Unit Test Strategy

The `POST /identity/exchange` endpoint accepts a SPIFFE SVID (an X.509 certificate issued by SPIRE) and exchanges it for an Orbit platform session token.

Testing this endpoint requires a real SVID, which requires a running SPIRE Agent. The unit/integration test boundary is handled as follows:

**Unit tests** (`make test`): The identity service exposes a `SVIDValidator` interface. The real implementation calls the SPIRE Workload API to validate the presented SVID against the trust bundle. Unit tests inject a `StubSVIDValidator` that accepts a pre-generated test certificate (committed to the repo under `testdata/`). This keeps unit tests fast and SPIRE-independent.

**Integration tests** (`make test-integration`): Run against `make dev-cluster`. The real `SVIDValidator` implementation is used. A test workload pod is attested by the SPIRE Agent and receives a real SVID, which is then presented to the exchange endpoint. This test is tagged with `//go:build integration` and skipped in the standard `make test` target.

---

## Consequences

### Positive

- Engineers can run the full Go service suite locally with `make dev-up` in under 30 seconds — no cluster, no overhead
- The full SPIFFE/SPIRE/Istio/Envoy stack is testable locally via `make dev-cluster` for the identity and enforcement flows that genuinely require it
- The Cilium decision is documented — no engineer will spend time trying to make Cilium work on their MacBook only to discover it is a known unsupported configuration
- The `SVIDValidator` interface boundary makes the identity service testable without infrastructure at the unit test level, while preserving end-to-end correctness in integration tests
- SPIRE uses the same PostgreSQL instance as every other platform service — no divergence in storage technology between local dev tiers

### Negative

- Two-tier maintenance burden: both Docker Compose and k3d configurations must be kept up to date as new services are added
- Cilium network policies are not locally testable — bugs in Cilium-specific egress rules will not surface until CI
- `make dev-cluster` is slower to provision than `make dev-up` (~3–5 minutes for initial cluster creation)
- Tier 2 has an explicit dependency on Tier 1 postgres being running — engineers cannot spin up the cluster in isolation

### Neutral

- All Go services must be written to support both tiers — configuration must be environment-aware (SPIRE endpoint configurable, external service endpoints configurable, etc.)
- SPIRE trust domain for local development is `spiffe://orbit.local` — distinct from staging/production trust domains to prevent accidental cross-environment SVID acceptance

---

## Implementation Notes

### Required Local Tools

Installed by `make setup`:
- Docker Desktop (macOS)
- k3d
- kubectl
- istioctl
- helm (for Istio and other chart installations — SPIRE is deployed via kustomize manifests, not a helm chart)
- Existing tools: goose, air, golangci-lint, gitleaks

### make Targets

| Target | Description |
|---|---|
| `make dev-up` | Start Tier 1: Postgres + Redis via Docker Compose |
| `make dev-down` | Stop and remove Tier 1 containers |
| `make db-up` | Start Postgres only (prerequisite for `make dev-cluster`) |
| `make dev-cluster` | Provision Tier 2: k3d cluster + SPIRE + Istio (implicitly runs `db-up` first) |
| `make dev-cluster-down` | Tear down the k3d cluster |
| `make test` | Run unit tests (Tier 1, no cluster required) |
| `make test-integration` | Run integration tests (requires Tier 2 cluster running) |

### SPIRE Configuration — Base vs Local Overlay

SPIRE configuration is layered using kustomize:

- **`infrastructure/base/spire/`** — production-safe defaults. Uses `orbit-cluster` as the cluster name, `KeyManager "disk"` with durable storage, PostgreSQL with a production-pattern DSN (dedicated `spire` database, `sslmode=require`), and no `insecure_bootstrap`. The base layer must never contain dev-only values.
- **`infrastructure/overlays/local/patches/`** — local dev overrides applied on top of the base. Restores the dev-only values that would be incorrect or insecure in production.

The local overlay patches three ConfigMaps:

**spire-server-config (local overlay):**
```hcl
server {
  trust_domain = "orbit.local"
  bind_address = "0.0.0.0"
  bind_port    = "8081"
  log_level    = "INFO"
  data_dir     = "/run/spire/data"

  # Local dev: connect to the docker-compose postgres instance.
  # k3d nodes are joined to orbit-network so orbit-postgres is reachable by name.
}

plugins {
  DataStore "sql" {
    plugin_data {
      database_type     = "postgres"
      connection_string = "host=orbit-postgres port=5432 user=orbit password=orbit dbname=orbit sslmode=disable"
    }
  }

  NodeAttestor "k8s_psat" {
    plugin_data {
      clusters = {
        "orbit-local" = {
          # spire-agent ServiceAccount lives in the `spire` namespace,
          # not in orbit-system — SPIRE and its agent are self-contained.
          service_account_allow_list = ["spire:spire-agent"]
        }
      }
    }
  }

  KeyManager "memory" {}
}
```

**spire-agent-configmap (local overlay):**
```hcl
agent {
  trust_domain        = "orbit.local"
  server_address      = "spire-server"
  server_port         = 8081
  insecure_bootstrap  = true   # local dev only — skips trust bundle verification at first contact
  log_level           = "INFO"
  data_dir            = "/run/spire/agent-data"
}

plugins {
  NodeAttestor "k8s_psat" {
    plugin_data {
      cluster = "orbit-local"
      skip_kubelet_verification = true
    }
  }

  KeyManager "memory" {}
}
```

`insecure_bootstrap = true` and `KeyManager "memory"` are local dev only. They must never appear in the base layer or in any non-local overlay — `insecure_bootstrap` bypasses initial trust bundle validation, and `KeyManager "memory"` discards keys on restart, neither of which is acceptable outside local dev.

---

## Related Decisions

- [ADR-0010](0010-spiffe-spire-workload-attestation.md) — SPIFFE/SPIRE workload attestation
- [ADR-0011](0011-universal-gateway-policy-enforcement.md) — Universal gateway policy enforcement
- [ADR-0015](0015-service-mesh-and-network-enforcement-infrastructure.md) — Service mesh and network enforcement infrastructure
