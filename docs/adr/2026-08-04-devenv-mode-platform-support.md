# Devenv-Mode Platform Support

**Status:** Proposed
**Date:** 2026-08-04
**Deciders:** Francis Holt
**Technical Story:** [Devenv-Mode Platform Support PRD](../prd/devenv-mode-platform-support.md)

---

## Context

[`vcluster-mcp`](https://github.com/clear-route/vcluster-mcp) — a standalone, Orbit-independent MCP server and Kubernetes operator — provisions ephemeral dev environments as vanilla vclusters (virtual Kubernetes clusters running as pods) with no Orbit software inside them, and runs its own operator and MCP server as ordinary workloads in the **same host Kubernetes cluster** as Orbit's own platform install ([its PR #5](https://github.com/clear-route/vcluster-mcp/pull/5), "vanilla-vcluster/dedicated-ingress design," merged 2026-08-05). This is a substantially smaller scope than an earlier version of this same ADR, which decided how to make Orbit's own charts install correctly *inside* each provisioned vcluster (`spire.enabled=false`, host-attested identity, a shared ingress gateway). That design is dead: `vcluster-mcp` dropped the in-vcluster Orbit payload entirely, so none of the previous decisions (new `OrbitPlatformSpec` fields, a `chartrender` test harness, `ClusterSPIFFEID`/principal-extractor/`opa-gateway` devenv-mode branches) are needed.

What remains is a coexistence question, not an installation question: can `vcluster-mcp`'s operator, its MCP server, and the vclusters it provisions run alongside Orbit's own platform install on one shared cluster without either side touching the other's workloads? Orbit's own cluster-scoped surfaces — Istio sidecar injection, SPIRE `ClusterSPIFFEID` identity issuance, the `orbit-operator` pod-mutating admission webhook, and `NetworkPolicy` — are all real, cluster-visible Kubernetes primitives. An audit of each one (`charts/orbit-platform/templates/namespaces.yaml`, `clusterSpiffeIDs.yaml`, `netpols.yaml`; `charts/orbit-operator/templates/webhook.yaml`) shows every one of them is already namespace-scoped and opt-in, keyed on labels Orbit itself sets only on namespaces it creates (`istio-injection: enabled`, `spiffe.io/spire-managed: "true"`, `orbit.io/role: tenant`) — never a cluster-wide default. That scoping has never been verified against a real second control plane sharing the cluster, and it has already been wrong once in an adjacent surface: `vcluster-mcp` originally tried to reuse Orbit's shared ingress-gateway workload and hit Orbit's OPA `ext_authz` `EnvoyFilter` — `workloadSelector`-scoped to Orbit's own gateway pods, denying any vhost it doesn't explicitly exempt — denying every dev-environment vhost by default. `vcluster-mcp` fixed that on its own side (a dedicated ingress-gateway workload), but the pattern — "this Orbit selector looked scoped enough until a real co-tenant workload showed up" — is exactly what this ADR exists to close out for the remaining surfaces before it recurs.

### Decision Drivers

- **Minimum new machinery.** The [PRD](../prd/devenv-mode-platform-support.md)'s own Alternatives Considered rejects giving `vcluster-mcp` any Orbit-issued identity or building new orchestration; this ADR must not reintroduce scope through the back door by writing code the audit doesn't show is needed.
- **The reserved-label contract must be legible without reading Go/chart source.** A co-tenant operator (`vcluster-mcp`, or a future one) needs to know which namespace labels are Orbit-reserved without reverse-engineering `clusterSpiffeIDs.yaml`.
- **"We think this works" is not enough on its own.** The one interference bug already found live (the shared-ingress-gateway `ext_authz` collision) shows this class of bug is real, not hypothetical, even though it was fixed without any Orbit-side change. The audit's conclusions must be confirmed against a real, running co-tenant, not accepted on inspection alone.
- **No cluster in `make check`.** Per the Testing Strategy in `AGENTS.md`, unit tests run with no cluster and CI does not run the one existing live-cluster integration suite. Any verification mechanism for this ADR that needs a real second operator running has to be a live gate outside `make check`, following the same precedent as [Kubernetes-Only Local Development](2026-07-10-kubernetes-only-local-development.md) (a real cluster, not a synthetic stand-in) and the live-verification gate in this ADR's own prior version.

---

## Decision

Three sub-decisions, all scoped to confirming and documenting a boundary that the audit shows already holds — not building a new one.

### Decision 1 — This is a documentation-and-verification exercise; new code is written only if the live gate finds a real gap

**Options considered:**

- **A. Add proactive guardrails now** (an explicit `discoverySelectors` exclusion, an admission check rejecting `orbit.io/role`/`istio-injection`/`spiffe.io/spire-managed` on a namespace Orbit didn't create, etc.), speculatively, without evidence any of Orbit's existing selectors actually mis-fire. Rejected: the audit of `namespaces.yaml`, `clusterSpiffeIDs.yaml`, `webhook.yaml`, and `netpols.yaml` shows every one of them already scoped correctly. Writing defensive code against a gap that inspection doesn't show violates the minimum-new-machinery driver.
- **B. Document the boundary and stop there, with no live check.** Rejected: this is the exact "found live, fixed live" failure mode the shared-ingress-gateway bug already demonstrated once. A boundary that has only ever been read, never run against a real co-tenant, is not yet a guarantee.
- **C. Document the boundary now, verify it against a real, running `vcluster-mcp` deployment as a release gate, and write code only for whatever the live gate actually finds broken.** Chosen.

**Decision:** Treat the audit already performed for the [PRD](../prd/devenv-mode-platform-support.md) (§ Requirements, C-1 through C-5) as the starting hypothesis, not the conclusion. This ADR's Implementation section is the procedure that either confirms it or produces a scoped, evidence-based fix — never a speculative one.

### Decision 2 — The reserved-label contract lives as comments at each enforcement site, cross-referenced to the PRD's table; no new document

**Options considered:**

- **A. A dedicated `docs/reference/cluster-coexistence.md`.** Rejected: a third place to keep in sync (PRD table, chart comments, standalone doc) is exactly the drift the [previous version of this ADR](https://github.com/clear-route/orbit) already rejected the equivalent of, for the same reason — the PRD's own Requirements table (C-1…C-6) is already the enumerated, named list.
- **B. Chart comments only, no PRD cross-reference.** Rejected: a maintainer reading `clusterSpiffeIDs.yaml` in isolation wouldn't know the label choice is load-bearing for a co-tenant operator they may never have heard of; the comment needs to point at *why*.
- **C. A short comment naming the reserved label(s) and linking to the PRD's Requirements table, added at each of the five enforcement sites (C-1 through C-5).** Chosen.

**Decision:** Add one comment block to each of `charts/orbit-platform/templates/namespaces.yaml`, `clusterSpiffeIDs.yaml`, `netpols.yaml`, `charts/orbit-operator/templates/webhook.yaml`, and `charts/orbit/templates/opa-gateway/envoyfilter.yaml`, stating: which label/selector this file relies on to stay namespace-scoped, and a pointer to the [PRD](../prd/devenv-mode-platform-support.md)'s Requirements table (C-1…C-6) for the full contract. No new document.

### Decision 3 — Live verification is a documented manual procedure against Orbit's local dev cluster, run three times, not a `make check` target

**Options considered:**

- **A. Run it in CI.** Rejected: CI has no cluster today (Testing Strategy, `AGENTS.md`); standing one up solely for this would be new infrastructure disproportionate to a coexistence check, and is explicitly out of scope per the PRD's Non-Goals (no capacity/CI coordination between the two systems).
- **B. A single, one-time live check, treated as sufficient once it passes.** Rejected: the PRD's own Success Metrics require three consecutive fresh provisions with zero cross-interference, following the same "not just once" bar the prior version of this ADR already set and this revision inherits.
- **C. A documented procedure run against `make up`'s local dev cluster: deploy `vcluster-mcp`'s operator and MCP server (pinned to its PR #5 design) alongside Orbit's own platform install, provision a vcluster three consecutive times, and inspect both directions each time.** Chosen.

**Decision:** The procedure is § Implementation below. It is a release gate for closing out this ADR and the PRD's P0 requirements, not a `make check` step. A `make check`-runnable regression guard (the PRD's own P2 Nice-to-Have) is explicitly deferred — see Decision 4.

### Decision 4 — No automated no-cluster regression guard yet

There is no existing chart-testing infrastructure in this repo (the prior version of this ADR noted the same gap, then built a `helm template`-shelling harness for a set of templates that no longer exist). Building an equivalent harness now, for a boundary the audit shows already holds, would be new machinery ahead of evidence it's needed — the same violation Decision 1 already rules out. **Decision:** defer this to the PRD's own Future Considerations. If the live-verification gate (Decision 3) or a future incident shows one of the C-1…C-5 selectors actually drifted, that is the trigger to revisit this decision, not this ADR's inspection today.

---

## Requirements → Enforcement → Verification

Restates the PRD's Requirements table (C-1 through C-6) with what confirms each row under this ADR's decisions.

| # | Enforcement site | Audit finding (Decision 1) | Live verification (Decision 3) |
|---|---|---|---|
| C-1 | `charts/orbit-platform/templates/namespaces.yaml` | `istio-injection: enabled` / `spiffe.io/spire-managed: "true"` are set only on the `orbit-system` and Orbit-managed tenant namespace blocks this template renders | After a fresh provision, `vcluster-mcp`'s namespace(s) carry neither label |
| C-2 | `charts/orbit-platform/templates/clusterSpiffeIDs.yaml` | Every `ClusterSPIFFEID`'s `namespaceSelector` keys on `orbit.io/role` or the fixed system-namespace name; none omits a `namespaceSelector` | No SPIRE-issued SVID is ever observed for a pod in a `vcluster-mcp`-owned namespace |
| C-3 | `charts/orbit-operator/templates/webhook.yaml` | The `aaa-orbit-agent-pod-mutator` `MutatingWebhookConfiguration`'s `namespaceSelector` is `orbit.io/role: tenant` | No Orbit sidecar-injection label or annotation appears on a pod `vcluster-mcp` creates |
| C-4 | `charts/orbit-platform/templates/netpols.yaml` | Every `NetworkPolicy` this template renders is scoped to `orbit-system` (`$ns`); none is cluster-scoped | No `NetworkPolicy`-attributable connection denial appears against a `vcluster-mcp` pod or namespace |
| C-5 | `charts/orbit/templates/opa-gateway/envoyfilter.yaml` | The `ext_authz` `EnvoyFilter` is `workloadSelector`-scoped to Orbit's own gateway workload | `vcluster-mcp`'s own dedicated gateway workload is never matched by this `EnvoyFilter`; no Orbit-attributable `403` on its ingress |
| C-6 | This PRD; inline comments at the five sites above (Decision 2) | Not documented outside chart source until this ADR | N/A — delivered by Implementation step 1, not a live check |

---

## Implementation

1. **Reserved-label comments** (Decision 2): add the cross-referencing comment block to each of the five files in the table above. No behavior change.
2. **Stand up the co-tenant**: deploy `vcluster-mcp`'s operator and MCP server (pinned to [PR #5](https://github.com/clear-route/vcluster-mcp/pull/5)'s design) into the same local dev cluster Orbit's own platform install runs on (`make up`), per `vcluster-mcp`'s own deploy docs.
3. **Provision one vcluster** and inspect it against every row of the table above: pod labels/annotations for an unexpected Orbit sidecar, `kubectl get clusterspiffeid`/SPIRE server logs for any SVID issued into the co-tenant's namespace, the `MutatingWebhookConfiguration`'s effect (absence of Orbit's injected labels), `NetworkPolicy` events, and the ingress gateway's access logs for any Orbit-attributable denial.
4. **Repeat step 3 two more times** (three consecutive fresh provisions total), confirming no flakiness between runs.
5. **Confirm the reverse direction**: Orbit's own `orbit-system` and tenant-namespace workloads show no restarts, no degraded health, and no policy denials across the same window `vcluster-mcp` is active.
6. **If any row's audit finding doesn't hold live** — the only case this ADR calls for new code — fix the specific selector or template named in that row, re-run steps 3–5, and record the gap and fix in this ADR's Consequences before closing it out. Nothing broader than the specific row is in scope for that fix.
7. **Record the outcome** (clean pass, or gap found and fixed) in § Validation below before treating the PRD's P0 requirements as met.

No phased rollout: this is verification of behavior the audit already shows exists, not new functionality. A host install's rendered manifests are unaffected by steps 1–5; step 6 only changes anything if the live gate finds a real gap, and even then only the specific enforcement site involved.

---

## Consequences

### Positive

- The coexistence boundary this PRD needs becomes a documented, live-verified contract instead of an assumption resting on today's label choices happening to be scoped correctly.
- `vcluster-mcp` (or a future co-tenant) can read the reserved-label comments at each of the five enforcement sites, or the PRD's own table, without reading Orbit's Go/chart source to find the boundary.
- Zero new test infrastructure, CRD fields, or controller code, unless the live gate in step 6 finds an actual gap — consistent with the "Orbit's role here is coexistence, not orchestration" framing the PRD sets.

### Negative

- Verification is a manual, live-cluster procedure, not a `make check` target. A future change that widens one of the C-1…C-5 selectors from namespace-scoped to effectively cluster-wide will not be caught automatically between now and the next live re-verification (Decision 4). This is an accepted gap, not an oversight — see Decision 4 for the condition under which it gets revisited.
- The live-verification procedure depends on `vcluster-mcp`'s design staying close to its PR #5 shape; if `vcluster-mcp` changes its own deploy topology again, this ADR's step 2 needs re-pinning, not a full redo of the audit in Decision 1.

### Neutral

- The prior version of this ADR's decisions (new `OrbitPlatformSpec` fields, the `chartrender` harness, the dual-principal `opa-gateway` allow-list) remain fully superseded and are not revived by this revision.

---

## Validation

### Success Metrics

Mirrors the PRD's Success Metrics directly — this ADR does not add separate ones:

- Every C-row above is confirmed live, not just by inspection, with `vcluster-mcp`'s operator and a provisioned vcluster active on the same cluster.
- Zero Orbit-managed sidecar, SPIFFE identity, admission mutation, or network-policy denial ever touches a `vcluster-mcp`-owned namespace or pod, across three consecutive fresh provisions.
- Orbit's own workloads show no restarts, degraded health, or policy denials attributable to `vcluster-mcp`'s presence, across that same run.

**Outcome:** _Not yet run — record the result of Implementation steps 3–7 here once the live-verification gate has been executed._

### Monitoring

No new runtime monitoring — this is a verification-time contract, not a running-system behavior. If the live gate (or a future incident) ever finds one of the C-1…C-5 selectors has drifted, that is the trigger to revisit Decision 4 (an automated, no-cluster regression guard), not a reason to add monitoring here.

---

## Related Decisions

- [SPIFFE/SPIRE Workload Attestation](2026-03-05-spiffe-spire-workload-attestation.md) — the `ClusterSPIFFEID` model C-2 audits for correct namespace scoping
- [Dynamic SPIFFE Identity Grants and Tenant Isolation](2026-03-17-dynamic-spiffe-identity-grants-and-tenant-isolation.md) — the `orbit.io/role`-keyed identity-grant model C-1/C-2/C-3 all depend on
- [Orbit Kubernetes Operator](2026-03-31-orbit-kubernetes-operator.md) — `orbit-operator`'s admission webhook, the C-3 enforcement site
- [Service Mesh and Network Enforcement Infrastructure](2026-03-05-service-mesh-and-network-enforcement-infrastructure.md) — the mesh/gateway trust model C-1/C-5 confirm stays scoped to Orbit's own workloads
- [Kubernetes-Only Local Development](2026-07-10-kubernetes-only-local-development.md) — the local dev cluster (`make up`) this ADR's live-verification procedure runs against

---

## References

- [Devenv-Mode Platform Support PRD](../prd/devenv-mode-platform-support.md)
- [`vcluster-mcp`](https://github.com/clear-route/vcluster-mcp) — the co-tenant this contract is verified against; [PR #5](https://github.com/clear-route/vcluster-mcp/pull/5) is the design reference point
- This ADR's own prior revision (git history) — decided how to install Orbit's charts *inside* a provisioned vcluster; fully superseded by `vcluster-mcp`'s PR #5 dropping that design, and not reproduced here

---

## Notes

- C-6 (the reserved-label contract) needs no live verification — it's satisfied by Implementation step 1 alone.
- If the live-verification gate ever finds a real gap, fix only the specific enforcement site named in the relevant C-row (Implementation step 6) — this ADR does not authorize a broader redesign as a side effect of that fix.

**Last Updated:** 2026-08-05
