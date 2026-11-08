# Event Spine

<!-- Filename: 2026-11-06-event-spine.md  (decision date + slug; no sequential number) -->

**Status:** Accepted  
**Date:** 2026-11-06  
**Deciders:** Justin Wilkin  
**Technical Story:** [Event Spine PRD](../prd/event-spine.md). Record tenant-scoped platform events durably, fire triggers on them, and serve them to the Events page and the kora MCP.

---

## Context

kora has one Postgres database with a public schema. Each binary owns its tables and its migrations, and `cmd/migrate` applies them all ([Database Migrations](2026-09-10-database-migrations.md)). Redis holds key and value data only. No binary uses `LISTEN` or `NOTIFY`.

The durable history today is `run_events` for activity inside one run and `trigger_deliveries` for inbound fires. Run streaming fans out in memory inside `kora-agents` and replays from `run_events` through `Last-Event-ID` (`backend/internal/agents/server.go`).

The code that will record events has these properties:

*   `storage.WithTransaction` passes a `pgx.Tx` to its callback (`backend/internal/platform/storage/transaction.go`). Nothing carries a transaction through `context.Context`.
*   Repositories call `r.pool.Exec`, `r.pool.Query` and `r.pool.QueryRow` on a `*pgxpool.Pool` directly. Most state changes that need an event are a single `Exec` outside any transaction.
*   Claims come from `token.ClaimsFromContext`, delegation from `policy.DelegationFromContext`, the run from `mw.RunIDFromContext` and the trace from `observability.TraceParent`. Scheduler, reaper and worker contexts carry no claims.
*   Budget spend is a Redis counter that inferencegw increments after it writes the usage row.
*   Declared apply outcomes are returned by `handleApply` in `kora-agents`, `kora-triggers`, `policy-service` and `inference-gateway`. The operator writes them to resource status.
*   `kora-triggers` and `inference-gateway` serialize writers with `pg_advisory_xact_lock`. No binary outside the operator runs leader election.
*   Sessions are created on interactive sign-in, on every token refresh (`RefreshSession` calls `createSession`) and for agents (`createAgentSession`).

This touches *Who is this agent?* Every event records the principal, the principal type and the delegator that caused it. An event trigger runs as the trigger's own principal like every other trigger kind ([Agent Triggers and Webhook Ingestion](2026-06-17-agent-triggers-and-webhook-ingestion.md)). It touches *What can it access?* Reading events requires `events:read`, and every read is limited to the caller's tenant. *How does it connect?* is unchanged, and no event leaves the cluster in v1.

### Goals

*   Record every v1 event in the same transaction as the state change it describes.
*   Give every event a tenant, a principal and a gapless position in one log.
*   Let triggers fire on events through the existing delivery pipeline.
*   Serve history and a live feed to the Events page and the kora MCP.
*   Keep terminated runs.
*   Publish the fields and values of every event type for trigger authors.

### Non-Goals

*   Events for platform-wide resources under the `_platform` tenant: virtual models, provider enablement, price overrides and provider refresh failures.
*   Events for resources that exist only as Kubernetes resources: `MCPServer`, skill marketplaces and `Connection`.
*   A message broker.
*   Policy decision events.
*   Delivery outside the cluster.

---

## Decision Drivers

*   An event commits only when its state change commits.
*   Producer repositories keep their method signatures and narrow interfaces.
*   Each binary writes only its own tables, except for one documented path.
*   No new infrastructure in the cluster.

---

## Considered Options

### Decision 1: Transport

*   **Option A (chosen):** Postgres. Producers write an outbox row in their transaction. A relay copies outbox rows into an `events` log, assigns each a sequence number and sends `NOTIFY`. Consumers read the log after their cursor.
*   **Option B (rejected):** NATS JetStream behind the outbox, as in constellation-core. It adds a stateful cluster component to every install. Every v1 consumer is inside kora and reads one log.

### Decision 2: Transaction Propagation

