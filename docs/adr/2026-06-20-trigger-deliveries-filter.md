# Trigger Deliveries: Client-Side Status Filter and Inline Payload Excerpt

**Status:** Accepted  
**Date:** 2026-06-20  
**Deciders:** Platform Team  
**Technical Story:** docs/prd/trigger-deliveries-filter.md

---

## Context

The Triggers → Deliveries tab lists every inbound webhook delivery. The backend records four delivery statuses — `verified`, `dispatched`, `skipped`, `failed` — but the UI renders them all in a single, unfiltered table. Admins debugging broken pipelines must scroll through large lists of `dispatched` rows to find the `failed` or `skipped` ones.

Additionally, the `payload_excerpt` field (a truncated snapshot of the raw webhook body, ≤ 512 bytes, already stored and returned by the API) is never surfaced in the UI. Admins must cross-reference external systems to understand what event caused a delivery outcome.

### Goals

- Let admins filter deliveries by one or more statuses without a page reload.
- Show the payload excerpt inline so the triggering context is visible without leaving the page.

### Non-Goals

- Full payload display (modal/side panel) — future iteration.
- Server-side or URL-driven filter — delivery volume does not yet justify API round-trips on filter changes.
- Adding new columns or changing the table schema.

---

## Decision Drivers

* **Minimal surface area**: The change should touch exactly one file (`triggers-page.tsx`) and introduce no new components.
* **Consistency with existing patterns**: `model-gateway/logs-tab.tsx` already uses `DataTable`'s `filter.mode = 'client'` API for status filtering — we should follow the same pattern.
* **No backend changes**: `payload_excerpt` already exists in the `TriggerDelivery` API type and in `DeliveryRow`-equivalent data; it just needs wiring through `toDeliveryRow` and the cell renderer.
* **Client-side is sufficient**: Deliveries are fetched server-side at page load and held in memory. Filtering a list of a few hundred rows client-side is instantaneous and requires no infrastructure.

---

## Considered Options

### Option A: Client-side filter on the Status column (chosen)

Wire the `filter` property on the `status` column definition in the `deliveryColumns` array. Use `mode: 'client'` with four options (one per status). The `DataTable` component manages the selection Set internally; no state is added to `TriggersPage`.

Extend `DeliveryRow` and `toDeliveryRow` to pass through `payload_excerpt`, then render it in the Detail cell after the existing priority chain (error → run ID → excerpt → "—").

**Pros:**
- Zero new components or state hooks in `TriggersPage`.
- Identical pattern to the inference logs status filter — no new conventions.
- No network round-trips on filter changes.
- The `DataTable` handles the "filter active" indicator and "Clear filter" affordance automatically.

**Cons:**
- Filter resets when the user navigates away and returns (page is server-rendered fresh each visit). Acceptable: delivery lists are live data, and a stale filter on an out-of-date dataset would be misleading anyway.

### Option B: Toolbar-level status toggle buttons

Add a set of pill-shaped toggle buttons above the table (one per status) to filter rows, similar to a segmented control.

**Pros:**
- More immediately visible than a column-header popover.

**Cons:**
- Requires a new UI pattern not present elsewhere in the codebase.
- Adds component-level state to `TriggersPage` (`useState` for the active set).
- Inconsistent with how the existing inference logs filter works.
- The DataTable's own `filteredEmptyMessage` and count logic would not integrate automatically.

### Option C: Server-side filter via URL search params

Navigate to `?status=failed` on filter selection, trigger a Next.js RSC re-render, and pass the filter to the BFF query.

**Pros:**
- Filter survives page refresh and is shareable via URL.

**Cons:**
- Requires BFF route and backend query changes to accept a `status` filter parameter.
- RSC re-render causes a loading skeleton flash on each filter toggle — poor UX for interactive exploration.
- Significantly more code for a feature whose delivery volume does not yet justify server-side pagination.

---

## Decision

**Option A — client-side column filter on the Status column.**

It is the lowest-friction, highest-consistency path. It reuses the DataTable API already used by the inference logs tab, requires no new components or state, and introduces no backend changes. The filter-resets-on-navigate trade-off is acceptable given that the page is server-rendered fresh on each visit.

The payload excerpt is threaded through `toDeliveryRow` (in `_types/index.ts`) and rendered in the Detail cell with the lowest priority in the existing chain: error > run ID > payload excerpt > "—".

---

## Consequences

### Positive

- Admins can isolate failed/skipped deliveries in one click.
- The triggering event context (payload excerpt) is visible inline, reducing context-switching.
- The change is contained to two files and follows established patterns.

### Negative

- Filter state is ephemeral (resets on navigation). If users find this limiting, Option C can be layered on later as a URL-driven progressive enhancement.
- The Detail column now potentially shows four different content types (run count, error, run ID, payload excerpt). The priority chain keeps it predictable but the column is doing more than before.

### Neutral

- No migration, no schema change, no backend deployment required.

---

## References

- PRD: [docs/prd/trigger-deliveries-filter.md](../prd/trigger-deliveries-filter.md)
- Existing filter pattern: `frontend/src/app/(dashboard)/model-gateway/_components/logs-tab.tsx`
- DataTable filter API: `frontend/src/packages/terra/src/components/table/data-table.tsx`
- Delivery status constants: `backend/internal/triggers/models.go`
