# Golden Path: UI Component Placement and the Three-Tier Model

**Audience:** Engineers (human and AI) creating or extracting any reusable UI component in the Orbit frontend.
**Pattern version:** 1.1
**Last updated:** 2026-04-21
**Authoritative ADR:** [`docs/adr/0043-frontend-component-consolidation.md`](../../docs/adr/0043-frontend-component-consolidation.md)

---

> ⚠️ **Run this decision tree before writing a single line of JSX.** Placing a component in the wrong tier causes duplication, broken discoverability, and lint failures. Read the placement decision tree first.

---

## The Three-Tier Model

All UI components belong to exactly one of three tiers. The tier determines the filesystem location, import path, authoring requirements, and Storybook story convention.

| Tier  | Name             | Filesystem path                                       | Import path                        | Storybook title prefix                                                  |
| ----- | ---------------- | ----------------------------------------------------- | ---------------------------------- | ----------------------------------------------------------------------- |
| **1** | Terra            | `frontend/src/packages/terra/src/components/`         | `import { X } from '@orbit/terra'` | `Terra/Atoms/`, `Terra/Molecules/`, `Terra/Layout/`, `Terra/Organisms/` |
| **2** | App-level shared | `frontend/src/components/`                            | `import { X } from '@/components'` | `App/Shared/`                                                           |
| **3** | Feature-scoped   | `frontend/src/app/(dashboard)/[feature]/_components/` | relative import                    | none required                                                           |

> **`App/Shared/` is a Storybook `title` field value — a sidebar path, not a filesystem directory.** Setting `title: 'App/Shared/MetricCard'` in a story file's meta places it under `App → Shared → MetricCard` in the Storybook sidebar. The story file itself lives at `frontend/src/components/`.

---

## Placement Decision Tree

Apply this algorithm in order. Stop at the first match.

```
1. Is it used by only one file?
   Yes → keep inline. Extract only if the host file exceeds ~300 lines of JSX.

2. Are all consumers within the same feature area?
   (e.g. all files under app/(dashboard)/model-gateway/)
   Yes → Tier 3.
         Place in: frontend/src/app/(dashboard)/[feature]/_components/
         Open the file with:
           // @tier 3 — [feature]-scoped.
           // Promote to src/components/ if a second feature needs this.

3. Are consumers in two or more distinct feature areas?
   Yes → proceed to step 4.

4. Does the component contain Orbit domain knowledge?
   Domain knowledge = Orbit status token names (healthy/degraded/error),
   principal types (user/service-account/agent), model/provider names,
   destructive-action patterns, permission or scope vocabulary.

   No domain knowledge → Tier 1 (Terra).
     Place in: frontend/src/packages/terra/src/components/
     Export from: frontend/src/packages/terra/src/index.ts
     Import via: import { ComponentName } from '@orbit/terra'
     Must follow ADR-0021 authoring conventions (see below).
     Storybook story required under Terra/<Category>/<ComponentName>.

   Has domain knowledge → Tier 2 (app-level shared).
     Place in: frontend/src/components/
     Export from: frontend/src/components/index.ts (in the same PR).
     Import via: import { ComponentName } from '@/components'
     Storybook story required — title: 'App/Shared/<ComponentName>'.
```

**Tiebreaker**: prefer the more specific tier. Promote explicitly when the trigger fires — don't pre-generalise. It is much easier to promote Tier 3 → 2 → 1 than to split an over-generalised component.

---

## `@tier` Annotation

Every extracted component file (all tiers) must open with a `@tier` JSDoc comment. Searchable via `grep -r "@tier"`. Surfaces in IDE hover.

```ts
/**
 * @tier 1
 * Zero Orbit domain knowledge. Could be extracted to a standalone library.
 */

/**
 * @tier 2
 * Orbit-domain-aware: uses Orbit status tokens (healthy/degraded/error).
 * Used by: dashboard overview, model-gateway, agents pages.
 */

/**
 * @tier 3 — model-gateway-scoped.
 * Promote to src/components/ if a second feature needs this.
 */
```

---

## Storybook Story Title Conventions

The `title` field in a story's `meta` object sets its position in the Storybook sidebar. These are sidebar hierarchy strings — not filesystem paths.

