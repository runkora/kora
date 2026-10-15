# Hades Frontend Design System

**Status:** Accepted (Amended 2026-10-11)
**Date:** 2026-03-25
**Deciders:** Engineering Team
**Technical Story:** [Design System PRD](../prd/design-system.md) — establishing a coherent visual foundation for the kora control plane UI.

---

## Context

kora's frontend is a bare Next.js scaffold. The only styling in `globals.css` is two hardcoded color variables (`--background`, `--foreground`) and a system font fallback. The single page (`page.tsx`) uses arbitrary Tailwind utility classes directly (`bg-gray-950`, `border-gray-800`, `text-green-400`) with no token abstraction, no reusable components, and no defined visual identity system.

Building product features on top of this state produces compounding problems: every new UI element requires designing from scratch, global visual changes require a full codebase search-and-replace, and AI-assisted generation (Forge, v0.dev) produces output that doesn't integrate cleanly because there is no token vocabulary or component API for the tools to reason about.

### Background

kora positions itself as a production-grade control plane for autonomous agents — the nervous system that coordinates distributed AI agents through identity, policy, and event infrastructure. The UI must reflect that character: technical, signal-oriented, and authoritative. An ad-hoc, inconsistent interface works against that positioning and erodes operator trust.

The monorepo structure established in [monorepo-technology-stack](2026-02-16-monorepo-technology-stack.md) places the frontend at `frontend/` alongside the Go backend. The question is where a shared design system lives and how it is structured — whether inside the app, or as a separate workspace package.

### Problem Statement

We need a set of architectural decisions that establish:

1. Where the design system code lives and how it is consumed by the frontend
2. How visual design tokens are defined, structured, and integrated with Tailwind v4
3. Which component foundation to build on (accessibility primitives, variant management)
4. How components are structured internally (folder conventions, co-location)
5. How depth and elevation are communicated without shadows
6. How the design system is developed and documented in isolation

### Goals

- Establish a semantic design token system encoding kora's visual identity
- Deliver reusable, on-brand UI components importable as `import { Button } from "@orbit/hades"`
- Enable isolated design system development with no dependency on the Next.js application
- Provide a well-documented token and component vocabulary that AI tooling can reliably consume
- Keep the system maintainable: global visual changes should require changing one token value, not hunting through component files

### Non-Goals

- Defining full dashboard layouts or product feature UI
- A code-to-Figma or Figma-first design workflow
- Custom data visualization components (charts, graphs, timelines)
- Mobile-responsive layout system
- Dark/light mode toggle (dark-first; system preference respected via media query)

---

## Decision Drivers

* **Isolation**: Design system components must be buildable and testable without running the Next.js app. Coupling design system work to the application's dev server slows iteration on both.
* **AI-assisted development**: Forge and v0.dev generate better output when given a known, documented component API and token vocabulary. The system must be legible to AI tooling.
* **Accessibility**: Control plane users depend on the UI for operational decisions. Components must be accessible by default; bespoke accessibility implementation is expensive and error-prone.
* **Tailwind v4 compatibility**: The frontend uses Tailwind v4 with no `tailwind.config.js`. Any component foundation must work within this constraint.
* **Zero arbitrary utility classes in components**: Color references in component source must go through semantic tokens. This is the only way global visual changes remain a one-line edit.
* **Dark-mode first**: The initial target aesthetic is a deep, near-black background with elevated surface layers. The depth model must communicate hierarchy without shadows.
* **Minimal runtime overhead**: The design system should add no JavaScript runtime beyond what the components themselves require. CSS-in-JS and managed theme systems are excluded on this basis.

---

## Considered Options

### Option 1: Components inside `frontend/src/app/components/` (no separate package)

**Description:** Keep components co-located with application code in the Next.js app directory. No workspace package, no separate Storybook config.

**Pros:**
- Zero monorepo setup cost — no `pnpm-workspace.yaml`, no `package.json` for the package
- Tailwind class scanning works without any additional `@source` configuration
- Import paths resolve naturally within the Next.js module system

**Cons:**
- No isolation: working on the design system requires the Next.js app to be running
- Storybook requires significant extra configuration plumbing into the app's build, mixing concerns
- Components are coupled to the application's development cycle and cannot be versioned or shared independently
- If a second kora surface (admin panel, embedded dashboard) ever needs the same components, extraction is expensive after the fact
- No clean boundary between "design system code" and "application code" — the line blurs over time

### Option 2: Fully-managed component library (Material UI / Mantine)

