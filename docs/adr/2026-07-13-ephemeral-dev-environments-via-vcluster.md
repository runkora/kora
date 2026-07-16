# Ephemeral Development Environments via vcluster — Production Delivery

**Status:** Proposed
**Date:** 2026-07-13
**Deciders:** Engineering Team
**Technical Story:** [Ephemeral Development Environments — Production Delivery PRD](../prd/ephemeral-dev-environments.md). This ADR is the technical design for delivering the dev-environments feature to production on the cloud host cluster.

---

**IMPORTANT NOTE:** Any reference to prototype code and design can be found on the git branch `feat/vcluster`

## Context

The 2026-07-07 ADR proposed the architecture; a deliberately vibe-coded prototype then **proved it end-to-end on k3d**. A developer presses *New dev environment* in the Orbit UI and gets a complete, isolated Orbit installation — its own vcluster, operator, charts, in-vcluster Istio, host-provided SPIFFE identity, database, migrations, fixtures, and browser login — reachable at a path-prefixed URL, connectable with a real unmodified `kubectl` over a brokered short-lived kubeconfig, sleeping when idle (~97% measured CPU reduction), and expiring on a TTL with nothing orphaned. Every architectural risk the original ADR flagged was spiked, root-caused live, or fixed: host-SPIRE attestation of vcluster-synced pods, in-vcluster Istio XDS authentication, kubeconfig brokering over SNI passthrough, shared-host path ingress, reconciler-implemented sleep.

**The architecture is therefore not in question.** What is in question — and what this ADR decides — is how the prototype becomes a production feature. The prototype cannot ship as-is, for five reasons established in the [PRD](../prd/ephemeral-dev-environments.md):

1. **k3d-hardwired.** `apiServer.externalAddress: "localhost:16443"` is a checked-in default (`backend/internal/devenvironments/config/base.yaml`); the operator defaults `DevEnvAppExternalURL` to `https://orbit.local.clearroute.io:8443` (`backend/cmd/operator/config/config.go`); images come from `orbit-registry:5001`; a CoreDNS split-horizon mirror (`devenvironment_platform.go`) runs unconditionally; the SNI domain defaults to `dev.orbit.local`.
2. **Governance gaps.** No audit events exist anywhere in the feature — the "audit trail" is a soft-deleted Postgres row. The nested-provisioning guard lives only in the BFF and UI (`frontend/src/app/api/dev-environments/route.ts`); `internal/devenvironments.Service.CreateEnvironment` has no signal that it is running inside a dev environment. Scope enforcement of `/extend`, `/wake`, and `/kubeconfig` relies implicitly on `POST /api/v1/dev-environments` prefix matching, untested per route. Minted kubeconfigs carry `O=system:masters`.
3. **Correctness debt.** `spec.ref` is accepted and ignored — every environment installs the in-vcluster operator at `:latest` with `pullPolicy: Always` (`devenvironment_platform.go`, `inVClusterOperatorValues`). A `Ready` environment reconciles roughly every 12 seconds against an intended 5-minute requeue. The routing-cookie ingress fallback can hijack main-app traffic into a stale dev environment (stopgap: 15-minute `Max-Age` cap). Sleep has a residual race that strands synced pods. `failReconcile` surfaces transient errors as phase `Failed`.
4. **Code quality.** Quota ceilings are self-described guesses re-bumped against live runs (`devenvironment_controller.go:55-77`); the 7-day TTL cap is encoded three times in two languages; `16443`, `0.35.1`, and `orbit-devenv-` are each duplicated across packages; stale comments reference deleted files; fixture credentials are hex literals; there is no Makefile tooling and no CI integration suite.
5. **Trapped knowledge.** Roughly two dozen load-bearing, live-debugged constraints (why NetworkPolicy is disabled inside environments, why the host `istio-sidecar` ClusterSPIFFEID needs a `devenv` exclusion, why istiod's env must go through the flat `env:` key, why the JWKS is fetched by the host operator, why Istio routing objects live in `istio-system`, …) exist only as prototype-ADR prose and inline comments. A clean implementation that does not treat them as requirements re-hits every one — and most reproduce only on a live cluster.

### Background

- The chosen architecture — **A3** (vcluster per environment) + **B3** (`OrbitDevEnvironment` CRD reconciled by the operator) + **C3** (host-provided workload identity) — and its two later amendments (path-based shared-host app ingress; JWKS-mirror/JWT_RULE mesh authentication) are recorded, argued, and empirically validated in the prototype ADR on the `feat/vcluster` branch (not present in this tree). They are **re-affirmed, not re-litigated** here.
- The implementation exists and works: `backend/internal/devenvironments/` (API feature package in `cmd/identity`), `DevEnvironmentReconciler` plus eight `devenvironment_*.go` satellites in `backend/internal/operator/`, shared CRD types in `backend/internal/platform/orbitapi/`, the CRD in `charts/orbit-operator/templates/crd-orbitdevenvironment.yaml`, chart helpers (`orbit.spiffeIDBase`) in `charts/orbit/`, BFF routes and the *Dev Environments* page in `frontend/src/app/`.
- The v1 PRD decisions stand: the rollout host is the **existing staging cluster**, and post-GA the button is available to **every authenticated Orbit user** under quota/TTL guardrails.
- This feature **replaces Tier 2** (`make dev-cluster`) behind a parity gate, per [local-development-environment](2026-03-10-local-development-environment.md); Tier 1 (`make dev-up`) is untouched.

### Problem Statement

Define the production implementation and infrastructure for the dev-environments feature such that: (1) it runs on the cloud staging cluster, not k3d; (2) every prototype finding is preserved as an enforced requirement — a test, a pinned constant, or a rerunnable cluster assertion — rather than tribal knowledge; (3) the feature is governed to Orbit's own standard (scoped, quota'd, audited, attributable); and (4) the codebase meets Orbit's conventions (`make check` clean, typed config, single-sourced constants, injected loggers, sentinel errors).

### Goals

- Ship to the cloud staging host: button-press provisioning at a selectable, actually-honoured version, ≤ 5 minutes p90 to `Ready` (measured on cloud, with image pre-pull)
- Zero re-discovery: each pinned prototype finding carries a test or assertion script; PRs touching those areas cite the pinned decision they preserve
- Full governance: per-route scope enforcement with explicit tests, per-user quota, backend-enforced nested-provisioning guard, audit events for **every** lifecycle action including reconciler-initiated ones
- Correctness: `spec.ref` honoured; steady-state reconcile at its computed interval, not a 12s spin; sleep with no stranded pods; transient errors never flap phase to `Failed`; main-app traffic can never be routed into a dev environment
- Retire Tier 2 behind the parity gate, exactly as previously committed

### Non-Goals

