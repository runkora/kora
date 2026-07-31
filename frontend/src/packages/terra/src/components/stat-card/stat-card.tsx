import * as React from 'react';
import { cva, type VariantProps } from 'class-variance-authority';
import { cn } from '../../utils';
import { sectionLabelVariants } from '../section-label';

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

export interface StatCardProps
  extends
    Omit<React.HTMLAttributes<HTMLDivElement>, 'children'>,
    VariantProps<typeof toneVariants> {
  /** Short all-caps label, e.g. "ACTIVE TRIGGERS". Rendered in tracked small caps. */
  label: string;
  /** The headline figure. Monospaced so a column of cards aligns digit-for-digit. */
  value: string;
  /** Supporting line under the value: a comparison, a breakdown, or a caveat. */
  detail?: string;
  /** Tone for the detail line alone, when it carries the signal rather than the value. */
  detailTone?: StatCardTone;
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
 * Distinct from the app-level `MetricCard`, which carries a delta indicator, an
 * optional breakdown table and a "view all" link. This one is deliberately
 * flatter: in a five-across row those affordances stop being scannable.
 *
 * ```tsx
 * <StatCard label="SUCCESS RATE" value="98.9%" detail="2,387 ok · 27 failed"
 *           icon={<Icon name="circle-check" size="xs" />} />
 * ```
 */
const StatCard = React.forwardRef<HTMLDivElement, StatCardProps>(
  ({ label, value, detail, detailTone = 'default', icon, tone, className, ...props }, ref) => (
    <div
      ref={ref}
      data-slot="stat-card"
      className={cn('rounded-lg border border-border-subtle bg-surface px-4 py-[15px]', className)}
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
      <div className={cn('mt-[11px]', VALUE_CLASSES, toneVariants({ tone }))}>{value}</div>
      {detail != null && (
        <div className={cn('mt-1 text-[11px] text-subtle', toneVariants({ tone: detailTone }))}>
          {detail}
        </div>
      )}
    </div>
  ),
);
StatCard.displayName = 'StatCard';

export { StatCard };
