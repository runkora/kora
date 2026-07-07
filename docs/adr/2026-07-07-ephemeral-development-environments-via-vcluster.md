# Ephemeral Development Environments via vcluster

**Status:** Proposed
**Date:** 2026-07-07
**Deciders:** Engineering Team
**Technical Story:** Developers building the Orbit platform need on-demand, isolated, full-stack environments. Today the options are the local k3d cluster ([local-development-environment](2026-03-10-local-development-environment.md)) — per-machine, resource-heavy, and divergent from real deployments — or a shared staging cluster, which is contended and cannot be broken safely. This ADR defines button-press ephemeral environments, running as vclusters on the same infrastructure that runs Orbit itself, provisioned from the Orbit UI. It **replaces Tier 2 (`make dev-cluster`, k3d) as the full-stack development path**; Tier 1 (`make dev-up`) remains the fast local inner loop.

---

## Context

Orbit is a multi-service control plane (13 binaries) whose interesting behaviour lives in the seams between services and infrastructure: the operator's Helm release sequencing, SPIRE attestation, Istio sidecar and `ext_proc` wiring, OPA bundle distribution, and tenant namespace reconciliation. A meaningful share of platform work — operator changes, chart changes, CRD schema changes, sidecar injection changes — cannot be validated in Tier 1 (`make dev-up`) at all, and validating it in Tier 2 (`make dev-cluster`) means rebuilding a k3d cluster on a laptop that is also running an IDE, Docker, and an agent or two.

The shared staging cluster is the other escape valve, and it has the usual problems: one engineer's broken operator build blocks everyone; testing a destructive migration means coordinating a freeze; two branches cannot deploy concurrently.

What developers actually need is a **disposable, isolated Orbit installation** — including the cluster-scoped machinery (CRDs, operator, mesh) — that exists for the lifetime of a branch and costs nothing when idle.

This ADR is not an addition to the two-tier local strategy — it **replaces Tier 2**. The k3d cluster earns its keep today only because nothing better exists: it duplicates deployment configuration that must be kept in lockstep with the real charts (a maintenance burden [local-development-environment](2026-03-10-local-development-environment.md) itself lists as a negative consequence), it needs workarounds a real cluster does not (CoreDNS endpoint injection for Docker Compose services, `k3d image import` for unreachable registries), it consumes a large share of a laptop's resources, and it still diverges from real deployments. Once a higher-fidelity environment can be provisioned in minutes from the UI, maintaining a second, lower-fidelity full-stack mechanism is pure cost. Tier 1 (`make dev-up` — Postgres, Redis, Dex via Docker Compose, services under `air`) is untouched: the fast, offline Go inner loop is a different job and stays local.

There is a second, deliberate motivation: **dogfooding**. Orbit's product thesis is that provisioning governed workloads should be a button press backed by identity, policy, and audit. Provisioning Orbit's own development environments through the Orbit UI exercises exactly that thesis — the button is scope-gated by the policy gateway, the action is attributed to a human principal, and the lifecycle is reconciled by the same operator that runs production installs.

### Background

Relevant existing machinery:

- The **operator** ([orbit-kubernetes-operator](2026-03-31-orbit-kubernetes-operator.md)) already reconciles cluster-scoped CRDs (`OrbitPlatform`, `OrbitTenant`) into namespaces, Helm releases, and config projection. It owns the full release sequence `spire-crds → spire → istio-base → istiod → gateway → orbit-platform → orbit`.
- The **UI → BFF → backend** provisioning pattern ([agents-bff-integration-and-run-chat-wiring](2026-06-04-agents-bff-integration-and-run-chat-wiring.md)) is established: the browser never calls backend services directly; BFF routes validate the session cookie and forward the platform JWT.
- `orbit-agents` is today the **only service that talks to the Kubernetes API**, with deliberately broad RBAC across `orbit-tenant-*` namespaces ([agent-execution-infrastructure](2026-05-11-agent-execution-infrastructure.md)).
- All actions are scope-gated by the policy gateway ([policy-gateway-and-authorisation-model](2026-04-14-policy-gateway-and-authorisation-model.md), [platform-scope-enforcement-middleware](2026-06-04-platform-scope-enforcement-middleware.md)).

Naming note: `orbit-agents` already has an `EnvironmentConfig` concept (eager sandbox provisioning for agent sessions — `backend/internal/agents/environments.go`). That is a different thing. This ADR consistently uses **dev environment** / `OrbitDevEnvironment` to avoid collision.

### Problem Statement

We need to define:

1. The isolation model for ephemeral environments — how one developer's full Orbit installation (including CRDs, operator, and mesh) coexists with others on shared infrastructure
2. Where the environments run
3. How an environment is provisioned from a button in the Orbit UI — which service exposes the API, and what reconciles the environment into existence
4. The lifecycle model: TTL, sleep/wake, teardown, and cost controls
5. How the developer accesses a provisioned environment (kubeconfig, ingress) without handling long-lived credentials
6. How environment workloads obtain SPIFFE identities, given that SPIRE's agent performs node-level attestation and a vcluster has no nodes of its own

### Goals

- **Button-press provisioning**: an authenticated developer clicks *New dev environment* in the Orbit UI and gets a ready environment in minutes, with progress visible in the UI
- **Full-stack fidelity**: each environment is a real Orbit installation deployed through the same operator and charts as production installs — not a mock or a subset
- **Cluster-admin inside, least-privilege outside**: developers can install CRDs, replace the operator, and break the mesh *inside their environment* without any grant on the host cluster
- **Same infrastructure as Orbit itself**: environments run on the cluster that runs the Orbit control plane — no separate dev-cluster fleet to build, patch, and pay for
- **Replace the k3d full-stack tier**: retire `make dev-cluster` (Tier 2) and its supporting machinery once parity is demonstrated — one full-stack environment mechanism to maintain, not two
- **Governed like everything else**: provisioning is scope-gated, attributed to a human identity, and audited; environments have TTLs and quotas by default

### Non-Goals

- Replacing Tier 1 (`make dev-up`) for daily Go development — the fast, offline inner loop stays local. (Replacing Tier 2, by contrast, is an explicit goal)
- Preserving an offline *full-stack* development path — after Tier 2 retirement, full-stack work requires connectivity to the host cluster, deliberately
- Ephemeral environments for Orbit *customers* or their agents (agent sandboxes are covered by [sandbox-provisioning-and-shell-proxy](2026-05-11-sandbox-provisioning-and-shell-proxy.md))
- Preview environments wired to PRs / CI automation — a natural follow-up, but the trigger in scope here is the UI button
- Production-hardening validation (Cilium eBPF enforcement, real cloud IAM) — that remains a CI/staging concern, consistent with [local-development-environment](2026-03-10-local-development-environment.md)

---

