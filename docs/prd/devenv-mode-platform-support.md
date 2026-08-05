# Devenv-Mode Platform Support - Product Requirements Document

**Status:** Draft
**Created:** 2026-08-03
**Last Updated:** 2026-08-05
**Owner:** Francis Holt
**Stakeholders:** Platform Engineering, `vcluster-mcp` maintainers

---

> **Revision note, 2026-08-05.** This PRD originally covered a much larger surface: `vcluster-mcp` installing a full nested Orbit replica inside every provisioned dev environment, with Orbit's charts/operator supporting a SPIRE-disabled "devenv mode." `vcluster-mcp` has since dropped that design entirely ([its PR #5](https://github.com/clear-route/vcluster-mcp/pull/5), "vanilla-vcluster/dedicated-ingress design," merged 2026-08-05) — a provisioned environment is now a **vanilla Kubernetes vcluster** with no Orbit software inside it at all, reachable through `vcluster-mcp`'s own dedicated ingress gateway, not Orbit's. **No in-vcluster Orbit payload means none of the original requirements (SPIRE-disabled identity, principal-extractor prefix parsing, shared-gateway `ext_authz` carve-outs) apply anymore.** What remains, and what this revision defines, is simpler: `vcluster-mcp`'s own operator and MCP server now run as ordinary workloads **in the same host Kubernetes cluster as Orbit's own platform install**, and that operator provisions vclusters into that same cluster. This PRD now covers what Orbit must guarantee so that coexistence is safe — nothing more. The companion ADR ([2026-08-04-devenv-mode-platform-support](../adr/2026-08-04-devenv-mode-platform-support.md)) covered the old, larger design and is superseded on that basis; a new ADR, scoped to this revision, is expected once the audit below either confirms no code change is needed or turns up a real gap.

---

## Executive Summary

