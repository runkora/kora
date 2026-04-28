import * as React from 'react';
import { cn } from '../../utils';

// ── Types ─────────────────────────────────────────────────────────────────────

type AlertVariant = 'error' | 'warning' | 'info';

const VARIANT_CLASSES: Record<AlertVariant, string> = {
  error: 'border-status-error/30 bg-status-error/5',
  warning: 'border-status-degraded/30 bg-status-degraded/10',
  info: 'border-accent/30 bg-accent/10',
};

// ── Props ─────────────────────────────────────────────────────────────────────

export interface AlertProps {
  /**
   * Visual intent of the alert.
   *
   * - `error`   — red, maps to `status-error` tokens. Use for validation errors and failures.
   * - `warning` — amber, maps to `status-degraded` tokens. Use for degraded-state notices.
   * - `info`    — accent, maps to the brand accent tokens. Use for neutral informational callouts.
   *
   * Default: `'info'`
   */
  variant?: AlertVariant;
  children?: React.ReactNode;
  /** @internal Used for internal Terra composition via cn(). */
  className?: string;
  id?: string;
  role?: React.AriaRole;
  'aria-label'?: string;
  'aria-live'?: 'off' | 'assertive' | 'polite';
}

// ── Component ─────────────────────────────────────────────────────────────────

/**
 * Alert
 *
 * A compact inline callout for errors, warnings, and informational notices.
 * Renders a horizontal row with a rounded border and a tinted background
 * derived from the active colour token.
 *
 * ```tsx
 * <Alert variant="error">
 *   <AlertCircle className="size-3.5 shrink-0 text-status-error" />
 *   <Text size="caption" color="muted">Something went wrong.</Text>
 * </Alert>
 *
 * <Alert variant="warning">
 *   <AlertCircle className="size-3.5 shrink-0 text-status-degraded" />
 *   <Text size="caption">Weights must sum to 100%.</Text>
 * </Alert>
 * ```
 */
const Alert = React.forwardRef<HTMLDivElement, AlertProps>(
  ({ variant = 'info', className, children, ...props }, ref) => {
    return (
      <div
        ref={ref}
        role="alert"
        className={cn(
          'flex items-center gap-2 rounded-lg border px-4 py-3',
          VARIANT_CLASSES[variant],
          className,
        )}
        {...props}
      >
        {children}
      </div>
    );
  },
);

Alert.displayName = 'Alert';

export { Alert };
