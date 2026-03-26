# Orbit Local Cluster Bootstrap

This directory contains the **k3d cluster creation config** for local development.

Run this **once** to create the k3d cluster. After the cluster exists, use
`deploy/local/` to install Orbit and its platform dependencies via Helm.

## Prerequisites

- [k3d](https://k3d.io/) ≥ v5
- [kubectl](https://kubernetes.io/docs/tasks/tools/)
- [helm](https://helm.sh/) ≥ v3.12
- Docker running with the `orbit-network` bridge network (created by `make dev-up`)

## Create the cluster

```bash
make dev-cluster
```

This will:
1. Start Postgres + Redis + Dex via `make dev-up` (if not already running)
2. Create the k3d cluster from `infrastructure/bootstrap/k3d-config.yaml`
3. Patch CoreDNS to resolve `dex.orbit.local` → Docker bridge gateway

**This command is idempotent** — re-running it when the cluster already exists is a no-op.

## After cluster creation

Install Orbit's platform layer and services using the Helm charts in `deploy/local/`:

```bash
# Step 1 — install SPIRE + Istio (Core Infrastructure Layer)
helm dependency update charts/orbit-platform
helm upgrade --install orbit-platform charts/orbit-platform \
  -f deploy/local/platform-values.yaml \
  --namespace orbit-system --create-namespace

# Step 2 — wait for SPIRE agent + Istio control plane
kubectl rollout status daemonset/spire-agent -n spire --timeout=120s
kubectl rollout status deployment/istiod -n istio-system --timeout=120s

# Step 3 — install Orbit services
helm upgrade --install orbit charts/orbit \
  -f deploy/local/orbit-values.yaml \
  --namespace orbit-system

# Step 4 — apply non-Helm local fixtures
kubectl apply -f deploy/local/fixtures/
```

See `deploy/local/README.md` for the full annotated install sequence.

## What lives here vs deploy/local/

| Concern | Location |
|---------|----------|
| Create / delete k3d cluster | `infrastructure/bootstrap/` (here) |
| Install SPIRE + Istio + Orbit via Helm | `deploy/local/` |
| Helm values for local overrides | `deploy/local/platform-values.yaml`, `deploy/local/orbit-values.yaml` |
| Non-Helm fixtures (example tenant) | `deploy/local/fixtures/` |

## Tear down

```bash
make dev-cluster-down
```
