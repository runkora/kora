# Reference: Component Placement, Three-Tier Model

**Audience:** Engineers (human and AI) creating or extracting any reusable UI component in the kora frontend.
**Last updated:** 2026-04-28
**Authoritative ADR:** [`docs/adr/2026-05-04-frontend-component-consolidation.md`](../../../docs/adr/2026-05-04-frontend-component-consolidation.md)

> For the placement decision tree and `@tier` annotation rules, see `AGENTS.md` → **Frontend Development**.

---

## The Three-Tier Model

All UI components belong to exactly one of three tiers. The tier determines the filesystem location, import path, authoring requirements, and Storybook story convention.

| Tier  | Name             | Filesystem path                                       | Import path                             | Storybook title prefix                                                  |
| ----- | ---------------- | ----------------------------------------------------- | --------------------------------------- | ----------------------------------------------------------------------- |
| **1** | Terra            | `frontend/src/packages/terra/src/components/`         | `import { X } from '@orbit/terra'`      | `Terra/Atoms/`, `Terra/Molecules/`, `Terra/Layout/`, `Terra/Organisms/` |
| **2** | App-level shared | `frontend/src/components/`                            | `import { X } from '@orbit/components'` | `App/Atoms/`, `App/Molecules/`, `App/Organisms/`                        |
| **3** | Feature-scoped   | `frontend/src/app/(dashboard)/[feature]/_components/` | relative import                         | none required                                                           |

> **`App/Atoms/` and its siblings are Storybook `title` field values: sidebar paths, not filesystem directories.** Setting `title: 'App/Atoms/StatusBadge'` in a story file's meta places it under `App → Atoms → StatusBadge` in the Storybook sidebar. The story file itself lives at `frontend/src/components/`.

---

## Storybook Story Title Conventions

The `title` field in a story's `meta` object sets its position in the Storybook sidebar. These are sidebar hierarchy strings, not filesystem paths.

| Tier              | Story title prefix | Example                                  |
| ----------------- | ------------------ | ---------------------------------------- |
| Tier 1: atoms     | `Terra/Atoms/`     | `title: 'Terra/Atoms/Icon'`              |
| Tier 1: molecules | `Terra/Molecules/` | `title: 'Terra/Molecules/Modal'`         |
| Tier 1: layout    | `Terra/Layout/`    | `title: 'Terra/Layout/Stack'`            |
| Tier 1: organisms | `Terra/Organisms/` | `title: 'Terra/Organisms/DataTable'`     |
| Tier 2: atoms     | `App/Atoms/`       | `title: 'App/Atoms/StatusBadge'`         |
| Tier 2: molecules | `App/Molecules/`   | `title: 'App/Molecules/ScopePicker'`     |
| Tier 2: organisms | `App/Organisms/`   | `title: 'App/Organisms/ConnectionCards'` |

### Choosing atom, molecule or organism

The Terra sub-tier follows what a component is built from:

| Sub-tier  | Rule                                                                                    | Examples                                                   |
| --------- | --------------------------------------------------------------------------------------- | ---------------------------------------------------------- |
| Atoms     | A single primitive. Uses no other Terra component beyond `Icon`, layout and typography. | `Button`, `Badge`, `Progress`, `NavItem`                   |
| Molecules | Composes atoms for one job.                                                             | `CodeBlock`, `Indicator`, `PageSectionHeader`, `FormField` |
| Organisms | Composes molecules or many atoms and owns its own state or flow.                        | `DataTable`, `Wizard`                                      |
| Layout    | Arranges other content and has no content of its own.                                   | `Stack`, `Grid`, `PageLayout`                              |

---

## Terra Authoring Conventions (Tier 1)

New Tier 1 components must follow these patterns (per [terra-frontend-design-system](../../../docs/adr/2026-03-25-terra-frontend-design-system.md)):

- `React.forwardRef` with `React.ComponentPropsWithoutRef<'div'>` (not `React.HTMLAttributes`)
- `data-slot="component-name"` on every rendered element
- `cn()` for class merging, `cva()` for variant maps
- `displayName` set explicitly on all sub-components
- Static Tailwind class strings in variant maps: no dynamic string concatenation
- A Storybook story file in the same directory with `tags: ['autodocs']`

