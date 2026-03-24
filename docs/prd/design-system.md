# Orbit Terra Design System — Product Requirements Document

**Status:** Approved
**Created:** 2025-07-11
**Last Updated:** 2025-07-11
**Owner:** Frontend
**Stakeholders:** Product, Engineering

> **Name:** Terra (`@orbit/terra`, `frontend/src/packages/terra/`) — the destination of every orbital descent; terra firma means solid ground. The design system is the solid, stable visual foundation that everything else lands on.

---

## Executive Summary

Orbit needs a cohesive design system to build a production-quality control plane UI. Today, the frontend has no visual foundation — styles are ad-hoc inline Tailwind utilities with no token system, no reusable components, and no defined visual identity. This PRD defines the work required to establish a design foundation as an **isolated monorepo workspace package** with its own Storybook environment, enabling fast, consistent, and on-brand UI development for the Orbit control plane — and for any future Orbit surface that needs it.

---

## Problem Statement

### Current State

The Orbit frontend is a bare Next.js scaffold. The only styling in `globals.css` is two hardcoded color variables (`--background`, `--foreground`) and a system font fallback. The single page in the app (`page.tsx`) uses arbitrary Tailwind utility classes directly (`bg-gray-950`, `border-gray-800`, `text-green-400`) with no token abstraction. There are no reusable components, no layout primitives, and no visual identity system.

### User Pain Points

- **Developers building features** have no component library to reach for — every new UI element requires designing from scratch, leading to inconsistency and wasted time.
- **Developers styling UI** must use arbitrary gray/color shades without a semantic token system, making global visual changes (e.g., changing the brand accent color) require a full codebase search-and-replace.
- **End users of Orbit** (platform engineers, AI ops teams) will experience an incoherent visual product if each feature is built without a shared visual language.
- **AI-assisted development** (v0.dev, Forge) is hampered without a defined token vocabulary and component library that AI tools can reliably reason about.

### Business Impact

- Feature development velocity is reduced — every UI feature requires building primitives from scratch.
- Inconsistent UI reduces trust in Orbit as a production-grade control plane.
- Without a design system, AI-assisted UI generation produces output that doesn't integrate cleanly into the codebase.

---

## Goals and Objectives

### Primary Goals

1. Establish a semantic design token system that encodes Orbit's visual identity and can be applied consistently across all UI.
2. Integrate a battle-tested, AI-friendly component library (shadcn/ui) as the foundation for all Orbit UI components.
3. Deliver the design system as an isolated monorepo workspace package (`frontend/src/packages/terra/`) with its own Storybook environment, so components can be built, tested, and documented entirely independently of the application.

### Success Metrics

- All colors, typography, spacing, and radius values referenced in UI code come from design tokens — zero arbitrary Tailwind shades (e.g., `gray-950`) in component code.
- A suite of at least 8 foundational components is available and used in at least one real page.
- New UI features can be built using existing components without writing new CSS.
- The design system is documented well enough that an AI code assistant can generate on-brand components from a prompt without manual correction.
- Every P0 component has at least one Storybook story demonstrating its variants.

### Non-Goals

- This PRD does **not** define the full Orbit dashboard or any specific product feature UI.
- This is **not** a full Figma/design-file-first process — tokens and components will be defined in code.
- This does **not** include custom data visualization components (charts, graphs, timelines) — those are a future concern once the foundation is in place.
- This does **not** address mobile-responsive layouts — the initial target is desktop/tablet (control plane users).

---

## User Stories

### Primary User Stories

**As a** frontend developer building an Orbit feature,  
**I want to** reach for pre-built, on-brand components (buttons, cards, badges, inputs),  
**So that** I can focus on feature logic rather than styling primitives from scratch.

**Acceptance Criteria:**
- [ ] A `Button` component exists with variants (primary, secondary, ghost, destructive)
- [ ] A `Card` component exists for containing content blocks
- [ ] A `Badge` component exists for status indicators (e.g., agent status, policy state)
- [ ] All components use design tokens, not arbitrary utility classes
- [ ] Components are importable via `import { Button } from "@orbit/terra"`

---

