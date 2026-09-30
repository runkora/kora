# Terra Design System Hardening — Product Requirements Document

**Status:** Implemented
**Created:** 2026-04-23
**Last Updated:** 2026-04-24
**Owner:** Frontend
**Stakeholders:** Engineering

---

## Executive Summary

The first Terra enforcement pass brought the kora frontend to zero lint violations and established nine custom ESLint rules blocking the most common design-system escape hatches. This PRD defines the second phase: closing the remaining gaps in the Terra component API surface, extending ESLint coverage to block all surviving raw HTML workarounds, replacing fragile blacklist-based prop interfaces with explicit whitelisting, and completing accessibility auto-wiring inside Terra form components.

---

## Problem Statement

### Current State

The frontend passes `pnpm lint && pnpm tsc --noEmit` with zero errors. However, a clean lint report is not the same as a fully hardened design system. Seven escape hatches remain open:

**1. `style` prop still exposed on `Text` and `StatusDot`.**
Stack and Icon already had their `style` props removed and replaced with typed layout props. `Text` and `StatusDot` still accept arbitrary inline styles. A concrete remediation plan exists (`terra-style-removal-plan.md`) but the code changes have not been made.

**2. Blacklist-based prop interfaces on all Terra components.**
Every Terra component today extends `Omit<React.HTMLAttributes<HTMLElement>, ...>`, selectively removing known-bad props. This is fragile: any prop not listed in the Omit is silently available. The correct approach is explicit whitelisting — each `Props` interface declares only the props it intentionally supports.

**3. Raw `<button>` elements not blocked.**
The app contains inline `<button>` elements that bypass Terra's `Button` component, producing inconsistent focus styles, hover states, and accessibility attributes. No ESLint rule blocks them.

**4. Raw `<code>`, `<kbd>`, `<samp>`, `<var>` elements not blocked.**
`vocabulary-tab.tsx` and `setup-tab.tsx` contain raw `<code className="font-mono ...">` elements with hand-authored Tailwind classes. Terra's `Code` component covers these cases, but no ESLint rule prevents the pattern from recurring.

**5. `<section>` and other semantic elements only conditionally blocked.**
The current `no-raw-layout-elements` rule fires on semantic elements only when they carry a Tailwind layout class. A bare `<section>` with no class passes silently — an observed agent workaround.

**6. `FormField` context not consumed by Terra input components.**
`FormField` exists as a context provider but `Input`, `Textarea`, and `NativeSelect` do not call `useFormField()`. The intended accessibility auto-wiring (`id`, `aria-describedby`, `aria-invalid`) never materialises. Callers who follow the documented pattern receive no benefit.

**7. Inconsistent `as`/`asChild` across Terra components.**
Some components use `as?: ElementTypeUnion`, some use `asChild?: boolean`, some use both without clear rationale. This causes confusion for human and AI authors alike and produces subtle HTML validity bugs (e.g. a block `<p>` rendered inside a `<button>`).

### User Pain Points

- Developers can still bypass the token system by passing `style={{ flexShrink: 0 }}` to `Text` — exactly the pattern the previous enforcement pass was designed to eliminate.
- AI agents have no lint signal when writing `<button onClick={...}>` or a bare `<section>` — these patterns propagate silently until a human review catches them.
- Accessibility on forms is unreliable: `FormField` context is established but never consumed by inputs. Developers who follow the documented pattern assume auto-wiring is active when it is not.
- Terra component maintainers cannot reason about which native HTML props are intentionally exposed without tracing through `Omit` chains.

### Business Impact

- AI-assisted feature development continues to produce technically non-compliant UI requiring manual remediation passes, negating the productivity gain the design system is meant to deliver.
- Every open escape hatch compounds over time — the longer `style` remains on `Text`, the more call sites accumulate and the larger the future migration.
- Inconsistent `as`/`asChild` patterns increase per-component learning cost and create subtle HTML validity bugs in production.

---

## Goals and Objectives

### Primary Goals

1. Remove `style` from all remaining Terra component public APIs (`Text`, `StatusDot`), completing the work started with Stack and Icon.
2. Replace `Omit<React.HTMLAttributes, ...>` blacklisting with explicit prop whitelisting on every Terra component.
3. Extend ESLint to block raw `<button>`, `<code>`, `<kbd>`, `<samp>`, `<var>`, and unconditional `<section>`, with error messages naming the correct Terra replacement.
4. Wire `useFormField()` into `Input`, `Textarea`, and `NativeSelect` so `FormField` delivers its documented accessibility guarantee automatically.
5. Standardise `as`/`asChild` across all Terra components: `as` is restricted to phrasing-content overrides on `Text`; all other element-override cases use `asChild` only.

### Success Metrics

