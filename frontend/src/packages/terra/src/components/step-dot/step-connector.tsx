import * as React from 'react';

/**
 * StepConnector
 *
 * A 1 px vertical line that fills the remaining space between step dots in a
 * sequential flow. Always used as a direct sibling of `<StepDot>` inside a
 * vertical `<Stack>`.
 *
 * ```tsx
 * <Stack align="center" gap="none">
 *   <StepDot>{number}</StepDot>
 *   <StepConnector />
 * </Stack>
 * ```
 */
const StepConnector = React.forwardRef<HTMLSpanElement, React.HTMLAttributes<HTMLSpanElement>>(
  (props, ref) => <span ref={ref} className="mt-2 w-px flex-1 bg-border" aria-hidden {...props} />,
);

StepConnector.displayName = 'StepConnector';

export { StepConnector };
