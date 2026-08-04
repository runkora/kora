# Devenv-Mode Platform Support - Product Requirements Document

**Status:** Draft
**Created:** 2026-08-03
**Last Updated:** 2026-08-03
**Owner:** Francis Holt
**Stakeholders:** Platform Engineering, `vcluster-mcp` maintainers, Security Review

---

## Executive Summary

**The aim of this work is to enable ephemeral dev environments of Orbit** — a disposable, fully working replica of the platform, provisioned on demand with a single tool call, so a platform engineer or an agent can test a real operator/CRD/chart change against a running instance instead of a shared staging cluster or a synthetic stand-in. Provisioning itself is not built here: it is delivered by `vcluster-mcp`, a standalone, Orbit-independent MCP server and Kubernetes operator hosted in a separate repository (`../vcluster`), which creates a vcluster per environment and installs Orbit inside it as a payload. This PRD covers Orbit's own side of that: what Orbit's charts and operator code must deliberately support for the environment `vcluster-mcp` provisions to actually come up — SPIRE disabled, identity host-provided instead of self-issued, sharing a mesh and an ingress gateway with the real platform install that also lives on the host cluster.

That support was captured once, then lost: PR #143 (`feat/vcluster-mcp-devenv-chart-support`), a 13-commit branch built the hard way — run `vcluster-mcp`'s operator against a real k3d cluster, hit a failure, read logs and certs until the cause is found, patch it, repeat — **was closed and its branch deleted before merging.** It is referenced throughout this document purely as the historical record of that implementation journey (what broke, and how it was fixed), not as code available to merge, rebase, or recover by reopening a PR. `main` has none of this support, which means ephemeral dev environments of Orbit cannot be provisioned from `main` today. Nothing in this repo enumerates what a "devenv-mode install" actually requires, so there was never a way to tell, short of another live run, whether that branch's fixes were complete enough to deliver the capability — and at least one of them (documented below) is already known, from that historical record, to have been a no-op against the label convention the real external consumer uses. This PRD defines devenv-mode support as an explicit, versioned, tested contract, implemented fresh against `main` and informed by PR #143's journey but not dependent on its code, so that enabling ephemeral dev environments doesn't depend on the next gap being found live on a real cluster.

---

## How Provisioning Works

**Context only — none of this is built, owned, or changed by this repo or this PRD.** Included so a reader doesn't need to go read `../vcluster`'s own docs to understand where Orbit fits into the flow it does not control.

```
Caller (Claude Code / Cursor / a CI bot, or a human driving one)
  │  MCP tool call: provision_environment
  ▼
vcluster-mcp's MCP server  (TS, ../vcluster, OAuth via Passport)
  │  simple CRUD: creates a DevEnvironment CR
  │  (vcluster-mcp's own CRD/API group — not Orbit's)
  ▼
vcluster-mcp's Go operator  (controller-runtime, ../vcluster)
  │  reconciles the CR:
  │   1. namespace + host SPIFFE identity grants
  │   2. a vcluster (a virtual Kubernetes cluster, running as a pod)
  │   3. installs Orbit INSIDE the vcluster, pulling Orbit's own published
  │      Helm charts + images from Orbit's registry as artifacts — no
  │      dependency on Orbit's source, same as any other Helm consumer
  │   4. sets the values that put that install into devenv mode:
  │      spire.enabled=false, platform.spiffe.enabled=true, shared-gateway
  │      ingress, etc. — exactly the values this PRD's Requirements target
  │   5. wires ingress, sleep/TTL/wake, issues a brokered kubeconfig on request
  ▼
Caller → browser (the nested Orbit's own login) and/or kubectl (brokered kubeconfig)
```

**Orbit's only role in this entire flow is step 3-4's payload**: a chart/operator that boots correctly when installed with devenv-mode values. Everything above the payload line — the CRD, the reconciler, the MCP tools, auth/quota/audit, sleep/TTL — is `vcluster-mcp`'s to design, build, and operate, and stays there per this PRD's Non-Goals. This document exists entirely to make that one payload role reliable and tested, rather than something re-discovered by running the flow above against a real cluster.

---

