# Config-Driven Kustomize-Style Manifest Patches for orbit-agents

<!-- Filename: 2026-09-15-manifest-patches-configmap.md  (decision date + slug; no sequential number) -->

**Status:** Proposed
**Date:** 2026-09-15
**Deciders:** Tom Morelly
**Technical Story:** Productionise the operator surface for customising the Kubernetes resources orbit-agents provisions in tenant namespaces. Supersedes [2026-06-12: K8s Manifest Field Overrides via Environment Variables](2026-06-12-k8s-manifest-overrides-via-env-vars.md).

---

## Context

`orbit-agents` provisions a fixed set of Kubernetes resources for each managed agent type — a `StatefulSet`, a headless `Service`, a `ServiceAccount`, a `HorizontalPodAutoscaler`, a `PodDisruptionBudget`, and a `PersistentVolumeClaim` embedded as a `VolumeClaimTemplate` on the StatefulSet — plus a `Pod` for every run's sandbox. All manifests are built as typed Go structs in `backend/internal/agents/k8s.go`.

[ADR 2026-06-12](2026-06-12-k8s-manifest-overrides-via-env-vars.md) introduced a per-field override mechanism: env vars of the form `ORBIT_MANIFEST__<Kind>__<name>__<path>=<value>` parsed at startup and applied to the built typed object via a JSON round-trip using `github.com/tidwall/sjson`. The mechanism works for shallow scalar overrides but has hard limits that are showing up as we broaden the operator surface:

- **Poor ergonomics for structured fields.** Anything list-shaped — `imagePullSecrets`, `tolerations`, `resources`, node affinities — must be embedded as a raw JSON string inside an env var value. The Helm chart already does this seven times for `runtimePods.*` (`charts/orbit/templates/agents/deployment.yaml:75-113`) and the pattern does not scale.
- **One env var per field.** Non-trivial customisations bloat the orbit-agents pod spec and cross the fields-per-container-env comfort threshold quickly.
- **No selector semantics beyond `Kind + slug|*`.** No `labelSelector`, no `annotationSelector`, no way to say "every sandbox pod, whatever the slug"; and no way to touch a specific list index (e.g. `container[0].resources`) without JSON-string overrides.
- **No debug/preview path.** An operator authoring an override has to `kubectl apply` and diff by hand — there is no way to render the final manifest before it hits the API server.

We want a productionised, file-driven approach that borrows from a mechanism that is already battle-tested and familiar to every Kubernetes operator: kustomize patches.

### Background

Kustomize's [patches reference](https://kubectl.docs.kubernetes.io/references/kustomize/kustomization/patches/) defines two production-grade patch flavours:

- **Strategic Merge Patch (SMP)** — a partial Kubernetes object; the target is either inferred from the patch's own `apiVersion`/`kind`/`metadata.name`, or supplied via a `target:` selector. List merging is driven by `patchMergeKey`/`patchStrategy` OpenAPI tags baked into the k8s API types.
- **JSON6902** — an RFC 6902 op array (`add`/`remove`/`replace`/`move`/`copy`/`test`) applied against the target selected by `target:`.

Both are widely used across Flux, Argo CD, and Cluster API. Argo CD's `spec.source.kustomize.patches` and Flux's `spec.patches[]` both accept the exact same shape as kustomize's own `patches:` list.

### Problem Statement

Give operators a maintainable, familiar mechanism for customising the Kubernetes resources orbit-agents provisions, with selector semantics rich enough to cover the real needs (image pull secrets, storage class, labels/annotations, resource limits, tolerations, priority class, node selector), and a way to preview the final manifest before deploying.

### Goals

- Operators author patches in a ConfigMap using kustomize's SMP and JSON6902 syntax — the same file format they already know.
- Patches are loaded at orbit-agents startup and applied to every provisioned resource without a code change per new field.
- Selectors match by `kind` + `name` + optional `labelSelector` + optional `annotationSelector`, matching kustomize's semantics.
- A CLI mode renders the final YAML for a given agent slug (plus optional sandbox) so operators can diff patches locally or in CI before deploying.
- Fail loud: a malformed patch file fails startup with the filename in the error. Silent no-ops are a regression from the current design.

