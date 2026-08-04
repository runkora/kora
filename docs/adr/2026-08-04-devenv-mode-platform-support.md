# Devenv-Mode Platform Support

**Status:** Proposed
**Date:** 2026-08-04
**Deciders:** Francis Holt
**Technical Story:** [Devenv-Mode Platform Support PRD](../prd/devenv-mode-platform-support.md)

---

## Context

`vcluster-mcp` (`../vcluster`) provisions a disposable, real Orbit install per dev environment by installing Orbit's own published charts inside a vcluster, with `spire.enabled=false` and `platform.spiffe.enabled=true` — identity is still real, just issued by the *host* cluster's SPIRE server instead of a SPIRE control plane running inside the vcluster. Orbit's only obligation in that flow is to boot correctly under those values; everything else (the CR, the reconciler, sleep/wake, ingress brokering) is `vcluster-mcp`'s.

That obligation was met once by PR #143 (`feat/vcluster-mcp-devenv-chart-support`), a 13-commit branch arrived at by running `vcluster-mcp`'s operator against a real k3d cluster and patching whatever broke — four separate times, all the same shape of bug: code that silently assumed SPIRE is always enabled. **That PR was closed and its branch deleted before merging.** It is used throughout this ADR only as the historical record of what broke and how it was fixed — none of its code exists on any branch today, so nothing below is a rebase; it is a fresh reimplementation informed by that record. `main` has none of this support. The PRD this ADR implements enumerates every Orbit-owned enforcement site that assumption touches (§ Requirements, F-1 through F-10) and requires each one to carry an automated test that runs with no cluster; this ADR decides *how* to rebuild and test that support from scratch.

Three things stop this from being a pure "write the tests" exercise:

1. The devenv-mode inputs PR #143's now-deleted branch introduced (`spec.devEnvironmentID`, `spec.meshJWTValidation`) don't exist on `OrbitPlatform` on `main` today, and can't be inherited from that branch since it's gone — they need a real, documented home on the CRD, built fresh, not just a values-map key threaded through by convention.
2. This repo has **no chart-level test infrastructure at all** — not even `helm-unittest` — so "a rendered-values assertion for chart templates" (PRD, Goal 4) has no mechanism to land in yet.
3. Two things the PRD's Requirements table depends on already have a decision made in the PRD itself and are *inputs* here, not re-litigated: F-8 (the `ClusterSPIFFEID` exclusion label mismatch) is fixed in `vcluster-mcp`'s own namespace code, and F-9 (shared ingress) needs no Orbit-side change at all.

**Identity note, since this touches SPIFFE parsing and an `AuthorizationPolicy`:** every decision below either keeps a devenv-mode workload's identity real and host-attested (never a claimed or self-asserted principal) or widens an authorization check to accept one *additional* legitimate identity shape, never a wildcard — see Decision 4 and the Security consequences below. A host install's authorization surface must be provably unaffected by any branch this ADR adds.

### Decision Drivers

- Every branch keyed on `spire.enabled` must default to host-install (stricter) behavior — a devenv-mode code path must be impossible to reach unless devenv-mode is explicitly requested.
- The test mechanism must run in `make check` with **no cluster**, per PRD Should-Have — this is the hard constraint the whole "chart-testing" decision (Decision 3) is scoped around.
- Minimum new machinery. The PRD's own Alternatives Considered already rejects a general cross-repo chart-testing framework as disproportionate; this ADR must not reintroduce that scope through the back door.
- The CRD is the only artifact `vcluster-mcp` actually reads. Whatever Orbit "guarantees" must be legible from `OrbitPlatformSpec`'s own Go doc comments, not rediscoverable only by reading `platform_controller.go`.

---

## Decision

Four sub-decisions. Each is scoped tightly to what PR #143's now-deleted branch already discovered live; none introduces new architecture beyond what's needed to make that discovery — preserved here as this document's contract since the branch itself is gone — tested and legible.

### Decision 1 — Devenv-mode is two new structured fields on the existing `OrbitPlatform` CRD, not a new CRD and not raw values

**Options considered:**

- **A. Raw values passthrough only** (`spec.platformValues`/`spec.orbitValues`, which already exist). Rejected: `vcluster-mcp` would have to know Orbit's internal Helm value paths (`platform.spiffe.devEnvID`, istiod's `JWT_RULE` shape) by convention, exactly the "re-derive it from source" failure the PRD's Goal 6 exists to end. It also can't be validated or defaulted by the CRD's OpenAPI schema.
- **B. A separate `OrbitDevEnvironment` CRD, cluster-side.** Rejected outright — this is Alternative 2 in the PRD, already decided against and already implemented (`7a758b2`). Not reopened here.
- **C. Two new structured, documented fields on `OrbitPlatformSpec`, consumed by the existing reconcile path.** Chosen.