**As a** developer using AI assistance (Forge, v0.dev) to generate UI,  
**I want to** have a well-defined token vocabulary and component set,  
**So that** AI-generated components integrate into the codebase without manual rework.

**Acceptance Criteria:**
- [ ] Design tokens are defined as named CSS custom properties exported from the package
- [ ] Tailwind v4 `@theme` maps semantic names (e.g., `--color-surface`, `--color-accent`) to token values
- [ ] An AI prompt referencing Orbit's token names produces on-brand output

---

**As a** design system developer,  
**I want to** build and iterate on components in complete isolation from the application,  
**So that** I can work on the design system without running the Next.js app or touching application code.

**Acceptance Criteria:**
- [ ] Running `pnpm storybook` from `frontend/src/packages/terra/` launches a fully functional Storybook with all components
- [ ] Storybook has no dependency on or awareness of the Next.js application
- [ ] Component changes are reflected in the consuming frontend immediately via workspace linking

---

**As an** Orbit platform operator,  
**I want to** use a UI that feels professional, technical, and consistent,  
**So that** I trust Orbit as a serious production control plane rather than a prototype.

**Acceptance Criteria:**
- [ ] The UI has a cohesive dark-mode-first visual identity
- [ ] Status indicators (agent health, policy evaluation results) use consistent semantic colors
- [ ] Typography is legible at both information-dense and summary-level views

---

### Secondary User Stories

**As a** developer,  
**I want to** apply the correct theme to new pages automatically (dark background, correct font),  
**So that** I don't need to manually apply base styles to every new route.

**Acceptance Criteria:**
- [ ] The root layout (`layout.tsx`) applies base theme classes globally
- [ ] New pages automatically inherit the correct background, font, and text color

---

## Proposed Solution

### High-Level Approach

Build the design system in four layers:

1. **Isolated Workspace Package** — The design system lives at `frontend/src/packages/terra/`, inside the frontend directory. It has its own `package.json`, TypeScript config, and Storybook instance. The frontend consumes it via the pnpm workspace protocol (`"@orbit/terra": "workspace:*"`), meaning changes are reflected immediately with no publish step. This isolation means the design system can be developed, tested, and documented with no dependency on application code.

2. **Design Tokens** — Orbit's visual vocabulary is defined as semantic CSS custom properties in the package's `src/tokens.css` file, exported and imported by the frontend's `globals.css`. Tokens are mapped into Tailwind v4 via `@theme inline` and cover: color (brand, surface, text, status, border), typography (font families, size scale), spacing extensions, and border radius. **The design system is intentionally flat — no shadow tokens are defined.** Depth and hierarchy are communicated through background lightness, borders, and spacing, not drop shadows. The frontend's Tailwind config uses the `@source` directive to scan the package's component files so token-based utility classes are generated correctly across both codebases.

3. **Component Library** — Components are built on **Radix UI** accessibility primitives, styled with Tailwind v4 and Terra tokens. All component code is owned by the Terra package — there are no runtime design-system dependencies beyond Radix UI, `class-variance-authority`, `clsx`, and `tailwind-merge`. Each component lives in its own named folder under `src/components/` (e.g. `src/components/card/`) containing: the component file (`card.tsx`), a Storybook story (`card.stories.tsx`), and an `index.ts` that re-exports the public API. This keeps everything related to a component co-located and discoverable. **Atomic design** (atoms → molecules → organisms) is a useful mental model for reasoning about component complexity and reuse, but it is not enforced as a directory structure. All components are exported from the package's public API (`frontend/src/packages/terra/src/index.ts`). Shadcn/ui is used purely as a **scaffolding tool** to generate initial Radix-based component code; after generation, all shadcn references are stripped and the components become fully Terra-native.

4. **Storybook** — Storybook runs against `frontend/src/packages/terra/` directly with no application code involved. It serves as the isolated development environment for building components and doubles as living documentation for design tokens and component variants. Every P0 component has a story file alongside its source.

### User Experience

Orbit's visual identity should feel like infrastructure — not a consumer app. The aesthetic should be:

- **Dark-mode first**: Deep, near-black backgrounds with carefully layered surface elevations
- **Technical but clean**: Monospace accents for identifiers (agent IDs, policy names), clear typographic hierarchy
- **Signal-oriented**: Status and health states are immediately readable through consistent semantic color use (green = healthy, amber = degraded, red = error/blocked)
- **Minimal chrome**: Controls are present and functional, not decorative

**Brand color direction:**
- Background: Very dark navy/neutral (e.g., `#0a0f1e` or near-black)
- Surface: Slightly elevated dark panels for cards and containers
- Accent/Primary: A cool electric blue or indigo — signaling "system intelligence"
- Status colors: Semantic greens, ambers, and reds for agent/policy states
- Text: High-contrast primary, muted secondary for supporting labels

#### User Flow — Developer experience (consuming app)

1. Developer needs a new UI element (e.g., an agent status card)
2. Imports primitives from the design system package: `import { Card, Badge } from "@orbit/terra"`
3. Composes them using Orbit token classes (e.g., `text-foreground`, `bg-surface`, `text-status-healthy`)
4. Result integrates visually without any bespoke CSS

#### User Flow — Design system development (isolated)

1. Developer is building or modifying a component in the design system
2. Runs Storybook from `frontend/src/packages/terra/`: `pnpm storybook`
3. Works entirely in isolation — no Next.js app running, no application code involved
4. Writes or updates the component's Story to cover all variants
5. Changes are immediately reflected in the consuming app via workspace linking

#### User Flow — AI-assisted component generation

1. Developer prompts AI with component description + "use Terra design tokens and Radix UI primitives"
2. AI generates component using Radix UI primitives + Terra semantic token classes
3. Developer places it in `src/components/<name>/`, adds a co-located Story and `index.ts`, exports from the package `index.ts`

### Key Components

- **Workspace package scaffold** (`frontend/src/packages/terra/`): `package.json`, `tsconfig.json`, public API entry point (`src/index.ts`)
- **Design Tokens** (`frontend/src/packages/terra/src/tokens.css`): Complete `@theme` block with all semantic token categories; exported and imported by the frontend
- **Component library** (`frontend/src/packages/terra/src/components/`): Folder-per-component structure — each component gets its own directory containing the component file, a co-located Storybook story, and an `index.ts` re-export. Initial set: `Button`, `Badge`, `Card` (exports `Card`, `CardHeader`, `CardTitle`, `CardDescription`, `CardContent`, `CardFooter`), `Input`, `Label`, `Separator`, `Skeleton`, `Tooltip`, `PageLayout`, `Section`
- **Shadcn scaffolding** (dev-time only): CLI used once to bootstrap Radix-based component code; output is immediately cleaned of all shadcn-specific imports and references before committing
- **Storybook instance**: Configured in `frontend/src/packages/terra/` to run against the package directly with Tailwind v4 support
- **Frontend integration**: `globals.css` updated to import `tokens.css` from the package; `layout.tsx` updated to apply global base theme classes; `page.tsx` refactored to import components from `@orbit/terra`

---

## Scope

### In Scope

- Monorepo workspace setup: `frontend/src/packages/terra/` directory, `package.json` with workspace protocol, root-level `pnpm-workspace.yaml` updated
- Design token definition (color, typography, radius, spacing extensions) in `frontend/src/packages/terra/src/tokens.css` — no shadow tokens; the system is intentionally flat
- Tailwind v4 `@theme` configuration exported from the package and imported by the frontend's `globals.css`
- Frontend Tailwind config updated with `@source` directive to scan package component files
- shadcn/ui initialization and configuration (`components.json`) targeting the package directory
- Installation of Radix UI primitives (as shadcn peer dependencies in the package)
- First 8 foundational UI components: Button, Card, Badge, Input, Label, Separator, Skeleton, Tooltip
- Storybook setup in `frontend/src/packages/terra/` with Tailwind v4 support; one story per P0 component
- Public package API (`frontend/src/packages/terra/src/index.ts`) exporting all components and the `cn()` utility
- Frontend `package.json` updated to depend on `"@orbit/terra": "workspace:*"`
- Updated root `layout.tsx` with global theme classes applied
- Updated `page.tsx` (landing page) refactored to import and use components from `@orbit/terra`
- A `cn()` utility helper (`class-variance-authority` + `clsx` + `tailwind-merge`)

