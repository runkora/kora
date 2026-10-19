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
│   ├── theme/                    # The theme module: validator, loader, CSS generator (@runkora/hades/theme)
│   │   ├── validate.ts           # validateTheme(): document, tokens, slots, asset URLs, the report
│   │   ├── css.ts                # generateThemeCss() and fontPreloads()
│   │   ├── formats.ts            # The value-format patterns, shared with theme.schema.json
│   │   └── index.ts              # The subpath's public API
│   └── index.ts                  # Public API — all component exports live here
├── scripts/
│   ├── manifest.ts               # Generates manifest.json and theme.schema.json (make hades-manifest)
│   └── lib/                      # The pure parts of the generator, with their tests
├── .storybook/                   # Storybook configuration
├── manifest.json                 # The theme surface manifest, generated, checked in
├── theme.schema.json             # JSON Schema of the theme document, generated from the manifest, checked in
├── package.json
├── tsconfig.json
└── README.md
```

---

## Versioning and the theme surface manifest

Hades has no version of its own. It is released with kora, under the kora version, so the version of Hades in a release is that release's version. A theme records the kora release it was written against, and an instance accepts a theme of the same major. Nothing checks that a release's version reflects its Hades changes: the version is chosen by whoever cuts the release. The decision is in the [design system versioning ADR](../../../../docs/adr/2026-10-11-hades-versioning-and-theme-surface-manifest.md); this section is the working summary.

### The version

The version is the kora release tag, `vX.Y.Z`. `package.json` carries the placeholder `0.0.0` in the tree; the release workflow sets it from the tag before packing, so nothing here is bumped by hand and nothing can drift. There is no `version.ts` and the manifest carries no version: it ships inside a release, and the release is its version. A build that is not from a release tag identifies as `0.0.0-dev`.

Nothing is promised below `1.0.0`. From the first final release at or above it, a theme written for one major is not promised to work on another.

### The manifest

`manifest.json` is the theme surface: every settable token, how each semantic name resolves to raw tokens, the asset slots, and the product-name bounds. Each token and semantic name also carries a `group` and a `label`, and a flat top-level `groups` list (`{ id, label, order }`) names the groups, so a consumer such as a theme preview can list the surface without its own token-to-group table. `branding` is the group of brand colours. Which tokens a consumer derives from the brand colours (the "shades") is the consumer's own configuration, keyed by token name: status, provider and chart colours are settable but are not derived, so "every non-branding group" is not a safe rule. Groups are display metadata, not contract, but their ids are package surface. It is generated, never edited, and the same run generates `theme.schema.json` from it:

```bash
make hades-manifest          # regenerate the manifest, the schema and defaults.json after changing tokens.css or surface.ts
make hades-manifest-check    # what `make check` runs: fails on drift in any of them
```

The generator reads token names from the `:root` blocks of `src/tokens/tokens.css` (every `--hades-raw-*` property is settable, in both modes, by rule) and everything else from `src/tokens/surface.ts`: the kind, group and label of each token, the group and label of each semantic name, the groups themselves, the slots with their formats, and the product-name bounds. A raw token or semantic name declared in one file and not the other fails generation, and so does one with no group, an unknown group or an empty group, so adding a token means touching both and choosing where it is listed.

| Field         | Contents                                                                                                                                                                                                                   |
| ------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `modes`       | `["dark", "light"]`.                                                                                                                                                                                                       |
| `tokens`      | One entry per raw token: `name`, `kind` (`color`, `shadow`, `length`, `font`), `format` (`hex-or-oklch`, `css-shadow-list`, `css-length`, `font-family-name`) and `modes`.                                                 |
| `mapping`     | For each `@theme inline` name, the raw tokens it resolves to, transitively, and whether it is `derived` through an expression such as `color-mix()`. A theme never sets a semantic name, but components read through them. |
| `contrast`    | The text pairs a theme must keep, sorted by `id`: `text`, `on` (backgrounds), `min` (WCAG 2.x ratio) and `severity` (`error` or `warn`). See Contrast pairs below.                                                         |
| `slots`       | One entry per asset slot: `name`, `kind`, `formats` (the extensions its URL path may end in), `modes` (empty when one URL serves both modes).                                                                              |
| `productName` | `minLength`, `maxLength` and a Unicode `pattern`, applied after trimming.                                                                                                                                                  |

### Contrast pairs

The manifest's `contrast` array lists the text colours a theme must keep readable. Each entry is `{ id, text, on, min, severity }`: one `id` per text colour, `text` and the `on` backgrounds as semantic names (`--color-*`, never a derived one such as `--color-accent-subtle`), and `min` a WCAG 2.x contrast ratio. The pairs are declared as `CONTRAST` in `src/tokens/surface.ts`, and the generator rejects an unknown, non-colour or derived name, an empty or repeated id or `on`, a `min` outside 1 to 21 and a bad severity. Text only: border and other non-text pairs are not declared.

`validateTheme` checks the pairs per mode, resolving each name through `mapping` to its raw token and reading the value from the theme, or from the `defaults` option for a token the theme leaves alone. Pass `defaults` from `defaults.json` (generated from `tokens.css` with the manifest, outside it, because kora's default values are not part of the contract). A pair is checked when the theme sets at least one side of it; without `defaults`, only when it sets both. Hex and `oklch()` values are measured by relative luminance, ignoring alpha. A failed `error` pair is a validation error, and a failed `warn` pair appears in `report.contrastWarnings` and in the output of `validate-theme`, and never fails the theme.

### The theme module and the schema

`@runkora/hades/theme` is the validator, the loader and the CSS generator for a white-labelling theme, in one pure module: no React, no DOM, no Node built-ins, so the same code runs in the frontend's init container, its server module and a browser preview. `validateTheme(text, { manifest, koraVersion, defaults })` takes the `theme.yaml` text (JSON is YAML), the manifest, the instance's own kora version (its release version, or `null` for a build that is not from a release, which skips the comparison and says so in the report) and, optionally, `defaults` (the per-mode colour values from `defaults.json`, which the contrast check uses for a token the theme leaves alone), and resolves to either `{ ok: true, theme, report }` or `{ ok: false, errors, report }`. It collects every fault rather than stopping at the first; each error carries the document path it names, such as `tokens.--hades-raw-black.dark`. The report lists every unknown token or slot it ignored and every identity value (`productName`, `logo.dark`, `logo.light`, `favicon.dark`, `favicon.light`) the theme leaves to the default, and `contrastWarnings`, the failed `warn` contrast pairs. The theme carries the validated values, with each slot as the `https://` URL the document gave, and a SHA-256 hash over the document. The validator checks the form of a slot URL and never fetches it, so it does not see whether the asset exists or what it contains. `generateThemeCss(theme)` turns the theme into the one `<style>` string the root layout inlines, and `fontPreloads(theme)` lists the font URLs to preload (with `crossorigin`: a font host must send `Access-Control-Allow-Origin`). The rules, from the [white-labelling theme ADR](../../../../docs/adr/2026-10-12-white-labelling-theme.md): a known token with a malformed value, a slot value that is not an `https://` URL (no credentials, no fragment, at most 2048 characters) ending in the slot's extension, an unknown top-level key, a font slot set together with its family token, or a `koraVersion` in another major than the instance's each fail; an unknown token or slot is ignored and reported; a missing value never fails.

