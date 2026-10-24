# Declarative Resources

<!-- Filename: 2026-10-24-declarative-resources.md  (decision date + slug; no sequential number) -->

**Status:** Proposed  
**Date:** 2026-10-24  
**Deciders:** Justin Wilkin  
**Technical Story:** [Declarative Resources PRD](../prd/declarative-resources.md). Define virtual models, environments, agents, trigger sources, triggers and roles as Kubernetes resources beside the dashboard and API.

---

## Context

Six kinds are Kubernetes resources today: `KoraTenant`, `Connection`, `OAuthClient`, `MCPServer`, `InferenceProvider` and `SandboxImage`. Each is the only definition of its object. The operator aggregates most of them into ConfigMaps that services mount, and the inference gateway watches `InferenceProvider` directly ([CRD-Driven Inference Providers](2026-10-01-crd-inference-providers.md)).

Everything else an administrator defines lives in Postgres behind a REST API:

*   `kora-agents` owns `agent_types`, `environments` and `agent_environments`.
*   `kora-triggers` owns `webhook_sources`, `triggers`, `trigger_routes` and `trigger_route_candidates`.
*   `policy-service` owns `policy_role_scopes` and `policy_tenant_budgets`.
*   `inference-gateway` owns `virtual_models` and `virtual_model_routes`. These tables are global and have no `tenant_id`.

Rows are keyed by UUID with a per-tenant unique slug or name. Agents already refer to `Connection`, `MCPServer` and `SandboxImage` resources by name. Trigger rows refer to agents and sources by UUID.

Every create path reads the caller. Agent create checks the caller's identity-mode write permission and scope ceiling, and records `created_by` (`backend/internal/agents/agent_type_service.go:352`, `:395`, `:397`). Trigger create requires the caller to hold `can_run` on the target agent (`backend/internal/triggers/service_lifecycle.go:129-142`). Role upsert checks the caller's scope ceiling (`backend/internal/policyservice/api/controller_auth.go:58-78`) and budget permission (`backend/internal/policyservice/api/handlers_roles.go:103`). A resource applied from a repository has no kora caller.

Deleting an agent hard-deletes its runs before the agent row (`backend/internal/agents/agent_type_service.go:899`). Deleting a trigger source cascades to its triggers through `triggers_source_id_fkey ON DELETE CASCADE` (`backend/cmd/kora-triggers/migrations/20260910000029_create_triggers.sql:34`).

This touches *What can it access?* A declared agent's grant is derived from its spec as it is today. The repository and the team that runs it are the authority for which declared agents exist and what they can access. Write access to a tenant namespace bounds that authority to the tenant. *Who is this agent?* and *How does it connect?* are unchanged. A declared agent gets the same SPIFFE identity, sidecars and egress as a dashboard-built agent.

### Goals

*   An organization defines virtual models, environments, agents, trigger sources, triggers and roles as resources in a repository.
*   The dashboard, API and kora MCP server keep creating and editing items for organizations that do not use a repository.
*   A platform team can lock a tenant to its resources.
*   Run history survives an agent deletion.

### Non-Goals

*   Export of dashboard-built items as resources.
*   Adoption of a dashboard-built item by a resource with the same name.
*   Grants for individual users as resources.
*   Model prices and per-model provider toggles as resources.
*   Tenant-scoped virtual models.

---

## Decision Drivers

*   One provisioning path for declared and dashboard-built items.
*   Runs, deliveries and usage keep their foreign keys and queries.
*   Each service reads no more of the cluster than it needs.
*   A dashboard edit never silently disappears on the next repository sync.

---

## Considered Options

### Decision 1: Source of Truth

*   **Option A (chosen):** Postgres stays the store. Each owning service projects its resources into its own tables and marks each row with its source.
*   **Option B (rejected):** Resources become the only store and the API writes resources through the Kubernetes API server. Runs and deliveries would need a projection table for their foreign keys, and every write path would be rewritten.
*   **Option C (rejected):** Each kind keeps two stores and reads merge them. Every read, write and delete would have two paths.

### Decision 2: Delivery to Services

