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

Every create path reads the caller. Agent create checks the caller's identity-mode write permission (`checkAgentTypeModeWrite`) and scope ceiling (`checkCeiling`), and records `created_by` (`creatorSubject`). Trigger create requires the caller to hold `can_run` on the target agent (`requireRunAuthority`). Role upsert checks the caller's scope ceiling (`enforceScopeCeiling`) and budget permission (`roleBudgetChangeAllowed`). A resource applied from a repository has no kora caller.

Deleting an agent hard-deletes its runs before the agent row (`AgentTypeService.DeleteAgentType`). Deleting a trigger source cascades to its triggers through `triggers_source_id_fkey ON DELETE CASCADE` (`backend/cmd/kora-triggers/migrations/20260910000029_create_triggers.sql`).

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

*   **Option A (rejected):** Each owning service watches its own kinds with a controller-runtime manager and writes their status. Four services would gain Kubernetes API access. `kora-triggers` receives public webhooks and would read Secrets in every tenant namespace.
*   **Option B (rejected):** The operator aggregates resources into ConfigMaps that services mount. A service could not write status. Name conflicts, invalid specs and readiness would not reach the resource. Changes would wait for the kubelet sync, and each ConfigMap is limited to 1 MiB.
*   **Option C (rejected):** A new projector binary in the mesh watches every kind and calls each service. It adds a binary, a chart and a deployment for work the operator can do.
*   **Option D (chosen):** The operator watches every kind, calls an internal endpoint on each owning service and writes status from the response.

### Decision 3: Operator Transport

*   **Option A (chosen):** The operator joins the mesh with its webhook, health and metrics ports excluded from inbound interception. It calls services with a platform token from its SPIFFE identity.
*   **Option B (rejected):** The operator calls services from outside the mesh. Strict mTLS refuses a caller with no mesh certificate.

### Decision 4: Deletion Detection

*   **Option A (chosen):** The operator deletes a declared item when its resource is gone, and at startup and every sync period when a declared item's UID matches no resource.
*   **Option B (rejected):** Each resource carries a finalizer. A stopped operator would block every namespace deletion in the cluster.

### Decision 5: Trigger Source Signing Secrets

*   **Option A (chosen):** The operator reads the referenced Secret and sends its value to `kora-triggers` with the item. The operator already holds Secret `get`.
*   **Option B (rejected):** `kora-triggers` reads tenant Secrets itself. An internet-facing service would gain Secret access in every tenant namespace.

### Decision 6: Platform Scopes in Roles

*   **Option A (chosen):** A `PolicyRole` in a tenant namespace cannot hold `platform:*` scopes. A `PolicyRole` in the system namespace can, and states its tenant in `spec.tenant`.
*   **Option B (rejected):** Any `PolicyRole` can hold any scope. Write access to a tenant namespace would grant platform-wide scopes such as `platform:catalog:write`.

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

**Chosen Options:** Decision 1 A, Decision 2 D, Decision 3 A, Decision 4 A, Decision 5 A, Decision 6 A, Decision 7 A, Decision 8 A, Decision 9 A.

### Architecture

```
  repository ──► Argo CD / Flux ──► Kubernetes API
                                          │ watch (one Lease)
                                          ▼
                                       operator
            resolves tenant and references, reads TriggerSource Secrets,
            writes status
                                          │ POST /internal/declared/{kind}
          ┌───────────────┬───────────────┼───────────────┐
          ▼               ▼               ▼               ▼
  inference-gateway  kora-agents     kora-triggers   policy-service
  VirtualModel       Environment     TriggerSource   PolicyRole
                     Agent           Trigger
          │               │               │               │
          ▼               ▼               ▼               ▼
  virtual_models     environments    webhook_sources policy_role_scopes
  (source=crd)       agent_types     triggers
                          │
                          ▼
             same provisioning path as the API
             (StatefulSet, agent grant, mesh policy)
```

### 1. Resource Kinds

