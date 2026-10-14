# Hades Design System Hardening

**Status:** Accepted
**Date:** 2026-05-04
**Last Updated:** 2026-04-24
**Deciders:** Engineering Team
**Technical Story:** [Hades Design System Hardening PRD](../prd/terra-design-system-hardening.md) — closing remaining escape hatches in the Hades design system through ESLint coverage extension, prop interface whitelisting, `style` prop removal from `Text`/`StatusDot`, and `FormField` accessibility auto-wiring.

---

## Context

[terra-frontend-design-system](2026-03-25-terra-frontend-design-system.md) established the Hades design system (`@orbit/hades`) as the mandatory UI primitive layer for the kora frontend. [embeddings-and-responses-api-endpoints](2026-05-03-embeddings-and-responses-api-endpoints.md) extended enforcement with a suite of custom ESLint rules and expanded Hades' component catalogue to eliminate recurring inline patterns across the product. The result of those two phases is a frontend that passes `pnpm lint && pnpm tsc --noEmit` with zero errors and has nine active custom ESLint rules preventing the most common escape hatches.

However, a clean lint report is not the same as a fully hardened design system. The following structural gaps remain after Phase 1:

### Remaining Escape Hatches

**Prop surface leakage via `Omit<React.HTMLAttributes>` blacklisting.**
All 35 Hades components use `Omit<React.HTMLAttributes<HTMLElement>, 'style' | 'color'>` (or similar) to remove specific props from the native HTML attribute set. The remaining native props — including `id`, `tabIndex`, `role`, `data-*` attributes, and many others — are inherited silently. This blacklist approach means the component's public API is implicitly defined by subtraction from the entire HTML spec rather than explicit declaration. Every time a new HTML attribute becomes problematic (e.g. `enterKeyHint`, `inert`), a human must notice and add it to every component's Omit.

**`style` prop still accepted by `Text` and `StatusDot`.**
`TextProps` and `StatusDotProps` include `style` in their inherited props (it is not in their Omit sets). This directly contradicts the design principle established in Phase 1 — where `Stack` and `Icon` had `style` removed — and means text and status content can still be styled with arbitrary inline CSS bypassing the token system.

**Raw `<button>`, `<code>`, and conditionally-exempted `<section>` elements not blocked.**
The existing `no-raw-inline-elements` rule covers `<span>`, `<strong>`, `<b>`, `<em>`, and related text formatting elements but does not cover `<code>`, `<kbd>`, `<samp>`, or `<var>`. The `no-raw-layout-elements` rule blocks `<section>` only when it carries a Tailwind layout class, not unconditionally. There is no rule at all covering `<button>`. All three element families have known occurrences in the app codebase and have been observed as agent workaround patterns.

**`FormField` context not consumed by Hades inputs.**
The `FormField` component establishes a React context (via `useFormField()`) that is intended to auto-wire `id`, `aria-describedby`, and `aria-invalid` onto the enclosed input element. None of the three Hades input components (`Input`, `Textarea`, `NativeSelect`) currently call `useFormField()`. The accessibility guarantee documented in `hades-design-system.md` is therefore not delivered — callers must thread `id` and aria attributes manually or they are absent from the DOM.

**Inconsistent `as`/`asChild` element-override API.**
Different Hades components use different mechanisms to let callers change the rendered HTML element: some expose `as?: 'span' | 'li' | ...`, some expose `asChild?: boolean`, and some expose both. There is no documented rule for which pattern to use in which situation, making the API surface unpredictable for both human and AI-assisted development.

### Background

The root cause of these gaps is that Phase 1 focused on establishing the enforcement framework (ESLint rules, rule ban on disable-comments, token system) and expanding the component catalogue. It explicitly deferred API-surface cleanup. This ADR documents the decisions required to complete that cleanup in Phase 2.

### Goals

- Define the canonical prop interface pattern for Hades components going forward (whitelist)
- Specify how `style` is to be removed from `Text` and `StatusDot`
- Define which additional raw HTML elements are blocked and the ESLint rule implementation strategy for each
- Specify how `FormField` auto-wiring is implemented in `Input`, `Textarea`, `NativeSelect`
- Define the canonical `as`/`asChild` rule across all Hades components

