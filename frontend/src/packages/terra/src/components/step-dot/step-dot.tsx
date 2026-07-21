import * as React from 'react';
import { cn } from '../../utils';

/** Visual appearance per wizard-rail state — full static strings for the Tailwind scanner. */
const STATE_CLASSES: Record<NonNullable<StepDotProps['state']>, string> = {
  pending: 'border-[1.5px] border-border bg-transparent text-subtle',
  current: 'border-[1.5px] border-accent bg-accent text-accent-foreground',
  complete: 'border-[1.5px] border-accent/30 bg-accent/15 text-accent',
};

export interface StepDotProps extends React.ComponentPropsWithoutRef<'span'> {
  /**
   * Visual variant.
   * - `accent` (default): accent background, for step numbers in onboarding flows
   * - `surface`: surface background with muted text, for priority/rank indicators
   *
   * Ignored when `state` is set.
   */
  variant?: 'accent' | 'surface';

  /**
   * Size of the dot.
   * - `md` (default): size-6 / 24 px — onboarding step numbers
   * - `sm`: size-4 / 16 px — compact rank indicators
   * - `lg`: 30 px with mono numerals — wizard rail dots (e.g. the agent builder)
   */
  size?: 'md' | 'sm' | 'lg';

  /**
   * Wizard-rail state. When set it takes over the dot's colouring
   * (superseding `variant`); when `undefined` (default) the dot renders
   * exactly as before.
   * - `pending`: outlined, subtle text — steps not yet reached
   * - `current`: accent fill — the active step
   * - `complete`: translucent accent fill with accent text — finished steps
   */
  state?: 'pending' | 'current' | 'complete';
}

/**
 * StepDot
 *
 * A circular badge for displaying step numbers in a sequential flow or
 * compact rank/priority indicators in a list. For multi-step wizard rails,
 * pass `state` to express pending / current / complete appearances.
 *
 * ```tsx
 * <StepDot>1</StepDot>
 * <StepDot variant="surface" size="sm">{idx + 1}</StepDot>
 * <StepDot size="lg" state="complete">1</StepDot>
 * ```
 */
const StepDot = React.forwardRef<HTMLSpanElement, StepDotProps>(
  ({ children, variant = 'accent', size = 'md', state, className, ...props }, ref) => (
    <span
      ref={ref}
      data-slot="step-dot"
      data-state={state}
      className={cn(
        'inline-flex shrink-0 items-center justify-center rounded-full font-bold',
        size === 'lg'
          ? 'size-[30px] text-xs font-mono'
          : size === 'md'
            ? 'size-6 text-[11px]'
            : 'size-4 text-[10px]',
        state
          ? STATE_CLASSES[state]
          : variant === 'accent'
            ? 'bg-accent text-accent-foreground'
            : 'bg-surface text-muted',
        className,
      )}
      {...props}
    >
      {children}
    </span>
  ),
);

StepDot.displayName = 'StepDot';

export { StepDot };