### Out of Scope

- Custom data visualization components
- Animation/motion system (Framer Motion integration)
- Mobile-responsive layout system
- Dark/light mode toggle (dark-mode first, system preference respected via media query)
- Orbit-specific complex components (agent cards, policy editors, event feeds) — those follow once the foundation exists

### Future Considerations

- Motion design system (Framer Motion tokens for transitions)
- Complex Orbit-domain components (AgentStatusCard, PolicyBadge, EventFeed)
- Theme switching (light mode variant)
- Icon system (Lucide React, already a shadcn dependency)
- Form system with validation (React Hook Form + Zod, common shadcn pairing)
- Storybook deployment (hosted living documentation)

---

## Requirements

### Functional Requirements

**Must Have (P0):**
- [ ] Monorepo workspace package scaffolded at `frontend/src/packages/terra/` with correct `package.json`, TypeScript config, and public `src/index.ts` entry point
- [ ] Semantic color tokens defined and exported from `frontend/src/packages/terra/src/tokens.css`, mapped through Tailwind v4 `@theme` (background, foreground, surface, border, accent, status colors)
- [ ] Typography tokens defined (font families, scale mapped to Tailwind)
- [ ] `src/components/` directory in place with folder-per-component structure (each component in `src/components/<name>/` with co-located story and `index.ts`)
- [ ] Radix UI primitives installed; shadcn used only for bootstrapping — no shadcn references in committed component code
- [ ] Storybook running successfully against `frontend/src/packages/terra/` in isolation (no app code)
- [ ] `cn()` utility exported from the package's public API
- [ ] Button component with variants: default, secondary, ghost, destructive, outline — with Storybook story
- [ ] Card component with sub-components: CardHeader, CardTitle, CardDescription, CardContent, CardFooter — with Storybook story
- [ ] Badge component with variants: default, secondary, destructive, outline + Orbit status variants (healthy, degraded, error) — with Storybook story
- [ ] Frontend `package.json` references `"@orbit/terra": "workspace:*"`; `globals.css` imports `tokens.css` from the package
- [ ] Root `layout.tsx` applies global background and text color from tokens
- [ ] Landing `page.tsx` refactored to import and use components from `@orbit/terra` (no inline arbitrary color classes)

**Should Have (P1):**
- [ ] Input, Label, Separator components — each with a Storybook story
- [ ] Skeleton component for loading states — with Storybook story
- [ ] Tooltip component — with Storybook story
- [ ] `PageLayout` and `Section` layout wrapper components
- [ ] `frontend/src/packages/terra/README.md` documenting the token vocabulary, component list, and usage for AI assistant context
- [ ] Storybook "Design Tokens" documentation story showing all token swatches

**Nice to Have (P2):**
- [ ] Avatar component (for user/agent identity display)
- [ ] DropdownMenu component
- [ ] Dialog/Modal component
- [ ] Alert component with status variants

### Non-Functional Requirements

**Performance:**
- Component library adds no runtime JavaScript overhead beyond what's needed (shadcn is tree-shakeable)
- CSS bundle size increase from design tokens is negligible (custom properties are inlined, not generated as utilities)

**Developer Experience:**
- Import paths for components are short and predictable: `import { Button } from "@orbit/terra"`
- Design system components can be developed entirely in isolation via `pnpm storybook` from `frontend/src/packages/terra/` — no application server required
- The `cn()` utility is re-exported from the package's public API
- AI tools (Forge, v0.dev) can generate correct on-brand components using the documented token vocabulary in the package README and Storybook

**Accessibility:**
- All shadcn/ui components inherit Radix UI's WAI-ARIA compliance
- Color contrast ratios for text on backgrounds must meet WCAG AA (4.5:1 for body text, 3:1 for large text)
- Focus states must be visible and use a token-based focus ring color