*   **Option A (chosen):** `WithTransaction` stores the transaction in the context. A nested call joins the open transaction, and the outermost call commits or rolls back. A `storage.Pool` type holds a `*pgxpool.Pool` in an unexported field and exposes only methods that use the context transaction when one is open.
*   **Option B (rejected):** A `storage.Querier(ctx, pool)` helper that every producer call site uses. Every producer site would change, and a missed site would run outside the transaction with no error.
*   **Option C (rejected):** Pass `pgx.Tx` through repository methods. Every repository method, narrow interface and fake on a producer path would change its signature.
*   **Option D (rejected):** Port constellation-core's generic query helpers (`QueryExec`, `QueryRowToStruct`, `QueryRowsToSlice`) as well. Producer sites would move to the helpers and replace hand-written `Scan` calls. The pool type gives the same transaction joining with a smaller change.

### Decision 3: Outbox Ownership

*   **Option A (chosen):** One `event_outbox` table created by `kora-events` migrations. Every producer writes it.
*   **Option B (rejected):** One outbox table per producer binary. The relay would read five tables, and every new producer would need a new table. kora has one database.

### Decision 4: Relay Writer

*   **Option A (chosen):** Every `kora-events` replica runs the relay loop. Each batch runs in a transaction that takes `pg_advisory_xact_lock` first.
*   **Option B (rejected):** A separate relay Deployment with `replicas: 1`. The service would have two Deployments, and the relay would stop during a rollout of its pod.

### Decision 5: Events With No Postgres Change

*   **Option A (chosen):** The producer writes a standalone outbox row with a deterministic event id and `ON CONFLICT DO NOTHING`. A retry or a repeated reconcile records one event.
*   **Option B (rejected):** The operator posts events for Kubernetes-only resources to an internal `kora-events` endpoint. These resources are defined only in git, and git review is their change record.

### Decision 6: Terminated Runs

*   **Option A (rejected):** Add `runs.deleted_at` and keep the run's state. A terminated live run would keep `active` or `idle`, and pod capacity, the reaper and agent deletion would treat it as live.
*   **Option B (rejected):** Add a `terminated` state for every terminated run. Terminating a completed or failed run would overwrite its outcome.
*   **Option C (rejected):** Add `runs.deleted_at` and end a live run as `completed` or `failed`. A run terminated mid-task would read `completed`.
*   **Option D (chosen):** Add `runs.deleted_at` and a terminal `terminated` state. Terminate ends a live run as `terminated`, and a run that has already ended keeps its outcome.

### Decision 7: Consumer Reads

*   **Option A (chosen):** `kora-triggers` reads an internal SSE stream from `kora-events` and stores its cursor in its own table.
*   **Option B (rejected):** `kora-triggers` queries `events` and listens on the `NOTIFY` channel directly. It would add a second cross-binary SQL path, and every consumer would depend on the table layout.

### Decision 8: Retention Override

*   **Option A (chosen):** `KoraTenant` gains `spec.events.retentionDays`, and the chart value is the default for tenants that do not set it.
*   **Option B (rejected):** A chart map of tenant overrides. Every override would be a platform chart change.
*   **Option C (rejected):** An API and dashboard setting behind an `events:write` scope. Retention is an install and tenant definition setting.

### Decision 9: Event Payload Schema

*   **Option A (chosen):** Each entity kind has a typed payload struct in `platform/events`, and the schema of each event type is derived from its struct by reflection. `kora-events` serves the schemas as JSON Schema, and trigger create and edit check fields against them.

---

## Decision

**Chosen Options:** Decision 1 A, Decision 2 A, Decision 3 A, Decision 4 A, Decision 5 A, Decision 6 D, Decision 7 A, Decision 8 A, Decision 9 A.

### Architecture

```
  identity   kora-agents   kora-triggers   policy-service   inference-gateway
     │            │              │                │                 │
     │   state change + events.Publish(ctx, event) in one transaction
     ▼            ▼              ▼                ▼                 ▼
  ┌──────────────────────────── event_outbox ─────────────────────────────┐
  └───────────────────────────────────┬───────────────────────────────────┘
                                      │ relay batch under pg_advisory_xact_lock
                                      ▼
                         kora-events: events (seq) ── NOTIFY kora_events
                                      │
          ┌───────────────────────────┼────────────────────────────┐
          ▼                           ▼                            ▼
  GET /v1/events             GET /v1/events/stream       GET /internal/v1/events/stream
  GET /v1/events/{id}        (SSE, tenant, events:read)  (SSE, all tenants,
  (events:read)                       │                   kora-triggers only)
          │                           │                            │
          ▼                           ▼                            ▼
  kora MCP events.list,       Events page                 kora-triggers consumer
  events.get                                              trigger_event_cursor
                                                          event triggers ──► runs
```

