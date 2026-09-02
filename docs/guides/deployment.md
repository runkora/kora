# Deploying Orbit

Orbit is deployed through its Kubernetes operator. The operator is the only component installed directly. An `OrbitPlatform` resource then declares the platform, and an `OrbitTenant` resource declares each tenant.

The operator reconciles the installation in dependency order: SPIRE, Istio, the ingress gateway, cluster-wide Orbit resources, and the Orbit services. It also creates tenant namespaces and keeps the identity service's tenant registry in sync.

## Before you begin

Orbit installs the identity, policy, and connectivity layers that govern agents. It does not create the cluster, data stores, public DNS, or external identity provider. Prepare those dependencies before installing the operator.

### Kubernetes cluster

The cluster must provide:

- Cluster-admin access for the initial installation. The operator installs CRDs, cluster roles, admission webhooks, namespaces, and other cluster-scoped resources.
- A CNI that enforces Kubernetes `NetworkPolicy`. Orbit installs a default-deny policy in `orbit-system` and explicit rules for the mesh, gateway, Kubernetes API, DNS, and declared external services. Disabling those policies is suitable only for development environments where the CNI cannot enforce them.
- A default `ReadWriteOnce` StorageClass, or an equivalent storage-class override, for agent workspace volumes.
- Working cluster DNS and a route from workloads to the Kubernetes API.
- A `LoadBalancer` implementation for the default gateway configuration, or another way to expose the Istio gateway when using `NodePort` or `ClusterIP`.
- Admission and pod-security settings that allow the pinned SPIRE and Istio components, including the SPIRE agent and CSI driver, to run in their own namespaces.

Orbit's NetworkPolicies are part of its defence in depth. The mesh remains responsible for workload identity and policy enforcement, but a production cluster should not silently ignore the network layer. See [connectivity](../architecture/connectivity.md) for the request path and [identity](../architecture/identity.md) for the SPIRE and Istio trust model.

### Database, cache, and storage

PostgreSQL and Redis must be reachable from `orbit-system`. Orbit manages its schema migrations, but it does not provision, back up, patch, or replicate either service.

Use private endpoints and TLS in production. When either service sits outside the cluster network, add its CIDR and port to `platformValues.networkPolicy.externalServices`. The default-deny policy otherwise blocks the connection.

Managed services whose addresses change need a stable private CIDR, a cluster egress path with stable addresses, or CNI-specific policy outside the Orbit chart. The built-in setting uses standard Kubernetes `ipBlock` rules and therefore accepts CIDRs rather than DNS names.

Agent workspaces use persistent volumes. Confirm the default StorageClass has the capacity, topology, expansion, retention, and encryption behaviour you expect before allowing agents to run.

### DNS and TLS

Choose one dashboard hostname, for example `orbit.example.com`. Orbit derives the API hostname by adding the `api.` label:

- `orbit.example.com` serves the dashboard and BFF.
- `api.orbit.example.com` serves the platform APIs, inference API, MCP endpoints, and OAuth discovery documents.

Create DNS records for both names that resolve to the same Istio gateway. With a cloud `LoadBalancer`, they normally point to the load balancer's address or hostname. If the gateway is private, publish both records in the private DNS zone used by operators and agent clients.

The gateway certificate must be valid for both names. Provision a `kubernetes.io/tls` Secret in `istio-system`, either directly or through cert-manager, and set `platformValues.gateway.tls.credentialName` to its name. Orbit terminates public TLS at the Istio gateway. Service-to-service mTLS uses SPIRE identities and is configured separately by the operator.

Register `https://orbit.example.com/auth/callback` as an allowed redirect URI for the OIDC client. The issuer and its discovery endpoints must be reachable from `orbit-system`.

### Artifact access

The machine running Helm must be able to pull the operator chart. The cluster must also be able to pull the operator image before reconciliation can begin.

After startup, the operator uses the Orbit charts bundled in its image and fetches the pinned SPIRE and Istio charts over HTTPS. Cluster nodes then pull the images referenced by those charts and the Orbit release. In a restricted network, allow outbound access to the release registries and upstream chart repositories, or mirror every chart and image into registries the cluster can reach.

Private registries also need a pull-secret strategy for `orbit-operator`, `orbit-system`, and the tenant namespaces where agent workloads run. Decide how those namespaces and service accounts receive `imagePullSecrets` before setting `imageRegistry` to a private mirror.

### Identity and secrets

Provide an OIDC client for each tenant and decide which email domains route to it. Confidential clients need a Kubernetes Secret containing the client secret. Public clients can omit it.

Choose the platform's SPIFFE trust domain before installation. It becomes part of every workload identity issued by Orbit and should be treated as permanent. Changing it is an identity migration, not a routine configuration update.

Platform credentials and signing keys must come from an external secret-management system. [Secrets](../configuration/secrets.md) describes the ownership and projection model.

## Install the operator

Install the operator chart into its own namespace:

```bash
helm upgrade --install orbit-operator \
  oci://ghcr.io/clear-route/charts/orbit-operator \
  --version <version> \
  --namespace orbit-operator \
  --create-namespace
```

The chart installs the Orbit CRDs, service account, RBAC, and operator deployment. The operator itself is not injected into the mesh because it is responsible for installing the mesh.

Verify that it is ready before applying the platform resource:

```bash
kubectl rollout status deployment/orbit-operator -n orbit-operator
kubectl get crd orbitplatforms.orbit.io orbittenants.orbit.io
```

## Provide platform secrets

Project credentials into Kubernetes Secrets in the `orbit-system` namespace. The operator creates the namespace during initial reconciliation and continues once the referenced Secrets are available. In production, use your existing secret manager and a controller such as External Secrets Operator rather than committing Secret manifests.

