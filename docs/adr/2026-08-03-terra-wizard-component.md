# Hades Wizard Component

**Date:** 2026-08-03  
**Status:** Proposed  
**Deciders:** Engineering  
**Technical Story:** [terra-reusable-wizard-flow](../prd/terra-reusable-wizard-flow.md)

---

## Context

The agent builder ([agent-builder](2026-07-17-agent-builder.md)) and the trigger wizard ([extended-trigger-types](2026-06-20-extended-trigger-types.md)) both implement an identical structural pattern: a `Modal size="builder"` containing a hairline progress strip, a vertical step rail on the left, a scrollable content column on the right, and a footer with Back / Continue / Submit controls. Neither implementation lives in Hades — both are Tier 3 inside their feature directory.

The agent builder ADR stated explicitly: *"The modal-over-context pattern introduced here is intended to be reused. If a second builder-shaped flow adopts it, the shell is the part worth promoting out of the agents feature."* The trigger wizard is that second flow.

The duplication is concrete:

**Step rail** — `StepRail` (agents) and `WizardRail` (triggers) are the same component written twice. Both render a vertical stack of ghost tile buttons, each containing `StepDot size="lg"` with state derived from position relative to `current`, connected by `StepConnector`. Both apply `disabled={i > maxVisited}` and `aria-current="step"` on the active entry. The only divergence is the step data source (context vs props) and the info card copy at the bottom.

**Review card** — Both `step-review.tsx` files define a private `ReviewGroup` function with identical DOM structure: `Card > CardHeader (leadingIcon + title + optional Edit Button) > CardContent > Stack of key/value rows with Separator dividers`. The row type is named differently (`{ k, v }` vs `{ key, value }`) and the triggers version adds a `children` slot for its multi-line prompt block.

**Modal chrome** — The two-column layout (rail | `Separator orientation="vertical"` | content column with `overflowY="auto"` and `minWidth/maxWidth="document"`), the hairline `Progress` strip, and the sticky footer are structurally identical in both modal components. Both features also share an identical header structure — icon container + title + subtitle chip + grow spacer + optional actions + close button — where only the subtitle content and the actions slot content differ (the agent builder carries a draft-id chip and save-status controls; the trigger wizard carries the kind name and no actions).

The motif-component-promotion ADR ([motif-component-promotion](2026-07-17-motif-component-promotion.md)) established the pattern: promote when a second confirmed consumer exists and the component has no kora domain knowledge. All three duplicated pieces — rail, review card, chrome — pass that test.

---

## Options Considered

### Option A: Promote as three separate Hades components

Export `WizardRail`, `WizardStep`, and `WizardReviewCard` as independent Hades Tier 1 components.

**Pros:**
- Maximum composability — a consumer can use the rail without the chrome, or the review card outside a wizard entirely.
- Each component is small and independently testable.

**Cons:**
- Nothing enforces that the three pieces are used together. A future wizard author could use `WizardRail` inside a completely different layout, drifting from the pattern.
- The import surface is three separate names with no structural relationship; the wizard pattern is only visible by convention.
- `WizardReviewCard` used outside a wizard review context would be an odd fit.

### Option B: A single compound component — `Wizard` (chosen)

Export a single `Wizard` compound component with static sub-components: `Wizard.Rail`, `Wizard.Step`, `Wizard.ReviewStep`, `Wizard.ReviewCard`, and `Wizard.Footer`. The `Wizard` root owns the `Modal` shell and renders the header internally from props (`open`, `onClose`, `icon`, `title`, `subtitle`, `actions`), making both the Modal boilerplate and the header private implementation details. The `rail` and `footer` props are optional to support fullPage steps that hide both. `Wizard.Step` accepts a `fullPage` prop: when active, rail and footer are hidden, the step fills the full modal body, and the progress bar displays a frozen value (last non-fullPage progress).