**Decision:** Add to `backend/internal/operator/models.go`:

```go
// OrbitPlatformSpec defines the desired state.
type OrbitPlatformSpec struct {
    // ... existing fields unchanged ...

    // DevEnvironmentID marks this install as an ephemeral dev environment and
    // carries its identifier. Workload SVIDs are path-scoped under
    // /devenv/{DevEnvironmentID}/ instead of the bare host-trust-domain shape
    // (see backend/internal/identity/principal_extractor.go). Leave empty for
    // a normal host install. Set by whatever provisions the environment
    // (vcluster-mcp); never set by hand.
    // +optional
    DevEnvironmentID string `json:"devEnvironmentID,omitempty"`

    // MeshJWTValidation lets an in-vcluster istiod authenticate mesh
    // sidecars whose bound ServiceAccount tokens are signed by the HOST
    // cluster's issuer, which the virtual apiserver's own TokenReview can
    // never validate. Empty/disabled on a host install.
    // +optional
    MeshJWTValidation MeshJWTValidationSpec `json:"meshJWTValidation,omitempty"`
}

// MeshJWTValidationSpec configures istiod's JWT_RULE authenticator for a
// devenv-mode install. See buildIstiodValues in platform_controller.go.
type MeshJWTValidationSpec struct {
    // Enabled turns on the host-issuer JWT authenticator. Default: false.
    // +kubebuilder:default=false
    Enabled bool `json:"enabled"`

    // Issuer is the host cluster's OIDC issuer URL for bound ServiceAccount
    // tokens (JWT_RULE's "issuer").
    // +optional
    Issuer string `json:"issuer,omitempty"`

    // JWKS is the host issuer's JSON Web Key Set document, fetched by
    // whatever provisions the environment and handed to the operator as
    // plain text. Public key material only — never a credential.
    // +optional
    JWKS string `json:"jwks,omitempty"`
}
```

`SPIRESpec.Enabled` is unchanged and stays the sole switch for "does this cluster run its own SPIRE control plane" — it is **not** repurposed to also mean "this is a devenv install," per the distinction PR #143's now-deleted branch had already established correctly (`buildOrbitPlatformSpiffeValues`) and which F-1's requirement depends on. That distinction is reimplemented here from this document, not inherited from that branch.

**Rationale:** the CRD's own `+optional`/doc-comment schema is the one artifact `vcluster-mcp` reads without touching Orbit source, satisfying PRD Goal 6 and Acceptance Criterion "every value `vcluster-mcp` must set is documented in this repo." A dedicated field also lets validation (a devenv ID's shape, an empty JWKS with `Enabled: true`) live in the type rather than a convention.

### Decision 2 — The contract is the PRD's Requirements table plus these CRD doc comments; no separate contract document

**Options considered:**

- **A. A dedicated `docs/reference/devenv-mode.md`.** Rejected: a third place (PRD table, CRD comments, standalone doc) to keep in sync is exactly the drift this work exists to stop; the PRD's own Requirements table (F-1…F-10) is already the enumerated, named list PRD Goal 3 asks for.
- **B. CRD field docs only, no table.** Rejected: the CRD documents *what to set*, not *why each Orbit-side branch is safe or tested* — the table's per-row "enforcement site" and "test" columns have no equivalent home in a Go doc comment.
- **C. Both, cross-linked: the PRD Requirements table is canonical; `OrbitPlatformSpec`'s doc comments are the machine-facing mirror a consumer reads without opening this repo's docs at all.** Chosen.

**Decision:** No new document. The PRD's Requirements table (already written) is the single canonical checklist a PR cites against, per PRD Acceptance Criterion under "platform engineer reviewing a PR." `DevEnvironmentID` and `MeshJWTValidation`'s Go doc comments (Decision 1) are what `vcluster-mcp` actually reads day to day. This ADR's Decision 4 table is where "which test proves which requirement" lives, since that's an implementation artifact, not a product contract.

### Decision 3 — Chart-level verification is a targeted `helm template` + Go assertion harness, not a general chart-testing framework and not `helm-unittest`

**Options considered:**