The platform services read the following keys from `orbit-secrets`:

| Key | Purpose |
|---|---|
| `DB_PASSWORD` | PostgreSQL password. |
| `REDIS_PASSWORD` | Redis password. |
| `ORBIT_TOKEN_SIGNING_KEY` | RSA private key used to sign platform tokens. |
| `ORBIT_TRIGGERS_ENCRYPTION_KEY` | Base64-encoded 32-byte key used to encrypt stored trigger credentials. |

The dashboard reads `BFF_SESSION_SECRET` from `orbit-frontend-secrets`. Use at least 32 random characters.

The operator reads these Secrets but does not create or rotate them. Rotation remains the responsibility of the system that owns the source secret.

## Define the platform

`OrbitPlatform` is the desired state of the installation. The example below uses an operator-managed SPIRE and Istio deployment, a cloud load balancer, and externally managed PostgreSQL and Redis services.

```yaml
apiVersion: orbit.io/v1alpha1
kind: OrbitPlatform
metadata:
  name: orbit
spec:
  version: "0.1.0"
  trustDomain: orbit.example.com
  clusterName: production

  spire:
    enabled: true
  istio:
    enabled: true
  gateway:
    serviceType: LoadBalancer

  secrets:
    platformSecretRef:
      name: orbit-secrets
      namespace: orbit-system

  config:
    db:
      host: postgres.example.internal
      port: 5432
      name: orbit
      user: orbit
    redis:
      host: redis.example.internal
      port: 6379

  platformValues:
    frontendHost: orbit.example.com
    networkPolicy:
      enabled: true
      externalServices:
        - cidr: 10.20.0.0/24
          ports:
            - 5432
            - 6379
    gateway:
      tls:
        enabled: true
        credentialName: orbit-gateway-tls

  orbitValues:
    frontendHost: orbit.example.com
    publicHosts:
      - orbit.example.com
      - api.orbit.example.com

  upgradeStrategy:
    mode: Explicit
```

Replace `spec.version` with the Orbit release you installed. Leave `imageRegistry` unset to use the images associated with that release. Set it only when the images are mirrored into another registry.

The dashboard is served from `frontendHost`. The backend API uses the corresponding `api.` host. Provision the TLS Secret named by `credentialName` in `istio-system`, either through cert-manager or your secret-management controller.

The `externalServices` entry permits `orbit-system` workloads to reach PostgreSQL and Redis on the example private CIDR. Use the narrowest CIDRs and ports that cover your services. Omit the entry when both services are reached through destinations already allowed by your cluster network policy.

Apply the resource and follow reconciliation through its status:

```bash
kubectl apply -f orbit-platform.yaml
kubectl get orbitplatform orbit -w
```

The release process applies pending database migrations before rolling the application services forward. Treat a migration failure as a failed platform reconciliation. Do not bypass it by starting services against an older schema.

The `OrbitPlatform` conditions show which dependency is still installing or which release failed. The operator logs contain the corresponding Helm error:

```bash
kubectl describe orbitplatform orbit
kubectl logs deployment/orbit-operator -n orbit-operator -c operator
```

## Register a tenant

An `OrbitTenant` binds an email domain and OIDC client to a tenant namespace. The operator creates the namespace with the required SPIRE and Istio labels, then publishes the tenant to the identity service.

```yaml
apiVersion: orbit.io/v1alpha1
kind: OrbitTenant
metadata:
  name: example
spec:
  displayName: Example Organisation
  active: true

  oidc:
    issuerURL: https://idp.example.com/oauth2/default
    clientID: orbit-production
    clientSecretRef:
      name: orbit-example-oidc
      namespace: orbit-system
    scopes:
      - openid
      - profile
      - email
      - groups
    emailDomains:
      - example.com

  deleteNamespaceOnRemoval: false
```

For a confidential OIDC client, the referenced Secret must contain the client secret under the key `clientSecret`. Public clients can omit `clientSecretRef`.

Apply the tenant and wait for it to become ready:

```bash
kubectl apply -f orbit-tenant-example.yaml
kubectl get orbittenant example -w
kubectl get namespace orbit-tenant-example
```

Keep `deleteNamespaceOnRemoval` false unless deleting the `OrbitTenant` should also delete every workload and resource in its namespace.

## Verify the installation

Once the platform and tenant report `Ready`, check the workloads and the public endpoints:

```bash
kubectl get orbitplatform,orbittenant
kubectl get pods -n orbit-system
kubectl get pods -n orbit-tenant-example
curl -I https://orbit.example.com
curl -I https://api.orbit.example.com/health
```

Authentication and policy are separate checks. A healthy endpoint proves that ingress is available. It does not prove that the OIDC client, tenant routing, or grants are correct. Configure initial access using [policy and roles](policy-and-roles.md).

## Upgrade Orbit

With the explicit upgrade strategy, change `spec.version` to the target release and apply the resource again. The operator advances the managed releases in dependency order and records progress in `status.conditions`.

Review the release notes before changing versions. In particular, check for trust-domain, CRD, database migration, and identity-provider changes. Do not change `trustDomain` as part of a routine upgrade.

## Use an existing Istio or SPIRE installation

Set `istio.enabled: false` or `spire.enabled: false` when the cluster already provides that component. Orbit still expects the same runtime contract:

- SPIRE issues SVIDs in the `OrbitPlatform` trust domain and exposes the workload API through the CSI driver.
- Istio uses those SVIDs for workload mTLS and applies the Orbit gateway and policy configuration.

Disabling installation transfers lifecycle ownership. It does not remove Orbit's dependency on workload identity or mesh enforcement. See [identity](../architecture/identity.md) and [runtime topology](../architecture/runtime-topology.md) before integrating an existing mesh.