- Zero Terra component `Props` interfaces expose the `style` prop.
- Zero Terra components use `Omit<React.HTMLAttributes>` in their exported type — all accepted props are explicitly declared.
- ESLint blocks raw `<button>`, `<code>`/`<kbd>`/`<samp>`/`<var>`, and unconditional `<section>` in all product code.
- `FormField` + `Input` integration: placing `<Input>` inside `<FormField id="x">` automatically sets `id="x"` on the input without manual prop threading — verified by a Storybook a11y check.
- `pnpm lint && pnpm tsc --noEmit` continues to pass with zero errors after every track is landed.

### Non-Goals

- New visual design changes or new colour tokens.
- New Terra components beyond those strictly required to replace newly blocked raw elements.
- Redesigning the form composition model — only wiring the existing `useFormField()` hook.
- Chart or data-visualisation component wrappers.
- Mobile-responsive layout changes.

---

## User Stories

### Primary User Stories

**As a** developer building a new feature tab,
**I want** any raw `<button>` I write to immediately fail lint with a message pointing to `<Button>`,
**So that** I cannot accidentally introduce inconsistent interaction patterns.

**Acceptance Criteria:**
- [ ] `local/no-raw-interactive-elements` rule blocks `<button>` in all `src/**/*.tsx` except `src/packages/terra/**`
- [ ] Error message names `<Button>` from `@orbit/terra` and links to `terra-design-system.md`
- [ ] All existing raw `<button>` usages in the app migrated to `<Button>` before rule activation

---

**As a** developer using `<FormField>` to build an accessible form,
**I want** `id`, `aria-describedby`, and `aria-invalid` to be set on the enclosed `<Input>` automatically,
**So that** I cannot accidentally omit them and produce an inaccessible form.

**Acceptance Criteria:**
- [ ] `Input`, `Textarea`, and `NativeSelect` call `useFormField()` and merge the returned context into their root element
- [ ] A `FormField` + `Input` Storybook story passes the a11y addon check
- [ ] `create-token-modal.tsx` and `evaluate-tab.tsx` migrated from raw `<label>` + `<input>` to `<FormField>` + `<Input>`

---

**As a** Terra component author reviewing a PR,
**I want** to see every accepted prop listed explicitly in the component's `Props` interface,
**So that** I can audit the public API without tracing through `Omit` chains.

**Acceptance Criteria:**
- [ ] Every exported Terra `Props` interface is a flat, explicit type — no `Omit<React.HTMLAttributes, ...>`
- [ ] TypeScript reports a compile error if a caller passes `style` to any Terra component
- [ ] Internal DOM forwarding uses `React.ComponentPropsWithoutRef<'element'>` scoped to the correct element

### Secondary User Stories

**As an** AI coding agent generating product UI,
**I want** lint rules that name the correct Terra replacement whenever I use a blocked raw HTML element,
**So that** my first-pass output is compliant and requires minimal human correction.

---

## Proposed Solution

### High-Level Approach

Work is divided into four tracks. Tracks A and B can begin in parallel. Track C should follow Track B. Track D is fully independent.

**Track A — ESLint coverage gaps**
Add `local/no-raw-interactive-elements` blocking `<button>`. Extend `local/no-raw-inline-elements` to cover `<code>`, `<kbd>`, `<samp>`, `<var>`. Update `local/no-raw-layout-elements` to block `<section>` unconditionally. All existing violations must be migrated before each sub-rule is activated.

**Track B — `style` prop removal**
Remove `style` from `TextProps` and `StatusDotProps` following the per-file plan in `terra-style-removal-plan.md`. This is type-system enforcement — callers get a TypeScript compile error, not just a lint warning.

**Track C — Prop interface whitelisting**
Audit all 35 Terra components. Replace every `Omit<React.HTMLAttributes<...>, ...>` in a public `Props` interface with explicit prop declarations. Internal implementations continue to forward to the DOM using `React.ComponentPropsWithoutRef` scoped to the correct element type.

**Track D — FormField auto-wiring**
Wire `useFormField()` into `Input`, `Textarea`, and `NativeSelect`. Update the `FormField` Storybook story. Migrate `create-token-modal.tsx` and `evaluate-tab.tsx`.

### Key Components

- **`frontend/eslint.config.mjs`** — new `no-raw-interactive-elements` rule; `no-raw-inline-elements` extended; `no-raw-layout-elements` section-handling updated; `no-terra-disable-comment` TERRA_RULES set extended.
- **`typography.tsx`** — `TextProps` updated; all 9 affected call sites migrated per `terra-style-removal-plan.md`.
- **`status-dot.tsx`** — `StatusDotProps` updated; `dashboard-header.tsx` migrated to `<Indicator>`.
- All 35 components under `src/packages/terra/src/components/` — `Props` interfaces converted from blacklist to whitelist.
- **`input.tsx`**, **`textarea.tsx`**, **`native-select.tsx`** — each wired to `useFormField()`.
- **`vocabulary-tab.tsx`**, **`setup-tab.tsx`** — raw `<code>` elements migrated to `<Code>`.

