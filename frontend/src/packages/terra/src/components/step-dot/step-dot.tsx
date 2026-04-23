import * as React from 'react';
import { cn } from '../../utils';

export interface StepDotProps extends React.HTMLAttributes<HTMLSpanElement> {
  /**
   * Visual variant.
   * - `accent` (default): accent background, for step numbers in onboarding flows
   * - `surface`: surface background with muted text, for priority/rank indicators
   */
  variant?: 'accent' | 'surface';

  /**
   * Size of the dot.
   * - `md` (default): size-6 / 24 px — onboarding step numbers
   * - `sm`: size-4 / 16 px — compact rank indicators
   */
  size?: 'md' | 'sm';
}

/**
 * StepDot
 *
 * A circular badge for displaying step numbers in a sequential flow or
 * compact rank/priority indicators in a list.
 *
 * ```tsx
 * <StepDot>1</StepDot>
 * <StepDot variant="surface" size="sm">{idx + 1}</StepDot>
 * ```
 */
const StepDot = React.forwardRef<HTMLSpanElement, StepDotProps>(
  ({ children, variant = 'accent', size = 'md', className, ...props }, ref) => (
    <span
      ref={ref}
      className={cn(
        'inline-flex shrink-0 items-center justify-center rounded-full font-bold',
        size === 'md' ? 'size-6 text-[11px]' : 'size-4 text-[10px]',
        variant === 'accent' ? 'bg-accent text-accent-foreground' : 'bg-surface text-muted',
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
