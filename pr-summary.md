# PR #42 — Terra Design System Uplift & Frontend Lint Rules

**Branch:** `feat/Uplift-Frontend-Lint-Rules`
**Stats:** ~22,360 additions / ~6,925 deletions / 193 changed files / 36 commits
**Scope:** Frontend only (`frontend/`, `docs/`)

---

## Overview

This PR hardens the Orbit frontend's design-system boundary end-to-end. It does three things in concert:

1. **Extends Terra** — 18 new components, hardened prop APIs across 15 existing components, Storybook coverage for the full surface.
2. **Closes every raw-HTML escape hatch** — 8 custom ESLint rules now unconditionally ban every structural, inline, interactive, list, and form HTML element in product code, with a self-sealing rule that prevents suppression via disable comments.
3. **Migrates every violation** — all 72 product-code files that previously used raw HTML or bypassed the design system are updated to use the correct Terra primitive.

The end state: `make check` passes with zero lint errors, zero TypeScript errors, and zero raw HTML in `src/app/**` or `src/components/**`.

---

## 1. ESLint Rule System

Eight custom rules are now active, all registered at `error` level and all guarded by a ninth rule that forbids disabling them:

| Rule | What it bans |
|------|-------------|
| `no-raw-layout-elements` | `<div>`, `<section>`, `<article>`, `<aside>`, `<nav>`, `<header>`, `<footer>` |
| `no-raw-inline-elements` | `<span>`, `<strong>`, `<b>`, `<code>`, `<kbd>`, `<samp>`, `<var>` |
| `no-raw-html-text-tags` | `<p>`, `<h1>`–`<h6>` |
| `no-raw-interactive-elements` | `<button>` |
| `no-raw-list-elements` | `<ul>`, `<ol>`, `<li>` |
| `no-raw-form-elements` | `<form>`, `<label>`, `<input>`, `<output>` |
| `no-terra-classname` | `className` on any `@orbit/terra` component |
| `no-direct-lucide-import` | Direct imports from `lucide-react` |
| `no-terra-disable-comment` | `eslint-disable` directives targeting any of the above |

The terra package (`src/packages/terra/**`) is fully exempt — it is the canonical implementation layer. Three ADRs (0045, 0046, 0047) and three PRDs document the decisions behind each rule group.

---

## 2. New Terra Components (18)

### Layout & Structure
| Component | Description |
|-----------|-------------|
| `Section` | Semantic `<section>` wrapper; replaces the `<Stack asChild><section>` pattern |
| `PageSectionHeader` | Standardised section heading with title + optional action slot |
| `Navigation` | Sidebar navigation shell; eliminates the hand-rolled `<nav>` pattern |
| `PageLayout` | Full-page shell with `sidebar`, `main`, and `sidebarFooter` slots |
| `Form` | `<form>` wrapper with `gap` and `maxWidth` layout props; no `className` leak |
| `List` / `ListItem` | `<ul>` / `<ol>` / `<li>` wrappers with `ordered` and `separator` props |

### Overlays & Interaction
| Component | Description |
|-----------|-------------|
| `SidePanel` | Radix-backed slide-over panel; replaces hand-rolled `dialogRef` + `showModal()` pattern. Handles focus trapping, Escape key, and scroll lock out of the box |
| `Modal` | Radix-backed modal dialog; replaces previous custom implementation |
| `Show` | Responsive visibility gate (`above` / `below` breakpoint props) |

### Form Controls
| Component | Description |
|-----------|-------------|
| `Checkbox` | `<label>` + `<input type="checkbox">` with `indeterminate` support and `variant="row"` for full-width interactive rows |
| `Output` | `<output>` element with `field` prop for input-like readonly display |

### Feedback & Data
| Component | Description |
|-----------|-------------|
| `Callout` | Info/warning/success/error callout box; replaces raw `<aside>` patterns |
| `TableToolbarShell` | Opinionated table toolbar layout with search + filter + action slots |
| `Indicator` | Numeric badge overlay for icon buttons |
| `StepDot` / `StepConnector` | Step-progress indicator primitives |
| `DragCard` | Draggable card with handle affordance |
| `ChatBubble` / `ChatBubbleActions` | Chat message bubble with action tray |
| `FormField` | Label + input + hint/error composition |

