# Frontend Cluster Deployment and Ingress Routing

**Status:** Accepted
**Date:** 2026-04-06
**Deciders:** Engineering Team
**Technical Story:** The kora frontend ([frontend-scaffolding-and-toolchain](2026-03-17-frontend-scaffolding-and-toolchain.md), [browser-auth-flow-and-bff-session-architecture](2026-04-01-browser-auth-flow-and-bff-session-architecture.md), [home-realm-discovery-and-email-based-tenant-routing](2026-04-01-home-realm-discovery-and-email-based-tenant-routing.md)) runs in Tier 1 local development via `pnpm dev` on the host, talking to the Go identity service over localhost. The Tier 2 k3d cluster ([local-development-environment](2026-03-10-local-development-environment.md)) runs the identity service inside Kubernetes under SPIRE/Istio ([spiffe-spire-workload-attestation](2026-03-05-spiffe-spire-workload-attestation.md), [service-mesh-and-network-enforcement-infrastructure](2026-03-05-service-mesh-and-network-enforcement-infrastructure.md)) but has no frontend deployment. Developers cannot exercise the full end-to-end platform — browser login, dashboard, and agent CLI inference — without spinning up the cluster and the dev server separately, pointing at different origins. This ADR closes that gap.

---

## Context

### Background

[local-development-environment](2026-03-10-local-development-environment.md) established two local development tiers:

- **Tier 1 (host binaries):** `make dev` — Go services running directly on the host with hot-reload. Frontend runs via `pnpm dev` on `localhost:3000`. All services talk over localhost.
- **Tier 2 (k3d cluster):** `make dev-cluster && make cluster-operator` — SPIRE, Istio, and all kora services running in Kubernetes. The identity service has a SPIFFE SVID and participates in the mTLS mesh.

Until now the frontend has had no Tier 2 representation. An engineer running the full cluster still has to start the dev server manually on the host, which:

1. Bypasses the real service mesh path (the BFF calls `localhost:8080`, not the in-cluster identity service)
2. Makes end-to-end testing of the auth flow against a real cluster impractical
3. Means the cluster is never fully self-contained for demo or CI use

[frontend-scaffolding-and-toolchain](2026-03-17-frontend-scaffolding-and-toolchain.md) scaffolded the Next.js frontend. [browser-auth-flow-and-bff-session-architecture](2026-04-01-browser-auth-flow-and-bff-session-architecture.md) defined the BFF session architecture (iron-session sealed cookie, server-side token handling). [home-realm-discovery-and-email-based-tenant-routing](2026-04-01-home-realm-discovery-and-email-based-tenant-routing.md) defined Home Realm Discovery for multi-tenant login. This ADR defines how the frontend runs *in* the cluster.

### What Already Exists

The operator ([orbit-kubernetes-operator](2026-03-31-orbit-kubernetes-operator.md)) installs the `orbit` Helm chart, which currently deploys:
- `kora-identity` — the Go identity service in `kora-system`
- `identity-proxy` — an Envoy EnvoyFilter for outbound token injection ([envoy-ext-proc-sidecar-token-injection](2026-03-21-envoy-ext-proc-sidecar-token-injection.md))

The `orbit-platform` chart (also operator-managed) owns cluster-scoped infrastructure:
- Namespaces (`kora-system`, `spire-system`, `istio-system`, tenant namespaces)
- ClusterSPIFFEIDs ([dynamic-spiffe-identity-grants-and-tenant-isolation](2026-03-17-dynamic-spiffe-identity-grants-and-tenant-isolation.md))
- NetworkPolicies

The Istio gateway Helm release (installed by the operator in order) creates an Envoy-based ingress gateway in `istio-system` and exposes it via the k3d loadbalancer on ports 8880 (HTTP) and 8443 (HTTPS). No application traffic currently routes through it — the identity service is a ClusterIP reached only from within the mesh.

The inference gateway ([inference-gateway-architecture](2026-04-01-inference-gateway-architecture.md)) is also a platform service in `kora-system`. Developers running agent CLIs against a local cluster currently have no stable external address for it.

### Problem Statement

Two gaps must be closed:

1. **Frontend has no cluster deployment.** It cannot be reached from a browser when using the Tier 2 cluster.
2. **No service has external routing.** The Istio ingress gateway exists but carries no VirtualServices — neither the frontend nor the inference gateway is reachable from outside the cluster.

Both gaps require the same foundational piece: an Istio `Gateway` resource that binds listening configuration to the gateway pods, and per-service `VirtualService` resources that define routing rules.

### Goals

- `make cluster-operator` produces a fully working cluster: browser can reach the frontend at a stable local hostname, agent CLIs can reach the inference gateway at a stable local hostname
- Both hostnames resolve inside and outside k3d via the existing `*.orbit.local.clearroute.io` DNS infrastructure
- The frontend pod is a first-class mesh citizen: SPIFFE identity, Istio sidecar, mTLS to the identity service
- The `kora-frontend-secrets` Secret (BFF session key) follows the same pre-creation pattern as `kora-secrets` — created by `create-secrets.sh`, referenced by the chart
- No changes to the operator's reconciliation logic — all new resources are in the existing charts

### Non-Goals

- TLS termination at the ingress gateway (deferred — requires cert-manager or a static cert; HTTP is sufficient for local dev)
- Rate limiting at the ingress layer (deferred to a future ADR)
- Production ingress configuration (cloud load balancers, external DNS, cert provisioning)
- Exposing the identity service externally (it is an internal platform API, not a developer-facing endpoint)

---

## Decision Drivers

* **Consistency with existing patterns** — the frontend should be deployed, secreted, and configured exactly like the identity service. No new patterns unless forced.
* **Mesh citizenship** — the frontend calls the identity service in-cluster. Both must participate in the mTLS mesh to preserve the security properties established in [service-mesh-and-network-enforcement-infrastructure](2026-03-05-service-mesh-and-network-enforcement-infrastructure.md).
* **Operator transparency** — the operator should not need Go code changes. New resources belong in the Helm charts it already drives.
* **Host-based routing correctness** — Istio merges multiple VirtualServices that share the same host. Path-based routing on a wildcard host from separate VirtualService resources produces non-deterministic route ordering. The routing design must avoid this.
* **DNS coherence** — new hostnames should follow the `*.orbit.local.clearroute.io` convention already established for `dex.orbit.local.clearroute.io`.

---

## Considered Options

### Option 1: Path-based routing, single VirtualService

Route all external traffic through one VirtualService on a wildcard host (`*`), matching by URI prefix:
- `/v1/*` → inference gateway
- `/*` → frontend (catch-all)

**Pros:**
- Single object to manage for all external routing
- No DNS changes beyond what exists

**Cons:**
- A single VirtualService becomes a bottleneck as more services need external access
- Path prefixes constrain the inference gateway API surface (the OpenAI-compatible `/v1/` prefix happens to work today, but future APIs may not fit)
- Route ordering is implicit — adding a new prefix silently changes which requests the catch-all captures

### Option 2: Path-based routing, multiple VirtualServices (wildcard host)

One VirtualService per service, all binding to host `*` on the same Gateway.

**Pros:**
- Each service owns its routing rules

**Cons:**
- Istio merges VirtualServices with the same host. Merge order is determined by creation timestamp and resource name — not by route specificity. A `/*` catch-all in the frontend VS can absorb requests intended for the inference gateway VS, depending on which object was created first. This is a known Istio footgun documented in the project's own FAQs.
- Non-deterministic behaviour that is hard to debug under `istioctl proxy-config`.

### Option 3: Host-based routing, separate VirtualServices per service ✓

Each service gets a distinct hostname. Each VirtualService binds exclusively to that hostname — no shared host, no merge.

- `orbit.local.clearroute.io` → frontend VirtualService → `kora-frontend` ClusterIP service
- `api.orbit.local.clearroute.io` → inference gateway VirtualService → `kora-inference-gateway` ClusterIP service

**Pros:**
- No Istio merge ambiguity — VirtualServices on different hosts are completely independent
- Each service owns its routing rules and can evolve them independently (add retries, timeouts, header rewrites) without touching another service's configuration
- Maps cleanly to production deployment (real subdomains, separate DNS records, independent certificates)
- `orbit.local.clearroute.io` is the natural apex for the user-facing UI; `api.orbit.local.clearroute.io` is the natural subdomain for the developer API

