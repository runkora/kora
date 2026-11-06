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

- `status=` takes one of `dispatched`, `verified`, `skipped`, `failed`, `superseded`, `duplicate` or `missed`. An unknown value is a 400.
- With a trigger scope, `status` matches the trigger's outcome rows, and the delivery's status for a recorded delivery.
- Without a trigger scope, `status` matches the delivery's status with scalar equality to use `idx_trigger_deliveries_tenant_status`. `superseded` needs a `trigger_id`.
- `failed` matches the `failed`, `denied` and `dropped` outcomes.

### Trigger Stats

Each trigger in `GET /api/v1/triggers` carries `stats`, and the envelope carries the tenant's `stats`. A trigger's stats count its outcome rows and the deliveries recorded against it, as the trigger scope defines them. Windows run over the 24 hours before the request and read `trigger_delivery_runs` by `received_at`.

The per-trigger stats are:

- `last_fired_at`: the `received_at` of the newest outcome row with a `dispatched`, `joined`, `failed` or `denied` status, over all history. Each trigger reads it with one probe of `idx_trigger_delivery_runs_last_fired`, a partial index over those statuses.
- `deliveries_24h`, `started_24h` (`dispatched`, `joined`) and `failed_24h` (`failed`, `denied`, `dropped`).
- `status_24h`: counts by `status=` value, with the same match the trigger-scoped filter applies.
- `buckets`: thirteen buckets of `started`, `failed` and `other`, aligned to UTC two-hour boundaries with `date_bin`, from the bucket holding now minus 24 hours to the bucket holding now. The first and last buckets are partial, and the bucket counts sum to `deliveries_24h`.

The tenant stats are `deliveries_24h` (every arrival), `started_24h`, `failed_24h`, and `dispatch_p50_ms` and `dispatch_p95_ms`. The percentiles use `percentile_cont` over started outcomes, measured from delivery arrival to the outcome row. The console derives the success rate as started over started plus failed.

## Consequences

### Positive

- A status filter and every count mean the same thing at any tenant size.
- Paging is stable while deliveries arrive.
- The Triggers page and the agent page no longer fetch 100 deliveries to compute their numbers.
- A trigger's last fire and any trigger-scoped page cost the same at any history depth.

### Negative

- Every arrival maintains `idx_trigger_deliveries_tenant_status`, and every outcome row maintains three more indexes on `trigger_delivery_runs`.
- The 24-hour stats aggregate every outcome row in the window on each trigger list read. The cost grows with 24-hour volume and does not grow with history.
- A delivery recorded against a trigger with a `dispatched`, `failed` or `skipped` status and no outcome row for that trigger is absent from the trigger's list and stats.
- The cursor encoding is a compatibility surface for any client that parses it.

### Neutral

- The payload excerpt from the superseded ADR is unchanged.

## References

- `backend/internal/triggers/postgres.go`
- `backend/internal/triggers/stats.go`
- `backend/cmd/kora-triggers/migrations/20261106005343_add_delivery_status_index.sql`
- `backend/cmd/kora-triggers/migrations/20261106014701_add_delivery_run_received_at.sql`
