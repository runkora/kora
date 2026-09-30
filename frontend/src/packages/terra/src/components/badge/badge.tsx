import * as React from 'react';
import { cva, type VariantProps } from 'class-variance-authority';
import { cn } from '../../utils';

/**
 * Badge variants — structural, status signals, and provider/strategy identity.
 *
 * Status variants map 1-to-1 to the `--color-status-*` design tokens so that
 * a badge's colour is always semantically meaningful in the context of kora's
 * agent-state model (healthy / degraded / error / idle / pending).
 *
 * Provider variants use `--color-provider-*` brand identity tokens.
 * Strategy variants use `--color-strategy-*` tokens.
 * The `elevated` variant uses surface/border tokens for neutral filled chips.
 *
 * Size variants:
 *   sm — compact chip for toolbars, input bars, and inline metadata
 *   md — standard badge (default)
 *   lg — larger pill for role chips and prominent inline labels
 *
 * Shape variants:
 *   default — rounded-md (standard rectangular badge)
 *   pill    — rounded-full (softer shape for status pills and role chips)
 */
const badgeVariants = cva(
  [
    'inline-flex items-center',
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
         * Elevated — surface-elevated background with a subtle border.
         * Use for neutral filled chips (scope labels, service names, role names)
         * where the background provides gentle visual separation without colour.
         *
         * ```tsx
         * <Badge variant="elevated" size="sm" mono>resource:read</Badge>
         * ```
         */
        elevated: 'bg-surface-elevated text-foreground border border-border-subtle font-normal',

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

        /**
         * Muted — for inactive / disabled states.
         * Use when an entity is present but not operational (e.g. a disabled
         * route, an inactive model). Pairs with `dot` and `shape="pill"`.
         *
         * ```tsx
         * <Badge variant="muted" shape="pill" dot>Disabled</Badge>
         * ```
         */
        muted: 'bg-muted/20 text-muted border border-muted/30 font-medium',

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

        /* ── Provider identity variants ── */

        /**
         * AWS Bedrock — uses the `--color-provider-bedrock` token (AWS Orange).
         * For use in ProviderBadge and anywhere AWS Bedrock is identified visually.
         */
        'provider-bedrock':
          'bg-provider-bedrock/15 text-provider-bedrock border border-provider-bedrock/30 font-medium',

        /**
         * Google Vertex AI — uses the `--color-provider-vertexai` token (Google Blue).
         */
        'provider-vertexai':
          'bg-provider-vertexai/15 text-provider-vertexai border border-provider-vertexai/30 font-medium',

        /**
         * Azure OpenAI — uses the `--color-provider-azure` token (Azure Blue).
         */
        'provider-azure':
          'bg-provider-azure/15 text-provider-azure border border-provider-azure/30 font-medium',

        /* ── Strategy identity variants ── */

        /**
         * Weighted split strategy — uses the `--color-strategy-weighted` token (purple).
         */
        'strategy-weighted':
          'bg-strategy-weighted/15 text-strategy-weighted border border-strategy-weighted/30 font-medium',

        /* ── Semantic role variants ── */

        /**
         * Reserved — subtle accent tint for elevated/privileged roles such as
         * `platform-admin`. Uses a very light accent fill so
         * the chip reads as "special" without competing with primary CTAs.
         *
         * ```tsx
         * <Badge variant="reserved" shape="pill" size="lg" mono>platform-admin</Badge>
         * ```
         */
        reserved: 'bg-accent/5 text-accent border border-accent/30 font-medium',
      },

      size: {
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

        /**
         * Standard badge — `text-xs`, `px-2 py-0.5`.
         * Use for status indicators, category labels, and version numbers.
         */
        md: 'px-2 py-0.5 text-xs',

        /**
         * Large pill — `text-xs`, `px-2.5 py-1`.
         * Use for role chips, prominent inline labels, and pill-shaped
         * status indicators where more breathing room is appropriate.
         *
         * ```tsx
         * <Badge variant="default" shape="pill" size="lg" mono>platform-admin</Badge>
         * ```
         */
        lg: 'px-2.5 py-1 text-xs',
      },

      /**
       * Shape — controls border-radius.
       *
       * - `default`: `rounded-md` — standard rectangular badge (default).
       * - `sharp`: `rounded-sm` — tighter corners; use for code/scope chips.
       * - `pill`: `rounded-full` — fully rounded; use for status pills and role
       *    chips where a softer shape is appropriate.
       *
       * ```tsx
       * <Badge variant="healthy" shape="pill" dot>Enabled</Badge>
       * <Badge variant="elevated" shape="sharp" size="sm" mono>inference:anthropic:use</Badge>
       * <Badge variant="elevated" shape="pill" size="lg" mono>platform-admin</Badge>
       * ```
       */
      shape: {
        default: 'rounded-md',
        sharp: 'rounded-sm',
        pill: 'rounded-full',
      },

      /** Gap between flex children. Use `none` for tightly packed token chips. */
      gap: {
        default: 'gap-1.5',
        none: 'gap-0',
      },
    },
    defaultVariants: {
      variant: 'default',
      size: 'md',
      shape: 'default',
      gap: 'default',
    },
  },
);

