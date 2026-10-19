# Sandbox Environments

<!-- Filename: 2026-10-03-sandbox-environments.md  (decision date + slug; no sequential number) -->

**Status:** Accepted (Amended 2026-10-08)  
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

## Amendment 1 (2026-10-07): Environment names and slugs

**Status:** Accepted

Touches none of the three questions. Agents still select an environment by the same stored value, and the access an environment grants is unchanged.

**Context.** An environment has one `name` that serves as both its identifier and its label. Agents select an environment by this value in `provision_sandbox` and `replace_environment`, and `environments_tenant_id_name_key` keeps it unique within a tenant. Editing the label of an environment changes the value its agents select by.

**Options.**

*   **Separate display name (chosen):** A new `display_name` column holds the name for people, and the existing `name` column holds the slug that agents select by.
*   **Rename the column (rejected):** Rename `name` to `slug` and add a new `name` column. The previous release's orbit-agents reads and writes `name` during an upgrade and would fail against the renamed column.
*   **Single name (rejected):** Keep a single name. A readable label with spaces and capitals cannot also serve as a stable selector, and every relabel would change what agents select.

For the API shape:

*   **`slug` and `name` (chosen):** Environments carry `slug` and `name` in the same way as agents.
*   **`name` and `display_name` (rejected):** The API keeps `name` as the identifier and adds `display_name`. The dashboard, the orbit MCP tools and the agent API would use `name` for two different things.

**Decision.**

*   A new migration will add `display_name TEXT NOT NULL DEFAULT ''` to `environments` and set it to `name` on every existing row. An empty `display_name` reads as the slug. The previous release's orbit-agents writes rows without `display_name` and keeps working.
*   The `name` column will hold the slug. Its column name and its unique constraint stay unchanged.
*   The environments API will return `slug` from the `name` column and `name` from `display_name`. Create and update requests will take `slug` and `name`, and `name` is required.
*   A create request will validate the slug as a DNS-1123 label. A create request without `slug` will derive it from `name` with the dashboard's rule: lower case, each run of other characters replaced by a hyphen, and leading and trailing hyphens removed. A derived slug that is empty or not a valid DNS-1123 label (e.g., a name made only of symbols, or longer than 63 characters after derivation) returns 400, and the request must supply `slug`.
*   The slug will not change after create. An update without `slug` keeps the stored slug, and an update with a different slug returns 400. Existing slugs that are not DNS-1123 labels stay valid.
*   Display names do not need to be unique within a tenant.
*   `agents.environments.create` and `agents.environments.replace` in the orbit MCP server will take `slug` and `name` with the same rules.
*   The run environment list will carry `slug` and `name`. Each `list_environments` line will start with the slug, followed by the name, the marks, the description, the image and the size. `provision_sandbox` and `replace_environment` will select by slug, and an unknown slug returns the agent's slugs.
*   The built-in default environment will have the slug `default` and the name `Default`.
*   Pod annotations, the `sandbox_environment` run event and agent attachments will keep the slug and the environment ID as they do today.

**Consequences.**

*   An environment can be relabeled without changing what its agents select.
*   Until an agent pod rolls to the new session-manager image, its `list_environments` shows the name in the place of the slug, and a selection by a name that differs from the slug fails with the list of slugs.
*   The `name` field changes from the identifier to the display name.
*   A previous-release client that reads `name` as the identifier receives the display name. The two values match for every environment that existed before the upgrade.
*   While the previous release's orbit-agents still serves requests during an upgrade, an update it handles writes the requested name into the slug column. The new release rejects slug changes once it serves every request.

## Amendment 2 (2026-10-08): Sandbox resource requests

**Status:** Accepted

Touches none of the three questions. A sandbox keeps its identity, its grants and its egress path. The amendment sets how much node capacity a sandbox reserves.

**Context.** Decision 5 states that the size sets CPU, memory and local disk. `sandboxResources` in `backend/internal/agents/k8s.go` uses the size as the limit and requests 100m CPU, 256Mi memory and 128Mi ephemeral storage. The injected sidecars request 200m CPU and 224Mi memory and can use up to 2.45 CPU and 1.2Gi. The sandbox pod is Burstable QoS. The scheduler and the autoscaler count a busy sandbox as 100m CPU, and the node overcommits.

The 1m rates of 244 sandbox pods on a deployed cluster over 7 days show the following:

*   `sandbox-exec` reached its 2 CPU limit and was throttled in 99% of CFS periods at its peak. Its working set reached 3.95Gi of a 4Gi limit.
*   istio-proxy peaked at 81m CPU and 69Mi and was never throttled.
*   identity-proxy peaked at 12m CPU and 10Mi and was throttled in 23% of periods at its 200m limit.
*   opa peaked at 144m CPU and 27Mi and was throttled in 50% of periods at its 250m limit.

On an idle local node with a 2 CPU limit and cold caches, `go build ./...` on `backend` took 90.9s and `go test ./internal/agents/...` took 32.1s. The same commands took 23.3s and 22.7s on an 18-core laptop.

The default `maxSize` of 8 CPU and 16Gi exceeds what a 4 vCPU, 16GiB node can hold. A sandbox pod that cannot schedule fails at the end of the provision budget (`agents.guardrails.sandboxProvisionBudgetSecs`) with a generic timeout. Each retry reuses the Pending pod, and the cold path gets the whole budget.

**Options.** For what the size means:

*   **Reservation (chosen):** Requests equal limits for CPU, memory and ephemeral storage. The sandbox gets its size on a busy node, and the autoscaler sees its demand.
*   **Ceiling (rejected):** Keep the fixed small requests. A busy node slows every sandbox on it, and memory pressure evicts a sandbox mid-run.
*   **Reserved floor (rejected):** Request a fixed share of the size. The sandbox is predictable only up to the floor, and the node still overcommits above it.

