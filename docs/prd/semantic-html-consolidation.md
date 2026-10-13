# Semantic HTML Consolidation — header, article, nav, aside, footer

**Status:** Implemented
**Created:** 2026-04-28
**Last Updated:** 2026-04-28
**Owner:** Frontend
**Stakeholders:** Engineering
**Authoritative ADR:** [docs/adr/2026-05-04-semantic-html-consolidation.md](../adr/2026-05-04-semantic-html-consolidation.md)

---

## Executive Summary

The kora dashboard frontend currently contains 53 raw `<header>`, `<article>`, `<nav>`, `<aside>`, and `<footer>` elements in product code (`src/app/**` and `src/components/**`). The existing ESLint rule (`no-raw-layout-elements`) only bans these elements conditionally — when they carry Tailwind layout-mode classes — leaving a significant gap in design-system enforcement. This PRD defines the work to eliminate all 53 instances and upgrade the lint rule to enforce an unconditional ban, consistent with the treatment already applied to `<div>` and `<section>`.

---

## Problem Statement

### Current State

[frontend-component-consolidation](../adr/2026-05-04-frontend-component-consolidation.md) established that raw layout HTML is banned in product code. `<div>` and `<section>` are banned unconditionally. Five additional semantic elements — `<header>`, `<article>`, `<nav>`, `<aside>`, and `<footer>` — are currently only banned when they carry layout-mode Tailwind classes (`flex`, `grid`, `block`, etc.). A systematic audit finds **53 instances** that bypass this rule today:

| Tag | Count | Primary use patterns |
|-----|-------|----------------------|
| `<aside>` | 24 | Info callout boxes (×8), slide panel shell (×5), layout containers (×11) |
| `<article>` | 13 | Prose/content containers (×8), interactive cards (×3), stat rows (×2) |
| `<header>` | 9 | Toolbar skeletons (×2), panel/modal headers (×3), section headings (×4) |
| `<nav>` | 3 | Sidebar navigation (×1), breadcrumb (×1), suggestion chips (×1) |
| `<footer>` | 4 | Chat input bar (×2), sidebar user/logout bar (×1), slide panel footer (×1) |

### User Pain Points

- **Developers** have no sanctioned Hades alternative for info callout boxes, slide panels, or semantic nav — so they reach for raw HTML.
- **Reviewers** cannot catch misuse because the lint rule only fires on layout-class presence, not element identity.
- **AI coding agents** see these raw elements as precedent and reproduce them in generated code.

### Business Impact

- Partial enforcement erodes confidence in the Hades rule system.
- Semantic HTML misuse (`<article>` for a stat row, `<aside>` for a scrollable list) degrades the document model without accessibility benefit.
- Missing patterns (`Callout`, `SidePanel`) are recreated inline each time a new feature is built.

---

## Goals and Objectives

### Primary Goals

1. Zero raw `<header>`, `<article>`, `<nav>`, `<aside>` elements in `src/app/**` and `src/components/**`.
2. The `no-raw-layout-elements` ESLint rule bans all four elements unconditionally.
3. Every migrated instance uses a component at the correct tier — no inline workarounds.

### Success Metrics

- `grep -rn "<header\b\|<article\b\|<nav\b\|<aside\b\|<footer\b" frontend/src/app frontend/src/components` returns zero results.
- `pnpm lint` passes with zero errors.
- `pnpm tsc --noEmit` passes with zero errors.
- Every new Hades component has a Storybook story.

### Non-Goals

- Visual redesign of any migrated component.
- Banning `<main>` — it is rendered exclusively by Hades' `PageLayout` and has no product-code instances.
- Changes to Hades source internals (Hades is deliberately exempt from product-code rules).

---

## Proposed Solution

### New Hades Components (Tier 1)

Four new components provide compliant alternatives for patterns that currently have no Hades equivalent.

**`Callout` (`Hades/Atoms/Callout`)** — Informational callout box. Two variants: `info` (accent tint) and `warning` (degraded tint). Replaces 8 info-box `<aside>` instances and 1 warning-banner `<aside>`. Accepts `children` and `variant`. Renders `<aside>` internally.

**`TableToolbarShell` (`Hades/Molecules/Table`)** — The bare toolbar container that `TableToolbar` renders internally. Accepts arbitrary `children`. `TableToolbar` is refactored to compose on top of it, guaranteeing both always share identical padding and border tokens. Replaces `<header>` in 2 loading skeleton files.

**`Navigation` (`Hades/Layout/Navigation`)** — A semantic `<nav>` wrapper for the primary sidebar navigation list. Accepts `children` (a list of `NavItem` elements) and an optional `aria-label`. Renders `<nav>` internally. Replaces the raw `<nav>` in `dashboard-sidebar.tsx`.

