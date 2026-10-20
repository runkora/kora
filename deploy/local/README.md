# Local Tenant Resources

Local-only tenant resources applied after the platform installation. Platform setup, image redeployment, and Helmfile environment values are documented in [docs/guides/local-development.md](../../docs/guides/local-development.md).

## Directory Structure

```
deploy/local/
├── connections/   # Connection resources for the example tenant (Kubernetes, GitHub)
├── fixtures/      # Example tenant workload and skill marketplace
└── tenants/       # OrbitTenant resources
```

## Tenant Registry

The operator derives `orbit-tenants` in `orbit-system` from `OrbitTenant` resources.

## Adding or Updating a Tenant

Add or edit an `OrbitTenant` resource in `tenants/`, then apply it:

```bash
kubectl apply -f deploy/local/tenants/example.yaml
```

## Connections

`make cluster-deploy` applies each connection only when its prerequisite credential is available:

| Connection | Credential |
|---|---|
| `github.yaml` | GitHub App private key at `secrets/orbit-github-app.pem` |
| `kubernetes.yaml` | A `Secret` named `orbit-local-token` in `orbit-tenant-example`, holding a bearer token for the target cluster. `scripts/cluster/register-k8s-connection.sh` creates it, along with the ServiceAccount and the connection itself. |

A connection without its credential is skipped, not failed.

## Fixtures

`fixtures/orbit-tenant-example.yaml` contains the example tenant's workload and a `SkillMarketplaceSource` for local testing. `make cluster-deploy` applies the file after the operator creates the tenant namespace.

## Skill marketplace sources

The example source registers `clear-route/skills-marketplace` as `Clear Route Skills` in `orbit-tenant-example`, reading the `main` branch. The repository is approved in `tenants/example.yaml` under `spec.allowedMarketplaceRepos`. Each source needs that approval for its tenant; a source without it stays `Pending`.

Listing the marketplace does not require GitHub access. Browsing its skills requires the local platform GitHub App key at `secrets/orbit-github-app.pem`, with access to the repository.
