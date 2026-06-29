# DataTable Mobile Card Fallback - Product Requirements Document

**Status:** Approved  
**Created:** 2026-06-29  
**Last Updated:** 2026-06-29  
**Owner:** Platform Engineering  
**Stakeholders:** Frontend Engineering

---

## Executive Summary

Multi-column data tables are structurally incompatible with narrow viewports — there is no way to display 5+ columns at 375 px without either truncating content or forcing horizontal scroll that discards most of the data. This PRD defines a first-class Terra `DataTable` feature that renders a card-per-row list on mobile and the full table on desktop, eliminating the need for callsites to manually duplicate grid/card logic alongside table logic.

---

## Problem Statement

### Current State

`DataTable` renders an HTML `<table>` at all viewport sizes. On mobile, the table either overflows the viewport or the horizontal scroll hides most columns. The sticky first-column fix reduces the pain but does not solve it: secondary columns (status, timestamps, run counts) are still inaccessible without scrolling.

Several pages have worked around this by maintaining a parallel card/grid view alongside the DataTable, managed with local `viewMode` state and a layout toggle button. Two distinct patterns exist:

**Table-primary pages** (agents): The table is the primary information-dense view. A card grid was added later as a mobile workaround. The card view shows a subset of what the table shows.

**Card-primary pages** (policy roles, policy grants): The card grid *is* the primary desktop view and defaults to cards. The table is an opt-in alternative for power users. The card grid has its own independent filter pipeline (separate `search` and `typeFilter` state) that is already well-suited to mobile — these grids already collapse to 1 column (`cols={{ base: 1, sm: 2 }}`).

This PRD addresses the table-primary pattern only. The card-primary pattern (roles, grants) has a different and simpler mobile fix: hide the `ViewToggle` below `md` so users cannot accidentally switch into the table view on a phone. Migrating those pages to `mobileCard` would demote their card grid from a desktop-primary view to a mobile-only fallback, which is a product change, not a mobile fix.

### User Pain Points

- Mobile users cannot see agent status, run counts, or framework badges in the agents table without horizontal scrolling.
- The same problem affects every *table-primary* DataTable in the app: triggers, connections, logs, models.
- Engineers adding a new table must duplicate card and table rendering rather than writing one column definition that works everywhere.
- On card-primary pages (roles, grants), the layout toggle is visible on mobile, exposing users to a table view that is unusable at 375 px.

### Business Impact

- Orbit's primary use case as a production agent control plane includes on-call access from a phone. Tables that require a laptop to read undermine that use case.
- Every new table-primary DataTable callsite adds more card/table duplication unless there is a canonical solution.
- Card-primary pages (roles, grants) expose a broken table view to mobile users via an unguarded `ViewToggle`.

---

## Goals and Objectives

### Primary Goals

1. Add `mobileCard` and `mobileCardFooter` render props to `DataTable` for table-primary pages that need a mobile card fallback.
2. Remove the manual `viewMode` state and duplicate rendering from the agents page by migrating it to the new `mobileCard` prop.
3. Hide the `ViewToggle` below `md` on the card-primary policy pages (roles, grants) so mobile users cannot switch into the unusable table view.

### Success Metrics

- Zero horizontal overflow on any DataTable page at 375 px viewport width.
- The agents page has a single rendering path for its row data — no parallel card/table conditional.
- The roles and grants pages do not expose a table view on mobile.
- `pnpm lint` and `pnpm tsc --noEmit` pass with zero errors after migration.

### Non-Goals

- Retrofitting `mobileCard` to every existing DataTable callsite in this pass. The agents page is the only mandatory migration; all others are opt-in.
- Changing the desktop table layout or column visibility.
- A dedicated mobile toolbar (search input, column toggle) redesign. The existing toolbar is acceptable on mobile for now.

---

## User Stories

### Primary User Stories