| Tier                | Story title prefix | Example                              |
| ------------------- | ------------------ | ------------------------------------ |
| Tier 1 — atoms      | `Terra/Atoms/`     | `title: 'Terra/Atoms/Icon'`          |
| Tier 1 — molecules  | `Terra/Molecules/` | `title: 'Terra/Molecules/Modal'`     |
| Tier 1 — layout     | `Terra/Layout/`    | `title: 'Terra/Layout/Stack'`        |
| Tier 1 — organisms  | `Terra/Organisms/` | `title: 'Terra/Organisms/DataTable'` |
| Tier 2 — app shared | `App/Shared/`      | `title: 'App/Shared/StatusBadge'`    |

---

## Terra Authoring Conventions (Tier 1)

New Tier 1 components must follow these patterns (per ADR-0021):

- `React.forwardRef` with `React.ComponentPropsWithoutRef<'div'>` (not `React.HTMLAttributes`)
- `data-slot="component-name"` on every rendered element
- `cn()` for class merging, `cva()` for variant maps
- `displayName` set explicitly on all sub-components
- Static Tailwind class strings in variant maps — no dynamic string concatenation
- A Storybook story file in the same directory with `tags: ['autodocs']`

---

## Available Terra Components (Tier 1)

All components below are exported from `@orbit/terra`. Never import from sub-paths.

Verify the full export list at: `frontend/src/packages/terra/src/index.ts`

### Layout

| Component    | Use instead of                  | Notes                                                                                                        |
| ------------ | ------------------------------- | ------------------------------------------------------------------------------------------------------------ |
| `Stack`      | `<div className="flex …">`      | Flex container. Props: `direction`, `gap`, `align`, `justify`, `wrap`, `asChild`, responsive variants.       |
| `Grid`       | `<div className="grid …">`      | CSS grid container. Props: `cols`, `gap`, `asChild`, responsive variants.                                    |
| `Show`       | `hidden md:block` / `md:hidden` | Responsive visibility gate. `<Show above="md">` or `<Show below="lg">`. Breakpoints: `sm`, `md`, `lg`, `xl`. |
| `PageLayout` | bare page `<div>`               | Top-level page shell for all dashboard pages.                                                                |

### Typography

| Component | Use instead of  | Notes                                                                                                                                                             |
| --------- | --------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `Heading` | `<h1>`–`<h6>`   | Use `variant` prop. Variants: `display`, `title`, `section`, `subsection`, `label`, `metric`.                                                                     |
| `Text`    | `<p>`, `<span>` | Sizes: `micro`, `caption`, `supporting`, `body`, `lead`. Colors: `default`, `muted`, `subtle`, `status-success`, `status-error`, `status-warning`, `status-info`. |
| `Code`    | `<code>` inline | Inline monospace code inside prose.                                                                                                                               |

### Atoms

| Component                          | Use instead of                     | Notes                                                                                                                                                                                                                                                                                                                                             |
| ---------------------------------- | ---------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `Icon`                             | `import { X } from 'lucide-react'` | **Always use this. Never import from `lucide-react` directly.** Props: `name` (`IconName`), `size` (`2xs`–`xl`), `color`, `container`, `containerVariant` (`accent`, `surface`, `muted`). To add a new icon: propose the icon name and source to a human engineer for approval, then add to `src/packages/terra/src/components/icon/registry.ts`. |
| `Button`                           | `<button>`                         | Variants: `default`, `destructive`, `outline`, `secondary`, `ghost`, `link`. Sizes: `sm`, `md`, `lg`, `icon-sm`, `icon`.                                                                                                                                                                                                                          |
| `Badge`                            | styled `<span>`                    | Variants: `default`, `secondary`, `destructive`, `outline`.                                                                                                                                                                                                                                                                                       |
| `Alert`                            | custom warning divs                | Informational / warning / error banner.                                                                                                                                                                                                                                                                                                           |
| `Input`                            | `<input>`                          | Has `hasError?: boolean` prop.                                                                                                                                                                                                                                                                                                                    |
| `Textarea`                         | `<textarea>`                       | Multi-line text input.                                                                                                                                                                                                                                                                                                                            |
| `NativeSelect`                     | `<select>`                         | Native select with consistent styling.                                                                                                                                                                                                                                                                                                            |
| `Toggle`                           | custom toggle                      | Toggle switch.                                                                                                                                                                                                                                                                                                                                    |
| `Label`                            | `<label>`                          | Accessible label.                                                                                                                                                                                                                                                                                                                                 |
| `Anchor`                           | `<a>`                              | Styled hyperlink. Variants: `default`, `subtle`, `underline`.                                                                                                                                                                                                                                                                                     |
| `Separator`                        | `<hr>` / `<div>` rule              | Horizontal or vertical rule.                                                                                                                                                                                                                                                                                                                      |
| `Skeleton`                         | custom loading div                 | Loading placeholder.                                                                                                                                                                                                                                                                                                                              |
| `Field`, `FieldHint`, `FieldError` | manual form wrappers               | Form field container. `FieldError` has `visible` prop so space is always reserved.                                                                                                                                                                                                                                                                |

