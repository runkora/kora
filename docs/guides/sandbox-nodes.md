# Configure sandboxes on your cluster

Sandboxes run on the node of their agent runtime pod by default. For heavier workloads, you can move sandboxes to their own node pool, size them to that pool's nodes, and warm the pool so new sandboxes start quickly. The sandbox mount speeds up builds in either placement.

The following guide assumes runkora is already installed. See [Install runkora on Kubernetes](install-on-kubernetes.md) for the install.

## Choose a placement

kora has two [placement modes](../architecture/sandboxes.md#placement):

- **Co-located:** the default. Each sandbox runs on the node of its agent runtime pod and shares its `ReadWriteOnce` workspace volume. It needs no setup, and suits small sandboxes.
- **Separate pool:** sandboxes run on their own node pool and scale independently of agents. It needs a `ReadWriteMany` StorageClass, such as EFS, Filestore or an NFS server.

Choose the placement before you create agents. An agent keeps the workspace volume it was created with. To move an existing agent, migrate its workspace data or recreate it.

## Move sandboxes to their own node pool

Set the placement of sandboxes in `agents.workloads.sandbox`, and of agent runtime pods in `agents.workloads.sessionManager`. Each takes `nodeSelector`, `tolerations`, `affinity`, `topologySpreadConstraints` and `priorityClassName`, and applies to every agent in the install.

This puts sandboxes and agent runtime pods on separate tainted pools with a `ReadWriteMany` workspace:

```yaml
agents:
  workloads:
    sessionManager:
      nodeSelector:
        workload-tier: kora
      tolerations:
        - key: workload-tier
          operator: Equal
          value: kora
          effect: NoSchedule
    sandbox:
      nodeSelector:
        workload-tier: kora-sandbox
      tolerations:
        - key: workload-tier
          operator: Equal
          value: kora-sandbox
          effect: NoSchedule
  workspace:
    storageClassName: efs
    accessModes:
      - ReadWriteMany
```

If the two placements differ and the workspace is not `ReadWriteMany`, the chart fails to render.

## Size sandboxes

An [environment](create-an-environment.md) requests a sandbox size. `agents.environments.defaultSize` applies when an environment sets none, and `agents.environments.maxSize` caps what an environment can request. `maxSize` defaults to `defaultSize`. The `disk` value sizes the sandbox's local volume for its home directory, caches and installed tools.

To offer larger sizes, set `maxSize` to what one sandbox node can hold. Subtract the node's DaemonSet requests and the sandbox sidecars in `operator.injection.sandboxSidecars` from its allocatable resources:

```text
maxSize.cpu    = node allocatable CPU               - DaemonSet CPU requests               - sidecar CPU
maxSize.memory = node allocatable memory            - DaemonSet memory requests            - sidecar memory
maxSize.disk   = node allocatable ephemeral storage - DaemonSet ephemeral storage requests - 1Gi
```

For example, take a node with 3900m CPU, 14Gi memory and 20Gi ephemeral storage allocatable, and DaemonSets that request 500m CPU and 1Gi memory. With the default sidecars, it holds a sandbox of up to 2.5 CPU, 12.6Gi memory and 19Gi disk. Round down to the sizes you want to offer:

```yaml
agents:
  environments:
    maxSize:
      cpu: 2500m
      memory: 12Gi
      disk: 18Gi
```

In a pool with mixed node sizes, calculate for the smallest node. `kubectl describe node <node>` shows a node's allocatable resources and current requests.

A sandbox too large for any node stays pending for up to 10 minutes. The run then fails with `sandbox_lost` and the scheduler's reason.

## Warm sandboxes

[Warming](../architecture/sandboxes.md#warming) removes the wait for an image pull or a node boot when a sandbox starts.

### Warm images

Image warming keeps an image pulled on every sandbox node. Set `warm: true` on a [`SandboxImage`](create-an-environment.md#register-a-sandbox-image), or warm `kora-base` and the bundled images in your values:

```yaml
agents:
  sandboxImages:
    warmBase: true
    bundled:
      go:
        warm: true
```

### Hold nodes open

Capacity warming runs low-priority placeholder pods that hold sandbox nodes open. A new sandbox takes a placeholder's place and starts at once, and the cluster autoscaler boots a replacement node for the next one. Karpenter and the cluster autoscaler do not remove a node that holds a placeholder.

```yaml
agents:
  sandboxWarming:
    mode: capacity
    replicas: 1
```

- `mode`: `capacity` to turn capacity warming on, or `none` (the default) to leave it off.
- `replicas`: the number of nodes to hold open.
- `resources`: what each placeholder reserves. The default fits one sandbox at `defaultSize`, and a larger value warms nodes for larger environments.
- `priority`: the placeholder priority, `-10` by default. Keep it below 0 so sandboxes can replace placeholders.

Placeholders follow the `agents.workloads.sandbox` placement. Set `nodeSelector`, `tolerations`, `affinity` or `topologySpreadConstraints` under `agents.sandboxWarming` to override it.

## Speed up builds with the sandbox mount

The [sandbox mount](../architecture/sandboxes.md#sandbox-mount) puts each sandbox's working folder on its node's local disk for faster package installs and builds. It works with either placement, and runs `kora-sandbox-mount` as a privileged DaemonSet on every agent and sandbox node.

### Check the cluster

Run these checks before you turn on the mount. If any check fails, leave the mount off and sandboxes keep using the workspace volume.

1.  **Check Privileged Admission:** Dry-run a privileged pod in a namespace at the privileged Pod Security level. A rejection means an admission controller blocks privileged pods.

    ```bash
    kubectl create namespace kora-sandbox-mount-check
    kubectl label namespace kora-sandbox-mount-check pod-security.kubernetes.io/enforce=privileged
    kubectl -n kora-sandbox-mount-check run privileged-check --image=busybox --restart=Never --dry-run=server \
      --overrides='{"spec":{"hostNetwork":true,"hostPID":true,"containers":[{"name":"privileged-check","image":"busybox","securityContext":{"privileged":true}}]}}'
    kubectl delete namespace kora-sandbox-mount-check
    ```

2.  **Check the NFS Modules:** Sandbox nodes need the `nfsd` kernel module, and agent nodes need both `nfsd` and `nfsv4`. Run these on one node of each pool. Each command exits with 0 when the module loads.

    ```bash
    kubectl debug node/<sandbox-node> -it --profile=sysadmin --image=busybox -- chroot /host modprobe nfsd
    kubectl debug node/<agent-node> -it --profile=sysadmin --image=busybox -- chroot /host modprobe nfsd
    kubectl debug node/<agent-node> -it --profile=sysadmin --image=busybox -- chroot /host modprobe nfsv4
    ```

3.  **Check the NFS Port:** Agent nodes must reach sandbox nodes on TCP port 20490. Run this from an agent node. A connection or a refusal means the path is open. A timeout means a firewall blocks it.

    ```bash
    kubectl debug node/<agent-node> -it --image=busybox -- nc -z -v -w 3 <sandbox-node-address> 20490
    ```

### Turn on the mount

```yaml
sandboxMount:
  enabled: true
```

The chart creates the `kora-sandbox-mount` namespace at the privileged Pod Security level. To use a namespace you manage, set `createNamespace: false` and `namespace`. If you pull images from your own registry, create the pull Secret in this namespace too.

The DaemonSet already tolerates the taints in `agents.workloads`, and `sandboxMount.tolerations` adds others. To use a port other than 20490, set `sandboxMount.port`.

Each sandbox's working folder counts against its environment's disk size, and is deleted with the sandbox.

### Troubleshoot the mount

When a folder cannot be mounted, the run's sandbox status event gives the reason:

- **"the node agent cannot reach the NFS port on the sandbox node":** open the NFS port from agent nodes to sandbox nodes.
- **"the node agent could not mount the sandbox folder":** the NFS mount failed. The export accepts only the agent node's `status.hostIP`. Check that NFS traffic leaves the agent node from that address.
- **"the sandbox folder was not mounted in time":** the node agent did not report within 60 seconds. Check that a `kora-sandbox-mount` pod runs on both the sandbox's node and its agent's node. A node with other taints needs a toleration in `sandboxMount.tolerations`.

The `kora-sandbox-mount` log on the agent node shows both nodes and the port.

### Security

NFS does not authenticate callers. kora exports each folder only to its agent node, and tenant network policies block agent and sandbox pods from the NFS port. A host-network workload on the agent node can still reach those exports. Restrict host network access to trusted system workloads, and block other workloads from reaching node addresses on the NFS port.

## Bundled sandbox images

`agents.config.sandboxImage` sets the image behind `kora-base`. When it is empty, kora uses `kora-sandbox-base` at the release version. Change `kora-base` through this value. The operator reverts edits made with `kubectl`.

`agents.sandboxImages.bundled` configures the bundled images with `enabled`, `warm` and `image`. An empty `image` uses `kora-sandbox-<name>` at the release version.

This points `kora-go` at a mirror and turns off `kora-node`:

```yaml
agents:
  sandboxImages:
    bundled:
      go:
        image: registry.example.com/kora/kora-sandbox-go:<version>
      node:
        enabled: false
```

Turning off a bundled image removes it from every tenant. Running sandboxes keep running. Environments that use the image cannot start new sandboxes until you choose another image or turn it back on.

If the operator reads images from a registry on a port other than 443, list the port in `agents.sandboxImages.registryPorts`. To read through a mirror, map the registry host to the mirror in `agents.sandboxImages.registryMirrors`.

## References

- [Sandboxes architecture](../architecture/sandboxes.md): placement modes, warming and the sandbox mount.
- [First-class workload placement](../adr/2026-09-16-first-class-workload-placement.md): `agents.workloads` and capacity warming.
- [Sandbox environments](../adr/2026-10-03-sandbox-environments.md): environment sizes and the `SandboxImage` resource.
- [Bundled sandbox images](../adr/2026-10-07-bundled-sandbox-images.md): the bundled images.
- [Sandbox workspace mount](../adr/2026-10-08-sandbox-workspace-mount.md): the NFS mount and its security model.
