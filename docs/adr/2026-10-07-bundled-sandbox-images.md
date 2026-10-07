# Bundled Sandbox Images

<!-- Filename: 2026-10-07-bundled-sandbox-images.md  (decision date + slug; no sequential number) -->

**Status:** Accepted  
**Date:** 2026-10-07  
**Deciders:** Justin Wilkin  
**Technical Story:** [Bundled Sandbox Images PRD](../prd/bundled-sandbox-images.md)  

---

## Context

The change touches _How does it connect?_ by adding images that sandboxes run in. Identity, policy and egress for a sandbox stay as they are for any registered image.

`backend/Dockerfile.sandbox-exec` builds `kora-sandbox-base`. The image carries `curl`, `wget`, `build-essential`, `git`, `jq`, `ripgrep`, `fd` and `gh` (lines 86-98), Python with pip, venv and PyYAML (lines 100-105), and `uv` (lines 107-109). Build args gate the other runtimes (lines 24-32). Go, `kubectl` and `kustomize` default to on, and Bun, Rust, Java, .NET, Ruby and the browsers default to off. The image has no Node. It sets `LABEL io.orbit.sandbox.base=$SANDBOX_BASE_VERSION` (lines 61-62) and runs as `USER 100:100` (line 308).

`docker-bake.hcl` defines `kora-sandbox-base` as a target (lines 175-190) with `SANDBOX_BASE_VERSION = IMAGE_TAG` and the tag `${REGISTRY}/${IMAGE_NAMESPACE}/kora-sandbox-base:${IMAGE_TAG}`, and lists it in `group "default"` (lines 226-239). `.github/workflows/images.yml` reads the targets from `docker buildx bake --print` (lines 37-40) and builds each one in its own matrix job (lines 49-52). `.github/workflows/release.yml` calls it on a `v*` tag with `image_tag: ${{ github.ref_name }}` and `platforms: linux/amd64,linux/arm64` (lines 26-30). The base builder cross-compiles `sandbox-exec` for the target architecture, and the runtime stage selects downloads by `dpkg --print-architecture` (`backend/Dockerfile.sandbox-exec` lines 49-54, 143).

`.github/scripts/stamp-release-charts.sh` writes `images.txt` from the chart's images and from every tag that `docker buildx bake --print -f docker-bake.hcl` lists (lines 48-53). `.github/scripts/sign-release.sh` signs each reference in `images.txt` under the release namespace (lines 13-17). A new bake target is listed and signed without a change to either script.

The chart renders the base image reference in `orbit.sandboxBaseImage` (`charts/orbit/templates/_helpers.tpl` line 146) from `agents.config.sandboxImage` (`charts/orbit/values.yaml` line 509), or from `<global.imageRegistry>/kora-sandbox-base:<global.imageTag or appVersion>`. The operator config carries it as `sandboxImages.baseImage` with `warmBase` from `agents.sandboxImages.warmBase` (`charts/orbit/values.yaml` line 629, `charts/orbit/templates/operator/config.yaml` lines 40-42). `backend/cmd/operator/main.go` line 319 passes both to `TenantReconciler.SandboxBase` (`backend/internal/operator/tenant_controller.go` line 59). On every tenant reconcile, `ensureBaseSandboxImage` creates or patches the `SandboxImage` `kora-base` and replaces its whole spec (`backend/internal/operator/tenant_sandboximage.go` lines 21-43, `tenant_controller.go` line 179). `kora-base` carries the label `app.kubernetes.io/managed-by: orbit-operator` from `tenantPolicyLabels` (`tenant_sandboximage.go` line 48, `backend/internal/operator/tenant_agent_egress.go` lines 151-153).

A `SandboxImage` spec has `image`, `description`, `pullSecretRef` and `warm` (`backend/internal/platform/sandboximages/v1alpha1/types.go` lines 51-57, `charts/orbit-crds/templates/crd-sandboximage.yaml`). `validateImageConfig` rejects an image whose user is not uid 100 with `WrongUser` and an image without the `io.orbit.sandbox.base` label with `MissingBaseLabel` (`backend/internal/operator/sandboximage_registry.go` lines 131-139). A warm image's pre-pull DaemonSet carries a controller reference to its `SandboxImage` (`backend/internal/operator/sandboximage_controller.go` line 224).