- Re-deciding the isolation model, provisioning trigger, identity topology, ingress scheme, or sleep mechanism — settled and validated (prototype ADR)
- PR/CI-triggered preview environments (the `spec.ref` design deliberately makes this a small follow-up, not a redesign)
- Customer-facing or multi-tenant dev environments; offline full-stack development after Tier 2 retirement
- Adopting vcluster Enterprise features (native sleep, auto-wake) — sleep remains reconciler-implemented
- Fixing pre-existing non-devenv gaps the prototype surfaced (`orbit-tool-gateway` missing from `REDEPLOY_PLATFORM_SERVICES`; `orbit-opa-gateway` health flapping) — tracked separately

---

## Decision Drivers

* **The findings are the asset.** The prototype's dollar value is ~two dozen live-debugged constraints; the delivery approach must maximise their survival into tested code
* **The feature must be governed to be credible.** Orbit's product thesis is governed, attributed, policy-gated provisioning — a dev-environments feature with no audit trail and a server-side authz hole contradicts the product it ships inside
* **Cloud ≠ k3d.** Managed API servers, LB TCP listeners, real DNS/TLS, cloud registries, and CNI timing all differ; every k3d assumption must become explicit config or be verified on staging
* **One full-stack mechanism.** Tier 2 retirement is a goal, not a side effect — whatever ships must carry the full Tier 2 validation load so the duplication can actually be deleted
* **Version determinism.** `:latest` + `pullPolicy: Always` makes environments irreproducible and makes `spec.ref` a lie; immutable refs are a precondition for preview environments and for debugging anything
* **Orbit conventions are enforced mechanically** (`make check`); the prototype predates several rules it now violates

---

## Considered Options

The five genuinely open decisions. (Isolation, trigger, identity, ingress, and sleep options are in the prototype ADR on the `feat/vcluster` branch (not present in this tree) and are not repeated.)

### A. Productionisation strategy

#### Option A1: Ship the prototype as-is, refactor later

**Pros:** immediate availability; zero rework risk.

**Cons:** the gaps are individually shipping-blockers (no audit, server-side nested-provisioning hole, traffic-hijack vector, unpinned vcluster licensing); "later" loses the one moment when every finding is fresh enough to encode as a test; more code accretes against the sloppy shapes weekly.

#### Option A2: Greenfield rewrite against the PRD

**Pros:** cleanest conceivable codebase; no prototype idioms survive.

**Cons:** maximises the re-discovery risk this delivery exists to avoid — most pinned findings (SDS selector collisions, Helm skip-if-at-version, syncer/PKI behaviours, Istio chart quirks) only reproduce on a live cluster, and a rewrite discards the exact code shapes that were verified against those behaviours; the prototype's structure (feature package + reconciler + satellites) already matches Orbit's conventions in outline.

#### Option A3: In-place hardening — tests first, then refactor, then cloud *(chosen)*

**Description:** Keep the working code as the substrate. Convert every pinned finding into a unit/integration test or rerunnable cluster assertion **before** refactoring the code it protects; then burn down the debt register file-by-file (constants, config, conventions, correctness fixes); then do cloud enablement as configuration plus a verification pass.

**Pros:** findings are locked in before anything moves; each refactor is regression-guarded; the live-verified behaviour is the fixture, not the casualty; incremental and shippable per phase.

**Cons:** sloppy idioms persist until their file's turn; discipline required not to "improve" untested areas first.

### B. `spec.ref` resolution shape

#### Option B1: Full chart + image ref resolution

**Description:** CI publishes versioned charts per ref; the reconciler resolves `spec.ref` to a chart version and image tags independently.

**Pros:** covers chart-schema changes without an operator rebuild; maximal flexibility.

**Cons:** a chart registry/publishing pipeline that does not exist today; two artifacts to resolve and keep coherent per ref; solves a problem the operator-image design already solves (below).

#### Option B2: Image-tag resolution; charts travel with the operator image *(chosen)*

**Description:** `spec.ref` resolves to a single image tag applied uniformly: the **in-vcluster operator image at that tag** (which bakes the charts, CRDs, and migrations of that ref — `Dockerfile.operator` already does this) and the Orbit service images at that tag. Chart changes at a ref are covered *because the charts ship inside the operator image of that ref* — no separate chart publishing needed.

**Pros:** one artifact family, one resolution step; the existing "charts baked into the operator image" design (prototype Phase 2, second slice) becomes the versioning mechanism instead of an accident; preview environments become "set `spec.ref` to the branch tag".

**Cons:** the *host* operator's devenv reconciler code is still the host's version — a `spec.ref` cannot change how the reconciler itself behaves (acceptable: the reconciler is host machinery, like the hypervisor under a VM); testing a change to the reconciler still means redeploying the host operator.

**Revision (post-implementation):** the symbolic default `main` was originally going to resolve *once, at first provision*, to the newest immutable `main-{shortsha}` CI tag (a registry list call), pinning each environment to whatever main happened to be at creation — with a dedicated CI tag scheme (`main-{shortsha}`/`br-{slug}-{shortsha}`) to support it. That turned out to be solving the wrong problem: a dev environment should just match whatever version of Orbit is *actually running*, not resolve independently to "the newest thing CI happens to have published," which can silently diverge from the host. `main` now resolves to `SelfImageTag` — the tag the HOST `orbit-operator` is itself deployed at, threaded in directly from the chart rather than looked up in a registry. This needs no CI tag-scheme change at all: `.github/workflows/docker.yml` is untouched, and whatever tag the deploy pipeline gives the host operator is exactly what a dev environment gets. Explicit immutable tags (e.g. a branch build, `v1.2.3`) still work verbatim for anyone who wants a *different* pin, via `spec.ref`. See §3 below.

#### Option B3: Leave `spec.ref` ignored (status quo)

Rejected outright: environments are irreproducible, `pullPolicy: Always` re-pulls on every restart, the field is API surface that lies, and preview environments are foreclosed.

### C. Audit emission mechanism

The platform event spine (`internal/events`) is an empty stub; the PRD requires attributed events for every lifecycle action anyway.

#### Option C1: Wait for the event spine

Rejected: blocks GA on an unscheduled platform feature; the PRD explicitly forbids silently skipping audit.

#### Option C2: Operator emits via the identity API

**Description:** the reconciler POSTs audit events to `cmd/identity` through a typed client.

**Pros:** single write path in the identity service.

**Cons:** couples the operator to the control-plane app's availability and auth (the operator would need a platform credential — a new identity class for no other purpose); reconciler-initiated events (auto-sleep, expiry) would be lost whenever the API is down, precisely when audit matters.

#### Option C3: Audit table in the control-plane database; the identity service is the single writer *(chosen)*

**Description:** a `dev_environment_audit_events` table owned by `internal/devenvironments`. API-initiated actions (create/extend/wake/kubeconfig/destroy) are recorded synchronously in the service layer with the acting principal. Reconciler-initiated transitions (auto-sleep, expiry, phase changes) are captured by a small ingester in the identity service **watching the `OrbitDevEnvironment` CRD** — the identity service already holds CRD-scoped RBAC; this adds `list`/`watch` verbs and nothing else — recorded with actor `system:operator`. Idempotency via a uniqueness key on `(environment_id, action, transition_time)`.

