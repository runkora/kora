# Orbit Local Cluster — Deploy

This directory contains the Helm values files and local fixtures needed to
install Orbit into the k3d local development cluster (`make cluster-up`).

---

## Prerequisites

Before running any `helm` commands here, ensure the following are satisfied:

1. **k3d cluster is running**
   ```bash
   make cluster-up
   ```
   This creates a k3d cluster (`orbit-local`) with a local registry at
   `orbit-registry:5001` and patches CoreDNS for `dex.orbit.local` resolution.

2. **Orbit images are built and pushed to the local registry**
   ```bash
   make build-backend
   make push-local
   ```

3. **Secrets are created in the cluster**
   ```bash
   make cluster-secrets
   ```
   Creates `orbit-secrets` in `orbit-system` and `orbit-example-tenant`
   namespaces via `scripts/cluster/create-secrets.sh`. Requires
   `secrets/signing.key`, `DB_PASSWORD`, and `REDIS_PASSWORD` to be set.

4. **Tenant registry ConfigMap exists**
   ```bash
   kubectl create configmap orbit-tenants \
     --from-file=tenants.yaml=config/tenants.yaml \
     -n orbit-system \
     --dry-run=client -o yaml | kubectl apply -f -
   ```
   The `config/tenants.yaml` file is committed to the repository and
   pre-configured for the local Dex OIDC provider. See
   [Tenant Registry](#tenant-registry) below.

5. **Orbit ConfigMap exists** (non-secret env vars for the identity service)
   ```bash
   kubectl create configmap orbit-config \
     --from-literal=DB_HOST=orbit-postgres \
     --from-literal=DB_NAME=orbit \
     --from-literal=DB_PORT=5432 \
     --from-literal=DB_USER=orbit \
     --from-literal=REDIS_HOST=host.k3d.internal \
     --from-literal=REDIS_PORT=6379 \
     -n orbit-system \
     --dry-run=client -o yaml | kubectl apply -f -
   ```

6. **Database migrations are applied**
   ```bash
   make migrate-up
   ```

---

## Three-Step Install Sequence

```bash
# Step 1 — Install platform layer (SPIRE + Istio + namespaces + ClusterSPIFFEIDs)
helm upgrade --install orbit-platform ./charts/orbit-platform \
  -f deploy/local/platform-values.yaml \
  --namespace orbit-system \
  --create-namespace

# Wait for SPIRE and Istio pods to become ready before proceeding:
kubectl -n spire wait --for=condition=ready pod -l app.kubernetes.io/name=spire-server --timeout=120s
kubectl -n istio-system wait --for=condition=ready pod -l app=istiod --timeout=120s

# Step 2 — Install the Orbit application chart
helm upgrade --install orbit ./charts/orbit \
  -f deploy/local/orbit-values.yaml \
  --namespace orbit-system

# Step 3 — Apply local fixtures (not Helm-managed)
kubectl apply -f deploy/local/fixtures/orbit-example-tenant.yaml
```

---

## Tenant Registry

The `config/tenants.yaml` file at the project root is committed to the
repository and pre-configured for the local Dex OIDC provider. It is the
source of truth for tenant registration in local dev.

When you modify `config/tenants.yaml`, re-sync the ConfigMap:

```bash
kubectl create configmap orbit-tenants \
  --from-file=tenants.yaml=config/tenants.yaml \
  -n orbit-system \
  --dry-run=client -o yaml | kubectl apply -f -

# Restart the identity deployment to pick up the new config
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

The `global.trustDomain` value **must be identical** in both values files:

| File | Field |
|------|-------|
| `deploy/local/platform-values.yaml` | `global.trustDomain` |
| `deploy/local/orbit-values.yaml` | `global.trustDomain` |

Both are currently set to `orbit.local`. Changing one without the other will
cause SPIFFE SAN validation failures in the DestinationRule and mTLS peer
authentication will break.

---

## Adapting for Non-Local Environments

This directory is intentionally **local dev only**. For staging/production:

1. Do not use `imagePullPolicy: Always` — the default `IfNotPresent` in
   `charts/orbit/values.yaml` is the safe production default.
2. Do not reference `orbit-registry:5001` — use your OCI registry:
   ```
   ghcr.io/orbit-sh/orbit-identity:<version>
   ```
3. Chart registry URLs (once published):
   ```
   oci://ghcr.io/orbit-sh/charts/orbit-platform
   oci://ghcr.io/orbit-sh/charts/orbit
   ```
4. Supply tenant config via your GitOps tooling — the operator is responsible
   for creating the `orbit-tenants` ConfigMap (or Secret) in the cluster before
   installing the `orbit` chart.
5. Use a separate deployment repository that references this repo's published
   charts. Do not fork or copy these charts into your deployment repo.

---

## Fixtures Disclaimer

Files in `deploy/local/fixtures/` are **not managed by Helm**. They are
applied with `kubectl apply` and are local-only reference implementations.

- They will not appear in `helm status orbit` or `helm history orbit`.
- They are not subject to `helm rollback`.
- They use local registry images (`orbit-registry:5001`) and `imagePullPolicy: Always`.

Do not copy fixtures directly into a production deployment without:
- Replacing the local registry reference with a versioned OCI image.
- Removing `imagePullPolicy: Always`.
- Adding appropriate resource limits, PodDisruptionBudgets, and HPA config.
