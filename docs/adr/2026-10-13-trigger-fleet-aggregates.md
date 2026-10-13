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
2026-10-06, the default list held 100 deliveries spanning 02:02Z–05:15Z:

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

## Decision Drivers

1. **Correctness before cost.** A number that silently means "within the last
   100 tenant arrivals" is worse than a slower number that means what it says.
2. **One request for the fleet.** N triggers must not become N round trips.
3. **Agreement with the detail surfaces.** The aggregate must count a delivery
   for a trigger under the same rule the detail panel uses, including the
   fan-out, or the row and the panel will disagree about the same arrival.
4. **Bounded database work.** Aggregating 24 hours per trigger must be index-
   supported, not a scan of the tenant's full delivery history.

## Considered Options

### Option A: Raise the limit (rejected)

Fetch 500 or 1000 instead of 100.

A cap is still a cap. The failure recurs as soon as unmatched arrivals exceed
the new number, which webhook bursts from a disabled source do easily. It also
pays the full payload cost of every extra row to compute three small numbers.
This defers the bug rather than fixing it, and the deferral is proportional to
how quiet the tenant is.

### Option B: One filtered request per trigger (rejected for the fleet)

Call `ListDeliveries` once per trigger with `trigger_id` set.

Correct, and it needs no new backend surface, since the filter exists. But the
fleet view renders every trigger a tenant has, so this is N requests to render
one page, each returning full delivery rows to compute three scalars.

This option is right for a *single* trigger's history — opening one trigger's
detail should use exactly this — and wrong for the fleet.

### Option C: Server-computed aggregates on the trigger list (chosen)

Return `last_fired_at`, a recent-failure count, and the sparkline buckets with
each trigger in the existing list response, computed in Postgres in one query
grouped by trigger.

The predicate and the window live in the same place, so no cap is applied ahead
of the filter. One request renders the page. The browser keeps the presentation
and loses only the derivation.

The `skipped`-delivery problem dissolves rather than needing a toggle: an
arrival that matched no trigger contributes to no trigger's aggregate, so it
cannot crowd anything out. The issue's suggestion to filter `skipped` out of the
fleet view is unnecessary once the aggregate is server-side — and would have
been the wrong fix, since it treats a symptom of the capped window.

### Option D: Denormalized `last_fired_at` column on `triggers` (rejected for now)

Maintain a column on the trigger row, written when a delivery dispatches.

The cheapest possible read, and the obvious end state if the aggregate query
proves too expensive. Rejected as the first step because it answers only one of
the three figures — the failure count and the sparkline are windowed and cannot
be a single column — and because a denormalized field has to be kept true on
every write path that can dispatch, including replay. That is a correctness
surface worth adding only once a measurement says the query needs it.

## Decision

Adopt **Option C**, with Option B for single-trigger history.

The trigger list response gains per-trigger aggregates computed in Postgres.
Three details decide whether the implementation is right:

**The fan-out is part of the predicate.** A delivery counts for a trigger when
`trigger_deliveries.trigger_id` matches *or* a `trigger_delivery_runs` row for
that delivery names it. `lastFired` in `fleet.ts` already implements both arms:

```ts
if (d.triggerId !== triggerId && !d.runs.some((r) => r.triggerId === triggerId)) continue;
```

An aggregate that reads only `trigger_deliveries.trigger_id` will disagree with
the detail panel on any multi-trigger arrival. As #412 found, the join form of
this predicate multiplies a delivery by its matching runs, so the aggregate must
not double-count a delivery that has several runs for one trigger.

**The sparkline is bucketed in SQL.** Returning raw timestamps for the browser
to bucket re-introduces an unbounded payload. The 12 two-hour buckets over 24
hours are produced by the query and returned as counts.

**It needs an index.** `idx_trigger_deliveries_trigger` is on `(trigger_id)`
alone. A 24-hour per-trigger aggregate wants the time column beside it, in the
shape #412 established for `(tenant_id, status, received_at DESC)`. The exact
columns are an implementation decision to be measured, not asserted here, and
the migration follows the `CONCURRENTLY` rules recorded in #412 and the session
lock in clear-route/orbit#516.

## Consequences

**Positive**

- "Last fired" and the failure count mean what they say, for a trigger that
  fired once a month as readily as one firing every minute.
- The fleet view stops shipping up to 100 full delivery rows, payloads included,
  to compute three scalars per trigger.
- The three derivations move behind one contract, so the Triggers page and the
  agent Triggers tab cannot drift apart in how they count.

**Negative**

- The trigger list response grows a nested shape, and the list endpoint becomes
  more expensive than a plain select. Its cost now scales with delivery volume
  in the window rather than with trigger count.
- `fleet.ts` loses `lastFired`, `recentFailures` and `sparkline`, so their unit
  tests go with them, and the equivalent assertions must be written against the
  SQL. Per the testing strategy in `AGENTS.md`, behaviour that lives in SQL is
  tested against real Postgres rather than a mock: a mock can only agree with
  whatever its author believed the query did, which is exactly the assumption
  worth testing for a grouped aggregate with a fan-out arm.

**Neutral**

- Option D remains available if measurement shows the aggregate is too costly
  for `last_fired_at` specifically; this decision does not foreclose it.
- The frontend still renders the same three things, so no visual change ships
  with this work.
