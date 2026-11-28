# Event types

Every platform event has a type of the form `<domain>.<entity>.<event>.v<N>`. The `<domain>.<entity>` prefix is the category, which the **Events** page groups into tabs and the API filters with `category`. See [events](../concepts/events.md) for the concepts and [event spine](../architecture/event-spine.md) for delivery.

The catalog lists every type below. `GET /v1/events/types` returns the same list, and `GET /v1/events/types/{type}` returns a type's JSON Schema with the field descriptions and the values each field can take. The `events.types` tool in the kora MCP server returns both.

## Envelope attributes

Every event carries these attributes, whatever its type.

| Attribute | Always present | Meaning |
| --- | --- | --- |
| `specversion` | Yes | The CloudEvents version, `1.0` |
| `id` | Yes | The event id |
| `source` | Yes | The binary that recorded the event |
| `type` | Yes | The event type |
| `dataschema` | No | The URL of the type's schema |
| `subject` | Yes | The resource the event is about |
| `time` | Yes | When the event was recorded |
| `datacontenttype` | Yes | `application/json` |
| `koratenant` | Yes | The tenant the event belongs to |
| `koraprincipal` | Yes | The principal that caused the event |
| `koraprincipaltype` | Yes | `USER`, `AGENT`, `TRIGGER` or `SYSTEM` |
| `koraprincipalname` | Yes | The principal's display name |
| `koradelegator` | No | The person an agent acted for |
| `korarun` | No | The run the event concerns |
| `koraactingrun` | No | The run an agent acted from when it caused the event |
| `koraagent` | No | The slug of the agent the event concerns |
| `koracausedby` | No | The event that led to this one |
| `korachaindepth` | Yes | How many trigger-caused steps led to the event |
| `koratriggers` | No | The triggers that started or messaged the run |
| `traceparent` | No | The W3C trace the event belongs to |
| `data` | Yes | The payload: `entity`, `changed`, `entity_version` and `occurred_at` |

`PAT` and human tokens record the principal type `USER`. A service token records `TRIGGER` when its subject is a trigger or trigger source, and `SYSTEM` otherwise.

## Types

### Runs

Category `agents.run`.

| Type | Recorded when |
| --- | --- |
| `agents.run.created.v1` | A run was created and queued. |
| `agents.run.placed.v1` | A run was placed on an agent pod. |
| `agents.run.started.v1` | A run began executing. |
| `agents.run.resumed.v1` | An idle run began a new turn. |
| `agents.run.completed.v1` | A run finished, including a run that ended after its idle timeout. |
| `agents.run.failed.v1` | A run failed. |
| `agents.run.budget_exhausted.v1` | A budget ran out and stopped a run. |
| `agents.run.terminated.v1` | A person or the platform terminated a run. |
| `agents.run.idle.v1` | A run finished its turn and is waiting for input. |
| `agents.run.interrupted.v1` | A person stopped the run's turn. |
| `agents.run.errored.v1` | A turn ended in an error and the run continued. |
| `agents.run.guardrail_warned.v1` | The loop guard warned the agent. |
| `agents.run.guardrail_paused.v1` | The loop guard paused the run. |
| `agents.run.message_received.v1` | A person or a trigger sent the run a message. |
| `agents.run.summarized.v1` | A run's task summary was set. |

### Sandboxes

Category `agents.sandbox`.

| Type | Recorded when |
| --- | --- |
| `agents.sandbox.created.v1` | A sandbox was provisioned for a run. |
| `agents.sandbox.replaced.v1` | A run's sandbox was replaced with a different environment or image. |
| `agents.sandbox.ready.v1` | A sandbox finished provisioning. |
| `agents.sandbox.failed.v1` | A sandbox failed to provision. |
| `agents.sandbox.deleted.v1` | A run's sandbox was removed. |

### Agents

Category `agents.agent`.

| Type | Recorded when |
| --- | --- |
| `agents.agent.created.v1` | An agent was created. |
| `agents.agent.updated.v1` | An agent was changed. |
| `agents.agent.deleted.v1` | An agent was deleted. |

### Environments

Category `agents.environment`.

| Type | Recorded when |
| --- | --- |
| `agents.environment.created.v1` | An environment was created. |
| `agents.environment.updated.v1` | An environment was changed. |
| `agents.environment.deleted.v1` | An environment was deleted. |

### Graders

Category `agents.grader`.

| Type | Recorded when |
| --- | --- |
| `agents.grader.created.v1` | A Grader was created. |
| `agents.grader.updated.v1` | A Grader was changed. |
| `agents.grader.deleted.v1` | A Grader was deleted. |

### Trigger deliveries

Category `triggers.delivery`.

| Type | Recorded when |
| --- | --- |
| `triggers.delivery.failed.v1` | A fire could not start its run. |
| `triggers.delivery.denied.v1` | A fire was refused for lack of permission. |

### Triggers

Category `triggers.trigger`.

| Type | Recorded when |
| --- | --- |
| `triggers.trigger.autopaused.v1` | A trigger paused itself after repeated failures. |
| `triggers.trigger.created.v1` | A trigger was created. |
| `triggers.trigger.updated.v1` | A trigger was changed. |
| `triggers.trigger.deleted.v1` | A trigger was deleted. |

### Trigger sources

Category `triggers.source`.

| Type | Recorded when |
| --- | --- |
| `triggers.source.created.v1` | A webhook source was created. |
| `triggers.source.updated.v1` | A webhook source was changed. |
| `triggers.source.deleted.v1` | A webhook source was deleted. |

### Budgets

Category `inference.budget`.

| Type | Recorded when |
| --- | --- |
| `inference.budget.threshold_crossed.v1` | Spend against a budget passed 50%, 80% or 100% of its limit. |
| `inference.budget.exceeded.v1` | An exhausted budget refused a request. |

### Sessions

Category `identity.session`.

| Type | Recorded when |
| --- | --- |
| `identity.session.created.v1` | A person signed in. |

### Personal access tokens

Category `identity.pat`.

| Type | Recorded when |
| --- | --- |
| `identity.pat.created.v1` | A personal access token was issued. |
| `identity.pat.revoked.v1` | A personal access token was revoked. |
| `identity.pat.updated.v1` | A personal access token's scopes changed. |

### Delegations

Category `identity.delegation`.

| Type | Recorded when |
| --- | --- |
| `identity.delegation.issued.v1` | A person delegated to an agent. |

### Tenant switches

Category `identity.tenant`.

| Type | Recorded when |
| --- | --- |
| `identity.tenant.switched.v1` | A person switched tenant. |

### Grants

Category `policy.grant`.

| Type | Recorded when |
| --- | --- |
| `policy.grant.created.v1` | A grant was created. |
| `policy.grant.updated.v1` | A grant was changed. |
| `policy.grant.deleted.v1` | A grant was deleted. |

### Roles

Category `policy.role`.

| Type | Recorded when |
| --- | --- |
| `policy.role.created.v1` | A role was created. |
| `policy.role.updated.v1` | A role was changed. |
| `policy.role.deleted.v1` | A role was deleted. |

### Tenant budget

Category `policy.tenant_budget`.

| Type | Recorded when |
| --- | --- |
| `policy.tenant_budget.created.v1` | The tenant budget was set. |
| `policy.tenant_budget.updated.v1` | The tenant budget was changed. |
| `policy.tenant_budget.deleted.v1` | The tenant budget was removed. |

### Declared resources

Category `declarative.resource`.

| Type | Recorded when |
| --- | --- |
| `declarative.resource.failed.v1` | A declared resource could not be applied. |
