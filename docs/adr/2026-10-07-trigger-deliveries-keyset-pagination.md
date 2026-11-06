# Trigger Deliveries: Keyset Pagination, Server Status Filter and Trigger Stats

**Status:** Accepted  
**Date:** 2026-10-07  
**Deciders:** Platform Team  
**Supersedes:** [Trigger Deliveries: Client-Side Status Filter and Inline Payload Excerpt](2026-06-20-trigger-deliveries-filter.md)  
**Technical Story:** [#412](https://github.com/runkora/kora/issues/412), [#410](https://github.com/runkora/kora/issues/410)

---

## Context

`GET /api/v1/trigger-deliveries` paged on `received_at` alone, and the console filtered statuses and computed "Last fired", recent failures, the 24-hour sparkline and the KPI cards in the browser from the tenant's 100 newest deliveries. A filter or a count applied after a row cap is wrong at any cap. A cursor on `received_at` alone drops or repeats rows that share a timestamp.

## Considered Options

- **Option A (rejected):** Raise the row cap and keep filtering and counting in the browser. A larger cap moves the boundary and does not remove it.
- **Option B (rejected):** Drive the filter through URL search params and a server component re-render. Every filter change re-renders the page behind a loading skeleton.
- **Option C (rejected):** Use `LIMIT`/`OFFSET` with a server filter. Deliveries arrive continuously, and an offset page repeats or skips rows after a burst.
- **Option D (chosen):** Use a keyset cursor and a status filter in SQL, fetched from the console without navigation, and compute trigger stats in Postgres on the trigger list.

## Decision

The deliveries list pages with a keyset cursor and filters by status in SQL, and `GET /api/v1/triggers` returns trigger stats computed by grouped queries.

### Deliveries Paging

- The cursor is an opaque, base64url-encoded `(received_at, id)` pair that the request takes as `cursor=` and every page but the last returns as `next_cursor`.
- The query orders by `received_at DESC, id DESC` and reads `limit + 1` rows to decide whether another page follows.
- `received_before` is removed. The kora MCP tool `triggers.deliveries.list` keeps `received_after` and takes `cursor`.

### Trigger Scope

- `trigger_id=` lists the deliveries that reached the trigger: those with one of its outcome rows in `trigger_delivery_runs`, and those recorded against it with a `verified`, `missed` or `duplicate` status and no outcome of their own.
- `agent_type_id=` applies the same scope to every trigger `GET /api/v1/triggers?agent_type_id=` lists for the agent. It does not combine with `trigger_id`, and neither combines with `source_id` or `kind`.
- `trigger_delivery_runs` carries the delivery's `received_at`, copied on insert and backfilled by the migration. The column defaults to `now()` for rows written by older pods during a rollout.
- The query reads one index-ordered page per trigger and outcome from `idx_trigger_delivery_runs_reach` or `idx_trigger_delivery_runs_reach_status`, and one per trigger from the partial `idx_trigger_deliveries_recorded`. It merges the pages and keeps the newest. A page costs the same at any history depth.

### Status Filter

- With a trigger scope, `status=` takes a trigger status. Every outcome status `trigger_delivery_runs` permits and every recorded delivery status counts under exactly one:
  - `started`: `dispatched` and `joined`.
  - `failed`: `failed`, `denied` and `dropped`.
  - `filtered`: `filtered`.
  - `skipped`: `unrouted` and `paused`.
  - `superseded`: `superseded`.
  - `duplicate`, `missed` and `verified`: a recorded delivery with that status.
- Without a trigger scope, `status=` takes one of `dispatched`, `verified`, `skipped`, `failed`, `duplicate` or `missed`. It matches the delivery's status with scalar equality to use `idx_trigger_deliveries_tenant_status`.
- A value outside the scope's set is a 400.

### Trigger Stats

Each trigger in `GET /api/v1/triggers` carries `stats`, and the envelope carries the tenant's `stats` and the `window` they cover. A trigger's stats count its outcome rows and the deliveries recorded against it, as the trigger scope defines them. `window=` takes `24h` or `7d`, defaults to `24h` and runs to the time of the request. Any other value is a 400. `idx_trigger_delivery_runs_window` on `(tenant_id, received_at)` limits those reads to the window's rows on every supported Postgres version.

The per-trigger stats are:

- `last_fired_at`: the `received_at` of the newest outcome row with a `dispatched`, `joined`, `failed` or `denied` status, over all history. Each trigger reads it with one probe of `idx_trigger_delivery_runs_last_fired`, a partial index over those statuses.
- `deliveries`, `started` and `failed`: the window's deliveries and its `started` and `failed` trigger statuses.
- `status_counts`: the window's deliveries per trigger status. The counts of all trigger statuses sum to `deliveries`.
- `buckets`: buckets of `started`, `failed` and `other`, aligned to UTC with `date_bin`, from the bucket holding the window's start to the bucket holding now. `24h` has thirteen two-hour buckets and `7d` has fifteen twelve-hour buckets. The first and last buckets are partial, and the bucket counts sum to `deliveries`.

The tenant stats cover the same window. They are `deliveries` (every arrival), `started`, `failed`, and `dispatch_p50_ms` and `dispatch_p95_ms`. The percentiles use `percentile_cont` over started outcomes, measured from delivery arrival to the outcome row. The console derives the success rate as started over started plus failed.

The console bounds a delivery list to the selected window with `received_after`, and its activity sparkline draws `started` and `failed` only.

## Consequences

### Positive

- A status filter and every count mean the same thing at any tenant size.
- Paging is stable while deliveries arrive.
- The Triggers page and the agent page no longer fetch 100 deliveries to compute their numbers.
- A trigger's last fire and any trigger-scoped page cost the same at any history depth.

### Negative

- Every arrival maintains `idx_trigger_deliveries_tenant_status`, and every outcome row maintains four more indexes on `trigger_delivery_runs`.
- The stats aggregate every outcome row in the window on each trigger list read. The cost grows with the window's volume and does not grow with history. A `7d` read aggregates about seven times the rows of a `24h` read.
- A delivery recorded against a trigger with a `dispatched`, `failed` or `skipped` status and no outcome row for that trigger is absent from the trigger's list and stats.
- The cursor encoding is a compatibility surface for any client that parses it.

### Neutral

- The payload excerpt from the superseded ADR is unchanged.

## References

- `backend/internal/triggers/postgres.go`
- `backend/internal/triggers/stats.go`
- `backend/cmd/kora-triggers/migrations/20261106005343_add_delivery_status_index.sql`
- `backend/cmd/kora-triggers/migrations/20261106014701_add_delivery_run_received_at.sql`
