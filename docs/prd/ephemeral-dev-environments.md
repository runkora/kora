# Ephemeral Development Environments - Product Requirements Document

**Status:** Draft
**Created:** 2026-07-07
**Last Updated:** 2026-07-07
**Owner:** Francis Holt (Platform Engineering)
**Stakeholders:** Platform engineering team (primary users), operator of the shared Orbit cluster, security review

---

## Executive Summary

Developers building the Orbit platform get their full-stack test environments from a resource-heavy local k3d cluster (`make dev-cluster`) or a contended shared staging cluster — both slow them down and neither matches real deployments. This feature gives every developer a button in the Orbit UI that provisions a disposable, isolated, full Orbit installation on the same infrastructure Orbit itself runs on, ready in minutes, governed by Orbit's own identity and policy machinery. **It replaces the local k3d full-stack tier (`make dev-cluster`) as the standard way to develop the Orbit platform**, leaving only the lightweight `make dev-up` inner loop running locally.

---

## Problem Statement

### Current State

Local development follows the two-tier strategy in [local-development-environment](../adr/2026-03-10-local-development-environment.md):

- **Tier 1 (`make dev-up`)** — Postgres, Redis, and Dex in Docker Compose; Go services under hot-reload. Fast and fit for purpose. **Not changing.**
- **Tier 2 (`make dev-cluster`)** — a per-laptop k3d cluster with SPIRE, Istio, and the full chart stack for integration work. This is the tier this feature replaces.

Beyond Tier 2, the only full-stack option is the single shared staging cluster.

### User Pain Points

- **Tier 2 is heavy and slow**: 3–5 minutes to provision, a large share of laptop CPU/RAM while running, and it competes with the IDE, Docker, and coding agents on the same machine
- **Tier 2 diverges from reality**: it needs local-only workarounds (CoreDNS endpoint injection, `k3d image import`, `values.local.yaml` overlays) that must be maintained in lockstep with every chart change — a duplicated deployment configuration that regularly drifts and breaks
- **Tier 2 approximates, rather than reproduces, the platform**: no Cilium, degraded SPIRE topology, minimal Istio config — a class of integration bugs still only appears in staging
- **Staging is contended**: one engineer's broken operator build blocks everyone; destructive tests (migrations, CRD changes) require coordinating a freeze; two branches cannot deploy at once
- **Cluster-scoped work has nowhere safe to go**: CRD schema changes, operator changes, and mesh config are cluster-global — they cannot be isolated on any shared cluster, so developers either risk staging or fight their laptop

### Business Impact

- Platform-layer changes (operator, charts, CRDs, identity/mesh wiring) are validated late — install-path regressions reach staging or customers before they are caught
- Engineering time is lost to k3d cluster rebuilds, workaround maintenance, and staging coordination rather than product work
- The k3d tier is a second, parallel deployment mechanism that must be maintained forever if not replaced
- Orbit's own pitch — governed, button-press provisioning of workloads — is not exercised by the team that builds it; dogfooding this flow hardens the product

---

## Goals and Objectives

### Primary Goals

1. Any authorised developer can provision a complete, isolated Orbit environment from the Orbit UI in minutes, without touching `kubectl`, Helm, or the host cluster
2. **Replace Tier 2 (`make dev-cluster`) entirely**: once parity is demonstrated, the k3d full-stack tier and its maintenance surface are retired — one full-stack mechanism, not two
3. Environments are governed Orbit-style: scope-gated provisioning, owner attribution, audit trail, TTLs, and quotas by default
4. Environments run on the same infrastructure as Orbit itself — no separate dev-cluster fleet to build, patch, or pay for
5. Cost approaches zero when idle: environments sleep when unused and expire automatically

### Success Metrics

- Time from button press to a ready environment ≤ 5 minutes (p90)
- ≥ 80% of platform-layer PRs (operator/charts/CRDs/mesh) validated in an ephemeral environment before merge, measured over a quarter
- `make dev-cluster` invocations trend to zero after GA; the target and its supporting scripts are **removed** within one quarter of passing the parity gate
- Staging breakage incidents caused by in-development changes trend to zero
- A sleeping environment consumes only its control-plane pod and storage

### Non-Goals

- Replacing Tier 1 (`make dev-up`) — the fast, offline Go inner loop stays local and unchanged
- Preserving an offline *full-stack* development path — after Tier 2 retirement, full-stack work requires connectivity to the host cluster, deliberately
- Ephemeral environments for Orbit customers or their agents (agent sandboxes are an existing, separate capability)
- PR-triggered preview environments and CI integration (future iteration; the trigger in scope is the UI button)
- Production-hardening validation (Cilium/eBPF, cloud IAM) — remains a CI/staging concern, as it already is today

---

## User Stories

