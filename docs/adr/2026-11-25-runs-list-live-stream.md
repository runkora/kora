# Runs List Live Stream

**Status:** Accepted (Amended 2026-11-26)  
**Date:** 2026-11-25  
**Deciders:** Justin Wilkin  
**Technical Story:** [Runs Tab PRD](../prd/runs-tab.md)

---

## Context

The Runs tab lists every run the viewer can read and updates the list live. It reads `GET /api/v1/runs` on `kora-agents` and needs a live feed of run changes with the same visibility.

`handleListRuns` (`backend/internal/agents/server.go`) serves the list. It takes `agent_type_id` (repeatable), `state` (repeatable or comma-separated), `started_by=trigger`, `created_after`, `created_before`, the grade filter (`grader`, `question`, `verdict`), a keyset `cursor` and `limit` (default 50, at most 200). `ListRuns` and `listRuns` (`backend/internal/agents/run_service.go`) apply both visibility rules in SQL:

*   `readableRunScope` lists the agent types whose runs the caller may read. `readableRunAgentTypes` queries policy for `agents:runs:<slug>:read` and adds the agents the caller created. Deleted agents stay in the scope.
*   `Own` is set for a `HUMAN` caller (`readsOwnRuns`). It adds the runs the caller started, of any agent type.
*   `RunVisibility` (`backend/internal/agents/run_access.go`) limits runs of a delegated agent to their starter unless the caller holds `agents:delegated:read`.
*   `appendScope` and `appendVisibility` write both rules into the query. `PostgresRepository.ListRuns` also filters `r.deleted_at IS NULL`. A terminated run leaves the list and stays readable by id.

`RunUsageTotals` adds each run's cost and tokens to the page. The list row carries no person name. The BFF resolves `created_by` to a display name (`listRunsWithNames`).

The per-run stream `handleStreamEvents` replays `run_events` and then reads the in-memory `PubSub` (`backend/internal/agents/pubsub.go`). `PubSub` fans out only within one process ([Agent Event Streaming Protocol](2026-05-11-agent-event-streaming-protocol.md)).

`kora-agents` records `agents.run.*` events in the event spine in the same transaction as the run change ([Event Spine](2026-11-06-event-spine.md)). The catalog is in `backend/internal/platform/events/catalog.go`. `stateEventType` (`backend/internal/agents/spine_events.go`) records no event for two transitions:

*   `pending` to `starting`, written by `AssignPod` (`backend/internal/agents/postgres.go`).
*   `idle` to `active`, a new turn.

`SetRunSummary` writes `runs.summary` once and records no event.

A run records its starter's subject in `created_by` (`StartRun` in `run_service.go`). The `runs` table and `RunPayload` carry no kind of principal for the starter.

`kora-events` serves `GET /internal/v1/events/stream?after=<seq>` with every tenant's events in seq order (`backend/internal/koraevents/handlers.go`). The `InternalController` middleware and the `kora-events-internal-stream` AuthorizationPolicy admit only `kora-triggers`. A request without `after` replays from seq 0. `kora-triggers` reads the stream through `backend/internal/platform/clients/events/client.go`. Its `EventConsumer` (`backend/internal/triggers/consumer.go`) runs on one replica under an advisory lock and stores its cursor. The public `GET /v1/events/stream` requires `events:read` and applies no run visibility.

### Security and Identity

The change touches the question "What can it access?". The stream is a read surface, and it must show a viewer exactly the runs `GET /api/v1/runs` shows them. It adds no identity, grant, scope or credential, and it leaves "Who is this agent?" and "How does it connect?" unchanged. `kora-agents` gains read access to every tenant's `agents.run` events on the internal stream. `kora-agents` records these events and already holds every tenant's runs. `kora-events` refuses it every other category.

### Goals

*   Push new runs, state changes and summaries to an open Runs tab.
*   Apply the list endpoint's visibility to every pushed run.
*   Keep the stream correct with more than one `kora-agents` replica.
*   Add the Mine, Source and search filters to the list and the stream.

