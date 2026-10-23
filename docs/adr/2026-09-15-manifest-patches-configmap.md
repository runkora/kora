# Config-Driven Kustomize Manifest Patches for orbit-agents

<!-- Filename: 2026-09-15-manifest-patches-configmap.md  (decision date + slug; no sequential number) -->

**Status:** Accepted
**Date:** 2026-09-15
**Deciders:** Tom Morelly, Justin Wilkin
**Technical Story:** Productionise the operator surface for customising the Kubernetes resources orbit-agents provisions in tenant namespaces. Supersedes [2026-06-12: K8s Manifest Field Overrides via Environment Variables](2026-06-12-k8s-manifest-overrides-via-env-vars.md).

---

## Context

`orbit-agents` provisions a fixed set of Kubernetes resources for each managed agent type: a `StatefulSet`, a headless `Service`, a `ServiceAccount`, a `HorizontalPodAutoscaler`, a `PodDisruptionBudget`, and a `PersistentVolumeClaim` embedded as a `VolumeClaimTemplate` on the StatefulSet. It also creates a `Pod` for every run's sandbox. All manifests are built as typed Go structs in `backend/internal/agents/k8s.go`.

[ADR 2026-06-12](2026-06-12-k8s-manifest-overrides-via-env-vars.md) introduced a per-field override mechanism: env vars of the form `ORBIT_MANIFEST__<Kind>__<name>__<path>=<value>` parsed at startup and applied to the built typed object via a JSON round-trip using `github.com/tidwall/sjson`. The mechanism works for shallow scalar overrides but has hard limits:

- **Poor ergonomics for structured fields.** Anything list-shaped (`imagePullSecrets`, `tolerations`, `resources`, node affinities) must be embedded as a raw JSON string inside an env var value. The Helm chart did this seven times for `runtimePods.*`.
- **One env var per field.** Non-trivial customisations bloat the orbit-agents pod spec.
- **No selector semantics beyond `Kind + slug|*`.** No `labelSelector`, no `annotationSelector`, no way to say "every sandbox pod, whatever the slug", and no way to touch a specific list index without JSON-string overrides.
- **No debug or preview path.** An operator authoring an override has to `kubectl apply` and diff by hand.

### Background

Kustomize's [patches reference](https://kubectl.docs.kubernetes.io/references/kustomize/kustomization/patches/) defines one `patches:` entry shape carrying either of two patch flavours:

- **Strategic Merge Patch (SMP):** a partial Kubernetes object. List merging is driven by `patchMergeKey`/`patchStrategy` metadata from the Kubernetes API types.
- **JSON6902:** an RFC 6902 op array (`add`/`remove`/`replace`/`move`/`copy`/`test`).

Each entry carries the patch body in `patch:` and an optional `target:` selector (kind, name, namespace, `labelSelector`, `annotationSelector`). Argo CD's `spec.source.kustomize.patches` and Flux's `spec.patches[]` accept the same shape.

### Problem Statement

Give operators a maintainable, familiar mechanism for customising the Kubernetes resources orbit-agents provisions, with selector semantics rich enough to cover the real needs (image pull secrets, storage class, labels and annotations, resource limits, tolerations, priority class, node selector), and a way to preview the final manifest before deploying.

### Goals

- Operators author patches as kustomize `patches:` entries, the format they already know.
- Patches are loaded at orbit-agents startup and applied to every provisioned resource without a code change per new field.
- Patches cannot change the identity, security context or workspace isolation orbit-agents builds.
- A CLI mode renders the final YAML for a given agent slug (plus optional sandbox) so operators can diff patches locally or in CI before deploying.
- Fail loud: a patch that does not load or apply fails startup with the filename in the error.

### Non-Goals

- Full `kustomization.yaml` surface (`replacements`, `images`, `namePrefix`, `generators`, `components`, `commonLabels`, …). Operators get `patches:` only.
- Per-tenant patches. All tenants sharing an orbit-agents pod get the same patches.
- Patching the Istio `DestinationRule` and `AuthorizationPolicy`, which are created as `unstructured`.
- Editing patches at runtime. Patches are loaded once at startup; a `ConfigMap` change rolls the orbit-agents pod through a Helm-computed `checksum/patches` annotation.

---

## Decision Drivers