## Decision Drivers

* **Cluster-scoped resources are the crux**: Orbit development routinely mutates CRDs, the operator, `ClusterSPIFFEID`s, and mesh config — resources that cannot be namespaced away on a shared cluster
* **Provisioning speed**: an environment that takes 20 minutes to appear will not be used; the target is minutes, not tens of minutes
* **Cost**: environments must share host-cluster nodes, scale to zero when idle, and die on a TTL — a per-developer cloud cluster fleet is not acceptable
* **Consistency with the operator model**: Orbit already expresses "desired platform state" as CRDs reconciled by the operator; environment provisioning should follow the same declarative pattern, not introduce a parallel imperative path
* **Two full-stack mechanisms is one too many**: Tier 2 duplicates chart/values maintenance (`values.local.yaml` overlays, CoreDNS injection scripts, image-import workarounds) that must track every platform change; whatever replaces it must carry the full Tier 2 validation load so that duplication can actually be deleted
* **Dogfooding**: the provisioning flow should exercise Orbit's own identity, policy, and audit machinery
* **Security of the host cluster**: developer cluster-admin must be confined; the blast radius of a broken environment must be one namespace on the host

---

## Considered Options

### A. Isolation model

#### Option A1: Namespace-per-environment on the shared cluster

**Description:** Each environment is a set of namespaces on the host cluster; Orbit services deploy into them directly.

**Pros:**
- Cheapest and simplest — no extra control-plane pods
- Instant "provisioning" (namespace creation is immediate)

**Cons:**
- **Fails the core requirement**: CRDs, the operator, `ClusterSPIFFEID`s, admission webhooks, and Istio config are cluster-scoped — two environments testing different CRD schema versions collide immediately
- Developer workflows need broad host-cluster RBAC, which leaks across environments
- One environment's misbehaving operator reconciles everyone's resources

#### Option A2: Real cluster per environment (cloud cluster or k3d-on-VM fleet)

**Description:** Provision a dedicated Kubernetes cluster (e.g. EKS, or k3s on a pooled VM) per environment.

**Pros:**
- Maximum isolation and fidelity, including node-level components (Cilium, kernel eBPF)
- Developer is genuinely cluster-admin

**Cons:**
- Slow: cloud cluster creation is 15–20+ minutes; a pre-warmed pool mitigates this at significant standing cost
- Expensive: per-cluster control-plane fees and poor bin-packing of mostly-idle environments
- A second infrastructure estate to build, patch, monitor, and pay for — explicitly *not* "the same infra as Orbit itself"

#### Option A3: vcluster per environment *(chosen)*

**Description:** Each environment is a [vcluster](https://www.vcluster.com/) — a certified virtual Kubernetes cluster whose control plane (k3s API server + controller-manager + datastore) runs as a pod inside a single namespace of the host cluster, with a syncer that maps virtual pods onto host nodes.

**Pros:**
- **Cluster-scoped isolation without a cluster**: each vcluster has its own API server, so CRDs, operators, webhooks, and RBAC are private to the environment — the exact gap A1 cannot close
- Fast: a vcluster is running in well under a minute; the Orbit install on top dominates time-to-ready
- Cheap: one namespace, a small control-plane pod, shared host nodes; an idle environment can be slept — workloads scaled to zero with state preserved (implemented in our reconciler; see lifecycle and the licensing con below)
- Developer gets `cluster-admin` *inside* the vcluster with zero grants on the host API server
- Host-side blast radius is one namespace, cappable with `ResourceQuota`/`LimitRange`
- Runs on the existing host cluster — satisfies "same infra as Orbit itself" literally

**Cons:**
- Not full fidelity at the node layer: workloads share host nodes and the host kernel — Cilium/eBPF behaviour is out of scope (already the accepted posture for local dev per [local-development-environment](2026-03-10-local-development-environment.md))
- Node-level infrastructure cannot run *inside* a vcluster: anything that attests or observes workloads via the host kubelet — the SPIRE agent DaemonSet in particular — must be provided by the host instead (see option group C)
- Running Istio inside a vcluster is workable but has sharp edges (injection webhook interplay with the syncer) — see Consequences
- vcluster's native sleep mode and auto-wakeup are **Enterprise-licensed** (and auto-wake on ingress additionally requires the vCluster Platform agent) — the OSS core adopted here does not include them. Sleep is therefore implemented in our own reconciler (see lifecycle, decision 8), and wake is an explicit UI/API action rather than transparent on connection
- A new third-party dependency with its own upgrade cadence

### B. Provisioning trigger and control flow

#### Option B1: GitOps pull request per environment

**Description:** The button opens a PR against the deployment repo adding environment manifests; Flux/ArgoCD syncs it.

**Pros:**
- Every environment is in Git; audit for free
- No new API surface

**Cons:**
- Minutes of PR + sync latency before provisioning even starts; teardown equally indirect
- "Press a button, wait for a robot to merge a PR" is a poor product experience, and this flow *is* product surface (dogfooding)
- Ephemeral, hours-lived state churns the deployment repo with noise

#### Option B2: Imperative provisioning from a backend service

**Description:** A backend handler runs `helm install vcluster ...` and the Orbit install steps directly, the way `orbit-agents` imperatively creates StatefulSets today.

**Pros:**
- Direct; no CRD or operator changes
- Precedent exists in `orbit-agents`

**Cons:**
- No reconciliation: a half-failed install needs bespoke retry/repair logic that the operator already has
- Long-running multi-release install sequence held inside an HTTP request lifecycle
- Duplicates the operator's Helm SDK machinery in a second service
- Grants a request-serving API service broad host-cluster RBAC (namespaces, Helm releases, cluster-scoped reads)

#### Option B3: `OrbitDevEnvironment` CRD reconciled by the operator *(chosen)*

**Description:** The button results in a small API creating an `OrbitDevEnvironment` custom resource on the host cluster. The existing operator grows a third reconciler that turns the CR into a namespace + vcluster + Orbit installation, and reflects progress into `status`, which the UI polls.

**Pros:**
- Identical shape to `OrbitPlatform`/`OrbitTenant`: declarative spec, level-triggered reconcile, self-healing on partial failure, status conditions for the UI
- The operator already owns Helm release sequencing and namespace lifecycle — this reuses it rather than duplicating it
- TTL enforcement is a natural reconcile concern (requeue until `expiresAt`, then delete)
- The API service's host-cluster RBAC shrinks to CRUD on one CRD; everything privileged stays in the operator

**Cons:**
- Operator scope grows; a dev-tooling concern now ships in the production operator binary (mitigated: the reconciler is inert unless `OrbitDevEnvironment` CRs exist, and the CRD is only installed on clusters that opt in)
- Status-over-CR is a slightly slower feedback channel than a synchronous API (acceptable: provisioning takes minutes anyway)

### C. Workload identity for environment workloads

SPIRE is the forcing constraint on how "complete" an in-vcluster Orbit install can be, so the identity topology is decided here rather than left to implementation.

#### Option C1: SPIRE runs inside each vcluster

**Description:** The in-vcluster `OrbitPlatform` installs SPIRE exactly as a real deployment does — per-environment SPIRE server, agent DaemonSet, and trust domain.

**Pros:**
- Maximum conceptual fidelity: every environment is a byte-identical Orbit install, including its identity plane
- Per-environment trust domains give the cleanest isolation story

**Cons:**
- **Does not work.** The SPIRE agent performs node-level workload attestation against the host kubelet, and a vcluster has no nodes — its pods are synced onto host nodes with translated names/namespaces (e.g. `identity-x-orbit-system-x-{env-id}`). An in-vcluster agent's kubelet view can never match the registrations created by the in-vcluster SPIRE controller manager, which sees virtual names
- The Workload API socket is a host node path (hostPath/CSI); an in-vcluster SPIRE cannot own one without colliding with the host agent and every other environment
- Node attestation (`k8s_psat`) needs `TokenReview` against the API server that actually runs the agent pods — the host, not the virtual one

#### Option C2: Nested SPIRE — per-environment server chained to the host

**Description:** Each environment runs its own SPIRE *server* (a plain pod — this part works), chained to the host Orbit SPIRE via the `UpstreamAuthority` plugin, giving per-environment trust domains rooted in the host CA.

**Pros:**
- Per-environment trust domains and a per-environment server, so SPIRE-server-adjacent control-plane logic could be exercised in-environment
- Standard, supported SPIRE topology (nested/downstream servers)

**Cons:**
- Solves the wrong half: the *agents* still cannot attest through the syncer's name translation (C1's core failure), so the host operator would have to run a dedicated agent DaemonSet per environment on host nodes, outside the vcluster, with per-environment socket paths — N agent DaemonSets per node
- Substantial reconciler complexity for a fidelity gain no current development task needs

