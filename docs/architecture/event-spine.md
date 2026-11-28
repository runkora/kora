# Event spine

The event spine records platform events and delivers them to readers and triggers. A producer writes an event in the same database transaction as the change it describes. The `kora-events` service copies each event into an ordered log, serves the log to the console and the API, and streams it to `kora-triggers`. See [events](../concepts/events.md) for what an event is.

## Recording

A service that changes state calls `Publish` from `internal/platform/events` inside the transaction of the change. `Publish` inserts a row into `event_outbox` and sends a `pg_notify` on the `kora_events_outbox` channel. The notification is delivered when the transaction commits. A rolled-back change leaves no row.

The envelope is built from the context. The principal, its type and its display name come from the platform token, and the delegator comes from the delegation token. A context with no token records the `SYSTEM` principal under the name of the producing binary. The producer supplies the tenant, subject, run, agent and chain, and the typed payload. `Publish` refuses an event with no tenant, an unknown type, no subject, or a payload that is not the struct of its type.

A few producers have no Postgres change to join. They call `PublishOnce`, which derives the event id from the type and a key, so a repeated key records nothing while the earlier event is retained. Declared resource failures, budget thresholds, budget exceeded and sandbox deletion use it.

## Relay

Every `kora-events` replica runs the relay. A batch runs in one transaction:

1. Take the advisory lock `kora_events_relay`.
2. Read `last_seq` from `event_relay_state`.
3. Read up to 500 outbox rows in `position` order.
4. Insert each row into `events` with the next `seq`, skipping an id already in the log.
5. Delete the outbox rows and store the new `last_seq`.

Only the holder of the lock assigns `seq`, so the sequence has no gaps. A row that commits after a later row was relayed is picked up by the next batch. A row the log cannot hold, such as an envelope that does not decode, moves to `event_outbox_dead` with the reason. The relay logs it, counts it in the `kora.events.relay.dead_lettered` metric and continues with the rest of the batch.

The relay runs on each outbox notification and every second. After a batch it notifies the `kora_events` channel with the last `seq`.

## Delivery

Each replica holds one `LISTEN` connection for both channels and reconnects after a loss. A hub on the replica reads each new event from the log once and fans it out in memory to the replica's open streams. A stream that falls 64 batches behind is dropped from the hub and catches up from the log.

| Reader | Route | Behaviour |
| --- | --- | --- |
| Console and API clients | `GET /v1/events` | Pages newest first, filtered by tenant, category, type, agent, principal and run, with `q`, `before`, `after`, `cursor` and `limit` (default 50, at most 200) |
| Console and API clients | `GET /v1/events/{id}` | One event of the caller's tenant |
| Console and API clients | `GET /v1/events/stream` | Server-sent events. Each message carries its `seq` as the id, and `Last-Event-ID` replays matching events after that `seq` before live ones. The stream sends a keepalive every 30 seconds and ends when the token expires or is revoked |
| Anyone authenticated | `GET /v1/events/types`, `GET /v1/events/types/{type}` | The type catalog and each type's JSON Schema |
| `kora-triggers`, `kora-agents` | `GET /internal/v1/events/stream` | Every tenant's events in `seq` order, from `after` or from the hub's position |

The public routes require `events:read`, except the type catalog, which needs only an authenticated caller. The edge routes `/v1/events` to port 8099 with a 3600-second timeout so streams stay open.

The internal stream is open only to the `kora-triggers` and `kora-agents` identities, enforced by a mesh `AuthorizationPolicy` and again by the controller. `kora-agents` may read only the `agents.run` category, for the live runs list. A request with `tenant` and `after` returns 410 when the tenant holds no event at `after`, because retention removed it.

## Event triggers

`kora-triggers` consumes the internal stream. One replica consumes at a time. It holds an advisory lock and the other replicas retry every 10 seconds. The consumer keeps its position in `trigger_event_cursor` and advances it after recording the deliveries of an event, so a restart replays from the cursor and the delivery dedup drops events already handled.

For each event, the consumer lists the tenant's event triggers of the event's type that are enabled or auto-paused and applies each trigger's filters over the envelope flattened to dotted paths, such as `data.entity.state` or `koraagent`. An event that matches at least one trigger records one delivery, with a unique index on the event id so a replayed event records once. Each trigger's outcome goes through the same admission, coalescing, routing and auto-pause as other triggers.

Two guards decide whether a matching trigger fires:

| Guard | Rule | Recorded outcome |
| --- | --- | --- |
| Own runs | An event whose `koratriggers` list holds the trigger is skipped, unless the trigger sets `ignore_own_runs` to false. The default is on | `filtered`, reason `own_run` |
| Chain depth | An event with `korachaindepth` of 5 or more fires no trigger | `filtered`, reason `chain_depth` |

A fire reports the event id and the event's depth plus one to `kora-agents`. The run stores the cause and the highest depth, and every event about the run carries them as `koracausedby` and `korachaindepth`.

An agent can cause an event from inside a run by calling a platform API through the kora MCP server. The MCP server sends such calls under a `/run/{run_id}` path prefix, and the tool gateway passes the run in the `X-Kora-Run-Id` header. `Publish` records the run as `koraactingrun`. When an agent's event carries no chain and a trigger's filters match it, the consumer reads the run's chain from `kora-agents` and stamps the cause, depth and triggers onto the envelope before the guards run. The log keeps the envelope as published.

An event trigger's prompt keeps the template as written and appends the whole event in a `<delivery-data>` block marked as untrusted data. A delivery of an event can be replayed against the tenant's triggers as they are now, by a caller with `events:read`.

## Type schemas

Each entity has a typed payload struct in `internal/platform/events`, and the schema of an event type is derived from the struct by reflection. `kora-events` serves the schemas. Creating or editing an event trigger refuses a filter key or prompt placeholder that is not a field of the type's schema, and a filter value outside a field's enumerated values.

## Retention

A sweep runs every hour under an advisory lock and deletes events in batches of 5,000. It deletes events older than the tenant's retention and dead-letter rows past the same period. A tenant's retention is `spec.events.retentionDays` on its `KoraTenant`, which the operator writes to the `kora-tenants` ConfigMap that `kora-events` mounts. Other tenants use `koraEvents.retention.defaultDays`, 30 by default. A row of an unknown or empty tenant uses the default. The sweep is skipped when the registry cannot be read, so tenants that keep events longer are not swept at the default.

## ADRs

[event-spine](../adr/2026-11-06-event-spine.md), [runs-list-live-stream](../adr/2026-11-25-runs-list-live-stream.md), [run-activity-events](../adr/2026-09-29-run-activity-events.md).

## See also

- [Events](../concepts/events.md) and [event types](../reference/events.md).
- [Create a trigger](../guides/create-a-trigger.md) for platform event triggers.
- [MCP tool gateway](mcp-tool-gateway.md) for how the acting run reaches the kora MCP server.
