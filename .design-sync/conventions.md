# Terra Design System Conventions

## Wrapping and Setup

Terra components require a Theme Provider at the root of your application tree. All Terra components are already bundled with theming built-in, but you should wrap your UI in the context provider:

```jsx
import { ThemeProvider } from '@orbit/terra'

export default function App() {
  return (
    <ThemeProvider defaultTheme="light">
      {/* Your UI here: all Terra components use the theme automatically */}
    </ThemeProvider>
  )
}
```

The theme provider manages token values and responds to system preference changes. If not provided, Terra falls back to light theme. Components will render but tokens won't update when the system theme changes.

## Styling Idiom

Terra uses CSS custom properties (tokens) for all styling. There are no class-name utilities; all design decisions are made via props and components. The token vocabulary is:

### Color Tokens (use as `color` prop or via CSS `var()`)
- `--color-surface-*` (1-5): backgrounds and containers
- `--color-action-*` (primary, secondary, ghost, destructive)
- `--color-accent-*` (highlight/hover states)
- `--color-status-*` (healthy, degraded, error, pending, idle)
- `--color-semantic-*` (success, warning, danger, info)
- `--color-text-*` (default, muted, disabled, inverse)

### Spacing Tokens (use as `gap`, `p`, `pt`, `pb`, `pl`, `pr` props)
- `--space-tight`: 2px
- `--space-snug`: 4px
- `--space-comfortable`: 8px
- `--space-relaxed`: 12px
- `--space-spacious`: 16px
- `--space-generous`: 24px

### Typography Tokens (use as `size`, `weight` props on `Text` / `Heading`)
- Sizes: `sm` (12px), `base` (14px), `lg` (16px), `xl` (18px), `2xl` (20px)
- Weights: `regular`, `medium`, `heavy` (bold)

## Where the Truth Lives

Stylesheet: all compiled styles are in `_ds_bundle.css` (already imported via `styles.css`). Tokens are CSS custom properties defined there; read it to understand available token names.

Component API: each component's `.d.ts` file lists its full prop interface. Always check the `.d.ts` for the latest props (e.g., `Stack` takes `gap`, `direction`, `align`, `justify`, not CSS classes).

Per-component docs: each `.prompt.md` file shows usage examples and highlights when a component is composite (e.g., `FormField` wraps `Label` + `Input`). Read it before composing.

## Idiomatic Build

Here's a real Terra example from the bundled components:

```jsx
import React from 'react'
import { Stack, Button, Text, Heading } from 'OrbitTerra'

export function LoginFlow() {
  return (
    <Stack gap="relaxed" align="center">
      <Heading variant="title">Sign In</Heading>
      <Stack gap="comfortable" direction="horizontal">
        <Button variant="primary">Log In</Button>
        <Button variant="ghost">Sign Up</Button>
      </Stack>
      <Text size="sm" color="muted">
        By continuing, you agree to our Terms.
      </Text>
    </Stack>
  )
}
```

Key patterns:
- Use `Stack` for vertical layout (default) or `Stack direction="horizontal"` for rows
- Use `gap="..."` (not margin/padding utilities) for spacing between children
- Use `Heading` + `Text` for typography, never raw `<h1>` or `<p>`
- Use `Button` + `Link` (called `Anchor`) for interactivity, never raw `<button>` or `<a>`
- Color and tone are controlled via props (`color`, `variant`), not CSS classes
- Icons come from the bundled `Icon` component with a `name` prop. Never raw `<svg>`

## Component Scoping

Terra components are grouped by layer:

- Atoms (`Alert`, `Badge`, `Button`, `Input`, etc.): smallest reusable units
- Layout (`Stack`, `Grid`, `Section`, `PageLayout`): structural containers
- Molecules (`FormField`, `Modal`, `DataTable`, `Tabs`): composed atomic + layout
- Organisms (`DataTable`): complex multi-part components

Composition flows atoms to layout + molecules. Don't skip layers. For example, use `FormField` (molecule) for form rows, not bare `Input` + custom label wrapper.

## Testing Your Output

Every design built with Terra should:

1. Use only components from the bundle (Icon, Button, Stack, Text, Heading, Grid, etc.)
2. Pass props only (no `className` on Terra components; all styling goes through props)
3. Render token-driven colors and spacing (check that light/dark theme toggle works)
4. Have no raw HTML elements (`<div>`, `<button>`, `<p>`, `<span>`); use Terra equivalents
5. Be functional React code (hooks allowed; doesn't need backend)

If output renders blank, check: (1) provider is wrapping the root, (2) bundled CSS loaded in `styles.css`, (3) component props match the `.d.ts` interface.