---

## 3. Terra Props API Hardening

15 existing components replaced `extends React.HTMLAttributes<T>` with explicit typed props, preventing consumers from passing arbitrary HTML attributes the component silently ignores or incorrectly forwards:

`Alert` · `Badge` · `Button` · `ButtonGroup` · `Card` · `Input` · `Modal` · `NativeSelect` · `Progress` · `Separator` · `Skeleton` · `Stack` · `StatusDot` · `Textarea` · `Text` / `Heading`

Key changes:
- `Stack` / `Navigation`: `style` prop removed; replaced by `height`, `overflow`, `overflowY`, and `disabled` props mapped to design tokens.
- `ButtonGroup`: `className` removed from public interface entirely (no Terra component exposes `className` to consumers).
- `Text` / `Heading`: gained `truncate`, `tabular`, `align`, `as`, and `preserveWhitespace` props, eliminating the need for inline style workarounds.
- Terra barrel (`src/packages/terra/src/index.ts`): `'use client'` added at the top level so Radix's `createContext` calls don't execute in RSC module evaluation.

---

## 4. Storybook Promotion

Storybook was moved from a per-package script to the workspace root, now covering both `src/packages/terra/**` and `src/components/**` in a single session. Stories are organised under a `Terra/` and `App/Shared/` prefix hierarchy with `@tier` JSDoc annotations on every component.

New story files added for all 18 new Terra components plus every existing component that lacked coverage.

---

## 5. Shared Component Extraction (Tier 2)

Seven new cross-feature components extracted from `src/app/**` into `src/components/**`, each with typed props, `@tier 2` JSDoc, a `@promote-to-tier-1 if:` escalation note, and a Storybook story:

| Component | Replaces |
|-----------|----------|
| `EmptyState` | Repeated inline empty-state patterns across policy and model-gateway |
| `ConfirmDialog` | Hand-rolled destructive-action confirmation modals (grants, roles, token revocation) |
| `MetricCard` | Inline KPI card components duplicated across dashboard and model-gateway |
| `EventFeedItem` | Activity feed row pattern (`<ListItem>` + `StatusDot` + text) |
| `ScopeChip` | Scope string display; now uses `<Badge variant="elevated" mono>` |
| `ScopePicker` | Searchable accordion scope selector with `<Checkbox variant="row">` |
| `StatusBadge` | Orbit status → Badge variant mapping via `as const satisfies` |

All seven are exported from `@orbit/components` (`src/components/index.ts`) with both value and type exports.

---

## 6. Product Code Migration

All 72 files in `src/app/**` and `src/components/**` that contained raw HTML or bypassed Terra are migrated. Selected highlights:

- **`edit-scopes-modal.tsx`** — replaced hand-rolled `dialogRef` / `showModal()` / close-event listener with `<SidePanel>`, eliminating a `useEffect` and three event-handler lines.
- **`dashboard-sidebar.tsx`** — `<aside>` + custom scroll wrapper replaced by `<PageLayout>` with `sidebarFooter` slot; `SidebarUserFooter` extracted as its own `'use client'` component.
- **`scope-picker.tsx`** — `<label>` / `<input type="checkbox">` patterns replaced with `<Checkbox variant="row">` (supports `indeterminate` for the select-all state).
- **`chat/page.tsx`** — message thread migrated from `<ol>` / `<li>` to `<List ordered>` / `<ListItem>`; chat input `<form>` replaced with `<Form maxWidth="document">`.
- **`overview-tab.tsx`** (model-gateway) — analytics data wired to real inference API; `KPICard` local component replaced by shared `MetricCard`; progress bars migrated from raw `<span>`/`<div>` to `<Progress>` and `<StatusDot>`.
- **`grants-tab.tsx`** / **`roles-tab.tsx`** — raw `<input type="text">` replaced with Terra `<Input>`; `<ul>`/`<li>` (list-none) layout containers replaced with `<Stack>` + `<Separator>`.
- **`catalog/editor-panel.tsx`** — `<output className="h-9 ...">` replaced with `<Output field>`.
- **`catalog/routes-tab.tsx`** — `<output>` replaced with `<Output>`.
- **`email-sign-in-form.tsx`** / **`create-token-modal.tsx`** / **`edit-scopes-modal.tsx`** — `<form>` replaced with `<Form>`.
- **`event-feed-item.tsx`** — `<li>` replaced with `<ListItem>`; flex layout expressed via `<Stack>` padding tokens.
- **`message-content.tsx`** (chat) — `<figure>` / `<figcaption>` / `<pre>` retained for code-block rendering with inline documentation confirming the intentional use (these elements are not covered by `no-raw-layout-elements`).
- **`table-controls.tsx`** — decorative `<label>` replaced with plain `<Text>` (no `htmlFor` association was present).
- **`analytics/`** — all analytics components (`analytics-dashboard.tsx`, `analytics-kpi-cards.tsx`, `analytics-principals-table.tsx`) migrated from `<section>` / `<div>` / lucide imports to Terra components and `<Stack asChild><figure>` loading wrappers.