- **A. `helm-unittest` (the Helm plugin).** Adds a YAML-based DSL and a plugin dependency for exactly three templates (`clusterSpiffeIDs.yaml`, `opa-gateway`'s `_helpers.tpl` principal helper, the gateway/istiod values). Rejected: a new test *language* and a plugin install step, for a test surface this small, when the repo's existing convention (Go, `pgx`, no third-party frameworks per `AGENTS.md`) already has everything needed to shell out to `helm template` and assert on the rendered YAML.
- **B. A general, reusable Helm-chart-unit-testing setup for all of Orbit's charts.** This is the PRD's own Alternative 3, already rejected there as disproportionate to the immediate need — restated here as rejected for this ADR too, and left as a Future Consideration.
- **C. A small `backend/internal/operator/chartrender` test helper: shells out to `helm template` against the real chart source with fixed values, parses the rendered manifests, and asserts on specific fields (presence/absence of a `ClusterSPIFFEID`, a `notPrincipals` entry, an `env` key).** Chosen.

**Decision:** Add a test-only helper package, `backend/internal/operator/chartrender/`, with one function:

```go
// Package chartrender renders a chart's templates via `helm template` against
// fixed values and returns the decoded manifests, for asserting on rendered
// YAML in tests without a live Kubernetes/Helm backend. Devenv-mode-specific
// use only — not a general chart-testing framework (see
// docs/adr/2026-08-04-devenv-mode-platform-support.md, Decision 3).
package chartrender

// Render runs `helm template <chartPath>` with the given values (marshaled
// to a temp values.yaml) and returns one decoded object per "---" document,
// keyed by "<kind>/<name>" for lookup in assertions.
func Render(t *testing.T, chartPath string, values map[string]any) map[string]*unstructured.Unstructured
```

Requires `helm` on `PATH` (already a listed dev-tool dependency per `make setup`); no new binary or plugin. Tests using it live beside the code that owns each template, e.g. `charts/orbit-platform/templates/clusterspiffeids_test.go` calling `chartrender.Render(t, "../../charts/orbit-platform", values)`.

**Rationale:** this satisfies PRD's Should-Have ("`make check` fails on a devenv-mode regression, not just a live cluster run") for the chart layer specifically, at the smallest footprint that covers F-1, F-7, and F-2/F-3 (`discoverySelectors`/native-sidecars are already Go-unit-testable since `buildIstiodValues` is pure Go — `chartrender` is only needed for templates the Go layer doesn't render itself). It resolves PRD Open Question 3 in favor of "targeted Go-level assertions on rendered values," using `helm template` rather than `helm lint` (syntax only) to actually assert on output.

### Decision 4 — F-7's `opa-gateway` identity-shape acceptance stays a dual-principal allow-list, confirmed by test, not narrowed to one shape

PR #143's `orbit.workloadPrincipals` helper (`charts/orbit/templates/_helpers.tpl`) already encoded the right answer, discovered live, before that branch was closed and deleted and its code lost with it: because SVID issuance for a SPIRE-disabled, in-vcluster workload is a genuine startup race between the host's CSI-delivered SVID and istiod's own per-ServiceAccount fallback cert — not a deterministic either/or — the `AuthorizationPolicy` must accept **both** plausible shapes (the SPIRE-style `/devenv/{id}/system/{component}` path, and istiod's fallback `*/sa/{serviceAccount}-x-{namespace}-x-vcluster` suffix match), never just one.

**Decision:** reimplement this same approach fresh — it cannot literally be carried forward, since PR #143's branch is deleted and none of its code is available to pull from — and require F-7's test (Decision 4 table below) to assert **both** shapes are matched — a test that only checks the SPIRE-style shape would pass today and still leave the intermittent 403 that PR #143's historical record shows was found live. This is not a narrower or wider grant than either individual shape already carries: it is an allow-list of two specific, fully-qualified principal strings, never a wildcard beyond the single suffix-match Istio's own principal matcher requires (documented in that now-lost helper's own comment, restated here) — so a host install, which never sets `devEnvID`, cannot reach either branch.

---

## Requirements → Enforcement → Test (Decision 4 continued)

Restates the PRD's Requirements table with the concrete test artifact each row gets, per Decision 3. This is the implementation checklist; the PRD table remains the canonical requirement list.

