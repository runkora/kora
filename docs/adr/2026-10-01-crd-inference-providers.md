# CRD-Driven Inference Providers

<!-- Filename: 2026-10-01-crd-inference-providers.md  (decision date + slug; no sequential number) -->

**Status:** Proposed
**Date:** 2026-10-01
**Deciders:** Justin Wilkin
**Technical Story:** [CRD-Driven Inference Providers PRD](../prd/crd-inference-providers.md). Register inference providers with a Kubernetes resource instead of gateway env vars.

---

## Context

The inference gateway builds its provider connectors once at start from env vars.

- Bedrock: `BEDROCK_ACCOUNTS` and `BEDROCK_<ALIAS>_REGION`, with optional per-alias static keys ([Multi-Account Bedrock](2026-06-19-multi-account-bedrock.md)). Without keys the AWS default chain applies (IRSA, EKS Pod Identity). There is no AssumeRole.
- Vertex AI: `VERTEX_ENABLED`, `VERTEX_PROJECT_ID`, `VERTEX_LOCATION`. Application Default Credentials only.
- Azure: `AZURE_ENABLED`, `AZURE_AI_ENDPOINT`, `AZURE_AI_API_KEY`, `AZURE_OPENAI_API_VERSION`.
- There is no connector for the Anthropic API or for OpenAI-compatible endpoints such as OpenAI, OpenRouter or vLLM.

The chart renders the Bedrock env from `inferenceGateway.bedrock.accounts`. The local cluster writes Bedrock regions and credentials into `orbit-secrets`. Every platform service mounts `orbit-secrets` through `envFrom`. A changed credential needs a gateway restart.

`RoutingEngine.connectors` is fixed at construction. Postgres holds virtual models, routes, per-model toggles and prices, keyed by provider name. Three CHECK constraints allow only `bedrock`, `vertexai`, `azure` and `bedrock:<alias>`. There is no providers table.

Every other admin-declared registration in Orbit is a CRD reconciled by the operator: `OrbitTenant`, `Connection`, `MCPServer`, `OAuthClient`. The operator renders CRs into a ConfigMap and a Secret in the system namespace. The consumer mounts them and reloads on change. [JIT Credential Brokering](2026-06-06-jit-credential-brokering-egress-gateway.md), Amendment 3, rejected the operator writing a service's database and rejected the service watching CRs. [MCP Server Registration](2026-06-29-mcp-server-registration-and-introspection.md) split CR status (reconciliation) from runtime health (the consuming service).

This touches *How does it connect?*. Provider credentials move from env into Secrets referenced by a resource, and they stay in the gateway. *Who is this agent?* and *What can it access?* are unchanged. Agents call the gateway with their platform identity. Grants stay on virtual models.

### Goals

- A platform admin registers any provider type the gateway supports with one resource and, where needed, one Secret. No code change, no env var, no restart.
- Creating, changing or deleting a provider takes effect in the running gateway.
- An OpenAI-compatible endpoint with an API key, such as OpenRouter, is a first-class registration.
- Existing env-configured Bedrock installs migrate with no route changes.

### Non-Goals

- Tenant-scoped providers.
- Declaring virtual models, routes or prices as resources.
- Azure Entra workload identity.
- Operator-reconciled ServiceEntries for provider hosts.

---

## Decision Drivers

* Provider credentials never reach an agent, a tenant namespace or a caller.
* Follow the operator's existing aggregation pattern. The gateway gains no Kubernetes API access.
* One source of truth for each fact.
* No restart for any provider change, including credential rotation.
* Existing Bedrock names, routes, grants and prices keep working.

---

## Considered Options

### D1. Resource shape

- **Option A: one kind with a type discriminator and a block per type.** CEL rules require the block that matches `spec.type` and forbid the others.
- **Option B: one generic OpenAI-compatible shape** (base URL and key). Bedrock and Vertex need a region, project and cloud identity that this shape cannot express without a freeform bag.
- **Option C: one kind per vendor.** One reconciler, aggregate and list per kind.
- **Option D: credentials as a separate kind attached to a provider**, as in Envoy AI Gateway's `BackendSecurityPolicy`. A second object and a join, with no second consumer of the credential.

