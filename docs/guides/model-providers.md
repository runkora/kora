# Model providers

A platform admin registers each model provider with an `InferenceProvider` resource in the Orbit system namespace (`orbit-system` by default). The inference gateway picks up a new, changed or deleted resource without a restart. The provider registers as `<type>:<metadata.name>`, for example `openai:openrouter`. See [inference gateway](../architecture/inference-gateway.md) for how routing and the model catalogue work, and [crd-inference-providers](../adr/2026-10-01-crd-inference-providers.md) for the design.

Credentials live in a Secret in the same namespace. The resource names the Secret and never holds the value. Only the inference gateway receives provider credentials. Agents call the gateway with their Orbit identity and never see them.

## Register OpenRouter or any OpenAI-compatible endpoint

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: openrouter
  namespace: orbit-system
stringData:
  apiKey: sk-or-v1-...
---
apiVersion: orbit.io/v1alpha1
kind: InferenceProvider
metadata:
  name: openrouter
  namespace: orbit-system
spec:
  type: openai
  displayName: OpenRouter
  models:
    - openai/gpt-4o-mini
    - meta-llama/llama-3.3-70b-instruct
  openai:
    baseURL: https://openrouter.ai/api/v1
    apiKeySecretRef:
      name: openrouter
      key: apiKey
```

`baseURL` defaults to `https://api.openai.com/v1`, which registers OpenAI itself. Point it at a vLLM, KServe or llm-d service to register a self-hosted model. `apiKeySecretRef` is optional for an unauthenticated server.

`baseURL` must be `https://`. The gateway's sidecar replaces the `Authorization` header of any plain HTTP call with the gateway's own platform token, so a plain HTTP upstream would receive that token instead of its key. Serve an in-cluster model over TLS and set `caCert` to the PEM of its private CA:

```yaml
  openai:
    baseURL: https://vllm.models.svc.cluster.local/v1
    caCert: |
      -----BEGIN CERTIFICATE-----
      ...
      -----END CERTIFICATE-----
``` `models` limits what the provider offers. Leave it out to offer every model the endpoint lists. An endpoint without a `/models` list needs `models`.

An `openai` provider serves `/v1/chat/completions`, `/v1/embeddings` and `/v1/responses`. It does not serve `/v1/messages`. Claude Code needs a model on a `bedrock`, `vertexai`, `azure` or `anthropic` provider. Codex and OpenCode run on any type.

## Register the Anthropic API

```yaml
apiVersion: orbit.io/v1alpha1
kind: InferenceProvider
metadata:
  name: anthropic
  namespace: orbit-system
spec:
  type: anthropic
  anthropic:
    apiKeySecretRef:
      name: anthropic
      key: apiKey
```

`baseURL` defaults to `https://api.anthropic.com`. Set it to register another Anthropic Messages endpoint. It must be `https://`, and `caCert` works as for `openai`.

## Register AWS Bedrock

With the gateway's workload identity (IRSA or EKS Pod Identity on its `orbit-inference-gateway` ServiceAccount):

```yaml
apiVersion: orbit.io/v1alpha1
kind: InferenceProvider
metadata:
  name: aus
  namespace: orbit-system
spec:
  type: bedrock
  bedrock:
    region: ap-southeast-2
    assumeRoleARN: arn:aws:iam::123456789012:role/orbit-bedrock   # optional: another account
    externalID: orbit                                              # optional
```

With static keys, add a Secret with keys `accessKeyID`, `secretAccessKey` and optionally `sessionToken`, and name it:

```yaml
    credentials:
      secretRef:
        name: bedrock-credentials
```

The chart renders one Bedrock resource per `inferenceGateway.bedrock.accounts` entry. Local development does the same from `BEDROCK_ACCOUNTS` in `.env`. `make cluster-bedrock-creds` refreshes the SSO session in Secret `orbit-bedrock-credentials`.

## Register Google Vertex AI

```yaml
apiVersion: orbit.io/v1alpha1
kind: InferenceProvider
metadata:
  name: gcp
  namespace: orbit-system
spec:
  type: vertexai
  vertexai:
    projectID: my-project
    location: us-central1
    anthropicLocation: us-east5          # optional: region for Claude models
    credentialsSecretRef:                # optional: absent uses GKE Workload Identity
      name: vertex-sa
      key: key.json
```

## Register Azure AI

```yaml
apiVersion: orbit.io/v1alpha1
kind: InferenceProvider
metadata:
  name: eastus
  namespace: orbit-system
spec:
  type: azure
  azure:
    endpoint: https://my-resource.openai.azure.com
    apiVersion: "2024-10-21"
    anthropicEndpoint: https://my-resource.services.ai.azure.com/anthropic   # optional
    apiKeySecretRef:
      name: azure-ai
      key: apiKey
```

## Check a provider

```bash
kubectl get inferenceproviders -n orbit-system
```

`PHASE` is `Ready` when the operator accepted the resource. `Invalid` carries the reason in the `Aggregated` condition: a wrong namespace, a missing Secret or a missing key. A resource without the block its `type` names is rejected on apply.

Runtime health (the model count, the last discovery, any connection error) is on the dashboard's Catalog page, in `GET /v1/catalog/providers`, and in the Orbit MCP tool `models.providers.status`.

## Offer a model to agents

A provider's models appear under provider models in the Catalog. Agents use virtual models. Create one routed to the provider model on the Catalog page or with `models.virtual.create`, then grant `inference:<family>:<name>:use` as for any model.

## Env var configuration

`BEDROCK_*`, `VERTEX_*` and `AZURE_*` on the gateway still register providers for one release and log a deprecation warning. A resource with the same provider name replaces the env provider. Env Vertex and Azure providers are named `vertexai` and `azure`. Their resources are named `vertexai:<name>` and `azure:<name>`. Move their routes to the new name in the Catalog.

A mesh on `REGISTRY_ONLY` gets ServiceEntries for the Bedrock accounts in the chart's values. Add a ServiceEntry for any other provider host.