### 1. Event Envelope

Events are CloudEvents 1.0 structured JSON. `backend/internal/platform/events` defines the envelope, the type catalog and `Publish`.

```json
{
  "specversion": "1.0",
  "id": "0193f3c2-7a4e-7c1a-9d2e-5b8f0c6a1e42",
  "source": "kora-agents",
  "type": "agents.run.failed.v1",
  "dataschema": "https://api.example.com/v1/events/types/agents.run.failed.v1",
  "subject": "runs/2b4e8c1d-...",
  "time": "2026-11-06T03:14:07.512Z",
  "datacontenttype": "application/json",
  "koratenant": "kora-tenant-example",
  "koraprincipal": "<agent subject>",
  "koraprincipaltype": "AGENT",
  "koraprincipalname": "triage",
  "koradelegator": "<user subject>",
  "korarun": "2b4e8c1d-...",
  "koraagent": "triage",
  "koracausedby": "0193f3b9-...",
  "korachaindepth": 1,
  "koratriggers": ["<trigger id>"],
  "traceparent": "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
  "data": {
    "entity": {"id": "2b4e8c1d-...", "agent": "triage", "state": "failed", "error": "subprocess_exit"},
    "changed": ["state", "completed_at"],
    "entity_version": 7,
    "occurred_at": "2026-11-06T03:14:07.498Z"
  }
}
```

*   `id` is a UUIDv7. An event with no Postgres change uses a UUIDv5 over its deterministic key.
*   `type` has the form `<domain>.<entity>.<event>.v<N>`. The catalog in `platform/events` holds every type in the PRD with its payload struct (section 14).
*   `dataschema` is the URL of the type's schema on `GET /v1/events/types/{type}` (section 14).
*   `koraprincipaltype` is `USER`, `AGENT`, `TRIGGER` or `SYSTEM`. A context with no claims records `SYSTEM` and the binary as principal.
*   `koraprincipalname` is the principal's display name from the claims at publish time: the name or email of a person, the slug of an agent or trigger, and the binary for `SYSTEM`.
*   `koracausedby`, `korachaindepth` and `koratriggers` are set on events about a run that an event trigger started or messaged, or that an agent started or messaged from such a run (section 7). Every other event has depth 0.
*   `data.entity` is the resource after the change, or the resource before the delete for a delete event.
*   `data.changed` lists the fields that an update changed.
*   `data.entity` follows the type's payload schema (section 14). A producer maps its model into the payload struct and does not marshal its storage row.
*   `data` must not carry token values, signing secrets, OAuth codes or credential references.

### 2. Producer API

```go
package events

// Publish writes the event to event_outbox through the transaction in ctx.
// Outside a transaction it writes a standalone row.
func Publish(ctx context.Context, pool *storage.Pool, e Event) error

// PublishOnce writes a standalone row whose id derives from key.
// A repeated key records nothing.
func PublishOnce(ctx context.Context, pool *storage.Pool, key string, e Event) error

type Event struct {
    Type       Type
    Tenant     string
    Subject    string
    RunID      string
    Agent      string
    CausedBy   string
    ChainDepth int
    Triggers   []string
    Entity     any
    Changed    []string
    Version    int64
}
```

`Publish` returns an error when `Entity` is not the payload struct of `Type`. The producer sets the tenant, run, agent, cause and depth from the row it changed. `Publish` refuses an event with no tenant. Bulk updates such as `ReapStaleRuns` and `CancelRunsByAgentType` return the tenant and agent of each row they change. It fills the principal, delegator and trace from the context. A producer site wraps its write and the publish in one transaction:

```go
err := storage.WithTransaction(ctx, r.pool, func(ctx context.Context, _ pgx.Tx) error {
    if _, err := r.pool.Exec(ctx, `UPDATE runs SET state = $2 WHERE id = $1`, id, state); err != nil {
        return err
    }
    return events.Publish(ctx, r.pool, events.Event{Type: events.RunFailed, Tenant: run.TenantID, Subject: "runs/" + id, RunID: id, Entity: runEntity})
})
```

