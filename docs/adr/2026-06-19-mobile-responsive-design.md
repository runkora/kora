# Mobile & Tablet Responsive Design

<!-- Filename: mobile-responsive-design.md -->

**Status:** Accepted  
**Date:** 2026-06-19  
**Deciders:** Platform Engineering  
**Technical Story:** [PRD — Mobile & Tablet Responsive Design](../prd/mobile-responsive-design.md)

---

## Context

kora's control plane dashboard is built on Next.js 16 with the Terra design system (Tailwind v4 + Radix UI). The current shell assumes a desktop viewport: the `PageLayout` Terra component renders a fixed 240 px sidebar unconditionally, page grids assume ≥ 1 024 px, and data tables have no horizontal scroll container. The result is an unusable experience on phones and tablets.

### Background

Terra already ships `Responsive<T>` type support on `Stack`, `Grid`, and sizing props — the infrastructure for breakpoint-driven layout is in place. Tailwind v4's static scanner requires full string literals for responsive prefixes, which constrains how we can extend Terra's lookup tables. The ESLint rules (`local/no-raw-layout-elements`, `local/no-terra-classname`) prohibit raw HTML and `className` on Terra components in product code, so all responsive changes must go through Terra's prop API or be made inside Terra itself.

### Problem Statement

The dashboard is unusable at narrow viewports. On-call engineers cannot act from a phone; tablet users cannot browse logs without horizontal pan. The solution must not break the desktop layout, must respect the Terra-only JSX constraint, and must be achievable with minimal new abstractions.

### Goals

- Full-width, scroll-free layout at 375 px viewport.
- Comfortable layout on 768 px tablet.
- Sidebar accessible via a drawer toggle on mobile.
- Data tables horizontally scrollable with sticky locked column.
- All existing linting and type-checking passes.

### Non-Goals

- Native mobile app.
- Redesigning the visual language or Terra component API surface.
- Responsive chart height (charts retain their fixed pixel heights in this pass).

---

## Decision Drivers

* **Terra-only JSX constraint** — no raw `<div>` or `className` on Terra components in product code; all layout must go through Terra props.
* **Minimal Terra surface area change** — Terra changes require human approval; the bar is high and the scope should be tight.
* **No new third-party dependencies** — existing Radix UI `Dialog` is sufficient for the sidebar drawer.
* **Mobile-first Tailwind** — changes should follow Tailwind's mobile-first convention (`base` → `sm` → `md` → `lg`).
* **Zero desktop regression** — all Playwright and Storybook tests must remain green.

---

## Considered Options

### Option 1: Responsive sidebar entirely inside Terra `PageLayout`

Extend `PageLayout` with responsive Tailwind classes directly: hide the sidebar `<aside>` on `< md` breakpoints, add a `data-mobile-open` attribute toggled by a React context, and render the sidebar conditionally in a `<dialog>`-based overlay when `data-mobile-open` is true.

**Pros:**
- Single component owns the entire shell layout contract.
- No new component tiers needed for the drawer.
- Keeps `DashboardLayout` trivially thin.

**Cons:**
- `PageLayout` becomes a client component (needs `useState` for the open/closed drawer). It is currently a pure render function that works as a Server Component.
- Puts mobile-drawer concerns (event handlers, Radix Dialog portal) inside a Tier 1 Terra component, which must stay domain-agnostic.

### Option 2: Split the mobile drawer into a Tier 3 feature component, pass as a slot to `PageLayout`

`PageLayout` gets one new prop: `mobileDrawer?: React.ReactNode`. The dashboard layout assembles a `MobileSidebarDrawer` Tier 3 component and passes it. `PageLayout` applies the responsive `hidden md:flex` class to its `<aside>` so the sidebar is hidden on small viewports when a `mobileDrawer` is provided. (An earlier draft also considered a `mobileHeader` prop, but the existing `header` slot already collapses responsively via padding utilities, so no separate mobile header was needed.)

**Pros:**
- `PageLayout` stays a pure render function (Server Component compatible).
- The open/close state lives in a Tier 3 context/hook, not in Terra.
- Terra only grows by two optional props; the existing desktop API is unchanged.
- `DashboardSidebar` is reused inside the drawer without modification.

**Cons:**
- Slightly more files to coordinate (context, drawer component, header button).
- The dashboard `layout.tsx` becomes a client component to wire the sidebar-open context.

### Option 3: CSS-only responsive sidebar (no JavaScript drawer)

Use the `peer` Tailwind pattern with a hidden `<input type="checkbox">` to toggle the sidebar width between 0 and 240 px on small viewports. No client-side state.

**Pros:**
- Zero JavaScript overhead.
- Works without a context or `useState`.

**Cons:**
- Hidden checkbox is an accessibility anti-pattern; fails ARIA semantics for a navigation drawer.
- Cannot close the drawer on navigation (no event handler).
- The `<input>` and `<label>` are raw HTML elements that would trip ESLint (`local/no-raw-interactive-elements`, `local/no-raw-layout-elements`).

---

## Decision

**Chosen Option:** Option 2 — Split the mobile drawer into a Tier 3 component, pass as a slot to `PageLayout`.

### Rationale

Option 2 threads the needle between all constraints:

1. **Terra stays a pure render function.** `PageLayout` grows by one optional prop (`mobileDrawer`). It applies `hidden md:flex` to its sidebar `<aside>` when `mobileDrawer` is provided, and renders `mobileDrawer` alongside the aside. No `useState`, no event handlers, no client component promotion inside Terra.

2. **Open/close state is scoped to the dashboard.** A `useSidebarState` hook in `DashboardLayout` provides `{ isOpen, toggle, close }`. The hamburger button in `DashboardHeader` and the `MobileSidebarDrawer` both receive this state. When the user taps a nav link, `DashboardSidebar` calls `close()` via the context.