*   **Option A (chosen):** Each owning service watches its own kinds with a controller-runtime manager and writes their status.
*   **Option B (rejected):** The operator aggregates resources into ConfigMaps that services mount. Status would need a second writer, and the file format would be a second contract between two binaries.

### Decision 3: Tenant Mapping and Switch Delivery

*   **Option A (chosen):** The operator adds each tenant's namespace, authoring switch and budget to the `kora-tenants` registry. Services that need them mount the registry.
*   **Option B (rejected):** Every service watches `KoraTenant`. Four services would gain cluster-scoped access to tenants and each would resolve namespaces itself.

### Decision 4: Deletion Detection

*   **Option A (chosen):** A service deletes a declared row on the resource's delete event, and at startup when the row's resource UID is absent from a synced cache.
*   **Option B (rejected):** Each resource carries a finalizer. A stopped service would block every namespace deletion in the cluster.

### Decision 5: Trigger Source Signing Secrets

*   **Option A (chosen):** The operator binds `kora-triggers` to Secret `get` in each tenant namespace. `kora-triggers` reads the referenced Secret on each reconcile and stores it encrypted as it does today.
*   **Option B (rejected):** `kora-triggers` gets a ClusterRole to watch Secrets. It could read every Secret in the cluster.

### Decision 6: Concurrent Replicas

*   **Option A (chosen):** Each service's manager runs with leader election. One replica projects and provisions at a time.
*   **Option B (rejected):** Every replica projects. Two replicas could provision the same agent at once.

### Decision 7: Role Kind Name

*   **Option A (chosen):** `PolicyRole`.
*   **Option B (rejected):** `Role`. A bare `kubectl get roles` resolves to the Kubernetes RBAC `Role` kind.

### Decision 8: Agent Deletion

*   **Option A (chosen):** Soft delete. The agent row is marked deleted and its runs stay.
*   **Option B (rejected):** Hard delete as today. A repository prune would erase the agent's run history.

### Decision 9: Trigger Source Deletion

*   **Option A (chosen):** `triggers.source_id` becomes `ON DELETE SET NULL`. A trigger without a source stops firing.
*   **Option B (rejected):** Keep the cascade. Deleting a `TriggerSource` resource would delete `Trigger` rows whose resources still exist.

---

## Decision

**Chosen Options:** Decision 1 A, Decision 2 A, Decision 3 A, Decision 4 A, Decision 5 A, Decision 6 A, Decision 7 A, Decision 8 A, Decision 9 A.

### Architecture

```
  repository ──► Argo CD / Flux ──► Kubernetes API
                                          │ watch
          ┌───────────────┬───────────────┼───────────────┐
          ▼               ▼               ▼               ▼
  inference-gateway  kora-agents     kora-triggers   policy-service
  VirtualModel       Environment     TriggerSource   PolicyRole
                     Agent           Trigger         tenant budget
          │               │               │               │
          ▼               ▼               ▼               ▼
  virtual_models     environments    webhook_sources policy_role_scopes
  (source=crd)       agent_types     triggers        policy_tenant_budgets
                          │
                          ▼
             same provisioning path as the API
             (StatefulSet, agent grant, mesh policy)
```

### 1. Resource Kinds

All kinds are `runkora.io/v1alpha1`, namespaced, with a status subresource. They ship in `charts/kora-crds`. Go types live in `backend/internal/platform/<kind>/v1alpha1/` and follow the hand-written pattern of `backend/internal/platform/inferenceproviders/v1alpha1/`.

| Kind | Namespace | Owner | Table |
|---|---|---|---|
| `VirtualModel` | system | `inference-gateway` | `virtual_models`, `virtual_model_routes` |
| `Environment` | tenant | `kora-agents` | `environments` |
| `Agent` | tenant | `kora-agents` | `agent_types`, `agent_environments` |
| `TriggerSource` | tenant | `kora-triggers` | `webhook_sources` |
| `Trigger` | tenant | `kora-triggers` | `triggers`, `trigger_routes`, `trigger_route_candidates` |
| `PolicyRole` | tenant | `policy-service` | `policy_role_scopes` |