### D2. Scope

- **Option A: namespaced, honoured only in the system namespace.**
- **Option B: cluster-scoped.** A Secret reference then needs a namespace, and nothing stops it naming a tenant namespace.
- **Option C: system and tenant namespaces, as MCPServer.** The catalog tables have no tenant column. Provider keys would sit in tenant namespaces.

### D3. Delivery to the gateway

- **Option A: operator aggregates into a ConfigMap and a Secret; the gateway mounts them and hot-reloads.**
- **Option B: the gateway watches the CRDs.** The gateway gains Kubernetes API access and a second config mechanism.
- **Option C: the operator writes providers into the gateway's Postgres.** Couples the operator to a service schema.
- **Option D: the operator calls a gateway API.** The operator makes no outbound service calls today.

### D4. Source of truth

- **Option A: the resource owns providers; the database keeps the catalog.**
- **Option B: the resource feeds a providers table the API can also edit.** Two writers of one fact.
- **Option C: the API owns providers and the resource is an import.** Credentials would travel through the API.

### D5. Credentials

- **Option A: per-type credential fields referencing same-namespace Secrets, with workload identity where the provider supports it.**
- **Option B: one uniform credentials union for every type.** Allows invalid pairs such as workload identity on an API-key provider.
- **Option C: Secrets only.** Forces static AWS and GCP keys where workload identity exists.

### D6. Migration

- **Option A: read env and resources side by side for one release; a resource replaces an env provider of the same name.**
- **Option B: remove env support in the same release.** Every install must apply resources before upgrading.

---

## Decision

**D1: Option A. D2: Option A. D3: Option A. D4: Option A. D5: Option A. D6: Option A.**

### 1. The `InferenceProvider` resource

`orbit.io/v1alpha1`, kind `InferenceProvider`, short name `oip`, namespaced, status subresource. It ships in `charts/orbit-crds`.

```yaml
apiVersion: orbit.io/v1alpha1
kind: InferenceProvider
metadata:
  name: aus
  namespace: orbit-system
spec:
  type: bedrock            # bedrock | vertexai | azure | openai | anthropic
  displayName: AWS Bedrock (Sydney)
  models: []               # optional upstream model ids; empty offers every discovered model
  bedrock:
    region: ap-southeast-2
    credentials:           # optional; absent uses the gateway's workload identity
      secretRef:
        name: orbit-bedrock-credentials
    assumeRoleARN: ""      # optional
    externalID: ""         # optional
```

Per-type blocks:

| Type | Fields | Credential |
|---|---|---|
| `bedrock` | `region` (required), `assumeRoleARN`, `externalID` | `credentials.secretRef.name`: keys `accessKeyID`, `secretAccessKey`, optional `sessionToken`. Absent: workload identity (IRSA, EKS Pod Identity). AssumeRole applies on top of either. |
| `vertexai` | `projectID`, `location` (required), `anthropicLocation` | `credentialsSecretRef {name, key}`: service account JSON. Absent: Application Default Credentials (GKE Workload Identity). |
| `azure` | `endpoint`, `apiVersion` (required), `anthropicEndpoint` | `apiKeySecretRef {name, key}`, required. |
| `openai` | `baseURL` (default `https://api.openai.com/v1`), `caCert` | `apiKeySecretRef {name, key}`, optional. |
| `anthropic` | `baseURL` (default `https://api.anthropic.com`), `caCert` | `apiKeySecretRef {name, key}`, required. |