For what the size covers:

*   **The agent's container (chosen):** `sandbox-exec` gets the full size, and the sidecars and the scratch volumes sit on top.
*   **The whole pod (rejected):** The size would include the sidecars. orbit-agents would have to subtract sidecar resources that the operator webhook sets, and a small size would lose a large share to the sidecars.

For the sidecars on sandbox pods:

*   **Pin every sidecar (chosen):** istio-proxy, identity-proxy and opa get requests equal to limits, and the pod is Guaranteed QoS.
*   **Pin istio-proxy only (rejected):** identity-proxy and opa would still overcommit the node, and the pod would stay Burstable.
*   **Mesh defaults (rejected):** istio-proxy alone can use 1.9 CPU above its request.

For an environment larger than any node:

*   **Operator-set maximum (chosen):** `maxSize` stays the save-time check, and the timeout error carries the scheduler's reason.
*   **Read nodes or the autoscaler (rejected):** orbit-agents would need cluster-wide read access to nodes or to autoscaler resources. A pool scaled to zero has no nodes to read, and an autoscaler can launch nodes larger than any that exist.
*   **Detect unschedulable pods at run time (rejected):** An autoscaler leaves every new pod unschedulable until a node joins. Telling a pod that never fits from a pod that waits for a node needs autoscaler-specific signals.

For the default maximum:

*   **`maxSize` equals `defaultSize` (chosen):** The chart cannot know the operator's nodes. A node that runs the default sandbox runs every size the default install accepts.
*   **A larger default (rejected):** The default install would accept sizes that never schedule on small nodes.

**Decision.**

*   `sandboxResources` will set the CPU and memory requests equal to the size. The ephemeral-storage request will equal its limit: the environment's disk plus 1Gi for the scratch volumes, the logs and the writable layer.
*   The operator webhook will set the resources of the sidecars on pods with the sandbox role from new Helm values. The defaults will be 200m and 128Mi for istio-proxy, 200m and 64Mi for identity-proxy, and 500m and 128Mi for opa, with requests equal to limits. The webhook will set istio-proxy through the `sidecar.istio.io/proxyCPU`, `proxyCPULimit`, `proxyMemory` and `proxyMemoryLimit` annotations. Sidecars on agent pods keep their current resources.
*   The default `maxSize` will equal `defaultSize`: 2 CPU, 4Gi memory and 10Gi disk.
*   The deployment guide will state the sizing rule for `maxSize`: the size plus the sandbox sidecars and 1Gi of disk must fit a sandbox node's allocatable capacity after its DaemonSet requests.
*   The headroom placeholder will request and limit `defaultSize` plus the sandbox sidecars, with ephemeral storage of `defaultSize.disk` plus 1Gi. A value in `agents.sandboxWarming.resources` will replace the derived value.
*   orbit-agents will end each wait for a sandbox pod 5s before one attempt's share of the provision budget left after the backoffs between attempts. A wait that ends before the pod is ready will return 503 with the message of the pod's `PodScheduled` condition. The session-manager will carry that message into the run's failure event and into the message the agent reads when the sandbox is lost.
*   The provision budget and its default is raised to 600s so a cold node scale-up fits.
*   Existing environments keep their stored sizes.

**Consequences.**

*   A sandbox gets its full size on a busy node, and the autoscaler adds nodes for the demand that sandboxes declare.
*   A sandbox is the last pod evicted under node memory pressure.
*   opa on a sandbox pod gets twice its previous CPU limit.
*   A run that cannot schedule reports the scheduler's reason.
*   A default sandbox reserves 2.9 CPU and about 4.3Gi, and a 4 vCPU node holds one default sandbox.
*   A sandbox holds its reservation while the agent waits on the model.
*   A sandbox stays bound by its CPU limit. A larger size is the only way to make it faster.
*   The default install accepts no size above `defaultSize` until the operator raises `maxSize`.
*   istio-proxy on a sandbox pod is OOM-killed above 128Mi, and the sandbox loses its network until the proxy restarts.
*   identity-proxy keeps its 200m limit and its measured throttling.

## Amendment 3 (2026-10-08): The sandbox's local disk holds HOME, TMPDIR and its working folder

**Status:** Accepted

[Sandbox Workspace Mount](2026-10-08-sandbox-workspace-mount.md) supersedes Decision 8 and the neutral consequence that `node_modules` stays on NFS.

*   HOME and TMPDIR move to the sandbox's local disk, and a new or replaced sandbox starts with them empty.
*   With `sandboxMount.enabled`, the local disk also holds the sandbox's working folder at `/workspaces/<run>/sandbox`. The repository checkout and `node_modules` sit there for the life of the sandbox.

---

## Related Decisions

*   [Agent sandbox toolchain and connectivity environments](2026-06-14-agent-sandbox-toolchain-and-connectivity-environments.md) (superseded)
*   [Sandbox provisioning and shell proxy](2026-05-11-sandbox-provisioning-and-shell-proxy.md)
*   [Per-session home isolation](2026-06-04-per-session-home-isolation.md)
*   [Sandbox Workspace Mount](2026-10-08-sandbox-workspace-mount.md)
*   [Database migrations](2026-09-10-database-migrations.md)
*   [CRD-driven inference providers](2026-10-01-crd-inference-providers.md)

## References

*   [Sandbox Environments PRD](../prd/sandbox-environments.md)

---

**Last Updated:** 2026-10-08 (Amendment 3: the sandbox's local disk holds HOME, TMPDIR and its working folder)
