# CI Pipeline and Local Build Parallelisation

**Status:** Proposed
**Date:** 2026-07-21
**Deciders:** Justin Wilkin
**Technical Story:** Backend CI takes 10+ minutes wall clock; local full rebuilds are serial by design and no longer need to be on current hardware.

---

## Context

Backend CI wall clock is ~10 minutes per push. Job-level timings from a representative run (29805284979) break down as:

| Job | Duration | Dominant cost |
| --- | --- | --- |
| Build | 2m48s | Cold-cache compile of the `make build-backend` binaries |
| Lint | 3m36s | `go vet` alone is 2m22s |
| Test | 7m13s (starts only after Build) | `make test` step is 6m23s |

Three findings drive this decision:

1. **The Test job is gated on Build for no reason.** `needs: build` serialises the two jobs, but `go test` compiles its own artifacts and shares nothing with the Build job's output. The gate adds Build's full duration to the critical path.
2. **The 6m23s test step is compilation, not tests.** Summing the per-package `ok` lines shows ~90s of actual test execution (the slowest package, `internal/identity`, is 18.5s; most are ~1s). The rest is compiling 82 packages with `-race` and atomic coverage instrumentation against an effectively cold build cache. `actions/setup-go` keys its cache on `go.sum` alone and only saves on a key miss, so the cache is a snapshot of the tree at the last dependency change and decays as source evolves under an unchanged `go.sum`.
3. **The standalone `go vet` step is redundant.** `.golangci.yml` sets `govet: enable-all: true`, so golangci-lint already runs everything `go vet` runs, plus more. The separate step pays a full compile (2m22s) to duplicate a 13s check.

The frontend gate had the same shape in miniature: one job ran ~45s of static checks (lint, type check, format) serially ahead of ~75s of vitest suites, with no dependency between the two groups.

Locally, `redeploy-platform` builds all 13 platform images serially, with a comment recording why: parallel builds overloaded the previous development machine's Colima VM and cascaded into mesh and Postgres failures. The current machine has 18 CPUs and 64 GiB RAM, with Colima allocated 8 CPUs / 16 GiB. The serial constraint is a workaround for hardware that no longer exists, but it is still the right default for lower-spec machines.

A bake-based build path already exists (`docker-bake.hcl` + `docker-bake.local.hcl`, driven by `redeploy-platform-bake`) but has drifted: `docker-bake.local.hcl` is missing the `egress-gateway`, `kora-mcp`, and `tool-gateway` targets, and the push loop derives image names as `orbit-$svc`, which produces `orbit-orbit-mcp` for the `kora-mcp` service.

### Problem Statement

CI feedback is 10 minutes when the work it does justifies about 3, and local full rebuilds are serial on hardware that can comfortably parallelise them.

### Goals

- Backend CI wall clock at or under ~4 minutes warm.
- A supported, opt-in parallel build path for local development on high-spec machines.
- No weakening of the quality gate: everything checked today is still checked.

### Non-Goals

- Larger or self-hosted CI runners (cost decision, separate conversation).
- Changing what `make check` runs locally.
- Making parallel builds the local default; low-spec machines keep working unchanged.

---

## Decision Drivers

* CI minutes are the team's tightest feedback loop; every push pays the full cost.
* The quality gate must stay byte-for-byte equivalent in what it checks.
* Local defaults must stay safe for the lowest-spec machine on the team, per the recorded Colima failure history.
* Prefer configuration over new infrastructure.

---

## Considered Options

### Option 1: Remove the job gate, fix cache keying, drop the redundant vet step; opt-in parallel bake locally

**Description:** Three CI changes: drop `needs: build` from Test, replace setup-go's stale-by-design cache with an explicit `actions/cache` using per-commit keys and prefix restore-keys (so every run restores the nearest warm cache and saves an updated one), and delete the standalone `go vet` step. Locally, gate a single-invocation `docker buildx bake` over the full platform group behind `KORA_PARALLEL_BUILDS=1` in `.env`, repairing the drifted bake overlay in the process.

**Pros:**
- Attacks the measured costs directly; no new infrastructure or spend.
- BuildKit deduplicates the shared `go mod download` and source-copy layers across all 13 images in one bake graph, which serial `docker build` loops cannot do.
- Opt-in flag preserves the safe serial default.

**Cons:**
- Per-commit cache keys churn GitHub's 10 GB cache quota; old entries evict LRU.
- Two build paths (serial loop and bake graph) to keep in sync when services are added.

### Option 2: Larger GitHub-hosted runners

**Description:** Move backend jobs to 8- or 16-core runners.

**Pros:**
- No workflow restructuring; compile times drop roughly linearly.

**Cons:**
- Recurring per-minute cost multiplier on every push forever.
- Does nothing about the wasted work (redundant vet, serialised jobs, cold caches); it just pays for it faster.

### Option 3: Trim the gate itself (drop `-race` or coverage on PRs)

**Description:** Run PR tests without `-race`/coverage and keep the full matrix on main only.

**Pros:**
- Largest possible test-step reduction.

**Cons:**
- Race detection on PRs is exactly where it earns its keep; finding a race after merge is strictly worse.
- Weakens the gate, which is a non-goal.

