# Sandbox Environments

<!-- Filename: 2026-10-03-sandbox-environments.md  (decision date + slug; no sequential number) -->

**Status:** Proposed  
**Date:** 2026-10-03  
**Deciders:** Justin Wilkin  
**Technical Story:** [Sandbox Environments PRD](../prd/sandbox-environments.md). Environments become prepackaged sandboxes built on a kora base image.

---

## Context

Every sandbox runs the image set in `agents.config.sandboxImage` (`charts/orbit/values.yaml`). `AgentType` has no image field (`backend/internal/agents/models.go`). An environment holds egress hosts and connectors only (`backend/internal/agents/environments.go`). An agent links to at most one environment through `agent_types.environment_id`.

`runfs.InstallEnv` (`backend/internal/agents/sandbox/runfs/runfs.go`) places `GOPATH`, `GOCACHE`, the npm and pip caches and tool installs under the run's HOME. HOME sits on the shared workspace volume. The workspace volume is NFS in prod. No cache survives the run.

Prod runs show the cost:

*   Issue Verifier run `33c9f403` ran one Go package test for about 17 minutes without a result.
*   Coder run `5ae18ee2` took about 24 minutes for `make build-backend`, mostly module downloads.
*   Coder run `5ae18ee2` spent about 15 minutes building `buf` from source. The image did not include it.

The sandbox image is built from `backend/Dockerfile.sandbox-exec` with Node, Go and Python. Heavier runtimes are build arguments that no published image sets. The image is pulled with `PullAlways` (`backend/internal/agents/k8s.go`).

### Goals

*   Platform teams register sandbox images per tenant through GitOps.
*   An agent attaches several environments and selects one per sandbox.
*   Build caches and tool installs sit on fast local disk.
*   No run writes state that another run reads.

### Non-Goals

*   Building images inside kora.
*   A package cache shared across runs. The decision waits for measurements after this change ships.
*   Sandbox benchmarks. They need a production-like environment.
*   Triggers or run starts that select the starting environment.
*   Moving existing environments into the new model.

---

## Decision Drivers

*   Platform teams manage tooling through GitOps.
*   Environments and their images are per tenant.
*   No shared writable state between runs.
*   The agent and the sandbox see the same source files.
*   An agent knows when it replaces its sandbox.

---

## Considered Options

### D1. Where sandbox images are registered

*   **Option A:** The tenant namespace. Each tenant has its own images, through GitOps.
*   **Option B:** The system namespace, available to every tenant. This matches `InferenceProvider`. Tenants cannot have different images.

### D2. Resource shape

*   **Option A:** One resource per image. Each image has its own status and history.
*   **Option B:** One resource per tenant with a map of images. Every image shares one status. Concurrent edits conflict on one object.

### D3. Who builds images

*   **Option A:** Platform teams build images on the base image in their own pipelines.
*   **Option B:** Kora builds images in the cluster from setup commands. It needs a builder and write access to a registry.

### D4. Image validation

*   **Option A:** Read the image config from the registry. Check the user and the base image label.
*   **Option B:** Start a pod from the image and run `sandbox-exec`. One pod per image and version.

### D5. Access with several environments

*   **Option A:** The union of every attached environment.
*   **Option B:** Only the environment of the current sandbox. The grant changes with each replacement.
*   **Option C:** Agent connectors for every sandbox, environment egress for its own sandbox.

### D6. Environment size

*   **Option A:** Any size per environment.
*   **Option B:** A size per environment within platform maximums.

### D7. Environment selection

*   **Option A:** An `environment` parameter on `provision_sandbox`, with the environments listed in its description. A replacement looks the same as a first provision.
*   **Option B:** Separate tools to list environments, provision a sandbox and replace the environment.

### D8. Fast local disk

*   **Option A:** Caches and tool installs on a sandbox-local volume. Source stays on the shared workspace.
*   **Option B:** Copy the workspace into each new sandbox. The agent edits files outside the sandbox. Its edits would not reach the copy.
*   **Option C:** An EBS volume shared by the agent pod and the sandbox. An EBS volume attaches to one node. Sandboxes run in a different node pool from agent pods.

### D9. Repository-declared tool versions

*   **Option A:** None. Platform teams add toolchains in their images.
*   **Option B:** A version manager in the base image.

---

## Decision