*   A ValidatingAdmissionPolicy in the `kora` chart rejects a `VirtualModel` outside the system namespace.
*   `metadata.name` is a DNS-1123 label. CEL limits an `Agent` name to a DNS-1035 label of at most 52 characters.
*   Spec fields mirror the API request fields in camelCase. Server-derived fields are absent.
*   A reference to another resource is a name in the same namespace. `VirtualModel` references from tenant kinds use `family` and `name`.
*   Secrets are referenced by `{name, key}` in the same namespace.

A `VirtualModel`:

```yaml
apiVersion: runkora.io/v1alpha1
kind: VirtualModel
metadata:
  name: claude-sonnet
  namespace: kora-system
spec:
  family: anthropic
  name: sonnet
  displayName: Claude Sonnet
  strategy: priority
  routes:
    - provider: bedrock:prod
      modelID: anthropic.claude-sonnet-4-5-20250929-v1:0
      priority: 1
```

An `Environment` and an `Agent`:

```yaml
apiVersion: runkora.io/v1alpha1
kind: Environment
metadata:
  name: go-build
  namespace: kora-tenant-acme
spec:
  displayName: Go build
  sandboxImage: kora-go
  size: {cpu: "2", memory: 4Gi, disk: 10Gi}
  egressHosts: [proxy.golang.org]
---
apiVersion: runkora.io/v1alpha1
kind: Agent
metadata:
  name: code-reviewer
  namespace: kora-tenant-acme
spec:
  displayName: Code reviewer
  framework: claude-code
  identityMode: autonomous
  model: {family: anthropic, name: sonnet}
  systemPrompt: |
    Review pull requests for correctness.
  environments:
    - name: go-build
      default: true
  connectors:
    - connection: github
      actions: [read, write]
  mcpTools:
    - server: github
      tools: [create_review]
  budget: {limitUSD: 50, resetCadence: monthly}
```

*   `identityMode` is immutable through a CEL `self == oldSelf` rule.
*   `mcpTools` expands to the existing `mcp:<server>:<tool>:use` scopes.

A `TriggerSource` and a `Trigger`:

```yaml
apiVersion: runkora.io/v1alpha1
kind: TriggerSource
metadata:
  name: github-org
  namespace: kora-tenant-acme
spec:
  provider: github
  signingSecretRef: {name: github-webhook, key: secret}
---
apiVersion: runkora.io/v1alpha1
kind: Trigger
metadata:
  name: review-on-pr
  namespace: kora-tenant-acme
spec:
  kind: webhook
  source: github-org
  eventType: pull_request
  agent: code-reviewer
  promptTemplate: "Review {{ .payload.pull_request.html_url }}"
```

*   `spec.agent` and `spec.routing` are mutually exclusive through CEL. `spec.routing.candidates` and `spec.routing.fallback` are `Agent` names.
*   `spec.schedule` and `spec.manual` carry the existing scheduled and manual configuration.

A `PolicyRole`:

```yaml
apiVersion: runkora.io/v1alpha1
kind: PolicyRole
metadata:
  name: reviewers
  namespace: kora-tenant-acme
spec:
  role: Reviewers
  scopes: ["agents:runs:code-reviewer:read", "agents:runs:code-reviewer:write"]
  budget: {limitUSD: 200, resetCadence: monthly}
```

*   `spec.role` is the identity provider's role claim. It defaults to `metadata.name`.

### 2. KoraTenant Additions

```yaml
spec:
  apiAuthoring: Disabled   # Enabled (default) or Disabled
  budget: {limitUSD: 1000, resetCadence: monthly}
```

*   The operator adds `namespace`, `apiAuthoring` and `budget` to each entry in the `kora-tenants` registry (`backend/internal/operator/registry.go`).
*   CEL makes `spec.namespace` immutable once set. The operator refuses to reconcile a tenant whose namespace is the system namespace or another tenant's namespace.
*   `kora-agents` and `kora-triggers` mount the registry as `identity` and `policy-service` do.
*   The platform switch for virtual models is `inferenceGateway.catalog.apiAuthoring` in the `kora` chart values.
*   A tenant budget on `KoraTenant` replaces any budget set through the API. Removing it from the spec deletes the declared budget.

### 3. Projection

