# Trigger Fleet Aggregates Computed in Postgres

<!-- Filename: 2026-10-13-trigger-fleet-aggregates.md  (decision date + slug; no sequential number) -->

**Status:** Proposed
**Date:** 2026-10-13
**Deciders:** Pratik Dhanave
**Technical Story:** clear-route/orbit#410. Shares a root cause with clear-route/orbit#412, whose ADR (`2026-10-07-trigger-deliveries-keyset-pagination.md`) lands with that PR.

---

## Context

### Background

The Triggers page shows a fleet view: one row per trigger, each carrying a
"Last fired" time, a recent-failure count, and a 24-hour sparkline. All three
are derived in the browser by `frontend/src/app/(dashboard)/triggers/_types/fleet.ts`
(`lastFired`, `recentFailures`, `sparkline`), scanning a single delivery list.

That list comes from one call in `triggers-server.ts`:

```ts
listDeliveries(accessToken, { limit: 100 })
```

No trigger filter, no status filter: the tenant's 100 newest arrivals across
every source. Each derivation then scans that array for rows belonging to a
given trigger.

### Problem Statement

A trigger matches a delivery when `delivery.trigger_id` is the trigger **or**
any fan-out row in `trigger_delivery_runs` names it. The window is capped at
100 arrivals *before* that predicate is applied, so arrivals that match nothing
still consume the budget.

They routinely do. `skipped` deliveries from sources whose triggers are
disabled carry no `trigger_id` at all. Observed in the `orbit-dev` tenant on
2026-10-06, the default list held 100 deliveries spanning 02:02Z-05:15Z:

| Source | Deliveries | Status |
| --- | --- | --- |
| `372ee51e` (GitHub) | 54 | `skipped` |
| `3741f5a6` (Alertmanager) | 45 | `skipped` |
| `34ce444d` (GitHub) | 1 | `skipped` |

None carried a `trigger_id`. A matched delivery for the "Orbit Bug Verifier"
trigger at 01:49Z existed in the database but sat outside the window, so every
trigger on the page read "never fired" while its history was intact in Postgres.

This is the same defect as #412 in a different surface: **a predicate evaluated
after a cap is wrong at any cap size**, because the cap is spent on rows the
predicate would have discarded. #412 moved the delivery list's filter and page
boundary into SQL. The fleet view's three derivations were left in the browser,
so they still read a capped, unfiltered window.

### Goals

- "Last fired", recent failures, and the sparkline reflect a trigger's real
  history, not whatever fell inside the newest 100 tenant-wide arrivals.
- The fleet view costs one request, not one per trigger.
- A trigger's own history remains reachable through the existing `trigger_id`
  filter, which `ListDeliveries` already supports.

### Non-Goals

- Changing what the fleet view *displays*. The three figures and the 12-bucket
  sparkline shape stay as they are; only where they are computed changes.
- Real-time push. The page refreshes on its existing cadence.
- Reworking the deliveries list itself. #412 already did that.

---

## Decision Drivers

1. **Correctness before cost.** A number that silently means "within the last
   100 tenant arrivals" is worse than a slower number that means what it says.
2. **One request for the fleet.** N triggers must not become N round trips.
3. **Agreement with the detail surfaces.** The aggregate must count a delivery
   for a trigger under the same rule the detail panel uses, including the
   fan-out, or the row and the panel will disagree about the same arrival.
4. **Bounded database work.** Aggregating 24 hours per trigger must be index-
   supported, not a scan of the tenant's full delivery history.

---

## Considered Options

### Option 1: Raise the limit

**Description:** Fetch 500 or 1000 arrivals instead of 100 and keep deriving in
the browser.

**Pros:**
- Smallest possible change; no API or SQL work.
- Fixes the reported symptom for tenants below the new cap.

**Cons:**
- A cap is still a cap. The failure returns as soon as unmatched arrivals exceed
  the new number, which a webhook burst from a disabled source does easily.
- Pays the full payload cost of every extra row to compute three small numbers.
- Defers the bug in proportion to how quiet the tenant is, so it fails worst on
  the busiest tenants.

### Option 2: One filtered request per trigger

**Description:** Call `ListDeliveries` once per trigger with `trigger_id` set.

