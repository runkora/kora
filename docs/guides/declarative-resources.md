# Declarative resources

Virtual models, environments, agents, trigger sources, triggers and roles can be defined as Kubernetes resources and kept in a Git repository. The kora operator watches every kind and applies each resource through the service that owns it. The service writes the item into the same tables the dashboard, API and kora MCP server write. A declared agent is provisioned, granted and run like an agent built in the dashboard.

All kinds are `runkora.io/v1alpha1` and namespaced. `VirtualModel` lives in the kora system namespace (`kora-system` by default). The other kinds live in a tenant namespace and belong to the tenant that owns it. A `PolicyRole` may also live in the system namespace and set `spec.tenant`.

Each kind has a short name for `kubectl`, an owning service, and a key that identifies its item:

- `VirtualModel` (`kvm`): `inference-gateway`, keyed by `spec.family` and `spec.name`.
- `Environment` (`kenv`): `kora-agents`, keyed by `metadata.name`.
- `Agent` (`kag`): `kora-agents`, keyed by `metadata.name`.
- `TriggerSource` (`ktsrc`): `kora-triggers`, keyed by `metadata.name`.
- `Trigger` (`ktrig`): `kora-triggers`, keyed by `metadata.name`.
- `PolicyRole` (`kpr`): `policy-service`, keyed by `spec.role`, or `metadata.name` when `spec.role` is unset.

The key is the item's name in the API. `metadata.name` is a DNS label. An `Agent` name is a DNS-1035 label of at most 52 characters and cannot be `delegated` or `autonomous`.

Working examples for the local example tenant are in `deploy/local/declarative/`.

## VirtualModel

A `VirtualModel` is a platform-wide catalog model, served as `<family>/<name>`. The cluster rejects one outside the system namespace.

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

- `family` and `name`: lowercase letters, digits, `_` and `-`. The family is the first segment of the model's scope `inference:<family>:<name>:use`.
- `strategy`: `priority` (default), `weighted` or `lowest_cost`. The weights of the enabled routes must sum to 100.
- `routes[].provider`: a provider name of the form `<type>:<InferenceProvider name>`, as described in [Register an inference provider](register-inference-provider.md).
- `routes[].priority` defaults to 1. `enabled` on the model and on each route defaults to `true`.

A provider model can be the route of one virtual model only. A route that another virtual model serves leaves the resource at `Accepted=False` with reason `NameConflict`.

## Environment

An `Environment` is a sandbox environment that agents attach by its `metadata.name`.

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
  connectors:
    - connection: github
      actions: [read]
```

- `sandboxImage`: a `SandboxImage` in the same namespace. Empty runs `kora-base`. A missing image gives `ResolvedRefs=False`, and an image the operator has not validated gives `Accepted=False` with reason `Invalid`.
- `size`: Kubernetes quantities. An empty dimension takes the install default.
- `egressHosts`: exact hosts or `*.suffix`.
- `connectors`: `Connection` names in the same namespace, the actions allowed, and optional `resources` to narrow the connector.

See [Create an environment](create-an-environment.md) for images and sizes.

## Agent

An `Agent` is an agent type whose slug is its `metadata.name`.

```yaml
apiVersion: runkora.io/v1alpha1
kind: Agent
metadata:
  name: code-reviewer
  namespace: kora-tenant-acme
spec:
  displayName: Code reviewer
  description: Reviews pull requests in the acme repositories for correctness.
  framework: claude-code
  identityMode: autonomous
  model: {family: anthropic, name: sonnet}
  systemPrompt: |
    Review pull requests for correctness.
  environments:
    - name: go-build
      default: true
  connectivity: {mode: none}
  connectors:
    - connection: github
      actions: [read, write]
  mcpTools:
    - server: github
      tools: [create_review]
  budget: {limitUSD: 50, resetCadence: monthly}