### Molecules

| Component                                                                         | Notes                                                                                                                                                                                       |
| --------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `Card`, `CardHeader`, `CardTitle`, `CardDescription`, `CardContent`, `CardFooter` | Content card. `interactive` prop adds focus ring for clickable cards.                                                                                                                       |
| `Modal`, `ModalHeader`, `ModalBody`, `ModalFooter`, `ModalAccessibleTitle`        | Dialog modal backed by Radix. `ModalHeader` provides required accessible title. Use `ModalAccessibleTitle` (sr-only) for custom-header modals.                                              |
| `FormField`                                                                       | Composes `Label`, `Field`, `FieldHint`, `FieldError` with auto-wired aria attributes. Use `useFormField()` in custom inputs to read `id`, `hasError`, `errorId`.                            |
| `ChatBubble`, `ChatBubbleActions`                                                 | Chat message bubble. `direction="start"` (inbound/AI) or `direction="end"` (user/outbound). `ChatBubbleActions` must be a direct child of `ChatBubble` to inherit `group-hover` visibility. |
| `NavItem`                                                                         | Sidebar nav item with `active` state and icon slot.                                                                                                                                         |
| `Tooltip`, `TooltipProvider`, `TooltipTrigger`, `TooltipContent`                  | Hover tooltip (Radix). Wrap app root with `TooltipProvider`.                                                                                                                                |
| `Popover`, `PopoverTrigger`, `PopoverAnchor`, `PopoverContent`                    | Click-triggered floating panel.                                                                                                                                                             |
| `Tabs`, `TabsList`, `TabsTrigger`, `TabsContent`                                  | Horizontal tab navigation.                                                                                                                                                                  |
| `Progress`                                                                        | Horizontal progress bar.                                                                                                                                                                    |

### Data Display

| Component                                                           | Notes                                                                                                                                        |
| ------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------- |
| `DataTable`                                                         | Fully opinionated TanStack Table. Handles sorting, filtering, pagination, column visibility. Prefer over composing raw table sub-components. |
| `TableContainer`, `TableEmpty`, `TableToolbar`, `TableColumnHeader` | Table wrappers and helpers.                                                                                                                  |
| `ChartContainer`, `ChartTooltipContent`, `ChartLegendContent`       | Recharts wrappers with consistent theme.                                                                                                     |

### Hooks

| Hook                       | Notes                                                                                           |
| -------------------------- | ----------------------------------------------------------------------------------------------- |
| `useDialog(open, onClose)` | Syncs a `<dialog>` element with React `open`/`onClose`. Returns `{ ref, handleBackdropClick }`. |
| `useFormField()`           | Reads `id`, `hasError`, `errorId` from `FormField` context. Use inside custom input components. |

---

## Banned Patterns (ESLint — violations fail `pnpm lint`)

