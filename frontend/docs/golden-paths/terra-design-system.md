# Golden Path: Building UI with the Terra Design System

**Audience:** Frontend engineers writing any JSX in the Orbit frontend.  
**Pattern version:** 1.0  
**Reference implementation:** `src/packages/terra/src/` — the Terra source is the canonical example of every component used correctly.  
**Related:** `frontend/docs/golden-paths/ui-components.md` — where to put components (tier placement model, decision tree, full Terra inventory, `@tier` annotations).

---

> ⚠️ **The conventions in this document are enforced by five ESLint rules in `frontend/eslint.config.mjs`. Violations will fail `pnpm lint`.**

## Before You Start

Terra (`@orbit/terra`) is the **mandatory** UI foundation for all product JSX. Five local ESLint rules enforce this — there are no exceptions in product code:

| Rule                           | What it bans                                                                                               | Escape hatch                                      |
| ------------------------------ | ---------------------------------------------------------------------------------------------------------- | ------------------------------------------------- |
| `local/no-raw-div`             | Every `<div>`                                                                                              | `eslint-disable-next-line` with a one-line reason |
| `local/no-raw-layout-elements` | Semantic containers (`section`, `header`, `footer`, `nav`, `aside`, `article`, `main`) with layout classes | None — use `<Stack asChild>` or `<Grid asChild>`  |
| `local/no-raw-inline-elements` | Every raw inline element (`span`, `strong`, `b`, `em`, `i`, `mark`, `abbr`, `s`, `u`, `sub`, `sup`)        | **None** — add a Terra component instead          |
| `local/no-raw-html-text-tags`  | `<p>` and `<h1>`–`<h6>`                                                                                    | None — use `<Text>` and `<Heading>`               |
| `local/no-terra-classname`     | `className` on any Terra component                                                                         | None — use the component's design-token props     |

All five rules fire in `src/**/*.tsx` and are ignored only inside `src/packages/terra/**` (Terra is the implementation layer and must render raw HTML to provide these components).