### Non-Goals

*   Changes to the per-run stream.
*   Live cost between state changes.
*   Counts per filter.

---

## Decision Drivers

*   **Replica-safe:** A stream must see every run change, whichever replica wrote it.
*   **Same visibility as the list:** A viewer must never receive a run that `GET /api/v1/runs` would not return to them.
*   **Resumable:** A dropped stream must resume without a gap or tell the client to reload.
*   **One change feed:** Run changes already flow through the event spine.

---

## Decision 1: Source of Run Changes

### Option 1A (chosen): The kora-events internal stream

Each `kora-agents` replica reads the `agents.run` category of the internal stream for every tenant. Every replica reads it with no advisory lock and no stored cursor. Each replica serves its own open streams from what it reads.

### Option 1B (rejected): In-memory fan-out at the write site

`kora-agents` would publish each run change to a `PubSub` hub when it writes the run. Each replica sees only the writes it handles. A stream on one replica would miss changes written on another.

### Option 1C (rejected): Postgres `LISTEN/NOTIFY` on `runs`

A trigger or the repository would notify on each run change. It would add a second change feed beside the spine, and it carries no seq to resume from.

### Option 1D (rejected): The public tenant event stream

The console would read `GET /v1/events/stream`. The stream requires `events:read`, and only `platform-admin` and `kora-operator` hold that scope. The stream applies no run visibility.

---

## Decision 2: The Stream Endpoint

`kora-agents` serves `GET /api/v1/runs/stream` as Server-Sent Events. The route registers like the other run routes: authenticated-only, with run visibility checked in the service ([kora-agents Route & Scope Registration](2026-05-11-orbit-agents-route-scope-registration.md)).

*   The stream takes the list's filter parameters through the same parsing as `handleListRuns`. It takes no `cursor` or `limit`.
*   The stream refuses the grade filter with a 400. No run event records a new grade.
*   Each message carries the spine seq as `id` and one list row:

    ```
    id: 48213
    event: run
    data: {"type":"agents.run.idle.v1","matches":true,"run":{...}}
    ```

*   `run` has the shape of a list row and carries usage and `created_by_name`.
*   `matches` is true when the run passes the stream's filters. The stream sends every run the viewer can read and sets `matches` to false for a run outside the filters.
*   The console inserts a row only for `agents.run.created.v1` with `matches` true. It replaces a row it already shows with any message for that run, and it ignores the rest.
*   The stream sends a `: open` comment as soon as it registers, before any catch-up.
*   A comment line every 30 seconds keeps the stream open through proxies.

The console opens the stream before it fetches the first page and merges both by run id. Amendment 1 adds a provisional first page.

---

## Decision 3: Matching Each Event

Each replica handles an `agents.run.*` event once for all of its open streams:

1.  Skip the event when no open stream on the replica belongs to the event's tenant.
2.  Load the run row once with its usage and the starter's display name.
3.  For each open stream in the tenant, test the row against the stream's readable scope and filters in Go.
4.  Send the row to each stream whose scope reads it.

The stream computes its `RunScope` with `readableRunScope` when it opens and keeps it until it closes. The row is the run's current state. A late or repeated event sends the newest row.

The Go predicate lives in `backend/internal/agents/run_page.go` next to `appendScope`. It mirrors the SQL rule by rule:

*   The run's tenant is the caller's tenant.
*   The run's agent type is in `AgentTypeIDs`, or `Own` is set and the run's `created_by` is the caller's non-empty subject.
*   The run's agent type is in `OnlyAgentTypeIDs` when that list is set.
*   The agent's identity mode is not `delegated`, or `Vis.SeeAll` is set, or `created_by` is the caller's non-empty subject.
*   For `matches` only: `deleted_at` is unset, and the run passes `state`, `started_by`, `created_after`, `created_before`, `created_by`, `trigger_kind` and `q`.

`TestPostgres_RunMatchAgreesWithListRuns` in `run_page_postgres_test.go` seeds runs across identity modes, starters, states, trigger kinds, summaries and terminated runs. For each scope and filter combination it asserts that `ListRuns` returns exactly the seeded runs that the Go predicate matches.