**Description:** Adopt MUI or Mantine as the component library, using their built-in theming systems.

**Pros:**
- Large component surface area out of the box — rich set of complex components (data grids, date pickers, etc.)
- Established community, extensive documentation, well-trained AI models
- Handles accessibility automatically

**Cons:**
- Both MUI and Mantine ship CSS-in-JS or pre-built CSS that conflicts with Tailwind v4's utility-first model and produces bloated output
- Components are opaque dependencies — customizing beyond theme tokens requires overriding a third-party theming system, not editing owned code
- The visual defaults of both libraries are strongly consumer-app oriented; achieving kora's technical, dark, minimal aesthetic requires deep style overrides
- Lock-in: migrating away later is significantly more expensive than starting with owned components
- AI tooling generates better Tailwind-native output for shadcn/Radix patterns than for MUI/Mantine

### Option 3: Headless UI (Tailwind Labs)

**Description:** Use Tailwind Labs' own headless component library, purpose-built to pair with Tailwind CSS.

**Pros:**
- Designed from the ground up to work with Tailwind — no friction with Tailwind v4
- Good accessibility foundations
- Official pairing with Tailwind CSS

**Cons:**
- Smaller primitive surface area than Radix UI — fewer low-level building blocks available
- Less community adoption and therefore less AI training data than shadcn/Radix
- Radix UI's accessibility coverage and primitive breadth (dialogs, tooltips, dropdown menus, popovers, focus management) is stronger
- No scaffolding tool equivalent to the shadcn CLI for bootstrapping component code

### Option 4: Isolated workspace package with Radix UI primitives, Tailwind v4 tokens, and shadcn as a scaffolding aid

**Description:** Create a dedicated workspace package (`frontend/src/packages/hades/`) containing the design token definitions, all components, and a Storybook instance. Components are built on Radix UI accessibility primitives, styled with Tailwind v4 and semantic Hades tokens. The shadcn/ui CLI is used as a one-time scaffolding tool to generate Radix-based component boilerplate; all shadcn-specific references are stripped immediately after generation, leaving fully Hades-native components. The frontend depends on the package via the pnpm workspace protocol (`"@orbit/hades": "workspace:*"`).

**Pros:**
- Complete isolation: the design system builds, runs in Storybook, and is documented with no Next.js app involvement
- Workspace protocol means changes are reflected in the consuming app immediately — no publish or version bump required
- Radix UI provides a comprehensive, production-grade accessibility layer (focus management, ARIA roles, keyboard navigation, portal handling) without requiring implementation
- shadcn's scaffolding produces idiomatic, already-styled Radix-based component code, dramatically reducing the cost of component bootstrapping
- Components are fully owned code — customization is editing a file, not overriding a theme system
- Tailwind v4 tokens via `@theme` integrate cleanly with utility classes generated from the package's component files (via `@source` directive in the frontend config)
- Token-based design means global visual changes are one-line edits to `tokens.css`
- AI tooling (Forge, v0.dev) has strong knowledge of shadcn and Radix component patterns, producing reliable on-brand output when given the token vocabulary
- Storybook in `frontend/src/packages/hades/` serves as a living design system documentation environment with zero app-code contamination
- Path is open to sharing the package with additional future kora surfaces

**Cons:**
- Upfront monorepo setup cost: `pnpm-workspace.yaml`, workspace-scoped `package.json`, TypeScript config for the package
- Cross-package Tailwind scanning requires an explicit `@source` directive in the frontend's CSS — misconfiguration silently omits component utility classes from the generated output
- shadcn CLI has occasional compatibility gaps with cutting-edge Tailwind versions; if it fails, component code must be bootstrapped by hand (acceptable since shadcn is not a runtime dependency)
- Two TypeScript configs to maintain (`frontend/tsconfig.json`, `frontend/src/packages/hades/tsconfig.json`)

---

## Decision

**Chosen Option:** Option 4 — Isolated workspace package (`frontend/src/packages/hades/`) with Radix UI primitives, Tailwind v4 semantic tokens, and shadcn as a one-time scaffolding aid.

### Rationale

The isolation requirement is the deciding factor. Designing and iterating on UI components while the Next.js application is running mixes two independent concerns — application development and design system development — in a way that slows both. Storybook provides exactly the right isolated environment for component work, but it requires a clean package boundary to operate without application code involvement. Option 4 provides that boundary; none of the others do.