### Non-Goals

- New Hades component additions not directly required by the above
- Visual or token design decisions
- Backend architecture changes
- Chart/data-visualisation component strategy

---

## Decision Drivers

* **Escape-hatch elimination**: Every open prop surface or missing ESLint rule is a footgun for both human and AI-authored code. The goal is to make non-compliant patterns structurally impossible, not just discouraged.
* **Consistency with established precedent**: `Stack` and `Icon` already had `style` removed from their public APIs in Phase 1. `Text` and `StatusDot` must follow the same pattern to avoid a two-tier design system where some primitives are hardened and others are not.
* **Inverted default (whitelist over blacklist)**: In a security-oriented system, safe default + explicit opt-in is always preferable to permissive default + opt-out. The prop whitelisting decision applies the same principle to the component API surface.
* **Accessibility correctness**: `FormField` context exists specifically to eliminate the manual `id`/aria attribute threading that developers routinely skip. If the inputs don't consume the context, the feature is documented but non-functional — actively misleading.
* **AI-agent code quality**: The ESLint rule messages are the primary mechanism by which AI agents (Forge, v0.dev) learn what the correct pattern is. A rule that says "don't use `<button>`" with no guidance produces unhelpful output; a rule that says "use `<Button>` from `@orbit/hades`" produces correct first-pass output.
* **Reviewability**: A component `Props` interface with 40 inherited native HTML props is not reviewable in a PR. An explicit 8-prop whitelist is.

---

## Considered Options

### Option A1: Prop interface — Keep blacklisting, extend Omit sets

**Description:** Continue using `Omit<React.HTMLAttributes<HTMLElement>, ...>` but expand the omitted prop list to include any prop that has caused problems (e.g. add `style` to all components).

**Pros:**
- Minimal diff; only the Omit union grows
- No risk of accidentally removing a prop callers rely on

**Cons:**
- The fundamental problem is not solved: the default is still "accept everything except this list". Any new HTML attribute is silently available until a human notices and adds it
- The Omit list across 35 components is already inconsistent; extending it deepens the inconsistency
- There is no way to document intent — it's impossible to tell if a prop is available because it was intentionally supported or because nobody thought to ban it

### Option A2: Prop interface — Explicit whitelisting (chosen)

**Description:** Each Hades component's exported `Props` interface is a handwritten, explicit type that lists only the props the component intentionally accepts. The internal component implementation uses `React.ComponentPropsWithoutRef<'element'>` for DOM forwarding as before; only the public-facing type changes.

**Pros:**
- The public API is self-documenting and reviewable
- The default is "reject everything not explicitly listed" — correct security posture
- TypeScript compile errors catch callers passing unsupported props, including `style` on any component that doesn't explicitly include it
- Consistent pattern across all 35 components, eliminating the current per-component Omit inconsistency

**Cons:**
- Larger one-time migration effort: 35 components × audit + rewrite
- Risk of accidentally removing a prop callers rely on (mitigated by audit step)
- Some props (`className`, `ref`, `children`, `data-*` attributes) need careful handling — they must be explicitly carried forward

**Handling of cross-cutting props under Option A2:**
- `className`: included in internal implementations (needed for `cn()` merging), excluded from public `Props` (already banned by `no-hades-classname` ESLint rule)
- `ref`: handled by `React.forwardRef` wrapper, not part of `Props` interface
- `children`: included explicitly where applicable
- `data-*` attributes: `data-slot` included explicitly for ShadCN sub-component composition; other `data-*` attributes not included unless a component has a specific need
- `id`: included explicitly where semantically meaningful (e.g. `Input`, `Label`)
- `aria-*` attributes: included as `aria-label?: string`, `aria-describedby?: string` etc. individually, or via `Pick<React.AriaAttributes, ...>` where a broad set is appropriate

---

### Option B1: `style` removal — ESLint-only enforcement