**Pros:**
- The names communicate intent at every use site: `<Wizard>`, `<Wizard.Rail>`, `<Wizard.ReviewCard>` — no ambiguity about what each piece is for.
- A single import. A new wizard author reaches for one name, not three.
- The `Modal` is no longer caller boilerplate that can be forgotten — it is owned by `Wizard`.
- `WizardHeader` is private: it cannot be used outside `<Wizard>`, preventing the misuse pattern of placing a standalone header outside the wizard tree.
- `Wizard.Step fullPage` brings pre-flow and post-flow screens (kind picker, success panels) inside the wizard, so header and progress bar are managed automatically for all phases.
- The typed `rail` prop makes the structural contract machine-enforced rather than documented-but-optional.
- Consistent with how Hades already structures compound components (`Card`/`CardHeader`/`CardContent`; `Tabs`/`TabsList`/`TabsTrigger`/`TabsContent`).

**Cons:**
- `Wizard.ReviewCard` is only reachable via the `Wizard` import; it cannot be imported standalone. Acceptable — its purpose is specific to wizard review screens.
- The "1 or more steps" constraint is not compiler-enforced; steps are rendered one at a time by the caller. The sub-component names make the intent clear without requiring a collection.

---

## Decision

**Option B** — a single `Wizard` compound component.

The compound component pattern matches Hades' existing API conventions and makes the relationship between the pieces unambiguous. `Wizard` owns the `Modal size="builder"` shell and renders a private `WizardHeader` from its own `icon`, `title`, `subtitle`, and `actions` props — callers never construct a header manually. The typed `rail` prop (optional when a fullPage step is active) gives structural enforcement at the one boundary where omission would silently break the layout. `Wizard.Step` accepts a `fullPage` prop that fills the modal body with no rail or footer; the progress bar freezes at its last non-fullPage value, advancing again when a normal step is active. `Wizard.Footer` absorbs the shared footer structure — Back button, step counter, Continue / Submit — with props for the variable parts.

---

## Implementation

### Component structure

One new directory: `frontend/src/packages/hades/src/components/wizard/`

```
wizard/
├── wizard.tsx          # Wizard root + all sub-components
├── wizard.stories.tsx  # Storybook stories with autodocs
└── index.ts            # Public re-exports
```

All sub-components live in a single file. Sub-components are attached as static properties on the root function after definition (`Wizard.Rail = WizardRail`, etc.) — the static-property compound pattern, distinct from Hades' flat export pattern (`CardHeader`, `CardContent`). Sub-components are not re-exported as flat named exports; consumers import them exclusively via the `Wizard` namespace. Prop types are exported flat from `wizard/index.ts` so consumers can type external references.

### TypeScript interface

```ts
export interface WizardRailProps {
  /** Eyebrow label at the top of the rail ("Create agent", "New trigger"). */
  label: string;
  /** Step metadata. `title`/`sub` normalise the agents `title`/`sub` and the
   *  triggers `railTitle`/`railSub` to a single consistent shape. */
  steps: { id: string; title: string; sub: string }[];
  current: number;
  /** Highest step index reached; steps beyond it are disabled. */
  maxVisited: number;
  onGoTo: (index: number) => void;
  /** The info card at the bottom of the rail. Copy differs between wizards. */
  footer?: React.ReactNode;
}

export interface WizardStepProps {
  children: React.ReactNode;
  /**
   * When true, the step fills the full modal body with no rail or footer.
   * Use for pre-flow (kind picker) and post-flow (success) phases.
   * The progress bar displays a frozen value while any fullPage step is active.
   */
  fullPage?: boolean;
}

export interface WizardProps {
  open: boolean;
  onClose: () => void;
  icon: IconName;
  title: string;
  /** Subtitle chip — the agent builder passes the draft id; the trigger wizard
   *  passes the kind name. Omit when there is nothing meaningful to show. */
  subtitle?: string;
  /** Arbitrary controls between the subtitle and the close button — the agent
   *  builder passes its save-status indicator here; the trigger wizard passes
   *  nothing. Optional. */
  actions?: React.ReactNode;
  /** Must be a <Wizard.Rail> element. Omit when the active step is fullPage. */
  rail?: React.ReactElement<WizardRailProps>;
  /** The footer row. Pass <Wizard.Footer> with the caller's step handlers.
   *  Omit when the active step is fullPage. */
  footer?: React.ReactElement<WizardFooterProps>;
  children: React.ReactNode;
}

export interface WizardReviewCardProps {
  icon: IconName;
  title: string;
  rows: { label: string; value: string; mono?: boolean }[];
  /** Renders an Edit button in the card header when provided. */
  onEdit?: () => void;
  /** Block-level content below the rows (e.g. the trigger wizard's multi-line
   *  prompt block, which does not fit a row chip). */
  children?: React.ReactNode;
}

export interface WizardFooterProps {
  step: number;
  totalSteps: number;
  /** Omit to hide the Back button (agent builder hides it at step 0). */
  onBack?: () => void;
  /** Defaults to "Back". Trigger wizard passes "Choose kind" at step 0. */
  backLabel?: string;
  /** Required. On the final step, provide `onSubmit` instead to swap Continue → Submit. */
  onNext: () => void;
  nextDisabled?: boolean;
  /** Swaps the primary action to a Submit button on the final step. */
  onSubmit?: () => void;
  /** Submit button label. Defaults to "Create". */
  submitLabel?: string;
  /** Disables both the Back button and the primary action, and shows `pendingLabel`. */
  pending?: boolean;
  /** Label shown on the primary action while `pending` is true. Defaults to "Creating…". */
  pendingLabel?: string;
  /** Validation hint shown between the step counter and the primary action. */
  hint?: string;
}
```

