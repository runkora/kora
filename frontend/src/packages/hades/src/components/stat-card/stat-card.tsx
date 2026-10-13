import * as React from 'react';
import { cva, type VariantProps } from 'class-variance-authority';
import { cn } from '../../utils';
import { sectionLabelVariants } from '../section-label';
import { Donut, donutSegmentColor } from '../donut';

/** @tier 1 — Zero Orbit domain knowledge. One headline figure in a row of them. */

/**
 * The one tone scale, shared by the value, the icon and the detail line, so a
 * card that is reporting a problem reads as one at a glance rather than
 * requiring the label to be read. `default` is deliberately empty: each slot
 * keeps its own resting colour (foreground value, subtle icon and detail) and
 * a non-default tone overrides it via `cn`'s Tailwind merge.
 */
const toneVariants = cva('', {
  variants: {
    tone: {
      default: '',
      error: 'text-status-error',
      degraded: 'text-status-degraded',
      healthy: 'text-status-healthy',
    },
  },
  defaultVariants: { tone: 'default' },
});

export type StatCardTone = NonNullable<VariantProps<typeof toneVariants>['tone']>;

/**
 * 25px is off the type scale deliberately: `Heading variant="metric"` is 30px
 * sans, and monospaced digits at that size overflow a five-across row. 25px
 * mono is the optical match for the sans metric at the density this card is
 * built for. Do not "snap" it.
 */
const VALUE_CLASSES = 'font-mono font-bold text-[25px] tracking-[-0.02em] text-foreground';

/** The total inside the ring, smaller than the plain value so it fits the ring's centre. */
const RING_VALUE_CLASSES = 'font-mono font-bold text-xl tracking-[-0.02em] text-foreground';

/**
 * A change indicator under the value. It is healthy when `positive` is true and degraded
 * when it is not. Set `positiveIsBad` for figures where up is worse, such as an error rate.
 */
export interface StatCardDelta {
  text: string;
  positive: boolean;
  positiveIsBad?: boolean;
}

export interface StatCardBreakdownItem {
  label: string;
  value: number;
}

/** Healthy when the change is good news, degraded when it is not. */
function deltaTone(delta: StatCardDelta): StatCardTone {
  return delta.positive !== Boolean(delta.positiveIsBad) ? 'healthy' : 'degraded';
}

export interface StatCardProps
  extends
    Omit<React.HTMLAttributes<HTMLDivElement>, 'children'>,
    VariantProps<typeof toneVariants> {
  /** Short label in any casing, e.g. "Active triggers". Rendered uppercase in tracked small caps. */
  label: string;
  /** The headline figure. Monospaced so a column of cards aligns digit-for-digit. */
  value: string | number;
  /** Supporting line under the value: a comparison, a breakdown, or a caveat. */
  detail?: string;
  /** Tone for the detail line alone, when it carries the signal rather than the value. */
  detailTone?: StatCardTone;
  /** Change indicator under the value, coloured by `positive`. Renders below `detail` if both are set. */
  delta?: StatCardDelta;
  /**
   * The parts that make up the figure, largest first. Draws a ring with `value` in the
   * centre and a legend beside it, so `value` should be short.
   */
  breakdown?: StatCardBreakdownItem[];
  /**
   * How many breakdown parts the legend lists before "+N more". The ring always draws all of them.
   * @default 3
   */
  breakdownLimit?: number;
  /** Bottom-right slot for a link such as "View all". The caller supplies the link so Hades stays router-free. */
  footer?: React.ReactNode;
  /** Icon rendered top-right, e.g. `<Icon name="activity" size="xs" />`. */
  icon?: React.ReactNode;
}

/**
 * StatCard
 *
 * A single headline figure with a label, an optional icon and a supporting
 * line. Built for a row of four or five reporting one system's health, where
 * the figures must be comparable at a glance.
 *
 * Optional `delta`, `breakdown` and `footer` cover overview cards that need a
 * change indicator, a split of the figure or a link to the full view. A
 * `breakdown` turns the card into a ring with a legend; leave it off in a
 * five-across row, where the flat form scans best.
 *
 * ```tsx
 * <StatCard label="SUCCESS RATE" value="98.9%" detail="2,387 ok · 27 failed"
 *           icon={<Icon name="circle-check" size="xs" />} />
 * ```
 */
const StatCard = React.forwardRef<HTMLDivElement, StatCardProps>(
  (
    {
      label,
      value,
      detail,
      detailTone = 'default',
      delta,
      breakdown,
      breakdownLimit = 3,
      footer,
      icon,
      tone,
      className,
      ...props
    },
    ref,
  ) => (
    <div
      ref={ref}
      data-slot="stat-card"
      className={cn(
        'flex h-full flex-col rounded-lg border border-border-subtle bg-surface px-4 py-[15px]',
        className,
      )}
      {...props}
    >
      <div className="flex items-center justify-between">
        {/* The same overline recipe as SectionLabel and the DataGrid header. */}
        <span className={cn(sectionLabelVariants())}>{label}</span>
        {icon != null && (
          <span className={cn('inline-flex shrink-0 text-subtle', toneVariants({ tone }))}>
            {icon}
          </span>
        )}
      </div>
      {breakdown != null && breakdown.length > 0 ? (
        <div className="mt-4 flex items-center gap-4">
          <Donut segments={breakdown}>
            <span className={cn(RING_VALUE_CLASSES, toneVariants({ tone }))}>{value}</span>
          </Donut>
          <ul className="min-w-0 flex-1 space-y-1.5 text-[12px]">
            {breakdown.slice(0, breakdownLimit).map((item, i) => (
              <li key={item.label} className="flex items-center gap-2">
                <span
                  className="size-2 shrink-0 rounded-full"
                  style={{ background: donutSegmentColor(i) }}
                />
                <span className="truncate text-muted">{item.label}</span>
                <span className="ml-auto font-medium tabular-nums text-foreground">
                  {item.value}
                </span>
              </li>
            ))}
            {breakdown.length > breakdownLimit && (
              <li className="pl-4 text-[11px] text-subtle">
                +{breakdown.length - breakdownLimit} more
              </li>
            )}
          </ul>
        </div>
      ) : (
        <div className={cn('mt-[11px]', VALUE_CLASSES, toneVariants({ tone }))}>{value}</div>
      )}
      {detail != null && (
        <div className={cn('mt-1 text-[11px] text-subtle', toneVariants({ tone: detailTone }))}>
          {detail}
        </div>
      )}
      {delta != null && (
        <div className={cn('mt-1 text-[11px]', toneVariants({ tone: deltaTone(delta) }))}>
          {delta.text}
        </div>
      )}
      {footer != null && (
        <div className="mt-auto flex justify-end pt-3 text-[11px] text-subtle [&_a]:transition-colors [&_a:hover]:text-foreground">
          {footer}
        </div>
      )}
    </div>
  ),
);
StatCard.displayName = 'StatCard';

export { StatCard };