**Description:** Add `style` to a new or extended ESLint rule that detects `style` prop usage on Hades components, rather than removing it from the TypeScript interface.

**Pros:**
- No TypeScript interface changes required
- Faster to implement

**Cons:**
- ESLint runs in editors and CI but not in all build paths; TypeScript errors surface at compilation everywhere
- Does not eliminate the runtime footgun — a caller who bypasses ESLint (or uses a non-TS context) can still pass `style`
- Inconsistent with the precedent set by `Stack` and `Icon`, which had `style` removed from their TypeScript interfaces in Phase 1
- A lint rule requires maintenance; a missing prop in the type interface requires zero maintenance

### Option B2: `style` removal — TypeScript interface removal (chosen)

**Description:** Remove `style` from the `Props` interface (or ensure it is not included in the whitelist) for `Text` and `StatusDot`. Replace any call sites that currently use `style` on these components with the appropriate typed prop or layout wrapper.

**Pros:**
- Structurally impossible to pass `style` — TypeScript compile error, not a lint warning
- Consistent with the approach taken for `Stack` and `Icon` in Phase 1
- Zero ongoing maintenance once the interface is changed

**Cons:**
- Requires migrating existing `style` call sites before or simultaneously with the interface change
- Migration must be done carefully — the `terra-style-removal-plan.md` document provides the per-file plan

---

### Option C1: Raw element blocking — Warn mode then error

**Description:** Introduce new ESLint rules in `warn` severity initially, allowing developers to see violations without breaking CI, then promote to `error` after all violations are fixed.

**Pros:**
- Reduces risk of a CI breakage if unknown violations exist
- Allows a grace period for fixing existing code

**Cons:**
- Warnings are ignored in practice — in a codebase where CI enforces zero errors, warnings create noise without urgency
- The established pattern in this codebase is to audit all violations before activating a rule at `error` severity; warn mode is redundant
- Inconsistent with how Phase 1 rules were introduced (audit first, activate at error)

### Option C2: Raw element blocking — Audit first, activate at error (chosen)

**Description:** Before adding any new rule, search the codebase for all existing violations, migrate them, then add the rule at `error` severity. The rule is never in warn mode in a committed state.

**Pros:**
- Consistent with the Phase 1 approach for `no-raw-layout-elements`, `no-raw-inline-elements`, etc.
- CI remains meaningful — zero errors means zero violations, not "zero errors plus N tolerated warnings"
- Forces the migration to happen atomically with the rule addition (no drift period)

**Cons:**
- Requires a complete audit before activation; cannot merge the rule independently of the migration

---

### Option D1: FormField auto-wiring — Compound component with implicit ref

**Description:** Re-architect `FormField` as a compound component that renders a wrapping DOM element and uses `React.useId()` to generate and automatically assign `id` to the enclosed input via DOM manipulation or cloneElement.

**Pros:**
- Completely transparent to callers; no awareness of `useFormField()` needed anywhere

**Cons:**
- Requires a non-trivial architectural change to `FormField`
- `cloneElement` is discouraged in modern React and does not work with Server Components
- DOM manipulation is fragile and not compatible with the Hades `React.forwardRef` convention
- Out of scope for this phase; this is a future architecture consideration

### Option D2: FormField auto-wiring — Context consumer in input components (chosen)

**Description:** `useFormField()` already exists and already provides `id`, `error`, and `disabled` derived from the nearest `FormField` context. The change is to call it inside `Input`, `Textarea`, and `NativeSelect` and merge the returned values into the component's root element props (with caller-provided props taking precedence via spread order).

**Pros:**
- Minimal change; the context and hook already exist and are correct
- Safe defaults when used outside a `FormField` (hook returns undefined/empty string; no behaviour change for existing `Input` usages)
- Consistent with how ShadCN's own form field pattern works

**Cons:**
- Callers who explicitly pass `id` or `aria-*` props must be aware that the `FormField` context will also provide them; the merge order (explicit props override context) must be clearly documented and implemented correctly

---

## Decision

**Chosen options:** A2 (whitelist), B2 (TypeScript removal), C2 (audit-first activation), D2 (context consumer).