3. **DashboardSidebar is reused unchanged.** The same `<DashboardSidebar>` renders both inside the drawer (mobile) and in the normal `PageLayout` sidebar slot (md+). No duplication.

4. **Drawer uses Radix `Dialog` (already a dependency).** Correct focus trapping, `aria-modal`, Escape-to-close, and scroll-lock come for free.

5. **Grid fixes are non-invasive.** Most `<Grid cols={N}>` calls already carry `base:1` via the Terra responsive API. The handful of hard-coded non-responsive counts are updated to add a `base:1` tier.

6. **DataTable sticky column** is a Terra-level fix: the locked-column `<td>` gets `sticky left-0 bg-surface z-10` via the internal Tailwind class. This is inside Terra's own JSX (not product code) so the lint rule does not apply.

---

## Consequences

### Positive

- The entire dashboard is usable on mobile and tablet with minimal new code.
- `PageLayout` remains a Server Component; no performance regression on the shell render.
- `DashboardSidebar` has a single implementation reused on all viewports.
- Future drawers (e.g., mobile filter panels) can follow the same slot pattern.

### Negative

- `DashboardLayout` promotes to a client component to host the sidebar context. This is a shallow promotion — the layout itself is simple; it just wraps children in a provider.
- Two additional files: `use-sidebar-state.tsx` (hook) and `mobile-sidebar-drawer.tsx` (Tier 3 component).

### Neutral

- `PageLayout` grows two optional props; the existing API is a strict superset and fully backwards-compatible.
- The hamburger button is `md:hidden`, so desktop users never see it.

---

## Implementation

### Components affected

| Component | Location | Change |
|-----------|----------|--------|
| `PageLayout` | `src/packages/terra/.../page-layout/page-layout.tsx` | Add `mobileDrawer?: React.ReactNode` prop; render it alongside the `<aside>` and apply `hidden md:flex` to the `<aside>` when `mobileDrawer` is provided. |
| `DashboardLayout` | `src/app/(dashboard)/layout.tsx` | Add `'use client'`; wrap with `SidebarStateProvider`. |
| `DashboardHeader` | `src/app/(dashboard)/_components/dashboard-header.tsx` | Add hamburger `Button` (icon, `md:hidden`) on the left; wire to `useSidebarState`. |
| `MobileSidebarDrawer` | `src/app/(dashboard)/_components/mobile-sidebar-drawer.tsx` | New Tier 3 component. Radix `Dialog` full-height left drawer. Renders `SidebarBrandHeader` + `DashboardSidebar` + `SidebarUserFooter`. |
| `useSidebarState` | `src/app/(dashboard)/_components/sidebar-state.tsx` | New React context + hook. `{ isOpen, open, close, toggle }`. |
| `DashboardSidebar` | `src/app/(dashboard)/_components/dashboard-sidebar.tsx` | Accept optional `onNavigate?: () => void` prop; call it when a nav item is clicked so the mobile drawer can close. |
| `DataTable` | `src/packages/terra/.../table/data-table.tsx` | Wrap table in `overflow-x-auto`; apply `sticky left-0 bg-surface z-10` to locked-column cells. |
| KPI/metric grids | Multiple page components | Audit all `<Grid cols={N}>` — add `base:1` tier if missing. |

### Breakpoint convention

kora uses Tailwind's default breakpoints:

| Token | Viewport |
|-------|----------|
| `base` | 0 px+ (mobile) |
| `sm` | 640 px+ |
| `md` | 768 px+ (tablet) |
| `lg` | 1 024 px+ (desktop) |

The sidebar is visible at `md:flex` (768 px and above) and hidden at base (below 768 px). This matches the PRD's decision: "full sidebar at md, drawer at sm and below."

### Migration Path

No migration required. Existing callers of `PageLayout` without the new prop are unaffected — `mobileDrawer` is optional and defaults to `undefined`.

### Timeline

Phase 1 (2026-06-19): All changes above in a single PR.

---

## Validation

### Success Metrics

- `pnpm lint` passes with zero errors.
- `pnpm tsc --noEmit` passes with zero errors.
- All existing Vitest unit and integration tests pass.
- Dashboard loads without horizontal scroll at 390 × 844 px (iPhone 14) in Chrome DevTools device emulation.
- Primary interactive elements use Terra's standard control sizes. **Note:** icon-only
  buttons (hamburger, drawer close, header refresh) render at Terra's `size="icon"`
  (36 × 36 px), which is below the WCAG 2.5.5 AAA target of 44 × 44 px the PRD aspired
  to. Bumping the `icon` size is a Terra-wide change affecting every icon button in the
  app, so it is **explicitly deferred** out of this pass rather than forked locally.
  Tracked as a follow-up for a dedicated Terra touch-target review.

### Monitoring

- Playwright E2E tests should be added to cover: (a) hamburger opens drawer, (b) nav tap closes drawer, (c) desktop layout unchanged. This is a Phase 2 item.

---

## Related Decisions

- [2026-04-14-frontend-policy-projection.md](2026-04-14-frontend-policy-projection.md) — Established the `PermissionsProvider` context pattern that this ADR extends for `SidebarStateProvider`.

---

## References

- [PRD — Mobile & Tablet Responsive Design](../prd/mobile-responsive-design.md)
- [Terra Design System Reference](../../frontend/docs/reference/terra-design-system.md)
- [Tailwind CSS v4 Responsive Design](https://tailwindcss.com/docs/responsive-design)
- [Radix UI Dialog](https://www.radix-ui.com/primitives/docs/components/dialog)

---

**Last Updated:** 2026-06-19
