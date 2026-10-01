# Model providers

Register each model provider with an `InferenceProvider` resource in the Orbit system namespace (`orbit-system` by default). The inference gateway picks up a new, changed or deleted resource, or a rotated Secret, within seconds and without a restart. The provider registers as `<type>:<metadata.name>`, for example `compatible:openrouter`.

Credentials live in a Secret in the same namespace, labelled `orbit.io/inference-provider=true`. The resource names the Secret and never holds the value. Only the inference gateway reads provider credentials.

## Register OpenRouter or any compatible endpoint

The `compatible` type covers any endpoint that speaks the OpenAI or Anthropic APIs: OpenRouter, OpenAI, Anthropic, vLLM. List the APIs the endpoint serves in `apis`.

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: openrouter
  namespace: orbit-system
  labels:
    orbit.io/inference-provider: "true"
stringData:
  apiKey: sk-or-v1-...
---
apiVersion: orbit.io/v1alpha1
kind: InferenceProvider
metadata:
  name: openrouter
  namespace: orbit-system
spec:
  type: compatible
  displayName: OpenRouter
  models:
    - anthropic/claude-sonnet-4.5
    - openai/gpt-4o-mini
  compatible:
    baseURL: https://openrouter.ai/api/v1
    apis: [chatCompletions, responses, messages]
    apiKeySecretRef:
      name: openrouter
      key: apiKey
```

- `apis` takes `chatCompletions`, `embeddings`, `responses` and `messages`. Declared APIs go straight to the endpoint.
- An undeclared API is translated when a declared one can carry it. Chat completions and Responses run over `messages`, and Responses over `chatCompletions`. Undeclared `messages` and `embeddings` are refused.
- Claude Code calls `messages`. Codex calls `responses`. OpenCode calls `chatCompletions`.
- `baseURL` is the prefix of `/chat/completions` and `/messages`. It must end in `/v1` when `messages` is listed.
- `baseURL` may be `http://` or `https://`.
- `apiKeySecretRef` is optional for an unauthenticated server.
- `models` limits what the provider offers. Leave it out to offer every model the endpoint lists. An endpoint without a model list needs it.

For the Anthropic API: `baseURL: https://api.anthropic.com/v1` and `apis: [messages]`. For OpenAI: `baseURL: https://api.openai.com/v1` and `apis: [chatCompletions, embeddings, responses]`.

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
    roleSessionName: platform-team                                 # optional, default orbit-inference-gateway
```

With static keys, add a labelled Secret with keys `accessKeyID`, `secretAccessKey` and optionally `sessionToken`, and name it:

```yaml
    credentials:
      secretRef:
        name: bedrock-credentials
```

Locally, `make cluster-bedrock-creds` writes your AWS SSO session into Secret `orbit-bedrock-credentials` and applies one Bedrock resource per alias in `BEDROCK_ACCOUNTS` in `.env`. `make up` runs it too.

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

`PHASE` is `Ready` when the gateway loaded the resource. `Invalid` carries the reason in the `Loaded` condition, such as a missing Secret, a missing key or an unlabelled Secret. A resource in another namespace, or without the block its `type` names, is rejected on apply.

Runtime health (the model count, the last discovery, any upstream error) is on the dashboard's Catalog page under Providers, in `GET /v1/catalog/providers`, and in the Orbit MCP tool `models.providers.status`.

## Offer a model to agents

A provider's models appear under Provider Catalog. Agents use virtual models. Create one routed to the provider model on the Catalog page or with `models.virtual.create`, then grant `inference:<family>:<name>:use`.

Bedrock, Vertex AI and Azure prices sync automatically. A `compatible` provider has no synced prices: set them per model on the Catalog page or with `models.pricing.set_override`.

A mesh on `REGISTRY_ONLY` needs a ServiceEntry for each provider host.

## Reference

- [Inference gateway](../architecture/inference-gateway.md): routing and the model catalogue.
- [CRD-driven inference providers](../adr/2026-10-01-crd-inference-providers.md): the design and its decisions.