## Problem Statement

### Current State

- **`main` has zero devenv-mode support.** Every SPIRE, mesh, and identity code path in Orbit's operator and charts assumes a normal, SPIRE-enabled host install. Nothing gates, branches, or documents an alternative.
- **PR #143 (`feat/vcluster-mcp-devenv-chart-support`) was the accumulated fix set — since closed and deleted, and referenced here only as the historical record of that journey**, arrived at reactively across several live-debugging sessions against a real k3d cluster (documented at length in `../vcluster`'s own ADR): discoverySelectors excluding devenv namespaces, `ClusterSPIFFEID` template gating on `spire.enabled`, a gateway/istiod cert-trust mismatch, `orbit-identity`'s principal extractor not parsing devenv-prefixed SPIFFE IDs, an `opa-gateway` `AuthorizationPolicy` hardcoded to a SPIRE-only identity shape. Each commit fixed one symptom found live; none had a design or a test written before the bug was hit, and none of that code exists on any branch anymore — it must be reimplemented from this document's contract, not recovered.
- **No enumerated definition of "devenv-mode support" exists in this repo.** The only place that list exists at all is `../vcluster`'s own ADR — a living document in a different repository, cataloguing 23 "pinned findings," most of which are that repo's own responsibility but several of which land squarely in Orbit's code and charts. Nothing here tracks which of those findings Orbit owns, whether each has a fix, or whether that fix is tested.
- **That external document is already stale relative to what PR #143's final commits resolved, before the branch was deleted.** It still describes a browser-facing TLS-trust gap as unresolved; PR #143's most recent commits resolved it differently (making gateway SPIRE injection unconditional), superseding an earlier, abandoned approach — a fact recorded only in a commit message on that now-deleted branch, not cross-referenced anywhere, and preserved only in this document going forward.
- **At least one fix already believed complete is provably a no-op.** The (now-deleted) PR #143's `ClusterSPIFFEID` devenv exclusion (`charts/orbit-platform/templates/clusterSpiffeIDs.yaml`) checked `orbit.io/role: devenv` — Orbit's own label convention. `vcluster-mcp`'s actual namespaces carry `vcluster.clearroute.io/role: devenv` instead. The exclusion only ever worked live via a manual `kubectl patch` on the host cluster, which does not survive a fresh Istio/SPIRE install and was not what the shipped chart did.
- **None of this has automated verification below the level of a live cluster.** The chart-level fixes (`clusterSpiffeIDs.yaml`, `_helpers.tpl`, `envoyfilter.yaml`) have `helm lint` (syntax only) and were otherwise verified by direct `kubectl`/`curl` inspection of a running k3d cluster. Only the Go-level values-builder functions (`buildIstiodValues`, `buildGatewayValues`, `principal_extractor.go`) have unit tests.

### User Pain Points

- Whoever runs `vcluster-mcp`'s operator against a new Orbit release has no way to know whether that release still satisfies devenv-mode support without provisioning a real environment and watching what breaks — exactly the workflow this rebuild exists to stop.
- An Orbit maintainer reviewing PR #143 back when it was open (or reviewing any future change that touches SPIRE, mesh config, or identity code) had no checklist to review it against; "it worked, last time someone tested it live" was the only available signal — a gap this document exists to close, now that PR is closed and deleted and its code isn't there to inherit.
- A future Orbit change to `buildIstiodValues`, the `ClusterSPIFFEID` templates, or the principal extractor has no CI signal telling the author they've reintroduced a "this code assumes SPIRE is always enabled" bug — the exact class of bug found four separate times during `../vcluster`'s live testing.

### Business Impact

- `vcluster-mcp`'s entire value proposition — letting platform engineers and agents provision a disposable, real Orbit install on demand — depends on this support surface working every time, not "the last time it was live-tested." An unreviewed pile of live-debug patches is not a foundation either side can build on with confidence.
- The pattern already found (four distinct instances of "this chart/controller code assumes SPIRE is always on") strongly suggests more latent bugs exist in code neither repo has yet exercised on the devenv-mode path.

---

## Goals and Objectives

### Primary Goals