All kinds are `runkora.io/v1alpha1`, namespaced, with a status subresource. They ship in `charts/kora-crds`. Go types for all six kinds live in `backend/internal/platform/declarative/v1alpha1/`. controller-gen generates their deepcopy functions and CRDs from markers on the types.

| Kind | Namespace | Owner | Table |
|---|---|---|---|
| `VirtualModel` | system | `inference-gateway` | `virtual_models`, `virtual_model_routes` |
| `Environment` | tenant | `kora-agents` | `environments` |
| `Agent` | tenant | `kora-agents` | `agent_types`, `agent_environments` |
| `TriggerSource` | tenant | `kora-triggers` | `webhook_sources` |
| `Trigger` | tenant | `kora-triggers` | `triggers`, `trigger_routes`, `trigger_route_candidates` |
| `PolicyRole` | tenant, or system with `spec.tenant` | `policy-service` | `policy_role_scopes` |

*   A ValidatingAdmissionPolicy in the `kora` chart rejects a `VirtualModel` outside the system namespace.
*   `metadata.name` is a DNS-1123 label. CEL limits an `Agent` name to a DNS-1035 label of at most 52 characters.
*   Spec fields mirror the API request fields in camelCase. Server-derived fields are absent.
*   A reference to another resource is a name in the same namespace. An `MCPServer` reference also matches a global server in the system namespace. `VirtualModel` references from tenant kinds use `family` and `name`.
*   Secrets are referenced by `{name, key}` in the same namespace. A `TriggerSource` Secret must carry the label `runkora.io/trigger-source=true`.
*   The optional annotation `runkora.io/repository` names the repository that drives a resource. It is display only and grants nothing.
*   `identityMode` is immutable through a CEL `self == oldSelf` rule.
*   `mcpTools` expands to the existing `mcp:<server>:<tool>:use` scopes.
*   `spec.agent` and `spec.routing` are mutually exclusive through CEL. `spec.routing.candidates` and `spec.routing.fallback` are `Agent` names.
*   `spec.schedule` and `spec.manual` carry the existing scheduled and manual configuration.
*   `spec.role` is the identity provider's role claim. It defaults to `metadata.name`.
*   A `PolicyRole` in the system namespace sets `spec.tenant` and can hold any scope. `spec.tenant` is refused in a tenant namespace.

Examples per kind are in `docs/guides/declarative-resources.md` and are validated against the schemas.

### 2. KoraTenant Additions

```yaml
spec:
  apiAuthoring: Disabled   # Enabled (default) or Disabled
  budget: {limitUSD: 1000, resetCadence: monthly}
```

*   The operator adds `apiAuthoring` and `budget` to each entry in the `kora-tenants` registry (`backend/internal/operator/registry.go`).
*   `kora-agents` and `kora-triggers` mount the registry as `identity` and `policy-service` do. The registry is a mounted file and gives them no Kubernetes access.
*   CEL makes `spec.namespace` immutable. An unset namespace can only be set to the derived `kora-tenant-<name>`. The operator refuses to reconcile a tenant whose namespace is the system namespace or another tenant's namespace.
*   The platform switch for virtual models is `inferenceGateway.catalog.apiAuthoring` in the `kora` chart values.
*   A tenant budget on `KoraTenant` replaces any budget set through the API. Removing it from the spec deletes the declared budget.

### 3. Projection

The operator runs one controller per kind under its existing leader election:

*   A reconcile resolves the tenant from the resource's namespace, checks references against the other resources, reads a TriggerSource's Secret and calls the owning service.
*   The operator watches Secrets labelled `runkora.io/trigger-source=true`. A changed or deleted Secret reconciles the `TriggerSource`s that reference it.
*   The service validates the item with its API validation code and applies it through its declared write path. Its response sets the resource's status.
*   The row key is the item's existing unique key: `metadata.name` for `Agent`, `Environment`, `TriggerSource` and `Trigger`, `spec.role` for `PolicyRole`, and `spec.family` with `spec.name` for `VirtualModel`.
*   A live row with the same key and `source = 'api'` leaves the resource unprojected with `Accepted=False` and reason `NameConflict`. Soft-deleted rows do not conflict.
*   When two resources claim the same key, the older resource keeps it and the newer one reports `NameConflict`.
*   A resource in a namespace with no tenant gets `Accepted=False` and reason `UnknownTenant`.
*   A missing reference gives `ResolvedRefs=False` with reason `RefNotFound` and the missing name in the message. A new resource with a missing reference is not projected. A projected resource keeps its last applied row and reports `Ready=False` until its current spec is applied.
*   A reference to a declared kind counts as resolved when the referenced resource is `Ready`. A `Connection`, `MCPServer` or `SandboxImage` reference only needs to exist. A reference to a dashboard-built item counts as missing.
*   Unresolved resources requeue every 30 seconds, and every resource resyncs every 5 minutes.
*   Each row stores its resource UID in `resource_uid`. A reconcile that finds the resource gone deletes its declared row.
*   At startup and every sync period, the operator lists each service's declared items per tenant and deletes those whose UID matches no resource. Deleting a namespace deletes its resources, so a tenant whose namespace is gone loses the items declared there.
*   Deleting an `Environment` resource while an agent is attached keeps the row and retries every 30 seconds.
*   Changing a `Trigger`'s `kind`, or switching between `agent` and `routing`, recreates the trigger. Changing a `TriggerSource`'s `provider` recreates the source with a new id and webhook path.

Each resource reports three conditions:

*   `Accepted`: the spec is valid, the tenant is known and the key is free.
*   `ResolvedRefs`: every reference to another declared kind is `Ready`, and every other reference exists.
*   `Ready`: the current spec is projected and live. An `Agent` is `Ready` when its row reaches `state = 'active'`, and a trigger without a source is never `Ready`.

Status also carries `status.id`, the projected row's id. A `TriggerSource` reports `status.webhookPath`, the ingest path that includes its id.

### 4. Service Endpoints

`backend/internal/platform/declarative` holds the contract and a server helper. The client is `backend/internal/platform/clients/declared`.

*   The operator sends an item: tenant, namespace, name, UID, generation, spec, repository annotation, resolved reference ids and, for a `TriggerSource`, the Secret value.
*   The service returns an outcome (`Projected`, `NameConflict`, `Invalid` or `Pending`) with a message, the item's id and its readiness.
*   Each service implements apply, delete and list once per kind.
*   Each service mounts `POST /internal/declared/{kind}`, `DELETE /internal/declared/{kind}/{tenant}/{name}` and `GET /internal/declared/{kind}/{tenant}` under its base path (`/` for kora-agents, `/api/v1` for kora-triggers and policy-service, `/v1` for inference-gateway).
*   The helper accepts only the operator's system principal.
*   The operator calls every service through one client that embeds `httpclient.Base`.
*   `Apply` is idempotent. The operator re-applies an unready item to read its readiness.

Each `Applier` runs the API's validation and provisioning without a caller:

*   `kora-agents` skips the identity-mode write check, the scope ceiling and `refuseDelegatedAutonomousWrite`. `created_by` is `resource:<namespace>/<name>`. The repository annotation is stored in `repository` and never becomes `created_by`.
*   `kora-triggers` takes agent and source ids from `Item.Refs`. The operator reads them from the referenced resources' `status.id`. A recreated agent gets a new id, and its declared triggers, routed candidates and fallbacks follow it.
*   `kora-triggers` skips `requireRunAuthority` and `authorizeCaller`. The operator refuses a `Trigger` that names a delegated agent before it calls `kora-triggers`.
*   The declared trigger path sets `agent_type_id`, `source_id` and the routing candidates directly. The API still cannot change them.
*   A routed trigger's `routing.model` is a `VirtualModel` reference.
*   `policy-service` skips the role scope ceiling and the budget permission check. A scope missing from the vocabulary gives `ResolvedRefs=False`.
*   Grants for agents, trigger sources and triggers are created as they are today.

