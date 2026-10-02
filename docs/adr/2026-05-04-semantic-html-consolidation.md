# Semantic HTML Consolidation — Unconditional Ban on header, article, nav, aside, footer

**Status:** Accepted
**Date:** 2026-05-04
**Last Updated:** 2026-04-28
**Deciders:** Engineering Team
**Technical Story:** [Semantic HTML Consolidation PRD](../prd/semantic-html-consolidation.md)

---

## Context

[frontend-component-consolidation](2026-05-04-frontend-component-consolidation.md) established Terra as the mandatory UI foundation and introduced `no-raw-layout-elements`, which unconditionally bans `<div>` and `<section>` in product code. Four additional semantic elements — `<header>`, `<article>`, `<nav>`, `<aside>`, and `<footer>` — are currently only banned when they carry Tailwind layout-mode classes. A full audit of `src/app/**` and `src/components/**` finds 53 raw instances that bypass enforcement today.

The existing conditional rule creates an inconsistency: `<div className="flex">` is an error but `<aside className="h-full">` or `<footer className="px-4 py-3">` (no layout class) is not, even though all are equally problematic from a design-system perspective. More importantly, several recurring patterns — info callout boxes (8 instances), slide-over panel (5 instances in one file), sidebar navigation (1 instance) — have no sanctioned Terra alternative, so developers reach for raw semantic HTML as the only viable option.

---

## Decision Drivers

- **Rule completeness.** The four elements belong in the same unconditional category as `<div>` and `<section>`. Partial enforcement is more confusing than full enforcement and teaches the wrong lesson.
- **Semantic correctness.** Most instances misuse semantic HTML for layout: `<aside>` wraps a scrollable list; `<article>` wraps a stat row; `<header>` wraps a loading skeleton. This degrades the document model without adding accessibility value.
- **Gap closure.** New Terra components (`Callout`, `TableToolbarShell`, `Navigation`, `SidePanel`) eliminate the root cause — the absence of a sanctioned alternative forces developers to reach for raw HTML.
- **[terra-frontend-design-system](2026-03-25-terra-frontend-design-system.md) boundary.** Terra components render raw HTML internally and are exempt from product-code rules. Providing Terra wrappers is the architecturally correct response.
- **Agent discoverability.** Once `Callout` and `SidePanel` exist in Terra, AI coding agents will find and use them rather than generating inline `<aside>` patterns.

---

## Considered Options

### Option 1 — Extend `no-raw-layout-elements` unconditionally + add missing Terra components *(chosen)*

Ban all five elements unconditionally. Add `Callout`, `TableToolbarShell`, `Navigation`, and `SidePanel` to Terra. Migrate all 53 instances.

**Pros:** Complete enforcement parity with `<div>`/`<section>`. Root cause addressed — new components prevent future recurrence. Genuinely reusable beyond the migration.

**Cons:** Four new Terra components require full [terra-frontend-design-system](2026-03-25-terra-frontend-design-system.md) authoring. `SidePanel` is non-trivial (Radix Dialog integration). Migration touches 18 files.

### Option 2 — Extend the rule only; replace everything with `Stack`/`Grid`/`Alert`/`Card`

Same rule change, but avoid new components entirely.

**Pros:** No new Terra components.

**Cons:** `<nav>` becomes a `Stack` with no landmark role — accessibility regression. Info callout boxes become undifferentiated `Stack`s — pattern will be recreated as raw `<aside>` by the next developer who needs it. Root cause unaddressed.

### Option 3 — Keep the conditional rule; document acceptable patterns

No rule change. Add golden-path guidance for when each element is allowed.

**Pros:** Zero migration work.

**Cons:** Leaves 49 instances as-is. Does not close the enforcement gap. Inconsistent with the unconditional treatment of `<div>`.

---

## Decision

