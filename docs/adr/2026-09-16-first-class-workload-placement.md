# First-Class Customisation Points for Provisioned Agent Workloads

<!-- Filename: 2026-09-16-first-class-workload-placement.md  (decision date + slug; no sequential number) -->

**Status:** Accepted
**Date:** 2026-09-16
**Deciders:** Justin Wilkin, Tom Morelly
**Technical Story:** Give the customisations every enterprise deployment needs a supported values surface, instead of requiring kustomize patches for them. Amends [2026-09-15: Config-Driven Kustomize Manifest Patches](2026-09-15-manifest-patches-configmap.md). Motivated by clear-route/orbit#309, #308 and #311.

---

## Context  

`orbit-agents` builds the resources it provisions in tenant namespaces as typed Go structs in `backend/internal/agents/k8s.go`: a `StatefulSet` per agent type with the workspace `volumeClaimTemplate`, a headless `Service`, `ServiceAccount`, `HorizontalPodAutoscaler`, `PodDisruptionBudget`, and a `Pod` per run for the sandbox.

[2026-09-15](2026-09-15-manifest-patches-configmap.md) made kustomize `patches:` entries the mechanism for customising any field of those resources, replacing per-field env vars. That decision holds for the long tail. What it did not settle is which fields are common enough that requiring a patch is the wrong default.

### Background

Installing 0.1.0-rc.4 into a production cluster exercised the mechanism properly for the first time. Every customisation that deployment needed, and every one a regulated enterprise asks for on day one, had to be written as a patch: which node pool each workload runs on, what storage class backs the workspace, what labels and annotations land on tenant pods for their own tooling.

Patches handled it. Three problems sat around them:

1. **The chart's own warm-start workloads could not follow the sandboxes.** `agents.imageWarmer` and `agents.warmHeadroom` are Helm-owned workloads in the system namespace, not resources `orbit-agents` provisions, so no patch can reach them. They took their scheduling from the `agents` block, which places the `orbit-agents` Deployment, so they warmed nodes no sandbox runs on. #311 gives them their own scheduling keys.

2. **Placement ends up expressed twice**, once in the sandbox patch and once in the warmer values, with nothing checking the two agree. When they drift the platform holds a warm node in a pool no sandbox schedules onto: the deployment pays for the node and still cold-starts, with no error anywhere.

3. **The coupling between placement and workspace access mode is invisible.** Splitting the pools makes `ReadWriteMany` mandatory, and a `ReadWriteOnce` workspace fails at attach time with a Multi-Attach error on the first run rather than at configuration time. #311 adds a guard, but it guards configuration the platform does not model.

### What is already first-class

Not everything needs adding, and the ADR should not pretend otherwise:

- **Image registry and tags.** `global.imageRegistry` and `global.imageTag` default the provisioned images, and `agents.config.sessionManagerImage` and `agents.config.sandboxImage` override them outright. An air-gapped registry is already a values change.
- **Image pull secrets.** `global.imagePullSecrets` drives three paths that have to agree: `orbit-agents` sets them on provisioned pods, the operator replicates the Secrets into every tenant namespace, and the pod webhook appends them to tenant pods. This stays a single global list precisely because a per-workload override would fragment the replication that makes it work.

### What is not

- **Placement.** Node selectors, tolerations, affinity, topology spread and priority class for the session-manager `StatefulSet` and the sandbox `Pod`. Patches only.
- **Pod labels and annotations on provisioned pods.** The chart's `agents.podLabels` and `agents.podAnnotations` apply to the `orbit-agents` Deployment, not to anything in a tenant namespace. Enterprises need these for cost allocation, network policy selection, backup exclusion and their own admission controllers.
- **Workspace storage.** Storage class, access modes and size. The deployment guide lists the storage class as a cluster prerequisite, then asks for a kustomize patch to set it.
- **Service account annotations on the agent's ServiceAccount.** The cloud identity path (IRSA, Workload Identity) is an annotation on a `ServiceAccount`, and `buildServiceAccount` has no annotations at all. Note this applies to the session-manager only: sandbox Pods set no `ServiceAccountName` and run under the tenant's `default` account.

