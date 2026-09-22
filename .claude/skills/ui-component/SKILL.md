---
name: ui-component
description: >-
  Create new UI components, promote existing ones across tiers, or verify a component is in the
  right tier. Applies the three-tier placement decision tree (Terra / App-shared / Feature-scoped),
  implements with correct patterns (forwardRef, cva, cn, data-slot, @tier JSDoc), and produces or
  updates the co-located Storybook story. Use when asked to "add a component", "create a
  component", "promote a component", "check the tier", "wire up Storybook", "what tier does this
  belong to", "should this be promoted", or when reviewing a Tier 2 or Tier 3 component for
  potential promotion. TRIGGER — invoke this skill before creating, modifying, or reviewing any
  component file under frontend/src/; do not skip because the task looks like a one-liner.
---

# Create, promote, or verify a UI component

Three tasks in one skill: **new** (create from scratch), **promote** (move a component up the
tier ladder), and **verify** (audit whether a component sits at the right tier). All three end
with the lint+tsc gate passing.

> **The defining principle: tier first, code second.**
> No component gets written until the placement decision is made and — for Tier 1 — explicitly
> approved by a human. The decision tree is not a suggestion; it is enforced by ESLint rules that
> fail `pnpm lint` on violations.

## Before you start

Read `frontend/docs/reference/component-placement.md` and skim
`frontend/docs/reference/terra-design-system.md` so you have the current Terra inventory and
ESLint rule table in mind. The tier decision tree and all authoring conventions live there; treat
them as the authority, not your training data.

---

## Phase 0 — Identify task and gather context

**Goal:** know which task type you're doing and have enough consumer information to make the tier
decision.

Ask the user:
1. **Task type** — new component, promote an existing one, or verify a tier?
2. **Component name** — what it's called.
3. **Consumers** — which file(s) or feature areas use (or will use) it? How many, and do they
   span more than one feature area?
4. **Orbit domain knowledge** — does it reference any Orbit-specific vocabulary: agent/principal
   types, model or provider names, status tokens (e.g. `OrbitStatus`), or permission/role
   concepts? A component that renders a plain spinner knows nothing about Orbit; one that renders
   an agent status badge is domain-aware.

For an **existing** component (promote or verify):
- Read its current implementation file to find: current path, `@tier` JSDoc comment (if present),
  any Orbit-specific imports.
- Check whether a story file exists alongside it.
- `grep -r "from '.*ComponentName'" frontend/src --include="*.tsx" --include="*.ts" -l` — enumerate
  real consumers (substitute the actual component name for `ComponentName`).

---

## Phase 1 — Determine correct tier

Apply this decision tree **in order, stopping at the first match**:

1. **Inline** — used by exactly one file, and that file is under 300 lines of JSX. Don't extract.
   Report this and stop unless the user says otherwise.
2. **Tier 3 (Feature-scoped)** — all consumers live in the same feature area
   (`src/app/(dashboard)/{feature}/`). Path: `src/app/(dashboard)/{feature}/_components/`.
3. **Tier 1 (Terra)** — consumers span two or more feature areas **and** the component has zero
   Orbit domain knowledge. Requires a **human approval gate before any code is written** (see
   below).
4. **Tier 2 (App-shared)** — consumers span two or more feature areas **and** the component is
   Orbit-domain-aware. Path: `src/components/`.

Surface your determination with one paragraph of reasoning. For Tier 1, also confirm with the
user which Storybook category fits: Atoms / Molecules / Layout / Organisms.

### Promotion eligibility check (Tier 3 → 2, Tier 2 → 1)

When the task type is **verify** or when reviewing a Tier 2 or Tier 3 component, apply the
decision tree above to determine whether the component should move up. Present the finding as:

- **Current tier** and **why it was placed there** (from `@tier` JSDoc or by inference).
- **Evidence for promotion**: new consumers found outside the original scope, loss of any
  Orbit domain knowledge since it was written, etc.
- **Recommendation**: promote, stay, or "promote only if X".

For a Tier 3 component, the trigger for promotion to Tier 2 is a second feature area importing
it. Run the consumer grep to verify — don't assume.

For a Tier 2 component, the trigger for promotion to Tier 1 is absence of Orbit domain knowledge
*across the entire implementation*: no status tokens, no principal types, no model/provider names,
no permission vocabulary. If any domain knowledge exists, the component stays at Tier 2.