---

## Decision 4: Resume and Stream End

*   A replica opens its feed at the hub position. It reopens a dropped feed from the last seq it handled.
*   A client reconnects with `Last-Event-ID`. The handler registers with the replica's feed and notes the feed's seq. It then reads the internal stream for the caller's tenant and the `agents.run` category from `Last-Event-ID` up to that seq, and it skips feed events at or below that seq.
*   The catch-up reads up to the feed's last seq for the caller's tenant.
*   The stream sends `event: reset`, and the console reloads the first page, when:
    *   Retention removed the event at `Last-Event-ID`.
    *   More than 1,000 events of the tenant follow `Last-Event-ID`.
    *   `kora-events` ends the catch-up before that seq.
    *   `Last-Event-ID` is 0.
    *   The replica's feed holds no position for the tenant.
*   A reconnect without `Last-Event-ID` starts at the feed's position.
*   Each stream buffers 256 messages. The feed closes a stream whose buffer is full, and its client reconnects with `Last-Event-ID`.
*   The stream closes at 60 minutes or at the caller's token expiry, whichever comes first. It also closes when the token is revoked, checked on each heartbeat as `kora-events` does.
*   A run that fails to load closes that tenant's streams on the replica. The feed moves on to the next event.

`kora-events` changes its internal stream to support this:

*   `InternalController` and the `kora-events-internal-stream` AuthorizationPolicy admit `kora-agents` beside `kora-triggers`.
*   `kora-events` refuses a `kora-agents` request with a 403 unless its only `category` is `agents.run`.
*   A request without `after` starts at the hub position with no replay. `kora-triggers` always sends `after`.
*   The stream takes the `tenant` and `category` filters of the public stream.
*   A request with `tenant` and `after` returns 410 when that tenant holds no event with seq `after`.

---

## Decision 5: New Filters

The list and the stream gain three filters:

*   `created_by=me` keeps the runs the caller started: `r.created_by` equals the caller's verified subject. Other values are refused.
*   `trigger_kind` keeps runs by source. The value `none` keeps runs with no trigger (`COALESCE(r.trigger_id, '') = ''`), and `webhook`, `scheduled`, `event` and `manual` match `r.trigger_kind`. The Source select offers `none`, `webhook`, `scheduled` and `event`.
*   `q` matches a case-insensitive substring of the run's summary, the agent's name, the trigger's name or the starter's display name. The query uses `ILIKE` with `%`, `_` and `\` escaped.

`ListRuns` and the stream's row load join `user_profiles` on `(id, tenant_id) = (r.created_by, r.tenant_id)` for `created_by_name`. The [Database Migrations](2026-09-10-database-migrations.md) ADR permits this cross-service read at query time. The row's `created_by_name` is the display name and never the email.

The cost of each filter is as follows:

*   Every list query walks `idx_runs_tenant_id` (`tenant_id, created_at DESC`) newest first and stops after `limit + 1` matches.
*   `created_by=me` gains a new index `runs (tenant_id, created_by, created_at DESC)`.
*   `trigger_kind` and `state` filter rows on the tenant walk.
*   `q` filters rows on the tenant walk. No index serves a substring `ILIKE`, and no migration installs `pg_trgm`. A rare term reads every run in the tenant.
*   The agent name comes from the `agent_types` join that `selectRun` already makes. The starter name comes from `user_profiles` through its primary key.

---

## Decision 6: New Run Events

Three run events join the catalog, and each payload is `RunPayload`. The event catalog in the [Event Spine PRD](../prd/event-spine.md) lists the types.

*   `agents.run.placed.v1`: `AssignPod` records it when it moves a run from `pending` to `starting`. `changed` is `["state", "pod_name"]`, and `pod_name` joins the `hiddenChanges` of `RunPayload`. The catalog description is "A run was placed on an agent pod."
*   `agents.run.resumed.v1`: `stateEventType` returns it for `idle` to `active`. `changed` is `["state"]`. The catalog description is "An idle run began a new turn."
*   `agents.run.summarized.v1`: `SetRunSummary` records it when it sets the summary. `changed` is `["summary"]`. `summary` joins the `hiddenChanges` of `RunPayload`, and the payload does not carry the summary text. The catalog description is "A run's task summary was set."

---

## Rationale

None at this time.

---

## Consequences

### Positive

*   The Runs tab shows run changes from every replica through one feed.
*   Event triggers can match `agents.run.placed.v1`, `agents.run.resumed.v1` and `agents.run.summarized.v1`.

### Negative

*   The internal stream carries every tenant's run events to each `kora-agents` replica, including tenants with no open stream.
*   A stream keeps the scope it opened with. A grant change or a new agent applies at the next connect, and a stream lasts no longer than its token.
*   `agents.run.resumed.v1` adds one event to the log for every turn after the first.
*   `trigger_kind=none` keeps runs an agent started with no trigger beside the runs people started. A run does not record the kind of principal that started it.
*   `q` reads every run in the tenant for a term that matches few runs.
*   Postgres `ILIKE` and Go case folding can disagree on characters outside ASCII.
*   `TestPostgres_*` tests do not run in CI. A change to the list SQL needs the agreement test run locally.

### Neutral

*   Runs created before `trigger_kind` was added have a trigger and no kind. They match no `trigger_kind` value.
*   The per-run stream and `PubSub` stay unchanged.

---

## Implementation

1.  **kora-events:** Admit `kora-agents` on the internal stream for the `agents.run` category only. Add the hub start, the `tenant` and `category` filters and the 410 answer.
2.  **kora-agents filters and events:**
    *   Add `agents.run.placed.v1`, `agents.run.resumed.v1` and `agents.run.summarized.v1` and record them in `AssignPod`, `stateEventType` and `SetRunSummary`.
    *   Add `created_by`, `trigger_kind` and `q` to the list parsing and to `RunListQuery`, and the `created_by` index through `make migrate-create`.
    *   Add the `user_profiles` join and `created_by_name` to the list row.
3.  **kora-agents stream:**
    *   Add the Go predicate and `TestPostgres_RunMatchAgreesWithListRuns`.
    *   Add the per-replica feed, the stream registry and `GET /api/v1/runs/stream`.
4.  **Frontend:** Add the Runs page, the BFF stream route, the sidebar entry in the place of Dashboard and the `/` redirect.

---

## Open Questions

None at this time.

---

## Amendment 1 (2026-11-26): Provisional First Page

**Status:** Accepted

The console draws a provisional first page before the page it reads after `: open`.

*   The Runs route (`frontend/src/app/(dashboard)/runs/page.tsx`) reads the first page for the URL's filters on the server with the caller's session. The page renders with those rows.
*   When the server sends no page for the filters, or the filters change, the console reads a first page while the stream opens.
*   The console still reads the first page after `: open`. That page replaces the provisional page.
*   Stream messages that arrive before that page lands wait in the feed. The console replays them onto that page.
*   A provisional page that lands after that page is dropped.
*   A provisional page comes from `GET /api/v1/runs` with the caller's token. It holds only runs the list returns to the caller.
*   The provisional rows can be stale from the moment they are read until the page read after `: open` lands. A change in that window shows when that page lands.

### Consequences

*   **Negative:** Each Runs load reads the first page twice.

---

## Related Decisions

*   [Event Spine](2026-11-06-event-spine.md)
*   [Agent Event Streaming Protocol](2026-05-11-agent-event-streaming-protocol.md)
*   [Delegated Agent Identity](2026-07-23-delegated-agent-identity.md)
*   [kora-agents Route & Scope Registration](2026-05-11-orbit-agents-route-scope-registration.md)
*   [Database Migrations](2026-09-10-database-migrations.md)
*   [Nav Cleanup and Live Dashboard](2026-06-19-nav-cleanup-and-live-dashboard.md): this decision supersedes its live dashboard home in part.
