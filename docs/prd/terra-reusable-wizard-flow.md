# Terra Reusable Wizard Flow — Product Requirements Document

**Status:** In Review  
**Created:** 2026-08-03  
**Last Updated:** 2026-08-03  
**Owner:** Platform Engineering  
**Stakeholders:** Platform Engineering, Frontend

---

## Executive Summary

Orbit now has two wizard flows — the agent builder and the trigger wizard — both built from the same structural pattern but implemented independently at Tier 3. The agent builder ADR noted explicitly that the shell was intended to be reused the moment a second builder-shaped flow adopted it; the trigger wizard is that second flow. This PRD ratifies the promotion: a set of Terra Tier 1 components covering the wizard chrome, the step rail, and the review summary card, so that the next wizard composes the pattern from the design system rather than reconstructing it by hand.

---

## Problem Statement

### Current State

Both the agent builder (`agents/_components/builder/`) and the trigger wizard (`triggers/_components/trigger-wizard/`) share an identical structural pattern:

- A `Modal size="builder"` containing a hairline progress strip, a vertical step rail, a scrollable content column, and a sticky footer with Back / Continue / Submit controls.
- A step rail built from `StepDot` and `StepConnector`, with steps rendered as ghost tile buttons, a `done ? check : index` inside each dot, a disabled-past-`maxVisited` rule, and a fixed info card at the bottom.
- A review step composed of `ReviewGroup` cards — `Card > CardHeader (icon + title + Edit button) > CardContent (key/value rows separated by Separators)` — one card per preceding wizard step.

None of this lives in Terra. The step rail is written twice as `StepRail` (agents) and `WizardRail` (triggers). `ReviewGroup` is a private function in each feature's `step-review.tsx`. The overall modal chrome — header, progress strip, two-column layout, footer — is embedded in each feature's modal component. The row type is named differently (`{ k, v }` vs `{ key, value }`), and the triggers version has a `children` slot the agents version lacks.

### User Pain Points

- **The next wizard author** must either copy-paste the pattern a third time (which the agents builder ADR explicitly ruled out), or track down both existing implementations to understand what they share before writing any code.
- **Reviewers** must hold the two implementations in mind to evaluate whether a third wizard drifts from the pattern.
- **Design changes** to the wizard motif must be applied in two places, and there is no enforcement that they stay in sync.

### Business Impact

Orbit's builder-modal pattern is now established as the right shape for multi-step configuration tasks. Without a shared implementation, every new wizard adds duplication debt and risks introducing visual inconsistency in Orbit's most consequential flows.

---

## Goals and Objectives

### Primary Goals

1. Promote the wizard chrome, step rail, and review summary card to Terra Tier 1 as a single compound component: `import { Wizard } from '@orbit/terra'`.
2. Migrate both existing wizards to consume the promoted components, eliminating the duplicated implementations.
3. Ensure the promoted API is opinionated enough to keep future wizards visually consistent, without constraining how they manage state.

### Success Metrics

- `StepRail`, `WizardRail`, and the private `ReviewGroup` functions in both feature directories are replaced by the Terra components, with no net change to the visual output.
- A third wizard can be built by composing only Terra and Tier 2/3 components, without writing any structural chrome from scratch.
- `pnpm lint && pnpm tsc --noEmit` pass with zero errors after migration.

### Non-Goals

- **Wizard state management.** The hook-plus-context pattern the agent builder uses and the local `useState` pattern the trigger wizard uses are both correct for their cases. This PRD does not unify them.
- **A new feature wizard.** This work is a promotion and migration, not a product feature. No new wizard is built here.
- **The modal size itself.** `Modal size="builder"` already exists in Terra as a consequence of the agent builder ADR. This work sits inside it, not around it.
- **KindPicker or SuccessPanel generalisation.** These are feature-specific screens within the trigger wizard and agent builder respectively, with no shared structure worth promoting yet.

---

## User Stories

**As a** frontend developer building a new wizard-style flow,  
**I want to** import a single `Wizard` compound component from Terra,  
**So that** I can compose a consistent multi-step builder without reconstructing the pattern, and the names alone communicate what each piece is for.

