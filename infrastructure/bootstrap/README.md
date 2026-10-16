# kora Local Cluster Bootstrap

This directory contains the **k3d cluster creation config** for local development.

Run this **once** to create the k3d cluster. After the cluster exists, use
`deploy/local/` to install kora and its platform dependencies via Helm.

## Prerequisites

- [k3d](https://k3d.io/) ≥ v5
- [kubectl](https://kubernetes.io/docs/tasks/tools/)
- [helm](https://helm.sh/) and [helmfile](https://helmfile.readthedocs.io/)
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

Install SPIRE, Istio and kora from the reference Helmfile:

```bash
make cluster-platform   # build + push missing images; secrets and TLS;
                        # helmfile -e local apply from deploy/reference/

make cluster-deploy     # build + push tenant images; apply OrbitTenant
                        # resources and workload fixtures in deploy/local/
```

See [docs/contributing/local-development.md](../../docs/contributing/local-development.md) for the full annotated install sequence.


## What lives here vs deploy/local/

| Concern | Location |
|---------|----------|
| Create / delete k3d cluster | `infrastructure/bootstrap/` (here) |
| Helmfile, values and environments | `deploy/reference/` |
| Tenant and connection resources | `deploy/local/tenants/`, `deploy/local/connections/` |
| Non-Helm fixtures (example tenant) | `deploy/local/fixtures/` |

## Tear down

```bash
make dev-cluster-down
```
