# Orbit Local Cluster — Deploy

This directory contains Helm values files, local fixtures, and CR manifests
needed to install Orbit into the k3d local development cluster using the
**orbit-operator**.

---

## Quick Start

```bash
make dev-cluster       # start Docker Compose (Postgres, Redis, Dex) + k3d cluster
make cluster-operator  # build + deploy orbit-operator; installs SPIRE, Istio,
                       # orbit-platform, and orbit automatically via the OrbitPlatform CR
make cluster-deploy    # create secrets (orbit-secrets) and apply workload fixtures
```

That's it. The operator drives the full install sequence — you do not need to
run `helm upgrade` or `helmfile` commands manually.

---

## What Each Step Does

### `make dev-cluster`

1. Starts Docker Compose services: Postgres (`orbit-postgres`), Redis, and Dex OIDC.
2. Creates the k3d cluster `orbit-local` with a local registry at `orbit-registry:5001`.
3. Patches CoreDNS for `dex.orbit.local` resolution.

### `make cluster-operator`

1. Builds and pushes the `orbit-operator` image to `orbit-registry:5001`.
2. Applies `deploy/local/orbit-system-namespace.yaml` (pre-creates `orbit-system`).
3. Installs `charts/orbit-operator/` via Helm with `deploy/local/operator-values.yaml`.
4. Applies `deploy/local/orbit-platform.yaml` (the `OrbitPlatform` CR).
5. Applies `deploy/local/tenants/local.yaml` (the `OrbitTenant` CR for local dev).

The operator then reconciles the full release sequence:

```
spire-crds → spire → istio-base → istiod → gateway → orbit-platform → orbit
```

Waits for `daemonset/spire-agent` and `deployment/istiod` to be ready before
declaring `OrbitPlatform` as `Ready`.

### `make cluster-deploy`

1. Builds and pushes all service images (`identity`, `example`, `identity-proxy`).
2. Runs `scripts/cluster/create-secrets.sh` — creates `orbit-secrets` in
   `orbit-system` and `orbit-example-tenant`.
3. Applies all manifests in `deploy/local/fixtures/` (workload fixtures, including
   `orbit-example-tenant.yaml` which is fully self-contained).
4. Waits for `deployment/orbit-identity` to be ready.

---

## Directory Structure

```
deploy/local/
├── orbit-platform.yaml        # OrbitPlatform CR — drives the full release sequence
├── orbit-system-namespace.yaml # Pre-bootstrap namespace manifest
├── operator-values.yaml       # Values for charts/orbit-operator (local overrides)
├── orbit-values.yaml          # Values for charts/orbit (identity service)
├── platform-values.yaml       # Values for charts/orbit-platform (cluster resources)
├── spire-values.yaml          # Values for the SPIRE Helm release
├── istio-values.yaml          # Values for istiod (includes spire + identity-proxy
│                              #   sidecar injection templates)
├── gateway-values.yaml        # Values for istio gateway (NodePort on k3d)
├── helmfile-env.yaml          # helmfile environment bindings (escape-hatch use only)
├── fixtures/
│   └── orbit-example-tenant.yaml  # Self-contained example tenant workload
└── tenants/
    └── local.yaml             # OrbitTenant CR for the local dev tenant
```

---

## Tenant Registry

The `config/tenants.yaml` file at the project root is committed to the
repository and pre-configured for the local Dex OIDC provider. The operator
syncs it into the `orbit-tenants` ConfigMap in `orbit-system` automatically
when `OrbitTenant` CRs are applied.

When you modify `config/tenants.yaml`, re-apply the tenant CR to trigger a
registry sync:

```bash
kubectl apply -f deploy/local/tenants/local.yaml
kubectl rollout restart deployment/orbit-identity -n orbit-system
```

For **non-cluster local dev** (`make dev` / air hot-reload on the host):

```bash
export ORBIT_TENANTS_CONFIG=./config/tenants.yaml
make dev
```

Or set `ORBIT_TENANTS_CONFIG=./config/tenants.yaml` in your `.env` file.

---

## Trust Domain Consistency

The `global.trustDomain` value **must be identical** across all values files
that reference it:

| File | Field |
|------|-------|
| `deploy/local/platform-values.yaml` | `global.trustDomain` |
| `deploy/local/orbit-values.yaml` | `global.trustDomain` |
| `deploy/local/spire-values.yaml` | `global.spire.trustDomain` |

All are currently set to `orbit.local`. Changing one without the others will
cause SPIFFE SAN validation failures in the DestinationRule and mTLS peer
authentication will break.

---

## Updating a Running Cluster

```bash
make cluster-update-operator   # rebuild + redeploy orbit-operator only
make cluster-update            # rebuild + redeploy all service images
make cluster-update-service    # rebuild + redeploy a single service (SERVICE=identity)
```

---

## Tear Down

```bash
make dev-cluster-down   # tears down the k3d cluster only
                        # Docker Compose services (Postgres, Redis, Dex) keep running
make dev-down           # stop Docker Compose services
```

---

## Escape Hatch — Helmfile

The `helmfile.yaml` at the repo root can be used to install or inspect
individual releases without the operator. This is useful for debugging a
specific chart version or recovering from a stuck reconcile loop.

```bash
helmfile diff --environment local                        # preview changes
helmfile apply --environment local -l name=istiod        # upgrade a single release
```

See `helmfile.yaml` for the full release list and dependency order.