### 3. Context-Carried Transactions

*   `storage.TxFunc` becomes `func(ctx context.Context, tx pgx.Tx) error`. The existing `WithTransaction` callers move to the new signature.
*   `storage.TxFromContext(ctx)` returns the open transaction.
*   `storage.Pool` exposes `Exec`, `Query`, `QueryRow` and `Begin`. `Begin` inside an open transaction returns a savepoint on it. The pool has no exported field or other method that bypasses the context transaction.
*   Producer repositories and their shared query helpers (e.g. `getOne` and `queryList` in `backend/internal/triggers/postgres.go`) change from `*pgxpool.Pool` to `*storage.Pool`. Other repositories move when they next need it.
*   A ruleguard rule bans `*pgxpool.Pool` in the producer packages.

### 4. Tables

`kora-events` owns all four tables. `event_outbox` is the one table that other binaries write. The database migrations ADR records it as a cross-binary SQL path, and `make migrate-lint` allows it.

```sql
CREATE TABLE event_outbox (
    id          UUID PRIMARY KEY,
    position    BIGINT GENERATED ALWAYS AS IDENTITY,
    tenant_id   TEXT NOT NULL,
    type        TEXT NOT NULL,
    envelope    JSONB NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX event_outbox_position_idx ON event_outbox (position);

CREATE TABLE events (
    seq             BIGINT PRIMARY KEY,
    id              UUID NOT NULL UNIQUE,
    tenant_id       TEXT NOT NULL,
    type            TEXT NOT NULL,
    category        TEXT NOT NULL,
    subject         TEXT NOT NULL,
    principal       TEXT NOT NULL,
    principal_type  TEXT NOT NULL,
    run_id          UUID,
    agent           TEXT,
    caused_by       UUID,
    chain_depth     INT NOT NULL DEFAULT 0,
    occurred_at     TIMESTAMPTZ NOT NULL,
    recorded_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    envelope        JSONB NOT NULL
);
CREATE INDEX events_tenant_seq_idx ON events (tenant_id, seq DESC);
CREATE INDEX events_tenant_category_seq_idx ON events (tenant_id, category, seq DESC);
CREATE INDEX events_tenant_type_seq_idx ON events (tenant_id, type, seq DESC);
CREATE INDEX events_tenant_principal_seq_idx ON events (tenant_id, principal, seq DESC);
CREATE INDEX events_tenant_agent_seq_idx ON events (tenant_id, agent, seq DESC) WHERE agent IS NOT NULL;
CREATE INDEX events_run_idx ON events (run_id) WHERE run_id IS NOT NULL;
CREATE INDEX events_recorded_at_idx ON events (recorded_at);

CREATE TABLE event_outbox_dead (
    id          UUID PRIMARY KEY,
    position    BIGINT NOT NULL,
    tenant_id   TEXT NOT NULL,
    type        TEXT NOT NULL,
    envelope    JSONB NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL,
    reason      TEXT NOT NULL,
    dead_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE event_relay_state (
    id        BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (id),
    last_seq  BIGINT NOT NULL DEFAULT 0
);
```

`category` is the `<domain>.<entity>` prefix of `type`. The indexed columns copy envelope attributes for filtering. The `q` search adds no index and reads `events_tenant_seq_idx` newest first up to the page limit.

### 5. Relay

A relay batch runs in one transaction:

1.  Take `pg_advisory_xact_lock(hashtext('kora_events_relay'))`.
2.  Read `last_seq` from `event_relay_state`.
3.  Select up to 500 outbox rows ordered by `position`.
4.  Drop rows whose id is already in `events`. Insert the rest into `events` with `seq` values from `last_seq + 1` in `position` order.
5.  Delete the selected outbox rows, update `last_seq` and send `NOTIFY kora_events, '<last seq>'`.
6.  Commit.

Only the lock holder assigns `seq`, inside the batch transaction. A row whose envelope does not decode or does not match its `tenant_id` moves to `event_outbox_dead` with the reason. The relay logs it, counts it in `kora.events.relay.dead_lettered` and relays the rest of the batch. An outbox row that commits after a later `position` was relayed is picked up by the next batch. `Publish` sends `pg_notify('kora_events_outbox', '')` in the producer transaction. The relay runs a batch on each notification and every second.

