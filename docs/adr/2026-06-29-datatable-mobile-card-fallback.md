# DataTable Mobile Card Fallback

<!-- Filename: 2026-06-29-datatable-mobile-card-fallback.md -->

**Status:** Accepted  
**Date:** 2026-06-29  
**Deciders:** Platform Engineering  
**Technical Story:** [PRD — DataTable Mobile Card Fallback](../prd/2026-06-29-datatable-mobile-card-fallback.md)

---

## Context

`DataTable` renders an HTML `<table>` at all viewport widths. On mobile (≤ 375 px) most columns are inaccessible without horizontal scrolling, even with the sticky first column fix delivered in the mobile responsive ADR (2026-06-19). The fundamental problem is structural: a 5-column table cannot fit in 375 px.

Several pages have worked around this by maintaining parallel card/grid views alongside DataTable with local `viewMode` state. Two structurally distinct patterns exist, and they require different fixes:

**Table-primary pages** (e.g. agents): The table is the primary view; the card grid was added as a mobile workaround. The `DataTable` needs a `grid` config prop so the card view is integrated into the component and the callsite no longer maintains two rendering paths.

**Card-primary pages** (e.g. policy roles, policy grants): The card grid *is* the primary and default desktop view. The table is an opt-in alternative for power users. The card grids already collapse to 1 column on mobile (`cols={{ base: 1, sm: 2 }}`). Their mobile problem is narrower: the `ViewToggle` is visible on mobile, so a user can accidentally switch to an unusable table view. The correct fix is to hide the toggle below `md` — not to migrate to `grid`, which would demote the card grid to a mobile-only fallback and change the desktop UX.

### Problem Statement

There is no canonical way to give a table-primary `DataTable` a mobile layout. Each callsite either accepts poor mobile UX (horizontal scroll) or maintains a bespoke card/grid view alongside the table. Additionally, card-primary pages expose a `ViewToggle` on mobile that leads users into an unusable table view.

### Goals

- Add a `grid?: DataTableGridConfig<TData>` prop to `DataTable` that renders a card grid on mobile (always) and on desktop when the user selects grid view via a toolbar toggle.
- Add an optional `grid.footer` node for static trailing content (e.g. a "Create new" affordance) not tied to a data row.
- Remove the hand-rolled grid/card view from the agents page in favour of the new prop.
- Hide the `ViewToggle` below `md` on the card-primary policy pages (roles, grants) so mobile users cannot switch into the unusable table view.
- Leave all existing `DataTable` callers unaffected (`grid` is optional).

### Non-Goals

- Retrofitting `mobileCard` to every existing DataTable callsite.
- A redesigned mobile toolbar.
- A standard `DataTableCard` chrome component (deferred).

---

## Decision Drivers

* **Terra-only JSX constraint** — product code cannot use raw HTML or `className` on Terra components. The implementation must live inside Terra (where the rule is exempt) or use Terra's existing prop API.
* **Single rendering path per callsite** — the agents page should not maintain parallel card and table rendering paths. `grid.card` must operate on the same filtered/sorted dataset the table uses.
* **Opt-in, non-breaking** — existing callers must be unaffected. Adding `grid` to `DataTableProps` as an optional prop satisfies this.
* **CSS visibility, not JS re-mount** — switching between card and table view on resize must use Tailwind responsive classes, not conditional rendering driven by a JS resize observer, to avoid layout thrash and unnecessary re-mounting.

---

## Considered Options

### Option 1: `grid` config prop on `DataTable` — CSS breakpoint switch

Add `grid?: DataTableGridConfig<TData>` to `DataTableProps`. When provided, render both the card grid and the table in the DOM simultaneously, with Tailwind responsive visibility classes controlling which is shown. Mobile always shows the grid; desktop shows whichever view the user has selected via the toolbar toggle (defaulting to `grid.defaultViewMode`). Both paths receive the same filtered/sorted row array.

**Pros:**
- No JS resize observer; the switch is pure CSS, so no layout thrash on window resize.
- The filtered dataset is naturally shared — both paths are driven by the same `rows` array from TanStack Table.
- Fully opt-in; callers without `grid` are unaffected.
- Consistent with how `Show` works elsewhere in Terra (static CSS visibility).

**Cons:**
- Both card and table DOM nodes are always mounted, even though only one is visible. For very large datasets this doubles the DOM node count. Acceptable given DataTable is a client-paginated component and typical row counts are ≤ 200.

