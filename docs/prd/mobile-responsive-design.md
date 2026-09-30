# Mobile & Tablet Responsive Design — Product Requirements Document

**Status:** Approved  
**Created:** 2026-06-19  
**Last Updated:** 2026-06-19  
**Owner:** Platform Engineering  
**Stakeholders:** Platform operators, on-call engineers, agent developers

---

## Executive Summary

kora's control plane dashboard is currently desktop-only. Engineers and operators who need to monitor agent runs, check policy status, or respond to incidents from a phone or tablet have no viable path. This PRD scopes the work required to make every screen in the kora dashboard fully usable on mobile (≥ 375 px) and tablet (≥ 768 px) viewports without breaking the existing desktop experience.

---

## Problem Statement

### Current State

The `PageLayout` component renders a fixed 240 px sidebar that is always visible. All page content is laid out assuming a desktop viewport of at least 1 024 px. Navigation items, data tables, KPI card grids, and the chat/run thread are not adapted to narrow widths. Viewing the dashboard on a phone produces horizontal overflow, overlapping elements, and inaccessible touch targets.

### User Pain Points

- **On-call response**: An engineer paged at 2 am wants to check active agent runs and policy violations from their phone. The dashboard is unusable at 390 px.
- **Tablet management**: A platform admin in a meeting wants to review inference logs on an iPad. Tables overflow horizontally and the sidebar occupies half the visible area.
- **Progressive disclosure**: Mobile users cannot dismiss or reveal the sidebar — they are stuck with the desktop layout regardless of viewport.
- **Touch targets**: Buttons and nav items are sized for pointer devices; touch targets below 44 × 44 px cause frequent mis-taps on mobile.

### Business Impact

- Operators cannot respond to incidents from mobile, increasing MTTR.
- Reduces perceived quality and enterprise readiness of the platform.
- Blocks kora from being used by teams where phones/tablets are common during on-call rotations.

---

## Goals and Objectives

### Primary Goals

1. Make the full dashboard shell usable on phones (375 px viewport and above).
2. Make the full dashboard shell comfortable on tablets (768 px viewport and above).
3. Preserve the existing desktop layout (1 024 px+) without regression.
4. Add a collapsible/drawer sidebar pattern driven by Terra components.

### Success Metrics

