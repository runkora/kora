# Code Review: PR #42 — Terra Design System Uplift & Component Migration

**PR**: https://github.com/clear-route/orbit/pull/42
**State**: OPEN
**Stats**: 21,489 additions / 6,797 deletions / 181 changed files / 30 commits
**Authors**: anvxl, QjStout (co-authored with Claude Sonnet 4.6)

---

## Summary

This PR executes three parallel workstreams:

1. **ESLint hardening** — upgrades `no-raw-div` (warn) to `no-raw-layout-elements` (error, wider scope), adds `no-raw-inline-elements`, `no-direct-lucide-import`, and `no-terra-disable-comment`, which closes the escape-hatch loop entirely.
2. **Terra library expansion** — adds ~14 new components: `ButtonGroup`, `Indicator`, `Callout`, `SidePanel`, `Navigation`, `PageSectionHeader`, `Show`, `ChatBubble` family, `DragCard`, `StepDot`/`StepConnector`, `TableToolbarShell`. Hardens existing component prop APIs by replacing `extends React.HTMLAttributes<T>` with explicit typed props on `Alert`, `Card`, `Stack`, `Badge`, `Input`, `Modal`, `NativeSelect`, `Skeleton`, `StatusDot`, `Textarea`, `Heading`, and `Text`.
3. **Product code migration** — removes every raw `div`, `section`, `span`, `strong`, `code`, and direct Lucide import in the product layer, replacing them with the correct Terra primitive or a new shared/feature-scoped component.

Process was followed correctly: three PRDs (`docs/prd/`) and three ADRs (0043, 0044, 0045) were written before the code. The three-tier component model is applied consistently with `@tier` JSDoc annotations and `@promote-to-tier-N if:` escalation notes on every new component.

---

## What Works Well

### ESLint escape-hatch is now self-sealing

`no-terra-disable-comment` is the standout addition. Before this PR, `eslint-disable-next-line local/no-raw-div` comments existed in the codebase (visible in the `login/page.tsx` and `models/loading.tsx` diffs). This rule makes enforcement self-sealing: suppressing any Terra rule now triggers a _separate_ lint error. That is the correct design for a constraint you intend to be permanent.

Elevating `no-raw-div` from `warn` to `error` and widening scope to `no-raw-layout-elements` (covering `section`, `aside`, `nav`, `header`, `footer`, `article`, `main`) closes the category of violations that `warn` simply deferred rather than prevented.

### `'use client'` on the Terra barrel

Adding `'use client'` to `frontend/src/packages/terra/src/index.ts` is the correct fix for Radix UI components that call `React.createContext()`. Without it, any RSC importing from `@orbit/terra` would throw at runtime. The fix is minimal and in the right location.

### Props API hardening

Replacing `extends React.HTMLAttributes<HTMLDivElement>` with explicit typed props on `Alert`, `Card`, `Stack`, `Input`, `Modal`, `Badge`, `Skeleton`, etc. is a meaningful correctness improvement. It prevents consumers from passing arbitrary HTML attributes that the component silently ignores or incorrectly forwards. The pattern in the updated `AlertProps` — explicitly listing `className`, `id`, `role`, `aria-label`, `aria-live` — is a good reference model: minimal footprint, still covers the accessibility attributes the component actually needs.

### Storybook promoted to workspace root

Moving Storybook from a per-package script (`pnpm --filter @orbit/terra storybook`) to a root-level script covering both Terra and `src/components/**` is the right call. The old setup left Tier 2 app-level shared components with no visual documentation path. The `@tier` annotations combined with the Storybook prefix hierarchy (`Terra/Atoms/`, `Terra/Molecules/`, `App/Shared/`) make the three-tier model tangible and reviewable in one place. The `storySort` order in `preview.ts` enforces a sensible browsing structure.

### `SidePanel` replacing the hand-rolled `<dialog>`

The `edit-scopes-modal.tsx` diff shows the old implementation manually managing a `dialogRef`, syncing `dialog.showModal()` / `dialog.close()`, and wiring a `close` event listener for backdrop-click dismissal. The entire `useEffect` is gone in the new version. Replacing this with `SidePanel` (backed by `@radix-ui/react-dialog`) is strictly better: Radix handles focus trapping, Escape key, scroll lock, pointer-events isolation, and accessible labelling correctly out of the box.

### `PageLayout.sidebarFooter` slot

The sidebar refactor in `page-layout.tsx` is clean. The `flex-1 overflow-y-auto` wrapper keeps the navigation scrollable independently of the footer, `shrink-0` on the footer prevents it from being squeezed out on short viewports, and the semantic `<footer>` element is correct. Extracting `SidebarUserFooter` into its own `'use client'` file and passing it through `sidebarFooter` keeps `DashboardLayout` (a server component) clean.

### Semantic corrections are accurate

- Replacing `<section className="h-[240px]">` with `<figure>` for chart empty states is semantically accurate — a chart is self-contained content that could be relocated without affecting document flow.
- `<TableHead srOnly>Actions</TableHead>` replacing `<TableHead><span className="w-10 block" /></TableHead>` fixes a real accessibility gap; screen readers previously announced an empty unlabelled column header.
- Removing `asChild` + `<section>` wrappers from `scope-guard.tsx` in favour of `<Stack minHeight="full">` removes a DOM node and eliminates an `asChild` composition that added conceptual indirection with no semantic value.