### Sub-component anatomy

**`Wizard`** (root) — Owns the `Modal size="builder" dismissOnOutsideClick={false}` shell and `ModalAccessibleTitle`. Renders a private `WizardHeader` from its own props. The hairline `Progress` strip sits below the header. When the active step has `fullPage`, only the progress bar and full-body step content render — rail and footer are hidden. Otherwise renders the two-column layout: `rail` on the left, a `Separator orientation="vertical" span="full"`, and the content column on the right. The progress bar displays a frozen value while any fullPage step is active, resuming live calculation when a normal step is active.

**`Wizard.Header`** (private) — The header row: `height="header"`, horizontal `Stack`, icon container + title + optional subtitle chip + `Stack grow` + optional `actions` slot + close button. Not exported; not usable outside `<Wizard>`.

**`Wizard.Rail`** — The vertical step navigator. Renders an `<Eyebrow>` from the `label` prop at the top. Derives `StepDot` state (`pending | current | complete`) from each step's index relative to `current`. Renders the step number or a check icon inside `StepDot size="lg"`. Connects dots with `StepConnector complete={i < current}`. Steps past `maxVisited` receive `disabled`. `aria-current="step"` on the active entry. The `footer` slot renders below a `Stack grow` spacer, matching both current info-card placements.

**`Wizard.Step`** — Scrollable content region for a single step. Without `fullPage`: outer `Stack grow overflowY="auto" align="center"` scroll wrapper, inner `Stack minWidth="document" maxWidth="document" padding="loose" gap="relaxed"` content constraint. With `fullPage`: `Stack grow overflow="hidden"` that fills the full modal body; rail, separator, and footer are not rendered while this step is active. Callers render one `<Wizard.Step>` at a time based on their own step state.

**`Wizard.ReviewStep`** — A `Stack gap="cozy"` wrapping `Wizard.ReviewCard` children — the gap value both review screens use between groups.

**`Wizard.ReviewCard`** — `Card > CardHeader (compact, divider, leadingIcon, title, optional Edit Button action) > CardContent (padded, compact) > Stack gap="none"` of row entries, each a horizontal `Stack` with a `caption subtle` label, grow spacer, and `caption` value (mono-optional). Rows beyond the first are preceded by `Separator span="full"`. Optional `children` render below the rows, separated by a `Separator span="full"`, for the trigger wizard's prompt block.

