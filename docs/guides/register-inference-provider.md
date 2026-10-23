# Register an inference provider

Register each model provider with an `InferenceProvider` resource in the kora system namespace (`orbit-system` by default). The provider's name is `<type>:<metadata.name>`, for example `compatible:openrouter`. Changes to the resource or its Secret apply without a restart.

Store credentials in a Secret in the same namespace. Label it `runkora.io/inference-provider=true` and reference it from the resource.

## Register OpenRouter or any compatible endpoint

The `compatible` type registers any endpoint that serves OpenAI chat completions, Responses or embeddings, or Anthropic Messages. Examples are OpenRouter, OpenAI, Anthropic and vLLM.

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: openrouter
  namespace: orbit-system
  labels:
    runkora.io/inference-provider: "true"
stringData:
  apiKey: sk-or-v1-...
---
apiVersion: runkora.io/v1alpha1
kind: InferenceProvider
metadata:
  name: openrouter
  namespace: orbit-system
spec:
  type: compatible
  displayName: OpenRouter
  models:
    - anthropic/claude-haiku-4.5
    - openai/gpt-oss-20b
  compatible:
    baseURL: https://openrouter.ai/api/v1
    apis:
      - chatCompletions
      - responses
      - messages
    apiKeySecretRef:
      name: openrouter
      key: apiKey