**`SidePanel` (`Hades/Molecules/SidePanel`)** — A Radix `Dialog`-based slide-over panel from the right edge. Provides `SidePanelHeader`, `SidePanelBody`, and `SidePanelFooter` sub-components. Handles backdrop, Escape-to-close, focus trap, and ARIA automatically via Radix. Replaces 5 raw elements in `editor-panel.tsx`. Renders `<aside>` for the panel shell internally.

**`PageLayout.sidebarFooter` prop extension** — Rather than a standalone `Footer` component, `PageLayout` gains an optional `sidebarFooter?: React.ReactNode` prop. When provided, `PageLayout` renders it as a semantically correct `<footer>` pinned to the bottom of the sidebar `<aside>` shell. When omitted the sidebar renders exactly as today. `dashboard-sidebar.tsx` is updated to pass its user/logout strip via this prop rather than using a raw `<footer>` element.

### ESLint Rule Upgrade

The `no-raw-layout-elements` rule is extended to ban all five elements unconditionally, matching the existing treatment of `<div>` and `<section>`. Five new message IDs are added pointing to the correct Hades replacement.

### Migration Strategy for Remaining 34 Instances

- Layout containers with no semantic value → `Stack` (drop the raw element)
- Interactive content cards → Hades `Card` with `interactive` prop
- Progress bar wrappers → Hades `Progress` (already exists)
- Warning/error banners inside modals → Hades `Alert`
- Breadcrumb `<nav>` → `Stack` with `aria-label="Breadcrumb"`
- Suggestion chips `<nav>` in chat → `Grid` (not genuinely navigation)

---

## Complete Instance Inventory

### `<header>` (9 instances)

| File | Line | Purpose | Replacement |
|------|------|---------|-------------|
| `app/(dashboard)/_components/dashboard-sidebar.tsx` | 28 | Sidebar logo bar | `PageLayout` renders `<header>` internally — drop raw usage |
| `app/(dashboard)/_components/dashboard-header.tsx` | 14 | Top nav bar | Same |
| `app/(dashboard)/policy/_components/vocabulary-tab.tsx` | 175 | Scope resource-type group heading | `Stack` |
| `app/(dashboard)/model-gateway/catalog/_components/editor-panel.tsx` | 96 | Slide panel header | `SidePanelHeader` |
| `app/(dashboard)/model-gateway/logs/loading.tsx` | 23 | Toolbar skeleton | `TableToolbarShell` |
| `app/(dashboard)/model-gateway/models/loading.tsx` | 26 | Toolbar skeleton | `TableToolbarShell` |
| `app/(dashboard)/model-gateway/_components/token-created-modal.tsx` | 50 | Custom modal header | `ModalAccessibleTitle` + `Stack` |
| `app/(dashboard)/model-gateway/_components/setup-tab.tsx` | 80 | Step title | `Stack` |
| `app/(dashboard)/model-gateway/_components/setup-tab.tsx` | 172 | Info box header | `Stack` |

### `<article>` (13 instances)

| File | Line | Purpose | Replacement |
|------|------|---------|-------------|
| `app/(dashboard)/chat/page.tsx` | 440 | Main chat content wrapper | `Stack` |
| `app/(dashboard)/chat/_components/message-content.tsx` | 30 | Message prose container | `Stack` (drop asChild) |
| `app/(dashboard)/policy/_components/evaluate-tab.tsx` | 257 | Role info card | `Card` |
| `app/(dashboard)/policy/_components/evaluate-tab.tsx` | 287 | Grant info card | `Card` |
| `app/(dashboard)/policy/_components/evaluate-tab.tsx` | 313 | Empty state callout | `Stack` |
| `app/(dashboard)/policy/_components/evaluate-tab.tsx` | 443 | Effective scopes container | `Stack` |
| `app/(dashboard)/model-gateway/catalog/_components/editor-panel.tsx` | 195 | Scrollable editor body | `SidePanelBody` |
| `app/(dashboard)/policy/_components/vocabulary-tab.tsx` | 172 | Resource type grouping | `Stack` |
| `app/(dashboard)/model-gateway/usage/_components/stat-row.tsx` | 18 | Stat row container | `Stack` (drop asChild) |
| `app/(dashboard)/model-gateway/_components/model-card-grid.tsx` | 30 | Interactive model card | `Card interactive` |
| `app/(dashboard)/model-gateway/_components/setup-tab.tsx` | 79 | Setup step container | `Stack` |
| `app/(dashboard)/model-gateway/_components/keys-tab.tsx` | 69 | Key card with conditional opacity | `Stack` |
| `src/components/scope-picker.tsx` | 97 | Scope group accordion item | `Stack` |