`Makefile` lines 26-33 pin kora's development tools: `GITLEAKS_VERSION`, `GOLANGCI_LINT_VERSION`, `NODE_VERSION`, `PNPM_VERSION`, `PROTOC_GEN_GO_VERSION`, `PROTOC_GEN_GO_GRPC_VERSION`, `BUF_VERSION` and `DEADCODE_VERSION`. `backend/go.mod` line 3 sets `go 1.26.0`, and the base image defaults `GO_VERSION` to `1.26.0` (`backend/Dockerfile.sandbox-exec` line 11). `Makefile` runs `docker buildx bake` with `-f $(DOCKER_BAKE_FILE)` (lines 55, 505 and 514) and with `-f docker-bake.hcl -f docker-bake.local.hcl` (line 613), and `images.yml` passes `docker-bake.hcl` to `bake --print` and to `docker/bake-action` (lines 38 and 81).

Measured with empty caches on 2026-10-07 (Go 1.26.4, pnpm 10.32.1):

*   `go mod download` in `backend/` fills a 1.13 GiB module cache (378 MB with zstd).
*   `pnpm fetch` against `frontend/pnpm-lock.yaml` fills an 884 MiB store (208 MB with zstd).
*   A `GOOS=linux go build ./...` in `backend/` fills a 2.0 GiB build cache (399 MB with zstd).
*   `kora-sandbox-base:v0.7.0-rc.3` is 346 MB compressed on amd64 and 330 MB on arm64.

A run does not read caches at their default paths in the image. `runfs.InstallEnv` sets `GOMODCACHE`, `GOCACHE` and the npm paths to the run-local volume (`backend/internal/agents/sandbox/runfs/runfs.go` lines 44-60), sandbox-exec moves `HOME` into the run's workspace (`backend/cmd/sandbox-exec/main.go` lines 270-287), and the sandbox root filesystem is read-only (`backend/internal/agents/k8s.go` line 1570). `pnpm install --frozen-lockfile --offline` against a pre-fetched store on a read-only mount fails with `EROFS` on `/store/v10/projects/`. pnpm writes to its store on every install.

### Goals

*   kora ships Go, Python and Node sandbox images built on `kora-sandbox-base` and released with it.
*   Platform teams choose which bundled images every tenant receives and which of them stay warm.
*   kora publishes `kora-dev` with kora's development tools at the versions the repository pins.

### Non-Goals

*   Changes to `SandboxImage` validation, warming or environments.
*   Registering `kora-dev` through the chart.

---

## Decision Drivers

*   A bundled image must pass the existing `SandboxImage` validation without changes to it.
*   Disabling a bundled image must leave no `SandboxImage` for it in any tenant.

---

## Considered Options

### D1. Where the Images Are Built

*   **Same repository and pipeline (chosen):** The images live in this repository and are built, versioned, signed and released with `kora-sandbox-base` in the same pipeline.
*   **Separate images repository (rejected):** A separate images repository builds and releases the images. The base label version would need coordination across repositories, and the images would need a second release and signing pipeline.

### D2. What the Language Images Add

*   **Toolchain extras on the base (chosen):** `kora-sandbox-base` keeps Go and Python as common scripting languages, and each language image adds toolchain extras.
*   **Runtimes out of the base (rejected):** Go and Python move out of the base image into the language images. Agents would lose the common scripting languages in the default environment.

### D3. How Tenants Receive the Images

*   **Chart-registered images (chosen):** The chart registers each bundled image in every tenant with `enabled` and `warm` values per image.
*   **Documented examples only (rejected):** kora documents example images that platform teams build themselves. Every organization would rebuild the same images.

### D4. Where `kora-dev` Is Registered

*   **Outside the chart (chosen):** `kora-dev` is published with the other images and stays out of the chart.
*   **In the chart (rejected):** The chart registers `kora-dev` in every tenant. The image serves kora development and does not serve tenants.

### D5. What `kora-dev` Pre-Caches

*   **Tools only (chosen):** `kora-dev` carries the pinned tools and no dependencies.
*   **Modules and pnpm store at their default paths (rejected):** Runs would bypass both caches and still download every dependency.
*   **Go modules through a `file://` GOPROXY (rejected for now):** The image holds the module download cache, and `GOPROXY=file:///<cache>/cache/download,https://proxy.golang.org,direct` copies modules into the run-local `GOMODCACHE` without a network fetch. It adds about 378 MB compressed to the image, and the download time it saves is not measured yet.
*   **pnpm store seeded at sandbox start (rejected for now):** sandbox-exec copies a store from the image into the run-local volume and points pnpm at it. It needs a change to sandbox-exec, and every `kora-dev` sandbox start copies about 884 MiB of small files.
*   **Modules, pnpm store and Go build cache (rejected):** `kora-dev` also carries a Go build cache. The image would grow to about four times the base image, and the build cache is the first cache to go stale.