`vcluster-mcp` — a standalone, Orbit-independent MCP server and Kubernetes operator ([clear-route/vcluster-mcp](https://github.com/clear-route/vcluster-mcp)) — provisions disposable dev environments as vanilla vclusters (virtual Kubernetes clusters running as pods), and runs its own operator and MCP server as ordinary workloads, **in the same host Kubernetes cluster that Orbit's own platform is installed on**. Orbit is not installed inside those vclusters, is not asked to authenticate or authorize `vcluster-mcp`, and is not on the request path for anything it provisions. Orbit's only obligation is to **not get in the way**: its mesh, SPIRE identity issuance, admission webhook, and network policy surfaces must stay confined to the namespaces Orbit itself manages, so a second, independent control plane can run alongside it on the same cluster without either side touching the other's workloads. This PRD enumerates that boundary as an explicit, verified contract instead of an assumption.

---

## Problem Statement

### Current State

- Orbit has never run alongside a second, independent Kubernetes operator in the same cluster in a way anyone has verified. Its mesh (istiod), identity issuance (SPIRE `ClusterSPIFFEID` templates), admission control (`orbit-operator`'s pod-mutating webhook), and network policy are all real, cluster-scoped Kubernetes primitives — nothing today proves live that they leave a co-tenant's namespaces and pods alone.
- That isolation is real today, but by convention rather than by a hard boundary: Istio sidecar injection and SPIRE attestation are opt-in per namespace, gated on labels Orbit itself sets (`istio-injection: enabled`, `spiffe.io/spire-managed: "true"`) only on `orbit-system` and its own tenant namespaces (`charts/orbit-platform/templates/namespaces.yaml`); the `orbit-operator` pod-mutating webhook is scoped to `namespaceSelector: {orbit.io/role: tenant}` (`charts/orbit-operator/templates/webhook.yaml`); Orbit's `NetworkPolicy` set is scoped entirely inside `orbit-system` (`charts/orbit-platform/templates/netpols.yaml`). Nothing enforces that a future change keeps these scoped this way, and nothing has verified live that a real co-tenant operator's namespaces are actually untouched.
- `vcluster-mcp`'s earlier design tried to reuse Orbit's shared ingress gateway workload for its own dev-environment traffic and hit exactly the failure mode this PRD exists to prevent recurring elsewhere: Orbit's OPA `ext_authz` `EnvoyFilter` is `workloadSelector`-scoped to Orbit's own gateway pods and denies any vhost it doesn't explicitly exempt, so sharing that workload — even with fully separate `Gateway` objects — got every dev-environment vhost denied by default. `vcluster-mcp` fixed this on its own side by giving itself a **dedicated** ingress-gateway workload (Istio's own upstream `gateway` chart, no dependency on Orbit's chart). The fix that matters to Orbit is not to reproduce that shared-workload coupling anywhere else, not to build a carve-out for it.

### User Pain Points

- A platform engineer or agent provisioning a dev environment today has no documented guarantee that doing so is safe to run on Orbit's own staging/host cluster rather than requiring a second, dedicated cluster purely to avoid unverified interference.
- An Orbit maintainer changing mesh, SPIRE, admission-webhook, or network-policy code has no test or checklist telling them whether a change has widened one of those surfaces from namespace-scoped to effectively cluster-wide — the exact kind of regression that would only be discovered by a co-tenant operator failing live.

### Business Impact

- Running dev environments on a second, dedicated cluster purely for isolation duplicates infrastructure `vcluster-mcp`'s own architecture already avoids by targeting the shared host cluster (per its own ADR).
- The one interference bug already found live (the shared-ingress-gateway `ext_authz` collision) shows this class of bug is real, not hypothetical, even though it was ultimately resolved without any Orbit-side change.

---

## Goals and Objectives

### Primary Goals

1. **Let `vcluster-mcp`'s operator and MCP server run as ordinary workloads in the same host cluster as Orbit**, with no onboarding, identity grant, or code change on Orbit's side — it is not an Orbit tenant, system component, or agent, and must not need to become one.
2. **Let `vcluster-mcp` provision vanilla vclusters (and whatever pods a vcluster syncs down to the host cluster) in that same cluster** without Orbit's mesh, SPIRE identity issuance, admission webhook, or network policy ever matching them.
3. **Make Orbit's isolation boundary explicit and documented**, not just an emergent property of today's label choices: name the reserved namespace labels (`orbit.io/role`, `istio-injection`, `spiffe.io/spire-managed`) any co-tenant operator must not set on its own namespaces, so the boundary is legible without reading Orbit's Go/chart source.
4. **Verify live that this coexistence actually holds**: Orbit's own workloads stay healthy and `vcluster-mcp`'s workloads never pick up an Orbit-managed sidecar, identity, or policy, across multiple fresh provisions on a shared cluster.
5. **Confirm no Orbit-side CRD, controller, chart, or identity extension is needed for dev environments** — that decision (`7a758b2`) stands; this PRD's job is coexistence, not orchestration.

### Success Metrics

- Every cluster-scoped Orbit surface named in § Requirements (mesh injection, `ClusterSPIFFEID`, admission webhook, network policy, ingress `ext_authz`) is confirmed, by inspection and by a live run, to be scoped to namespaces Orbit itself manages — never cluster-wide.
- A live run with `vcluster-mcp`'s operator and at least one provisioned vcluster running concurrently with Orbit's own platform install on one host cluster shows zero Orbit-managed sidecars, SPIFFE identities, or policies attached to any `vcluster-mcp`-owned namespace or pod, across three consecutive fresh provisions.
- Orbit's own workloads show no restarts, no degraded health, and no policy denials attributable to `vcluster-mcp`'s presence on the same cluster, across that same run.
- The reserved-label contract is written down in this repo (this PRD plus chart comments), not only inferable from reading `clusterSpiffeIDs.yaml`, `webhook.yaml`, and `netpols.yaml` directly.

### Non-Goals

- **No Orbit software runs inside a provisioned vcluster.** That design is superseded (see banner) and is not being revived by this revision.
- **No shared ingress gateway between Orbit and `vcluster-mcp`.** `vcluster-mcp` now runs its own dedicated gateway; Orbit's obligation is only to not need to be asked for one.
- **No SPIFFE identity, OPA grant, or Orbit tenant/system-component status for `vcluster-mcp`'s operator, MCP server, or the vclusters it creates.** It authenticates and authorizes itself independently (Passport OAuth, per its own architecture) and must not need anything from Orbit's identity model to operate.
- **No change to Orbit's SPIRE-enabled, mesh-enabled behavior for its own tenants and system components.** This is entirely about confirming Orbit's existing boundary holds against a co-tenant, not changing how Orbit governs its own workloads.
- **Not `vcluster-mcp`'s own CRD, reconciler, sleep/wake, kubeconfig brokering, or MCP tool surface** — entirely that repo's, per the decision already made and already implemented (commit `7a758b2` removed Orbit's own prior attempt at owning this).
- **Not capacity planning or a quota/scheduling handshake between the two systems.** Both consuming the same node pool's finite compute is a real operational fact, not something either side's code needs to negotiate with the other about today (see § Risks). Flagged, not solved, here.

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

Treat "a second, independent operator and its workloads can safely share this cluster with Orbit" as an explicit, audited, and live-verified property — not an assumption resting on today's label choices happening to be namespace-scoped. Concretely: audit each of Orbit's cluster-scoped surfaces to confirm it only ever matches namespaces/pods Orbit itself creates and labels; write down the reserved labels a co-tenant must avoid; and run a live coexistence check with `vcluster-mcp`'s operator and a provisioned vcluster active alongside Orbit's own platform install. If the audit or the live run finds a surface that isn't actually scoped the way it looks (the kind of gap `vcluster-mcp` already found once, in the ingress `ext_authz` case, before fixing it entirely on its own side), fixing that gap in this repo is in scope; otherwise this work produces documentation and a verification gate, not new code.

### Key Components

- **Mesh injection.** Istio sidecar injection stays namespace-opt-in via `istio-injection: enabled`, set only on namespaces Orbit's own chart creates (`charts/orbit-platform/templates/namespaces.yaml`) — never a cluster-wide `discoverySelectors`/injection default that would sweep in a co-tenant's namespaces.
- **SPIRE identity issuance.** The `ClusterSPIFFEID` templates in `charts/orbit-platform/templates/clusterSpiffeIDs.yaml` (`istio-gateway`, `istio-sidecar`, `orbit-system-component`, `orbit-tenant-service`, `orbit-tenant-agent`, `orbit-tenant-sandbox`) each key on `orbit.io/role` and/or a fixed system-namespace name — confirmed to never match a namespace Orbit didn't itself label.
- **Admission control.** `orbit-operator`'s `aaa-orbit-agent-pod-mutator` `MutatingWebhookConfiguration` (`charts/orbit-operator/templates/webhook.yaml`) stays `namespaceSelector`-scoped to `orbit.io/role: tenant` — confirmed it can't fire on a co-tenant's pod creates.
- **Network policy.** Orbit's `NetworkPolicy` set (`charts/orbit-platform/templates/netpols.yaml`) stays scoped inside `orbit-system`; nothing cluster-wide-default-deny that could catch a co-tenant's namespace.
- **Ingress `ext_authz`.** Orbit's OPA `ext_authz` `EnvoyFilter` stays `workloadSelector`-scoped to Orbit's own gateway workload — a regression guard, not new work, since `vcluster-mcp` already worked around a version of this exact class of bug by not sharing that workload at all.
- **Reserved-label contract.** `orbit.io/role`, `istio-injection`, and `spiffe.io/spire-managed` are Orbit-owned namespace labels; documented here as reserved so a co-tenant operator (`vcluster-mcp` or any future one) knows not to set them on its own namespaces.

---

## Scope

### In Scope

- Auditing each cluster-scoped Orbit surface named above to confirm namespace-scoping holds as designed
- Documenting the reserved-label contract in this repo
- A live coexistence verification gate: `vcluster-mcp`'s operator plus a provisioned vcluster running concurrently with Orbit's own platform install on one host cluster, three consecutive fresh provisions, zero cross-interference either direction
- Fixing any gap the audit or live run finds in the surfaces above (expected to be none, based on the audit already done for this revision — see § Dependencies)

### Out of Scope

- Any dev-environment provisioning orchestration, CRD, or controller in Orbit
- Any Orbit software, chart, or identity inside a provisioned vcluster
- A shared ingress gateway between Orbit and `vcluster-mcp`
- Any identity, authentication, or authorization relationship between Orbit and `vcluster-mcp`'s operator or MCP server
- Capacity/quota coordination between the two systems' resource consumption on the shared node pool
- `vcluster-mcp`'s own operator, MCP server, CRD, auth model, or ingress design

### Future Considerations

- If shared-node-pool resource contention between Orbit and `vcluster-mcp`-provisioned vclusters becomes a real operational problem, a capacity/quota policy may need to be documented (which side reserves what) — not designed here, since there's no evidence yet that it's needed.
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

- `charts/orbit-platform/templates/namespaces.yaml`, `clusterSpiffeIDs.yaml`, `netpols.yaml` — the mesh-injection, SPIRE-identity, and network-policy scoping already audited for this revision
- `charts/orbit-operator/templates/webhook.yaml` — the admission-webhook scoping already audited for this revision
- `charts/orbit/templates/opa-gateway/envoyfilter.yaml` — the ingress `ext_authz` scoping, already the site of the one interference bug `vcluster-mcp` found and fixed on its own side

### External Dependencies

- **[`vcluster-mcp`](https://github.com/clear-route/vcluster-mcp)** — the sole co-tenant this contract is verified against today. Its [PR #5](https://github.com/clear-route/vcluster-mcp/pull/5) (vanilla-vcluster/dedicated-ingress design) is the reference point for its current architecture; its own architecture decisions (CRD, auth model, dedicated ingress gateway) are inputs here, not renegotiated.
- **A shared host cluster** running both Orbit's own platform install and `vcluster-mcp`'s operator (Orbit's own staging cluster or local dev stack) — the environment this coexistence contract targets.

---

## Risks and Mitigations

| Risk | Impact | Probability | Mitigation |
|------|--------|-------------|------------|
| A future Orbit change widens one of the C-row selectors from namespace-scoped to effectively cluster-wide, with nothing catching it before a live co-tenant fails | Medium | Medium | Name the surfaces explicitly in this document (§ Requirements) so a PR touching one of them is reviewed against this contract; consider the P2 automated assertion if this recurs |
| Shared node-pool resource contention between Orbit and `vcluster-mcp`-provisioned vclusters degrades either side's performance | Medium | Medium | Out of scope for code changes today (see Non-Goals); revisit as a Future Consideration if it becomes a real operational problem |
| `vcluster-mcp`'s own design keeps moving (five PRs merged in three days as of this revision) — this contract could target a design already superseded again | Low | Medium | Pin this work to `vcluster-mcp` PR #5 as the reference point; treat any further design change there as a follow-up, not silent scope creep here |

---

## Alternatives Considered

### Alternative 1: A dedicated second cluster for dev environments, avoiding the coexistence question entirely

**Why not chosen:** Duplicates infrastructure that `vcluster-mcp`'s own architecture already avoids by targeting Orbit's shared host cluster (per its own ADR). The coexistence boundary this PRD documents already holds today by design (namespace-scoped, opt-in selectors); paying for a second cluster to avoid verifying that is disproportionate.

### Alternative 2: Orbit onboards `vcluster-mcp` as a system component or tenant, with its own SPIFFE identity and OPA grants

**Why not chosen:** `vcluster-mcp` already has its own independent authentication (Passport OAuth) and doesn't call into Orbit's identity or policy model at all. Giving it Orbit-issued identity would be unnecessary coupling in the direction this PRD's Non-Goals explicitly reject — it needs Orbit to leave it alone, not to recognize it.

### Alternative 3: Move dev-environment orchestration back into Orbit, so one team/repo owns the whole thing

**Why not chosen:** Already decided against, and already implemented — `7a758b2` removed Orbit's prior attempt at exactly this. `vcluster-mcp`'s own ADR gives a specific, still-valid reason (no Orbit source-code dependency for an Orbit-independent provisioning control plane). Not reopened here.

---

## Open Questions

1. **Is namespace-label opt-in alone sufficient, or does Orbit's istiod need explicit `discoverySelectors` excluding non-Orbit namespaces** for control-plane scale/resource reasons even though injection itself is already opt-in?
   - **Status:** Open — resolve via the audit/live run before writing a follow-up ADR, only if a gap is actually found
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

- [`vcluster-mcp`](https://github.com/clear-route/vcluster-mcp)'s own architecture ADR and PRD, and its [PR #5](https://github.com/clear-route/vcluster-mcp/pull/5) — the external consumer's current design (vanilla vclusters, dedicated ingress gateway, no in-vcluster Orbit payload) this revision is scoped against
- The prior, superseded, much larger version of this PRD (in this file's own history) and its companion ADR ([2026-08-04-devenv-mode-platform-support](../adr/2026-08-04-devenv-mode-platform-support.md)) — covered installing a full Orbit replica inside every dev environment, a design `vcluster-mcp` has since dropped entirely
- The closed, superseded PR #113 (`feat/vcluster-design`) — Orbit's own earlier, in-repo attempt at dev-environment orchestration, since removed (`7a758b2`)

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
