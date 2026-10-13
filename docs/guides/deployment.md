# Deploying kora

kora is two Helm charts. `orbit-crds` installs the custom resources; `orbit` installs the operator, the cluster-scoped identity and gateway resources, and the platform services.

The `orbit` chart installs the operator alongside the platform services. Once running, the operator watches the kora CRDs and reconciles them into live tenants: it creates tenant namespaces, mints the per-tenant CA and OPA config, aggregates the registries, and serves the admission webhooks. 

## Installation paths

| Path | When |
|---|---|
| Reference Helmfile | A blank cluster. Installs SPIRE, Istio, and both kora charts in dependency order. |
| Direct Helm | You already run SPIRE and Istio. Install the two charts against your own values. |
| Argo CD | GitOps. An app-of-apps applies the same releases with sync waves. |

## Before you begin

kora installs the identity, policy, and connectivity layers that govern agents. kora is designed to run in a Kubernetes cluster with Istio and Spire.

### Kubernetes cluster

kora is tested on Kubernetes 1.34 to 1.36. The charts require 1.34 or later.

The cluster must provide:

- Cluster-admin access for the initial installation. The charts install CRDs, cluster roles, admission webhooks, and other cluster-scoped resources.
- A CNI that enforces Kubernetes `NetworkPolicy`. The `orbit` chart installs a default-deny policy in the system namespace and explicit rules for the mesh, gateway, Kubernetes API, DNS, and declared external services. Set `networkPolicy.enabled: false` only where the CNI cannot enforce them.
- A StorageClass for agent workspace volumes, either the cluster default or one named in `agents.workspace.storageClassName`. `ReadWriteMany` when sandboxes run on their own node pool.
- Working cluster DNS and a route from workloads to the Kubernetes API.
- A `LoadBalancer` implementation for the default gateway, or another way to expose the Istio gateway when using `NodePort` or `ClusterIP`.
- Pod Security Admission that admits the privileged SPIRE and Istio node components. The SPIFFE CSI driver and the Istio CNI must run privileged, whilst kora namespaces run at restricted.
- Nodes on Kubernetes 1.36 or later with containerd 2.1 or CRI-O 1.31 or later, only when a [theme](#white-labelling-theme) is configured. The theme bundle is mounted as an image volume. The chart's own floor stays 1.34, and nothing checks this at render time.

See [connectivity](../architecture/connectivity.md) for the request path and [identity](../architecture/identity.md) for the SPIRE and Istio trust model.

### Istio

kora runs on Istio in sidecar mode and handles its own sidecar injection through an admission webhook. You must install Istio before installing kora, by following the [Istio install guide](https://istio.io/latest/docs/setup/install/) or the reference Helmfile in this repository. kora supports Istio 1.30 and 1.31. The reference install pins 1.31.1. The operator's preflight rejects any other Istio version. The mesh must provide:

- Sidecar injection on kora's namespaces, through `istio-injection: enabled` or an `istio.io/rev` label matching a live revision. Set the revision in the chart's `istio.revision` value when you run a revisioned control plane.
- `meshConfig.trustDomain` equal to kora's `global.trustDomain`.
- `pilot.cni.enabled: true` on istiod, with the Istio CNI DaemonSet running. The `restricted` profile of orbit's namespaces reject the `istio-init` container.

### SPIRE

kora registers workload identities into an existing SPIRE deployment. You must install SPIRE before installing kora, using the [spiffe/spire chart](https://github.com/spiffe/helm-charts-hardened), or by following the reference Helmfile in this repository. The reference install pins the `spire` chart 0.30.2 (SPIRE 1.15.3) and `spire-crds` 0.6.1. It must provide:

- A trust domain equal to kora's.
- The SPIFFE CSI driver `csi.spiffe.io` registered, and spire-controller-manager present with the `ClusterSPIFFEID` CRD served.
- The chart's default `ClusterSPIFFEID` disabled or set `fallback: true`, so it does not race kora's own registrations.
- Agent SDS enabled with `defaultSVIDName: default`, `defaultBundleName: ROOTCA`, `defaultAllBundlesName: ALL`.
- A controller-manager class matching the chart's `spire.className` value (`spire-system-spire` by default). A mismatch means kora's `ClusterSPIFFEID`s are ignored.

The reference Helmfile and the Argo CD example install a SPIRE the minimum spec required for kora.

The platform's SPIFFE trust domain is part of every workload identity kora issues and should be treated as permanent. It is important you choose this upfront before installation. Changing it later is an identity migration, not a routine update.

### Database, cache, and storage

PostgreSQL and Redis must be reachable from the system namespace. kora does not provision, back up, patch, or replicate either service.

The `orbit` chart applies its own schema. A `pre-install,pre-upgrade` hook Job (`orbit-migrate`) runs every pending migration before any workload rolls; Argo CD runs it as a `PreSync` hook. The Job connects with the services' credentials. That role needs `CREATE` on schema `public` and owns every table the migrations create. PostgreSQL 14 through 18 are supported. The Job runs without an Istio sidecar and needs the same network path to the database as the services.

- Use private endpoints and TLS in production.
- When either service sits outside the cluster network, add its CIDR and port to `networkPolicy.externalServices`. The default-deny policy otherwise blocks the connection.
- When the mesh restricts egress, set `istio.outboundTrafficPolicy: REGISTRY_ONLY`. The chart renders a `ServiceEntry` for Postgres and Redis. The operator renders one for each [model provider](model-providers.md#network-access).

Each agent runs against its own workspace on a persistent volume. kora requests a `10Gi ReadWriteOnce` volume from your cluster's default StorageClass. Your cluster's StorageClass controls how the volume behaves, covering its expansion, retention and encryption policies. Set `agents.workspace.storageClassName`, `accessModes` and `size` to use a different class. Agent and sandbox pods run as uid 100 and set `fsGroup: 100`. If your cluster needs a different group for root-owned writable volumes, patch `spec.template.spec.securityContext.fsGroup` on the `StatefulSet` and `spec.securityContext.fsGroup` on the sandbox `Pod`.

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

On a `ReadWriteOnce` workspace, kora pins each sandbox to the node running the session-manager replica whose workspace it mounts, since matching node selectors alone leave a pool free to place them apart. A sandbox waits for room on that node rather than starting elsewhere and failing to attach.

When adopting this topology, you should do so before creating agents. A `StatefulSet`'s `volumeClaimTemplates` are immutable. An agent created against a `ReadWriteOnce` class keeps that volume even after the class changes. Migrating an existing agent requires moving its workspace data to a new claim, or deleting and recreating the agent.

#### Sandbox warming

When running on a dedicated pool that scales to zero, a new sandbox request has to wait for a node to boot and the image to pull. kora provides an out of the box sandbox warmer that provisions a low-priority placeholder pod, holding a node open with the sandbox image already cached.

Warming is off by default. Enable it in the chart values:

```yaml
agents:
  sandboxWarming:
    mode: capacity
    replicas: 1
    priority: -10          # below real agent workloads
```

Placement of sandbox warmers follows `agents.workloads.sandbox`.

Each placeholder reserves one default sandbox pod: the default size, 1Gi of disk and the sandbox sidecars. Set `resources` to reserve a different amount. It must cover a sandbox pod's total requests.

`replicas` is how many warm nodes you need.

To keep an image pulled on a pool that does not scale to zero, set `warm` on its `SandboxImage`.

#### Scale-out sandboxes

A scale-out install runs sandboxes on their own node pool with a `ReadWriteMany` workspace. Package installs and builds are slow on the network filesystem behind that workspace. Set `sandboxMount.enabled` to give every sandbox a working folder on its node's local disk at `/workspaces/<run>/sandbox`. The agent's file tools and the sandbox see the same files in that folder, and the folder ends with the sandbox.

```yaml
agents:
  workspace:
    storageClassName: shared-files   # a ReadWriteMany StorageClass
    accessModes:
      - ReadWriteMany
  workloads:
    sandbox:
      nodeSelector: { workload-tier: orbit-sandbox }
sandboxMount:
  enabled: true
```

The chart deploys the node agent `orbit-sandbox-mount` as a privileged DaemonSet in the `orbit-sandbox-mount` namespace. The agent exports each sandbox's folder with the kernel NFS server to its session-manager's node and mounts it into the running session-manager pod.

The cluster must provide:

*   A node kernel with the NFS server and NFSv4 client modules.
*   Admission for a privileged pod that uses the host network and the host PID namespace.
*   TCP `sandboxMount.port` (default 20490) open from session-manager nodes to sandbox nodes. NFS uses no other port.

Check each requirement before you enable the feature:

1.  **Check Privileged Admission:** Run a server-side dry run of a privileged pod in a namespace at the privileged Pod Security level.

    ```bash
    kubectl create namespace orbit-sandbox-mount
    kubectl label namespace orbit-sandbox-mount pod-security.kubernetes.io/enforce=privileged
    kubectl -n orbit-sandbox-mount run privileged-check --image=busybox --restart=Never --dry-run=server \
      --overrides='{"spec":{"hostNetwork":true,"hostPID":true,"containers":[{"name":"privileged-check","image":"busybox","securityContext":{"privileged":true}}]}}'
    ```

2.  **Check the NFS Server Module:** Load the module on a sandbox node. A zero exit status means the kernel has the NFS server.

    ```bash
    kubectl debug node/<sandbox-node> -it --profile=sysadmin --image=busybox -- chroot /host modprobe nfsd
    ```

3.  **Check the NFS Port:** Connect from a session-manager node to the port on a sandbox node. A refused connection means the port is open. A timeout means a firewall drops it.

    ```bash
    kubectl debug node/<session-manager-node> -it --image=busybox -- nc -z -v -w 3 <sandbox-node-address> 20490
    ```

Leave `sandboxMount.enabled` off on a platform that fails a check. Sandboxes then work on the shared workspace volume.

If the session-manager's node cannot reach the port, the sandbox fails to start with "the node agent cannot reach the NFS port on the sandbox node". The node agent's log on the session-manager's node lists the port and both nodes.

The DaemonSet carries the sandbox and session-manager tolerations plus any in `sandboxMount.tolerations`. Set `sandboxMount.createNamespace: false` to run it in a namespace you manage.

NFS does not authenticate callers. Each folder is exported only to its session-manager's node address, and the tenant NetworkPolicies deny agent and sandbox pods the NFS port. A host-network workload, or a pod whose traffic your CNI masquerades to the node address, can still reach the export. Restrict host network access to trusted system workloads, and deny other workloads egress to node addresses on `sandboxMount.port`.

### Sandbox images and environments

A sandbox runs the image of its environment. kora publishes the base image `kora-sandbox-base` with the general command-line tools, Go and Python. Platform teams build their own images `FROM` it and register each one as a `SandboxImage` in the tenant namespace:

```yaml
apiVersion: orbit.io/v1alpha1
kind: SandboxImage
metadata:
  name: acme-go
  namespace: orbit-tenant-acme
spec:
  image: ghcr.io/acme/sandbox-go:1.4.0
  description: Go with buf and golangci-lint.
  warm: true
  pullSecretRef:
    name: acme-registry
```

*   **`image`:** The image reference, given as a tag or a digest such as `ghcr.io/acme/sandbox-go@sha256:...`.
*   **`description`:** Shown to people who choose an image for an environment.
*   **`warm`:** Keeps the image pulled on every node that runs sandboxes. The operator runs one DaemonSet per warm image in the tenant namespace, placed like the sandboxes.
*   **`pullSecretRef`:** A `kubernetes.io/dockerconfigjson` Secret in the same namespace. The operator reads the registry with it and sandboxes pull with it. The Secrets in `global.imagePullSecrets` also apply.

The operator reads the image config from the registry. It accepts an image that runs as uid 100 and carries the `io.orbit.sandbox.base` label. The base image sets both. The operator writes the resolved digest to `status.digest`, and sandboxes pull the image by that digest. Inspect an image with:

```bash
kubectl -n orbit-tenant-acme get sandboximages
kubectl -n orbit-tenant-acme describe sandboximage acme-go
```

The `Valid` condition states why the operator rejected an image. An environment cannot select an image that is not valid. The operator re-reads each image every sync period, and new sandboxes pick up a moved tag after the next read.

The operator keeps a `SandboxImage` named `kora-base` in every tenant namespace. It points at `agents.config.sandboxImage`, and `agents.sandboxImages.warmBase` sets its `warm` flag. Every tenant reconcile restores it.

kora also publishes three bundled images built on the base image, and the operator registers each enabled one in every tenant namespace:

*   **`kora-go`:** `kora-sandbox-go`, with `golangci-lint`, `buf` and `protoc`.
*   **`kora-python`:** `kora-sandbox-python`, with `ruff` and `pytest`.
*   **`kora-node`:** `kora-sandbox-node`, with Node LTS and `pnpm`.

Each one is enabled with `warm: false` by default. An empty `image` uses the release image from `global.imageRegistry`. Set `image` to a mirror or to your own build:

```yaml
agents:
  sandboxImages:
    bundled:
      go: { enabled: true, warm: true, image: "" }
      python: { enabled: true, warm: false, image: "" }
      node: { enabled: false }
```

Disabling a bundled image deletes its `SandboxImage` from every tenant namespace. An environment that selects it can no longer start a sandbox, and the agent receives a message that the image is not registered. Running sandboxes keep running. The operator changes only the bundled `SandboxImage` resources it created. If a tenant registers its own `SandboxImage` under a bundled name, the operator leaves it in place, does not register the bundled image in that namespace, and logs the conflict.

An environment combines a sandbox image with a size, egress hosts and connectors. Users with `agents:environments:write` create environments through the API or the Environments page and attach them to agents. An agent with no environment runs its sandboxes on `kora-base` at the default size. Helm values set the default size and the largest size an environment can request:

```yaml
agents:
  environments:
    defaultSize: { cpu: "2", memory: 4Gi, disk: 10Gi }
    maxSize: { cpu: "2", memory: 4Gi, disk: 10Gi }
```

`disk` sizes a node-local volume in each sandbox that holds its `HOME`, `TMPDIR`, the Go, npm and pip caches, user-level tool installs and, with `sandboxMount.enabled`, its working folder.

The sandbox container gets exactly its environment's size. The pod also runs sidecars (`operator.injection.sandboxSidecars`, 900m CPU and 320Mi memory by default) and 1Gi of scratch disk on top of the size. `maxSize` starts equal to `defaultSize`. To offer larger sizes, set each `maxSize` field to the largest value that fits one sandbox node:

```text
maxSize.cpu    = node allocatable CPU     - DaemonSet CPU requests    - sidecar CPU
maxSize.memory = node allocatable memory  - DaemonSet memory requests - sidecar memory
maxSize.disk   = node allocatable ephemeral storage - 1Gi
```

For example, a node with 3900m CPU, 14Gi memory and 20Gi ephemeral storage allocatable, running DaemonSets that request 500m CPU and 1Gi memory, holds a sandbox of up to 2.5 CPU, 12.6Gi memory and 19Gi disk. Round each value down to a size you want to offer, such as `{ cpu: "2", memory: 12Gi, disk: 18Gi }`.

`kubectl describe node <node>` shows the node's allocatable resources and the requests of the pods on it. A run on a size that fits no node fails with the scheduler's reason.

The operator needs `agents.sandboxImages.registryPorts` for a registry on a port other than 443. `agents.sandboxImages.registryMirrors` maps a registry host in image references to the host the operator reads from, as the nodes' registry mirrors do. A mirror given as `http://host` is read over plain HTTP, such as the local k3d registry.

### Pod metadata and cloud identity for agents

`agents.workloads.common.podLabels` and `podAnnotations` reach both provisioned pods, for cost allocation, network policy selection, backup exclusion or an admission controller of your own. The per-workload blocks take the same keys and win where both set one.

kora owns the labels identifying a pod to SPIRE, the sidecar injector and its StatefulSet, and the annotations wiring it into the mesh. Setting one of those fails `orbit-agents` at startup naming the key, rather than silently moving a pod off its attested identity.

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

Choose a hostname for your dashboard, for example `orbit.example.com`. kora derives the API hostname by adding the `api.` prefix:

- `orbit.example.com` serves the dashboard and BFF (`frontendHost`).
- `api.orbit.example.com` serves the platform APIs, inference API, MCP endpoints, and OAuth discovery documents.

Create DNS records for both names resolving to the same Istio gateway, fronted by a cloud `LoadBalancer`.

The gateway certificate must be valid for both names. Provision a `kubernetes.io/tls` Secret in the gateway's namespace, directly or through cert-manager, and set `gateway.tls.credentialName` to it. kora terminates public TLS at the Istio gateway. Inside the mesh, service-to-service mTLS uses SPIRE identities.

Register `https://orbit.example.com/auth/callback` as an allowed redirect URI for the OIDC client. kora's services must be able to reach the issuer and its discovery endpoints from the system namespace.

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

Your secret manager owns rotation. The kora operator does not watch these Secrets, and only picks up a rotated Connection or OIDC client secret on its next periodic reconcile.

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
    tls:
      caSecret: redis-ca      # only for a private CA
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

Redis TLS is on by default. Set `config.redis.tls.enabled: false` for a Redis that serves plaintext.

`global` holds the values every service shares: trust domain, image registry and tag, log format, Postgres SSL mode, and OTLP settings.

The chart renders each service's settings into a ConfigMap of config files mounted at `/etc/orbit/config` ([rendered service config files](../adr/2026-10-06-rendered-service-config-files.md)). A component's `configOverrides` merges last, keyed by config module, and its keys follow the Go config structs:

```yaml
policyService:
  configOverrides:
    internal/platform/observability:
      logFormat: console
```

An unknown key in `configOverrides` fails the service at startup. A component's `extraEnv` is for secrets only.

Image tags resolve as follows:

| `global.imageTag` | Result |
|---|---|
| Empty (default) | The chart's `appVersion`. A release stamps per-component digests. |
| A tag | Every kora image uses it.  |

Use the narrowest CIDRs and ports in `networkPolicy.externalServices` that cover your services. Omit the entry when Postgres and Redis are reached through destinations your cluster network policy already allows.

## Telemetry

kora exports traces and metrics over OTLP, configure your endpoint as follows:

```yaml
global:
  observability:
    otlpEndpoint: http://otel-collector.observability.svc:4317
    collectorNamespace: observability
    samplingRate: "1.0"
```

`collectorNamespace` opens the egress on the endpoint's port. Without it the default-deny policy drops the traffic. For a collector outside the cluster, leave it empty, add the collector's CIDR and port to `networkPolicy.externalServices`, and create a `ServiceEntry` for the collector host so the mesh routes to it. Agent pods export to the same endpoint, providing telemetry for their run; their egress policy admits the collector and no other external host ([agent pod egress lockdown](../adr/2026-09-30-agent-pod-egress-lockdown.md)).

An external collector is the one external address an agent can reach. On a shared SaaS endpoint, a prompt-injected agent could export data under an attacker's own key to the attacker's account on the same service. If that matters for your install, run an in-cluster collector that forwards to the external one and point `otlpEndpoint` at it.

Point your service mesh's own telemetry at the same collector to get the mesh hop and the application span in one trace.

## White-labelling theme

A deployment can carry one theme: the product name, the colours and the identity assets a client's people see in place of kora's ([white-labelling theme](../adr/2026-10-12-white-labelling-theme.md)). A theme is a bundle, a directory with the document `theme.yaml` at its root and its assets under `assets/`, written against the Hades version the release runs. The frontend validates it before any page serves. Without a theme the chart renders exactly as before.

### Nodes

The bundle reaches the frontend pod as an image volume, which the kubelet mounts straight from the registry. That needs nodes on Kubernetes 1.36 or later with containerd 2.1 or CRI-O 1.31 or later, ahead of the chart's floor of 1.34. The chart does not check this. On a cluster below it the frontend pod never starts, the rollout stalls on an event that names the `theme-bundle` volume, and the previous pods keep serving.

### Build the bundle image

The image is the bundle directory and nothing else, with the document at `/theme/theme.yaml` and the assets under `/theme/assets/`:

```dockerfile
FROM scratch
COPY theme.yaml /theme/theme.yaml
COPY assets/ /theme/assets/
```

Build it, push it to a registry the nodes can pull from with the configured pull secrets, and record the digest:

```bash
docker build -t registry.example.com/acme/kora-theme:2026-10-12 .
docker push registry.example.com/acme/kora-theme:2026-10-12
docker inspect --format='{{index .RepoDigests 0}}' registry.example.com/acme/kora-theme:2026-10-12
```

Where `assets/` comes from is your pipeline's business: a directory beside `theme.yaml` in the repository, or an artefact store the pipeline assembles from. Nothing requires the assets in git. A theme that sets colours only has an empty `assets/` and is still an image.

### Configure the chart

The chart takes the digest-pinned image reference and the document itself. The document is the committed `theme.yaml`, passed as a file, so the committed file is the one thing you edit:

```yaml
theme:
  bundle:
    image: registry.example.com/acme/kora-theme@sha256:3f9e...   # a digest; the schema rejects a tag
    pullSecrets: []     # {name: <secret>} objects; empty uses global.imagePullSecrets
```

```bash
helm upgrade --install orbit oci://ghcr.io/clear-route/charts/orbit \
  --namespace orbit-system \
  --values orbit.yaml \
  --set-file theme.document=theme.yaml
```

Under Argo CD, `fileParameters` is the `--set-file` equivalent. With the reference app-of-apps, where the chart and the values are separate sources, the path starts with the values source's `ref`, as `valueFiles` does:

```yaml
helm:
  valueFiles:
    - $values/orbit.yaml
  fileParameters:
    - name: theme.document
      path: $values/theme.yaml
```

The image and the document come together: one without the other fails the render, as does a tag in place of a digest or a document that is not YAML, each with a message that names the fault. The chart renders the document into a ConfigMap for the frontend and reads `productName` from it into the config of the services that produce text a client's people or agents read.

### Roll out a theme

A theme change is a values change: a new digest, a changed document, or both, rolled out by the standard rolling update and rolled back by a revert. The frontend pod mounts the bundle read-only at `/etc/orbit/theme` and runs a `validate-theme` init container before the application starts. It validates the bundle with the Hades version the image carries and checks that the bundle's `theme.yaml` matches the document the chart was given. A fault stops the pod there: the rollout stalls, the previous pods keep serving, and under Argo CD the Application goes Degraded once the Deployment's progress deadline passes.

When a rollout stalls, read the init container's log on the new pod:

```bash
kubectl get pods -n orbit-system -l app=orbit-frontend
kubectl logs -n orbit-system <pod> -c validate-theme
```

It prints every fault, or, on success, every entry this version ignores and every identity value left at the kora default. A bundle that was not rebuilt after the document changed, or a document that was not committed after the bundle was, fails with a message that says so: rebuild and push, or commit, then roll again. A pod stuck in `ContainerCreating` with an event about the `theme-bundle` volume is a node below the floor above.

## Run behind an existing gateway

By default kora creates its own ingress gateway. Set `gateway.create: false` to front kora with an Istio gateway you already operate. kora then renders no `Gateway` resource, and binds its `VirtualService`s to the provided gateway.

There are four points of configuration documented in `deploy/reference/values/gateway-byo.yaml`:

 1. Chart values: `gateway.create: false`, with `gateway.name` and `gateway.byo` pointing at your gateway.
 2. A `Gateway` resource on your gateway for the two public hosts.
 3. A `ClusterSPIFFEID` registering your gateway's pods.
 4. SPIRE wiring in your gateway's own values, pointing its Envoy at SPIRE for certificates instead of istiod.

Points 3 and 4 give your gateway a SPIRE identity, required to interact with kora's own pods. See [identity](../architecture/identity.md) and [runtime topology](../architecture/runtime-topology.md) for a detailed look at the trust model.

## Install

### Reference Helmfile (blank cluster)

The reference Helmfile in `deploy/reference/` installs SPIRE, Istio, and the two kora charts in dependency order. 

Copy `deploy/reference/` into your own repository, and edit the two files: `environments/example.yaml` (or whatever you name it) for the cluster facts, and `values/orbit.yaml` for the chart config.

To apply from your local copy:

```bash
helmfile -e example apply
```

If SPIRE and Istio are already running in your cluster, you can skip the helmfile and use helm directly for the kora charts.

### Direct Helm (existing mesh)

With SPIRE and Istio already running, you install the two kora charts directly. First label the system namespace, then install `orbit-crds`, and then `orbit`.

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
| `prune: false` on `orbit-crds` | Stops ArgoCD from pruning the kora CRDs. Removing one cascade-deletes every tenant, connection, etc. of that kind across the cluster.  |

The app-of-apps installs SPIRE, Istio, and the kora charts. Add your `OrbitTenant`, `Connection`, `OAuthClient`, and `MCPServer` CRs as a separate `Application` with a sync wave after `orbit-crds`. This ensures their `prune: true` is independent of the `orbit-crds` `prune: false`. 

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
- [OAuth clients](oauth-clients.md): configure clients that authenticate users against kora.
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

- [Install and distribution model](../adr/2026-09-04-install-and-distribution-model.md): why kora ships as two charts and the operator manages only tenants.
- [Enforcement sidecar injection and preflight](../adr/2026-09-04-enforcement-sidecar-injection-and-preflight.md): how kora injects its sidecars and runs preflight on the mesh before install.
- [Service mesh and network enforcement](../adr/2026-03-05-service-mesh-and-network-enforcement-infrastructure.md): the mesh and NetworkPolicy enforcement the platform runs on.
- [Manifest patches](../adr/2026-09-15-manifest-patches-configmap.md): how operators patch the resources orbit-agents provisions in tenant namespaces.
- [White-labelling theme](../adr/2026-10-12-white-labelling-theme.md): how a theme reaches a deployment, where it is validated, and the node requirement it adds.