### Rationale

All four decisions follow the same underlying principle: make non-compliant usage structurally impossible rather than merely discouraged. ESLint rules can be bypassed, ignored, or misconfigured; TypeScript compile errors and absent props cannot. Where a type-system solution and a lint solution are both available, the type-system solution is preferred.

The whitelist approach (A2) is the natural consequence of applying this principle to the component prop surface. It also resolves the existing inconsistency between components — some of which strip `style`, some of which strip `color`, and at least one (`Code`) which strips nothing — by establishing a single pattern applied uniformly.

The audit-first rule activation pattern (C2) preserves the invariant that CI zero-errors means genuinely zero violations, which is a prerequisite for the design system enforcement story to be credible to AI agents. An agent that generates `<button>` because it saw no lint error in the codebase is a failure of the enforcement system.

The FormField context consumer approach (D2) delivers the documented accessibility guarantee with the minimum possible disruption to existing call sites, which is the appropriate scope for a hardening pass (not a re-architecture pass).

---

## Implementation

### Track A — ESLint rule extensions

#### A1. New rule: `local/no-raw-interactive-elements`

Add to `frontend/eslint.config.mjs` after the existing `no-raw-inline-elements` rule:

```js
const noRawInteractiveElements = {
  create(context) {
    return {
      JSXOpeningElement(node) {
        const name = node.name.name
        if (name === 'button') {
          context.report({
            node,
            message:
              'Use <Button> from @runkora/hades instead of a raw <button>. ' +
              'See frontend/docs/reference/hades-design-system.md.',
          })
        }
      },
    }
  },
}
```

Applied to `src/**/*.tsx` excluding `src/packages/hades/**`. Added to the `no-hades-disable-comment` HADES_RULES set.

**Pre-activation migration:** Search for `<button` in `src/**/*.tsx` (excluding hades). Migrate each occurrence to `<Button>` from `@runkora/hades`. Verify variant mapping (if the raw button used custom classes to achieve a ghost/outline/destructive appearance, map to the appropriate `variant` prop).

#### A2. Extend rule: `local/no-raw-inline-elements`

Add `'code'`, `'kbd'`, `'samp'`, `'var'` to the blocked element set in `no-raw-inline-elements`. Update the error message to include: `"Use <Code> from @runkora/hades for inline code, keyboard input, and sample output."` The `Code` component in Hades accepts a `variant` prop (`"inline"` | `"bare"`) for the two common use cases.

**Pre-activation migration:**
- `vocabulary-tab.tsx`: replace raw `<code>` elements with `<Code variant="inline">` or `<Code variant="bare">` as appropriate
- `setup-tab.tsx`: same migration

#### A3. Update rule: `local/no-raw-layout-elements`

In the `no-raw-layout-elements` rule implementation, the current logic is:

```js
if (SEMANTIC_ELEMENTS.has(name)) {
  const hasLayoutClass = /* check className for LAYOUT_CLASSES */
  if (hasLayoutClass) { context.report(...) }
}
```

Change to: report unconditionally for `section` (and any other semantic element where Hades provides a direct equivalent). Elements like `nav`, `header`, `footer`, `aside`, `article`, `main` which do not yet have Hades wrappers should continue to be blocked only when carrying a layout class (to avoid blocking legitimate semantic markup in edge cases).

The decision on `section` specifically: Hades has a `components/section/` directory. `<section>` without a layout class is still a raw HTML element in product code and must use the Hades wrapper. Block unconditionally.

### Track B — `style` prop removal

Following `terra-style-removal-plan.md`:

**`TextProps` change:**
```ts
// Before
export interface TextProps extends Omit<React.HTMLAttributes<HTMLElement>, 'color'> { ... }

// After — style is not in the explicit whitelist, so it is implicitly absent
export interface TextProps {
  as?: 'span' | 'li' | 'dt' | 'dd' | 'label' | 'legend'
  asChild?: boolean
  variant?: TextVariant
  weight?: TextWeight
  color?: TextColor
  truncate?: boolean
  preserveWhitespace?: boolean
  className?: string   // internal only; kept for cn() but not surfaced to callers
  children?: React.ReactNode
  // explicitly forwarded HTML props:
  id?: string
  htmlFor?: string
  onClick?: React.MouseEventHandler<HTMLElement>
  // aria attributes used by known call sites:
  'aria-label'?: string
  'aria-describedby'?: string
  'aria-hidden'?: boolean | 'true' | 'false'
}
```

