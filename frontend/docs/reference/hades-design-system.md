# Reference: Hades Design System Usage

**Audience:** Frontend engineers writing any JSX in the kora frontend.  
**Reference implementation:** `src/packages/hades/src/`, the Hades source that is the canonical example of every component used correctly.  
**Related:** `frontend/docs/reference/component-placement.md`, where to put components (tier placement model, full Hades inventory, `@tier` annotations); `AGENTS.md` → **Frontend Development** for the placement decision tree.

---

> ⚠️ **The conventions in this document are enforced by the Hades design-system ESLint rules in `frontend/eslint.config.mjs` (enumerated below). Violations will fail `pnpm lint`.**

## Before You Start

Hades (`@orbit/hades`) is the **mandatory** UI foundation for all product JSX. These local ESLint rules enforce it, with no exceptions in product code. The enforced set is `HADES_RULES` in `frontend/eslint.config.mjs`.

| Rule                                | What it bans                                                                                                                                       | Escape hatch                                                                                                                                                                                                              |
| ----------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `local/no-raw-layout-elements`      | Every `<div>` (unconditional), `<section>`, `<aside>`, `<nav>`, `<header>`, `<footer>`, `<article>` (unconditional) + `<main>` with layout classes | **None**: use `<Stack>` / `<Grid>` / `<Section>` / `<Navigation>` / `<Callout>` / `<SidePanel>`; for other semantic HTML use `<Stack asChild>` or `<Grid asChild>`; if missing, propose a new Hades component (see below) |
| `local/no-raw-inline-elements`      | Every raw inline element (`span`, `strong`, `b`, `em`, `i`, `mark`, `abbr`, `s`, `u`, `sub`, `sup`, `code`, `kbd`, `samp`, `var`)                  | **None**: use a Hades equivalent; if missing, propose a new Hades component (see below)                                                                                                                                   |
| `local/no-raw-html-text-tags`       | `<p>` and `<h1>`–`<h6>`                                                                                                                            | **None**: use `<Text>` and `<Heading>`                                                                                                                                                                                    |
| `local/no-raw-interactive-elements` | `<button>`                                                                                                                                         | **None**: use `<Button>` from `@orbit/hades`                                                                                                                                                                              |
| `local/no-raw-list-elements`        | `<ul>`, `<ol>`, `<li>`                                                                                                                             | **None**: use `<List>` (or `<List ordered>`) and `<ListItem>` from `@orbit/hades`; `<Stack>` for pure layout containers                                                                                                   |
| `local/no-raw-form-elements`        | `<form>`, `<label>`, `<input>`, `<output>`                                                                                                         | **None**: use `<Form>`, `<Label>` (or `<Checkbox>` for labelled checkboxes), `<Input>` / `<Checkbox>`, `<Output>` from `@orbit/hades`                                                                                     |
| `local/no-direct-lucide-import`     | `import { X } from 'lucide-react'`                                                                                                                 | **None**: use `<Icon name="…" />`; add new icons to `src/packages/hades/src/components/icon/registry.ts` only                                                                                                             |
| `local/no-hades-classname`          | `className` on any Hades component                                                                                                                 | **None**: use the component's design-token props                                                                                                                                                                          |