| Rule                            | What is banned                                                            | Use instead                                                                                                          |
| ------------------------------- | ------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------- |
| `local/no-raw-block-elements`   | `<div>`, `<section>`, `<article>`, `<header>`, `<footer>`, `<main>`       | `Stack`, `Grid`, or `asChild`                                                                                        |
| `local/no-raw-inline-elements`  | `<span>`, `<strong>`, `<b>`, `<em>`, `<i>`                                | `Text`, `Heading`, or `Code`                                                                                         |
| `local/no-raw-heading-elements` | `<h1>`–`<h6>`                                                             | `Heading`                                                                                                            |
| `local/no-raw-anchor-elements`  | `<a>`                                                                     | `Anchor`                                                                                                             |
| `local/no-terra-classname`      | `className` on any Terra component (except `asChild` passthrough)         | Remove the className; use the component's own props                                                                  |
| `local/no-direct-lucide-import` | `import { X } from 'lucide-react'` in `src/app/**` or `src/components/**` | `<Icon name="…">` — **no escape hatch**. Add new icons to `src/packages/terra/src/components/icon/registry.ts` only. |

---

## Checking Your Work

Before marking any frontend change complete:

```bash
cd frontend && pnpm lint           # ESLint — must pass with zero errors
cd frontend && pnpm tsc --noEmit   # TypeScript — must pass with zero errors
```

---

## Implementation Rule 9: Keep This Document Current

When you add a new component at any tier — update this document in the **same PR**. A component not listed in the inventory is invisible to the next agent and will be duplicated.

---

## Tier 2: App-Shared Component Inventory

Tier 2 components live at `frontend/src/components/` and are exported from `frontend/src/components/index.ts`. Import all Tier 2 components via `@orbit/components` (the `@orbit/*` path alias maps to `./src/*`).

| Component           | Description                                                                                                                                                                                                                                              |
| ------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `EmptyState`        | Zero-state placeholder card: icon + title + description + optional CTA. Key props: `icon: IconName`, `title`, `description`, `action?: ReactNode`.                                                                                                       |
| `ErrorBanner`       | **Removed** — use `<Alert variant="error">` from `@orbit/terra` with `<Icon name="triangle-alert" color="status-error" />` and `<Text size="caption" color="status-error">` as children.                                                                 |
| `StatusBadge`       | Maps `OrbitStatus` (`'healthy' \| 'degraded' \| 'error' \| 'idle' \| 'pending' \| 'unknown'`) to a labelled `Badge` with status dot. Key props: `status: OrbitStatus`, `label?: string`.                                                                 |
| `PageSectionHeader` | Canonical list/management tab header: title + optional description + right-aligned action. Key props: `title`, `description?`, `action?: ReactNode`. **Import from `@orbit/terra`** (promoted from Tier 2).                                              |
| `ConfirmDialog`     | Destructive-action confirmation modal (composes Terra `Modal`). Key props: `open`, `onClose`, `title`, `description`, `confirmLabel`, `onConfirm`, `confirming?`, `error?`, `children?`.                                                                 |
| `MetricCard`        | KPI display card: icon + label + value + optional delta row + optional breakdown list + optional "View all" footer link. Key props: `icon: ReactNode`, `label`, `value`, `delta?: { text, positive, positiveIsBad? }`, `breakdown?`, `href?`.            |
| `EventFeedItem`     | Single row in a `divide-y` activity feed: status dot + primary label (mono) + optional meta + message + timestamp. Key props: `status: OrbitStatus`, `label`, `meta?`, `message`, `timestamp`. Rendered as `<li>` — wrap in `<ul className="divide-y">`. |

Full documentation (import paths, usage guidance, JSX examples): `frontend/docs/golden-paths/app-components.md`.

---

## Related Documents

| Document                                                                                                       | What it covers                                                                |
| -------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------- |
| [`frontend/docs/golden-paths/terra-design-system.md`](./terra-design-system.md)                                | Full Terra component usage guide — props, examples, every lint rule explained |
| [`frontend/docs/golden-paths/frontend-state-pattern.md`](./frontend-state-pattern.md)                          | State management patterns (server actions, URL state, local state)            |
| [`docs/adr/0043-frontend-component-consolidation.md`](../../docs/adr/0043-frontend-component-consolidation.md) | Authoritative ADR for all placement decisions                                 |
| [`frontend/src/packages/terra/src/index.ts`](../../src/packages/terra/src/index.ts)                            | Full Terra export list — source of truth for available component names        |