The operator owns every spec field. `auto_paused_at` and `consecutive_failures` stay runtime state on `triggers`, and a reconcile never clears an auto-pause.

### 5. Source Columns

Each projected table gains a `source` column in an expand migration, plus the resource columns its declared path needs:

```sql
ALTER TABLE agent_types
  ADD COLUMN source TEXT NOT NULL DEFAULT 'api' CHECK (source IN ('api', 'crd')),
  ADD COLUMN resource_uid TEXT,
  ADD COLUMN resource_generation BIGINT NOT NULL DEFAULT 0;
```

| Table | Columns added |
|---|---|
| `agent_types`, `environments` | `source`, `resource_uid`, `resource_generation`, `repository` |
| `webhook_sources`, `triggers` | `source`, `resource_uid`, `repository` |
| `virtual_models`, `policy_role_scopes` | `source`, `resource_uid`, `resource_name`, `repository` |
| `policy_tenant_budgets` | `source` |

*   Child tables (`virtual_model_routes`, `agent_environments`, `trigger_routes`, `trigger_route_candidates`) follow their parent.

### 6. API Behavior

*   Every list and get response for the projected kinds carries `source: "api" | "crd"`, and `repository` when the resource names one.
*   An update or delete of a `source = 'crd'` row returns HTTP 409 with code `managed_by_resource`. A role `PUT` on a declared role counts as an update.
*   With `apiAuthoring: Disabled`, these writes in the tenant return HTTP 403 with code `api_authoring_disabled`:
    *   Create, update and delete of agents, environments, triggers, trigger sources and roles.
    *   Role budget changes.
*   With the platform switch off, virtual model writes return the same 403.
*   Tenant budget writes return 409 while the tenant budget is declared.
*   These stay available in every case: run starts, manual trigger fires, clearing a trigger's auto-pause, secret rotation on an API source, grants, PATs and user preferences.
*   The agent fields `can_edit` and `can_delete` are false for declared agents and for every agent in a locked tenant.
*   An API trigger cannot take the name of a declared trigger. A declared trigger with the name of a live API trigger reports `NameConflict`.
*   The kora MCP server calls the same APIs and inherits these refusals.
*   `identity` returns `api_authoring` for the caller's tenant on the session response. `GET /v1/catalog/virtual-models` returns `{"data": [...], "api_authoring": bool}`.
*   An API create, or a rename, that takes a declared item's key returns HTTP 409 with code `resource_already_exists`.

### 7. Dashboard

*   Declared items show a "Managed by <repository>" badge, or "Managed in git" when the resource names no repository, and no edit or delete controls.
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
*   A deleted agent's runs stay readable by the run's initiator, the agent's creator and holders of the tenant-wide `agents:runs:read` scope. Per-agent run scopes do not cover them. A new agent with the same slug does not expose them.
*   A trigger whose agent is deleted auto-pauses on the 404 as it does today.
*   A new agent with a deleted agent's slug gets a new row and no runs.

### 9. Trigger Source Deletion

*   `triggers_source_id_fkey` becomes `ON DELETE SET NULL`.
*   Deleting a source still revokes every grant its principal holds.
*   A webhook trigger with no source does not fire and reports `source_missing` in the API. Paths that read a trigger's source handle a null `source_id`.
*   An API trigger with no source cannot be re-linked. It can only be disabled or deleted.
*   A declared trigger reports `ResolvedRefs=False`. When the source resource returns, the trigger is re-linked and its grants recreated.

### 10. Access

*   The six new CRDs carry `helm.sh/resource-policy: keep`.
*   The operator ClusterRole gains `get/list/watch` on the six kinds and `get/patch` on their status.
*   The operator gets an Istio sidecar. Its webhook, health and metrics ports are excluded from inbound interception with `traffic.sidecar.istio.io/excludeInboundPorts`. It mounts the SPIRE workload socket and gets a platform token as `/system/operator`.
*   A mesh AuthorizationPolicy on each service denies `/internal/declared/` under its base path to every principal except the operator. The edge refuses these paths.
*   `inference-gateway`, `kora-agents`, `kora-triggers` and `policy-service` gain no Kubernetes access.