**Acceptance Criteria:**
- [ ] `Wizard.Header` accepts `icon`, `title`, optional `subtitle` chip, optional `actions` slot, and `onClose` — rendering the shared header structure used by both wizards.
- [ ] `Wizard` renders the two-column chrome (rail slot, content column, footer slot) with the hairline progress strip, matching the visual output of both existing wizards.
- [ ] `Wizard.Footer` accepts `onNext` (required), `onBack`, `backLabel`, `nextDisabled`, `onSubmit`, `submitLabel`, `pending` (disables both buttons), `pendingLabel`, `hint`, `step`, and `totalSteps` — rendering the shared footer layout used by both wizards.
- [ ] `Wizard.Rail` accepts a `steps` array and renders the `StepDot`/`StepConnector`/ghost-tile-button rail, including disabled-past-`maxVisited` state and a `footer` slot for the info card.
- [ ] `Wizard.Step` renders a scrollable content region — one rendered per active step.
- [ ] `Wizard.ReviewStep` renders the review screen layout: a stack of `Wizard.ReviewCard` components.
- [ ] `Wizard.ReviewCard` renders a `Card` with icon, title, optional Edit action, key/value rows with `Separator` dividers, and an optional `children` slot for block-level content.
- [ ] TypeScript enforces that `Wizard` receives a `rail` prop typed as `Wizard.Rail`.
- [ ] All sub-components have co-located Storybook stories with autodocs.
- [ ] The Terra reference doc (`frontend/docs/reference/terra-design-system.md`) and component placement doc (`frontend/docs/reference/component-placement.md`) are updated in the same change.

---

**As a** developer maintaining an existing wizard,  
**I want to** migrate to the shared Terra components without any visible change,  
**So that** the migration is low-risk and reviewable as a straight substitution.

**Acceptance Criteria:**
- [ ] The agent builder's `StepRail` and `step-review.tsx`'s `ReviewGroup` are replaced by the Terra components.
- [ ] The trigger wizard's `WizardRail` and `step-review.tsx`'s `ReviewGroup` are replaced by the Terra components.
- [ ] Screenshots before and after are indistinguishable (verified in local cluster, per `make cluster-update`).

---

## Proposed Solution

### Component API

A single compound component exported as `Wizard`, with static sub-components that make the structure self-documenting and the wizard-specific intent visible at every use site.

```tsx
<Wizard
  rail={
    <Wizard.Rail
      label="New agent"
      steps={steps}
      current={step}
      maxVisited={maxVisited}
      onGoTo={goTo}
      footer={<InfoCard />}
    />
  }
  footer={
    <Wizard.Footer
      step={step}
      totalSteps={steps.length}
      onBack={step > 0 ? () => setStep(s => s - 1) : undefined}
      onNext={() => goTo(step + 1)}
      nextDisabled={!valid}
      onSubmit={handleSubmit}
      submitLabel="Create trigger"
      pending={pending}
      hint={!valid ? validation.hint : undefined}
    />
  }
>
  <Wizard.Header
    icon="zap"
    title="New agent"
    subtitle={draftId}
    actions={<SaveControls />}
    onClose={onClose}
  />
  {step === 0 && <Wizard.Step><StepIdentity /></Wizard.Step>}
  {step === 4 && (
    <Wizard.ReviewStep>
      <Wizard.ReviewCard icon="user" title="Identity" onEdit={() => goTo(0)} rows={[...]} />
      <Wizard.ReviewCard icon="bot" title="Behaviour" onEdit={() => goTo(1)} rows={[...]} />
    </Wizard.ReviewStep>
  )}
</Wizard>
```

**`Wizard`** — Root. Renders `Wizard.Header`, the hairline `Progress` strip, and the two-column body: `Wizard.Rail` on the left (via the `rail` prop, typed as `React.ReactElement<WizardRailProps>`), a vertical separator, and the scrollable content column on the right. The `footer` slot accepts a `Wizard.Footer` element.

**`Wizard.Header`** — The shared header row: icon, title, optional subtitle chip, `Stack grow`, optional `actions` slot, close button. The agent builder passes `subtitle={draftId}` and its save-status controls in `actions`; the trigger wizard passes `subtitle={definition.id}` and no `actions`.

**`Wizard.Rail`** — The left step navigator. `label` (eyebrow string), `steps: { id, title, sub, icon? }[]`, `current`, `maxVisited`, `onGoTo`, `footer` slot. Derives `StepDot` state from step position relative to `current`. Steps past `maxVisited` are disabled.