export interface BadgeProps extends VariantProps<typeof badgeVariants> {
  /**
   * When true, renders a small filled circle before the label to indicate
   * live / pulsing status. Pairs best with the status variants.
   */
  dot?: boolean;

  /**
   * When true, renders the badge label in the mono typeface.
   * Use for code identifiers, scope strings, and machine-readable labels.
   *
   * ```tsx
   * <Badge variant="elevated" size="sm" mono>models:read</Badge>
   * ```
   */
  mono?: boolean;

  /** @internal className is locked — all styling must go through typed props. */
  className?: never;
  children?: React.ReactNode;
  id?: string;
  role?: React.AriaRole;
  title?: string;
  'aria-label'?: string;
  'aria-hidden'?: boolean | 'true' | 'false';
  'aria-describedby'?: string;
}

/**
 * Badge
 *
 * A compact label for conveying status, category, or metadata. The five status
 * variants (healthy, degraded, error, idle, pending) map directly to kora's
 * agent operational state model.
 *
 * Provider variants (provider-bedrock, provider-vertexai, provider-azure) and
 * the strategy-weighted variant use design tokens for brand-identity colouring
 * without any arbitrary hex values.
 *
 * Use `size="sm"` with `variant="dim"` for low-emphasis toolbar chips.
 * Use `shape="pill"` with a status variant for pill-shaped status indicators.
 * Use `variant="elevated"` for neutral filled chips (scope labels, service names).
 * Use `mono` for code identifiers and scope strings.
 *
 * ```tsx
 * <Badge variant="healthy" dot>Healthy</Badge>
 * <Badge variant="pending">Awaiting approval</Badge>
 * <Badge variant="secondary">v1.4.2</Badge>
 * <Badge variant="dim" size="sm">orbit-3-turbo</Badge>
 * <Badge variant="healthy" shape="pill" dot>Enabled</Badge>
 * <Badge variant="muted"   shape="pill" dot>Disabled</Badge>
 * <Badge variant="elevated" size="sm" mono>models:read</Badge>
 * <Badge variant="provider-bedrock" size="sm">AWS Bedrock</Badge>
 * <Badge variant="strategy-weighted" size="sm">Weighted</Badge>
 * <Badge variant="default" shape="pill" size="lg" mono>platform-admin</Badge>
 * ```
 */
const Badge = React.forwardRef<HTMLSpanElement, BadgeProps>(
  ({ variant, size, shape, gap, dot = false, mono = false, children, ...props }, ref) => {
    return (
      <span
        ref={ref}
        className={cn(badgeVariants({ variant, size, shape, gap }), mono && 'font-mono')}
        {...props}
      >
        {dot && (
          <span
            aria-hidden="true"
            className="size-1.5 rounded-full bg-current opacity-80 shrink-0"
          />
        )}
        {children}
      </span>
    );
  },
);

Badge.displayName = 'Badge';

export { Badge, badgeVariants };
