# CRD-Driven Inference Providers

<!-- Filename: 2026-10-01-crd-inference-providers.md  (decision date + slug; no sequential number) -->

**Status:** Proposed
**Date:** 2026-10-01
**Deciders:** Justin Wilkin
**Technical Story:** [CRD-Driven Inference Providers PRD](../prd/crd-inference-providers.md). Register inference providers with a Kubernetes resource instead of gateway env vars.

---

## Context

The inference gateway builds its provider connectors once at start from env vars: `BEDROCK_ACCOUNTS` and per-alias regions and keys, `VERTEX_*` and `AZURE_*`. The chart renders the Bedrock env from `inferenceGateway.bedrock.accounts`. Local development writes Bedrock credentials into `orbit-secrets`, which every platform service mounts through `envFrom`. A changed provider or credential needs a gateway restart. There is no connector for OpenAI, Anthropic or other compatible endpoints.

`RoutingEngine.connectors` is fixed at construction. Postgres holds virtual models, routes, per-model toggles and prices, keyed by provider name. Three CHECK constraints allow only `bedrock`, `vertexai`, `azure` and `bedrock:<alias>`.

The gateway pod carries the identity-proxy ext_proc filter. It replaces the `Authorization` header of every plain HTTP outbound call with the gateway's platform token. TLS calls pass through Envoy untouched.

This touches *How does it connect?*. Provider credentials move from env into Secrets that only the gateway reads, and they stay in the gateway. *Who is this agent?* and *What can it access?* are unchanged. Agents call the gateway with their platform identity. Grants stay on virtual models.

### Goals

- A platform admin registers any provider type with one resource and, where needed, one Secret. No code change, no env var, no restart.
- Creating, changing or deleting a provider or its Secret takes effect in the running gateway.
- An endpoint such as OpenRouter that serves several APIs is one registration.

### Non-Goals

- Tenant-scoped providers.
- Virtual models, routes or prices as resources.
- Azure Entra workload identity.
- ServiceEntries per provider for `REGISTRY_ONLY` meshes.

---

## Decision Drivers

* Provider credentials never reach an agent, a tenant namespace or a caller.
* The gateway reads no more of the cluster than it needs.
* One source of truth for each fact.
* No restart for any provider change, credential rotation included.

---

## Considered Options

### D1. Resource shape

- **Option A: one kind with a type discriminator and a block per type,** enforced by CEL.
- **Option B: one generic shape for every provider.** Bedrock and Vertex need a region, a project and cloud identity that a base URL and a key cannot express.
- **Option C: one kind per vendor.** One list, watch and dashboard section per kind.

### D2. Generic endpoints

- **Option A: one `compatible` type whose resource declares the APIs the endpoint serves.** Declared APIs pass through. Undeclared APIs use an existing bridge or are refused.
- **Option B: separate `openai` and `anthropic` types.** OpenRouter serves chat completions, Responses and Anthropic messages from one base URL and one key. It would need two resources and two provider names.

### D3. Delivery to the gateway

- **Option A: the gateway watches InferenceProviders and their Secrets itself.** Changes apply on the watch event.
- **Option B: the operator renders resources into a ConfigMap and a Secret the gateway mounts.** Changes wait for the kubelet mount sync, about a minute. A rotated Secret waits for the operator's resync. The file format is a second contract between two binaries.

### D4. Secret access

- **Option A: Secrets in the system namespace, labelled `orbit.io/inference-provider=true`.** The gateway caches only labelled Secrets.
- **Option B: a dedicated providers namespace.** RBAC confines the gateway to that namespace's Secrets. Resources and their Secrets move out of the system namespace.

### D5. Status

- **Option A: the gateway writes each resource's status** with the outcome of its build.
- **Option B: no status.** A missing Secret shows only in the gateway API.

### D6. Env var configuration

- **Option A: remove it in the same release.** Every install applies resources as it upgrades.
- **Option B: read env and resources side by side for a window.** Two code paths and a migration guide.

---

## Decision

**D1: A. D2: A. D3: A. D4: A. D5: A. D6: A.**

### 1. The `InferenceProvider` resource

`orbit.io/v1alpha1`, kind `InferenceProvider`, short name `oip`, namespaced, status subresource. It ships in `charts/orbit-crds`. The Go types live in `internal/platform/inferenceproviders`.