**Exception: Dialog-shell composites.** A component whose entire job is to manage
a Radix `Dialog` lifecycle (open state, focus, scroll lock) behind a small,
deliberately curated prop surface, not a generic element consumers decorate with
arbitrary DOM props, is exempt from the `forwardRef` + `ComponentPropsWithoutRef`
rule. `Modal` (`open`/`onClose`/`size`/`children`) is the existing precedent;
`BottomSheet` (`open`/`onOpenChange`/`title`/`children`) follows the same shape
for the same reason.

---

## Available Terra Components (Tier 1)

All components below are exported from `@orbit/terra`. Never import from sub-paths.

> **`frontend/src/packages/terra/src/index.ts` is the authoritative export list.** This table is a curated view with usage notes; when the two disagree, the index is correct. If a lint error tells you to use a component, it exists in the index. Check there first, then add it here.

### Layout

| Component          | Use instead of                  | Notes                                                                                                                        |
| ------------------ | ------------------------------- | ---------------------------------------------------------------------------------------------------------------------------- |
| `Stack`            | `<div className="flex …">`      | Flex container. Props: `direction`, `gap`, `align`, `justify`, `wrap`, `asChild`, responsive variants.                       |
| `Grid`             | `<div className="grid …">`      | CSS grid container. Props: `cols`, `gap`, `asChild`, responsive variants.                                                    |
| `Section`          | `<section>`                     | Content grouping with optional `heading` + `description` (renders a semantic `<section>` + `<h2>`).                          |
| `Navigation`       | `<nav>`                         | Sidebar nav landmark. Takes `aria-label` (default `'Main'`). This is what `no-raw-nav` tells you to use.                     |
| `List`, `ListItem` | `<ul>` / `<ol>` / `<li>`        | Semantic list container (`<ul>`, or `<ol>` via `ordered`) + item. Props: `gap`, `separator`. Use for `no-raw-list-elements`. |
| `Show`             | `hidden md:block` / `md:hidden` | Responsive visibility gate. `<Show above="md">` or `<Show below="lg">`. Breakpoints: `sm`, `md`, `lg`, `xl`.                 |
| `PageLayout`       | bare page `<div>`               | Top-level page shell for all dashboard pages (organism).                                                                     |

### Typography

| Component   | Use instead of  | Notes                                                                                                                                                             |
| ----------- | --------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `Heading`   | `<h1>`–`<h6>`   | Use `variant` prop. Variants: `display`, `title`, `section`, `subsection`, `label`, `metric`.                                                                     |
| `Text`      | `<p>`, `<span>` | Sizes: `micro`, `caption`, `supporting`, `body`, `lead`. Colors: `default`, `muted`, `subtle`, `status-success`, `status-error`, `status-warning`, `status-info`. |
| `Code`      | `<code>` inline | Inline monospace code inside prose.                                                                                                                               |
| `CodeBlock` | `<pre>` block   | Multi-line code block with copy button, optional `filename` header, and a `wrap` prop for long lines. Props: `text`, `filename?`, `wrap?`.                        |

### Atoms

