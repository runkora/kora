import * as React from 'react';
import { cva } from 'class-variance-authority';
import { cn } from '../../utils';

/** @tier 1 — Zero Orbit domain knowledge. A row of mutually exclusive filters on one track. */

/** The recessed track the options sit on. */
const segmentedControlVariants = cva([
  'inline-flex',
  'gap-1',
  'rounded-md',
  'border',
  'border-border-subtle',
  'bg-surface',
  'p-[3px]',
]);

/**
 * One option on the track; `selected` fills it with the accent, like a primary button.
 * The radius is the track's (the medium step) minus the track's 1px border and
 * 3px padding, so the selected fill sits concentric inside the track.
 */
const segmentedControlOptionVariants = cva(
  [
    'inline-flex',
    'h-7',
    'cursor-pointer',
    'items-center',
    'gap-1.5',
    'rounded-[calc(var(--radius-md)_-_4px)]',
    'border-none',
    'px-[11px]',
    'font-sans',
    'text-xs',
    'transition-colors',
    'focus-visible:outline-2',
    'focus-visible:-outline-offset-2',
    'focus-visible:outline-accent',
  ],
  {
    variants: {
      selected: {
        true: 'bg-accent font-semibold text-accent-foreground',
        false: 'bg-transparent font-medium text-muted hover:text-foreground',
      },
    },
    defaultVariants: { selected: false },
  },
);

/** The trailing count. Monospaced so numbers align down the row. */
const segmentedControlCountVariants = cva([
  'font-mono',
  'text-[10px]',
  'font-medium',
  'text-subtle',
]);

/**
 * Reserves the width of the bold text with an invisible copy in `::after`, so an
 * item keeps the same width whether or not it is selected.
 */
const reserveBold =
  'inline-flex flex-col after:invisible after:h-0 after:overflow-hidden after:content-[attr(data-text)]';

export interface SegmentedControlOption {
  /** Stable value passed to `onChange`. */
  value: string;
  /** Visible label. */
  label: string;
  /** Optional leading icon, e.g. `<Icon name="clock" size="2xs" />`. */
  icon?: React.ReactNode;
  /**
   * Optional trailing count. Monospaced and dimmed so the numbers align down a
   * row of options without competing with the labels.
   */
  count?: number | string;
}

export interface SegmentedControlProps extends Omit<
  React.HTMLAttributes<HTMLDivElement>,
  'onChange'
> {
  options: SegmentedControlOption[];
  /** Currently selected value. */
  value: string;
  onChange: (value: string) => void;
  /** Accessible name for the group. */
  'aria-label'?: string;
}

/**
 * SegmentedControl
 *
 * A row of mutually exclusive filters sharing one recessed track. Use when the
 * options partition a single list and their relative sizes matter — the counts
 * are part of the answer, not decoration.
 *
 * Prefer `Tabs` when the options swap between genuinely different views rather
 * than filtering one.
 *
 * **Semantics.** A `group` of toggle buttons each carrying `aria-pressed`, not
 * a radio group. A radio group promises the roving-tabindex keyboard contract —
 * one tab stop for the set, arrow keys to move between options, selection
 * following focus — and this does not implement it. Announcing an honest set of
 * toggle buttons that Tab reaches individually beats announcing a contract the
 * keyboard does not honour. Rebuild on Radix ToggleGroup if the roving model is
 * ever wanted; the `options` API would not have to change.
 *
 * ```tsx
 * <SegmentedControl
 *   aria-label="Filter by kind"
 *   value={kind}
 *   onChange={setKind}
 *   options={[
 *     { value: 'all', label: 'All', icon: <Icon name="layers" size="2xs" />, count: 40 },
 *     { value: 'webhook', label: 'Webhook', count: 27 },
 *   ]}
 * />
 * ```
 */
const SegmentedControl = React.forwardRef<HTMLDivElement, SegmentedControlProps>(
  ({ options, value, onChange, className, ...props }, ref) => (
    <div
      ref={ref}
      role="group"
      data-slot="segmented-control"
      className={cn(segmentedControlVariants(), className)}
      {...props}
    >
      {options.map((o) => {
        const selected = o.value === value;
        return (
          <button
            key={o.value}
            type="button"
            aria-pressed={selected}
            data-slot="segmented-control-option"
            data-state={selected ? 'active' : 'inactive'}
            onClick={() => onChange(o.value)}
            className={cn(segmentedControlOptionVariants({ selected }))}
          >
            {o.icon}
            <span data-text={o.label} className={cn(reserveBold, 'after:font-semibold')}>
              {o.label}
            </span>
            {o.count !== undefined && (
              <span
                data-text={o.count}
                className={cn(
                  segmentedControlCountVariants(),
                  reserveBold,
                  'after:font-sans after:text-xs after:font-bold after:tabular-nums',
                  selected && 'font-sans text-xs font-bold tabular-nums text-accent-foreground',
                )}
              >
                {o.count}
              </span>
            )}
          </button>
        );
      })}
    </div>
  ),
);
SegmentedControl.displayName = 'SegmentedControl';

export { SegmentedControl, segmentedControlVariants, segmentedControlOptionVariants };