**Pros:** the operator stays Kubernetes-only (no HTTP dependency, no platform credential); one writer, one table, one query surface for the admin inventory; the CR is already the operator→platform handoff vehicle everywhere else in this design; when the event spine lands, emission moves there and the table becomes its projection.

**Cons:** watch-derived events are eventually consistent and attribution is coarse (`system:operator`, not "which reconcile"); a gap while the identity service is down is backfilled only for transitions still observable from current CR state (deletions during downtime are inferred from registry rows whose CR is gone — the existing self-heal path, now also emitting `expired`).

### D. Unprefixed app-request routing

#### Option D1: Keep the routing-cookie fallback, scoped and capped

Rejected as an end state: any header-matched route outranks the main app's unconditional catch-all (Istio hoists conditional matches), so *every* cookie-fallback variant keeps some hijack window in which a plain visit to the main Orbit app lands in a dev environment. The 15-minute `Max-Age` stopgap (already shipped) bounds it but cannot close it.

#### Option D2: App-level runtime prefix awareness; delete the general cookie fallback *(chosen — re-affirms the prototype's 2026-07-13 amendment)*

**Description:** the gateway already strips `/{env-id}` and sets `X-Forwarded-Prefix`; the frontend reads it at runtime (`frontend/src/lib/routing/devenv-prefix.ts`, already used by the login round trip) and threads the prefix through **all** internal navigation, link construction, and BFF fetches — not Next.js `basePath`, which is build-time and ruled out by the shared-image constraint. Once an audited sweep confirms every unprefixed emission point is fixed, the general-purpose `devEnvAppCookieRoute` is **deleted**. At most a narrowly `uri`-scoped `/_next/*` asset fallback remains if asset URLs resist prefixing.

**Pros:** closes the hijack structurally; browser URLs keep `/{env-id}` through navigation (kills the one-environment-per-browser-profile limitation); deep links work.

