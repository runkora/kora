# Sandbox Environments

<!-- Filename: 2026-10-03-sandbox-environments.md  (decision date + slug; no sequential number) -->

**Status:** Accepted  
**Date:** 2026-10-03  
**Deciders:** Justin Wilkin  
**Technical Story:** [Sandbox Environments PRD](../prd/sandbox-environments.md)  

---

## Context

Every sandbox runs the image set in `agents.config.sandboxImage` (`charts/orbit/values.yaml`), and `AgentType` has no image field (`backend/internal/agents/models.go`). An environment holds only egress hosts and connectors (`backend/internal/agents/environments.go`), and an agent links to at most one environment through `agent_types.environment_id`.

`runfs.InstallEnv` (`backend/internal/agents/sandbox/runfs/runfs.go`) places `GOPATH`, `GOCACHE`, the npm and pip caches and tool installs under the run's HOME. In prod, HOME sits on the shared NFS workspace volume, and no cache survives the run. Prod runs show the cost:

*   Issue Verifier run `33c9f403` ran one Go package test for about 17 minutes without a result.
*   Coder run `5ae18ee2` spent about 24 minutes on `make build-backend`, mostly downloading modules.
*   The image lacked `buf`, and Coder run `5ae18ee2` spent about 15 minutes building it from source.

`backend/Dockerfile.sandbox-exec` builds the image with Node, Go and Python. The sandbox container sets no command and pulls its image with `PullAlways` (`backend/internal/agents/k8s.go`). In a tenant namespace, the operator's admission webhook injects Istio, identity-proxy and OPA into every pod and rejects pods that opt out.

### Goals

*   Platform teams register sandbox images per tenant through GitOps.
*   An agent attaches several environments and selects one per sandbox.
*   Build caches and tool installs sit on the sandbox's local disk.
*   No run writes state that another run reads.

### Non-Goals

*   Building images inside kora.
*   A package cache shared across runs. The decision waits for measurements after this change ships.
*   Sandbox benchmarks until a production-like environment exists.
*   Triggers or run starts that select the starting environment.
*   Moving existing environments into the new model.

---

## Decision Drivers

*   The agent and the sandbox must see the same source files.
*   An agent must know when it replaces its sandbox.
*   Tenant enforcement must stay unchanged for every pod in a tenant namespace.

---

## Considered Options

### D1. Where Sandbox Images Are Registered

*   **Option A (chosen):** Each tenant registers its own images in its namespace through GitOps.
*   **Option B (rejected):** The system namespace holds images for every tenant. Tenants could not hold different images.

### D2. Resource Shape

*   **Option A (chosen):** One resource registers one image.
*   **Option B (rejected):** One resource per tenant holds a map of images. Every image would share one status, and concurrent edits would conflict on one object.

### D3. Who Builds Images

*   **Option A (chosen):** Platform teams build images on the base image in their own pipelines.
*   **Option B (rejected):** kora builds images in the cluster from setup commands. It needs a builder and write access to a registry.

### D4. Image Validation

*   **Option A (chosen):** The operator reads the image config from the registry and checks the user and the base image label.
*   **Option B (rejected):** The operator starts a pod from the image and runs `sandbox-exec`. It needs one pod per image and version.

### D5. The Base Image in Each Tenant

*   **Option A (chosen):** The operator creates and owns a `SandboxImage` for the base image in every tenant namespace.
*   **Option B (rejected):** A chart warmer and `PullAlways` handle the base image only. The base image would take a second path.
*   **Option C (rejected):** orbit-agents resolves the base image digest itself and one system-wide warmer pulls it. orbit-agents would need a second registry client.

### D6. The Default Environment

*   **Option A (chosen):** The default environment sets only the default sandbox image, and nothing is stored for it.
*   **Option B (rejected):** orbit-agents reads the `orbit-tenants` ConfigMap and creates a stored default environment for each tenant on a timer. It needs a ConfigMap mount and a sync loop.
*   **Option C (rejected):** orbit-agents creates the stored default environment on first use. The default would not exist until first use.
*   **Option D (rejected):** The operator calls a new orbit-agents endpoint when it reconciles a tenant. It needs a new service client and a new mesh rule.

### D7. Where Warmers Run

*   **Option A (chosen):** Warmers run in the tenant namespace with the full sidecar injection.
*   **Option B (rejected):** Warmers run in the system namespace. Tenant pull secrets would be copied into the system namespace, and cleanup would need a finalizer.
*   **Option C (rejected):** The admission webhook exempts operator warmer pods. orbit-agents can set any label in a tenant namespace, and the exemption would weaken an enforcement point.