### Primary User Stories

**As a** platform developer,
**I want to** provision a full Orbit environment by pressing a button in the Orbit UI,
**So that** I can test operator, chart, CRD, and service changes against a real installation without building a local cluster or queueing for staging.

**Acceptance Criteria:**
- [ ] A *Dev Environments* page in the Orbit UI lists my environments and offers *New dev environment*
- [ ] Provisioning shows live status (Pending → Provisioning → Ready) and surfaces failures with actionable detail
- [ ] A ready environment is a complete Orbit installation (control plane, seeded tenant, fixture data) reachable at its own URL
- [ ] I can pick the version/ref to install (default: latest `main` build)

---

**As a** platform developer,
**I want to** connect to my environment with cluster-admin rights inside it,
**So that** I can deploy work-in-progress builds, break things, and iterate freely without any risk to the host cluster or other developers.

**Acceptance Criteria:**
- [ ] A *Connect* action issues me a short-lived kubeconfig (default 8h) scoped to my environment only
- [ ] I am cluster-admin inside the environment; I hold no credentials for the host cluster
- [ ] Deploying a broken operator or CRD inside my environment has no effect on the host or on other environments
- [ ] The environment's own Orbit UI/API is reachable and sign-in works against its seeded identity provider

---

**As a** platform developer,
**I want** my environment to sleep when idle, wake when I return, and expire on a TTL I can extend,
**So that** I never pay attention to cleanup and the team never pays for forgotten environments.

**Acceptance Criteria:**
- [ ] Environments sleep after 2h idle and wake on the next connection or UI action
- [ ] Default TTL 24h, extendable from the UI up to a hard cap (7 days)
- [ ] Expiry deletes the environment and everything in it; the UI warns before expiry
- [ ] I can destroy an environment immediately from the UI

---

**As a** platform administrator,
**I want** provisioning to be scope-gated, quota-limited, and fully audited,
**So that** the button is safe to expose and capacity stays under control.

**Acceptance Criteria:**
- [ ] Provision/connect/extend/destroy require `devenvironments:*` scopes enforced by the policy gateway
- [ ] Per-developer cap on active environments (default 2, policy-configurable)
- [ ] Every lifecycle action is audited with the acting principal
- [ ] Each environment is resource-quota-capped on the host; an admin view shows active/sleeping counts, age, and owners

### Secondary User Stories

**As a** developer reviewing a colleague's PR, **I want to** open their running environment, **so that** I can verify behaviour without provisioning my own. (P1 — read access via existing policy grants.)

**As a** platform administrator, **I want to** force-destroy any environment, **so that** I can reclaim capacity or remove a wedged environment. (P1.)

---

## Proposed Solution

### High-Level Approach

Each environment is a **vcluster** — a virtual Kubernetes cluster running in a single namespace of the host cluster that runs Orbit's control plane. A vcluster gives the developer a private API server (own CRDs, operator, RBAC — the exact things a shared cluster cannot isolate) while its workloads share host nodes for speed and cost. The Orbit UI button creates a declarative environment record; the existing Orbit operator reconciles it into a namespace, a vcluster, and a full Orbit installation inside — deployed through the same charts and operator path as production installs, so every provision also exercises the real install path. Workload identity is provided by the host Orbit installation's SPIRE (environment pods receive real, path-scoped SVIDs). Full technical design: [ephemeral-development-environments-via-vcluster](../adr/2026-07-07-ephemeral-development-environments-via-vcluster.md).

### User Experience

#### User Flow

1. Developer opens **Dev Environments** in the Orbit UI and clicks **New dev environment**
2. Names it, optionally picks a ref/version and size preset, confirms
3. Watches provisioning progress (minutes); the card flips to **Ready** with the environment URL and expiry
4. Clicks **Connect** for a short-lived kubeconfig; iterates — deploys branch images, runs integration tests, breaks and re-provisions at will
5. Environment sleeps when idle; developer extends the TTL if the branch lives longer, or destroys it when done; otherwise it expires and vanishes on its own

### Key Components

- **Dev Environments UI**: list/provision/status/connect/extend/destroy page, following existing frontend patterns (Terra, BFF policy projection)
- **Environments API**: new `devenvironments` feature in the main control-plane API — registry, quota enforcement, kubeconfig minting, audit
- **Operator reconciler**: turns the declarative environment record into namespace + vcluster + in-vcluster Orbit install, and enforces TTL/sleep
- **Host identity & ingress wiring**: host-SPIRE identities for environment workloads; per-environment ingress `{env-id}.dev.{domain}`

---

## Scope

### In Scope