**Option 1.** Unconditional enforcement closes the gap cleanly. New components remove the root cause. Options 2 and 3 each leave the codebase in a worse state than Option 1 in different ways — Option 2 loses semantic correctness, Option 3 preserves the inconsistency indefinitely.

---

## Component Assignments

### `Callout` — Tier 1 (Terra) · `Terra/Atoms/Callout`

A styled informational callout box. Domain-free: any surface could use an informational callout. Does not encode kora-specific concepts.

```ts
type CalloutVariant = 'info' | 'warning'
interface CalloutProps {
  variant?: CalloutVariant  // default: 'info'
  children: React.ReactNode
}
```

`info` → `rounded-lg border border-accent/20 bg-accent/5 px-4 py-3`. `warning` → `border-status-degraded/30 bg-status-degraded/10`. Renders `<aside>` internally (Terra-exempt). `className` blocked per [terra-frontend-design-system](2026-03-25-terra-frontend-design-system.md) conventions.

**Callers:** routes-tab×2, vocabulary-tab, grants-tab, evaluate-tab, roles-tab, setup-tab, token-created-modal (warning variant). 9 instances total.

### `TableToolbarShell` — Tier 1 (Terra) · `Terra/Molecules/Table`

The bare toolbar container. Extracted from `TableToolbar` so that loading skeletons can render the correct container without using a raw `<header>`. `TableToolbar` is refactored to compose on top, guaranteeing both share identical padding/border tokens forever.

```ts
interface TableToolbarShellProps {
  children: React.ReactNode
}
```

Renders `<div className="flex items-center gap-3 px-4 py-3 border-b border-border-subtle">` internally.

**Callers:** `model-gateway/logs/loading.tsx`, `model-gateway/models/loading.tsx`. 2 instances.

### `Navigation` — Tier 1 (Terra) · `Terra/Layout/Navigation`

A semantic `<nav>` landmark wrapper for the primary sidebar navigation list. Renders `<nav>` internally.

```ts
interface NavigationProps {
  children: React.ReactNode
  'aria-label'?: string  // default: 'Main'
}
```

**Caller:** `dashboard-sidebar.tsx`. 1 instance. **Promotion trigger:** second navigation area outside the sidebar.

### `SidePanel` — Tier 1 (Terra) · `Terra/Molecules/SidePanel`

A Radix `@radix-ui/react-dialog`-based slide-over panel from the right edge. Same Radix package already used by `Modal`. Provides backdrop, Escape-to-close, focus trap, and ARIA automatically.

```ts
interface SidePanelProps {
  open: boolean
  onClose: () => void
  size?: 'sm' | 'md' | 'lg'  // maps to w-[440px] | w-[580px] | w-[720px]
  children: React.ReactNode
}
interface SidePanelHeaderProps { title: string; description?: string }
interface SidePanelBodyProps   { children: React.ReactNode; flush?: boolean }
interface SidePanelFooterProps { children: React.ReactNode; justify?: 'start' | 'end' | 'between' }
```

Renders `<aside>` for the panel shell (Terra-exempt). The `editor-panel.tsx` backdrop `<aside>`, shell `<aside>`, `<header>`, and `<article>` body are all absorbed into `SidePanel` internals. **Caller:** `editor-panel.tsx`. 5 instances.

### `PageLayout.sidebarFooter` — `PageLayout` prop extension

Rather than a standalone `Footer` Terra component, `PageLayout` gains an optional `sidebarFooter?: React.ReactNode` prop. When provided it renders a semantic `<footer>` pinned at the bottom of the sidebar `<aside>` shell. When omitted the sidebar layout is identical to today.

```ts
// Updated PageLayoutProps
interface PageLayoutProps {
  sidebar?: React.ReactNode
  sidebarFooter?: React.ReactNode  // ← new, optional
  header?: React.ReactNode
  children: React.ReactNode
  className?: string
}
```

The sidebar `<aside>` internal structure changes to accommodate the footer:

```tsx
<aside className="h-full w-60 shrink-0 flex flex-col …">
  <div className="flex-1 overflow-y-auto">{sidebar}</div>
  {sidebarFooter && (
    <footer className="shrink-0">{sidebarFooter}</footer>
  )}
</aside>
```

`dashboard-sidebar.tsx` is updated to remove its raw `<footer>` element and instead pass the user/logout strip content via `PageLayout`'s `sidebarFooter` prop. The two remaining `<footer>` instances in `chat/page.tsx` are not semantic page footers — they are input bar containers — and are replaced with `Stack`.

**Why `PageLayout` rather than a new `Footer` component:** `PageLayout` already renders the sidebar `<aside>` and top `<header>` shells internally. The sidebar footer is structurally part of that shell — it must be pinned below the scrollable nav area, which `PageLayout` already controls. A standalone `Footer` component would require callers to manually manage the pinning layout, defeating the point. Folding it into `PageLayout` keeps the shell semantics cohesive.

**File:** `frontend/eslint.config.mjs` — `no-raw-layout-elements` rule, `create()` function.

After the existing `<section>` unconditional block (~line 403), add four new unconditional checks:

```js
if (tag === 'header')  { context.report({ node, messageId: 'noRawHeader' });  return; }
if (tag === 'article') { context.report({ node, messageId: 'noRawArticle' }); return; }
if (tag === 'nav')     { context.report({ node, messageId: 'noRawNav' });     return; }
if (tag === 'aside')   { context.report({ node, messageId: 'noRawAside' });   return; }
if (tag === 'footer')  { context.report({ node, messageId: 'noRawFooter' });  return; }
```

Remove these five from `SEMANTIC_ELEMENTS` (the conditional set, ~line 392) since they are now handled unconditionally above. Add five new message strings to the `messages` object pointing to the appropriate Terra replacement and `terra-design-system.md`.

The existing config block (`files: ['src/**/*.tsx']`, `ignores: ['src/packages/terra/**']`) already applies this rule to all product code — no additional config changes needed.

---

## Migration Map

| Replacement | Count | Files |
|-------------|-------|-------|
| `Callout` | 9 | routes-tab×2, vocabulary-tab, grants-tab, evaluate-tab, roles-tab, setup-tab, token-created-modal, model-detail-modal |
| `SidePanel` (shell+header+body) | 5 | editor-panel.tsx |
| `Navigation` | 1 | dashboard-sidebar |
| `TableToolbarShell` | 2 | logs/loading, models/loading |
| `Stack` (drop asChild/tag) | 20 | 7 files — all remaining layout-only uses |
| `Stack` (footer — drop asChild) | 2 | chat/page.tsx×2 (input bar containers, not semantic footers) |
| `PageLayout.sidebarFooter` | 1 | dashboard-sidebar user/logout strip |
| `SidePanelFooter` | 1 | editor-panel.tsx |
| `Card interactive` | 1 | model-card-grid |
| `Alert` | 2 | token-created-modal (warning), editor-panel (error) |
| `Grid` | 1 | chat page suggestion chips |
| `Stack aria-label="Breadcrumb"` | 1 | dashboard-header |
| `Progress` | 2 | overview-tab progress bar |
| **Total** | **53** | **18 files** |

---

## Consequences

### Positive

- `no-raw-layout-elements` now provides complete, consistent enforcement across all six structural HTML elements that belong in the unconditional category.
- `SidePanel` gives the model catalog editor focus trap, Escape-to-close, and correct ARIA that the current hand-rolled implementation lacks.
- `Callout` eliminates the single most-duplicated raw pattern (8+ identical `<aside>` callout boxes).
- Future developers and AI agents reach for `Callout`, `Navigation`, and `SidePanel` before writing raw HTML.

### Negative

- `SidePanel` requires non-trivial Radix Dialog integration (~150 lines of Terra component code).
- Migration touches 18 files; each phase must pass `pnpm lint` before the next begins.

### Neutral