- All pages render without horizontal scroll on a 390 px iPhone 14 viewport.
- All interactive touch targets meet the WCAG 2.5.5 AAA minimum of 44 × 44 px. **(Deferred — icon-only buttons remain at Terra's 36 px `size="icon"`; bumping this is a Terra-wide change tracked as a follow-up. See the ADR Success Metrics note.)**
- All existing desktop Playwright/Storybook tests continue to pass.
- Lighthouse mobile score improves from current baseline.

### Non-Goals

- Native mobile application (iOS/Android).
- Offline support or PWA features.
- Mobile-specific push notifications.
- Redesigning the visual language or color system.
- Rewriting or replacing the Terra design system from scratch.

---

## User Stories

### Primary User Stories

**As an** on-call platform engineer,  
**I want to** open the kora dashboard on my phone and see active agent runs and any policy violations,  
**So that** I can assess severity and take action without needing a laptop.

**Acceptance Criteria:**
- [ ] The dashboard is navigable on a 390 px viewport without horizontal overflow.
- [ ] The sidebar is accessible via a hamburger menu on mobile and closes after navigation.
- [ ] Active runs, policy stats, and health indicators are readable without zooming.

---

**As a** platform admin working from a tablet,  
**I want to** browse inference logs, check key usage, and view KPI cards without panning,  
**So that** I can monitor the system during meetings without opening my laptop.

**Acceptance Criteria:**
- [ ] All data tables have horizontal scroll with sticky first column on narrow viewports.
- [ ] KPI cards reflow from 4-column to 2-column on tablet, 1-column on phone.
- [ ] The sidebar collapses to icon-only on tablet or to a drawer on phone.

---

**As an** operator deploying an agent from a phone,  
**I want to** tap the "New Run" button and complete the start-run modal,  
**So that** I can kick off an agent run without a desktop.

**Acceptance Criteria:**
- [ ] The start-run modal is fully usable at 390 px (form fields, dropdowns, submit).
- [ ] ~~All modal dialogs have at least 44 px touch targets for primary actions.~~ **(Deferred — Terra-wide touch-target review; icon buttons currently 36 px.)**

### Secondary User Stories

**As a** developer checking agent connectivity settings on a tablet,  
**I want to** navigate the agent detail tabs (Overview / Connectivity / Runs / Settings),  
**So that** I can review configuration without horizontal scrolling.

**Acceptance Criteria:**
- [ ] Tab bars wrap or scroll horizontally when they overflow their container.
- [ ] Tab content reflows to a single column below 768 px.

---

## Proposed Solution

### High-Level Approach

The solution is **progressive enhancement of the existing layout system** rather than a parallel mobile build. The four concrete changes are:

1. **`PageLayout` responsive sidebar** — add a `md:` breakpoint that shows the full sidebar on desktop/tablet and hides it behind a slide-in drawer on mobile. A hamburger toggle in the `DashboardHeader` opens/closes the drawer on small viewports.

2. **`DashboardHeader` mobile menu** — add a menu button on the left of the header (`md:hidden`) that toggles the sidebar drawer state. The existing breadcrumbs and action buttons remain, but collapse gracefully.

3. **Grid responsiveness** — audit every `<Grid cols={N}>` in the dashboard and ensure it carries a `base: 1` starting point (most already do). Hard-coded non-responsive column counts that would overflow at mobile are updated.

4. **Data tables mobile treatment** — the `DataTable` component already renders in a horizontally scrollable container (`TableRoot` carries `overflow-x-auto`). We make the locked column sticky (`sticky left-0`) so users can scroll right and still see what row they are on.

5. **Touch targets** — _Deferred._ Auditing and expanding icon-button sizes to a 44 px minimum is a Terra-wide change (every `size="icon"` button in the app) and is tracked as a follow-up rather than forked locally in this pass.

### User Experience

#### Mobile (< 768 px)
- The sidebar is hidden. A hamburger icon in the top-left of the header opens a full-height drawer overlay.
- Tapping a nav item navigates and closes the drawer.
- Page content fills the full width. KPI card grids stack 1-column.
- Data tables scroll horizontally with the first column sticky.
- Modals use bottom-sheet style on phone (full-width, anchored to bottom).

#### Tablet (768 px – 1 023 px)
- The sidebar collapses to a narrow icon-only rail (48 px wide). Labels are hidden.
- Hovering/tapping a nav item shows a tooltip with the label.
- KPI card grids go to 2 columns.
- Data tables are full-width with horizontal scroll.

#### Desktop (≥ 1 024 px)
- Full sidebar (240 px) with labels, exactly as today.

### Key Components

- **`PageLayout` (Terra Tier 1)**: Add responsive sidebar classes and a mobile drawer slot.
- **`DashboardHeader`**: Add hamburger button visible only on mobile; wire to sidebar context.
- **`MobileSidebarDrawer` (new Tier 3)**: Slide-in overlay sidebar for mobile.
- **`useSidebarState` hook (new Tier 3)**: Shared open/close state between the header toggle and drawer.
- **`DashboardSidebar`**: Existing component remains; reused inside the drawer for mobile.
- **Data tables**: Ensure `overflow-x-auto` wrapper and `sticky left-0` on the locked column.

---

## Scope

### In Scope

- `PageLayout` Terra component: responsive sidebar breakpoints + mobile drawer support.
- `DashboardHeader`: hamburger button (mobile only).
- All `(dashboard)` pages: grid column audit and responsive fix-ups.
- `DataTable` Terra component: horizontal scroll wrapper + sticky locked column.
- Modal components: ensure full-width layout on mobile.
- Touch target audit for all primary interactive elements.

### Out of Scope

- The `(chat)` layout — separate layout group, addressed in a follow-on PR.
- Auth/login pages — already single-column and mobile-friendly.
- Backend or API changes.
- New Terra components beyond what is strictly required.
- Advanced gestures (swipe to open sidebar).

### Future Considerations

- Bottom navigation bar on mobile as an alternative to the hamburger drawer.
- Swipe-to-open sidebar gesture.
- Responsive chart sizing (charts currently use fixed pixel heights).
- PWA manifest and install prompt.

---

## Requirements

### Functional Requirements

**Must Have (P0):**
- [ ] Dashboard renders without horizontal overflow at 375 px viewport width.
- [ ] Sidebar accessible on mobile via a toggle button in the header.
- [ ] Sidebar drawer closes on navigation on mobile.
- [ ] All KPI card grids respond to `base:1, sm:2, lg:4` or equivalent.
- [ ] All `DataTable` instances can scroll horizontally on mobile.
- [ ] Locked/first column in `DataTable` is sticky on horizontal scroll.

**Should Have (P1):**
- [ ] Tablet (768 px) sidebar collapses to icon-rail or stays full-width with labels.
- [ ] ~~All touch targets ≥ 44 × 44 px on mobile.~~ **(Deferred — Terra-wide touch-target review; icon buttons currently 36 px.)**
- [ ] Page header breadcrumbs truncate gracefully on narrow viewports.
- [ ] Modal dialogs are full-width on mobile.

**Nice to Have (P2):**
- [ ] Sidebar icon-rail tooltip on tablet.
- [ ] Bottom-sheet modal animation on phone.

### Non-Functional Requirements

**Performance:**
- No new JavaScript bundle weight > 5 kB gzipped for the responsive changes.
- No layout shift (CLS) regression on desktop from the responsive additions.

**Accessibility:**
- Sidebar drawer focusable and closeable with keyboard (Escape key).
- Hamburger button has `aria-label="Open navigation"` and `aria-expanded`.
- All focus traps within drawers and modals remain correct.

**Reliability:**
- All existing unit and integration tests pass unchanged.
- Responsive changes are verified against desktop, tablet (768 px), and phone (390 px) viewports.

---

## Dependencies

### Internal Dependencies

- **Terra design system** (`src/packages/terra/`): `PageLayout`, `Stack`, `Grid`, `DataTable`, `Modal` require modification or extension. Changes require human approval per the Terra contribution policy.
- **`DashboardHeader`** and **`DashboardSidebar`**: Client components wired together via a new context.

### External Dependencies

- Tailwind CSS v4 responsive prefixes (`sm:`, `md:`, `lg:`) — already in use.
- Radix UI Dialog — already used for modals; will be reused for the sidebar drawer.

---

## Risks and Mitigations

| Risk | Impact | Probability | Mitigation |
|------|--------|-------------|------------|
| Terra lint rules reject raw HTML in new components | High | High | Use Terra primitives (`Stack`, `Grid`, etc.) exclusively; no raw divs or nav elements |
| Sticky column in DataTable conflicts with existing shadow/z-index | Medium | Medium | Test across Chrome, Firefox, Safari; adjust z-index tokens if needed |
| Sidebar drawer adds re-render overhead on desktop | Low | Low | Drawer is conditionally rendered only at `< md` breakpoint; no cost on desktop |
| ESLint `no-terra-classname` blocks responsive Tailwind on Terra components | Medium | Medium | Use Terra's responsive props (Responsive<> types) — not className |
| Icon-rail sidebar changes Terra API surface area | Medium | Low | Implement icon-rail as a Tier 3 overlay, not modifying Terra Navigation directly |

---

## Alternatives Considered

### Alternative 1: Separate mobile layout

Build a completely separate `(mobile-dashboard)` layout route group rendered only when `user-agent` or `viewport` meta indicates mobile.

**Why not chosen:**
- Doubles the UI surface area to maintain.
- Server-side UA sniffing is fragile and SSR-incompatible with CDN caching.
- Does not benefit from the Terra design system's existing responsive primitives.

### Alternative 2: Third-party responsive sidebar library

Adopt a library such as shadcn-sidebar or cmdk for the mobile drawer.

**Why not chosen:**
- kora forbids raw HTML imports and has strict Terra-only JSX rules. Bringing in a third-party sidebar would require numerous eslint-disable exemptions or rewriting the library into Terra-compatible primitives — more work than the in-house solution.

---

## Open Questions

1. **Icon-rail sidebar on tablet**: Should 768 px show icon-only or full sidebar?
   - **Status:** Resolved — full sidebar at `md` (768 px), drawer at `sm` and below.
   - **Owner:** Platform Engineering

2. **Bottom-sheet modals on phone**: Is the current centered-modal pattern acceptable for P0?
   - **Status:** Resolved — centered modal with 100% width at `base` is P0-acceptable; bottom-sheet is P2.

---

## Timeline and Milestones

### Phases

**Phase 1: Foundation** (Target: 2026-06-19)
- PRD + ADR approved.
- `PageLayout` responsive sidebar + mobile drawer.
- `DashboardHeader` hamburger button.
- Grid column audits and fixes.

**Phase 2: Table & Modal Polish** (Target: 2026-06-26)
- DataTable horizontal scroll + sticky column.
- Modal full-width on mobile.
- Touch target audit complete.

### Key Milestones

- [x] PRD Approved: 2026-06-19
- [x] ADR Created: 2026-06-19
- [x] Implementation Started: 2026-06-19
- [ ] Alpha/Beta: 2026-06-20
- [ ] Launch: 2026-06-26

---

## Appendix

### Related Documents

- [ADR-2026-06-19](../adr/2026-06-19-mobile-responsive-design.md) — Architecture decision for responsive design implementation
- [Terra Design System Reference](../../frontend/docs/reference/terra-design-system.md)
- [Component Placement Guide](../../frontend/docs/reference/component-placement.md)

### Glossary

- **Responsive breakpoint**: A viewport width at which the layout changes. kora uses Tailwind's `sm` (640 px), `md` (768 px), `lg` (1 024 px), `xl` (1 280 px).
- **Sidebar drawer**: A slide-in overlay that covers page content on small viewports, dismissed by tapping outside or pressing Escape.
- **Icon-rail**: A narrow sidebar variant showing only icons (no text labels).
- **Sticky column**: A table column with `position: sticky` so it remains visible when the table body scrolls horizontally.

---

**Approval Signatures:**

- [x] Product Owner: Platform Engineering — 2026-06-19
- [x] Technical Lead: Platform Engineering — 2026-06-19
