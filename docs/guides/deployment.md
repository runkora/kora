# Deploying Orbit

Orbit is two Helm charts. `orbit-crds` installs the custom resources; `orbit` installs the operator, the cluster-scoped identity and gateway resources, and the platform services.

The `orbit` chart installs the operator alongside the platform services. Once running, the operator watches the Orbit CRDs and reconciles them into live tenants: it creates tenant namespaces, mints the per-tenant CA and OPA config, aggregates the registries, and serves the admission webhooks. 

## Installation paths

| Path | When |
|---|---|
| Reference Helmfile | A blank cluster. Installs SPIRE, Istio, and both Orbit charts in dependency order. |
| Direct Helm | You already run SPIRE and Istio. Install the two charts against your own values. |
| Argo CD | GitOps. An app-of-apps applies the same releases with sync waves. |

## Before you begin

Orbit installs the identity, policy, and connectivity layers that govern agents. Orbit is designed to run in a Kubernetes cluster with Istio and Spire.

### Kubernetes cluster

The cluster must provide:

- Cluster-admin access for the initial installation. The charts install CRDs, cluster roles, admission webhooks, and other cluster-scoped resources.
- A CNI that enforces Kubernetes `NetworkPolicy`. The `orbit` chart installs a default-deny policy in the system namespace and explicit rules for the mesh, gateway, Kubernetes API, DNS, and declared external services. Set `networkPolicy.enabled: false` only where the CNI cannot enforce them.
- A StorageClass for agent workspace volumes, either the cluster default or one named in `agents.workspace.storageClassName`. `ReadWriteMany` when sandboxes run on their own node pool.
- Working cluster DNS and a route from workloads to the Kubernetes API.
- A `LoadBalancer` implementation for the default gateway, or another way to expose the Istio gateway when using `NodePort` or `ClusterIP`.
- Pod Security Admission that admits the privileged SPIRE and Istio node components. The SPIFFE CSI driver and the Istio CNI must run privileged, whilst Orbit namespaces run at restricted.

See [connectivity](../architecture/connectivity.md) for the request path and [identity](../architecture/identity.md) for the SPIRE and Istio trust model.

### Istio