- CEL rules: the block named by `spec.type` is set and no other block is. `metadata.name` is a DNS label.
- `baseURL`, `endpoint` and `anthropicEndpoint` must be `https://`. The gateway pod's identity-proxy replaces the `Authorization` header of every plain HTTP call with the gateway's platform token. A plain HTTP upstream would lose its key and receive that token. The operator rejects a plain HTTP URL with reason `InvalidSpec` as well.
- `caCert` is an inline PEM authority for an upstream signed by a private CA, such as an in-cluster vLLM. It is public material and sits in the resource, as `Connection.spec.caCert` does.
- `openai` covers OpenAI, OpenRouter, vLLM, KServe and any other OpenAI-compatible endpoint. It serves chat completions and embeddings natively and the Responses API through the existing chat-completions bridge. It does not serve Anthropic Messages.
- `anthropic` covers the Anthropic API and any Anthropic-compatible endpoint. It serves Anthropic Messages natively, chat completions through the existing OpenAI-to-Anthropic conversion, and Responses through the existing Anthropic bridge.
- The provider name is `<type>:<metadata.name>`. A resource `aus` of type `bedrock` registers `bedrock:aus`, the name `BEDROCK_ACCOUNTS=aus` registers today.
- `spec.models` filters discovery. When discovery fails or the upstream has no list endpoint, `spec.models` is the offered list.

An OpenRouter registration:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: openrouter
  namespace: orbit-system
stringData:
  apiKey: <key>
---
apiVersion: orbit.io/v1alpha1
kind: InferenceProvider
metadata:
  name: openrouter
  namespace: orbit-system
spec:
  type: openai
  displayName: OpenRouter
  models: ["openai/gpt-4o-mini", "meta-llama/llama-3.3-70b-instruct"]
  openai:
    baseURL: https://openrouter.ai/api/v1
    apiKeySecretRef:
      name: openrouter
      key: apiKey