### Tier 1 hard gate

Before writing a single line in `src/packages/terra/`, present a short proposal:

- Component name and description
- Minimal props API
- Radix or ShadCN primitive it builds on (if any)
- Which Storybook category it belongs to

Then **wait for explicit approval**. "Looks good / approved / proceed" means go; anything else is
a revision request. This gate has no escape hatch — it is a hard project rule.

---

## Phase 2 — Implement

### Tier 1 — Terra (`frontend/src/packages/terra/src/components/{name}/`)

Create three files:

**`{name}.tsx`**
```tsx
/** @tier 1 — Zero Orbit domain knowledge. {One-line description.} */
import * as React from 'react'
import { cva, type VariantProps } from 'class-variance-authority'
import { cn } from '../../utils'

const {name}Variants = cva(
  // base classes
  '',
  {
    variants: {
      // variant maps — static Tailwind strings only, no dynamic construction
    },
    defaultVariants: {},
  },
)

export interface {Name}Props
  extends React.ComponentPropsWithoutRef<'div'>,
    VariantProps<typeof {name}Variants> {
  className?: never  // tightly-scoped atoms block external className at type level
}

const {Name} = React.forwardRef<HTMLDivElement, {Name}Props>(
  ({ variant, size, ...rest }, ref) => {
    // Destructure variant props explicitly; spread the rest to avoid passing reserved/unused attrs
    return (
      <div
        ref={ref}
        data-slot="{name}"
        className={cn({name}Variants({ variant, size }))}
        {...rest}
      />
    )
  },
)
{Name}.displayName = '{Name}'

export { {Name}, {name}Variants }
```

Key invariants:
- `React.ComponentPropsWithoutRef<'div'>` (not `HTMLAttributes`) — matches the project standard
- `data-slot="{name}"` on every rendered element; sub-components get their own slot names
- `cn()` for all class merging — no string concatenation
- `cva()` for variant maps — static Tailwind strings only (no template literals, no dynamic values)
- `.displayName` set on every sub-component
- `className?: never` on tightly-scoped atoms; `/** @internal */ className?: string` only when
  composing inside Terra

**`index.ts`** — re-export only:
```ts
export { {Name}, {name}Variants, type {Name}Props } from './{name}'
```

**Update `frontend/src/packages/terra/src/index.ts`** — add the export under the correct section
comment (`// ─── Atoms`, `// ─── Molecules`, `// ─── Layout`, `// ─── Organisms`).

### Tier 2 — App-shared (`frontend/src/components/{name}.tsx`)

```tsx
/** @tier 2 — Orbit-domain-aware: {note domain knowledge used}. Used by: {feature areas}. */
'use client'  // only if this file uses React hooks
import { ... } from '@orbit/terra'
// Import Orbit types as needed; no raw HTML

export interface {Name}Props { ... }

export function {Name}({ ... }: {Name}Props) {
  return (
    <Stack ...>
      {/* Terra primitives only — raw HTML here fails pnpm lint */}
    </Stack>
  )
}
```

Then add an export to `frontend/src/components/index.ts`.

### Tier 3 — Feature-scoped (`frontend/src/app/(dashboard)/{feature}/_components/{name}.tsx`)

```tsx
/** @tier 3 — {feature}-scoped. Promote to src/components/ if a second feature needs this. */
'use client'  // only if this file uses React hooks
import { ... } from '@orbit/terra'

export function {Name}({ ... }: {Name}Props) { ... }
```

No barrel export — relative imports only inside the feature.

### Promotion changes

When moving a component up a tier:
1. Move the file to the new path.
2. Update the `@tier` JSDoc comment.
3. Add barrel export (`index.ts`) if promoting to Tier 2 or Tier 1.
4. Update all consumer import paths: `grep -r "from '.*ComponentName'" frontend/src -l` then fix each.
5. Update the story file (see Phase 3).

---

## Phase 3 — Storybook story

### Tier 1 — co-located `{name}.stories.tsx`

