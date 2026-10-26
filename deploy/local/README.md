# Local Tenant Resources

Local-only tenant resources applied after the platform installation. Platform setup, image redeployment, and Helmfile environment values are documented in [docs/guides/local-development.md](../../docs/guides/local-development.md).

## Directory Structure

```
deploy/local/
├── connections/   # Connection resources for the example tenant (Kubernetes, GitHub)
├── fixtures/      # Example tenant workload
├── marketplace/   # SkillMarketplaceSource examples (applied by make cluster-deploy)
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

`fixtures/orbit-tenant-example.yaml` is the example tenant's workload: a self-contained `Deployment` and `Service` for local testing.

## Skill marketplace sources

`marketplace/` holds `SkillMarketplaceSource` examples for the example tenant. Each repository must also be on the tenant's `allowedMarketplaceRepos` in `tenants/example.yaml`, or the source stays `Pending`. See [Skill marketplace sources](../../docs/guides/skill-marketplace-sources.md).