| Type | Fields | Credential |
|---|---|---|
| `bedrock` | `region` (required), `assumeRoleARN`, `externalID`, `roleSessionName` (default `orbit-inference-gateway`) | `credentials.secretRef.name`: keys `accessKeyID`, `secretAccessKey`, optional `sessionToken`. Absent: workload identity (IRSA, EKS Pod Identity). AssumeRole applies on top of either. |
| `vertexai` | `projectID`, `location` (required), `anthropicLocation` | `credentialsSecretRef {name, key}`: service account JSON. Absent: Application Default Credentials (GKE Workload Identity). |
| `azure` | `endpoint`, `apiVersion` (required), `anthropicEndpoint` | `apiKeySecretRef {name, key}`, required. |
| `compatible` | `baseURL` (required), `apis` (required), `caCert` | `apiKeySecretRef {name, key}`, optional. |

- CEL requires the block named by `spec.type` and forbids the others. `metadata.name` is a DNS label.
- The provider name is `<type>:<metadata.name>`. Routes, overrides and usage filters accept only that form.
- `spec.models` optionally limits the offered models. When the upstream has no model list, it is the list.
- A ValidatingAdmissionPolicy in the `orbit` chart rejects an InferenceProvider outside the system namespace.

### 2. The `compatible` type

- `apis` lists what the endpoint serves: `chatCompletions`, `embeddings`, `responses`, `messages`.
- Declared APIs pass through natively.
- An undeclared API runs through an existing bridge when a declared API can carry it:
  - chat completions over `messages`, through the OpenAI-to-Anthropic conversion;
  - Responses over `chatCompletions`, else over `messages`, through the existing Responses bridges.
- `messages` and `embeddings` have no bridge. An endpoint that does not declare them refuses them with HTTP 400 and a message naming the API.
- `baseURL` is the prefix of `/chat/completions` and `/messages`. An endpoint serving `messages` needs a base URL ending in `/v1`; the Anthropic SDK appends `v1/messages`.
- The key is sent as `Authorization: Bearer` on the OpenAI APIs and as `x-api-key` on messages.
- `baseURL` must be `https://`. On plain HTTP the gateway's identity-proxy would replace the key with the gateway's platform token. `caCert` is an inline PEM for a private CA, as `Connection.spec.caCert` is.
- One connector implements the type. It reuses the `convert` and `bridge` packages.

An OpenRouter registration:

```yaml
apiVersion: orbit.io/v1alpha1
kind: InferenceProvider
metadata:
  name: openrouter
  namespace: orbit-system
spec:
  type: compatible
  compatible:
    baseURL: https://openrouter.ai/api/v1
    apis: [chatCompletions, responses, messages]
    apiKeySecretRef:
      name: openrouter
      key: apiKey
```

### 3. The gateway watches

- The gateway runs a controller-runtime cache scoped to the system namespace. It caches InferenceProviders and the Secrets labelled `orbit.io/inference-provider=true`.
- At start it waits for the cache, builds every provider and constructs the routing engine with them.
- Any add, spec change or delete of a resource or a labelled Secret triggers one debounced rebuild. The new connector set is swapped into the routing engine. In-flight requests finish on the connector they started with.
- A resource that fails to build is left out and described with its error. The others load.
- Model discovery reruns after every rebuild, bounded by a timeout. A rebuild cancels the previous discovery, and results from a replaced provider set are dropped.
- When `spec.models` is set and discovery fails, the declared models are still offered and the error shows in the provider's health.
- RBAC is a Role in the system namespace: `get/list/watch` on `inferenceproviders`, `patch` on `inferenceproviders/status`, and `get/list/watch` on `secrets`. RBAC cannot narrow Secrets by label; the label bounds what the gateway caches and reads.
- The gateway pod carries `orbit.io/kube-api-client: "true"` to reach the API server.
- The operator does not reconcile InferenceProviders.

### 4. Status

- The gateway writes `phase` (`Ready` or `Invalid`), `providerName`, `observedGeneration` and a `Loaded` condition. Reasons: `Loaded`, `InvalidSpec`, `SecretNotFound`, `SecretKeyNotFound`, `BuildFailed`.
- It writes only a change. Every replica computes the same status.
- Runtime health (model count, last discovery, upstream errors) is in `GET /v1/catalog/providers`, the MCP tool `models.providers.status` and the dashboard Providers tab. None returns a credential.

### 5. Source of truth

- The resource is the only definition of a provider. There is no providers table and no env var configuration.
- Postgres keeps virtual models, routes, per-model toggles and prices. `models.providers.list`, `.refresh` and `.set_enabled` keep their meaning.

### 6. Discovery, pricing and policy

- Each type discovers models from its upstream. `inferencegw.InferFamily` names the vendor of a model known only by its id, for every provider.
- Bedrock, Vertex AI and Azure prices sync per type and are shared by every resource of that type. A `compatible` provider is priced under its own name through the override API, which creates the row when no synced price exists.
- Scopes stay `inference:<family>:<name>:use` on virtual models. Providers never appear in a scope.

### 7. Database

