import * as React from 'react';
import { cva, type VariantProps } from 'class-variance-authority';
import { cn } from '../../utils';

/**
 * StatusDot variants.
 *
 * Each variant maps to a `--color-status-*` design token so the dot colour is
 * always semantically meaningful in the context of Orbit's agent-state model.
 *
 * An optional `glow` prop renders a soft halo using `color-mix` to reinforce
 * "live / active" versus "inactive / error" at a glance.
 */
const statusDotVariants = cva(['inline-block', 'rounded-full', 'shrink-0'], {
  variants: {
    /**
     * Visual state — maps to the `--color-status-*` token family.
     *
     * - `healthy`  — green; agent operating normally
     * - `degraded` — amber; agent running with warnings
     * - `error`    — red; agent has encountered an error
     * - `idle`     — grey-blue; agent is inactive / not scheduled
     * - `pending`  — blue; agent is starting up or awaiting a decision
     * - `muted`    — grey; generic inactive / disabled (no status semantics)
     * - `accent`   — brand accent; use for primary-colour indicators (e.g. chart legends)
     */
    status: {
      healthy: 'bg-status-healthy',
      degraded: 'bg-status-degraded',
      error: 'bg-status-error',
      idle: 'bg-status-idle',
      pending: 'bg-status-pending',
      muted: 'bg-muted/40',
      accent: 'bg-accent',
    },

    /**
     * Size — diameter of the dot.
     *
     * - `sm` (`size-1.5` / 6 px): micro-indicator inside badges and table cells
     * - `md` (`size-2.5` / 10 px): standard standalone availability indicator (default)
     * - `lg` (`size-3`   / 12 px): prominent use in headers and hero cards
     */
    size: {
      sm: 'size-1.5',
      md: 'size-2.5',
      lg: 'size-3',
    },
  },
  defaultVariants: {
    status: 'healthy',
    size: 'md',
  },
});

export interface StatusDotProps
  extends
    Omit<React.HTMLAttributes<HTMLSpanElement>, 'className'>,
    VariantProps<typeof statusDotVariants> {
  /**
   * When true, renders a soft halo ring around the dot using `color-mix` on
   * the status token. Reinforces "live / active" vs "inactive / error".
   * Best used with `size="md"` or `size="lg"`.
   */
  glow?: boolean;

  /** @internal className is locked — all styling must go through typed props. */
  className?: never;
}

/**
 * StatusDot
 *
 * A tiny filled circle that signals an entity's operational state. Maps
 * directly to Orbit's agent-state token set (healthy / degraded / error /
 * idle / pending) plus a neutral `muted` value for disabled entities.
 *
 * ```tsx
 * <StatusDot status="healthy" glow aria-label="Available" role="img" />
 * <StatusDot status="error"   glow aria-label="Unavailable" role="img" />
 * <StatusDot status="muted"   size="sm" />
 * ```
 */
const StatusDot = React.forwardRef<HTMLSpanElement, StatusDotProps>(
  ({ status, size, glow = false, style, ...props }, ref) => {
    // Glow is implemented via box-shadow using color-mix on the status token.
    // We resolve the CSS variable name from the status value at render time so
    // there is no need for a Tailwind arbitrary-value class per status.
    const glowStyle: React.CSSProperties =
      glow && status && status !== 'muted'
        ? {
            boxShadow: `0 0 0 3px color-mix(in oklch, var(--color-status-${status}) 15%, transparent)`,
            ...style,
          }
        : (style ?? {});

    return (
      <span
        ref={ref}
        className={cn(statusDotVariants({ status, size }))}
        style={glowStyle}
        {...props}
      />
    );
  },
);

StatusDot.displayName = 'StatusDot';

export { StatusDot, statusDotVariants };
