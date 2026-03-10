# Implementation Review — ADR-0016 Helm Migration
**Reviewer:** Forge  
**Date:** 2025-01-27  
**Scope:** Post-Helm migration audit of the ADR-0016 local development environment implementation  
**Verdict:** Implementation is functionally correct. Three categories of work remain: ADR update, dead-code removal, and a handful of minor script/values issues.

---

## 1. Executive Summary

The Helm migration is mechanically sound. Istio installs cleanly via `istio/base` + `istio/istiod`, and SPIRE installs cleanly via `spiffe/spire-crds` + `spiffe/spire`. The two-tier dev environment (Docker Compose + k3d) works as designed. The ordering guarantees (CRDs before umbrella chart, postgres before SPIRE, db-up as Makefile prerequisite) are all correct.

The main outstanding issues are:

1. **ADR-0016 is significantly stale** — it still documents `istioctl`, kustomize-based SPIRE config, and the old overlay patch structure. It must be updated to reflect the Helm-based implementation.
2. **Orphaned files on disk** — three kustomize patch files and the entire `infrastructure/base/spire/` directory are dead code; nothing applies them.
3. **Stale comment in `versions.env`** — references `infrastructure/base/spire/*.yaml` which is no longer in use.
4. **SPIRE `helm upgrade --install` is missing `--wait`/`--timeout`** — makes the subsequent `kubectl wait` calls the only failure gate for a slow deploy.
5. **CoreDNS injection pattern is undocumented** — this is the most non-obvious part of the setup and the ADR says the wrong thing about it.

---

## 2. ADR-0016 Drift

The ADR was written before the Helm migration and has not been updated. The following sections contain statements that are now incorrect.

### 2.1 §Implementation Notes — Required Local Tools

**Current text (line 213–214):**
```
- istioctl
- helm (for Istio and other chart installations — SPIRE is deployed via kustomize manifests, not a helm chart)
```

**What is actually true:**
- `istioctl` is **not** a required tool. `install-tools-cluster.sh` no longer installs it.
- `helm` is the install mechanism for **both** Istio and SPIRE.
- The parenthetical `— SPIRE is deployed via kustomize manifests, not a helm chart` is the inverse of reality.

**Required fix:** Remove `istioctl` from the tool list. Update the `helm` description to state it manages Istio (`istio/base`, `istio/istiod`) and SPIRE (`spiffe/spire-crds`, `spiffe/spire`).

### 2.2 §Decision — Tier 2 description

**Current text (line 138–139):**
```
- **Istio** — installed via `istioctl` with sidecar injection enabled for the `orbit-system` namespace
```

**Required fix:** Replace with:
```
- **Istio** — installed via `helm upgrade --install` (`istio/base` + `istio/istiod`) with sidecar injection enabled for the `orbit-system` namespace; no `istioctl` dependency
```

### 2.3 §SPIRE Configuration — Base vs Local Overlay (lines 229–302)

This entire section documents the kustomize layering strategy (`infrastructure/base/spire/` → `infrastructure/overlays/local/patches/`) with inline HCL config snippets. That config path no longer exists at runtime. The actual config lives in:

- `infrastructure/helm/spire/values.yaml` — production-safe base defaults
- `infrastructure/helm/spire/values.local.yaml` — local dev overrides

**Required fix:** Replace the kustomize config section with a Helm values section. Key values to document:

| Concern | Base (`values.yaml`) | Local (`values.local.yaml`) |
|---|---|---|
| Trust domain | `orbit.local` | (same) |
| Datastore | PostgreSQL, `sslmode=require`, dedicated `spire` DB | `host=orbit-postgres`, `sslmode=disable`, shared `orbit` DB |
| Node attestor | `k8s_psat` | `skipKubeletVerification: true` |
| Key manager (server) | `disk` | (same) |
| Key manager (agent) | `memory` | (same) |
| Controller manager | Enabled, sidecar on server pod | (same) |
| `cgr.dev/chainguard/bash` | `pullPolicy: IfNotPresent` | `pullPolicy: Never` (cgr.dev unreachable inside k3d nodes) |
| SPIFFE CSI driver | Disabled | (same) |
| OIDC discovery provider | Disabled | (same) |

Also worth documenting: the SPIRE Controller Manager is included in the umbrella chart. This is an architectural addition not mentioned anywhere in ADR-0016. The controller manager watches `ClusterSPIFFEID` CRDs and automatically creates SPIRE registration entries — no `kubectl exec spire-server spire-server entry create` needed.

### 2.4 §Implementation Notes — CoreDNS DNS injection (missing entirely)