```

**Fields**

- `apis`: the APIs the endpoint serves. One or more of `chatCompletions`, `responses`, `embeddings`, `messages` and `systemone`. See [Register a decision model](#register-a-decision-model).
- `baseURL`: the endpoint's API root, over `http` or `https`. For example `https://openrouter.ai/api/v1`. It must end in `/v1` when `apis` includes `messages`.
- `apiKeySecretRef`: the Secret and key that hold the API key. Omit it for an endpoint without authentication.
- `models`: the models the provider offers. Omit it to offer every model the endpoint lists. Required when the endpoint has no model list.
- `decisionModels`: model ids served on `systemone` by an endpoint that serves chat models too and does not mark its decision models. Requires `systemone` in `apis`. See [Register a decision model](#register-a-decision-model).

**APIs the endpoint does not serve**

The gateway sends a listed API to the endpoint as is. An API missing from `apis` is handled as follows:

- `chatCompletions`: sent as `messages`.
- `responses`: sent as `chatCompletions`, or as `messages` when `chatCompletions` is not listed.
- `messages`: refused.
- `embeddings`: refused.

**Common endpoints**

- Anthropic API: `baseURL` is `https://api.anthropic.com/v1`. `apis` is `messages`.
- OpenAI API: `baseURL` is `https://api.openai.com/v1`. `apis` is `chatCompletions`, `responses` and `embeddings`.

## Register Ollama

Ollama serves all four APIs:

```yaml
apiVersion: runkora.io/v1alpha1
kind: InferenceProvider
metadata:
  name: ollama
  namespace: orbit-system
spec:
  type: compatible
  models:
    - gpt-oss:20b
    - nomic-embed-text:latest
  compatible:
    baseURL: http://ollama.example.internal:11434/v1
    apis:
      - chatCompletions
      - responses
      - embeddings
      - messages
```

## Register AWS Bedrock

```yaml
apiVersion: runkora.io/v1alpha1
kind: InferenceProvider
metadata:
  name: aus
  namespace: orbit-system
spec:
  type: bedrock
  bedrock:
    region: ap-southeast-2
    assumeRoleARN: arn:aws:iam::123456789012:role/orbit-bedrock
    externalID: orbit
```

**Fields**

- `region`: the AWS region.
- `credentials.secretRef`: a labelled Secret with `accessKeyID`, `secretAccessKey` and optional `sessionToken`. Omit it to use the gateway's workload identity, which is IRSA or EKS Pod Identity on the `orbit-inference-gateway` ServiceAccount.
- `assumeRoleARN`: a role to assume, for example in another account. Optional.
- `externalID`: the external ID the role requires. Optional.
- `roleSessionName`: the session name. Optional. The default is `orbit-inference-gateway`.

For local development, `make cluster-bedrock-creds` creates one Bedrock provider per alias in `BEDROCK_ACCOUNTS` in `.env`. It stores your AWS SSO session in the Secret `orbit-bedrock-credentials`. `make up` runs it.

## Register Google Vertex AI

```yaml
apiVersion: runkora.io/v1alpha1
kind: InferenceProvider
metadata:
  name: gcp
  namespace: orbit-system
spec:
  type: vertexai
  vertexai:
    projectID: my-project
    location: us-central1
    anthropicLocation: us-east5
    credentialsSecretRef:
      name: vertex-sa
      key: key.json
```

**Fields**

- `projectID`: the Google Cloud project.
- `location`: the region.
- `anthropicLocation`: the region for Claude models. Optional.
- `credentialsSecretRef`: a labelled Secret and key holding a service account key. Omit it to use GKE Workload Identity.

## Register Azure AI

```yaml
apiVersion: runkora.io/v1alpha1
kind: InferenceProvider
metadata:
  name: eastus
  namespace: orbit-system
spec:
  type: azure
  azure:
    endpoint: https://my-resource.openai.azure.com
    apiVersion: "2024-10-21"
    anthropicEndpoint: https://my-resource.services.ai.azure.com/anthropic
    apiKeySecretRef:
      name: azure-ai
      key: apiKey
```

**Fields**

- `endpoint`: the Azure OpenAI resource endpoint.
- `apiVersion`: the Azure OpenAI API version.
- `anthropicEndpoint`: the endpoint for Claude models. Optional.
- `apiKeySecretRef`: the Secret and key that hold the API key.

## Register a decision model

The gateway serves decision models at `POST /v1/systemone` in the System One API format. Add `systemone` to `apis` on any `compatible` provider whose endpoint serves that API. The gateway calls `<baseURL>/systemone`. Bedrock, Vertex AI and Azure do not serve `systemone`.

**Endpoint with decision models only**

Every model the provider lists is a decision model. This example registers TypeSafe:

```yaml
apiVersion: runkora.io/v1alpha1
kind: InferenceProvider
metadata:
  name: typesafe
  namespace: orbit-system
spec:
  type: compatible
  displayName: TypeSafe
  compatible:
    baseURL: https://api.typesafe.ai/v1
    apis:
      - systemone
    apiKeySecretRef:
      name: typesafe
      key: apiKey
```

**Endpoint with chat and decision models**

For OpenRouter, add `systemone` to the `apis` of the provider that already serves its chat models:

```yaml
  compatible:
    baseURL: https://openrouter.ai/api/v1
    apis:
      - chatCompletions
      - responses
      - systemone
```

The gateway marks a model as a decision model when the endpoint's model list declares `decisions` in `architecture.output_modalities`. OpenRouter does. For an endpoint without that metadata, such as Ollama, list the decision models in `decisionModels`:

```yaml
  compatible:
    baseURL: http://ollama.example.internal:11434/v1
    apis:
      - chatCompletions
      - systemone
    decisionModels:
      - clef
```

A decision model is served only on `/v1/systemone`. Chat models are not served there.

**Offer the model**

Create a virtual model routed to the decision model and grant its scope, as in [Offer a model to agents](#offer-a-model-to-agents). `GET /v1/models` lists the virtual model with the `decision` capability. `GET /v1/models?capability=decision` lists only decision models.

**Call the model**

A call needs the `inference:invoke` scope and the model scope `inference:<family>:<name>:use`. The gateway replaces `model` with the target's model id and forwards the rest of the body unchanged:

```bash
curl -s https://<gateway>/v1/systemone \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "decide/jev",
    "state": {"subject": "Refund not received"},
    "questions": {"urgent": {"type": "noul", "instructions": "Is this urgent?"}}
  }'
```

- The route does not stream, and the request body limit is 10 MB.
- A call times out after 15 seconds. A timeout, a 429 or a 5xx from the endpoint fails over to the next route in the virtual model.
- An endpoint 400 or 422 returns with the endpoint's message. An endpoint 401 or 403 returns 503 `provider_error`.
- Errors use the gateway's envelope (`code`, `message`).

## Network access

The gateway's network policy allows outbound traffic on port 443. For a model server outside the cluster on another port, add its address and port to `networkPolicy.externalServices`. The gateway reaches a model server inside the cluster only on port 443.

The operator creates an Istio ServiceEntry, `inference-provider-<name>`, for each provider's hosts. Deleting the provider deletes it.

For local development, `make up` adds Ollama on the host. Use `baseURL: http://host.k3d.internal:11434/v1`.

## Check a provider

```bash
kubectl get inferenceproviders -n orbit-system
```

- `Ready`: the gateway loaded the provider.
- `Invalid`: the `Loaded` condition gives the reason. For example a missing Secret, a missing key or an unlabelled Secret.

The cluster rejects a resource outside `orbit-system` or without the block for its `type`.

Provider health shows the model count, the last discovery and any upstream error. It appears in:

- the dashboard: Catalog, Providers
- the API: `GET /v1/catalog/providers`
- the kora MCP tool: `models.providers.status`

## Offer a model to agents

1. Find the model under Provider Catalog.
2. Create a virtual model routed to it, on the Catalog page or with `models.virtual.create`.
3. Grant `inference:<family>:<name>:use`.

## List models by capability

`GET /v1/models` accepts an optional `capability` parameter. It keeps models whose capabilities include any requested value. Repeat the parameter or separate values with commas. Valid values are `chat`, `embedding`, `image`, `video`, `rerank`, `audio` and `decision`. An unknown value returns `400` with code `invalid_request`. Without the parameter the list is unchanged.

```bash
curl -H "Authorization: Bearer $TOKEN" "$API/v1/models?capability=embedding"
```

## Prices

- Bedrock, Vertex AI and Azure: synced automatically.
- `compatible` endpoints that publish prices in `/models`, such as OpenRouter: synced on each discovery.
- Other endpoints, such as Ollama, vLLM and TypeSafe: set a price override per model, on the Catalog page or with `models.pricing.set_override`. An override applies at the next pricing refresh, up to an hour later.

An unpriced model records a cost of zero and does not count against budgets.

An override always wins over a synced price.

## Reference

- [Inference gateway](../architecture/inference-gateway.md): routing and the model catalogue.
- [CRD-driven inference providers](../adr/2026-10-01-crd-inference-providers.md): the design and its decisions.