### `<nav>` (3 instances)

| File | Line | Purpose | Replacement |
|------|------|---------|-------------|
| `app/(dashboard)/_components/dashboard-sidebar.tsx` | 42 | Primary sidebar nav list | `Navigation` |
| `app/(dashboard)/_components/dashboard-header.tsx` | 17 | Breadcrumb trail | `Stack aria-label="Breadcrumb"` |
| `app/(dashboard)/chat/page.tsx` | 463 | Suggestion chips grid | `Grid` |

### `<footer>` (4 instances)

| File | Line | Purpose | Replacement |
|------|------|---------|-------------|
| `app/(dashboard)/chat/page.tsx` | 549 | Chat input bar outer shell | `Stack` (drop asChild) |
| `app/(dashboard)/chat/page.tsx` | 574 | Chat input card bottom toolbar | `Stack` (drop asChild) |
| `app/(dashboard)/_components/dashboard-sidebar.tsx` | 87 | Sidebar user/logout bar | `PageLayout sidebarFooter` prop |
| `app/(dashboard)/model-gateway/catalog/_components/editor-panel.tsx` | 212 | Slide panel footer | `SidePanelFooter` |

### `<aside>` (24 instances)

| File | Line | Purpose | Replacement |
|------|------|---------|-------------|
| `app/(dashboard)/_components/dashboard-sidebar.tsx` | 25 | Sidebar shell | `Stack` (drop asChild) |
| `app/(dashboard)/policy/_components/routes-tab.tsx` | 125 | Info callout | `Callout` |
| `app/(dashboard)/policy/_components/routes-tab.tsx` | 150 | Info callout | `Callout` |
| `app/(dashboard)/policy/_components/vocabulary-tab.tsx` | 104 | Info callout | `Callout` |
| `app/(dashboard)/policy/_components/grants-tab.tsx` | 155 | Info callout | `Callout` |
| `app/(dashboard)/policy/_components/grants-tab.tsx` | 271 | Grant details preview | `Card` |
| `app/(dashboard)/policy/_components/grants-tab.tsx` | 655 | Scrollable list container | `Stack` |
| `app/(dashboard)/policy/_components/grants-tab.tsx` | 657 | List item | `Stack` |
| `app/(dashboard)/policy/_components/evaluate-tab.tsx` | 165 | Info callout | `Callout` |
| `app/(dashboard)/policy/_components/overview-tab.tsx` | 185 | Progress bar background | `Progress` |
| `app/(dashboard)/policy/_components/overview-tab.tsx` | 186 | Progress bar fill | `Progress` (absorbed) |
| `app/(dashboard)/policy/_components/roles-tab.tsx` | 118 | Info callout | `Callout` |
| `app/(dashboard)/model-gateway/catalog/_components/editor-panel.tsx` | 87 | Modal backdrop | `SidePanel` (internal) |
| `app/(dashboard)/model-gateway/catalog/_components/editor-panel.tsx` | 92 | Slide panel shell | `SidePanel` |
| `app/(dashboard)/model-gateway/catalog/_components/editor-panel.tsx` | 121 | Form section | `Stack` + `Separator` |
| `app/(dashboard)/model-gateway/catalog/_components/editor-panel.tsx` | 181 | Grid colspan helper | `Stack` |
| `app/(dashboard)/model-gateway/catalog/_components/editor-panel.tsx` | 200 | Error container | `Alert` |
| `app/(dashboard)/model-gateway/_components/model-detail-modal.tsx` | 113 | Status strip | `Stack` + `Separator` |
| `app/(dashboard)/model-gateway/_components/token-created-modal.tsx` | 64 | Warning banner | `Alert variant="warning"` |
| `app/(dashboard)/model-gateway/_components/setup-tab.tsx` | 171 | Info callout | `Callout` |
| `src/components/scope-picker.tsx` | 82 | Scrollable accordion wrapper | `Stack` |
| `src/components/scope-picker.tsx` | 84 | Empty state message | `Stack` |
| `src/components/scope-picker.tsx` | 126 | Group body container | `Stack` |
| `app/(dashboard)/chat/page.tsx` | 446 | Chat welcome screen | `Stack` |

---

## Functional Requirements