**Pros:**
- Correct, and needs no new backend surface: the filter already exists.
- Each response is naturally scoped, so nothing can crowd anything out.

**Cons:**
- The fleet renders every trigger a tenant has, so this is N requests for one
  page.
- Each request returns full delivery rows to compute three scalars.

Right for a *single* trigger's history; wrong for the fleet.

### Option 3: Server-computed aggregates on the trigger list (chosen)

**Description:** Return the three figures with each trigger in the existing list
response, computed in Postgres in one query grouped by trigger.

**Pros:**
- Predicate and window live in the same place, so no cap precedes the filter.
- One request renders the page; the browser keeps presentation only.
- The `skipped` problem dissolves: an arrival matching no trigger contributes to
  no trigger's aggregate, so it cannot crowd anything out.
- One contract for both surfaces, so the Triggers page and the agent Triggers
  tab cannot drift in how they count.

**Cons:**
- The list endpoint becomes more expensive, and its cost now scales with
  delivery volume in the window rather than with trigger count.
- Moves three tested browser functions into SQL, where testing them requires a
  real database.

### Option 4: Denormalized `last_fired_at` column on `triggers`

**Description:** Maintain a column on the trigger row, written when a delivery
dispatches.

**Pros:**
- The cheapest possible read, and the obvious end state if the aggregate query
  proves too expensive.

**Cons:**
- Answers only one of the three figures; the failure count and the sparkline are
  windowed and cannot be a single column.
- The field must be kept true on every write path that can dispatch, including
  replay, which is a new correctness surface.

---

## Decision

Adopt **Option 3**, with Option 2 for single-trigger history.

### Response shape

Each trigger in the list response gains one nested object. Field names are
snake_case to match the existing envelope:

```jsonc
{
  "id": "trg-1",
  // ...existing trigger fields...
  "activity": {
    // RFC 3339 UTC, or null when the trigger has never fired. Null rather than
    // a zero time or empty string: "never" is a real state the row renders
    // differently, and the existing envelope already uses null this way for
    // next_cursor.
    "last_fired_at": "2026-10-13T04:12:09Z",

    // Count of failed runs in the window. 0, never null: a trigger with no
    // failures and a trigger with no history both show no badge, and the
    // caller should not have to distinguish them here.
    "recent_failures": 3,

    // Exactly SPARK_BUCKETS (12) entries, oldest first, never null and never
    // short. A quiet trigger is twelve zeroed buckets, so the chart reads as
    // quiet rather than as a shorter chart than its neighbour - the property
    // `sparkline` documents today.
    "sparkline": [
      { "started": 0, "failed": 0, "other": 0 }
      // ...11 more
    ]
  }
}
```

A trigger that has never fired returns `last_fired_at: null`,
`recent_failures: 0`, and twelve zeroed buckets. The key is always present.

### Window semantics

- **Anchor.** The window is `(now - 24h, now]`, where `now` is the server's
  clock at query time, in UTC. One anchor is chosen per request and used for
  every trigger in it, so two rows in the same response cannot be measured
  against different instants.
- **Bucket alignment.** Buckets are aligned to the anchor, not to wall-clock
  hours: bucket 0 begins exactly 24 hours before `now` and each spans two
  hours. This matches `sparkline` today, whose index is
  `floor((t - (now - DAY_MS)) / bucketMs)` clamped to the last bucket. Aligning
  to wall-clock boundaries instead would change what the chart shows.
- **An arrival exactly on `now`** falls in the last bucket; one older than the
  anchor is excluded. `t > now` is excluded, as the browser does today, so a
  clock skew ahead of the server cannot land a delivery in the future.

### What counts, and for which figure

The three figures do not share one predicate, and an implementation that
assumes they do will be wrong:

- **`last_fired_at`** is delivery-level, and matches on either arm:
  `trigger_deliveries.trigger_id` is the trigger **or** a `trigger_delivery_runs`
  row for that delivery names it. It is the newest matching delivery's
  `received_at`.
- **`recent_failures`** and **`sparkline`** are run-level. They count rows in
  `trigger_delivery_runs` for this trigger whose parent delivery falls in the
  window, classified by run status: `dispatched` is `started`, the failure
  outcomes are `failed`, everything else is `other`.

