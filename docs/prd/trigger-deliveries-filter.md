# Trigger Deliveries Filter and Event Detail — Product Requirements Document

**Status:** Approved  
**Created:** 2026-06-20  
**Last Updated:** 2026-06-20  
**Owner:** Platform Team  
**Stakeholders:** Platform Admins, SREs debugging webhook delivery pipelines

---

## Executive Summary

The Triggers → Deliveries tab lists every inbound webhook delivery but offers no way to isolate deliveries by status. As volume grows, admins need to filter to "failed" or "skipped" deliveries without scrolling through hundreds of "dispatched" rows. We also surface the first bytes of the raw webhook payload so admins can understand exactly what event triggered (or failed to trigger) a run, without leaving the page.

---

## Problem Statement

### Current State

The Deliveries table shows all deliveries in a single unfiltered list sorted by received time. It displays: received timestamp, event type, status badge, source name, and a brief detail field (run ID / error / run count).

### User Pain Points

- **Signal-to-noise ratio**: On active webhook sources, "dispatched" deliveries dominate the list. Admins debugging a broken pipeline must scan visually for "failed" or "skipped" rows.
- **No quick-scan filter**: There is no way to toggle to "show only failed" or "show only skipped" deliveries without reloading a filtered URL.
- **Opaque triggering events**: The table shows the event type label (e.g. `push`) but not what was in the payload — admins must correlate with GitHub UI or logs to understand what the event contained.

### Business Impact

- Debugging time increases linearly with delivery volume; without filtering, on-call SREs spend time scrolling rather than triaging.
- Incomplete context (no payload excerpt) forces context-switching to external tools to diagnose a skipped or failed delivery.

---

## Goals and Objectives

### Primary Goals

1. Let admins filter the Deliveries table to one or more statuses (`verified`, `dispatched`, `skipped`, `failed`) in a single click.
2. Display a short payload excerpt in the Detail column so the triggering event context is visible without navigating away.

### Success Metrics

- A platform admin can filter to "failed + skipped" deliveries in ≤ 2 clicks.
- The payload excerpt (≤ 120 chars) is visible inline in the Detail column whenever the backend returns one.
- The filter state is client-side — no network round-trip or URL change required.
- Zero regressions in the Sources and Triggers tabs.

### Non-Goals

- Server-side / URL-driven status filtering (out of scope for v1 — client-side filter is sufficient for the expected delivery volume).
- Full payload display (a modal or side panel for the complete payload is a future item).
- Filtering by event type, source, or date range (future iterations).
- Backend changes (the `payload_excerpt` field already exists in the API; no schema or handler changes needed).

---

## User Stories

### Primary User Stories

**As a** platform admin,  
**I want to** filter the Deliveries list by status (verified / dispatched / skipped / failed),  
**So that** I can immediately find failed or skipped deliveries without scrolling through successful ones.

**Acceptance Criteria:**
- [ ] A filter control appears on the Status column header in the Deliveries table.
- [ ] Selecting one or more statuses hides non-matching rows instantly (client-side).
- [ ] Selecting multiple statuses shows rows matching any of the selected statuses (OR semantics).
- [ ] A "Clear filter" action resets the filter to show all rows.
- [ ] When the filter produces zero rows, the table shows the existing `filteredEmptyMessage`.
- [ ] Filter options are labelled to match the badge labels: "Verified", "Dispatched", "Skipped", "Failed", each with a status-tone colour dot.

---

**As a** platform admin,  
**I want to** see the first bytes of the webhook payload inline in the Deliveries table,  
**So that** I can understand what event caused a delivery outcome without leaving the page.

**Acceptance Criteria:**
- [ ] When `payload_excerpt` is present on a delivery, it is shown in the Detail column (truncated to fit the cell).
- [ ] Payload excerpt is styled as monospace text, colour "muted", to distinguish it from IDs.
- [ ] When both `error` and `payload_excerpt` are present, the error takes priority (errors are more actionable).
- [ ] When neither is present, the existing fallback (run ID or "—") is unchanged.
- [ ] The Detail column header is unchanged; no new column is added.

---

## Constraints and Assumptions

- The `payload_excerpt` field is already returned by the BFF — no backend changes are required.
- The DataTable component supports client-side column filters via the `filter.mode = 'client'` API.
- Status values are the four constants defined in the backend: `verified`, `dispatched`, `skipped`, `failed`.
- The feature is purely presentational — no new API calls, no URL state.

---

## Out of Scope

- Expanding a delivery row to show the full payload.
- Per-trigger fan-out detail in the Deliveries table (already handled via the `runs` field and the Detail column).
- Any backend or database changes.