A shared package, `backend/internal/platform/declarative`, holds the parts every owning service uses:

```go
// Tenants resolves a resource's namespace to its tenant from the mounted registry.
type Tenants interface {
	TenantForNamespace(namespace string) (tenantID string, ok bool)
	APIAuthoringEnabled(tenantID string) bool
}

// SetConditions sets the standard conditions and observedGeneration on a status.
func SetConditions(status *Status, generation int64, accepted, resolved, ready metav1.Condition)
```

*   Each service starts one controller-runtime manager with leader election and the Lease name `<service>-declarative`.
*   A reconcile resolves the tenant, validates the spec with the API's validation code, resolves references against the cache and upserts the row through the service's declared write path.
*   The row key is the item's existing unique key: `metadata.name` for `Agent`, `Environment`, `TriggerSource` and `Trigger`, `spec.role` for `PolicyRole`, and `spec.family` with `spec.name` for `VirtualModel`.
*   A live row with the same key and `source = 'api'` leaves the resource unprojected with `Accepted=False` and reason `NameConflict`. Soft-deleted rows do not conflict.
*   When two resources claim the same key, the older resource keeps it and the newer one reports `NameConflict`.
*   A resource in a namespace with no tenant gets `Accepted=False` and reason `UnknownTenant`.
*   A missing reference gives `ResolvedRefs=False` with reason `RefNotFound` and the missing name in the message. A new resource with a missing reference is not projected. A projected resource keeps its last applied row.
*   A reference to a dashboard-built item counts as missing.
*   Unresolved resources requeue every 30 seconds, and every resource resyncs every 5 minutes.
*   Each row stores its resource UID in `resource_uid`. A delete event deletes the row with that UID.
*   At startup, after the cache syncs, each service deletes live `source = 'crd'` rows whose UID is absent from the cache. It skips a tenant missing from the registry and a tenant whose namespace does not exist.

Each resource reports three conditions:

*   `Accepted`: the spec is valid, the tenant is known and the key is free.
*   `ResolvedRefs`: every reference exists as a resource.
*   `Ready`: the item is projected and live. An `Agent` is `Ready` when its row reaches `state = 'active'`.

A `TriggerSource` also reports `status.webhookPath`, the ingest path that includes the source's id.

### 4. Declared Write Path

Each service gains a write path for declared items that runs the API's validation and provisioning without a caller:

*   `kora-agents` skips the identity-mode write check, the scope ceiling and `refuseDelegatedAutonomousWrite`. `created_by` is `resource:<namespace>/<name>`.
*   `kora-agents` serves an internal lookup of an agent by tenant and slug for system callers. It returns the id, `source`, `state` and `identity_mode`.
*   `kora-triggers` resolves each agent name against the `Agent` resources it watches, then calls the internal lookup with its own service identity for the id. A declared trigger requires a declared, active agent.
*   `kora-triggers` re-resolves agent ids when an `Agent` resource changes. A recreated agent gets a new id, and its declared triggers, routed candidates and fallbacks follow it.
*   `kora-triggers` skips `requireRunAuthority` and `authorizeCaller` and still runs `refuseDelegatedBinding`.
*   The declared trigger path sets `agent_type_id`, `source_id` and the routing candidates directly. The API still cannot change them.
*   A routed trigger's `routing.model` is a `VirtualModel` reference.
*   `policy-service` skips the role scope ceiling and the budget permission check. A scope missing from the vocabulary gives `ResolvedRefs=False`.
*   Grants for agents, trigger sources and triggers are created as they are today.

The reconciler owns every spec field. `auto_paused_at` and `consecutive_failures` stay runtime state on `triggers`, and a reconcile never clears an auto-pause.

### 5. Source Columns

Each projected table gains two columns in an expand migration:

```sql
ALTER TABLE agent_types
  ADD COLUMN source TEXT NOT NULL DEFAULT 'api' CHECK (source IN ('api', 'crd')),
  ADD COLUMN resource_uid TEXT;
```

*   The tables are `virtual_models`, `environments`, `agent_types`, `webhook_sources`, `triggers`, `policy_role_scopes` and `policy_tenant_budgets`.
*   Child tables (`virtual_model_routes`, `agent_environments`, `trigger_routes`, `trigger_route_candidates`) follow their parent.

