import * as React from 'react';
import { cn } from '../../utils';

/** @tier 1 — Zero Orbit domain knowledge. A ring split into proportional segments. */

export interface DonutSegment {
  label: string;
  value: number;
}

/**
 * Segments are one hue at falling strength, so the order of the parts carries
 * no good-or-bad meaning. A legend reads its dot colours from the same function.
 */
const STRENGTHS = [100, 72, 52, 38, 26, 16];

export function donutSegmentColor(index: number): string {
  const strength = STRENGTHS[index] ?? STRENGTHS[STRENGTHS.length - 1];
  return `color-mix(in oklch, var(--color-accent) ${strength}%, transparent)`;
}

/** Space left between neighbouring segments so each reads as its own part. */
const SEGMENT_GAP = 2;

export interface DonutProps extends Omit<React.HTMLAttributes<HTMLDivElement>, 'children'> {
  /** The parts of the whole, largest first. Zero values draw nothing. */
  segments: DonutSegment[];
  /**
   * Outer diameter in pixels.
   * @default 76
   */
  size?: number;
  /**
   * Ring thickness in pixels.
   * @default 7
   */
  thickness?: number;
  /** Content centred inside the ring, usually the total. */
  children?: React.ReactNode;
}

/**
 * Donut
 *
 * A ring divided into proportional segments, with room in the middle for the
 * total. Use it for a few parts of one whole; past about five parts the
 * segments get too thin to compare. Pair it with a legend that carries the
 * exact numbers.
 *
 * ```tsx
 * <Donut segments={[{ label: 'Custom', value: 3 }, { label: 'Reserved', value: 1 }]}>
 *   4
 * </Donut>
 * ```
 */
const Donut = React.forwardRef<HTMLDivElement, DonutProps>(
  ({ segments, size = 76, thickness = 7, children, className, style, ...props }, ref) => {
    const radius = (size - thickness) / 2;
    const circumference = 2 * Math.PI * radius;
    const sum = segments.reduce((n, s) => n + s.value, 0);
    const gap = segments.filter((s) => s.value > 0).length > 1 ? SEGMENT_GAP : 0;
    const starts = segments.map((_, i) =>
      segments.slice(0, i).reduce((n, s) => n + (s.value / sum) * circumference, 0),
    );

    return (
      <div
        ref={ref}
        data-slot="donut"
        role="img"
        aria-label={
          segments.length > 0 ? segments.map((s) => `${s.label} ${s.value}`).join(', ') : 'No data'
        }
        className={cn('relative shrink-0', className)}
        style={{ width: size, height: size, ...style }}
        {...props}
      >
        <svg viewBox={`0 0 ${size} ${size}`} className="size-full -rotate-90" aria-hidden>
          <circle
            cx={size / 2}
            cy={size / 2}
            r={radius}
            fill="none"
            strokeWidth={thickness}
            className="stroke-surface-elevated"
          />
          {sum > 0 &&
            segments.map((s, i) =>
              s.value > 0 ? (
                <circle
                  key={s.label}
                  cx={size / 2}
                  cy={size / 2}
                  r={radius}
                  fill="none"
                  strokeWidth={thickness}
                  stroke={donutSegmentColor(i)}
                  strokeDasharray={`${Math.max((s.value / sum) * circumference - gap, 1)} ${circumference}`}
                  strokeDashoffset={-(starts[i] ?? 0)}
                />
              ) : null,
            )}
        </svg>
        {children != null && (
          <div className="absolute inset-0 flex items-center justify-center">{children}</div>
        )}
      </div>
    );
  },
);
Donut.displayName = 'Donut';

export { Donut };
