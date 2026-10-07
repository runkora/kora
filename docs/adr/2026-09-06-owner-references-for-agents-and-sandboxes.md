# Owner References for Agents and Sandboxes

<!-- Filename: 2026-09-06-owner-references-for-agents-and-sandboxes.md -->

**Status:** Proposed
**Date:** 2026-09-06
**Deciders:** Tom Morelly, kora control-plane engineers
**Technical Story:** GitOps observability gap — the Kubernetes objects that `orbit-agents` provisions on behalf of a tenant (agent StatefulSets, sandbox Pods, associated Services/HPAs/PDBs, and Istio policy objects) carry no owner references, so an ArgoCD `Application` that ships an `OrbitTenant` CR cannot show them nested under it in the resource tree, and their lifecycle is not tied to the CR's lifecycle by Kubernetes garbage collection.

---

## Context

kora ships as two versioned Helm charts (`charts/orbit-platform`, `charts/orbit`) plus per-tenant `OrbitTenant` custom resources, and is designed to be operated via GitOps ([Helm Chart Distribution and GitOps Repository Structure](2026-03-31-helm-chart-distribution-and-gitops-repository-structure.md)). Operators typically manage `OrbitTenant` CRs through an ArgoCD `Application` or a Flux `HelmRelease`.

`orbit-agents` (see [orbit-agents Service Architecture](2026-05-11-orbit-agents-service-architecture.md), [Agent Execution Infrastructure](2026-05-11-agent-execution-infrastructure.md), [Sandbox Provisioning and Shell Proxy](2026-05-11-sandbox-provisioning-and-shell-proxy.md)) provisions the Kubernetes objects that back each agent type and each sandbox run:

- Per agent type, in the tenant's namespace: `ServiceAccount`, `StatefulSet`, headless `Service`, `HorizontalPodAutoscaler`, `PodDisruptionBudget`, and Istio `AuthorizationPolicy`; plus an Istio `DestinationRule` in `orbit-system`.
- Per run, in the tenant's namespace: a sandbox `Pod` (`sandbox-{run.ID}`), attached to the agent StatefulSet's workspace `PersistentVolumeClaim` via a per-run `subPath`.

Today `backend/internal/agents/k8s.go` sets no `metadata.ownerReferences` on any of these objects — this is verifiable by a repository-wide grep. Lineage is expressed through labels (`app`, `orbit.io/agent-type`, `orbit.io/tenant`, `orbit.io/run-id`, `orbit.io/agent-slug`) and enforced by the deprovisioning path in `k8s.go`, which enumerates and deletes each object explicitly.

### Background

The [kora Kubernetes Operator](2026-03-31-orbit-kubernetes-operator.md) already defines the `OrbitTenant` custom resource (`orbit.io/v1alpha1`, cluster-scoped, `metadata.name` = tenantID; type in `backend/internal/operator/models.go`). Every tenant that `orbit-agents` provisions for exists as an `OrbitTenant` in the cluster at the time of provisioning — the operator creates the namespace off the back of that CR. This makes it a natural, stable parent for the agent-owned objects that live in the tenant's namespace.

### Problem Statement

Two concrete problems flow from the absence of owner references:

1. **ArgoCD resource tree is flat.** An ArgoCD `Application` that syncs an `OrbitTenant` CR renders the CR as a leaf. Operators cannot see, from the Application UI, which agent StatefulSets, sandbox Pods, and Istio policy objects belong to which tenant. This defeats a primary reason to run kora under GitOps at all: single-pane-of-glass visibility of what a given manifest actually causes to exist in the cluster.
2. **Kubernetes cascade delete is unavailable.** Deleting an `OrbitTenant` does not remove the agent objects orbit-agents created for it. `DeprovisionAgentType` (in `k8s.go`) has to walk each object explicitly; a partial failure leaves orphaned StatefulSets, Services, HPAs, PDBs, and Istio policy objects that only label queries can locate. Sandbox Pods created for a StatefulSet outlive that StatefulSet if it is deleted for any reason.

This is a *how does it connect* question in the kora three-questions framing — it changes representation and lifecycle of objects in the cluster. It does not touch identity or policy.

### Goals

- Every Kubernetes object created by `orbit-agents` for a tenant is reachable, in the ArgoCD resource tree, under the `OrbitTenant` CR that caused it.
- Deleting an `OrbitTenant` cascades to delete every agent- and sandbox-owned object the platform created for it, without new orchestration in `orbit-agents`.
- Deleting an agent's `StatefulSet` cascades to delete the in-flight sandbox Pods that share its workspace PVC.
- Local development without the operator installed continues to work; missing CRD or CR is a warning, not a hard failure.