- **Battle-tested UX.** Operators already know kustomize patch syntax.
- **Exact kustomize semantics.** A patch should behave in orbit-agents as it does under `kustomize build`, including `$patch: delete`, list-merge keys and selectors.
- **Least code to maintain.** Parsing, flavour detection and patch application are solved problems.
- **Protected invariants.** The typed builder stays the source of truth for identity, pod security and per-run workspace isolation.
- **Deterministic ordering.** Lex-sorted filenames with a numeric prefix (`010-*.yaml`, `020-*.yaml`) decide which patch applies first.
- **Fail loud.** A broken patch is an operator error at startup, not a silent no-op later.
- **Debug parity.** The renderer and the runtime share one code path.

---

## Considered Options

### Option 1: Embed the kustomize engine, patches only (chosen)

**Description:** Each patch file is one kustomize `patches:` entry. For every resource orbit-agents builds, it runs `sigs.k8s.io/kustomize/api/krusty` over an in-memory kustomization containing that one resource and one patch, once per matching patch in file order. Only the `patches:` field is exposed.

**Pros:**
- Exact kustomize behaviour, because it is kustomize: SMP, JSON6902, `$patch` directives and target selectors.
- No patch parsing, flavour detection or selector matching code to maintain.
- The file format is documented upstream.

**Cons:**
- `sigs.k8s.io/kustomize/api` is v0.x.
- Adds `sigs.k8s.io/kustomize/api` and `sigs.k8s.io/kustomize/kyaml` as direct dependencies, plus four small indirect modules. No Kubernetes module versions change.
- Each patch costs a typed-to-YAML-to-typed round-trip, about 0.5ms per patch per resource.
- Every entry needs `target.kind`, and SMP bodies need a placeholder `metadata.name`: kustomize requires a name in an SMP body and matches target-less SMP patches by exact name, which operators cannot know for runtime-named resources.

### Option 2: Reimplement SMP and JSON6902 with `strategicpatch` and `evanphx/json-patch`

**Description:** Apply SMP with `k8s.io/apimachinery/pkg/util/strategicpatch` and JSON6902 with `github.com/evanphx/json-patch/v5`, with a custom loader that autodetects each file's flavour and a custom selector matcher.

**Pros:**
- No new direct dependencies; both libraries are already indirect.

**Cons:**
- A custom loader, flavour autodetection, selector matching and path-based protection checks to maintain.
- A file format that resembles kustomize without matching it (for example a YAML op array under `patch:` where kustomize takes a string), so operators cannot reuse existing patches.
- Behaviour diverges from kustomize at the edges, such as whether a later patch's selector sees labels an earlier patch added.

### Option 3: SMP only

**Description:** Ship strategic merge patch, drop JSON6902.

**Pros:**
- Smallest possible surface.

**Cons:**
- No precise list-index editing or `remove` semantics.
- Every operator hitting a limit reaches for JSON6902; adding it later changes the file layout.

### Option 4: Full user-supplied manifests via a Helm-shipped ConfigMap

**Description:** Instead of patching over orbit-agents' built manifests, the operator provides the complete Kubernetes manifests (StatefulSet, Service, ServiceAccount, HPA, PDB, sandbox Pod) as YAML in a Helm-managed ConfigMap, which orbit-agents applies with minimal substitution instead of building typed Go structs.

**Feasibility analysis.** Every field a manifest needs must either be static across tenants and agent types or knowable at ConfigMap-generation time. What `buildSessionManagerStatefulSet` and `buildSandboxPod` compute at provisioning time:

Fields orbit-agents computes per agent type at runtime:
- `metadata.name` = agent type slug (`at.Slug`); StatefulSet, Service, ServiceAccount, HPA and PDB are all named after it.
- `metadata.namespace` = the tenant namespace, one per tenant.
- `metadata.labels` = `{app: {slug}, orbit.io/agent-type: {at.ID}, orbit.io/tenant: {at.TenantID}}`; the agent-type UUID exists only once the row is written.
- `metadata.ownerReferences` = the OrbitTenant CR's UID, looked up at provision time.
- StatefulSet `spec.serviceName` = agent slug (must equal the headless Service name for DNS).
- StatefulSet `spec.selector.matchLabels` and pod template labels share `podLabels(at)`, which includes the agent-type UUID.
- StatefulSet pod template `spec.serviceAccountName` = agent slug.
- StatefulSet pod template `envFrom` references a per-tenant `{at.TenantID}-config` ConfigMap.
- StatefulSet pod template `env` includes URLs built from the system namespace (`ANTHROPIC_BASE_URL`, `ORBIT_AGENTS_URL`).
- HPA `spec.scaleTargetRef.name` = agent slug.

