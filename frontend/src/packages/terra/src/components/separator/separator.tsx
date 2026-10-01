import * as React from 'react';
import * as SeparatorPrimitive from '@radix-ui/react-separator';
import { cva, type VariantProps } from 'class-variance-authority';

const separatorVariants = cva('shrink-0 bg-border-subtle', {
  variants: {
    orientation: {
      horizontal: 'h-px w-full',
      vertical: 'w-px h-full',
    },
  },
  defaultVariants: {
    orientation: 'horizontal',
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
 * competing with content — consistent with ADR SD-3 (no shadow tokens).
 *
 * ```tsx
 * <Separator />
 * <Separator orientation="vertical" />
 * ```
 */
const Separator = React.forwardRef<
  React.ElementRef<typeof SeparatorPrimitive.Root>,
  SeparatorProps
>(({ orientation = 'horizontal', decorative = true, ...props }, ref) => (
  <SeparatorPrimitive.Root
    ref={ref}
    decorative={decorative}
    orientation={orientation}
    className={separatorVariants({ orientation })}
    {...props}
  />
));

Separator.displayName = SeparatorPrimitive.Root.displayName;

export { Separator, separatorVariants };
