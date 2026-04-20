import * as React from 'react';
import { cva, type VariantProps } from 'class-variance-authority';
import { cn } from '../../utils';

/**
 * Badge variants — structural + status signals.
 *
 * Status variants map 1-to-1 to the `--color-status-*` design tokens so that
 * a badge's colour is always semantically meaningful in the context of Orbit's
 * agent-state model (healthy / degraded / error / idle / pending).
 *
 * Size variants:
 *   md — standard badge (default)
 *   sm — compact chip for toolbars, input bars, and inline metadata
 */
const badgeVariants = cva(
  [
    'inline-flex items-center gap-1.5',
    'rounded-md',
    'leading-none',
    'whitespace-nowrap select-none',
    'transition-colors duration-150',
  ],
  {
    variants: {
      variant: {
        /** Default — accent tint */
        default: 'bg-accent/15 text-accent border border-accent/30 font-medium',

        /** Secondary — neutral surface */
        secondary: 'bg-surface-elevated text-muted border border-border font-medium',

        /**
         * Outline — transparent with a standard border and full-weight text.
         * Use for neutral labels that need clear delineation.
         */
        outline: 'bg-transparent text-foreground border border-border font-medium',

        /**
         * Dim — transparent with a subtle border and muted text at normal weight.
         * Use for low-emphasis metadata chips such as model selectors, keyboard
         * hints, and toolbar annotations that should not compete with primary UI.
         *
         * ```tsx
         * <Badge variant="dim" size="sm">orbit-3-turbo</Badge>
         * ```
         */
        dim: 'bg-transparent text-subtle border border-border font-normal',

        /** Destructive — error intent */
        destructive:
          'bg-status-error/15 text-status-error border border-status-error/30 font-medium',

        /* ── Status variants (agent operational states) ── */

        /** Agent is operating normally */
        healthy:
          'bg-status-healthy/15 text-status-healthy border border-status-healthy/30 font-medium',

        /** Agent is running but degraded */
        degraded:
          'bg-status-degraded/15 text-status-degraded border border-status-degraded/30 font-medium',

        /** Agent has encountered an error */
        error: 'bg-status-error/15 text-status-error border border-status-error/30 font-medium',

        /** Agent is inactive / not scheduled */
        idle: 'bg-status-idle/15 text-status-idle border border-status-idle/30 font-medium',

        /** Agent is starting up or awaiting a decision */
        pending:
          'bg-status-pending/15 text-status-pending border border-status-pending/30 font-medium',
      },

      size: {
        /**
         * Standard badge — `text-xs`, `px-2 py-0.5`.
         * Use for status indicators, category labels, and version numbers.
         */
        md: 'px-2 py-0.5 text-xs',

        /**
         * Compact chip — `text-xs`, `px-1.5 py-px`, reduced height.
         * Use for inline toolbar chips, model selectors, keyboard hint pills,
         * and anywhere a standard badge would feel too large relative to its
         * surrounding text.
         *
         * ```tsx
         * <Badge variant="dim" size="sm">orbit-3-turbo</Badge>
         * <Badge variant="secondary" size="sm">⌘ K</Badge>
         * ```
         */
        sm: 'px-1.5 py-px text-xs h-5',
      },
    },
    defaultVariants: {
      variant: 'default',
      size: 'md',
    },
  },
);

export interface BadgeProps
  extends React.HTMLAttributes<HTMLDivElement>, VariantProps<typeof badgeVariants> {
  /**
   * When true, renders a small filled circle before the label to indicate
   * live / pulsing status. Pairs best with the status variants.
   */
  dot?: boolean;
}

/**
 * Badge
 *
 * A compact label for conveying status, category, or metadata. The five status
 * variants (healthy, degraded, error, idle, pending) map directly to Orbit's
 * agent operational state model.
 *
 * Use `size="sm"` with `variant="dim"` for low-emphasis toolbar chips such as
 * model selectors and keyboard shortcut hints.
 *
 * ```tsx
 * <Badge variant="healthy" dot>Healthy</Badge>
 * <Badge variant="pending">Awaiting approval</Badge>
 * <Badge variant="secondary">v1.4.2</Badge>
 * <Badge variant="dim" size="sm">orbit-3-turbo</Badge>
 * ```
 */
const Badge = React.forwardRef<HTMLDivElement, BadgeProps>(
  ({ className, variant, size, dot = false, children, ...props }, ref) => {
    return (
      <div ref={ref} className={cn(badgeVariants({ variant, size }), className)} {...props}>
        {dot && (
          <div
            aria-hidden="true"
            className="size-1.5 rounded-full bg-current opacity-80 shrink-0"
          />
        )}
        {children}
      </div>
    );
  },
);

Badge.displayName = 'Badge';

export { Badge, badgeVariants };