The ADR states (line 134–135):
```
k3d nodes joined to the same Docker bridge network as the Tier 1 postgres container (orbit-network)
so cluster pods can reach orbit-postgres by name without additional networking hacks
```

**This is incorrect.** k3d nodes do join `orbit-network` and can reach `orbit-postgres` at the IP level, but **pods inside k3s use CoreDNS for name resolution, not Docker's embedded DNS**. Docker container hostnames like `orbit-postgres` are not visible to CoreDNS. Without step 9 of `dev-cluster-up.sh`, the SPIRE Server pod would crash-loop attempting to connect to `orbit-postgres` because CoreDNS returns NXDOMAIN.

The fix in `dev-cluster-up.sh` (step 9) is to create a headless `Service` + manual `Endpoints` object in the `spire` and `orbit-system` namespaces, pointing at the Docker IP of `orbit-postgres` on `orbit-network`. This is how `orbit-postgres` resolves inside the cluster.

**Required ADR addition:** A subsection explaining this CoreDNS injection pattern. Engineers who need to add future docker-compose services that cluster pods must reach will need to replicate this pattern.

---

## 3. Orphaned / Dead Code

### 3.1 `infrastructure/overlays/local/patches/` — three files

The following files are on disk but are **not applied anywhere**:

- `infrastructure/overlays/local/patches/spire-server-config.yaml`
- `infrastructure/overlays/local/patches/spire-agent-configmap.yaml`
- `infrastructure/overlays/local/patches/spire-controller-manager-config.yaml`

`infrastructure/overlays/local/kustomization.yaml` no longer lists `../../base/spire` as a resource and no longer references these patches. The comment at the top of that file correctly explains why (`SPIRE is now managed via Helm`). The patches themselves just weren't deleted.

**Action:** Delete the `infrastructure/overlays/local/patches/` directory entirely.

### 3.2 `infrastructure/base/spire/` — kustomize manifests

The kustomize SPIRE manifests in `infrastructure/base/spire/` (server.yaml, agent.yaml, server-config.yaml, etc.) are no longer applied by any kustomization. The Helm chart owns all SPIRE resources now.

**Action:** Delete `infrastructure/base/spire/` once the team is confident the Helm-based install is stable. Consider doing this in the same PR as the ADR update to keep the "before" and "after" visible in git history.

### 3.3 Stale comment in `versions.env` (line 7–8)

**Current comment:**
```bash
# When bumping a version, change it here only. The image tags below must stay
# in sync with the tags used in infrastructure/base/spire/*.yaml — if you
# change a SPIRE image version, update the kustomize manifests to match.
```

`infrastructure/base/spire/*.yaml` is dead code. The SPIRE image versions are informational only — the Helm chart pins them internally via its `appVersion`.

**Required fix:** Replace with something like:
```bash
# When bumping a version, change it here only.
# SPIRE_SERVER/AGENT/CONTROLLER_MANAGER_VERSION are informational — the Helm chart
# (spiffe/spire SPIRE_CHART_VERSION) pins these internally. They are used only
# for pre-pulling images into the local Docker cache before cluster creation.
```

---

## 4. Implementation Correctness

Things that were reviewed and are correct.

### 4.1 Helm install ordering ✓

The ordering in `dev-cluster-up.sh` is correct:
1. `spiffe/spire-crds` installed first with `--wait` + `kubectl wait --for=condition=Established` before the umbrella chart
2. `spiffe/spire` umbrella chart installed after CRDs are confirmed Established
3. `ClusterSPIFFEID` resources applied (`kubectl apply -k`) after SPIRE Controller Manager is Ready

This matches the requirements of the `spiffe/spire` chart.

### 4.2 Istio Helm install ✓

`istio/base` installs CRDs; `istio/istiod` installs the control plane. Both use `--wait`. The explicit `kubectl rollout status deployment/istiod` guard after the helm installs is a sensible belt-and-suspenders check given Istio's webhook initialization timing.

### 4.3 Image pre-pull strategy ✓

Pre-pulling all images to the local Docker cache before cluster creation, then using `k3d image import` (fast local tarball copy) is the correct pattern. The `cgr.dev/chainguard/bash:latest` handling (pull on host, import into k3d, `pullPolicy: Never` in values.local.yaml) correctly works around cgr.dev being unreachable from inside k3d nodes.

### 4.4 `db-up` as Makefile prerequisite ✓

`make dev-cluster` declares `db-up` as a prerequisite. `dev-cluster-up.sh` also has an inline postgres health check (step 8) that guards against direct script invocation. Both layers of protection are correct and the redundancy is appropriate.

### 4.5 Namespace pre-creation strategy ✓

