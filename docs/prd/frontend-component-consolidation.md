# Frontend Component Consolidation — Product Requirements Document

**Status:** Implemented
**Created:** 2026-04-20
**Last Updated:** 2026-04-21
**Owner:** Frontend
**Stakeholders:** Engineering

---

## Executive Summary

The Orbit control plane frontend has accumulated 22 recurring UI patterns — metric cards, modal dialogs, status badges, empty states, section headers, and more — that are implemented inline, independently, in each page or feature tab that needs them. There is no shared abstraction layer between the Terra design system and the product pages. This PRD defines the work to extract those patterns into reusable components placed at the correct level of the component hierarchy, eliminating duplication and establishing a stable, consistent UI vocabulary for all current and future dashboard features.

A Phase 0 of Terra primitive additions (Icon, ChatBubble, Show, Card `interactive`, Text enhancements) has been completed ([embeddings-and-responses-api-endpoints](../adr/2026-05-03-embeddings-and-responses-api-endpoints.md)). Phase 1 (`Modal`, `useDialog`, `FormField`) is complete. The remaining phases address the full suite of app-level shared components.

---

## Problem Statement

### Current State

The Terra design system (`@orbit/terra`) provides low-level primitives: `Button`, `Card`, `Badge`, `Stack`, `Grid`, `Heading`, `Text`, `Separator`, `Progress`, `Alert`. Product pages assemble these primitives into higher-order patterns — a metric card, a confirm dialog, an empty state — but every page or tab does so independently. There is no intermediate layer.

The result is that logically identical UI constructs — `StatCard` in `(dashboard)/page.tsx`, `KPICard` in `model-gateway/_components/overview-tab.tsx`, and `KPICard` in `policy/_components/overview-tab.tsx` — are three separate implementations of the same component. The same duplication applies to modal dialogs (5+ implementations of a raw `<dialog>` scaffold), status badges (5+ separate variant maps), empty states (the policy version is extracted but every other section reimplements it inline), and section headers (3 byte-identical occurrences). A `formatDate` utility function is copy-pasted into three separate files.

### User Pain Points

- **Developers building new features** cannot reach for a "confirm delete" dialog or an "empty state" or a "metric card" — they either discover one of the existing inline implementations and copy it, or build a new one from scratch, increasing divergence.
- **Developers maintaining existing features** must update the same logic or style in multiple places. A change to the modal backdrop blur, the status badge colour mapping, or the empty state layout must be applied to each duplication site separately.
- **Reviewers and AI tooling** cannot reason reliably about what "standard" UI patterns look like in this codebase because there is no canonical reference. Every file has its own version.
- **End users** experience subtle inconsistencies across dashboard sections — slightly different empty state copy, badge rendering, or modal sizing — that accumulate into an incoherent product feel.

### Business Impact

- Feature velocity is reduced: developers spend time rebuilding patterns that already exist elsewhere rather than implementing new product logic.
- Defect surface is widened: a bug in (for example) the modal open/close `useEffect` sync must be found and fixed in 5+ files.
- Design consistency erodes over time as each copy drifts independently.
- AI-assisted development is hampered because there is no vocabulary of intermediate-level components for Forge or v0.dev to reason about. Without `<MetricCard>`, `<EmptyState>`, and `<Modal>` in scope, generated code invents new inline implementations.

---

## Goals and Objectives

### Primary Goals

1. Extract the 22 identified recurring patterns into canonical, shared components, placed at the correct tier of the component hierarchy.
2. Establish a clear three-tier placement model that every developer and AI assistant can follow when deciding where a new component belongs.
3. Remove all duplicated `formatDate` / `formatTimestamp` / `fmtCost` utility implementations into `@orbit/utils`.
4. Make shared components **discoverable** — so developers and AI agents find them before writing inline alternatives.

### Success Metrics

- Zero inline reimplementations of the 22 extracted patterns remain in product pages after migration.
- The `formatDate`, `formatTimestamp`, and `fmtCost` helper functions exist once, in `@orbit/utils`, and all call sites import from there.
- New features that need a metric card, modal dialog, empty state, or status badge reach for the shared component rather than building inline.
- `pnpm lint` passes with zero errors after migration — no `no-raw-layout-elements`, `no-raw-inline-elements`, or similar violations introduced by the refactor.
- Every new Terra component (Tier 1) has a Storybook story under `Terra/<Category>/<Name>`. Every Tier 2 shared component has a Storybook story whose `title` is `App/Shared/<ComponentName>` — this is the Storybook sidebar path, not a filesystem directory.

### Non-Goals

- This work does **not** add new product features or change existing product behaviour.
- This does **not** redesign any component visually — extracted components must be visually identical to the pattern they replace.
- This does **not** introduce a new state management pattern — components remain stateless presentational units that accept controlled props.
- This does **not** address chart or data-visualisation component wrappers beyond the `ChartLegend` utility row.
- This does **not** migrate the Terra design system to a different component foundation.
- This does **not** add animation or motion primitives to Terra — the typing-indicator bounce and streaming cursor remain explicitly deferred (see [embeddings-and-responses-api-endpoints](../adr/2026-05-03-embeddings-and-responses-api-endpoints.md) Notes).