The workspace protocol eliminates the operational friction that would otherwise come with a separate package: no publish cycle, no version bumps, no symlink management. Changes to `frontend/src/packages/hades/` are immediately visible in the consuming frontend. The isolation is conceptual and developmental, not operational.

Radix UI is the strongest available accessibility primitive layer for React. It handles the hard problems — focus trapping, ARIA attribute management, keyboard navigation, portal rendering for overlays — that are notoriously easy to get wrong when building from scratch, and nearly impossible to get right consistently across a component library without a dedicated primitives library. shadcn/ui provides a curated, well-maintained scaffold layer on top of Radix, giving a starting point for each component that is already styled and functional. Because shadcn is used only for its CLI output — not as a runtime dependency — we inherit none of its constraints and can evolve the components freely.

Compared to MUI or Mantine, this approach gives us complete ownership of component code, full compatibility with Tailwind v4's utility model, and a visual aesthetic that we define rather than override. The cost of that ownership is bootstrapping time, which the shadcn CLI largely absorbs.

---

## Sub-Decisions

Several architectural choices within the selected option require explicit recording.

### SD-1: Design Token Strategy — Semantic CSS Custom Properties via Tailwind v4 `@theme`

**Decision:** Design tokens are defined as semantic CSS custom properties in `frontend/src/packages/hades/src/tokens/tokens.css` and registered as Tailwind v4 theme tokens via the `@theme inline` directive. All color, typography, radius, and spacing extension values are referenced by semantic name throughout component code. Zero arbitrary Tailwind shade values (e.g., `gray-950`, `blue-500`) appear in committed component source.

Token categories:
- **Color — brand/background layer**: `--color-background`, `--color-surface`, `--color-surface-elevated`
- **Color — text**: `--color-foreground`, `--color-foreground-muted`, `--color-foreground-subtle`
- **Color — border**: `--color-border`, `--color-border-subtle`
- **Color — accent/interactive**: `--color-accent`, `--color-accent-hover`, `--color-accent-foreground`
- **Color — status**: `--color-status-healthy`, `--color-status-degraded`, `--color-status-error`, `--color-status-idle`, `--color-status-pending`
- **Color — destructive**: `--color-destructive`, `--color-destructive-foreground`
- **Typography**: `--font-sans`, `--font-mono`
- **Border radius**: `--radius-sm`, `--radius-md`, `--radius-lg`, `--radius-full`

**Why semantic names over generic role names:** Generic names (`--color-success`, `--color-warning`) are interchangeable with any product. kora has a specific operational model — agents are healthy, degraded, or errored; policies are enforced or blocked; executions are pending or idle. These are domain concepts, not generic states. Semantic status tokens encode that vocabulary directly into the design system, making component intent legible and making AI-generated code more accurately on-brand.

**Cross-package Tailwind scanning:** The frontend's `globals.css` must include an `@source "../packages/hades/src"` directive so Tailwind v4 scans the package's component files for utility class usage when generating the CSS bundle. Without this, classes used in `frontend/src/packages/hades/` components that are not also used in `frontend/` source would be missing from the output.

### SD-2: Component Structure — Folder-Per-Component, Flat Hierarchy

**Decision:** All components live directly under `frontend/src/packages/hades/src/components/` as individual named directories. Each component directory contains at least three files:

```
frontend/src/packages/hades/src/components/
├── button/
│   ├── button.tsx          # Component implementation
│   ├── button.stories.tsx  # Storybook story
│   └── index.ts            # Public re-export
├── card/
│   ├── card.tsx
│   ├── card.stories.tsx
│   └── index.ts
└── badge/
    ├── badge.tsx
    ├── badge.stories.tsx
    └── index.ts
```

There is **no** `atoms/`, `molecules/`, or `organisms/` subdirectory hierarchy. Atomic design (atoms → molecules → organisms) is used as a mental model for reasoning about component complexity and composition — not as a directory prescription. Applying it as a folder structure forces components to be categorized before their usage patterns are known, produces frequent re-categorizations as components evolve, and adds navigation friction without meaningful benefit.

All public component exports are re-exported from `frontend/src/packages/hades/src/index.ts`, giving consumers a single, stable import surface: `import { Button, Card, Badge } from "@orbit/hades"`.

**Why folder-per-component over single files:** Co-locating the story and re-export with each component keeps everything related to a component in one place. Finding `button/button.stories.tsx` requires no search; adding a test file or documentation file for a specific component has an obvious home.

### SD-3: Depth Model — Surface Elevation, No Shadows

**Decision:** Hades has no shadow design tokens. The system is intentionally flat. Depth and visual hierarchy are communicated exclusively through:

- **Background lightness progression**: `--color-background` (deepest) → `--color-surface` → `--color-surface-elevated` (lightest). Each successive layer is a slightly lighter shade of the base navy.
- **Subtle borders**: `--color-border` and `--color-border-subtle` delineate containers and interactive elements.
- **Spacing and typography**: Information hierarchy is expressed through type size, weight, and spacing rather than visual depth cues.

**Why no shadows:** Drop shadows on dark backgrounds require careful calibration to avoid looking muddy or heavy, and they communicate physical depth metaphors that feel mismatched to a technical control plane interface. The flat elevation model is simpler to maintain (no shadow scale to calibrate and update), produces cleaner output on dark backgrounds, and is consistent with the aesthetic direction of infrastructure tooling (terminal emulators, monitoring dashboards, IDEs in dark mode). Adding shadows in the future is always possible by introducing shadow tokens; removing them from an established system is harder.

### SD-4: shadcn/ui as Scaffolding Tool Only — No Runtime Dependency

**Decision:** shadcn/ui is used exclusively as a CLI scaffolding tool to generate initial Radix-based component boilerplate. It is invoked once during initial component bootstrapping (`npx shadcn@latest add button`, etc.). Immediately after generation, all shadcn-specific imports, comments, and references are stripped from the output. The resulting component code is Hades-native and has no shadcn runtime dependency.

**Operational implications:**
- `shadcn` does not appear in `frontend/src/packages/hades/package.json` dependencies (it is an `npx` invocation)
- The generated `components.json` used by the shadcn CLI may exist temporarily during scaffolding but is not committed as a permanent package artifact
- No component in the committed codebase imports from `@shadcn/*` or references shadcn in comments

**Why this boundary matters:** shadcn's components are authored for shadcn's own conventions — they import from `@/lib/utils`, use shadcn's own `cn()` location, and reference shadcn-internal types. These references would create a confusing dual identity if left in Hades. Treating the CLI output as a starting point that is immediately made Hades-native produces a cleaner codebase and correctly sets expectations: contributors see Hades components, not shadcn components that happen to live in a different package.

### SD-5: Storybook as Isolated Development Environment

**Decision:** Storybook runs from `frontend/src/packages/hades/` with its own configuration, using the `@storybook/react-vite` builder and Tailwind v4 support. It has zero dependency on or awareness of the Next.js application. Every P0 component ships with at least one story file covering its variants. Storybook doubles as living documentation for design tokens (via a dedicated token showcase story) and component APIs.

**Builder choice:** `@storybook/react-vite` (rather than webpack-based) because the package has no Next.js involvement and Vite's development server starts significantly faster. There is no requirement for the SSR-aware build configuration that `@storybook/nextjs` provides.

---

## Consequences

### Positive

- **Complete design system isolation**: Components can be built, reviewed, and documented without the Next.js app. Design system work and application work can proceed independently.
- **Immediate workspace feedback loop**: The `workspace:*` protocol means no publish cycle — editing a component in `frontend/src/packages/hades/` is reflected in the frontend on the next save.
- **Owned component code**: All component logic is editable source under our control. Customizing a component is editing a file, not fighting a third-party theme override system.
- **Token-driven maintainability**: Changing kora's accent color (currently orange/amber at `--hades-accent-hue: 26`) is a one-line change in `tokens/tokens.css`. No component file needs to be touched.
- **AI-friendly surface**: The combination of a documented token vocabulary, a known component API (shadcn/Radix conventions), and Storybook stories gives AI tools (Forge, v0.dev) the context needed to generate accurate, on-brand output.
- **Accessibility by default**: Radix UI handles focus management, ARIA, keyboard navigation, and portal rendering — the hardest parts of accessible component development — without requiring implementation per component.
- **Extensible to future kora surfaces**: If a second kora UI surface is built, it can consume `@orbit/hades` via the same workspace protocol with no additional infrastructure.

### Negative

- **Monorepo setup overhead**: The one-time cost of configuring `pnpm-workspace.yaml`, the package's `package.json` and TypeScript config, and the `@source` Tailwind directive must be paid before any component work begins.
- **`@source` misconfiguration risk**: If the cross-package source directive is incorrect or missing, component utility classes will silently vanish from the CSS output. This is a non-obvious failure mode that requires validation after initial setup (building the frontend and inspecting the CSS output).
- **Two TypeScript configs**: `frontend/tsconfig.json` and `frontend/src/packages/hades/tsconfig.json` must be maintained independently. Changes to shared compiler options (strict mode, target) must be applied in both places.
- **shadcn CLI compatibility risk**: The shadcn CLI occasionally lags behind cutting-edge Tailwind versions. If the CLI fails, component code must be written from scratch. This risk is bounded — it only affects initial bootstrapping, and Radix-based component code is straightforward to write by hand.