Step 9 creates `spire` and `orbit-system` namespaces using `--dry-run=client -o yaml | kubectl apply --server-side` before the Helm install. This avoids a race where the Helm chart creates the namespace concurrently with the CoreDNS service/endpoints injection.

### 4.6 NetworkPolicy is namespace-scoped ✓

`infrastructure/base/network-policy/default-deny.yaml` applies to `orbit-system` only. The SPIRE components are in the `spire` namespace, which is intentionally outside this default-deny scope. The `allow-spire-server-grpc` policy correctly permits egress from `orbit-system` to port 8081 in the `spire` namespace.

### 4.7 ClusterSPIFFEID resources ✓

ClusterSPIFFEIDs are still managed via kustomize in `infrastructure/services/overlays/local` — this is correct per the migration plan. The Controller Manager watches these CRDs and creates SPIRE entries automatically.

### 4.8 ISTIO_VERSION / ISTIO_CHART_VERSION naming ✓

`ISTIO_VERSION="1.21.2"` is used for Docker image tags in `PREFETCH_IMAGES` (`docker.io/istio/pilot:${ISTIO_VERSION}`). `ISTIO_CHART_VERSION="1.21.2"` is used for `helm upgrade --install --version`. For Istio, the chart version equals the app version, so both are `1.21.2` — correct.

---

## 5. Issues and Suggestions

### 5.1 SPIRE `helm upgrade --install` is missing `--wait --timeout` ⚠️

**File:** `scripts/cluster/dev-cluster-up.sh`, lines 279–283

**Current:**
```bash
helm upgrade --install spire spiffe/spire \
  --namespace spire \
  --version "${SPIRE_CHART_VERSION}" \
  -f "${HELM_DIR}/spire/values.yaml" \
  -f "${HELM_DIR}/spire/values.local.yaml"
```

The Istio installs both use `--wait --timeout 180s`. The SPIRE install does not. Instead, readiness is verified via `kubectl wait --for=condition=Ready pod/spire-server-0` and `kubectl rollout status daemonset/spire-agent` immediately after.

This is **functionally acceptable** but the `helm upgrade --install` call itself can exit successfully while SPIRE pods are still initialising (Helm marks the release as deployed once the API server accepts the resources, not when pods are Ready). If the `kubectl wait` call is removed or the timeout is too short, it is possible for subsequent steps to proceed against an unready SPIRE.

**Suggestion:** Either add `--wait --timeout 300s` to the helm install for consistency with the Istio installs, or add a comment explicitly explaining why `--wait` is omitted and the `kubectl wait` calls are the readiness gate.

### 5.2 `Endpoints` API is soft-deprecated ℹ️

**File:** `scripts/cluster/dev-cluster-up.sh`, step 9

The inline manifest in the CoreDNS injection step uses:
```yaml
apiVersion: v1
kind: Endpoints
```

Kubernetes 1.21+ introduced `EndpointSlice` as the preferred API and the `Endpoints` object for services is created by the controller as a compatibility shim. For k8s 1.29 (the pinned version), `Endpoints` still works and there is no functional issue, but on future Kubernetes versions this may emit deprecation warnings.

**Suggestion:** No immediate action required. Note this if the cluster version is ever bumped past 1.29 and warnings appear.

### 5.3 `values.yaml` sentinel password is the right pattern — add a CI guard

**File:** `infrastructure/helm/spire/values.yaml`

The `spire-server.dataStore.sql.password: "REPLACE-VIA-OVERLAY"` sentinel is a good fail-fast guard: if this value somehow reaches a non-local install, the connection string will fail immediately with an authentication error rather than silently connecting with a weak password.

**Suggestion:** Add a CI lint step (e.g., `grep -r "REPLACE-VIA-OVERLAY" infrastructure/helm/spire/values.yaml && exit 1`) that fails if the sentinel value is detected in any non-base values file. This prevents the sentinel from accidentally being copy-pasted into a staging overlay.

### 5.4 `traceSampling: 1.0` in Istio base values should be production-safe

**File:** `infrastructure/helm/istio/values.yaml`

```yaml
pilot:
  traceSampling: 1.0  # 100% sampling
```

If `values.yaml` is used as a base for production (or staging), 100% trace sampling will flood a tracing backend. This is fine for local dev but is a footgun if a staging overlay extends this file and forgets to override `traceSampling`.

**Suggestion:** Lower `traceSampling` in `values.yaml` to a production-appropriate value (e.g. `1.0` as a percentage → `0.1` for 0.1%) and **raise** it to `100.0` in `values.local.yaml`. Currently it is the opposite.

### 5.5 `make setup` does not include a `dev-up` prerequisite

