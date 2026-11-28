# Declarative resources

In kora, the models, environments, agents, trigger sources, triggers, Graders and roles a tenant runs can live in a Git repository as Kubernetes resources. The kora operator applies each resource, and the item runs as if you had built it in the console. The console marks declared items as managed in Git and shows no edit controls for them.

The following guide assumes kora is installed and your tenant exists. See [Tenants](tenants.md) to create one. The [kora examples repository](https://github.com/runkora/examples) holds complete repositories for one tenant and for several tenants.

## Lay out the repository

All kinds are `runkora.io/v1alpha1`. Declare `VirtualModel` resources, and roles that hold `platform:` scopes, in the kora system namespace (`kora-system` by default). Declare the other kinds in the tenant's namespace. A platform team usually keeps the system resources in its own repository, and each tenant team keeps a repository for its tenant.

Set the namespace and the `runkora.io/repository` annotation once with Kustomize:

```yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
namespace: kora-tenant-example
commonAnnotations:
  runkora.io/repository: github.com/example-org/example-agents
resources:
  - environment.yaml
  - agent.yaml
  - trigger-source.yaml
  - triggers.yaml
  - role.yaml
```

The console shows "Managed by" and the annotation's value on each declared item. The annotation is for display only and grants nothing.

Commit the resources to your GitOps repository, or `kubectl apply` them for a quick start. Apply them in any order. A resource waits until the resources it references are ready.

## Offer a model

A `VirtualModel` is a model every tenant can use, served as `<family>/<name>`. Declare it in the system namespace.

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
      modelID: us.anthropic.claude-sonnet-5-5
      priority: 1
```

Each route names a provider as `<type>:<InferenceProvider name>`. A provider model can be the route of one virtual model only. See [Register an inference provider](register-inference-provider.md#offer-a-model-to-agents) for strategies and routes.

## Create an environment

An `Environment` gives agents a sandbox image, a size, egress hosts and connectors.

```yaml
apiVersion: runkora.io/v1alpha1
kind: Environment
metadata:
  name: go-build
spec:
  displayName: Go build
  sandboxImage: kora-go
  size: { cpu: "2", memory: 4Gi, disk: 10Gi }
  egressHosts: [proxy.golang.org]
  connectors:
    - connection: github
      actions: [read]
```

`sandboxImage` and each `connection` name a `SandboxImage` or a `Connection` in the same namespace. See [Create an environment](create-an-environment.md) for images and sizes.

## Create an agent

An `Agent` is an agent whose slug is its `metadata.name`.

```yaml
apiVersion: runkora.io/v1alpha1
kind: Agent
metadata:
  name: code-reviewer
spec:
  displayName: Code reviewer
  description: Reviews pull requests for correctness.
  identityMode: autonomous
  model: { family: anthropic, name: sonnet }
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
  budget: { limitUSD: 50, resetCadence: monthly }
```

- `model`: the family and name of a `VirtualModel`.
- `environments`: the `Environment` resources the agent can use. Mark one as the default.
- `mcpTools`: the tools the agent can call on each `MCPServer` in the tenant or system namespace.
- `identityMode`: fixed once the agent exists. To change it, delete and recreate the `Agent`.

The agent's grant comes from its spec. See [Create an agent](create-an-agent.md) for what each setting does.

## Connect a trigger source

A `TriggerSource` is the webhook endpoint a provider posts events to. Its signing secret comes from a Secret in the same namespace with the label `runkora.io/trigger-source=true`. Keep the value out of Git. Create the Secret with a tool such as External Secrets Operator, and set the label through the tool's template:

```yaml
apiVersion: external-secrets.io/v1
kind: ExternalSecret
metadata:
  name: github-webhook
spec:
  refreshInterval: 1h
  secretStoreRef:
    kind: ClusterSecretStore
    name: aws-secrets-manager
  target:
    name: github-webhook
    template:
      metadata:
        labels:
          runkora.io/trigger-source: "true"
  data:
    - secretKey: secret
      remoteRef:
        key: example/kora/github-webhook
---
apiVersion: runkora.io/v1alpha1
kind: TriggerSource
metadata:
  name: github-org
spec:
  provider: github
  signingSecretRef: { name: github-webhook, key: secret }
```

Set the provider's webhook URL to the path in `status.webhookPath` on the API host:

```bash
kubectl get triggersources -n kora-tenant-example -o wide
```

For a system with no built-in provider, use `provider: generic`:

```yaml
apiVersion: runkora.io/v1alpha1
kind: TriggerSource
metadata:
  name: tracker
spec:
  provider: generic
  config:
    verification:
      { method: hmac_sha256, header: X-Signature, encoding: hex }
    event_type: { path: event }
    attribution:
      links:
        - label: "#{{ data.number }} {{ data.title }}"
          url: "https://tracker.example.com/projects/{{ data.project_id }}/issues/{{ data.id }}"
      actor: { name: Tracker, kind: system }
      object:
        label: "#{{ data.number }} {{ data.title }}"
        url: "https://tracker.example.com/projects/{{ data.project_id }}/issues/{{ data.id }}"
      verbs:
        issue.created: created
        issue.comment.created: commented on
  signingSecretRef: { name: tracker-webhook, key: secret }
```

`verification.method` is `hmac_sha256`, `standard_webhooks` or `bearer`. `header`, `prefix` and `encoding` apply to `hmac_sha256` only, and `event_type` takes a `header` or a `path`.

The optional `attribution` block sets the links and the event summary a run shows. [Create a trigger](create-a-trigger.md#connect-any-other-system) describes its `links`, `actor`, `object` and `verbs` keys.

Recreating a `TriggerSource`, or changing its `provider`, gives it a new webhook path.

## Create a trigger

A `Trigger` starts runs of an agent on a webhook event, a schedule, a manual fire or a platform event.

```yaml
apiVersion: runkora.io/v1alpha1
kind: Trigger
metadata:
  name: review-on-pr
spec:
  source: github-org
  eventType: pull_request
  match:
    action: [opened, synchronize]
  agent: code-reviewer
  promptTemplate: "Review {{ pull_request.html_url }}"
```

Set `kind: scheduled` with a `schedule`, `kind: manual` with `manual.inputs`, or `kind: event` with an `eventType` such as `agents.run.failed.v1`, for the other kinds. An event trigger also takes `match`, `matchContains` and `ignoreOwnRuns`. A trigger names one `agent` or a `routing` block of candidate agents. Every agent it starts must be an autonomous `Agent` resource. See [Create a trigger](create-a-trigger.md) for filters, prompts and routing.

## Grade an agent's runs

A `Grader` asks fixed questions about each run of one agent when the run ends. A decision model on the System One API answers them from the run's recorded events.

```yaml
apiVersion: runkora.io/v1alpha1
kind: Grader
metadata:
  name: code-reviewer-review
spec:
  agent: code-reviewer
  description: Checks that a review reports correctness problems.
  model: { family: typesafe, name: jev-1-14 }
  minPassRate: 90%
  appliesWhen: Was the agent asked to review a pull request?
  questions:
    - name: posted-review
      type: boolean
      question: Did the agent post a review through its own tool calls?
      expect: true
    - name: findings
      type: scale
      question: How many correctness problems did the review report?
      levels:
        - { name: no-review, description: No review was posted }
        - { name: none-found, description: No correctness problems }
        - { name: some, description: One or more correctness problems }
      accept: [none-found, some]
      skipOn: [no-review]
```

A question's `type` is `boolean`, `choice` or `scale`. A `boolean` sets `expect`. A `choice` lists `options` and a `scale` lists `levels`, lowest first. Both set `accept`, and can set `skipOn` for answers that mean the question has no subject on the run. A `scale` needs a `skipOn` level. kora adds a `none` option to every `choice`. `threshold` sets the pass and fail band, 0.7 and 0.3 by default, and a question can set its own. `minPassRate` is optional.

A run's verdict is the worst verdict of its questions: `fail` beats `uncertain`, and `uncertain` beats `pass`. The Grader passes when its last 30 graded runs pass at `minPassRate` or above. A Grader with `minPassRate` and under 15 graded runs reads "not enough runs". A Grader without `minPassRate` reads "no minimum" at any count. A change to `model`, `threshold`, `appliesWhen` or `questions` is a new version, and regrades the last 30 graded runs. A Grader grades runs that end after it is applied. See [Graders](../concepts/graders.md) for how verdicts work and [Create a Grader](create-a-grader.md) for a walkthrough.

Anyone who can read an agent's runs can read its grades. A delegated agent's Graders also need `agents:delegated:read`. Their counts include runs other people started. kora-agents serves the grades:

| Endpoint | Returns |
| --- | --- |
| `GET /api/v1/runs/{id}/grades` | The run's grade per Grader: the current version's, or the newest version that graded the run. Each grade has its status, verdict, model, input tokens, cost, state cap and answers with their question text. A run that has not ended has none. |
| `GET /api/v1/agent-types/{id}/graders` | Each Grader of the agent with its passed and graded counts over the current version's last 30 graded runs, its `too_large` count and its status: `passing`, `failing`, `not_enough_runs` or `no_minimum`. Failing Graders come first, then by pass rate, lowest first. Graders with no graded runs come last. |
| `GET /api/v1/agent-types/{id}/graders/{name}` | The same counts, the version's spec, and per question the failed and uncertain runs of the window. Per question it also lists every answer in spec order: yes and no, a choice's options with `none`, or a scale's levels lowest first. Each answer says whether it passes or skips the question, and counts the window's runs whose stored probabilities make it the most likely answer. A tie goes to the answer first in spec order. A yes or no answer is yes at p 0.5 or above. |
| `GET /api/v1/runs?grader=&question=&verdict=` | Runs with a grade on the Grader's current version. `verdict` is the run's verdict, or with `question` that question's answer verdict. |

## Grant access with a role

A `PolicyRole` grants scopes to every user in the tenant whose identity provider role claim matches `role`.

```yaml
apiVersion: runkora.io/v1alpha1
kind: PolicyRole
metadata:
  name: reviewers
spec:
  role: Reviewers
  scopes:
    - agents:runs:code-reviewer:read
    - agents:runs:code-reviewer:write
  budget: { limitUSD: 200, resetCadence: monthly }
```

`budget` gives each holder the limit, or one shared limit with `pooled: true`. A role in a tenant namespace cannot hold `platform:` scopes. Declare such a role in the system namespace and set `spec.tenant` to the tenant's name. See [Policy and roles](policy-and-roles.md) for scopes.

## Lock a tenant

Set `apiAuthoring: Disabled` on the tenant's `KoraTenant` to make the repository the only way to change its agents, environments, trigger sources, triggers and roles:

```yaml
apiVersion: runkora.io/v1alpha1
kind: KoraTenant
metadata:
  name: example
spec:
  # displayName, oidc and the other fields as in the Tenants guide.
  apiAuthoring: Disabled
  budget: { limitUSD: 1000, resetCadence: monthly }
```

With `Disabled`:

- The console, API and MCP server refuse those writes with HTTP 403 and the code `api_authoring_disabled`.
- Items built in the console become read-only.
- Runs, manual fires, grants and PATs stay available.

`budget` sets the tenant's spending limit from the repository.

Virtual models have one switch for the platform. Set the `kora` chart value `inferenceGateway.catalog.apiAuthoring` to `false`.

## Check status

```bash
kubectl get environments,agents.runkora.io,triggersources,triggers.runkora.io,graders,policyroles -n kora-tenant-example
kubectl describe agents.runkora.io code-reviewer -n kora-tenant-example
```

Each resource reports three conditions. The condition message lists what is missing.

| Condition      | True when                                                                 | Reasons when false                         |
| -------------- | ------------------------------------------------------------------------- | ------------------------------------------ |
| `Accepted`     | The spec is valid, the namespace belongs to a tenant and the name is free | `Invalid`, `UnknownTenant`, `NameConflict` |
| `ResolvedRefs` | Every resource it references exists and is ready                          | `RefNotFound`, `Pending`                   |
| `Ready`        | The item is live with the current spec                                    | `Pending`                                  |

A declared resource references declared resources only. An item built in the console counts as missing, even when its name matches.

## Change and delete resources

| What                                                 | Happens                                                                   |
| ---------------------------------------------------- | ------------------------------------------------------------------------- |
| Edit a resource                                      | The item changes to match.                                                |
| Delete a resource                                    | The item is deleted. A deleted agent keeps its runs, sessions and events. |
| Delete a `TriggerSource`                             | Its triggers stay and stop firing until the source is declared again.     |
| Delete a `Grader`                                    | Its grades stay. Views no longer show them.                               |
| Declare a name that a console item holds             | The resource reports `NameConflict`, and the console item stays.          |
| Edit or delete a declared item in the console or API | The API refuses with HTTP 409 and the code `managed_by_resource`.         |

## Who can write these resources

Anyone who can write these kinds in a tenant namespace can define an agent with any scope in that tenant. Anyone who can write a `PolicyRole` in the system namespace can give any tenant's users any scope. Grant write access to these kinds only to the identity your GitOps tool runs as.

## Reference

See the [declarative resources ADR](../adr/2026-10-24-declarative-resources.md) for how the operator projects resources. Examples for the local cluster are in `deploy/local/declarative/`.