### Neutral

- **Storybook is separate from the Next.js app**: `pnpm storybook` from `frontend/src/packages/hades/` and `pnpm dev` from `frontend/` are independent processes. This is the intended model, but it means there is no single command to run both.
- **Font loading stays in the app**: Because `frontend/src/packages/hades/` has no knowledge of the Next.js app, font loading (currently Geist Sans and Geist Mono via `next/font/google`) remains in `frontend/src/app/layout.tsx`. Hades tokens reference fonts by name (`--font-sans`, `--font-mono`) without knowing where they are loaded. This is architecturally clean but means Storybook must load fonts separately for accurate visual rendering.
- **Atomic design remains a mental model**: Teams can use atom/molecule/organism vocabulary in discussion and PR reviews to reason about component complexity and reuse. That vocabulary is deliberately not reflected in the directory structure.

---

## Implementation

### Package Structure

```
frontend/src/packages/hades/
├── .storybook/
│   ├── main.ts                   # Storybook config: react-vite builder, Tailwind v4
│   └── preview.ts                # Global decorators, token imports
├── src/
│   ├── components/
│   │   ├── badge/
│   │   │   ├── badge.tsx
│   │   │   ├── badge.stories.tsx
│   │   │   └── index.ts
│   │   ├── button/
│   │   │   ├── button.tsx
│   │   │   ├── button.stories.tsx
│   │   │   └── index.ts
│   │   ├── card/
│   │   │   ├── card.tsx
│   │   │   ├── card.stories.tsx
│   │   │   └── index.ts
│   │   └── ...                   # Additional components follow same pattern
│   ├── lib/
│   │   └── utils.ts              # cn() utility (clsx + tailwind-merge)
│   ├── tokens.css                # @theme block: all semantic design tokens
│   └── index.ts                  # Public API: re-exports all components + cn()
├── package.json                  # name: "@orbit/hades"; runtime deps: @radix-ui/*, cva, clsx, tailwind-merge
└── tsconfig.json
```

### Frontend Integration

```css
/* frontend/src/app/globals.css */
@import "@orbit/hades/src/tokens/tokens.css";
@source "../packages/hades/src";
```

```json
// frontend/package.json (dependencies)
{
  "@orbit/hades": "workspace:*"
}
```

### Component Variant Pattern

Components use `class-variance-authority` (cva) to define variant-driven class sets. The `cn()` utility (re-exported from `@orbit/hades`) composes and deduplicates classes at the call site.

```typescript
// Example: frontend/src/packages/hades/src/components/button/button.tsx
import { cva, type VariantProps } from "class-variance-authority";
import { cn } from "../../utils";

const buttonVariants = cva(
  "inline-flex items-center justify-center rounded-md text-sm font-medium transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent disabled:pointer-events-none disabled:opacity-50",
  {
    variants: {
      variant: {
        default: "bg-accent text-accent-foreground hover:bg-accent-hover",
        secondary: "bg-surface text-foreground border border-border hover:bg-surface-elevated",
        ghost: "hover:bg-surface text-foreground-muted hover:text-foreground",
        destructive: "bg-destructive text-destructive-foreground hover:bg-destructive/90",
        outline: "border border-border bg-transparent hover:bg-surface text-foreground",
      },
      size: {
        sm: "h-8 px-3 text-xs",
        md: "h-9 px-4",
        lg: "h-10 px-6",
      },
    },
    defaultVariants: { variant: "default", size: "md" },
  }
);
```

All class names in variants reference only Tailwind utility tokens derived from the Hades `@theme` token definitions. No arbitrary shade values.

### Migration Path

There is no prior component system to migrate from. The implementation is additive:

1. **Phase 1**: Scaffold `frontend/src/packages/hades/`, configure workspace, define tokens, validate Storybook and cross-package Tailwind scanning
2. **Phase 2**: Bootstrap and clean P0 components (Button, Badge, Card), then P1 components (Input, Label, Separator, Skeleton, Tooltip)
3. **Phase 3**: Layout primitives (PageLayout, Section), README documentation, token showcase story; refactor `page.tsx` to import from `@orbit/hades`
4. **Ongoing**: P2 components (Avatar, DropdownMenu, Dialog, Alert) added as real feature development surfaces the need

