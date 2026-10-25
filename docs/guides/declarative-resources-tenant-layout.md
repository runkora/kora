# Tenant repository layout

A tenant repository can hold several use cases that share environments, trigger sources and roles. This guide lays out one such repository and promotes it from a dev tenant to a prod tenant with Kustomize overlays.

## Layout

```
example-agents/
├── base/
│   ├── kustomization.yaml
│   ├── shared/
│   │   ├── kustomization.yaml
│   │   ├── environments/
│   │   │   ├── go-build.yaml          # Environment
│   │   │   └── ops-tools.yaml         # Environment
│   │   ├── sources/
│   │   │   ├── github-org.yaml        # ExternalSecret + TriggerSource
│   │   │   ├── slack.yaml             # ExternalSecret + TriggerSource
│   │   │   └── alertmanager.yaml      # ExternalSecret + TriggerSource
│   │   └── roles/
│   │       ├── engineers.yaml         # PolicyRole
│   │       └── sre.yaml               # PolicyRole
│   └── use-cases/
│       ├── pr-review/
│       │   ├── kustomization.yaml
│       │   ├── agent.yaml             # Agent code-reviewer
│       │   └── triggers.yaml          # Trigger review-on-pr
│       ├── incident-triage/
│       │   ├── kustomization.yaml
│       │   ├── agent.yaml             # Agent sre-triage
│       │   └── triggers.yaml          # Trigger on-alert-firing
│       ├── weekly-report/
│       │   ├── kustomization.yaml
│       │   ├── agent.yaml             # Agent pr-reporter
│       │   └── triggers.yaml          # Trigger monday-report
│       └── slack-assistant/
│           ├── kustomization.yaml
│           └── triggers.yaml          # Trigger on-mention, routed
└── overlays/
    ├── dev/
    │   └── kustomization.yaml
    └── prod/
        └── kustomization.yaml
```

- **`base/shared`:** Resources that several use cases reference. An environment, a trigger source or a role is declared once.
- **`base/use-cases`:** One folder per use case, holding its `Agent` and its `Trigger` resources.
- **`overlays`:** One folder per tenant. Each sets the namespace and the repository annotation and patches what differs between tenants.

## Base

`base/kustomization.yaml` lists the shared folder and every use case:

```yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
  - shared
  - use-cases/pr-review
  - use-cases/incident-triage
  - use-cases/weekly-report
  - use-cases/slack-assistant
```

A use case folder lists its own files:

```yaml
# base/use-cases/incident-triage/kustomization.yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
  - agent.yaml
  - triggers.yaml
```

A use case references shared resources by name. The incident triage agent attaches the shared `ops-tools` environment, and its trigger uses the shared `alertmanager` source:

```yaml
# base/use-cases/incident-triage/agent.yaml
apiVersion: runkora.io/v1alpha1
kind: Agent
metadata:
  name: sre-triage
spec:
  displayName: SRE triage
  model: {family: anthropic, name: sonnet}
  systemPrompt: |
    Triage the firing alert and post findings to the incident channel.
  environments:
    - name: ops-tools
      default: true
  budget: {limitUSD: 100, resetCadence: monthly}
---
# base/use-cases/incident-triage/triggers.yaml
apiVersion: runkora.io/v1alpha1
kind: Trigger
metadata:
  name: on-alert-firing
spec:
  source: alertmanager
  eventType: alert.firing
  agent: sre-triage
  promptTemplate: "Triage {{ commonLabels.alertname }}: {{ commonAnnotations.summary }}"
```

A use case can declare triggers only. The Slack assistant routes each mention to one of the agents declared by the other use cases:

```yaml
# base/use-cases/slack-assistant/triggers.yaml
apiVersion: runkora.io/v1alpha1
kind: Trigger
metadata:
  name: on-mention
spec:
  source: slack
  eventType: mention
  routing:
    candidates: [code-reviewer, sre-triage]
    fallback: code-reviewer
    model: {family: anthropic, name: haiku}
  promptTemplate: "{{ event.text }}"
```

A role grants access across use cases. The `sre` role lets SRE users run the triage agent:

```yaml
# base/shared/roles/sre.yaml
apiVersion: runkora.io/v1alpha1
kind: PolicyRole
metadata:
  name: sre
spec:
  role: SRE
  scopes:
    - agents:runs:sre-triage:read
    - agents:runs:sre-triage:write
```

## Add a use case

1. Create a folder under `base/use-cases/` with an `agent.yaml`, a `triggers.yaml` and a `kustomization.yaml` that lists them.
2. Add the folder to `resources` in `base/kustomization.yaml`.
3. Reference shared environments and trigger sources by name. Add a new shared resource when no existing one fits.
4. Add the agent's run scopes to the roles whose users run it.

## Overlays

Each overlay targets one tenant. The prod overlay sets the namespace and the repository annotation:

```yaml
# overlays/prod/kustomization.yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
namespace: kora-tenant-example
commonAnnotations:
  runkora.io/repository: github.com/example-org/example-agents
resources:
  - ../../base
```

The dev overlay targets the dev tenant, lowers agent budgets and reads its signing secrets from dev keys:

```yaml
# overlays/dev/kustomization.yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
namespace: kora-tenant-example-dev
commonAnnotations:
  runkora.io/repository: github.com/example-org/example-agents
resources:
  - ../../base
patches:
  - target: {kind: Agent}
    patch: |
      - op: replace
        path: /spec/budget/limitUSD
        value: 5
  - target: {kind: ExternalSecret, name: github-webhook}
    patch: |
      - op: replace
        path: /spec/data/0/remoteRef/key
        value: example-dev/kora/github-webhook
```

A `TriggerSource` gets its own webhook path in each tenant. Configure each provider with the path from `status.webhookPath` in each tenant.

Point the GitOps tool at one overlay per tenant, such as one Argo CD `Application` or one Flux `Kustomization` for `overlays/dev` and one for `overlays/prod`.

## Tenant settings

The authoring switch is per tenant. A common setup locks prod and leaves dev open for building agents in the dashboard:

- **`kora-tenant-example`:** `apiAuthoring: Disabled`. Every agent, trigger and role comes from the repository.
- **`kora-tenant-example-dev`:** `apiAuthoring: Enabled`. Dashboard-built agents sit next to the declared ones.

`KoraTenant` resources, `VirtualModel` resources and system-namespace `PolicyRole` resources live in the platform team's repository:

```
kora-platform/
├── tenants/
│   ├── example.yaml                      # KoraTenant, apiAuthoring: Disabled
│   └── example-dev.yaml                  # KoraTenant, apiAuthoring: Enabled
├── models/
│   ├── claude-sonnet.yaml             # VirtualModel
│   └── claude-haiku.yaml              # VirtualModel
└── roles/
    └── example-platform-admins.yaml      # PolicyRole with spec.tenant
```

The agents and triggers in `example-agents` reference the models by family and name. A model the platform repository has not declared leaves them at `ResolvedRefs=False`.

## Several repositories

Several repositories can drive one tenant. Each team keeps its own repository and stamps its own `runkora.io/repository` value. Names must stay unique across the repositories of a tenant. Two repositories that declare the same kind and name in one namespace write the same Kubernetes object and overwrite each other. A resource can reference a resource from another repository by name.

## Reference

- [Declarative resources](declarative-resources.md): every kind, field and condition.
- [Tenants](tenants.md): `KoraTenant` and tenant namespaces.