#### Option C3: Host-provided identity — the main Orbit installation's SPIRE attests environment workloads *(chosen)*

**Description:** Environments run no SPIRE at all (`spire.enabled=false` on the in-vcluster `OrbitPlatform`). Because environment pods physically run on host nodes, the **host** SPIRE agents attest them natively, like any other host pod — no proxy layer is required. The reconciler creates a host-side `ClusterSPIFFEID` for the environment namespace minting path-scoped IDs (`spiffe://{trust-domain}/devenv/{env-id}/…`), the host SPIFFE CSI driver mounts the host Workload API socket into environment pods (the CSI volume spec syncs through to the host kubelet), and the reconciler projects the host trust bundle into the vcluster so in-environment services can validate SVIDs.

**Pros:**
- Works with SPIRE's actual attestation model instead of against it
- Environment services receive **real SVIDs** — identity-consumption flows (`POST /identity/exchange`, `identity-proxy` injection, SVID-based mTLS) are exercised with genuine attestation, which Tier 1 local dev cannot offer at all
- One SPIRE deployment to operate; no per-environment agent or socket management

**Cons:**
- Isolation is by **path prefix within the shared host trust domain**, not per-environment trust domains — host authorization policy must never match `devenv/…` prefixed IDs (host policies match exact `system/…` and `tenant/…` paths today, and environment namespaces are NetworkPolicy-fenced from the host control plane regardless)
- Changes to SPIRE deployment itself (charts, agent config, attestor plugins) cannot be validated in a dev environment — that work stays on Tier 2 / staging
- Prerequisite: the SPIFFE CSI driver, currently disabled in our SPIRE chart values ([local-development-environment](2026-03-10-local-development-environment.md)), must be enabled on host installs that support dev environments
- The exact `ClusterSPIFFEID` selector strategy (namespace-scoped vs. syncer-propagated labels) depends on how faithfully the syncer propagates pod labels — pinned down in the Phase 1 spike
- **The `devenv/…` path prefix is not parseable by current platform code.** `principalFromSpiffeID` (`backend/internal/identity/principal_extractor.go`) recognises only `system/…` and `tenant/…` paths, and chart-shipped authorization policies match exact paths — so the in-environment identity service, XFCC handling, and mesh authz would reject host-minted `devenv/{env-id}/…` SVIDs as written. Prefix handling is resolved in the Phase 1 spike; the preferred direction is teaching platform code to strip an optional `devenv/{env-id}` prefix (one code change, shipped everywhere) rather than parameterising paths through per-environment chart values, which would recreate the Tier 2 values-divergence this ADR exists to delete

---

## Decision

**Chosen Options:** A3 (vcluster per environment) + B3 (`OrbitDevEnvironment` CRD reconciled by the operator) + C3 (host-provided workload identity).

### Architecture

```
Developer clicks "New dev environment" in the Orbit UI
  → BFF route (frontend/src/app/api/dev-environments/**)
      session cookie → platform JWT → typed client
  → identity API: POST /v1/dev-environments        (scope: devenvironments:write)
      validates quota (max active envs per principal), generates env ID
  → creates OrbitDevEnvironment CR on the host cluster
  → operator devenvironment reconciler:
      1. host namespace  orbit-devenv-{env-id}   (ResourceQuota, LimitRange,
         NetworkPolicy, TTL + owner labels)
      2. vcluster Helm release into that namespace
      3. host-side identity wiring: ClusterSPIFFEID for the env namespace
         (spiffe://{trust-domain}/devenv/{env-id}/…) + host trust bundle
         projected into the vcluster
      4. inside the vcluster: orbit-operator chart → OrbitPlatform CR
         (spire.enabled=false — identity comes from the host, see C3) →
         release sequence, plus Postgres, Redis, Dex, and a seeded
         OrbitTenant (mirrors the deploy/local reference install)
      5. Ingress  {env-id}.dev.{domain}  → the env's gateway
      6. surfaces status.phase: Pending → Provisioning → Ready
         (also Sleeping | Expired | Failed) and status.ingressHost,
         status.expiresAt
  → UI polls GET /v1/dev-environments/{id} and renders progress
```

### Key decisions within the design

1. **Environments run on the host cluster that runs Orbit's control plane**, in namespaces named `orbit-devenv-{env-id}`, labelled `orbit.io/role: devenv` and `orbit.io/owner: {principal}`. No separate fleet. Capacity is managed with a dedicated node pool (taint/toleration) where the host cluster has one, so dev-environment load cannot starve the control plane; on single-pool clusters, `ResourceQuota` per environment namespace is the backstop. The ≤5-minute time-to-ready target additionally **requires environment images pre-pulled onto the dev-environment nodes** (DaemonSet pre-puller or node-image baking) — a cold pull of the full stack (Istio, Postgres, Redis, Dex, the Orbit binaries) does not fit the budget.