| Component                                                 | Use instead of                     | Notes                                                                                                                                                                                                                                                                                                                                             |
| --------------------------------------------------------- | ---------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `Icon`                                                    | `import { X } from 'lucide-react'` | **Always use this. Never import from `lucide-react` directly.** Props: `name` (`IconName`), `size` (`2xs`–`xl`), `color`, `container`, `containerVariant` (`accent`, `surface`, `muted`). To add a new icon: propose the icon name and source to a human engineer for approval, then add to `src/packages/terra/src/components/icon/registry.ts`. |
| `KoraMark`                                                | the Kora brand mark                | The circle cut in half, as an SVG. Props: `size` (height in px), `title`.                                                                                                                                                                                                                                                                         |
| `CopyButton`                                              | custom copy button                 | Icon button that copies `value` and shows a check. Never submits a surrounding form.                                                                                                                                                                                                                                                              |
| `Button`                                                  | `<button>`                         | Variants: `default`, `destructive`, `outline`, `secondary`, `ghost`, `link`. Sizes: `sm`, `md`, `lg`, `icon-sm`, `icon`.                                                                                                                                                                                                                          |
| `Badge`                                                   | styled `<span>`                    | Variants: `default`, `secondary`, `destructive`, `outline`.                                                                                                                                                                                                                                                                                       |
| `TruncatingBadge`                                         | `Badge` in a shrinkable wrapper    | Props: `label`, `variant` (`secondary`, `outline`, `elevated`), `textColor`. Ends in an ellipsis with the full label as the tooltip.                                                                                                                                                                                                              |
| `Alert`                                                   | custom warning divs                | Informational / warning / error banner.                                                                                                                                                                                                                                                                                                           |
| `Callout`                                                 | `<aside>` info box                 | Bordered informational callout box. Variants: `info`, `warning`, `success`, `error`. This is what `no-raw-aside` points you to.                                                                                                                                                                                                                   |
| `Input`                                                   | `<input>`                          | Has `hasError?: boolean`, `mono`, `compact`, and `startAdornment?` / `endAdornment?` (React nodes pinned inside the field edges: currency signs, unit labels; named to avoid the HTML `prefix` attribute).                                                                                                                                        |
| `Textarea`                                                | `<textarea>`                       | Multi-line text input.                                                                                                                                                                                                                                                                                                                            |
| `NativeSelect`                                            | `<select>`                         | Native select with consistent styling.                                                                                                                                                                                                                                                                                                            |
| `Checkbox`                                                | `<input type="checkbox">`          | Styled checkbox. Supports `checked`, `indeterminate`, `onChange`, sizes `md`/`lg`. Use for `no-raw-form-elements`.                                                                                                                                                                                                                                |
| `RadioGroup`, `RadioGroupItem`                            | `<input type="radio">`             | Single-select group (Radix). `RadioGroupItem` is the bare 18 px circle: compose with `Label`/`Text` or `Card selected` for card pickers. `value`/`onValueChange` or `defaultValue`.                                                                                                                                                               |
| `Collapsible`, `CollapsibleTrigger`, `CollapsibleContent` | hand-rolled expand/collapse        | Radix expand/collapse region with `aria-expanded` wiring. `CollapsibleContent` animates open/close height by default (respects `prefers-reduced-motion`); all other visual treatment is left to the composition. `open`/`onOpenChange` or `defaultOpen`. Trigger supports `asChild`.                                                              |
| `Toggle`                                                  | custom toggle                      | Toggle switch.                                                                                                                                                                                                                                                                                                                                    |
| `ToggleRow`                                               | hand-rolled label + control row    | Labelled control row (label + optional description + a leading control like `Toggle`/`Checkbox`).                                                                                                                                                                                                                                                 |
| `Label`                                                   | `<label>`                          | Accessible label.                                                                                                                                                                                                                                                                                                                                 |
| `Form`                                                    | `<form>`                           | Semantic `<form>` wrapper with `onSubmit`, `gap`, `maxWidth`. Use for `no-raw-form-elements`.                                                                                                                                                                                                                                                     |
| `Output`                                                  | `<output>`                         | Semantic `<output>` for computed/submitted values; `htmlFor` associates it with a control. `field` prop gives input-like styling.                                                                                                                                                                                                                 |
| `Anchor`                                                  | `<a>`                              | Styled hyperlink. Variants: `default`, `subtle`, `underline`.                                                                                                                                                                                                                                                                                     |
| `Breadcrumbs`                                             | hand-rolled breadcrumb `<nav>`     | Breadcrumb trail from an `items` array (`{ label, href? }`); last item is the current page.                                                                                                                                                                                                                                                       |
| `Separator`                                               | `<hr>` / `<div>` rule              | Horizontal or vertical rule.                                                                                                                                                                                                                                                                                                                      |
| `Skeleton`                                                | custom loading div                 | Loading placeholder.                                                                                                                                                                                                                                                                                                                              |
| `OrbitalLoader`                                           | custom spinner                     | Branded loading animation. Sizes `sm` / `md`.                                                                                                                                                                                                                                                                                                     |
| `StatusDot`                                               | styled coloured `<span>` dot       | Status dot mapped to `--color-status-*` tokens. Optional `glow` halo for live/active states.                                                                                                                                                                                                                                                      |
| `Indicator`                                               | absolutely-positioned dot          | Overlays a small notification status dot on the top-right of a child element.                                                                                                                                                                                                                                                                     |
| `StepDot`, `StepConnector`                                | hand-rolled step markers           | Numbered step dots + connectors for onboarding/wizard flows and rank indicators. `StepDot` sizes `md`/`lg` (30 px, mono) and `state` (`pending`/`current`/`complete`) for wizard rails; `StepConnector complete` tints the line accent.                                                                                                           |
| `Eyebrow`                                                 | hand-rolled ALL-CAPS `<span>`      | Compact uppercase overline label (11 px, bold, widened tracking, subtle). Replaces `Text as="span" size="micro" weight="heavy" color="subtle" transform="uppercase"`.                                                                                                                                                                             |
| `SectionHeading`                                          | hand-rolled step/section header    | Builder-motif section opener: accent icon tile + `subsection` title + optional muted purpose statement. Props: `icon` (`IconName`), `title`, `description?`.                                                                                                                                                                                      |
| `DragCard`                                                | custom draggable card              | Card with `dragging` / `dropTarget` visual states for drag-and-drop lists.                                                                                                                                                                                                                                                                        |
| `Field`, `FieldHint`, `FieldError`                        | manual form wrappers               | Form field container. `FieldError` has `visible` prop so space is always reserved.                                                                                                                                                                                                                                                                |