1. **Enable ephemeral dev environments of Orbit.** A platform engineer or agent can call `vcluster-mcp`, get back a disposable, isolated Orbit installation, and reach it in a browser and via `kubectl` — reliably, on `main`, without anyone needing to live-debug Orbit's charts against a real cluster first. This is the actual product outcome; every goal below is a means to it, scoped to Orbit's own side of the work.
2. **Reach and verify `Ready` end-to-end.** A devenv-mode `OrbitPlatform` install (SPIRE disabled, Istio-in-vcluster, shared-gateway ingress) reaches every application service `Running` with zero identity-race restarts, and its browser-facing app URL is reachable through the shared ingress gateway with a working login — verified live as a release gate, not assumed from code review.
3. **Enumerate the contract.** Produce an explicit, named list — in this repo — of every place Orbit's own code and charts must behave differently for a SPIRE-disabled, devenv-mode install versus a normal host install, cross-referenced to `../vcluster`'s pinned findings where one exists.
4. **Test the contract.** Every item in that list has an automated check in this repo — a Go unit test for controller/values-builder logic, or a rendered-values assertion for chart templates — that fails if the devenv-mode behavior regresses. No item's correctness may depend solely on a live cluster run.
5. **Close the known gap, not just the tested ones.** Fix the `ClusterSPIFFEID` devenv-exclusion no-op (§ above) as part of this work, since it is a known-broken item PR #143's (now-deleted) fix set got wrong before that branch was closed, not a hypothetical — and it is currently the thing standing between `main` and a working environment.
6. **Make the contract legible without cross-reading source.** A person or agent working in `../vcluster` should be able to tell what Orbit requires and provides from this repo's own docs/CRD schema, not by reading Orbit's Go source or re-deriving it from a live failure.

### Success Metrics

