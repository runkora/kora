# Hades: kora Design System

**`@runkora/hades`** · `frontend/src/packages/hades/`

---

kora is another name for Persephone, who lives half the year in Olympus and half in the underworld. Hades is the underworld half of kora: the tokens, components, and visual language beneath every screen, the layer the product surfaces stand on. It carries the platform experience from the control plane down to the user's screen.

---

## Overview

Hades is a monorepo workspace package. It lives at `frontend/src/packages/hades/` inside the kora frontend directory and is consumed via the pnpm workspace protocol:

```json
"@runkora/hades": "workspace:*"
```

Changes to Hades are reflected immediately in the consuming app: no build step, no publish cycle.

Hades is built on:

- **[Radix UI](https://www.radix-ui.com/primitives)**: unstyled, accessibility-first React primitives (focus management, keyboard navigation, ARIA)
- **[Tailwind CSS v4](https://tailwindcss.com/)**: utility-first styling with a semantic token layer
- **[Storybook](https://storybook.js.org/)**: isolated component development and living documentation

> **On shadcn/ui:** The shadcn CLI is used once during development to scaffold initial Radix-based component boilerplate. It is not a runtime dependency: all shadcn-specific imports and references are stripped immediately after generation. The resulting components are Hades-native.

---

## Getting Started

### Run Storybook (isolated component development)

```bash
cd frontend/src/packages/hades
pnpm storybook
```

Storybook runs entirely in isolation: no Next.js app, no backend, no application code involved. This is the primary environment for building and reviewing components.

### Use components in the frontend

```tsx
import { Button, Card, CardContent, Badge } from '@runkora/hades';

export default function AgentCard() {
  return (
    <Card>
      <CardContent>
        <Badge variant="healthy">Running</Badge>
        <Button variant="ghost">View logs</Button>
      </CardContent>
    </Card>
  );
}
```

### Use the `cn()` utility

```tsx
import { cn } from '@runkora/hades';

<div className={cn('base-class', isActive && 'active-class')} />;
```

---

## Design Tokens

Hades' visual vocabulary is defined as semantic CSS custom properties in `src/tokens/tokens.css`. All tokens are mapped into Tailwind v4 via `@theme inline` and available as utility classes.

**Never use arbitrary Tailwind shades** (e.g., `gray-950`, `blue-500`) in component code. Always reference a semantic token. If the right token doesn't exist yet, add it here first.

### Color: Background & Surface

| Token                      | Tailwind class        | Purpose                              |
| -------------------------- | --------------------- | ------------------------------------ |
| `--color-background`       | `bg-background`       | Page/root background                 |
| `--color-surface`          | `bg-surface`          | Card and panel backgrounds           |
| `--color-surface-elevated` | `bg-surface-elevated` | Elevated panels, popovers, dropdowns |
| `--color-surface-overlay`  | `bg-surface-overlay`  | Modal backdrops                      |

### Color: Text

| Token                | Tailwind class    | Purpose                    |
| -------------------- | ----------------- | -------------------------- |
| `--color-foreground` | `text-foreground` | Primary text               |
| `--color-muted`      | `text-muted`      | Secondary/supporting text  |
| `--color-subtle`     | `text-subtle`     | Placeholder, disabled text |

### Color: Accent & Brand

| Token                       | Tailwind class              | Purpose                         |
| --------------------------- | --------------------------- | ------------------------------- |
| `--color-accent`            | `text-accent` / `bg-accent` | Primary interactive color       |
| `--color-accent-hover`      | `bg-accent-hover`           | Hover state for accent elements |
| `--color-accent-foreground` | `text-accent-foreground`    | Text on accent backgrounds      |

### Color: Border

| Token                   | Tailwind class         | Purpose                     |
| ----------------------- | ---------------------- | --------------------------- |
| `--color-border`        | `border-border`        | Default borders             |
| `--color-border-subtle` | `border-border-subtle` | Subtle/inset borders        |
| `--color-border-accent` | `border-border-accent` | Focus rings, active borders |

### Color: Status

Status tokens represent agent, policy, and system health states. Use these consistently so operators can read UI state at a glance.

| Token                     | Tailwind class         | Meaning                       |
| ------------------------- | ---------------------- | ----------------------------- |
| `--color-status-healthy`  | `text-status-healthy`  | Operational, running, passing |
| `--color-status-degraded` | `text-status-degraded` | Degraded, warning, slow       |
| `--color-status-error`    | `text-status-error`    | Failed, blocked, critical     |
| `--color-status-idle`     | `text-status-idle`     | Inactive, paused, standby     |
| `--color-status-pending`  | `text-status-pending`  | Queued, starting, in-progress |

Background variants (e.g., `bg-status-healthy-subtle`) are available for badge and chip backgrounds.

### Typography

| Token         | Resolves through        | kora default | Purpose                                    |
| ------------- | ----------------------- | ------------ | ------------------------------------------ |
| `--font-sans` | `--hades-raw-font-sans` | Inter        | Body text, UI labels                       |
| `--font-mono` | `--hades-raw-font-mono` | Geist Mono   | Agent IDs, policy names, code, identifiers |

The two raw font tokens are theme slots. Hades defaults them to the generic stacks; the kora app sets them to its fonts in `globals.css`, and a loaded theme sets them to the families it declares.

Use `font-mono` for any value that represents a system identifier: agent IDs, credential names, event IDs, policy slugs. This reinforces the technical character of the UI.

### Border Radius

Five literal steps, each a raw token a theme can set (`--hades-raw-radius-*`), resolved through the semantic names below.

| Token          | Tailwind class | Default | Purpose                       |
| -------------- | -------------- | ------- | ----------------------------- |
| `--radius-sm`  | `rounded-sm`   | 6px     | Tight elements (badges, tags) |
| `--radius-md`  | `rounded-md`   | 12px    | Inputs, buttons               |
| `--radius-lg`  | `rounded-lg`   | 18px    | Cards, panels                 |
| `--radius-xl`  | `rounded-xl`   | 18px    | Dialogs                       |
| `--radius-2xl` | `rounded-2xl`  | 24px    | Chat input                    |

### Shadows: none

**Hades is intentionally flat.** No shadow tokens are defined and no `shadow-*` utility classes should be used in components. Depth and hierarchy are expressed through:

- **Background lightness**: `background` → `surface` → `surface-elevated` tokens create visual layering
- **Borders**: `border-border` and `border-border-subtle` separate surfaces without adding drop shadows
- **Spacing**: vertical rhythm and padding communicate structure

---

## Components

Each component lives in its own named folder under `src/components/`. The folder contains the component file, a co-located Storybook story, and an `index.ts` that re-exports the public API, e.g. `src/components/card/card.tsx`, `src/components/card/card.stories.tsx`, `src/components/card/index.ts`. There is no enforced subdirectory hierarchy beyond this: atomic design (atoms → molecules → organisms) is a useful mental model for reasoning about component complexity, not a directory structure.

| Component                                                                         | Import           | Notes                                                                           |
| --------------------------------------------------------------------------------- | ---------------- | ------------------------------------------------------------------------------- |
| `Button`                                                                          | `@runkora/hades` | Variants: `default`, `secondary`, `ghost`, `outline`, `destructive`             |
| `Badge`                                                                           | `@runkora/hades` | Variants: standard + status (`healthy`, `degraded`, `error`, `idle`, `pending`) |
| `Card`, `CardHeader`, `CardTitle`, `CardDescription`, `CardContent`, `CardFooter` | `@runkora/hades` | All exported from a single `card.tsx` file                                      |
| `Input`                                                                           | `@runkora/hades` | Text input, Radix-based                                                         |
| `Label`                                                                           | `@runkora/hades` | Form label, associates with inputs                                              |
| `Separator`                                                                       | `@runkora/hades` | Horizontal/vertical divider                                                     |
| `Skeleton`                                                                        | `@runkora/hades` | Loading placeholder                                                             |
| `Tooltip`, `TooltipTrigger`, `TooltipContent`, `TooltipProvider`                  | `@runkora/hades` | Radix Tooltip with Hades-token styling                                          |
| `PageLayout`                                                                      | `@runkora/hades` | Root page wrapper: applies background, font, padding                            |
| `Section`                                                                         | `@runkora/hades` | Content section with consistent vertical rhythm                                 |
| _(more added as features are built)_                                              |                  |                                                                                 |

### Button variants

```tsx
<Button>Default</Button>
<Button variant="secondary">Secondary</Button>
<Button variant="ghost">Ghost</Button>
<Button variant="outline">Outline</Button>
<Button variant="destructive">Destructive</Button>
```

### Badge variants

Standard:

```tsx
<Badge>Default</Badge>
<Badge variant="secondary">Secondary</Badge>
<Badge variant="outline">Outline</Badge>
<Badge variant="destructive">Destructive</Badge>
```

Status (use for agent/policy/system states):

```tsx
<Badge variant="healthy">Running</Badge>
<Badge variant="degraded">Degraded</Badge>
<Badge variant="error">Failed</Badge>
<Badge variant="idle">Idle</Badge>
<Badge variant="pending">Starting</Badge>
```

---

## Package Structure

```
frontend/src/packages/hades/
├── src/
│   ├── components/
│   │   ├── button/
│   │   │   ├── button.tsx
│   │   │   ├── button.stories.tsx
│   │   │   └── index.ts
│   │   ├── badge/
│   │   │   ├── badge.tsx
│   │   │   ├── badge.stories.tsx
│   │   │   └── index.ts
│   │   ├── card/
│   │   │   ├── card.tsx          # Exports Card, CardHeader, CardTitle, CardDescription, CardContent, CardFooter
│   │   │   ├── card.stories.tsx
│   │   │   └── index.ts
│   │   ├── input/
│   │   │   ├── input.tsx
│   │   │   ├── input.stories.tsx
│   │   │   └── index.ts
│   │   ├── label/
│   │   │   ├── label.tsx
│   │   │   ├── label.stories.tsx
│   │   │   └── index.ts
│   │   ├── separator/
│   │   │   ├── separator.tsx
│   │   │   ├── separator.stories.tsx
│   │   │   └── index.ts
│   │   ├── skeleton/
│   │   │   ├── skeleton.tsx
│   │   │   ├── skeleton.stories.tsx
│   │   │   └── index.ts
│   │   ├── tooltip/
│   │   │   ├── tooltip.tsx
│   │   │   ├── tooltip.stories.tsx
│   │   │   └── index.ts
│   │   ├── page-layout/
│   │   │   ├── page-layout.tsx
│   │   │   ├── page-layout.stories.tsx
│   │   │   └── index.ts
│   │   └── section/
│   │       ├── section.tsx
│   │       ├── section.stories.tsx
│   │       └── index.ts
│   ├── utils.ts                  # cn() utility (clsx + tailwind-merge)
│   ├── tokens/
│   │   ├── tokens.css            # Design token definitions (@theme block)
│   │   ├── surface.ts            # Theme surface declaration: token kinds, slots, product-name bounds
│   │   └── tokens.stories.tsx    # Storybook token showcase
│   └── index.ts                  # Public API — all exports live here
├── scripts/
│   ├── manifest.ts               # Generates manifest.json (make hades-manifest)
│   ├── classify.ts               # Classifies a change against the last release (make hades-classify)
│   └── lib/                      # The pure parts of both scripts, with their tests
├── .storybook/                   # Storybook configuration
├── manifest.json                 # The theme surface manifest, generated, checked in
├── CONTRACT.md                   # The five contract boundary rules
├── package.json
├── tsconfig.json
└── README.md
```

---

## Versioning and the theme surface manifest

Hades has no version of its own. It is released with kora, under the kora version, so the version of Hades in a release is that release's version. A theme records the kora release it was written against, and an instance accepts a theme of the same major. Hades follows Semantic Versioning, so a consumer of the published `@runkora/hades` package can use a caret range, and the release workflow makes that true: it refuses a kora release that contains a Hades break unless the release is a major. The rules are in the [design system versioning ADR](../../../../docs/adr/2026-10-11-design-system-semantic-versioning.md); this section is the working summary.

### The version

The version is the kora release tag, `vX.Y.Z`. `package.json` carries the placeholder `0.0.0` in the tree; the release workflow sets it from the tag before packing, so nothing here is bumped by hand and nothing can drift. There is no `version.ts` and the manifest carries no version: it ships inside a release, and the release is its version. A build that is not from a release tag identifies as `0.0.0-dev`.

Nothing is promised below `1.0.0`. From the first final release at or above it, a theme written for one major is not promised to work on another, and a Hades break needs a major kora release.

### The manifest

`manifest.json` is the theme surface: every settable token, how each semantic name resolves to raw tokens, the asset slots, and the product-name bounds. It is generated, never edited:

```bash
make hades-manifest          # regenerate after changing tokens.css or surface.ts
make hades-manifest-check    # what `make check` runs: fails on drift
```

The generator reads token names from the `:root` blocks of `src/tokens/tokens.css` (every `--hades-raw-*` property is settable, in both modes, by rule) and everything else from `src/tokens/surface.ts`: the kind of each token, the slots with their formats and size limits, and the product-name bounds. A raw token declared in one file and not the other fails generation, so adding a token means touching both.

| Field         | Contents                                                                                                                                                                                                                   |
| ------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `modes`       | `["dark", "light"]`.                                                                                                                                                                                                       |
| `tokens`      | One entry per raw token: `name`, `kind` (`color`, `shadow`, `length`, `font`), `format` (`hex-or-oklch`, `css-shadow-list`, `css-length`, `font-family-name`) and `modes`.                                                 |
| `mapping`     | For each `@theme inline` name, the raw tokens it resolves to, transitively, and whether it is `derived` through an expression such as `color-mix()`. A theme never sets a semantic name, but components read through them. |
| `slots`       | One entry per asset slot: `name`, `kind`, `formats`, `maxBytes`, `modes` (empty when one file serves both modes).                                                                                                          |
| `productName` | `minLength`, `maxLength` and a Unicode `pattern`, applied after trimming.                                                                                                                                                  |

### The contract boundary rules

`CONTRACT.md` carries the five rules that decide what is and is not a change to the contract. The same list is in the Hades PRD and the versioning ADR, and `make hades-contract-check` fails when the copies differ.

### The classifier

The classifier compares the manifest, the exported component API, the peer ranges and the changed files at head against the **last final kora release**, never a release candidate, and says what the release carrying the change must be: `none`, `patch`, `minor` or `major`. Each finding cites the boundary rule or Semantic Versioning it applied and says whether it breaks themes:

```bash
make hades-classify                       # what the next release owes, against the last final release
make hades-classify RELEASE=1.4.0         # also checks that 1.4.0 is enough; exits 1 with an explanation if not
make hades-classify BASE=origin/main      # before a final release exists
make hades-classify FORMAT=markdown       # what CI writes to the job summary
```

In short: a token or slot removed, renamed or re-typed, a semantic name remapped, a slot's limits shrunk, the product-name bounds tightened, or a new token that a pre-existing component reads is a **major** and breaks themes. A new token read only by new components, or a slot's limits grown, is a **minor**. Changes that no theme can see still count for the package: an export removed or renamed, a prop removed or made required, a peer dependency added or narrowed, or a semantic name removed is a **major**; an export or optional prop added, a peer range widened, a peer removed, or a semantic name added is a **minor**. Any other source change is a **patch**. Documentation, stories and tests alone are **none**. The release owes the highest level of any finding.

It runs in two places. On a pull request that touches Hades, a job writes the report to the job summary and never fails, so a developer or an agent learns early that a change is a break. At release, the first job of `release.yml` runs it against the tag being cut and refuses the release when the version is too low for the changes in it, with a message that names the findings and says how to fix it. Below `1.0.0` it reports and refuses nothing. It also prints two questions no script decides: whether a token kept its name but changed its meaning, and whether a prop narrowed its type or a function tightened its signature. Both need a major.

---

## Consuming Hades from another repository

Each `hades/vX.Y.Z` tag publishes `@runkora/hades` to the GitHub Packages npm registry and creates a GitHub Release of the same name with the tarball, `manifest.json` and `CONTRACT.md` attached. The package ships TypeScript source, `tokens.css`, the manifest, the contract and this README: no compiled JavaScript, no fonts, no Storybook, no stories, no tests. The rules are in the [Hades delivery ADR](../../../../docs/adr/2026-10-11-hades-delivery-to-external-consumers.md); a consumer needs the following.

**Registry and token.** An `.npmrc` with the scope mapped to the registry, and a token that has `read:packages` for this repository (a personal access token (classic), or, for a workflow in a repository granted access to the package, its own `GITHUB_TOKEN`; the GitHub Packages npm registry accepts neither fine-grained personal access tokens nor GitHub App installation tokens):

```ini
@runkora:registry=https://npm.pkg.github.com
//npm.pkg.github.com/:_authToken=${NPM_TOKEN}
```

**A caret range is safe, and the contract version is a separate number.** The package follows Semantic Versioning: a removed export, a removed or newly required prop, a narrowed peer range or a removed token is a major, so a caret range such as `^1.2.0` will not take a breaking change. The theme contract has its own version, `CONTRACT_VERSION`, exported from the package and recorded as `contractVersion` in `manifest.json`. A consumer that renders themes reads it to decide whether a theme is compatible: a theme passes when its contract major equals the instance's, whatever the package version.

```ts
import { CONTRACT_VERSION } from '@runkora/hades';
```

**React 19 and the declared peers.** Every runtime dependency except `@tanstack/react-table` is a peer dependency; install each at the range `package.json` declares.

**Tailwind v4, with `tokens.css` as the entry.** `tokens.css` contains `@import 'tailwindcss'` itself, so it is the single Tailwind entry: never add a second one. Tailwind does not scan `node_modules`, so an `@source` must point at the installed package for its components' utilities to be generated:

```css
@import '@runkora/hades/src/tokens/tokens.css';
@source '../node_modules/@runkora/hades/src';
```

**TypeScript compilation of the source.** The package has no build output; the consumer's toolchain compiles its `.ts` and `.tsx` files. Vite does so unaided; Next.js needs `transpilePackages: ['@runkora/hades']`, as the kora frontend sets.

**The two font slots.** Hades never loads a font. It reads `--hades-raw-font-sans` and `--hades-raw-font-mono`, which default to generic stacks; the consumer, or the theme it renders, sets them to font families it loads itself.

`make hades-smoke` packs the package and builds exactly such a consumer (Vite, React, Tailwind v4) from the tarball in a temporary directory; the release workflow runs the same script on every tag.

---

## Adding a New Component

### Step 1: Scaffold (optional)

If the component has a Radix UI primitive, use the shadcn CLI to generate boilerplate:

```bash
cd frontend/src/packages/hades
npx shadcn@latest add <component-name>
```

**Immediately after generating:**

1. Create the component folder: `src/components/<name>/`
2. Move the generated file into it and rename to `<name>.tsx`
3. Fix the import path for `cn` (e.g., `@/lib/utils` → `../../utils`)
4. Verify no reference to `shadcn`, `@/components/ui`, or `components.json` remains

If there is no shadcn equivalent, write the component from scratch using Radix UI + Hades tokens directly.

### Step 2: Add an `index.ts`

Each component folder exposes its public API via `index.ts`:

```ts
// src/components/card/index.ts
export { Card, CardHeader, CardTitle, CardDescription, CardContent, CardFooter } from './card';
```

### Step 3: Export from the package public API

All component exports are re-exported from the top-level `src/index.ts`:

```ts
export { Button } from './components/button';
export { Badge } from './components/badge';
export {
  Card,
  CardHeader,
  CardTitle,
  CardDescription,
  CardContent,
  CardFooter,
} from './components/card';
export { Tooltip, TooltipTrigger, TooltipContent, TooltipProvider } from './components/tooltip';
export { PageLayout } from './components/page-layout';
export { cn } from './utils';
```

### Step 4: Add a Storybook story

Every component must have at least one story co-located in the same folder:

```
src/components/button/button.stories.tsx
src/components/card/card.stories.tsx
```

Stories are the primary way to review and document component variants. Run `pnpm storybook` to verify before committing.

---

## For AI Assistants

When generating components for kora using Hades:

1. **Import from `@runkora/hades`**: never import directly from internal paths like `src/components/button`
2. **Use semantic token classes**: `bg-surface`, `text-foreground`, `text-muted`, `border-border`, `text-accent`. Never arbitrary shades like `gray-800` or `blue-500`
3. **Use `font-mono`** for any system identifier (agent IDs, policy names, event IDs, credential names)
4. **Use status badge variants** for agent/policy health: `healthy`, `degraded`, `error`, `idle`, `pending`
5. **Never use `shadow-*` classes**: Hades is a flat design system. Use `bg-surface-elevated` + `border-border` to distinguish layers instead
6. **Use `cn()`** from `@runkora/hades` for conditional class composition
7. **Check `src/index.ts`** for the current list of available exports before reaching for a new primitive
8. **Do not modify Hades while working on application code**: if a gap is found (a missing component, token, or variant), stop and propose the addition before making any change. Hades is a stable design API from the application's point of view; changes to the design system itself are a separate, deliberate act that requires explicit confirmation from the developer

---

## Development Scripts

```bash
# Isolated component development
pnpm storybook

# Type check only (no lint, no formatting)
pnpm build
```

### Linting and formatting

Hades has no standalone `lint` or `format` scripts. Both tools are configured at the
`frontend/` level and cover Hades' source files automatically:

- **ESLint** config lives at `frontend/eslint.config.mjs`. It applies to all
  `src/**/*.tsx` files, including those under `src/packages/hades/`. Hades is
  intentionally excluded from the two custom design-system rules
  (`no-hades-classname`, `no-raw-html-text-tags`) because Hades is the canonical
  implementation site for those rules, not a consumer of them.

- **Prettier** config lives at `frontend/.prettierrc.json`. Prettier resolves
  config by walking up the directory tree from each file, so Hades files inherit
  this config automatically: no local `.prettierrc` is needed or wanted.

Run all checks from the **repository root** using `make`:

```bash
make frontend-lint        # ESLint only
make frontend-fmt         # Prettier (--write locally, --check in CI)
make frontend-check       # ESLint + tsc --noEmit + Prettier
make check                # Everything — Go + frontend combined
```

Do not add `lint` or `format` scripts to Hades' `package.json`. Doing so would
require duplicating or referencing the parent config, creating two sources of truth
for the same rules.

---

## Related

- [Design System PRD](../../../../docs/prd/design-system.md)
- [Architecture Decision Record](../../../../docs/adr/2026-03-25-terra-frontend-design-system.md)
- [kora Frontend](../../)
