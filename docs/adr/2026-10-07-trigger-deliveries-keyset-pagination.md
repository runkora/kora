# Trigger Deliveries: Keyset Pagination and Server-Side Filtering

**Status:** Accepted
**Date:** 2026-10-07
**Deciders:** Platform Team
**Supersedes:** [Trigger Deliveries: Client-Side Status Filter and Inline Payload Excerpt](2026-06-20-trigger-deliveries-filter.md)
**Technical Story:** [#412](https://github.com/clear-route/orbit/issues/412)

---

## Context

[2026-06-20-trigger-deliveries-filter](2026-06-20-trigger-deliveries-filter.md) chose a client-side status filter for the Deliveries tab and recorded a Non-Goal: "Server-side or URL-driven filter, delivery volume does not yet justify API round-trips on filter changes." That was the right call for the volume at the time.

The premise has expired. `GET /api/v1/trigger-deliveries` returns the newest 100 arrivals and the console requests exactly that, so the filter runs over a 100-row window and there is no way to ask for row 101. A tenant whose deliveries exceed that window gets an answer to a question nobody asked: "show me every failed delivery" returns "every failed one among the last 100", and the two agree only while the tenant is small.

This is not a filter problem. A filter over a capped window is wrong at any window size, because the cap is applied before the predicate. Fixing it means the predicate and the page boundary have to move to the same place, which is the database.

## Decision Drivers

- The filter must apply to the full history, not to a page already chosen.
- Paging must be stable while new webhooks arrive, which they do continuously.
- Changing a filter is an interactive act and must not flash a loading skeleton over the whole page.
- `trigger_deliveries` is write-hot. Read-path indexes are not free.
- The platform already paginates one endpoint. A second convention would be a worse outcome than either convention alone.

## Considered Options

### Option A: Keep client-side filtering, raise the cap

Raise `defaultDeliveryPageSize` and filter in the browser as today.

Rejected. It moves the boundary without removing it, and it is the option that degrades worst: every reader pays the full transfer so that a minority can filter deeply. The cap is also what protects a write-hot table from unbounded scans.

### Option B: Option C from the superseded ADR, URL search params plus RSC re-render

Navigate to `?status=failed`, re-render the server component, pass the filter to the BFF.

Rejected for the reason the original ADR gave, which still holds: an RSC round trip on every filter toggle flashes a skeleton over the page during interactive exploration. The original ADR's other objection, that this is "significantly more code for a feature whose delivery volume does not yet justify server-side pagination", no longer applies, because the volume is now the problem.

### Option C: Offset pagination (`LIMIT`/`OFFSET`) with a server-side filter

Rejected. Deliveries arrive continuously and the list is newest-first, so rows shift into the window between requests. `OFFSET 100` after a burst re-shows rows the reader already saw and skips others. `OFFSET` also makes the database count and discard every skipped row on each page.

### Option D (chosen): Keyset pagination with the filter in SQL, driven by a client-side fetch

`ListDeliveries` takes a `(received_at, id)` cursor and filters on `status` and `outcome` in the query. The console fetches from the existing BFF route handler on filter change and on Load more, rather than navigating.

## Decision

**Option D.**

The filter and the page boundary both live in the query, so paging applies to the filtered set. The console keeps its interactive feel because the fetch is a client-side call to `/api/trigger-deliveries`, not a navigation: there is no RSC re-render and no skeleton flash, which is what the superseded ADR was protecting and what Option B gives up.

Three details carry the decision:

**The cursor is `(received_at, id)`, not `received_at`.** `received_at` is not unique and a webhook burst lands many rows on one timestamp, where a single-column cursor drops or repeats rows at the page boundary. The id breaks the tie.

**The outcome filter is an `EXISTS` over `trigger_delivery_runs`, not a join.** One delivery can fan out to several triggers, each with its own run. A join returns that delivery once per matching run, which corrupts both the page size and the cursor.

**`limit + 1` answers "is there more".** One extra row is exact and costs nothing, where `COUNT(*)` over the filtered set is a second pass on every page.

The envelope follows the convention `inferencegw` established for `GET /v1/usage`: `has_next_page` plus a nullable `next_cursor`, with the enum values derived from the status constants rather than re-listed, so what a caller may filter on cannot drift from what the pipeline writes. The cursor is opaque to clients and carries no tenant; every query is scoped by `tenant_id` from verified claims.

## Consequences

### Positive

- A status filter means the same thing at any tenant size.
- Paging is stable under concurrent arrivals, which offset pagination is not.
- Filter changes stay interactive, with no skeleton flash.
- The repo has three keyset implementations rather than one shared helper. This change follows the `inferencegw` envelope rather than inventing a fourth, but consolidating them is still owed.

### Negative

- One more index on a write-hot table: `(tenant_id, status, received_at DESC)`. `received_at DESC` matches the keyset `ORDER BY` so a single index serves both the filter and the page walk, and `id` is deliberately excluded because it only breaks exact ties the existing index already locates cheaply.
- A single status is emitted as scalar equality rather than `= ANY($n)`. A ScalarArrayOp cannot serve as an index condition on the leading columns, so the planner costs this index worse than the tenant index plus a filter and abandons it. Measured on 300k rows under the default `plan_cache_mode`: the scalar form read 102 rows under an index condition using 106 buffers, the `ANY` form discarded 101,898 rows by filter using 2,266, and the `ANY` plan varied between indexes across sessions where the scalar one did not. This is not a generic-plan effect; `pg_prepared_statements` reported `generic_plans = 0` throughout, so it holds for the plans pgx actually gets. The multi-value case keeps `ANY` and keeps the scan; a covering answer for multi-select would need a different index and is not justified by current usage.
- The cursor encoding is now a compatibility surface. It is documented as opaque and is ours to change, but a client that parses it will break.
- The keyset cursor now exists in three packages (`triggers`, `inferencegw` and `agents`), each with its own envelope. Lifting one into `internal/platform/` is the right follow-up and is not done here.

### Neutral

- `matchesStatusFilter` and the client-side filtering the superseded ADR introduced are removed. The payload excerpt that ADR also introduced is unaffected and stays.
- The `outcome` parameter is implemented server-side per #412 but has no console consumer yet.

## Related Decisions

- Supersedes [2026-06-20-trigger-deliveries-filter](2026-06-20-trigger-deliveries-filter.md), whose payload-excerpt decision still stands.
- Follows the cursor envelope `inferencegw` uses for `GET /v1/usage` (`internal/inferencegw/handlers_usage.go`). No ADR records that convention; this is the first to write it down.

## References

- [#412](https://github.com/clear-route/orbit/issues/412)
- `backend/internal/triggers/postgres.go`, `repository.go`, `service_fire.go`
- `backend/cmd/orbit-triggers/migrations/20261007064253_add_delivery_status_index.sql`