2. **Each environment is a self-contained Orbit install — identity excepted.** The reconciler installs the `orbit-operator` chart *inside* the vcluster and applies an `OrbitPlatform` CR plus a seeded dev `OrbitTenant`, so the environment exercises the real install path end-to-end — the operator being reconciled inside the vcluster is the very artifact under development. Postgres, Redis, and Dex run inside the vcluster with fixture data; **environments never share the host control plane's database or Redis**. Isolation beats convenience here: a dev environment testing a migration must not be able to touch real control-plane state.

3. **Workload identity is provided by the host Orbit installation's SPIRE** (option C3). SPIRE cannot run inside a vcluster — its agent attests via the host kubelet, where synced pods appear under translated names. Instead, the in-vcluster install runs with `spire.enabled=false`; host SPIRE agents attest environment pods natively (they are host pods), minting path-scoped IDs under `spiffe://{trust-domain}/devenv/{env-id}/…` via a reconciler-managed `ClusterSPIFFEID`, with the Workload API socket delivered by the host SPIFFE CSI driver and the trust bundle projected into the vcluster. Environment services therefore consume **real SVIDs** from real attestation. One compatibility gap gates this: current platform code and chart authz policies parse exact `system/…`/`tenant/…` paths and would reject the `devenv/…` prefix — prefix handling (preferred: platform code strips an optional `devenv/{env-id}` segment) is a Phase 1 spike exit criterion, not an implementation detail.

4. **The environment installs a chosen ref.** `spec.ref` selects the chart/image version to install (default: latest `main` build). Developers iterate by pushing a branch image (built locally or by CI) and either bumping `spec.ref` or using their kubeconfig to `helm upgrade` inside the vcluster directly — inside the vcluster they are cluster-admin, and nothing they do there is visible to the host.

5. **The API surface is a new feature package `internal/devenvironments` in the main control-plane API (`cmd/identity`)**, following the standard feature structure (repository/service/handlers). It owns the environment registry rows (owner, quota accounting, audit trail) and creates/deletes `OrbitDevEnvironment` CRs. This deliberately relaxes, narrowly, the "only `orbit-agents` talks to the Kubernetes API" posture: the identity service gains a k8s client whose RBAC is **CRUD on `OrbitDevEnvironment` CRs only** — it cannot create namespaces, pods, or Helm releases. `orbit-agents` was considered as the host (it has the k8s client precedent) but rejected: dev environments have nothing to do with agent execution, and parking them there couples two unrelated lifecycles to one service's already-broad RBAC.

6. **Access is brokered, not handed out — and the operator does the signing.** The UI's *Connect* action calls `POST /v1/dev-environments/{id}/kubeconfig`, which returns a **short-lived kubeconfig** (client certificate against the vcluster's CA, default 8h) scoped to that vcluster. The identity service never touches the vcluster CA key (which would breach its single-CRD RBAC posture, decision 5): it generates the client keypair, places the CSR on the `OrbitDevEnvironment` CR, and the reconciler — which already holds namespace-scoped privileges — signs it against the vcluster CA and publishes the certificate and CA bundle in `status`. Only public material transits the CR; the private key never leaves the identity service, which assembles and returns the kubeconfig. Developers never receive host-cluster credentials of any kind. The environment's Orbit UI/API is reachable at `https://{env-id}.dev.{domain}`, authenticated against the environment's own Dex — consistent with the agent-credential principle that raw long-lived credentials are never exposed ([jit-credential-brokering-egress-gateway](2026-06-06-jit-credential-brokering-egress-gateway.md)).

7. **These environments replace Tier 2, on a parity gate — not on day one.** `make dev-cluster` remains available (and is the test bed for developing the dev-environment reconciler itself — this feature cannot be developed inside itself until it exists) through initial rollout. Tier 2 is deprecated only when the Phase 1 fidelity matrix plus real usage demonstrate that dev environments cover the Tier 2 workload: the `POST /identity/exchange` flow against real SVIDs, operator and chart iteration, sidecar and `ext_proc` wiring. Work dev environments deliberately do not cover is re-homed explicitly rather than silently orphaned: SPIRE deployment work and Cilium validation go to CI/staging (where Cilium already lives).

8. **Lifecycle defaults**: TTL 24h, extendable from the UI up to a hard cap (7 days); **sleep** after 2h idle; per-principal cap of 2 active environments (policy-configurable). Sleep is implemented **by the devenvironment reconciler, not by vcluster** — vcluster's native sleep mode is Enterprise-licensed and its ingress-triggered auto-wake requires the vCluster Platform agent, neither of which we adopt. The reconciler's crude equivalent: scale the vcluster control-plane StatefulSet and its synced workloads to zero, retaining PVCs (the vcluster datastore and all state survive). Idleness is derived from platform-visible signals (last kubeconfig mint, last ingress/UI activity). Wake is **explicit** — a UI action or a `POST /v1/dev-environments/{id}/kubeconfig` call flips the CR's desired state and the reconciler scales back up; there is no transparent wake-on-connection, and a `kubectl` against a sleeping environment fails until woken. On expiry the reconciler deletes the namespace — everything, including the vcluster's datastore, goes with it. Deletion is the recovery path for a wedged environment: delete and re-provision, never repair in place.

### The three questions

- **Who is this agent?** The provisioning action is performed by an authenticated human principal; the CR carries the owner identity, every state transition is audited against it, and quota/TTL policy binds to it. Workloads *inside* the environment carry real, host-SPIRE-issued SVIDs path-scoped under `spiffe://{trust-domain}/devenv/{env-id}/…` — every environment workload is attested and attributable, and no host or tenant authorization policy matches the `devenv/…` prefix.
- **What can it access?** The button is gated by `devenvironments:*` scopes through the policy gateway. On the host, the environment can access exactly one namespace under quota, fenced by NetworkPolicy from the host control plane; the developer's cluster-admin exists only inside the virtual API server. The identity service's new k8s RBAC is confined to one CRD.
- **How does it connect?** Access credentials (kubeconfig, session) are short-lived and brokered through the platform; developers never hold host-cluster or long-lived credentials.

---

## Consequences

### Positive

- Platform work that today requires the shared staging cluster — operator changes, CRD migrations, chart refactors, mesh wiring — gets an isolated, disposable target per branch
- Staging stops being a contention point and a single point of breakage for the team
- Environments deploy through the production install path (operator + charts), so the install path itself is exercised dozens of times a week — install regressions surface in dev environments before any customer sees them
- The feature dogfoods Orbit's identity/policy/audit stack on a real internal workflow
- Idle cost approaches zero via reconciler-managed sleep + TTL; blast radius is one quota-capped host namespace
- The local strategy collapses from two tiers to one: the entire Tier 2 maintenance surface (`values.local.yaml` overlays, CoreDNS endpoint injection, `k3d image import` workarounds, laptop resource pressure) is deleted after retirement, and full-stack fidelity *improves* — dev environments run real SVID attestation, which Tier 2's k3d SPIRE approximated