### Option 2: `grid` config prop — JS resize observer switch

Same prop API, but use a `useMediaQuery('(min-width: 768px)')` hook to conditionally render either the card grid or the table. Only one is in the DOM at a time.

**Pros:**
- Half the DOM nodes compared to Option 1.

**Cons:**
- Requires a JS resize observer (or `window.matchMedia` listener) running inside `DataTable`. This adds a client-side effect to a component that currently has no resize-related side effects.
- On first paint, before the media query resolves, the component renders a loading/empty state or the wrong view — a flash of incorrect content.
- Harder to test: requires mocking `matchMedia` in Vitest.
- Inconsistent with the `Show` pattern already established in Terra.

### Option 3: Separate `DataTableMobile` component at each callsite

Do not change `DataTable`. Callers that want a mobile layout compose `DataTable` (desktop) and a custom card list (mobile) themselves, wrapped in `Show`.

**Pros:**
- No changes to Terra.

**Cons:**
- Perpetuates the duplication problem. Callsites must still maintain two rendering paths and manually wire the same filtered/sorted data to both.
- Not a reusable solution; each callsite solves it differently.

---

## Decision

**Chosen Option:** Option 1 — `grid` config prop with CSS breakpoint switch.

### Rationale

Option 1 is the correct tradeoff given this codebase's constraints:

1. **Consistency with `Show`.** Terra already uses static Tailwind responsive classes for breakpoint-driven visibility (`Show above="md"` renders `hidden md:block`). Using the same mechanism inside `DataTable` is coherent rather than introducing a second pattern.

2. **No flash of incorrect content.** CSS classes are applied at paint time; there is no first-render ambiguity about which view to show.

3. **Shared filtered dataset without extra wiring.** The card grid and table both receive `rows` from TanStack's `getRowModel()`, which already applies search and client-side filters. No separate filter state is needed for the card view.

4. **Double DOM is acceptable.** DataTable is always client-paginated and renders at most a few hundred rows. The cost of doubled DOM nodes is negligible compared to the complexity of a JS resize observer.

5. **Opt-in and non-breaking.** `grid` is optional. Every existing callsite continues to render identically.

---

## Consequences

### Positive

- A single `DataTable` call handles both mobile and desktop layout; no parallel rendering paths at callsites.
- The agents page sheds its `viewMode` state, layout toggle buttons, and duplicate card rendering branch.
- Future callsites get correct mobile layouts for free by passing `grid`.

### Negative

- When `grid` is provided, both the card grid and table DOM are always present. For tables with ≥ 200 rows this adds DOM overhead. Considered acceptable for now; Option 2 is available as a future optimisation if profiling shows a real cost.

### Neutral

- `DataTableProps` gains one optional prop. The existing TypeScript API is a strict superset.
- The agents page desktop layout now has a grid/list toggle, with grid as the default. This is intentional — the desktop card grid was a valid design choice preserved by the `grid` prop, not dropped.
- The roles and grants pages are unchanged on desktop. Their card grid remains the primary view at all sizes; only the `ViewToggle` is hidden on mobile.

---

## Implementation

### API surface change

```ts
// New exported interface (src/packages/terra/src/components/table/data-table.tsx):
export interface DataTableGridConfig<TData> {
  /**
   * Render a card for a row.
   *
   * DataTable wraps each rendered card in a focusable `div[role=button]`
   * when `onRowClick` is provided — do NOT add a top-level click handler to
   * the card itself, or both will fire.
   */
  card: (row: TData) => React.ReactNode;
  /**
   * Static node rendered after the last card in the grid.
   *
   * `onRowClick` does NOT wrap this node — attach click handling directly.
   */
  footer?: React.ReactNode;
  /** Column count for the grid. Defaults to `{ base: 1, sm: 2, lg: 3 }`. */
  cols?: GridProps['cols'];
  /**
   * Initial view mode on desktop. Defaults to `'grid'`. Set `'list'` to start
   * in table view. No-op on mobile — grid is always shown regardless.
   */
  defaultViewMode?: 'grid' | 'list';
}

// In DataTablePropsBase<TData>:
/** Card grid view. See {@link DataTableGridConfig}. */
grid?: DataTableGridConfig<TData>;
```

### Rendering structure (inside `DataTable`)

