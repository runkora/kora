# Ephemeral Development Environments — Production Delivery PRD

**Status:** Draft
**Created:** 2026-07-13
**Last Updated:** 2026-07-13
**Owner:** Francis Holt (Platform Engineering)
**Stakeholders:** Platform engineering team (primary users), operator of the shared staging/host cluster, security review
**Technical design:** [ephemeral-development-environments-via-vcluster](../adr/2026-07-07-ephemeral-development-environments-via-vcluster.md) (ADR, including the full prototype log)

---

## Executive Summary

The v1 PRD and its ADR led to a **working end-to-end prototype on k3d**: a developer presses *New dev environment* in the Orbit UI and gets a complete, isolated Orbit installation — its own vcluster, its own operator, charts, mesh, identity, database, and login — reachable in the browser, connectable with `kubectl`, sleeping when idle, and expiring on a TTL. Every architectural risk the ADR flagged has been spiked, root-caused, or fixed live: host-SPIRE identity for vcluster workloads, in-vcluster Istio, kubeconfig brokering, path-based shared-host ingress, reconciler-implemented sleep.

The prototype was deliberately vibe-coded to learn fast. It is **not production code**: it is k3d-hardwired (hostnames, registry, ports, DNS hacks), carries known correctness and governance gaps (no audit events, `spec.ref` is a no-op, a routing-cookie hijack vector, a backend hole in the nested-provisioning guard), and encodes a dozen non-obvious, live-debugged design constraints only in ADR prose and code comments.

This PRD defines the **production delivery**: rebuild/harden the feature to Orbit's engineering standards, deploy it to the cloud host cluster (the existing staging cluster, per the v1 decision), and drive it to the parity gate that retires the k3d Tier 2 (`make dev-cluster`). The single most important property of this delivery is that **none of the prototype's hard-won findings are lost** — each one is restated here as a requirement or a pinned design decision, so the clean implementation never re-discovers them in production.

---

## Problem Statement

### Current State

Two problems remain, one inherited and one new.

**The inherited problem (unchanged from v1):** full-stack Orbit development happens on a per-laptop k3d cluster (`make dev-cluster`, Tier 2) or a contended shared staging cluster. Tier 2 is slow, heavy, drifts from real deployments, and duplicates deployment configuration that must track every chart change; staging is a single point of contention and breakage. Cluster-scoped work (CRDs, operator, mesh) has no safe home.

**The new problem (created by the prototype):** a feature that works impressively on one k3d cluster now exists, but it cannot ship as-is:

1. **k3d-hardwired.** The app base URL (`orbit.local.clearroute.io:8443`), image registry (`orbit-registry:5001`), a CoreDNS split-horizon ConfigMap hack, the `localhost:16443` kubeconfig default, and `dev.orbit.local` SNI domain are all local-cluster assumptions baked into config defaults and code paths.
2. **Governance gaps.** No audit events are emitted for any lifecycle action — the "audit trail" is a soft-deleted Postgres row. The guard preventing a dev environment from provisioning nested dev environments exists only in the frontend/BFF; the backend API has no check. Kubeconfigs mint `system:masters` client certs.
3. **Correctness debt.** `spec.ref` (the version selector) is accepted and ignored — every environment installs `:latest` with `pullPolicy: Always`. The reconciler spins (~every 12s in steady state, against an intended 5-minute requeue). The routing-cookie ingress fallback can hijack main-app traffic into a stale dev environment (stopgap: 15-minute cookie cap; real fix designed, not built). Sleep has a residual race that strands pods. Transient errors surface as `Failed` phase.
4. **Sloppy, unoptimised code.** Hardcoded magic constants ("re-guessed against live runs"), stale comments contradicting behaviour, duplicated constants across packages, no Makefile/dev tooling, no CI integration suite, fixture credentials as literals.
5. **Knowledge is trapped.** ~15 load-bearing findings (why NetworkPolicy is disabled inside environments, why the host ClusterSPIFFEID needs a `devenv` exclusion, why native sidecars are pinned off, why the JWKS is fetched by the host operator, why Istio config lives in `istio-system`, …) exist only as ADR progress-log prose and inline comments. A clean rebuild that doesn't treat them as requirements will re-hit every one of them — most only reproduce on a live cluster.

### User Pain Points

- Platform developers still cannot use the feature: it runs on one laptop's k3d cluster, not on shared cloud infrastructure
- The team cannot trust it yet: no audit, no admin inventory, no metrics, no CI coverage of the only future full-stack path
- Every week the prototype code lives unrefactored, more code is written against its shapes and the rebuild cost grows

### Business Impact

- The v1 business case (late validation of platform changes, staging contention, Tier 2 maintenance burden) remains unrealised until this ships to the cloud
- The prototype investment depreciates if its findings are not converted into tested, documented production code
- Orbit's dogfooding story ("governed, button-press provisioning of workloads, built on Orbit itself") is only credible when the feature itself is governed — audited, policy-gated, quota-capped — on real infrastructure

---

## Goals and Objectives

### Primary Goals

1. **Ship to cloud**: any authorised developer provisions a complete, isolated Orbit environment from the Orbit UI on the cloud host cluster (staging), in minutes, without `kubectl`, Helm, or host-cluster access
2. **Production-grade implementation**: the prototype is refactored/rebuilt to Orbit's conventions (`make check` clean, typed configs, injected loggers, sentinel errors, feature-package structure), with every prototype finding encoded as a test or a pinned decision — zero re-discovery
3. **Fully governed**: scope-gated, quota-limited, TTL-bounded, and **audited** — every lifecycle action emits an attributed event; the nested-provisioning guard is enforced server-side
4. **Version-selectable environments**: `spec.ref` actually selects the chart/image version installed (default: latest `main` CI build); no `:latest` floating tags
5. **Replace Tier 2 (`make dev-cluster`)** behind the parity gate, exactly as v1 committed: one full-stack mechanism, not two
6. **Cost approaches zero when idle**: sleep verified to scale in-vcluster workloads (measured ~97% CPU reduction in the prototype), TTL expiry leaves nothing orphaned

