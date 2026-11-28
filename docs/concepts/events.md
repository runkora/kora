# Events

An event is a durable record that something happened in a tenant: a run failed, an agent was created, a budget was crossed, a person signed in. runkora records an event for each change of state and each outcome, together with who caused it. Events are kept for a retention period, shown on the **Events** page, readable through the API and the kora MCP server, and able to start runs through [platform event triggers](../guides/create-a-trigger.md#set-up-the-event).

Events are CloudEvents 1.0 documents. Each type is named `<domain>.<entity>.<event>.v<N>`, such as `agents.run.failed.v1`. The `<domain>.<entity>` prefix is the event's category. See [event types](../reference/events.md) for the full list and [event spine](../architecture/event-spine.md) for how events are recorded and delivered.

## What an event carries

| Attribute | Meaning |
| --- | --- |
| `id`, `type`, `time` | The event's unique id, its type and when it was recorded |
| `subject` | The resource the event is about |
| `koratenant` | The tenant the event belongs to |
| `koraprincipal`, `koraprincipaltype`, `koraprincipalname` | The principal that caused the event, its type (`USER`, `AGENT`, `TRIGGER` or `SYSTEM`) and its display name |
| `koradelegator` | The person an agent acted for, when it acted for one |
| `korarun`, `koraagent` | The run and the agent slug the event concerns, when there is one |
| `koracausedby`, `korachaindepth`, `koratriggers` | The event that led to this one, how many trigger-caused steps led to it, and the triggers that started or messaged the run |
| `traceparent` | The trace the event belongs to |
| `data` | The payload: the resource after the change (`entity`), the fields an update changed (`changed`), the resource version and when the change happened |

For a delete, `data.entity` holds the resource as it was before the change. An update event lists the fields that changed in `data.changed`. The payload of a resource never holds a secret value. Fields that an update can change but that a payload leaves out, such as an agent's system prompt, appear by name in `changed` without their values.

An event is recorded in the same database transaction as the change it describes. A change that rolls back records no event, and a committed change always has one.

## What is recorded

Events cover runs, sandboxes, agents, environments, Graders, triggers, webhook sources, trigger deliveries that failed or were denied, budgets, sign-ins, personal access tokens, delegations, tenant switches, grants, roles, the tenant budget and declared resources that could not be applied. Per-call activity is not an event. Tool calls and inference requests stay in the run's own history and the inference usage log.

An agent's token exchange is not an event, because agents exchange tokens every few minutes. A token refresh is not an event either.

## Reading events

| Surface | What it gives |
| --- | --- |
| **Events** page | A live feed with history, category tabs, filters and a detail view |
| `GET /v1/events` | A page of events, newest first, with filters |
| `GET /v1/events/{id}` | One event |
| `GET /v1/events/stream` | A server-sent stream of new events |
| `events.list` and `events.get` | The same list and read through the kora MCP server |

Reading events needs the `events:read` scope. The `kora-operator` role and `platform-admin` hold it. A caller sees only the events of the tenant on its token.

The list and the MCP tools filter by `category`, `type`, `agent`, `principal` and `run_id`, and search a substring of the type, subject, principal, agent and run id with `q`. Search does not read the payload. A list page holds up to 200 events and returns a `next_cursor` for the next page.

The type catalog is open to any authenticated caller. `GET /v1/events/types` lists each type with its category and description. `GET /v1/events/types/{type}` returns the type's JSON Schema, and the `events.types` tool returns both.

## Retention

Events are deleted after a retention period. The chart value `koraEvents.retention.defaultDays` sets it for the install, and 30 days is the default. A tenant overrides it with `spec.events.retentionDays` on its `KoraTenant`. See [tenants](../guides/tenants.md).

## Triggers and loops

A platform event trigger starts a run of an autonomous agent when an event of its type matches its filters. An agent's run can change the platform, and the change records an event that can match the same trigger, so two guards stop a loop:

- A trigger ignores events about runs it started or messaged, unless **Ignore its own runs** is turned off.
- No trigger fires on an event five or more steps down a chain of trigger-caused events.

The guards decide only whether a trigger fires. The event is always recorded. Each event of a run carries the chain of the run: the event that caused it, its depth and the triggers involved.

## Related decisions

- [Event spine](../adr/2026-11-06-event-spine.md)
- [Run activity events](../adr/2026-09-29-run-activity-events.md)
- [Runs list live stream](../adr/2026-11-25-runs-list-live-stream.md)