### Non-Goals

- Owner references on resources the *operator* manages (tenant `Namespace`, aggregated `orbit-tenants`/`orbit-tenant-connections` ConfigMaps, `orbit-tenant-oidc-secrets`, `Connection` and `MCPServer` CRs). Helm co-owns some of these today (`tenant_controller.go`) — a separate ADR.
- Introducing a per-run CRD (`AgentRun`, `Session`). Runs remain PostgreSQL entities as decided in [Run & Session Lifecycle](2026-05-11-run-and-session-lifecycle.md).
- Changing agent identity (SPIFFE) or agent authorization (OPA / Istio `AuthorizationPolicy`) semantics.
- Backfilling owner references onto agent objects that already exist in a running cluster. The `ensureX` reconciliation paths in `k8s.go` will add the reference on next reconcile; no explicit migration is defined here.

---

## Decision Drivers

* **GitOps observability.** ArgoCD's tree view is the primary user of `ownerReferences` for operators running kora under GitOps.
* **Kubernetes-native lifecycle.** GC by owner reference is well-understood, transactional, and does not require code paths in `orbit-agents` to remain correct under partial failure.
* **No new coupling to Helm.** Owning objects off the tenant `Namespace` would tie us to whatever manages the namespace (Helm today, operator later). The tenant CR is stable and orbit-agents already needs the tenant to exist.
* **Local development.** The operator, and therefore the `OrbitTenant` CRD, may be absent in stripped test environments. The system must degrade gracefully.
* **Manifest override compatibility.** `orbit-agents` supports operator-driven manifest overrides ([K8s Manifest Field Overrides via Environment Variables](2026-06-12-k8s-manifest-overrides-via-env-vars.md)); the ordering of owner-reference application must not interfere with that surface.

---

## Considered Options

### Option 1: Own everything off the tenant `Namespace`

**Description:** Set the tenant namespace as `ownerReferences[0]` on every agent-owned object. Same-namespace ownership; simple lookup.

**Pros:**
- Trivially resolves via the k8s API — no CRD dependency.
- Namespace delete already cascades in-namespace objects, so behaviour is close to today.

**Cons:**
- The tenant namespace is co-managed by Helm and the operator today (`tenant_controller.go` has an explicit workaround for Helm ownership). We would be coupling agent-owned objects to whichever managed resource happens to own the Namespace at any moment.
- The `DestinationRule` lives in `orbit-system`, not the tenant namespace, so the model does not cover it.
- Argo will render the tree under the `Namespace` node, not under the `OrbitTenant` CR the operator actually syncs — the GitOps observability goal is only partially met.

### Option 2: Own everything off the cluster-scoped `OrbitTenant` CR *(chosen)*

**Description:** Look up the `OrbitTenant` for the tenant at provision time, set it as the controller owner (`Controller=true, BlockOwnerDeletion=true`) on the agent-type resources (SA, StatefulSet, Service, HPA, PDB, AuthorizationPolicy, DestinationRule). Sandbox Pods inherit the StatefulSet as a non-controller owner.

**Pros:**
- Matches the GitOps mental model exactly — the CR that operators write in Git is what Argo's tree fans out from.
- The `OrbitTenant` is cluster-scoped, so it may legally own namespaced children in any namespace — including the `DestinationRule` in `orbit-system`.
- Stable identity (`metadata.name` = tenantID, immutable UID) so the reference does not shift over the tenant's lifetime.
- Two-tier cascade (`OrbitTenant` → StatefulSet → sandbox Pod) matches the workspace-PVC coupling that already exists in `k8s.go` between agent and sandbox.

**Cons:**
- `orbit-agents` gains a runtime dependency on the operator's CRD being installed. In practice that is already true operationally — the tenant namespace does not exist without the operator — but the *code path* was previously CRD-agnostic.
- A tiny GVR (`orbit.io/v1alpha1/orbittenants`) must live in the agents package to avoid a feature→feature import from `internal/agents` into `internal/operator`.

### Option 3: Introduce an `AgentRun` (or `Session`) CRD as the parent for sandbox Pods

**Description:** Materialise the run as a Kubernetes object with a controller. Sandbox Pods own to the `AgentRun`; `AgentRun` owns to its `OrbitTenant`.

**Pros:**
- Runs become inspectable objects in the cluster; Argo could show a run's Pod under the run.

