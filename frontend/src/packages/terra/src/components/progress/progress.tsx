import * as React from 'react';
import { cva, type VariantProps } from 'class-variance-authority';
import { cn } from '../../utils';

/**
 * Progress bar variants — maps to Terra's status token set so the fill
 * colour communicates operational meaning without additional labelling.
 */
const progressVariants = cva(
  // Fill bar — absolutely positioned inside the track
  'h-full rounded-full transition-[width] duration-500 ease-out',
  {
    variants: {
      variant: {
        /** Default — accent colour (e.g. general resource utilisation). */
        default: 'bg-accent',

        /** Healthy — green; use when value is within safe operating range. */
        healthy: 'bg-status-healthy',

        /** Degraded — amber; use when approaching a threshold. */
        degraded: 'bg-status-degraded',

        /** Error — red; use when a limit is exceeded or a fault exists. */
        error: 'bg-status-error',

        /** Pending — accent-pending; use for in-progress operations. */
        pending: 'bg-status-pending',
      },
      size: {
        /** Compact indicator — useful inside table cells or card footers. */
        sm: 'h-1',
        /** Standard bar — default for dashboard metric cards. */
        md: 'h-1.5',
        /** Prominent bar — suitable for full-width resource panels. */
        lg: 'h-2.5',
      },
    },
    defaultVariants: {
      variant: 'default',
      size: 'md',
    },
  },
);

export interface ProgressProps
  extends React.HTMLAttributes<HTMLDivElement>, VariantProps<typeof progressVariants> {
  /**
   * Current value — must be in the range [0, max]. Values outside the range
   * are clamped before rendering.
   */
  value: number;

  /**
   * Maximum value. Defaults to 100 so `value` can be treated as a percentage
   * directly.
   */
  max?: number;

  /**
   * Accessible label describing what the progress bar represents.
   * Required for screen readers — the numeric value alone is insufficient.
   *
   * ```tsx
   * <Progress label="CPU utilisation" value={72} />
   * ```
   */
  label: string;

  /**
   * When true, renders a small numeric percentage to the right of the track.
   * Useful when the bar is the primary data representation in a card.
   */
  showValue?: boolean;
}

/**
 * Progress
 *
 * A linear progress / utilisation bar that maps value → fill width with a
 * smooth CSS transition. The five colour variants match Terra's status token
 * set so the bar's colour communicates operational state at a glance.
 *
 * Always supply a meaningful `label` — it is used as `aria-label` on the
 * ARIA progressbar role.
 *
 * ```tsx
 * <Progress label="CPU utilisation" value={72} variant="degraded" showValue />
 * <Progress label="Memory" value={45} />
 * <Progress label="Disk" value={91} variant="error" size="lg" showValue />
 * ```
 */
const Progress = React.forwardRef<HTMLDivElement, ProgressProps>(
  ({ className, variant, size, value, max = 100, label, showValue = false, ...props }, ref) => {
    // Guard against max ≤ 0 (would produce Infinity/NaN in pct calculation and
    // invalid ARIA values). Fall back to the prop default of 100.
    const safeMax = max > 0 ? max : 100;
    const clamped = Math.min(Math.max(value, 0), safeMax);
    const pct = (clamped / safeMax) * 100;

    return (
      <div ref={ref} className={cn('flex items-center gap-3', className)} {...props}>
        {/* Track */}
        <div
          role="progressbar"
          aria-label={label}
          aria-valuenow={clamped}
          aria-valuemin={0}
          aria-valuemax={safeMax}
          className={cn(
            'relative flex-1 overflow-hidden rounded-full bg-surface-overlay',
            // Track height mirrors fill height
            size === 'sm' ? 'h-1' : size === 'lg' ? 'h-2.5' : 'h-1.5',
          )}
        >
          <div className={cn(progressVariants({ variant, size }))} style={{ width: `${pct}%` }} />
        </div>

        {/* Optional numeric label */}
        {showValue && (
          <span className="shrink-0 w-9 text-right text-xs tabular-nums text-muted">
            {Math.round(pct)}%
          </span>
        )}
      </div>
    );
  },
);

Progress.displayName = 'Progress';

export { Progress, progressVariants };
