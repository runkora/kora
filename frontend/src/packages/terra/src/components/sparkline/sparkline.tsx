import * as React from 'react';
import { cva, type VariantProps } from 'class-variance-authority';
import { cn } from '../../utils';

/** @tier 1 — Zero Orbit domain knowledge. A bare inline bar series for table rows and cards. */

/**
 * Sparkline bar tones.
 *
 * `accent` is the live series; `muted` is the same data drawn as inactive, so a
 * disabled row keeps its shape and its place in the column instead of going
 * blank and breaking the eye's ability to scan down the table.
 */
const barVariants = cva(['flex-1', 'rounded-sm'], {
  variants: {
    tone: {
      accent: 'bg-accent',
      muted: 'bg-surface-overlay',
      error: 'bg-status-error',
    },
  },
  defaultVariants: { tone: 'accent' },
});

export interface SparklineProps
  extends Omit<React.HTMLAttributes<HTMLDivElement>, 'children'>, VariantProps<typeof barVariants> {
  /** Bar values, oldest first. Rendered in order across the width. */
  values: number[];
  /**
   * Height of the plot area in pixels.
   * @default 24
   */
  height?: number;
  /**
   * Minimum bar height as a percentage of the plot area.
   *
   * A zero-value bucket still draws a stub so the series reads as a continuous
   * timeline with quiet periods, rather than as gaps that look like missing
   * data.
   * @default 8
   */
  floorPercent?: number;
  /** Accessible description, e.g. "142 deliveries over 24 hours". */
  label?: string;
}

/**
 * Sparkline
 *
 * A bare bar series for inline trend display inside table rows and cards.
 * Deliberately axis-less, label-less and tooltip-less: at this size the shape
 * is the whole message, and any chrome would cost more space than it earns.
 *
 * For a full chart with axes, scales and tooltips, use `ChartContainer` with a
 * recharts chart instead. This exists because that machinery does not survive
 * being shrunk to a 24px table cell.
 *
 * ```tsx
 * <Sparkline values={[3, 5, 2, 8, 6, 9]} label="Deliveries over 24 hours" />
 * <Sparkline values={buckets} tone="muted" />  // disabled row
 * ```
 */
const Sparkline = React.forwardRef<HTMLDivElement, SparklineProps>(
  ({ values, height = 24, floorPercent = 8, tone, label, className, style, ...props }, ref) => {
    // Scale against the series' own peak so each row is readable on its own
    // terms. A shared scale would flatten every quiet trigger to nothing beside
    // one busy neighbour.
    const peak = Math.max(1, ...values);

    return (
      <div
        ref={ref}
        data-slot="sparkline"
        role="img"
        aria-label={label}
        className={cn('flex items-end gap-[2px]', className)}
        style={{ height, ...style }}
        {...props}
      >
        {values.map((v, i) => (
          <div
            // Bars are fixed positions in a series, so the index is the identity.
            key={i}
            data-slot="sparkline-bar"
            className={cn(barVariants({ tone }))}
            style={{ height: `${Math.max(floorPercent, (v / peak) * 100)}%`, minHeight: 2 }}
          />
        ))}
      </div>
    );
  },
);
Sparkline.displayName = 'Sparkline';

export { Sparkline, barVariants as sparklineBarVariants };