Fields orbit-agents computes per run at runtime for the sandbox Pod:
- `metadata.name` = `sandbox-{run.ID}`.
- `metadata.labels` = `{orbit.io/role: sandbox, orbit.io/run-id: {run.ID}, orbit.io/agent-slug: {slug}}`; the run ID is a database-generated UUID.
- PVC volume reference = `workspaces-{slug}-{run.PodOrdinal}`, the StatefulSet's per-ordinal claim name.
- Container `volumeMounts[…].subPath` = `run.ID`, so the sandbox only mounts its own workspace subdirectory.
- Container env includes `WORKSPACE`, `SANDBOX_EXEC_PORT`, per-tenant CA paths and a base64-encoded kubeconfig built from the granted Kubernetes clusters.
- Owner reference = the parent StatefulSet's UID, looked up at provision time.

None of these fields is static. A "fully user-supplied manifest" can only be a template that orbit-agents renders with the runtime variables above, which means a template engine plus a documented variable contract.

**Pros:**
- Maximum control for operators who want to hand-craft every field.

**Cons:**
- Requires a template engine and a variable contract, not plain YAML.
- Deletes the compile-time guarantees on pod security, SPIFFE labels, per-run subpath isolation and cross-resource identity.
- Every new field orbit-agents needs becomes a breaking template contract change.
- The render mode can only echo the template with variables substituted.

---

## Decision

**Chosen Option:** Option 1. orbit-agents embeds the kustomize engine and exposes only its `patches:` entries, loaded from a ConfigMap-mounted directory, one entry per file, with every entry targeting a kind.

### Rationale

Option 1 gives operators kustomize itself rather than an approximation of it, with no patching code of our own beyond the resource round-trip and the invariant check. Option 2 avoids the dependency but reimplements parsing, selection and application that kustomize already gets right, in a format operators cannot carry over from their existing kustomizations. Option 3 runs out of expressive power at the first `remove` or list-index edit. Option 4 replaces typed builders with templates and loses the identity and isolation guarantees along the way.

Protection is checked on the patched object, not on patch paths. After each patch, orbit-agents compares a fixed set of fields against the object it built and rejects the patch if any differ. This catches every way of reaching a field (an SMP merge, a JSON6902 op on the field, or a removal of its parent) with one check.

Failing hard on a broken patch file departs from ADR 2026-06-12's warn-and-continue posture on malformed env var keys. A file explicitly authored into a ConfigMap is under direct operator control and behaves like a broken manifest anywhere else.

---

## Consequences

### Positive

- Operators use kustomize's own `patches:` format and semantics.
- Every field on every provisioned resource outside the protected set is reachable without a code change.
- A patch that does not load, does not apply, names a field the target type does not have, or changes a protected field fails orbit-agents at startup with the filename.
- The `-render` CLI mode previews the final YAML before deploying.
- The Helm chart is a thin passthrough: each `agents.manifestPatches` entry is written into the ConfigMap as is.
- Patch changes reach existing agents on the next reconcile, which runs before every run.

### Negative

- Installations using the `ORBIT_MANIFEST__*` env vars or the chart's earlier `agents.runtimePods.*` values must migrate to `agents.manifestPatches`. No deprecation window is offered.
- orbit-agents depends on the v0.x `sigs.k8s.io/kustomize/api`.
- SMP bodies need a placeholder `metadata.name`, which reads oddly to operators used to name-targeted patches.
- ConfigMap changes require an orbit-agents pod restart. There is no live reload.

### Neutral

- `github.com/tidwall/sjson` and `github.com/evanphx/json-patch/v5` become indirect dependencies.
- `sigs.k8s.io/kustomize/api` and `sigs.k8s.io/kustomize/kyaml` become direct dependencies.

---

## Implementation

### Package

Feature-owned sub-package `backend/internal/agents/manifestpatch/`:

| File | Role |
|------|------|
| `patch.go` | `Patch{File string; Entry types.Patch}` and the Kinds a patch may target, with their apiVersions. |
| `load.go` | `Load(dir)` reads every `*.yaml`/`*.yml` file in filename order, strictly decodes each into a kustomize `types.Patch`, and rejects entries with a file `path`, an empty `patch`, a missing or unknown `target.kind`, or an unparseable selector. A missing directory means no patches. |
| `apply.go` | `Apply[T](obj T, kind string, patches []Patch) (T, error)` runs each patch whose `target.kind` equals `kind` through krusty, one patch at a time, then checks protected fields and strictly decodes the result onto `T`. |
| `protected.go` | The protected field list and the before/after comparison. |

### Patch format

Each file holds one kustomize `patches:` entry. `target.kind` is required. `target.name`, `target.labelSelector` and `target.annotationSelector` narrow it further.

Strategic merge, with a placeholder name:

```yaml
target:
  kind: PersistentVolumeClaim
patch: |
  apiVersion: v1
  kind: PersistentVolumeClaim
  metadata:
    name: any
  spec:
    storageClassName: gp3
```

JSON6902:

```yaml
target:
  kind: Pod
  labelSelector: orbit.io/role=sandbox
patch: |
  - op: replace
    path: /spec/containers/0/resources
    value:
      limits: { cpu: "2", memory: 4Gi }
      requests: { cpu: 500m, memory: 512Mi }
```

Patches apply in filename order. Each runs against the output of the previous one, so a later patch's selector sees labels an earlier patch added, as in kustomize. A target that matches nothing is a no-op.

### Protected fields

A patch is rejected if, compared with the built object, it changes any of:

- `apiVersion`, `kind`, `metadata.name`, `metadata.namespace`, `metadata.ownerReferences`.
- The identity labels `spiffe.io/agent`, `spiffe.io/spire-managed-identity`, `orbit.io/identity-proxy`, `orbit.io/tenant`, `orbit.io/agent-type`, `orbit.io/role`, `orbit.io/run-id` and `orbit.io/agent-slug`, on the resource and on the StatefulSet pod template.
- `spec.selector`, `spec.serviceName`, `spec.scaleTargetRef`, and the StatefulSet's VolumeClaimTemplate names.
- `serviceAccountName`, `automountServiceAccountToken` and `enableServiceLinks` on the Pod and the StatefulSet pod template.
- Pod `securityContext.runAsNonRoot`, `runAsUser` and `seccompProfile`, and every container's `securityContext`.
- The sandbox Pod's PVC `claimName`s and the `volumeMounts` of every container and init container.

Fields absent from both objects are equal, so a patch may add volumes, containers without a security context, tolerations and so on. Adding a container that mounts volumes changes the `volumeMounts` list and is rejected; this keeps a patch from mounting the shared workspace PVC without the per-run `subPath`.

### Wiring into `K8sProvisioner`

- `K8sProvisionerConfig.Patches []manifestpatch.Patch` replaces `ManifestOverrides`.
- `ensureServiceAccount`, `ensureStatefulSet` (and its `VolumeClaimTemplates[0]` as `PersistentVolumeClaim`), `ensureHeadlessService`, `ensureHPA`, `ensurePDB` and `ProvisionSandbox` call `manifestpatch.Apply` on the built object.
- `ProvisionAgentType` runs on agent-type create and at every orbit-agents startup. Run start creates only missing resources ([Run Scheduling](2026-10-12-run-scheduling.md)). When the resource already exists, `ProvisionAgentType` writes the patched object's mutable fields onto the live one:

  | Kind | Synced on reconcile | Create-only |
  |------|--------------------|-------------|
  | ServiceAccount | labels, annotations | |
  | StatefulSet | the full pod template, keeping the live session-manager image that redeploy pins by digest | replicas (owned by the HPA), selector, VolumeClaimTemplates, other StatefulSet spec fields |
  | Service | labels, annotations, ports | clusterIP, selector |
  | HorizontalPodAutoscaler | labels, annotations, spec | |
  | PodDisruptionBudget | labels, annotations, minAvailable | selector |

  Sandbox Pods are created per run and always carry the current patches.

### Loading at startup