---

## 7. Post-Rebase Fixes (main → branch)

Three commits on `main` added real inference analytics to the model-gateway overview tab while the branch was in flight. Rebase conflicts resolved by keeping main's live-data wiring and applying the branch's Terra migration on top:

- Duplicate imports (`fmtNumCompact` from two `@orbit/utils` lines), dead lucide-react block, and `MetricCard` import cleaned up.
- Stray `</Stack>` / duplicate `<Text>` from shared conflict-marker code removed.
- `nav-entries.ts` updated from lucide component references to `IconName` string tokens.
- Five stale files (`revoke-token-modal.tsx`, policy `scope-chip.tsx`, `scope-picker.tsx`, `empty-state.tsx`, `error-banner.tsx`) removed after a failed bisect attempt left main's deleted files in the working tree.

---

## 8. Utility & Routing Changes

- **`src/utils/format.ts`** — `fmtUsd`, `fmtTokens`, `fmtDateAxis`, `fmtNum`, `fmtDate`, `fmtExpiry` extracted from three separate chart/table files; the old `formatDateAxis` had a bare `catch {}` swallowing parse errors — fixed.
- **`src/lib/routing/routes.ts`** — `modelGateway.myUsage` and `modelGateway.analytics` routes added alongside the existing usage route.
- **`frontend/src/packages/terra/package.json`** — `@radix-ui/react-dialog` added to `peerDependencies` (previously undeclared despite `SidePanel` depending on it).

---

## 9. Documentation

| File | Content |
|------|---------|
| `docs/adr/0045-frontend-component-consolidation.md` | Decision record for the three-tier component model |
| `docs/adr/0046-terra-design-system-hardening.md` | Decision record for props API hardening and `no-terra-classname` |
| `docs/adr/0047-semantic-html-consolidation.md` | Decision record for all raw-HTML bans (Phase 1: layout/inline/interactive; Phase 2: list/form elements) |
| `docs/prd/frontend-component-consolidation.md` | PRD for shared component extraction |
| `docs/prd/terra-design-system-hardening.md` | PRD for props API hardening |
| `docs/prd/semantic-html-consolidation.md` | PRD for raw-HTML elimination (both phases) |
| `frontend/docs/reference/terra-design-system.md` | Golden-path guide: component catalogue, migration patterns, lint rule reference |
| `frontend/docs/reference/app-components.md` | Tier 2 shared component reference |
| `frontend/docs/reference/component-placement.md` | Three-tier model decision guide |

---

## 10. Test Plan

- `make check` passes: Go vet + linters, gitleaks, Go tests, frontend ESLint, TypeScript `--noEmit`, Prettier.
- `make frontend-build` compiles and type-checks cleanly (Next.js Turbopack + TypeScript). Runtime page-data collection fails only for API routes that require `POLICY_GATEWAY_URL` — a pre-existing infra constraint unrelated to this PR.
- All 72 migrated product files are lint-clean with zero `eslint-disable` comments.
- No surviving imports of deleted components (`RevokeTokenModal`, old policy-scoped `EmptyState`, `ErrorBanner`).