**`Wizard.Footer`** — The sticky footer row: horizontal `Stack` with a Back `Button variant="secondary"` (hidden when `onBack` is undefined, disabled when `pending`), `Stack grow`, optional `hint` `Text caption subtle`, step counter `Text caption subtle`, and a primary action button. When `onSubmit` is provided, the primary action is a Submit button with `submitLabel` (defaulting to `"Create"`); otherwise it is a Continue button driven by `onNext`. While `pending`, both buttons are disabled and the primary action label switches to `pendingLabel` (defaulting to `"Creating…"`). This matches the footer of both existing wizards exactly: the agent builder omits `onBack` at step 0; the trigger wizard passes `onBack={handleBack}` always (at step 0 it navigates back to the kind picker) and `backLabel={step === 0 ? 'Choose kind' : 'Back'}`; both use `hint` for per-step validation copy.

### Step metadata normalisation

The agents builder's `BUILDER_STEPS` uses `{ key, title, sub }` where `key` is only a React render key — the display fields are `title` and `sub`. The trigger wizard uses `{ id, railTitle, railSub }`. `Wizard.Rail` accepts `{ id, title, sub }`. Both features adapt their step metadata to this shape during migration: agents renames nothing for display fields (already `title`/`sub`); triggers maps `railTitle → title` and `railSub → sub` at the `steps` array construction site in `trigger-wizard.tsx`.

### data-slot values

Each sub-component carries a `data-slot` attribute for structural targeting, consistent with all Hades components:

| Sub-component | `data-slot` value |
|---|---|
| `Wizard` (root) | `"wizard"` |
| `Wizard.Header` (private) | `"wizard-header"` |
| `Wizard.Rail` | `"wizard-rail"` |
| `Wizard.Step` | `"wizard-step"` |
| `Wizard.Step fullPage` | `"wizard-step-full"` |
| `Wizard.ReviewStep` | `"wizard-review-step"` |
| `Wizard.ReviewCard` | `"wizard-review-card"` |
| `Wizard.Footer` | `"wizard-footer"` |

### Row property normalisation

The triggers `ReviewRow` uses `{ key, value, mono? }`; the agents version uses `{ k, v, mono? }`. `WizardReviewCardProps` uses `{ label, value, mono? }` — unambiguous and consistent with Hades' other labelled-value patterns. Both features update their `StepReview` to pass `label` instead of `key`/`k`.

### Migration

Both existing wizards are migrated in the same PR that introduces the component, per the motif-component-promotion precedent. The migrations are mechanical substitutions with no visual change:

| Before | After |
|---|---|
| `agents/.../step-rail.tsx` `StepRail` | `Wizard.Rail` with `label="Create agent"`, steps/current/maxVisited/onGoTo passed from context via modal |
| `triggers/.../wizard-rail.tsx` `WizardRail` | `Wizard.Rail` with `label="New trigger"`; map `railTitle → title`, `railSub → sub` at the `steps` array construction site |
| `agents/.../step-review.tsx` `ReviewGroup` (private) | `Wizard.ReviewCard` |
| `triggers/.../step-review.tsx` `ReviewGroup` (private) | `Wizard.ReviewCard`; remove the explicit `<Separator span="full" />` from the `children` prop — `Wizard.ReviewCard` emits it automatically before `children` |
| Agent builder header (inline in modal) | `Wizard.Header` with `subtitle={draftId}` and `actions={<SaveControls />}` |
| Trigger wizard header (inline in modal) | `Wizard.Header` with `subtitle={definition?.id}` and no `actions` |
| Agent builder footer (inline in modal) | `Wizard.Footer` with `onBack={step > 0 ? builder.back : undefined}` |
| Trigger wizard footer (inline in modal) | `Wizard.Footer` with `onBack={handleBack}`, `backLabel={step === 0 ? 'Choose kind' : 'Back'}`, and `hint={!validation.valid ? validation.hint : undefined}` |
| Two-column chrome in both modal components | `<Wizard rail={...} footer={...}>` |

The `StepRail` component in the agents feature reads step state from `useAgentBuilderContext()`. After migration it becomes a thin wrapper: the context hook call stays in the agent builder modal, which passes `step`, `maxVisited`, and `goTo` down as props to `<Wizard.Rail>`.

### Storybook

`wizard.stories.tsx` includes:

- **Default** — A full `<Wizard>` with `open`, `icon`, `title`, `subtitle`, a complete rail, a content area showing a `<Wizard.Step>`, and a `<Wizard.Footer>`.
- **WithSubtitle / WithActions** — `<Wizard>` with `subtitle` only; with `subtitle` and an `actions` slot (the save-status indicator from the agent builder).
- **FullPageStep** — `<Wizard>` with a single `<Wizard.Step fullPage>` showing a centered success-panel layout; progress bar frozen at 0%.
- **Footer variants** — `<Wizard.Footer>` at step 0 with Back hidden; mid-flow with Back and Continue; final step with Submit and a `pending` state; with a `hint` string shown.
- **Review screen** — A `<Wizard.ReviewStep>` with three `<Wizard.ReviewCard>` instances, one with an `onEdit` callback and one with a `children` prompt block.
- **Rail states** — `<Wizard.Rail>` standalone with `label="Create agent"` showing step 0 (all pending), step 2 (two complete, one current, rest pending), and final step (all complete).
- **Rail footer variants** — Two `footer` slot examples: the "Policy-checked" card from the trigger wizard and the "Identity-first" card from the agent builder.

All stories carry `tags: ['autodocs']`.

### Exports

`wizard/index.ts` re-exports `Wizard` and the prop types `WizardProps`, `WizardRailProps`, `WizardStepProps`, `WizardFooterProps`, `WizardReviewCardProps`. `WizardHeaderProps` is not exported — `WizardHeader` is a private implementation detail. `src/packages/hades/src/index.ts` gains the barrel re-export. `frontend/docs/reference/component-placement.md` and `frontend/docs/reference/hades-design-system.md` are updated in the same change.

---

## Consequences

**Good**

- The wizard pattern is now API, not convention. A third wizard author imports `Wizard` from Hades and the structure is immediately clear.
- The `Modal` shell is owned by `Wizard` — callers cannot forget to wrap in it, or use the wrong size.
- `WizardHeader` is private — it cannot be placed outside the wizard tree, which was an easy mistake to make when managing multi-phase wizards (kind picker, success panels) before `fullPage` existed.
- `Wizard.Step fullPage` brings pre-flow and post-flow phases inside the wizard. Progress bar, header, and modal are all managed automatically for all phases — no manual `Wizard.Header + Separator` in callers.
- The step rail, review card, and footer implementations are consolidated. Design changes to the motif propagate to all wizards from one file.
- The typed `rail` prop catches structural mistakes at compile time rather than at runtime.
- Row property naming is normalised across both wizards — no more `k`/`v` vs `key`/`value` divergence.
- The `children` slot on `Wizard.ReviewCard` generalises the trigger wizard's prompt block without special-casing it.
- `Wizard.Footer` captures the full footer layout: both wizards' footers are structurally identical; the variable parts (Back visibility, back label, validation hint, submit label, pending state) map cleanly to props.

**Bad**

- `Wizard.ReviewCard` is only importable via the `Wizard` namespace, not as a standalone export. This is a minor inconvenience if someone wants the review card motif outside a wizard; they can re-export it if the need arises.
- The agents builder `StepRail` currently reads from context directly, which means it needs no props at its call site. After migration it requires three props (`step`, `maxVisited`, `goTo`) to be threaded from the modal to `<Wizard.Rail>`. This is a small increase in explicitness that is otherwise healthy.
- The "1 or more steps" invariant is not enforced by TypeScript. It is a documentation constraint only.

**Neutral**

- The `Progress` strip is rendered by the `Wizard` root between `Wizard.Header` and the two-column body. It is not a separate caller concern.

---

## Related Decisions

- [agent-builder](2026-07-17-agent-builder.md) — Introduced the builder modal pattern; noted the shell should be promoted when a second flow adopted it.
- [motif-component-promotion](2026-07-17-motif-component-promotion.md) — Established the promotion pattern this ADR follows.
- [extended-trigger-types](2026-06-20-extended-trigger-types.md) — Introduced the trigger wizard, the second consumer that triggers this promotion.
- [terra-frontend-design-system](2026-03-25-terra-frontend-design-system.md) — Compound component conventions and folder-per-component structure this ADR follows.