### Non-Goals

- Full `kustomization.yaml` surface (`replacements`, `images`, `namePrefix`, `generators`, `components`, `commonLabels`, …). We want the patch mechanism, not the whole build.
- Per-tenant patches. All tenants sharing an orbit-agents pod get the same patches. Per-tenant customisation is a separate ADR if it ever comes up.
- Overriding Istio `DestinationRule` / `AuthorizationPolicy` (created as `unstructured` today). Adding them to the patch surface is a follow-up.
- Editing patches at runtime. Patches are loaded once at startup; a `ConfigMap` change requires an orbit-agents pod restart, driven by a Helm-computed `checksum/patches` annotation.

---

## Decision Drivers

- **Battle-tested UX.** Operators already know kustomize patch syntax; anything else is a learning tax that this platform doesn't need to charge.
- **No process shell-outs.** Embedding the kustomize CLI or the full `sigs.k8s.io/kustomize/api/krusty` engine drags in a large public API surface that is still v0.x. We can get the patch behaviour without the build machinery.
- **Zero new direct dependencies beyond what k8s.io already brings.** `github.com/evanphx/json-patch/v5` and `sigs.k8s.io/yaml` are already indirect in `go.mod`; `k8s.io/apimachinery` is already direct. Promoting the first two to direct imports carries no new supply-chain weight.
- **Deterministic ordering.** Two operators authoring separate patches must be able to reason about which one wins. Kustomize-style lex-sorted filenames with a numeric prefix (`010-*.yaml`, `020-*.yaml`) is the established idiom.
- **Fail loud.** A malformed patch is an operator error at startup, not a silent no-op later.
- **Debug parity.** The renderer must produce the exact same YAML the runtime would produce; the same code path builds both.

---

## Considered Options

### Option 1: Embed the full kustomize engine (`sigs.k8s.io/kustomize/api/krusty`)

**Description:** Bring the kustomize build engine into orbit-agents. Each resource orbit-agents wants to provision becomes an in-memory "base"; operator-supplied `kustomization.yaml` fragments layer on top. The full patch surface plus `replacements`, `images`, `namePrefix`, `commonLabels`, generators, components.

**Pros:**
- Complete parity with kustomize; anything a Flux/Argo user knows carries over.
- Long-term, easiest onboarding for teams who already ship `kustomization.yaml` in git.

**Cons:**
- Adds thousands of lines of imports (`sigs.k8s.io/kustomize/api`, `kyaml`, `resmap`, `resource`, `krusty`) — most of which we do not need.
- `sigs.k8s.io/kustomize/api` is v0.x with an unstable public API; several packages call out that they are subject to change.
- The `kyaml.RNode` intermediate representation is at odds with our client-go typed structs; every apply becomes a two-way marshal.
- The build engine assumes a filesystem layout with `kustomization.yaml` at the root — designing this around orbit-agents' provisioning-time model is awkward.

### Option 2: Strategic Merge Patch only, target inferred from the patch itself (chosen)

**Description:** Support two patch flavours, but only the subset each needs to do the job we actually want done:
- **SMP** — a partial Kubernetes object. Target inferred from the patch's own `apiVersion`/`kind`/`metadata.name`. Applied with `k8s.io/apimachinery/pkg/util/strategicpatch.StrategicMergePatch(orig, patch, typedInstance)` — the schema for `patchMergeKey`/`patchStrategy` comes free from the client-go struct being patched, so no OpenAPI plumbing.
- **JSON6902** — an RFC 6902 op array wrapped in a `{target, patch}` envelope so the target selector travels with the ops. Applied with `github.com/evanphx/json-patch/v5.DecodePatch(...)` + `ApplyWithOptions({AllowMissingPathOnRemove: true, EnsurePathExistsOnAdd: true})`.

Selectors are `kind` + optional `name` + optional `labelSelector` + optional `annotationSelector`; label/annotation selectors are parsed with `k8s.io/apimachinery/pkg/labels.Parse`.