The two options rejected for now can be revisited if measured download times justify them.

---

## Decision

kora will build `kora-go`, `kora-python`, `kora-node` and `kora-dev` on `kora-sandbox-base` in the existing image pipeline, and the chart will register the three language images in every tenant with per-image `enabled` and `warm` values.

### 1. Images

Each image will have its own Dockerfile that builds `FROM` `kora-sandbox-base`. A Dockerfile that installs as root will end with `USER 100:100`. Each image inherits the `io.orbit.sandbox.base` label from the base. Each tool version will be pinned in a build arg in the same way as the base image.

The initial toolchain set for each language image is a proposal:

*   **`kora-go`:** `golangci-lint`, `buf` and `protoc`.
*   **`kora-python`:** `ruff` and `pytest` on top of the `uv` that the base image already installs.
*   **`kora-node`:** The current Node LTS release and `pnpm`.

`kora-dev` will install `golangci-lint`, `buf`, `protoc-gen-go`, `protoc-gen-go-grpc`, `gitleaks`, `deadcode`, Node and `pnpm` at the versions in a shared versions file. It will use the Go from the base image. It will not carry a Go module cache, a pnpm store or a Go build cache. `kora-dev` will build on `kora-sandbox-node` and copy `golangci-lint` and `buf` from `kora-sandbox-go` to keep each pin and digest in one Dockerfile.

### 2. The Shared Versions File

The tool pins in `Makefile` lines 26-33 and `GO_VERSION` will move to `versions.hcl` at the repository root. Each line will be an HCL attribute with a quoted value:

```hcl
GO_VERSION = "1.26.0"
GOLANGCI_LINT_VERSION = "v2.11.3"
NODE_VERSION = "24"
```

*   `docker buildx bake` will read the file as a second definition file (`-f docker-bake.hcl -f versions.hcl`). A top-level attribute sets the bake variable of the same name, and the `kora-sandbox-dev` target will pass each variable as a build arg.
*   The `Makefile` will `include versions.hcl` and strip the quotes from each value with `patsubst`.
*   The `Makefile` bake targets and both `images.yml` bake calls will add `-f versions.hcl`.
*   The `kora-sandbox-base` target will set its `GO_VERSION` build arg from the bake variable, and `kora-dev` will use the same Go.
*   A new `make check-versions` target will fail when `GO_VERSION` in `versions.hcl` differs from the `go` directive in `backend/go.mod`. `make check` will run it.
*   An environment variable of the same name still overrides a bake variable.

### 3. Names, Tags and Architectures

*   The image repositories will be `kora-sandbox-go`, `kora-sandbox-python`, `kora-sandbox-node` and `kora-sandbox-dev` under `${REGISTRY}/${IMAGE_NAMESPACE}`.
*   The chart will register the language images as the `SandboxImage` resources `kora-go`, `kora-python` and `kora-node`. kora's own deployment will register `kora-dev`.
*   Every image will be tagged `${IMAGE_TAG}`, the kora release tag. The inherited base label carries the same version.
*   Every image will build for `linux/amd64` and `linux/arm64` with the base image.

### 4. Build and Release

`docker-bake.hcl` will gain one target per image and list each one in `group "default"`. Each target will set `contexts = { kora-sandbox-base = "target:kora-sandbox-base" }`. Each matrix job in `images.yml` will then build its image on a base from the same commit without waiting on a pushed base tag.

The four new bake targets put the sandbox images in `images.txt`, and the release signs them with the other release images. The chart does not pin the digests of sandbox images. The operator resolves each tag to a digest when it validates the `SandboxImage`, and sandboxes pull that digest. `kora-base` works the same way.

### 5. Chart and Operator

`agents.sandboxImages` will gain one entry per language image. Every bundled image is enabled by default, and `warm` defaults to `false`:

```yaml
agents:
  sandboxImages:
    warmBase: false
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

*   A non-empty `image` replaces the image reference in the same way as `agents.config.sandboxImage`. An empty `image` renders `<global.imageRegistry>/kora-sandbox-<language>:<global.imageTag or appVersion>` in the same way as `orbit.sandboxBaseImage`.
*   The operator config will carry the image reference, `enabled` and `warm` for each bundled image.
*   `backend/internal/platform/sandboximages/v1alpha1/types.go` will hold the bundled `SandboxImage` names and descriptions next to `BaseName` and `BaseDescription`.
*   The operator will create each bundled `SandboxImage` with the labels `app.kubernetes.io/managed-by: orbit-operator` and `orbit.io/component: bundled-sandbox-image`. It will patch or delete only a `SandboxImage` that carries both labels. `backend/internal/operator/tenant_pullsecrets.go` lines 16-19 pair the same two labels for the same reason.
*   On every tenant reconcile, the operator will create or patch the `SandboxImage` of each enabled bundled image and delete the `SandboxImage` of each disabled bundled image.
*   If a tenant has registered its own `SandboxImage` under a bundled name without both labels, the operator leaves it unchanged and does not register the bundled image in that tenant. It logs one message at info level with the tenant, the namespace and the name, and the tenant reconcile continues.
*   Kubernetes garbage collection removes the warmer DaemonSet of a deleted `SandboxImage` through its controller reference.

Tenants keep registering their own `SandboxImage` resources alongside `kora-base` and the bundled images.

### 6. Environments on a Disabled Image

An environment keeps its stored `SandboxImage` name when the platform team disables the image. The current code then behaves as follows:

1.  `RunService.sandboxSpec` calls `usableSandboxImage` (`backend/internal/agents/run_environments.go` line 126). The lookup returns `ErrNotFound`, and `usableSandboxImage` returns `sandbox image unavailable: sandbox image "kora-go" is not registered` (`backend/internal/agents/environments.go` lines 382-386).
2.  The provision handler answers `ErrSandboxImageUnavailable` with 409 Conflict and the error message (`backend/internal/agents/server.go` lines 969-976).
3.  The session-manager maps the 409 to `ErrEnvironmentRejected` (`backend/internal/agents/sessionmanager/notifier.go` lines 203-208). It does not retry (`guardrails.go` lines 144-147), does not mark the sandbox lost (`session.go` lines 525-527), and records the failed sandbox phase with the message (`session.go` lines 410-418). The agent's `provision_sandbox` or `replace_environment` call returns the message.
4.  A running sandbox keeps running. Its pod references the image by digest and is owned by the agent StatefulSet (`backend/internal/agents/k8s.go` line 459).
5.  An edit to the environment fails validation with 400 until the environment selects a registered image (`backend/internal/agents/environments.go` lines 372-378).

Enabling the image again recreates the `SandboxImage` with the same name, and the environment works again once the operator validates it.

### 7. `kora-dev` Registration

kora's own deployment will register `kora-dev` as a `SandboxImage` with `warm: true` in its tenant namespace through its GitOps repository. Each sandbox node then pulls the image once.

### Rationale

None at this time.

---

## Consequences

### Positive

None at this time.

### Negative

*   Every release builds four more images, and each of their matrix jobs also builds the base image.
*   Every enabled bundled image adds one `SandboxImage` to each tenant, and every warm one adds a pre-pull DaemonSet to each tenant.
*   A tenant that registers its own `SandboxImage` under a bundled name does not receive that bundled image.
*   Environments on a disabled bundled image cannot start a sandbox until the image is enabled again or the environment selects another image.
*   Every `kora-dev` run downloads kora's Go modules and frontend packages.

### Neutral

*   `kora-base` and `agents.config.sandboxImage` are unchanged. The operator still patches `kora-base` without checking its labels.
*   The `Makefile` tool pins move to `versions.hcl`.

---

## Implementation

*   `backend/` Dockerfiles for `kora-sandbox-go`, `kora-sandbox-python`, `kora-sandbox-node` and `kora-sandbox-dev`.
*   `versions.hcl`, `Makefile` and `.github/workflows/images.yml`: the shared tool versions and the `check-versions` target.
*   `docker-bake.hcl`: the four targets and their `group "default"` entries.
*   `charts/orbit/values.yaml`, `charts/orbit/templates/_helpers.tpl` and `charts/orbit/templates/operator/config.yaml`: `agents.sandboxImages.bundled` and the image references.
*   `backend/cmd/operator/config/config.go` and `backend/cmd/operator/main.go`: the bundled image config.
*   `backend/internal/platform/sandboximages/v1alpha1/types.go` and `backend/internal/operator/tenant_sandboximage.go`: the bundled names, and the create, patch and delete steps.
*   `docs/guides/deployment.md`: the bundled images and their values.

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

**Last Updated:** 2026-10-07
