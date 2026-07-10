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
3. Patch CoreDNS to resolve `dex.orbit.local.clearroute.io` → Docker bridge gateway

**This command is idempotent**: re-running it when the cluster already exists is a no-op.

## After cluster creation

Install Orbit and its full platform layer via the operator:

```bash
make cluster-operator   # build + push orbit-operator image, install the operator
                        # Helm chart, and apply the OrbitPlatform + OrbitTenant CRs.
                        # The operator then drives the full release sequence:
                        # spire-crds → spire → istio-base → istiod → gateway
                        #   → orbit-platform → orbit

make cluster-deploy     # build + push service images; create orbit-secrets;
                        # apply workload fixtures in deploy/local/fixtures/
```

See `deploy/local/README.md` for the full annotated install sequence.


## What lives here vs deploy/local/

| Concern | Location |
|---------|----------|
| Create / delete k3d cluster | `infrastructure/bootstrap/` (here) |
| Operator + platform CRDs / CRs | `deploy/local/operator/`, `deploy/local/platform/`, `deploy/local/tenants/` |
| Helm values for local overrides | `deploy/local/operator/values.yaml`, `deploy/local/platform/values.yaml` |
| Non-Helm fixtures (example tenant) | `deploy/local/fixtures/` |

## Tear down

```bash
make dev-cluster-down
```
