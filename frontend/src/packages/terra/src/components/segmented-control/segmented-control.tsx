import * as React from 'react';
import { cn } from '../../utils';

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
      role="radiogroup"
      data-slot="segmented-control"
      className={cn(
        'inline-flex gap-1 rounded-md border border-border-subtle bg-surface p-[3px]',
        className,
      )}
      {...props}
    >
      {options.map((o) => {
        const selected = o.value === value;
        return (
          <button
            key={o.value}
            type="button"
            role="radio"
            aria-checked={selected}
            data-slot="segmented-control-option"
            data-state={selected ? 'active' : 'inactive'}
            onClick={() => onChange(o.value)}
            className={cn(
              'inline-flex h-7 cursor-pointer items-center gap-1.5 rounded-[6px] border-none px-[11px] font-sans text-xs transition-colors',
              selected
                ? 'bg-surface-overlay font-semibold text-foreground'
                : 'bg-transparent font-medium text-muted hover:text-foreground',
            )}
          >
            {o.icon}
            {o.label}
            {o.count !== undefined && (
              <span className="font-mono text-[10.5px] font-medium text-subtle">{o.count}</span>
            )}
          </button>
        );
      })}
    </div>
  ),
);
SegmentedControl.displayName = 'SegmentedControl';

export { SegmentedControl };