**`Wizard.Step`** — A scrollable content region for a single step. Wraps its children in the correct `overflowY="auto"` and `minWidth/maxWidth="document"` constraints.

**`Wizard.ReviewStep`** — The review screen layout. A `Stack` of `Wizard.ReviewCard` children with appropriate gap.

**`Wizard.ReviewCard`** — A single review group: `icon`, `title`, `rows: { label, value, mono? }[]`, optional `onEdit` (renders an Edit button when present), optional `children` for block-level content. Maps to `Card > CardHeader > CardContent > key/value rows`.

**`Wizard.Footer`** — The sticky footer row shared by both wizards: Back button, grow spacer, optional validation hint, step counter, and a Continue / Submit primary action. `onNext` is required; `onSubmit` overrides it with a Submit button on the final step. `pending` disables both the Back button and the primary action. Other variable parts: `onBack` (omit to hide Back), `backLabel` (defaults to `"Back"`), `nextDisabled`, `submitLabel` (defaults to `"Create"`), `pendingLabel` (defaults to `"Creating…"`), and `hint`. The trigger wizard's `"Choose kind"` back label and the agent builder's hidden-at-step-0 Back button both map cleanly to this interface.

The `rail` prop is typed as `React.ReactElement<WizardRailProps>` rather than `React.ReactNode`, making it a TypeScript error to omit the rail or pass arbitrary content in its place. The "1 or more steps" constraint is not compiler-enforced — steps are rendered one at a time by the caller — but the sub-component names make the required structure unambiguous.

### Migration

Both existing implementations are replaced by the Terra components in the same change that introduces them, per the motif-component-promotion precedent. Row property names are normalised to `{ label, value, mono? }`.

---

## Scope

### In Scope

- `Wizard` compound component in Terra, with sub-components `Header`, `Rail`, `Step`, `ReviewStep`, `ReviewCard`, and `Footer`.
- Co-located Storybook stories with autodocs for the compound component and each sub-component.
- Migration of agents and triggers modal headers → `Wizard.Header`.
- Migration of `agents/_components/builder/step-rail.tsx` → `Wizard.Rail`.
- Migration of `agents/_components/builder/step-review.tsx`'s `ReviewGroup` → `Wizard.ReviewCard`.
- Migration of `triggers/_components/trigger-wizard/wizard-rail.tsx` → `Wizard.Rail`.
- Migration of `triggers/_components/trigger-wizard/step-review.tsx`'s `ReviewGroup` → `Wizard.ReviewCard`.
- Updates to `frontend/docs/reference/terra-design-system.md` and `frontend/docs/reference/component-placement.md`.

### Out of Scope

- Wizard state management, context, or hook patterns.
- The trigger wizard's `KindPicker`, `SuccessPanel`, or any kind-specific step components.
- The agent builder's step components (`step-identity.tsx` etc.), header, or draft persistence.
- Any new wizard flow.

---

## Requirements

### Functional Requirements

**Must Have (P0):**
- [ ] `Wizard` and all sub-components exported from `@orbit/terra`.
- [ ] Each sub-component: `forwardRef`, `cva` variants where applicable, `data-slot`, `.displayName`, co-located story with `tags: ['autodocs']`.
- [ ] `Wizard`'s `rail` prop typed as `React.ReactElement<WizardRailProps>` — passing non-Rail content is a TypeScript error.
- [ ] Row property naming normalised to `{ label, value, mono? }` across both wizards.
- [ ] Both wizards migrated, including footer → `Wizard.Footer`, passing `pnpm lint && pnpm tsc --noEmit` with zero errors.
- [ ] `component-placement.md` updated to document the wizard organism group.

**Should Have (P1):**
- [ ] `Wizard.Header` `actions` slot documented with a Storybook story showing the agent builder's save-status + Save Draft button variant alongside the no-actions variant.
- [ ] `Wizard.Rail` footer slot documented with a Storybook story showing the "Policy-checked" and "Identity-first" info card variants from the existing wizards.
- [ ] `Wizard.ReviewCard`'s `children` slot documented with a Storybook story showing the prompt block variant from the trigger wizard.

### Non-Functional Requirements

**Consistency:** Visual output of both migrated wizards must be pixel-equivalent to the current implementation.

**Accessibility:** No regression against existing keyboard navigation or ARIA attributes in either wizard.