**Cons:**
- Requires a new CRD, a new controller, and reconciliation between the PostgreSQL run row and the CR — a large surface change contradicting [Run & Session Lifecycle](2026-05-11-run-and-session-lifecycle.md), which chose the DB as source of truth.
- Solves a problem the two-tier `OrbitTenant → StatefulSet → Pod` chain already solves for the GitOps use case.
- Cost far exceeds benefit for the observability goal.

---

## Decision

**Chosen Option:** Option 2 — own agent-type resources off the cluster-scoped `OrbitTenant` CR; own sandbox Pods off the agent's `StatefulSet`.

Topology:

```
OrbitTenant  (orbit.io/v1alpha1, cluster-scoped)
├── ServiceAccount                 (tenant namespace)
├── StatefulSet                    (tenant namespace)
│   └── Sandbox Pod                (tenant namespace, ownerRef → StatefulSet)
├── Service (headless)             (tenant namespace)
├── HorizontalPodAutoscaler        (tenant namespace)
├── PodDisruptionBudget            (tenant namespace)
├── AuthorizationPolicy (Istio)    (tenant namespace)
└── DestinationRule (Istio)        (orbit-system namespace, cluster-scoped owner)
```

For each agent-type resource, set:

```go
metav1.OwnerReference{
    APIVersion:         "orbit.io/v1alpha1",
    Kind:               "OrbitTenant",
    Name:               tenant.Name,
    UID:                tenant.UID,
    Controller:         ptr.To(true),
    BlockOwnerDeletion: ptr.To(true),
}
```

For each sandbox `Pod`, set the parent `StatefulSet` as a non-controller owner (`Controller=false`, `BlockOwnerDeletion=false`) — the StatefulSet controller is not us, and the reference exists purely to gate garbage collection.

### Rationale

The `OrbitTenant` CR is the stable, canonical parent for everything `orbit-agents` provisions on behalf of a tenant, and it is the object an operator holds in their GitOps repo. It is cluster-scoped, so it can own children in any namespace, which covers the cross-namespace `DestinationRule`. The `StatefulSet` is the correct parent for sandbox Pods because it already owns the workspace PVC they mount — decoupling sandbox lifecycle from StatefulSet would only produce orphan Pods.

---

## Consequences

### Positive

- ArgoCD `Application`s that ship an `OrbitTenant` CR fan out to every agent StatefulSet, sandbox Pod, and Istio policy object the platform created for that tenant.
- `kubectl delete orbittenant <name>` garbage-collects the entire tenant's agent footprint. `DeprovisionAgentType` becomes best-effort rather than the sole delete path.
- Deleting a StatefulSet garbage-collects its in-flight sandbox Pods; workspace PVC coupling is no longer implicit.
- The manual label-based cleanup risk (orphans on partial deprovision failure) is eliminated.

### Negative

- `orbit-agents` now requires `get` on `orbittenants.orbit.io` — a ClusterRole line — and requires the `OrbitTenant` CRD to be installed to reach full behaviour. When it is not (test clusters), we degrade to today's no-owner behaviour with a warning.
- A tiny amount of duplicated GVR / GroupVersion metadata lives in the agents package instead of in a shared platform package. Deliberate — `internal/agents` must not import `internal/operator`.

### Neutral

- The manifest-override mechanism ([2026-06-12](2026-06-12-k8s-manifest-overrides-via-env-vars.md)) applies its patches to the object as constructed by `orbit-agents`, and owner references are part of that constructed object. An operator with sufficient motivation can strip them via override; the escape hatch is documented, and this ADR does not change it.
- Existing in-cluster objects created before this change will gain owner references on their next reconcile (`ensureX` merges into existing objects); no dedicated migration.

---

## Implementation

Code changes land in `backend/internal/agents/`:

- Add a package-local GVR for `orbit.io/v1alpha1/orbittenants` and a helper on `K8sProvisioner` that fetches the `OrbitTenant` via the existing `dynamicClient` and returns a fully-populated `metav1.OwnerReference`. Cache in-process for the lifetime of a `ProvisionAgentType` / `ProvisionSandbox` call only; do not persist. On not-found, forbidden, or CRD-missing errors, log at warn and return the zero-value reference — callers apply owner refs only when non-empty.
- Thread the owner reference through `ProvisionAgentType` into each `ensureX` call (`ensureServiceAccount`, `ensureStatefulSet`, `ensureHeadlessService`, `ensureHPA`, `ensurePDB`, `ensureAuthorizationPolicy`, `ensureDestinationRule`). Each `ensureX` sets `metadata.ownerReferences` on the constructed object *before* manifest overrides are applied, so the override system remains the last word. On `AlreadyExists`, each `ensureX` also fetches the existing object and merges the owner reference in via a shared `mergeOwnerRefs` helper — the update path uses `SetOwnerReferences` for unstructured Istio objects and a direct `ObjectMeta` update for typed ones.
- In `ProvisionSandbox`, fetch the parent `StatefulSet` (already implicitly needed for PVC/ordinal accounting) and set its owner reference on the sandbox Pod.
- Add unit tests alongside the existing `k8s_authorizationpolicy_test.go` / `k8s_overrides_test.go` / `k8s_sandbox_replace_test.go`, using the same fake-client pattern, asserting the owner reference on every created object and asserting graceful degradation when the tenant CR / CRD is missing.

Helm change lands in `charts/orbit`:

- Extend the `orbit-agents` ClusterRole with `get` on `orbittenants.orbit.io`. No other verbs.

### Migration Path

No explicit migration. Each `ensureX` follows a create-then-backfill pattern: on `AlreadyExists`, it fetches the existing object, merges the owner reference (matching on `APIVersion/Kind/Name` so unrelated refs are preserved), and issues an Update. On the first reconcile after upgrade, every agent-type object in every tenant gains its `OrbitTenant` owner reference. Sandbox Pods created before the upgrade run to completion without an owner reference and are cleaned up by the existing DB-driven path; new Pods carry the reference.

### Timeline

- ADR ratified: 2026-09-06.
- Implementation PR: single change against `backend/internal/agents/` and `charts/orbit/`, gated by `make check`.

---

## Validation

### Success Metrics

- `kubectl get statefulset,service,hpa,pdb,authorizationpolicy -n orbit-tenant-example -o jsonpath='{.items[*].metadata.ownerReferences[*].kind}'` reports `OrbitTenant` for every object.
- `kubectl get destinationrule -n orbit-system -l app=<slug> -o jsonpath='{.items[*].metadata.ownerReferences[*].kind}'` reports `OrbitTenant`.
- `kubectl get pod -l orbit.io/role=sandbox -n orbit-tenant-example -o jsonpath='{.items[*].metadata.ownerReferences[*].kind}'` reports `StatefulSet`.
- `kubectl delete orbittenant example` garbage-collects the tenant's StatefulSet, HPA, PDB, headless Service, ServiceAccount, AuthorizationPolicy, and the `orbit-system`-namespaced DestinationRule; sandbox Pods disappear with the StatefulSet.
- In an ArgoCD environment, the `Application` that ships the `OrbitTenant` CR renders the tenant's agent objects as descendants of the CR in the resource tree.

### Monitoring

- The provisioner emits a structured log at warn severity when the `OrbitTenant` cannot be fetched. Alert on any sustained rate of this in production; it means the operator's CRD is not installed or RBAC is misconfigured.

---

## Related Decisions

- [Helm Chart Distribution and GitOps Repository Structure](2026-03-31-helm-chart-distribution-and-gitops-repository-structure.md) — the GitOps model this ADR serves.
- [kora Kubernetes Operator](2026-03-31-orbit-kubernetes-operator.md) — defines the `OrbitTenant` CR used as owner.
- [K8s Manifest Field Overrides via Environment Variables](2026-06-12-k8s-manifest-overrides-via-env-vars.md) — override ordering interaction.
- [Sandbox Provisioning and Shell Proxy](2026-05-11-sandbox-provisioning-and-shell-proxy.md) — sandbox Pod lifecycle.
- [Agent Execution Infrastructure](2026-05-11-agent-execution-infrastructure.md) — agent-type StatefulSet lifecycle.
- [Run & Session Lifecycle](2026-05-11-run-and-session-lifecycle.md) — why runs stay in Postgres and not a CRD.

---

## References

- Kubernetes [Owners and Dependents](https://kubernetes.io/docs/concepts/overview/working-with-objects/owners-dependents/) — cluster-scoped owners of namespaced children.
- Kubernetes [Garbage Collection](https://kubernetes.io/docs/concepts/architecture/garbage-collection/).
- ArgoCD [Resource Tracking](https://argo-cd.readthedocs.io/en/stable/user-guide/resource_tracking/) — how the resource tree derives from `ownerReferences`.

---

## Notes

**Last Updated:** 2026-09-06