**Maintainability:**
- Zero arbitrary Tailwind color shades in component source — all color references go through semantic tokens
- Adding a new status color requires changing only the token definition, not hunting through component files

---

## Dependencies

### Internal Dependencies

- **Monorepo workspace config** (`frontend/pnpm-workspace.yaml`): Declares `src/packages/*` as workspace members.
- **`frontend/src/packages/terra/`**: The workspace package. Scaffolded with `package.json`, `tsconfig.json`, and Storybook config.
- **`frontend/package.json`**: Will gain `"@orbit/terra": "workspace:*"` as a dependency. Runtime deps (`class-variance-authority`, `clsx`, `tailwind-merge`, `@radix-ui/*`) will live in the package's `package.json`, not the frontend's.
- **`frontend/src/app/globals.css`**: Must be updated to import `tokens.css` from the package; the full `@theme` block moves to the package.
- **`frontend/src/app/layout.tsx`**: Must be updated to apply base theme classes globally.
- **Frontend Tailwind config**: Must add `@source` directive pointing at `../packages/terra/src/**` (relative to `globals.css`) so component class names are included in the generated CSS.

### External Dependencies

- **shadcn/ui CLI** (`npx shadcn@latest init`): Used once as a scaffolding tool to generate Radix-based component boilerplate. Not a runtime dependency; no shadcn imports remain in committed code. Requires Tailwind v4 compatible version.
- **Radix UI** (`@radix-ui/react-*`): Accessibility primitives that underpin all Terra components; installed as runtime deps of `frontend/src/packages/terra/`.
- **class-variance-authority (cva)**: Variant management for components; dep of `frontend/src/packages/terra/`. CVA is a first-class part of the shadcn/ui ecosystem — every shadcn component uses it by convention, meaning AI tooling (Forge, v0.dev) is well-trained on the pattern and generates correct CVA-based variant schemas reliably.
- **clsx + tailwind-merge**: Class utility composition; deps of `frontend/src/packages/terra/`.
- **Lucide React**: Icon library used by shadcn components; dep of `frontend/src/packages/terra/`.
- **Storybook** (`@storybook/react`, `@storybook/react-vite`): Isolated component development environment; dev dep of `frontend/src/packages/terra/`. Version 8.x required for Tailwind v4 compatibility.

---

## Risks and Mitigations

| Risk | Impact | Probability | Mitigation |
|------|--------|-------------|------------|
| Monorepo workspace setup adds upfront complexity | Medium | Medium | Workspace protocol (`workspace:*`) is well-supported by pnpm. One-time setup cost; significantly lower than extracting components later. |
| Tailwind v4 `@source` cross-package scanning misconfigured | High | Medium | Frontend Tailwind config must explicitly declare `@source "../packages/terra/src"` (relative to `globals.css`). Validate by building frontend and confirming component utility classes are present in the CSS output. |
| shadcn/ui CLI incompatibility with Tailwind v4 | Low | Low | shadcn CLI is only used once for scaffolding. If the CLI fails, Radix-based component code can be written by hand — no runtime dependency is affected. |
| Storybook incompatibility with Tailwind v4 in the package context | Medium | Medium | Storybook 10.x supports Tailwind v4; use `@storybook/react-vite` builder with Tailwind v4 plugin. Validate during Phase 1 setup before building components. |
| Token naming conflicts with existing Tailwind utilities | Medium | Low | Tailwind v4 `@theme` uses explicit `--color-*` namespace; test token names against existing class usage in the project. |
| Design token choices don't scale to future dashboard density | Medium | Medium | Define tokens at semantic level (surface, surface-elevated) not literal level (gray-900) — semantic tokens can be remapped without changing component code. |
| Over-engineering the token system before real UI exists | Low | Medium | Start with the minimum token set needed for the P0 components. Expand tokens as real UI surfaces new needs. |
| AI-generated components use arbitrary classes instead of tokens | Low | High | Package README documents the full token vocabulary; Storybook doubles as living reference. |

---

## Alternatives Considered

### Alternative 1: Components inside `frontend/src/app/` (no separate package)

**Description:** Keep an earlier approach — components live at `src/app/components/` inside the Next.js app, with no workspace package.