The exact set of forwarded HTML props must be determined by the call-site audit. `style` must not appear in the final list.

**`StatusDotProps` change:** Same approach — explicit whitelist, `style` absent.

**Call site migrations** follow the table in `terra-style-removal-plan.md` exactly.

### Track C — Prop interface whitelisting

For each of the 35 Hades components:

1. Read the current `Props` interface and note the Omit set.
2. Search for all usages of the component across `src/**/*.tsx` (excluding hades).
3. Collect the union of all props actually passed by callers.
4. Construct a new explicit `Props` interface containing: (a) all component-specific props, (b) all props from step 3, (c) standard cross-cutting props (`id`, `children`, `className` internal, common aria attributes) as appropriate for the element type.
5. Remove the `Omit<React.HTMLAttributes, ...>` extension.
6. Verify `pnpm tsc --noEmit` passes after each component.

**Canonicalised cross-cutting prop handling:**

| Prop | Rule |
|------|------|
| `className` | Include in internal implementation for `cn()`; **exclude from public Props** (banned by `no-hades-classname`) |
| `style` | **Never include** in public Props |
| `ref` | Handled by `React.forwardRef`; not part of Props interface |
| `children` | Include explicitly where the component accepts children |
| `id` | Include explicitly for components where `id` is semantically meaningful (form inputs, labels) |
| `data-slot` | Include explicitly for components using ShadCN sub-component composition |
| `aria-*` | Include the specific aria attributes that are semantically meaningful for the element's role |
| `onClick`, `onKeyDown`, etc. | Include event handlers that are semantically appropriate for the element type |

### Track D — FormField auto-wiring

**Hook integration pattern** (apply to `Input`, `Textarea`, `NativeSelect`):

```ts
export const Input = React.forwardRef<HTMLInputElement, InputProps>(
  ({ id, 'aria-describedby': ariaDescribedby, 'aria-invalid': ariaInvalid, ...props }, ref) => {
    const field = useFormField()  // returns safe defaults when outside FormField

    return (
      <input
        ref={ref}
        id={id ?? field.id}
        aria-describedby={ariaDescribedby ?? field.descriptionId}
        aria-invalid={ariaInvalid ?? field.invalid}
        {...props}
      />
    )
  }
)
```

The `??` (nullish coalescing) merge order ensures explicitly passed props always win over context values. `useFormField()` must return `undefined` for all values when called outside a `FormField` provider (use a default context value of `{}`).

**Storybook story:** Add a `FormField` + `Input` story under `Hades/FormField` that demonstrates:
- `id` auto-wired from FormField
- `aria-describedby` pointing to the description element
- `aria-invalid` set to `true` when the field has an error
- Storybook a11y addon check must pass

**Call site migrations:**
- `create-token-modal.tsx`: replace raw `<label htmlFor="...">` + `<input id="...">` pairs with `<FormField id="..."><Label /><Input /></FormField>`
- `evaluate-tab.tsx`: same pattern

### Track E — `as`/`asChild` documentation

Add a decision table to `frontend/docs/reference/hades-design-system.md`:

| Use case | Pattern | Example |
|----------|---------|---------|
| Render `Text` as a different phrasing element | `as` prop with string union | `<Text as="span">` |
| Render any Hades component as a custom React component | `asChild` + single child | `<Button asChild><Link href="...">...</Link></Button>` |
| Render a layout element (`Stack`, `Grid`) as a semantic HTML element | `asChild` + semantic element child | `<Stack asChild><nav>...</nav></Stack>` |

**Rule:** No Hades component other than `Text` should expose a string `as` prop. All element-override use cases other than `Text`'s phrasing-content list use `asChild`.