**Cons:** real frontend work across every `AppLink`/`router.push`/`fetch` call site; the fallback must not be deleted before the sweep completes (a premature removal 404s everything still unprefixed, with nothing left to catch it — verified by inspection during the prototype's partial implementation).

### E. Kubeconfig certificate subject

#### Option E1: `O=system:masters` (status quo)

**Pros:** works with zero in-vcluster setup (built-in binding).

**Cons:** `system:masters` bypasses RBAC entirely inside the vcluster — unnameable, unauditable, and irrevocable short of CA rotation; it also normalises a group Kubernetes itself warns against ever minting.

#### Option E2: Dedicated group `orbit:devenv-admin` bound to cluster-admin inside the vcluster *(chosen)*

**Description:** the reconciler creates, at provision time, an in-vcluster `ClusterRoleBinding` from group `orbit:devenv-admin` to `cluster-admin`; `generateKubeconfigCSR` (`internal/devenvironments/service.go`) sets `O=orbit:devenv-admin` (CN stays the owner subject). Same effective power — the developer is deliberately cluster-admin inside their environment — but the access is a nameable, revocable RBAC object rather than an authz bypass.

---

## Decision

**Chosen options:** A3 (in-place hardening, tests first) + B2 (image-tag `spec.ref`, charts via the operator image) + C3 (audit table, identity-service writer with CRD watch) + D2 (runtime prefix awareness, cookie fallback deleted) + E2 (`orbit:devenv-admin` group), on top of the re-affirmed prototype architecture.

### 1. Architecture (re-affirmed, restated for reference)

```
Developer clicks "New dev environment" (or an admin force-destroys) in the Orbit UI
  → BFF route (frontend/src/app/api/dev-environments/**)          session cookie → platform JWT
  → identity API  /api/v1/dev-environments/**                      (scopes: devenvironments:read|write|admin,
      quota check · nested-provisioning guard · registry row ·      enforced per route)
      audit event · OrbitDevEnvironment CR (spec.ref resolved)
  → operator DevEnvironmentReconciler (backend/internal/operator/devenvironment_*.go):
      namespace orbit-devenv-{id} (quota/limits/labels, node-pool tolerations)
      → host ClusterSPIFFEIDs (devenv/{id}/system/{component} + per-pod catch-all)
      → vcluster (OSS image pinned, k8s distro, toHost SA+netpol sync, extraSANs at first provision)
      → in-vcluster: orbit-operator chart @ spec.ref → OrbitPlatform (spire.enabled=false,
        meshJWTValidation, devEnvironmentID) + seeded OrbitTenant + fixtures (Postgres/Redis/Dex/glauth)
        + migration Job (operator image @ spec.ref)
      → ingress: aggregated VirtualService on orbit-gateway (app, path-prefixed; Dex routes first)
        + SNI TLS-passthrough on :16443 (kubectl) + per-env ServiceEntries in istio-system
      → TTL / idle-sleep / wake / expiry;  status: phase · conditions · appURL · ingressHost · expiresAt
  → UI polls; identity-service CRD watch ingests reconciler-initiated transitions into the audit table
```

Workload identity is host-provided (C3): host SPIRE agents attest synced pods natively and mint path-scoped SVIDs under `spiffe://{trust-domain}/devenv/{id}/…`; environments run no SPIRE. Access is brokered: kubeconfigs are minted via the CSR-on-CR flow, signed by the operator against the vcluster's own CA; the private key never leaves the identity service and never touches the CR.

### 2. Pinned prototype findings — normative constraints with verification obligations

These were each discovered by live debugging (full narratives in the prototype ADR's progress log) and are **requirements, not implementation history**. Each MUST be preserved and MUST carry the listed verification. A PR that touches an enforcement site MUST cite the pinned decision it preserves. The numbering matches the PRD's *Pinned Design Decisions* section.

| # | Constraint (one line — see prototype ADR for the why) | Enforcement site | Verification |
|---|---|---|---|
| 1 | vcluster values: `distro.k8s.enabled: true` explicit; `sync.toHost.serviceAccounts` + `sync.toHost.networkPolicies` true; `sync.fromHost.csiDrivers` enabled | `ensureVCluster`, `devenvironment_controller.go` | unit test on rendered values; spike scripts on version bumps |
| 2 | `controlPlane.proxy.extraSANs` carries the kubectl SNI host at **first provision**; PKI persists on the PVC — recovery is always delete-and-re-provision | `ensureVCluster` | unit test; runbook entry |
| 3 | Kubeconfig secret is `vc-{release}`; its `server:` rewritten to `https://{release}.{namespace}` for in-cluster use | `VClusterRestConfig`, `devenvironment_platform.go` | unit test |
| 4 | Two **disjoint** host ClusterSPIFFEIDs per env: `devenv/{id}/system/{component}` (component label Exists) + per-pod catch-all (label DoesNotExist) — overlapping selectors give nondeterministic SVIDs via SDS `Identities[0]` | `devEnvClusterSPIFFEIDSpecs`, `devenvironment_identity.go` | unit test asserting selector disjointness; live assertion: one SPIRE entry per pod UID |
| 5 | Host-wide `istio-sidecar` ClusterSPIFFEID excludes `orbit.io/role: devenv` namespaces | `charts/orbit-platform/templates/clusterSpiffeIDs.yaml` | `helm template` test |
| 6 | `principalFromSpiffeID` strips an optional `devenv/{id}` prefix into claims; a devenv path never parses to a bare `system/…` principal; no rego changes (policies match structured fields, never raw paths) | `backend/internal/identity/principal_extractor.go` | existing table tests, retained |
| 7 | DestinationRule SANs / AuthorizationPolicy principals render via `orbit.spiffeIDBase` helpers, appending `/devenv/{id}` only when `platform.spiffe.devEnvID` is set — one optional prefix shipped everywhere, never per-env values files | `charts/orbit/templates/_helpers.tpl`; `reconcileOrbit` | `helm template` both ways (default byte-identical) |
| 8 | `ENABLE_NATIVE_SIDECARS: "false"` pinned via the istiod chart's **flat `env:` key** (`pilot.env.*` is not read for istiod's own pod — a false confirmation made twice) | `reconcileIstiod`, `platform_controller.go` | unit test on values; live assertion on istiod pod env |
| 9 | In-vcluster istiod XDS auth is `JWT_RULE` (host issuer/JWKS, `audiences: ["istio-ca"]` — mandatory, empty audiences rejects everything), host operator fetches issuer+JWKS itself via `rest.HTTPClientFor` (never the Discovery client), hands them down via the `OrbitPlatform` CR; credential-less loopback `jwks-mirror` sidecar serves them; JWT_RULE JSON must be YAML-escaped (`helmDoubleQuotedScalarEscape`). The multicluster remote-secret approach is a **structural dead end** in stock Istio — never re-attempt | `devenvironment_jwks.go`; `reconcileIstiod` | unit tests; live assertion: XDS connects with zero `KubeJWTAuthenticator` errors |
| 10 | Host istiod sets `meshConfig.discoverySelectors` excluding devenv namespaces; consequences carried deliberately: per-env Istio routing objects live in `istio-system`, destinations registered via `ServiceEntry` (`MESH_EXTERNAL`, `resolution: DNS`), translated-name CA-root ConfigMap mirrored to the literal `istio-ca-root-cert` | `reconcileIstiod`; `devenvironment_{ingress,app_ingress,identity}.go` | unit tests; live mesh assertion |
| 11 | Never create a second Istio `Gateway` claiming a host+port another Gateway serves on the same workload (Istio silently drops one). App routes attach to `orbit-gateway`; only kubectl gets its own Gateway (port 16443 is exclusively its own) | `devenvironment_app_ingress.go` (incl. legacy-gateway cleanup) | unit test; documented lesson |
| 12 | All app path-routes in **one aggregated VirtualService** rebuilt from the full CR list per reconcile (cross-VS merge order on one host is undefined); per-env Dex routes (`/{id}/dex`, rewrite preserving `/dex`) ordered before the env's general prefix route | `devenvironment_app_ingress.go` | `TestReconcile_AppRoutesAggregateAcrossEnvironments` (exists, retained) |
| 13 | In-vcluster installs override host-topology values explicitly (`gateway.tls.enabled=false`; `frontendHost` = shared host) — baked `deploy/local/*-values.yaml` defaults are never safe to inherit verbatim | `applyInVClusterOrbitPlatform` | unit test on the CR values |
| 14 | The unscoped routing cookie is a hijack vector; the general fallback is removed only after every unprefixed emission point is fixed (Decision D2) | `devenvironment_app_ingress.go`; frontend routing | integration test: plain visit to the main app is never devenv-routed |
| 15 | `spire.enabled=false` gates the `orbit-platform` chart's ClusterSPIFFEID templates; `platform.spiffe.enabled` means "workloads carry SPIFFE identities" (true in devenvs), distinct from "SPIRE is installed here" | `clusterSpiffeIDs.yaml`; `reconcileOrbitPlatform` | `helm template` both ways |
| 16 | The reconciler creates in-vcluster `orbit-system` before the nested operator exists; it MUST carry `istio-injection: enabled` and `spiffe.io/spire-managed: "true"` at creation | `ensureInVClusterSecrets`, `devenvironment_fixtures.go` | unit test on namespace labels |
| 17 | NetworkPolicy enforcement is **disabled inside** devenv installs (`networkPolicy.enabled=false`): enforcement on synced pods proved unreliable regardless of rule shape; the host-side `default-deny-all` fence isolates the namespace. Revisit only with a CNI change, deliberately | `reconcileOrbitPlatform` | unit test; host-fence integration assertion |
| 18 | Fixtures: Postgres/Redis/Dex + **glauth** (LDAP — Dex's static-password DB has no groups claim; without groups the seeded user resolves zero scopes). Dex issuer = `{appExternalURL}/{id}/dex`, byte-identical between Dex config and the seeded tenant. Migrations run as a Job using the operator's own image; environments never re-migrate, they re-provision | `devenvironment_fixtures.go`; `devenvironment_migrations.go` | fixture unit tests; live browser-login gate |
| 19 | `LimitRange` defaults must accommodate istiod's 2Gi request (admission rejects limit < request synchronously); quota ceilings account **declared limits** — every injected sidecar declares 2 CPU/1Gi; "stuck Pending inside the vcluster" (invisible on the host) is the quota-exhaustion signature | quota/limit constants → config (§7) | sizing from measured footprint; runbook entry |
| 20 | Local (baked) charts always upgrade: the skip-if-at-version optimisation keys on the requested `chartVersion` parameter (empty for local charts), never the loaded chart's own metadata version. Values-only changes to version-pinned remote releases do not reapply without a version bump — documented operational gap | `helm.go` `InstallOrUpgrade` | unit test on the skip predicate |
| 21 | Every CRD status field must be in the structural schema (pruning silently drops unknown fields). Kubeconfig re-signing keys on the CSR fingerprint, never on certificate presence | `crd-orbitdevenvironment.yaml`; `devenvironment_csr.go` | schema-completeness test (§5); `TestReconcileKubeconfigCSR_ReSignsWhenSpecCSRChanges` |
| 22 | Operator sized ≥1Gi memory (four serial in-process Helm installs per provision); RBAC stays minimal — no TokenReview grant (the JWKS design needs none; the prototype's escalations were fully reverted) | `charts/orbit-operator/` values + clusterrole | RBAC diff review; chart test |
| 23 | Sleep scales in-vcluster Deployments/StatefulSets via the vcluster API **and waits for propagation** before killing the control plane (the syncer needs it alive to delete synced pods); `spec.awake` is the single source of truth — idle-sleep flips it itself; activity signals (kubeconfig mint, wake, UI actions) actually update `lastActivityAt` | `reconcilePowerState` / `scaleInVClusterWorkloads` | unit tests incl. the propagation wait (§5); live no-strays assertion |

The Phase 1 spike scripts (`scripts/cluster/spike-*.sh`) are retained permanently as the **regression suite for vcluster and Istio version bumps** — pins are upgraded deliberately, never floated, and the scripts re-run on every bump.

### 3. Version selection — `spec.ref` (Decision B2, revised)

- **No separate CI obligation.** `.github/workflows/docker.yml` is unchanged from before this feature — main-branch pushes publish `${GITHUB_SHA}` as always. Dev environments don't depend on any particular CI tag scheme at all.
- **Resolution:** `spec.ref` accepts either an immutable tag verbatim (e.g. a branch build's tag, or a real release tag) or the symbolic default `main`. The reconciler resolves `main` (or an unset `spec.ref`) to `SelfImageTag` — the tag the HOST `orbit-operator` container is itself running at, read from `ORBIT_DEVENV_SELF_IMAGE_TAG` (`cmd/operator/config`'s `DevEnvSelfImageTag`) — and records it as `status.resolvedRef`, purely so later reconciles reuse the same value without re-deriving it. An immutable tag given explicitly is stored and used verbatim instead, pinning that environment to a *different* build than the host.
- **Threading `SelfImageTag`:** `charts/orbit-operator/templates/deployment.yaml` sets `ORBIT_DEVENV_SELF_IMAGE_TAG` unconditionally from `{{ include "orbit-operator.imageTag" . }}` — the exact same helper expression that renders the container's own `image:` line — so the value the operator process reads can never drift out of sync with what it's actually running as. No registry lookup, no separate publishing step.
- **Application:** `inVClusterOperatorValues` sets the in-vcluster operator image to `{registry}/orbit-operator:{resolvedRef}` with `pullPolicy: IfNotPresent` (never `:latest`+`Always`) — safe because `resolvedRef` names one specific, immutable build, the same one the host is running; the migration Job (`inVClusterOperatorImage()`) uses the same tag, so applied migrations always match the installed ref; `applyInVClusterOrbitPlatform` threads the ref into the `OrbitPlatform` values so service images resolve at the same tag. Charts and CRDs at the ref come for free — they are baked into the operator image at that ref (existing `Dockerfile.operator` design, now load-bearing).
- **Iteration story unchanged:** developers with a kubeconfig remain cluster-admin inside the vcluster and can `helm upgrade`/`kubectl set image` freely; `spec.ref` selects the starting point.
- The identity API validates ref syntax (`ErrValidation` on garbage) and stores it on the registry row (already a column); the UI's provision modal offers "latest `main`" (meaning: whatever the host is running) plus a free-text tag field for anyone who wants to pin to something else.

### 4. Governance

**Audit (Decision C3).** New migration `dev_environment_audit_events` (`id BIGSERIAL`, `environment_id TEXT`, `action TEXT`, `actor TEXT`, `actor_type TEXT CHECK (actor_type IN ('user','system'))`, `occurred_at TIMESTAMPTZ`, `details JSONB`, unique `(environment_id, action, occurred_at)`). Actions: `created`, `provision_ready`, `provision_failed`, `extended`, `woken`, `kubeconfig_issued`, `auto_slept`, `expired`, `destroyed`, `force_destroyed`. API-side events are written in the same service call that performs the action, attributed to the verified token subject. Reconciler-side events come from a CRD watch ingester in `internal/devenvironments` (identity ClusterRole gains `list`,`watch` on `orbitdevenvironments` — still no status subresource write, still single-CRD): phase transitions to `Sleeping` with `spec.awake=false` → `auto_slept`; CR deletion without a prior API `destroyed` → `expired`; phase `Ready` first reached → `provision_ready`. Actor for watch-derived events is `system:operator`. The existing read-path self-heal (`mergeList`/`GetEnvironment` marking rows whose CR is gone) also emits `expired` as backfill. When the platform event spine ships, emission moves onto it; the table remains as the queryable projection.

**Nested-provisioning guard, backend-enforced.** `applyInVClusterOrbitPlatform` already sets `frontend.config.devEnvironmentId`; it now also threads the same value into the identity service's config (`charts/orbit/templates/identity/` templates `ORBIT_DEV_ENVIRONMENT_ID` into the deployment env; `internal/devenvironments/config.go` gains `DevEnvironmentID string`). `CreateEnvironment` refuses with a new sentinel `ErrNestedProvisioning` → `403 devenvironments_nested_forbidden` whenever it is set — before quota, before any write. The BFF and UI guards are retained as UX (they explain *why*), but the backend check is the enforcement. Covered by a service unit test and an integration assertion from inside a live environment.

**Explicit per-route scopes.** `devEnvironmentRoutes()` (`cmd/identity/main.go`) enumerates every route explicitly — including `POST …/{id}/extend`, `POST …/{id}/wake`, `POST …/{id}/kubeconfig` — instead of relying on the `POST /api/v1/dev-environments` prefix match. Scope mapping: `read` → list/get; `write` → create/extend/wake/kubeconfig/destroy; `admin` → `GET /all` **and** admin force-destroy. Every route gets a with-scope/without-scope test pair in the integration suite; the policy dashboard vocabulary is unchanged.

**Admin inventory and force-destroy.** `devenvironments:admin` gains a real operational path: `GET /api/v1/dev-environments/all` (exists) plus `DELETE /api/v1/dev-environments/{id}` honouring admin scope as an owner-check bypass (`force_destroyed` audit action records both admin and owner). The UI's admin view lists all environments (owner, phase, age, expiry) and offers force-destroy behind a confirm. Owner-strictness for all other single-resource actions is retained.

**Quota.** `maxActivePerOwner` stays (default 2, env-overridable); capacity planning uses the **measured** per-environment footprint (prototype observation: ~20 pods, ~3.5 CPU / 6 Gi requested, ~38 CPU / 27 Gi declared limits) rather than the prototype's guessed ceilings, and the namespace ResourceQuota/LimitRange values move from constants to operator config (§7) with the rationale documented against the measurement.

### 5. Correctness fixes

**Steady-state reconcile loop.** Root cause of the ~12s spin: every reconcile unconditionally patches status (`lastReconciledAt` always changes), each status write raises a watch event on the object, which re-enqueues it. Fixes: (a) status is patched only when a semantic field actually changed (deep-compare guard); (b) `status.lastReconciledAt` is dropped — reconcile liveness is a metric, not an API field; (c) the controller's own watch uses `GenerationChangedPredicate` so self-inflicted status writes never re-enqueue; time-driven behaviour (TTL, idle, expiry) rides `RequeueAfter` exclusively, via the existing `nextRequeueAfter` math. Success metric: an unchanged `Ready` environment reconciles no more often than its computed requeue interval, asserted in the integration suite from operator logs/metrics.

**Sleep propagation race.** `reconcilePowerState` scales in-vcluster workloads to zero, then **waits for the host-visible synced pods (control plane excluded) to drain** — checked per reconcile with a bounded overall deadline (requeue, never a blocking sleep) — before scaling the vcluster StatefulSet to zero. If the deadline expires with stragglers, sleep proceeds and the stragglers are logged and re-reconciled on wake (their in-vcluster owner references make the syncer clean them up). Unit-tested with the fake clock; integration-asserted: post-sleep host namespace contains only PVCs and the scaled-to-zero StatefulSet.

**Transient errors never mean `Failed`.** `failReconcile` currently stamps `phase: Failed` on any error, including the documented self-healing races (vcluster kubeconfig Secret not yet written, Helm busy). Fix: errors keep the current phase and set a `Progressing=False`/`reason` condition plus a consecutive-failure counter in memory; `Failed` is reserved for non-retryable causes (invalid spec, unresolvable `spec.ref`) or the same stage failing beyond a threshold. The UI shows condition detail either way.

**Kubeconfig wait.** The 500 ms poll loop in `Service.Kubeconfig` is replaced with a bounded single-object watch (field selector on `metadata.name`) with the same 20 s budget — same semantics, no parked polling thread. The 503 response now carries `Retry-After`; the BFF timeout (25 s) remains strictly above the server budget, with both values cross-referenced in config comments and a test asserting the ordering at build time (values exported to the BFF via its env).

**CRD hygiene.** The `EXPIRESAT` printer column is fixed (renders a timestamp, not `<invalid>`); spec defaulting is verified so `ttl`/`idleTimeout`/`awake` surface in `spec` on read (CRD `default:` markers exist — the Go types' `omitempty`/zero-value handling is corrected to match); the duplicated kubebuilder printer-column markers in `orbitapi/devenvironment_types.go` are **deleted** — the chart YAML is the single source of truth — and a schema-completeness unit test reflects over the `Status` struct's JSON tags and asserts each appears in the chart CRD's structural schema, preventing the field-pruning bug class (pinned decision 21) permanently. The unused `DevEnvPhaseExpired` constant is removed (expiry deletes the CR; the phase can never be observed).

### 6. App prefix awareness and cookie-fallback removal (Decision D2)

Delivery order is load-bearing:

1. **Sweep**: audit every unprefixed URL emission point — `AppLink`/`router.push`/`router.replace` call sites, BFF `fetch` targets, OIDC `redirect_uri` construction, RSC navigation — building on the shipped login-path work (`frontend/src/lib/routing/devenv-prefix.ts`, `proxy.ts`, `login/page.tsx`).
2. **Thread**: a runtime prefix context (from `X-Forwarded-Prefix`, server-read, threaded to client code via the root layout) becomes the single base for all internal URL construction. Next.js `basePath` remains ruled out (build-time; violates the shared-image constraint, prototype amendment 2026-07-10).
3. **Verify**: two concurrent environments on staging, full click-through (login, navigation, deep links, BFF calls) with the fallback still present but **unused** (gateway access logs show zero cookie-routed page requests).
4. **Delete**: remove `devEnvAppCookieRoute` from the aggregated VirtualService. Retain at most a `uri`-scoped `/_next/*` asset fallback if assets resist prefixing; it can never outrank a user-navigable route.
5. **Regression-guard**: integration test asserting a cookie-less and a stale-cookie request to `/` both reach the main Orbit app, never a dev environment.

Side effects banked: browser URLs keep `/{env-id}` through navigation and login; the one-environment-per-browser-profile limitation and the deep-link caveat both dissolve; the `/api/dev-environment/exit` cookie-clearing route is deleted with the cookie.

### 7. Configuration surface and constants consolidation

All hardcoded values become typed config (operator: chart values → env vars → `config.Load`; identity: `config.LoadService[T]` + env overrides — never `APP_ENV` YAML files for in-cluster overrides, per the prototype's dead-`development.yaml` finding). Single-source constants move to `internal/platform/orbitapi` (both feature packages already import it):

| Value | Today | Production |
|---|---|---|
| 7-day TTL hard cap | 3 copies: `devEnvTTLHardCap` (operator), `ttlHardCap` (devenvironments), `TTL_HARD_CAP_HOURS` (frontend) | `orbitapi.DevEnvTTLHardCap`; frontend receives it in the list response's `limits` object; frontend constant deleted |
| kubectl passthrough port `16443` | `devEnvAPIGatewayPort` (operator) + `base.yaml` default + k3d config | `orbitapi.DevEnvAPIGatewayPort`; external host:port is config (`devEnv.apiServerExternalAddress` / `ORBIT_DEVENV_API_SERVER_EXTERNAL_ADDRESS`) with **no k3d default checked in** — deploy/local sets `localhost:16443` |
| `orbit-devenv-` namespace prefix | `devEnvNamespace()` + inline literals in `devenvironment_app_ingress.go` | `orbitapi.DevEnvNamespacePrefix`; inline literals replaced |
| App external URL | operator default `https://orbit.local.clearroute.io:8443` | `devEnv.appExternalURL` / `ORBIT_DEVENV_APP_EXTERNAL_URL`, **required** (no default); deploy/local supplies the k3d value |
| Image registry | `ORBIT_DEVENV_IMAGE_REGISTRY`, defaulting local | required config; cloud registry on staging |
| SNI ingress domain | `dev.orbit.local` default | `devEnv.ingressDomain`, required |
| vcluster version + image | `0.35.1` in `util.go` + `versions.env`; image = chart default (**observed `vcluster-pro`**) | `scripts/cluster/versions.env` stays the pin source, wired through chart values; image **explicitly pinned to the OSS distribution** (`controlPlane.statefulSet.image`), verified live as a Phase B gate (PRD P0 item 5; licensing check before rollout) |
| Quota/LimitRange ceilings | constants, "re-guessed against live runs" | `devEnv.quota.*` chart values, defaults derived from the measured footprint (§4), rationale documented |
| Node placement | none | `devEnv.nodeSelector` / `devEnv.tolerations` chart values, applied to vcluster values and namespace defaults (tainted devenv pool on staging) |
| CoreDNS split-horizon mirror | runs unconditionally (no-ops if host ConfigMap absent) | gated behind `devEnv.localDNSSplitHorizon` (default `false`); deploy/local enables it; the code path is otherwise dead on cloud |
| BFF kubeconfig timeout | `KUBECONFIG_TIMEOUT_MS=25_000` magic | derived from BFF env (`DEVENV_KUBECONFIG_TIMEOUT_MS`) documented against the backend's `signTimeoutSeconds`; ordering asserted by test |
| Fixture secrets | random per env (kept); dev-user password SHA-256 hex literal | generated material stays per-environment; the fixed seeded login (`dev@orbit.local.clearroute.io` / `password`) is retained **for the in-environment login only** and documented as such (PRD security requirement); the hash literal gains a named constant and comment |

Stale comments (the `devenvironment_multicluster.go` reference in `devenvironment_types.go:11`, the evolving quota-bump war story at `devenvironment_controller.go:55-77`, the CRD's inline pruning war story) are replaced with pointers to the prototype ADR; the history lives there, not in code.

### 8. Cloud enablement (staging host)

Numbered to match the PRD's *Cloud Deployment Requirements*; all are Phase B exit criteria:

1. **Node pool**: tainted/labelled pool for `orbit-devenv-*` workloads; reconciler applies tolerations/nodeSelector from config (§7). Single-pool fallback is ResourceQuota-only.
2. **Image pre-pull**: DaemonSet pre-puller (or baked node images) on the devenv pool for the vcluster, Istio, fixture, and current-`main` Orbit images — the ≤5-minute p90 does not survive a cold pull.
3. **Registry**: `devEnv.imageRegistry` → cloud registry; CI publishes immutable tags per §3; host-node pull credentials cover synced pods (they are ordinary host pods).
4. **App ingress**: `devEnv.appExternalURL` = the staging gateway's real host; TLS remains `orbit-gateway`'s own credential (never devenv-specific — pinned decision 11); one DNS record, no wildcard.
5. **kubectl ingress**: the Istio ingress-gateway `LoadBalancer` Service exposes TCP 16443 passthrough; `apiServerExternalAddress` = `{lb-host}:16443`; SNI hosts (`{id}.{ingressDomain}`) need no DNS records (verified property, prototype Phase 3 second slice).
6. **DNS hygiene**: split-horizon mirror gated off (§7); the shared app host resolves publicly — for browsers and, via the ordinary vcluster→node→upstream DNS chain, for the in-vcluster Dex issuer fetch.
7. **Identity plane**: host SPIRE with the SPIFFE CSI driver (already enabled in chart values — verify empirically per install, per the prototype's own correction); trust domain read from the host `OrbitPlatform`, never configured per environment.
8. **JWKS/issuer**: verify the host-operator fetch against the managed API server (`system:service-account-issuer-discovery` covers the operator's SA by default) — a checklist item, not an assumption.
9. **Capacity & ownership**: capacity plan for N concurrent environments from the measured footprint; a named availability owner for the host cluster **before** Tier 2 deprecation — post-retirement it is critical developer infrastructure.

Cloud divergences found during Phase B (storage classes for vcluster PVCs, LB behaviour, CNI timing) are appended to this ADR as amendments, continuing the prototype ADR's discipline.

### 9. Security posture

- Developer privilege is confined to the vcluster; host blast radius is one quota-capped namespace behind the host `default-deny-all` fence. Kubeconfig subject moves to `O=orbit:devenv-admin` (Decision E2) with a provision-time in-vcluster ClusterRoleBinding; the credential remains 8h, per-request-keypair, host-rejected (existing tests retained).
- Environment identities stay path-scoped (`devenv/{id}/…`) and never match host or tenant policy (pinned decisions 5/6; existing tests retained).
- Identity-service Kubernetes RBAC: CRUD + `list`/`watch` on the one CRD, no status subresource. Operator RBAC stays at its post-revert minimum (pinned decision 22).
- vcluster image pinned to the **OSS** distribution (§7); licensing verified before rollout.
- All fixture secrets generated per environment; the one fixed seeded credential is documented and scoped to the in-environment login.

### 10. Observability

- **Metrics** (operator, existing OTel discipline): `orbit_devenv_provision_stage_duration_seconds{stage}` (histogram per condition: namespace, identity, vcluster, in-vcluster apply, in-vcluster ready), `orbit_devenv_reconcile_failures_total{stage}`, `orbit_devenv_environments{phase}` gauge, wake duration. These are the data source for the p90 success metric and the hot-loop assertion.
- **Audit** per §4 — the admin inventory reads the same table.
- Controller-runtime logs continue through the injected `logging.Logger` sink (`cmd/operator/logsink.go`) — the discarded-logger bug stays fixed and is covered by a startup log assertion in the integration suite.
- The UI polls wake transitions too (Sleeping→Ready), closing the prototype's polling gap (`use-dev-environments.ts` covered Pending/Provisioning only).

### 11. Testing strategy

- **Unit** (exists, extended): reconciler paths against fake client/Helm/clock — including the new propagation-wait, status-write guard, transient-error handling, ref resolution, and every pinned decision marked "unit test" in §2. CSR signing stays pure-`crypto/x509` table tests.
- **Repository**: Postgres tests env-gated on `ORBIT_TEST_DB_URL` (exists; extended for the audit table).
- **Integration suite (new)**: `backend/tests/integration/devenvironments/` under `//go:build integration`, automating the prototype's acceptance checklist plus the production requirements — provision→Ready with per-stage conditions at a pinned ref; kubeconfig works in-vcluster and is host-rejected; sleep leaves no strays; wake restores state; TTL expiry orphan-sweep (`kubectl get all,pvc,clusterspiffeids,virtualservices,serviceentries -A | grep {id}` empty); operator-kill-mid-provision convergence; per-route scope pairs; quota; nested-guard refusal; browser login (Playwright, real Dex/glauth, scope-bearing sidebar); main-app-never-hijacked. Runs via `make test-integration` against a local k3d host now, in **scheduled CI before the parity gate is declared passed**, and re-targeted at a staging-provisioned environment after GA.
- **Version-bump regression**: the spike scripts (§2, footer) re-run on every vcluster/Istio pin change.
- **Developer tooling**: Makefile targets for the reconciler iteration loop (`redeploy-operator` + apply of `deploy/local/devenvironment.yaml` variants) and a runbook (`docs/guides/dev-environments.md`) covering: recovery is delete-and-re-provision; the values-only-change gap on pinned remote charts (pinned decision 20); the quota-exhaustion signature (pinned decision 19); reading per-stage conditions.

### The three questions

- **Who is this agent?** Every lifecycle action is now *recorded*, not just attributed: API actions carry the verified human principal, reconciler actions carry `system:operator`, and the audit table is the queryable record. Environment workloads carry real host-SPIRE SVIDs path-scoped under `devenv/{id}/…`, parseable by the platform (prefix-stripping with claims) and matchable by no host or tenant policy.
- **What can it access?** Scope gating becomes explicit and tested per route, including sub-routes; the nested-provisioning guard moves into the backend service so a dev environment can never mint further environments regardless of client; the developer's cluster-admin becomes a named, revocable group inside the vcluster; host-side access remains one quota-capped, deny-fenced namespace; the identity service's Kubernetes reach remains a single CRD.
- **How does it connect?** Unchanged in principle, hardened in practice: brokered short-lived kubeconfigs (CSR-on-CR, operator-signed, private key never persisted), one shared TLS-terminating app host with path routing that can no longer leak main-app traffic into an environment, and an SNI passthrough listener for kubectl that needs no per-environment DNS.

---

## Consequences

### Positive

- The prototype's findings become permanent, executable knowledge — tests and pinned constants instead of progress-log prose; version bumps get a regression suite instead of re-discovery
- The feature becomes trustworthy infrastructure: audited, quota'd, version-deterministic, admin-operable, and covered by a scheduled CI suite — the preconditions for retiring Tier 2 and for the dogfooding story to be credible
- `spec.ref` + immutable CI tags make environments reproducible and make PR-preview environments a small follow-up
- Idle cost and steady-state reconcile cost both drop to their intended levels (sleep without strays; no hot loop)
- The main Orbit app can never be hijacked into a dev environment; browser URLs survive navigation

### Negative

- The prefix-awareness sweep is real frontend work across every internal URL construction site, and the cookie fallback cannot be deleted until it is complete — a sequencing constraint that stretches Phase A
- CI takes on a new standing obligation (immutable per-merge image publishing) and the registry accumulates tags (lifecycle policy needed)
- The staging cluster formally becomes critical developer infrastructure with a named availability owner — an operational commitment, not just a deployment target
- The audit ingester adds a watch-based background component to the identity service — small, but a new moving part with eventual-consistency semantics to document
- In-place hardening means living with prototype idioms in files whose turn hasn't come; the debt register (PRD) is the burn-down authority, not ad-hoc judgement

### Neutral

- The prototype ADR remains the canonical narrative record; this ADR supersedes it as design-of-record and inherits its amendment discipline (cloud findings appended here)
- `deploy/local/` keeps its dual role as the fixture seed for every environment and must stay installable at all times
- The scope vocabulary (`devenvironments:read|write|admin`) is unchanged; only its enforcement becomes explicit
- Tier 2 stays alive through Phase A as the development bed for this very work — the feature still cannot bootstrap inside itself until staging hosts it

---

## Implementation

Phases per the PRD; each shippable, each with a hard exit:

- **Phase A — Hardening on k3d.** Pinned decisions → tests first (§2); then the P0 debt items: audit (§4), backend nested guard, `spec.ref` (§3), per-route scopes, prefix-awareness sweep + cookie-fallback removal (§6), hot-loop and sleep-race and transient-`Failed` fixes (§5), constants/config consolidation (§7), conventions pass. **Exit:** `make check` clean; integration suite green on k3d; live browser login verified (a release gate — the prototype closed it in code without final k3d verification).
- **Phase B — Cloud enablement.** §8 items 1–8; vcluster OSS pin verified live; CoreDNS hack gated off. **Exit:** integration suite + spike assertions green on the staging host; divergence findings appended to this ADR.
- **Phase C — Staging rollout & measurement.** Behind the scope gate; measure provision p90 (with pre-pull), wake time, sleep cost, per-environment footprint; tune quotas from measurements; scheduled CI job live. **Exit:** success-metric baselines recorded.
- **Phase D — GA & governance.** Scope opened to all human principals; admin inventory + force-destroy; expiry warnings in the UI; metrics dashboards; runbook published. **Exit:** the team uses dev environments for platform PRs.
- **Phase E — Parity gate & Tier 2 retirement.** One quarter of real usage against the parity bar (fidelity matrix — now largely proven — plus usage evidence); deprecation warning on `make dev-cluster`; then removal of the target, its scripts, `values.local.yaml` overlays, and CoreDNS/image-import workarounds; `make test-integration` retargets an ephemeral environment; [local-development-environment](2026-03-10-local-development-environment.md) amended.

### Migration Path

Unchanged from the prototype ADR's commitment: coexistence through rollout → formal deprecation at the parity gate → removal within one quarter of passing it. Work dev environments deliberately do not cover (SPIRE deployment layer, Cilium/eBPF) is re-homed to CI/staging explicitly, never silently orphaned. After retirement, developers without connectivity have Tier 1 only — accepted deliberately.

---

## Validation

### Success Metrics

- Button press → `Ready` ≤ 5 minutes p90, **measured on the cloud host** with pre-pull (k3d numbers do not count)
- Wake ≤ 60 s; sleeping environment ≤ 5% of its running CPU footprint (prototype baseline: ~7m vs ~280m CPU)
- 100% of lifecycle actions — including reconciler-initiated — emit attributed audit events
- No reconcile of an unchanged `Ready` environment more often than its computed requeue interval
- ≥ 80% of platform-layer PRs validated in a dev environment before merge, over the post-GA quarter
- `make dev-cluster` invocations trend to zero after GA; target and scripts removed within one quarter of the parity gate
- The integration suite runs in scheduled CI before the parity gate is declared passed

### Monitoring

Per-stage provision metrics, failure counters, and phase-inventory gauges (§10) on the operator; audit events and admin inventory in the control plane; gateway access logs during the cookie-fallback deprecation window (step 3 of §6) to prove the fallback is unused before deletion.

---

## Related Decisions

- `ephemeral-development-environments-via-vcluster` (prototype ADR on the `feat/vcluster` branch — not present in this tree) — the prototype this supersedes; canonical record of the spike, all ten Phase-2 slices, amendments, and corrections behind every pinned decision here
- [local-development-environment](2026-03-10-local-development-environment.md) — Tier 1 unchanged; Tier 2 retired by this delivery behind the parity gate
- [orbit-kubernetes-operator](2026-03-31-orbit-kubernetes-operator.md) — the reconcile model the `DevEnvironmentReconciler` extends
- [helm-chart-distribution-and-gitops-repository-structure](2026-03-31-helm-chart-distribution-and-gitops-repository-structure.md) — the charts and `deploy/local/` reference install reused inside each environment; the baked-charts design that §3 makes load-bearing
- [policy-gateway-and-authorisation-model](2026-04-14-policy-gateway-and-authorisation-model.md) / [platform-scope-enforcement-middleware](2026-06-04-platform-scope-enforcement-middleware.md) — scope gating made explicit per route
- [browser-auth-flow-and-bff-session-architecture](2026-04-01-browser-auth-flow-and-bff-session-architecture.md) — the BFF/session machinery the prefix-awareness work threads through
- [opentelemetry-observability-discipline](2026-03-21-opentelemetry-observability-discipline.md) — the metrics/logging discipline §10 follows

---

## References

- [Ephemeral Dev Environments — Production Delivery PRD](../prd/ephemeral-dev-environments.md) — requirements, debt register (P0/P1/P2), pinned-decision list this ADR's §2 enforces
- [vcluster documentation](https://www.vcluster.com/docs) · [OSS vs Free/Enterprise tiers](https://www.vcluster.com/docs/vcluster/introduction/oss-vs-free) (image pin + licensing check, §7/§9)
- Prototype spike scripts: `scripts/cluster/spike-*.sh` — retained as the vcluster/Istio version-bump regression suite

---

## Notes

This ADR deliberately contains no new architecture. Its substance is (1) five delivery decisions (A3/B2/C3/D2/E2), (2) the conversion of every live-debugged prototype finding into a normative, test-carrying constraint, and (3) the configuration/infrastructure surface that separates the feature from k3d. The prototype ADR is superseded as design-of-record but is **not** historical trivia — it is the evidence base, and §2's table is intentionally useless without it for anyone asking *why* a constraint exists.

**Last Updated:** 2026-07-13
