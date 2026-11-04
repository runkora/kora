# Prepare your cluster

runkora installs onto a cluster that already runs Istio and SPIRE, with a PostgreSQL database and a Redis instance it can reach. SPIRE issues each workload its identity, and Istio carries traffic between workloads over mutual TLS.

## Kubernetes cluster

kora supports Kubernetes 1.34 and later. Installing needs cluster-admin to create the CRDs, cluster roles, admission webhooks and other cluster-scoped resources.

The CNI must enforce `NetworkPolicy`. The `kora` chart installs a default-deny policy in `kora-system` with rules for the traffic kora needs. Set `networkPolicy.enabled: false` only on a CNI that cannot enforce it.

Pod Security Admission must admit the privileged SPIRE and Istio node components. kora's own namespaces run at the `restricted` level.

Agent nodes need a kernel with Landlock ABI v2 or later (Linux 5.19) and Landlock enabled at boot. On a node without it, agent pods stay not ready and the session-manager log cites `agents.landlock`. Set `agents.landlock: bestEffort` to run agents on such nodes without Landlock confinement.

## Trust domain

Istio, SPIRE and kora must use the same trust domain. Every workload's SPIFFE ID starts with it, such as `spiffe://example.com/tenant/kora-tenant-engineering/agent/coder`. Choose it before you install. Changing it later means migrating every workload identity.

Set it in all three places:

| Component | Setting |
| --- | --- |
| Istio | `meshConfig.trustDomain` on istiod |
| SPIRE | `global.spire.trustDomain` in the spire chart |
| kora | `global.trustDomain` in the kora chart |

## Istio

kora supports Istio 1.30 and 1.31 in sidecar mode. On any other minor version, the operator's preflight fails and every tenant stays `Degraded`. To accept another minor version, add it to `operator.preflight.supportedIstioMinors`. kora has not verified sidecar injection outside of the listed minors.

Install Istio with the [Istio install guide](https://istio.io/latest/docs/setup/install/) and the values in the quickstart's [`istiod.yaml`](https://github.com/runkora/examples/blob/main/quickstart/values/istiod.yaml). kora's namespaces reject Istio's default `istio-init` container. Install the Istio CNI plugin and run istiod with `pilot.cni.enabled: true`.

Set these in kora's values when your mesh differs from the defaults:

- `istio.namespace`: where istiod and the `istio-cni-node` DaemonSet run, `istio-system` by default.
- `istio.revision`: the Istio revision kora's namespaces use. With a revision, label `kora-system` with `istio.io/rev=<revision>` in place of `istio-injection=enabled`.
- `istio.outboundTrafficPolicy: REGISTRY_ONLY`: for a mesh that blocks unregistered hosts. The chart then registers an external Postgres or Redis host, and the operator registers [model providers](register-inference-provider.md#network-access) and external MCP servers. Register the OIDC issuer and other external hosts yourself.

## SPIRE

kora registers its workload identities through spire-controller-manager.

To install SPIRE, use the [spiffe/spire chart](https://github.com/spiffe/helm-charts-hardened) with the quickstart's [`spire.yaml`](https://github.com/runkora/examples/blob/main/quickstart/values/spire.yaml) values.

If you already run SPIRE:

- Run a SPIRE agent and the SPIFFE CSI driver on every node that runs kora, agent or sandbox pods, tainted nodes included.
- Set kora's `spire.className` to your spire-controller-manager's class name. It defaults to `spire-system-spire`.
- Set `fallback: true` on any `ClusterSPIFFEID` that matches kora's pods, or exclude kora's namespaces from it.

## Database and cache

kora supports PostgreSQL 14 and later and Redis 6.2 and later. Both must be reachable from `kora-system`. kora does not provision, back up, patch or replicate either service.

The database role in `config.db.user` needs `CREATE` on schema `public`. The `kora-migrate` Job connects without an Istio sidecar. A database that accepts only mesh mTLS rejects it.

## Workspace storage

Each agent runtime pod gets a `10Gi` `ReadWriteOnce` workspace volume from the default StorageClass. Set `agents.workspace.storageClassName`, `accessModes` and `size` to change it. A change applies only to agents created afterwards.

Sandboxes on a [separate node pool](sandbox-nodes.md) need a `ReadWriteMany` StorageClass, such as EFS, Filestore or an NFS server.

## DNS and TLS

Choose a hostname for the dashboard, such as `example.com`, and set it as `frontendHost`. kora serves its APIs on `api.example.com`.

Point both names at the Istio gateway. Its Service is a `LoadBalancer` unless you set `ingressGateway.service.type` to `NodePort` or `ClusterIP`. The gateway certificate must be valid for both names. cert-manager or another issuer can provide it.

## Registry

The charts and kora images are public on `ghcr.io`, and the gateway's OPA image is on `docker.io`. None needs a registry login.

To pull from your own registry, create a `kubernetes.io/dockerconfigjson` Secret in `kora-system`. The operator copies it into each tenant namespace.

```yaml
global:
  imageRegistry: registry.example.com/kora
  imagePullSecrets:
    - name: registry-credentials
opaGateway:
  image:
    registry: registry.example.com
```