`backend/cmd/kora-agents/main.go` reads patches from `ORBIT_MANIFEST_PATCH_DIR` (default `/etc/orbit/manifest-patches`), then renders every patchable resource, sandbox Pod included, for a stand-in agent type. A patch that fails to load or apply, or that changes a protected field, fails the pod at boot. A patch scoped by name or label to a real agent type is a no-op in that pass and is checked again when it applies at provision time. The provisioner config is built by one function shared by the server and the render mode, so both see the same images, pull secrets, trust domain and ports.

orbit-agents logs a warning for every `ORBIT_MANIFEST__*` env var it finds, since those are ignored.

### Debug render mode

`orbit-agents -render -agent-slug=<slug> [-agent-type-id=<id>] [-tenant-id=<id>] [-namespace=<ns>] [-patch-dir=<dir>] [-sandbox]` loads the service config and patches, runs the same startup validation, calls `agents.RenderResources` and prints each object as YAML separated by `---`. It needs no kubeconfig or database. `-agent-type-id` sets the `orbit.io/agent-type` label so patches selecting on it match; it defaults to `render-<slug>`. `metadata.ownerReferences` is omitted: the runtime resolves it from live OrbitTenant and StatefulSet lookups.

### Helm chart

- `templates/agents/patches-configmap.yaml` renders `agents.manifestPatches` into the `orbit-agents-manifest-patches` ConfigMap. `agents.manifestPatches` is a map from a bare patch name to a kustomize `patches:` entry; each entry is written as `<key>.yaml`, so keys decide apply order.
- `templates/agents/deployment.yaml` drops the `ORBIT_MANIFEST__*` env stanzas, mounts the ConfigMap at `/etc/orbit/manifest-patches`, and adds a `checksum/patches` pod annotation.
- `values.yaml` removes `agents.runtimePods.*` and adds `agents.manifestPatches: {}`.

Example:

```yaml
agents:
  manifestPatches:
    50-imagepullsecret:
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
              imagePullSecrets:
                - name: acme-registry
    51-nodeselector:
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

### Deletion

Removed, with no deprecation window:

- `backend/internal/agents/k8s_overrides.go` and its test.
- `K8sProvisionerConfig.ManifestOverrides` and the `ParseManifestOverrides` block in `main.go`.
- Every `ORBIT_MANIFEST__*` env stanza and the `agents.runtimePods.*` values in the Helm chart.

This ADR supersedes ADR 2026-06-12.

### Migration Path

- `agents.runtimePods.*` users write the equivalent patches under `agents.manifestPatches`.
- Users of raw `ORBIT_MANIFEST__*` env vars translate each into a patch under `agents.manifestPatches` and preview the result with `-render`.

### Timeline

Ships as a single PR covering the Go package, the Helm chart and the ADR set. The rollout is a rolling restart of `orbit-agents`; no tenant coordination is required.

---

## Validation

### Success Metrics

- With no patches configured, every provisioned object is identical to the pre-change build.
- `make check` passes, with table-driven tests covering SMP merges, `$patch: delete`, JSON6902, selector matching, sequential application, strict decoding, protected-field rejection and loader errors.
- `orbit-agents -render -patch-dir=<dir> -agent-slug=coder -sandbox | kubectl apply --dry-run=server -f -` is accepted by the local k3d API server.

### Monitoring

- The startup log records the loaded patch count; a drop to zero after a chart change points at a mis-mounted ConfigMap.
- Provisioning failures surface as structured errors on the `agents.k8s.*` spans.

---

## Related Decisions

- [K8s Manifest Field Overrides via Environment Variables](2026-06-12-k8s-manifest-overrides-via-env-vars.md): superseded by this ADR.
- [Agent Execution Infrastructure](2026-05-11-agent-execution-infrastructure.md): the Kubernetes resources being patched.
- [orbit-agents Service Architecture](2026-05-11-orbit-agents-service-architecture.md): overall orbit-agents architecture.

---

## References

- [Kustomize patches reference](https://kubectl.docs.kubernetes.io/references/kustomize/kustomization/patches/)
- [`sigs.k8s.io/kustomize/api/krusty`](https://pkg.go.dev/sigs.k8s.io/kustomize/api/krusty)
- [Flux Kustomize Controller `spec.patches`](https://fluxcd.io/flux/components/kustomize/kustomizations/#patches)

---

**Last Updated:** 2026-09-15