**As an** on-call engineer on a phone,  
**I want to** see each agent's name, status, and active run count in a card list,  
**So that** I can assess the fleet at a glance without horizontal scrolling.

**Acceptance Criteria:**
- [ ] At 375 px viewport, the agents page renders one card per agent with name, status badge, framework badge, and active run count.
- [ ] Tapping a card navigates to the agent detail page.
- [ ] The search input in the DataTable toolbar filters the card list the same way it filters the table.

---

**As a** frontend engineer adding a new data-heavy page,  
**I want to** pass a single `mobileCard` render prop to `DataTable`,  
**So that** I get a correct mobile layout without writing parallel card and table rendering paths.

**Acceptance Criteria:**
- [ ] `DataTable` with `mobileCard` provided renders the card list below `md` and the table at `md` and above.
- [ ] `DataTable` without `mobileCard` behaves identically to today (table at all sizes).
- [ ] The card list respects the same filtered dataset as the table (search + column filters both apply).

---

**As a** mobile user on the policy roles or grants page,  
**I want to** see only the card grid view,  
**So that** I cannot accidentally switch into a table that is unusable at 375 px.

**Acceptance Criteria:**
- [ ] The `ViewToggle` on the roles and grants pages is not rendered below the `md` breakpoint.
- [ ] The card grid remains the default and only view on mobile; the table toggle remains accessible on desktop.

---

## Proposed Solution

### High-Level Approach

Add two optional props to `DataTableProps`:
- `mobileCard?: (row: TData) => React.ReactNode` — renders one card per data row.
- `mobileCardFooter?: React.ReactNode` — renders a single static node after the last card (e.g. a "Create new" affordance).

When `mobileCard` is provided, `DataTable` renders a `Stack` of `mobileCard(row)` outputs below the `md` breakpoint, optionally followed by `mobileCardFooter`, and the existing table above it. The toolbar (search, count, column toggle) is shown at all sizes. The card list operates on the same filtered and sorted dataset that the table uses, so search and client-side filters apply to both views without any additional wiring.

The switch between views uses Tailwind responsive visibility classes applied inside the Terra `DataTable` component (which is exempt from the `no-raw-classname` ESLint rule because it is inside Terra itself).

### User Experience

#### Mobile (< 768 px)

- Toolbar: search input spans full width. Column toggle and count label are shown.
- Card list renders below the toolbar. Each card is the caller-provided `mobileCard(row)` output.
- The layout toggle buttons (grid/list) are not shown — the card view is the only mobile layout.

#### Desktop (≥ 768 px)

- Existing table layout unchanged.
- If the callsite previously had a separate grid view toggle, that is removed in favour of the table-only desktop layout.

#### Agents page migration

- Remove `viewMode` state and the grid/list toggle buttons.
- Pass the existing card JSX (currently in the `viewMode === 'grid'` branch) as the `mobileCard` render prop.
- The desktop view becomes table-only (the card grid is mobile-only after migration).

### Key Components

- **`DataTable` (`src/packages/terra/src/components/table/data-table.tsx`)**: Add `mobileCard` and `mobileCardFooter` props; render card list below `md`, table above `md`.
- **`AgentsPage` (`src/app/(dashboard)/agents/_components/agents-page.tsx`)**: Remove `viewMode` state and toggle; pass agent card JSX as `mobileCard`.
- **`RolesTab` (`src/app/(dashboard)/policy/_components/roles-tab.tsx`)**: Wrap `ViewToggle` in `Show above="md"`.
- **`GrantsTab` (`src/app/(dashboard)/policy/_components/grants-tab.tsx`)**: Wrap `ViewToggle` in `Show above="md"`.

---

## Scope

### In Scope

- `DataTable` `mobileCard` and `mobileCardFooter` props (Terra change).
- Agents page migration to `mobileCard` (table-primary → card fallback on mobile).
- Hide `ViewToggle` below `md` on the policy roles and grants pages (card-primary → no accidental table on mobile).