```

- `framework`: `claude-code` (default), `codex` or `opencode`.
- `identityMode`: `autonomous` (default) or `delegated`. To change the identity mode of an existing agent, delete and recreate the `Agent`.
- `model`: a `VirtualModel` by family and name.
- `environments`: `Environment` names, with one marked as the default. An agent with none runs `kora-base` at the default size.
- `connectivity.mode`: `none` (default), `whitelist` or `open`. The `hosts` list applies in `whitelist` mode.
- `mcpTools`: the tools the agent may call on each `MCPServer`. Each tool becomes the scope `mcp:<server>:<tool>:use`. The server is looked up in the tenant namespace, then in the system namespace.
- `scopes`: further scopes on the agent's grant.
- `guardrails`: loop-guard thresholds `repeatedCallWarn`, `repeatedCallPause`, `waitRearmWarn` and `waitRearmPause`. Each pause must be above its warn.
- `budget`: `limitUSD`, `resetCadence` (`weekly` or `monthly`, default `monthly`) and `perRunLimitUSD`.
- `maxConcurrentRunsPerPod`: runs one agent pod holds at once. Unset takes the install default.

The agent's grant is derived from its spec with no caller scope ceiling. See [Create an agent](create-an-agent.md) for what each setting does.

## TriggerSource

A `TriggerSource` is a webhook endpoint for a provider. Its signing secret comes from a Secret in the same namespace. The Secret must carry the label `runkora.io/trigger-source=true`. The operator reads the Secret, ignores it without the label, and sends its value to kora-triggers.

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: github-webhook
  namespace: kora-tenant-acme
  labels:
    runkora.io/trigger-source: "true"
stringData:
  secret: <the webhook secret configured in GitHub>
---
apiVersion: runkora.io/v1alpha1
kind: TriggerSource
metadata:
  name: github-org
  namespace: kora-tenant-acme
spec:
  provider: github
  signingSecretRef: {name: github-webhook, key: secret}
```

- `provider`: the webhook provider, such as `github` or `slack`.
- `config`: provider-specific configuration, as the API accepts it.
- `enabled` defaults to `true`.

`status.webhookPath` is the ingest path to configure in the provider, `/api/v1/webhooks/<status.id>` on the API host. It appears in `kubectl get triggersources -o wide`. A changed or deleted Secret reconciles the source at once.

A recreated `TriggerSource`, or a change of `provider`, creates a new source with a new id and webhook path. Update the provider's webhook configuration to the new path.

## Trigger

A `Trigger` starts runs of an `Agent` on a webhook event, a schedule or a manual fire. `kind` defaults to `webhook`.

```yaml
apiVersion: runkora.io/v1alpha1
kind: Trigger
metadata:
  name: review-on-pr
  namespace: kora-tenant-acme
spec:
  kind: webhook
  source: github-org
  eventType: pull_request
  match:
    action: [opened, synchronize]
  agent: code-reviewer
  promptTemplate: "Review {{ pull_request.html_url }}"
```

A scheduled trigger takes `schedule` and no `source`:

```yaml
spec:
  kind: scheduled
  agent: code-reviewer
  schedule:
    cronExpression: "0 9 * * 1-5"
    timezone: Australia/Sydney
  promptTemplate: "List the pull requests still open on {{ var.date }}."
```

- Set exactly one of `agent` and `routing`.
- A webhook trigger needs `source` and `eventType`. `match`, `matchContains`, `senderRestriction`, `coalesce` and `routing` apply to webhook triggers only.
- `schedule` takes a five-field `cronExpression`, a `timezone` and `catchUp`. `timezone` is an IANA name and defaults to `UTC`.
- `manual.inputs` lists the fields a manual trigger accepts. Each input has a `name` and a `type` of `text`, `number`, `boolean` or `choice`.
- `routing.candidates` and `routing.fallback` are `Agent` names, and the fallback must be one of the candidates. `routing.model` is a `VirtualModel` in the system namespace.
- `maxParallel`, `overflow`, `maxQueueDepth`, `timeoutSeconds`, `dedupeEnabled` and `autopauseEnabled` take the platform defaults when unset.

Placeholders use `{{ field }}` with no leading dot. See [Create a trigger](create-a-trigger.md) for filters, prompts and routing.