### Success Metrics

- Button press → `Ready` ≤ 5 minutes (p90), **measured on the cloud host cluster** with image pre-pull in place (k3d numbers do not count)
- Wake from sleep ≤ 60 seconds; sleeping environment ≤ 5% of its running CPU footprint (prototype baseline: ~7m CPU vs ~280m running)
- 100% of lifecycle actions (provision/connect/extend/wake/destroy/expire/auto-sleep) emit audit events attributed to the acting principal
- Steady-state reconcile cost: no reconcile of an unchanged `Ready` environment more often than its computed requeue interval (the prototype's ~12s spin is a defect, not a baseline)
- ≥ 80% of platform-layer PRs (operator/charts/CRDs/mesh) validated in a dev environment before merge, measured over a quarter after GA
- `make dev-cluster` invocations trend to zero after GA; the target and its supporting scripts are removed within one quarter of passing the parity gate
- The `devenvironments` integration suite runs in scheduled CI before the parity gate is declared passed

### Non-Goals

- Replacing Tier 1 (`make dev-up`) — the fast, offline Go inner loop stays local and unchanged
- Preserving an offline full-stack development path after Tier 2 retirement (deliberate, per v1)
- Ephemeral environments for Orbit customers or their agents (agent sandboxes are a separate capability)
- PR/CI-triggered preview environments (future iteration; the trigger in scope is the UI button — but `spec.ref` + CI-built images are designed so previews are a small follow-up, not a redesign)
- Production-hardening validation (Cilium/eBPF, cloud IAM policy work) — remains a CI/staging concern
- Transparent wake-on-connection (vcluster's native sleep/auto-wake is Enterprise-licensed; wake stays an explicit UI/API action)

---

## User Stories

### Primary User Stories

**As a** platform developer,
**I want to** provision a full Orbit environment at a chosen version by pressing a button in the Orbit UI,
**So that** I can test operator, chart, CRD, and service changes against a real installation without building a local cluster or queueing for staging.

**Acceptance Criteria:**
- [ ] A *Dev Environments* page lists my environments and offers *New dev environment*
- [ ] I can pick the ref/version to install (default: latest `main` CI build) — **and the environment actually installs that ref** (charts and images pinned to it, no floating tags)
- [ ] Provisioning shows live status (Pending → Provisioning → Ready) with per-stage conditions and surfaces failures with actionable detail; transient reconcile errors do not flap the phase to `Failed`
- [ ] A ready environment is a complete Orbit installation (control plane, seeded tenant, fixture data, working browser login with full admin scopes) reachable at its own URL

---

**As a** platform developer,
**I want to** connect to my environment with cluster-admin rights inside it,
**So that** I can deploy work-in-progress builds, break things, and iterate freely without any risk to the host cluster or other developers.

**Acceptance Criteria:**
- [ ] *Connect* issues a short-lived kubeconfig (default 8h) scoped to my environment only; the private key is generated per-request and never persisted
- [ ] I am cluster-admin inside the environment; the same credential is rejected by the host API server (verified by test, as in the prototype)
- [ ] Deploying a broken operator or CRD inside my environment has no effect on the host or other environments
- [ ] The environment's own Orbit UI is reachable at its URL, and browser sign-in works end-to-end against its seeded identity provider with `platform-admin` scopes
- [ ] Connecting to a sleeping environment tells me it is asleep and offers an explicit wake (no silent failure)

---

**As a** platform developer,
**I want** my environment to sleep when idle, wake when I ask, and expire on a TTL I can extend,
**So that** I never pay attention to cleanup and the team never pays for forgotten environments.

**Acceptance Criteria:**
- [ ] Environments sleep after 2h idle (kubeconfig mints and UI actions count as activity); sleep scales the vcluster control plane **and** in-vcluster workloads to zero with state preserved on PVCs, with no stranded pods (the prototype's scale-down/control-plane-kill race is fixed)
- [ ] Default TTL 24h, extendable from the UI up to a hard cap (7 days); extension semantics (total-from-creation) are stated in the UI
- [ ] Expiry deletes the environment and everything in it — namespace, vcluster PVCs, host ClusterSPIFFEIDs, Istio routes; a sweep for the env ID finds nothing (automated as a test)
- [ ] I can destroy immediately from the UI; destroy and TTL expiry converge on the identical cleanup path

---

**As a** platform administrator,
**I want** provisioning to be scope-gated, quota-limited, and fully audited,
**So that** the button is safe to expose to every Orbit user and capacity stays under control.

**Acceptance Criteria:**
- [ ] Provision/list/get/extend/wake/connect/destroy require `devenvironments:read|write|admin` scopes enforced by the policy gateway — including the sub-routes (`/wake`, `/extend`, `/kubeconfig`), each covered by an explicit test, not transitively by prefix-matching assumptions
- [ ] Per-developer cap on active environments (default 2, policy/config-adjustable)
- [ ] **Every lifecycle action — including reconciler-initiated ones (auto-sleep, expiry) — emits a structured audit event with the acting principal (or `system:operator` for reconciler actions) and the environment ID**
- [ ] An admin inventory view shows all environments (owner, phase, age, expiry) via `devenvironments:admin`, and an admin can force-destroy any environment from it
- [ ] A dev environment's own Orbit install **cannot** provision nested environments — enforced in the backend service (via its own config's environment identity), not only in the BFF

### Secondary User Stories

**As a** developer reviewing a colleague's PR, **I want to** open their running environment's URL, **so that** I can verify behaviour without provisioning my own. (P1 — the app URL is shareable; read access via existing policy grants.)

**As a** platform operator, **I want** provision-duration and failure metrics per reconcile stage, **so that** I can see p90 drift and diagnose failures without reading operator logs. (P1.)

---

## Proposed Solution

### High-Level Approach (validated by the prototype — unchanged)

Each environment is a **vcluster** (OSS, version-pinned, k8s distro) in a host namespace `orbit-devenv-{id}`. The UI creates a registry row and an `OrbitDevEnvironment` CR; the existing operator's third reconciler turns the CR into namespace → host identity wiring → vcluster → in-vcluster Orbit install (operator chart → `OrbitPlatform` + seeded `OrbitTenant`, `spire.enabled=false`) → ingress routes, and enforces TTL/sleep. Workload identity is host-provided (option C3): host SPIRE agents attest the synced pods natively and mint path-scoped SVIDs under `spiffe://{td}/devenv/{id}/…`. Access is brokered: kubeconfigs are minted via a CSR-on-CR flow signed by the operator against the vcluster's own CA and delivered over a shared SNI TLS-passthrough listener; the browser app is served from one shared hostname with the env ID as a path prefix.

The prototype proved all of this end-to-end on k3d, including the full nested install reaching `Ready` with real mesh mTLS on devenv-prefixed identities, browser login, and the complete lifecycle. **The architecture is not in question; this PRD is about the implementation and the infrastructure.**

### What Changes From Prototype to Production

| Area | Prototype (k3d) | Production |
|------|-----------------|------------|
| Host cluster | Local k3d (`k3d-orbit-local`) | Cloud staging cluster; dedicated/tainted node pool for devenv workloads |
| Version selection | `spec.ref` ignored; `:latest` + `pullPolicy: Always` from `orbit-registry:5001` | `spec.ref` resolves to CI-built, immutably-tagged charts+images in the cloud registry |
| App ingress base | `https://orbit.local.clearroute.io:8443` (k3d LB port) | Real DNS + TLS on the staging gateway host; same path-prefix scheme |
| kubectl access | `localhost:16443` via k3d port mapping | Cloud LB TCP listener on the Istio ingress gateway Service (`16443` passthrough); `apiServer.externalAddress` config |
| Unprefixed app requests | Routing-cookie fallback (hijack vector; 15-min stopgap cap) | App-wide runtime prefix awareness; cookie fallback deleted (at most a `/_next/*`-scoped asset fallback) |
| DNS for in-vcluster issuer | CoreDNS `coredns-custom` split-horizon hack | Real public DNS; hack removed or gated to local-only |
| Audit | None | Events for every lifecycle action, attributed |
| Nested-provisioning guard | Frontend/BFF only | Backend service check as well |
| Config | Hardcoded constants, magic quota numbers | Typed config via `config.LoadService[T]` + chart values; quota sized from measured footprint |
| Testing | Unit tests + manual live verification | Unit + repository + `//go:build integration` E2E suite in scheduled CI; regression scripts for vcluster/Istio version bumps |
| Observability | Operator logs only | Per-stage provision metrics, inventory gauges, audit events; hot-loop fixed |

### User Experience

Unchanged from v1 in shape, with the prototype's corrections folded in:

1. Developer opens **Dev Environments**, clicks **New dev environment**, names it, optionally picks a ref, confirms
2. Card shows live per-stage progress (the CRD's conditions: NamespaceReady → VClusterReady → InVClusterPlatformApplied → InVClusterPlatformReady); flips to **Ready** with the environment URL and expiry countdown
3. **Open** goes to `{appExternalURL}/{env-id}` — login with the seeded dev user works in the browser, and the URL keeps its `/{env-id}` prefix through navigation (prefix-aware app, not cookie routing)
4. **Connect** returns a one-time-displayed kubeconfig (8h); against a sleeping environment it offers wake first
5. Sleep after idle; explicit **Wake**; **Extend** to a total TTL up to 7 days; **Destroy** or expiry removes everything
6. Inside a dev environment, the Dev Environments page explains that nested provisioning is unavailable (and the backend refuses it regardless of client)

---

## Pinned Design Decisions (prototype findings — do not re-derive, do not silently change)

These were each discovered by live debugging and are load-bearing. The production implementation MUST preserve them, and each MUST be covered by a test or a rerunnable assertion script. References point at the ADR's progress log and current code.

**vcluster configuration**
1. `controlPlane.distro.k8s.enabled: true` must be explicit (defaults off; k3s distro removed in vcluster ≥0.33). `sync.toHost.serviceAccounts` and `sync.toHost.networkPolicies` must be `true` (SPIRE `k8s_psat` TokenReview and host-side fencing depend on them); `sync.fromHost.csiDrivers` enabled.
2. `controlPlane.proxy.extraSANs` must carry the kubectl SNI host **at first provision** — vcluster persists its PKI on the PVC and crash-loops if SANs change on upgrade. Recovery from any wedged state is delete-and-re-provision, never in-place repair.
3. The kubeconfig secret is `vc-{release}` (not `vc-config-{release}`); its `server:` must be rewritten to `https://{release}.{namespace}` for in-cluster access.

**Identity (C3)**
4. Two disjoint host `ClusterSPIFFEID`s per environment: component-scoped stable paths `devenv/{id}/system/{component}` (selector: `spiffe.io/component` present) and a per-pod catch-all (selector: component label absent). Selectors must be disjoint — a pod matching two entries gets a nondeterministic SVID (SPIRE SDS returns `Identities[0]` for `"default"`).
5. The host-wide `istio-sidecar` ClusterSPIFFEID must exclude `orbit.io/role: devenv` namespaces (the prototype's Bug 4: devenv pods double-matched and shipped `ns/{ns}/sa/{sa}` SANs).
6. `principalFromSpiffeID` strips an optional `devenv/{id}` prefix and lifts the ID into claims; a devenv path must never parse into a bare `system/…` principal. No rego changes needed (policies match structured principal fields, never raw SPIFFE paths) — keep the tests asserting this.
7. Chart-shipped `DestinationRule` SANs and `AuthorizationPolicy` principals render through the `orbit.spiffeIDBase` helpers, appending `/devenv/{id}` only when `platform.spiffe.devEnvID` is set. One optional prefix shipped everywhere; never per-environment values files. Without the matching DestinationRule, mesh mTLS fails `SAN match` (the spike's 503).

**Istio in a vcluster**
8. Pin `ENABLE_NATIVE_SIDECARS: "false"` via the istiod chart's **flat `env:` key** (`pilot.env.*` is not read for istiod's own pod — a false-confirmation the prototype made twice). The vcluster reports a newer k8s version than the host, which otherwise flips Istio into native-sidecar mode that our injection templates cannot merge into.
9. In-vcluster istiod XDS auth uses `JWT_RULE` (issuer + JWKS from the **host** API server, `audiences: ["istio-ca"]`), with the host operator fetching `/.well-known/openid-configuration` and `/openid/v1/jwks` itself (its SA token is in `system:serviceaccounts`, which the default issuer-discovery binding authorises — no anonymous auth, no extra RBAC) and handing them down via the `OrbitPlatform` CR; a credential-less loopback sidecar serves the JWKS to istiod. The multicluster remote-secret approach is a **structural dead end** in stock Istio (KubeJWTAuthenticator selects clusters by a gRPC header nothing sets) — do not re-attempt. The JWT_RULE JSON must be YAML-escaped (the chart's env template does no quoting). Use `rest.HTTPClientFor`, not the Discovery client, for the fetch (content-negotiation rejects these endpoints).
10. The host istiod must set `meshConfig.discoverySelectors` excluding devenv namespaces (else it clobbers each environment's `istio-ca-root-cert`). Consequences the implementation must carry: per-environment Istio routing objects live in `istio-system` (invisible namespaces), destinations are registered via `ServiceEntry` (`MESH_EXTERNAL`, `resolution: DNS`), and the environment's translated-name CA-root ConfigMap is mirrored back to the literal `istio-ca-root-cert` name.

**Ingress**
11. Never create a second Istio `Gateway` claiming a host+port another Gateway already serves on the same workload — Istio silently drops one. App routes attach to the platform's own `orbit-gateway`; only the kubectl route gets its own Gateway because port 16443 is exclusively its own.
12. All app path-routes live in **one aggregated VirtualService** rebuilt from the full CR list per reconcile — cross-VirtualService merge order on one host is undefined, and per-env objects would let a stale cookie-fallback outrank another env's path match. Per-env Dex routes (`/{id}/dex`, rewrite preserving `/dex`) are ordered before the env's general prefix route.
13. In-vcluster installs must override host-topology values explicitly (`gateway.tls.enabled=false` — TLS terminates once at the host gateway; `frontendHost` = shared host). Baked host values are never safe to inherit verbatim.
14. The unscoped routing cookie is a hijack vector (it outranks the main app's catch-all for every path). Production removes the general cookie fallback in favour of app-level runtime prefix awareness (`X-Forwarded-Prefix`, threaded through routing/auth/link construction — Next.js `basePath` is build-time and ruled out by the shared-image constraint). Do not delete the fallback before every unprefixed emission point is fixed.

**In-vcluster install mechanics**
15. `spire.enabled=false` must gate the `orbit-platform` chart's `ClusterSPIFFEID` templates (no SPIRE CRDs exist in a vcluster); `platform.spiffe.enabled` means "workloads carry SPIFFE identities" (true for devenvs), not "SPIRE is installed here" — conflating them suppressed all nine DestinationRules once.
16. The reconciler creates the in-vcluster `orbit-system` namespace before the nested operator exists; it must carry `istio-injection: enabled` and `spiffe.io/spire-managed: "true"` at creation (the silent no-sidecars bug).
17. NetworkPolicy enforcement is **disabled inside devenv installs** (`networkPolicy.enabled=false`): enforcement on vcluster-synced pods proved unreliable regardless of rule shape, and the host-side `default-deny-all` fence already isolates the namespace. Revisit only with a CNI change, deliberately.
18. Fixtures: Postgres/Redis/Dex + **glauth** (LDAP connector — Dex's static-password DB has no groups claim, and without groups the seeded user resolves zero scopes). Dex's issuer must be the externally-routable `{appExternalURL}/{id}/dex` and byte-identical between Dex config and the seeded tenant. Migrations run as a Kubernetes Job using the operator's own image (goose + baked migrations) — never the reconciler speaking Postgres; environments never re-migrate, they re-provision.
19. `LimitRange` container defaults must accommodate istiod's 2Gi memory request (admission rejects limit < request synchronously); quota ceilings account **declared limits**, and every injected sidecar declares 2 CPU/1Gi — size from the measured full stack, and treat "stuck Pending inside the vcluster" (invisible on the host) as the quota-exhaustion signature.

**Operator/Helm plumbing**
20. Local (baked) charts always upgrade; the skip-if-at-version optimisation must key on the requested `chartVersion` parameter (empty for local charts), never the loaded chart's own metadata version. Values-only changes to version-pinned remote releases (gateway ports, istiod env) do not reapply without a version bump — an operational gap to document and design around, not assume away.
21. Every CRD status field must be in the structural schema — pruning silently drops unknown fields (the CSR-fingerprint re-signing bug). Kubeconfig re-signing keys on a CSR fingerprint, not on certificate presence (Connect generates a fresh keypair per click).
22. The operator needs ≥1Gi memory (four serial in-process Helm installs per provision) and `authentication.k8s.io` access only as actually required (the prototype's escalated RBAC was fully reverted — keep it minimal).
23. Sleep must scale in-vcluster Deployments/StatefulSets (via the vcluster API) **and wait for propagation** before killing the vcluster control plane — the syncer needs the control plane alive to delete synced pods (the residual-race fix, required in production). `spec.awake` is the single source of truth; idle-sleep flips it itself. Activity signals must actually update `lastActivityAt` (kubeconfig mint, wake, UI actions).

---

## Scope

### In Scope

- Productionisation of the entire prototype surface: `internal/devenvironments` (identity API), the `DevEnvironmentReconciler` and its `devenvironment_*.go` satellites, CRD schema, charts wiring, BFF routes, and the Dev Environments UI
- `spec.ref` implementation: CI publishes immutably-tagged images (and chart refs) for `main` and branches; the reconciler resolves and installs the requested ref
- Cloud enablement: staging host cluster, node pool + taints, image pre-pull, LB listener for 16443, real DNS/TLS, cloud registry, removal/gating of every k3d-only code path
- Governance completion: audit events, backend nested-provisioning guard, admin inventory + force-destroy, explicit scope tests for all sub-routes
- App-wide prefix awareness and removal of the general cookie-routing fallback
- Observability: per-stage provision metrics, failure counts, inventory gauges; steady-state reconcile-loop fix
- Test completion: integration suite (`backend/tests/integration/devenvironments/`) automating the ADR's acceptance checklist, running in scheduled CI; spike scripts retained as vcluster/Istio version-bump regression checks
- **Phased retirement of Tier 2** behind the parity gate: deprecation warning, doc updates, then removal of `make dev-cluster`, its scripts and overlays; `make test-integration` retargets an ephemeral environment

### Out of Scope

- PR/CI-triggered preview environments (the `spec.ref` design deliberately leaves this a small follow-up)
- Customer-facing or multi-tenant use of dev environments
- Offline full-stack development after Tier 2 retirement
- SPIRE-deployment-layer and Cilium validation — re-homed to CI/staging, as v1 decided
- Adopting vcluster Enterprise features (native sleep, auto-wake) — sleep remains reconciler-implemented
- Fixing the pre-existing, non-devenv-specific gaps the prototype surfaced (`orbit-tool-gateway` missing from `REDEPLOY_PLATFORM_SERVICES`, `orbit-opa-gateway` health flapping) — tracked separately

### Future Considerations

- PR-linked preview environments with auto-teardown on merge
- Environment templates (slim vs full presets) and team-shared environments
- Per-environment kubectl subdomains under a real wildcard domain, if the SNI-only hostnames ever need to be resolvable

---

## Requirements

### Functional Requirements

**Must Have (P0):**
- [ ] Provision a complete Orbit environment from the UI at a selectable, actually-honoured `ref`; per-stage status visible end-to-end; transient errors do not surface as `Failed`
- [ ] Short-lived (8h), environment-scoped kubeconfig issuance via the CSR-on-CR flow; private key never persisted; credential verified rejected by the host API server; no host-cluster credentials ever reach a developer
- [ ] TTL with UI extension (7d hard cap, single source of truth for the constant), idle sleep/wake (with the propagation-race fix), expiry deletion leaving nothing orphaned (asserted by test), immediate destroy converging on the same cleanup path
- [ ] `devenvironments:read|write|admin` scope gating with explicit per-route coverage (including `/wake`, `/extend`, `/kubeconfig`); per-user active-environment quota; **audit events for every lifecycle action including reconciler-initiated ones**
- [ ] Backend-enforced nested-provisioning guard (identity service knows it is running inside a dev environment and refuses `CreateEnvironment`)
- [ ] Browser login works end-to-end (external Dex issuer, glauth-backed groups → `platform-admin` scopes) — verified live as a release gate, since the prototype closed this in code without final k3d verification
- [ ] App-wide runtime prefix awareness; general cookie-routing fallback removed; a plain visit to the main Orbit app can never be routed into a dev environment
- [ ] Environments fully isolated from the host control plane's data stores; host authorisation policies never match `devenv/…` identities (existing tests retained)
- [ ] All pinned design decisions (§ above) covered by unit/integration tests or rerunnable cluster assertions
- [ ] Parity-gate definition and Tier 2 deprecation/removal plan executed as part of this feature, not deferred

**Should Have (P1):**
- [ ] Admin inventory view (all environments: owner, phase, age, expiry) and admin force-destroy (the prototype's strict owner-only model has no admin path at all today)
- [ ] Per-stage provision-duration and failure metrics; steady-state reconcile loop fixed (no ~12s spin on unchanged objects); `EXPIRESAT` printer column fixed; spec defaults surfaced via CRD defaulting rather than status-only behaviour
- [ ] Expiry warning surfaced in the UI ahead of deletion
- [ ] Kubeconfig subject uses a dedicated group (e.g. `orbit:devenv-admin` bound to cluster-admin inside the vcluster) instead of `system:masters`, so access is nameable and revocable inside the environment
- [ ] Developer tooling: Makefile targets for the reconciler-iteration loop and a documented runbook replacing tribal knowledge (`redeploy-operator` + hand-applied CRs)

**Nice to Have (P2):**
- [ ] Size presets (small/standard/large) mapping to quota/LimitRange profiles
- [ ] One-click "re-provision with same settings" after destroy (pairs with the delete-and-re-provision recovery philosophy)
- [ ] Helm release-history cap (`--history-max`) for the operator's always-upgrading local charts

### Non-Functional Requirements

**Performance:**
- Button press → Ready ≤ 5 minutes (p90) on the cloud host, which **requires image pre-pull** (DaemonSet pre-puller or node-image baking) for the full stack — a cold pull does not fit the budget
- Wake ≤ 60s; provisioning N environments concurrently must not degrade the host control plane (dedicated node pool via taint/toleration; per-namespace ResourceQuota as backstop)
- Operator sized for its real profile: ≥1Gi memory, four serial Helm installs per provision; concurrent provisions bounded (reconciler concurrency configured deliberately, not defaulted)

**Security:**
- Developer privilege confined to the virtual cluster; host-side blast radius is one quota-capped namespace fenced by the host's default-deny NetworkPolicy
- Environment workload identities path-scoped (`devenv/{id}/…`) and never authorised against host or tenant policies; the identity service's Kubernetes RBAC stays CRUD-on-one-CRD with no status subresource; the operator's RBAC stays minimal (no TokenReview grant — the JWKS design needs none)
- All access credentials short-lived and brokered; fixture secrets generated per-environment at provision time (no literal passwords in code for anything security-relevant; the fixed dev-user credential is acceptable only for the seeded in-environment login and must be documented as such)
- vcluster image pinned to the **OSS** distribution explicitly — the prototype's live cluster was observed running `vcluster-pro:0.35.1` (the chart default); licensing posture must be verified and the image ref made deliberate before cloud rollout

**Reliability:**
- Level-triggered, idempotent reconcile: operator killed mid-provision converges unassisted (prototype-verified; keep the test); wedged environments are deleted and re-provisioned, never repaired
- The host cluster is critical developer infrastructure post-retirement: named availability owner, and the integration suite in CI as the independent safety net

**Scalability:**
- Support ≥ 2 active environments per developer across the team concurrently within planned host capacity; capacity plan built from the **measured** per-environment footprint (prototype observation: ~20 pods, ~3.5 CPU / 6Gi requested, ~38 CPU / 27Gi declared limits) rather than the prototype's guessed ceilings

**Observability:**
- Provision duration and failure counts per reconcile stage exported as metrics; environment inventory (active/sleeping/expired, age, owner) as gauges; all lifecycle actions as audit events; operator logs flow through the injected logger including controller-runtime's own (the discarded-logger bug stays fixed)

**Code Quality:**
- `make check` clean; all Orbit conventions (typed config via `config.LoadService[T]` + chart-value env vars — never APP_ENV YAML files for in-cluster overrides; injected loggers; sentinel errors; feature independence via `internal/platform/orbitapi`)
- Constants defined once (the 7-day cap currently lives in two packages); stale comments from the prototype removed; magic values promoted to config with documented rationale

---

## Cloud Deployment Requirements

The prototype ran on k3d standing in for the host. Cloud rollout (staging cluster, per the v1 decision) needs:

1. **Node pool**: a tainted/labelled pool for `orbit-devenv-*` workloads with toleration wiring in the reconciler's namespace/vcluster values, so environment bursts cannot starve staging workloads; single-pool fallback is ResourceQuota-only
2. **Image pre-pull**: DaemonSet pre-puller (or baked node images) for the vcluster, Istio, fixture, and Orbit images of the current `main` ref on the devenv pool
3. **Registry**: `devEnv.imageRegistry` pointed at the cloud registry; CI publishes immutably-tagged Orbit images per `main` merge (and branch builds for `spec.ref`); host-node pull credentials cover synced pods (they are ordinary host pods)
4. **App ingress**: `devEnv.appExternalURL` = the staging gateway's real host; TLS remains `orbit-gateway`'s own credential (never a devenv-specific one, per pinned decision 11); one DNS record — no wildcard needed (the path-based amendment's whole point)
5. **kubectl ingress**: the Istio ingress gateway Service (LoadBalancer) exposes TCP 16443 passthrough; `apiServer.externalAddress` = `{lb-host}:16443`; SNI hosts (`{id}.{ingressDomain}`) need no DNS records
6. **DNS hygiene**: the CoreDNS split-horizon hack is removed/gated local-only; the shared app host must resolve publicly (browser, and from inside vclusters for the Dex issuer — via ordinary DNS, no CoreDNS patching)
7. **Identity plane**: host SPIRE with the SPIFFE CSI driver (already enabled in chart values — verify empirically per install, per the ADR's correction); trust domain read from the host `OrbitPlatform`, never configured per environment
8. **JWKS/issuer**: verify the host-operator issuer/JWKS fetch against the cloud API server (managed control planes serve the same endpoints; `system:service-account-issuer-discovery` covers the operator's SA by default) — a Phase-B checklist item, not an assumption
9. **Capacity & ownership**: capacity plan for N concurrent environments from measured footprint; named availability owner for the host cluster before Tier 2 deprecation

---

## Technical Debt Register (prototype → production)

The definitive fix-list distilled from the codebase and live-cluster review. P0 items block cloud GA; P1 items block the parity gate; P2 are tracked follow-ups.

| # | Item | Where | P |
|---|------|-------|---|
| 1 | No audit events anywhere in the feature | `internal/devenvironments/service.go`, reconciler | P0 |
| 2 | `spec.ref` accepted but ignored; in-vcluster operator image `:latest` + `pullPolicy: Always` | `devenvironment_platform.go`, `devenvironment_migrations.go` | P0 |
| 3 | Nested-provisioning guard BFF-only; backend has no environment-identity signal | `frontend/src/app/api/dev-environments/route.ts`, `internal/devenvironments/config.go` | P0 |
| 4 | Cookie-routing fallback can hijack main-app traffic (stopgap: 900s Max-Age) | `devenvironment_app_ingress.go`, frontend routing | P0 |
| 5 | vcluster image observed as `vcluster-pro` on the live cluster; OSS pin/licensing unverified | `ensureVCluster` values, `versions.env` | P0 |
| 6 | Scope enforcement of `/wake`, `/extend`, `/kubeconfig` relies on prefix-matching semantics, untested per-route | `cmd/identity/main.go` route table | P0 |
| 7 | Browser login + zero-scopes fixes landed but never live-verified | `devenvironment_fixtures.go` (glauth/Dex), app-ingress Dex routes | P0 |
| 8 | Hot reconcile loop: `Ready` env reconciles ~every 12s against a 5m requeue (status-write churn) | `devenvironment_controller.go` | P1 |
| 9 | Sleep race: control plane killed before in-vcluster scale-down propagates; strands pods | `reconcilePowerState` / `scaleInVClusterWorkloads` | P1 |
| 10 | Hardcoded constants: quota ceilings ("re-guessed"), ports, domains, `localhost:16443` default | `devenvironment_controller.go:55-90`, `config.go` | P1 |
| 11 | CoreDNS split-horizon mirror is k3d-only and runs unconditionally | `ensureInVClusterCoreDNSSplitHorizon` | P1 |
| 12 | `failReconcile` marks transient errors as phase `Failed` | `devenvironment_controller.go` | P1 |
| 13 | No admin path for other users' environments (owner-strict everywhere; `admin` scope only lists) | `internal/devenvironments/service.go` | P1 |
| 14 | Kubeconfig endpoint blocks a request thread polling the CR up to 20s | `service.go` Kubeconfig | P1 |
| 15 | `EXPIRESAT` printer column renders `<invalid>`; spec defaults (`ttl: 0s`) not defaulted into spec | `crd-orbitdevenvironment.yaml` | P1 |
| 16 | `system:masters` in minted client certs | `service.go` / `devenvironment_csr.go` | P1 |
| 17 | No integration suite in CI; acceptance checklist is manual | `backend/tests/integration/` (absent) | P1 |
| 18 | `ttlHardCap` duplicated across packages; stale comments (controller header, port-constant claim) | `models.go`, `devenvironment_controller.go`, `devenvironment_ingress.go` | P2 |
| 19 | Fixture literals: SHA-256 of "password", `insecureNoSSL`, 2048-bit signing key | `devenvironment_fixtures.go` | P2 |
| 20 | No Makefile/dev tooling for the devenv iteration loop | `Makefile` | P2 |
| 21 | Helm release-history growth from always-upgrading local charts | `helm.go` | P2 |
| 22 | Polling only covers Pending/Provisioning; Sleeping→Ready wake transition not polled in UI | `use-dev-environments.ts` | P2 |

---

## Dependencies

### Internal Dependencies

- **Orbit Kubernetes operator** — hosts the reconciler (exists; hardened per this PRD)
- **Policy gateway & scope registration** — `devenvironments:*` family (exists; per-route tests added)
- **Charts (`orbit-operator`, `orbit-platform`, `orbit`) + `deploy/local/` reference install** — the in-environment install and fixture seed; must stay installable at all times; charts baked into the operator image including `orbit-operator` itself
- **Host SPIRE installation** with SPIFFE CSI driver (exists, enabled; verify per install)
- **Event/audit emission path** — whatever the platform's current audit mechanism is; if the platform event spine is still a stub, the interim mechanism must still capture attributed lifecycle events (blocker to resolve in the ADR update, not silently skipped)
- **CI image publishing** — immutably-tagged `main`/branch images for `spec.ref` (new requirement on CI)
- **Frontend BFF pattern & Terra** — Dev Environments page (exists; prefix-awareness work added)

### External Dependencies

- **vcluster** (OSS, version-pinned — currently 0.35.1; upgraded deliberately with the spike scripts as regression checks)
- **Istio / SPIRE chart pins** (existing) — the JWT_RULE, native-sidecars, and discoverySelectors behaviours are version-sensitive; re-run the assertion scripts on every bump
- **Cloud LB** supporting a TCP passthrough listener alongside HTTPS on the ingress gateway Service
- **Container registry** reachable from host nodes

---

## Risks and Mitigations

| Risk | Impact | Probability | Mitigation |
|------|--------|-------------|------------|
| Cloud behaviours diverge from k3d (managed API server endpoints, LB TCP listeners, CNI NetworkPolicy timing, storage classes for vcluster PVCs) | High | Medium | Phase B is an explicit cloud-verification pass re-running the acceptance checklist and spike assertions on staging before any GA; findings appended to the ADR |
| Prototype findings lost in the rewrite — clean code re-hits live-only bugs | High | Medium | Pinned-decisions section is a review checklist; each finding carries a test or assertion script; PRs touching these areas reference the pinned decision they preserve |
| Cookie-fallback removal breaks unprefixed call sites not yet found | Medium | High | Remove only after an audited sweep of emission points; retain a `/_next/*`-scoped asset fallback; live verification on staging with two concurrent environments |
| vcluster licensing (pro image in use unnoticed) | Medium | Medium | P0 register item: pin the OSS image explicitly, legal/licensing check before cloud rollout |
| Host (staging) capacity exhausted by environment bursts | Medium | Medium | Tainted node pool, per-namespace ResourceQuota from measured footprint, sleep + TTL defaults, per-user quota |
| Parity gate never quite passes and both mechanisms live forever | Medium | Medium | Parity bar defined up front; removal is a tracked success metric with a deadline; work not covered (SPIRE deployment layer, Cilium) explicitly re-homed |
| Host cluster outage blocks all full-stack development post-retirement | High | Low | Availability ownership named before retirement; Tier 1 unaffected as partial fallback; CI integration suite keeps an independent verification path |
| Istio/vcluster version bumps silently change load-bearing behaviours (JWT auth chain, sidecar mode, syncer semantics) | Medium | Medium | Spike scripts kept as the version-bump regression suite; pins upgraded deliberately, never floated |

---

## Alternatives Considered

The architectural alternatives (isolation model, provisioning trigger, identity topology) were evaluated in the ADR and **validated empirically by the prototype** — they are not re-litigated here. One new alternative is worth recording:

### Alternative: Ship the prototype as-is, refactor later

**Why not chosen:** the gaps are not cosmetic — no audit trail, a server-side authorisation hole (nested provisioning), a traffic-hijack vector, and unpinned licensing are each individually shippable-blocking. Refactor-later also loses the one moment (now) when every finding is fresh enough to encode as a test.

### Alternative: Per-environment subdomains + wildcard DNS/TLS on cloud

**Why not chosen (re-affirmed):** the ADR's 2026-07-10 amendment stands — one fixed hostname needs one DNS record and one certificate; path-based routing was implemented and verified. The cost (app prefix-awareness work) is already sunk into the design. Subdomains remain a future option for the kubectl SNI names if resolvability is ever wanted.

---

## Open Questions

1. **vcluster licensing/image** — is `vcluster-pro:0.35.1` (the chart default, observed live) acceptable to run unlicensed as OSS-mode, or must the image be pinned to the OSS build? **Owner:** Francis Holt. **Blocker for:** cloud rollout (P0 register item 5).
2. **Audit emission mechanism** — the platform event spine is a stub; what carries devenv audit events in the interim (structured audit log via the existing logging/OTel path vs a minimal events table)? **Owner:** Engineering team. **Blocker for:** P0 requirement.
3. **Exact parity-gate evidence bar** — fidelity matrix (now largely proven) plus one quarter of real usage is the ADR's proposal; confirm or tighten. **Owner:** Engineering team. *(Carried from v1 — still open.)*
4. **`spec.ref` resolution shape** — image-tag-only (charts always from the operator image at the operator's version) vs full chart+image ref resolution (charts published per ref). Image-tag-only is dramatically simpler and covers the dominant use case (test my service/operator build); full resolution covers chart-schema changes. Needs an ADR amendment. **Owner:** Francis Holt.
5. **Docs cleanup** — `ephemeral-dev-environments.md` (v1) and this file diverge as of this PRD; mark v1 superseded on approval.

---

## Timeline and Milestones

### Phases

**Phase A — Hardening & debt burn-down (on k3d):** all P0 register items except cloud-only ones; audit events; backend nested guard; `spec.ref`; prefix-awareness + cookie-fallback removal; scope tests; reconcile-loop and sleep-race fixes; conventions/refactor pass; pinned decisions converted to tests. Exit: `make check` clean, acceptance checklist green on k3d, live browser-login verification done.

**Phase B — Cloud enablement:** config surface for registry/URLs/ports/quotas; node pool + pre-pull; LB 16443 listener; DNS/TLS wiring; CoreDNS hack removal; vcluster OSS pin; JWKS/issuer verification against the cloud API server. Exit: acceptance checklist + spike assertions green on the staging host.

**Phase C — Staging rollout & measurement:** deploy to staging behind the scope gate; measure provision p90 (with pre-pull), sleep cost, per-environment footprint; tune quotas from measurements; integration suite running in scheduled CI. Exit: success-metric baselines recorded.

**Phase D — GA & governance:** open the scope to all human principals (v1 decision); admin inventory + force-destroy; expiry warnings; metrics dashboards; runbook + docs. Exit: team using dev environments for platform PRs.

**Phase E — Parity gate & Tier 2 retirement:** one quarter of real usage against the parity bar; deprecation warning on `make dev-cluster`; then removal of the target, scripts, `values.local.yaml` overlays, and CoreDNS/image-import workarounds; `make test-integration` retargeted; the local-development ADR amended.

### Key Milestones

- [ ] PRD (this document) approved
- [ ] ADR amended: `spec.ref` resolution shape, audit mechanism, cloud-verification findings
- [ ] Phase A exit: P0 debt cleared, acceptance checklist green on k3d
- [ ] Phase B exit: checklist green on staging (cloud)
- [ ] GA to the team
- [ ] Parity gate passed; Tier 2 deprecated
- [ ] `make dev-cluster` removed

---

## Appendix

### What the prototype proved (evidence base)

Verified live on `k3d-orbit-local` (full detail in the ADR's progress log):

- **Full provisioning loop**: UI button → BFF → identity API (quota, registry row) → `OrbitDevEnvironment` CR → reconciler → namespace/quota/identity → vcluster (0.35.1, k8s distro) → in-vcluster orbit-operator → `OrbitPlatform` + seeded `OrbitTenant` → all ~11 Orbit services `Running` with real sidecars → `Ready` with conditions, in under 5 minutes on k3d
- **Identity (C3)**: host SPIRE attests synced pods via the CSI-mounted socket; stable `devenv/{id}/system/{component}` SVIDs; mesh mTLS passes on prefixed SANs; host rejects devenv identities; kubeconfig client cert works against the vcluster and is rejected by the host
- **In-vcluster Istio**: istiod runs inside the vcluster; XDS authenticated via JWT_RULE against host-issued SA tokens; injection survives the syncer
- **Access**: SNI TLS-passthrough kubectl route on 16443 (real unmodified `kubectl` verified); path-based app ingress with working browser login flow (final glauth/Dex-issuer fixes landed, live verification pending — carried as a P0 gate)
- **Lifecycle**: idle auto-sleep (measured ~97% CPU reduction after the in-vcluster scaling fix), explicit wake with state intact, TTL expiry with nothing orphaned, operator-crash-mid-provision convergence, delete/expiry converging on one cleanup path

### Related Documents

- [ephemeral-development-environments-via-vcluster](../adr/2026-07-07-ephemeral-development-environments-via-vcluster.md) — technical design + complete prototype log (spike results, ten Phase-2 slices, amendments, corrections)
- [ephemeral-dev-environments.md](ephemeral-dev-environments.md) — v1 PRD (superseded by this document on approval)
- [local-development-environment](../adr/2026-03-10-local-development-environment.md) — the two-tier local strategy whose Tier 2 this replaces
- [orbit-kubernetes-operator](../adr/2026-03-31-orbit-kubernetes-operator.md) — the reconcile model the provisioning extends
- [policy-gateway-and-authorisation-model](../adr/2026-04-14-policy-gateway-and-authorisation-model.md) — scope gating

### Glossary

- **Dev environment**: an ephemeral, per-developer full Orbit installation provisioned from the UI (distinct from `orbit-agents`' agent-sandbox `EnvironmentConfig`)
- **vcluster**: a certified virtual Kubernetes cluster whose control plane runs as a pod in a host-cluster namespace; its workloads are synced onto host nodes under translated names
- **C3 / host-provided identity**: the chosen identity model — the host Orbit installation's SPIRE attests environment workloads and mints path-scoped `devenv/{id}/…` SVIDs; environments run no SPIRE
- **Pinned decision**: a design constraint discovered by live debugging during the prototype, listed in this PRD, that the production implementation must preserve and test
- **Parity gate**: the evidence bar (fidelity matrix + one quarter of real usage) that must pass before Tier 2 is deprecated
- **Tier 1 / Tier 2**: the two local development tiers — Docker Compose inner loop / k3d full-stack cluster

---

**Approval Signatures:**

- [ ] Product Owner: _________________ Date: _______
- [ ] Technical Lead: _________________ Date: _______
- [ ] Security Review: _________________ Date: _______