### Negative

- **SPIRE deployment work is not exercisable in a dev environment.** With identity host-provided (C3), changes to SPIRE charts, agent configuration, or attestor plugins require a real (non-virtual) cluster — Tier 2 while it still exists, CI/staging after its retirement. The flip side is genuinely positive — identity *consumption* flows run against real attestation — but the SPIRE deployment layer itself is a fidelity hole by design.
- **Full-stack development stops working offline.** Once Tier 2 is retired, a developer without connectivity to the host cluster has Tier 1 only. This is accepted deliberately: the team does not have an offline-development requirement, and keeping Tier 2 alive for a hypothetical one forfeits the consolidation benefit.
- **The host cluster becomes critical developer infrastructure.** After Tier 2 retirement, a host-cluster outage blocks all full-stack validation for the whole team — it needs an availability owner and the same operational care as CI.
- **Istio fidelity inside a vcluster needs spike validation.** Injection webhooks interact with the syncer in ways that must be proven before implementation; if full in-vcluster Istio proves fragile, the fallback is the documented Tier-2 posture (NetworkPolicy substitute, degraded-mode flags) — acceptable for most platform work, but changes to the mesh layer itself would still need staging. Cilium/eBPF remains out of scope entirely, as it already is locally.
- **Host installs that support dev environments must enable the SPIFFE CSI driver** (currently disabled in the SPIRE chart values) so the host Workload API socket can be mounted into environment pods.
- **Sleep/wake is ours to build and operate.** vcluster's native sleep mode is Enterprise-licensed, so the reconciler carries the idle-detection and scale-down/up logic itself, and wake is an explicit UI/API action — a developer running `kubectl` against a sleeping environment gets a connection error until they wake it. More operator code, and a rougher edge than transparent wake-on-connection
- The operator binary takes on a third reconciler and a dev-tooling concern; its test surface grows
- vcluster becomes a load-bearing third-party dependency (version-pinned in the operator's chart references, upgraded deliberately like the Istio/SPIRE pins)
- The host cluster must be capacity-planned for N concurrent environments; a burst of provisioning can pressure shared nodes if no dedicated node pool exists
- The identity service gains a (single-CRD) Kubernetes client — a small but real widening of its dependency surface

### Neutral

- A new scope family (`devenvironments:read|write|admin`) enters the policy vocabulary and dashboard
- `deploy/local/` gains a sibling role: the fixture set it defines becomes the seed content for every dev environment, so it must be kept installable at all times
- The UI grows a *Dev Environments* page (list, provision, connect, extend, destroy) following existing frontend patterns (Terra components, BFF policy projection)

---

## Implementation

Phased, each phase shippable:

1. **Spike (gate for the rest):** vcluster on the existing k3d/host setup running the Orbit chart sequence inside with `spire.enabled=false`, host-provided identity wired per C3, and Istio enabled in-vcluster. Must answer: does the host SPIFFE CSI volume sync through the syncer; how faithfully are pod labels propagated (fixes the `ClusterSPIFFEID` selector strategy); does Istio injection survive the syncer; **do `devenv/{env-id}/…`-prefixed SVIDs flow end-to-end through principal extraction, XFCC handling, OPA rules, and chart-shipped authorization policies** (fixes the prefix-handling approach — preferred: platform code strips the optional prefix); **does mesh mTLS work end-to-end with devenv-prefixed identities** (injection surviving the syncer is necessary but not sufficient). Output: a written fidelity matrix (what works, what needs degraded mode) appended to this ADR before Phase 2 starts.
2. **CRD + reconciler:** `OrbitDevEnvironment` types, operator reconciler (namespace → vcluster release → in-vcluster install → status), TTL handling, reconciler-implemented sleep/wake (scale-to-zero on idle, explicit wake), CSR fulfilment for kubeconfig requests (decision 6). Provisioning driven by `kubectl apply` only.
3. **API + registry:** `internal/devenvironments` feature package in `cmd/identity`, scope registration, quota enforcement, kubeconfig endpoint (keypair + CSR on the CR; signing done by the operator per decision 6).
4. **UI:** BFF routes + *Dev Environments* page with provision/status/connect/extend/destroy.
5. **Follow-ups (out of scope, tracked separately):** PR-triggered preview environments; CI-built branch images wired to `spec.ref`; environment templates (slim vs full presets).

### Local Deployment and Test Plan

The feature is developed and verified locally before it ever touches the shared host cluster. The local k3d cluster plays the role of the **host** cluster — vcluster is indifferent to what runs underneath it — which is why decision 7 keeps Tier 2 alive through initial rollout. Testing proceeds in two local rings; staging validates only what a laptop cannot (see *Out of local scope* below).

#### Prerequisites (one-time local setup deltas)

- Bring up Tier 2 as the host: `make dev-cluster && make cluster-operator && make cluster-deploy`.
- **Enable the SPIFFE CSI driver** in the SPIRE values applied by the platform reconciler (`backend/internal/operator/platform_controller.go`) — it is currently disabled, and C3 depends on mounting the host Workload API socket into environment pods. This is the same change host installs need, made first on k3d.
- Resource budget: a full Orbit install inside a vcluster inside k3d is heavy. Plan on **one environment at a time** locally, and a slimmed in-vcluster `OrbitPlatform` spec if laptop pressure demands it.

#### Ring 1 — no cluster (`make test`, runs in CI today)

Two enabling refactors make the reconciler unit-testable and are prerequisites for Phase 2, not cleanup:

1. **Extract a Helm interface.** `HelmClient` (`backend/internal/operator/helm.go`) is a concrete struct constructed inline inside `Reconcile`; the devenvironment reconciler takes it as an injected interface so tests can fake `InstallOrUpgrade`/`Status`/`Uninstall`.
2. **Inject a clock.** TTL expiry, the 2h idle threshold, and sleep/wake decisions are all time-driven; tests advance a fake clock instead of waiting.

With those in place, unit coverage (fake controller-runtime client with the `orbit.io/v1alpha1` scheme — precedent: the client-go fake in `backend/internal/agents/k8s_sandbox_replace_test.go`):

- Reconcile-loop: namespace creation with quota/limit/NetworkPolicy and owner/TTL labels; status phase transitions (`Pending → Provisioning → Ready → Sleeping/Expired/Failed`); **idempotent re-reconcile after each partial-failure point** (the self-healing claim of B3, as a test); TTL requeue math; sleep scale-down / wake scale-up decisions.
- CSR fulfilment (decision 6): pure `crypto/x509` table tests in the style of `tenant_ca_test.go` — valid CSR signed with correct expiry and scoping, malformed CSR rejected, and an explicit assertion that **no private-key material ever appears on the CR**.
- Prefix handling (C3 gate): table-driven tests on `principalFromSpiffeID` — `system/…`/`tenant/…` unchanged, `devenv/{env-id}/…` strips correctly, and a `devenv/…` ID can never parse into a bare `system/…` principal. Matching OPA rule tests assert host policies do not match `devenv`-prefixed identities.
- `internal/devenvironments` (Phase 3): service/handler tests with hand-rolled mocks + `httptest` per the `internal/identity` pattern (quota cap, env-ID generation, kubeconfig assembly, error mapping); the feature's k8s client tested against the fake, including an assertion it touches **only** `OrbitDevEnvironment` resources (decision 5's RBAC posture, encoded).

