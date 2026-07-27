# Extended Trigger Types and an Operable Delivery Pipeline

<!-- Filename: 2026-06-20-extended-trigger-types.md -->

**Status:** Proposed
**Date:** 2026-07-27
**Deciders:** Justin Wilkin, Platform Engineering
**Technical Story:** [PRD: Extended Trigger Types](../prd/extended-trigger-types.md)

---

## Context

The Triggers v1 ADR ([2026-06-17](2026-06-17-agent-triggers-and-webhook-ingestion.md)) shipped webhook ingestion as the only trigger kind: an inbound signed request from a registered `WebhookSource` starts an agent run under a per-source JIT-minted `SERVICE` principal. That design is sound and stays.

This ADR covers three things:

1. **Extracting `orbit-triggers`** — the separate binary the v1 ADR committed to when a second trigger kind landed.
2. **Two new trigger kinds** — scheduled (cron) and manual (API-fired).
3. **An operable delivery pipeline** — exact outcomes for every way a run can fail to start, per-trigger admission control, and the delivery-safety machinery (timeout, duplicate suppression, auto-pause, payload retention) that the console redesign needs in order to have anything real to show.

### The state these decisions start from

Four facts about the code as it stands. Each one bears directly on a decision below, and two of them are defects.

- **`internal/triggers/` runs inside `identity`.** The v1 ADR called that a v1-only choice and committed to extracting it "when the second trigger surface lands". Two trigger surfaces now land at once. Decision 1.
- **The fire endpoint is not properly guarded.** `server.go` carries a `KNOWN GAP` comment: `POST /triggers/{id}/fire` requires only `triggers:write`, so any principal that can manage triggers can fire any trigger. A per-trigger scope was intended but never enforced. Decision 6.
- **`ScopeEnforcementMiddleware` cannot express a per-trigger scope.** `RouteScope` matches static prefixes; there is no route-param substitution. This is why the gap above went unnoticed — the protection was assumed to be declarative when no such mechanism exists. Decision 6.
- **The scheduler is already safe across replicas.** `TickScheduled` inserts its delivery under `UNIQUE (trigger_id, scheduled_at)` *before* dispatching, so a second replica's tick loses the insert race and stops. At-most-once holds today without any coordination. Decision 2.

### Where the design reference is not followed

The design reference proposes a `slug`-addressed, token-in-path webhook endpoint (`hooks.orbit.sh/t/{slug}/{token}`) and a flat one-trigger-per-delivery model. Neither is adopted. The PRD records the product reasoning (Alternatives 3 and 5); the technical reasoning is that the current header-based HMAC-over-body scheme is correct and changing it would re-point every registered source, and that the existing `trigger_deliveries` → `trigger_delivery_runs` fan-out **is already the two-level outcome model the PRD asks for**. Flattening it would be a regression.

### Security and identity

This feature creates agent identity and gates access, so the three questions apply. One of them applies differently here than elsewhere in the platform, and it is worth being exact about why.

**Who is this agent? A trigger's principal is autonomous, not delegated.** Orbit's usual identity story is delegation: a person hands scope to an agent, the agent is accountable to that person, and it can never access more than the delegator. **Triggers are the case where that model does not apply.** A GitHub push at 3am and a cron tick have no human initiator. There is no delegator, and therefore **no delegator ceiling to intersect with**.

What holds instead is attribution to *configuration* rather than to *initiation*: an admin created the source or trigger and attached a grant, and that grant is the **sole** bound on what the resulting run can do. Nothing clips it from above.

Three consequences follow, and they raise the stakes on the rest of this ADR rather than lowering them:

1. **Creating a trigger creates a standing autonomous capability.** Its grant is not a request for permission that something else will narrow; it is the whole permission. Trigger creation therefore deserves the same scrutiny as granting a long-lived credential, because that is what it is.
2. **A manual trigger's caller does not narrow it either.** The run executes under the trigger's principal, so a caller with narrow permissions firing a broadly-scoped trigger gets the trigger's scope, not their own. This is deliberate — a manual trigger is a published capability with a fixed blast radius — and it is exactly why firing needs its own per-trigger authorization check rather than inheriting from trigger-management permission (Decision 6).
3. **The audit question is "who configured this and what policy authorised it", not "who asked for it".** Delivery history answers the first; the policy decision answers the second. Neither can be replaced by a delegation chain, because there isn't one.

**What can it access?** Exactly what its grant allows, and nothing narrows that at request time. The new access-control surface is the fire endpoint, currently under-enforced (Decision 6). Firing is an authorization decision checked before the run starts, per trigger, default-deny.

**How does it connect?** Unchanged. Triggers start runs; a run's egress goes through the existing broker. Nothing here touches credentials.

One further consideration: the PRD wants a failed signature to be visible to an admin, but a failed signature is an unauthenticated request, so anything written in response to it has attacker-controlled volume. Decision 4 settles this without writing to the delivery table at all.

---

## Decision Drivers

- **Minimal disruption is a hard requirement**, not a preference. Registered sources and delivery history exist. No re-pointing endpoints, no re-issuing secrets, no destructive migration.
- **The delivery record must not assume an HTTP origin.** A scheduled tick has no sender, signature, or response code, and four of the six planned kinds have none either.
- **Prefer extending existing structures to adding new ones.** The fan-out table, the dedup indexes, and the kind discriminant already exist and are right.
- **Fail closed on authorization.** An unenforced scope is worse than an absent one, because the documentation claims protection that is not there.
- **Do not build HA machinery that a unique index already provides.**
- **Keep internet-reachable ingestion away from the token authority.** Webhook ingestion is the one surface a hostile party can drive directly. It should not share a process with the service that mints every token in the platform.
- **Triggers are a front door, not an orchestrator.** They are the set of ways something can start in the platform. What gets started is a separate concern, and the multi-agent workflow engine is a separate pillar. Nothing here may grow into it.

---

## Considered Options

### Decision 1 — Where does the triggers service run?