### Out of Scope

- Migrating other table-primary DataTable callsites (triggers, connections, logs, models) — opt-in after this PR ships.
- Redesigning the card-primary pages (roles, grants) to use `mobileCard` — their card grid is already the correct mobile layout.
- Mobile toolbar redesign.
- Responsive chart heights.
- Per-callsite `defaultVisible` column hiding (Option B from the mobile responsive review).

### Future Considerations

- A `DataTableCard` Terra component with standardised card chrome (border, padding, header/footer slots) that `mobileCard` callers can use instead of assembling raw `Card` components.
- Migrating remaining DataTable callsites to use `mobileCard`.

---

## Requirements

### Functional Requirements

**Must Have (P0):**
- [ ] `DataTable` renders `mobileCard(row)` output in a `Stack` below the `md` breakpoint when `mobileCard` is provided.
- [ ] `DataTable` renders the existing table layout at `md` and above, unchanged.
- [ ] The card list is filtered and sorted by the same logic that drives the table (search query + client filters).
- [ ] The agents page uses `mobileCard` and `mobileCardFooter` and removes its manual `viewMode` state and toggle buttons.
- [ ] `DataTable` without `mobileCard` is unaffected — no behaviour change for existing callers.
- [ ] The `ViewToggle` on the policy roles and grants pages is hidden below the `md` breakpoint.

**Should Have (P1):**
- [ ] The card list is wrapped in a `TableContainer` so it shares the same border/background chrome as the table.
- [ ] The row click handler (`onRowClick`) works in the card list: tapping a card triggers navigation.
- [ ] An empty state is shown in the card list view when the filtered dataset is empty (same `emptyMessage` / `filteredEmptyMessage` props as the table uses).
- [ ] When the `error` prop is set, the error content is rendered in the card list view in place of the cards (same `error` prop as the table uses).
- [ ] An optional `mobileCardFooter?: React.ReactNode` prop renders a single static node after the last card. This allows callsites to append a "Create new" affordance or similar trailing item that is not tied to a data row.

### Non-Functional Requirements

**Performance:**
- CSS visibility toggle (not JS-driven re-mount) so the table DOM is not destroyed and recreated on resize.

**Accessibility:**
- Cards in the list are focusable and keyboard-activatable when `onRowClick` is provided.

---

## Dependencies

### Internal Dependencies

- Terra `Show` component (`src/packages/terra/src/components/show/`) — used to conditionally render card list vs table by breakpoint. Existing, no changes required.
- Terra `Stack` component — used to lay out the card list. Existing, no changes required.

---

## Alternatives Considered

### Alternative A: Horizontal scroll with sticky first column

Rely on `TableContainer`'s `overflow-x-auto` and the existing pinned first column. No new API surface.

**Why not chosen:** Horizontal scroll hides most columns on phones. Secondary data (status, timestamps) requires scrolling to reach, which is a poor experience for the primary use case (on-call triage on a phone).

### Alternative B: Column hiding via `defaultVisible`

Mark non-essential columns `defaultVisible: false` on mobile via breakpoint detection at each callsite.

**Why not chosen:** Still renders a table with 1-2 visible columns, which wastes vertical space on a narrow card-like layout. Also requires per-callsite auditing of column importance.

### Alternative D: `hideBelow` prop per column

Add a responsive `hideBelow` prop to `DataTableColumn` that applies `hidden md:table-cell` to cells.

**Why not chosen:** Same problem as B — still a table, just narrower. Does not address the fundamental incompatibility between dense multi-column tables and 375 px viewports.

---

## Related Documents

- [ADR — DataTable Mobile Card Fallback](../adr/2026-06-29-datatable-mobile-card-fallback.md)
- [PRD — Mobile & Tablet Responsive Design](2026-06-19-mobile-responsive-design.md)
- [ADR — Mobile & Tablet Responsive Design](../adr/2026-06-19-mobile-responsive-design.md)