---

## Decision

**Chosen Option:** Option 1 - Remove the gate, fix caching, drop redundant vet; opt-in parallel bake locally.

### Rationale

The measured waste is structural, not compute-bound: a false job dependency, a cache that decays by design, and a duplicated check. Fixing those costs nothing recurring and keeps the gate identical. The trade-off is cache-quota churn and a second build path to maintain, both cheap next to a permanent runner bill or a weaker gate. Locally, the bake graph is the only approach that deduplicates shared layers across images, and the opt-in flag keeps the serial default that low-spec machines depend on.

---

## Consequences

### Positive

- Backend CI critical path drops from ~10m to roughly max(lint, test) ≈ 3–4m warm.
- Full local platform rebuilds on high-spec machines drop substantially; shared compile work happens once per bake instead of once per image.
- The drifted bake overlay is repaired and covered by the same service list as the serial path.

### Negative

- Per-commit cache keys across three backend key families increase GitHub Actions cache churn against the repo's 10 GB quota; eviction costs an occasional cold-ish run.
- A fully cold lint run costs ~6m (inside the 15m ceiling). This recurs on cache eviction and on any `.golangci.yml` change.
- `docker-bake.local.hcl` must be updated alongside `REDEPLOY_PLATFORM_SERVICES` when a service is added; drift between them breaks the opt-in path only.
- The frontend split pays a second ~25s job setup in runner minutes on every push.

### Neutral

- `make check` still runs `vet` locally; it is cached and cheap there. Only CI drops the duplicate.
- Colima VM sizing stays a per-machine choice, documented in the local development guide rather than enforced.

---

## Implementation

- `.github/workflows/backend-ci.yml`: remove `needs: build`; explicit `actions/cache` in Build, Test, and Lint with per-job key prefixes for `~/.cache/go-build`, plus a single module cache (`~/go/pkg/mod`) shared by all three jobs and keyed on `go.sum` alone, since bundling the module cache into the per-commit keys stored the same content three times per commit; delete the `go vet` step.
- The Lint job's cache additionally includes `~/.cache/golangci-lint` (the analysis cache), and the golangci-lint action runs with `skip-cache: true` so its built-in 7-day rotating cache does not double-save the same directory. Each job gets its own key family (`go-build-`, `go-test-`, `go-lint-`) because the three compile with different flag sets and would overwrite each other's saves.
- `.golangci.yml`: `run.timeout` raised from 5m to 15m. A fully cold lint run (no compile cache, no analysis cache) takes ~6m, and `actions/cache` does not save on job failure, so a timeout failure left every subsequent run cold and failing the same way. The ceiling breaks that loop; warm runs finish in about a minute and never approach it. The cold cost recurs only on cache eviction or a `.golangci.yml` change, since golangci keys its analysis cache on the config hash.
- `.github/workflows/frontend-ci.yml`: static checks (lint, type check, format) split into their own `frontend-static` job so the vitest suites no longer wait on them.
- `Makefile`: `redeploy-platform` dispatches to the bake path when `KORA_PARALLEL_BUILDS=1`; `redeploy-platform-bake` becomes a single bake invocation of the `local-platform` group with corrected image-name push loop and the mkcert CA layer preserved.
- `docker-bake.local.hcl`: add `kora-egress-gateway`, `kora-mcp`, `kora-tool-gateway`; extend the `local-platform` group.
- `.env.example`: document `KORA_PARALLEL_BUILDS=1` and recommended Colima sizing for high-spec machines.
- `docs/contributing/local-development.md`: parallel-builds section under the inner-loop guide.

---

## Validation

### Measured Results

Warm-cache timings from the implementing PR (#126), against the pre-change baseline:

| Job | Baseline | Warm |
| --- | --- | --- |
| Backend Build | 2m48s | 0m46s |
| Backend Lint | 3m36s | 1m07s |
| Backend Test | 7m13s (gated on Build) | 1m31s |
| Frontend Static Checks | (inside Frontend Check) | 1m07s |
| Frontend Check | 2m32s | 1m43s |
| **Total wall clock** | **10m07s** | **~1m50s** |

The warm Test figure benefits from Go's test-result cache; a change touching many packages recompiles and re-runs those suites, so expect 1.5–3m in practice. The first run after merge (and after any cache eviction) is cold once, then stays warm.

### Success Metrics

- Backend CI wall clock ≤ 4 minutes on a warm cache (measured: under 2 minutes).
- `KORA_PARALLEL_BUILDS=1 make redeploy-platform` completes a full rebuild faster than the serial loop on the reference machine, with all 13 images pushed and rolled correctly. Not yet measured; needs a resized Docker VM.

### Monitoring

- CI job durations in the Actions UI; regressions are visible per push.
- `make redeploy-status` confirms cluster images match the registry after a parallel rebuild.

---

## Related Decisions

- [kubernetes-only-local-development](2026-07-10-kubernetes-only-local-development.md) - Defines the single local environment this build pipeline feeds.
- [multi-service-backend-deployment-structure](2026-03-19-multi-service-backend-deployment-structure.md) - The multi-binary model that makes shared-layer deduplication valuable.

---

**Last Updated:** 2026-07-21
