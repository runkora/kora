# Devenv-Mode Platform Support - Product Requirements Document

**Status:** Draft
**Created:** 2026-08-03
**Last Updated:** 2026-08-05
**Owner:** Francis Holt
**Stakeholders:** Platform Engineering, `vcluster-mcp` maintainers

---

## Executive Summary

`vcluster-mcp` — a standalone, Orbit-independent MCP server and Kubernetes operator ([clear-route/vcluster-mcp](https://github.com/clear-route/vcluster-mcp)) — provisions ephemeral dev environments as vclusters (virtual Kubernetes clusters running as pods), and runs its own operator and MCP server as ordinary workloads in the **same host Kubernetes cluster as Orbit's own platform install**. It authenticates and authorizes itself independently of Orbit and is never on Orbit's request path. Orbit's obligation is to confirm and guarantee that its own cluster-scoped surfaces — mesh sidecar injection, SPIRE identity issuance, its admission webhook, and its network policy — stay confined to the namespaces Orbit itself manages, so that this second, independent control plane can run alongside it on the same cluster without either side touching the other's workloads. This PRD defines that coexistence boundary as an explicit, documented, and live-verified contract.

---

## Problem Statement

### Current State

- Orbit's isolation from a co-tenant operator is real today but rests on convention rather than a hard boundary: Istio sidecar injection and SPIRE attestation are namespace-opt-in, gated on labels Orbit itself sets (`istio-injection: enabled`, `spiffe.io/spire-managed: "true"`) only on `orbit-system` and its own tenant namespaces (`charts/orbit-platform/templates/namespaces.yaml`); the `orbit-operator` pod-mutating webhook is scoped to `namespaceSelector: {orbit.io/role: tenant}` (`charts/orbit-operator/templates/webhook.yaml`); Orbit's `NetworkPolicy` set is scoped entirely inside `orbit-system` (`charts/orbit-platform/templates/netpols.yaml`). None of this has been verified against a real second operator actually sharing the cluster.
- Orbit's OPA `ext_authz` ingress policy (`charts/orbit/templates/opa-gateway/envoyfilter.yaml`) is `workloadSelector`-scoped to Orbit's own gateway workload and denies any vhost it doesn't explicitly exempt. A workload sharing Orbit's ingress-gateway pods gets every vhost denied by default; `vcluster-mcp` runs its own dedicated ingress gateway specifically to avoid that, and this policy must stay scoped the way it is today for that separation to keep working.

### User Pain Points

- A platform engineer or agent provisioning a dev environment has no documented guarantee that doing so is safe to run on Orbit's own staging/host cluster.
- An Orbit maintainer changing mesh, SPIRE, admission-webhook, or network-policy code has no checklist telling them whether a change has widened one of those surfaces from namespace-scoped to effectively cluster-wide — the kind of regression that would only surface as a co-tenant operator failing live.

### Business Impact

- Running dev environments on Orbit's own shared host cluster, instead of a second dedicated cluster, avoids duplicating infrastructure.
- Orbit's ingress `ext_authz` policy has already denied traffic for a workload sharing its gateway — this class of interference is real, not hypothetical, and the same could happen on any of Orbit's other cluster-scoped surfaces without warning.

---

## Goals and Objectives

### Primary Goals

1. **Let `vcluster-mcp`'s operator and MCP server run as ordinary workloads in the same host cluster as Orbit**, with no onboarding, identity grant, or code change on Orbit's side.
2. **Let `vcluster-mcp` provision vclusters (and whatever pods a vcluster syncs down to the host cluster) in that same cluster** without Orbit's mesh, SPIRE identity issuance, admission webhook, or network policy ever matching them.
3. **Make Orbit's isolation boundary explicit and documented**: name the reserved namespace labels (`orbit.io/role`, `istio-injection`, `spiffe.io/spire-managed`) a co-tenant operator must not set on its own namespaces, so the boundary is legible without reading Orbit's Go/chart source.
4. **Verify live that this coexistence actually holds**: Orbit's own workloads stay healthy and `vcluster-mcp`'s workloads never pick up an Orbit-managed sidecar, identity, or policy, across multiple fresh provisions on a shared cluster.

### Success Metrics

- Every cluster-scoped Orbit surface named in § Requirements (mesh injection, `ClusterSPIFFEID`, admission webhook, network policy, ingress `ext_authz`) is confirmed, by inspection and by a live run, to be scoped to namespaces Orbit itself manages.
- A live run with `vcluster-mcp`'s operator and at least one provisioned vcluster running concurrently with Orbit's own platform install shows zero Orbit-managed sidecars, SPIFFE identities, or policies attached to any `vcluster-mcp`-owned namespace or pod, across three consecutive fresh provisions.
- Orbit's own workloads show no restarts, degraded health, or policy denials attributable to `vcluster-mcp`'s presence, across that same run.
- The reserved-label contract is written down in this repo, not only inferable from reading `clusterSpiffeIDs.yaml`, `webhook.yaml`, and `netpols.yaml` directly.

### Non-Goals

- **Onboarding `vcluster-mcp` as an Orbit tenant, system component, or agent, or issuing it any SPIFFE identity or OPA grant.** It authenticates and authorizes itself independently (Passport OAuth, per its own architecture) and needs nothing from Orbit's identity or policy model.
- **Any dev-environment provisioning orchestration, CRD, or controller in Orbit.** That belongs entirely to `vcluster-mcp`, per the decision already made and already implemented (commit `7a758b2`).
- **A shared ingress gateway between Orbit and `vcluster-mcp`.** `vcluster-mcp` runs its own dedicated gateway.
- **Capacity/quota coordination between the two systems' resource consumption on the shared node pool.** Both consuming the same node pool's finite compute is a real operational fact, tracked as a risk (§ Risks), not something this PRD's code needs to negotiate.

---

## User Stories

### Primary User Stories

**As** a platform engineer running `vcluster-mcp` against Orbit's own staging cluster,
**I want** a documented guarantee that provisioning a dev environment cannot affect Orbit's own running platform, and vice versa,
**so that** I don't need a second, dedicated cluster just to be safe, and don't need to live-test every Orbit release before trusting it.

**Acceptance Criteria:**
- [ ] Orbit's mesh injection, `ClusterSPIFFEID` templates, admission webhook, and network policies are confirmed namespace-scoped to Orbit-managed namespaces only, with the scoping mechanism documented in this repo
- [ ] A live run with both systems active on one cluster shows zero cross-interference in either direction, across three consecutive fresh provisions

---

**As** an Orbit maintainer changing mesh, SPIRE, admission-webhook, or network-policy code,
**I want** to know which of those surfaces are relied on to stay namespace-scoped for `vcluster-mcp` coexistence,
**so that** I don't silently widen one to cluster-wide and break a co-tenant I never see fail in Orbit's own test suite.

**Acceptance Criteria:**
- [ ] This document's Requirements table names every such surface and the file where it's enforced
- [ ] The reserved-label contract (`orbit.io/role`, `istio-injection`, `spiffe.io/spire-managed`) is documented, not just conventionally followed

### Secondary User Stories

**As** a developer working in [`vcluster-mcp`](https://github.com/clear-route/vcluster-mcp), **I want to** know which namespace labels are reserved by Orbit before I write my own namespace-creation code, **so that** I don't accidentally opt one of my own namespaces into Orbit's mesh, identity, or admission control. (P1 — served by this doc's Requirements table.)

---

## Proposed Solution

### High-Level Approach

Audit each of Orbit's cluster-scoped surfaces to confirm it only ever matches namespaces/pods Orbit itself creates and labels; document the reserved labels a co-tenant must avoid; and run a live coexistence check with `vcluster-mcp`'s operator and a provisioned vcluster active alongside Orbit's own platform install. If the audit or the live run finds a surface that isn't actually scoped the way it looks, fix that specific surface; otherwise this work delivers documentation and a verification gate, not new code.

### Key Components

- **Mesh injection.** Istio sidecar injection stays namespace-opt-in via `istio-injection: enabled`, set only on namespaces Orbit's own chart creates (`charts/orbit-platform/templates/namespaces.yaml`) — never a cluster-wide default that would sweep in a co-tenant's namespaces.
- **SPIRE identity issuance.** The `ClusterSPIFFEID` templates in `charts/orbit-platform/templates/clusterSpiffeIDs.yaml` (`istio-gateway`, `istio-sidecar`, `orbit-system-component`, `orbit-tenant-service`, `orbit-tenant-agent`, `orbit-tenant-sandbox`) each key on `orbit.io/role` and/or a fixed system-namespace name, and never match a namespace Orbit didn't itself label.
- **Admission control.** `orbit-operator`'s `aaa-orbit-agent-pod-mutator` `MutatingWebhookConfiguration` (`charts/orbit-operator/templates/webhook.yaml`) stays `namespaceSelector`-scoped to `orbit.io/role: tenant`.
- **Network policy.** Orbit's `NetworkPolicy` set (`charts/orbit-platform/templates/netpols.yaml`) stays scoped inside `orbit-system`.
- **Ingress `ext_authz`.** Orbit's OPA `ext_authz` `EnvoyFilter` stays `workloadSelector`-scoped to Orbit's own gateway workload.
- **Reserved-label contract.** `orbit.io/role`, `istio-injection`, and `spiffe.io/spire-managed` are Orbit-owned namespace labels, documented as reserved so a co-tenant operator (`vcluster-mcp` or any future one) knows not to set them on its own namespaces.

---

## Scope

### In Scope

- Auditing each cluster-scoped Orbit surface named above to confirm namespace-scoping holds as designed
- Documenting the reserved-label contract in this repo
- A live coexistence verification gate: `vcluster-mcp`'s operator plus a provisioned vcluster running concurrently with Orbit's own platform install on one host cluster, three consecutive fresh provisions, zero cross-interference either direction
- Fixing any gap the audit or live run finds in the surfaces above

### Out of Scope

- Any dev-environment provisioning orchestration, CRD, or controller in Orbit
- A shared ingress gateway between Orbit and `vcluster-mcp`
- Any identity, authentication, or authorization relationship between Orbit and `vcluster-mcp`'s operator or MCP server
- Capacity/quota coordination between the two systems' resource consumption on the shared node pool
- `vcluster-mcp`'s own operator, MCP server, CRD, auth model, or ingress design

### Future Considerations

- If shared-node-pool resource contention between Orbit and `vcluster-mcp`-provisioned vclusters becomes a real operational problem, a capacity/quota policy may need to be documented (which side reserves what).
- An automated, low-cost assertion (short of a full live cluster) that a given namespace label/selector stays scoped the way this PRD documents, so a future Orbit change that widens one is caught in `make check` rather than only by a live co-tenant failing.

---

## Requirements

### Coexistence Surfaces (Orbit-Owned)

| # | Requirement | Enforcement site (this repo) | Confirmed scoped today? |
|---|---|---|---|
| C-1 | Istio sidecar injection is namespace-opt-in (`istio-injection: enabled`), set only on namespaces Orbit's own chart creates | `charts/orbit-platform/templates/namespaces.yaml` | Yes, by inspection — needs live confirmation |
| C-2 | Every `ClusterSPIFFEID` template matches only namespaces/pods carrying an Orbit-set label (`orbit.io/role`) or a fixed Orbit system-namespace name | `charts/orbit-platform/templates/clusterSpiffeIDs.yaml` | Yes, by inspection — needs live confirmation |
| C-3 | The `orbit-operator` pod-mutating webhook fires only on `orbit.io/role: tenant` namespaces | `charts/orbit-operator/templates/webhook.yaml` | Yes, by inspection — needs live confirmation |
| C-4 | Orbit's `NetworkPolicy` set is scoped to `orbit-system`; no cluster-wide default-deny | `charts/orbit-platform/templates/netpols.yaml` | Yes, by inspection — needs live confirmation |
| C-5 | Orbit's ingress `ext_authz` `EnvoyFilter` stays `workloadSelector`-scoped to Orbit's own gateway workload, never widened to cluster-wide | `charts/orbit/templates/opa-gateway/envoyfilter.yaml` | Yes, by inspection — regression guard only |
| C-6 | `orbit.io/role`, `istio-injection`, and `spiffe.io/spire-managed` are documented as Orbit-reserved namespace labels | This document; chart comments | Not yet documented outside chart comments |

### Functional Requirements

**Must Have (P0):**
- [ ] Every C-row above is confirmed live, not just by inspection, with `vcluster-mcp`'s operator and a provisioned vcluster active on the same cluster
- [ ] Zero Orbit-managed sidecar, SPIFFE identity, admission mutation, or network-policy denial ever touches a `vcluster-mcp`-owned namespace or pod, across three consecutive fresh provisions
- [ ] Orbit's own workloads show no restarts, degraded health, or policy denials attributable to `vcluster-mcp`'s presence, across that same run

**Should Have (P1):**
- [ ] The reserved-label contract (C-6) is written down in this repo, not only inferable from chart source

**Nice to Have (P2):**
- [ ] A `make check`-runnable assertion (no live cluster) that each C-row's selector stays scoped the way this document describes, so a future regression is caught before it reaches a live co-tenant

### Non-Functional Requirements

**Security:**
- Confirming this coexistence boundary must never involve widening any Orbit selector to be less specific — every surface stays scoped to namespaces Orbit itself manages, and a host install's authorization surface must be provably unaffected by a co-tenant's presence

**Reliability:**
- The live verification (three consecutive fresh provisions) must show no flakiness attributable to running alongside `vcluster-mcp` — a one-time pass is not sufficient evidence

**Observability:**
- If a future change does widen one of the C-row surfaces, that should be visible in rendered Helm values or in what a co-tenant's namespace actually receives (an injected sidecar it shouldn't have, an unexpected SPIFFE ID) — not only discoverable by that co-tenant failing live

---

## Dependencies

### Internal Dependencies

- `charts/orbit-platform/templates/namespaces.yaml`, `clusterSpiffeIDs.yaml`, `netpols.yaml` — the mesh-injection, SPIRE-identity, and network-policy scoping this PRD audits
- `charts/orbit-operator/templates/webhook.yaml` — the admission-webhook scoping this PRD audits
- `charts/orbit/templates/opa-gateway/envoyfilter.yaml` — the ingress `ext_authz` scoping this PRD audits, and the one surface that has already caused a real denial for a shared-workload scenario

### External Dependencies

- **[`vcluster-mcp`](https://github.com/clear-route/vcluster-mcp)** — the co-tenant this contract is verified against. Its own dedicated ingress gateway and independent auth model (Passport OAuth) are inputs here, not renegotiated.
- **A shared host cluster** running both Orbit's own platform install and `vcluster-mcp`'s operator (Orbit's own staging cluster or local dev stack) — the environment this coexistence contract targets.

---

## Risks and Mitigations

| Risk | Impact | Probability | Mitigation |
|------|--------|-------------|------------|
| A future Orbit change widens one of the C-row selectors from namespace-scoped to effectively cluster-wide, with nothing catching it before a live co-tenant fails | Medium | Medium | Name the surfaces explicitly in this document (§ Requirements) so a PR touching one of them is reviewed against this contract; consider the P2 automated assertion if this recurs |
| Shared node-pool resource contention between Orbit and `vcluster-mcp`-provisioned vclusters degrades either side's performance | Medium | Medium | Out of scope for code changes today (see Non-Goals); revisit as a Future Consideration if it becomes a real operational problem |
| `vcluster-mcp`'s own architecture continues to evolve, and this contract's assumptions about its ingress/auth model could drift out of date | Low | Medium | Confirm `vcluster-mcp`'s current architecture before running the live verification gate; treat any material change there as a follow-up, not silent scope creep here |

---

## Alternatives Considered

### Alternative 1: A dedicated second cluster for dev environments, avoiding the coexistence question entirely

**Why not chosen:** Duplicates infrastructure. The coexistence boundary this PRD documents already holds today by design (namespace-scoped, opt-in selectors); paying for a second cluster to avoid verifying that is disproportionate.

### Alternative 2: Orbit onboards `vcluster-mcp` as a system component or tenant, with its own SPIFFE identity and OPA grants

**Why not chosen:** `vcluster-mcp` already has its own independent authentication and doesn't call into Orbit's identity or policy model at all. Giving it Orbit-issued identity would be unnecessary coupling — it needs Orbit to leave it alone, not to recognize it.

### Alternative 3: Move dev-environment orchestration into Orbit, so one team/repo owns the whole thing

**Why not chosen:** Already decided against, and already implemented — commit `7a758b2` removed Orbit's prior attempt at exactly this, in favor of an Orbit-independent provisioning control plane with no dependency on Orbit's source. Not reopened here.

---

## Open Questions

1. **Is namespace-label opt-in alone sufficient, or does Orbit's istiod need explicit `discoverySelectors` excluding non-Orbit namespaces** for control-plane scale/resource reasons even though injection itself is already opt-in?
   - **Status:** Open — resolve via the audit/live run, only if a gap is actually found
   - **Owner:** Francis Holt

2. **Should the reserved-label contract (C-6) be enforced** (e.g., Orbit's operator logs or rejects if it observes `orbit.io/role: tenant` on a namespace it didn't create) **or stay documentation-only?**
   - **Status:** Open
   - **Owner:** Francis Holt

3. **Does the shared host cluster need a documented minimum node-pool size** once both systems run on it concurrently, and whose doc owns that — Orbit's or `vcluster-mcp`'s?
   - **Status:** Open — cross-repo, likely resolved in `vcluster-mcp`'s own capacity docs first
   - **Owner:** Francis Holt

---

## Appendix

### Related Documents

- [`vcluster-mcp`](https://github.com/clear-route/vcluster-mcp) — the co-tenant this contract is verified against; see its own docs for its ingress and auth architecture

### Glossary

- **Vcluster**: a virtual Kubernetes cluster running as a pod inside a host cluster's namespace; `vcluster-mcp` provisions one per dev environment
- **Host cluster**: the real Kubernetes cluster both Orbit's own platform install and `vcluster-mcp`'s operator run on
- **Co-tenant operator**: an independent Kubernetes controller (here, `vcluster-mcp`'s) running in the same host cluster as Orbit, with no identity or authorization relationship to it
- **Reserved label**: a namespace label Orbit's own charts set and key selectors on (`orbit.io/role`, `istio-injection`, `spiffe.io/spire-managed`); a co-tenant must not set these on its own namespaces to avoid being swept into Orbit's mesh/identity/admission model

---

**Approval Signatures:**

- [ ] Product Owner: _________________ Date: _______
- [ ] Technical Lead: _________________ Date: _______
- [ ] Security Review: _________________ Date: _______
