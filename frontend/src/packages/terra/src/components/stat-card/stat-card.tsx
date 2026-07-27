import * as React from 'react';
import { cva, type VariantProps } from 'class-variance-authority';
import { cn } from '../../utils';

/**
 * Tone drives the value and icon colour together, so a card that is reporting a
 * problem reads as one at a glance rather than requiring the label to be read.
 */
const valueVariants = cva(['font-mono', 'font-bold', 'text-[25px]', 'tracking-[-0.02em]'], {
  variants: {
    tone: {
      default: 'text-foreground',
      error: 'text-status-error',
      healthy: 'text-status-healthy',
    },
  },
  defaultVariants: { tone: 'default' },
});

const iconVariants = cva(['inline-flex', 'shrink-0'], {
  variants: {
    tone: {
      default: 'text-subtle',
      error: 'text-status-error',
      healthy: 'text-status-healthy',
    },
  },
  defaultVariants: { tone: 'default' },
});

export interface StatCardProps
  extends
    Omit<React.HTMLAttributes<HTMLDivElement>, 'children'>,
    VariantProps<typeof valueVariants> {
  /** Short all-caps label, e.g. "ACTIVE TRIGGERS". Rendered in tracked small caps. */
  label: string;
  /** The headline figure. Monospaced so a column of cards aligns digit-for-digit. */
  value: string;
  /** Supporting line under the value: a comparison, a breakdown, or a caveat. */
  detail?: string;
  /** Tone for the detail line alone, when it carries the signal rather than the value. */
  detailTone?: 'default' | 'error' | 'healthy';
  /** Icon rendered top-right, e.g. `<Icon name="activity" size="xs" />`. */
  icon?: React.ReactNode;
}

const detailToneClass: Record<string, string> = {
  default: 'text-subtle',
  error: 'text-status-error',
  healthy: 'text-status-healthy',
};

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
        <span className="font-sans text-[10px] font-semibold tracking-[0.11em] text-subtle">
          {label}
        </span>
        {icon != null && <span className={cn(iconVariants({ tone }))}>{icon}</span>}
      </div>
      <div className={cn('mt-[11px]', valueVariants({ tone }))}>{value}</div>
      {detail != null && (
        <div className={cn('mt-1 text-[11.5px]', detailToneClass[detailTone])}>{detail}</div>
      )}
    </div>
  ),
);
StatCard.displayName = 'StatCard';

export { StatCard };
