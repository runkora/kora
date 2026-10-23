# Local Tenant Resources

Local-only tenant resources applied after the platform installation. Platform setup, image redeployment, and Helmfile environment values are documented in [docs/contributing/local-development.md](../../docs/contributing/local-development.md).

## Directory Structure

```
deploy/local/
├── connections/   # Connection resources for the example tenant (Kubernetes, GitHub)
├── fixtures/      # Example tenant workload
└── tenants/       # KoraTenant resources
```

## Tenant Registry

The operator derives `kora-tenants` in `kora-system` from `KoraTenant` resources.

## Adding or Updating a Tenant

Add or edit an `KoraTenant` resource in `tenants/`, then apply it:

```bash
kubectl apply -f deploy/local/tenants/example.yaml
```

## Connections

`make cluster-deploy` applies each connection only when its prerequisite credential is available:

| Connection | Credential |
|---|---|
| `github.yaml` | GitHub App private key at `secrets/orbit-github-app.pem` |
| `kubernetes.yaml` | A `Secret` named `kora-local-token` in `kora-tenant-example`, holding a bearer token for the target cluster. `scripts/cluster/register-k8s-connection.sh` creates it, along with the ServiceAccount and the connection itself. |

A connection without its credential is skipped, not failed.

## Fixtures

`fixtures/kora-tenant-example.yaml` is the example tenant's workload: a self-contained `Deployment` and `Service` for local testing.