### Shared component extraction quality

`EmptyState`, `ConfirmDialog`, `MetricCard`, `EventFeedItem`, `ScopeChip`, `ScopePicker`, and `StatusBadge` are all well-designed at Tier 2. Each has an explicit typed props interface, a `@tier 2` JSDoc with a `@promote-to-tier-1 if:` condition, a Storybook story with realistic arg values, and is exported from `src/components/index.ts` with both value and type exports.

`ScopeChip` deserves specific mention: replacing the hand-rolled `<span>` tree with `<Badge variant="elevated" size="sm" mono>` is cleaner, and using `as const satisfies Record<Provider, BadgeVariant>` in `catalog-constants.ts` to map providers to typed badge variants — rather than arbitrary hex class strings — ties visual styling to the token system and gets compile-time exhaustiveness checking.

### Utility deduplication

`fmtUsd`, `fmtCost`, and `fmtDateAxis` extracted from three separate chart files into `@orbit/utils` eliminate near-identical local helpers. The old `formatDateAxis` in `latency-chart.tsx` had a bare `catch {}` block silently swallowing date parse errors; centralising it means any fix benefits all consumers. Replacing the inline-styled `PeriodButton` (nine lines of style object) with `<Button variant="secondary"|"ghost" size="xs">` removes design-token duplication.

---

## Issues and Concerns

### 1. `ButtonGroup` still extends `React.HTMLAttributes` — inconsistent with the hardening effort

```tsx
// button-group.tsx — added in this PR
export interface ButtonGroupProps extends React.HTMLAttributes<HTMLDivElement> {
  children: React.ReactNode;
}
```

Every other component hardened in this PR dropped `extends React.HTMLAttributes`. `ButtonGroup` was introduced in this same PR and still uses the old pattern. It should follow the `Alert` model: list only the attributes the component actually forwards (`className`, `id`, `role`, `aria-label`, `aria-labelledby`). As written, consumers can pass `onClick`, `onMouseEnter`, and arbitrary `data-*` attributes — none of which `ButtonGroup` is designed to handle.

### 2. `anchor.stories.tsx` converts `<span>` to `<div>` — should be `<Stack>`

```tsx
// Before (in this PR's diff)
<span className="w-36 shrink-0 font-mono text-xs text-subtle">{label}</span>

// After (in this PR's diff)
<div className="w-36 shrink-0 font-mono text-xs text-subtle">{label}</div>
```

The file was actively edited in this PR, but the conversion goes the wrong direction. The correct migration is `<Stack>`, not a block element swap. If story files inside the Terra package are intentionally excluded from `no-raw-layout-elements`, that exclusion needs to be documented in `eslint.config.mjs` with a comment explaining why.

### 3. `message-content.tsx` retains raw `<pre>`, `<figure>`, `<figcaption>` with no documented exception

```tsx
// message-content.tsx (Tier 3, chat scoped)
<Stack key={i} gap="none" asChild>
  <figure className="rounded-lg border border-border-subtle overflow-hidden m-0">
    <Stack direction="horizontal" align="center" justify="between" asChild>
      <figcaption className="px-4 py-2 bg-surface border-b border-border-subtle">
```

`<figure>`, `<figcaption>`, and `<pre>` are raw HTML elements not yet covered by Terra. Using them here may be intentional — code block rendering is genuinely specialised — but without an inline comment or documented lint exception, the next engineer touching this file won't know whether this is an approved pattern or an oversight. The inline `className` strings on `<figure>` and `<pre>` also bypass the spirit of `no-terra-classname`, even if they don't technically trigger it (the rule targets Terra component props, not raw elements).

### 4. `analytics-principals-table.tsx`: `<Stack asChild><figure>` is semantically incorrect for a data table

```tsx
<Stack asChild>
  <figure className={loading ? 'pointer-events-none opacity-50 transition-opacity' : undefined}>
    <DataTable ... />
  </figure>
</Stack>
```

The HTML spec defines `<figure>` as self-contained content — diagrams, code samples, illustrations — that could be relocated in the document without affecting surrounding flow. A paginated analytics table is part of the main content flow and does not qualify. The previous `<section>` was closer to correct. The right fix is to remove the wrapper entirely and achieve the opacity/pointer-events effect via a `Stack` prop or by passing it directly to `DataTable`.

### 5. `chart-legend.tsx` duplicates `StatusDotStatus` locally instead of importing from Terra

```tsx
// chart-legend.tsx (Tier 3, model-gateway scoped)
type StatusDotStatus = 'healthy' | 'degraded' | 'error' | 'idle' | 'pending' | 'muted' | 'accent';
```

This is a manually duplicated type. If Terra's `StatusDot` status union is extended later, `ChartLegend` will silently accept stale values. The type should be imported via `StatusDotProps['status']` (or a dedicated exported type if one exists). The Terra barrel already exports `StatusDotProps` — use it.