### D8. Warming Control

*   **Option A (chosen):** Each `SandboxImage` carries a `warm` flag that the platform team sets.
*   **Option B (rejected):** Every valid image is always warm. Every sandbox node would pull every registered image.
*   **Option C (rejected):** One cluster-wide mode turns all image warming on or off. The source records no reason.

### D9. Access with Several Environments

*   **Option A (chosen):** An agent holds the union of every attached environment.
*   **Option B (rejected):** A sandbox holds only the access of its current environment. The team that builds an agent decides its reach when it attaches environments.
*   **Option C (rejected):** Agent connectors apply to every sandbox, and environment egress applies to its own sandbox. The team that builds an agent decides its reach when it attaches environments.

### D10. Environment Size

*   **Option A (chosen):** Each environment sets its size within maximums that the platform sets.
*   **Option B (rejected):** Each environment sets CPU, memory and local disk without any maximum from the platform. The source records no reason.

### D11. Environment Selection

*   **Option A (chosen):** Separate tools list environments, provision a sandbox and replace the environment.
*   **Option B (rejected):** `provision_sandbox` takes an `environment` parameter and lists the environments in its description. A replacement would look the same as a first provision.

### D12. Fast Local Disk

*   **Option A (chosen):** Caches and tool installs sit on a sandbox-local volume, and source stays on the shared workspace.
*   **Option B (rejected):** Each new sandbox gets a copy of the workspace. The agent edits files outside the sandbox, and its edits would not reach the copy.
*   **Option C (rejected):** The agent pod and the sandbox share an EBS volume. An EBS volume attaches to one node, and sandboxes run in a different node pool from agent pods.

### D13. Repository-Declared Tool Versions

*   **Option A (chosen):** Platform teams add toolchains in their images.
*   **Option B (rejected):** A version manager in the base image installs the versions a repository declares. The base image packages only the tools agents use generally.

---

## Decision

Each tenant will register sandbox images as `SandboxImage` resources built on `kora-sandbox-base`, environments will combine one of them with a size, egress hosts and connectors, and agents will select environments through dedicated tools.

### 1. The Base Image

`backend/Dockerfile.sandbox-exec` will build the base image, published as `kora-sandbox-base:<version>`. It will keep the general command-line tools, Go and Python, drop Node, run as uid 100 and set the label `io.orbit.sandbox.base=<version>`. The sandbox container will run `/usr/local/bin/sandbox-exec` as its command.

### 2. The `SandboxImage` Resource

`SandboxImage` will be a namespaced `orbit.io/v1alpha1` resource (`charts/orbit-crds/templates/crd-sandboximage.yaml`) with these fields:

*   `spec.image`, `spec.description`, `spec.pullSecretRef.name` and `spec.warm`.
*   `status.image`, `status.digest` and the `Valid` and `Warm` conditions.

On every tenant reconcile, the operator will create and patch a `SandboxImage` named `kora-base` that points at `agents.config.sandboxImage`. Its `warm` flag will come from `agents.sandboxImages.warmBase`.

### 3. Validation

The operator will read the image config from the registry. An image will be valid when it runs as uid 100 and carries the `io.orbit.sandbox.base` label. An invalid image cannot be selected, and its `Valid` condition will state the reason. The operator will read the registry on every reconcile and pin the resolved digest. An image will stay valid while `status.image` matches `spec.image`, and a registry failure will keep its accepted digest. Provisioning against an image the operator has not yet read will return a retryable error.

### 4. Warming

For each valid image with `warm: true`, the operator will run a pre-pull DaemonSet in the tenant namespace that the `SandboxImage` owns. Sandboxes will reference images by digest with `IfNotPresent`. `agents.sandboxWarming.mode` will keep `none` and `capacity` and drop `image`, and the chart's single image warmer will be removed.

### 5. Environments

The `environments` table will gain image and size columns. An environment will have a name, a description, a `SandboxImage`, a size, egress hosts and connectors. An environment created without an image will store `kora-base`, and a legacy empty value will read as `kora-base`.