### Rationale

Projection keeps provisioning, grants, runs and analytics on the code that exists. Read-only declared items keep the dashboard and the repository from overwriting each other. The operator already holds the cluster access the feature needs. The owning services stay free of Kubernetes access.

---

## Implementation

The work ships as one stacked chain of pull requests:

1.  This ADR and the PRD.
2.  Agent soft delete in `kora-agents`.
3.  `ON DELETE SET NULL` for trigger sources in `kora-triggers`.
4.  The six CRDs, their Go types, the `KoraTenant` fields, the registry fields, the endpoint contract in `backend/internal/platform/declarative` and its client in `backend/internal/platform/clients/declared`.
5.  The `VirtualModel` endpoint, the source column and the platform switch in `inference-gateway`.
6.  The `Environment` and `Agent` endpoints, source columns and API refusals in `kora-agents`.
7.  The `TriggerSource` and `Trigger` endpoints, source columns and API refusals in `kora-triggers`.
8.  The `PolicyRole` endpoint, tenant budget projection, source columns and API refusals in `policy-service`.
9.  The operator controllers, its mesh sidecar and its platform token.
10.  The dashboard badge, read-only views, locked-tenant create controls and the `api_authoring` session field.
11.  `docs/guides/declarative-resources.md` with an example per kind, and local examples under `deploy/local/`.

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

*   Anyone who can write the tenant namespace can define agents with any scope, including `platform:*`, and `PolicyRole`s with any scope outside `platform:*`.
*   An API agent can attach a declared `Environment`. A later change to that `Environment` widens the agent's egress and connectors without a scope ceiling check.
*   Inference gateway replicas other than the one that served the operator's call load declared virtual model changes on restart. API writes have the same per-replica gap today.
*   Declared changes wait while the operator is down. Projected items keep running from Postgres.
*   The operator's start depends on the mesh issuing it a certificate.
*   The operator depends on an internal endpoint in four services.
*   The authoring switch reaches each service when its registry mount refreshes. Services can disagree for a few minutes.
*   `kora-crds` must be applied before `kora`.
*   Changing a tenant's namespace requires deleting and recreating the `KoraTenant`.
*   Inference usage is keyed by `agent_slug`. A new agent with a deleted agent's slug shares its usage history.
*   API triggers can still share a name with each other.
*   A recreated trigger source gets a new id and a new webhook path. The provider's webhook configuration needs the new path.
*   `kora-agents` builds tenant namespaces from `TenantNamespacePrefix` and ignores `KoraTenant.spec.namespace`. A tenant with a custom namespace keeps that gap.

### Neutral

*   Existing resource kinds are unchanged.

---

## Related Decisions

*   [Agent Registry and Definition Model](2026-05-11-agent-registry-and-definition-model.md): Postgres stays the agent store. Resources become a second authoring surface.
*   [CRD-Driven Inference Providers](2026-10-01-crd-inference-providers.md): the watcher and status pattern this decision reuses.
*   [Sandbox Environments](2026-10-03-sandbox-environments.md): environments gain a resource form.
*   [Agent Triggers and Webhook Ingestion](2026-06-17-agent-triggers-and-webhook-ingestion.md): trigger source deletion no longer cascades.
*   [Tenant Registration and OIDC Provider Binding](2026-03-31-tenant-registration-and-oidc-provider-binding.md): the registry gains fields.
*   [Enforcement Sidecar Injection, Preflight and Restricted Pod Security](2026-09-04-enforcement-sidecar-injection-and-preflight.md): the operator now joins the mesh with its webhook, health and metrics ports excluded.
*   [Install and Distribution Model](2026-09-04-install-and-distribution-model.md): CRD packaging and upgrade rules. The operator's role widens from tenants to declared resources (Amendment 1 there).
*   [Database Migrations](2026-09-10-database-migrations.md): expand migrations for the new columns.

---

**Last Updated:** 2026-10-24