```

### 2. Scope

- The operator honours resources in the system namespace only. A resource elsewhere is not aggregated and its status is `Invalid` with reason `WrongNamespace`.
- Providers are platform-wide. Which tenants and agents may use a model is decided by grants on the virtual models routed to it.

### 3. Operator reconciliation

- A dedicated `InferenceProviderReconciler` watches `InferenceProvider`. Any event, annotation changes included, runs one aggregator over every resource in the system namespace.
- For each resource it validates the spec, reads the referenced Secret, and copies each referenced key into the aggregate as `<metadata.name>.<field>`.
- Output, in the system namespace:
  - ConfigMap `orbit-inference-providers`, key `providers.json`: one entry per valid resource with its provider name, type, display name, models, type config, and the aggregate Secret key of each credential. No credential values.
  - Secret `orbit-inference-provider-secrets`: the credential values.
- Unchanged output is not written. Output is sorted.
- The chart never renders either aggregate.
- Status per resource: `phase` `Ready` or `Invalid`, condition `Aggregated` with a reason (`WrongNamespace`, `SecretNotFound`, `SecretKeyNotFound`, `InvalidSpec`), `providerName`, `observedGeneration`, `lastReconciledAt`. Status is reconciliation state only.
- The operator does not watch Secrets. It re-renders on its periodic resync. A rotated Secret reaches the aggregate within one resync period.
- RBAC: the operator ClusterRole gains `get/list/watch` on `inferenceproviders` and `get/patch` on `inferenceproviders/status`.
- The file contract (entry types and key naming) lives in `internal/platform/inferenceproviders`. The operator and the gateway both import it. Neither imports the other.

### 4. Gateway loading and hot reload

- The gateway mounts the ConfigMap at `/etc/orbit/inference-providers` and the Secret at `/etc/orbit/inference-provider-secrets`, both `optional: true`.
- It watches both directories with fsnotify and a 200ms debounce.
- On change it reads the entries, reads each credential file, builds a connector per entry, merges the legacy env providers underneath, and swaps the whole set into the routing engine under a lock.
- In-flight requests finish on the connector they started with.
- A connector that fails to build is left out and its error is recorded for the providers API. The others load.
- A read error on the files keeps the current set.
- Model discovery reruns after every swap.
- The gateway's ServiceAccount gains no RBAC.

### 5. Credentials

- The resource carries Secret names and keys, never values.
- The referenced Secret lives in the system namespace. The deployer's secret pipeline (External Secrets, SOPS, sealed secrets) creates it. The charts create none.
- The aggregate Secret is mounted by the inference gateway only.
- Bedrock `assumeRoleARN` wraps the base credentials (Secret or workload identity) in an STS AssumeRole provider with `externalID` when set.
- Vertex without a Secret uses Application Default Credentials. With a Secret it uses the service account JSON.
- There is no caller credential passthrough and no override header.
- Agents keep calling the gateway with their platform identity. Agent pod egress stays locked to the platform services ([Agent Pod Egress Lockdown](2026-09-30-agent-pod-egress-lockdown.md)).

### 6. Source of truth, API, MCP and dashboard

- The resource is the only definition of a provider. There is no providers table.
- Postgres keeps virtual models, routes, per-model toggles and prices.
- `models.providers.list`, `models.providers.refresh` and `models.providers.set_enabled` keep their meaning: provider models, rediscovery and per-model toggles.
- New `GET /v1/catalog/providers` (`platform:catalog:read`) returns each loaded or failed provider: name, type, display name, source (`resource` or `environment`), resource name, region or endpoint, credential mode (`secret`, `workload identity`, `none`), status (`ready` or `error`), error, model count and last discovery time. It returns no credential.
- New MCP tool `models.providers.status` proxies that endpoint.
- The dashboard Catalog page gains a read-only Providers section with the same fields. The empty state shows the resource to apply. There is no create or edit form.

### 7. Discovery and pricing

- Each type discovers models from its upstream: Bedrock foundation models and inference profiles; Vertex Model Garden; Azure, OpenAI-compatible and Anthropic `/models`.
- Pricing folds a provider name to its type, as it already folds `bedrock:<alias>`. The LiteLLM source map gains `anthropic` and `openai`.
- A third-party OpenAI-compatible provider, such as OpenRouter, gets prices through the existing override API.

### 8. Policy

- Scopes stay `inference:<family>:<name>:use` on virtual models. Providers never appear in a scope.
- The catalog admin scopes stay `platform:catalog:read` and `platform:catalog:write`.

### 9. Database

One expand migration in `backend/cmd/inference-gateway/migrations/` replaces the provider CHECK on `virtual_model_routes`, `provider_model_overrides` and `inference_usage` with `provider ~ '^[a-z]+(:[a-z0-9][-a-z0-9_]*)?$'`. `isValidProvider` checks the same syntax and a known type.

### 10. Migration

- For one release the gateway still reads `BEDROCK_*`, `VERTEX_*` and `AZURE_*`. It logs a deprecation warning for each env provider. A resource with the same provider name replaces the env provider.
- The chart renders each `inferenceGateway.bedrock.accounts` entry as an `InferenceProvider` using workload identity. It no longer renders `BEDROCK_*` env. Provider names are unchanged. No route changes.
- The chart's Bedrock ServiceEntries add `bedrock-mantle.<region>.api.aws`.
- Local development: `create-secrets.sh` writes the SSO session into Secret `orbit-bedrock-credentials` and applies one resource per alias in `.env`. `orbit-secrets` no longer carries Bedrock values. `make cluster-bedrock-creds` patches that Secret and bumps an annotation on the resources. It does not restart the gateway.
- Env Vertex and Azure keep the names `vertexai` and `azure` during the window. A resource renames them to `vertexai:<name>` and `azure:<name>`. Their routes are updated through the catalog.
- The release after next removes env provider loading.

### Rationale

One typed kind gives admission-time validation per provider with one reconciler and one list. The system namespace keeps keys out of tenant namespaces and matches the platform-wide catalog. Aggregation keeps the gateway free of Kubernetes API access, the same boundary the egress gateway holds. The resource owns what carries credentials. The database keeps what admins tune daily. Reusing `bedrock:<alias>` names makes the Bedrock migration a no-op for routes, grants and prices.

---

## Consequences

### Positive

- A provider is one reviewable manifest in GitOps. An evaluator registers OpenRouter with a Secret and a resource.
- Provider changes and credential rotation apply without a restart.
- Provider credentials leave `orbit-secrets`. Only the inference gateway mounts them.
- One gateway identity can reach many AWS accounts through AssumeRole.
- The gateway gains Anthropic API and OpenAI-compatible connectors.

### Negative

- Two provider sources for one release.
- Secret rotation waits for the operator resync, 300s by default.
- Renaming a resource renames its provider. Routes, overrides and prices keyed on the old name stop matching.
- Claude Code needs a model on a provider that speaks Anthropic Messages. An `openai` provider does not serve it.
- Installs on `REGISTRY_ONLY` add a ServiceEntry for any provider host other than Bedrock.
- A tenant cannot bring its own provider.

### Neutral

- Virtual models, routes, grants and prices are unchanged.
- The catalog MCP tools keep their names and meaning.

---

## Implementation

- `charts/orbit-crds/templates/crd-inferenceprovider.yaml`.
- `backend/internal/operator/inferenceprovider_types.go`, `inferenceprovider_aggregator.go`, `inferenceprovider_controller.go`; registration in `cmd/operator/main.go` and `scheme.go`.
- `backend/internal/platform/inferenceproviders/`: file contract, loader and watcher.
- `backend/internal/inferencegw/providers/openaicompat/` and `providers/anthropic/`: new connectors.
- `backend/internal/inferencegw/providers/registry/`: builds a connector from an entry for every type.
- `backend/internal/inferencegw/`: swappable connectors in the routing engine, provider status, `GET /v1/catalog/providers`, provider-name validation and pricing by type.
- `backend/cmd/inference-gateway/migrations/`: the CHECK widening.
- `backend/internal/orbitmcp/tools_models.go`: `models.providers.status`.
- `frontend/src/app/(dashboard)/model-gateway/catalog/`: the Providers section.
- `charts/orbit/`: operator RBAC, gateway mounts, rendered resources from `inferenceGateway.bedrock.accounts`, ServiceEntry host.
- `scripts/cluster/create-secrets.sh`, `scripts/cluster/refresh-bedrock-creds.sh`, `.env.example`.
- `docs/guides/model-providers.md`: one example per type, OpenRouter included.
- Status lines: [Inference Gateway Architecture](2026-04-01-inference-gateway-architecture.md) "BYOP Credential Model" and [Multi-Account Bedrock](2026-06-19-multi-account-bedrock.md) amended by this ADR.

---

## Validation

On the local cluster:

- With no `BEDROCK_*` env, a Bedrock `InferenceProvider` serves `/v1/models` and chat completions.
- Creating, changing and deleting a resource changes the served providers without a gateway restart.
- Claude Code, Codex and OpenCode runs complete on models served by resource-defined providers.
- `inference:<family>:<name>:use` grants allow and deny as before.
- A resource with a missing block is rejected at admission. A resource with a missing Secret reports `Invalid`.
- An install on env Bedrock upgrades and keeps serving with the same provider names.
- An `openai` resource pointing at an in-cluster OpenAI-compatible mock serves chat completions and Responses.
- The dashboard lists the providers and their status.

---

## Related Decisions

- [Inference Gateway Architecture](2026-04-01-inference-gateway-architecture.md): its env-at-startup credential model is amended.
- [Multi-Account AWS Bedrock Support](2026-06-19-multi-account-bedrock.md): `bedrock:<alias>` names are kept; env configuration is deprecated.
- [Virtual Model Catalog and Platform Routing](2026-04-17-virtual-model-catalog-and-platform-routing.md): the catalog stays in Postgres.
- [JIT Credential Brokering via a Per-Tenant Egress Gateway](2026-06-06-jit-credential-brokering-egress-gateway.md): the aggregate-and-mount pattern.
- [MCP Server Registration & Introspection](2026-06-29-mcp-server-registration-and-introspection.md): status versus runtime health.
- [Install and Distribution Model](2026-09-04-install-and-distribution-model.md): CRD packaging and upgrade rules.
- [Database Migrations](2026-09-10-database-migrations.md): expand-only constraint widening.

---

## References

- [Envoy AI Gateway (Agent Router) API](https://theagentrouter.ai/docs/api/)
- [kagent ModelConfig](https://github.com/kagent-dev/kagent/blob/main/go/api/v1alpha3/modelconfig_types.go)
- [agentgateway API](https://agentgateway.dev/docs/kubernetes/latest/reference/api/)
- [LiteLLM proxy config](https://docs.litellm.ai/docs/proxy/configs)
- [Crossplane AWS authentication](https://github.com/crossplane-contrib/provider-upjet-aws/blob/main/AUTHENTICATION.md)

---

**Last Updated:** 2026-10-01