### Molecules

| Component                                                                                      | Notes                                                                                                                                                                                                                                                                                                                                                                                                        |
| ---------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `Card`, `CardHeader`, `CardTitle`, `CardDescription`, `CardContent`, `CardFooter`              | Content card. `interactive` prop adds focus ring for clickable cards; `selected` renders the accent-border selected state, drawn per `selectedVariant`: `'tint'` (default, accent fill; radio-card pickers) or `'outline'` (elevated bg, no fill; large container cards). Composes with `interactive` for RadioGroup + Card pickers.                                                                         |
| `Modal`, `ModalHeader`, `ModalBody`, `ModalFooter`, `ModalAccessibleTitle`                     | Dialog modal backed by Radix. `ModalHeader` provides required accessible title. Use `ModalAccessibleTitle` (sr-only) for custom-header modals. Sizes `sm`/`md`/`lg` (width only) plus `builder` (1200 × 840 px fixed-height workspace; children fill and scroll internally). Modals hosting collapsible/expandable content must set `ModalBody constrained`, or expansion grows the modal past the viewport. |
| `FormField`                                                                                    | Composes `Label`, `Field`, `FieldHint`, `FieldError` with auto-wired aria attributes. Use `useFormField()` in custom inputs to read `id`, `hasError`, `errorId`.                                                                                                                                                                                                                                             |
| `ChatBubble`, `ChatBubbleActions`                                                              | Chat message bubble. `direction="start"` (inbound/AI) or `direction="end"` (user/outbound). `ChatBubbleActions` must be a direct child of `ChatBubble` to inherit `group-hover` visibility.                                                                                                                                                                                                                  |
| `InputSurface`, `TypingIndicator`, `StreamingCursor`                                           | Chat primitives: styled chat-input container, animated "AI is typing" dots, and blinking streaming cursor.                                                                                                                                                                                                                                                                                                   |
| `SidePanel`, `SidePanelHeader`, `SidePanelBody`, `SidePanelFooter`, `SidePanelAccessibleTitle` | Slide-over side panel (sizes `md`/`lg`). This is what `no-raw-aside` / `no-raw-footer` point you to for panel chrome.                                                                                                                                                                                                                                                                                        |
| `ButtonGroup`                                                                                  | Horizontal row of buttons fused into a single segmented control.                                                                                                                                                                                                                                                                                                                                             |
| `NavItem`                                                                                      | Sidebar nav item with `active` state and icon slot.                                                                                                                                                                                                                                                                                                                                                          |
| `Tooltip`, `TooltipProvider`, `TooltipTrigger`, `TooltipContent`                               | Hover tooltip (Radix). Wrap app root with `TooltipProvider`.                                                                                                                                                                                                                                                                                                                                                 |
| `Popover`, `PopoverTrigger`, `PopoverAnchor`, `PopoverContent`                                 | Click-triggered floating panel.                                                                                                                                                                                                                                                                                                                                                                              |
| `Tabs`, `TabsList`, `TabsTrigger`, `TabsContent`                                               | Horizontal tab navigation. `TabsTrigger` takes `icon?: IconName` for a leading icon before the label.                                                                                                                                                                                                                                                                                                        |
| `RadioCard`                                                                                    | Single-select option card (icon tile + radio + title + description) on a `Card interactive selected` surface. Must render inside a `RadioGroup`. Props: `value`, `selected`, `onSelect(value)`, `icon?`, `title`, `description?`.                                                                                                                                                                            |
| `SectionNav`                                                                                   | Vertical section rail: tile buttons with a numbered `StepDot` when inactive and an accent icon tile when active. Props: `sections` (`{ key, title, sub?, icon }[]`), `active`, `onSelect(key)`.                                                                                                                                                                                                              |
| `SaveBar`                                                                                      | Floating bottom-centre save bar for dirty forms: pulsing status dot, optional error, Discard + Save actions; success state hides the actions. Props: `visible`, `saving?`, `success?`, `error?`, `onSave`, `onDiscard`, plus `dirtyLabel?`/`successLabel?`/`saveLabel?`/`discardLabel?`.                                                                                                                     |
| `Progress`                                                                                     | Horizontal progress bar. Sizes `hairline` (2 px) / `sm` / `md` / `lg`; status-token colour variants.                                                                                                                                                                                                                                                                                                         |
| `PageSectionHeader`                                                                            | Section header: `title` (`Heading variant="title"`) + optional `description` + right-aligned `action`. Replaces hand-rolled headers.                                                                                                                                                                                                                                                                         |
| `StatCard`                                                                                     | One headline figure with a label, optional icon, `detail`, `delta`, `breakdown` and `footer` slot. Tones: `default`, `healthy`, `degraded`, `error`.                                                                                                                                                                                                                                                         |
| `CopyField`                                                                                    | Read-only value that copies on click. Props: `value`, `label`, `mono`, `bordered`, `wrap`, `fullWidth`, `ariaLabel`. Never submits a surrounding form.                                                                                                                                                                                                                                                       |
| `EmptyState`                                                                                   | Zero-state placeholder: `icon`, `title`, `description`, optional `action`.                                                                                                                                                                                                                                                                                                                                   |
| `ConfirmDialog`                                                                                | Destructive-action confirmation built on `Modal`: `open`, `onClose`, `title`, `description`, `confirmLabel`, `onConfirm`, `confirming?`, `error?`.                                                                                                                                                                                                                                                           |

