import * as React from 'react';
import { cn } from '../../utils';

export interface StepConnectorProps extends React.ComponentPropsWithoutRef<'span'> {
  /**
   * When `true`, tints the line with a translucent accent (`bg-accent/30`)
   * to mark the connector above/below a finished step in a wizard rail.
   * Default `false` keeps the standard `bg-border` line.
   */
  complete?: boolean;
  /**
   * Axis the connector runs along.
   * - `'vertical'` (default) — 1 px wide, grows in height; used in the desktop rail.
   * - `'horizontal'` — 1 px tall, grows in width; used in the mobile step rail.
   */
  orientation?: 'vertical' | 'horizontal';
}

/**
 * StepConnector
 *
 * A 1 px vertical line that fills the remaining space between step dots in a
 * sequential flow. Always used as a direct sibling of `<StepDot>` inside a
 * vertical `<Stack>`. Pass `complete` to tint the line accent for the
 * finished portion of a wizard rail.
 *
 * ```tsx
 * <Stack align="center" gap="none">
 *   <StepDot state="complete">1</StepDot>
 *   <StepConnector complete />
 *   <StepDot state="current">2</StepDot>
 *   <StepConnector />
 * </Stack>
 * ```
 */
const StepConnector = React.forwardRef<HTMLSpanElement, StepConnectorProps>(
  ({ complete = false, orientation = 'vertical', className, ...props }, ref) => (
    <span
      ref={ref}
      data-slot="step-connector"
      className={cn(
        'flex-1',
        orientation === 'horizontal' ? 'h-px self-center' : 'mt-2 w-px',
        complete ? 'bg-accent/30' : 'bg-border',
        className,
      )}
      aria-hidden
      {...props}
    />
  ),
);

StepConnector.displayName = 'StepConnector';

export { StepConnector };
