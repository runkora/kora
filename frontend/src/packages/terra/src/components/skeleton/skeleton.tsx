import * as React from 'react';
import { cva, type VariantProps } from 'class-variance-authority';

const skeletonVariants = cva('animate-pulse bg-surface-elevated', {
  variants: {
    /**
     * Controls border-radius and aspect behaviour.
     *
     * - `line`   — single-row text placeholder (`rounded-sm`)
     * - `block`  — rectangular area placeholder (`rounded-md`)
     * - `circle` — avatar / icon placeholder (`rounded-full` + `aspect-square`)
     *              Width is determined by `size` alone; the `width` prop is ignored.
     */
    shape: {
      line: 'rounded-sm',
      block: 'rounded-md',
      circle: 'rounded-full aspect-square',
    },
    /**
     * Fraction of the parent container width (ignored when `shape="circle"`).
     */
    width: {
      quarter: 'w-1/4',
      third: 'w-1/3',
      half: 'w-1/2',
      'two-thirds': 'w-2/3',
      'three-quarters': 'w-3/4',
      full: 'w-full',
    },
    /**
     * Height token (also controls the diameter when `shape="circle"`).
     *
     * xs → h-3  (12 px) — fine text / captions
     * sm → h-4  (16 px) — body text
     * md → h-6  (24 px) — subheadings / inputs
     * lg → h-12 (48 px) — medium avatars
     * xl → h-16 (64 px) — large avatars / hero areas
     */
    size: {
      xs: 'h-3',
      sm: 'h-4',
      md: 'h-6',
      lg: 'h-12',
      xl: 'h-16',
    },
  },
  defaultVariants: {
    shape: 'line',
    width: 'full',
    size: 'sm',
  },
});

/**
 * Skeleton
 *
 * A loading placeholder that communicates shape and layout while content is
 * being fetched. Uses a subtle pulse animation against the surface token so it
 * reads as "loading" without jarring contrast shifts.
 *
 * Size and shape are expressed through first-class props backed by design
 * tokens — `className` is not accepted.
 *
 * ```tsx
 * // Body-text line, half the container width
 * <Skeleton shape="line" width="half" size="sm" />
 *
 * // Medium avatar circle
 * <Skeleton shape="circle" size="lg" />
 *
 * // Card image placeholder
 * <Skeleton shape="block" width="full" size="xl" />
 * ```
 */
const Skeleton = React.forwardRef<
  HTMLDivElement,
  Omit<React.HTMLAttributes<HTMLDivElement>, 'className'> & VariantProps<typeof skeletonVariants>
>(({ shape = 'line', width = 'full', size = 'sm', ...props }, ref) => (
  <div
    ref={ref}
    // When shape is 'circle', width is intentionally omitted so that
    // aspect-square + the size height token control the diameter cleanly.
    className={skeletonVariants({
      shape,
      width: shape === 'circle' ? undefined : width,
      size,
    })}
    {...props}
  />
));

Skeleton.displayName = 'Skeleton';

export { Skeleton };
export type { VariantProps as SkeletonVariantProps };