### Data Display

| Component                                                           | Notes                                                                                                                                        |
| ------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------- |
| `DataTable`                                                         | Fully opinionated TanStack Table. Handles sorting, filtering, pagination, column visibility. Prefer over composing raw table sub-components. |
| `TableContainer`, `TableEmpty`, `TableToolbar`, `TableColumnHeader` | Table wrappers and helpers.                                                                                                                  |
| `ChartContainer`, `ChartTooltipContent`, `ChartLegendContent`       | Recharts wrappers with consistent theme.                                                                                                     |

### Organisms

| Component                                                                                                          | Notes                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                         |
| ------------------------------------------------------------------------------------------------------------------ | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `Wizard`, `Wizard.Header`, `Wizard.Rail`, `Wizard.Step`, `Wizard.ReviewStep`, `Wizard.ReviewCard`, `Wizard.Footer` | Multi-step builder modal chrome. Renders inside `Modal size="builder"`. `Wizard` root takes a `rail` prop (`React.ReactElement<WizardRailProps>`) and a `footer` prop (`React.ReactElement<WizardFooterProps>`): passing arbitrary content in these slots is a TypeScript error. Children should include one `Wizard.Header` and one active `Wizard.Step` (or `Wizard.ReviewStep` on the final step). `Wizard.Rail` renders the vertical step navigator with `StepDot`/`StepConnector`; steps past `maxVisited` are disabled. `Wizard.ReviewCard` renders a `Card` of key/value rows with an optional `children` slot for block-level content (e.g. multi-line prompts). `Wizard.Footer` renders Back / Continue / Submit controls; omit `onBack` to hide Back; pass `onSubmit` to swap Continue to Submit on the final step. |