#### Ring 2 — local k3d as the host cluster

**Phase 1 (spike):** performed directly on the k3d host — `helm install` a vcluster into a hand-made `orbit-devenv-spike` namespace, run the Orbit chart sequence inside with `spire.enabled=false`, wire C3 by hand. Each spike question is scripted as a rerunnable assertion under `scripts/cluster/` (style of `test-egress-lockdown.sh`) rather than checked by hand once: CSI socket sync, pod-label propagation, Istio injection through the syncer, `devenv/…` SVID flow through principal extraction/XFCC/OPA/chart authz, end-to-end mesh mTLS. These scripts outlive the spike as the regression suite for future vcluster version bumps.

**Phase 2 (CRD + reconciler):** the iteration loop is `make redeploy-operator` (charts are baked into the operator image) followed by `kubectl apply` of an example `OrbitDevEnvironment` CR checked in under `deploy/local/`. Lifecycle paths run in test time via short `spec` overrides (TTL of minutes, idle threshold of seconds). The local acceptance checklist, verified with `kubectl` against the k3d host:

1. CR apply → namespace `orbit-devenv-{env-id}` with quota/limits/NetworkPolicy and labels → vcluster release → in-vcluster install → `status.phase: Ready`, `status.ingressHost` populated.
2. A kubeconfig minted via the CR's CSR flow reaches the vcluster (`kubectl get pods` succeeds) and **cannot reach the host API server**.
3. From inside the environment: workloads hold `devenv/{env-id}/…` SVIDs; a probe at the host control plane is refused (NetworkPolicy fence); host services reject the environment's SVIDs.
4. Idle past the (shortened) threshold → control plane and synced workloads scale to zero, PVCs retained; explicit wake restores `Ready` with state intact.
5. TTL expiry → namespace deleted, nothing orphaned on the host (`kubectl get all,pvc,clusterspiffeids -A | grep {env-id}` is empty).
6. Kill the operator mid-provision, restart it → reconcile converges without manual repair.

**Phase 3 (API + registry):** `make redeploy SERVICE=identity`, authenticate with `make auth-device-k3d`, drive `POST /v1/dev-environments` (and kubeconfig/extend/destroy) with `curl` per the standard token pattern. Repository tests run against the Tier 1 Postgres, env-gated on `ORBIT_TEST_DB_URL` per the `backend/internal/triggers/postgres_test.go` pattern. The end-to-end path lands as `backend/tests/integration/devenvironments/` under the existing `//go:build integration` tag, run by `make test-integration` against the k3d host — automating checklist items 1–6 plus scope gating (`devenvironments:write` refused without the scope) and quota (third concurrent create refused).

**Phase 4 (UI):** the local frontend against the k3d cluster exercises the full button flow (provision → progress polling → connect → extend → destroy); BFF routes get MSW integration tests per existing frontend patterns.

#### Out of local scope (validated on the staging rollout)

Provisioning-time targets (the ≤5-minute p90 depends on node pools and image pre-pull that k3d does not model), capacity behaviour under concurrent environments, real ingress/DNS/TLS for `{env-id}.dev.{domain}`, and multi-principal quota behaviour in anger. The `devenvironments` integration suite should additionally gain a scheduled CI job on a throwaway k3d cluster before the parity gate is declared passed — once Tier 2 is retired, this feature is the only full-stack path, and an untested-in-CI single path is not acceptable (today no integration suite runs in CI at all).

### Migration Path

This ADR supersedes **Tier 2** of [local-development-environment](2026-03-10-local-development-environment.md). Tier 1 (`make dev-up` + `air`) is unchanged and remains the fast inner loop for Go development. The retirement is phased behind a parity gate:

1. **Coexistence (Phases 1–4):** `make dev-cluster` keeps working, and is the test bed for developing the dev-environment reconciler itself — the feature cannot bootstrap inside itself.
2. **Parity gate:** Tier 2 is formally deprecated (Makefile warning, docs updated) only when the Phase 1 fidelity matrix plus at least one quarter of real usage show dev environments covering the Tier 2 workload — identity exchange against real SVIDs, operator/chart/CRD iteration, sidecar and `ext_proc` wiring. Anything not covered is explicitly re-homed (SPIRE deployment work and Cilium → CI/staging), not silently orphaned.
3. **Retirement:** `make dev-cluster`, `dev-cluster-up.sh`, the `values.local.yaml` overlays, and the CoreDNS/image-import workarounds are removed. `infrastructure/bootstrap/` (k3d config) is retained for CI and for standing up a *host* cluster, not for daily development. [local-development-environment](2026-03-10-local-development-environment.md) is amended at that point to mark its Tier 2 sections as superseded by this decision; `make test-integration` retargets a dev environment.
4. After retirement, developers without connectivity fall back to Tier 1 only — there is deliberately no offline full-stack path.

---

## Phase 1 Spike — Preparatory Findings (spike not yet executed)

Before writing any spike scripts, the assumptions behind Option A3/C3 and the Phase 1 plan were checked against the current state of `main` and current vcluster documentation. Several materially change the spike's risk profile and required configuration. **No cluster changes have been made yet** — the k3d host (`k3d-orbit-local`, 1 server + 2 agents, `k3s v1.29.4+k3s1`, confirmed up and reachable) is untouched by this work; `vcluster` CLI is not installed locally (`helm`, `k3d`, `docker` are). This section exists so the next session can resume directly at script-writing instead of re-deriving these facts.

### Corrections to this ADR's existing claims