---

## Scope

### In Scope

- Removing `style` from `Text` and `StatusDot` and migrating all affected call sites per `terra-style-removal-plan.md`
- Whitelisting props on all 35 Terra components
- ESLint rules blocking `<button>`, `<code>`/`<kbd>`/`<samp>`/`<var>`, and unconditional `<section>`
- Migrating all existing violations before activating each new rule
- `useFormField()` wired into `Input`, `Textarea`, `NativeSelect`
- Migrating `create-token-modal.tsx` and `evaluate-tab.tsx` to `<FormField>` + `<Input>`
- `as`/`asChild` convention documented in `terra-design-system.md`

### Out of Scope

- New Terra components beyond those needed to replace newly blocked elements
- Visual or token changes
- Internal `className` usage inside Terra implementations (only public APIs are in scope)
- Animation or motion primitives
- Backend changes

### Future Considerations

- A custom ESLint rule detecting `Omit<React.HTMLAttributes>` in Terra source files, enforcing the whitelist pattern at authoring time
- Storybook interaction tests (beyond a11y snapshots) for `FormField` auto-wiring
- A Terra authoring linter enforcing `data-slot`, `React.forwardRef`, and `cva` conventions on new component PRs

---

## Requirements

### Functional Requirements

**Must Have (P0):**
- [ ] `style` prop removed from `TextProps`; all 9 affected call sites in `overview-tab.tsx`, `models-tab.tsx`, and `token-created-modal.tsx` migrated
- [ ] `style` prop removed from `StatusDotProps`; `dashboard-header.tsx` migrated to `<Indicator>`
- [ ] `local/no-raw-interactive-elements` ESLint rule blocks `<button>` in product code; existing violations migrated before activation
- [ ] `local/no-raw-inline-elements` extended to block `<code>`/`<kbd>`/`<samp>`/`<var>`; `vocabulary-tab.tsx` and `setup-tab.tsx` migrated to `<Code>` before activation
- [ ] `local/no-raw-layout-elements` updated to block `<section>` unconditionally (not only when a layout class is present)
- [ ] Every Terra component exported `Props` interface contains only explicitly declared props — no `Omit<React.HTMLAttributes, ...>`
- [ ] `Input`, `Textarea`, `NativeSelect` auto-wire `id`, `aria-describedby`, `aria-invalid` from `FormField` context

**Should Have (P1):**
- [ ] `FormField` + `Input` Storybook story added or updated with a11y check passing
- [ ] `create-token-modal.tsx` migrated to `<FormField>` + `<Input>`
- [ ] `evaluate-tab.tsx` migrated to `<FormField>` + `<Input>`
- [ ] `as`/`asChild` convention documented in `terra-design-system.md` with a clear decision rule
- [ ] `no-terra-disable-comment` TERRA_RULES set updated to include `local/no-raw-interactive-elements`

**Nice to Have (P2):**
- [ ] Storybook interaction test for `FormField` accessibility auto-wiring
- [ ] ESLint rule detecting `Omit<React.HTMLAttributes>` in Terra source files to enforce whitelist at authoring time

### Non-Functional Requirements

**Developer Experience:**
Every new ESLint error message must name the exact Terra replacement and link to `terra-design-system.md` — zero ambiguous "don't use this" messages without a "use this instead" counterpart.

**Type Safety:**
Passing `style` to any Terra component must produce a TypeScript compile error, not a lint warning.

**Backwards Compatibility:**
The prop whitelisting refactor must not introduce TypeScript errors in existing app code — any prop currently accepted and used must remain accepted after the whitelist migration.

---

## Dependencies

### Internal Dependencies

- `terra-style-removal-plan.md` (repo root) — per-file migration guide for Track B; exists
- `Indicator` component — already created in Terra; required for the `StatusDot` `style` removal
- `Code` component — already exists in Terra (`typography.tsx`); required for `<code>` migration
- `Button` component — already exists in Terra; required for `<button>` migration
- `FormField` / `useFormField()` hook — already exists; Track D wires existing inputs into it

### External Dependencies

None. All work is contained within the frontend monorepo package.

---

## Risks and Mitigations