> 🚫 **`eslint-disable` comments targeting any of these rules are themselves a lint error** (`local/no-hades-disable-comment`). There are no escape hatches. Fix violations by using the correct Hades component. If no Hades component exists for your use case, write a proposal and wait for human engineer approval before making any changes to Hades (see [Proposing a Missing Hades Component](#proposing-a-missing-hades-component)).

Every rule above fires in `src/**/*.tsx` and is ignored only inside `src/packages/hades/**` (Hades is the implementation layer and must render raw HTML to provide these components).

> **Structural rules too.** Beyond the design-system rules above, three structural ESLint rules (`require-server-only`, `require-use-client-for-hooks`, `require-kebab-filename`) are error-severity and are common first-PR failures. See [Structural ESLint Rules](./component-placement.md#structural-eslint-rules-serverclient-boundary--filenames) in the component-placement reference.

**If a Hades component is missing for your use case, do not use raw HTML and do not modify Hades unilaterally.** Write a short proposal and wait for approval from a human engineer before making any changes to the Hades package. See [Proposing a Missing Hades Component](#proposing-a-missing-hades-component) at the bottom of this guide.

---

## Quick-Reference Replacement Table

The most common migrations at a glance.

| Raw HTML                                                              | Hades replacement                                                                                                                         |
| --------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------- |
| `<div className="flex flex-col gap-4">`                               | `<Stack gap="comfortable">`                                                                                                               |
| `<div className="flex flex-row gap-2">`                               | `<Stack direction="horizontal" gap="snug">`                                                                                               |
| `<div className="grid grid-cols-3 gap-4">`                            | `<Grid cols={3} gap="comfortable">`                                                                                                       |
| `<div style={{ display: 'grid', gridTemplateColumns: '1fr 372px' }}>` | `<SidebarLayout sidebarWidth="detail-panel" gap="relaxed">`                                                                               |
| `<div className="flex items-center justify-between">`                 | `<Stack direction="horizontal" align="center" justify="between">`                                                                         |
| `<section>`                                                           | `<Section>` from `@orbit/hades`                                                                                                           |
| `<section className="flex flex-col gap-8 p-6">`                       | `<Stack asChild gap="relaxed"><section>…</section></Stack>`                                                                               |
| `<aside className="grid grid-cols-2">`                                | `<Grid asChild cols={2}><aside>…</aside></Grid>`                                                                                          |
| `<button onClick={…}>label</button>`                                  | `<Button onClick={…}>label</Button>`                                                                                                      |
| `<button onClick={…}><Icon /></button>`                               | `<Button variant="ghost" size="icon-sm" aria-label="…"><Icon /></Button>`                                                                 |
| `<h1>Page title</h1>`                                                 | `<Heading variant="title">Page title</Heading>`                                                                                           |
| `<h2>Section heading</h2>`                                            | `<Heading variant="section">Section heading</Heading>`                                                                                    |
| `<p>Body copy</p>`                                                    | `<Text>Body copy</Text>`                                                                                                                  |
| `<p className="text-sm text-muted">Helper</p>`                        | `<Text size="supporting" color="muted">Helper</Text>`                                                                                     |
| `<span>Inline label</span>`                                           | `<Text as="span" size="supporting">Inline label</Text>` (inline context) or `<Text size="supporting">Inline label</Text>` (block context) |
| `<strong>Important</strong>`                                          | `<Text weight="heavy">Important</Text>`                                                                                                   |
| `<b>Bold text</b>`                                                    | `<Text weight="heavy">Bold text</Text>`                                                                                                   |
| `<em>Emphasised</em>`                                                 | No Hades equivalent yet: propose one for human approval before proceeding                                                                 |
| `<i>Italic</i>`                                                       | No Hades equivalent yet: propose one for human approval before proceeding                                                                 |
| `<code>agent-id</code>`                                               | `<Code>agent-id</Code>`                                                                                                                   |
| `<kbd>Ctrl+K</kbd>`                                                   | `<Code>Ctrl+K</Code>` (keyboard input)                                                                                                    |
| `<code className="font-mono text-xs">…</code>`                        | `<Code>…</Code>` (inline) or `<Code variant="bare">…</Code>` (inside a container)                                                         |

---

## Layout: Replacing `<div>`

Every `<div>` must become a `Stack` (flex), `Grid` (equal-column CSS grid), or `SidebarLayout` (fluid main + fixed sidebar). These are the layout primitives in Hades.

### Stack: flex layouts

`Stack` replaces `<div className="flex …">`. It renders a `div` by default.

```tsx
import { Stack } from '@orbit/hades';

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
| `align`     | `'start' \| 'center' \| 'end' \| 'stretch' \| 'baseline'`           |              | Cross-axis (`items-*`)             |
| `justify`   | `'start' \| 'center' \| 'end' \| 'between' \| 'around' \| 'evenly'` |              | Main-axis (`justify-*`)            |
| `wrap`      | `boolean`                                                           | `false`      | `flex-wrap`                        |
| `maxWidth`  | `ContainerWidth \| Responsive<…>`                                   |              | Max width constraint               |
| `minHeight` | `ContainerHeight \| Responsive<…>`                                  |              | Min height constraint              |
| `textAlign` | `'start' \| 'center' \| 'end'`                                      |              | Text alignment for all descendants |
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

### Grid: equal-column layouts

`Grid` replaces `<div className="grid grid-cols-N gap-…">`.

```tsx
import { Grid } from '@orbit/hades';

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
| `gapX` / `gapY` | `SpacingSize \| Responsive<SpacingSize>` |          |
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

### SidebarLayout: fluid main + fixed sidebar

`SidebarLayout` addresses a layout that neither `Stack` nor `Grid` can express: a two-column grid where one column is fluid (`1fr`) and the other is a fixed-width sidebar. Use it instead of the inline-style workaround.

```tsx
import { SidebarLayout } from '@orbit/hades';

// ❌ Banned — inline style workaround
<div style={{ display: 'grid', gridTemplateColumns: '1fr 372px', gap: 24 }}>
  <div>…main…</div>
  <div>…sidebar…</div>
</div>

// ✅ Correct
<SidebarLayout sidebarWidth="detail-panel" gap="relaxed">
  <SidebarLayout.Main>…main…</SidebarLayout.Main>
  <SidebarLayout.Sidebar>…sidebar…</SidebarLayout.Sidebar>
</SidebarLayout>
```

**SidebarLayout props:**

| Prop              | Type                                        | Default   | Purpose                                         |
| ----------------- | ------------------------------------------- | --------- | ----------------------------------------------- |
| `sidebarWidth`    | `SidebarWidthToken \| number \| string`     | required  | Width of the fixed column. Prefer a token.      |
| `sidebarPosition` | `'start' \| 'end'`                          | `'end'`   | Which column is fixed (`'end'` = right sidebar) |
| `gap`             | `SpacingSize`                               |           | Gap between the two columns and rows            |
| `align`           | `'start' \| 'center' \| 'end' \| 'stretch'` | `'start'` | `align-items` for both columns                  |

**`SidebarWidthToken` values:**

| Token            | Value | Use case                                      |
| ---------------- | ----- | --------------------------------------------- |
| `'detail-panel'` | 372px | Fixed sidebar in a main+sidebar detail layout |

For one-off widths with no token, pass a number (`sidebarWidth={300}`) or a string (`sidebarWidth="20rem"`). If the same ad-hoc width recurs across two or more surfaces, add a token to `sizing.ts`.

```tsx
// Sticky sidebar — apply position via the inner Stack, not the layout shell
<SidebarLayout sidebarWidth="detail-panel" gap="relaxed" align="start">
  <SidebarLayout.Main>
    <Stack gap="comfortable" style={{ minWidth: 0 }}>
      {/* main content */}
    </Stack>
  </SidebarLayout.Main>
  <SidebarLayout.Sidebar>
    <Stack gap="comfortable" style={{ position: 'sticky', top: 0 }}>
      {/* sticky sidebar cards */}
    </Stack>
  </SidebarLayout.Sidebar>
</SidebarLayout>
```

> **Note:** `SidebarLayout` does not currently support `asChild`. If you need the grid to render on a semantic element (`<main>`, `<aside>`, etc.), wrap the semantic element inside `SidebarLayout.Main` or `SidebarLayout.Sidebar` using `<Stack asChild>`.

---

## Layout: Semantic Elements

`<section>` is blocked **unconditionally**. Use `<Section>` from `@orbit/hades` for content groupings with optional heading and description. For layout-only groupings without those semantics, use `<Stack>` (renders a `<div>`).

```tsx
// ❌ Banned — raw <section>
<section>…</section>
<section className="flex flex-col gap-8">…</section>

// ✅ Correct — Hades Section for content blocks
<Section heading="Active agents" description="Agents currently running.">
  <AgentTable />
</Section>

// ✅ Correct — Stack for layout-only groups
<Stack gap="comfortable">
  <Card>…</Card>
  <Card>…</Card>
</Stack>

// ✅ Correct — asChild for semantic HTML that also needs layout
<Stack asChild gap="relaxed">
  <article>…</article>
</Stack>
```

`<aside>`, `<nav>`, `<header>`, `<footer>`, `<article>`, and `<main>` must not carry display-mode Tailwind classes directly. Use `asChild` to merge layout classes onto the semantic element without an extra DOM node.

```tsx
// ❌ Banned — semantic element carrying layout classes directly
<aside className="grid grid-cols-2 gap-4">…</aside>

// ✅ Correct — layout lives in Stack/Grid; semantic element is the rendered node
<Grid asChild cols={2} gap="comfortable">
  <aside>…</aside>
</Grid>
```

> **Note:** Plain `<span>` without layout classes is also banned unconditionally by `no-raw-inline-elements`. See [Inline Elements](#typography-replacing-inline-elements) below.

---

## Typography: Headings

Every heading must use `Heading`. The variant selects both the visual scale and the default HTML element, so you never need to write `h1`–`h4` directly.

```tsx
import { Heading } from '@orbit/hades';

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
<Heading variant="display">kora</Heading>
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
import { Text } from '@orbit/hades';

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
| `body`       | 16px | Default prose, the majority of UI text        |
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
  kora AI can make mistakes. Review important information.
</Text>
```

---

## Typography: Replacing Inline Elements

The `no-raw-inline-elements` rule fires unconditionally on all raw inline HTML. There is no escape hatch. `eslint-disable` comments targeting this rule are themselves a lint error via `local/no-hades-disable-comment`.

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
> - Use `as="span"` when the rendered element must be inline, for example inside a `<button>`, `<a>`, `<p>`, or `<li>` where a nested `<p>` would be invalid HTML. This is the correct way to express inline text without writing a raw `<span>`.
> - Use `asChild` when you need to merge Text styles onto a child element you already control (e.g. a router `<Link>`). The child must not be a raw `<span>`. That is still caught by `no-raw-inline-elements`.
> - In all other cases, use `<Text>` directly (it renders `<p>`).

### `<strong>` / `<b>` → `Text weight="heavy"`

```tsx
// ❌ Banned
<strong>Critical alert</strong>
<b>Required field</b>

// ✅ Correct
<Text weight="heavy">Critical alert</Text>

// Inside a sentence — must remain inline; Text renders as a <p> by default,
// so wrap the bold run with asChild pointing at a non-raw child element
// (the child element itself must also be a Hades component or a valid element
// that is not caught by the inline-element rule, e.g. a router Link)
```

Use `weight="strong"` (semibold) for column headers and key values; use `weight="heavy"` (bold) for maximum emphasis.

### `<em>` / `<i>`: no Hades equivalent yet

Hades has no italic primitive. `no-raw-inline-elements` bans `<em>` and `<i>` unconditionally, and `no-hades-classname` bans `className` on `<Text>`, so `<Text className="italic">` is also invalid.

If italics are genuinely needed, write a short proposal and wait for approval from a human engineer before making any changes to Hades. Until a Hades component is approved and added, the use case is not supported in product code.

### `<em>`, `<i>`, `<mark>`, `<abbr>`, `<s>`, `<u>`, `<sub>`, `<sup>`

These elements have **no current Hades equivalent**. `local/no-raw-inline-elements` bans them unconditionally. If your use case genuinely requires one of these:

1. Do not use the raw element, because `pnpm lint` will fail.
2. Write a short proposal describing the element, its intended use, and a suggested component API. Post it for review and **wait for approval from a human engineer** before touching the Hades package.
3. Once approved, implement the Hades component and use it from product code.

The lint error message will read:

> _Raw `<em>` has no Hades equivalent yet. Add a Hades atom in `src/packages/hades/src/components/` first, then use it here._

This is intentional: the rule prevents the codebase from accumulating ad-hoc HTML while the design system grows to cover new needs ONLY IF NECESSARY.

---

## Typography: Inline Code (`Code`)

`Code` renders a `<code>` element with consistent monospace styling. Use it for technical identifiers inside prose.

```tsx
import { Code } from '@orbit/hades';

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

## Interactive Elements: `Button`

All interactive elements must use `<Button>` from `@orbit/hades`. Raw `<button>` is banned unconditionally by `local/no-raw-interactive-elements`.

```tsx
import { Button } from '@orbit/hades';

// ❌ Banned
<button onClick={handleSave}>Save</button>
<button type="button" aria-label="Delete"><TrashIcon /></button>

// ✅ Correct
<Button onClick={handleSave}>Save</Button>
<Button variant="ghost" size="icon-sm" aria-label="Delete">
  <Icon name="trash" />
</Button>
```

**Button variants:** `default` (accent fill), `secondary` (surface + border), `outline` (transparent + border), `ghost` (no fill), `muted` (dimmed ghost), `destructive` (error intent).

**Button sizes:** `xs`, `sm`, `md` (default), `lg`, `icon` (square 36px), `icon-sm` (square 28px).

**Tip:** For icon-only buttons, always use `size="icon"` or `size="icon-sm"` and provide `aria-label`.

```tsx
// Clickable link — Button styles on a Next.js Link
<Button asChild variant="outline">
  <Link href="/agents">View agents</Link>
</Button>

// Full-width CTA
<Button fullWidth>Deploy agent</Button>
```

---

## The `asChild` Pattern

Both layout and typography components support `asChild`. When `true`, the component merges its classes onto the **immediate child element** via Radix Slot instead of rendering a wrapper element. This is how you apply Hades primitives to semantic HTML without adding extra DOM nodes.

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

- The child must be a single element: no fragments, no arrays.
- The child must accept a `className` prop (all HTML elements and most components do).
- Do not nest `asChild` components: `<Stack asChild><Grid asChild>…</Grid></Stack>` is incorrect.

### `as` prop vs `asChild`: Decision Rule

Only `Text` exposes a string `as` prop. All other element-override use cases use `asChild`.

| Use case                                               | Pattern                            | Example                                            |
| ------------------------------------------------------ | ---------------------------------- | -------------------------------------------------- |
| Render `Text` as a different inline element            | `as` prop with string union        | `<Text as="span">`                                 |
| Render any Hades component as a custom React component | `asChild` + single child           | `<Button asChild><Link href="…">…</Link></Button>` |
| Render a layout element as a semantic HTML element     | `asChild` + semantic element child | `<Stack asChild><nav>…</nav></Stack>`              |

**Rule:** No Hades component other than `Text` should expose a string `as` prop. `Text`'s `as` prop is restricted to phrasing-content tags (`span`, `li`, `dt`, `dd`, `label`, `legend`) where a block `<p>` would be invalid HTML. For all other element-override scenarios (including applying Hades layout to semantic elements or composing components with router links), use `asChild`.

---

## Status Indicators: Badge

`Badge` is the canonical inline status/label chip. It replaces ad-hoc `<span className="rounded …">` patterns. All colour, size, and typography come from the variant prop. Never override with `className`.

```tsx
import { Badge } from '@orbit/hades';
```

### Variants

| Variant     | Visual                      | When to use                                                                                                                                   |
| ----------- | --------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------- |
| `healthy`   | Green dot/fill              | Resource is live, available, or passing                                                                                                       |
| `degraded`  | Amber                       | Partial failure, warning, stale state                                                                                                         |
| `error`     | Red                         | Failed, unavailable, blocked                                                                                                                  |
| `idle`      | Grey-blue                   | Inactive, not scheduled                                                                                                                       |
| `pending`   | Blue                        | Starting up, awaiting input                                                                                                                   |
| `default`   | Subtle fill                 | Neutral label: no status semantics                                                                                                            |
| `secondary` | Muted fill                  | De-emphasised label (second-tier metadata)                                                                                                    |
| `outline`   | Border only                 | Lowest-emphasis neutral chip                                                                                                                  |
| `elevated`  | Surface background + border | **Neutral filled chip for technical tokens** (scope strings, service names, provider labels). Stays readable on both light and dark surfaces. |

```tsx
// Status badge with dot indicator
<Badge variant="healthy" dot>Available</Badge>
<Badge variant="error" dot>Unreachable</Badge>

// Scope chip — technical token in monospace
<Badge variant="elevated" size="sm" mono>models:read</Badge>

// Service name label
<Badge variant="elevated">anthropic</Badge>

// Neutral pill with no semantic meaning
<Badge variant="default">v2</Badge>
```

### Size and shape

```tsx
<Badge size="sm">small</Badge>   // compact — use in table cells and card metadata
<Badge size="md">medium</Badge>  // default — use in standalone contexts
<Badge shape="pill" size="sm">pill</Badge>  // pill shape for tag-cloud layouts
```

---

## Page-Level Layout

Every dashboard page gets one gutter from `app/(dashboard)/layout.tsx`: `Stack padding="comfortable"` (16px on all sides), fluid, with no max width. Pages and feature layouts set no outer padding or max width of their own. `/chat` is the one route that fills the shell edge to edge.

Use `Section` for page sections and `PageLayout` as the top-level page wrapper. These are higher-level primitives that compose `Stack` and `Grid` internally.

```tsx
import { PageLayout, Section } from '@orbit/hades';

// Top-level page shell
<PageLayout>
  <Section>
    <Heading variant="title">Agents</Heading>
    <Stack gap="comfortable">{/* feature content */}</Stack>
  </Section>
</PageLayout>;
```

---

## Wizard (multi-step builder modal)

Use `Wizard` inside a `Modal size="builder"` to build consistent multi-step configuration flows. `Wizard` is a compound component; all sub-components are accessed via the `Wizard` namespace.

```tsx
import { Modal, ModalAccessibleTitle, Wizard } from '@orbit/hades';

<Modal open onClose={onClose} size="builder" dismissOnOutsideClick={false}>
  <ModalAccessibleTitle>New agent</ModalAccessibleTitle>
  <Wizard
    rail={
      <Wizard.Rail
        label="Create agent"
        steps={steps}           // { id, title, sub }[]
        current={step}
        maxVisited={maxVisited}
        onGoTo={goTo}
        footer={<InfoCard />}   // optional info card at the bottom of the rail
      />
    }
    footer={
      <Wizard.Footer
        step={step}
        totalSteps={steps.length}
        onBack={step > 0 ? () => setStep(s => s - 1) : undefined}
        onNext={() => setStep(s => s + 1)}
        nextDisabled={!valid}
        onSubmit={isLast ? handleSubmit : undefined}
        submitLabel="Create trigger"
        pending={submitting}
        hint={!valid ? 'Name is required' : undefined}
      />
    }
  >
    <Wizard.Header
      icon="zap"
      title="New agent"
      subtitle={draftId}        // optional subtitle chip
      actions={<SaveControls />} // optional feature-specific controls
      onClose={onClose}
    />
    {step === 0 && <Wizard.Step><StepIdentity /></Wizard.Step>}
    {step === 4 && (
      <Wizard.Step>
        <Wizard.ReviewStep>
          <Wizard.ReviewCard icon="user" title="Identity" onEdit={() => goTo(0)} rows={[...]} />
          <Wizard.ReviewCard icon="bot" title="Behaviour" onEdit={() => goTo(1)} rows={[...]}>
            {/* optional block-level content below rows, e.g. a multi-line prompt */}
            <Stack gap="tight" paddingBlock="snug">
              <Text as="span" size="caption" color="subtle">Prompt</Text>
              <Text size="caption" mono preserveWhitespace>{promptTemplate}</Text>
            </Stack>
          </Wizard.ReviewCard>
        </Wizard.ReviewStep>
      </Wizard.Step>
    )}
  </Wizard>
</Modal>
```

### Sub-component props

**`Wizard`**: Root. Props: `rail` (`React.ReactElement<WizardRailProps>`, required), `footer` (`React.ReactElement<WizardFooterProps>`, required), `children`. Renders the header (extracted from children), a hairline `Progress` strip, and the two-column body (rail | content column + footer).

**`Wizard.Header`**: Props: `icon` (`IconName`), `title`, `subtitle?`, `actions?` (arbitrary controls, e.g. save-status indicator + Save Draft button), `onClose`.

**`Wizard.Rail`**: Props: `label` (eyebrow string), `steps` (`{ id, title, sub }[]`), `current`, `maxVisited`, `onGoTo`, `footer?`. Steps past `maxVisited` are disabled.

**`Wizard.Step`**: Scrollable content region for one step. Wrap one per active step.

**`Wizard.ReviewStep`**: Gap container for `Wizard.ReviewCard` children. Use inside `Wizard.Step` on review screens.

**`Wizard.ReviewCard`**: Props: `icon` (`IconName`), `title`, `rows` (`{ label, value, mono? }[]`), `onEdit?` (renders Edit button), `children?` (block-level content below rows, separated by a `Separator`).

**`Wizard.Footer`**: Props: `step`, `totalSteps`, `onBack?` (omit to hide Back), `backLabel?` (default `"Back"`), `onNext` (required), `nextDisabled?`, `onSubmit?` (swaps Continue to Submit on the final step), `submitLabel?` (default `"Create"`), `pending?` (disables both buttons), `pendingLabel?` (default `"Creating…"`), `hint?`.

### Exported prop types

```ts
import type {
  WizardProps,
  WizardRailProps,
  WizardRailStep,
  WizardHeaderProps,
  WizardFooterProps,
  WizardReviewCardProps,
} from '@orbit/hades';
```

---

## Theming (light & dark)

kora ships a dark theme by default and a light theme. Theming is driven entirely
by design tokens: **you never write a colour value, and components you author are
theme-aware for free if (and only if) you consume tokens instead of hardcoding
colours.**

### How it works

- The app is wrapped in `next-themes` `ThemeProvider` in `src/app/layout.tsx`,
  configured with `attribute="data-theme"`, `defaultTheme="system"`, and
  `enableSystem`. It stamps `data-theme="light"` or `data-theme="dark"` on the
  `<html>` element (and `suppressHydrationWarning` is set because the attribute
  is applied pre-hydration).
- Hades' token stylesheet (`src/packages/hades/src/tokens/tokens.css`) defines
  the whole palette in OKLCH on `:root` (dark) and overrides the seed variables
  under a `[data-theme='light']` block. Only the raw seed/palette variables flip;
  every semantic token (`--color-background`, `--color-foreground`,
  `--color-border`, `--color-status-*`, …) is derived from them via Tailwind v4's
  `@theme`, so a single attribute swap re-colours the entire UI. All light values
  are tuned to clear the same WCAG contrast thresholds as their dark counterparts.
- The user-facing switch is the dashboard's `ThemeToggle`
  (`src/app/(dashboard)/_components/theme-toggle.tsx`), which calls `setTheme(...)`
  from `next-themes`.

### Writing theme-aware components

The single rule: **consume semantic tokens; never hardcode a colour.**

- Use the Tailwind utilities backed by Hades semantic tokens: `bg-background`,
  `bg-surface`, `text-foreground`, `text-muted`, `border-border-subtle`,
  `text-status-error`, etc. These resolve per-theme automatically.
- Prefer the Hades components' own colour props (`<Text color="muted">`,
  `<Heading color="accent">`, `<Badge variant="healthy">`) over raw utilities,
  since they are already token-backed.
- **Do not** hardcode hex/rgb/named colours or fixed OKLCH literals in `className`
  or inline styles (e.g. `text-[#fff]`, `bg-black`, `style={{ color: '#111' }}`).
  A hardcoded colour will look correct in one theme and wrong in the other.
- Because `no-hades-classname` bans `className` on Hades components, you cannot
  patch a colour onto a Hades component anyway. Use its props, or a token-backed
  wrapper (`<Stack>`), which is the intended path.

If you genuinely need a new colour, add it to the token system in
`tokens.css` (with both a `:root` and a `[data-theme='light']` value) rather than
inlining it, and, as with any Hades change, get human engineer approval first.

---

## Proposing a Missing Hades Component

> ⚠️ **Agents must not add or modify Hades components without explicit human approval.** Hades is a stable design API; unilateral changes break the contract between the design system and all product code. If you identify a gap, write a proposal and stop. Do not implement until a human engineer approves.

If none of the existing Hades components cover your use case:

1. **Do not use the raw HTML element.** The lint rules have no escape hatch.
2. **Write a short proposal** (inline in your PR description or as a comment) covering:
   - Which raw element is needed and why
   - A suggested component name and minimal API (props)
   - Whether a ShadCN/Radix primitive exists as a base
3. **Wait for approval from a human engineer** before writing any code in `src/packages/hades/`.
4. **Once approved**, implement the Hades component following the ShadCN convention:
   - Base it on a ShadCN/Radix UI component where one exists.
   - When no Radix primitive exists, follow ShadCN's styled-div convention.
   - Use `cva` + `cn`, `React.forwardRef`, `.displayName`, and `data-slot` attributes.
   - Use `React.ComponentPropsWithoutRef<'div'>` (not `React.HTMLAttributes<HTMLDivElement>`).
   - Add the component to `src/packages/hades/src/index.ts`.
5. **Replace the raw element** with the new component across the codebase.

See `src/packages/hades/src/components/alert/alert.tsx` as a reference for a styled-div component with no Radix primitive.

---

## Verification Checklist

Use this checklist before opening a PR. Every item must be true.

### Layout

- [ ] No `<div>` elements in product JSX: all replaced with `Stack` or `Grid`.
- [ ] No `<section>` elements: all replaced with `<Section>` or `<Stack>`.
- [ ] No semantic elements (`aside`, `nav`, etc.) carrying display-mode Tailwind classes directly: use `Stack asChild` or `Grid asChild`.
- [ ] `Stack` props use token values (`gap="comfortable"`): no raw Tailwind gap classes via `className`.
- [ ] Responsive layout props use the `{ base, sm, md, lg, xl }` object form: no conditional `cn()` for breakpoints.
- [ ] `asChild` is used when the rendered element must be semantic HTML, not to avoid adding a DOM node for its own sake.
- [ ] `as` prop is only used on `Text` (for phrasing-content overrides); all other element-override cases use `asChild`.

### Interactive Elements

- [ ] No raw `<button>` elements: all replaced with `<Button>` from `@orbit/hades`.
- [ ] Icon-only buttons have `size="icon"` or `size="icon-sm"` and an `aria-label`.

### Lists & Forms

- [ ] No raw `<ul>`, `<ol>`, `<li>` elements: replaced with `<List>` / `<List ordered>` and `<ListItem>` (or `<Stack>` for pure layout containers).
- [ ] No raw `<form>`, `<label>`, `<input>`, `<output>` elements: replaced with `<Form>`, `<Label>` (or `<Checkbox>`), `<Input>` / `<Checkbox>`, `<Output>` from `@orbit/hades`.

### Typography

- [ ] No `<h1>`–`<h6>` elements: all replaced with `Heading` using the correct variant.
- [ ] No `<p>` elements: all replaced with `Text`.
- [ ] No `<span>` elements: replaced with `Text` (block context) or `Text as="span"` (inline context, e.g. inside a `<Button>` or `<a>`).
- [ ] No `<strong>` or `<b>` elements: replaced with `Text weight="heavy"` or `weight="strong"`.
- [ ] No `<em>` or `<i>` elements: no Hades equivalent yet; (see [Adding a Missing Hades Component](#adding-a-missing-hades-component)).
- [ ] No `<code>` elements: replaced with `Code` (in prose) or `Text mono` (standalone technical values).
- [ ] No `<code>`, `<kbd>`, `<samp>`, `<var>` elements: replaced with `<Code>` from `@orbit/hades`.
- [ ] No `<mark>`, `<abbr>`, `<s>`, `<u>`, `<sub>`, `<sup>` elements
- [ ] `Text as="span"` is used when the rendered element must be inline (e.g. inside a `<Button>`, `<a>`, `<p>`, or `<li>`); `asChild` is used only to merge Text styles onto a non-span child element you already control.

### ESLint

- [ ] `pnpm lint` passes with zero errors.
- [ ] No `eslint-disable` comments targeting any Hades rule (`local/no-raw-layout-elements`, `local/no-raw-inline-elements`, `local/no-raw-html-text-tags`, `local/no-hades-classname`, `local/no-direct-lucide-import`, `local/no-raw-interactive-elements`, `local/no-raw-list-elements`, `local/no-raw-form-elements`): these are themselves a lint error.

### New Hades components (if applicable)

- [ ] Component is created from a ShadCN/Radix UI base, not from scratch.
- [ ] Component uses `cva` + `cn`, `React.forwardRef`, `.displayName`, and `data-slot`.
- [ ] Component exported from `src/packages/hades/src/index.ts`.
- [ ] Component is categorised correctly: atom, molecule, or organism.

---

_Guide owned by: Frontend Engineering_  
_Last updated: 2026-07-10_