### 6. `ConfirmDialog` error state uses `<Text color="status-error">` rather than `<Alert variant="error">`

```tsx
// confirm-dialog.tsx
{error && (
  <Text color="status-error" size="caption">
    {error}
  </Text>
)}
```

The rest of this PR consistently uses `<Alert variant="error">` for error messages (login page, email sign-in form). `<Alert>` automatically applies `role="alert"`, which is the correct ARIA role for dynamically injected error content inside a dialog. `<Text color="status-error">` is visually similar but semantically silent — assistive technologies will not announce it as an error. Since `ConfirmDialog` is a canonical cross-feature Tier 2 component, this inconsistency sets a precedent that other consumers will copy.

### 7. `@radix-ui/react-dialog` in app `package.json` — verify it is not already a transitive dep of `@orbit/terra`

`SidePanel` (inside the Terra package) depends on `@radix-ui/react-dialog`. If Terra's own `package.json` already declares this dependency, adding it to the app's `package.json` as well creates a second version declaration that pnpm must reconcile. If deduplication doesn't align them, the app could end up with two copies of the Radix dialog runtime in its bundle. Verify this is a net-new dependency before keeping the entry.

### 8. `keys-tab.tsx` imports `@orbit/components` three times separately

```tsx
import { ScopeChip } from '@orbit/components';
...
import { EmptyState } from '@orbit/components';
import { ConfirmDialog } from '@orbit/components';
```

These should be a single import statement: `import { ScopeChip, EmptyState, ConfirmDialog } from '@orbit/components';`. Fragmented imports from the same module suggest the file was assembled incrementally without a tidy pass.

### 9. `revoke-token-modal.tsx` deleted — confirm all call sites are migrated

The file was removed (117 lines deleted, 0 added) and replaced by `ConfirmDialog`. The diff shows it was removed from `keys-tab.tsx`, which is the only known call site. Verify no other file in the repository still imports `RevokeTokenModal` — a deleted export with a surviving import is a silent compile error. `tsc --noEmit` should catch it, but an explicit `grep` before merge removes the risk.

---

## Minor Nits

- **`Indicator` uses `aria-hidden` as a boolean prop without `={true}`** — `<span ... aria-hidden />` is valid JSX shorthand and React will serialize it as `aria-hidden="true"`, so this is not a bug. Noting it for consistency since other components in this PR use explicit boolean assignments.

- **`Callout`'s `className` prop is marked `@internal`** — This is a good practice for preventing consumers from fighting the component's own styles. However, if `className` is intended only for internal Terra compositions, consider not exposing it in the public interface at all (or using a separate `__unsafeClassName` convention to make the risk visible at the call site).

- **`SidePanel.dismissOnOutsideClick` defaults to `true`** — The prop name is slightly awkward as a negative (`dismissOn...`). A positive name like `closeOnOverlayClick` would be easier to read at a call site: `closeOnOverlayClick={false}` reads more naturally than `dismissOnOutsideClick={false}`.

- **`PageSectionHeader` and `TableToolbarShell` are exported from the Terra barrel but have no Storybook stories visible in the diff** — Every other new component in this PR has a `.stories.tsx` file. These two appear to be missing stories, or the stories were not included in the 181 changed files the GitHub API returned. Verify they have coverage in Storybook before merging.

- **`Show` component** — A render-guard `<Show when={condition}>` primitive is a useful addition, but confirm it handles the falsy-but-renderable `0` case correctly. A naive `condition && children` implementation will render `0` when `condition` is `0`. The implementation should coerce `condition` to boolean: `!!when && children`.

---

## Verdict

**Approve with action items before merge.**

The PR is well-scoped, the process was followed, and the end state is meaningfully better than the starting point: the design system boundary is enforced at the tooling layer (not just by convention), 181 files are clean of raw HTML, and Storybook now covers the full component surface.

The items that need resolution before merge:

| # | Severity | Item |
|---|----------|------|
| 1 | **Must fix** | `ButtonGroup` — remove `extends React.HTMLAttributes`, use explicit props |
| 3 | **Must fix** | `message-content.tsx` — document or lint-except the raw `<figure>`/`<pre>`/`<figcaption>` usage |
| 6 | **Must fix** | `ConfirmDialog` — replace `<Text color="status-error">` with `<Alert variant="error">` for correct `role="alert"` semantics |
| 2 | **Should fix** | `anchor.stories.tsx` — migrate `<div>` to `<Stack>`, or document why story files are excluded from the rule |
| 4 | **Should fix** | `analytics-principals-table.tsx` — replace `<figure>` wrapper with a semantically neutral alternative |
| 5 | **Should fix** | `chart-legend.tsx` — import `StatusDotStatus` from `@orbit/terra` rather than duplicating it |
| 7 | **Should fix** | Verify `@radix-ui/react-dialog` is not already a transitive dep of `@orbit/terra` before keeping it in the app `package.json` |
| 8 | **Should fix** | Consolidate the three separate `@orbit/components` imports in `keys-tab.tsx` |
| 9 | **Verify** | Confirm no surviving import of the deleted `RevokeTokenModal` exists anywhere in the repo |
