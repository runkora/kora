import * as React from 'react';
import * as SeparatorPrimitive from '@radix-ui/react-separator';
import { cva, type VariantProps } from 'class-variance-authority';

const separatorVariants = cva('shrink-0 bg-border-subtle', {
  variants: {
    orientation: {
      horizontal: 'h-px',
      vertical: 'w-px',
    },
    /**
     * How much of the parent container the separator spans.
     *
     * - `full`   — 100% (default)
     * - `wide`   — 80%
     * - `narrow` — 40%
     * - `slim`   — 20%
     *
     * Horizontal separators use width fractions; vertical separators use
     * height fractions. Non-full spans are centered with `mx-auto` /
     * `my-auto` so the line stays visually balanced within its container.
     */
    span: {
      full: '',
      wide: '',
      narrow: '',
      slim: '',
    },
  },
  compoundVariants: [
    // Horizontal spans
    { orientation: 'horizontal', span: 'full', class: 'w-full' },
    { orientation: 'horizontal', span: 'wide', class: 'w-4/5 mx-auto' },
    { orientation: 'horizontal', span: 'narrow', class: 'w-2/5 mx-auto' },
    { orientation: 'horizontal', span: 'slim', class: 'w-1/5 mx-auto' },
    // Vertical spans
    { orientation: 'vertical', span: 'full', class: 'h-full' },
    { orientation: 'vertical', span: 'wide', class: 'h-4/5 my-auto' },
    { orientation: 'vertical', span: 'narrow', class: 'h-2/5 my-auto' },
    { orientation: 'vertical', span: 'slim', class: 'h-1/5 my-auto' },
  ],
  defaultVariants: {
    orientation: 'horizontal',
    span: 'wide',
  },
});

export type SeparatorProps = Omit<
  React.ComponentPropsWithoutRef<typeof SeparatorPrimitive.Root>,
  'className'
> &
  VariantProps<typeof separatorVariants>;

/**
 * Separator
 *
 * A thin ruled line for visually dividing sections of content. Built on the
 * Radix UI Separator primitive for correct `role="separator"` semantics and
 * `aria-orientation` support.
 *
 * Use the `span` prop to control what fraction of the parent container the
 * line occupies. Sizing is always proportional — expressed through design
 * tokens, not raw `className`.
 *
 * Uses `bg-border-subtle` so it reads as a structural boundary without
 * competing with content — consistent with ADR SD-3 (no shadow tokens).
 *
 * ```tsx
 * <Separator />
 * <Separator span="wide" />
 * <Separator orientation="vertical" span="narrow" />
 * ```
 */
const Separator = React.forwardRef<
  React.ElementRef<typeof SeparatorPrimitive.Root>,
  SeparatorProps
>(({ orientation = 'horizontal', span = 'wide', decorative = true, ...props }, ref) => (
  <SeparatorPrimitive.Root
    ref={ref}
    decorative={decorative}
    orientation={orientation}
    className={separatorVariants({ orientation, span })}
    {...props}
  />
));

Separator.displayName = SeparatorPrimitive.Root.displayName;

export { Separator, separatorVariants };