Each tenant registers sandbox images as `SandboxImage` resources built on `kora-sandbox-base`. An environment combines one of them with a size, egress hosts and connectors. Agents attach several environments and select one through dedicated tools.

### 1. The base image

`backend/Dockerfile.sandbox-exec` becomes the base image, published as `kora-sandbox-base:<version>`:

*   It keeps the general command-line tools, Go and Python. It drops Node.
*   It runs as uid 100.
*   It sets the label `io.orbit.sandbox.base=<version>`.

The default environment uses the base image.

### 2. The `SandboxImage` resource

`SandboxImage` is a namespaced resource in the same API group as `InferenceProvider`. Platform teams apply it to the tenant namespace through GitOps. One resource registers one image.

*   **Spec:** image reference, description and an optional pull secret.
*   **Status:** the resolved digest, the validation result and the warm state.

### 3. Validation

The operator reads the image config from the registry. An image is valid when it runs as uid 100 and carries the `io.orbit.sandbox.base` label. An invalid image cannot be selected. Its status states the failure.

The sandbox container runs `/usr/local/bin/sandbox-exec` as its command. An image `CMD` has no effect.

### 4. Warming

The operator runs one pre-pull DaemonSet per valid `SandboxImage` on the nodes that run sandboxes. This replaces the single image warmer. Sandboxes reference the image by digest with `IfNotPresent`.

### 5. Environments

The `environments` table is replaced. An environment has a name, a description, a `SandboxImage`, a size, egress hosts and connectors. An empty `SandboxImage` means the base image.

*   `agent_environments` links agents to environments with an `is_default` flag. `agent_types.environment_id` is removed.
*   Each tenant gets a default environment when it is registered. New agents have it attached.
*   The size sets CPU, memory and local disk. Helm values set the maximums.
*   The existing `agents:environments:read` and `agents:environments:write` scopes govern environments.

### 6. Access

The effective grant is the union of every attached environment and the agent's own scopes. It compiles when the agent is reconciled.

### 7. Agent tools

The runtime MCP server exposes three tools:

*   `list_environments` returns the agent's environments with their descriptions and the default.
*   `provision_sandbox` takes an optional environment. It uses the default when none is given.
*   `replace_environment` replaces the run's sandbox with one in another environment. Running commands end. The workspace stays.

Each sandbox start and replacement records the environment and the image on the run.

### 8. Local disk

The sandbox mounts a local volume sized from the environment. `GOPATH`, `GOCACHE`, `GOMODCACHE`, the npm and pip caches and the npm prefix point at it. HOME and the repository checkout stay on the shared workspace. `node_modules` stays in the checkout.

### Rationale

Tenant-scoped resources, one per image, match the tenancy of environments and give each image its own status (D1, D2). Pipeline-built images keep tooling in GitOps (D3). A registry read validates without a pod per image (D4). The union leaves an agent's reach to the team that attaches its environments (D5). Platform maximums bound sizes that tenants configure (D6). Separate tools make a replacement explicit to the agent (D7). Caches on local disk avoid NFS for small-file work and keep source shared (D8). Platform images carry toolchains (D9).

---

## Consequences

### Positive

*   Platform teams add toolchains without a kora release.
*   A general agent picks a sandbox per task and replaces it when the task changes.
*   Builds and tool installs avoid the NFS workspace.
*   No run writes to a cache another run reads.

### Negative

*   Replacing an environment discards its local caches.
*   An agent with several environments holds the access of all of them for the whole run.
*   One pre-pull DaemonSet per image uses disk on every sandbox node.
*   Existing environments and their agent links are dropped.
*   Validation does not run the image. An image that replaces `sandbox-exec` passes validation and fails at sandbox start.

### Neutral

*   The capacity warmer is unchanged.
*   Every sandbox start still downloads its packages.
*   `node_modules` stays on NFS until measurements show otherwise.

---

## Implementation

None at this time.

---

## Validation

None at this time.

---

## Related Decisions

*   [Agent sandbox toolchain and connectivity environments](2026-06-14-agent-sandbox-toolchain-and-connectivity-environments.md): superseded by this decision.
*   [Sandbox provisioning and shell proxy](2026-05-11-sandbox-provisioning-and-shell-proxy.md)
*   [Per-session home isolation](2026-06-04-per-session-home-isolation.md)
*   [CRD-driven inference providers](2026-10-01-crd-inference-providers.md)

## References

*   [Sandbox Environments PRD](../prd/sandbox-environments.md)
