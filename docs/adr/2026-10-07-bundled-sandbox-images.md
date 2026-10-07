# Bundled Sandbox Images

**Status:** Accepted  
**Date:** 2026-10-07  
**Deciders:** Justin Wilkin  
**Technical Story:** [Bundled Sandbox Images PRD](../prd/bundled-sandbox-images.md)  

---

## Context

The change touches _How does it connect?_ by adding images that sandboxes run in. Identity, policy and egress for a sandbox stay as they are for any registered image.

kora ships one sandbox image, `kora-sandbox-base`, built from `backend/Dockerfile.sandbox-exec`. It carries general command-line tools, Go and Python, and has no Node. The operator registers it in every tenant as the `SandboxImage` `kora-base` (`backend/internal/operator/tenant_sandboximage.go`). A platform team that needs another toolchain builds an image on the base in its own pipeline and registers it per tenant.

The operator accepts a `SandboxImage` only when the image runs as uid 100 and carries the `io.orbit.sandbox.base` label (`backend/internal/operator/sandboximage_registry.go`). `docker-bake.hcl` defines every published image, and the release signs every image it lists (`.github/scripts/stamp-release-charts.sh`).

A run keeps its build caches on its own volume. `runfs.InstallEnv` points `GOMODCACHE`, `GOCACHE` and the npm paths at the run-local volume, sandbox-exec moves `HOME` into the run's workspace, and the sandbox root filesystem is read-only. pnpm also writes to its store on every install. A dependency cache baked into an image at its default path is therefore never used. Measured with empty caches on 2026-10-07:

*   `go mod download` in `backend/` fills a 1.13 GiB module cache (378 MB with zstd). In a sandbox it takes about 38 s.
*   `pnpm install --frozen-lockfile` in `frontend/` fills an 884 MiB store (208 MB with zstd). In a sandbox it takes about 27 s.
*   A Go build cache for `backend/` is 2.0 GiB (399 MB with zstd).
*   `kora-sandbox-base` is about 340 MB compressed.

### Goals

*   kora ships Go, Python and Node sandbox images built on `kora-sandbox-base` and released with it.
*   Platform teams choose which bundled images every tenant receives and which of them stay warm.
*   kora publishes `kora-dev` with kora's development tools at the versions the repository pins.

### Non-Goals

*   Changes to `SandboxImage` validation, warming or environments.

---

## Decision Drivers

None at this time.

---

## Considered Options

### D1. Where the Images Are Built

*   **Same repository and pipeline (chosen):** The images live in this repository and are built, versioned, signed and released with `kora-sandbox-base`.
*   **Separate images repository (rejected):** A separate repository builds and releases the images. The base label version would need coordination across repositories, and the images would need a second release and signing pipeline.

### D2. What the Language Images Add

*   **Toolchain extras on the base (chosen):** `kora-sandbox-base` keeps Go and Python as common scripting languages, and each language image adds its toolchain extras.
*   **Runtimes out of the base (rejected):** Go and Python move into the language images. Agents would lose the common scripting languages in the default environment.

### D3. How Tenants Receive the Images

*   **Chart-registered images (chosen):** The chart registers each bundled image in every tenant with per-image `enabled` and `warm` values.
*   **Documented examples only (rejected):** kora documents example images that platform teams build themselves. Every organization would rebuild the same images.

### D4. Where `kora-dev` Is Registered

*   **Outside the chart (chosen):** `kora-dev` is published with the other images and stays out of the chart.
*   **In the chart (rejected):** Every tenant would receive `kora-dev`. The image serves kora development and does not serve tenants.

### D5. What `kora-dev` Pre-Caches

*   **Tools only (chosen):** `kora-dev` carries the pinned tools and no dependencies.
*   **Caches at their default paths (rejected):** Runs do not read them and still download every dependency.
*   **Go modules through a `file://` GOPROXY (rejected):** The image holds the module download cache, and Go copies modules from it into the run-local cache. It adds about 378 MB compressed to save about 38 s per run.
*   **pnpm store seeded at sandbox start (rejected):** sandbox-exec copies a store from the image into the run-local volume. It needs a change to sandbox-exec, and every `kora-dev` sandbox start copies about 884 MiB of small files.
*   **Modules, pnpm store and Go build cache (rejected):** The image grows to about four times the base, and the build cache goes stale first.

---

## Decision

kora will build `kora-go`, `kora-python`, `kora-node` and `kora-dev` on `kora-sandbox-base` in the existing image pipeline, and the chart will register the three language images in every tenant.