| Risk | Impact | Probability | Mitigation |
|------|--------|-------------|------------|
| Prop whitelisting reveals app code relied on native HTML props not in the explicit whitelist | Medium | Low | Audit call sites before removing props; add any intentionally-used native props explicitly to the whitelist |
| `FormField` auto-wiring changes `Input` id/aria behaviour in existing usages outside `FormField` (id/aria-* should be no-ops when context is absent) | Medium | Low | `useFormField()` must return safe defaults (empty string / undefined) when called outside a `FormField` provider |
| Extending `no-raw-inline-elements` reveals more `<code>` violations than currently identified | Low | Medium | Run lint with the rule in warn mode first; audit all violations before switching to error |
| `<section>` unconditional block may affect Terra's own internal `Section` component implementation | Low | Low | Rule already excludes `src/packages/terra/**`; verify exclusion covers `section/` component dir |

---

## Alternatives Considered

### Alternative: Lint-only enforcement (no type-system changes)

**Description:** Rely entirely on ESLint to block `style` on Terra components rather than removing it from the TypeScript `Props` interface.

**Why not chosen:**
- ESLint runs only in editors and CI; TypeScript errors surface at compile time and in IDEs without any ESLint configuration.
- The goal is to make misuse _impossible_, not just _warned about_. Type-system enforcement is stronger and requires zero ESLint rule maintenance.
- The `style` removal from `Stack` and `Icon` in the prior session set the precedent of type-system enforcement; this change makes the approach consistent.

### Alternative: Keep `Omit<React.HTMLAttributes>` but add more props to the Omit list

**Description:** Expand the blacklist rather than switching to whitelisting.

**Why not chosen:**
- Every new problematic HTML attribute requires a human to notice it and add it to every Terra component's `Omit` list — an indefinitely growing maintenance burden.
- The whitelist approach inverts the default: new HTML attributes are rejected unless explicitly approved, which is the correct security posture for a design system trying to prevent escape hatches.
- The blacklist is already inconsistent across components (some `Omit` `style`, some omit `color`, some omit both, `Code` omits nothing).

---

## Open Questions

1. **FormField default behaviour when used without a provider**
   - **Status:** Open
   - When `useFormField()` is called inside an `Input` that is not wrapped in a `FormField`, should it return safe no-op defaults or throw? The safest approach is safe defaults (undefined for all context values) so existing `Input` usages outside `FormField` continue to work unchanged.

2. **`as` prop on components other than `Text`**
   - **Status:** Open
   - The `as` prop audit may reveal that some Terra components legitimately need to render as different elements (e.g. `Heading` rendering as `div` for display contexts). The ADR should specify whether these use `as` or `asChild` and document the decision rule.

---

## Timeline and Milestones

### Phases

**Track A — ESLint (can run first):** 2–3 days
- Audit all existing `<button>`, `<code>`, `<section>` violations
- Migrate violations
- Implement and activate new/extended rules

**Track B — `style` prop removal:** 1–2 days
- Follows `terra-style-removal-plan.md` directly; largely mechanical

**Track C — Prop interface whitelisting:** 3–5 days
- 35 components × audit + refactor; mostly mechanical but requires per-component verification that no existing callers break

**Track D — FormField auto-wiring:** 2–3 days
- Hook wiring + Storybook + 2 call site migrations

### Key Milestones

- [ ] PRD Approved
- [ ] [frontend-component-consolidation](../adr/2026-05-04-frontend-component-consolidation.md) Created and Accepted
- [ ] Track A complete (ESLint extended, violations migrated)
- [ ] Track B complete (style removed from Text + StatusDot)
- [ ] Track C complete (all 35 components whitelisted)
- [ ] Track D complete (FormField auto-wiring live)
- [ ] Full `pnpm lint && pnpm tsc --noEmit` green

---

## Appendix

### Related Documents

- [terra-design-system-hardening](../adr/2026-05-04-terra-design-system-hardening.md) — Architecture decision for this work
- [terra-frontend-design-system](../adr/2026-03-25-terra-frontend-design-system.md) — Original Terra design system decision
- [frontend-component-consolidation](../adr/2026-05-04-frontend-component-consolidation.md) — Component consolidation (Phase 0 + component hierarchy)
- `terra-style-removal-plan.md` (repo root) — Per-file migration guide for `style` prop removal
- `frontend-lint-remediation-progress.md` (repo root) — Phase 1 progress tracker (completed)
- `frontend/docs/golden-paths/terra-design-system.md` — Terra component usage reference

### Glossary

- **Blacklist approach:** `Omit<React.HTMLAttributes<HTMLElement>, 'style' | 'color'>` — accepts all native props except the listed ones
- **Whitelist approach:** Explicit `Props` interface listing only the props the component intentionally supports
- **`asChild`:** Radix UI Slot pattern — the component renders its single child element instead of its own root element, forwarding all props
- **`as` prop:** A string enum prop that changes which HTML element the component renders as (e.g. `as="span"` on `Text`)
- **FormField auto-wiring:** `useFormField()` hook reads `id`, `error`, and `disabled` from `FormField` context and returns them ready to spread onto an input element
