# Extended Trigger Types and an Operable Delivery Pipeline

<!-- Filename: 2026-06-20-extended-trigger-types.md -->

**Status:** Accepted ("Prompt templating and the field map", the empty `event_type` rule in Decision 3, and Decision 7's declared event types superseded by [Trigger Payload Templating](2026-09-28-trigger-payload-templating.md) 2026-09-29)
**Date:** 2026-07-27
**Deciders:** Justin Wilkin, Platform Engineering
**Technical Story:** [PRD: Extended Trigger Types](../prd/extended-trigger-types.md)

---

## Context

The Triggers v1 ADR ([2026-06-17](2026-06-17-agent-triggers-and-webhook-ingestion.md)) shipped webhook ingestion as the only trigger kind: an inbound signed request from a registered `WebhookSource` starts an agent run under a per-source JIT-minted `SERVICE` principal.

That design holds and nothing here revisits it. Webhook verification stays HMAC-SHA256 over the raw body against a provider header, the ingestion URL is unchanged, and the identity model — a JIT-minted `SERVICE` principal bounded by its own grant — is what the two new kinds adopt rather than replace.

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

### Security and identity

This feature creates agent identity and gates access, so the three questions apply. One of them applies differently here than elsewhere in the platform, and it is worth being exact about why.

**Who is this agent? A trigger's principal is autonomous, not delegated.** kora's usual identity story is delegation: a person hands scope to an agent, the agent is accountable to that person, and it can never access more than the delegator. **Triggers are the case where that model does not apply.** A GitHub push at 3am and a cron tick have no human initiator. There is no delegator, and therefore **no delegator ceiling to intersect with**.

What holds instead is attribution to *configuration* rather than to *initiation*: an admin created the source or trigger and attached a grant, and that grant is the **sole** bound on what the resulting run can do. Nothing clips it from above.

Three consequences follow, and they raise the stakes on the rest of this ADR rather than lowering them:

1. **Creating a trigger creates a standing autonomous capability.** Its grant is not a request for permission that something else will narrow; it is the whole permission. Trigger creation therefore deserves the same scrutiny as granting a long-lived credential, because that is what it is.
2. **A manual trigger's caller does not narrow it either.** The run executes under the trigger's principal, so a caller with narrow permissions firing a broadly-scoped trigger gets the trigger's scope, not their own. This is deliberate — a manual trigger is a published capability with a fixed blast radius — and it is exactly why firing needs its own per-trigger authorization check rather than inheriting from trigger-management permission (Decision 6).
3. **The audit question is "who configured this and what policy authorised it", not "who asked for it".** Delivery history answers the first; the policy decision answers the second. Neither can be replaced by a delegation chain, because there isn't one.

**A delegated agent type therefore cannot be bound to a trigger.** [Delegated Agent Identity](2026-07-23-delegated-agent-identity.md) defines an agent whose authority is the intersection of its own grant and the delegator's, and whose runs are accountable to the person who started them. A trigger has no delegator to intersect with, so binding one would either run a delegated agent with no ceiling or run it under a person who did not ask for it. Both defeat the model. All three create paths refuse a delegated agent type (`createWebhookTrigger`, `createScheduledTrigger`, `createManualTrigger`), which makes "no trigger ever binds a delegated agent" an invariant of the stored data rather than a rule enforced at one entry point. `UpdateTrigger` needs no check of its own because it cannot change a trigger's target agent type; retargeting means creating a new trigger, which goes through the same refusal.

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

**An empty `event_type` subscribes to every event the source emits**; a non-empty one is an exact match. That is what makes the console's "any event" label true, and it is the reason a source can send an event type no adapter models without the delivery going unmatched: a catch-all trigger still fires on it with the adapter's common fields.

**`filtered` is not a failure.** A delivery whose only per-trigger outcomes are `filtered` derives to `skipped`, not `failed`: every trigger that saw it worked exactly as configured and nothing was ever going to run. `DeriveDeliveryStatus` reads `dispatched` first, then `failed` for any of `failed`/`denied`/`dropped`, and falls through to `skipped`. It leaves `duplicate` and `missed` alone, since those describe the arrival rather than the fan-out.

**Duplicate semantics.** A repeat arrival is a delivery row, not silence. The row carries `status = 'duplicate'` and `redelivery_of` pointing at the original, so "the provider sent this five times" is answerable from history. The uniqueness guarantee is unchanged in substance and narrowed in expression: the per-source unique index over `(source_id, provider_delivery_id)` now excludes lineage rows (`redelivery_of IS NOT NULL`), so it guarantees **at most one original** per provider delivery id while leaving repeats and replays representable. The original's outcome is never rewritten by a retry that arrived days later.

Whether a repeat *re-runs* is a separate, per-trigger question, and `dedupe_enabled` is where it is answered. The index cannot express a per-trigger choice; it is the storage-level guarantee. A repeat is fanned out only to the triggers that turned dedupe off, and a trigger with dedupe on records nothing at all: the duplicate delivery row is the acknowledgement, and a per-trigger "did nothing" row on every trigger of a noisy source is noise rather than audit. No payload is captured on a duplicate, because the body is by definition the one already stored on the original.

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

**Cons:** An unauthenticated caller can still cause rows to be created. Bounding the rate makes it survivable rather than correct: mitigating with an index is a weaker position than not writing at all. It also mixes unauthenticated input into a table whose other rows are all verified.

#### 4C: Metrics and logs only

**Pros:** Strictest posture; an unauthenticated request causes no database write at all.

**Cons:** An admin with a wrong signing secret sees silence in the console and must find someone with log access. That is the PRD's headline complaint, unaddressed.

**Chosen: 4A.** It gives the diagnosis 4C loses without the unauthenticated `INSERT` 4B needs. Requests carrying an *unknown* source id are handled by 4C's rule and touch nothing — there is no tenant to attribute them to and no row that legitimately exists to update.

**Consequence for the delivery vocabulary:** `rejected` is therefore **not** a delivery status. The vocabulary in Decision 3 drops it, and `trigger_deliveries` carries only arrivals that authenticated successfully. `duplicate` and `missed` remain, since both describe authenticated or platform-originated arrivals.

**The counter is paired with an accepted counter.** A rejection count on its own says nothing: five rejections on a source taking a million deliveries is noise, and five on a source taking five is a wrong signing secret. The ratio is the diagnostic, so `accepted_count` exists alongside `rejection_count` and both are kept on the source. The reason stored is the most recent one rather than a history, which is the cost 4A accepted above.

**Every rejection answers the same `404`.** The reasons stay distinguishable where they belong, on the source, which only the owning tenant can read. On the wire they collapse into one response: "there is nothing here". A distinct `401` for a bad signature told a prober that the source id they guessed is real and that only the secret is missing, which is the one fact an unauthenticated endpoint should never hand out. An invalid signature, a missing delivery id, a replayed payload, an unknown source and a disabled source are therefore indistinguishable to the sender.

#### Replay defence

Dedup keys on the provider's delivery id, and for GitHub that id travels in an **unsigned** header. A captured, correctly-signed body therefore replays forever: rewrite `X-GitHub-Delivery` on each copy and every one is a fresh, verified arrival that starts a run. Signature verification alone does not close this, because the signature is still genuine.

The fingerprint that cannot be rewritten is the body itself. The signature is a deterministic function of `(secret, body)`, so a hash of the body identifies exactly the repeats a hash of the signature would, without the mechanism needing to know where any given provider puts its signature. `trigger_delivery_replay_keys` stores `(source_id, body_hash)` as its primary key, with the provider delivery id the body **first arrived under** beside it.

That second column is what keeps the defence from breaking honest senders. A provider retrying an unacknowledged delivery re-sends the same body under the *same* id, which is an ordinary duplicate that Decision 3 already handles and which still records as a linked duplicate row. The attack is the same body under a *different* id, and that is the only case refused.

The window is **24 hours**, pruned by the retention job so this stays a window rather than a growing ledger of every body ever received. It runs **behind** signature verification and **fails open**: a replay-key lookup that errors is logged and the delivery is accepted. The check is defence in depth on top of a signature that has already been verified, so a database problem should not take webhook ingestion down with it.

---

### Decision 5 — Admission control and the waiting backlog

Per-trigger concurrency requires knowing how many runs a trigger has in flight, across replicas.

#### 5A: A durable pending-delivery queue, claimed with `FOR UPDATE SKIP LOCKED` (**chosen**)

A `trigger_pending_deliveries` table holds fires that are waiting. A worker loop claims rows with `SELECT ... FOR UPDATE SKIP LOCKED`, checks the in-flight count, and dispatches. In-flight count is a query against `trigger_delivery_runs` for runs started and not yet terminal.

**Pros:** Correct across replicas and restarts, so a `202` is a real promise rather than best-effort. The three overflow behaviours become ordinary row operations: `replace` deletes waiting rows for the trigger before inserting, `wait` trims the oldest past the depth limit, `refuse` skips the insert. Disabling a trigger deletes its waiting rows and records them `dropped`, which the PRD requires and nothing else provides.

**Cons:** A new table and a worker loop.

#### 5B: In-memory counter and channel per trigger

**Pros:** Simplest, lowest latency.

**Cons:** Wrong with more than one replica — each enforces its own ceiling, so a limit of 4 becomes 4N. Everything waiting is lost on restart, making the `202` a lie. This is the failure mode most easily taken by default and least easily noticed.

#### 5C: Redis

Rejected: Redis is available, but a second durability story for data that must agree with the Postgres delivery record is worse than one table.

#### 5D: A durable execution engine (Temporal or similar)

Considered seriously, because kora intends to build a multi-agent workflow engine later (see Decision 8) and a durable execution engine is a plausible foundation for one.

**Pros:** Durability, retries, backoff, timeouts and long-running state machines all come as primitives rather than as code. If a workflow pillar arrives, much of it is already built.

**Cons, in the order they matter:**

- **It does not solve the hard part.** Durability is commodity — `SKIP LOCKED`, River, Temporal and SQS all provide it. What no engine provides is kora's actual differentiator: carrying *which principal this runs as* and *what authorised it* through deferred execution. That layer gets built either way. Adopting an engine means building it **and** operating the engine.
- **Sequencing.** The workload that would justify it does not exist yet. Designing a durable-execution foundation against one consumer — a per-trigger concurrency limit — bakes in that consumer's assumptions. The workflow engine is the consumer with real requirements, and it should be designed against those.
- **Operational cost.** Self-hosted Temporal's weight is not its process count (the four server roles can run as one binary); it is history shard management, retention configuration and datastore growth, on a platform already operating SPIRE, Istio, OPA, Postgres, Redis and Dex.
- **Retry is not currently a benefit.** The PRD excludes automatic retry deliberately: webhook providers already retry, so platform-side retry compounds someone else's backoff. The headline feature is one this feature does not want.

**Rejected for now, revisit when the workflow engine is designed.** That is the decision point, not this one. `Admission` is a concrete type over the repository rather than an interface, so swapping in a durable-execution-backed implementation is a small refactor first: extracting the two methods the pipeline actually calls, `Admit` and `Drain`, into an interface and injecting it. That is a contained change because the pipeline's coupling is already narrow, but it is honest to say the seam has to be cut rather than that it is already there.

**Chosen: 5A, with every fire routed through the queue.**

The tempting optimisation is a fast path: when a trigger is under its ceiling and nothing is waiting, dispatch directly and leave the queue for the contended case. That covers almost every fire, so the queue would rarely be touched. It is rejected, because it means a process dying between recording the delivery and starting the run loses that fire silently — the delivery sits at `verified` forever with no run and nothing to retry it. That is a durable *record* of a start that never happened, which is worse than either a durable start or an honest failure.

Triggers is the platform's front door for starting things (Decision 8), so **an acknowledged fire always starts** is a property worth being able to state without qualification. Routing everything through the queue makes it true, and leaves one dispatch path instead of two — two paths being the same class of coherence risk as two status vocabularies, and worth avoiding for the same reason.

The cost is claim latency on every fire. That is acceptable because the sender has already been acknowledged by then: everything after **RECORD** is asynchronous, so the latency is on no caller's path. The worker polls on a **one second tick**, claiming up to sixteen fires per pass. A poll rather than a `LISTEN`/`NOTIFY` wake-up: the claim query is a cheap indexed lookup that returns nothing when idle, so a second of latency on a path nobody is waiting on does not justify a second delivery mechanism whose failure mode (a missed notification) is silent. The batch bound is what keeps a burst draining steadily rather than arriving at the agent API as one thundering herd. If sub-second start latency ever becomes a requirement, `LISTEN`/`NOTIFY` is the change, and the poll stays underneath it as the backstop.

`Admission` stays deliberately narrow — concurrency and overflow, nothing more. It is not an attempt at a general deferred-work primitive, because that primitive belongs with the workflow engine (Decision 8) and would be under-designed if invented here.

**Timeout.** `dispatchTimeout` is currently a package constant covering the whole fan-out. It becomes per-trigger and covers **starting** the run, not running it. A timeout records the trigger-level outcome as `failed` and counts toward auto-pause.

**Admission bounds.** The three tunables are bounded on the way in, and `max_parallel` is bounded again in the schema so no path can write a value the runtime cannot honour:

| Setting | Range | Default | Enforced by |
|---|---|---|---|
| `max_parallel` | 1 to 16 | 4 | Service validation and a DB `CHECK` |
| `max_queue_depth` | 1 to 500 | 32 | Service validation |
| `timeout_seconds` | 5 to 600 | 30 | Service validation |

The same bounds apply to create and to update, so a trigger cannot be edited into a configuration it could not have been created in. Changes take effect on the next delivery: the worker re-reads the trigger when it claims a fire, so work already in flight is not retimed under it.

**What "in flight" means.** `max_parallel` counts runs that are **genuinely running**, not runs that were started recently. The count is a read-only join from `trigger_delivery_runs` to the `runs` table, excluding runs that have **settled**: `completed`, `failed`, `budget_exhausted`, and `idle`. Counting starts-within-a-horizon instead would make the ceiling drift with run duration: a trigger whose runs finish in ten seconds would be throttled for the rest of the horizon by work that had already finished.

**`idle` counts as settled, and this changes what `max_parallel` means.** `orbit-agents` does not treat `idle` as terminal, and correctly so: an idle run has finished its turn and is waiting for a message that would continue the conversation. A **trigger-started** run has nobody to send that message. There is no user attached to it, so the message never comes and the run sits idle until something reaps it. Treating idle as occupying a slot would let a trigger with `max_parallel = 4` wedge permanently after four fires, each holding a slot against a conversation that will never resume. Idle is therefore terminal *for admission purposes only*, and the divergence from the agent control plane's own view is deliberate rather than an oversight. The consequence to state plainly: `max_parallel` bounds runs that are actively working, not runs that exist.

`runs` is owned by `orbit-agents`, so this is a **deliberate cross-service read**, and it is worth naming as a coupling rather than leaving it to be discovered. It is safe here for the reason it would not be in general: the platform runs one Postgres with pooled migrations, and the read is read-only and additive, so it can never be the cause of a write conflict. If the two services are ever split across databases, this join is the thing that breaks, and the replacement is a count served by `orbit-agents` over its typed client.

Two bounds sit on top of that count:

- **The hour horizon stays**, no longer as the definition of in-flight but as a backstop for a run whose terminal state was never recorded at all: a lost pod, a dropped callback. Without it such a run holds a slot forever and silently throttles a healthy trigger to nothing. Over-admitting after a lost outcome is recoverable and visible; wedging is neither.
- **Claimed-but-undispatched fires count too.** A worker that has claimed a row but not yet recorded its outcome is invisible to the fan-out, so omitting it would let a burst admit past the ceiling in the window between claim and dispatch. That half is bounded by the reclaim window below, so one dead pod cannot permanently cost a trigger a slot.

**One batch cannot blow the ceiling.** `ClaimPending` ranks waiting rows per trigger (`row_number()` over a partition by `trigger_id`, oldest first) and claims only those where `in_flight + position <= max_parallel`. Without the ranking every waiting row of an under-ceiling trigger satisfies the same capacity check simultaneously, and a burst claims all of them in one pass: a ceiling of 4 with 30 waiting fires admits 30. The ranking is what makes a single batch admit at most the remaining capacity.

**The stuck-claim reaper.** A worker claims a fire and then dispatches it. If the pod dies in between, the row stays claimed forever: nothing re-claims it, the fire never starts, and it costs its trigger a parallel slot permanently. That falsifies the one promise the queue exists to keep. Each drain pass therefore reclaims first: a claim older than **fifteen minutes** has its `claimed_at` cleared and returns to the queue, and each reclaim is logged individually at warning level, because a pod dying while holding acknowledged work is worth knowing about even though the queue recovers by itself. Fifteen minutes is chosen against the ceiling on a legitimate dispatch: `timeout_seconds` is capped at 600, so a claim older than the window cannot belong to a dispatch still in progress. The margin is deliberately generous, because reclaiming a live dispatch would start the same fire twice, which is worse than a stranded fire starting late.

**The claim is re-stamped immediately before each dispatch**, and that is what makes the window sound for a batch. A batch is claimed in **one** statement but dispatched one row at a time, each bounded by its own trigger's timeout. Without re-stamping, the last row of a full batch carries a `claimed_at` set before the first row's dispatch even began, which can already be far older than fifteen minutes by the time its turn arrives. Another replica's reaper would then take it back and dispatch it a second time, mid-flight, which is the exact double-start the generous margin was chosen to avoid. Re-stamping makes the window mean what its name says: how long *this* dispatch has been running. With the per-dispatch bound at 600 seconds, a live dispatch can never age past the window.

The reaper and the orphan sweep both scan the whole queue table and neither is urgent, so they run on their own slower cadence rather than on every tick, ahead of the claim so anything they recover is picked up in the same pass.

**`refuse` counts the fires already waiting, not only those in flight.** In-flight alone sees only work a worker has picked up, and the worker polls on an interval, so two fires a fraction of a second apart both observed an empty in-flight count and both were accepted. That is silent queueing under the one overflow behaviour whose entire promise is that it does not queue. The check is therefore `in_flight + unclaimed_waiting >= max_parallel`. A person told "not now" can retry; a person told "accepted" and then made to wait cannot tell that apart from a stall.

**Refusal is a `429`, not a `500`.** A manual fire refused at the ceiling returns `429 Too Many Requests`: the trigger is healthy and busy, and the caller's correct response is to retry shortly. The fire is recorded either way, and the error distinguishes "at capacity" from "auto-paused", because the fixes differ.

**Deletion drains first.** `DeleteTrigger` calls `Drain` before removing the trigger, so waiting fires are recorded as `dropped` rather than disappearing with the row. `trigger_pending_deliveries` is deliberately **foreign-key-free** for the same reason the delivery history is: a cascade would delete the evidence at the moment it becomes interesting. The queue's referential integrity is maintained by the drain paths (disable, auto-pause, delete) rather than by the database, and the worker re-reads the trigger on claim, so a row that outlives its trigger is recorded as dropped instead of dispatching against nothing.

**An orphan sweep is the backstop under those drain paths.** They each drain their own backlog, but a fire admitted in the instant between a disable committing and its drain running belongs to no path at all: claiming requires an enabled trigger, so such a row is never claimed, and the stale-claim reaper only touches rows a worker already holds. Without a sweep it waits forever, invisible, holding nothing and explaining nothing. `DrainOrphanedPending` therefore collects waiting fires whose trigger has been deleted or disabled and records each as `dropped`, so the work is visibly accounted for rather than silently stranded.

**Auto-paused triggers stay matchable.** The matching queries select `enabled = true OR auto_paused_at IS NOT NULL`. An auto-paused trigger is carried through matching precisely so admission can record `paused` against the fire; filtering it out at the query would make it indistinguishable from a deleted trigger in the delivery history, which is the failure the outcome exists to prevent. A trigger a person disabled *is* excluded, because that silence was asked for. This is what makes `enabled` and `auto_paused_at` two settings rather than one: manually disabling a trigger clears `auto_paused_at`, so an explicit "off" stops presenting as the platform's decision, and re-enabling clears both the marker and the failure counter.

---

### Decision 6 — Fire authorization (closing the KNOWN GAP)

The fire endpoint requires `triggers:write` today, so trigger-management permission implies permission to fire anything. `ScopeEnforcementMiddleware` cannot express a per-trigger scope: `RouteScope` matches static prefixes.

#### 6A: Explicit in-handler policy evaluation against a qualified scope (**chosen**)

The route keeps its static registration for authentication; the handler evaluates a per-trigger scope through the policy evaluator it already holds, before the service is called.

The scope literal follows the platform's qualified-scope convention (`type:path...:action`, hierarchy by prefix `covers`, unbounded dimension deepest):

```
trigger:<trigger-id>:fire
```

The obvious alternative form, `triggers:fire:<trigger-id>`, is wrong: it puts the action last-but-one, and the coverage rule requires the action to be the final segment. Placing the identifier in the path means a covering scope is formed by dropping path segments while keeping the action, so **`trigger:fire` covers every `trigger:<id>:fire`** — which is what makes the per-trigger scope grantable at all, since a caller may only grant scopes they themselves hold. No `*` literal is introduced. The built-in admin roles therefore carry `trigger:fire`.

**Pros:** Correct hierarchy, default-deny, fails closed. Uses the evaluator already wired into the controller. No middleware change.

**Cons:** Authorization lives in a handler rather than declared on a route, which is easier to forget on a future endpoint. Mitigated by a test asserting that a principal with `triggers:write` and no trigger grant is refused.

#### 6B: Extend `ScopeEnforcementMiddleware` to substitute route params

**Pros:** Declarative, and benefits every future per-resource endpoint.

**Cons:** A change to the enforcement path used by every protected route in the platform, made to fix one endpoint. Worth doing, but as its own ADR with its own review, not as a side effect of a triggers feature.

**Chosen: 6A**, with 6B recorded as the better long-term shape in [Future Work](#future-work).

**The caller's identity does not become the run's identity.** The run continues to execute under the trigger's own `SERVICE` principal; the caller is recorded in `fired_by`. This is deliberate: a manual trigger is a published capability with a fixed blast radius, not a way to run something as yourself.

#### What a trigger's principal may start

Fire authorization decides who may *ask*. This decides what the resulting principal can reach, and it is the larger of the two surfaces, because a trigger principal is autonomous and nothing narrows it at request time.

A source or trigger principal is provisioned with `agents:types:read` and nothing else. The per-agent pair `agents:runs:<slug>:read` and `agents:runs:<slug>:write` is added only once a binding to that agent exists. The broad `agents:runs:read`/`agents:runs:write` pair is never granted: scope hierarchy is by prefix, so `agents:runs:write` covers every `agents:runs:<slug>:write`, and holding it would make the per-agent narrowing decorative. A leaked source token would start any agent in the tenant.

That narrowing only holds if the route does not undo it. **`POST /api/v1/runs` is therefore authenticated-only at the route layer**, registered with an empty required scope, which the policy service records as authenticated-only. The scope that governs starting a run is per-agent, and the slug arrives in the request body, where a static path-prefix route map cannot see it. A route-level `agents:runs:write` would not narrow the endpoint; it would **widen** it, because the parent prefix-covers every child, so every principal permitted to start any run would need the scope that permits starting all of them. That is precisely what let a source token start any agent in its tenant. The authoritative check is `orbit-agents`' own per-slug `Authz.checkRun`, run in `RunService.authorizeStart` before the first write, so a refused start leaves nothing behind. It answers `403`, not `500`: a refusal is the caller's answer rather than a server fault, and the propagation retry below can only recognise a refusal that arrives as `403`. [orbit-agents Route and Scope Registration](2026-05-11-orbit-agents-route-scope-registration.md) is corrected accordingly.

#### Who may reach `orbit-triggers` at all

Scope enforcement treats a `SYSTEM` principal as unrestricted, which is the platform-wide convention. `orbit-triggers` holds every webhook source's signing secret and can start a run under any trigger's principal, so under that convention a token minted by any other in-mesh component reaches every triggers route. Mesh-wide reachability is a far larger surface than the callers this service actually has.

An Istio `AuthorizationPolicy` restricts it to them. It is an **ALLOW**, not a DENY: a DENY listing the callers to exclude is a list that must be kept complete forever, whereas an ALLOW listing the callers that exist fails closed when something new appears, which is the direction this service should fail in. The check runs in the sidecar, before the application sees the request.

#### Bounds on what a caller can write

Every write path is bounded on the way in, so a request cannot be turned into storage of the caller's choosing: a trigger name is capped at 128 characters, a prompt template at 8 KiB, and management request bodies at 1 MiB. The webhook ingest body has its own, larger ceiling of 5 MiB, since its size is the sender's choice rather than an operator's.

---

### Decision 7 — Payload storage and the event catalogue

**Payload.** `payload_excerpt` is superseded by a full `payload` column plus `payload_bytes`, with a size ceiling: oversized payloads are truncated and flagged, never rejected, because a trigger should still fire. A purge job nulls `payload` past the tenant's retention (default 30 days, tenant-shortenable per PD4) and leaves every other column, so delivery metadata is kept indefinitely as the PRD requires.

**Event catalogue (PD7).** Adapters already know their provider's event types, so `SourceAdapter` gains `EventTypes() []string`. Because a compiled list goes stale between releases — the liability the PRD names — the list offered in the console is the **union of the adapter's declared types and the event types actually observed for that source**, which delivery history already records. A provider's new event type becomes selectable as soon as one arrives, without a release.

The declared set is pinned to what `Fields` models, not to what the provider can emit. GitHub emits dozens of event types; the adapter declares `issues`, `pull_request` and `push`, because those are the ones it extracts provider-specific fields for. Declaring the rest would promise a richness that is not there: a trigger on one would see only the common fields. `GET /trigger-sources/{id}/event-types` returns the two sets separately (`declared` and `observed`) rather than a merged list, so an event that arrives without being declared reads as what it is, the signal that the adapter should grow a case for it, and the catalogue never becomes a restriction on what the platform accepts.

---

### Decision 8 — A trigger targets *something the platform can start*, not an agent

kora intends to build a multi-agent workflow engine as a separate pillar. Triggers are not that engine and must not grow into it. Triggers are the **front door**: the set of ways something can *start* in the platform. What gets started is a separate concern, and today there is exactly one answer.

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

Most decisions above take the same form: **the structure needed already exists, so extend it rather than add alongside it.** The fan-out table already is the two-level outcome model. The unique index already is the scheduler's concurrency control. The adapter already knows its event types. The three genuinely new mechanisms — the pending queue, the source rejection counter, and the fire scope check — are each the smallest thing that makes a specific PRD requirement true, and each replaces something currently either absent or incorrect.

Two decisions cost more than the minimum, deliberately.

The **pending-delivery queue** could have been an in-memory counter, which would be far less code and is wrong: it enforces the wrong limit under multiple replicas and loses waiting work on restart, making the acknowledgement the platform sends a lie. The durability of an acknowledged fire is a property worth paying for explicitly.

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

-- max_parallel is bounded in the schema as well as in service validation: a
-- ceiling the runtime cannot honour should not be storable by any path.
-- max_queue_depth (1..500) and timeout_seconds (5..600) are bounded in the
-- service only, since neither has a value that would corrupt the queue.
ALTER TABLE triggers
    ADD CONSTRAINT triggers_max_parallel_check
    CHECK (max_parallel BETWEEN 1 AND 16);

-- Per-kind defaults are applied at creation, not by the column default:
-- webhook -> 'wait', scheduled -> 'replace', manual -> 'refuse'.

-- Delivery: full payload and the widened outcome vocabulary.
ALTER TABLE trigger_deliveries
    ADD COLUMN payload        BYTEA,
    ADD COLUMN payload_bytes  INT,
    ADD COLUMN payload_purged BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN reason         TEXT,
    ADD COLUMN redelivery_of  TEXT;

ALTER TABLE trigger_deliveries DROP CONSTRAINT IF EXISTS trigger_deliveries_status_check;
ALTER TABLE trigger_deliveries
    ADD CONSTRAINT trigger_deliveries_status_check
    CHECK (status IN ('verified','dispatched','skipped','failed',
                      'duplicate','missed'));

-- Duplicates and replays both carry the original's provider delivery id, so the
-- dedup index has to admit them while still guaranteeing one ORIGINAL per
-- (source, provider id). Lineage rows name what they descend from and are
-- excluded rather than blocked.
DROP INDEX IF EXISTS idx_trigger_deliveries_dedup;
CREATE UNIQUE INDEX idx_trigger_deliveries_dedup
    ON trigger_deliveries (source_id, provider_delivery_id)
    WHERE provider_delivery_id <> '' AND redelivery_of IS NULL;

CREATE INDEX idx_trigger_deliveries_redelivery_of
    ON trigger_deliveries (redelivery_of)
    WHERE redelivery_of IS NOT NULL;

-- Failed authentication never creates a row (Decision 4A). It updates the
-- source the tenant already owns, so an unauthenticated request cannot grow
-- any table. Only the most recent reason is kept.
ALTER TABLE webhook_sources
    ADD COLUMN rejection_count       BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN last_rejection_at     TIMESTAMPTZ,
    ADD COLUMN last_rejection_reason TEXT;

-- Trigger names captured onto history, so a deleted trigger's deliveries stay
-- readable (US-004). Neither delivery table carries a foreign key to triggers,
-- deliberately: history has to survive the trigger. The cost was that a deleted
-- trigger left rows identified by an id alone, unreadable at exactly the moment
-- someone was asking what had happened.
--
-- Captured at write time rather than joined at read time. A join returns NULL
-- for a deleted trigger, which is the case that matters, and a later rename
-- would silently rewrite history. This column is the name AS IT WAS.
ALTER TABLE trigger_deliveries    ADD COLUMN trigger_name TEXT;
ALTER TABLE trigger_delivery_runs ADD COLUMN trigger_name TEXT;

-- Per-trigger outcome: the level admission control and auto-pause act on.
ALTER TABLE trigger_delivery_runs DROP CONSTRAINT IF EXISTS trigger_delivery_runs_status_check;
ALTER TABLE trigger_delivery_runs
    ADD CONSTRAINT trigger_delivery_runs_status_check
    CHECK (status IN ('dispatched','failed',
                      'filtered','denied','superseded','dropped','paused'));

-- The waiting backlog. Rows are transient; they do not replace delivery history.
--
-- Deliberately foreign-key-free. A cascade from triggers or trigger_deliveries
-- would delete waiting fires at exactly the moment they become interesting:
-- disabling or deleting a trigger has to record what that cost, not erase it.
-- Drain does that explicitly on disable, auto-pause and delete, and the worker
-- re-reads the trigger when it claims a row, so a row that outlives its trigger
-- is recorded as dropped rather than dispatching against nothing.
--
-- claimed_at is nullable rather than a boolean so a stuck claim is visible and
-- can be reaped by age.
CREATE TABLE trigger_pending_deliveries (
    id          TEXT        PRIMARY KEY,
    tenant_id   TEXT        NOT NULL,
    trigger_id  TEXT        NOT NULL,
    delivery_id TEXT        NOT NULL,
    fields      JSONB       NOT NULL DEFAULT '{}'::jsonb,
    enqueued_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    claimed_at  TIMESTAMPTZ
);

CREATE INDEX idx_pending_claim
    ON trigger_pending_deliveries (trigger_id, enqueued_at)
    WHERE claimed_at IS NULL;

CREATE INDEX idx_pending_delivery ON trigger_pending_deliveries (delivery_id);

-- Tenant payload retention (PD4). An absent row means the 30-day default, and
-- row existence is what distinguishes "we chose 30" from "we chose nothing".
CREATE TABLE trigger_retention_settings (
    tenant_id    TEXT     PRIMARY KEY,
    payload_days SMALLINT NOT NULL DEFAULT 30
                 CHECK (payload_days BETWEEN 1 AND 30),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- The purge job scans by age; without this it is a sequential scan over every
-- delivery ever recorded.
CREATE INDEX idx_trigger_deliveries_purge
    ON trigger_deliveries (received_at)
    WHERE payload IS NOT NULL;
```

### Contracts

```go
// Admission decides whether a matched trigger may start a run now, and durably
// holds the fires that cannot. A concrete type over the Repository, not an
// interface: there is one implementation, and inventing a seam for a second
// that does not exist would be the speculative generality Decision 5D declines.
// Correctness across replicas comes from the queue's SQL, not from the type.
type Admission struct { /* repo, logger */ }

// Admit records a fire for later dispatch, applying the trigger's overflow
// behavior when it is already at its ceiling. There is no "dispatch now"
// result: every fire is enqueued, including the uncontended case, which is
// what makes an acknowledged fire survive a restart.
func (a *Admission) Admit(ctx context.Context, t Trigger, deliveryID string, fields map[string]string) (AdmissionResult, error)

// Drain removes everything waiting for a trigger, recording each as dropped
// under the given reason. Called when a trigger is disabled, auto-paused, or
// deleted. It takes the Trigger rather than an id because it writes the
// trigger's name onto each outcome row.
func (a *Admission) Drain(ctx context.Context, t Trigger, reason string) (int, error)

// There is no Release. A slot is freed by the run reaching a terminal state,
// which admission reads through the runs join rather than being told about;
// the claimed pending row is cleared by the worker (ReleasePending) or by the
// reaper. Nothing has to remember to hand a slot back.

type AdmissionResult int

const (
    AdmitEnqueued   AdmissionResult = iota // durably waiting; the normal outcome
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

    EnqueuePending(ctx context.Context, p *PendingDelivery) error
    // Claims oldest-first, per-trigger-ranked so one batch admits at most the
    // remaining capacity.
    ClaimPending(ctx context.Context, limit int) ([]PendingDelivery, error)
    ReleasePending(ctx context.Context, id string) error
    // Un-claims fires whose worker died mid-dispatch, returning the ids taken
    // back so the caller can log each one.
    ReclaimStalePending(ctx context.Context, olderThan time.Duration) ([]string, error)
    InFlightCount(ctx context.Context, triggerID string) (int, error)
    SupersedePending(ctx context.Context, triggerID string) ([]PendingDelivery, error)
    TrimPending(ctx context.Context, triggerID string, depth int) ([]PendingDelivery, error)
    DrainPending(ctx context.Context, triggerID string) ([]PendingDelivery, error)

    BumpConsecutiveFailures(ctx context.Context, triggerID string) (int, error)
    ResetConsecutiveFailures(ctx context.Context, triggerID string) error
    AutoPause(ctx context.Context, triggerID string) error

    DeriveDeliveryStatus(ctx context.Context, deliveryID string) error
    // Takes the platform default rather than a cutoff time: the cutoff is
    // per tenant, so it is resolved inside the query against each tenant's
    // configured window, falling back to the default where none is set.
    PurgePayloads(ctx context.Context, defaultDays int) (int64, error)
    GetRetention(ctx context.Context, tenantID string) (days int, configured bool, err error)
    SetRetention(ctx context.Context, tenantID string, days int) error

    ObservedEventTypes(ctx context.Context, sourceID string) ([]string, error)
    PendingStats(ctx context.Context) (PendingStats, error)
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

`trigger:fire` covers every trigger by the coverage rule (same resource type, same action, shorter path). There is no `trigger:*:fire` literal and none should be introduced.

### HTTP surface

Everything is under `/api/v1`. Ingestion is the only unauthenticated route; the rest carry the platform's standard auth middleware, and the fire endpoint adds the per-trigger check above.

| Route | Purpose |
|---|---|
| `POST /webhooks/{sourceID}` | Inbound webhook. Unauthenticated by design; verified by signature. Path unchanged from v1 |
| `POST`/`GET`/`PATCH`/`DELETE /trigger-sources[/{id}]` | Source registration and management |
| `GET /trigger-sources/{id}/event-types` | Event catalogue: `declared` from the adapter, `observed` from this source's history (Decision 7) |
| `POST`/`GET`/`PATCH`/`DELETE /triggers[/{id}]` | Trigger management |
| `POST /triggers/{id}/fire` | Fire a manual trigger. Authorized against `trigger:{id}:fire` (Decision 6) |
| `POST /triggers/schedule-preview` | Next fire times for a cron expression and timezone, before the trigger is saved |
| `GET /trigger-deliveries` | The cross-trigger deliveries feed |
| `POST /trigger-deliveries/{id}/redeliver` | Replay a past delivery as a new one |
| `GET`/`PUT /trigger-retention` | The tenant's payload retention window |

A trigger read carries `pending_count`: how many fires are waiting on it right now, counted from the queue rather than stored. It is the number that makes a backlog visible before it becomes a surprise, and computing it per read is what keeps it from disagreeing with the queue it describes.

### Manual input validation

A manual fire is validated against the trigger's declared inputs before anything is admitted, and **the rejection is recorded as a delivery**: status `failed`, with a summary of the per-field problems as its reason and the submitted values stored as its payload. The values and the complaint are the pair that makes a rejection diagnosable.

Recording it matters for the same reason recording a policy denial does. An invalid fire that vanished was indistinguishable from one that was never made, which is precisely what leaves "why did nothing happen?" unanswerable. The rejected fire is recorded and never admitted: it must not reach the queue, or it would start the run the refusal was about. The caller gets the per-field detail, which is the part they can act on, and the delivery's `reason` carries a summary of the same per-field problems so the record explains itself without the caller's response beside it.

**The captured variables are bounded**, and the bound matters more here than anywhere else payloads are stored, because a manual fire is recorded **before** its inputs are judged. Without a ceiling, a caller could write a row of their choosing on a request the platform is in the act of refusing. The capture obeys the same 1 MiB payload ceiling as every other stored body. An oversized capture is **dropped rather than truncated**: truncated JSON cannot be read back, so storing it would trade a large row for an unparseable one. Capture is otherwise best-effort, since a fire must not be refused because its own audit record failed to encode.

A trigger a person disabled is refused before any of this, with nothing recorded: they asked for silence. An **auto-paused** trigger is carried through to admission instead, which records `paused` against the fire, because otherwise an auto-paused trigger and a deleted one look identical in the history.

### Run attribution

The forward link always existed: a delivery records the run it started. The reverse did not, so a run opened from the runs list could not say a trigger started it at all. Its `created_by` is the trigger's `SERVICE` principal, which reads as an opaque id.

A run therefore carries `started_by`: `{trigger_id, trigger_name, delivery_id}`, three typed nullable columns on `runs` rather than a metadata bag, following the precedent `summary` and `delegation_jti` already set. Absent means a person started the run, which is what its absence means everywhere downstream.

**The attribution is authorized, not trusted.** A person who could label their own run as trigger-started could manufacture evidence that an automated, policy-governed path did something they did themselves, so the check fails closed and mirrors the delegation model's `requireDelegatorPrincipal`. Attribution that names a delivery without naming a trigger is refused outright: a dangling delivery id renders as nothing and would quietly become a run claiming automation without saying what automated it.

`SERVICE` alone is not sufficient, because every in-mesh workload is a `SERVICE` principal and any of them could otherwise stamp a run as automation a person can no longer tell from the real thing. The caller's principal id is **bound to the claim**, against the prefixes the triggers feature mints its principals under:

- A principal named `trigger:<id>` may attribute a run to trigger `<id>` **and to no other**. The principal id *is* the trigger id, so the claim carries its own proof and needs no lookup.
- A principal named `trigger-source:<id>` is accepted without naming which trigger it is, because a source fans one delivery out to the several triggers hanging off it and that binding is knowable only in the triggers database. Tenancy still bounds it: `StartRun` derives `tenant_id` from the token and never from the body, so a source can only ever name a trigger inside its own tenant.
- Every other principal, including a person, a PAT, the session-manager and any other in-mesh workload, is refused.

**The residual, stated rather than hidden:** a source principal can name a *sibling* trigger id within its own tenant. Closing it would require `orbit-agents` to read the triggers database to resolve the source-to-trigger binding, which the feature boundary deliberately forbids. The durable fix is to stamp the trigger id into the minted token's claims so the binding travels with the credential and needs no lookup at all; it is recorded in [Future Work](#future-work).

`trigger_name` is a snapshot taken at start time and there is no foreign key, for the same reason the delivery tables have neither: a run is audit history and outlives the trigger that started it. A deleted trigger still reads as something a person recognises, and a later rename does not rewrite what this run says happened.

### Replay

`POST /api/v1/trigger-deliveries/{id}/redeliver` re-runs a past delivery. It is an admin action, never automatic, and it is the only recovery path this ADR provides.

A replay is a **new delivery linked by `redelivery_of`**, not an edit of the original. The original's outcome is the record of what happened the first time, and an operator re-running an event after fixing its cause must not overwrite the evidence of the failure they fixed.

The replay is matched and validated against configuration **as it is now**, not as it was. A webhook replay fans out against the source's current triggers; a manual replay is revalidated against the trigger's current input declaration, so a declaration that has changed since the original fire is the contract that applies. Authorization is likewise re-evaluated, which is what makes a revoked grant stop a replay.

**No signature is re-verified.** The body was verified when it first arrived and has been under the platform's control since. Re-verifying would fail for any source whose secret has been rotated, which is exactly the kind of fix that precedes a replay. Verification is a property of arrival, not of storage.

That concession is what makes replay the one path that re-runs a stored payload without re-authenticating it, so it carries two bounds of its own.

**A payload predating the source's current secret is refused.** `webhook_sources.secret_rotated_at` dates the secret currently on the source, moving only when the stored ciphertext actually changes, and a replay of a delivery received before that timestamp is rejected. Rotating a secret is what an operator does when the old one may be in someone else's hands, and everything that arrived under it is exactly what they are trying to disown. Existing rows inherit their creation time, which is when their secret was in fact issued.

**Ten replays per original.** Replay is a deliberate admin action, not a retry loop, so a bound this generous never obstructs legitimate use while still stopping one stored payload from becoming an unbounded run generator, whether through a mistake or through a compromised admin credential.

Four further refusals, each with its own message:

- A **scheduled tick** cannot be replayed. It is defined by the minute it was due, so a replay would either collide with that minute's row or invent a fire that was never due. Neither is something an operator can reason about; firing the trigger manually is the answer.
- A **purged payload** has nothing to replay. Retention has run and the body is gone.
- A **missing trigger or source**, or a disabled source, has nowhere to replay to.
- A **manually disabled trigger** is refused. That is a person's decision to respect, not to work around with a replay. An **auto-paused** trigger is carried through, because replaying after fixing the cause is the common case and admission records `paused` if it has not been fixed.

The response is `202 Accepted` with the **new** delivery id whenever the replay was recorded, including when admission then refuses it. Recording is the guarantee; whether a run started is the new delivery's own fan-out, exactly as for a fresh arrival.

### Payload retention

`GET` and `PUT /api/v1/trigger-retention` read and set the tenant's window. There is no id in the path: the tenant comes from the caller's claims, and a tenant can only read or change its own.

The window is 1 to 30 days. It can be shortened relative to the platform default and never extended, which is the asymmetry that matters: retention is a promise about third-party payload data the platform is holding, so a tenant extending it would raise the platform's exposure rather than only their own.

`GET` returns `is_default` on a tenant that has never chosen. Row existence is the whole mechanism: it distinguishes "we picked 30" from "we picked nothing", which is what decides whether a later change to the platform default moves this tenant.

### Prompt templating and the field map

A trigger's prompt template supports restricted `{{ dotted.field }}` substitution over the adapter's allow-listed field map, and nothing more. No expressions, no arbitrary payload access. An unknown key renders as an empty string rather than failing the fire or leaving the placeholder visible. Values are substituted verbatim; treating field values as untrusted input is deferred, and named as such.

The same field map is what `match_condition` filters on, which is why the console's field catalogue drives both the filter picker and the prompt builder from one definition. If they disagreed, one of them would be lying about what a trigger can read.

Manual triggers get their own `var.` namespace: a declared input `region` is readable as `{{ var.region }}`, built from the same declaration the form renders and the API validates.

### Grant propagation

A trigger's run grant is written by `orbit-triggers` but enforced by `orbit-agents`, which learns about it only on its next OPA bundle poll (`policy.bundlePollIntervalSecs`, currently 3s). Between the two there is a window in which the grant exists and is refused. A `403` in that window means "not visible yet", not "not allowed", and the two are indistinguishable from the outside.

There are **two** sites on that window, sharing one retry budget of six seconds with exponential backoff from 250ms. The budget is sized against the poll interval and must comfortably exceed one poll; raising `bundlePollIntervalSecs` without raising the budget breaks trigger creation.

**At create, `resolveAgentType`.** It mints a token for the trigger's own principal and asks `orbit-agents` for the agent type, which validates tenant ownership and stops a client-supplied slug from shaping the run grant's scope. For a scheduled or manual trigger this runs moments after the grant was created, so the self-signed token is used in the same request that wrote the grant. `ErrForbidden` is retried; `ErrAgentTypeNotFound` fails fast, because it means genuinely absent. A webhook trigger's principal is its source's, granted at source-creation time, so that path resolves on the first attempt.

**At dispatch, the run start.** The same window reopens on the first fire, and it needs a second bound the create site does not, because dispatch happens repeatedly and indefinitely. A refusal here is only attributable to propagation while the **trigger is young**, so the retry is gated on `time.Since(trigger.CreatedAt) < 60s`. Beyond that window a `403` is a real denial (a revoked grant, a refused delegated agent type) and is returned immediately. Retrying it would delay an honest failure and hold a queue slot for the whole budget on every fire. The 60 second window is deliberately wider than the six second budget, because a fire queued behind others can reach dispatch well after its trigger was written.

**This depends on `orbit-agents` answering `403` rather than `500`.** A start refused by the per-agent scope check previously fell through to an internal error, which misreports the caller's own answer as a server fault and silently defeats the retry: the loop can only recognise a refusal that arrives as `ErrForbidden`. The fix belongs on the agents side and is recorded in Decision 6 above.

### Observability

Four signals, wired on the meter `orbit-triggers` builds in its own `main`:

| Signal | Instrument | Why |
|---|---|---|
| Queue depth and oldest waiting age | `orbit.triggers.queue.depth` (by claim state), `orbit.triggers.queue.oldest_unclaimed_age` | Depth alone cannot tell a busy queue that drains from a stalled one that does not |
| Auto-pauses | `orbit.triggers.auto_pauses`, attributed by trigger | "Which trigger keeps pausing" is unanswerable from an unattributed total, and trigger ids are bounded by configuration rather than traffic |
| Purge freshness | `orbit.triggers.retention.last_success_time`, `orbit.triggers.payloads_purged` | A rows counter sits at zero both when there is nothing to purge and when the job is dead. Only the timestamp separates them, and it advances **only on a successful run** |
| Scheduler ticks | `orbit.triggers.scheduler.tick_duration`, `orbit.triggers.scheduler.triggers_scanned` | A tick taking longer than a minute has silently stopped being a minutely schedule (Decision 2A's known limit) |

The queue and purge-freshness signals are observable gauges read at collection time rather than counters the code must remember to update, so they cannot drift from the table. A failed read of one gauge is logged and skipped rather than returned, so it does not abort the collection cycle and take the other instruments with it.

The meter is set up in `cmd/orbit-triggers/main.go`, not in `internal/platform/observability/`. That package is traces-only and shared by every binary, so adding a metrics pipeline to it would change the runtime behaviour of a dozen services at once. Triggers is the first service to need metrics; the exporter selection deliberately mirrors `observability.Setup`, including its priority order, so traces and metrics are configured by the same two settings and cannot end up pointing at different collectors. This shape is what should be lifted into the platform package when a second service needs it. A `*Metrics` is nil-safe throughout, so a binary with no meter behaves exactly as it did before instrumentation existed.

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
- Admission and the agent control plane disagree about `idle` on purpose. `max_parallel` therefore bounds runs that are actively working, not runs that exist, and anyone reading the two counts side by side has to know why they differ.
- A webhook source principal can still attribute a run to a sibling trigger in its own tenant. Tenancy bounds the residual, and closing it needs the token claim in Future Work rather than a cross-service read.
- The grant-propagation retries couple `orbit-triggers` to `orbit-agents`' bundle poll interval. Raising that interval without raising the retry budget breaks trigger creation, which is a coupling between two services' configuration that nothing enforces.

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
2. `Admission` implementation; the `SKIP LOCKED` worker loop on a one second poll, with the stuck-claim reaper running ahead of each claim. Every fire routes through the queue.
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
- A source's token cannot start an agent type it is not bound to: the per-agent check refuses it with `403`, and nothing is written.
- The same signed body resent under a rewritten provider delivery id is refused; resent under its original id it records as a linked duplicate.
- A person starting a run cannot attribute it to a trigger, and a trigger principal cannot attribute a run to a different trigger.
- A replay of a delivery received before the source's last secret rotation is refused, as is an eleventh replay of the same original.

### Monitoring

Four signals, instrumented as described in [Observability](#observability):

- Pending-queue depth and oldest-unclaimed age. A growing backlog is the earliest signal that a target agent is unhealthy.
- Auto-pause events, attributed to the trigger.
- Purge job last-success timestamp, alerting when stale. A retention promise that silently stops running is worse than not making it.
- Scheduler tick duration and triggers-scanned count, to catch Decision 2A's known scaling limit before it bites.

---

## Future Work

- **Push-based bundle invalidation, to remove the poll coupling.** Both grant-propagation retry sites exist only because a grant written by `orbit-triggers` is not visible to `orbit-agents` until that service's next OPA bundle poll. Two retry loops, a six-second budget and a 60-second attributability window are all compensation for one missing push. [Policy Gateway and Authorisation Model](2026-04-14-policy-gateway-and-authorisation-model.md) already names push-based invalidation as deferred; when it lands, both loops here delete rather than shrink, and a refusal becomes unambiguously a denial. Until then the budget is coupled to `policy.bundlePollIntervalSecs` and cannot be tuned independently of it.
- **Stamp the trigger id into the minted `SERVICE` token's claims.** Attribution currently proves itself from the shape of the caller's principal id, which works exactly for a `trigger:<id>` principal and not at all for a `trigger-source:<id>` one, leaving the sibling-trigger residual described in [Run attribution](#run-attribution). Carrying the trigger id in the token makes the binding travel with the credential, so `orbit-agents` verifies it against a claim rather than a prefix and needs no lookup into a database it must not read. It closes the residual without weakening the feature boundary, which is why it is the durable fix rather than a cross-service query.
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
- [Delegated Agent Identity](2026-07-23-delegated-agent-identity.md) — the model a trigger cannot satisfy. All three create paths refuse a delegated agent type, because a trigger has no delegator to intersect with.
- [orbit-agents Route and Scope Registration](2026-05-11-orbit-agents-route-scope-registration.md). Its `POST /api/v1/runs` binding is corrected by Decision 6: the governing scope is per-agent, so a route-level parent scope widens rather than narrows.
- [Policy Gateway and Authorisation Model](2026-04-14-policy-gateway-and-authorisation-model.md). The OPA bundle poll whose propagation window both grant-propagation retry sites compensate for.
- [Platform-wide Scope Enforcement Middleware](2026-06-04-platform-scope-enforcement-middleware.md) — the middleware Decision 6 works around and 6B would extend.
- [Dynamic Route-Scope Registration](2026-04-14-dynamic-route-scope-registration.md) — the `RouteScope` mechanism whose limits Decision 6 documents.
- [JIT Credential Brokering via a Per-Tenant Egress Gateway](2026-06-06-jit-credential-brokering-egress-gateway.md) — unchanged; triggered runs use it as any run does.

---

## References

- [PRD: Extended Trigger Types](../prd/extended-trigger-types.md) — including PD1-PD7, the product decisions this ADR implements.
- `backend/internal/triggers/` — the implementation this ADR extends.
- [robfig/cron](https://github.com/robfig/cron) — the cron library the scheduler uses.

---

**Last Updated:** 2026-07-29