### 6. Read API

`kora-events` serves on port 8099. The edge routes `/v1/events` to it with a 3600-second timeout for streams.

*   `GET /v1/events` returns events for the caller's tenant, newest first. Filters: `category`, `type`, `agent`, `principal`, `run_id`, `q`, `before` and `after` (seq or time), `limit` up to 200. The response carries an opaque `next_cursor`.
*   `category`, `type`, `agent` and `principal` take several values. A filter matches any of its values, and an event matches every filter.
*   `q` matches a substring of the type, subject, principal, agent or run id with `ILIKE`. It does not search the payload.
*   `GET /v1/events/{id}` returns one event in the caller's tenant, or 404.
*   `GET /v1/events/stream` is an SSE stream of the caller's tenant with the `category`, `type`, `agent`, `principal` and `run_id` filters and no `q`. Each SSE message carries its seq as `id`, and a `Last-Event-ID` replays every matching event after that seq before the live events. A stream ends when the caller's token expires or is revoked.
*   `GET /internal/v1/events/stream?after=<seq>` streams every tenant's events in seq order. An AuthorizationPolicy and `kora-events` itself allow only the `kora-triggers` identity.
*   `GET /v1/events/types` and `GET /v1/events/types/{type}` return the type catalog and each type's JSON Schema (section 14).

The public event routes register `events:read` through `routescopes` and are enforced by `ScopeEnforcementMiddleware` ([Platform Scope Enforcement Middleware](2026-06-04-platform-scope-enforcement-middleware.md)). `platform-admin` and `kora-operator` hold `events:read`. Each replica holds one `LISTEN kora_events` connection and fans live events out to its open streams in memory.

### 7. Event Triggers

`kora-triggers` gains the kind `event` beside `webhook`, `scheduled` and `manual`.

*   An event trigger stores `event_type` and the existing `match_condition` and `match_contains` maps. Matching runs over the envelope flattened to dotted fields (`data.entity.state`, `koraagent`) with the existing `matchesCondition` and `matchesContains`.
*   Create and edit refuse a `match_condition` or `match_contains` key, and a prompt placeholder, that is not a field of the type's schema (section 14). They also refuse a `match_condition` value outside the field's enum.
*   A trigger matches only events of its own tenant.
*   `ignore_own_runs` defaults to true. When it is set, an event does not fire if its `koratriggers` list holds this trigger. `kora-agents` keeps the list on the run with every trigger that started the run or sent it a message, and copies it onto every event about the run. The delivery records outcome `filtered` with reason `own_run`.
*   A trigger does not fire on an event with `korachaindepth` 5 or more. The delivery records outcome `filtered` with reason `chain_depth`.
*   An event that matches at least one trigger records one delivery with `event_id`. A unique index on `trigger_deliveries (event_id) WHERE redelivery_of IS NULL` dedups a replayed event and leaves redelivery working. Per-trigger outcomes go in `trigger_delivery_runs` as for a webhook fan-out.
*   Each fire goes through the existing admission, coalescing, routing and auto-pause.
*   `StartRunRequest` and `SendRunMessageRequest` carry `caused_by` (the event id) and `chain_depth` (the event depth plus one). `kora-agents` stores the cause and the highest depth on the run. Every event about that run carries them as `koracausedby` and `korachaindepth`.
*   A run that an agent starts or messages from an attested run joins that run's chain when the attested run is in one: its depth is above 0 or a trigger started or messaged it. A run outside a trigger chain passes no chain on, and its sub-agent runs stay at depth 0. Two claims attest the run: `run_id` in the token of a per-run sandbox identity, and `run_id` in a delegation token. `kora-agents` checks that the run belongs to the calling agent in the same tenant. The target then gets the run's cause, its depth plus one and its triggers.
*   An agent in a shared pod has no attested run, and an admin change an agent makes carries no chain. Auto-pause and budgets bound such a loop until per-run attestation covers these cases.
*   The prompt carries the event in `<delivery-data>` with the same escaping as webhook payloads (`backend/internal/triggers/template.go`).
*   The declarative `Trigger` kind, the API, the MCP `triggerKinds` enum and the trigger wizard gain `event`.
*   The trigger wizard and the detail modal offer only the schema's fields for filters and prompt placeholders, and an enum field's values for its filter.

