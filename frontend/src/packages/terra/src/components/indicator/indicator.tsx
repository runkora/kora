import * as React from 'react';
import { cn } from '../../utils';
import { statusDotVariants } from '../status-dot/status-dot';

type IndicatorStatus = 'healthy' | 'degraded' | 'error' | 'idle' | 'pending';

export interface IndicatorProps {
  /**
   * Status of the notification dot rendered in the top-right corner.
   * @default 'error'
   */
  status?: IndicatorStatus;

  /** The element the dot is overlaid on. Must be a single React element. */
  children: React.ReactNode;
}

/**
 * Indicator
 *
 * Wraps a child element and overlays a small status dot in its top-right
 * corner. Use for notification badges on icon buttons.
 *
 * ```tsx
 * <Indicator status="error">
 *   <Button variant="ghost" size="icon" aria-label="Notifications">
 *     <Icon name="bell" />
 *   </Button>
 * </Indicator>
 * ```
 */
const Indicator = React.forwardRef<HTMLSpanElement, IndicatorProps>(
  ({ status = 'error', children }, ref) => (
    <span ref={ref} className="relative inline-flex">
      {children}
      <span
        className={cn(
          'pointer-events-none absolute right-1.5 top-1.5',
          statusDotVariants({ status, size: 'sm' }),
        )}
        aria-hidden
      />
    </span>
  ),
);

Indicator.displayName = 'Indicator';

export { Indicator };