Triggers went into `identity` in v1 because it was small. It is no longer small, and size is the weakest reason to move it. Two better ones decide this.

**Webhook ingestion is unauthenticated, internet-reachable traffic. `identity` is the platform's token authority.** It serves OAuth, the device grant, session exchange and JWKS — the auth path every other service depends on. Co-locating the one endpoint a hostile party can drive directly with the service that mints every token in the platform is a coupling worth breaking on its own merits. A webhook flood should not be able to degrade authentication.

**Triggers is about to become three background loops** — the cron scheduler, the pending-delivery worker, and the retention purge job. Long-running background work inside a latency-sensitive request-serving auth API is the actual structural problem, more than the line count is.

#### 1A: Keep `internal/triggers/` in `identity`

**Pros:** No deployment work. Token minting stays in-process.

**Cons:** Both couplings above persist and get worse with every phase of this ADR. `identity` is pinned at one replica, so triggers cannot scale independently even though Decision 2 establishes that it safely could.

#### 1B: Extract `orbit-triggers` (**chosen**)

**Pros:** Separates internet-facing ingestion from the token authority. Moves three background loops out of the auth API. Lets triggers scale independently. Executes the commitment the v1 ADR made, closing the gap between the documented and actual architecture.

**Cons:** A new deployment to operate. Token minting becomes a network call.

**Chosen: 1B.** The obvious objection to extraction is that it would move the webhook ingestion URL and break every registered source, which the PRD forbids outright. It does not, and the three costs that look largest are the three that are already paid:

- **The URL does not move.** `/api/v1/webhooks/` is a path-prefix match in the api VirtualService. Extraction changes one `destination.host`. The address every registered source posts to is unchanged, so no source is re-pointed and no secret re-issued. The edge is path-routed precisely so services can move behind it.
- **Token minting was already designed for this.** `TokenMinter` is a one-method interface injected from main, and `internal/triggers/` never imports the identity feature package. Swapping the in-process implementation for a typed client is the platform-clients pattern used throughout the codebase, and `internal/triggers/` itself does not change.
- **Migrations are a file move.** `make migrate-up` collects every `backend/cmd/*/migrations/*.sql` into one directory against a single `goose_db_version` table, ordered by timestamp prefix. Moving the trigger migrations to a new binary preserves filenames and ordering, so goose sees no change at all.