- `<main>` is not addressed — it is exclusively rendered by `PageLayout` (Terra-exempt) and has no product-code instances.
- `<header>` inside `dashboard-sidebar.tsx` and `dashboard-header.tsx` will be resolved by migrating these components to use `PageLayout` to supply the structural shell — the raw `<header>` elements disappear as a by-product of that refactor, not as a direct migration step.

---

## Implementation Status

| Phase | Status |
|-------|--------|
| 0 — PRD + ADR | ✅ Complete (2026-04-28) |
| 1 — New Terra components (`Callout`, `TableToolbarShell`, `Navigation`, `SidePanel`, `PageLayout.sidebarFooter`) | ✅ Complete (2026-04-28) |
| 2 — ESLint rule upgrade (unconditional ban, 5 new message IDs) | ✅ Complete (2026-04-28) |
| 3 — Migrate `<aside>` (24) | ✅ Complete (2026-04-28) |
| 4 — Migrate `<article>` (13) | ✅ Complete (2026-04-28) |
| 5 — Migrate `<header>` (9) | ✅ Complete (2026-04-28) |
| 6 — Migrate `<nav>` (3) | ✅ Complete (2026-04-28) |
| 7 — Migrate `<footer>` (4) | ✅ Complete (2026-04-28) |
| 8 — Update golden-path docs | ⏳ Pending |


---

## Extension — Form and List Element Consolidation

**Date:** 2026-04-29

### Context

After banning `<div>`, `<section>`, `<article>`, `<aside>`, `<nav>`, `<header>`, and `<footer>`, AI coding agents shifted to using the next available uncovered elements — `<ul>`, `<ol>`, `<li>`, `<form>`, `<label>`, `<input>`, and `<output>` — as structural workarounds (e.g. `<ul className="list-none m-0 p-0">` replacing a `<Stack>`, `<label>` replacing a `<span>`). This closes those remaining loopholes.

### Decision

Ban all seven element types unconditionally in product code via two new ESLint rules:

| Rule | Elements |
|------|----------|
| `local/no-raw-list-elements` | `<ul>`, `<ol>`, `<li>` |
| `local/no-raw-form-elements` | `<form>`, `<label>`, `<input>`, `<output>` |

### Replacements

| Raw element | Terra replacement |
|-------------|-------------------|
| `<ul>` used as layout container | `<Stack>` |
| `<ul>` as a genuine list | `<List>` from `@orbit/terra` |
| `<ol>` as an ordered list | `<List ordered>` from `@orbit/terra` |
| `<li>` | `<ListItem>` from `@orbit/terra` |
| `<form>` | `<Form>` from `@orbit/terra` (accepts `onSubmit`, `noValidate`, `gap`, `maxWidth`) |
| `<label>` for text | `<Label>` from `@orbit/terra` |
| `<label>` wrapping a checkbox | `<Checkbox>` from `@orbit/terra` |
| `<input type="checkbox">` | `<Checkbox>` from `@orbit/terra` (supports `indeterminate`) |
| `<input type="text/...">` | `<Input>` from `@orbit/terra` |
| `<output>` | `<Output>` from `@orbit/terra` (`field` prop for input-like styling) |

### New Terra components added (2026-04-29)

- **`Form`** — Renders `<form>` with flex-column layout and token-aligned gap.
- **`Checkbox`** — Renders `<label>` + `<input type="checkbox">` with `indeterminate` support and a `variant="row"` for full-width interactive rows.
- **`List`** — Renders `<ul>` or `<ol>` with `list-none` reset and optional `separator` prop.
- **`ListItem`** — Renders `<li>`; children own their own layout.
- **`Output`** — Renders `<output>` with an optional `field` prop for input-like styling.

### Consequences

All seven element types are now unconditionally banned in product code. The `no-terra-disable-comment` rule also tracks both new rules, preventing suppression. Terra source (`src/packages/terra/**`) remains fully exempt.
