# Advanced customisations

Shape the Kubernetes objects kora creates for an agent type, without forking the
chart.

`orbit-agents` provisions a StatefulSet, a workspace PersistentVolumeClaim, a
Service, a ServiceAccount, an HPA, a PDB and a sandbox Pod for each agent type.
Manifest patches let you adjust any of them (node selectors, tolerations,
resource ceilings, extra labels) while leaving the platform's own fields alone.

## Write a patch

A patch is one kustomize `patches:` entry in its own file, with a `target.kind`
naming the object it applies to. The chart mounts the directory of patch files at
`/etc/orbit/manifest-patches`, configured as `agents.manifestPatches`.

Keep one concern per file. A directory of small, named patches is easier to
review and to roll back than a single file that touches six kinds.

## Preview before you deploy

Render the exact YAML `orbit-agents` would produce. No cluster, no database:

```bash
cd backend
go run ./cmd/orbit-agents -render \
  -patch-dir=/path/to/patches \
  -agent-slug=coder \
  -namespace=tenant-example \
  -sandbox
```

| Flag | Meaning |
| --- | --- |
| `-patch-dir` | Directory of `*.yaml` / `*.yml` patch files. Mirrors what the chart mounts. |
| `-agent-slug` | Required. The agent type slug to render. |
| `-agent-type-id` | The agent type's ID, for patches selecting on the `orbit.io/agent-type` label. Defaults to `render-<slug>`. |
| `-namespace` | Tenant namespace to render into. Defaults to `tenant-example`. |
| `-sandbox` | Also render a sample sandbox Pod. |

## Confirm the cluster accepts it

Rendering proves kustomize can apply the patch. It does not prove the API server
will accept the result, so pipe it through a server-side dry run:

```bash
go run ./cmd/orbit-agents -render -patch-dir=/path/to/patches -agent-slug=coder \
  | kubectl apply --dry-run=server -f -
```

## When a patch is rejected

Invalid patches fail immediately and name the offending file. This is the same
validation `orbit-agents` runs at startup, so a patch that renders will not
surprise you on deploy. A patch is rejected when it has a missing or unknown
target kind, cannot be applied by kustomize, contains a field typo, or changes a
protected identity or security field.

That last category is deliberate. Patches cannot weaken the identity or security
fields the platform sets, because those are what tie a running agent back to the
human who deployed it.

## What the preview does not show

`metadata.ownerReferences` is omitted. The runtime resolves those from a live
lookup of the OrbitTenant CR and the StatefulSet UID, which `-render` has no
cluster to query. Every other field a patch can reach is faithful to what
`orbit-agents` creates.

## Migrating from `ORBIT_MANIFEST__*`

Environment-variable overrides were removed. An operator who still sets
`ORBIT_MANIFEST__*` on the pod spec gets a startup warning naming the variable,
and the value is ignored rather than silently half-applied.

Move each one to a patch file in the mounted directory and preview it with
`-render` before rolling out.

## References

- [Manifest patches via ConfigMap](../adr/2026-09-15-manifest-patches-configmap.md): why patches are mounted files rather than env vars, and which fields are protected.
- [Local development](local-development.md): running the local cluster you would dry-run against.