**Cons:**
- Requires DNS entries for each hostname (`orbit.local.clearroute.io`, `api.orbit.local.clearroute.io`) in addition to the existing `dex.orbit.local.clearroute.io`
- `dev-cluster-up.sh` CoreDNS patch must cover both new hostnames

The DNS overhead is trivial — both hostnames are added to the existing `orbit.local.clearroute.io.server` CoreDNS block in a single patch. The routing correctness benefit is definitive.

---

## Decision

**Chosen Option:** Option 3 — host-based routing with one VirtualService per service.

### Rationale

The Istio VirtualService merge behaviour with shared wildcard hosts is a well-documented source of production incidents. Given that kora's architecture is explicitly designed to avoid implicit, hard-to-debug behaviour (see [service-mesh-and-network-enforcement-infrastructure](2026-03-05-service-mesh-and-network-enforcement-infrastructure.md) on explicit NetworkPolicy over implicit CNI trust), accepting a routing model with non-deterministic merge semantics is inconsistent. Host-based routing eliminates the problem entirely and mirrors how external ingress works in every production Kubernetes environment.

The single shared piece — the Istio `Gateway` resource — is a platform concern and belongs in `charts/orbit-platform/`, not in any individual service's template directory. The `orbit-platform` chart already owns namespace creation, ClusterSPIFFEIDs, and NetworkPolicies. The Gateway resource is the same category of cluster-scoped infrastructure.

---

## Implementation

### Istio Gateway Resource

A single `Gateway` resource in `charts/orbit-platform/templates/gateway.yaml`:

```yaml
apiVersion: networking.istio.io/v1beta1
kind: Gateway
metadata:
  name: kora-gateway
  namespace: {{ .Values.platform.namespaces.system }}
spec:
  selector:
    app: gateway   # selects Istio ingress gateway pods in istio-system
  servers:
    - port:
        number: 80
        name: http
        protocol: HTTP
      hosts:
        - "orbit.local.clearroute.io"
        - "api.orbit.local.clearroute.io"
```

Referenced by VirtualServices as `kora-system/kora-gateway`.

### Frontend Service

The frontend is deployed in `kora-system` as a platform service via the `kora` chart, following the identity service pattern exactly:

| Attribute | Value |
|-----------|-------|
| Namespace | `kora-system` |
| Service account | `kora-frontend` |
| Container port | 3000 |
| Service port | 80 → 3000 |
| SPIFFE SVID | `spiffe://<trust-domain>/system/frontend` |
| Istio templates | `sidecar,spire` |
| ConfigMap | `kora-frontend-config` (KORA_API_URL, NEXT_PUBLIC_APP_URL) |
| Secret | `kora-frontend-secrets` (BFF_SESSION_SECRET) |

The frontend pod gets the `spiffe.io/component: frontend` label, which causes the existing `kora-system-component` ClusterSPIFFEID to issue it a SVID at path `/system/frontend`. No new ClusterSPIFFEID is required.

`KORA_API_URL` points at the identity service via in-cluster DNS: `http://kora-identity.kora-system/api/v1`. The frontend's Envoy sidecar transparently upgrades this to mTLS using the SPIFFE SVIDs of both pods.

`NEXT_PUBLIC_APP_URL` is both injected at runtime via the `kora-frontend-config` ConfigMap (for server-side code) and baked into the Docker image at build time via `--build-arg NEXT_PUBLIC_APP_URL=...` (for Next.js client bundles, which inline `NEXT_PUBLIC_*` values during `next build`).

### Container Image

The frontend uses a multi-stage Dockerfile with `frontend/` as the build context (self-contained pnpm workspace). The `NEXT_OUTPUT=standalone` environment variable activates Next.js's standalone output mode, which traces all runtime dependencies and produces a minimal `server.js` entry point. The runtime stage copies only `.next/standalone`, `.next/static`, and `public/` — no `node_modules` from the build stage are carried over.

The container runs as uid/gid 65532 (`orbit`/`orbit`), consistent with all other kora service containers.

### BFF Session Secret