**Audit:** Inspect all Hades components for `as?: ...` props that are not on `Text`. Any found must be converted to `asChild` with migration of call sites.

---

## Consequences

### Positive

- Every Hades component's public API is fully auditable from its `Props` interface alone — no tracing through `Omit` chains.
- `style` becomes structurally impossible to pass to any Hades component — TypeScript compile error at the call site, not a lint warning.
- `<button>`, `<code>`, `<kbd>`, `<samp>`, `<var>`, and unconditional `<section>` are blocked in product code, eliminating the remaining known agent workaround patterns.
- `FormField` delivers its documented accessibility guarantee automatically; callers who follow the documented pattern get correct `id`/aria wiring without manual threading.
- The `as`/`asChild` API is consistent and documented; AI agents generating Hades usage will produce correct first-pass output.

### Negative

- Track C (prop whitelisting) is the largest-effort item: 35 components, each requiring an audit and interface rewrite. The migration is mechanical but time-consuming.
- The whitelist approach requires more deliberate decision-making when adding a new prop to a component — the author must explicitly add it rather than inheriting it. This is the correct behaviour but adds a small authoring overhead.
- Extending `no-raw-inline-elements` and adding `no-raw-interactive-elements` requires upfront call-site migration before the rules can be activated; this is a one-time cost but must not be skipped.

### Neutral

- The `useFormField()` integration changes the `id` and `aria-*` resolution order on `Input`, `Textarea`, and `NativeSelect`. Callers who already pass `id` explicitly are unaffected (explicit props win via `??`). Callers who do not pass `id` and are not inside a `FormField` are also unaffected (`useFormField()` returns `undefined`, which is the same as the current behaviour of no `id`).
- The `as`/`asChild` documentation change affects only documentation and any Hades components found to have an incorrect string `as` prop. Product code using `asChild` is unaffected.

---

## Validation

### Success Metrics

- `pnpm tsc --noEmit` emits zero errors and zero of those errors relate to Hades component prop types
- `pnpm lint` emits zero errors
- `grep -r "Omit<React.HTMLAttributes" frontend/src/packages/hades/src` returns zero matches
- `grep -r 'style=' frontend/src/packages/hades/src/components/typography` returns zero matches in `TextProps`
- Storybook a11y check passes on `FormField` + `Input` story

### Monitoring

- CI enforces `pnpm lint && pnpm tsc --noEmit` on every PR — regressions are caught immediately
- Storybook a11y is run as part of the component review checklist

---

## Related Decisions

- [terra-frontend-design-system](2026-03-25-terra-frontend-design-system.md) — Original Hades design system decision; this ADR extends its enforcement model
- [frontend-component-consolidation](2026-05-04-frontend-component-consolidation.md) — Phase 1 ESLint rule framework and component catalogue expansion; this ADR builds on its enforcement infrastructure

---

## References

- `terra-style-removal-plan.md` (repo root) — Per-file migration plan for `style` prop removal
- `frontend-lint-remediation-progress.md` (repo root) — Phase 1 progress tracker
- `frontend/eslint.config.mjs` — Current ESLint rule implementations
- `frontend/docs/reference/hades-design-system.md` — Hades component usage reference
- `frontend/src/packages/hades/src/components/` — 35 component directories to be migrated

---

## Notes

**Implementation order recommendation:**
1. Start Track A (ESLint) and Track D (FormField) in parallel — both are self-contained and low-risk.
2. Track B (`style` removal) follows naturally once the `terra-style-removal-plan.md` call sites are confirmed.
3. Track C (whitelisting) should be done last — it is the most mechanical and can be reviewed as a single large PR per component family (typography, layout, interactive, feedback, etc.) to keep individual PRs reviewable.

**`Code` component note:** Hades' existing `Code` component renders a `<code>` element. The `no-raw-inline-elements` extension blocks `<code>` in product code but Hades' own implementation is excluded from the rule scope (`src/packages/hades/**` is excluded from all product-code rules). No change to the `Code` component itself is required.

**Last Updated:** 2026-04-24