### Hooks

| Hook                       | Notes                                                                                                                        |
| -------------------------- | ---------------------------------------------------------------------------------------------------------------------------- |
| `useDialog(open, onClose)` | Syncs a `<dialog>` element with React `open`/`onClose`. Returns `{ ref, handleBackdropClick }`.                              |
| `useCopy(value)`           | Writes `value` to the clipboard. Returns `{ status, copy }`; `status` is `idle`, `copied` or `failed` and resets after 1.5s. |
| `useFormField()`           | Reads `id`, `hasError`, `errorId` from `FormField` context. Use inside custom input components.                              |

---

## Banned Patterns (ESLint: violations fail `pnpm lint`)

| Rule                                | What is banned                                                                                     | Use instead                                                                                                         |
| ----------------------------------- | -------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------- |
| `local/no-raw-layout-elements`      | `<div>`, `<section>`, `<aside>`, `<nav>`, `<header>`, `<footer>`, `<article>`, `<main>`            | `Stack`, `Grid`, or `Stack asChild`/`Grid asChild` for semantic elements                                            |
| `local/no-raw-html-text-tags`       | `<p>`, `<h1>`–`<h6>`                                                                               | `Text`, `Heading`                                                                                                   |
| `local/no-raw-inline-elements`      | `<span>`, `<strong>`, `<b>`, `<em>`, `<i>`, `<code>`, `<kbd>`, and all other phrasing-content HTML | `Text`, `Text weight="heavy"`, `Code`                                                                               |
| `local/no-raw-interactive-elements` | `<button>`                                                                                         | `Button`                                                                                                            |
| `local/no-raw-list-elements`        | `<ul>`, `<ol>`, `<li>`                                                                             | `List`, `List ordered`, `ListItem` (or `Stack` for pure layout containers)                                          |
| `local/no-raw-form-elements`        | `<form>`, `<label>`, `<input>`, `<output>`                                                         | `Form`, `Label` (or `Checkbox` for labelled checkboxes), `Input` / `Checkbox`, `Output`                             |
| `local/no-terra-classname`          | `className` on any Terra component                                                                 | Use the component's own props                                                                                       |
| `local/no-direct-lucide-import`     | `import { X } from 'lucide-react'` in `src/app/**` or `src/components/**`                          | `<Icon name="…">`: **no escape hatch**. Add new icons to `src/packages/terra/src/components/icon/registry.ts` only. |
| `local/no-terra-disable-comment`    | Any `eslint-disable` comment targeting a Terra rule                                                | There is no escape hatch. Use the correct Terra component                                                           |

---

## Structural ESLint rules (server/client boundary + filenames)

Beyond the Terra design-system rules, three structural rules in
`frontend/eslint.config.mjs` are **error-severity** and are among the most common
first-PR failures. They enforce the App Router server/client boundary and a
consistent filename convention. None have an escape hatch.

