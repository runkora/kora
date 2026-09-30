# Frontend Component Consolidation

**Status:** Accepted
**Date:** 2026-05-04
**Last Updated:** 2026-04-21
**Deciders:** Engineering Team
**Technical Story:** [Frontend Component Consolidation PRD](../prd/frontend-component-consolidation.md) — eliminating 22 recurring inline UI patterns by extracting them into canonical shared components at the correct layer of the component hierarchy. Includes the absorption of the Terra Component Additions work stream (Icon, ChatBubble, Show, Card interactive, Text enhancements) following its completion in Phase 0.

---

## Context

The kora control plane frontend is structured around a Terra design system (`@orbit/terra`) that provides low-level primitives — `Button`, `Card`, `Badge`, `Stack`, `Heading`, `Text`, `Separator`, `Progress` — and a set of product pages and feature tabs that compose those primitives into full UI. What is missing is the intermediate layer: the collection of higher-order, application-aware components that every feature page assembles from the same primitives in the same way.

A systematic audit of the five densest page and component files revealed 22 recurring patterns implemented independently, without sharing. The most significant examples:

- **Modal dialogs**: 5 files (`create-token-modal.tsx`, `revoke-token-modal.tsx`, `DeleteGrantModal`, `CreateGrantModal`, `DeleteRoleModal`, `UpsertRoleModal`) each implement an identical raw `<dialog>` scaffold — two `useEffect` hooks for open/close sync, a `handleBackdropClick` callback, and an identical header/body/footer layout skeleton with the same `className` strings. The only variation is the content.
- **Metric cards**: `StatCard` in `(dashboard)/page.tsx`, `KPICard` in `model-gateway/overview-tab.tsx`, and `KPICard` in `policy/overview-tab.tsx` are three independent implementations of icon + label + metric heading + optional delta/breakdown in a Card.
- **Status badges**: Five files each maintain their own mapping from a status enum (`healthy | degraded | error | idle | pending`) to `<Badge variant={status} dot>`.
- **Empty states**: A clean `EmptyState` component exists in `policy/_components/empty-state.tsx` but is not shared — the model-gateway keys tab and the chat page both implement bespoke zero-state layouts inline.
- **Section headers**: The identical `Stack direction="horizontal" justify="between"` → `[Heading + Text] + Button` pattern appears verbatim in three separate tab files.
- **`formatDate`**: Defined as a local helper in three separate files.

The full inventory of 22 candidates — with source file paths and line numbers for every occurrence — is documented in `reusable-components.md`.

