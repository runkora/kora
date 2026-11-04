# Trigger Fire Coalescing

<!-- Filename: 2026-10-13-trigger-fire-coalescing.md -->

**Status:** Accepted
**Date:** 2026-10-13
**Deciders:** Justin Wilkin
**Technical Story:** [#500 triggers: coalesce fires about the same subject with an author-defined key](https://github.com/clear-route/orbit/issues/500)

---

## Context

### Background

A webhook trigger starts one run for every event that matches it.
A burst of events about the same subject starts one run per event, and each run repeats the same work.
Rebasing a stack of 3 pull requests sent 5 `synchronize` events to one trigger within 2 seconds.
A force-push to a stacked pull request's base branch sends that pull request a `synchronize` event, and its own head push sends a second one.

Every fire goes through the durable queue `trigger_pending_deliveries`.
`Admission.Admit` (`backend/internal/triggers/admission.go`) writes the row.
The worker claims it through `ClaimPending` once the trigger is under `max_parallel`.
The worker polls every second.

Three mechanisms exist today and none fits.

- `overflow: replace` keeps only the newest waiting fire of the whole trigger and marks the others `superseded`. It acts only at the ceiling.
- `dedupe_enabled` drops a redelivery of the same provider delivery id. Distinct events about the same subject all run.
- `overflow: wait` holds fires in order and evicts the oldest past `max_queue_depth`.

The audit spine is `trigger_deliveries` plus one `trigger_delivery_runs` row per trigger outcome.
`superseded` is already a run status.
A delivery's own status is derived from its run rows and is never written directly.

A trigger can resolve any dotted path of the payload (`triggerFields` in `payload.go`, from [Trigger Payload Templating](2026-09-28-trigger-payload-templating.md)).
The prompt template uses `{{ path }}` placeholders.

### Problem Statement

A trigger author needs one run per subject for a burst of events, where the author defines what the subject is.
The platform cannot define it.
A pull request number is the subject for one provider and an alert fingerprint for another.

### Goals

- A webhook trigger can name a key template and a window.
- Fires with the same rendered key inside the window start one run, from the newest payload.
- The audit trail shows the key of each delivery and which delivery replaced which.
- Triggers without the setting behave as they do today.

### Non-Goals

- Per-key serialisation of runs that are already running.
- Per-provider default keys.
- Scheduled and manual triggers.
- A window that extends with each new fire.

---

## Which of the three questions this touches

**Who is this agent?** Unchanged. A coalesced run starts under the same source principal as any other run. The superseded delivery keeps its audit row and names the delivery that replaced it.

**What can it access?** Unchanged. No grant, scope or policy check moves. The key is computed from the payload the trigger already reads.

**How does it connect?** This is the question it touches. It changes how an inbound webhook becomes a run: the fire may wait up to 300 seconds in the queue, and a newer fire may replace it.

---

## Decision Drivers

- A fire the platform acknowledged must still start, or be recorded as replaced by a named delivery.
- Replacement must be safe when several replicas insert the same key at once.
- The default behaviour of every existing trigger must not change.
- The author, not the platform, owns the meaning of the key.
- Audit history must explain every run that did not start.

---

## Considered Options

### D1. Where the hold lives

**Option 1a: a `dispatch_after` column on the queue row.**
The row is durable and the claim query skips rows that are not due.
A restart loses nothing.
The claim query gains one predicate.

**Option 1b: an in-memory debounce in the ingest path.**
It needs no schema.
A restart loses every held fire after the sender received a 2xx.
Each replica holds its own timers, so two replicas cannot coalesce one key.

**Option 1c: a separate window table.**
It separates the hold from the queue.
It adds a second durable structure that must agree with the queue.

### D2. Making replace atomic

**Option 2a: lock the trigger row, then update the waiting row in place or insert one.**
`SupersedeAndEnqueue` and `EnqueueWithinCeiling` already serialise on the trigger row.
One transaction holds the lookup and the write.
Coalescing inserts for one trigger run one at a time.

**Option 2b: an advisory lock on a hash of the trigger and key.**
It serialises per key rather than per trigger.
It adds a hashing scheme that can collide.

**Option 2c: a unique index on the key.**
A window is bounded by time, and an index predicate cannot reference `now()`.
The index would need a window identifier that something else must assign.

### D3. Where the key and the replacement pointer are recorded

**Option 3a: columns on `trigger_deliveries`.**
It matches the wording "each delivery records its key".
One delivery can match several triggers with different key templates, so one column loses data.

**Option 3b: columns on `trigger_delivery_runs`.**
The run row is the per-trigger record.
No run row exists while the fire is waiting, and the key must be visible from enqueue.

**Option 3c: a new table keyed by delivery and trigger for the key, and `superseded_by` on the run row.**
The key is visible from enqueue and is stored per trigger.
The pointer sits on the row whose status is `superseded`.
It adds one table.

### D4. How the key renders

**Option 4a: the inline substitution used for scheduled prompts (`renderTemplate`).**
A missing path renders empty and the result is the literal key.

**Option 4b: the webhook prompt form (`renderWebhookPrompt`).**
It appends a data block to the template.
That is wrong for a key.

---

## Decision

1. **Configuration.** A webhook trigger gains `coalesce.key` and `coalesce.window_seconds`. Both are off by default. The feature is on only when both are set. `window_seconds` is 1 to 300.
2. **Key.** The author writes `coalesce.key` as a template in the `prompt_template` syntax, for example `{{ repository.full_name }}#{{ pull_request.number }}`. It renders with Option 4a against the same field map as the prompt. The platform keeps no per-provider default keys.
3. **Window.** A fire with a key is enqueued with `dispatch_after = now() + window_seconds`. The claim query skips rows that are not due. The window is fixed from the first fire and is never extended.
4. **Replacement.** A fire whose key matches a waiting row of the same trigger, still unclaimed and still inside its window, replaces that row in place. The row keeps the first fire's `dispatch_after`. The newest payload wins.
5. **Atomicity.** Option 2a. One transaction locks the trigger row, updates the matching waiting row if one exists, and inserts a row if none did. Two concurrent fires with the same key cannot both insert.
6. **Running runs.** Only unclaimed rows are replaced. A claimed or started fire is untouched. A fire after the window closes starts its own window and its own run.
7. **Audit.** Option 3c. `trigger_delivery_keys` records the rendered key for each delivery and trigger when the fire is enqueued. The replaced delivery gets a `superseded` run row whose `superseded_by` is the replacing delivery id. The existing `overflow: replace` path records the same pointer.
8. **Delivery list.** The delivery list returns the keys and the pointer.
9. **Kinds.** Webhook triggers only. Scheduled triggers keep `replace` and manual fires keep `refuse`. A create or update that sets `coalesce` on another kind is rejected.

### Details

10. **Overflow.** Coalescing requires `overflow: wait`. Validation rejects `coalesce` with `replace` or `refuse`. `replace` removes every waiting fire of the trigger whatever its key. A per-key window cannot hold under it. The check runs on the stored value after an update is applied. Changing overflow on a coalescing trigger away from `wait` is also rejected.
11. **Empty key.** The rendered key is trimmed of surrounding whitespace. A key that is empty after trimming does not coalesce, and the fire dispatches as it does today. A partial render such as `org/repo#` is a key and coalesces.
12. **Key length.** The key template is at most 256 bytes, checked at create and update. A rendered key over 256 bytes does not coalesce. The fire dispatches as it does today, and the delivery records that the key was too long and stores no key text.
13. **Recording time.** The row in `trigger_delivery_keys` is written before the fire is enqueued. The delivery list returns it from then on, including while the fire is waiting.
14. **Several triggers.** One delivery can match several triggers. Each trigger records its own key for the delivery.
15. **Queue depth.** Held fires count toward `max_queue_depth` and `pending_count`. A replacement does not grow the backlog. After a coalescing enqueue, trim keeps the backlog at `max_queue_depth` and may evict the oldest row, held or not.
16. **Queue age.** The oldest-waiting age counts from the time a row became due. A long window does not read as a stalled worker.
17. **Edits.** A waiting row keeps the window it was enqueued with. New settings apply from the next fire.
18. **Redelivery.** A replayed delivery is a webhook fire and coalesces like any other.
19. **Update shape.** A `coalesce` object in an update replaces the stored one. `{}` turns the feature off. An update without `coalesce` leaves it unchanged.

---

## Consequences

### Positive

- A burst about one subject starts one run from the newest payload.
- The hold survives a restart because it lives in the queue row.
- Replacement is one transaction, so concurrent replicas cannot start two runs for one window.
- The delivery history answers why a delivery did not run and which delivery replaced it.
- Existing triggers are unchanged.

### Negative

- A coalescing trigger starts runs up to `window_seconds` later than it does today.
- Coalescing inserts for one trigger serialise on the trigger row.
- A continuing burst starts one run per window, not one run for the burst, because the window never extends.
- The rendered key outlives payload retention. It can hold payload-derived text for as long as delivery history is kept. The author controls which fields enter it, and the 256 byte cap bounds it.
- A held fire can be evicted by the `max_queue_depth` trim like any waiting fire.
- One more table, three more columns and one index on the queue and run tables.

### Neutral

- The worker is unchanged. It claims rows once they are due.
- Runs already started are not serialised per key. A new window can start a run while an earlier one still runs.
- A superseded delivery derives to `skipped`, as it does under `overflow: replace`.

---

## Implementation

Delivery is a stack of pull requests:

1. This ADR.
2. Schema and queue SQL, with Postgres-backed tests.
3. Trigger configuration, admission and fan-out.
4. MCP tools and the console.

---

## Validation

### Success Metrics

- Two fires with the same rendered key inside the window start one run from the second fire's payload.
- The first delivery is `superseded` and points at the second.
- Fires with different keys start one run each.
- A fire after the window closes starts its own run.
- A trigger without `coalesce` dispatches as it does today.

### Monitoring

- `pending_count` and the queue depth gauge include held fires.
- The oldest-waiting age gauge excludes the hold.

---

## Related Decisions

- [Agent Triggers and Webhook Ingestion](2026-06-17-agent-triggers-and-webhook-ingestion.md) - the ingest path this changes
- [Extended Trigger Types and an Operable Delivery Pipeline](2026-06-20-extended-trigger-types.md) - admission control, the queue and the audit spine
- [Trigger Payload Templating](2026-09-28-trigger-payload-templating.md) - the template syntax and field resolution the key reuses
- [Database Migrations](2026-09-10-database-migrations.md) - rules the migrations follow

---

## References

- [#500](https://github.com/clear-route/orbit/issues/500)

**Last Updated:** 2026-10-13