| Rule                                 | Applies to                                                                   | What it requires / when it fires                                                                                                                                                                                                                                | How to satisfy it                                                                                    |
| ------------------------------------ | ---------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------- |
| `local/require-server-only`          | `src/server/**/*.{ts,tsx}` and every Route Handler `src/app/api/**/route.ts` | Fires unless `import 'server-only'` is the **first** import in the file. Both a missing guard and a guard that is not first fail. Prevents server-only modules (backend URLs, session secret) leaking into the client bundle.                                   | Add `import 'server-only';` as the very first import (after any JSDoc block).                        |
| `local/require-use-client-for-hooks` | `src/app/**/*.{ts,tsx}` except Route Handlers                                | Fires when a file imports a React client hook (`useState`, `useEffect`, `useRef`, …) from `react`, or a navigation hook (`useRouter`, `usePathname`, `useSearchParams`, …) from `next/navigation`, but has no `"use client"` directive.                         | Add `"use client";` as the first line of the file.                                                   |
| `local/require-kebab-filename`       | all `src/**/*.{js,jsx,ts,tsx}` (including the Terra package)                 | Fires when a filename stem is not kebab-case (e.g. `HealthStatus.tsx`, `myComponent.ts`). Prevents case-sensitivity mismatches between macOS (case-insensitive) and Linux CI (case-sensitive). Dot-files and Next.js dynamic segments (`[id].tsx`) are skipped. | Rename the file to kebab-case (the error message suggests the exact name, e.g. `health-status.tsx`). |

## Checking Your Work

Before marking any frontend change complete:

```bash
cd frontend && pnpm lint           # ESLint — must pass with zero errors
cd frontend && pnpm tsc --noEmit   # TypeScript — must pass with zero errors
```

---

## Implementation Rule 9: Keep This Document Current

When you add a new component at any tier, update this document in the **same PR**. A component not listed in the inventory is invisible to the next agent and will be duplicated.

---

## Tier 2: App-Shared Component Inventory

Tier 2 components live at `frontend/src/components/` and are exported from `frontend/src/components/index.ts`. Import all Tier 2 components via `@orbit/components` (the `@orbit/*` path alias maps to `./src/*`).

| Component           | Description                                                                                                                                                                                                                                             |
| ------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `ErrorBanner`       | **Removed**: use `<Alert variant="error">` from `@orbit/terra` with `<Icon name="triangle-alert" color="status-error" />` and `<Text size="caption" color="status-error">` as children.                                                                 |
| `StatusBadge`       | Maps `OrbitStatus` (`'healthy' \| 'degraded' \| 'error' \| 'idle' \| 'pending' \| 'unknown'`) to a labelled `Badge` with status dot. Key props: `status: OrbitStatus`, `label?: string`.                                                                |
| `PageSectionHeader` | Canonical list/management tab header: title + optional description + right-aligned action. Key props: `title`, `description?`, `action?: ReactNode`. **Import from `@orbit/terra`** (promoted from Tier 2).                                             |
| `EventFeedItem`     | Single row in a `divide-y` activity feed: status dot + primary label (mono) + optional meta + message + timestamp. Key props: `status: OrbitStatus`, `label`, `meta?`, `message`, `timestamp`. Rendered as `<li>`, wrap in `<ul className="divide-y">`. |

Full documentation (import paths, usage guidance, JSX examples): `frontend/docs/reference/app-components.md`.

---

## Related Documents

| Document                                                                                                                      | What it covers                                                               |
| ----------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------- |
| [`frontend/docs/reference/terra-design-system.md`](./terra-design-system.md)                                                  | Full Terra component usage guide: props, examples, every lint rule explained |
| [`frontend/docs/reference/app-components.md`](./app-components.md)                                                            | Tier 2 component API: StatusBadge, EventFeedItem                             |
| [`frontend/docs/golden-paths/frontend-state-pattern.md`](../golden-paths/frontend-state-pattern.md)                           | State management patterns (server actions, URL state, local state)           |
| [`docs/adr/2026-05-04-frontend-component-consolidation.md`](../../../docs/adr/2026-05-04-frontend-component-consolidation.md) | Authoritative ADR for all placement decisions                                |
| [`frontend/src/packages/terra/src/index.ts`](../../src/packages/terra/src/index.ts)                                           | Full Terra export list: source of truth for available component names        |