An aggregate that reads only `trigger_deliveries.trigger_id` will disagree with
the detail panel on any multi-trigger arrival. As #412 found, the join form of
this predicate multiplies a delivery by its matching runs, so `last_fired_at`
must not be computed by a join that double-counts a delivery with several runs.

### Index

`idx_trigger_deliveries_trigger` is on `(trigger_id)` alone. A 24-hour
per-trigger aggregate wants the time column beside it, in the shape #412
established for `(tenant_id, status, received_at DESC)`. The exact columns are
to be measured rather than asserted here. The migration follows the
`CONCURRENTLY` rules recorded in #412 and the session lock in
clear-route/orbit#516.

### Rationale

The `skipped`-delivery filter the issue proposes is deliberately **not** part of
this decision. It treats a symptom of the capped window: once the aggregate is
server-side, an arrival that matched no trigger contributes to no trigger's
figures, so there is nothing to filter out. Adding the filter as well would hide
the remaining evidence if the aggregate were ever wrong.

---

## Consequences

### Positive

- "Last fired" and the failure count mean what they say, for a trigger that
  fired once a month as readily as one firing every minute.
- The fleet view stops shipping up to 100 full delivery rows, payloads included,
  to compute three scalars per trigger.
- The three derivations move behind one contract, so the Triggers page and the
  agent Triggers tab cannot drift apart in how they count.

### Negative

- The trigger list response grows a nested object, and the list endpoint becomes
  more expensive than a plain select. Its cost now scales with delivery volume
  in the window rather than with trigger count.
- `fleet.ts` loses `lastFired`, `recentFailures` and `sparkline`, so their unit
  tests go with them, and the equivalent assertions must be written against the
  SQL.
- The window is anchored server-side, so a client in another timezone can no
  longer re-derive the chart against its own clock. This is intended: one anchor
  per response is what makes two rows comparable.

### Neutral

- Option 4 remains available if measurement shows the aggregate is too costly
  for `last_fired_at` specifically; this decision does not foreclose it.
- The frontend still renders the same three things, so no visual change ships
  with this work.

---

## Implementation

### Migration Path

1. Add the aggregate query and the `activity` object to the trigger list
   response, with the index the measurement calls for.
2. Switch the frontend to read `activity`, deleting the three derivations and
   the 100-row delivery fetch that fed them.
3. Point single-trigger history at `ListDeliveries` with `trigger_id`
   (Option 2), which needs no new backend work.

Steps 1 and 2 can ship together or in order; the response is additive, so a
frontend still deriving in the browser keeps working against a backend that has
already shipped step 1.

### Timeline

Not scheduled. This ADR records the approach so implementation does not have to
re-derive it.

---

## Validation

The decision is correct if a trigger's "Last fired" survives a burst of
unmatched arrivals, which is precisely what reproduced #410.

### Success Metrics

- A trigger that fired once, followed by more than 100 unmatched arrivals, still
  shows its real "Last fired" time. This is the #410 reproduction, and it is the
  one assertion that must exist.
- The fleet view issues one request regardless of trigger count.
- The trigger list endpoint's latency stays within its current budget at
  production delivery volume, measured rather than assumed.

### Monitoring

The list endpoint is already traced; the aggregate query appears as a child span,
so a regression shows up as that span's duration rather than as a mystery in the
page load.

Per the testing strategy in `AGENTS.md`, behaviour that lives in SQL is tested
against real Postgres rather than a mock: a mock can only agree with whatever its
author believed the query did, which is exactly the assumption worth testing for
a grouped aggregate with a fan-out arm and a windowed bucket index.

---

## Related Decisions

- `2026-10-07-trigger-deliveries-keyset-pagination.md` - the same root cause on
  the deliveries list; lands with clear-route/orbit#412.
- [Trigger Deliveries: Client-Side Status Filter and Inline Payload Excerpt](2026-06-20-trigger-deliveries-filter.md) - the superseded client-side filter, for the same reason recorded here.
- [Extended Trigger Types and an Operable Delivery Pipeline](2026-06-20-extended-trigger-types.md) - defines the delivery pipeline and its statuses.

---

## References

- clear-route/orbit#410 - the reported defect, with the `orbit-dev` evidence.
- `frontend/src/app/(dashboard)/triggers/_types/fleet.ts` - the derivations this
  decision moves, and the source of the bucket and matching semantics above.