`theme.schema.json` is the JSON Schema (draft 2020-12) of the document, generated from the manifest, so a producer can check a theme before a deploy with any schema validator. Its value patterns are the regular expressions the validator compiles, imported from `src/theme/formats.ts` and defined once; what the schema cannot express (trimming the product name, the font-slot rule, the same-major rule) the validator still applies. The document's one required key, `koraVersion`, is the kora release the theme was written against and must be a quoted string such as `koraVersion: "1.4.0"`: unquoted, YAML reads `1.10` as the number 1.1, so the validator refuses a number and says to quote it. A theme passes when its major equals the instance's, whatever its minor or patch; no promise is made across a major. Quote any YAML value that contains ` #`, such as a shadow ending in a hex colour: unquoted, YAML reads it as a comment.

### No kora identity

Hades contains no kora identity. The kora default is a theme like any other, owned by the app, and the kora logo is an asset of that theme, not a Hades component. Hades provides the agnostic components that render whatever a theme supplies for a slot.

---

## Consuming Hades from another repository

Each final kora release `vX.Y.Z` publishes `@runkora/hades@X.Y.Z` to the GitHub Packages npm registry and attaches the tarball, `manifest.json`, `theme.schema.json` and `defaults.json` to kora's GitHub Release. A release candidate attaches the same files and publishes no package. The package ships TypeScript source, `tokens.css`, the theme module, the manifest, the theme schema and this README: no compiled JavaScript, no fonts, no Storybook, no stories, no tests. The rules are in the [Hades delivery ADR](../../../../docs/adr/2026-10-11-hades-delivery-to-external-consumers.md); a consumer needs the following.

**Registry and token.** An `.npmrc` with the scope mapped to the registry, and a token that has `read:packages` for this repository (a personal access token (classic), or, for a workflow in a repository granted access to the package, its own `GITHUB_TOKEN`; the GitHub Packages npm registry accepts neither fine-grained personal access tokens nor GitHub App installation tokens):

```ini
@runkora:registry=https://npm.pkg.github.com
//npm.pkg.github.com/:_authToken=${NPM_TOKEN}
```

**The package version is the kora release version.** Nothing checks that a release containing a breaking change to the exports, props, peer ranges or tokens is a major, so pin an exact version or review each upgrade rather than relying on a caret range. A consumer that renders themes decides whether a theme is compatible by comparing the theme's `koraVersion` with this version: a theme passes when its major equals the package's major. Every release carries its own `manifest.json`, so a consumer can read the surface of any release. Below `1.0.0` nothing is promised.

**React 19 and the declared peers.** Every runtime dependency except `@tanstack/react-table` and `yaml` (the theme module's parser) is a peer dependency; install each at the range `package.json` declares.

**Tailwind v4, with `tokens.css` as the entry.** `tokens.css` contains `@import 'tailwindcss'` itself, so it is the single Tailwind entry: never add a second one. Tailwind does not scan `node_modules`, so an `@source` must point at the installed package for its components' utilities to be generated:

```css
@import '@runkora/hades/src/tokens/tokens.css';
@source '../node_modules/@runkora/hades/src';
```

**TypeScript compilation of the source.** The package has no build output; the consumer's toolchain compiles its `.ts` and `.tsx` files. Vite does so unaided; Next.js needs `transpilePackages: ['@runkora/hades']`, as the kora frontend sets.

**The two font slots.** Hades never loads a font. It reads `--hades-raw-font-sans` and `--hades-raw-font-mono`, which default to generic stacks; the consumer, or the theme it renders, sets them to font families it loads itself.

`make hades-smoke` packs the package and builds exactly such a consumer (Vite, React, Tailwind v4) from the tarball in a temporary directory; the release workflow runs the same script on every release tag.

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
