# Ephemeral Dev Environments — Operator's Runbook

This guide covers day-to-day operation of the Ephemeral Dev Environments feature: what to do when an environment gets stuck, how to read its status, and the handful of behaviours that look like bugs but are actually documented decisions. It assumes familiarity with the architecture in `docs/adr/2026-07-07-ephemeral-development-environments-via-vcluster.md` and `code_plan.md`.

---

## Overview

An `OrbitDevEnvironment` custom resource describes one disposable, full-stack Orbit install running as a vcluster in its own host namespace (`orbit-devenv-{id}`). The operator's `DevEnvironmentReconciler` (`backend/internal/operator/devenvironment_controller.go`) drives the CR through namespace creation, vcluster provisioning, in-vcluster platform install, TTL expiry, and idle-based sleep/wake. The `devenvironments` feature package (`backend/internal/devenvironments/`), hosted in `cmd/identity`, is the only externally-reachable surface — it owns the Postgres registry row (owner, quota) and creates/reads/deletes the CR; it never touches the privileged host resources itself.

---

## Recovery: something is stuck

**There is no in-place repair path.** If an environment is stuck — a failed reconcile stage that isn't clearing, a wedged vcluster Helm release, a namespace in a strange state — the supported recovery is **delete and re-provision**, not manual surgery on the CR or its namespace.

```bash
kubectl delete orbitdevenvironment {id}
# then provision a fresh one from the UI or:
#   POST /api/v1/dev-environments  {"ref": "main-<shortsha>"}
```

This is safe because the environment's PKI (the vcluster's client CA, used to sign kubeconfig CSRs) lives on the vcluster's own PVC, not in the CR or in Postgres — deleting the CR triggers the finalizer path that tears down the namespace (and its PVC) along with everything else, so there is no orphaned certificate material left behind to reconcile around. There is deliberately no "resume a half-provisioned environment" code path: partial state is always cheaper to discard and rebuild than to reconcile forward, given how young the reconciler's own state machine is.

If `kubectl delete` itself hangs, check for a stuck finalizer:

```bash
kubectl get orbitdevenvironment {id} -o jsonpath='{.metadata.finalizers}'
```

Do not manually strip the finalizer as a first move — that abandons the host namespace, vcluster PVC, ClusterSPIFFEIDs, and app-ingress routes as orphans. Investigate why the finalizer's cleanup step is failing (usually a namespace stuck in `Terminating` because of a lingering resource with its own finalizer, most often a `PersistentVolumeClaim` or a SPIRE-managed object) before resorting to it.

---

## Chart version bumps require a version bump, not just a values change

The operator's `helm.go` `InstallOrUpgrade` skips a Helm upgrade when the release is already deployed **at the requested chart version**:

```go
// skip only when: state.Deployed && chartVersion != "" && state.ChartVersion == chartVersion
```