- Every finding from `../vcluster`'s ADR with an enforcement site in this repo (§ Requirements) has a named, passing automated test, listed in one place in this repo.
- The `ClusterSPIFFEID` devenv exclusion demonstrably excludes a namespace carrying `vcluster.clearroute.io/role: devenv` — Orbit's actual external consumer's real label — not just `orbit.io/role: devenv`.
- A fresh devenv-mode install reaches `Ready` with **zero pod restarts** from identity races across three consecutive fresh provisions (the same "not just won once" bar `../vcluster`'s own ADR sets for its live verification).
- Browser login against the devenv-mode install's seeded tenant succeeds end-to-end through the shared ingress gateway.
- The fresh implementation that replaces (deleted) PR #143 merges with every commit citing a named requirement from this document, not "found live, fixed live."

### Non-Goals

- **No dev-environment orchestration, CRD, or reconciler in Orbit.** Provisioning, sleep/wake, TTL, kubeconfig brokering, and the MCP tool surface belong entirely to `vcluster-mcp`, per the decision already made and already implemented (commit `7a758b2` removed Orbit's own prior attempt at this).
- **No change to Orbit's SPIRE-enabled, host-install behavior.** This is entirely about the alternate, SPIRE-disabled code path a nested/devenv install takes; the normal path is out of scope and must not regress.
- **Not re-opening `vcluster-mcp`'s own architecture decisions** (two-runtime split, its own CRD instead of Orbit's, Passport OAuth, Kubernetes-native quota/audit) — those are settled in that repo's own ADR and are treated here as fixed inputs.
- **Not building any of `vcluster-mcp`'s own missing pieces** (kubeconfig CSR signing, sleep/wake propagation, its MCP tool handlers, its own audit trail) — those live and are tracked entirely in that repo.
- **Not making Orbit's `ClusterSPIFFEID` devenv-exclusion label configurable (F-8).** Decided 2026-08-03: the fix is a dual-label change in `vcluster-mcp`'s own namespace-creation code, not a chart change here. Do not revisit this as part of this PRD's implementation.

---

## User Stories

### Primary User Stories

**As** `vcluster-mcp`'s operator (an automated system, not a human),
**I want** Orbit's SPIRE-disabled devenv-mode behavior to be documented and enforced by tests rather than discovered by trial and error,
**so that** provisioning a devenv-mode install against a new Orbit release doesn't require another live-debugging session against a real cluster.

**Acceptance Criteria:**
- [ ] A devenv-mode `OrbitPlatform` CR (`spire.enabled: false`) installs cleanly and reaches `Ready` without any manual, undocumented, non-chart-level host patch (the current `kubectl patch clusterspiffeid` workaround must be eliminated or turned into shipped chart/config behavior)
- [ ] Every value/field `vcluster-mcp`'s operator must set on the `OrbitPlatform` CR to get correct devenv-mode behavior is documented in this repo (CRD field comments or an explicit doc), not only inferable from Orbit's own Go source

---

**As** an Orbit maintainer changing SPIRE, mesh, or identity code,
**I want** a test that fails if my change breaks the devenv-mode path,
**so that** I don't silently reintroduce a "this code assumes SPIRE is always enabled" bug that only a different repo's live testing would ever catch.

**Acceptance Criteria:**
- [ ] `buildIstiodValues`, `buildGatewayValues`, the `ClusterSPIFFEID`/`_helpers.tpl` chart templates, and the `opa-gateway` `AuthorizationPolicy` template each have a test asserting their devenv-mode-specific behavior, runnable in `make check` with no cluster required
- [ ] `make check` fails if a devenv-mode-relevant code path is changed without an accompanying test update

---

**As** a platform engineer reviewing a PR that touches this support surface,
**I want** a single, current checklist mapping each fix to a still-relevant requirement,
**so that** I can tell whether the change set is complete rather than relying on "it worked when it was tested live."

**Acceptance Criteria:**
- [ ] This document's Requirements table is the canonical list; a PR touching an enforcement site cites the requirement it satisfies, mirroring the discipline `../vcluster`'s own ADR already applies to its 23 findings

### Secondary User Stories

**As** a developer working in `../vcluster`, **I want to** open one Orbit doc and know exactly what devenv-mode support Orbit guarantees today, **so that** I don't need to read Orbit's Go source or re-run a live k3d session to find out. (P1 — served by this doc plus its companion ADR.)

---

## Proposed Solution

### High-Level Approach

Treat "devenv-mode install" as a first-class, explicitly designed configuration of Orbit's existing `OrbitPlatform`/`OrbitTenant` install path — not a side effect discovered by patching whatever broke. Concretely: `spire.enabled: false` plus an identity that is still real, just host-provided rather than self-issued (`platform.spiffe.enabled: true`, distinct from `spire.enabled` — a distinction correctly established in PR #143's now-deleted implementation and preserved here, since that branch is gone, as this document's source of truth). Every Orbit-owned code path this mode touches gets: (1) an explicit branch keyed on that flag, (2) a test asserting both branches, and (3) a line in this doc's Requirements table naming it. The companion ADR (next step, not part of this PRD) decides *how* each item is implemented; this PRD fixes *what* must be true.

### Key Components

- **Identity (SPIFFE/SPIRE).** `ClusterSPIFFEID` template rendering gated on `spire.enabled`; the `istio-sidecar` cluster-wide identity grant must not double-match a devenv pod that already has its own host-granted identity — and the exclusion mechanism must actually match the label convention the real external consumer uses, not only Orbit's own.
- **Mesh (Istio-in-vcluster).** `discoverySelectors` excluding devenv namespaces on host installs; `ENABLE_NATIVE_SIDECARS` pinned via istiod's flat `env:` key; the shared ingress gateway included in SPIRE sidecar injection unconditionally, so it shares one real trust root with every other in-vcluster workload instead of trusting only istiod's self-signed CA.
- **Ingress reuse.** A devenv-mode install's browser-facing routes must ride the *same* shared `Gateway`/`frontendHost` path Orbit's own real platform install already uses and is already exempted from mesh-wide `ext_authz` enforcement for — not a second, separately hostnamed Gateway needing its own authorization carve-out. (This was already discovered and corrected on `vcluster-mcp`'s own side; Orbit's obligation is to keep that existing `frontendHost` bypass stable, since an external consumer now depends on it.)
- **Principal extraction.** `orbit-identity`'s XFCC/SPIFFE-ID parser must accept a `devenv/{id}/...` path prefix without ever collapsing it into a bare `system/...` or `tenant/...` principal. (Well-tested in PR #143's now-deleted implementation; carried forward here as a requirement to reimplement, not re-derived from scratch.)
- **Authorization policies.** Any Orbit chart template that hardcodes an expected SPIRE-issued principal shape (e.g. `opa-gateway`'s policy-bundle-fetch `AuthorizationPolicy`) must also accept the identity shape a SPIRE-disabled, host-provided-identity workload actually presents — including the fact that which shape a given pod gets is a genuine startup race, not a deterministic either/or, confirmed live across multiple provisions of the identical workload.

### What Changes From PR #143's Approach

PR #143 itself is closed and its branch deleted; this table compares against what its commits did while it was open, preserved here only as a historical record — none of it is a baseline that can be rebased or merged.

| Area | PR #143's approach (branch deleted; historical record only) | Target of this rebuild |
|---|---|---|
| Contract definition | Lived only in `../vcluster`'s ADR (external, already stale) | Enumerated and owned in this repo (§ Requirements), cross-referenced to that ADR |
| Chart-level verification | `helm lint` (syntax only) + live `kubectl`/`curl` inspection | Rendered-values assertions in this repo's test suite; no chart-level devenv behavior verified only live |
| `ClusterSPIFFEID` devenv exclusion | Hardcoded to `orbit.io/role: devenv`, provably not matched by the real consumer's namespace label, only ever worked via a manual host patch | Actually excludes the namespaces the real consumer creates, without a manual, non-shipped patch |
| Traceability | Commits referenced the bug found; no link to a stable requirement | Every enforcement-site change cites a named requirement in this doc |
| Coverage confidence | "Ready was reached, once, this session" | Three consecutive fresh provisions, zero identity-race restarts, as the release gate |

---

## Scope

### In Scope

- Enumerating every Orbit-owned enforcement site implied by `../vcluster`'s 23 pinned findings, plus every additional gap found during that repo's live testing that had an Orbit-side fix (gateway SPIRE injection, `opa-gateway` principal shapes, principal extractor devenv-prefix parsing)
- A named, tested requirement for each of those enforcement sites, verifiable in `make check` with no live cluster
- Fixing the currently-shipped-but-non-functional `ClusterSPIFFEID` devenv exclusion
- Live verification (three consecutive fresh provisions) as an explicit release gate, matching `../vcluster`'s own bar
- Documentation, in this repo, of what Orbit guarantees for a devenv-mode install, sufficient for `../vcluster` to consume without reading Go source

### Out of Scope

- Any dev-environment provisioning orchestration, CRD, or controller in Orbit
- Any change to SPIRE-enabled host-install behavior
- `vcluster-mcp`'s own operator, MCP server, CRD, auth/quota/audit model, or ingress design
- Multi-cluster or non-staging-cluster host targets

### Future Considerations

- An automated, cross-repo compatibility check (e.g. a `vcluster-mcp`-owned CI job that installs Orbit's latest published chart in devenv mode against a throwaway cluster) so a breaking Orbit change is caught before either side notices manually
- Extending this contract's test coverage into a reusable `helm template` unit-testing setup for Orbit's charts generally, beyond just the devenv-mode templates

---

## Requirements

### Carried-Forward Findings (Orbit-Owned Enforcement Sites)

Cross-referenced to `../vcluster`'s ADR numbering where a numbered finding exists. "Status" reflects the state of PR #143's final commits before that branch was closed and deleted, not `main` — PR #143 itself no longer exists to inspect; this table is the only surviving record of that state.

| # | Requirement | Enforcement site (this repo) | Status on PR #143 (final commits; branch since deleted) | Test |
|---|---|---|---|---|
| F-1 (#15) | `spire.enabled=false` must suppress every `ClusterSPIFFEID` template render entirely; `platform.spiffe.enabled` ("workloads carry SPIFFE identities") must stay distinct from `spire.enabled` ("this install runs its own SPIRE control plane") | `charts/orbit-platform/templates/clusterSpiffeIDs.yaml`; `buildOrbitPlatformSpiffeValues` | Done | Go unit test only; no chart-level render test |
| F-2 (#5/#10) | Host istiod's `meshConfig.discoverySelectors` excludes devenv namespaces; gated to host installs only | `buildIstiodValues` | Done | Go unit test |
| F-3 (#8) | `ENABLE_NATIVE_SIDECARS: "false"` set via istiod's flat `env:` key | `buildIstiodValues` | Done | Go unit test |
| F-4 (#9, in-vcluster half) | In-vcluster istiod's XDS auth via `JWT_RULE` (host issuer/JWKS, non-empty `audiences`) | `buildIstiodValues` (MeshJWTValidation) | Done | Go unit test |
| F-5 (#6) | Principal extraction strips an optional `devenv/{id}` prefix; a devenv path never collapses to a bare `system/...` or `tenant/...` principal | `backend/internal/identity/principal_extractor.go` | Done | Extensive Go unit tests |
| F-6 (#7) | Gateway/mesh sidecar injection gives the shared ingress gateway the same real trust root as every other in-vcluster workload, regardless of `spire.enabled` | `buildGatewayValues` | Done | Go unit test |
| F-7 (new — `opa-gateway` identity shape) | `opa-gateway`'s policy-bundle-fetch `AuthorizationPolicy` accepts every identity shape a SPIRE-disabled workload can actually present, including the genuine startup race between host-SPIRE delivery and istiod's own fallback CA | `charts/orbit/templates/_helpers.tpl` (`orbit.workloadPrincipals`) | Done | Not verified — no chart-level test exists |
| F-8 (#5, exclusion mechanism) | ~~Orbit-owned~~ **Resolved as an external fix, 2026-08-03 (§ Open Questions):** the `istio-sidecar` `ClusterSPIFFEID`'s devenv-namespace exclusion stays hardcoded to `orbit.io/role: devenv`; `vcluster-mcp` applies that label (alongside its own `vcluster.clearroute.io/role: devenv`) to every devenv namespace it creates, in its own `namespace.go`. Orbit's existing exclusion clause needs no chart change — this row is tracked here for traceability only, not as a requirement on this repo | `../vcluster`'s `operator/internal/controller/namespace.go` (external) | **Decided, not yet implemented** — `vcluster-mcp` still creates namespaces with only its own label; the dual-label change hasn't landed there yet | N/A to this repo; `vcluster-mcp`'s own test suite once it lands |
| F-9 (implicit — shared ingress) | Any devenv-mode-relevant install must be able to route browser traffic through Orbit's existing `frontendHost` `Gateway`/`ext_authz`-bypass path without a separate, unauthorized Gateway or policy carve-out | `charts/orbit/templates/opa-gateway/envoyfilter.yaml` (no devenv-specific code — the requirement is *stability* of the existing bypass) | Done (by design; no change needed) | Existing frontend-routing tests, not devenv-specific |
| F-10 (#17) | Orbit's chart must correctly support installing with `networkPolicy.enabled=false` | `charts/orbit` NetworkPolicy templates | Assumed working (generic toggle, not devenv-specific) | Not devenv-specifically verified |

### Functional Requirements

**Must Have (P0):**
- [ ] Every row in the table above marked "Broken" or "Not verified" gets a fix and/or a test before this work is considered done, except F-8, which is explicitly out of this repo's remediation scope (fix lands in `vcluster-mcp`)
- [ ] A devenv-mode install reaches `Ready` with zero identity-race pod restarts on three consecutive fresh provisions
- [ ] Browser login against a devenv-mode install's seeded tenant works end-to-end through the shared ingress gateway
- [ ] No devenv-mode behavior depends on an undocumented, non-chart-level, manual cluster patch that doesn't survive a fresh install

**Should Have (P1):**
- [ ] A documented, versioned statement (CRD field docs or a dedicated doc) of what Orbit guarantees for `spire.enabled: false` installs, consumable without reading Go source
- [ ] `make check` fails on a devenv-mode regression, not just a live cluster run

**Nice to Have (P2):**
- [ ] Chart-level (`helm template`-based) unit testing infrastructure generalized beyond just the devenv-mode templates

### Non-Functional Requirements

**Security:**
- Devenv-mode identity relaxations (SPIRE disabled) must never weaken host-install security — every branch keyed on `spire.enabled` must default to the SPIRE-enabled (stricter) behavior, and a host install must be provably unaffected by any devenv-mode-specific code path
- Any authorization-policy relaxation made to accommodate a SPIRE-disabled identity shape (F-7) must not widen what a host-install identity can do — it accepts an *additional* legitimate shape, never a wildcard

**Reliability:**
- Devenv-mode reconciliation must be idempotent and level-triggered, consistent with the rest of Orbit's operator conventions — no devenv-mode-specific code path may introduce a stateful assumption a normal reconcile doesn't already make

**Observability:**
- Any devenv-mode-specific behavior difference should be visible in the rendered Helm values or operator logs, not only inferable by diffing a working install against a broken one live

---

## Dependencies

### Internal Dependencies

- `backend/internal/operator/platform_controller.go` (`buildIstiodValues`, `buildGatewayValues`, `buildOrbitPlatformSpiffeValues`) — devenv-mode-aware in PR #143's now-deleted implementation; this work reimplements and completes that logic from scratch, informed by (not copied from) that historical record
- `backend/internal/identity/principal_extractor.go` — devenv-mode-aware and well-tested in PR #143's now-deleted implementation; same treatment
- `charts/orbit-platform`, `charts/orbit`, `charts/orbit-operator` — currently the least-tested layer of this support surface; the primary target of this work

### External Dependencies

- **`../vcluster` (`vcluster-mcp`)** — the sole consumer of this contract; its own ADR is the historical record of every finding this PRD formalizes. Its architecture decisions (CRD, auth model, orchestration) are inputs here, not renegotiated.
- **A host cluster running SPIRE + the SPIFFE CSI driver + an Istio control plane** — the precondition `../vcluster`'s own ADR already resolved (Orbit's own staging cluster / local dev stack); unchanged by this work.

---

## Risks and Mitigations

| Risk | Impact | Probability | Mitigation |
|------|--------|-------------|------------|
| `../vcluster`'s own design keeps moving (three branches already observed: `feat/vcluster-design`, `feat/vcluster-mcp-devenv-chart-support`-adjacent work, `feat/vcluster`) — this contract could target a design that's already superseded again by the time it's implemented | Medium | Medium | Pin this work to a specific, dated reference point in `../vcluster`'s own repo before starting the ADR; treat any further design change there as a follow-up, not silent scope creep here |
| F-8's fix lives entirely in `vcluster-mcp`; this repo has no way to enforce it lands, and no test here can catch it if it doesn't | Medium | Medium | Tracked in this doc for visibility; confirm with `vcluster-mcp` maintainers that the dual-label change is planned before treating this PRD's own work as sufficient for devenv-mode to actually work end-to-end |
| Chart-level test infrastructure doesn't exist in this repo today; adding it is nontrivial and could balloon scope | Medium | Medium | Scope the ADR to the minimum testing mechanism that covers the devenv-mode templates specifically (e.g. targeted `helm template` + assertion scripts), not a general chart-testing framework, unless the ADR finds one is trivially reusable |
| Two-repo coordination overhead is structural and this PRD can't fully remove it | Medium | High | This work makes Orbit's half legible and tested; a true cross-repo compatibility check is explicitly deferred to Future Considerations, not promised here |

---

## Alternatives Considered

### Alternative 1: Leave the contract informal, keep fixing bugs as `vcluster-mcp` finds them

**Why not chosen:** This is the status quo PR #143 represented while it was open — that PR is now closed and its branch deleted, which is itself evidence for why this alternative fails: an informal contract that lives only on a branch disappears with the branch. While open, it shipped one fix (F-8) on that branch that didn't work, discoverable only by comparing the chart's label key against the consumer's actual label — something a live run confirmed once and nothing since has re-checked, and which can no longer even be inspected on that branch now that it's gone. The pattern of "chart assumes SPIRE always enabled" recurred four times before the branch was deleted; nothing suggests it was exhausted. (F-8 itself is now resolved as a decision — the fix lands in `vcluster-mcp`, § Open Questions — but the fact that it shipped broken and undetected for this long, on a branch that was then deleted with no trace of the fix left anywhere else, is exactly the informal-contract failure mode this alternative would keep accepting.)

### Alternative 2: Move devenv-mode orchestration back into Orbit, so one team/repo owns the whole thing

**Why not chosen:** Already decided against, and already implemented — `7a758b2` removed Orbit's prior attempt at exactly this. `vcluster-mcp`'s own ADR gives a specific, still-valid reason (no Orbit source-code dependency for an Orbit-independent provisioning control plane). Re-litigating it is out of scope for this PRD.

### Alternative 3: A shared chart-testing framework across both repos before touching this contract

**Why not chosen:** Disproportionate to the immediate need. The devenv-mode templates are a small, known set; targeted rendered-values assertions for exactly those templates gets the coverage this PRD needs without a cross-repo tooling project as a prerequisite. Left as a Future Consideration if it turns out to generalize easily.

---

## Open Questions

1. **How should the `ClusterSPIFFEID` devenv-exclusion label mismatch (F-8) actually be fixed** — should Orbit's chart accept a configurable exclusion label/value (so any consumer can supply its own convention), or should the contract instead require consumers to also apply Orbit's own `orbit.io/role: devenv` label alongside their own?
   - **Status:** Resolved, 2026-08-03 — the change belongs in `vcluster-mcp`, not here. Its own namespace-creation code (`operator/internal/controller/namespace.go`) will stamp `orbit.io/role: devenv` on every devenv namespace alongside its own `vcluster.clearroute.io/role: devenv` label, so Orbit's existing, already-shipped exclusion clause works unmodified. This repo makes no chart change for F-8; the row stays in the Requirements table for traceability only, and this repo's Non-Goals now explicitly exclude reopening this decision.
   - **Owner:** Francis Holt (decision); implementation owner is whoever picks up the `vcluster-mcp` fix

2. **Should the devenv-mode contract be expressed as a doc, CRD field documentation, or an automated cross-repo compatibility test** — or some combination?
   - **Status:** Open — for the companion ADR
   - **Owner:** Francis Holt

3. **Is `helm lint` + targeted Go-level assertions on rendered values sufficient chart-level coverage, or does this actually need real `helm template` unit testing** (no such infrastructure exists in this repo today)?
   - **Status:** Open — for the companion ADR
   - **Owner:** Francis Holt

---

## Appendix

### Related Documents

- `../vcluster`'s [Ephemeral Dev Environments MCP Server PRD](../../../vcluster/docs/prd/vcluster-mcp.md) and [architecture ADR](../../../vcluster/docs/adr/2026-07-21-vcluster-mcp-server-architecture.md) — the external consumer's own design and the source of every pinned finding this PRD formalizes Orbit's side of
- PR #143 (`feat/vcluster-mcp-devenv-chart-support`) — **closed and deleted**; the reactive fix set it contained is referenced throughout this document only as the historical record of the implementation journey that motivated it, and is replaced here with a designed, tested contract implemented fresh against `main`
- The closed, superseded PR #113 (`feat/vcluster-design`) — Orbit's own earlier, in-repo attempt at dev-environment orchestration, since removed (`7a758b2`)

### Glossary

- **Devenv-mode install**: an `OrbitPlatform` install with `spire.enabled: false` but `platform.spiffe.enabled: true` — identity is real and host-provided, not self-issued by an in-cluster SPIRE server
- **Host-provided identity**: a workload's SPIFFE SVID issued by the *host* cluster's real SPIRE server via a CSI-synced socket, rather than by a SPIRE control plane running inside the vcluster itself
- **Pinned finding**: a load-bearing design constraint `../vcluster`'s ADR catalogued from live-debugging; this PRD's Requirements table restates the subset Orbit itself must enforce
- **Enforcement site**: the specific file/function in this repo responsible for a given requirement's correctness

---

**Approval Signatures:**

- [ ] Product Owner: _________________ Date: _______
- [ ] Technical Lead: _________________ Date: _______
- [ ] Security Review: _________________ Date: _______