**If a Terra component is missing for your use case, add one — do not use raw HTML.** See [Adding a Missing Terra Component](#adding-a-missing-terra-component) at the bottom of this guide.

---

## Quick-Reference Replacement Table

The most common migrations at a glance.

| Raw HTML                                              | Terra replacement                                                                                                                         |
| ----------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------- |
| `<div className="flex flex-col gap-4">`               | `<Stack gap="comfortable">`                                                                                                               |
| `<div className="flex flex-row gap-2">`               | `<Stack direction="horizontal" gap="snug">`                                                                                               |
| `<div className="grid grid-cols-3 gap-4">`            | `<Grid cols={3} gap="comfortable">`                                                                                                       |
| `<div className="flex items-center justify-between">` | `<Stack direction="horizontal" align="center" justify="between">`                                                                         |
| `<section className="flex flex-col gap-8">`           | `<Stack asChild gap="relaxed"><section>…</section></Stack>`                                                                               |
| `<aside className="grid grid-cols-2">`                | `<Grid asChild cols={2}><aside>…</aside></Grid>`                                                                                          |
| `<h1>Page title</h1>`                                 | `<Heading variant="title">Page title</Heading>`                                                                                           |
| `<h2>Section heading</h2>`                            | `<Heading variant="section">Section heading</Heading>`                                                                                    |
| `<p>Body copy</p>`                                    | `<Text>Body copy</Text>`                                                                                                                  |
| `<p className="text-sm text-muted">Helper</p>`        | `<Text size="supporting" color="muted">Helper</Text>`                                                                                     |
| `<span>Inline label</span>`                           | `<Text as="span" size="supporting">Inline label</Text>` (inline context) or `<Text size="supporting">Inline label</Text>` (block context) |
| `<strong>Important</strong>`                          | `<Text weight="heavy">Important</Text>`                                                                                                   |
| `<b>Bold text</b>`                                    | `<Text weight="heavy">Bold text</Text>`                                                                                                   |
| `<em>Emphasised</em>`                                 | No Terra equivalent yet — add a Terra atom first                                                                                          |
| `<i>Italic</i>`                                       | No Terra equivalent yet — add a Terra atom first                                                                                          |
| `<code>agent-id</code>`                               | `<Code>agent-id</Code>`                                                                                                                   |

---

## Layout: Replacing `<div>`

Every `<div>` must become a `Stack` (flex) or `Grid` (CSS grid). These are the two layout primitives in Terra.

### Stack — flex layouts

`Stack` replaces `<div className="flex …">`. It renders a `div` by default.

```tsx
import { Stack } from '@orbit/terra';

// ❌ Banned
<div className="flex flex-col gap-4">…</div>

// ✅ Correct
<Stack gap="comfortable">…</Stack>
```

**Common Stack props:**

| Prop        | Type                                                                | Default      | Purpose                            |
| ----------- | ------------------------------------------------------------------- | ------------ | ---------------------------------- |
| `direction` | `'vertical' \| 'horizontal' \| Responsive<…>`                       | `'vertical'` | Flex direction                     |
| `gap`       | `SpacingSize \| Responsive<SpacingSize>`                            | `'snug'`     | Gap between children               |
| `align`     | `'start' \| 'center' \| 'end' \| 'stretch' \| 'baseline'`           | —            | Cross-axis (`items-*`)             |
| `justify`   | `'start' \| 'center' \| 'end' \| 'between' \| 'around' \| 'evenly'` | —            | Main-axis (`justify-*`)            |
| `wrap`      | `boolean`                                                           | `false`      | `flex-wrap`                        |
| `maxWidth`  | `ContainerWidth \| Responsive<…>`                                   | —            | Max width constraint               |
| `minHeight` | `ContainerHeight \| Responsive<…>`                                  | —            | Min height constraint              |
| `textAlign` | `'start' \| 'center' \| 'end'`                                      | —            | Text alignment for all descendants |
| `asChild`   | `boolean`                                                           | `false`      | Merge classes onto child element   |

**Spacing size tokens** (smallest → largest): `none`, `tight`, `snug`, `comfortable`, `relaxed`, `spacious`.

```tsx
// Horizontal toolbar: items centred, space between left and right groups
<Stack direction="horizontal" align="center" justify="between">
  <Heading variant="label">Active agents</Heading>
  <Button size="sm">New agent</Button>
</Stack>

// Responsive direction: column on mobile, row on md+
<Stack direction={{ base: 'vertical', md: 'horizontal' }} gap="comfortable">
  …
</Stack>

// Full-screen centred wrapper — no extra DOM node via asChild
// DOM output: <main class="flex flex-col min-h-dvh items-center justify-center">
<Stack asChild direction="vertical" align="center" justify="center" minHeight="screen">
  <main>…</main>
</Stack>

// Badge row that wraps at narrow widths
<Stack direction="horizontal" wrap gap="tight">
  {tags.map((t) => <Badge key={t}>{t}</Badge>)}
</Stack>
```

### Grid — equal-column layouts

`Grid` replaces `<div className="grid grid-cols-N gap-…">`.

```tsx
import { Grid } from '@orbit/terra';

// ❌ Banned
<div className="grid grid-cols-3 gap-6">…</div>

// ✅ Correct
<Grid cols={3} gap="comfortable">…</Grid>
```

**Grid props:**

| Prop            | Type                                     | Default  |
| --------------- | ---------------------------------------- | -------- |
| `cols`          | `ColCount \| Responsive<ColCount>`       | `1`      |
| `gap`           | `SpacingSize \| Responsive<SpacingSize>` | `'snug'` |
| `gapX` / `gapY` | `SpacingSize \| Responsive<SpacingSize>` | —        |
| `asChild`       | `boolean`                                | `false`  |

`ColCount` values: `1 | 2 | 3 | 4 | 5 | 6 | 12`.

```tsx
// 1 column on mobile, 2 on small, 3 on large screens
<Grid cols={{ base: 1, sm: 2, lg: 3 }} gap="comfortable">
  {cards.map((c) => (
    <Card key={c.id}>…</Card>
  ))}
</Grid>
```

---

## Layout: Semantic Elements as Layout Containers

`<section>`, `<aside>`, `<nav>`, `<header>`, `<footer>`, `<article>`, and `<main>` must not carry display-mode Tailwind classes directly. Use `asChild` to merge layout classes onto the semantic element without an extra DOM node.

```tsx
// ❌ Banned — semantic element carrying layout classes directly
<section className="flex flex-col gap-8 p-6">…</section>
<aside className="grid grid-cols-2 gap-4">…</aside>

// ✅ Correct — layout lives in Stack/Grid; semantic element is the rendered node
<Stack asChild gap="relaxed">
  <section>…</section>
</Stack>

<Grid asChild cols={2} gap="comfortable">
  <aside>…</aside>
</Grid>
```

> **Note:** Plain `<span>` without layout classes is also banned unconditionally by `no-raw-inline-elements` — see [Inline Elements](#typography-replacing-inline-elements) below.

---

## Typography: Headings

Every heading must use `Heading`. The variant selects both the visual scale and the default HTML element — you never need to write `h1`–`h4` directly.

```tsx
import { Heading } from '@orbit/terra';

// ❌ Banned
<h1 className="text-3xl font-bold">Agents</h1>
<h2 className="text-2xl font-semibold text-muted">Configuration</h2>

// ✅ Correct
<Heading variant="title">Agents</Heading>
<Heading variant="section" color="muted">Configuration</Heading>
```

**Heading variants:**

| Variant      | Default element | Use for                                           |
| ------------ | --------------- | ------------------------------------------------- |
| `display`    | `h1`            | Hero text on splash/landing screens               |
| `title`      | `h1`            | The single page-level title per view              |
| `section`    | `h2`            | Major section headings (most common)              |
| `subsection` | `h3`            | Secondary groupings within a section              |
| `label`      | `h4`            | Card titles, sidebar headers, group labels        |
| `metric`     | `div`           | Large KPI/stat numbers (not a structural heading) |

**Heading colors:** `foreground` (default), `muted`, `accent`.

```tsx
<Heading variant="display">Orbit</Heading>
<Heading variant="title">Agents</Heading>
<Heading variant="metric">4,821</Heading>
<Heading variant="label" color="muted">Last 7 days</Heading>

// Render as a router Link without losing heading styles
<Heading variant="section" asChild>
  <Link href="/agents">Agents</Link>
</Heading>
```

---

## Typography: Body Copy (`Text`)

`Text` covers all prose, metadata, captions, and descriptive strings. It replaces both `<p>` and `<span>`.

```tsx
import { Text } from '@orbit/terra';

// ❌ Banned
<p className="text-sm text-muted">Last seen 2 minutes ago</p>
<span>Inline label</span>

// ✅ Correct
<Text size="supporting" color="muted">Last seen 2 minutes ago</Text>
<Text as="span" size="supporting">Inline label</Text>
```

**Text size variants (semantic names):**

| Size         | px   | Use for                                       |
| ------------ | ---- | --------------------------------------------- |
| `micro`      | 11px | Chat timestamps, keyboard hints, disclaimers  |
| `caption`    | 12px | Timestamps, status labels, auxiliary metadata |
| `supporting` | 14px | Secondary body, descriptions, table metadata  |
| `body`       | 16px | Default prose — the majority of UI text       |
| `lead`       | 18px | Introductory paragraphs, feature descriptions |

**Text color:** `foreground`, `muted`, `subtle`, `accent`, `status-healthy`, `status-degraded`, `status-error`.

**Text weight:** `regular`, `emphasis` (medium), `strong` (semibold), `heavy` (bold).

```tsx
// Eyebrow label
<Text size="caption" weight="strong" transform="uppercase" color="subtle">
  Today
</Text>

// Truncated filename
<Text size="supporting" weight="emphasis" truncate>
  very-long-agent-name-that-overflows.json
</Text>

// Technical identifier
<Text size="supporting" mono>apollo-retriever</Text>

// Numeric counter (tabular to prevent layout jitter)
<Text size="supporting" mono tabular>{runs.toLocaleString()}</Text>

// Status delta
<Text size="caption" color="status-healthy">+2 this hour</Text>

// Disclaimer centred below a form
<Text size="caption" align="center" color="subtle">
  Orbit AI can make mistakes. Review important information.
</Text>
```

---

## Typography: Replacing Inline Elements

The `no-raw-inline-elements` rule fires unconditionally on all raw inline HTML. There is no `eslint-disable` escape hatch.

### `<span>` → `Text` or `Text as="span"`

```tsx
// ❌ Banned
<span className="text-sm text-muted">helper text</span>
<span>plain label</span>

// ✅ Correct — render a p with Text's styles
<Text size="supporting" color="muted">helper text</Text>

// ✅ Correct — when the element must be inline (inside a <button>, <a>, <p>, etc.)
<Text as="span" size="supporting" color="muted">helper text</Text>
```

> **`as="span"` vs `asChild` on Text:**
>
> - Use `as="span"` when the rendered element must be inline — for example inside a `<button>`, `<a>`, `<p>`, or `<li>` where a nested `<p>` would be invalid HTML. This is the correct way to express inline text without writing a raw `<span>`.
> - Use `asChild` when you need to merge Text styles onto a child element you already control (e.g. a router `<Link>`). The child must not be a raw `<span>` — that is still caught by `no-raw-inline-elements`.
> - In all other cases, use `<Text>` directly (it renders `<p>`).

### `<strong>` / `<b>` → `Text weight="heavy"`

```tsx
// ❌ Banned
<strong>Critical alert</strong>
<b>Required field</b>

// ✅ Correct
<Text weight="heavy">Critical alert</Text>

// Inside a sentence — must remain inline, so use asChild
<Text asChild size="body" weight="heavy">
  <span>Critical alert</span>
</Text>
```

Use `weight="strong"` (semibold) for column headers and key values; use `weight="heavy"` (bold) for maximum emphasis.

### `<em>` / `<i>` — no Terra equivalent yet

Terra has no italic primitive. `no-raw-inline-elements` bans `<em>` and `<i>` unconditionally, and `no-terra-classname` bans `className` on `<Text>`, so `<Text className="italic">` is also invalid.

If italics are genuinely needed, add a Terra component first — see [Adding a Missing Terra Component](#adding-a-missing-terra-component). Until then, the use case is not supported in product code.

### `<em>`, `<i>`, `<mark>`, `<abbr>`, `<s>`, `<u>`, `<sub>`, `<sup>`

These elements have **no current Terra equivalent**. `local/no-raw-inline-elements` bans them unconditionally. If your use case genuinely requires one of these:

1. Do not use the raw element — `pnpm lint` will fail.
2. Add a Terra component for the use case first — see [Adding a Missing Terra Component](#adding-a-missing-terra-component).
3. Use the new Terra component from product code.

The lint error message will read:

> _Raw `<em>` has no Terra equivalent yet. Add a Terra atom in `src/packages/terra/src/components/` first, then use it here._

This is intentional: the rule prevents the codebase from accumulating ad-hoc HTML while the design system grows to cover new needs ONLY IF NECESSARY.

---

## Typography: Inline Code (`Code`)

`Code` renders a `<code>` element with consistent monospace styling. Use it for technical identifiers inside prose.

```tsx
import { Code } from '@orbit/terra';

// ❌ Banned
<code>ORBIT_API_KEY</code>

// ✅ Correct
<Code>ORBIT_API_KEY</Code>                      // default: inline (subtle bg)
<Code variant="bare">--color-accent</Code>      // inside a table cell or badge
<Code variant="highlight">ORBIT_API_KEY</Code>  // deliberate emphasis
```

For a whole cell or label that is a technical value (not inside prose), use `Text mono` instead:

```tsx
// Text mono — whole element is a technical value
<Text size="caption" color="subtle" mono>agt_01HZ9K3V</Text>

// Code — technical string embedded in prose
<Text size="body">
  Set the <Code>ORBIT_API_KEY</Code> environment variable before starting.
</Text>
```

---

## The `asChild` Pattern

Both layout and typography components support `asChild`. When `true`, the component merges its classes onto the **immediate child element** via Radix Slot instead of rendering a wrapper element. This is how you apply Terra primitives to semantic HTML without adding extra DOM nodes.

```tsx
// Without asChild — Stack renders a <div> wrapping <nav>
// DOM: <div class="flex flex-col gap-2"><nav>…</nav></div>
<Stack gap="snug">
  <nav>…</nav>
</Stack>

// With asChild — layout classes land directly on <nav>
// DOM: <nav class="flex flex-col gap-2">…</nav>
<Stack asChild gap="snug">
  <nav>…</nav>
</Stack>

// Text as="span" — renders a <span> instead of the default <p> for inline contexts
// DOM: <span class="font-sans text-sm text-muted">…</span>
<Text as="span" size="supporting" color="muted">Inline metadata</Text>

// Heading asChild — heading styles on a router Link
// DOM: <a class="font-sans tracking-tight text-3xl font-bold …" href="…">…</a>
<Heading variant="title" asChild>
  <Link href="/agents">Agents</Link>
</Heading>
```

**Rules for `asChild`:**

- The child must be a single element — no fragments, no arrays.
- The child must accept a `className` prop (all HTML elements and most components do).
- Do not nest `asChild` components: `<Stack asChild><Grid asChild>…</Grid></Stack>` is incorrect.

---

## Page-Level Layout

Use `Section` for page sections and `PageLayout` as the top-level page wrapper. These are higher-level primitives that compose `Stack` and `Grid` internally.

```tsx
import { PageLayout, Section } from '@orbit/terra';

// Top-level page shell
<PageLayout>
  <Section>
    <Heading variant="title">Agents</Heading>
    <Stack gap="comfortable">{/* feature content */}</Stack>
  </Section>
</PageLayout>;
```

---

## Adding a Missing Terra Component

If none of the existing Terra components cover your use case and you need a raw HTML element:

1. **Do not use the raw element.** The lint rules have no escape hatch for `no-raw-inline-elements`.
2. **Create a new Terra component** following the ShadCN convention:
   - Base it on a ShadCN/Radix UI component where one exists.
   - When no Radix primitive exists (as with `Alert`), follow ShadCN's styled-div convention.
   - Use `cva` + `cn`, `React.forwardRef`, `.displayName`, and `data-slot` attributes.
   - Use `React.ComponentPropsWithoutRef<'div'>` (not `React.HTMLAttributes<HTMLDivElement>`).
   - Add the component to `src/packages/terra/src/index.ts`.
3. **Replace the raw element** with the new component across the codebase.

See `src/packages/terra/src/components/alert/alert.tsx` as a reference for a styled-div component with no Radix primitive.

---

## Verification Checklist

Use this checklist before opening a PR. Every item must be true.

### Layout

- [ ] No `<div>` elements in product JSX — all replaced with `Stack` or `Grid`.
- [ ] No semantic elements (`section`, `aside`, `nav`, etc.) carrying display-mode Tailwind classes directly — use `Stack asChild` or `Grid asChild`.
- [ ] `Stack` props use token values (`gap="comfortable"`) — no raw Tailwind gap classes via `className`.
- [ ] Responsive layout props use the `{ base, sm, md, lg, xl }` object form — no conditional `cn()` for breakpoints.
- [ ] `asChild` is used when the rendered element must be semantic HTML — not to avoid adding a DOM node for its own sake.

### Typography

- [ ] No `<h1>`–`<h6>` elements — all replaced with `Heading` using the correct variant.
- [ ] No `<p>` elements — all replaced with `Text`.
- [ ] No `<span>` elements — replaced with `Text` (block context) or `Text as="span"` (inline context, e.g. inside a `<button>` or `<a>`).
- [ ] No `<strong>` or `<b>` elements — replaced with `Text weight="heavy"` or `weight="strong"`.
- [ ] No `<em>` or `<i>` elements — no Terra equivalent yet; a Terra component must be added first (see [Adding a Missing Terra Component](#adding-a-missing-terra-component)).
- [ ] No `<code>` elements — replaced with `Code` (in prose) or `Text mono` (standalone technical values).
- [ ] No `<mark>`, `<abbr>`, `<s>`, `<u>`, `<sub>`, `<sup>` elements — a Terra component has been added for the use case.
- [ ] `Text as="span"` is used when the rendered element must be inline (e.g. inside a `<button>`, `<a>`, `<p>`, or `<li>`); `asChild` is used only to merge Text styles onto a non-span child element you already control.

### ESLint

- [ ] `pnpm lint` passes with zero errors.
- [ ] No `eslint-disable-next-line local/no-raw-inline-elements` comments — this rule has no escape hatch.
- [ ] Any `eslint-disable-next-line local/no-raw-div` comments include a one-line explanation.

### New Terra components (if applicable)

- [ ] Component is created from a ShadCN/Radix UI base — not from scratch.
- [ ] Component uses `cva` + `cn`, `React.forwardRef`, `.displayName`, and `data-slot`.
- [ ] Component exported from `src/packages/terra/src/index.ts`.
- [ ] Component is categorised correctly: atom, molecule, or organism.

---

_Guide owned by: Frontend Engineering_  
_Last updated: 2026-04-17_
