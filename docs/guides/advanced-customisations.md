# Advanced customisations

kora provisions a fixed set of Kubernetes resources for each agent type. Most teams never change them. When your cluster needs something kora does not set, such as a node pool, a storage class or an image pull secret, you can patch those resources without forking kora.

This guide walks through writing a manifest patch, previewing the result and deploying it. It assumes kora is already installed. See [Deploying kora](deployment.md) for the install.

## What you can patch

`orbit-agents` applies your patches to the StatefulSet, workspace PersistentVolumeClaim, Service, ServiceAccount, HPA, PDB and sandbox Pod it provisions for each agent type. Patches apply to every tenant that shares the `orbit-agents` pod.

Each patch is one kustomize `patches:` entry with a `target.kind`. Both strategic merge patches and JSON6902 patches work, and the entry accepts the usual selectors.

Some fields stay platform-owned. A patch that changes a resource's identity, its security context or the sandbox workspace mount is rejected when `orbit-agents` starts.

## Write a patch

Patches are values under `agents.manifestPatches` in your Helm values. Each key is one patch. Keys sort into apply order, so prefix them with a number.

This patch pins agent pods to a node pool:

```yaml
agents:
  manifestPatches:
    50-nodeselector:
      target:
        kind: StatefulSet
      patch: |
        apiVersion: apps/v1
        kind: StatefulSet
        metadata:
          name: any
        spec:
          template:
            spec:
              nodeSelector:
                disktype: ssd
```

Settings in `agents.workloads` and `agents.workspace` apply before manifest patches. When both set a field, the patch wins. Prefer those settings when they cover your need, and use a patch for the rest.

## Preview the result

Before you deploy, render the exact YAML `orbit-agents` would produce. The `-render` mode needs no cluster or database.

```bash
cd backend
go run ./cmd/orbit-agents -render \
  -patch-dir=/path/to/patches \
  -agent-slug=coder \
  -namespace=tenant-example \
  -sandbox
```

- `-patch-dir`: directory of `*.yaml` or `*.yml` patch files. Write each `agents.manifestPatches` key as a file named `<key>.yaml` in this directory, holding that key's entry. It mirrors what the Helm chart mounts at `/etc/orbit/manifest-patches`.
- `-agent-slug`: required, the agent type slug to render.
- `-agent-type-id`: the agent type's ID, for patches that select on the `orbit.io/agent-type` label. Defaults to `render-<slug>`.
- `-namespace`: tenant namespace to render into. Defaults to `tenant-example`.
- `-sandbox`: also render a sample sandbox Pod.

An invalid patch fails immediately with the offending filename. That covers a missing or unknown target kind, a patch kustomize cannot apply, a field typo and a change to a protected field. It is the same validation `orbit-agents` runs at startup.

To confirm the API server accepts every rendered object, pipe the output through `kubectl apply --dry-run=server -f -` against a non-production cluster.

The preview omits `metadata.ownerReferences`. The runtime resolves those from a live lookup of the tenant resource, which `-render` has no cluster to query. Every field a patch could reach is otherwise faithful to what `orbit-agents` creates.

## Deploy the patch

Set `agents.manifestPatches` in your values and upgrade the release. The chart writes each patch into the `orbit-agents-manifest-patches` ConfigMap and rolls the `orbit-agents` pod. Patches load once at startup, so a change takes effect on that roll.

If a patch fails to load, `orbit-agents` fails to start and the error names the file. Fix the patch and upgrade again.

## Migrate from environment variable overrides

Older installs set `ORBIT_MANIFEST__<Kind>__<name>__<path>` environment variables on the `orbit-agents` pod. These are no longer read. `orbit-agents` logs a warning at startup for each one it finds. Move each override into a patch under `agents.manifestPatches`, then remove the variable.

## References

- [Manifest patches](../adr/2026-09-15-manifest-patches-configmap.md): why kora uses kustomize patches and which fields stay protected.
- [First-class workload placement](../adr/2026-09-16-first-class-workload-placement.md): the `agents.workloads` settings that apply before patches.
- [K8s manifest overrides via environment variables](../adr/2026-06-12-k8s-manifest-overrides-via-env-vars.md): the superseded mechanism.