*   `agent_environments` will link agents to environments with an `is_default` flag.
*   `agent_types.environment_id` will stay unused for one minor release and then be removed under the [database migrations](2026-09-10-database-migrations.md) rule that each schema keeps working with the previous release's binaries.
*   An agent with no environments will provision on `kora-base` at the default size.
*   The size will set CPU, memory and local disk within maximums in the Helm values, and `GET /api/v1/environment-sizes` will return the default and maximum sizes.
*   The `agents:environments:read` and `agents:environments:write` scopes will govern environments.

### 6. Access

The effective grant will be the union of every attached environment and the agent's own scopes, compiled when the agent is reconciled.

### 7. Agent Tools

The runtime MCP server will expose `list_environments`, `provision_sandbox` with an optional environment, and `replace_environment`. Replacing an environment will end the sandbox's running commands and keep the workspace. A replacement will run through the provision path and share its retries and sandbox-lost handling. The run will record its current environment, and each start and replacement will emit an event with the environment and the image digest.

### 8. Local Disk

The sandbox will mount a local volume sized from the environment. `GOPATH`, `GOCACHE`, `GOMODCACHE`, the npm and pip caches and the npm prefix will point at it, and HOME, the repository checkout and `node_modules` will stay on the shared workspace.

### Rationale

None at this time.

---

## Consequences

### Positive

*   A moved image tag reaches new sandboxes within one operator sync period.
*   An image `CMD` has no effect on the sandbox.

### Negative

*   Replacing an environment discards its local caches.
*   An agent with several environments holds the access of all of them for the whole run.
*   Each warm image runs a pod with three sidecars and a SPIFFE identity on every sandbox node.
*   Existing environments stay as unattached base-image environments, and their agent links are dropped.
*   Validation does not run the image, and an image that replaces `sandbox-exec` passes validation and fails at sandbox start.

### Neutral

*   The capacity warmer is unchanged.
*   Every sandbox start still downloads its packages.
*   `node_modules` stays on NFS until measurements show otherwise.

---

## Implementation

*   `backend/Dockerfile.sandbox-exec`, `docker-bake.hcl` and `Makefile`: the `kora-sandbox-base` image.
*   `charts/orbit-crds/templates/crd-sandboximage.yaml` and `backend/internal/platform/sandboximages/v1alpha1/`: the `SandboxImage` resource and its types.
*   `backend/internal/operator/sandboximage_controller.go`, `sandboximage_registry.go` and `tenant_sandboximage.go`: validation, digest pinning, warmers and `kora-base`.
*   `backend/cmd/orbit-agents/migrations/20261003023303_sandbox_environments.sql`: the expand-only migration.
*   `backend/internal/agents/`: environments, attachments, the access union, environment selection, run events and the sandbox volume.
*   `backend/cmd/orbit-runtime-mcp/main.go` and `backend/internal/agents/sessionmanager/`: the `list_environments`, `provision_sandbox` and `replace_environment` tools.
*   `frontend/src/app/(dashboard)/environments/` and the agent builder and configuration tab: environment images and sizes, and several environments per agent.
*   `charts/orbit/values.yaml`: `agents.sandboxImages` and `agents.environments` with `defaultSize` and `maxSize`.
*   `docs/guides/deployment.md`: sandbox images and environments.

---

## Validation

On the local cluster:

*   `kora-base` and an image built on the base image report `Valid` with a pinned digest, and images that run as root or lack the base label report the reason.
*   Each warm image has one warmer DaemonSet, and deleting the `SandboxImage` removes it.
*   The API rejects a size above the maximum and an invalid image, and it refuses to delete an attached environment.
*   An agent with two environments receives the union of their egress hosts.
*   A Claude Code run lists its environments, provisions the default, and replaces it with the other environment. The workspace file survives, the custom image's marker disappears, and `GOCACHE` stays on the local volume.
*   Claude Code, Codex and OpenCode runs provision on the default environment.
*   The Environments page creates environments with images and sizes, and the agent builder attaches two environments with a default.

---

## Related Decisions

*   [Agent sandbox toolchain and connectivity environments](2026-06-14-agent-sandbox-toolchain-and-connectivity-environments.md) (superseded)
*   [Sandbox provisioning and shell proxy](2026-05-11-sandbox-provisioning-and-shell-proxy.md)
*   [Per-session home isolation](2026-06-04-per-session-home-isolation.md)
*   [Database migrations](2026-09-10-database-migrations.md)
*   [CRD-driven inference providers](2026-10-01-crd-inference-providers.md)

## References

*   [Sandbox Environments PRD](../prd/sandbox-environments.md)