**Pros:**
- Uses the same syntax operators already know from kustomize.
- Zero new direct dependencies; every library needed is already in `go.mod` as an indirect.
- Small surface: ~350 LOC across a self-contained sub-package.
- Fails closed on malformed input; no silent no-ops.
- The two flavours together cover every override case we have identified (image pull secrets, storage class, labels/annotations, resource limits, tolerations, priority class, node selectors, sandbox container resources).

**Cons:**
- Not identical to kustomize semantically — no `replacements`, no `namePrefix`, no automatic labelling. Operators who want those must fold them into their existing kustomize workflow upstream.
- SMP's list-merge semantics depend on the client-go struct tags; a change to the k8s API version could shift merge behaviour on some fields. Mitigated by test coverage on the shapes we actually produce.

### Option 3: SMP only

**Description:** Ship strategic merge patch, drop JSON6902 for a v1.

**Pros:**
- Smallest possible surface (~150 LOC).
- SMP alone covers most named knobs (image pull secrets, storage class, labels/annotations, tolerations, node selectors).

**Cons:**
- No precise list-index editing. Overriding `spec.containers[0].resources` on the sandbox container without disturbing anything else is awkward in SMP because the pod has one container today but the merge key is `name` — every operator would have to know and match it exactly.
- No `remove` semantics: SMP can only merge, not delete an existing field without the `$patch: delete` directive (which we would then have to support). JSON6902 gives us `op: remove` for free.
- Every operator hitting a limit will reach for JSON6902 anyway; adding it later is a breaking change to the file layout.

### Option 4: Full user-supplied manifests via a Helm-shipped ConfigMap

**Description:** Instead of patching over orbit-agents' built manifests, the operator provides the *complete* Kubernetes manifests (StatefulSet, Service, ServiceAccount, HPA, PDB, sandbox Pod) as YAML in a Helm-managed ConfigMap. orbit-agents reads them at startup, mounts them at `/etc/orbit/manifest-templates`, and applies them directly (with minimal substitution) instead of building via typed Go structs.

**Feasibility analysis.** For this to work, every field a manifest needs must either be static (identical across every tenant + agent type) or knowable at ConfigMap-generation time. Auditing what `buildSessionManagerStatefulSet` and `buildSandboxPod` in `backend/internal/agents/k8s.go` actually compute at provisioning time:

Fields orbit-agents computes per-agent-type at runtime:
- `metadata.name` = agent type slug (`at.Slug`) — StatefulSet, Service, ServiceAccount, HPA, PDB all named after it.
- `metadata.namespace` = the tenant namespace (`cfg.TenantNamespaceFn(at.TenantID)`), one namespace per tenant.
- `metadata.labels` = `{app: {slug}, orbit.io/agent-type: {at.ID}, orbit.io/tenant: {at.TenantID}}`; the agent-type UUID is only known once the row is written.
- `metadata.ownerReferences` = the OrbitTenant CR's UID, looked up from Kubernetes at provision time.
- StatefulSet `spec.serviceName` = agent slug (must equal the headless Service name for DNS).
- StatefulSet `spec.selector.matchLabels` + pod template `metadata.labels` share `podLabels(at)`, which includes `orbit.io/agent-type: {at.ID}` (UUID).
- StatefulSet pod template `spec.serviceAccountName` = agent slug.
- StatefulSet pod template `spec.containers[0].envFrom` references a per-tenant `{at.TenantID}-config` ConfigMap.
- StatefulSet pod template `spec.containers[0].env` includes URLs built from `p.cfg.SystemNamespace` (`ANTHROPIC_BASE_URL`, `ORBIT_AGENTS_URL`).
- HPA `spec.scaleTargetRef.name` = agent slug.