---

## User Stories

### Primary User Stories

**As a** frontend developer adding a new dashboard section (e.g., Agents, Tools, Audit),
**I want to** import `<MetricCard>`, `<EmptyState>`, `<PageSectionHeader>`, `<Modal>`, and `<StatusBadge>` from shared locations,
**So that** I can build a new feature tab without writing any UI boilerplate from scratch.

**Acceptance Criteria:**
- [ ] `MetricCard`, `EmptyState`, `PageSectionHeader`, `StatusBadge` are importable from `frontend/src/components/`
- [ ] `Modal`, `Modal.Header`, `Modal.Body`, `Modal.Footer` are importable from `@orbit/terra`
- [ ] All components are typed with full TypeScript prop interfaces
- [ ] All components accept and forward `className` for layout overrides where appropriate

---

**As a** frontend developer fixing a UI bug in the modal backdrop behaviour,
**I want to** fix the bug in one place,
**So that** the fix is automatically applied everywhere a modal is used.

**Acceptance Criteria:**
- [ ] All modal dialogs in the dashboard use the single `Modal` component from Terra
- [ ] The raw `<dialog>` + `useEffect` scaffold no longer appears in any product page or tab file
- [ ] The `useDialog` hook encapsulates open/close sync logic for cases where the shell cannot be used directly

---

**As a** frontend developer adding a "delete" flow to a new feature,
**I want to** reach for a `<ConfirmDialog>` component,
**So that** I do not need to re-implement the destructive-action confirmation modal pattern.

**Acceptance Criteria:**
- [ ] `ConfirmDialog` exists in `frontend/src/components/`
- [ ] It accepts `title`, `description`, `confirmLabel`, `confirming`, `error`, `onConfirm`, `onClose`, `open` props
- [ ] It internally composes `Modal` from Terra, not a raw `<dialog>`

---

### Secondary User Stories

**As a** frontend developer,
**I want** `formatDate` and `fmtCost` available from `@orbit/utils`,
**So that** I don't need to write date/currency formatting helpers in every file.

**As a** code reviewer,
**I want** a canonical reference for what each shared component looks like and where it lives,
**So that** I can reject PRs that inline-reimplement a pattern that already has a shared home.

**As an** AI coding agent (Forge),
**I want** a documented vocabulary of shared components and their import paths in my context window,
**So that** I generate code that composes `<MetricCard>` and `<ConfirmDialog>` rather than inventing new inline implementations.

---

## Proposed Solution

### High-Level Approach