### Timeline

See [Design System PRD — Timeline and Milestones](../prd/design-system.md#timeline-and-milestones).

---

## Validation

### Success Metrics

- **Zero arbitrary color classes in component source**: A grep for `gray-\d`, `blue-\d`, `green-\d` etc. in `frontend/src/packages/hades/src/components/**` returns no results
- **Storybook runs independently**: `pnpm storybook` from `frontend/src/packages/hades/` launches with no Next.js process running; all component stories render correctly with the correct token-driven visual style
- **Frontend CSS output is correct**: Building the frontend with `pnpm build` produces a CSS file that includes utility classes from Hades component files (validated by searching the output for a known component-specific class)
- **AI generation accuracy**: A prompt to Forge or v0.dev referencing `@orbit/hades` tokens and components produces output that imports correctly and renders on-brand without manual class corrections
- **P0 component suite complete**: Button, Badge, and Card components ship with full variant coverage and at least one Storybook story each
- **First real feature built on the system**: At least one product feature page is built using only components imported from `@orbit/hades`, with no bespoke CSS

### Monitoring

- Track arbitrary color class usage via CI lint rule or periodic grep audit — any new occurrences are a regression
- Review new component PRs against the folder-per-component convention; deviations (e.g., components added as flat files rather than named directories) are caught in review
- Monitor Storybook build time as the component library grows — Storybook builds should remain fast (under 30 seconds) to preserve the isolation benefit

---

## Related Decisions

- [monorepo-technology-stack](2026-02-16-monorepo-technology-stack.md) — Monorepo structure; established `frontend/` directory and monorepo conventions that this ADR extends with `frontend/src/packages/hades/`
- [language-and-framework-selection](2026-02-16-language-and-framework-selection.md) — Language and framework selection; Next.js and Tailwind CSS choices that this ADR builds on
- [design-system-semantic-versioning](2026-10-11-design-system-semantic-versioning.md) — Versions the package this ADR established, so a white-labelling theme contract can be defined against its major
- [hades-delivery-to-external-consumers](2026-10-11-hades-delivery-to-external-consumers.md) — Delivers each release to consumers outside the kora frontend

---

## References

- [Design System PRD](../prd/design-system.md) — Product requirements document this ADR implements
- [Tailwind CSS v4 — @theme directive](https://tailwindcss.com/docs/theme)
- [Tailwind CSS v4 — @source directive](https://tailwindcss.com/docs/detecting-classes-in-source-files)
- [Radix UI Primitives](https://www.radix-ui.com/primitives)
- [shadcn/ui Documentation](https://ui.shadcn.com/) — scaffolding reference only; not a runtime dependency
- [class-variance-authority](https://cva.style/docs)
- [Storybook Documentation](https://storybook.js.org/docs)
- [pnpm Workspace Protocol](https://pnpm.io/workspaces)

---

## Notes

The name **Hades** (`@orbit/hades`) follows the kora metaphor. kora is another name for Persephone, who lives half the year in Olympus and half in the underworld. The product surfaces are the Olympus half, what people see. Hades is the underworld half of kora: the tokens and primitives beneath every screen, the layer every product surface stands on. The system was named Terra (the destination of every orbital descent, terra firma as solid ground) until the kora rebrand, and was renamed Hades to match. The file name of this ADR keeps the original name, as the kora rebrand did for its own historical ADRs.

The key discipline this ADR enforces is the **token boundary**: no component should ever reference a literal color value or arbitrary Tailwind shade. That discipline is what makes the system maintainable at scale. A component library where individual components reference `gray-900` instead of `surface` is not a design system — it is a collection of styled components with no shared visual contract.

## Amendment 1 (2026-10-11): Versioning and delivery

Hades gains semantic versioning and delivery to consumers outside the kora frontend, so that the white-labelling theme contract can be defined against the kora release Hades ships in. The decisions are in [design-system-semantic-versioning](2026-10-11-design-system-semantic-versioning.md) (version source of truth, pinning and exposure, the theme surface manifest and classification, and the classifier) and [hades-delivery-to-external-consumers](2026-10-11-hades-delivery-to-external-consumers.md) (what a release ships and how). Nothing in this ADR's decisions changes; the workspace-link consumption in the Implementation section remains how the frontend consumes the package.

**Last Updated:** 2026-10-11 (Amendment 1: versioning and delivery)