Fields orbit-agents computes per-run at runtime for the sandbox Pod:
- `metadata.name` = `sandbox-{run.ID}` — one Pod per run.
- `metadata.labels` = `{orbit.io/role: sandbox, orbit.io/run-id: {run.ID}, orbit.io/agent-slug: {slug}}`; the run ID is a database-generated UUID.
- PVC volume reference = `workspaces-{slug}-{run.PodOrdinal}` — the StatefulSet's per-ordinal claim name.
- Container `volumeMounts[…].subPath` = `run.ID`, so this run's sandbox pod only ever mounts its own workspace subdirectory.
- Container env includes `WORKSPACE` (subpath-scoped), `SANDBOX_EXEC_PORT` from `cfg.SandboxGRPCPort`, per-tenant CA-hint env vars pointing at `sandboxTenantCAPath`, and a base64-encoded kubeconfig blob built from the granted Kubernetes clusters (`kubeconfig.Encode(kubeClusters)`).
- Owner reference on the sandbox Pod = the parent StatefulSet's UID, looked up from Kubernetes at provision time.

None of these fields is static across tenants or agent types. A "fully user-supplied manifest" can therefore never be literal YAML; it can only be a **template** that orbit-agents renders with the runtime variables above. In other words, Option 4 is not "the user provides plain manifests"; it is "the user provides a Go/Helm template of the manifest, and orbit-agents runs a template engine every time it provisions".

That is a much bigger surface than Option 2:

- Adds a template engine (Helm's Sprig, Go `text/template`, or CEL) as a runtime dependency of orbit-agents.
- Every field orbit-agents currently guarantees (SPIFFE-attested pod labels, per-run subpath isolation, ownerReferences for GC, envFrom on the per-tenant ConfigMap) becomes an operator-authored template variable. A missing or misspelled placeholder silently produces a broken pod (spec.serviceName mismatched with the Service name → StatefulSet won't get DNS; wrong subPath → runs share a workspace directory).
- Loses the strong typing that keeps `k8s_podsecurity_test.go` honest: the built Go struct is a compile-time check that securityContext, seccomp profile, capabilities-drop, and read-only-root are set correctly on every provisioned pod. A YAML template is a string until it's on the cluster.
- Removes the layered-defence property of the current model: patches can only *override* fields, they cannot omit the security invariants orbit-agents bakes in. Full-manifest templating deletes that guarantee.

**Pros:**
- Maximum control for operators who want to hand-craft every field.
- Conceptually simpler for someone who already ships Helm charts: "give me the ConfigMap, I'll fill in the YAML."

**Cons:**
- Not truly plain YAML — requires a template engine plus a documented variable contract, or per-tenant regeneration on every agent-type change.
- Deletes the compile-time guarantees on pod security, SPIFFE labels, per-run subpath isolation, and cross-resource identity (StatefulSet ↔ Service ↔ SA ↔ HPA).
- Every future field orbit-agents needs to add (a new SPIRE label, a new sidecar-injection annotation, a new env var pointing at a new platform component) becomes a breaking template contract change instead of a Go code change.
- Debug-render (`orbit-agents -render`) becomes weaker: the tool can only echo the template with variables substituted, not verify against typed k8s API objects.

---

## Decision

**Chosen Option:** Option 2 — Strategic Merge Patch plus JSON6902, target selector by kind + name + optional label/annotation selectors, files loaded from a ConfigMap-mounted directory.

### Rationale

Option 2 is the smallest surface that still matches what operators expect from a Kubernetes-native patching mechanism. SMP alone (Option 3) will run out of expressive power the first time somebody needs to remove a field or edit a specific list index; every real deployment reaches JSON6902 eventually. The full kustomize engine (Option 1) is the opposite extreme — it drags in a v0.x public API to get a feature we can implement in ~350 lines with `strategicpatch` and `evanphx/json-patch/v5`, both of which are already in our indirect deps via `k8s.io/*`. Full user-supplied manifests (Option 4) sound simpler than they are: the feasibility analysis above shows that every provisioned resource depends on runtime state orbit-agents alone knows, so Option 4 is not "the operator gives us YAML" but "the operator gives us a template plus a variable contract, and we lose the compile-time guarantees on identity, SPIFFE labelling, subpath isolation, and pod security along the way". Patches keep the strong-typed manifest builder as the source of truth and let operators layer on top, which is the smaller change with the fewer failure modes.

The chosen apply libraries are exactly what kustomize itself uses under the hood, so the semantic behaviour matches what an operator would see if they ran `kustomize build` locally. Selector matching is intentionally kept narrower than kustomize's full selector spec — no regex on names, no group/version matching — because orbit-agents provisions a small, closed set of Kinds and the extra machinery would be surface for no benefit.

Failing hard on malformed patch files is a deliberate departure from ADR 2026-06-12's WARN-and-continue posture on malformed env var keys. Env vars are a wide, ambient surface where a stray typo elsewhere in the shell environment could reasonably be non-fatal; a file explicitly authored into a ConfigMap is under direct operator control, and a broken file should behave like a broken YAML manifest anywhere else.

---

## Consequences

### Positive

- Operators use a syntax they already know; no new mental model.
- Every field on every provisioned resource is reachable without a code change; per-list-index precision is available via JSON6902.
- Malformed patches fail at startup with the offending filename, not later as a silent bad manifest. `manifestpatch.Validate` runs after `Load` and rejects unknown target Kinds (typos like `Deployement`), unparseable JSON6902 op arrays, malformed selectors, and SMP bodies that don't decode onto the target type.
- The `-render` CLI mode gives operators and CI a way to preview the final YAML before deploying — closes the gap ADR 2026-06-12 flagged as a Negative Consequence.
- The Helm chart is a thin passthrough: `agents.manifestPatches` writes each entry straight into the ConfigMap, no chart-side translation or generated files. Operators write the patch they want in the syntax they already know.
- The strong-typed manifest builder in `backend/internal/agents/k8s.go` remains the source of truth for the fields orbit-agents must control (pod security, SPIFFE labels, per-run subpath isolation, cross-resource identity), and patches layer on top — the layered-defence property Option 4 gives up.

### Negative

- Existing installations that consumed the `ORBIT_MANIFEST__*` env var API, or the chart's earlier `agents.runtimePods.*` named knobs, must migrate to a patch under `agents.manifestPatches`. No deprecation window is offered; this is a breaking change for both.
- The strategic-merge behaviour depends on the client-go struct tags; a `k8s.io/api` bump could subtly change list-merge outcomes on some fields. Test coverage on our actual provisioned Kinds bounds the risk.
- ConfigMap changes require an orbit-agents pod restart, driven by a Helm-computed `checksum/patches` annotation. There is no live reload.

### Neutral

- `github.com/tidwall/sjson` drops from a direct dependency (its only usage was in the deleted `k8s_overrides.go`).
- `github.com/evanphx/json-patch/v5` and `sigs.k8s.io/yaml` move from indirect to direct in `backend/go.mod`.

---

## Implementation

### Patch data model

New feature-owned sub-package `backend/internal/agents/manifestpatch/`:

| File | Role |
|------|------|
| `patch.go` | `Patch` struct: `Kind, Name, LabelSelector, AnnotationSelector string; Body []byte; Flavour PatchFlavour`. Flavour is `strategicMerge` or `jsonPatch`. |
| `load.go` | `Load(dir string) ([]Patch, error)` reads every `*.yaml`/`*.yml` file in `dir` (lex-sorted, non-recursive), autodetects flavour, wraps errors with the filename. Missing directory returns `(nil, nil)` — zero patches is a valid state. Malformed YAML fails hard. |
| `apply.go` | `Apply[T any](obj T, patches []Patch) (T, error)` filters patches to those matching `obj`'s kind/name/labels/annotations, then dispatches to the strategic-merge or JSON6902 applier. |
| `selector.go` | `matches(patch, kind, name, labels, annotations) bool` — kind and name equality (empty name = wildcard), labels and annotations via `k8s.io/apimachinery/pkg/labels.Parse`. |

Apply implementations:

```go
// Strategic merge — schema comes free from the typed struct.
import "k8s.io/apimachinery/pkg/util/strategicpatch"
import "sigs.k8s.io/yaml"

func applyStrategic[T any](obj T, patchYAML []byte) (T, error) {
    patchJSON, err := yaml.YAMLToJSON(patchYAML)
    if err != nil {
        return obj, fmt.Errorf("patch yaml -> json: %w", err)
    }
    origJSON, err := json.Marshal(obj)
    if err != nil {
        return obj, fmt.Errorf("marshal typed obj: %w", err)
    }
    merged, err := strategicpatch.StrategicMergePatch(origJSON, patchJSON, obj)
    if err != nil {
        return obj, fmt.Errorf("strategic merge: %w", err)
    }
    var out T
    if err := json.Unmarshal(merged, &out); err != nil {
        return obj, fmt.Errorf("unmarshal merged: %w", err)
    }
    return out, nil
}
```

```go
// JSON6902 — direct RFC 6902 apply.
import jsonpatch "github.com/evanphx/json-patch/v5"

func applyJSON6902[T any](obj T, opsYAML []byte) (T, error) {
    opsJSON, err := yaml.YAMLToJSON(opsYAML)
    if err != nil { return obj, fmt.Errorf("ops yaml -> json: %w", err) }
    p, err := jsonpatch.DecodePatch(opsJSON)
    if err != nil { return obj, fmt.Errorf("decode json6902: %w", err) }
    origJSON, err := json.Marshal(obj)
    if err != nil { return obj, fmt.Errorf("marshal typed obj: %w", err) }
    opts := jsonpatch.NewApplyOptions()
    opts.AllowMissingPathOnRemove = true
    opts.EnsurePathExistsOnAdd = true
    merged, err := p.ApplyWithOptions(origJSON, opts)
    if err != nil { return obj, fmt.Errorf("apply json6902: %w", err) }
    var out T
    if err := json.Unmarshal(merged, &out); err != nil { return obj, fmt.Errorf("unmarshal merged: %w", err) }
    return out, nil
}
```

### Patch file formats

Strategic Merge — file looks like a Kubernetes object; target inferred from its own `apiVersion` + `kind` + `metadata.name`:

```yaml
# 050-pvc.yaml
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: workspaces        # matches the StatefulSet's VolumeClaimTemplates[0]
spec:
  storageClassName: gp3
  accessModes: [ReadWriteOnce]
```

JSON6902 — wrapped so the target selector is co-located with the ops:

```yaml
# 040-sandbox-resources.yaml
target:
  kind: Pod
  labelSelector: orbit.io/role=sandbox
patch:
  - op: replace
    path: /spec/containers/0/resources
    value:
      limits: { cpu: "2", memory: 4Gi }
      requests: { cpu: 500m, memory: 512Mi }
```

Flavour autodetection: top-level `target:` **and** `patch:` keys → JSON6902; anything else (a `kind:`/`apiVersion:` root) → SMP. Anything that fails both parses is a malformed file, fatal at startup.

Filenames are lex-sorted before apply, so a `NNN-` numeric prefix gives deterministic ordering. Non-recursive: files in nested subdirectories are ignored.

### Wiring into `K8sProvisioner`

- Replace `ManifestOverrides ManifestOverrides` on `K8sProvisionerConfig` with `Patches []manifestpatch.Patch` in `backend/internal/agents/k8s.go`.
- Delete `backend/internal/agents/k8s_overrides.go` and its test.
- At every existing override call site, swap `applyOverrides(obj, overridesForSlug(...))` for `manifestpatch.Apply(obj, filterForResource(p.cfg.Patches, kind, name, labels, annotations))`:
  - `ensureServiceAccount` (`k8s.go:1160`)
  - `ensureStatefulSet` (`k8s.go:1188`) plus the inline PVC apply on `VolumeClaimTemplates[0]` (`k8s.go:1192-1197`)
  - `ensureHeadlessService` (`k8s.go:1384`)
  - `ensureHPA` (`k8s.go:1449`)
  - `ensurePDB` (`k8s.go:1480`)
  - `ProvisionSandbox` for the `Pod` (`k8s.go:373-378`)
- The resource's own labels (already computed via `agentTypeLabels(at)`) and annotations flow into `Apply` so label/annotation selectors resolve correctly.
- SMP patches with `metadata.name: workspaces` target the PVC template regardless of the parent StatefulSet slug, which is the desired wildcard behaviour for an operator patch targeting `.storageClassName` or `.accessModes` on every tenant's workspace.

### Loading at startup

In `backend/cmd/orbit-agents/main.go`, replace the current `ParseManifestOverrides` block with:

```go
patchDir := os.Getenv("ORBIT_MANIFEST_PATCH_DIR")
if patchDir == "" {
    patchDir = "/etc/orbit/manifest-patches"
}
patches, err := manifestpatch.Load(patchDir)
if err != nil {
    return fmt.Errorf("load manifest patches from %s: %w", patchDir, err)
}
if err := manifestpatch.Validate(patches); err != nil {
    return fmt.Errorf("validate manifest patches in %s: %w", patchDir, err)
}
logger.Info(ctx, "loaded manifest patches",
    logging.Field{Key: "dir", Value: patchDir},
    logging.Field{Key: "count", Value: len(patches)},
)
```

Missing dir → 0 patches, no error. Malformed YAML → hard fail at startup. `manifestpatch.Validate` dry-checks every patch's shape (parseable body, known target Kind, decodable JSON6902 op array, parseable label/annotation selectors, SMP body that unmarshals onto the target type) so a broken patch fails the orbit-agents pod at boot instead of on the first agent-type provision.

### Debug render mode

The `orbit-agents` binary has no CLI parsing today. Add stdlib `flag` at the top of `main()`:

```go
var (
    renderMode      = flag.Bool("render", false, "render final manifests to stdout and exit")
    renderPatchDir  = flag.String("patch-dir", "/etc/orbit/manifest-patches", "manifest patches directory")
    renderAgentSlug = flag.String("agent-slug", "", "agent type slug to render (required with -render)")
    renderNamespace = flag.String("namespace", "tenant-example", "tenant namespace for the rendered manifests")
    renderSandbox   = flag.Bool("sandbox", false, "also render a sample sandbox pod")
)
flag.Parse()
if *renderMode {
    return renderManifests(*renderPatchDir, *renderAgentSlug, *renderNamespace, *renderSandbox)
}
```

`renderManifests` (new file `backend/cmd/orbit-agents/render.go`) loads patches, calls a new package-level `agents.RenderResources(at AgentType, namespace string, patches []manifestpatch.Patch) ([]runtime.Object, error)` that reuses the same `build*` helpers the runtime uses, marshals each object with `sigs.k8s.io/yaml.Marshal`, and prints them separated by `---\n`. No kubeconfig, database, or observability initialisation.

### Helm chart migration

`charts/orbit/templates/agents/`:

- New `patches-configmap.yaml`: renders `agents.manifestPatches` into a `ConfigMap` named `orbit-agents-manifest-patches`. `agents.manifestPatches` is a map keyed by a bare patch name (no extension); the chart writes each entry into the ConfigMap as `<key>.yaml`, so the key directly controls apply order. Each entry is an object with `content` (the patch body, verbatim), `required` in the template so a missing value fails `helm template`/`helm upgrade` immediately. The chart does no translation of its own — the operator's `content` is written byte-for-byte, so the patch the operator authors is the patch orbit-agents loads.

- `deployment.yaml`: drop the seven `ORBIT_MANIFEST__*` env stanzas (lines 75-113), mount the `orbit-agents-manifest-patches` ConfigMap at `/etc/orbit/manifest-patches`, and add a `checksum/patches` pod annotation so `helm upgrade` rolls the pods when patches change.

- `values.yaml`: remove the `agents.runtimePods.*` block entirely; add `agents.manifestPatches: {}` for operator-supplied patches.

Both the old `ORBIT_MANIFEST__*` env var API and the chart's `agents.runtimePods.*` named knobs are removed with no deprecation window. Operators using either must write the equivalent patch under `agents.manifestPatches` themselves — see the examples below and `deploy/reference/values/orbit.yaml`.

#### Operator-authored patch examples

Two patches every operator eventually needs — an image pull secret for a private registry, and a node selector pinning agent pods to a node pool:

```yaml
agents:
  manifestPatches:
    50-sandbox-imagepullsecret:
      content: |
        apiVersion: apps/v1
        kind: StatefulSet
        spec:
          template:
            spec:
              imagePullSecrets:
                - name: acme-registry
    51-sandbox-nodeselector:
      content: |
        apiVersion: apps/v1
        kind: StatefulSet
        spec:
          template:
            spec:
              nodeSelector:
                disktype: ssd
```

The map key is a bare patch name (no extension); the chart writes it into the ConfigMap as `<key>.yaml`, so the key directly controls apply order. The patch body itself declares its own target (`kind`, and `metadata.name` or a JSON6902 `target:` block) — there is no separate chart-side configuration of which manifest a patch applies to. See `deploy/reference/values/orbit.yaml` for a working example wired into the reference installation.

### Deletion

Removed entirely, no deprecation window:

- `backend/internal/agents/k8s_overrides.go` and its test
- `K8sProvisionerConfig.ManifestOverrides`
- The `ParseManifestOverrides` block in `main.go`
- Every `ORBIT_MANIFEST__*` env stanza in the Helm chart
- `github.com/tidwall/sjson` from `backend/go.mod` (grep-confirmed only used in the deleted file)

This ADR supersedes ADR 2026-06-12.

### Migration Path

- Operators using the chart's earlier `agents.runtimePods.*` named knobs: write the equivalent patch under `agents.manifestPatches` (see the operator-authored examples above and `deploy/reference/values/orbit.yaml`).
- Operators who added raw `ORBIT_MANIFEST__*` env vars to the orbit-agents pod spec: translate each into a patch under `agents.manifestPatches`. `docs/guides/local-development.md` has a section on the `-render` mode so operators can preview the result before deploying.

### Timeline

Ship as a single PR that touches the Go package, the Helm chart, and the ADR set together. Both `ORBIT_MANIFEST__*` and `agents.runtimePods.*` are removed with no deprecation window; operators on either must write a replacement patch before upgrading. The rollout itself is a rolling restart of `orbit-agents`; no coordination with tenants required.

---

## Validation

### Success Metrics

- Every provisioned Kind still round-trips through `manifestpatch.Apply` when patches match, and stays byte-identical to the pre-change build when no patches are configured.
- `make check` passes with the sub-package's table-driven tests exercising SMP list-merge, JSON6902 `add`/`remove`/`replace`/`move`, label-selector matching, missing-path-on-remove, and malformed-file fatal paths.
- `orbit-agents -render -patch-dir=./testdata/patches -agent-slug=coder -sandbox | kubectl apply --dry-run=server -f -` accepts every rendered object against the local k3d API server.

### Monitoring

- Startup log line records the loaded patch count; a jump to zero after a chart change is the smell-test for a mis-mounted ConfigMap.
- Provisioning failures continue to surface as structured errors on the `agents.k8s.Ensure*` spans; a patch that produces an invalid manifest fails the first agent type it applies to, not silently later.

---

## Related Decisions

- [K8s Manifest Field Overrides via Environment Variables](2026-06-12-k8s-manifest-overrides-via-env-vars.md) — superseded by this ADR
- [Agent Execution Infrastructure](2026-05-11-agent-execution-infrastructure.md) — describes the K8s resources being patched
- [orbit-agents Service Architecture](2026-05-11-orbit-agents-service-architecture.md) — overall orbit-agents architecture

---

## References

- [Kustomize patches reference](https://kubectl.docs.kubernetes.io/references/kustomize/kustomization/patches/)
- [`k8s.io/apimachinery/pkg/util/strategicpatch`](https://pkg.go.dev/k8s.io/apimachinery/pkg/util/strategicpatch)
- [`github.com/evanphx/json-patch/v5`](https://pkg.go.dev/github.com/evanphx/json-patch/v5)
- [`sigs.k8s.io/yaml`](https://pkg.go.dev/sigs.k8s.io/yaml)
- [Cluster API `ClusterClass` patches](https://cluster-api.sigs.k8s.io/tasks/experimental-features/cluster-class/write-clusterclass) — prior art on the `{target, patch}` envelope for provisioning-time patches
- [Flux Kustomize Controller `spec.patches`](https://fluxcd.io/flux/components/kustomize/kustomizations/#patches)

---

**Last Updated:** 2026-09-15