The consumer holds the internal stream from its cursor in `trigger_event_cursor`. It advances the cursor after it has recorded the deliveries for an event. One replica runs the consumer at a time. It holds a session-level `pg_try_advisory_lock` on a dedicated connection, and the other replicas retry every 10 seconds. A restart replays from the cursor, and the delivery dedup drops events that were already handled.

### 8. Events With No Postgres Change

These producers call `PublishOnce`. Its dedup lasts as long as the earlier event is retained. A condition that outlives the tenant's retention records again.

*   **Declared failures:** `handleApply` in each owning service records `declarative.resource.failed.v1` when its result is not `Projected`. The key is the resource UID, generation and reason.
*   **Budget thresholds:** inferencegw compares spend before and after `IncrementSpends`. Crossing 50, 80 or 100 percent of a tenant or grant budget records `inference.budget.threshold_crossed.v1`. The key is the budget, the period start and the level.
*   **Budget exceeded:** inferencegw records `inference.budget.exceeded.v1` when it refuses a request with `budget_exceeded`. The key is the budget and the period start.
*   **Sandbox deleted:** `kora-agents` records `agents.sandbox.deleted.v1` in `deprovisionSandbox`. The key is the run and the sandbox.

### 9. Sessions

`identity.session.created.v1` is recorded by `ExchangeIDToken`, `PollDeviceAuth` and `ExchangeAuthCode` for people, and by `ExchangeSVID` through `createAgentSession` for agents. A refresh records nothing, and a tenant switch records only `identity.tenant.switched.v1`.

### 10. Terminated Runs

*   `runs` gains `deleted_at TIMESTAMPTZ`. Terminate sets it and keeps the `run_events`.
*   `RunStateTerminated` (`terminated`) joins the terminal states beside `completed`, `failed` and `budget_exhausted` in `backend/internal/agents/models.go`, the SQL state lists and the dashboard status tokens.
*   Run lists, run stats and the scheduling queue filter `deleted_at IS NULL`.
*   The run detail endpoint returns a terminated run with `deleted_at` set.

### 11. Retention

*   The chart value `koraEvents.retention.defaultDays` sets the default. The default is 30.
*   `KoraTenant` gains `spec.events.retentionDays`. The operator adds it to the tenant's entry in the `kora-tenants` registry, and `kora-events` mounts the registry.
*   A sweep every hour deletes events older than the tenant's retention in batches of 5,000. It runs under `pg_try_advisory_xact_lock`.
*   The sweep also deletes `event_outbox_dead` rows whose `dead_at` is past their tenant's retention. A row with an unknown or empty tenant uses the chart default.
*   Retention applies to `events` and `event_outbox_dead` only, and `run_events` rows live as long as their run.

### 12. kora MCP Tools

`kora-mcp` gains `events.list` and `events.get` through the shared `korajson` client. Each tool requires `mcp:kora:events:list:use` or `mcp:kora:events:get:use`, and `kora-events` enforces `events:read` on the call ([Scope-Gated Tool Exposure via MCP](2026-03-05-scope-gated-tool-exposure-via-mcp.md)).

### 13. Deployment

`kora-events` is a new binary in `backend/cmd/kora-events` with its own chart templates, NetworkPolicy, DestinationRule, ServiceMonitor and AuthorizationPolicy ([Multi-Service Backend Deployment Structure](2026-03-19-multi-service-backend-deployment-structure.md)). It runs two replicas by default.

### 14. Event Payload Schema

`platform/events` defines one payload struct per entity kind. The catalog registers each type with its struct:

*   `RunPayload`: the twelve `agents.run.*` types.
*   `SandboxPayload`: the five `agents.sandbox.*` types.
*   `AgentPayload`: `agents.agent.created.v1`, `agents.agent.updated.v1` and `agents.agent.deleted.v1`.
*   `EnvironmentPayload`: the three `agents.environment.*` types.
*   `TriggerPayload`: the three `triggers.trigger.*` change types and `triggers.trigger.autopaused.v1`.
*   `SourcePayload`: the three `triggers.source.*` types.
*   `DeliveryPayload`: `triggers.delivery.failed.v1` and `triggers.delivery.denied.v1`.
*   `BudgetPayload`: `inference.budget.threshold_crossed.v1` and `inference.budget.exceeded.v1`.
*   `SessionPayload`: `identity.session.created.v1`.
*   `PATPayload`: the three `identity.pat.*` types.
*   `DelegationPayload`: `identity.delegation.issued.v1`.
*   `TenantSwitchPayload`: `identity.tenant.switched.v1`.
*   `GrantPayload`, `RolePayload` and `TenantBudgetPayload`: the `policy.grant.*`, `policy.role.*` and `policy.tenant_budget.*` types.
*   `DeclaredResourcePayload`: `declarative.resource.failed.v1`.

`events.TypeSchema(type)` derives a JSON Schema (draft 2020-12) from the type's struct by reflection:

*   The `json` tag gives the field name, and the Go type gives the JSON type.
*   A `doc` tag gives the description, a `format` tag gives the string format, and an `enum` tag cites a value set that `platform/events` defines from Go constants.
*   A pointer or `omitempty` field is optional. A nested struct or slice becomes a nested schema.
*   The type schema covers the envelope attributes and `data`. `data.entity` is the payload schema.
*   `data.changed` is an array whose enum lists every field name an update can change. The list includes fields the payload does not carry (e.g. `system_prompt`, `prompt_template`, `config`). An event names such a field and never carries its value.
*   Each payload struct has one `state` enum for all of its types.
*   Run error codes and loop guard kinds are Go constants in `platform/events`. The `error` and `guardrail.kind` enums list them, and a producer records `unknown` for a value outside them. `guardrail.tool` is free text.
*   `agents.run.errored.v1` carries the code of the errored turn, and `agents.run.failed.v1` carries the code of the run's last run error. A failed run with no recorded run error carries `unknown`.

```go
type RunPayload struct {
    ID    string `json:"id" doc:"The run id."`
    Agent string `json:"agent" doc:"The slug of the run's agent."`
    State string `json:"state" doc:"The run state after the change." enum:"run_state"`
}
```

`kora-events` serves the catalog to any authenticated caller:

*   `GET /v1/events/types` lists each type with its category, its description and the URL of its schema.
*   `GET /v1/events/types/{type}` returns the type's JSON Schema, or 404 for a type outside the catalog.
*   The routes register through `routescopes` with no required scope. A schema holds no tenant data.

The relay sets `dataschema` on each event from the chart's public API host. `kora-triggers` reads the schemas from `platform/events` in process.

The schema of `agents.run.failed.v1` describes `data` as follows. The envelope attributes from section 1 are omitted.

```json
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "$id": "https://api.example.com/v1/events/types/agents.run.failed.v1",
  "title": "agents.run.failed.v1",
  "description": "A run failed.",
  "type": "object",
  "properties": {
    "data": {
      "type": "object",
      "required": ["entity", "occurred_at"],
      "properties": {
        "entity": {
          "type": "object",
          "required": ["id", "agent_id", "agent", "state", "created_by"],
          "properties": {
            "id": {"type": "string", "description": "The run id."},
            "agent_id": {"type": "string", "description": "The id of the run's agent."},
            "agent": {"type": "string", "description": "The slug of the run's agent."},
            "state": {"type": "string", "description": "The run state after the change.", "enum": ["pending", "starting", "active", "idle", "completing", "completed", "failed", "budget_exhausted", "terminated"]},
            "created_by": {"type": "string", "description": "The principal that created the run."},
            "trigger_id": {"type": "string", "description": "The trigger that started the run."},
            "delivery_id": {"type": "string", "description": "The delivery that started the run."},
            "environment_id": {"type": "string", "description": "The environment the run's sandbox uses."},
            "started_at": {"type": "string", "format": "date-time", "description": "When the run started."},
            "completed_at": {"type": "string", "format": "date-time", "description": "When the run ended."},
            "deleted_at": {"type": "string", "format": "date-time", "description": "When the run was terminated."},
            "error": {"type": "string", "description": "The error code of an errored turn, or of a failed run's last run error.", "enum": ["agent_error", "empty_response", "runner_error", "sandbox_lost", "loop_detected", "empty_turn", "subprocess_exit", "budget_exhausted", "start_failed", "unknown"]},
            "guardrail": {"type": "object", "description": "The loop guard finding of a warning.", "required": ["kind", "count", "tool"], "properties": {
              "kind": {"type": "string", "description": "The loop the guard detected.", "enum": ["repeated_call", "wait_rearm", "unknown"]},
              "count": {"type": "integer", "description": "How many times the agent repeated the call."},
              "tool": {"type": "string", "description": "The tool the agent repeated."}
            }},
            "sender_trigger_id": {"type": "string", "description": "The trigger that sent the run a message."}
          }
        },
        "changed": {"type": "array", "items": {"type": "string", "enum": ["agent", "agent_id", "completed_at", "created_by", "deleted_at", "delivery_id", "environment_id", "error", "guardrail", "id", "sender_trigger_id", "started_at", "state", "trigger_id"]}},
        "entity_version": {"type": "integer"},
        "occurred_at": {"type": "string", "format": "date-time"}
      }
    }
  }
}
```

