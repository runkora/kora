# Install runkora on Kubernetes

runkora installs as two Helm charts. `kora-crds` installs the custom resources, and `kora` installs the operator, the identity and gateway resources, and the platform services.

To try kora on a laptop first, run `make up` in [runkora/examples](https://github.com/runkora/examples).

## Before you begin

Complete [Prepare your cluster](prepare-your-cluster.md) first.

## Create the platform secrets

The chart creates no Secrets. Create the `kora-system` namespace and the platform Secrets before you install:

```bash
kubectl create namespace kora-system
kubectl label namespace kora-system \
  istio-injection=enabled \
  pod-security.kubernetes.io/enforce=restricted \
  pod-security.kubernetes.io/warn=restricted \
  pod-security.kubernetes.io/audit=restricted

openssl genrsa -out signing.key 4096

kubectl -n kora-system create secret generic kora-secrets \
  --from-literal=DB_PASSWORD='<postgres password>' \
  --from-literal=REDIS_PASSWORD='<redis password>' \
  --from-file=KORA_TOKEN_SIGNING_KEY=signing.key \
  --from-literal=KORA_TRIGGERS_ENCRYPTION_KEY="$(openssl rand -base64 32)"

kubectl -n kora-system create secret generic kora-frontend-secrets \
  --from-literal=BFF_SESSION_SECRET="$(openssl rand -hex 32)"

# Only when the kora gateway serves HTTPS (gateway.tls.enabled).
# Skip it if cert-manager creates kora-gateway-tls.
kubectl -n kora-system create secret tls kora-gateway-tls \
  --cert=tls.crt --key=tls.key
```

| Key | Holds |
| --- | --- |
| `DB_PASSWORD` | The PostgreSQL password |
| `REDIS_PASSWORD` | The Redis password |
| `KORA_TOKEN_SIGNING_KEY` | The RSA private key that signs platform tokens |
| `KORA_TRIGGERS_ENCRYPTION_KEY` | A base64-encoded 32-byte key that encrypts stored trigger credentials |
| `BFF_SESSION_SECRET` | At least 32 random characters for the dashboard session |

In production, project these Secrets from your secret manager. See [Secrets](../reference/secrets.md).

## Set the chart values

Create `kora.yaml` with at least these values:

```yaml
global:
  trustDomain: example.com      # the same trust domain as Istio and SPIRE

frontendHost: example.com       # the API is served on api.<frontendHost>

config:
  db:
    host: postgres.example.internal
  redis:
    host: redis.example.internal

gateway:
  tls:
    enabled: true
```

`helm show values oci://ghcr.io/runkora/charts/kora` lists every value and its default.

### Database and cache

kora connects to Postgres and Redis over TLS. For a service that serves plaintext, set `global.postgres.sslMode: disable` or `config.redis.tls.enabled: false`. When the Redis certificate comes from a private CA, store the CA certificate under the key `ca.crt` in a Secret in `kora-system`, and set `config.redis.tls.caSecret` to its name.

The default-deny network policy blocks Postgres and Redis until you list them under `networkPolicy.externalServices`. For a service inside the cluster, use the CIDR of the pods it runs in:

```yaml
networkPolicy:
  externalServices:
    - cidr: 10.0.0.0/24
      ports:
        - 5432
        - 6379
```

### TLS

With `gateway.tls.enabled`, the gateway serves HTTPS with the `kora-gateway-tls` Secret. Set `gateway.tls.credentialName` to use another Secret.

To terminate HTTPS in front of the gateway, leave `gateway.tls.enabled` off. For a Kubernetes Ingress such as Traefik or an ALB, also set `gateway.ingress.enabled: true`. The chart cannot enable an Ingress and `gateway.tls.enabled` together.

### Service configuration

To change a service setting that has no chart value, set it under the component's `configOverrides`. The first key is the config module: the Go package that defines the setting.

This switches policy-service logs to console format:

```yaml
policyService:
  configOverrides:
    internal/platform/observability:
      logFormat: console
```

A service with an unknown or mistyped key fails at startup and logs the key. Use a component's `extraEnv` for secrets only.

### Marketplace sources

Tenants register their own marketplaces with [skill marketplace sources](skill-marketplace-sources.md), which a platform administrator [approves on the `KoraTenant`](tenants.md#approving-skill-marketplace-repositories). Alternatively, set the Marketplace repositories in the release values. Each tenant ID must match the `metadata.name` of a `KoraTenant` resource, which the operator publishes as the platform tenant ID:

```yaml
skillsService:
  skillsMapping:
    tenants:
      - id: example
        marketplaces:
          - https://github.com/your-org/your-skills
```

The chart writes this mapping to `kora-skill-marketplace-sources` as `skills-mapping.yaml`, mounted at `/etc/kora/marketplace/skills-mapping.yaml`. An empty tenant list leaves the key out, so the chart creates an empty ConfigMap and the operator can render `skills-mapping.yaml` from `SkillMarketplaceSource` resources; skills-service treats a missing file as an empty registry.

Listing tenants here hands the file to the chart. The chart sets the annotation `runkora.io/skills-mapping-from-chart: "true"` on the ConfigMap, and the operator stops writing `skills-mapping.yaml`. Sources the operator would otherwise serve stay `Pending` with reason `MappingManagedByChart`. The chart's list is served as written: nothing checks it against any tenant's `allowedMarketplaceRepos`, so review it like an allowlist change. To hand the file back to the operator, empty the list and upgrade the release. The operator renders the sources the next time it reconciles a tenant, within five minutes.

The first Secret named in `skillsService.githubApps` must contain `GITHUB_APP_ID`, `GITHUB_APP_INSTALLATION_ID`, and `private-key.pem`. The service reads and validates the private key at startup; restart it after rotating that key.

The registry loads the file at startup and watches its directory for updates. Direct ConfigMap edits are reloaded after Kubernetes updates the mounted file; malformed YAML keeps the previous sources. Changing the mapping through a Helm upgrade also changes the pod-template checksum and rolls skills-service. Source declarations remain scoped to the authenticated caller's tenant; requests still require `marketplace:read`.

## Run behind an existing gateway

kora creates its own ingress gateway by default. To use an Istio gateway you already run, set `gateway.create: false`, and kora binds its `VirtualService`s to your gateway:

```yaml
gateway:
  create: false
  name: istio-system/istio-ingressgateway
  byo:
    namespace: istio-system
    selector:
      app: istio-ingressgateway
```

- `name`: the gateway as `<namespace>/<Gateway name>`.
- `byo.namespace` and `byo.selector`: the namespace and labels of the gateway's pods.

Your gateway must also:

- Serve both public hosts with the `kora-gateway-tls` Secret in its own namespace.
- Have its pods registered by a `ClusterSPIFFEID` that uses kora's `spire.className` and issues `spiffe://<trust domain>/system/ingress-gateway`.
- Take its certificates from SPIRE. Merge the SPIRE settings in [`gateway-byo.yaml`](https://github.com/runkora/kora/blob/main/deploy/reference/values/gateway-byo.yaml) into the gateway chart's values.

## Install

Install `kora-crds` first, then `kora`:

```bash
helm upgrade --install kora-crds oci://ghcr.io/runkora/charts/kora-crds \
  --namespace kora-system

helm upgrade --install kora oci://ghcr.io/runkora/charts/kora \
  --namespace kora-system \
  --values kora.yaml
```

Pin `--version` on both charts to install a specific release.

Before any service starts, Helm runs the `kora-migrate` Job to apply the database schema. If the Job fails, read its output with `kubectl -n kora-system logs job/kora-migrate`, fix the database access, and run the install again.

## Verify a release

Each release signs its charts and images with keyless cosign. Verify a chart with cosign 3 or later:

```bash
cosign verify ghcr.io/runkora/charts/kora:<version> \
  --certificate-identity-regexp '^https://github\.com/runkora/kora/\.github/workflows/release\.yml@refs/tags/v' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com
```

Verify `kora-crds` and the images under `ghcr.io/runkora/` the same way. The GitHub release also attaches a Sigstore bundle and SLSA build provenance for each file.

## Verify the installation

```bash
kubectl get pods -n kora-system
curl -I https://example.com
curl -I https://api.example.com/health
```

Every pod should be running and both endpoints should respond. `kubectl -n kora-system describe pod <name>` shows why a pod is not ready.

The operator checks the cluster prerequisites periodically and logs each result in `kubectl -n kora-system logs deployment/kora-operator`. Run the check on demand with `helm test kora -n kora-system`.

## Next steps

- [Onboard a tenant](tenants.md) so a team can sign in.
- [Register an inference provider](register-inference-provider.md) so agents can use models.
- [Create an agent](create-an-agent.md) in the tenant.

## References

- [Install and distribution model](../adr/2026-09-04-install-and-distribution-model.md): why kora ships as two charts.
- [Enforcement sidecar injection and preflight](../adr/2026-09-04-enforcement-sidecar-injection-and-preflight.md): sidecar injection and the prerequisites check.
- [Rendered service config files](../adr/2026-10-06-rendered-service-config-files.md): service config and `configOverrides`.
- [Service mesh and network enforcement](../adr/2026-03-05-service-mesh-and-network-enforcement-infrastructure.md): the mesh and network policies the platform runs on.