Every agent a declared trigger starts must be a `Ready` `Agent` resource. A trigger that names a delegated agent gives `Accepted=False` with reason `Invalid`. A changed `kind`, or a switch between `agent` and `routing`, recreates the trigger with a new id. A reconcile never clears an auto-pause.

## PolicyRole

A `PolicyRole` grants scopes to every user in the tenant whose identity provider role claim matches.

```yaml
apiVersion: runkora.io/v1alpha1
kind: PolicyRole
metadata:
  name: reviewers
  namespace: kora-tenant-acme
spec:
  role: Reviewers
  scopes:
    - agents:runs:code-reviewer:read
    - agents:runs:code-reviewer:write
  budget: {limitUSD: 200, resetCadence: monthly}
```

- `role`: the role claim, defaulting to `metadata.name`. A built-in role name gives `Accepted=False` with reason `Invalid`. Two roles of one tenant cannot share a role or a `metadata.name`.
- `scopes`: scopes from the vocabulary. A scope missing from the vocabulary gives `ResolvedRefs=False` with reason `Pending` until it exists. A per-agent scope exists once its agent is created.
- `budget`: `limitUSD` above zero, `resetCadence` (default `monthly`) and `pooled`. Each holder gets the limit, or all holders share it when `pooled` is set.
- A role in a tenant namespace cannot hold `platform:` scopes. Declare such a role in the system namespace and set `spec.tenant` to the tenant's name.
- `spec.tenant` is refused in a tenant namespace. A system-namespace role without it, or with a tenant that is not active, gives `Accepted=False`.

The scopes are not checked against a caller's ceiling. See [Policy and roles](policy-and-roles.md) for scopes and [Set budgets](set-budgets.md) for budgets.

## Tenant settings

`KoraTenant` carries two related fields, shown here in an excerpt of a tenant spec:

```yaml
apiVersion: runkora.io/v1alpha1
kind: KoraTenant
metadata:
  name: acme
spec:
  # displayName, oidc and the other tenant fields as in the Tenants guide.
  apiAuthoring: Disabled
  budget: {limitUSD: 1000, resetCadence: monthly}
```

- `apiAuthoring`: `Enabled` (default) or `Disabled`. With `Disabled`, the dashboard, API and MCP server cannot create, edit or delete agents, environments, triggers, trigger sources or roles in the tenant, or change role budgets. Those writes return HTTP 403 with code `api_authoring_disabled`. Existing dashboard-built items become read-only.
- `budget`: the tenant's spending limit. It replaces a tenant budget set through the API, and API writes to the tenant budget return HTTP 409 while it is set. Removing it deletes the declared budget.

Run starts, manual trigger fires, clearing a trigger's auto-pause, secret rotation on an API source, grants, PATs and user preferences stay available in a locked tenant.

The platform switch for virtual models is the `kora` chart value `inferenceGateway.catalog.apiAuthoring`. It defaults to `true`. With `false`, virtual model writes through the API, dashboard and MCP server return HTTP 403 with code `api_authoring_disabled`. `GET /v1/catalog/virtual-models` returns the switch as `api_authoring`.

## Status

Each resource reports three conditions:

- `Accepted`: the spec is valid, the namespace belongs to a tenant and the key is free. Reasons are `Accepted`, `Invalid`, `UnknownTenant` and `NameConflict`.
- `ResolvedRefs`: every reference to another declared kind is `Ready`, and every other reference exists. Reasons are `Resolved`, `RefNotFound` and `Pending`. The message lists what is missing or what the service waits for.
- `Ready`: the current spec is projected and live. Reasons are `Projected` and `Pending`, and a trigger without a source is never `Ready`.

`status.id` is the id of the projected item. While an `Agent` provisions, it already carries its `status.id` and reports `Ready=False` with reason `Pending` until the agent is active.

```bash
kubectl get virtualmodels -n kora-system
kubectl get environments,agents.runkora.io,triggersources,triggers.runkora.io,policyroles -n kora-tenant-acme
kubectl describe agents.runkora.io code-reviewer -n kora-tenant-acme
```