Introduce a three-tier placement model for UI components and migrate every identified duplication to the correct tier. Alongside extraction, establish a discoverability layer so that every new developer and AI agent can find shared components before writing inline alternatives (see the [Component Discoverability](#component-discoverability) section).

| Tier | Location | Rule |
|------|----------|------|
| **1 — Design system primitives** | `frontend/src/packages/terra/` (`@orbit/terra`) | Truly generic, zero domain knowledge, could ship as a standalone library. Must be built from a ShadCN/Radix UI base. |
| **2 — App-level shared components** | `frontend/src/components/` | Domain-aware but cross-feature. Used by two or more distinct dashboard features. Not generic enough for Terra. |
| **3 — Feature-scoped shared components** | `frontend/src/app/(dashboard)/[feature]/_components/` | Used by two or more files within a single feature area (e.g., model-gateway). Not needed outside that feature. |

Extracted components must be:
- **Controlled**: they accept data and callbacks via props; they own no server-fetching logic.
- **Composable**: they do not make assumptions about their container layout.
- **Typed**: full TypeScript interface for every prop, exported alongside the component.
- **Compliant**: Terra conventions (`data-slot`, `React.forwardRef`, `cva`, `cn`) for Tier 1; ESLint-clean for all tiers.

### Component Placement

#### Tier 1 — Terra (`@orbit/terra`)

**Phase 0 additions — implemented ([embeddings-and-responses-api-endpoints](../adr/2026-05-03-embeddings-and-responses-api-endpoints.md) Terra Primitives):**

| Component / Change | Description | Status |
|--------------------|-------------|--------|
| `Icon` + `IconName` registry | Strongly typed icon primitive with curated `IconName` union (~75 icons), optional container wrapping, size/colour/filled props. Replaces all direct `lucide-react` imports in product code. | ✅ Complete |
| `Text as` prop | Restricted inline element override (`span \| li \| dt \| dd \| label \| legend`) | ✅ Complete |
| `Text preserveWhitespace` | Adds `whitespace-pre-wrap` for AI/chat content | ✅ Complete |
| `Text` micro + status colours + mono/tabular/truncate | `size="micro"`, `color="status-healthy\|degraded\|error"`, utility text props | ✅ Complete |
| `Show` | Responsive visibility — replaces `hidden sm:block` wrapper divs | ✅ Complete |
| `Card interactive` prop | `focus-within:border-accent` ring for input-in-card delegation | ✅ Complete |
| `ChatBubble` + `ChatBubbleActions` | Chat bubble compound component with directional rounding and hover-reveal actions | ✅ Complete |
| `no-direct-lucide-import` ESLint rule | Bans direct `lucide-react` imports in product code | ✅ Complete |
| Storybook stories for all above | `Icon`, `ChatBubble`, `Show` stories created; `Card` and `Typography` stories updated; Component Grid extended | ✅ Complete |

**Phase 1 additions — implemented:**

| Component | Radix/ShadCN base | Status |
|-----------|-------------------|--------|
| `Modal` + sub-components (`ModalHeader`, `ModalBody`, `ModalFooter`, `ModalAccessibleTitle`) | Radix `Dialog` | ✅ Complete |
| `useDialog(open, onClose)` | — (hook) | ✅ Complete |
| `FormField` (label + children + hint + error) | Radix `Label` | ✅ Complete |

All 5 raw `<dialog>` implementations (create-token-modal, revoke-token-modal, token-created-modal, edit-scopes-modal, model-detail-modal) and all inline policy modals (DeleteGrantModal, CreateGrantModal, DeleteRoleModal, UpsertRoleModal) have been migrated to `Modal`.

These three belong in Terra because they carry zero Orbit domain knowledge: `Modal` is a styled dialog shell, `FormField` is a label/input wrapper, `useDialog` is a DOM sync hook. Any Orbit UI surface — dashboard, embedded widget, admin panel — could use them.

#### Tier 2 — App-level shared (`frontend/src/components/`)

| Component | Inline in... | Who relies on it |
|-----------|--------------|------------------|
| `EmptyState` | `policy/_components/empty-state.tsx` (promote); `model-gateway/keys-tab.tsx` lines 231–249; `chat/page.tsx` | Dashboard home, model-gateway keys, chat, all future feature tabs |
| `ErrorBanner` | `policy/_components/error-banner.tsx` (promote); ad-hoc `<Text color="status-error">` elsewhere | Policy, model-gateway, chat, any page with server actions |
| `StatusBadge` | `dashboard/page.tsx` line 192; `availability-dot.tsx`; `logs-tab.tsx`; `grants-tab.tsx`; `roles-tab.tsx` | Dashboard home, model-gateway (availability, logs), policy (grants, roles) |
| `MetricCard` | `dashboard/page.tsx` (`StatCard`); `model-gateway/overview-tab.tsx` (`KPICard`); `policy/overview-tab.tsx` (`KPICard`) | Dashboard home, model-gateway overview, policy overview — and all future feature overviews |
| `PageSectionHeader` | `keys-tab.tsx` lines 213–223; `grants-tab.tsx` lines 96–109; `roles-tab.tsx` lines 84–97 | Model-gateway keys, policy grants, policy roles — and all future list/management tabs |
| `ConfirmDialog` | `DeleteGrantModal`; `DeleteRoleModal`; `RevokeTokenModal` | Policy (grants, roles), model-gateway (tokens) — and any future destructive action |
| `EventFeedItem` | `dashboard/page.tsx` events list; `overview-tab.tsx` `RecentLogsCard`; `logs-tab.tsx` | Dashboard home, model-gateway overview, model-gateway logs |

These belong in `frontend/src/components/` (not Terra) because they carry Orbit-specific domain knowledge — `StatusBadge` maps Orbit status tokens (`status-healthy`, `status-error`, `status-degraded`), `MetricCard` uses Orbit heading variants, and `ConfirmDialog` follows the Orbit destructive-action convention — but they are needed by two or more distinct feature areas of the dashboard.

#### Tier 3 — Feature-scoped (`[feature]/_components/`)

**Model-gateway** (`frontend/src/app/(dashboard)/model-gateway/_components/`):

| Component | Inline in... | Who relies on it |
|-----------|--------------|------------------|
| `ChartLegend` | `overview-tab.tsx` lines 343–373; all 4 usage chart components | Model-gateway overview + all 4 usage charts |
| `MiniBar` | `overview-tab.tsx` `ModelHealthCard` + `TopModelsCard` | Model-gateway overview tab |
| `StatRow` | `stat-cards.tsx`; `keys-tab.tsx` `KeyCard` metadata grid | Usage stat cards, keys tab |
| `CardDivider` | `stat-cards.tsx`; ad-hoc in other model-gateway cards | Multiple cards within model-gateway |

**Chat** (`frontend/src/app/(dashboard)/chat/_components/`):

| Component | Inline in... | Who relies on it |
|-----------|--------------|------------------|
| `MessageContent` | `chat/page.tsx` lines 325–371 | Chat page (promote to Tier 2 if AI output appears elsewhere) |
| `TypingIndicator` | `chat/page.tsx` lines 305–321 | Chat page |
| `SuggestionGrid` | `chat/page.tsx` lines 534–557 | Chat page |
| `ChatInputBar` | `chat/page.tsx` lines 661–755 | Chat page |

**Policy** (`frontend/src/app/(dashboard)/policy/_components/`):

| Component | Inline in... | Who relies on it |
|-----------|--------------|------------------|
| `ViewToggle` | `grants-tab.tsx` lines 337–370 (`GrantViewToggle`); `roles-tab.tsx` lines 294–327 | Grants tab, roles tab |
| `FilterChip` | `grants-tab.tsx` lines 229–257 | Grants tab (promote to Tier 2 if a second feature adds filter tabs) |
| `PrincipalBadge` | `grants-tab.tsx` lines 261–268; `overview-tab.tsx` lines 285–295 | Grants tab, policy overview |
| `QuickLinkCard` | `overview-tab.tsx` lines 469–483 | Policy overview (promote to Tier 2 if another feature overview needs nav cards) |

`ScopeChip` and `ScopePicker` already exist as extracted files within `policy/_components/` — no migration needed.

#### Utility Extractions (`@orbit/utils`)

| Local helper | Duplicated in | Target name in `@orbit/utils` |
|---|---|---|
| `formatDate(iso)` | `grants-tab.tsx`, `roles-tab.tsx`, `policy/overview-tab.tsx` — 3 copies | `fmtDate` |
| `formatTimestamp(iso)` | `policy/overview-tab.tsx` | `fmtDateTime` |
| `fmt(usd)` | `stat-cards.tsx` | `fmtCost` |
| `fmtTokens(n)` | `stat-cards.tsx` | extend `fmtNumCompact` or add `fmtTokens` |

---

## Component Discoverability

### Problem

As shared components are extracted to Tier 2 (`frontend/src/components/`) and Tier 3 (`_components/` directories), the risk of silent re-invention grows. A developer building a new feature tab must know that `MetricCard`, `EmptyState`, and `ConfirmDialog` already exist before they write their own. Without active discovery mechanisms, the shared component layer becomes invisible — which is worse than the original duplication problem, because now there are two incompatible locations to maintain.

AI coding assistants (Forge, v0.dev) face the same problem in a more acute form: without an explicit vocabulary of named components in their context, they will generate inline implementations of patterns that already have canonical shared homes.

### Discovery Mechanisms

#### 1. Barrel export + IDE autocompletion (immediate)

All Tier 2 components are exported from a single `frontend/src/components/index.ts` barrel. When a developer types `import { ... } from '@/components'` (or the path alias), their IDE surfaces the full shared component vocabulary via TypeScript autocompletion. This is the lowest-friction, always-available discovery path.

**Requirement**: The barrel must be kept complete. Every new Tier 2 component is added to the barrel at the same time as it is created.

#### 2. Storybook extension — `App/Shared/` category (Phase 2)

The Storybook instance already covers all Terra (Tier 1) components under `Terra/`. A new category `App/Shared/` is added to Storybook for Tier 2 components. Each component gets a minimal story — a `Default` showing canonical usage with Orbit-flavoured content, plus stories for each significant variant.

This creates a visual catalogue that product developers can browse at a glance — the same function the Terra component grid already serves for primitives, extended up the hierarchy.

**Scope**: One story file per Tier 2 component, added in Phase 2 at the same time as the component is extracted. Stories live alongside the component in `frontend/src/components/`.

#### 3. Golden path document: `app-components.md` (Phase 2)

A new golden path document at `frontend/docs/golden-paths/app-components.md` is the human- and AI-readable index of all Tier 2 components. For each component it provides:

- Import path (`import { MetricCard } from '@/components'`)
- When to use it (and when not to — common misuse patterns)
- Props summary (TypeScript interface)
- Minimal JSX example

This document is the primary reference for Forge and new engineers. It is updated whenever a Tier 2 component is added or modified. The existing `terra-design-system.md` golden path links to it.

#### 4. Tier annotation JSDoc headers (all tiers, from Phase 1)

Each extracted component file starts with a standardised JSDoc block:

**Tier 2:**
```tsx
/**
 * @tier 2 — App-level shared. Cross-feature; imported by model-gateway, policy, dashboard-home.
 * @promote-to-tier-1 if: domain knowledge is removed and a second non-dashboard Orbit surface needs it.
 */
```

**Tier 3:**
```tsx
/**
 * @tier 3 — model-gateway scoped.
 * @promote-to-tier-2 if: a second feature area outside model-gateway imports this component.
 */
```

These comments make tier membership discoverable via `grep -r "@tier"` without consulting a document. They also surface in IDE hover tooltips when a developer imports the component.

#### 5. `reusable-components.md` as the audit register (ongoing)

The existing `reusable-components.md` inventory is maintained as the authoritative migration-progress register. After each phase completes, the inventory is updated to mark migrated items as resolved. Residual inline implementations that have been superseded by a shared component are marked with their replacement import path.

#### 6. Agent surfacing strategy

Shared components only eliminate duplication if AI agents discover them *before* generating inline alternatives. This section defines the agent-facing requirements: what sources agents must consult, what they must do before and after touching any frontend component code, and when each tier is considered agent-ready.

##### Why explicit surfacing is required

Without deliberate surfacing, an agent given a feature task has no signal that `Modal`, `FormField`, or `MetricCard` already exist. It will write a bespoke `<dialog>` implementation or an ad-hoc `Card + Heading + Text` pattern, creating a new duplicate that is harder to remove than the original inline version. The discoverability infrastructure — authority chain, pre/post-task protocols, phase-gate readiness — is the mechanism that prevents this.

##### Authority chain

When beginning any frontend component task, an agent must consult these sources in priority order and stop as soon as a need is satisfied:

| Priority | Source | What to look for |
|----------|--------|-----------------|
| 1 | `AGENTS.md` (repo root) — § Frontend Development | Three-tier model summary and banned patterns — read on every frontend task |
| 2 | `frontend/docs/golden-paths/ui-components.md` | Placement decision tree, full Tier 1 Terra inventory, Tier 2 inventory, `@tier` annotation format |
| 3 | `frontend/docs/golden-paths/terra-design-system.md` | Full Terra component props, JSX examples, every ESLint rule explained |
| 4 | `frontend/src/packages/terra/src/index.ts` | Authoritative list of what is actually exported from `@orbit/terra` |
| 5 | `frontend/src/components/index.ts` (Phase 2+) | Authoritative list of Tier 2 app-shared components |

An agent must not write a component that already exists in a higher-priority source. If a component *almost* satisfies the need, extend it via props before duplicating it.

##### Pre-task protocol (required before writing any JSX)

1. **Check the Tier 1 and Tier 2 inventories** in `ui-components.md`. If an existing component covers the need, use it.
2. **Run the placement decision tree** ([embeddings-and-responses-api-endpoints](../adr/2026-05-03-embeddings-and-responses-api-endpoints.md) § Agent placement decision tree). Every new component must pass through the decision tree before a file is created.
3. **Never reach for a raw HTML element that has a Terra equivalent.** The substitution table is unambiguous:

| Banned raw element | Use instead |
|--------------------|-------------|
| `<div>`, `<section>`, `<article>` | `Stack` or `Grid` |
| `<h1>`–`<h6>` | `Heading` |
| `<p>`, `<span>` | `Text` |
| `<dialog>` | `Modal` + `useDialog` |
| `<input>`, `<label>` | `Input`, `Label`, `FormField` |
| `<a>` | `Anchor` |
| `<hr>` | `Separator` |
| `<svg>` / Lucide component | `Icon` |

4. **Never import from `lucide-react`** in `src/app/**` or `src/components/**`. Use `<Icon name="..." />`. If the icon is missing from the registry, add it to `src/packages/terra/src/components/icon/registry.ts` (three synchronized edits: lucide import, `IconName` union, `ICON_REGISTRY` map).

##### Post-task protocol (required in the same PR as the new component)

Every agent that creates a component owns all five of these steps:

1. **Update `ui-components.md`** — add the component to the correct tier table with import path and one-line description. A component absent from this document will be duplicated by the next agent.
2. **Add `@tier N` annotation** — the component file must open with the appropriate JSDoc block (see Tier annotation section above).
3. **Add the barrel export** — Tier 1: `frontend/src/packages/terra/src/index.ts`; Tier 2: `frontend/src/components/index.ts`.
4. **Write a Storybook story** — Tier 1: `title: 'Terra/<Category>/<ComponentName>'`; Tier 2: `title: 'App/Shared/<ComponentName>'` (Storybook sidebar path — not a filesystem directory).
5. **Verify clean** — run `pnpm lint` and `pnpm tsc --noEmit` from `frontend/` and resolve all issues before marking complete.

A component that skips any step is incomplete. The agent that creates it owns all five steps.

##### Phase-gated agent-readiness

| Phase | Tier | Agent-ready? | What agents can rely on |
|-------|------|-------------|------------------------|
| Phase 0 ✅ | Tier 1 — Terra core | ✅ Yes | All pre-existing Terra components in `@orbit/terra` barrel and documented in `ui-components.md`. `no-direct-lucide-import` enforced. |
| Phase 1 ✅ | Tier 1 — Phase 1 additions | ✅ Yes | `Modal`, `useDialog`, `FormField` in barrel and documented. All raw `<dialog>` consumers migrated. |
| Phase 2 ✅ | Tier 2 — app-shared | ✅ Yes | `frontend/src/components/index.ts` barrel exports all Tier 2 components. `app-components.md` golden path documents each component. Import via `@orbit/components`. |

Tier 2 is now complete. All components are in the barrel, documented in `app-components.md`, and carry `@tier 2` annotations. Agents should import Tier 2 components via `@orbit/components` (the `@orbit/*` path alias resolves to `./src/*`).

#### 7. Future: `prefer-shared-component` ESLint rule

Once the Tier 2 set stabilises (end of Phase 2), an ESLint rule that detects inline patterns matching known extracted components can be introduced. For example, detecting a `Card` wrapping a `Heading` + delta `Text` and suggesting `MetricCard`. This is explicitly **not part of the current scope** — it is noted here as the long-term enforcement mechanism for preventing re-proliferation after the initial extraction work.

### Discoverability Acceptance Criteria

- [ ] `frontend/src/components/index.ts` barrel exports every Tier 2 component
- [ ] Every Tier 2 component has at least a `Default` Storybook story under `App/Shared/`
- [ ] `frontend/docs/golden-paths/app-components.md` exists and documents every Tier 2 component with import path, usage guidance, and minimal JSX example
- [ ] Every extracted component file (all tiers) includes a `@tier` JSDoc annotation with promotion trigger
- [x] `frontend/docs/golden-paths/ui-components.md` covers the tier placement model, all Terra components, banned patterns, and `@tier` annotation format — surfaced via root `AGENTS.md` Frontend Development section (Phase 0 ✅)
- [x] Agent surfacing strategy documented: authority chain, pre/post-task protocols, phase-gated readiness (Phase 1 ✅)

---

## Scope

### In Scope

- Extraction and migration of all 22 identified component patterns to their designated tier location
- Extraction of 4 formatting utilities into `@orbit/utils`
- Updating all existing call sites to import from the new canonical location
- Storybook stories for all Terra (Tier 1) additions; `App/Shared/` Storybook category for Tier 2
- TypeScript prop interfaces exported alongside every new component
- ESLint compliance: all new components must pass `pnpm lint` with zero violations
- Component discoverability infrastructure (barrel export, `App/Shared/` Storybook, `app-components.md` golden path, `@tier` annotations, `ui-components.md` agent golden path, agent surfacing strategy)

### Out of Scope

- Visual redesign of any extracted component
- New product features or behaviour changes
- Chart/data-visualisation component wrappers (Recharts, etc.)
- Animation primitives (typing indicator bounce, streaming cursor — deferred to a future ADR)
- Backend changes of any kind

### Future Considerations

- `MessageContent`, `TypingIndicator`, and `ChatInputBar` should be promoted from Tier 3 to Tier 2 if AI-generated output rendering is needed outside the chat page (e.g., policy explain, agent scratchpad viewer).
- `ViewToggle` and `FilterChip` should be promoted to Tier 2 if a second non-policy feature introduces a card/list toggle or a filter bar.
- `MetricCard` may eventually be promoted to Terra (Tier 1) if a second Orbit surface (e.g., an embedded agent widget) needs a metric display outside the dashboard application.

---

## Requirements

### Functional Requirements

**Must Have (P0):**
- [x] `Modal`, `ModalHeader`, `ModalBody`, `ModalFooter` added to Terra; all raw `<dialog>` implementations replaced ✅
- [x] `useDialog` hook added to Terra as a companion to `Modal` ✅
- [x] `FormField` added to Terra; all 6+ inline label+input+hint stacks replaced ✅
- [x] `EmptyState` promoted to `frontend/src/components/`; all inline empty states replaced ✅
- [x] `ErrorBanner` promoted to `frontend/src/components/`; all inline error text patterns replaced ✅
- [x] `StatusBadge` created in `frontend/src/components/`; all inline status-to-badge maps replaced ✅
- [x] `MetricCard` created in `frontend/src/components/`; all 3 `StatCard`/`KPICard` implementations replaced ✅
- [x] `PageSectionHeader` created in `frontend/src/components/`; all 3 inline header stacks replaced ✅
- [x] `ConfirmDialog` created in `frontend/src/components/`; all 3 delete/revoke modal implementations replaced ✅
- [x] `fmtDate`, `fmtDateTime`, `fmtCost`, `fmtTokens`, `fmtUsd` added to `@orbit/utils`; all duplicate implementations removed ✅
- [x] `frontend/src/components/index.ts` barrel exports all Tier 2 components ✅
- [x] `frontend/docs/golden-paths/app-components.md` documents all Tier 2 components ✅
- [x] `frontend/docs/golden-paths/ui-components.md` includes tier placement decision tree, component vocabulary, and agent surfacing strategy ✅

**Should Have (P1):**
- [x] `EventFeedItem` created in `frontend/src/components/`; event list rows replaced in dashboard home ✅
- [x] `ChartLegend`, `StatRow` extracted to `model-gateway/_components/` and `model-gateway/usage/_components/` ✅ — `MiniBar` and `CardDivider` remain inline in `stat-cards.tsx` (CardDivider kept as local helper; MiniBar not a named pattern)
- [x] `ViewToggle`, `FilterChip`, `PrincipalBadge`, `QuickLinkCard` extracted to `policy/_components/` ✅
- [x] Chat `MessageContent` extracted to `chat/_components/` ✅ — `TypingIndicator` is a Terra primitive (already done); `SuggestionGrid` and `ChatInputBar` remain inline (page-specific)
- [x] All extracted components have `@tier` JSDoc annotations ✅
- [ ] All Tier 2 components have at least one Storybook story under `App/Shared/`

**Nice to Have (P2):**
- [x] Storybook stories for `Modal` variants (sizes, with/without description, scrollable body) ✅
- [x] Storybook story for `FormField` (required, optional, with hint, with error state) ✅
- [ ] `prefer-shared-component` ESLint rule (post-stabilisation)

### Non-Functional Requirements

**Developer Experience:**
- Extracted component APIs must be as simple or simpler than the inline pattern they replace — no increase in prop-passing burden.
- All components must be fully TypeScript-typed with exported prop interfaces.

**Compliance:**
- All new and migrated files must pass `pnpm lint` with zero violations.
- Terra additions must comply with Terra conventions: `data-slot`, `React.forwardRef`, `cva`/`cn`, `React.ComponentPropsWithoutRef`, ShadCN sub-component composition.
- No raw `<div>`, `<span>`, `<strong>`, or other restricted elements in product JSX (enforced by `local/no-raw-layout-elements`, `local/no-raw-inline-elements`).

**Compatibility:**
- No visual regressions — extracted components must produce identical DOM output to the patterns they replace.
- No behavioural regressions — modal open/close, badge variants, and empty state CTAs must work identically after migration.

---

## Dependencies

### Internal Dependencies

- **Terra design system** (`@orbit/terra`): `Modal` and `FormField` additions depend on the existing Terra package structure and conventions ([terra-frontend-design-system](../adr/2026-03-25-terra-frontend-design-system.md)).
- **`@orbit/utils`**: utility extractions require this package to accept new exports. Verify the barrel export pattern before adding helpers.
- **ESLint custom rules**: `no-raw-layout-elements`, `no-raw-inline-elements`, and `no-direct-lucide-import` constrain how extracted components are authored. All new components must be written within these constraints from the start.

### External Dependencies

- **Radix UI** (`@radix-ui/react-dialog`, `@radix-ui/react-label`): required as the primitive base for `Modal` and `FormField` in Terra. Both packages are part of the ShadCN ecosystem that Terra already builds on.

---

## Risks and Mitigations

| Risk | Impact | Probability | Mitigation |
|------|--------|-------------|------------|
| Extracted component API differs subtly from one of the inline implementations, causing a visual regression | Medium | Medium | Side-by-side visual comparison for each extracted component before removing the original. Migrate one component at a time. |
| `useDialog` hook misses an edge case from one of the 5 existing modal implementations (e.g., focus trap, scroll lock) | Medium | Low | Read all 5 modal implementations in full before writing the hook. Document any non-standard requirement as an explicit prop or escape hatch. |
| TypeScript prop interface is too restrictive for a call site that needs a non-standard configuration | Low | Medium | All root elements should extend `React.ComponentPropsWithoutRef` and spread unknown props. |
| Utility naming conflicts with an existing export in `@orbit/utils` | Low | Low | Check `@orbit/utils` barrel export before adding. Use domain-prefixed names (`fmtDate`, not `format`). |
| Tier 2 components are created without stories or golden-path entries, remaining undiscoverable | Medium | Medium | Make story + barrel export + golden-path entry a required checklist item for every Tier 2 PR. |
| Agent generates duplicate inline component because `ui-components.md` Tier 2 section is unpopulated | Medium | High (Phase 2 gap) | Phase-gated readiness table in § Agent surfacing strategy explicitly marks Tier 2 as not-yet-agent-ready until Phase 2 completes. |

---

## Alternatives Considered

### Alternative 1: Leave all components inline, add a catalogue document

**Description:** Do not extract any components. Document the "canonical" inline pattern for each concept so developers copy the right version.

**Why not chosen:**
- Documentation does not prevent drift — the next developer still copies and modifies.
- Bugs must still be fixed in N places.
- Has no effect on the `formatDate` triplication or the 6-occurrence `<input>` className string.

### Alternative 2: Extract everything into Terra regardless of domain specificity

**Description:** All 22 candidates land in `@orbit/terra`.

**Why not chosen:**
- Terra is domain-free. `MetricCard`, `StatusBadge`, `PrincipalBadge`, and `ConfirmDialog` carry Orbit domain knowledge (status token names, principal types, destructive-action semantics) that does not belong in a portable component library.
- Terra components require Storybook stories and strict ShadCN conventions — imposing that overhead on every intermediate component is not justified.

### Alternative 3: Single `(dashboard)/_components/` for all shared dashboard components

**Description:** Skip the Terra and `src/components/` tiers; put everything under `(dashboard)/_components/`.

**Why not chosen:**
- `Modal` and `FormField` are genuinely generic and belong in Terra where they can be used by any Orbit surface.
- A flat shared directory that mixes chart legends, policy badges, modal shells, and utility rows quickly becomes an undifferentiated dumping ground with no clear ownership.

---

## Open Questions

1. **`@orbit/utils` barrel export structure**: Does `@orbit/utils` currently export from an `index.ts` barrel, or must new utilities be imported from sub-paths? Needs verification before adding `fmtDate` and `fmtCost`.
   - **Status:** Open
   - **Owner:** Frontend

2. **ShadCN `Dialog` vs custom `Modal`**: ShadCN ships a `Dialog` component. Should Terra adopt it verbatim (copy-paste in, as is the ShadCN pattern) and add `Modal.Header`/`Modal.Body`/`Modal.Footer` sub-components on top? Or write a thin wrapper around Radix `Dialog` directly?
   - **Status:** Resolved — implemented as a thin wrapper around Radix `Dialog` directly with named sub-components. ✅

---

## Timeline and Milestones

### Phases

**Phase 0: Tier 1 — Terra primitive foundations (complete)**
- ✅ `Icon` + `IconName` registry added to Terra with Storybook stories (`Terra/Atoms/Icon`)
- ✅ `Text as`, `preserveWhitespace`, `micro` size, status colours, `mono`/`tabular`/`truncate` props added; Typography stories updated
- ✅ `Show` responsive visibility primitive added with Storybook stories (`Terra/Layout/Show`)
- ✅ `Card interactive` prop added; card Storybook stories updated with `Interactive` and `CardFooterVariants` stories
- ✅ `ChatBubble` + `ChatBubbleActions` added with Storybook stories (`Terra/Atoms/ChatBubble`)
- ✅ `no-direct-lucide-import` ESLint rule enforced; all product Lucide imports migrated to `<Icon name="..." />`
- ✅ Component Grid overview story extended with `Icon` and `ChatBubble` sections; raw Lucide JSX replaced with `<Icon>`

**Phase 1: Tier 1 — Terra modal and form additions (complete)**
- ✅ `Modal` (with `ModalHeader`, `ModalBody`, `ModalFooter`, `ModalAccessibleTitle` sub-components) added to Terra with Storybook story
- ✅ `useDialog` hook added to Terra
- ✅ `FormField` added to Terra with Storybook story
- ✅ All 5 raw `<dialog>` implementations (model-gateway modals) migrated to `Modal`
- ✅ All inline policy modals (DeleteGrantModal, CreateGrantModal, DeleteRoleModal, UpsertRoleModal) migrated to `Modal`
- ✅ Agent surfacing strategy documented in PRD and ADR; phase-gated readiness table added

**Phase 2: Tier 2 — App-level shared components**
- Create and migrate: `EmptyState`, `ErrorBanner`, `StatusBadge`
- Create and migrate: `MetricCard`, `PageSectionHeader`, `ConfirmDialog`, `EventFeedItem`
- Extract to `@orbit/utils`: `fmtDate`, `fmtDateTime`, `fmtCost`
- Create `frontend/src/components/index.ts` barrel
- Create `frontend/docs/golden-paths/app-components.md`
- Add `App/Shared/` Storybook stories for each Tier 2 component
- Update `ui-components.md` Tier 2 inventory section
- Update phase-gated readiness table to mark Tier 2 as agent-ready

**Phase 3: Tier 3 — Feature-scoped extractions**
- Model-gateway: `ChartLegend`, `MiniBar`, `StatRow`, `CardDivider`
- Chat: `MessageContent`, `TypingIndicator`, `SuggestionGrid`, `ChatInputBar`
- Policy: `ViewToggle`, `FilterChip`, `PrincipalBadge`, `QuickLinkCard`
- Add `@tier` JSDoc annotations to all extracted components

### Key Milestones

- [x] PRD Approved
- [x] [embeddings-and-responses-api-endpoints](../adr/2026-05-03-embeddings-and-responses-api-endpoints.md) Created and Accepted
- [x] Phase 0 complete — Terra primitive foundations in place; all `chat/page.tsx` raw-div suppressions resolved; `no-direct-lucide-import` enforced
- [x] Phase 1 complete — zero raw `<dialog>` elements remain in product code; `Modal`, `useDialog`, `FormField` in Terra; agent surfacing strategy documented
- [ ] Phase 2 complete — zero duplicate `StatCard`/`KPICard`/`EmptyState` implementations remain; `App/Shared/` Storybook category, `app-components.md` golden path, and Tier 2 section of `ui-components.md` all populated; Tier 2 marked agent-ready
- [ ] Phase 3 complete — all 22 patterns extracted; `@tier` annotations on all components; inventory in `reusable-components.md` fully resolved

---

## Appendix

### Related Documents