**Must Have (P0):**
- [x] `Callout` added to Hades with `info` and `warning` variants; all callout `<aside>` instances replaced ✅
- [x] `TableToolbarShell` added to Hades; `TableToolbar` refactored to compose on top; toolbar skeleton `<header>` instances replaced ✅
- [x] `Navigation` added to Hades; sidebar `<nav>` replaced ✅
- [x] `SidePanel` added to Hades with `SidePanelHeader`, `SidePanelBody`, `SidePanelFooter`; all `editor-panel.tsx` raw elements replaced ✅
- [x] `PageLayout.sidebarFooter` prop added; sidebar user/logout `<footer>` migrated ✅
- [x] `no-raw-layout-elements` rule upgraded to unconditionally ban all five elements with new message IDs ✅
- [x] All 53 instances removed; `pnpm lint` passes with zero errors ✅
- [x] All new Hades components have Storybook stories exported from the Hades barrel ✅

**Should Have (P1):**
- [ ] `hades-design-system.md` quick-reference table updated with all five newly banned elements
- [ ] Golden-path verification checklist updated
- [ ] `no-hades-disable-comment` rule updated to include the five new message IDs

---

## Scope

### In Scope
- All 49 instances across 18 files in `src/app/**` and `src/components/**`
- `no-raw-layout-elements` rule upgrade in `frontend/eslint.config.mjs`
- 4 new Hades Tier 1 components with full [terra-frontend-design-system](../adr/2026-03-25-terra-frontend-design-system.md) authoring conventions
- Golden-path doc update

### Out of Scope
- `<footer>` and `<main>` — handled by existing patterns
- Hades internal source — deliberately exempt
- Visual changes to any migrated component

---

## Dependencies

- `@radix-ui/react-dialog` — already installed; `SidePanel` reuses it.
- Hades authoring conventions: `data-slot`, `React.forwardRef`, `cn`, `cva` per [terra-frontend-design-system](../adr/2026-03-25-terra-frontend-design-system.md).
- `pnpm lint` must continue to pass throughout migration (phased approach).

---

## Implementation Phases

| Phase | Work |
|-------|------|
| 0 | Write PRD + ADR (this document) |
| 1 | New Hades components: `Callout`, `TableToolbarShell`, `Navigation`, `SidePanel` + Storybook + barrel |
| 2 | ESLint rule upgrade — unconditional ban on all four elements |
| 3 | Migrate all `<aside>` instances (24) |
| 4 | Migrate all `<article>` instances (13) |
| 5 | Migrate all `<header>` instances (9) |
| 6 | Migrate all `<nav>` instances (3) |
| 7 | Migrate all `<footer>` instances (4) |
| 8 | Update golden-path docs |


---

## Phase 2 — Form and List Element Consolidation (2026-04-29)

### Problem

After Phase 1 banned layout and structural HTML elements, AI coding agents were observed using `<ul>`, `<ol>`, `<li>` as layout containers and `<label>`, `<input>` as inline element substitutes — effectively re-opening the loopholes Phase 1 closed.

### Scope

**Elements to ban (unconditionally):** `<ul>`, `<ol>`, `<li>`, `<form>`, `<label>`, `<input>`, `<output>`

### New Hades components

| Component | Description |
|-----------|-------------|
| `<Form>` | Wraps `<form>`; props: `onSubmit`, `noValidate`, `gap` (SpacingSize), `maxWidth` (ContainerWidth) |
| `<Checkbox>` | Wraps `<input type="checkbox">` + `<label>`; supports `indeterminate`, `variant="row"` |
| `<List>` | Wraps `<ul>` / `<ol>`; props: `ordered`, `gap`, `separator` |
| `<ListItem>` | Wraps `<li>`; children own layout |
| `<Output>` | Wraps `<output>`; `field` prop applies input-like styling |

### Acceptance criteria

- `no-raw-list-elements` and `no-raw-form-elements` ESLint rules are active and error-level.
- Both rules are in the `no-hades-disable-comment` guard set.
- Zero instances of `<ul>`, `<ol>`, `<li>`, `<form>`, `<label>`, `<input>`, `<output>` remain in `src/app/**` or `src/components/**`.
- All five new Hades components are exported from the `@orbit/hades` barrel.
- `make check` passes with no lint errors.

### Implementation status

| Step | Status |
|------|--------|
| New Hades components (`Form`, `Checkbox`, `List`, `ListItem`, `Output`) | ✅ Complete (2026-04-29) |
| ESLint rules (`no-raw-list-elements`, `no-raw-form-elements`) | ✅ Complete (2026-04-29) |
| Migrate all product-code instances | ✅ Complete (2026-04-29) |
| Update [terra-design-system-hardening](../adr/2026-05-04-terra-design-system-hardening.md) | ✅ Complete (2026-04-29) |
