import * as React from 'react';
import * as SeparatorPrimitive from '@radix-ui/react-separator';
import { cva, type VariantProps } from 'class-variance-authority';

const separatorVariants = cva('shrink-0', {
  variants: {
    orientation: {
      horizontal: 'h-px w-full',
      vertical: 'w-px h-full',
    },
    /** `strong` uses the standard border colour, for dividers that must stay visible beside bordered controls. */
    tone: {
      default: 'bg-border-subtle',
      strong: 'bg-border',
    },
  },
  defaultVariants: {
    orientation: 'horizontal',
    tone: 'default',
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
 * Always spans 100% of the parent container along its axis (width when
 * horizontal, height when vertical).
 *
 * Uses `bg-border-subtle` so it reads as a structural boundary without
 * competing with content — consistent with ADR SD-3 (no shadow tokens). Set
 * `tone="strong"` for the standard border colour where the subtle line gets
 * lost, e.g. between rows of bordered controls.
 *
 * ```tsx
 * <Separator />
 * <Separator orientation="vertical" />
 * <Separator tone="strong" />
 * ```
 */
const Separator = React.forwardRef<
  React.ElementRef<typeof SeparatorPrimitive.Root>,
  SeparatorProps
>(({ orientation = 'horizontal', tone = 'default', decorative = true, ...props }, ref) => (
  <SeparatorPrimitive.Root
    ref={ref}
    decorative={decorative}
    orientation={orientation}
    className={separatorVariants({ orientation, tone })}
    {...props}
  />
));

Separator.displayName = SeparatorPrimitive.Root.displayName;

export { Separator, separatorVariants };