### Rationale

None at this time.

---

## Implementation

The work ships as one stack of nine pull requests:

1.  **Foundation:** context-carried transactions and `storage.Pool`, `platform/events`, and `kora-events` with the outbox, relay, log, read API, streams, retention, `KoraTenant` field, chart and `events:read`.
2.  **Run and sandbox producers:** `runs.deleted_at`, run lifecycle and activity events, sandbox events, agent and environment changes.
3.  **Remaining producers:** triggers, identity, policy-service, inferencegw budgets and declared failures.
4.  **Event trigger kind:** the consumer, matching, both loop guards, the declarative `Trigger`, the API and the wizard.
5.  **Event payload schema:** the payload structs, the schema endpoints, `dataschema`, trigger field checks and the wizard's schema fields.
6.  **Run forwarding:** the acting run through the tool-gateway and the kora MCP, and the consumer's chain read.
7.  **kora MCP tools:** `events.list` and `events.get`.
8.  **Hades:** DataTable `bodyState`, a server search box and an `xl` Modal.
9.  **Events page:** a Storybook iteration first, then the page.

---

## Validation

*   `TestPostgres_*` tests cover the relay: gapless `seq` under concurrent producers and two relay loops, a late-committing outbox row, a repeated id and the retention sweep.
*   `TestPostgres_*` tests cover a rolled-back state change leaving no outbox row, and a nested `WithTransaction` joining its parent.
*   Cluster checks for each pull request fire its events through the API and read them back through `GET /v1/events` and the stream.

---

## Consequences

### Positive

*   An audit record of administrative changes exists with the actor and the resource after each change.
*   Run history survives termination.
*   A later consumer reads the internal stream with no change to producers.

### Negative

*   `kora-events` is a new binary, chart and deployment.
*   `event_outbox` is a write path into another binary's table.
*   Every producer write that records an event becomes a transaction with a second insert.
*   A repository that keeps `*pgxpool.Pool` does not join a context transaction. A producer must move to `storage.Pool` before it calls `Publish`.
*   Changes to MCP servers, skill marketplaces, connections and platform-wide resources record no event in v1.
*   `events:read` shows every run event in the tenant, including runs the caller cannot open through the runs API. Per-category scopes are the later fix.
*   Every producer maps its model into a payload struct, and a new entity field needs a struct change.

### Neutral

*   `storage.TxFunc` gains a context parameter.
*   Run queries gain a `deleted_at IS NULL` filter.

---

## Related Decisions

*   [Database Migrations](2026-09-10-database-migrations.md)
*   [Agent Triggers and Webhook Ingestion](2026-06-17-agent-triggers-and-webhook-ingestion.md)
*   [Extended Trigger Types](2026-06-20-extended-trigger-types.md)
*   [Trigger Payload Templating](2026-09-28-trigger-payload-templating.md)
*   [Trigger Fire Coalescing](2026-10-13-trigger-fire-coalescing.md)
*   [Agent Event Streaming Protocol](2026-05-11-agent-event-streaming-protocol.md)
*   [Run Activity Events](2026-09-29-run-activity-events.md)
*   [Declarative Resources](2026-10-24-declarative-resources.md)
*   [Dynamic Route Scope Registration](2026-04-14-dynamic-route-scope-registration.md)