**Why not chosen:**
- No isolation: design system development requires running the Next.js app, mixing concerns.
- No Storybook without significant extra configuration plumbing into the app.
- Harder to share with other future Orbit surfaces (e.g., an admin panel, a CLI dashboard) that need the same components.
- Design system changes are coupled to the application's development cycle.

### Alternative 2: Build components from scratch (no shadcn)

**Description:** Define all components as bespoke Tailwind-styled React components without using a third-party component library.

**Why not chosen:**
- Requires significant time building accessibility-correct primitives (focus management, keyboard navigation, ARIA roles) that shadcn/Radix provides for free.
- AI tooling has less reliable knowledge of bespoke component APIs vs. shadcn's well-documented, widely-used API surface.
- Reinvents solved problems (dropdown positioning, dialog focus trapping, tooltip portals).

### Alternative 3: Material UI (MUI) or Mantine

**Description:** Use a fully-managed component library with its own theming system.

**Why not chosen:**
- Both ship significant runtime CSS-in-JS or pre-built CSS that conflicts with the Tailwind v4-first approach.
- Components are dependencies, not owned code — customization requires overriding a third-party theme system rather than editing components directly.
- Less well-suited to a dark, technical, minimal aesthetic without significant style overrides.
- AI tooling generates better output for shadcn than for MUI/Mantine in the context of Tailwind projects.

### Alternative 4: Headless UI (by Tailwind Labs)

**Description:** Use Tailwind Labs' own headless component library.

**Why not chosen:**
- Smaller component surface area than shadcn; fewer primitives available.
- Less community adoption and AI training data than shadcn.
- shadcn is built on Radix which has broader primitive coverage and stronger accessibility story.

---

## Open Questions

1. **Design system name:** *(Decided)* — **Terra** (`@orbit/terra`, `frontend/src/packages/terra/`). The destination of every orbital descent; terra firma means solid ground. The design system is the solid, stable visual foundation that everything else lands on.
   - **Status:** Resolved

2. **Token naming convention:** *(Resolved)* Semantic naming — `--color-status-healthy`, `--color-status-degraded`, etc. This is more expressive for an agent control plane where "healthy", "degraded", and "idle" are domain concepts, not generic success/warning/error states. Generic role names like `--color-success` are interchangeable with any product; semantic names encode Orbit's specific operating model.
   - **Status:** Resolved

3. **Component output path:** *(Resolved)* Each component lives in its own named folder under `frontend/src/packages/terra/src/components/` — e.g. `src/components/card/card.tsx`, with a co-located `card.stories.tsx` and `index.ts` that re-exports the component's public API. There is no enforced subdirectory hierarchy beyond this folder-per-component convention; atomic design is a mental model, not a folder structure. The `src/app/` architecture in the frontend is unaffected — components are imported as a package, not co-located with app code.
   - **Status:** Resolved

4. **Geist font:** The current `layout.tsx` loads Geist Sans and Geist Mono from `next/font/google`. Should these remain as the design system's typography, or should we evaluate alternatives?
   - **Status:** Deferred
   - **Note:** No decision needed now. Because the design system references typefaces only by token name (`--font-sans`, `--font-mono`) and fonts are loaded by the Next.js app — not the package — swapping typefaces in the future is a one-line change in `layout.tsx` and a token value update. This is easy to amend whenever desired.

---

## Timeline and Milestones

### Phases

**Phase 1: Package Scaffold + Token Foundation** (Week 1)
- Scaffold `frontend/src/packages/terra/` with `package.json`, TypeScript config, and public `src/index.ts`
- Configure monorepo workspace (`pnpm-workspace.yaml`)
- Define complete design token vocabulary in `frontend/src/packages/terra/src/tokens.css`
- Initialize and validate Storybook in `frontend/src/packages/terra/` with Tailwind v4
- Wire frontend: add `@orbit/terra` dependency, import `tokens.css` in `globals.css`, add `@source` to Tailwind config
- Update `layout.tsx` to apply base theme