This means: if you change `values.yaml` for a version-pinned remote chart (vcluster, Istio) without bumping the pinned version string itself, **the change will not be applied** to already-provisioned environments — the skip predicate only looks at the chart version, not the values content. This is deliberate (re-running `helm upgrade` on every reconcile for every environment would be wasteful and would fight vcluster's own internal reconciliation), but it is a common source of "I changed the values but nothing happened" confusion.

- **Local baked charts** (empty `chartVersion`) are never skipped — every reconcile re-applies them, so values-only changes to Orbit's own charts take effect immediately.
- **Remote pinned charts** (`VCLUSTER_CHART_VERSION`, `ISTIO_CHART_VERSION` in `scripts/cluster/versions.env`) only re-apply when the pin itself changes.

If you need a values-only change to reach already-running environments, bump the pinned version (even to the same upstream version under a synthetic suffix, if upstream hasn't cut a new release) or delete-and-re-provision the affected environments.

**Before rolling out any bump to `VCLUSTER_CHART_VERSION` or `ISTIO_CHART_VERSION`,** run the version-bump regression suite against the cloud staging host:

```bash
export ORBIT_CLOUD_KUBE_CONTEXT=<staging-context>
scripts/cluster/spike-vcluster-up.sh
scripts/cluster/spike-identity-wire.sh
scripts/cluster/spike-istio-up.sh
scripts/cluster/spike-probe.sh   # exits non-zero if any assertion fails
scripts/cluster/spike-teardown.sh
```

This is the same sequence that originally de-risked running Istio sidecar mode inside a vcluster (see `spike-istio-up.sh`'s own comments on `ENABLE_NATIVE_SIDECARS` — a real, permanent finding, not spike scaffolding) — a version bump can silently change that behaviour again.

---

## The quota-exhaustion signature

When a principal is at their `maxActivePerOwner` limit (`devEnvironments:quota`, default 2 — `backend/internal/devenvironments/config/base.yaml`), `CreateEnvironment` refuses with `403 devenvironments_quota_exceeded` **before any Kubernetes object exists** — there is no CR, no namespace, nothing to look at cluster-side. The only visible evidence is the identity-service API response and its audit event.

The more confusing case: **an environment can look "stuck Pending" while actually being quota-exhausted one level down** — inside the vcluster, not on the host. If the in-vcluster orbit-operator's own `ResourceQuota`/`LimitRange` (`devEnv.quota.*`, `devEnv.limitRange.*`) is exhausted, pods queue as `Pending` **inside the vcluster**, which is invisible from the host: `kubectl get pods -n orbit-devenv-{id}` on the host only shows what vcluster has synced down, and a `Pending` pod that never got scheduled inside the vcluster's virtual scheduler may not sync to the host at all. To diagnose this signature:

```bash
# From the host: environment looks like it's stuck in Provisioning
kubectl get orbitdevenvironment {id} -o jsonpath='{.status.conditions}'

# The real story is inside the vcluster:
POST /api/v1/dev-environments/{id}/kubeconfig   # mint a kubeconfig
KUBECONFIG=<minted> kubectl get pods -A          # look for Pending pods in-vcluster
KUBECONFIG=<minted> kubectl describe pod <pod>   # events will show FailedScheduling / quota exceeded
```

If this is the cause, either the environment's chart values genuinely need more headroom (tune `devEnv.quota.*`/`devEnv.limitRange.*`) or something inside the environment is over-requesting — not a reconciler bug.

---

## Reading per-stage conditions

`status.conditions` reports five stage conditions plus one rollup, in the order the reconciler drives them:

| Condition | Meaning when `True` |
|---|---|
| `NamespaceReady` | Host namespace + quota/limit range exist |
| `VClusterReady` | The vcluster Helm release is deployed and its control plane pod is Ready |
| `KubeconfigCSRFulfilled` | The most recently submitted `spec.kubeconfigCSR` has been signed (re-flips to reflect re-signing whenever the CSR changes) |
| `InVClusterPlatformApplied` | The `OrbitPlatform`/`OrbitTenant` CRs have been submitted into the vcluster |
| `InVClusterPlatformReady` | The nested orbit-operator has actually finished its own release sequence — **this, not `InVClusterPlatformApplied`, gates `status.phase == Ready`** |
| `Progressing` | `False` (with `reason`/`message` naming the failing stage) while a reconcile-stage error is in flight but hasn't yet crossed the consecutive-failure threshold that promotes the environment to `phase: Failed`; flips back to `True` on recovery, so it never reads as a permanent stall |

`status.phase` is a coarser summary (`Pending` → `Provisioning` → `Ready` → `Sleeping` / `Failed`) derived from these conditions — there is deliberately no `Expired` phase, since TTL expiry deletes the CR outright rather than transitioning it.

```bash
kubectl get orbitdevenvironment {id} -o json | jq '.status.conditions'
```

When something is stuck in `Provisioning`, the first stage with `status: "False"` is where to look — its `message` carries the underlying reconcile error verbatim.

---

## Kubeconfig lifetime

`POST /api/v1/dev-environments/{id}/kubeconfig` mints a **new client certificate per request** (private key generated in-process, never persisted, never transiting the CR) signed against the vcluster's own client CA. Each minted kubeconfig is valid for **8 hours** from issuance — there is no refresh; request a new one via the same endpoint when the old one expires. The certificate is scoped to `orbit:devenv-admin` (bound to `cluster-admin` inside the vcluster only) — it is rejected outright by the host API server, since the host's CA has no trust relationship with the vcluster's client CA.

---

## Related references

- `docs/adr/2026-07-07-ephemeral-development-environments-via-vcluster.md` — architecture and the pinned decisions this feature preserves.
- `code_plan.md` — the work-package breakdown this implementation was built from; §11 lists the full integration-suite checklist.
- `backend/tests/integration/devenvironments/devenvironment_test.go` — the live staging integration suite (`make test-integration-devenv`).
- `scripts/cluster/spike-*.sh` — the vcluster/Istio version-bump regression suite.