### 6. API Behavior

*   Every list and get response for the projected kinds carries `source: "api" | "crd"`.
*   A create, update or delete of a `source = 'crd'` row returns HTTP 409 with code `managed_by_resource`.
*   With `apiAuthoring: Disabled`, these writes in the tenant return HTTP 403 with code `api_authoring_disabled`:
    *   Create, update and delete of agents, environments, triggers, trigger sources and roles.
    *   Role budget changes.
*   With the platform switch off, virtual model writes return the same 403.
*   Tenant budget writes return 409 while the tenant budget is declared.
*   These stay available in every case: run starts, manual trigger fires, clearing a trigger's auto-pause, secret rotation on an API source, grants, PATs and user preferences.
*   The agent fields `can_edit` and `can_delete` are false for declared agents and for every agent in a locked tenant.
*   An API trigger cannot take the name of a declared trigger. A declared trigger with the name of a live API trigger reports `NameConflict`.
*   The kora MCP server calls the same APIs and inherits these refusals.
*   `identity` returns `api_authoring` for the caller's tenant on the session response. `GET /v1/catalog/virtual-models` returns the platform switch.

### 7. Dashboard

*   Declared items show a "Managed in git" badge and no edit or delete controls.
*   A locked tenant shows agents, environments, triggers, trigger sources and roles read-only with no create controls.
*   With the platform switch off, virtual models are read-only.

### 8. Agent Soft Delete

```sql
ALTER TABLE agent_types ADD COLUMN deleted_at TIMESTAMPTZ;
ALTER TABLE agent_types DROP CONSTRAINT agent_types_state_check;
ALTER TABLE agent_types ADD CONSTRAINT agent_types_state_check
  CHECK (state IN ('provisioning', 'active', 'deleting', 'deleted'));
ALTER TABLE agent_types DROP CONSTRAINT agent_types_tenant_id_slug_key;
CREATE UNIQUE INDEX agent_types_tenant_slug_live ON agent_types (tenant_id, slug) WHERE deleted_at IS NULL;
```

*   Delete deprovisions Kubernetes resources, removes the agent grant, cancels pending and active runs, deletes their sandboxes and their `agent_environments` rows, and sets `state = 'deleted'` and `deleted_at`.
*   Runs, sessions, events and messages stay.
*   Agent lists, slug lookups, `GetAgentType` and `ListAgentTypesByEnvironment` exclude deleted agents. A deleted agent returns 404.
*   A pending run claimed after its agent is deleted fails with the existing missing-agent error.
*   A deleted agent's runs stay readable by the run's initiator and by platform admins. Per-agent run scopes do not cover them. A new agent with the same slug does not expose them.
*   A trigger whose agent is deleted auto-pauses on the 404 as it does today.
*   A new agent with a deleted agent's slug gets a new row and no runs.

### 9. Trigger Source Deletion

*   `triggers_source_id_fkey` becomes `ON DELETE SET NULL`.
*   Deleting a source still revokes every grant its principal holds.
*   A webhook trigger with no source does not fire and reports `source_missing` in the API. Paths that read a trigger's source handle a null `source_id`.
*   An API trigger with no source cannot be re-linked. It can only be disabled or deleted.
*   A declared trigger reports `ResolvedRefs=False`. When the source resource returns, the trigger is re-linked and its grants recreated.

### 10. RBAC

*   The six new CRDs carry `helm.sh/resource-policy: keep`.
*   `inference-gateway` gains `get/list/watch` on `virtualmodels` and `patch` on `virtualmodels/status` in its system namespace Role.
*   The `kora-agents` ClusterRole gains `get/list/watch` on `environments`, `agents`, `virtualmodels`, `connections`, `mcpservers` and `sandboximages`, and `patch` on the status of its kinds.
*   `kora-triggers` gains a ClusterRole with `get/list/watch` on `triggersources`, `triggers`, `agents` and `virtualmodels`, and `patch` on the status of its kinds.
*   `policy-service` gains a ClusterRole with `get/list/watch` on `policyroles` and `patch` on `policyroles/status`.
*   `kora-triggers` and `policy-service` gain the `runkora.io/kube-api-client` label.
*   The operator creates a RoleBinding in each tenant namespace that grants `kora-triggers` Secret `get`. `kora-triggers` reads Secrets through the manager's uncached API reader.
*   Each service gains `get/create/update` on `leases` in the system namespace.