### 1. Images

Each image has its own Dockerfile in `backend/` that builds `FROM kora-sandbox-base`, installs as root and ends with `USER 100:100`. Each image inherits the base label.

*   **`kora-go`:** `golangci-lint`, `buf` and `protoc`.
*   **`kora-python`:** `ruff` and `pytest` on top of the `uv` in the base.
*   **`kora-node`:** Node 24 LTS and `pnpm`.
*   **`kora-dev`:** builds on `kora-sandbox-node`, copies `golangci-lint` and `buf` from `kora-sandbox-go`, and adds `protoc-gen-go`, `protoc-gen-go-grpc`, `deadcode`, `gitleaks` and `helm`. It carries no dependency caches.

### 2. Tool Versions

The tool versions and `GO_VERSION` move from the `Makefile` to `versions.hcl` at the repository root. The `Makefile` and `docker buildx bake` both read it. `kora-sandbox-node` pins its full Node release in its Dockerfile. `make check-versions` fails when `GO_VERSION` differs from the `go` directive in `backend/go.mod`, and `make check` runs it.

### 3. Names, Tags and Architectures

The image repositories are `kora-sandbox-go`, `kora-sandbox-python`, `kora-sandbox-node` and `kora-sandbox-dev`. The registered `SandboxImage` names are `kora-go`, `kora-python`, `kora-node` and `kora-dev`. Every image is tagged with the kora release and builds for `linux/amd64` and `linux/arm64`. Each bake target builds on the base from the same commit through `contexts = { kora-sandbox-base = "target:kora-sandbox-base" }`.

### 4. Chart Values

Every bundled image is enabled by default and not warm:

```yaml
agents:
  sandboxImages:
    bundled:
      go:
        enabled: true
        warm: false
        image: ""
      python:
        enabled: true
        warm: false
        image: ""
      node:
        enabled: true
        warm: false
        image: ""
```

A non-empty `image` replaces the reference, the same as `agents.config.sandboxImage`. The chart does not pin digests. The operator resolves each tag to a digest when it validates the `SandboxImage`.

### 5. Operator

On every tenant reconcile, the operator creates or patches each enabled bundled `SandboxImage` and deletes each disabled one. It labels them `app.kubernetes.io/managed-by: orbit-operator` and `orbit.io/component: bundled-sandbox-image`, and it changes only a `SandboxImage` that carries both labels. If a tenant registered its own `SandboxImage` under a bundled name, the operator leaves it in place, does not register the bundled image in that tenant, and logs the conflict at debug level. A failure on one image does not stop the others.

### 6. Disabled Images

An environment keeps its image name when the image is disabled. Provisioning a sandbox in it returns a 409 that names the unregistered image. Running sandboxes keep running, and edits to the environment fail validation until it selects another image.

### 7. `kora-dev` Registration

kora's own deployment registers `kora-dev` with `warm: true` through its GitOps repository.

### Rationale

None at this time.

---

## Consequences

### Positive

*   A new installation offers Go, Python and Node sandboxes without an image pipeline of its own.
*   The release signs the bundled images with the other images.
*   `versions.hcl` keeps the `Makefile`, the base image and `kora-dev` on one set of tool versions.

### Negative

*   Every release builds four more images.
*   Every enabled bundled image adds a `SandboxImage` to each tenant, and every warm one adds a pre-pull DaemonSet.
*   Every `kora-dev` run downloads kora's Go modules and frontend packages.
*   A tenant that registered an image under a bundled name does not receive that bundled image.

### Neutral

*   `kora-base` and `agents.config.sandboxImage` are unchanged.

---

## Implementation

*   Dockerfiles: `backend/Dockerfile.sandbox-go`, `Dockerfile.sandbox-python`, `Dockerfile.sandbox-node` and `Dockerfile.sandbox-dev`.
*   Build: `versions.hcl`, `docker-bake.hcl` and `docker-bake.local.hcl`.
*   Chart: `charts/orbit/values.yaml` and `charts/orbit/templates/operator/config.yaml`.
*   Operator: `backend/internal/operator/tenant_sandboximage.go`.

---

## Validation

None at this time.

---

## Related Decisions

*   [Sandbox environments](2026-10-03-sandbox-environments.md)
*   [Install and distribution model](2026-09-04-install-and-distribution-model.md)

## References

*   [Sandbox Environments PRD](../prd/sandbox-environments.md)

---

**Last Updated:** 2026-10-08