What remains is mechanical and follows documented patterns: a `cmd/orbit-triggers/` main and config, an internal mint endpoint on `identity` with a typed client satisfying `TokenMinter`, a chart deployment and service, a client-mTLS DestinationRule and the app port in the mesh netpol, a Chart.yaml version bump and operator rebuild, and repointing in-mesh callers of the management API (the frontend BFF's `ORBIT_API_URL`, which gains a triggers base URL, and the MCP trigger tools).

**Sequencing: extraction lands first, before any feature phase.** The package is the smallest it will ever be right now. Extracting after admission control, the worker loop and retention have been built means moving substantially more code. It ships as its own PR so the deployment and mesh changes are reviewable separately from the feature work.

---

### Decision 2 — Scheduler concurrency safety

#### 2A: Rely on the existing insert-then-dispatch dedup (**chosen**)

`TickScheduled` already inserts a delivery keyed `UNIQUE (trigger_id, scheduled_at)` before starting a run. Any replica losing that race stops. Ticks are minute-granular, so the key is naturally idempotent per minute.

**Pros:** Already implemented and exercised. No lease, no lock, no new infrastructure. Correct under N replicas *and* under restart-within-the-minute, which is the case leader election handles worst.

**Cons:** Every replica runs the full `ListEnabledScheduledTriggers` scan each minute — wasted work proportional to replica count.

#### 2B: Kubernetes Lease leader election

**Pros:** Only one replica scans.

**Cons:** A new dependency in a binary that is not a controller, and a lease handover window can *miss* a tick, which is worse than the duplicate scanning 2A wastes. Solves a problem the unique index already solved.

#### 2C: Postgres advisory lock per tick

**Pros:** No new infrastructure.

**Cons:** Introduces a locking primitive the codebase does not otherwise use, for a guarantee the unique index already gives declaratively.

**Chosen: 2A.** The mechanism already in place is the right one. Two changes: the losing insert logs at `debug` rather than `info` (it is normal under multiple replicas and today reads as though it were noteworthy), and the scan gains the bound described in [Implementation](#implementation).

**Missed ticks.** PD1 says a fire missed during downtime is *recorded as missed and skipped*. Today it is silently not fired, with no record at all. The scheduler gains a bounded backfill: on each tick, for each enabled scheduled trigger, any due minute in the last hour with no delivery row is inserted with outcome `missed` and no run started. One hour bounds the write burst after a long outage. With `catch_up` set, that insert becomes a normal fire instead.

---

### Decision 3 — The outcome model

#### 3A: Extend both existing status columns; add no tables (**chosen**)

The schema already has exactly the two levels the PRD requires:

- `trigger_deliveries.status` — the arrival: `verified`, `dispatched`, `skipped`, `failed`
- `trigger_delivery_runs.status` — per matched trigger: `dispatched`, `failed`

Both vocabularies are extended. Nothing is renamed, so existing rows keep their meaning.

| Level | Value | New | Meaning |
|---|---|---|---|
| Delivery | `verified` | | Arrived and established as genuine |
| Delivery | `duplicate` | ● | Already handled; references the original |
| Delivery | `skipped` | | Genuine, but no trigger was listening |
| Delivery | `missed` | ● | A scheduled fire that was due but not run |
| Delivery | `dispatched` | | At least one trigger started a run |
| Delivery | `failed` | | Matched, but no trigger started a run |
| Trigger | `dispatched` | | This trigger started a run |
| Trigger | `failed` | | This trigger tried and failed, including dispatch timeout |
| Trigger | `filtered` | ● | Listening, but its `match_condition` excluded this event |
| Trigger | `denied` | ● | Policy refused this trigger's run |
| Trigger | `superseded` | ● | Replaced by a newer fire under `replace` overflow |
| Trigger | `dropped` | ● | Refused at the ceiling, or evicted from the backlog |
| Trigger | `paused` | ● | The trigger was auto-paused when this fire arrived |

**Pros:** Additive. `trigger_delivery_runs` already carries `error TEXT`, so denial reasons need no new column. The console's per-trigger badges read the level they belong to.

**Cons:** Two enums to keep coherent.

#### 3B: One flat status on the delivery

Rejected: cannot express "started two runs, denied by a third", which is the PRD's central requirement.

#### 3C: A new `delivery_outcomes` table

Rejected: `trigger_delivery_runs` *is* that table. A second one would leave two overlapping records of the same thing.

**Chosen: 3A**, with an invariant the implementation must uphold:

> A delivery's status is a **summary derived from its runs**, never independently authored. `dispatched` if any run dispatched; `failed` if runs exist and none dispatched; `skipped` if no trigger matched; `duplicate` or `missed` if it never reached matching. Today `dispatch()` sets delivery status inline from the first success, which is exactly what makes "started two, denied one" unrepresentable. The summary must be computed once the fan-out completes.

Every row in `trigger_deliveries` is an arrival that authenticated successfully (Decision 4). Failed authentication never reaches this table.

`filtered` needs a bound. Recording a row for every trigger that did *not* want an event turns a noisy source into a write multiplier. It is therefore recorded **only for triggers that matched the source and event type but were excluded by `match_condition`** — a filter the admin authored and can debug. A trigger listening for a different event type entirely is not a filter decision and produces no row.

---

### Decision 4 — Surfacing failed authentication without writing on an unauthenticated request

Two distinct things get conflated here, so state them separately.

**Recording before authorization is settled and safe.** The pipeline order is *authenticate → record → match → authorize → admit → run*. The PRD's "record before you decide" requirement concerns the gap between record and authorize: today a policy denial often leaves no delivery row, so "why didn't my trigger fire" is unanswerable. The delivery is recorded before the policy check. That request is already authenticated. No decision needed.

**This decision is only about requests that fail authentication** — a webhook with a bad signature, a missing delivery id, or a disabled source. Today `Ingest` returns before inserting anything, so nothing is recorded. The PRD wants an admin to be able to diagnose a wrong signing secret, which is the most common cause and currently presents as silence.

The constraint is that anything written here is written **in response to an unauthenticated request**, so its volume is attacker-controlled.

#### 4A: A rejection counter on the source; no delivery row (**chosen**)

`webhook_sources` gains `rejection_count`, `last_rejection_at`, and `last_rejection_reason`. A failed verification is an `UPDATE` to the single row the tenant already owns.

**Pros:** Volume is fixed no matter what an attacker sends — no rows are created, only one existing row is touched. `trigger_deliveries` stays exclusively a record of authenticated arrivals, which keeps the delivery vocabulary honest and means every payload in the table came from a verified sender. The source drawer can still show "142 rejections, last 30 seconds ago: invalid signature", so a wrong secret is diagnosable.

**Cons:** Rejections are not in the deliveries feed, so an admin looking at the cross-trigger feed won't see them; they have to open the source. Only the most recent reason is kept, not a history.

#### 4B: Bounded `rejected` delivery rows

One row per `(source_id, reason, minute)`, no payload, enforced by a partial unique index.

**Pros:** Rejections appear in the same feed as everything else, so diagnosis needs no separate place to look.

**Cons:** An unauthenticated caller can still cause rows to be created. Bounding the rate makes it survivable rather than making it right, and "we mitigated it with an index" is a weaker position than not doing it. It also mixes unauthenticated input into a table whose other rows are all verified.

#### 4C: Metrics and logs only

**Pros:** Strictest posture; an unauthenticated request causes no database write at all.

**Cons:** An admin with a wrong signing secret sees silence in the console and must find someone with log access. That is the PRD's headline complaint, unaddressed.

**Chosen: 4A.** It gives the diagnosis 4C loses without the unauthenticated `INSERT` 4B needs. Requests carrying an *unknown* source id are handled by 4C's rule and touch nothing — there is no tenant to attribute them to and no row that legitimately exists to update.

**Consequence for the delivery vocabulary:** `rejected` is therefore **not** a delivery status. The vocabulary in Decision 3 drops it, and `trigger_deliveries` carries only arrivals that authenticated successfully. `duplicate` and `missed` remain, since both describe authenticated or platform-originated arrivals.

---

### Decision 5 — Admission control and the waiting backlog

Per-trigger concurrency requires knowing how many runs a trigger has in flight, across replicas.

#### 5A: A durable pending-delivery queue, claimed with `FOR UPDATE SKIP LOCKED` (**chosen**)

A `trigger_pending_deliveries` table holds fires that are waiting. A worker loop claims rows with `SELECT ... FOR UPDATE SKIP LOCKED`, checks the in-flight count, and dispatches. In-flight count is a query against `trigger_delivery_runs` for runs started and not yet terminal.

**Pros:** Correct across replicas and restarts, so a `202` is a real promise — the PRD calls this the highest-consequence choice. The three overflow behaviours become ordinary row operations: `replace` deletes waiting rows for the trigger before inserting, `wait` trims the oldest past the depth limit, `refuse` skips the insert. Disabling a trigger deletes its waiting rows and records them `dropped`, which the PRD requires and nothing else provides.

**Cons:** A new table and a worker loop.

#### 5B: In-memory counter and channel per trigger

**Pros:** Simplest, lowest latency.

**Cons:** Wrong with more than one replica — each enforces its own ceiling, so a limit of 4 becomes 4N. Everything waiting is lost on restart, making the `202` a lie. This is precisely the default the PRD asked not to be taken silently.

#### 5C: Redis

Rejected: Redis is available, but a second durability story for data that must agree with the Postgres delivery record is worse than one table.

#### 5D: A durable execution engine (Temporal or similar)

Considered seriously, because Orbit intends to build a multi-agent workflow engine later (see Decision 8) and a durable execution engine is a plausible foundation for one.

**Pros:** Durability, retries, backoff, timeouts and long-running state machines all come as primitives rather than as code. If a workflow pillar arrives, much of it is already built.

**Cons, in the order they matter:**

- **It does not solve the hard part.** Durability is commodity — `SKIP LOCKED`, River, Temporal and SQS all provide it. What no engine provides is Orbit's actual differentiator: carrying *which principal this runs as* and *what authorised it* through deferred execution. That layer gets built either way. Adopting an engine means building it **and** operating the engine.
- **Sequencing.** The workload that would justify it does not exist yet. Designing a durable-execution foundation against one consumer — a per-trigger concurrency limit — bakes in that consumer's assumptions. The workflow engine is the consumer with real requirements, and it should be designed against those.
- **Operational cost.** Self-hosted Temporal's weight is not its process count (the four server roles can run as one binary); it is history shard management, retention configuration and datastore growth, on a platform already operating SPIRE, Istio, OPA, Postgres, Redis and Dex.
- **Retry is not currently a benefit.** The PRD excludes automatic retry deliberately: webhook providers already retry, so platform-side retry compounds someone else's backoff. The headline feature is one this feature does not want.

**Rejected for now, revisit when the workflow engine is designed.** That is the decision point, not this one. `Admission` is an interface, so a durable-execution-backed implementation can replace the Postgres one without touching the pipeline — the door stays open without being walked through early.

**Chosen: 5A, with every fire routed through the queue.**

The tempting optimisation is a fast path: when a trigger is under its ceiling and nothing is waiting, dispatch directly and leave the queue for the contended case. That covers almost every fire, so the queue would rarely be touched. It is rejected, because it means a process dying between recording the delivery and starting the run loses that fire silently — the delivery sits at `verified` forever with no run and nothing to retry it. That is a durable *record* of a start that never happened, which is worse than either a durable start or an honest failure.

Triggers is the platform's front door for starting things (Decision 8), so **an acknowledged fire always starts** is a property worth being able to state without qualification. Routing everything through the queue makes it true, and leaves one dispatch path instead of two — two paths being the same class of coherence risk as two status vocabularies, and worth avoiding for the same reason.

The cost is claim latency on every fire. That is acceptable because the sender has already been acknowledged by then: everything after **RECORD** is asynchronous, so the latency is on no caller's path. `LISTEN`/`NOTIFY` on insert reduces it to a wake-up rather than a poll interval.

`Admission` stays deliberately narrow — concurrency and overflow, nothing more. It is not an attempt at a general deferred-work primitive, because that primitive belongs with the workflow engine (Decision 8) and would be under-designed if invented here.

**Timeout.** `dispatchTimeout` is currently a package constant covering the whole fan-out. It becomes per-trigger and covers **starting** the run, not running it. A timeout records the trigger-level outcome as `failed` and counts toward auto-pause.

---

### Decision 6 — Fire authorization (closing the KNOWN GAP)

The fire endpoint requires `triggers:write` today, so trigger-management permission implies permission to fire anything. `ScopeEnforcementMiddleware` cannot express a per-trigger scope: `RouteScope` matches static prefixes.

#### 6A: Explicit in-handler policy evaluation against a qualified scope (**chosen**)

The route keeps its static registration for authentication; the handler evaluates a per-trigger scope through the policy evaluator it already holds, before the service is called.

The scope literal follows the platform's qualified-scope convention (`type:path...:action`, hierarchy by prefix `covers`, unbounded dimension deepest):

```
trigger:<trigger-id>:fire
```

The obvious alternative form, `triggers:fire:<trigger-id>`, is wrong: it puts the action before the identifier, so `triggers:fire` would not `covers` a specific trigger and the hierarchy inverts. A grant on the prefix `trigger` covers all triggers by the normal prefix rule; no `*` literal is introduced.

**Pros:** Correct hierarchy, default-deny, fails closed. Uses the evaluator already wired into the controller. No middleware change.

**Cons:** Authorization lives in a handler rather than declared on a route, which is easier to forget on a future endpoint. Mitigated by a test asserting that a principal with `triggers:write` and no trigger grant is refused.

#### 6B: Extend `ScopeEnforcementMiddleware` to substitute route params

**Pros:** Declarative, and benefits every future per-resource endpoint.

**Cons:** A change to the enforcement path used by every protected route in the platform, made to fix one endpoint. Worth doing, but as its own ADR with its own review, not as a side effect of a triggers feature.

**Chosen: 6A**, with 6B recorded as the better long-term shape in [Future Work](#future-work).

**The caller's identity does not become the run's identity.** The run continues to execute under the trigger's own `SERVICE` principal; the caller is recorded in `fired_by`. This is deliberate: a manual trigger is a published capability with a fixed blast radius, not a way to run something as yourself.

---

### Decision 7 — Payload storage and the event catalogue

**Payload.** `payload_excerpt` is superseded by a full `payload` column plus `payload_bytes`, with a size ceiling: oversized payloads are truncated and flagged, never rejected, because a trigger should still fire. A purge job nulls `payload` past the tenant's retention (default 30 days, tenant-shortenable per PD4) and leaves every other column, so delivery metadata is kept indefinitely as the PRD requires.

**Event catalogue (PD7).** Adapters already know their provider's event types, so `SourceAdapter` gains `EventTypes() []string`. Because a compiled list goes stale between releases — the liability the PRD names — the list offered in the console is the **union of the adapter's declared types and the event types actually observed for that source**, which delivery history already records. A provider's new event type becomes selectable as soon as one arrives, without a release.

---

### Decision 8 — A trigger targets *something the platform can start*, not an agent

Orbit intends to build a multi-agent workflow engine as a separate pillar. Triggers are not that engine and must not grow into it. Triggers are the **front door**: the set of ways something can *start* in the platform. What gets started is a separate concern, and today there is exactly one answer.

That framing has one concrete consequence, and it is cheap now and a migration later.

#### 8A: Polymorphic target (**chosen**)

`triggers` gains `target_kind TEXT NOT NULL DEFAULT 'agent'` alongside the existing `agent_type_id`. Today the only valid value is `agent`. When the workflow engine lands, `workflow` becomes valid and a trigger can start one without changing what a trigger *is*, how it is authorized, or how its deliveries are recorded.

**Pros:** One column and a check constraint. Every trigger kind already produces the same thing — a request to start something under a bounded identity — so nothing downstream has to know which. Keeps triggers a front door rather than an agent-launching mechanism that later needs prising apart.

**Cons:** A column with one legal value until a pillar that does not exist yet arrives. If the workflow engine is never built, it is dead weight — small, but real.

#### 8B: Leave the target as `agent_type_id`

**Pros:** Nothing speculative.

**Cons:** Every trigger kind, every API surface and every console view would encode "a trigger starts an agent". Adding a second target type later means a migration plus changes across all of them, when the alternative is one nullable-defaulted column now.

**Chosen: 8A.** This is the only place this ADR builds for a future pillar, and it is deliberately the smallest possible accommodation: a discriminator, not an abstraction. No target-dispatch interface, no plugin registry, no indirection. When the workflow engine exists it will bring its own requirements, and a discriminator is enough to let it attach without the trigger model fighting it.

Explicitly **not** built here: any deferred-work, step, or orchestration primitive. Those belong to the workflow engine and would be under-designed with triggers as their only consumer.

---

## Decision

**Chosen: 1B + 2A + 3A + 4A + 5A + 6A + 7 + 8A.**

`internal/triggers/` is extracted into an `orbit-triggers` binary, executing the commitment made in v1, with the public webhook URL preserved by the existing path routing. Triggers are the platform's front door for starting things, so a trigger's target carries a kind discriminator (`agent` today, `workflow` when that pillar lands) rather than being an agent binding. The scheduler's existing insert-then-dispatch dedup is the concurrency-safety mechanism; missed ticks gain a bounded backfill that records rather than silently drops. Both existing status vocabularies are extended rather than replaced, with delivery status derived from the run fan-out. A webhook that fails authentication updates a rejection counter on its source rather than creating a delivery record, so an unauthenticated request can never grow a table. Admission control is a durable pending-delivery queue claimed with `SKIP LOCKED`, with every fire routed through it so an acknowledged fire always starts. The fire endpoint's unenforced scope is closed with an explicit per-trigger policy check under a corrected scope literal. Payloads are stored in full with automatic retention, and the event catalogue is adapter-declared plus observed. No workflow, orchestration or general deferred-work primitive is built here.

### Rationale

Most decisions above take the same form: **the existing structure is closer to right than the design reference assumed, so extend it.** The fan-out table already is the two-level outcome model. The unique index already is the scheduler's concurrency control. The adapter already knows its event types. The three genuinely new mechanisms — the pending queue, the source rejection counter, and the fire scope check — are each the smallest thing that makes a specific PRD requirement true, and each replaces something currently either absent or incorrect.

Two decisions cost more than the minimum, deliberately.

The **pending-delivery queue** could have been an in-memory counter, which would be far less code and is wrong: it enforces the wrong limit under multiple replicas and loses waiting work on restart, making the acknowledgement the platform sends a lie. The PRD named this the decision not to default silently, and this is the answer.

The **extraction** is not required by any feature in this ADR, and it is done anyway. Webhook ingestion is the only unauthenticated, internet-reachable surface in the platform, and it currently shares a process with the service that mints every token. That coupling is worth breaking regardless of what else is being built, and the package will never be smaller or cheaper to move than it is now.

---

## Architecture

### Delivery lifecycle

```
                  ┌──────────────┐   ┌──────────────┐   ┌──────────────┐
   webhook ──────▶│              │   │              │   │              │
   POST /webhooks │              │   │              │   │              │
                  │   ADAPTER    │   │   RECORD     │   │   MATCH      │
   schedule ─────▶│  verify /    │──▶│  delivery    │──▶│  triggers    │
   cron tick      │  tick / bind │   │  (always,    │   │  for source  │
                  │              │   │   first)     │   │  + event     │
   manual ───────▶│              │   │              │   │              │
   POST /fire     └──────┬───────┘   └──────────────┘   └──────┬───────┘
                         │                                     │
     auth fails: counter │      delivery status is derived     │ per matched
     on source, no row ──┘      after the fan-out completes    │ trigger
                                (duplicate / missed / skipped  │
                                 / dispatched / failed)        ▼
                                                        ┌──────────────┐
                                                        │  AUTHORIZE   │──▶ denied
                                                        └──────┬───────┘
                                                               ▼
                                                        ┌──────────────┐
                             under ceiling ◀────────────│    ADMIT     │──▶ dropped
                                    │                   │  ceiling +   │    superseded
                                    │      at ceiling ─▶│  overflow    │    paused
                                    │                   └──────┬───────┘
                                    │                          │ wait
                                    ▼                          ▼
                             ┌──────────────┐          ┌──────────────┐
                             │  START RUN   │◀─────────│   PENDING    │
                             │  mint token  │  claimed │  (durable)   │
                             │  → agents    │  SKIP    └──────────────┘
                             └──────┬───────┘  LOCKED
                                    ▼
                             ┌──────────────┐
                             │   RECORD     │  per-trigger outcome, then
                             │   OUTCOME    │  derive the delivery summary
                             └──────────────┘
```

The response to the sender is sent at **RECORD**. Everything to the right of it is asynchronous.

### Schema

All changes are additive. No column is dropped or renamed; no existing row changes meaning.

```sql
-- Trigger target: what gets started (Decision 8). 'agent' is the only legal
-- value today; 'workflow' becomes legal when that pillar lands, without
-- changing what a trigger is or how it is authorised.
ALTER TABLE triggers
    ADD COLUMN target_kind TEXT NOT NULL DEFAULT 'agent';

ALTER TABLE triggers
    ADD CONSTRAINT triggers_target_kind_check
    CHECK (target_kind IN ('agent'));

-- Trigger: admission control and delivery safety.
ALTER TABLE triggers
    ADD COLUMN max_parallel         SMALLINT    NOT NULL DEFAULT 4,
    ADD COLUMN overflow             TEXT        NOT NULL DEFAULT 'wait',
    ADD COLUMN max_queue_depth      SMALLINT    NOT NULL DEFAULT 32,
    ADD COLUMN timeout_seconds      SMALLINT    NOT NULL DEFAULT 30,
    ADD COLUMN dedupe_enabled       BOOLEAN     NOT NULL DEFAULT TRUE,
    ADD COLUMN autopause_enabled    BOOLEAN     NOT NULL DEFAULT TRUE,
    ADD COLUMN consecutive_failures INT         NOT NULL DEFAULT 0,
    ADD COLUMN auto_paused_at       TIMESTAMPTZ;

ALTER TABLE triggers
    ADD CONSTRAINT triggers_overflow_check
    CHECK (overflow IN ('wait', 'replace', 'refuse'));

-- Per-kind defaults are applied at creation, not by the column default:
-- webhook -> 'wait', scheduled -> 'replace', manual -> 'refuse'.

-- Delivery: full payload and the widened outcome vocabulary.
ALTER TABLE trigger_deliveries
    ADD COLUMN payload        BYTEA,
    ADD COLUMN payload_bytes  INT,
    ADD COLUMN payload_purged BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN reason         TEXT,
    ADD COLUMN redelivery_of  UUID REFERENCES trigger_deliveries(id);

ALTER TABLE trigger_deliveries DROP CONSTRAINT IF EXISTS trigger_deliveries_status_check;
ALTER TABLE trigger_deliveries
    ADD CONSTRAINT trigger_deliveries_status_check
    CHECK (status IN ('verified','dispatched','skipped','failed',
                      'duplicate','missed'));

-- Failed authentication never creates a row (Decision 4A). It updates the
-- source the tenant already owns, so an unauthenticated request cannot grow
-- any table. Only the most recent reason is kept.
ALTER TABLE webhook_sources
    ADD COLUMN rejection_count       BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN last_rejection_at     TIMESTAMPTZ,
    ADD COLUMN last_rejection_reason TEXT;

-- Per-trigger outcome: the level admission control and auto-pause act on.
ALTER TABLE trigger_delivery_runs DROP CONSTRAINT IF EXISTS trigger_delivery_runs_status_check;
ALTER TABLE trigger_delivery_runs
    ADD CONSTRAINT trigger_delivery_runs_status_check
    CHECK (status IN ('dispatched','failed',
                      'filtered','denied','superseded','dropped','paused'));

-- The waiting backlog. Rows are transient; they do not replace delivery history.
CREATE TABLE trigger_pending_deliveries (
    id          UUID        PRIMARY KEY,
    tenant_id   TEXT        NOT NULL,
    trigger_id  UUID        NOT NULL REFERENCES triggers(id) ON DELETE CASCADE,
    delivery_id UUID        NOT NULL REFERENCES trigger_deliveries(id) ON DELETE CASCADE,
    fields      JSONB       NOT NULL DEFAULT '{}'::jsonb,
    enqueued_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    claimed_at  TIMESTAMPTZ
);

CREATE INDEX idx_pending_claim
    ON trigger_pending_deliveries (trigger_id, enqueued_at)
    WHERE claimed_at IS NULL;

-- Tenant payload retention (PD4). An absent row means the 30-day default.
CREATE TABLE trigger_retention_settings (
    tenant_id    TEXT     PRIMARY KEY,
    payload_days SMALLINT NOT NULL DEFAULT 30
                 CHECK (payload_days BETWEEN 1 AND 30)
);
```

### Contracts

```go
// Admission decides whether a matched trigger may start a run now.
// Implementations must be correct across replicas.
type Admission interface {
    // Admit returns AdmitDispatch when the trigger is under its ceiling, or
    // the terminal outcome to record when it is not. AdmitEnqueue means the
    // fire is durably waiting and will be claimed by a worker.
    Admit(ctx context.Context, t Trigger, deliveryID string, fields map[string]string) (AdmissionResult, error)

    // Release frees a slot when a run reaches a terminal state.
    Release(ctx context.Context, triggerID, runID string) error

    // Drain removes everything waiting for a trigger, recording each as
    // dropped. Called when a trigger is disabled, auto-paused, or deleted.
    Drain(ctx context.Context, triggerID string) (int, error)
}

type AdmissionResult int

const (
    AdmitDispatch   AdmissionResult = iota // start now
    AdmitEnqueue                           // durably waiting
    AdmitSuperseded                        // replaced a waiting fire, or was replaced
    AdmitDropped                           // refused at the ceiling, or evicted
    AdmitPaused                            // the trigger is auto-paused
)

// SourceAdapter gains one method; the existing ones are unchanged.
type SourceAdapter interface {
    // Verify, DeliveryID, EventType, Fields — unchanged.

    // EventTypes returns the provider's known event types. The console offers
    // the union of these and the types observed for the source, so a new
    // provider event becomes selectable without a release (Decision 7).
    EventTypes() []string
}

// Repository additions. InsertScheduledDelivery is promoted onto the interface:
// TickScheduled currently type-asserts to *postgresRepository to reach it,
// which defeats the interface and blocks testing against a fake.
type Repository interface {
    // existing methods unchanged

    InsertScheduledDelivery(ctx context.Context, d *Delivery) (bool, error)
    RecordSourceRejection(ctx context.Context, sourceID, reason string) error
    DueScheduledMinutesWithoutDelivery(ctx context.Context, triggerID string, since time.Time) ([]time.Time, error)

    ClaimPending(ctx context.Context, limit int) ([]PendingDelivery, error)
    InFlightCount(ctx context.Context, triggerID string) (int, error)

    BumpConsecutiveFailures(ctx context.Context, triggerID string) (int, error)
    ResetConsecutiveFailures(ctx context.Context, triggerID string) error
    AutoPause(ctx context.Context, triggerID string) error

    DeriveDeliveryStatus(ctx context.Context, deliveryID string) error
    PurgePayloads(ctx context.Context, before time.Time) (int64, error)
}
```

### Fire authorization

```go
// handleFireTrigger, before the service is called.
scope := "trigger:" + triggerID + ":fire"
if err := c.eval.Require(ctx, scope); err != nil {
    api.ErrorResponse(ctx, w, c.logger, http.StatusForbidden, "forbidden",
        "not authorised to fire this trigger")
    return
}
```

Granting it uses the existing grant path with no new vocabulary shape:

```json
POST /api/v1/policy/grants
{ "principal_type": "service", "principal_id": "<id>",
  "scopes": ["trigger:<trigger-id>:fire"] }
```

A grant on `trigger` covers every trigger by the normal prefix rule. There is no `trigger:*:fire` literal and none should be introduced.

---

## Consequences

### Positive

- The fire endpoint stops being an unenforced gap that the documentation claimed was closed.
- A `202` becomes a real promise: waiting work survives a restart, so the platform does not acknowledge fires it will silently lose.
- `orbit-triggers` can scale independently of `identity`, and Decision 2 establishes it is already safe to do so without leader election.
- The docs stop describing a binary that does not exist, because it now exists.
- Internet-facing ingestion no longer shares a process with the token authority, so a webhook flood cannot degrade OAuth, the device grant or JWKS.
- Three background loops move out of a latency-sensitive request-serving auth API.
- An acknowledged fire always starts: every fire is durably queued before dispatch, so a restart cannot lose one silently.
- A wrong signing secret becomes diagnosable in the console, without an unauthenticated request ever creating a row. `trigger_deliveries` stays exclusively a record of arrivals that authenticated.
- Adding any of the six planned kinds is an adapter plus a front door; nothing downstream changes.

### Negative

- A new table and a worker loop on the path of every fire, plus a new binary, deployment and mesh entry to operate. This is materially more moving parts than today, and the operator image must be rebuilt for the new chart.
- Fire authorization lives in a handler rather than on a route, which is easier to forget on a future endpoint. A test guards it; the durable fix is Decision 6B.
- Full payload storage grows the delivery table substantially and creates a retention obligation that must actually run. A purge job that silently stops is a compliance problem, so it needs monitoring rather than trust.
- Two status vocabularies must stay coherent. The derived-summary invariant is the mitigation and is the thing most likely to be got wrong under time pressure.
- Every replica scans all scheduled triggers each minute. Cheap now, and a known thing to revisit.

### Neutral

- `payload_excerpt` remains until the console reads `payload` everywhere, then is dropped in a later migration. Two representations exist briefly.
- Overflow defaults differ per kind, so two triggers showing identical settings may have arrived there differently. The drawer shows the effective value, not the default.

---

## Implementation

### Phase 0 — Extract `orbit-triggers`

Lands first, as its own PR, before any feature work. No behaviour changes.

1. `cmd/orbit-triggers/` with `main.go` and `config/`, wrapping `internal/triggers/` unchanged.
2. `git mv backend/cmd/identity/migrations/*trigger*.sql backend/cmd/orbit-triggers/migrations/`. Filenames and timestamps preserved, so the pooled collection and `goose_db_version` see no change.
3. Internal mint endpoint on `identity`; typed client in `internal/platform/clients/identity/` satisfying `TokenMinter`. The interface already exists and is injected from main, so nothing in `internal/triggers/` changes.
4. Chart: deployment, service, client-mTLS DestinationRule, app port in the mesh netpol, Chart.yaml version bump, operator image rebuild.
5. api VirtualService: repoint `/api/v1/webhooks/` to `orbit-triggers`. **The external URL is unchanged**; no source is re-pointed and no secret is re-issued.
6. Repoint in-mesh callers of the management API: the frontend BFF gains a triggers base URL alongside `ORBIT_API_URL`, and the MCP trigger tools follow.
7. Update the binary tables in `AGENTS.md` and `docs/code-organization.md`, plus the in-cluster port map. `docs-check` validates the table against `backend/cmd/` in both directions and will fail the build without it.
8. Verify before merging: an existing webhook source fires end to end against its unchanged URL, `make auth` still works, and the scheduler fires from the new binary.

### Phase 1 — Outcome model and pipeline invariants

1. Migration: widen both status constraints; add `reason`, `payload`, `payload_bytes`, `payload_purged`, `redelivery_of` to deliveries; add `target_kind` to triggers (Decision 8); add the rejection counters to `webhook_sources`.
2. Move delivery-status assignment out of the fan-out loop. `dispatch()` records per-trigger outcomes only; the delivery summary is derived once the fan-out completes (`DeriveDeliveryStatus`).
3. Record failed authentication on the source, not as a delivery (Decision 4A): `RecordSourceRejection` on signature failure, missing delivery id, and disabled source. An unknown source id touches nothing.
4. Record `duplicate` rather than logging and returning; link `redelivery_of`.
5. Record `filtered` only for triggers excluded by `match_condition`.
6. Promote `InsertScheduledDelivery` onto `Repository`; delete the `*postgresRepository` type assertion in `TickScheduled`.
7. Store full payloads; keep writing `payload_excerpt` until Phase 4.

### Phase 2 — The two new kinds, completed

The kinds exist; this phase brings them to the PRD's bar.

1. Fire authorization per Decision 6A, with the `triggers:write`-only refusal test. Remove the `KNOWN GAP` comments.
2. Manual input declaration (PD2): typed fields on `kind_config`, validated server-side; rejected input recorded as a delivery with per-field errors.
3. Scheduler backfill: `DueScheduledMinutesWithoutDelivery`, bounded to one hour; insert `missed`, or fire when `catch_up`.
4. Next-fire-time preview endpoint for the creation wizard.
5. Reject sub-minute cron expressions at creation with a clear message.
6. Demote the losing-insert log to `debug`.

### Phase 3 — Admission control and delivery safety

1. Migration: `trigger_pending_deliveries`, `trigger_retention_settings`, and the trigger columns.
2. `Admission` implementation; the `SKIP LOCKED` worker loop with `LISTEN`/`NOTIFY` wake-up. Every fire routes through the queue.
3. Per-trigger `timeout_seconds` replacing the package constant; a timeout counts toward auto-pause.
4. Auto-pause: denials count, admission rejections do not (PRD, US-006); `Drain` on pause, disable, and delete.
5. Payload purge job honouring `trigger_retention_settings`, with a rows-purged metric and an alert when it has not run.
6. `EventTypes()` on the adapters; catalogue endpoint returning declared ∪ observed.

### Phase 4 — Console

Per the PRD's UI section. Drop `payload_excerpt` once nothing reads it.

### Migration path

The extraction (Phase 0) carries no schema change and no URL change: migration files move between binaries with their filenames intact, and the webhook path is repointed at the edge. Existing sources keep posting to the same address with the same secrets.

Every subsequent migration is an additive `ALTER TABLE` with defaults, so it applies to the existing database without downtime and without touching existing rows. Existing webhook triggers become `max_parallel = 4, overflow = 'wait'`, which is the behaviour they have today since nothing has ever been at a ceiling. Existing deliveries keep their current status values, which remain valid members of the widened vocabulary. No re-pointing, no re-secreting, no reset.

---

## Validation

### Success Metrics

- A principal holding `triggers:write` and no trigger grant receives `403` from the fire endpoint.
- Killing the pod with fires waiting loses none of them; they dispatch on restart.
- A trigger at `max_parallel = 1, overflow = 'replace'` receiving three overlapping fires produces one further run and two `superseded` rows.
- Ten consecutive denials auto-pause a trigger; ten `dropped` rows do not.
- A delivery matching three triggers, where one dispatches, one is denied and one is dropped, records all three per-trigger outcomes and a delivery summary of `dispatched`.
- Two `orbit-triggers` replicas produce exactly one run per scheduled tick.
- After extraction, an existing webhook source fires end to end against its unchanged URL, with no reconfiguration on the sender's side.
- Killing the pod between acknowledgement and dispatch loses no fire: it starts when the pod returns.
- Payloads past the tenant's retention have `payload IS NULL` with every other column intact.

### Monitoring

- Pending-queue depth and oldest-unclaimed age per trigger. A growing backlog is the earliest signal that a target agent is unhealthy.
- Auto-pause events, with the trigger and the failure reason.
- Purge job last-success timestamp, alerting when stale. A retention promise that silently stops running is worse than not making it.
- Scheduler tick duration and triggers-scanned count, to catch Decision 2A's known scaling limit before it bites.

---

## Future Work

- **Route-declared per-resource scopes (Decision 6B).** The right long-term shape for every per-resource endpoint, not just this one. Its own ADR.
- **Per-trigger payload opt-out.** The first thing to add if a tenant handling PII needs to record that a delivery happened without storing its body (PRD, PD4).
- **Automatic retry with backoff.** Deliberately out of scope; revisit if Redeliver is observed being used as a manual substitute.
- **Agent-level concurrency ceilings.** Accepted gap under PD6. If the agent control plane's rate limiting bounds request rate rather than concurrent runs, this becomes real work rather than a duplicate guardrail.
- **The six planned trigger kinds.** Each is an adapter and a front door.
- **Multi-agent workflow engine.** A separate pillar with its own PRD and ADR. When it lands, `target_kind = 'workflow'` becomes legal (Decision 8) and a trigger can start a workflow exactly as it starts an agent today. Any general deferred-work or step primitive belongs there, designed against that engine's requirements rather than invented here with triggers as the only consumer. A durable execution engine (Decision 5D) is a live option at that point and should be evaluated then, not now.

---

## Related Decisions

- [Agent Triggers and Webhook Ingestion](2026-06-17-agent-triggers-and-webhook-ingestion.md) — v1. Its identity model, webhook verification and dispatch path are preserved unchanged. Its Notes committed to the `orbit-triggers` extraction; Decision 1 executes it.
- [Trigger Deliveries Filter](2026-06-20-trigger-deliveries-filter.md) — the deliveries feed this ADR's widened vocabulary feeds.
- [Run and Session Lifecycle](2026-05-11-run-and-session-lifecycle.md) — the run a trigger starts.
- [Agent Identity and Policy Provisioning](2026-05-11-agent-identity-and-policy-provisioning.md) — the principal and grant provisioning each trigger kind reuses.
- [Platform-wide Scope Enforcement Middleware](2026-06-04-platform-scope-enforcement-middleware.md) — the middleware Decision 6 works around and 6B would extend.
- [Dynamic Route-Scope Registration](2026-04-14-dynamic-route-scope-registration.md) — the `RouteScope` mechanism whose limits Decision 6 documents.
- [JIT Credential Brokering via a Per-Tenant Egress Gateway](2026-06-06-jit-credential-brokering-egress-gateway.md) — unchanged; triggered runs use it as any run does.

---

## References

- [PRD: Extended Trigger Types](../prd/extended-trigger-types.md) — including PD1-PD7, the product decisions this ADR implements.
- `backend/internal/triggers/` — the implementation this ADR extends.
- [robfig/cron](https://github.com/robfig/cron) — the cron library the scheduler uses.
- Design reference: claude.ai/design project `24c98b96-a1bc-4ba2-b744-3e363f9587ca`, `Triggers.dc.html`.

---

**Last Updated:** 2026-07-27