### Problem Statement

The customisations most deployments need are expressible only as opaque patch documents. The platform cannot read them, so it cannot validate them, cannot derive dependent configuration such as warm capacity, and cannot describe the resulting topology as supported. For an evaluator, "set your node pool" reading as "learn our patch format" is a poor first impression of an otherwise declarative chart.

### Goals

- A values surface for the customisations most deployments need: placement, pod labels and annotations, workspace storage, and service account annotations.
- One source of truth for sandbox placement, which the chart's warm capacity derives from.
- An incompatible combination of placement and workspace access mode fails when the chart renders, not when a run starts.
- Manifest patches keep working unchanged and still apply last.
- Identity, security context and workspace isolation stay owned by the platform, exactly as patches already guarantee.

### Non-Goals

- Per-agent-type or per-tenant values. Sandbox resources are fixed for every sandbox (clear-route/orbit#310); that needs a model and API change and is decided separately.
- Replacing manifest patches, or moving every provisioned field into values. The long tail stays with patches.
- A second pull secret mechanism, for the replication reason above.
- Provisioning node pools. Karpenter, Cluster Autoscaler, taints and labels stay the operator's.
- Changing the workspace of an existing agent. A `StatefulSet`'s `volumeClaimTemplates` are immutable and its claims outlive it (clear-route/orbit#308).

---

## Decision Drivers

* **The common case should be declarative.** A values key for what most deployments change; patches for the rest.
* **Single source of truth.** Placement stated twice will drift, and the drift is silent and billable.
* **Fail at configuration time.** The failure mode being replaced is a Multi-Attach error during someone's first run.
* **A bounded surface.** Only fields that are both commonly needed and safe for the platform to reason about earn a key.
* **Structured values cannot ride environment variables.** `orbit-agents` config is `base.yaml` plus string env overrides. Selectors, tolerations, affinity and label maps are maps and lists, which is what [2026-06-12](2026-06-12-k8s-manifest-overrides-via-env-vars.md) failed to express and [2026-09-15](2026-09-15-manifest-patches-configmap.md) replaced.
* **Invariants hold regardless of mechanism.** Values must not become a way around the protections patches respect.

---

## Considered Options

### Option 1: Status quo, patches only

**Description:** Keep every customisation as a patch. Keep the #311 warmer scheduling keys and the startup guard.

**Pros:**
- No new configuration surface, no new delivery mechanism.
- Already implemented.

**Cons:**
- Day-one customisations require learning a patch format, with a placeholder `metadata.name` and target selectors.
- Placement stays duplicated between the sandbox patch and the warmer values, with drift silent.
- The chart cannot validate placement against workspace access modes.
- The topology cannot honestly be called supported: the platform has no model of it.

### Option 2: First-class values for the common customisations, delivered by a rendered ConfigMap, patches applied last (chosen)

**Description:** Add `agents.workloads.*` (placement, pod labels and annotations, service account annotations) and `agents.workspace.*` to the chart. Render them into a ConfigMap mounted into the `orbit-agents` pod, load them into `K8sProvisionerConfig`, and apply them in the builders before patches. The warmers default their scheduling from `agents.workloads.sandbox`.

**Pros:**
- The customisations most deployments need are ordinary Helm values.
- Sandbox placement is stated once and warm capacity follows it.
- The chart can `fail` at render time on the combination that breaks at runtime.
- Reuses the ConfigMap, mount and `checksum/` rollout pattern manifest patches already use.
- Patch-based deployments are unaffected: every default is empty and patches apply after.

**Cons:**
- Two ways to express the same field, values and patches, with a precedence rule to document.
- A second ConfigMap, mount and rollout path to maintain.
- Partially walks back the "patches are the whole surface" position of the previous ADR.

### Option 3: First-class values delivered as environment variables

**Description:** As Option 2, but encode the values as JSON strings in env vars.

**Pros:**
- No ConfigMap, mount or rollout wiring.

**Cons:**
- JSON in env vars for tolerations, affinity and label maps is the ergonomic failure [2026-06-12](2026-06-12-k8s-manifest-overrides-via-env-vars.md) was superseded for. The chart did it seven times, and that was the stated reason for replacing the mechanism.

### Option 4: Ship a library of documented patch recipes

**Description:** Keep patches as the only surface and publish copy-paste recipes for the common cases.

**Pros:**
- No code or values change.

**Cons:**
- Copy-paste configuration drifts from the chart it targets, and the platform still cannot read the result to validate it or derive warm capacity.

---

## Decision

**Chosen Option:** Option 2.

### Rationale

Manifest patches remain the right surface for arbitrary fields, and nothing here changes that. What changes is the default: the handful of customisations that nearly every deployment makes become ordinary values, and the platform gains a model of them.

That model is what makes the rest possible. Once placement is a value, the chart puts warm capacity where sandboxes actually run instead of asking the operator to say it twice. Once workspace access mode is a value, `helm template` rejects the split-pool configuration that otherwise fails on someone's first run. Neither is achievable while the information is an opaque patch body.

### Configuration shape

```yaml
agents:
  # Applied to the workloads orbit-agents provisions in tenant namespaces.
  # Manifest patches still apply after these.
  workloads:
    # Applied to both the session-manager StatefulSet and sandbox Pods.
    common:
      podLabels: {}
      podAnnotations: {}
    sessionManager:
      nodeSelector: {}
      tolerations: []
      affinity: {}
      topologySpreadConstraints: []
      priorityClassName: ""
      podLabels: {}
      podAnnotations: {}
      # Annotations on the agent's ServiceAccount, for cloud identity
      # (IRSA, Workload Identity). Sandbox Pods run under the tenant's
      # default ServiceAccount and are not affected.
      serviceAccount:
        annotations: {}
    sandbox:
      nodeSelector: {}
      tolerations: []
      affinity: {}
      topologySpreadConstraints: []
      priorityClassName: ""
      podLabels: {}
      podAnnotations: {}
  # The agent workspace volume, mounted by the session-manager and by every
  # sandbox of that agent's runs.
  workspace:
    storageClassName: ""
    accessModes: [ReadWriteOnce]
    size: 10Gi
```

Precedence for any field, last wins: the builder's default, then `agents.workloads.common`, then the per-workload block, then manifest patches.

`agents.imageWarmer` and `agents.warmHeadroom` default their scheduling from `agents.workloads.sandbox`, then `agents`, then `global`. Their own keys, added in #311, still win when set.

### Invariants

Values are not a way around the protections patches respect. `orbit-agents` rejects at startup, naming the offending key:

- `podLabels` that collide with an identity label (`spiffe.io/agent`, `spiffe.io/spire-managed-identity`, `orbit.io/identity-proxy`, `orbit.io/tenant`, `orbit.io/agent-type`, `orbit.io/role`, `orbit.io/run-id`, `orbit.io/agent-slug`, `orbit.io/managed-agent`) or with the `app` selector label. These select pods for SPIRE attestation and sidecar injection; a silent overwrite breaks identity.
- `podAnnotations` that collide with the mesh annotations the builders set.

Security context, service account name, volume mounts and the sandbox workspace `subPath` have no values key at all. They remain patch-protected and platform-owned.

### Validation

- **Chart.** `fail` when `agents.workloads.sandbox` and `agents.workloads.sessionManager` select different nodes and `agents.workspace.accessModes` does not include `ReadWriteMany`.
- **`orbit-agents`.** Keep the post-patch check from #311, which stays the backstop for splits expressed as patches and now also covers splits expressed as values.

---

## Consequences

### Positive

- Node pools, pod metadata, workspace storage and cloud identity become values, which is what an evaluator expects of a chart.
- Sandbox placement is stated once and warm capacity follows it.
- The dedicated sandbox pool topology can be documented as supported, with the chart rejecting the combination that fails at runtime.
- Cloud identity for agents stops requiring a patch to a `ServiceAccount`.
- Deployments that never customise see no change: every key defaults to today's behaviour.

### Negative

- Two mechanisms can set the same field. The guide has to be explicit that values are the supported path and patches the escape hatch.
- A second ConfigMap, volume and checksum annotation on the `orbit-agents` pod.
- Changing these values rolls `orbit-agents`, and agent StatefulSets pick the change up on their next reconcile rather than instantly.
- More values to keep documented and tested, including a collision check that did not exist before.

### Neutral

- [2026-09-15](2026-09-15-manifest-patches-configmap.md) is amended, not superseded. Patches remain the mechanism for every field without a first-class key, and keep their protected-field rules.
- Deployments already patching these fields keep working and keep their behaviour, since patches apply last.

---

## Implementation

1. Chart values, the rendered ConfigMap, the mount and the `checksum/` annotation, following `agents/patches-configmap.yaml`.
2. `K8sProvisionerConfig` gains the workload and workspace fields; `cmd/orbit-agents` loads the mounted file as it already does for patches, and fails startup on an identity-label or mesh-annotation collision.
3. `buildSessionManagerStatefulSet`, `buildSandboxPod`, `buildServiceAccount` and the `volumeClaimTemplate` apply them before `manifestpatch.Apply`.
4. Warmers default from `agents.workloads.sandbox`.
5. Chart-side `fail` for the incompatible combination; keep the Go check from #311.
6. Deployment guide: the values, the precedence rule, when to reach for a patch instead, and the existing-agent migration note.

### Migration Path

No action for existing deployments. Every key defaults to current behaviour and patches apply last, so a deployment pinning these fields with patches keeps doing so. Moving to values means deleting those patches, with no change to the rendered result.

An agent created before its workspace class changed keeps its original volume, because `volumeClaimTemplates` are immutable and the claims outlive the StatefulSet. Adopting a split pool on an existing agent means migrating the workspace data or recreating the agent (clear-route/orbit#308).

---

## Validation

### Success Metrics

- A dedicated sandbox pool, an alternative storage class, and pod labels for cost allocation are all configurable without writing a manifest patch.
- Warm capacity lands on the sandbox pool with placement stated once.
- `helm template` rejects split placement over a `ReadWriteOnce` workspace.

---

## Related Decisions

- [manifest-patches-configmap](2026-09-15-manifest-patches-configmap.md) - amended by this decision; remains the mechanism for fields without a first-class key.
- [k8s-manifest-overrides-via-env-vars](2026-06-12-k8s-manifest-overrides-via-env-vars.md) - why structured values are not delivered as environment variables.
- [agent-execution-infrastructure](2026-05-11-agent-execution-infrastructure.md) - the resources being customised.
- [yaml-file-based-configuration](2026-02-18-yaml-file-based-configuration.md) - the config loading pattern the mounted file follows.

---

## Notes

Per-agent sandbox sizing (clear-route/orbit#310) is the natural next question, since "large sandboxes" usually means both bigger limits and a different instance family. If that lands as named profiles on the agent type, a profile could carry placement too, at which point `agents.workloads.sandbox` becomes the default a profile overrides.

Sandbox Pods deliberately get no `ServiceAccountName` and run under the tenant's `default` account, so there is no account of theirs to annotate, and annotating `default` would reach every pod in the namespace.

Extending service account annotations to sandboxes was considered and rejected. The point of such an annotation is to give an SDK inside the pod a cloud role it picks up automatically, and the code inside a sandbox is the agent's. [JIT credential brokering](2026-06-06-jit-credential-brokering-egress-gateway.md) exists so that pod never holds credentials: calls are intercepted, OPA authorizes them, and the gateway injects credentials per request, with AWS SigV4 as the bounded exception where short-lived STS credentials are minted and attributed to the run. A ServiceAccount annotation would hand the agent a role it can assume outside policy, attribution and audit. `spec.serviceAccountName` is already a protected field, so a manifest patch cannot give a sandbox its own account either; a values key would reverse that deliberately.

A sandbox that needs a cloud API should get a Connection, which provides scoped, short-lived, attributed access through the broker.

**Last Updated:** 2026-09-16