Orbit runs on Istio in sidecar mode and handles its own sidecar injection through an admission webhook. You must install Istio before installing Orbit, by following the [Istio install guide](https://istio.io/latest/docs/setup/install/) or the reference Helmfile in this repository. The mesh must provide:

- Sidecar injection on Orbit's namespaces, through `istio-injection: enabled` or an `istio.io/rev` label matching a live revision. Set the revision in the chart's `istio.revision` value when you run a revisioned control plane.
- `meshConfig.trustDomain` equal to Orbit's `global.trustDomain`.
- `pilot.cni.enabled: true` on istiod, with the Istio CNI DaemonSet running. The `restricted` profile of orbit's namespaces reject the `istio-init` container.

### SPIRE

Orbit registers workload identities into an existing SPIRE deployment. You must install SPIRE before installing Orbit, using the [spiffe/spire chart](https://github.com/spiffe/helm-charts-hardened), or by following the reference Helmfile in this repository. It must provide:

- A trust domain equal to Orbit's.
- The SPIFFE CSI driver `csi.spiffe.io` registered, and spire-controller-manager present with the `ClusterSPIFFEID` CRD served.
- The chart's default `ClusterSPIFFEID` disabled or set `fallback: true`, so it does not race Orbit's own registrations.
- Agent SDS enabled with `defaultSVIDName: default`, `defaultBundleName: ROOTCA`, `defaultAllBundlesName: ALL`.
- A controller-manager class matching the chart's `spire.className` value (`spire-system-spire` by default). A mismatch means Orbit's `ClusterSPIFFEID`s are ignored.

The reference Helmfile and the Argo CD example install a SPIRE the minimum spec required for Orbit.

The platform's SPIFFE trust domain is part of every workload identity Orbit issues and should be treated as permanent. It is important you choose this upfront before installation. Changing it later is an identity migration, not a routine update.

### Database, cache, and storage

PostgreSQL and Redis must be reachable from the system namespace. Orbit does not provision, back up, patch, or replicate either service.

The `orbit` chart applies its own schema. A `pre-install,pre-upgrade` hook Job (`orbit-migrate`) runs every pending migration before any workload rolls; Argo CD runs it as a `PreSync` hook. The Job connects with the services' credentials. That role needs `CREATE` on schema `public` and owns every table the migrations create. PostgreSQL 13 through 17 are supported. The Job runs without an Istio sidecar and needs the same network path to the database as the services.

- Use private endpoints and TLS in production.
- When either service sits outside the cluster network, add its CIDR and port to `networkPolicy.externalServices`. The default-deny policy otherwise blocks the connection.
- When the mesh restricts egress, set the value `istio.outboundTrafficPolicy: REGISTRY_ONLY`, and the orbit chart then renders `ServiceEntry` resources for the external hosts it knows about, including Postgres and Redis.

Each agent runs against its own workspace on a persistent volume. Orbit requests a `10Gi ReadWriteOnce` volume from your cluster's default StorageClass. Your cluster's StorageClass controls how the volume behaves, covering its expansion, retention and encryption policies. Set `agents.workspace.storageClassName`, `accessModes` and `size` to use a different class. Agent and sandbox pods run as uid 100 and set `fsGroup: 100`. If your cluster needs a different group for root-owned writable volumes, patch `spec.template.spec.securityContext.fsGroup` on the `StatefulSet` and `spec.securityContext.fsGroup` on the sandbox `Pod`.

### Sandbox node pools

Sandboxes are per-run and bursty, and they execute the agent's own code. Session-managers are long-lived and multiplex agent sessions. Giving sandboxes a node pool of their own keeps a heavy sandbox off the nodes serving concurrent live agents and sessions, and lets each pool be sized and scaled independently. It is not required to run sandboxes on separate nodes if your agents will not require heavy build or run activity, and instead use sandboxes as scratchpads. It is recommended to run sandboxes on separate nodes if your agents will do intensive work, where one run can saturate a node's CPU, memory or disk and slow sibling runs.

Place each workload with `agents.workloads.sessionManager` and `agents.workloads.sandbox`. Both take `nodeSelector`, `tolerations`, `affinity`, `topologySpreadConstraints` and `priorityClassName`:

```yaml
agents:
  workloads:
    sessionManager:
      nodeSelector: { workload-tier: orbit }
      tolerations:
        - { key: workload-tier, operator: Equal, value: orbit, effect: NoSchedule }
    sandbox:
      nodeSelector: { workload-tier: orbit-sandbox }
      tolerations:
        - { key: workload-tier, operator: Equal, value: orbit-sandbox, effect: NoSchedule }
  workspace:
    storageClassName: efs
    accessModes: [ReadWriteMany]
```

A sandbox mounts its session-manager's workspace volume, so the moment the two run on different nodes that volume must be `ReadWriteMany` on a network filesystem, as above. The chart refuses to render a split placement over a workspace that is not `ReadWriteMany`, and `orbit-agents` repeats the check at startup and when it provisions an agent type. The check compares node selectors and required node affinity; pod anti-affinity can separate pods without naming a pool and is not inspected.

On a `ReadWriteOnce` workspace, Orbit pins each sandbox to the node running the session-manager replica whose workspace it mounts, since matching node selectors alone leave a pool free to place them apart. A sandbox waits for room on that node rather than starting elsewhere and failing to attach.

When adopting this topology, you should do so before creating agents. A `StatefulSet`'s `volumeClaimTemplates` are immutable. An agent created against a `ReadWriteOnce` class keeps that volume even after the class changes. Migrating an existing agent requires moving its workspace data to a new claim, or deleting and recreating the agent.

#### Sandbox warming

When running on a dedicated pool that scales to zero, a new sandbox request has to wait for a node to boot and the image to pull. Orbit provides an out of the box sandbox warmer that provisions a low-priority placeholder pod, holding a node open with the sandbox image already cached.

Warming is off by default. Enable it in the chart values:

```yaml
agents:
  sandboxWarming:
    mode: capacity
    replicas: 1
    priority: -10          # below real agent workloads
    resources:             # reserved per replica
      requests: { cpu: "1", memory: 4Gi }
      limits: { cpu: "1", memory: 4Gi }
```

Placement of sandbox warmers follows `agents.workloads.sandbox`.

`resources` is the capacity each placeholder reserves on a node. Leave at default, or raise to hold a larger warm node for a heavier sandbox image.

`replicas` is how many warm nodes you need.

`mode: image` instead runs a DaemonSet that caches the image on node pools that do not scale to zero.

### Pod metadata and cloud identity for agents

`agents.workloads.common.podLabels` and `podAnnotations` reach both provisioned pods, for cost allocation, network policy selection, backup exclusion or an admission controller of your own. The per-workload blocks take the same keys and win where both set one.

Orbit owns the labels identifying a pod to SPIRE, the sidecar injector and its StatefulSet, and the annotations wiring it into the mesh. Setting one of those fails `orbit-agents` at startup naming the key, rather than silently moving a pod off its attested identity.

`agents.workloads.sessionManager.serviceAccount.annotations` annotates the ServiceAccount provisioned for each agent, which is where cloud identity goes:

```yaml
agents:
  workloads:
    sessionManager:
      serviceAccount:
        annotations:
          eks.amazonaws.com/role-arn: arn:aws:iam::123456789012:role/orbit-agent
```

Sandbox pods run under the tenant namespace's `default` ServiceAccount and never receive a cloud identity. That is deliberate: the code in a sandbox is the agent's, and it reaches external systems through Connections and the egress gateway, which mint scoped short-lived credentials per request and attribute them to the run.

Everything in `agents.workloads` and `agents.workspace` applies before manifest patches.

### DNS and TLS

Choose a hostname for your dashboard, for example `orbit.example.com`. Orbit derives the API hostname by adding the `api.` prefix:

- `orbit.example.com` serves the dashboard and BFF (`frontendHost`).
- `api.orbit.example.com` serves the platform APIs, inference API, MCP endpoints, and OAuth discovery documents.

Create DNS records for both names resolving to the same Istio gateway, fronted by a cloud `LoadBalancer`.

The gateway certificate must be valid for both names. Provision a `kubernetes.io/tls` Secret in the gateway's namespace, directly or through cert-manager, and set `gateway.tls.credentialName` to it. Orbit terminates public TLS at the Istio gateway. Inside the mesh, service-to-service mTLS uses SPIRE identities.

Register `https://orbit.example.com/auth/callback` as an allowed redirect URI for the OIDC client. Orbit's services must be able to reach the issuer and its discovery endpoints from the system namespace.

### Artifact access

Charts and images are currently private. Run `helm registry login ghcr.io` to pull the charts. The cluster pulls the images with a `ghcr.io` pull Secret configured by `global.imagePullSecrets`. The operator copies the pull Secret into every tenant namespace for workloads to pull from.

To pull from your own registry mirror, set `global.imageRegistry` and name a pull Secret in `global.imagePullSecrets`. 

### OIDC federation

Each tenant can have an isolated OIDC client. This allows for custom tenancy and routing between providers or email domains. Confidential clients require a Kubernetes Secret holding the OIDC client secret. It is not required to run multiple tenants, a single tenant is enough if you do not need separation between workloads.

## Provide platform secrets

Platform credentials and signing keys come from an external secret manager. [Secrets](../configuration/secrets.md) describes the ownership and projection model.

Platform credentials must be projected into Kubernetes Secrets in the system namespace before installing the `orbit` chart.

The platform services read these keys from `orbit-secrets`, which can be customised using `config.platformSecretName`:

| Key | Purpose |
|---|---|
| `DB_PASSWORD` | PostgreSQL password. |
| `REDIS_PASSWORD` | Redis password. |
| `ORBIT_TOKEN_SIGNING_KEY` | RSA private key used to sign platform tokens. |
| `ORBIT_TRIGGERS_ENCRYPTION_KEY` | Base64-encoded 32-byte key used to encrypt stored trigger credentials. |

The dashboard reads `BFF_SESSION_SECRET` from a separate secret, `orbit-frontend-secrets` by default. This can be configured via `frontend.existingSecret`. You must use at least 32 random characters.

Your secret manager owns rotation. The Orbit operator does not watch these Secrets, and only picks up a rotated Connection or OIDC client secret on its next periodic reconcile.

## Configure the chart

The example at `deploy/reference/values/orbit.yaml` can be copied directly into your repository, a full list of values can be found in the chart schema. The fields most installs will set:

```yaml
global:
  trustDomain: orbit.example.com      # required; equal to Istio and SPIRE
  # systemNamespace defaults to the install namespace
  imageRegistry: ghcr.io/clear-route

frontendHost: orbit.example.com       # required; api.<frontendHost> is the API host

config:
  db:
    host: postgres.example.internal
    port: 5432
    name: orbit
    user: orbit
  redis:
    host: redis.example.internal
    port: 6379
  platformSecretName: orbit-secrets

networkPolicy:
  enabled: true
  externalServices:         # only when Postgres or Redis is off-cluster
    - cidr: 10.20.0.0/24
      ports: [5432, 6379]

gateway:
  create: true
  tls:
    enabled: true
    credentialName: orbit-gateway-tls  # kubernetes.io/tls Secret in the gateway namespace
ingressGateway:
  service:
    type: LoadBalancer
```

`global` holds the values every service shares: trust domain, image registry and tag, log format, Postgres SSL mode, and OTLP settings. A component can override any of them.

Image tags resolve as follows:

| `global.imageTag` | Result |
|---|---|
| Empty (default) | The chart's `appVersion`. A release stamps per-component digests. |
| A tag | Every Orbit image uses it.  |

Use the narrowest CIDRs and ports in `networkPolicy.externalServices` that cover your services. Omit the entry when Postgres and Redis are reached through destinations your cluster network policy already allows.

## Telemetry

Orbit exports traces and metrics over OTLP, configure your endpoint as follows:

```yaml
global:
  observability:
    otlpEndpoint: http://otel-collector.observability.svc:4317
    collectorNamespace: observability
    samplingRate: "1.0"
```

`collectorNamespace` opens the egress on the endpoint's port. Without it the default-deny policy drops the traffic. For a collector outside the cluster, leave it empty and add the collector's CIDR and port to `networkPolicy.externalServices`. Agent pods export to the same endpoint, providing telemetry for their run, and only when `collectorNamespace` names the in-cluster collector: their egress policy admits no other collector ([agent pod egress lockdown](../adr/2026-09-30-agent-pod-egress-lockdown.md)).

Point your service mesh's own telemetry at the same collector to get the mesh hop and the application span in one trace.

## Run behind an existing gateway

By default Orbit creates its own ingress gateway. Set `gateway.create: false` to front Orbit with an Istio gateway you already operate. Orbit then renders no `Gateway` resource, and binds its `VirtualService`s to the provided gateway.

There are four points of configuration documented in `deploy/reference/values/gateway-byo.yaml`:

 1. Chart values: `gateway.create: false`, with `gateway.name` and `gateway.byo` pointing at your gateway.
 2. A `Gateway` resource on your gateway for the two public hosts.
 3. A `ClusterSPIFFEID` registering your gateway's pods.
 4. SPIRE wiring in your gateway's own values, pointing its Envoy at SPIRE for certificates instead of istiod.

Points 3 and 4 give your gateway a SPIRE identity, required to interact with Orbit's own pods. See [identity](../architecture/identity.md) and [runtime topology](../architecture/runtime-topology.md) for a detailed look at the trust model.

## Install

### Reference Helmfile (blank cluster)

The reference Helmfile in `deploy/reference/` installs SPIRE, Istio, and the two Orbit charts in dependency order. 

Copy `deploy/reference/` into your own repository, and edit the two files: `environments/example.yaml` (or whatever you name it) for the cluster facts, and `values/orbit.yaml` for the chart config.

To apply from your local copy:

```bash
helmfile -e example apply
```

If SPIRE and Istio are already running in your cluster, you can skip the helmfile and use helm directly for the Orbit charts.

### Direct Helm (existing mesh)

With SPIRE and Istio already running, you install the two Orbit charts directly. First label the system namespace, then install `orbit-crds`, and then `orbit`.

```bash
kubectl create namespace orbit-system
kubectl label namespace orbit-system \
  istio-injection=enabled \
  pod-security.kubernetes.io/enforce=restricted \
  pod-security.kubernetes.io/warn=restricted \
  pod-security.kubernetes.io/audit=restricted

helm upgrade --install orbit-crds oci://ghcr.io/clear-route/charts/orbit-crds \
  --namespace orbit-system

helm upgrade --install orbit oci://ghcr.io/clear-route/charts/orbit \
  --namespace orbit-system \
  --values orbit.yaml
```

Pin `--version` on both to install a specific release rather than the latest.
Your `orbit.yaml` must also set `global.trustDomain`, which the Helmfile injected for you but helm does not. 

### Argo CD (GitOps)

`deploy/reference/argocd/app-of-apps.yaml` is an app-of-apps example: one `Application` per release which is ordered by `argocd.argoproj.io/sync-wave`. Copy it into your own GitOps repository and edit the values:

- Point the `orbit` Application's second source at your own values repository.
- Configure OCI registry credentials (`argocd repo add`) if required.

The `orbit` Application includes the settings that stop ArgoCD and the operator fighting over the same resources:

| Setting | Purpose |
|---|---|
| `managedNamespaceMetadata` | Labels the system namespace (`istio-injection`, PSA `restricted`). |
| `ignoreDifferences` | Fields the operator writes on objects Argo CD does not own: the webhook `caBundle` and `failurePolicy`. |
| `prune: false` on `orbit-crds` | Stops ArgoCD from pruning the Orbit CRDs. Removing one cascade-deletes every tenant, connection, etc. of that kind across the cluster.  |

The app-of-apps installs SPIRE, Istio, and the Orbit charts. Add your `OrbitTenant`, `Connection`, `OAuthClient`, and `MCPServer` CRs as a separate `Application` with a sync wave after `orbit-crds`. This ensures their `prune: true` is independent of the `orbit-crds` `prune: false`. 

## Verify the installation

Check the platform workloads and the public endpoints:

```bash
kubectl get pods -n orbit-system
curl -I https://orbit.example.com
curl -I https://api.orbit.example.com/health
```

Every pod should be running and both endpoints should respond. At this stage, you should have a working install with no tenants registered yet. 

## Configure the platform

Now that the platform is running, you can configure it through the following guides:

- [Tenants](tenants.md): onboard a tenant so its team can sign in and get their own workspace.
- [Model providers](model-providers.md): connect the platform to upstream inference providers.
- [Connections](connections.md): register external services agents can reach without holding credentials.
- [OAuth clients](oauth-clients.md): configure clients that authenticate users against Orbit.
- [MCP servers](mcp-servers.md): give agents and humans governed access to a set of tools.
- [Policy and roles](policy-and-roles.md): decide what each person and agent is allowed to do.

## Upgrade

Upgrade `orbit-crds` before `orbit`, on every upgrade.

```bash
helm upgrade orbit-crds oci://ghcr.io/clear-route/charts/orbit-crds \
  --namespace orbit-system --version <target>
helm upgrade orbit oci://ghcr.io/clear-route/charts/orbit \
  --namespace orbit-system --version <target> --values orbit.yaml
```

- Under GitOps, bump the chart `targetRevision` on both Applications in the same order.
- Skipping minors migrates the schema in one pass. Pods from the old release may return errors between the migration hook completing and the rollout finishing, and rollback is supported to the immediately preceding revision only.
- Review the release notes for trust-domain, CRD, database migration, and identity-provider changes before upgrading.
- Do not change `trustDomain` as part of a routine upgrade.

Rollback the `orbit` release with `helm rollback orbit <previous revision>`, or revert the release revision under GitOps. This reverts the workloads only, not the database or the CRDs. Migrations are expand-contract: the schema at release N works with the binaries of release N-1, and the rolled-back release never runs a `Down` migration.

A failed migration fails the upgrade before any workload changes. Under Helm the failed Job is kept for inspection; under Argo CD the next retry replaces it and the failure is visible in the sync history.

## References

- [Install and distribution model](../adr/2026-09-04-install-and-distribution-model.md): why Orbit ships as two charts and the operator manages only tenants.
- [Enforcement sidecar injection and preflight](../adr/2026-09-04-enforcement-sidecar-injection-and-preflight.md): how Orbit injects its sidecars and runs preflight on the mesh before install.
- [Service mesh and network enforcement](../adr/2026-03-05-service-mesh-and-network-enforcement-infrastructure.md): the mesh and NetworkPolicy enforcement the platform runs on.
- [Manifest patches](../adr/2026-09-15-manifest-patches-configmap.md): how operators patch the resources orbit-agents provisions in tenant namespaces.