`kora-frontend-secrets` is created by `scripts/cluster/create-secrets.sh` alongside the existing `kora-secrets`, following the same idempotent `--dry-run=client | kubectl apply` pattern. It contains a single key: `BFF_SESSION_SECRET`, sourced from the `.env` file (which already documents this variable under the `# Frontend` section). The `gen-session-secret` Makefile target (`openssl rand -hex 32`) generates a suitable value.

### NetworkPolicy

The existing `default-deny-all` policy in `kora-system` blocks ingress from `istio-system`. A new `allow-gateway-ingress` NetworkPolicy allows the Istio gateway pods to reach kora-system pods on Envoy's inbound ports (15006 and 15008), enabling gateway-routed traffic to reach the frontend pod's sidecar.

### DNS

`dev-cluster-up.sh` patches CoreDNS with an `orbit.local.clearroute.io.server` block. `orbit.local.clearroute.io` and `api.orbit.local.clearroute.io` are added to the existing block alongside `dex.orbit.local.clearroute.io`, all resolving to the Docker bridge gateway IP. The host machine's dnsmasq already resolves `*.orbit.local.clearroute.io → 127.0.0.1` (configured in `make setup-dns`), so both hostnames work in the browser without additional host-side DNS configuration.

---

## Consequences

### Positive

- `make cluster-operator` produces a complete, self-contained local cluster — browser login and CLI inference work without any host-side service running
- The frontend participates fully in the SPIFFE/Istio mesh — its traffic to the identity service is mTLS-authenticated with SVID validation, not plain HTTP over localhost
- Each service owns its VirtualService — future routing changes (timeouts, retries, header injection) are isolated and independently reviewable
- The routing architecture maps directly to production: replace `orbit.local.clearroute.io` with a real domain, add TLS to the Gateway server block, and the structure is unchanged

### Negative

- `NEXT_PUBLIC_APP_URL` is baked into the Docker image at build time. Changing the external hostname requires a rebuild. This is a fundamental constraint of Next.js's static optimisation for `NEXT_PUBLIC_*` variables, not a consequence of this design.
- The CoreDNS patch in `dev-cluster-up.sh` now covers three hostnames. Developers who created their cluster before this change must re-run `make dev-cluster` (or apply the CoreDNS patch manually) to get DNS resolution for `orbit.local.clearroute.io` and `api.orbit.local.clearroute.io`.

### Neutral

- The `orbit-platform` chart now contains an Istio CRD resource (`Gateway`). This resource is applied after `istiod` is running (the operator's release sequence guarantees this), so the CRD is always present when the template is applied.

---

## Validation

### Success Metrics

- `curl -sk https://orbit.local.clearroute.io:8443 | grep -q "<!DOCTYPE html>"` returns 0 after `make cluster-operator`
- `curl -sk https://api.orbit.local.clearroute.io:8443/health` returns a 200 from the inference gateway
- `kubectl get pods -n kora-system` shows `kora-frontend-*` with 2/2 containers ready (app + Istio sidecar)
- Browser login at `https://orbit.local.clearroute.io:8443` completes the full OIDC flow and reaches the dashboard

---

## Related Decisions

- [service-mesh-and-network-enforcement-infrastructure](2026-03-05-service-mesh-and-network-enforcement-infrastructure.md) — Istio service mesh and NetworkPolicy
- [local-development-environment](2026-03-10-local-development-environment.md) — Two-tier local development environment
- [dynamic-spiffe-identity-grants-and-tenant-isolation](2026-03-17-dynamic-spiffe-identity-grants-and-tenant-isolation.md) — SPIFFE identity grants
- [frontend-scaffolding-and-toolchain](2026-03-17-frontend-scaffolding-and-toolchain.md) — Frontend scaffolding
- [inference-gateway-architecture](2026-04-01-inference-gateway-architecture.md) — Inference gateway
- [orbit-kubernetes-operator](2026-03-31-orbit-kubernetes-operator.md) — Kubernetes operator
- [browser-auth-flow-and-bff-session-architecture](2026-04-01-browser-auth-flow-and-bff-session-architecture.md) — BFF session architecture

---

**Last Updated:** 2026-04-02