One expand migration replaces the provider CHECK on `virtual_model_routes`, `provider_model_overrides` and `inference_usage` with `provider ~ '^[a-z]+(:[a-z0-9][-a-z0-9_]*)?$'`.

### 8. Env vars and local development

- The gateway reads no provider env vars. `inferenceGateway.bedrock.accounts` is removed from the chart, with its Bedrock ServiceEntries.
- `make cluster-bedrock-creds` writes the SSO session into Secret `orbit-bedrock-credentials` and applies one Bedrock resource per alias in `.env`. `make up` runs it when `BEDROCK_ACCOUNTS` is set.

### Rationale

One typed kind gives admission-time validation per provider with one list. A declared-API generic type makes a multi-API endpoint one resource. It reuses the bridges the gateway already has. Watching puts changes and rotations in effect on the event and removes a second contract between two binaries. The label keeps the gateway's cache and reads to provider Secrets inside the one namespace it may read. Removing env configuration leaves one path to test and document.

---

## Consequences

### Positive

- A provider is one reviewable manifest. OpenRouter is one resource serving three APIs.
- Provider changes and credential rotation apply within seconds, without a restart.
- Provider credentials leave `orbit-secrets`. Only the gateway reads them.
- One gateway identity can reach many AWS accounts through AssumeRole.

### Negative

- The gateway holds Kubernetes API access. Its Role allows reading every Secret in the system namespace, even though it caches and reads only labelled ones. The gateway is a platform service in that namespace and already receives `orbit-secrets` through `envFrom`.
- Every provider Secret needs the `orbit.io/inference-provider=true` label. An unlabelled Secret reports `SecretNotFound`.
- Upgrading needs the resources applied in the same deploy. There is no env fallback.
- Renaming a resource renames its provider. Routes, overrides and prices keyed on the old name stop matching.
- `REGISTRY_ONLY` installs add a ServiceEntry for every provider host.

### Neutral

- Virtual models, routes, grants and prices are unchanged.
- A dedicated providers namespace remains open. Moving resources and their Secrets there confines the Role to provider Secrets without changing the resource.

---

## Implementation

- `charts/orbit-crds/templates/crd-inferenceprovider.yaml`.
- `backend/internal/platform/inferenceproviders/`: API types.
- `backend/internal/inferencegw/providers/registry/`: build per type, the watcher and status writes.
- `backend/internal/inferencegw/providers/compatible/`: the compatible connector.
- `backend/internal/inferencegw/`: swappable connectors, provider status, `GET /v1/catalog/providers`, `InferFamily`, provider-name validation, pricing by type.
- `backend/cmd/inference-gateway/`: start the watcher and seed the routing engine; the CHECK widening migration.
- `backend/internal/orbitmcp/tools_models.go`: `models.providers.status`.
- `frontend/src/app/(dashboard)/model-gateway/catalog/`: the Providers tab.
- `charts/orbit/`: the gateway Role, API server label, namespace env and the admission policy.
- `scripts/cluster/refresh-bedrock-creds.sh`, `scripts/cluster/create-secrets.sh`, `Makefile`, `.env.example`.
- `docs/guides/model-providers.md`.

---

## Validation

On the local cluster:

- A Bedrock resource serves `/v1/models`, chat completions and messages with no provider env in the gateway.
- Creating, changing and deleting a resource, and rotating its Secret, apply without a restart.
- Claude Code, Codex and OpenCode runs complete on resource-defined models.
- One `compatible` resource serves chat completions, Responses and messages natively.
- Grants allow and deny as before.
- Invalid resources are rejected at admission or report the reason in their status.
- The gateway's service account can read nothing outside the system namespace.

---

## Related Decisions

- [Inference Gateway Architecture](2026-04-01-inference-gateway-architecture.md): its env-at-startup credential model is replaced.
- [Multi-Account AWS Bedrock Support](2026-06-19-multi-account-bedrock.md): replaced; one Bedrock resource per account.
- [Virtual Model Catalog and Platform Routing](2026-04-17-virtual-model-catalog-and-platform-routing.md): the catalog stays in Postgres.
- [Install and Distribution Model](2026-09-04-install-and-distribution-model.md): CRD packaging and upgrade rules.
- [Database Migrations](2026-09-10-database-migrations.md): expand-only constraint widening.

---

## References

- [Envoy AI Gateway (Agent Router) API](https://theagentrouter.ai/docs/api/)
- [kagent ModelConfig](https://github.com/kagent-dev/kagent/blob/main/go/api/v1alpha3/modelconfig_types.go)
- [agentgateway API](https://agentgateway.dev/docs/kubernetes/latest/reference/api/)
- [LiteLLM proxy config](https://docs.litellm.ai/docs/proxy/configs)

---

**Last Updated:** 2026-10-01
