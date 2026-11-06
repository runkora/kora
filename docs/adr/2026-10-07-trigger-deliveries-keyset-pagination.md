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

### Status Filter

- `status=` takes one of `dispatched`, `verified`, `skipped`, `failed`, `superseded`, `duplicate` or `missed`. An unknown value is a 400.
- With `trigger_id` set, the list holds every delivery that reached the trigger: recorded against it or carrying one of its outcome rows. `status` then matches the trigger's own outcome, or the delivery's status when the trigger has no outcome row.
- Without `trigger_id`, `status` matches the delivery's status with scalar equality to use `idx_trigger_deliveries_tenant_status`. `superseded` needs a `trigger_id`.
- `failed` matches the `failed`, `denied` and `dropped` outcomes.
- Outcome matches use `EXISTS` over `trigger_delivery_runs`. A join would return a delivery once per matching run and break the page size and the cursor.

### Trigger Stats

Each trigger in `GET /api/v1/triggers` carries `stats`, and the envelope carries the tenant's `stats`. Each delivery that reached a trigger counts once, by the trigger's first outcome row or by the delivery's status when it has none. Windows run over the 24 hours before the request.

The per-trigger stats are:

- `last_fired_at`: the newest delivery with a `dispatched`, `joined`, `failed` or `denied` outcome, over all history. `filtered`, `superseded`, `unrouted`, `paused`, `dropped`, `duplicate` and `missed` do not count.
- `deliveries_24h`, `started_24h` (`dispatched`, `joined`) and `failed_24h` (`failed`, `denied`, `dropped`).
- `status_24h`: counts by `status=` value, with the same match the trigger-scoped filter applies.
- `buckets`: twelve buckets of `started`, `failed` and `other`, aligned to UTC two-hour boundaries with `date_bin`. The last bucket holds the current time and is partial.

The tenant stats are `deliveries_24h` (every arrival), `started_24h`, `failed_24h`, and `dispatch_p50_ms` and `dispatch_p95_ms`. The percentiles use `percentile_cont` over started outcomes, measured from delivery arrival to the outcome row. The console derives the success rate as started over started plus failed.

## Consequences

### Positive

- A status filter and every count mean the same thing at any tenant size.
- Paging is stable while deliveries arrive.
- The Triggers page no longer fetches 100 deliveries to compute its numbers.

### Negative

- `idx_trigger_deliveries_tenant_status` on `(tenant_id, status, received_at DESC)` adds one index to the write path of every arrival.
- `last_fired_at` scans the tenant's attempted outcomes on every trigger list read.
- The cursor encoding is a compatibility surface for any client that parses it.

### Neutral

- The payload excerpt from the superseded ADR is unchanged.

## References

- `backend/internal/triggers/postgres.go`
- `backend/internal/triggers/stats.go`
- `backend/cmd/kora-triggers/migrations/20261106005343_add_delivery_status_index.sql`