```tsx
import type { Meta, StoryObj } from '@storybook/react'
import { {Name} } from './{name}'

const meta: Meta<typeof {Name}> = {
  title: 'Terra/{Atoms|Molecules|Layout|Organisms}/{Name}',
  component: {Name},
  tags: ['autodocs'],
  parameters: {
    layout: 'centered',
    backgrounds: {
      default: 'orbit',
      values: [{ name: 'orbit', value: '#070a0f' }],
    },
  },
  argTypes: {
    // Union props: control: 'select'
    // Boolean props: control: 'boolean'
    // Implementation props (asChild): table: { disable: true }
  },
}
export default meta

type Story = StoryObj<typeof {Name}>

/** Default story — one per meaningful variant. Args-based for simple controls. */
export const Default: Story = {
  args: { ... },
}

/** Showcase story — render() for multi-instance or composite layouts. */
export const AllVariants: Story = {
  render: () => (
    // raw <div> is permitted here because ESLint Terra rules are suppressed in
    // src/packages/terra/** story files
    <div className="flex flex-wrap gap-2 p-4">
      ...
    </div>
  ),
}
```

### Tier 2 — co-located `{name}.stories.tsx` in `src/components/`

```tsx
import type { Meta, StoryObj } from '@storybook/react'
import { {Name} } from './{name}'

const meta: Meta<typeof {Name}> = {
  title: 'App/Shared/{Name}',
  component: {Name},
  tags: ['autodocs'],
  // No parameters block — inherits preview defaults
}
export default meta

type Story = StoryObj<typeof {Name}>

export const Default: Story = { args: { ... } }
```

Use `decorators` when the component needs a wrapping container to look correct (e.g. a list item
that must be inside `<List separator>`):
```ts
decorators: [(Story) => <List separator><Story /></List>],
```

### Tier 3 — no story required

If the user explicitly requests a story anyway, create it without a `title` prefix and without
`tags: ['autodocs']` — it won't appear in the main Storybook sidebar.

### Promotion story changes

Update the `title` field to match the new tier's prefix. Add or remove the `parameters` block:
- Tier 3 → 2: remove `parameters.layout` and `parameters.backgrounds`; set `title: 'App/Shared/…'`
- Tier 2 → 1: add `parameters.layout: 'centered'` and the orbit `backgrounds` preset; set
  `title: 'Terra/{category}/…'`

---

## Phase 4 — Verify

```bash
cd frontend && pnpm lint && pnpm tsc --noEmit
```

Both must exit zero before the task is done. Do not declare success until they do.

**Common violations to check:**
- Missing barrel export in `index.ts` — lint and tsc only catch this if something already imports
  from the barrel; always verify manually that the new component is exported
- Raw `<div>` / `<span>` / `<p>` in a component file outside `src/packages/terra/` — replace with
  Terra primitives
- `className` passed to a Terra component that declares `className?: never` — remove it
- Missing `data-slot` on a new Tier 1 component's root element
- `import { X } from 'lucide-react'` — replace with `<Icon name="…" />`
- Story `title` doesn't match the tier prefix → Storybook sidebar placement is wrong (not a lint
  error, but still wrong)

---

## Reference — tier summary

| Tier | Path | Import | Story title prefix | Story required |
|---|---|---|---|---|
| 1 — Terra | `src/packages/terra/src/components/{name}/` | `@orbit/terra` | `Terra/{Category}/{Name}` | Yes (always) |
| 2 — App-shared | `src/components/{name}.tsx` | `@orbit/components` | `App/Shared/{Name}` | Yes (always) |
| 3 — Feature-scoped | `src/app/(dashboard)/{feature}/_components/{name}.tsx` | relative | — | No |

## Hard-won gotchas

1. **Tier 1 gate is absolute** — never write Terra code first and ask permission second. The rule
   has no `eslint-disable` escape hatch; a violation is a lint failure.
2. **No `className` on Terra components** — `no-terra-classname` ESLint rule bans it. Use the
   component's own props; if none exist for the use case, that's a missing prop, not a reason to
   reach for `className`.
3. **ESLint Terra rules are suppressed inside `src/packages/terra/**`** — raw `<div>` in a story
   render function inside the Terra package is fine. Everywhere else it fails lint.
4. **Storybook runs outside the component tree** — CSS variable tokens don't resolve. The three
   background presets in `preview.ts` use hardcoded hex values for this reason; don't change them.
5. **`pnpm tsc --noEmit` from `frontend/`** — not the repo root. The root `tsconfig.json` does
   not cover `frontend/src`.
6. **Tier 3 has no barrel** — relative imports only. A Tier 3 component accidentally exported
   from `src/components/index.ts` is a promotion that wasn't decided.