The card grid sits inside the existing `TableContainer` wrapper alongside the toolbar and table. No second `TableContainer` is added.

```tsx
{/* Grid view — visible on mobile always; on desktop only when viewMode='grid' */}
{grid && (
  <div className={viewMode === 'list' ? 'md:hidden' : undefined}>
    <div className="p-2">
      {error ? (
        <div className="px-2 py-4">{error}</div>
      ) : rows.length === 0 ? (
        <div className="px-2 py-4 text-sm text-muted-foreground">
          {hasActiveFilters ? filteredEmptyMessage : emptyMessage}
        </div>
      ) : (
        <Grid cols={grid.cols ?? { base: 1, sm: 2, lg: 3 }} gap="comfortable">
          {rows.map((row) => (
            <div
              key={getRowKey(row.original)}
              role={onRowClick ? 'button' : undefined}
              tabIndex={onRowClick ? 0 : undefined}
              aria-describedby={onRowClick ? rowDescId : undefined}
              onClick={
                onRowClick
                  ? (e) => { if (e.target === e.currentTarget) onRowClick(row.original); }
                  : undefined
              }
              onKeyDown={
                onRowClick
                  ? (e) => {
                      if ((e.key === 'Enter' || e.key === ' ') && e.target === e.currentTarget) {
                        e.preventDefault();
                        onRowClick(row.original);
                      }
                    }
                  : undefined
              }
            >
              {grid.card(row.original)}
            </div>
          ))}
          {!error && grid.footer}
        </Grid>
      )}
    </div>
  </div>
)}

{/* Table — always visible when grid is not provided; desktop-only when it is */}
<div className={grid ? (viewMode === 'grid' ? 'hidden' : 'hidden md:block') : undefined}>
  <TableRoot>…</TableRoot>
</div>
```

The `<div>` wrappers are inside `src/packages/terra/` and are therefore exempt from the `local/no-raw-layout-elements` ESLint rule.

### Agents page migration

1. Remove `viewMode` state and `setViewMode`.
2. Remove `Show above="md"` guard and the separator + toggle buttons from the page header.
3. Move the existing per-agent card JSX into a `renderAgentCard` function and pass it as `grid.card`.
4. Move the "Create new agent" dashed card JSX into a `grid.footer` node.
5. Pass `{ card: renderAgentCard, footer: newAgentCard }` as the `grid` prop to `DataTable`.
6. Remove the now-unused `Grid` / card grid branch.

The desktop view retains the grid/list toggle (provided automatically by `DataTable` when `grid` is present), with grid as the default.

### Files affected

| File | Change |
|------|--------|
| `src/packages/terra/src/components/table/data-table.tsx` | Add `DataTableGridConfig<TData>` interface and `grid` prop; render card grid (with error, empty, and footer handling) + table with responsive visibility; add desktop grid/list toggle to toolbar. |
| `src/app/(dashboard)/agents/_components/agents-page.tsx` | Remove `viewMode` state and toggle; pass agent card JSX as `grid.card` and "Create new agent" card as `grid.footer` to `DataTable`. |
| `src/app/(dashboard)/policy/_components/roles-tab.tsx` | Wrap `ViewToggle` in `Show above="md"`. |
| `src/app/(dashboard)/policy/_components/grants-tab.tsx` | Wrap `ViewToggle` in `Show above="md"`. |

---

## Validation

### Success Metrics

- `pnpm lint` passes with zero errors.
- `pnpm tsc --noEmit` passes with zero errors.
- At 375 px viewport, the agents page renders one card per agent with no horizontal overflow.
- At 768 px+ viewport, the agents page renders the card grid by default with a toggle to table view.
- All existing `DataTable` callers (no `grid`) render identically to before.

---

## Related Decisions

- [2026-06-19-mobile-responsive-design.md](2026-06-19-mobile-responsive-design.md) — Established the mobile-first responsive approach, sticky table column, and CSS-visibility breakpoint pattern this ADR extends.

---

## References

- [PRD — DataTable Mobile Card Fallback](../prd/2026-06-29-datatable-mobile-card-fallback.md)
- [Terra Design System Reference](../../frontend/docs/reference/terra-design-system.md)
- [Tailwind CSS v4 Responsive Design](https://tailwindcss.com/docs/responsive-design)

---

**Last Updated:** 2026-06-29