### Rationale

Projection keeps provisioning, grants, runs and analytics on the code that exists. Read-only declared items keep the dashboard and the repository from overwriting each other. The registry gives services the tenant facts they need without new access to `KoraTenant`.

---

## Implementation

The work ships as one stacked chain of pull requests:

1.  This ADR and the PRD.
2.  Agent soft delete in `kora-agents`.
3.  `ON DELETE SET NULL` for trigger sources in `kora-triggers`.
4.  The six CRDs, their Go types, the `KoraTenant` fields, the registry fields and `backend/internal/platform/declarative`.
5.  `VirtualModel` projection, the source column and the platform switch in `inference-gateway`.
6.  `Environment` and `Agent` projection, source columns and API refusals in `kora-agents`.
7.  `TriggerSource` and `Trigger` projection, source columns, the Secret RoleBinding and API refusals in `kora-triggers`.
8.  `PolicyRole` and tenant budget projection, source columns and API refusals in `policy-service`.
9.  The dashboard badge, read-only views, locked-tenant create controls and the `api_authoring` session field.
10.  `docs/guides/declarative-resources.md` with an example per kind, and local examples under `deploy/local/`.

---

## Validation

On the local cluster:

*   Each kind applied in any order becomes `Ready` once its references exist.
*   A declared agent runs, and a declared webhook trigger, scheduled trigger and manual trigger each start a run of a declared agent.
*   A declared role grants its scopes to a user with the role claim.
*   API writes to declared items return 409, and API writes in a locked tenant return 403.
*   Deleting a declared agent keeps its runs. Deleting a declared source leaves its triggers with `ResolvedRefs=False`.
*   A resource with the name of a dashboard-built item reports `NameConflict`.

---

## Consequences

### Positive

*   A tenant's agent setup is a set of manifests that can be reviewed, promoted and reapplied.
*   Declared and dashboard-built agents share one provisioning and policy path.

### Negative

*   Anyone who can write the tenant namespace can define an agent with any scope in that tenant.
*   `kora-triggers` and `policy-service` gain Kubernetes API access, and `kora-triggers` can read every Secret in tenant namespaces.
*   Changing a tenant's namespace requires deleting and recreating the `KoraTenant`.
*   A rotated trigger source Secret takes effect on the next resync, up to 5 minutes later.
*   Inference usage is keyed by `agent_slug`. A new agent with a deleted agent's slug shares its usage history.
*   API triggers can still share a name with each other.
*   A recreated trigger source gets a new id and a new webhook path. The provider's webhook configuration needs the new path.
*   `kora-agents` builds tenant namespaces from `TenantNamespacePrefix` and ignores `KoraTenant.spec.namespace`. A tenant with a custom namespace keeps that gap.

### Neutral

*   A service with one replica holds its own Lease.
*   Existing resource kinds are unchanged.

---

## Related Decisions

*   [Agent Registry and Definition Model](2026-05-11-agent-registry-and-definition-model.md): Postgres stays the agent store. Resources become a second authoring surface.
*   [CRD-Driven Inference Providers](2026-10-01-crd-inference-providers.md): the watcher and status pattern this decision reuses.
*   [Sandbox Environments](2026-10-03-sandbox-environments.md): environments gain a resource form.
*   [Agent Triggers and Webhook Ingestion](2026-06-17-agent-triggers-and-webhook-ingestion.md): trigger source deletion no longer cascades.
*   [Tenant Registration and OIDC Provider Binding](2026-03-31-tenant-registration-and-oidc-provider-binding.md): the registry gains fields.
*   [Install and Distribution Model](2026-09-04-install-and-distribution-model.md): CRD packaging and upgrade rules.
*   [Database Migrations](2026-09-10-database-migrations.md): expand migrations for the new columns.

---

**Last Updated:** 2026-10-24