- UI-button provisioning, status, connect (kubeconfig), extend, destroy
- Full in-environment Orbit install at a chosen ref with seeded tenant and fixture data
- Lifecycle automation: TTL, idle sleep/wake, expiry deletion, per-user quotas
- Policy scopes, audit events, and admin inventory view
- **Phased retirement of Tier 2 (`make dev-cluster`)** behind a parity gate: deprecation warning, doc updates, then removal of the target, scripts, and local-only overlays; `make test-integration` retargets an ephemeral environment

### Out of Scope

- PR/CI-triggered preview environments (future iteration)
- Customer-facing or multi-tenant use of dev environments
- Offline full-stack development after Tier 2 retirement
- SPIRE-deployment-layer and Cilium validation — explicitly re-homed to CI/staging, not silently dropped

### Future Considerations

- CI-built branch images auto-wired to an environment's ref
- Environment templates (slim vs full presets) and team-shared environments
- PR-linked preview environments with auto-teardown on merge

---

## Requirements

### Functional Requirements

**Must Have (P0):**
- [ ] Provision a complete Orbit environment from the UI; status visible end-to-end
- [ ] Short-lived, environment-scoped kubeconfig issuance; no host-cluster credentials ever reach a developer
- [ ] TTL with UI extension, idle sleep/wake, expiry deletion, immediate destroy
- [ ] `devenvironments:*` scope gating, per-user active-environment quota, full audit trail
- [ ] Environment installs a selectable ref; environments are fully isolated from the host control plane's data stores
- [ ] Parity gate definition and Tier 2 deprecation/removal plan executed as part of this feature, not deferred

**Should Have (P1):**
- [ ] Admin inventory view (counts, age, owner) and admin force-destroy
- [ ] Read access to a colleague's environment via existing policy grants
- [ ] Expiry warning notification in the UI

**Nice to Have (P2):**
- [ ] Size presets (small/standard/large)
- [ ] One-click "re-provision with same settings" after destroy

### Non-Functional Requirements

**Performance:**
- Button press → Ready ≤ 5 minutes (p90); wake from sleep ≤ 60 seconds
- Provisioning N environments concurrently must not degrade the host control plane (dedicated node pool or quota backstop)

**Security:**
- Developer privilege is confined to the virtual cluster; host-side blast radius is one quota-capped, NetworkPolicy-fenced namespace
- Environment workload identities are path-scoped and never authorised against host or tenant policies
- All access credentials are short-lived and brokered; nothing long-lived is handed out

**Reliability:**
- A half-failed provision self-heals or reports Failed with a destroy-and-retry path; wedged environments are deleted, never repaired in place
- The host cluster is treated as critical developer infrastructure with an availability owner (a host outage blocks all full-stack validation once Tier 2 is retired)

**Scalability:**
- Support at least 2 active environments per developer across the team concurrently within planned host capacity

**Observability:**
- Provision duration and failure counts per phase exported as metrics; all lifecycle actions emit structured audit events attributed to the acting principal

---

## Dependencies

### Internal Dependencies

- **Orbit Kubernetes operator** — gains the environment reconciler (exists; extended)
- **Policy gateway & scope registration** — new `devenvironments:*` scope family (exists; extended)
- **Charts and `deploy/local/` reference install** — reused as the in-environment install and fixture seed; must be kept installable at all times (exists)
- **Host SPIRE installation** — provides identity for environment workloads; requires enabling the SPIFFE CSI driver, currently disabled (change required)
- **Frontend BFF pattern & Terra** — new Dev Environments page (exists; extended)

### External Dependencies

- **vcluster** — the virtual-cluster runtime; version-pinned and upgraded deliberately like the Istio/SPIRE chart pins
- **Container registry** — hosts branch/`main` images that environments install

---

## Risks and Mitigations

| Risk | Impact | Probability | Mitigation |
|------|--------|-------------|------------|
| Istio injection or SPIFFE CSI volumes misbehave through the vcluster syncer | High | Medium | Phase 1 spike is a hard gate; documented degraded-mode fallback; Tier 2 not retired until the fidelity matrix passes |
| Parity gate never quite passes and both mechanisms live forever | Medium | Medium | Parity bar defined up front (identity exchange, operator/chart iteration, sidecar wiring); removal is a tracked success metric with a deadline |
| Host cluster capacity exhausted by environment bursts | Medium | Medium | Per-user quotas, per-namespace ResourceQuota, dedicated node pool where available, sleep + TTL defaults |
| Host cluster outage blocks all full-stack development post-retirement | High | Low | Availability ownership named before retirement; Tier 1 unaffected as partial fallback |
| Developers bypass the UI and hand-manage vclusters | Low | Low | The governed path is faster than the manual one; no host credentials are issued to make a manual path viable |

---

## Alternatives Considered

(Evaluated in depth in the [ADR](../adr/2026-07-07-ephemeral-development-environments-via-vcluster.md); summarised here.)

