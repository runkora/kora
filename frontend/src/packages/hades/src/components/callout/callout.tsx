import * as React from 'react';
import { cn } from '../../utils';

// ── Types ─────────────────────────────────────────────────────────────────────

type CalloutVariant = 'info' | 'warning' | 'success' | 'error';

const VARIANT_CLASSES: Record<CalloutVariant, string> = {
  info: 'border-accent/20 bg-accent/5',
  warning: 'border-status-degraded/30 bg-status-degraded/10',
  success: 'border-status-healthy/30 bg-status-healthy/5',
  error: 'border-status-error/30 bg-status-error/5',
};

// ── Props ─────────────────────────────────────────────────────────────────────

export interface CalloutProps {
  /**
   * Visual intent of the callout.
   *
   * - `info`    — accent tint; use for neutral explanatory copy (default).
   * - `warning` — amber tint; use for degraded-state notices or one-time
   *               warnings (e.g. "copy this key now — it won't be shown again").
   * - `success` — green tint; use for positive results or confirmations.
   * - `error`   — red tint; use for failed operations or denial results.
   */
  variant?: CalloutVariant;
  children: React.ReactNode;
  /** @internal Used for internal Hades composition via cn(). */
  className?: string;
  id?: string;
  'aria-label'?: string;
}

// ── Component ─────────────────────────────────────────────────────────────────

/**
 * Callout
 *
 * A styled informational callout box with a rounded border and a tinted
 * background. Use for contextual guidance copy placed inline with page
 * content — not for validation errors (use `Alert`) or empty states
 * (use `EmptyState`).
 *
 * ```tsx
 * // Neutral info callout — explaining how a feature works
 * <Callout>
 *   <Text size="caption" color="muted">
 *     Grants apply regardless of how the principal authenticates.
 *   </Text>
 * </Callout>
 *
 * // Warning callout — one-time notice
 * <Callout variant="warning">
 *   <Text size="caption">Copy this key now — it will not be shown again.</Text>
 * </Callout>
 * ```
 */
const Callout = React.forwardRef<HTMLElement, CalloutProps>(
  ({ variant = 'info', className, children, ...props }, ref) => (
    <aside
      ref={ref}
      data-slot="callout"
      data-variant={variant}
      className={cn('rounded-lg border px-4 py-3', VARIANT_CLASSES[variant], className)}
      {...props}
    >
      {children}
    </aside>
  ),
);

Callout.displayName = 'Callout';

export { Callout };