| # | Enforcement site | Test artifact | Mechanism |
|---|---|---|---|
| F-1 | `clusterSpiffeIDs.yaml` template gate (`{{- if .Values.spire.enabled }}`) | `charts/orbit-platform/templates/clusterspiffeids_test.go` | `chartrender.Render` with `spire.enabled: false` asserts zero `ClusterSPIFFEID` objects render |
| F-1 (values distinction) | `buildOrbitPlatformSpiffeValues` | `backend/internal/operator/platform_controller_test.go` | Go unit test: `spireEnabled != enabled` when `DevEnvironmentID` set and `SPIRE.Enabled=false` |
| F-2 | `buildIstiodValues` (`discoverySelectors`) | `platform_controller_test.go` | Go unit test: selector present iff `SPIRE.Enabled=true`; absent for in-vcluster istiod |
| F-3 | `buildIstiodValues` (`env.ENABLE_NATIVE_SIDECARS`) | `platform_controller_test.go` | Go unit test: key present under top-level `env`, never under `pilot.env` |
| F-4 | `buildIstiodValues` (`MeshJWTValidation` → `JWT_RULE`) | `platform_controller_test.go` | Go unit test: non-empty `audiences`; rejects `Enabled: true` with empty `Issuer`/`JWKS` at the reconcile-validation layer, not silently render an unauthenticatable rule |
| F-5 | `principal_extractor.go` (`stripDevEnvPrefix`) | `principal_extractor_test.go` (extensive coverage existed in PR #143's now-deleted branch; reimplement equivalent coverage fresh) | Go unit test: devenv-prefixed path never collapses to bare `system/...`/`tenant/...`; malformed prefix errors rather than falling through |
| F-6 | `buildGatewayValues` (`inject.istio.io/templates: gateway,spire`, unconditional) | `platform_controller_test.go` | Go unit test: `spire` template present regardless of `SPIRE.Enabled` |
| F-7 | `orbit.workloadPrincipals` (`_helpers.tpl`) | `charts/orbit/templates/opa_gateway_authz_test.go` | `chartrender.Render` asserts **both** the SPIRE-style and istiod-fallback principal strings appear in the rendered `AuthorizationPolicy`, only when `spireEnabled: false` |
| F-8 | *(external — `vcluster-mcp`'s `namespace.go`)* | none in this repo | Traceability only, per PRD; no chart change, no test here |
| F-9 | *(no code — stability requirement)* | existing frontend-routing tests | No devenv-specific test; regression coverage is the existing `frontendHost` bypass suite |
| F-10 | `charts/orbit` NetworkPolicy templates (`networkPolicy.enabled`) | `charts/orbit/templates/networkpolicy_test.go` | `chartrender.Render` asserts zero `NetworkPolicy` objects render when `networkPolicy.enabled: false` |

---

## Implementation

Ordered so each step is independently mergeable and citable against a specific requirement, per PRD's traceability goal:

1. **CRD fields** (Decision 1): add `DevEnvironmentID` and `MeshJWTValidation` to `OrbitPlatformSpec` in `models.go`; regenerate CRD manifests (`make manifests` or the operator's existing codegen target); no reconcile behavior change yet.
2. **`chartrender` harness** (Decision 3): the `Render` helper, plus a `go.mod` dev-tool check confirming `helm` is on `PATH` (mirrors how other `make check` steps already probe for external binaries).
3. **Reimplement the Go-layer changes fresh against `main`**, against the new CRD fields — `buildIstiodValues`, `buildGatewayValues`, `buildOrbitPlatformSpiffeValues`, `principal_extractor.go`'s `stripDevEnvPrefix`/`parsePrincipalPath` split. This is not a rebase: PR #143 is closed and its branch deleted, so none of that code is available to pull from — each function is written new, grounded in this ADR's Decisions and the behavior PR #143's historical record documented, citing F-1 through F-6 in the commit messages instead of "found live, fixed live."
4. **Reimplement the chart changes fresh** — add the `{{- if .Values.spire.enabled }}` gate to `clusterSpiffeIDs.yaml` (this does not exist on `main` today; PR #143 added it, but that branch is deleted, so it is written new here per F-1), the `orbit.workloadPrincipals` helper in `_helpers.tpl`, and the `opa-gateway` `AuthorizationPolicy` template consuming it — and write the three `chartrender`-based tests (F-1, F-7, F-10).
5. **Confirm F-8 externally**: before treating this repo's work as sufficient, get `vcluster-mcp` to confirm the dual-label change (`namespace.go` stamping `orbit.io/role: devenv` alongside its own label) has landed — this repo has no test that can catch its absence.
6. **Live verification gate**: three consecutive fresh `vcluster-mcp` provisions against this branch's chart/image build, zero identity-race pod restarts, working browser login — the PRD's Success Metric, run once before merge, not part of `make check`.
7. **Merge**, each commit's message citing its requirement (F-1…F-10) per PRD Acceptance Criterion.

No phased rollout beyond this: devenv-mode is additive and gated entirely on fields that default to zero-value/off, so a host install's rendered manifests are byte-for-byte unaffected by steps 1–4 landing.

---

## Consequences

### Positive

- Every enforcement site in the PRD's Requirements table has a test that fails with no cluster, closing the PRD's central gap: "found live, fixed live" becomes "asserted in `make check`."
- `DevEnvironmentID`/`MeshJWTValidation` becoming real, documented CRD fields means `vcluster-mcp` (or any future consumer) can read `OrbitPlatformSpec`'s own doc comments instead of Orbit's reconciler source — PRD Goal 6.
- The `chartrender` harness, though scoped to three templates today, is a real (if minimal) chart-unit-testing capability that didn't exist in this repo before — directly usable by the PRD's own Future Consideration ("extending this contract's test coverage into a reusable chart-testing setup") without redesign.

### Negative

- `helm` becomes a hard dependency of `make check` wherever `chartrender`-based tests run, not just of cluster-facing `make` targets. Acceptable: it's already a `make setup` dependency for every engineer running the cluster.
- The dual-principal allow-list in F-7 is intentionally not resolved to a single deterministic shape — a genuine race in `vcluster-mcp`'s own SVID delivery remains undiagnosed there. This repo's test only proves both shapes are *accepted*, not that the race itself is understood; a future fix on `vcluster-mcp`'s side could change or remove one shape without this repo's test catching the mismatch until the next live provision.
- F-8 stays a dependency this repo cannot enforce or test (unchanged from the PRD's own risk register) — the live-verification gate (step 6) is the only backstop, and it only catches an F-8 regression at merge time, not continuously.

### Neutral

- `SPIRESpec.Enabled` keeps its existing single meaning ("this cluster runs its own SPIRE control plane"); it does not grow a second, devenv-flavored meaning. `DevEnvironmentID` is the sole devenv-mode signal, which is a small but deliberate widening of the CRD's surface area for a capability most host installs will never set.

---

## Validation

### Success Metrics

Mirrors the PRD's Success Metrics directly — this ADR does not add separate ones:

- Every F-row above with an enforcement site in this repo has a named, passing test, runnable with no cluster.
- The `ClusterSPIFFEID` devenv exclusion demonstrably excludes `vcluster.clearroute.io/role: devenv` namespaces once F-8 lands externally (verified live, not by this repo's own tests).
- Three consecutive fresh provisions reach `Ready` with zero identity-race pod restarts.
- Browser login succeeds end-to-end through the shared ingress gateway.

### Monitoring

No new runtime monitoring — this is a build-time/test-time contract, not a running-system behavior. The existing PR-review discipline (a PR touching an enforcement site cites its F-row) is the ongoing check that the contract doesn't silently drift, per PRD Acceptance Criteria.

---

## Related Decisions

- [SPIFFE/SPIRE Workload Attestation](2026-03-05-spiffe-spire-workload-attestation.md) — the SPIFFE URI scheme `principal_extractor.go` and the `ClusterSPIFFEID` templates both extend for the `/devenv/{id}` prefix
- [Dynamic SPIFFE Identity Grants and Tenant Isolation](2026-03-17-dynamic-spiffe-identity-grants-and-tenant-isolation.md) — the `ClusterSPIFFEID`-per-workload-class model F-1/F-8's exclusion selector is part of
- [Orbit Kubernetes Operator](2026-03-31-orbit-kubernetes-operator.md) — `OrbitPlatformSpec`/`PlatformReconciler`, extended here with the two new fields
- [Service Mesh and Network Enforcement Infrastructure](2026-03-05-service-mesh-and-network-enforcement-infrastructure.md) — the mesh/gateway trust model F-6/F-4 keep consistent across host and devenv-mode installs
- [Kubernetes-Only Local Development](2026-07-10-kubernetes-only-local-development.md) — establishes that all local Orbit development, including this one, runs against a real cluster; this ADR's live-verification gate follows the same bar

---

## References

- [Devenv-Mode Platform Support PRD](../prd/devenv-mode-platform-support.md)
- `../vcluster`'s own architecture ADR and PRD — the external consumer's design and the source of every pinned finding this ADR formalizes Orbit's side of (see PRD Appendix for exact paths)
- PR #143, `feat/vcluster-mcp-devenv-chart-support` — **closed and deleted**; referenced throughout this ADR only as the historical record of the implementation journey (what broke, and how it was fixed) that this ADR's Decisions and Implementation section reimplement from scratch, not a branch available to rebase or merge

---

## Notes

- F-8 and F-9 need no work in this repo; they are listed above only so the requirement → test table is complete against the PRD's own numbering.
- The `chartrender` harness is deliberately not generalized beyond the three templates in the table above. If a future feature needs broader chart-template coverage, that is new scope for a new ADR, not an extension of this one — consistent with the PRD's own Future Considerations.

**Last Updated:** 2026-08-04