Resources can be applied in any order. The operator retries a resource that is not `Ready` every 30 seconds and resyncs every resource every 5 minutes. A new resource with a missing reference is not projected. A projected resource that loses a reference keeps its last applied state and reports `Ready=False` until its current spec is applied.

## References

A declared resource references other declared resources only:

- A reference is a resource name in the same namespace. An `mcpTools` server can also be a global server in the system namespace. Secrets are referenced by `{name, key}` in the same namespace.
- `model` and `routing.model` reference a `VirtualModel` resource by family and name.
- A reference to an `Environment`, `Agent`, `TriggerSource` or `VirtualModel` resolves once that resource is `Ready`. The operator passes its `status.id` to the service.
- An item built in the dashboard or API counts as missing, even when its name matches.

Dashboard-built items may reference declared items. For example, a dashboard-built agent can attach a declared environment.

## Name conflicts

A resource whose key belongs to a live item created through the API reports `Accepted=False` with reason `NameConflict`, and the API item stays. A soft-deleted agent does not hold its slug.

When two `VirtualModel` or two `PolicyRole` resources claim the same key, the older resource keeps it and the newer one reports `NameConflict`. A refused resource does not hold its key.

## Repository

The annotation `runkora.io/repository` names the repository that drives a resource, such as `github.com/acme/payments-agents`. The service stores it on the item, and the API returns it as `repository`. The dashboard shows "Managed by" the repository instead of "Managed in git". A declared agent's `created_by` stays `resource:<namespace>/<name>`.

The annotation is for display only. It grants nothing, and kora does not check it.

Each repository stamps its own value. Several repositories can drive one tenant, and each item shows the repository it came from. Kustomize sets the annotation on every resource of a repository with `commonAnnotations`:

```yaml
# kustomization.yaml
commonAnnotations:
  runkora.io/repository: github.com/acme/payments-agents
resources:
  - agents.yaml
  - triggers.yaml
```

## Edits through the API

The API returns `source: "api"` or `source: "crd"` on each item of these kinds. The dashboard marks declared items as described in [Repository](#repository) and shows no edit or delete controls. The API refuses writes to them:

- An update or delete of a declared item returns HTTP 409 with code `managed_by_resource`. Change the resource instead.
- A create, or a rename, that takes a declared item's key returns HTTP 409 with code `resource_already_exists`.
- A locked tenant refuses writes as described in [Tenant settings](#tenant-settings).

Declared agents report `can_edit` and `can_delete` as `false`.

## Deletion

Deleting a resource deletes its item:

- **Agent:** the agent is soft deleted. Its workload, grant, pending and active runs, and sandboxes are removed. Its runs, sessions and events stay. Per-agent run scopes no longer cover those runs. A recreated `Agent` is a new agent with a new id and no runs, and declared triggers follow it.
- **Environment:** the environment is deleted. An environment still attached to an agent is kept and retried every 30 seconds.
- **TriggerSource:** the source and its grants are deleted. Its triggers stay without a source and do not fire. A declared trigger reports `ResolvedRefs=False` and is re-linked when the source resource is `Ready` again. An API trigger without a source can only be disabled or deleted.
- **Trigger**, **PolicyRole** and **VirtualModel:** the item is deleted.

The operator also deletes items whose resource is gone when it starts and every sync period (`syncPeriodSecs`, 5 minutes by default). This catches a resource deleted while the operator or a service was down. A tenant that is inactive is skipped. A tenant whose namespace is gone loses the items declared there, as deleting a namespace deletes its resources.

## Access

Anyone who can write these kinds in a tenant namespace can define an agent with any scope in that tenant, and a role with any scope outside `platform:`. Anyone who can write a `PolicyRole` in the system namespace can give any tenant's users any scope. Grant write access to these kinds only to the identity your GitOps tool runs as.

The operator reads trigger source Secrets and calls each service's internal endpoint. The services that own the items have no Kubernetes access.

## Reference

- [Declarative resources ADR](../adr/2026-10-24-declarative-resources.md): the design and its decisions.
- [Tenants](tenants.md): `KoraTenant` and tenant namespaces.
