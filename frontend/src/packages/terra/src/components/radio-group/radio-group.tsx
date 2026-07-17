'use client';

import * as React from 'react';
import * as RadioGroupPrimitive from '@radix-ui/react-radio-group';
import { cn } from '../../utils';

/**
 * RadioGroup
 *
 * A single-select control group built on the Radix UI RadioGroup primitive
 * for correct `role="radiogroup"` / `role="radio"` semantics and full
 * keyboard navigation (arrow keys move selection, Tab enters/leaves the
 * group).
 *
 * `RadioGroupItem` renders only the circular control — 18 px, accent-filled
 * when checked with an accent-foreground dot. Compose it with `Label`,
 * `Text`, or a `Card selected` surface for richer selection rows; the item
 * stays minimal by design.
 *
 * Pass `defaultValue` for uncontrolled usage or `value` + `onValueChange`
 * for controlled usage.
 *
 * ```tsx
 * <RadioGroup value={framework} onValueChange={setFramework}>
 *   <Label>
 *     <RadioGroupItem value="claude-code" /> Claude Code
 *   </Label>
 *   <Label>
 *     <RadioGroupItem value="codex" /> Codex
 *   </Label>
 * </RadioGroup>
 * ```
 */
const RadioGroup = React.forwardRef<
  React.ComponentRef<typeof RadioGroupPrimitive.Root>,
  React.ComponentPropsWithoutRef<typeof RadioGroupPrimitive.Root>
>(({ className, ...props }, ref) => (
  <RadioGroupPrimitive.Root
    ref={ref}
    data-slot="radio-group"
    className={cn('grid gap-2.5', className)}
    {...props}
  />
));
RadioGroup.displayName = 'RadioGroup';

/**
 * RadioGroupItem
 *
 * A single radio circle inside a `RadioGroup`. Renders the 18 px control
 * only — no label — so it composes into labelled rows and card selection
 * surfaces without fighting a built-in layout.
 *
 * Visual states:
 *   - Unchecked — transparent fill, `border-border` ring
 *   - Checked   — accent fill and border with an accent-foreground dot
 *   - Disabled  — 50 % opacity, pointer events off
 */
const RadioGroupItem = React.forwardRef<
  React.ComponentRef<typeof RadioGroupPrimitive.Item>,
  React.ComponentPropsWithoutRef<typeof RadioGroupPrimitive.Item>
>(({ className, ...props }, ref) => (
  <RadioGroupPrimitive.Item
    ref={ref}
    data-slot="radio-group-item"
    className={cn(
      // Shape
      'inline-flex size-[18px] shrink-0 items-center justify-center rounded-full',
      // Unchecked ring
      'border-[1.5px] border-border bg-transparent',
      // Checked fill
      'data-[state=checked]:border-accent data-[state=checked]:bg-accent',
      // Transition + focus ring (house idiom)
      'transition-colors focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent',
      // Disabled
      'disabled:pointer-events-none disabled:opacity-50',
      className,
    )}
    {...props}
  >
    <RadioGroupPrimitive.Indicator
      data-slot="radio-group-indicator"
      className="flex items-center justify-center"
    >
      <span aria-hidden className="size-[7px] rounded-full bg-accent-foreground" />
    </RadioGroupPrimitive.Indicator>
  </RadioGroupPrimitive.Item>
));
RadioGroupItem.displayName = 'RadioGroupItem';

export type RadioGroupProps = React.ComponentPropsWithoutRef<typeof RadioGroupPrimitive.Root>;
export type RadioGroupItemProps = React.ComponentPropsWithoutRef<typeof RadioGroupPrimitive.Item>;

export { RadioGroup, RadioGroupItem };