**File:** `Makefile`, line 70

```makefile
setup: install-tools install-tools-cluster tidy hooks
```

`make setup` installs all tooling but does not start Tier 1 services. A new engineer following the ADR will run `make setup` and then immediately try `make dev-cluster` — which requires postgres to be running. The Makefile and ADR both note this ordering requirement, but requiring engineers to remember `make db-up` after `make setup` is a friction point.

**Suggestion:** Either add a note to the `setup` target echoing the next step, or add a `make setup` → `make dev-up` example to the ADR quickstart. (Not a bug — a DX suggestion.)

### 5.6 SPIRE Controller Manager version hard-coded in `values.yaml`

**File:** `infrastructure/helm/spire/values.yaml`

```yaml
spire-controller-manager:
  image:
    tag: "0.6.3"
```

The Controller Manager version `0.6.3` is explicit in `values.yaml` but is absent from `versions.env`. `versions.env` has `SPIRE_CONTROLLER_MANAGER_VERSION="0.6.3"` (used for image pre-pull in dev-cluster-up.sh), and that matches — but the two sources can drift independently. If someone bumps `SPIRE_CONTROLLER_MANAGER_VERSION` in `versions.env` they also need to update `values.yaml` (and vice versa).

**Suggestion:** Add a comment in `values.yaml` next to the controller manager image tag cross-referencing `SPIRE_CONTROLLER_MANAGER_VERSION` in `versions.env`. Or use a Helm `--set` flag in the install command to derive the tag from the env var, eliminating the duplication.

---

## 6. Summary Table

| # | Severity | File | Issue | Action |
|---|---|---|---|---|
| 1 | 🔴 Required | `docs/adr/0016-*` | `istioctl` listed as required tool; SPIRE described as kustomize-managed | Update ADR |
| 2 | 🔴 Required | `docs/adr/0016-*` | SPIRE config section documents kustomize HCL patches (no longer used) | Replace with Helm values table |
| 3 | 🔴 Required | `docs/adr/0016-*` | CoreDNS injection pattern undocumented; ADR incorrectly implies Docker DNS works inside pods | Add CoreDNS section |
| 4 | 🔴 Required | `docs/adr/0016-*` | SPIRE Controller Manager not mentioned at all | Add to Tier 2 component list |
| 5 | 🟡 Cleanup | `infrastructure/overlays/local/patches/` | 3 SPIRE kustomize patches orphaned (not applied anywhere) | Delete directory |
| 6 | 🟡 Cleanup | `infrastructure/base/spire/` | Entire kustomize SPIRE base orphaned | Delete directory (once Helm install is confirmed stable) |
| 7 | 🟡 Cleanup | `scripts/cluster/versions.env` | Comment references `infrastructure/base/spire/*.yaml` (dead code) | Update comment |
| 8 | 🟡 Suggestion | `scripts/cluster/dev-cluster-up.sh` | SPIRE helm install missing `--wait`/`--timeout` (unlike Istio installs) | Add `--wait --timeout 300s` or document intent |
| 9 | 🟢 Minor | `scripts/cluster/dev-cluster-up.sh` | `kind: Endpoints` is soft-deprecated in favour of `EndpointSlice` | No action now; revisit on k8s version bump |
| 10 | 🟢 Minor | `infrastructure/helm/istio/values.yaml` | `traceSampling: 1.0` (100%) in base values — wrong default for non-local environments | Move 100% to `values.local.yaml`; set conservative default in base |
| 11 | 🟢 Minor | `infrastructure/helm/spire/values.yaml` | Controller Manager image tag duplicates `SPIRE_CONTROLLER_MANAGER_VERSION` in versions.env | Add cross-reference comment |
| 12 | 🟢 Minor | `infrastructure/helm/spire/values.yaml` | Sentinel password `REPLACE-VIA-OVERLAY` has no CI guard | Add grep-based CI lint check |

---

## 7. Recommended Next Steps

**Priority 1 — ADR update (docs/adr/0016-*):**  
Update Required Local Tools, Tier 2 deployment description, SPIRE config section, and add CoreDNS injection and Controller Manager documentation. This is the highest-value item because the ADR is the primary reference engineers consult when onboarding.

**Priority 2 — Dead code removal:**  
Delete `infrastructure/overlays/local/patches/` and `infrastructure/base/spire/` in a single PR with a clear commit message referencing the Helm migration. Update the stale `versions.env` comment in the same PR.

**Priority 3 — Script hardening:**  
Add `--wait --timeout 300s` to the SPIRE `helm upgrade --install` call. Fix `traceSampling` default in `values.yaml`/`values.local.yaml`.