**Phase 0 (complete, 2026-04-21):** A lint-rule remediation pass on `chat/page.tsx` identified six additional missing Terra primitives (icon container, chat bubble, responsive visibility, whitespace preservation, interactive card, icon governance). Those primitives were designed, implemented, merged, and are now documented in the Phase 0 section of [Tier 1 — Component Assignments](#component-assignments) below. The companion ESLint rule `no-direct-lucide-import` enforces icon governance in CI. The original standalone ADR for that work has been folded into this document and is superseded by it.

### Background

[terra-frontend-design-system](2026-03-25-terra-frontend-design-system.md) established the Terra design system as an isolated monorepo workspace package built on ShadCN/Radix UI with strict authoring conventions (`data-slot`, `React.forwardRef`, `cva`, `cn`, `React.ComponentPropsWithoutRef`). Terra defines the floor: primitive, domain-free UI building blocks. [terra-frontend-design-system](2026-03-25-terra-frontend-design-system.md) intentionally excluded product-level composition — it was not designed to hold a `MetricCard` or a `ConfirmDialog`. The gap between Terra primitives and product pages has therefore been filled by each feature independently.

### Problem Statement

We need a set of architectural decisions that establish:

1. A three-tier model for where UI components live, with clear promotion criteria
2. Which of the 22 identified patterns belong in Terra (Tier 1), the app-level shared directory (Tier 2), or a feature-scoped directory (Tier 3)
3. The canonical API shape for each extracted component
4. How existing call sites are migrated

### Goals

- Eliminate all identified inline reimplementations by migrating them to canonical shared locations
- Define a durable placement model that guides future component creation decisions
- Ensure all extracted components comply with Terra conventions (for Tier 1) and ESLint rules (all tiers)
- Extract duplicated utility functions to `@orbit/utils`

### Non-Goals

- Visual redesign of any extracted component
- New product features or behaviour changes
- Addressing chart/data-visualisation component wrappers
- Animation or motion primitives (typing-indicator bounce dots, streaming cursor) — tracked in a future ADR
- Backend changes

---

## Decision Drivers

* **Duplication elimination**: The same `useEffect` pair, the same `className` string, and the same variant mapping table should not exist in N files. A single canonical source makes bugs easier to find and fixes automatic.
* **Developer ergonomics**: A developer building a new feature tab should be able to assemble it from named, importable building blocks rather than copying inline patterns from an adjacent file.
* **AI-assisted development**: Forge and v0.dev generate better output when there is a documented vocabulary of intermediate-level components. Without `<MetricCard>`, `<Modal>`, and `<EmptyState>` in scope, AI tools invent new inline implementations.
* **Terra boundary preservation**: Terra is a domain-free, portable design system. Components with kora domain knowledge (status tokens, principal types, destructive-action semantics) must not live in Terra — they belong one tier up.
* **ESLint compliance**: The custom rules `no-raw-layout-elements` and `no-raw-inline-elements` constrain how components can be written. Extracted components must be authored within these constraints from the start, not patched afterwards.
* **ShadCN convention for Terra additions**: Per [terra-frontend-design-system](2026-03-25-terra-frontend-design-system.md), new Terra components must be created from a ShadCN/Radix UI base component, not from scratch. This constrains the implementation path for `Modal` and `FormField`.
* **Lint rule purity**: Every suppressed raw HTML block is a potential vector for inconsistency and drift. Terra must supply a compliant replacement for every common pattern — including icon containers, chat bubbles, and responsive visibility wrappers.
* **Icon governance**: Freeform `lucide-react` imports give no compile-time guard against icon sprawl or library migrations. A curated `IconName` registry with a companion ESLint rule closes this gap.
* **Discoverability**: Shared components only eliminate duplication if developers and AI agents know they exist before writing inline alternatives. Discoverability mechanisms (barrel export, Storybook catalogue, golden path docs, tier annotations, agent context) are first-class deliverables.

---

## Considered Options

### Option 1: Three-tier model — Terra / `src/components/` / feature `_components/`

**Description:** Apply a strict placement hierarchy. Components are assigned to exactly one of three tiers based on their domain specificity and the breadth of their consumer set:

- **Tier 1 (Terra)**: Radix/ShadCN-based, zero kora domain knowledge, could be extracted to a standalone library. Required: `data-slot`, `React.forwardRef`, `cva`/`cn`, ShadCN sub-component composition, Storybook story.
- **Tier 2 (`frontend/src/components/`)**: kora domain-aware, but needed by two or more distinct dashboard features. Lightweight Storybook requirement: a single `Default` story under `App/Shared/<Name>` (not the full Terra authoring suite — no Component Grid entry, no `data-slot` conventions required); lint-clean and TypeScript-typed.
- **Tier 3 (`frontend/src/app/(dashboard)/[feature]/_components/`)**: Used within a single feature area by two or more files. Not needed elsewhere. Promotion to Tier 2 is triggered when a second feature area requires the same component.

**Pros:**
- Clear, mechanical placement rule — every component has an unambiguous home
- Terra boundary is preserved: domain-free primitives stay separate from application-aware patterns
- Feature directories remain lean — only feature-specific patterns live there
- Promotion path is explicit: Tier 3 → Tier 2 → Tier 1 as consumer breadth grows

**Cons:**
- Requires discipline to keep the tiers clean over time — a component placed in the wrong tier is not a compile error
- `src/components/` can accumulate components that should eventually move to Terra but never do
- A component needed by exactly one file does not fit neatly — single-use extractions still make sense for large inline blocks (e.g., `ChatInputBar`)

### Option 2: Two-tier model — Terra / single shared `(dashboard)/_components/`

**Description:** Everything that is shared at all goes into `(dashboard)/_components/`. No `src/components/` tier. Terra remains unchanged.

**Pros:**
- Simpler mental model: "is it Terra-generic or not?" is a binary question
- No decision required about whether something is "cross-feature" or "feature-scoped"

**Cons:**
- `(dashboard)/_components/` immediately becomes a mixed bag of chart legends, policy badges, modal shells, and metric cards — no internal structure
- No natural home for components used outside the `(dashboard)` route group (if any)
- Does not address Terra additions (`Modal`, `FormField`), which clearly belong in Terra — so a two-tier model is actually three tiers with one unnamed

### Option 3: Promote every extracted component into Terra

**Description:** All 22 candidates land in `@orbit/terra` as part of a Terra expansion.

**Pros:**
- Single import location for everything (`@orbit/terra`)
- Design system documents all reusable components in one place

**Cons:**
- Terra is domain-free by design. `MetricCard` uses kora heading variants and delta colour semantics. `StatusBadge` maps kora status token names. `ConfirmDialog` encodes the kora destructive-action pattern. None of these belong in a portable library.
- Terra components require Storybook stories and full ShadCN authoring conventions — imposing that overhead on every intermediate component is a significant friction cost that will slow extraction work.
- It makes Terra meaningfully harder for external consumers (or future open-source distribution) because it becomes tangled with kora-specific UX conventions.

### Option 4: Do nothing — add a catalogue document describing the canonical inline pattern

**Description:** Do not extract components. Instead, publish a document that shows the "correct" inline pattern for each concept, so at least all copies look the same.

**Pros:**
- Zero code change risk
- No migration cost

**Cons:**
- Duplication continues. Documentation does not prevent the next developer from making a small change in their copy.
- Bugs must still be fixed in N files.
- Has no effect on the `formatDate` triplication or the 6-occurrence `<input>` className string.
- AI tooling still cannot reason about shared component names because they do not exist.

---

## Decision

**Chosen Option:** Option 1 — Three-tier model: Terra / `frontend/src/components/` / feature `_components/`

### Rationale

Option 1 is the only approach that correctly handles the full spectrum of candidates. The 22 patterns fall into three distinct groups by nature:

1. `Modal` and `FormField` are genuinely domain-free (any kora surface could use a modal or a labelled form field). They belong in Terra, built from Radix UI with ShadCN conventions, with Storybook stories.
2. `MetricCard`, `StatusBadge`, `ConfirmDialog`, `EmptyState`, `PageSectionHeader`, `ErrorBanner`, and `EventFeedItem` are kora-specific in their semantics but are needed by two or more distinct feature areas of the dashboard (model-gateway + policy + dashboard home). They belong in `frontend/src/components/`.
3. `ChartLegend`, `ViewToggle`, `FilterChip`, `MessageContent`, `ChatInputBar`, and the other remaining candidates are used within a single feature area today. They belong in that feature's `_components/` directory, with an explicit promotion trigger when a second consumer appears.

Options 2, 3, and 4 all collapse important distinctions that the three-tier model preserves. The "do nothing" option (4) fails the primary goal. The "promote everything to Terra" option (3) violates the Terra boundary established by [terra-frontend-design-system](2026-03-25-terra-frontend-design-system.md).

---

## Component Assignments

### Tier 1 — Terra (`frontend/src/packages/terra/`)

#### Phase 0 — complete (2026-04-21)

The following primitives were added to Terra to eliminate lint-suppressed raw HTML patterns in `chat/page.tsx` and enforce icon governance across the codebase. All are domain-free and reusable by any kora surface.

| Component | Status | Storybook |
|-----------|--------|-----------|
| `Icon` + `IconName` + `no-direct-lucide-import` ESLint rule | ✅ Implemented | `Terra/Atoms/Icon` |
| `Text` enhancements (`as`, `preserveWhitespace`, `micro`, status colours, `mono`, `tabular`, `truncate`) | ✅ Implemented | `Terra/Atoms/Typography` |
| `Show` (responsive visibility) | ✅ Implemented | `Terra/Layout/Show` |
| `Card` `interactive` prop | ✅ Implemented | `Terra/Molecules/Card` |
| `ChatBubble` + `ChatBubbleActions` | ✅ Implemented | `Terra/Atoms/ChatBubble` |

**`Icon` / `IconName`**: A curated registry (`src/components/icon/registry.ts`) maps `IconName` string keys to Lucide components. The `Icon` component is the sole public surface; `lucide-react` imports are confined to the registry file. The `no-direct-lucide-import` ESLint rule (activated in `frontend/eslint.config.mjs` for `src/**/*.tsx`, excluding `src/packages/terra/**`) enforces this at CI time. `Icon` accepts `name`, `size` (`2xs`–`xl`), `variant`, `filled`, and `container`/`containerVariant` props. `className` is blocked — no escape hatch.

**`Text` enhancements**: Additive changes only. Adds `as?: 'span' | 'li' | 'dt' | 'dd' | 'label' | 'legend'` (restricted element override), `preserveWhitespace` (`whitespace-pre-wrap`, for AI/chat content), `size="micro"` (11 px), `color="status-healthy | status-degraded | status-error"`, `mono`, `tabular`, and `truncate` props. No existing call sites break.

**`Show`**: `<Show above="sm">` / `<Show below="md">`. Exactly one of `above` or `below` must be provided (TypeScript discriminated union). Breakpoints: `'sm' | 'md' | 'lg' | 'xl'`. Full Tailwind class string literals — no template construction (required for Tailwind v4 static scanner). Replaces all `<div className="hidden sm:block">` wrappers.

**`Card interactive`**: When `interactive={true}`, adds `focus-within:border-accent focus-within:ring-1 focus-within:ring-accent/30 transition-colors` and `data-interactive="true"`. Delegates the visual focus signal from an inner form control to the card border — eliminating the raw `<div>` wrapper that previously achieved the same effect.

**`ChatBubble` / `ChatBubbleActions`**: `direction="start|end"`, `variant="surface|accent"`. `ChatBubble` owns `max-w-[78%]` width cap, directional corner rounding (`rounded-tl-sm` / `rounded-tr-sm`), and the `group` scope. `ChatBubbleActions` owns `opacity-0 group-hover:opacity-100 transition-opacity` — must be a descendant of `ChatBubble`. The app layer maps `msg.role === 'user'` → `direction="end"`; Terra never sees message roles.

**Rationale**: The `direction`/`variant` API for `ChatBubble` correctly separates _visual intent_ (which end of the bubble track, which background treatment) from _application semantics_ (who sent the message). This boundary ensures the component is reusable and does not accumulate domain knowledge over time. The `IconName` registry approach provides a genuine governance gate — the companion lint rule closes the bypass route, giving Terra full ownership of the icon vocabulary. Restricting `Text as` to a literal union is a deliberate friction point: it forces any new inline-element need to go through a conscious decision, rather than silently accepting any element type.

---

#### Considered Options — Phase 0

##### `Icon` — curated registry vs. prop-passing

**Option A — `IconName` string union with internal registry (chosen)**
- Strongly typed at call site — unknown icon names are a compile error
- Single import point for all iconography — library migration touches only Terra
- Semantic aliases possible (`'close'` → `X`, `'loader'` → `Loader2`)
- Lint rule prevents bypass; registry is the only path to add an icon

**Option B — pass the Lucide component as a prop (`icon={Sparkles}`)**
- Pros: no registry to maintain; any Lucide component works immediately
- Cons: no controlled vocabulary; icon churn is invisible; library migration requires hunting all call sites; does not reduce `lucide-react` imports in product code — just wraps them
- **Rejected** — does not achieve icon governance

---

##### `Text as` — restricted union vs. `React.ElementType`

**Option A — `'span' | 'li' | 'dt' | 'dd' | 'label' | 'legend'` literal union (chosen)**
- Strongly typed; covers all legitimate inline text use-cases; expansion requires a conscious decision

**Option B — `React.ElementType`**
- Allows `<Text as="div">`, `<Text as={MyComponent}>` — effectively a backdoor around the lint rules
- **Rejected** — defeats the purpose of the enforcement system

---

##### `Show` — dedicated component vs. Stack props

**Option A — `<Show above="sm">` / `<Show below="md">` component (chosen)**
- Declarative and readable; encapsulates breakpoint class logic; works on any content regardless of layout context

**Option B — `hideAbove`/`hideBelow` props on `Stack`**
- Pros: no new component
- Cons: only applies when content is already in a `Stack`; forces a `Stack` where no layout is needed; `hidden sm:block` is commonly needed on non-layout wrappers
- **Rejected** — `Show` is a cleaner, more composable primitive

---

##### `Card interactive` — boolean prop vs. sub-component

**Option A — `interactive` boolean prop on `Card` (chosen)**
- No raw HTML in product code; generalises to any form-in-card scenario; composable with existing `Card` props and `cva` variants

**Option B — `CardFocus` sub-component**
- Pros: more explicit naming
- Cons: unnecessary nesting; a prop is idiomatic given `Card`'s existing `data-slot`/`cva` API
- **Rejected** — a prop is more in keeping with the existing `Card` API

---

##### `ChatBubble` — direction/variant API vs. role prop

**Option A — `direction="start|end"` + `variant="surface|accent"` (chosen)**
- Zero business logic in Terra; reusable in any messaging UI; `ChatBubbleActions` is independently testable; eliminates four suppressed `<div>` elements in `chat/page.tsx`

**Option B — pass `role="user|assistant"` directly to `ChatBubble`**
- Pros: slightly simpler call site
- Cons: leaks application domain (`user`, `assistant`) into the design system; violates the presentational-primitive constraint; cannot be reused in non-chat contexts
- **Rejected** — hard rule: no business logic in Terra

---

#### Phase 1 — complete (2026-04-24)

#### `Modal` + `Modal.Header` + `Modal.Body` + `Modal.Footer`

**Radix/ShadCN base**: `@radix-ui/react-dialog` (via ShadCN `Dialog` copy-paste pattern)

**Implementation approach**: Copy-paste ShadCN `Dialog` as the base component following the established Terra pattern. Add three named sub-components as styled `div` wrappers with `data-slot` attributes:
- `Modal.Header`: `border-b border-border p-5` with title `Heading` + optional description `Text`
- `Modal.Body`: scrollable content area, `p-5`
- `Modal.Footer`: `border-t border-border px-5 py-3` flex row for action buttons

**Props**:
```ts
interface ModalProps {
  open: boolean
  onClose: () => void
  size?: 'sm' | 'md' | 'lg'  // maps to max-w-sm | max-w-md | max-w-lg
  children: React.ReactNode
}
interface ModalHeaderProps {
  title: string
  description?: string
}
```

**Who relies on it**: All 5 existing modal implementations (create-token, revoke-token, delete-grant, create-grant, upsert-role/delete-role) plus all future modals across the entire app.

**Replaces**: The following raw `<dialog>` + `useEffect` + `handleBackdropClick` scaffold repeated across 5+ files:
```tsx
// This pattern is eliminated entirely:
const dialogRef = useRef<HTMLDialogElement>(null)
useEffect(() => { open ? dialogRef.current?.showModal() : dialogRef.current?.close() }, [open])
useEffect(() => { const el = dialogRef.current; const handler = () => onClose(); el?.addEventListener('close', handler); return () => el?.removeEventListener('close', handler) }, [onClose])
const handleBackdropClick = (e: React.MouseEvent<HTMLDialogElement>) => { if (e.target === dialogRef.current) onClose() }
```

---

#### `useDialog(open: boolean, onClose: () => void): { ref: RefObject<HTMLDialogElement> }`

A custom hook that encapsulates the two `useEffect` blocks and the backdrop-click handler for callers that cannot use the `Modal` shell directly (e.g., a Radix-based popover that needs to integrate with an existing `<dialog>`).

**Who relies on it**: Same consumers as `Modal`. Exported from the Terra package alongside `Modal`.

---

#### `FormField`

**Radix/ShadCN base**: `@radix-ui/react-label` (via ShadCN `Label` copy-paste)

**Implementation approach**: Thin wrapper that renders a `Stack gap="tight"` containing a ShadCN `Label`, the `children` slot, and an optional hint `Text`.

**Props**:
```ts
interface FormFieldProps {
  label: string
  hint?: string
  required?: boolean
  children: React.ReactNode
}
```

**Who relies on it**: `create-token-modal.tsx` (2 fields), `policy/grants-tab.tsx` `CreateGrantModal` (~3 fields), `policy/roles-tab.tsx` `UpsertRoleModal` (~3 fields). All 6+ occurrences of the raw `<Stack gap="tight"><label className="text-sm font-medium…"><input className="w-full rounded-md border border-border bg-surface-elevated…"><Text size="caption">` pattern.

#### Phase 1 implementation status

| Component | Status | Notes |
|-----------|--------|-------|
| `Modal` + `ModalHeader` + `ModalBody` + `ModalFooter` + `ModalAccessibleTitle` | ✅ Implemented | `Terra/Molecules/Modal` |
| `useDialog(open, onClose)` | ✅ Implemented | `frontend/src/packages/terra/src/hooks/use-dialog.ts` |
| `FormField` | ✅ Implemented | `Terra/Molecules/FormField` |
| All raw `<dialog>` consumers migrated | ✅ Complete | All 5 model-gateway modals + all inline policy modals migrated |

---

### Tier 2 — App-level shared (`frontend/src/components/`)

#### Phase 2 implementation status (complete 2026-04-24)

| Component | Status | Import |
|-----------|--------|--------|
| `EmptyState` | ✅ Implemented | `@orbit/components` |
| `ErrorBanner` | ✅ Deleted — styling transferred to `Alert variant="error"` in Terra (2026-04-28) | `@orbit/terra` `Alert` |
| `StatusBadge` | ✅ Implemented | `@orbit/components` |
| `PageSectionHeader` | ✅ Promoted to Tier 1 (2026-04-28) | `@orbit/terra` — zero domain knowledge confirmed |
| `ConfirmDialog` | ✅ Implemented | `@orbit/components` |
| `MetricCard` | ✅ Implemented | `@orbit/components` |
| `EventFeedItem` | ✅ Implemented | `@orbit/components` |
| Barrel `frontend/src/components/index.ts` | ✅ Complete | — |
| Golden path `frontend/docs/golden-paths/app-components.md` | ✅ Complete | — |

All callers migrated. All Tier 2 components carry `@tier 2` JSDoc annotations with `@promote-to-tier-1` triggers. `pnpm lint` and `pnpm tsc --noEmit` pass with zero errors.

---

#### `EmptyState`

**Source**: Promoted from `policy/_components/empty-state.tsx`. All callers migrated to `@orbit/components`.

**Props**:
```ts
interface EmptyStateProps {
  icon: IconName          // enforces icon governance — no raw SVG elements
  title: string
  description: string
  action?: React.ReactNode
}
```

**Who relies on it**: Policy (source), model-gateway keys tab, chat page, all future feature tabs.

---

#### `ErrorBanner`

**Source**: Promote `policy/_components/error-banner.tsx` verbatim.

**Props**:
```ts
interface ErrorBannerProps {
  message: string
}
```

**Who relies on it**: Policy (source), model-gateway tabs, chat, any page with server actions.

---

#### `StatusBadge`

Maps a kora status value to the correct `Badge` variant and display label. The mapping is kora-specific (status token names match `--color-status-*` CSS variables from Terra).

**Props**:
```ts
type OrbitStatus = 'healthy' | 'degraded' | 'error' | 'idle' | 'pending' | 'unknown'
interface StatusBadgeProps {
  status: OrbitStatus
  label?: string  // override auto-capitalised default
}
```

**Who relies on it**: `(dashboard)/page.tsx` line 192, `availability-dot.tsx`, `logs-tab.tsx`, `grants-tab.tsx`, `roles-tab.tsx` — 5 files today.

---

#### `MetricCard`

Replaces `StatCard` (dashboard), `KPICard` (model-gateway overview), and `KPICard` (policy overview). The `positiveIsBad` delta logic from the model-gateway version is normalised and included.

**Props**:
```ts
interface MetricCardProps {
  icon: React.ReactNode
  label: string
  value: string | number
  delta?: {
    text: string              // pre-formatted display string, e.g. "▲ 3.2% vs last 7 days"
    positive: boolean         // determines colour (green = positive, red = negative)
    positiveIsBad?: boolean   // inverts colour logic (e.g. error rate, latency)
  }
  breakdown?: Array<{ label: string; value: string | number }>
  href?: string          // makes the card a link via Next.js Link
}
```

**Who relies on it**: `(dashboard)/page.tsx`, `model-gateway/overview-tab.tsx`, `policy/overview-tab.tsx` — 3 files today; all future feature overview pages.

---

#### `PageSectionHeader`

> **Promoted to Tier 1 (2026-04-28).** `PageSectionHeader` carries zero kora domain knowledge — it is a generic layout primitive. It now lives at `frontend/src/packages/terra/src/components/page-section-header/` and is exported from `@orbit/terra`. All callers import from `@orbit/terra`. Storybook story at `Terra/Layout/PageSectionHeader`.

**Props**:
```ts
interface PageSectionHeaderProps extends React.HTMLAttributes<HTMLDivElement> {
  title: string
  description?: string
  action?: React.ReactNode
}
```

**Who relies on it**: `keys-tab.tsx`, `grants-tab.tsx`, `roles-tab.tsx` — 3 files; all future list/management tabs.

---

#### `ConfirmDialog`

Composes `Modal` from Terra. Encodes the destructive-action confirmation pattern: warning body text, optional inline error, cancel + destructive-variant action button.

**Props**:
```ts
interface ConfirmDialogProps {
  open: boolean
  onClose: () => void
  title: string
  description: string
  confirmLabel: string
  onConfirm: () => void
  confirming?: boolean
  error?: string | null
}
```

**Who relies on it**: `DeleteGrantModal` (policy/grants-tab), `DeleteRoleModal` (policy/roles-tab), `RevokeTokenModal` (model-gateway) — 3 files today; all future destructive actions.

---

#### `EventFeedItem`

A single row in a `divide-y` activity feed or log list. Status dot + identifier label + supporting metadata + message + timestamp.

**Props**:
```ts
interface EventFeedItemProps {
  status: OrbitStatus
  label: string           // primary identifier (monospace)
  meta?: string           // supporting identifier (e.g., agent name)
  message: string
  timestamp: string
}
```

**Who relies on it**: `(dashboard)/page.tsx` events list, `model-gateway/overview-tab.tsx` `RecentLogsCard`, `model-gateway/logs-tab.tsx` — 3 files today.

---

### Discoverability

Extracting components eliminates existing duplication. Discoverability prevents new duplication from forming. Without active mechanisms, shared components become invisible — which is worse than the original problem, because now two incompatible locations exist for the same pattern.

The following mechanisms are first-class deliverables alongside the component extractions. They are not optional follow-ups.

| Mechanism | Phase | Description |
|-----------|-------|-------------|
| Barrel export + JSDoc | Phase 2 | `frontend/src/components/index.ts` exports every Tier 2 component with a one-line JSDoc. Autocompletes in IDE. Rule: added in the same PR as the component. |
| Golden path: `app-components.md` | Phase 2 | `frontend/docs/golden-paths/app-components.md` — import path, when to use, TypeScript interface, minimal example. Linked from `terra-design-system.md`. |
| `App/Shared/` Storybook | Phase 2 | One story per Tier 2 component. The story's `title` field must be `"App/Shared/<ComponentName>"` — this is a **Storybook sidebar path**, not a filesystem directory. The component file itself lives at `frontend/src/components/`. At minimum a `Default` story with kora-flavoured content. |
| `@tier` JSDoc annotations | Phase 1+ | Each extracted component opens with `@tier N` + promotion trigger comment. Searchable via `grep -r "@tier"`. Surfaces in IDE hover. |
| `frontend/docs/golden-paths/ui-components.md` | Phase 0 ✅ | Golden path doc covering: (1) the three-tier placement model with decision tree, (2) all Tier 1 Terra components with import path and one-line description, (3) all Tier 2 shared components with import path and one-line description, (4) the placement algorithm for new components, (5) `@tier` annotation format and where it goes. Also surfaced via the root `AGENTS.md` Frontend Development section. Primary reference for Forge and other agents when generating or reviewing any frontend component code. |
| `reusable-components.md` status column | Phase 1+ | `inline` / `extracted` / `superseded` column tracks migration state. Used by reviewers to catch inline re-implementations. |

**Acceptance criteria:**

- [ ] `frontend/src/components/index.ts` barrel with JSDoc for every Tier 2 component
- [ ] `frontend/docs/golden-paths/app-components.md` documents every Tier 2 component
- [ ] `App/Shared/` Storybook story per Tier 2 component (`title: 'App/Shared/<ComponentName>'` — Storybook sidebar path, not a filesystem path)
- [ ] `@tier` annotation in every extracted component file (all tiers)
- [x] `frontend/docs/golden-paths/ui-components.md` with tier model, placement decision tree, full Terra inventory, and banned patterns — surfaced via root `AGENTS.md` Frontend Development section

- [ ] `reusable-components.md` has `Status` column with `superseded` entries for all completed migrations

---

### Agent surfacing strategy

The three-tier model only works if AI agents discover shared components **before** writing inline alternatives. This section defines the authority chain, the pre-task and post-task protocols that every agent must follow, and the phase gates that define when each tier becomes agent-ready.

#### Authority chain

When about to write any frontend component code, an agent must consult these sources in priority order:

| Priority | Source | What to look for |
|----------|--------|-----------------|
| 1 | `AGENTS.md` (repo root) — § Frontend Development | Three-tier model summary and banned patterns — read on every frontend task |
| 2 | `frontend/docs/golden-paths/ui-components.md` | Placement decision tree, full Tier 1 Terra inventory, full Tier 2 inventory, `@tier` annotation format |
| 3 | `frontend/docs/golden-paths/terra-design-system.md` | Full Terra component props, JSX examples, every ESLint rule explained |
| 4 | `frontend/src/packages/terra/src/index.ts` | Authoritative list of what is actually exported from `@orbit/terra` |
| 5 | `frontend/src/components/index.ts` (Phase 2+) | Authoritative list of Tier 2 app-shared components |

An agent must not write a component that already exists at a higher priority source. If a component satisfies the need, use it. If it almost satisfies the need, extend it via props rather than duplicating it.

#### Pre-task protocol

Before writing JSX for any new component or feature block, an agent must:

1. **Scan `ui-components.md`** — check the Tier 1 and Tier 2 inventories for an existing component that covers the need.
2. **Run the placement decision tree below** — if no existing component satisfies the need, the decision tree determines the correct tier before a single line is written.
3. **Never use a raw HTML element that has a Terra equivalent.** The mapping is unambiguous:
   - `<div>`, `<section>` → `Stack` or `Grid`
   - `<h1>`–`<h6>` → `Heading`
   - `<p>`, `<span>` → `Text`
   - `<dialog>` → `Modal`
   - `<input>`, `<label>` → `Input` + `FormField`
   - `<a>` → `Anchor`
   - `<hr>` → `Separator`
   - `<svg>` / Lucide component → `Icon`
4. **Never import from `lucide-react`** in `src/app/**` or `src/components/**`. Use `<Icon name="..." />`. If the icon is absent from the registry, add it to `src/packages/terra/src/components/icon/registry.ts` first (three synchronized changes: lucide import, `IconName` union, `ICON_REGISTRY` map).

#### Post-task protocol

After creating a new component at any tier, an agent owns all of the following in the **same PR**:

1. **Update `frontend/docs/golden-paths/ui-components.md`** — add the component to the correct tier table with its import path and a one-line description. A component absent from this document is invisible to the next agent and will be duplicated.
2. **Add the `@tier N` JSDoc annotation** at the top of the component file (see annotation format below).
3. **Add the barrel export** — Tier 1: `frontend/src/packages/terra/src/index.ts`; Tier 2: `frontend/src/components/index.ts`.
4. **Write a Storybook story** — Tier 1: `title: 'Terra/<Category>/<ComponentName>'`; Tier 2: `title: 'App/Shared/<ComponentName>'` (this is a Storybook sidebar path, not a filesystem directory).
5. **Run `pnpm lint` and `pnpm tsc --noEmit`** from the `frontend/` directory and resolve all issues before marking the task complete.

A component that skips any step is incomplete. The agent that creates it owns all five steps.

#### Phase-gated agent-readiness

| Phase | Tier | Agent-ready? | What agents can rely on |
|-------|------|-------------|------------------------|
| Phase 0 ✅ | Tier 1 — Terra core | ✅ Yes | All pre-existing Terra components discoverable via `ui-components.md` and `@orbit/terra` barrel. `no-direct-lucide-import` enforced. |
| Phase 1 ✅ | Tier 1 — Phase 1 additions | ✅ Yes | `Modal`, `useDialog`, `FormField` in barrel and documented. All raw `<dialog>` consumers migrated. |
| Phase 2 ✅ | Tier 2 — app-shared | ✅ Yes | `@orbit/components` barrel exports all 7 Tier 2 components. `ui-components.md` Tier 2 section populated. `app-components.md` golden path created. All callers migrated. |

Tier 2 is complete. Agents may import from `@orbit/components`.

---

### Agent placement decision tree

The following decision tree must be present in `frontend/docs/golden-paths/ui-components.md` so that Forge and other agents can apply it mechanically when writing or reviewing frontend component code:

```
When creating or extracting a UI component, determine its tier:

1. Is it used by only one file?
   Yes → keep inline. Extract only if the host file exceeds ~300 lines of JSX.

2. Are all consumers within the same feature area (e.g. all in model-gateway/)?
   Yes → Tier 3. Place in frontend/src/app/(dashboard)/[feature]/_components/.
         Add a @tier 3 JSDoc comment with the promotion trigger:
         // @tier 3 — [feature]-scoped. Promote to src/components/ if a second feature needs this.

3. Are consumers in two or more distinct feature areas?
   Yes → proceed to step 4.

4. Does the component contain any Orbit domain knowledge?
   Examples of domain knowledge: Orbit status token names (healthy/degraded/error),
   principal types (user/service-account/agent), destructive-action patterns,
   model/provider names, permission/scope vocabulary.

   No domain knowledge → Tier 1 (Terra).
     Place in frontend/src/packages/terra/src/components/.
     Must follow [terra-frontend-design-system](2026-03-25-terra-frontend-design-system.md) authoring conventions: data-slot, React.forwardRef,
     cva/cn, React.ComponentPropsWithoutRef, Storybook story required.
     Import via: import { ComponentName } from '@orbit/terra'

   Has domain knowledge → Tier 2 (app-level shared).
     Place in frontend/src/components/.
     Export from frontend/src/components/index.ts in the same PR.
     Import via: import { ComponentName } from '@/components'
```

This tree has no ambiguous branches. If a placement feels unclear, the tiebreaker is: **prefer the lower tier** (inline over Tier 3, Tier 3 over Tier 2, Tier 2 over Terra) and promote explicitly when the promotion trigger fires.

---

### Tier 3 — Feature-scoped

#### Model-gateway (`model-gateway/_components/`)

| Component | API summary | Replaces |
|-----------|-------------|----------|
| `ChartLegend` | `items: Array<{color: string, label: string}>` | Inline in `overview-tab.tsx` + all 4 usage chart files |
| `MiniBar` | `value: number`, `max: number`, `variant?: OrbitStatus` | `ModelHealthCard` + `TopModelsCard` inline bars |
| `StatRow` | `label: string`, `value: string \| number` | Inline in `stat-cards.tsx` + `keys-tab.tsx` |
| `CardDivider` | no props | `stat-cards.tsx` + ad-hoc `<figure>` wrappers in other cards |

#### Chat (`chat/_components/`)

| Component | API summary | Replaces |
|-----------|-------------|----------|
| `MessageContent` | `content: string` | `chat/page.tsx` lines 325–371 |
| `TypingIndicator` | no props | `chat/page.tsx` lines 305–321 |
| `SuggestionGrid` | `suggestions: Array<{icon, title, description, onSelect}>` | `chat/page.tsx` lines 534–557 |
| `ChatInputBar` | `value`, `onChange`, `onSubmit`, `onClear`, `model`, `onModelChange`, `disabled` | `chat/page.tsx` lines 661–755 |

#### Policy (`policy/_components/`)

| Component | API summary | Replaces |
|-----------|-------------|----------|
| `ViewToggle` | `value: 'cards' \| 'list'`, `onChange` | `GrantViewToggle` in grants-tab + `ViewToggle` in roles-tab |
| `FilterChip` | `label`, `count?`, `active`, `onClick` | Inline in `grants-tab.tsx` lines 229–257 |
| `PrincipalBadge` | `type: PrincipalType` | Inline in `grants-tab.tsx` + `overview-tab.tsx` |
| `QuickLinkCard` | `icon`, `title`, `description`, `href` | Inline in `overview-tab.tsx` lines 469–483 |

---

### Utility Extractions (`@orbit/utils`)

| Function | Signature | Replaces |
|----------|-----------|----------|
| `fmtDate(iso: string): string` | `"Jul 26, 2025"` | `formatDate` in grants-tab, roles-tab, policy/overview-tab |
| `fmtDateTime(iso: string): string` | `"Jul 26, 2025 14:32"` | `formatTimestamp` in policy/overview-tab |
| `fmtCost(usd: number): string` | `"$42.17"` | `fmt` in stat-cards.tsx |
| `fmtTokens(n: number): string` | `"1.2M"` | `fmtTokens` in stat-cards.tsx — or fold into existing `fmtNumCompact` |

All four follow the naming convention established in the kora coding preferences: domain-qualified, unambiguous names. They are exported from the `@orbit/utils` barrel and imported as `import { fmtDate } from '@orbit/utils'`.

---

### Promotion Triggers

A Tier 3 component should be promoted to Tier 2 when a second feature area outside its current feature needs to import it. A Tier 2 component should be considered for promotion to Tier 1 (Terra) when: (a) it carries no kora domain knowledge, and (b) it is or could be needed by a non-dashboard kora surface. Promotion decisions should be documented as a comment in the component file at the time of extraction:

```tsx
// Tier 3 — model-gateway scoped. Promote to src/components/ if a second feature needs a chart legend.
```

---

## Consequences

### Positive

- All 22 recurring patterns resolve to a single canonical import. Fixing a bug in the modal open/close logic, the status badge colour mapping, or the empty state layout requires editing one file.
- New feature tabs can be assembled from `<MetricCard>`, `<EmptyState>`, `<PageSectionHeader>`, `<Modal>`, and `<StatusBadge>` without writing boilerplate.
- AI tooling has a documented vocabulary of intermediate-level components to reason about. The component names map to unambiguous, stable import paths.
- The Terra package remains domain-free — its components remain portable and its authoring conventions remain enforceable via lint.
- `formatDate` and `fmtCost` exist once. Internationalisation or formatting changes require one edit.

### Negative

- Migration work: all existing call sites must be updated. Five modal implementations, three metric card implementations, three section header implementations, and multiple inline formatting functions must all be refactored. This is pure cost with no new functionality.
- A new component placement decision now requires consulting the tier model. This is a small cognitive overhead that most developers will internalise quickly, but it must be documented.
- Tier 3 components that are only used in one file today (e.g., `TypingIndicator`, `MiniBar`) are extracted "early" — their extraction is still worthwhile to prevent the host file from growing further, but a Tier 3 extraction with a single consumer is a weaker argument than one with two.

### Neutral

- `EmptyState` and `ErrorBanner` already exist as extracted components in `policy/_components/`. This decision promotes them — the code does not change, only the import path.
- The `ScopeChip` and `ScopePicker` components in `policy/_components/` are already in the correct Tier 3 location. No migration needed.
- The placement model described here is consistent with, and extends, the approach already used in `(dashboard)/_components/` for `dashboard-header.tsx`, `dashboard-sidebar.tsx`, and `scope-guard.tsx`.

---

## Implementation

### Migration Path

Migration proceeds in four phases:

**Phase 0 — Terra primitive foundations — ✅ Complete (2026-04-21)**

All work merged. See the [Phase 0](#phase-0--complete-2026-04-21) section in Component Assignments for full details. Summary:
- `Icon` + `IconName` registry + `no-direct-lucide-import` ESLint rule
- `Text` enhancements (`as`, `preserveWhitespace`, `micro`, status colours, `mono`, `tabular`, `truncate`)
- `Show` responsive visibility atom
- `Card interactive` prop
- `ChatBubble` + `ChatBubbleActions` molecule
- Storybook stories for all above; Component Grid updated

**Phase 1 — Tier 1 (Terra modal and form additions)**
1. Add `Modal` to Terra (`frontend/src/packages/terra/src/components/modal/modal.tsx`) following the ShadCN copy-paste pattern. Export `Modal`, `ModalHeader`, `ModalBody`, `ModalFooter`, and `ModalProps` from `index.ts`.
2. Add `useDialog` hook (`frontend/src/packages/terra/src/hooks/use-dialog.ts`). Export from `index.ts`.
3. Add `FormField` to Terra (`frontend/src/packages/terra/src/components/form/form-field.tsx`). Export from `index.ts`.
4. Write Storybook stories for both `Modal` and `FormField`.
5. Migrate each of the 5 raw `<dialog>` implementations to `Modal`. Delete the replaced scaffold code. Verify lint passes.
6. Migrate all 6+ `<Stack gap="tight"><label>…<input>…<Text>` stacks to `FormField`.

**Phase 2 — Tier 2 (`src/components/`) + discoverability infrastructure**
1. Promote `EmptyState` and `ErrorBanner` by moving files to `frontend/src/components/` and updating imports in `policy/_components/`.
2. Create `StatusBadge`, `MetricCard`, `PageSectionHeader`, `ConfirmDialog`, `EventFeedItem` in `frontend/src/components/`, one at a time.
3. For each new component: write it, migrate one call site, verify lint + visual output, migrate remaining call sites, delete original inline implementations.
4. Add `fmtDate`, `fmtDateTime`, `fmtCost` to `@orbit/utils`. Migrate all 3+ local helper definitions to imports.
5. Create `frontend/src/components/index.ts` barrel with one-line JSDoc per export. Each component ships its barrel entry in the same PR.
6. Create `frontend/docs/golden-paths/app-components.md` (import path, when to use, TypeScript interface, minimal example per component).
7. Create `frontend/docs/golden-paths/ui-components.md` with the tier placement decision tree, all Terra components, and all Tier 2 components with import examples. Link from `terra-design-system.md`.
8. Add `App/Shared/` Storybook story for each Tier 2 component.

**Phase 3 — Tier 3 (feature-scoped)**
1. For each feature area (model-gateway, chat, policy): create a new file per component in the feature's `_components/` directory, extract the inline component, update the host file to import it, delete the inline definition.
2. Work bottom-up within each feature: smallest components first (e.g., `CardDivider`, `StatRow`) then composite components that may compose them.
3. Add `@tier 3` JSDoc annotation to each extracted file with the promotion trigger.

### Implementation Rules

The following rules apply to all extracted components regardless of tier:

1. **No visual regressions**: The extracted component must produce identical DOM output to the pattern it replaces. Do not take the opportunity to redesign.
2. **Controlled props**: Components accept data and callbacks via props. No internal data fetching, no server actions, no context consumption unless explicitly documented.
3. **Full TypeScript types**: Every prop interface is exported alongside the component. No `any`, no implicit `object`.
4. **Lint-clean**: `pnpm lint` must pass with zero violations before marking a phase complete.
5. **Terra-tier additions follow [terra-frontend-design-system](2026-03-25-terra-frontend-design-system.md)**: `data-slot` on all elements, `React.forwardRef`, `cva`/`cn`, `React.ComponentPropsWithoutRef<'div'>` (not `React.HTMLAttributes`), ShadCN sub-component composition.
6. **Tier annotations**: Each extracted component file must open with a `@tier N` JSDoc comment stating its current tier and, for Tier 3, the trigger for promotion to Tier 2.
7. **Barrel-first**: Every Tier 2 component ships its `frontend/src/components/index.ts` export entry in the same PR. A Tier 2 component that is not in the barrel is incomplete.
8. **No escape hatches for icon governance**: Product code must never import from `lucide-react` directly. Use `<Icon name="..." />`. The `no-direct-lucide-import` ESLint rule enforces this; do not suppress it.
9. **Golden path doc stays current**: When a new component lands at any tier, `frontend/docs/golden-paths/ui-components.md` must be updated in the same PR — add it to the correct tier table with import path and one-line description. A component absent from the golden path doc is invisible to agents.

---

## Validation

### Success Metrics

**Phase 0 (complete):**
- [x] `Icon` + `IconName` registry + `no-direct-lucide-import` ESLint rule merged and enforced
- [x] `Text` enhancements merged; no existing call sites broken
- [x] `Show` merged; all `hidden sm:block` raw div suppressions in `chat/page.tsx` resolved
- [x] `Card interactive` merged; `focus-within` raw div suppression resolved
- [x] `ChatBubble` + `ChatBubbleActions` merged; chat bubble raw div suppressions resolved
- [x] Storybook stories written for all Phase 0 additions; Component Grid updated with Icon and ChatBubble sections
- [x] `no-direct-lucide-import` ESLint rule active; `component-grid.stories.tsx` raw Lucide usages replaced
- [x] `frontend/docs/golden-paths/ui-components.md` created with tier placement decision tree and full Tier 1 Terra inventory (Tier 2 section to be populated in Phase 2)

**Phase 1 (target):**
- Zero occurrences of the raw `<dialog>` + `useEffect` open/close scaffold remain in any product page or tab file.
- Zero occurrences of the `<Stack gap="tight"><label>` inline form field remain.
- `pnpm lint` passes with zero violations.

**Phase 2 (target):**
- Zero occurrences of a `StatCard`, `KPICard`, or equivalent inline metric card implementation remain.
- `formatDate` is defined in exactly one place (`@orbit/utils`).
- `frontend/src/components/index.ts` barrel exists with JSDoc for every Tier 2 component.
- `frontend/docs/golden-paths/app-components.md` documents every Tier 2 component.
- `frontend/docs/golden-paths/ui-components.md` documents the tier model, all Terra components, and all Tier 2 components with import examples. _(Doc created in Phase 0; Tier 2 section populated as components are extracted.)_
- Each Tier 2 component has an `App/Shared/` Storybook story.

**Ongoing:**
- A new feature tab can be assembled using only imported components — no inline component definitions required.
- `pnpm lint` passes across the entire `frontend/` tree with zero violations at every phase boundary.

### Monitoring

Code review: PRs introducing new dashboard features should be reviewed for inline reimplementations of the 22 extracted patterns. Reviewers can use `reusable-components.md` as the checklist of what now has a canonical shared home.

---

## Related Decisions

- [terra-frontend-design-system](2026-03-25-terra-frontend-design-system.md) — Terra design system conventions; defines Tier 1 authoring rules
- [frontend-scaffolding-and-toolchain](2026-03-17-frontend-scaffolding-and-toolchain.md) — Frontend scaffolding and monorepo structure
- Terra Component Additions (Phase 0) — originally a standalone ADR for the `Icon`, `ChatBubble`, `Show`, `Card interactive`, and `Text` enhancement decisions; superseded by and folded into the Phase 0 section of this document.

---

## References

- [Frontend Component Consolidation PRD](../prd/frontend-component-consolidation.md)
- Component inventory with source locations — see `reusable-components.md`
- [ShadCN component source](https://ui.shadcn.com/docs/components/dialog) — reference for Dialog copy-paste base

---

## Notes

The three-tier model described in this ADR is an extension of the implicit structure already present in the codebase: Terra exists (Tier 1), `frontend/src/components/` exists but is nearly empty (Tier 2, currently underused), and feature `_components/` directories exist (Tier 3). This ADR formalises what was already partially in place, fills in the missing Tier 2 components, and adds the missing Tier 1 primitives (`Icon`, `ChatBubble`, `Show`, `Card interactive`, `Text` enhancements, `Modal`, `FormField`) that the whole codebase was working around.

Phase 0 was executed as a standalone workstream (see superseded ADR) before this consolidation ADR was finalised. Merging it here ensures a single authoritative source of truth for all component placement decisions.

Two `eslint-disable-next-line local/no-raw-layout-elements` suppressions in `frontend/src/app/(dashboard)/chat/page.tsx` are **explicitly out of scope** for Phase 0 and remain in place: the `animationDelay` inline style on the typing indicator bounce dots, and the `animate-pulse` class on the streaming cursor. Neither has a Terra equivalent. Both carry `// TODO: replace with Terra animation primitive once implemented` comments and will be addressed in a future ADR covering Terra animation primitives.

**Last Updated:** 2026-04-21
