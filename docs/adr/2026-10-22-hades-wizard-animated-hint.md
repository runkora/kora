# Hades Wizard Animated Hint Banner

**Status:** Proposed
**Date:** 2026-10-22
**Deciders:** Hades maintainers
**Technical Story:** [#722](https://github.com/runkora/kora/issues/722)

---

## Context

The Hades `Wizard` renders `hint` as a banner above the step content only while a hint is set. When a step turns valid or invalid, the banner mounts or unmounts and the content jumps by about 38px. A quick second click can land on the wrong control. Every wizard that passes `hint` has this: the trigger wizard, the environment wizard, the agent builder and the create-grant wizard.

### Goals

- Stop the content from jumping when the hint appears or clears.
- Fix it once in Hades, with no change at the call sites.

### Non-Goals

- Handling hints that wrap to more than one line.
- Changing the hint text, variants or colours.

---

## Considered Options

### Option 1: Opt-in `reserveHintSpace` prop

**Description:** When set and no hint is present, render an invisible, `aria-hidden` banner of the same height.

**Pros:**

- The content never moves.

**Cons:**

- Every wizard that passes `hint` has the problem, so every wizard would set it. The prop adds API for nothing.
- Reserves an empty strip of about 38px when there is no hint.

### Option 2: Overlay banner

**Description:** Position the banner over the top of the content so it takes no layout space.

**Pros:**

- The content never moves.

**Cons:**

- The banner covers the top of the content. Step bodies set their own top padding, so each wizard would need adjusting.
- Over a scrolled step it covers whatever has scrolled under it.

### Option 3: Animate the banner in place

**Description:** Keep the banner in the layout and animate its height, rolling down when a hint is set and up when it clears. Hades already has this animation in `CollapsibleContent` (200ms, `--ease-standard`, honours `prefers-reduced-motion`).

**Pros:**

- Nothing is covered and nothing is reserved.
- No new API, tokens or keyframes, and no change at the call sites.
- The motion explains where the content went.

**Cons:**

- The content still moves, over about 200ms instead of in one frame. A click during that window can still land on a moved control.

---

## Decision

**Chosen Option:** Option 3 - Animate the banner in place

### Rationale

Reserving space and overlaying both cost something visible on every wizard: an empty strip, or covered content. The animation costs neither, reuses what Hades has, and turns an abrupt jump into a motion the eye follows. The remaining shift is short and predictable. It does not fully remove the click risk, and we accept that.

The hint is already `undefined` when it clears, so the banner keeps the last hint and variant while it rolls up.

---

## Consequences

### Positive

- Every wizard gets the animation with no call-site change.
- The banner stays the same size and colour.

### Negative

- Content still moves for about 200ms when the hint changes.
- A hint that wraps to two lines grows the banner further.

### Neutral

- With `prefers-reduced-motion` the banner appears and disappears instantly, as before.

---

## Related Decisions

- [Hades Wizard Component](2026-08-03-terra-wizard-component.md)

**Last Updated:** 2026-10-22