**Phase 2: Component Foundation** (Week 1–2)
- Use shadcn CLI to scaffold initial Radix-based component boilerplate; strip all shadcn references immediately after generation
- Place each component in its own folder under `src/components/<name>/` containing `<name>.tsx`, `<name>.stories.tsx`, and `index.ts`
- Add P0 components: `Button`, `Badge`, `Card` (with `CardHeader`, `CardTitle`, `CardDescription`, `CardContent`, `CardFooter` co-located in `card.tsx`) — each with co-located Storybook stories
- Add P1 components: `Input`, `Label`, `Separator`, `Skeleton`, `Tooltip` — each with Storybook stories
- Export all components from `src/index.ts`
- Refactor `page.tsx` to import from `@orbit/terra`

**Phase 3: Layout Primitives + Documentation** (Week 2)
- Create PageLayout and Section wrapper components
- Write `frontend/src/packages/terra/README.md` with token vocabulary and usage guide
- Add Storybook "Design Tokens" documentation story
- Add P2 components as needed by first real feature

### Key Milestones

- [ ] PRD Approved
- [x] Design system name decided — **Terra**
- [ ] ADR Created
- [ ] Monorepo workspace configured and validated
- [ ] Storybook running in isolation
- [ ] Design tokens defined and imported by frontend
- [ ] Atomic component structure scaffolded; Radix UI installed; shadcn references stripped
- [ ] P0 component suite complete with stories
- [ ] Landing page refactored to import from `@orbit/terra`
- [ ] First real Orbit feature built on design system

---

## Appendix

### Related Documents

- [ADR to be created](../adr/) — Architecture decision for package structure, Storybook setup, token conventions, and Tailwind cross-package config
- [model-gateway PRD](./model-gateway.md) — Example of a feature PRD; design system must support the UI this feature will require

### References

- [shadcn/ui Documentation](https://ui.shadcn.com/) — scaffolding reference only
- [Tailwind CSS v4 @theme Documentation](https://tailwindcss.com/docs/theme)
- [Radix UI Primitives](https://www.radix-ui.com/primitives)
- [class-variance-authority](https://cva.style/docs)
- [Storybook Documentation](https://storybook.js.org/docs)

### Glossary

- **Design Token**: A named, semantic CSS custom property that represents a single visual decision (e.g., `--color-accent` rather than `#3b82f6`).
- **Monorepo workspace package**: A package that lives inside the same repository as its consumers. The workspace protocol (`workspace:*`) allows the frontend to depend on `frontend/src/packages/terra/` directly — changes are reflected immediately without publishing or versioning.
- **shadcn/ui**: A CLI tool and component collection used as a one-time scaffolding aid to generate Radix-based component boilerplate. Shadcn is not a runtime dependency of Terra — all shadcn-specific code is stripped after generation, and the resulting components are Terra-native.
- **Atomic Design**: A methodology for reasoning about UI component complexity in a hierarchy from smallest (atoms) to largest (organisms). Terra uses it as a mental model — not a directory structure. It helps categorize components by how much they compose other components, without prescribing where files must live.
- **Radix UI**: A low-level, accessibility-first React component library providing unstyled UI primitives (dialogs, dropdowns, tooltips, etc.).
- **`@theme inline`**: Tailwind v4 syntax for registering custom CSS properties as Tailwind utility tokens.
- **`@source`**: Tailwind v4 directive for explicitly declaring additional paths to scan for utility class usage — required when components live outside the app's own directory.
- **`cn()`**: A utility function combining `clsx` (conditional class joining) and `tailwind-merge` (conflicting Tailwind class resolution) used universally in shadcn components.
- **Surface elevation**: Terra's approach to depth — UI layers are distinguished by progressively lighter background token shades (`background` → `surface` → `surface-elevated`), paired with subtle borders. No drop shadows are used; the aesthetic is intentionally flat.
- **Storybook**: An open-source tool for developing and documenting UI components in isolation. Version 8.x supports Tailwind v4 via the Vite builder.

---

**Approval Signatures:**

- [ ] Product Owner: _________________ Date: _______
- [ ] Technical Lead: _________________ Date: _______
- [ ] Security Review: N/A — no security-sensitive surface in this feature
