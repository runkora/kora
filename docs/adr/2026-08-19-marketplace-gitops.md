# Skills Marketplace: GitOps Source Registration and Reconciliation

<!-- Filename: 2026-08-19-marketplace-gitops.md -->

**Status:** Proposed
**Date:** 2026-08-19
**Deciders:** Platform Engineering, Operator Engineering, Product Owner, Security Review (TBD)
**Technical Story:** [Agent Marketplace: Skills PRD](https://github.com/clear-route/orbit/blob/main/docs/prd/agent-marketplace.md)
**Release Scope:** V1, skills-only, one GitHub repository source

---

## Context

Orbit needs a read-only Skills Marketplace where approved users can browse, search, and review skills from one approved GitHub repository. Platform administrators control which repository is used. Sources cannot be managed from the dashboard.

The source must be managed through GitOps so every change is reviewed, versioned, and auditable. GitOps applies a `SkillMarketplaceSource` resource to the `orbit-system` namespace.

The Orbit operator validates the source and its GitHub Connection. If valid, it writes the approved non-secret details to a ConfigMap mounted read-only in the Marketplace backend.

When the backend starts, it reads the ConfigMap and loads `.claude-plugin/marketplace.json` from the approved repository through the Egress Gateway. It validates the manifest, loads the referenced skill content, and builds one in-memory Store for browsing, search, filtering, sorting, and pagination. The operator does not access GitHub or process skill content.

GitHub credentials remain in Orbit's existing Connection, Secret, and Egress Gateway path. The Marketplace backend does not receive GitHub private keys, installation tokens, or Kubernetes API permissions.

The first version supports skills only, one platform-wide GitHub.com repository, and loading when the backend starts. Multiple repositories, tenant-specific sources, automatic refresh, and dashboard source management are outside V1.

### Background

Orbit already uses Kubernetes custom resources for administrator-managed configuration. The Orbit operator checks these resources and writes approved non-secret settings to ConfigMaps used by other Orbit services. `Connection` and `MCPServer` resources already follow this approach.

Each Marketplace source uses the existing status pattern:

- `Pending` means the source is still being processed or delivered.
- `Aggregated` means the approved configuration was added to the ConfigMap.
- `Invalid` means the source was rejected and was not delivered.

GitHub access is already managed through a Connection resource. The Connection refers to a Kubernetes Secret but does not contain the secret value. When the Marketplace backend requests repository content, the Egress Gateway verifies the service and its permissions, adds a temporary read-only GitHub token, and forwards the request.

The existing Egress Gateway normally selects Connections using the tenant and destination host. The Marketplace source is platform-wide and must use the exact Connection declared in `connectionRef`. A protected named-Connection path is therefore required for the Marketplace backend.

### Problem Statement

Orbit needs a secure, declarative, and auditable way for platform administrators to approve one GitHub repository as the Skills Marketplace source. The source configuration must be validated, connected to the correct GitHub Connection, and delivered to the Marketplace backend without exposing credentials or giving the backend Kubernetes API permissions.

### Goals

- Register one approved platform-wide Skills Marketplace source through GitOps.
- Ensure only approved platform operators can create or change the source.
- Validate the GitHub repository, pinned revision, and referenced Connection.
- Deliver approved non-secret source details to the Marketplace backend through a read-only ConfigMap.
- Reuse Orbit's existing Connection, Secret, and Egress Gateway credential path.
- Prevent credentials and repository content from appearing in Git, ConfigMaps, status, logs, or events.
- Report whether the source configuration is `Pending`, `Aggregated`, or `Invalid`.
- Keep the Marketplace backend free from Kubernetes API permissions and mounted GitHub credentials.
- Generate stable configuration and avoid ConfigMap updates when nothing has changed.
- Provide the backend with the information needed to load `.claude-plugin/marketplace.json` and its referenced skill content.

### Non-Goals

- Supporting more than one Marketplace source in V1.
- Supporting tenant-specific Marketplace sources.
- Managing Marketplace sources from the dashboard.
- Storing GitHub credentials in the source resource or generated ConfigMap.
- Giving the Marketplace backend direct access to Kubernetes Secrets or the Kubernetes API.
- Loading GitHub repository content through the Orbit operator.
- Generating or modifying `marketplace.json` through GitOps, the Orbit operator, or the Marketplace backend.
- Supporting configurable manifest paths in V1.
- Automatically refreshing repository content while the backend is running.
- Installing, assigning, updating, running, or removing skills.
- Executing files, scripts, hooks, or commands from a Marketplace repository.
- Supporting plugins, GitHub Enterprise Server, or providers other than GitHub.com in V1.
- Defining the frontend pages, backend search implementation, or in-memory Store design.

---

## Decision Drivers

* V1 supports one platform-wide GitHub repository and one in-memory Store per backend process.
* V1 uses `.claude-plugin/marketplace.json` as the fixed Marketplace manifest path.
* Marketplace source approval must be reviewable, versioned, and auditable through GitOps.
* Only approved platform operators may create or change the Marketplace source. The dashboard and frontend must remain read-only.
* The Marketplace backend must not receive Kubernetes API permissions.
* GitHub credentials must remain in the existing Connection, Secret, and Egress Gateway path.
* The declared `connectionRef` must select the exact GitHub Connection used by the backend.
* Invalid or missing configuration must be rejected and excluded from the generated ConfigMap.
* The operator must manage configuration only and must not load or process GitHub repository content.
* Generated configuration must remain stable when the approved source has not changed.
* The design should reuse Orbit's existing custom resource, status, and ConfigMap patterns.
* The responsibilities of GitOps, the operator, the backend, and the Egress Gateway must remain clearly separated.

---

## Considered Options

### Option 1: Helm values or a manually managed ConfigMap

**Description:** Platform administrators add the GitHub repository details directly to Helm values or manually create a ConfigMap for the Marketplace backend.

**Pros:**

- Small implementation for one repository.
- Requires little new operator code.
- Easy to use during early development.

**Cons:**

- Provides limited validation and status information.
- Can bypass Orbit's normal GitOps resource pattern.
- Makes it easier to place unsafe or sensitive values in configuration.
- Does not clearly connect the source to the intended GitHub Connection.
- Manual changes are harder to review and audit consistently.

### Option 2: Singleton SkillMarketplaceSource with operator-generated configuration

**Description:** Platform administrators create a `SkillMarketplaceSource` resource through GitOps. The Orbit operator validates the source and its Connection, then writes the approved non-secret details to a ConfigMap mounted in the Marketplace backend.

**Pros:**

- Matches the existing Connection and MCPServer patterns.
- Provides clear validation and reconciliation status.
- Records source approval through Git and Kubernetes.
- Keeps Kubernetes API permissions out of the Marketplace backend.
- Keeps GitHub credentials in the existing Egress Gateway path.
- Allows invalid configuration to be rejected before the backend uses it.

**Cons:**

- Requires a new custom resource and operator controller.
- Requires status handling, ConfigMap generation, and chart configuration.
- Creates a configuration contract between the operator and Marketplace backend.
- Source changes require operator reconciliation and a backend restart in V1.

### Option 3: Marketplace backend watches Kubernetes resources

**Description:** The Marketplace backend reads `SkillMarketplaceSource` and Connection resources directly from the Kubernetes API.

**Pros:**

- The backend can see configuration changes directly.
- A generated ConfigMap is not required.
- No separate mounted configuration reader is needed.

**Cons:**

- Requires the Marketplace backend to have Kubernetes API permissions.
- Increases the impact of a backend security compromise.
- Mixes Marketplace processing with Kubernetes configuration management.
- Introduces a different configuration delivery pattern from the one already used by Orbit.
- Makes the backend responsible for work that belongs to the Orbit operator.

---

## Decision

**Chosen Option:** Option 2 - Singleton `SkillMarketplaceSource` with operator-generated configuration

Platform administrators will register the approved Marketplace repository through a `SkillMarketplaceSource` resource in the `orbit-system` namespace. The resource will be stored and reviewed through GitOps.

The Orbit operator will validate the source details and the referenced GitHub Connection. If the configuration is valid, the operator will write the approved non-secret information to the `orbit-skill-marketplace-sources` ConfigMap.

The Marketplace backend will mount the ConfigMap as a read-only file named `sources.json`. It will not receive Kubernetes API permissions or direct access to GitHub credentials.

When the backend starts, it will read the source configuration and load `.claude-plugin/marketplace.json` from the approved repository through the Egress Gateway. It will validate the manifest, load the referenced skill content, and build its singleton in-memory Store.

### Rationale

This option follows an existing Orbit pattern instead of introducing a new way to deliver configuration. The Orbit operator already validates administrator-managed resources and provides approved settings to services through ConfigMaps.

It keeps responsibilities clear:

- GitOps records and reviews the source approval.
- The operator validates and delivers the configuration.
- The Marketplace backend loads and validates the manifest and referenced skill content.
- The Egress Gateway handles temporary GitHub credentials.

This option also provides the strongest security boundary for V1. The Marketplace backend receives only the approved repository details. It does not receive Kubernetes API permissions, GitHub private keys, or installation tokens.

The decision answers Orbit's three architecture questions:

- **Who is this agent or caller?** Git and Kubernetes history identify who approved the source. At runtime, the Marketplace workload has a verifiable Orbit identity.
- **What can it access?** Repository-specific read permissions and the pinned revision limit access to the approved source.
- **How does it connect?** The Egress Gateway verifies identity and policy, injects a temporary read-only GitHub token, and sends the request through the declared Connection.

Option 1 is simpler but does not provide enough validation, status, or auditability. Option 3 provides direct updates but gives the Marketplace backend unnecessary Kubernetes permissions and configuration responsibilities.

---

## Consequences

### Positive

- Marketplace source approval is reviewed and recorded through GitOps.
- The Orbit operator validates source configuration before the backend uses it.
- The declared `connectionRef` selects the intended GitHub App Connection.
- The Marketplace backend receives only approved non-secret source information.
- GitHub credentials remain protected by the Connection, Secret, and Egress Gateway path.
- The Marketplace backend does not need Kubernetes API permissions.
- Invalid source configuration is rejected and excluded from the generated ConfigMap.
- The dashboard remains read-only and cannot change Marketplace sources.
- Unchanged configuration does not cause unnecessary ConfigMap updates.

### Negative

- Orbit must add and maintain a new custom resource, operator controller, and status model.
- The operator and Marketplace backend must share and maintain the `sources.json` configuration format.
- Adding, changing, or deleting the source requires a controlled backend restart in V1.
- V1 supports only one platform-wide Marketplace source.
- Each Marketplace backend replica creates and stores its own in-memory Marketplace data.
- The Egress Gateway requires a protected named-Connection path for the `orbit-system` scope.
- Initial Marketplace loading depends on GitHub and the Egress Gateway being available.

### Neutral

- The generated configuration includes a version field so its format can evolve later.
- Operator status reports whether the source configuration was accepted and delivered.
- Marketplace backend status reports GitHub access, repository processing, and Marketplace availability.
- Deleting or invalidating the source removes it from the ConfigMap, but a running backend keeps its current in-memory data until restart.
- The Orbit operator manages source configuration only.
- The Marketplace backend remains responsible for loading the manifest, validating its content, and building the singleton Store.

---

## Implementation

### Source Resource

Add a namespaced `SkillMarketplaceSource` resource under `orbit.io/v1alpha1`. It contains the display name, provider, GitHub repository, pinned revision, and `connectionRef`. It does not contain credentials, repository content, or a `manifestPath`. For V1, the backend uses the fixed `.claude-plugin/marketplace.json` path.

### Source Scope

V1 supports one source in the `orbit-system` namespace. Additional sources are marked `Invalid` with the reason `AdditionalSourceNotSupported`.

### Validation

Kubernetes checks that all required fields are present. The provider must be `github`, the repository must use `owner/repository`, and the revision must be a complete Git commit identifier.

The Orbit operator then confirms that:

- the source is in `orbit-system`;
- only one source is accepted; and
- the referenced Connection uses the GitHub App adapter for GitHub.com and permits the repository.

The operator does not read the Connection Secret. Failed validation marks the source `Invalid` and excludes it from `sources.json`.

The operator does not access GitHub and therefore does not check whether the manifest exists. After loading the repository, the Marketplace backend confirms that `.claude-plugin/marketplace.json` exists, follows the expected schema, and contains safe relative skill paths. Missing or invalid manifest content is reported as a backend runtime source error.

### Generated Configuration

After validation, the operator writes the approved source details to the `orbit-skill-marketplace-sources` ConfigMap. The ConfigMap contains a versioned `sources.json` file with either no source or one source. It never contains credentials or repository content.

The operator renders the file deterministically and does not update the ConfigMap when the desired content is unchanged.

### Backend Startup

The Marketplace backend mounts `sources.json` as a read-only file. When it starts, it loads `.claude-plugin/marketplace.json` from the approved repository at the pinned revision. It validates the manifest, loads the referenced skill content, and builds its singleton in-memory Store.

### GitHub Access

The backend connects to GitHub through the Egress Gateway. The gateway verifies the backend identity, checks policy, adds a temporary read-only GitHub token, and forwards the request through the exact Connection named by `connectionRef`.

### Status

The operator reports the source as `Pending`, `Aggregated`, or `Invalid`. GitHub access, repository processing, and Marketplace availability are reported separately by the Marketplace backend.

### Permissions

Only approved platform operators and GitOps accounts can change the source. The Marketplace backend receives no Kubernetes API access, Secret access, or source modification permissions.

### Applying Changes

Source changes update the ConfigMap. A controlled backend restart or rollout is required before the changed source is loaded in V1.

### Migration Path

Marketplace source registration is new, so there is no stored source data to migrate.

---

## Validation

This decision is successful when a valid source reaches `sources.json` and the Marketplace backend loads and validates `.claude-plugin/marketplace.json`.

### Success Metrics

- A valid source is added to `sources.json` and receives `Aggregated` status.
- An invalid source receives `Invalid` status and is excluded from `sources.json`.
- An additional source receives `Invalid` status with reason `AdditionalSourceNotSupported`.
- The Marketplace backend loads `.claude-plugin/marketplace.json` and populates the Store with the referenced skills.
- The backend accesses GitHub through the declared Connection with read-only permissions.
- No credentials appear in the source resource, ConfigMap, status, logs, or events.
- The Marketplace backend runs without Kubernetes API or Secret access.
- Unchanged source configuration does not cause unnecessary ConfigMap updates.

### Monitoring

The Orbit operator will report:

- reconciliation count and duration;
- reconciliation errors;
- source phase; and
- ConfigMap updates.

The Marketplace backend will report:

- GitHub request failures;
- manifest loading and validation;
- referenced skill loading;
- Store population; and
- runtime source availability.

Logs, metrics, status, and events must not contain credentials, repository content, or raw GitHub responses.

---

## Related Decisions

- [Orbit Kubernetes Operator](2026-03-31-orbit-kubernetes-operator.md)
- [Helm Chart Distribution and GitOps Repository Structure](2026-03-31-helm-chart-distribution-and-gitops-repository-structure.md)
- [JIT Credential Brokering through the Egress Gateway](2026-06-06-jit-credential-brokering-egress-gateway.md)
- [GitHub Connection: Category-Scoped Actions, Repository Qualifiers, and Down-Scoped Token Minting](2026-07-22-github-connection-category-scoped-actions.md)

---

## References

- [Agent Marketplace: Skills PRD](https://github.com/clear-route/orbit/blob/main/docs/prd/agent-marketplace.md)
- [Orbit ADR Template](template.md)
- [Orbit Architecture](../architecture.md)
- [Orbit Code Organization](../code-organization.md)
- [Connection Custom Resource Definition](../../charts/orbit-operator/templates/crd-connection.yaml)
- [MCPServer Custom Resource Definition](../../charts/orbit-operator/templates/crd-mcpserver.yaml)
- [Connection Aggregator](../../backend/internal/operator/connection_aggregator.go)
- [MCPServer Aggregator](../../backend/internal/operator/mcpserver_aggregator.go)

---

## Notes

The operator's `Aggregated` status means that validated source configuration was delivered to the ConfigMap. It does not mean that GitHub was reachable or that the Marketplace backend successfully loaded the repository. Runtime source health belongs to the Marketplace backend.

**Last Updated:** 2026-08-22