### Alternative 1: Keep improving the k3d tier

**Description:** Invest in `make dev-cluster` — faster scripts, better parity, prebuilt images.

**Why not chosen:**
- The structural problems are not fixable locally: laptop resources, per-machine drift, no Cilium, and duplicated deployment config that must track every chart change
- It can never offer shared, governed, attributable environments — and it forgoes the dogfooding value entirely

### Alternative 2: Namespace-per-environment on the shared cluster

**Description:** Carve environments out of the host cluster with namespaces and RBAC.

**Why not chosen:**
- Orbit development mutates cluster-scoped resources (CRDs, operator, mesh config) that namespaces cannot isolate — two environments testing different CRD versions collide immediately

### Alternative 3: A real cluster per environment

**Description:** Provision a dedicated cloud cluster (or pooled VM running k3s) per environment.

**Why not chosen:**
- 15–20+ minute provisioning, per-cluster control-plane fees, poor bin-packing of idle environments
- A second infrastructure estate to operate — contradicts "same infra as Orbit itself"

---

## Open Questions

1. **Who gets the button beyond platform engineers?** (e.g. solutions/support engineers reproducing customer issues)
   - **Status:** Decided (2026-07-07) — **everyone who opens Orbit.** The `devenvironments:write` scope is granted to all human principals by default; per-principal quota (2 active) and TTL remain the guardrails. Restrictions can be layered on later via policy without code changes — the scope gate is already in the design.
   - **Owner:** Francis Holt

2. **Where does the host cluster for dev environments live during rollout** — the existing staging cluster, or a dedicated dev host cluster?
   - **Status:** Decided (2026-07-07) — **the existing staging cluster.** This makes the ADR's capacity safeguards (dedicated node pool with taint/toleration, per-environment `ResourceQuota`, pre-pulled images) load-bearing from day one, since dev-environment bursts must not starve staging workloads. Revisit a dedicated host only if contention shows up in practice.
   - **Owner:** Francis Holt

3. **Exact parity-gate evidence bar** — fidelity matrix plus one quarter of real usage is proposed in the ADR; confirm or tighten
   - **Status:** Open
   - **Owner:** Engineering team

---

## Timeline and Milestones

### Phases

(Mirrors the ADR implementation plan; dates set at PRD approval.)

**Phase 1: Fidelity spike** — vcluster running the Orbit chart sequence with host-provided identity and in-vcluster Istio; written fidelity matrix. **Hard gate for everything below.**

**Phase 2: CRD + reconciler** — declarative environment resource, operator reconciler, TTL/sleep; provisioning via `kubectl apply` only.

**Phase 3: API + registry** — `devenvironments` feature package, scopes, quotas, kubeconfig minting.

**Phase 4: UI** — Dev Environments page; GA to the team.

**Phase 5: Tier 2 retirement** — parity gate evaluation, deprecation, then removal of `make dev-cluster` and its supporting machinery; `make test-integration` retargeted.

### Key Milestones

- [ ] PRD Approved
- [ ] ADR Accepted: [ephemeral-development-environments-via-vcluster](../adr/2026-07-07-ephemeral-development-environments-via-vcluster.md) (Proposed, 2026-07-07)
- [ ] Spike fidelity matrix published (gate)
- [ ] First environment provisioned from the UI (GA)
- [ ] Parity gate passed; Tier 2 deprecated
- [ ] `make dev-cluster` removed

---

## Appendix

### Related Documents

- [ephemeral-development-environments-via-vcluster](../adr/2026-07-07-ephemeral-development-environments-via-vcluster.md) — technical design (ADR)
- [local-development-environment](../adr/2026-03-10-local-development-environment.md) — the two-tier local strategy whose Tier 2 this replaces
- [orbit-kubernetes-operator](../adr/2026-03-31-orbit-kubernetes-operator.md) — the reconcile model the provisioning extends

### References

- [vcluster documentation](https://www.vcluster.com/docs)

### Glossary

- **Tier 1 / Tier 2:** The two local development tiers from [local-development-environment](../adr/2026-03-10-local-development-environment.md) — Docker Compose inner loop / k3d full-stack cluster respectively
- **Dev environment:** An ephemeral, per-developer full Orbit installation provisioned from the UI (distinct from `orbit-agents`' `EnvironmentConfig` agent-sandbox concept)
- **vcluster:** A certified virtual Kubernetes cluster whose control plane runs as a pod in a host-cluster namespace
- **Parity gate:** The evidence bar (fidelity matrix + real usage) that must pass before Tier 2 is deprecated

---

**Approval Signatures:**

- [ ] Product Owner: _________________ Date: _______
- [ ] Technical Lead: _________________ Date: _______
- [ ] Security Review: _________________ Date: _______