- **The SPIFFE CSI driver is already enabled, not disabled.** This ADR states in several places (Option C3 cons, Consequences/Negative, and the local-setup Prerequisites) that "the SPIFFE CSI driver... is currently disabled in our SPIRE chart values" and lists enabling it as a Phase 1 prerequisite. That is stale relative to `main`: `backend/internal/operator/platform_controller.go:373-376` sets `"spiffe-csi-driver": map[string]any{"enabled": true}` and `git log -S"spiffe-csi-driver"` shows this has been `true` since it was introduced (commit `d999ca6`, #18) and never changed. `charts/orbit/templates/{frontend,identity}/deployment.yaml` already mount the CSI volume into in-mesh workloads, and `reconcileGateway` (`platform_controller.go:447-498`) does the same for the gateway. **No flag needs flipping on the host side before the spike.** Verify empirically with `kubectl get daemonset -n spire-system` (expect a `spiffe-csi-driver` DaemonSet) and `kubectl get csidriver csi.spiffe.io` after `make cluster-operator`, rather than assuming a change is needed.

### Current vcluster reality (checked against vcluster.com/docs and loft-sh/vcluster, current stable v0.35.1, July 2026 — this ADR's References section and Option A3 description predate these specifics)

- **The k3s distro is gone.** `controlPlane.distro.k3s` was deprecated in v0.25 and removed entirely in v0.33. Current stable only supports the k8s distro (real kube-apiserver + controller-manager, embedded etcd or sqlite datastore). The spike — and any future implementation — runs the k8s distro inside vcluster even though the *host* is k3d/k3s; this is heavier than the ADR's "fast: a vcluster is running in well under a minute" pro implicitly assumed, though likely still true in absolute terms. Doc: [K3s distro (deprecated)](https://www.vcluster.com/docs/vcluster/0.31.0/configure/vcluster-yaml/control-plane/components/distro/k3s).
- **Sidecar-mode Istio inside a vcluster is off vcluster's own supported path.** vcluster's official [Istio Integration](https://www.vcluster.com/docs/vcluster/integrations/istio) supports **only Istio in Ambient mode running on the host**, syncing `DestinationRule`/`Gateway`/`VirtualService` objects out — it explicitly does not cover running istiod and a sidecar-injection webhook *inside* a vcluster, which is exactly what Option C3's architecture requires (`spire.enabled=false` inside the vcluster still leaves `istio.enabled=true` inside the vcluster, per decision 3). This should still work mechanically — pod admission happens against the vcluster's own virtual API server before the syncer mirrors the (already-mutated) pod to the host, so an in-vcluster `MutatingWebhookConfiguration` should fire first — and third-party posts describe doing this pre-dating vcluster's official Ambient-only integration. But it is now the **single highest-uncertainty item in the whole spike**, more so than this ADR's Option A3 cons ("has sharp edges") anticipated. If it fails, the ADR's own documented fallback already covers it (Consequences/Negative: "the fallback is the documented Tier-2 posture... Cilium/eBPF remains out of scope entirely").
- **`sync.toHost.serviceAccounts` and `sync.toHost.networkPolicies` default to `false`.** Per [Sync to host](https://www.vcluster.com/docs/vcluster/configure/vcluster-yaml/sync/to-host/), OSS vcluster syncs Pods/Secrets/ConfigMaps/Services/Endpoints/PVCs virtual→host by default, but ServiceAccounts and NetworkPolicies are opt-in. Both **must** be explicitly set `true` in `vcluster.yaml` for this ADR's C3 identity model and NetworkPolicy host-fencing claim to work at all:
  - SPIRE's `k8s_psat` node attestor (`spire-agent.workloadAttestors.k8s`, configured in `platform_controller.go` per the SPIRE values block) does a `TokenReview` against the **host** API server using the pod's ServiceAccount token. If the ServiceAccount referenced by a synced pod doesn't exist as a real host object, attestation likely breaks.
  - This ADR's isolation claim ("fenced by NetworkPolicy from the host control plane", decisions 8/"three questions") depends on host-side NetworkPolicy objects actually existing, which requires this same opt-in.
  - This is a required line in `vcluster.yaml`, not a nice-to-have — flag prominently in the spike script and carry it into any Phase 2 reconciler-generated values.
- **CSI ephemeral inline volumes (as opposed to PVC-based CSI) are underspecified in vcluster's docs.** `sync.fromHost.csiDrivers` defaults to `auto` (enables when the virtual scheduler is on) and syncs `CSIDriver` objects host→virtual read-only — documented in the context of PVC-based storage-aware scheduling. The SPIFFE CSI driver (`driver: csi.spiffe.io`) is used as an **ephemeral inline volume** on pod specs, not a PVC. Since pod specs are created against the virtual API server and the resulting pod object is what gets synced/rewritten onto the host node, the inline CSI volume block should pass through as opaque pod-spec data and be honored by the **host** kubelet/CSI driver regardless of whether the `CSIDriver` object itself is synced into the vcluster (that object mainly matters for scheduling/validation). This is architecturally plausible but **not a documented guarantee** — it's exactly what `spike-probe.sh` assertion 3 (below) needs to prove empirically, not assume.

### Local host state as of this check

```
kubectl config current-context      → k3d-orbit-local
kubectl get nodes                   → k3d-orbit-local-server-0 (control-plane), k3d-orbit-local-agent-{0,1}
                                       all Ready, v1.29.4+k3s1
which vcluster                      → not installed
which helm / k3d / docker           → present (helm and k3d are Go-installed at ~/go/bin)
```
No `make dev-cluster`/`cluster-operator`/`cluster-deploy` state was disturbed. `vcluster` will be driven via its Helm chart (`helm install ... vcluster --repo https://charts.loft.sh`), not the `vcluster` CLI, so the missing CLI is not a blocker.

### Resumption plan — concrete deliverables for the next session

Scope is Phase 1 only: rerunnable scripts under `scripts/cluster/` (style of the existing `test-egress-lockdown.sh`) plus one small Go change the ADR already bundles into Phase 1 (`principalFromSpiffeID` prefix handling). No `OrbitDevEnvironment` CRD, no operator reconciler, no `HelmClient` interface extraction (that's an explicit Phase 2 prerequisite per this ADR's own Ring 1 test plan), no chart template changes shipped to `charts/orbit-platform` yet (the spike's `ClusterSPIFFEID` is hand-applied, not templated). Assumes the host is already up via `make dev-cluster && make cluster-operator && make cluster-deploy`.

1. **`scripts/cluster/spike-vcluster-up.sh`** — create namespace `orbit-devenv-spike` (labelled `orbit.io/role: devenv`, matching the selector convention in `charts/orbit-platform/templates/clusterSpiffeIDs.yaml`); `helm upgrade --install vcluster-spike vcluster --repo https://charts.loft.sh --version 0.35.1 -n orbit-devenv-spike -f <vcluster.yaml>` with the k8s distro default, `sync.toHost.serviceAccounts.enabled: true`, `sync.toHost.networkPolicies.enabled: true`, `sync.fromHost.csiDrivers.enabled: true`; wait for the control-plane pod ready; pull the vcluster kubeconfig into a **separate** file (never overwrite the default kubeconfig/context); print both host and vcluster contexts for later scripts.
2. **`scripts/cluster/spike-identity-wire.sh`** (hand-wires C3) — apply a spike-only host `ClusterSPIFFEID` modeled on the existing `orbit-tenant-service`/`orbit-tenant-agent` entries (`className: spire-system-spire`, namespace-selector on `orbit-devenv-spike`, template `spiffe://{trustDomain}/devenv/spike/…`) as a standalone `kubectl apply`d YAML, not a chart change; fetch the host SPIRE trust bundle (reuse however `orbit-identity`/gateway obtain it today) and project it into the vcluster as a ConfigMap.
3. **`scripts/cluster/spike-istio-up.sh`** — install `istio-base` + `istiod` only (skip `gateway` — not needed to answer any spike question, cuts one more heavy component from a laptop-constrained spike) inside the vcluster, same chart repo/version as `scripts/cluster/versions.env` (`1.29.1`), with the custom `sidecarInjectorWebhook.templates` from `backend/internal/operator/istio_templates.go` (`buildSpireInjectionTemplate`, `buildIdentityProxyInjectionTemplate`) copied inline; label the target namespace `istio-injection: enabled` inside the vcluster.
4. **`scripts/cluster/spike-probe.sh`** — deploy two annotated probe pods inside the vcluster and assert, non-interactively (PASS/FAIL, not eyeballed):
   1. pods land as translated/synced pods on a **host** node;
   2. the synced host pod has the injected `istio-proxy` and SPIRE-socket-mount containers (proves the in-vcluster webhook fired and survived the sync);
   3. the `csi.spiffe.io` ephemeral inline volume is mounted and the Workload API socket is readable from inside the pod;
   4. the host SPIRE agent attested the pod and issued an SVID under `spiffe://{trustDomain}/devenv/spike/…`;
   5. mTLS between the two probe pods negotiates using the devenv-prefixed identity;
   6. a NetworkPolicy fence on `orbit-devenv-spike` blocks the probe pod from reaching a host control-plane service (e.g. `orbit-identity.orbit-system`), contingent on the `sync.toHost.networkPolicies` opt-in above actually working.
5. **`scripts/cluster/spike-teardown.sh`** — idempotent uninstall of the in-vcluster istio releases, the vcluster itself, the namespace, the spike `ClusterSPIFFEID`, and the host-side bundle ConfigMap.
6. **Code change** — `backend/internal/identity/principal_extractor.go`, `principalFromSpiffeID`: before the existing `switch` on path segments, detect a leading `devenv` segment, lift `{env-id}` into `Claims` (mirroring how `run_id` is lifted today for the sandbox case), strip the `devenv/{env-id}` prefix, and fall through unchanged to the existing `system/…`/`tenant/…` handling. Table-driven tests: `system/...`/`tenant/...` unchanged (no regression), `devenv/{id}/system/...` and `devenv/{id}/tenant/.../agent/...` strip and parse correctly, a `devenv/...` ID never silently parses into a bare `system/...` principal without the env-id claim attached, malformed devenv paths still error. Check/extend nearby OPA rego tests to assert host policies don't match `devenv/…`-prefixed identities.
7. **Fidelity matrix** — once the above actually runs, replace/extend this section with real PASS/FAIL results per the ADR's stated spike questions (CSI socket sync, pod-label propagation / `ClusterSPIFFEID` selector strategy, Istio injection surviving the syncer, devenv SVID flow through principal extraction/XFCC/OPA/chart authz, end-to-end mesh mTLS) before Phase 2 (CRD + reconciler) starts, per this ADR's own gating.

---

## Validation

### Success Metrics

- Time from button press to `Ready` ≤ 5 minutes (p90)
- ≥ 80% of platform-layer PRs (operator/charts/CRDs/mesh) validated in a dev environment before merge, measured over a quarter
- Shared-staging breakage incidents attributable to in-development changes trend to zero
- Idle-environment cost: sleeping environment consumes storage only (control plane and workloads scaled to zero by the reconciler)
- Tier 2 retirement: `make dev-cluster` invocations trend to zero after GA, and the target plus its supporting scripts are removed within one quarter of passing the parity gate

### Monitoring

- Operator metrics per reconciler phase (provision duration, failure counts by step) via the existing OTel discipline ([opentelemetry-observability-discipline](2026-03-21-opentelemetry-observability-discipline.md))
- Environment inventory (active/sleeping/expired counts, age distribution, per-owner counts) surfaced on the *Dev Environments* page
- Audit events for provision/connect/extend/destroy, attributed to the acting principal

---

## Related Decisions

- [local-development-environment](2026-03-10-local-development-environment.md) — Tier 1 remains the local inner loop; **Tier 2 (k3d) is superseded by this decision** once the parity gate passes (Cilium posture inherited unchanged)
- [orbit-kubernetes-operator](2026-03-31-orbit-kubernetes-operator.md) — the operator and CRD reconcile model this extends
- [helm-chart-distribution-and-gitops-repository-structure](2026-03-31-helm-chart-distribution-and-gitops-repository-structure.md) — the charts and `deploy/local/` reference install reused inside each environment
- [policy-gateway-and-authorisation-model](2026-04-14-policy-gateway-and-authorisation-model.md) — scope gating for the provisioning API
- [agents-bff-integration-and-run-chat-wiring](2026-06-04-agents-bff-integration-and-run-chat-wiring.md) — the UI → BFF → backend pattern the button follows
- [sandbox-provisioning-and-shell-proxy](2026-05-11-sandbox-provisioning-and-shell-proxy.md) — the adjacent (and distinct) agent-sandbox "environment" concept

---

## References

- [vcluster documentation](https://www.vcluster.com/docs)
- [vcluster sleep mode](https://www.vcluster.com/docs/vcluster/next/configure/vcluster-yaml/sleep-mode) — Enterprise-licensed; **not adopted** (sleep is reconciler-implemented, see decision 8)
- [vcluster OSS vs Free/Enterprise tiers](https://www.vcluster.com/docs/vcluster/introduction/oss-vs-free)
- [CNCF Kubernetes conformance — vcluster](https://github.com/cncf/k8s-conformance)

---

## Notes

This ADR was drafted first at the requester's direction; the PRD was back-filled the same day: [ephemeral-dev-environments](../prd/ephemeral-dev-environments.md). Scope decisions recorded there: the button is available to **every authenticated Orbit user** (quota/TTL as guardrails, policy restrictions possible later), and the rollout host is the **existing staging cluster** (making the node-pool/quota/pre-pull safeguards of decision 1 mandatory from day one). The remaining open question — the exact parity-gate evidence bar — is tracked in the PRD.

**Last Updated:** 2026-07-07
