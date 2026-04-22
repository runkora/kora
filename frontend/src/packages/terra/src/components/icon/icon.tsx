import * as React from 'react';
import { cn } from '../../utils';
import { ICON_REGISTRY, type IconName } from './registry';

/**
 * Icon
 *
 * The Terra icon primitive. Renders any icon from the curated registry with
 * strongly-typed name, size, and optional container.
 *
 * **Never import from `lucide-react` directly** in product code. Use this
 * component instead — it ensures icon tokens, accessible labels, and the
 * container pattern all stay consistent.
 *
 * ## Bare icon (inline with text, inside a Button, etc.)
 *
 * ```tsx
 * <Icon name="search" size="sm" />
 * ```
 *
 * ## Icon with a container (feature icons, empty states, card corners)
 *
 * ```tsx
 * <Icon name="bot" size="md" container />
 * <Icon name="bot" size="md" container containerVariant="accent" />
 * ```
 *
 * ## Accessible icons
 *
 * Decorative icons (no label) are aria-hidden by default.
 * Pass `label` for icons that convey meaning standalone:
 *
 * ```tsx
 * <Icon name="trash" label="Delete agent" />
 * ```
 *
 * ## className is intentionally blocked
 *
 * `Icon` exposes no `className` prop. Size and colour come from the typed
 * props. If you need a one-off colour override, compose with `asChild` on a
 * parent Terra element. If you find yourself needing something `Icon` can't
 * express, the gap belongs in Terra — not in product code.
 */

// ── Types ─────────────────────────────────────────────────────────────────────

/** Pixel size each `IconSize` token maps to. */
const ICON_SIZE: Record<IconSize, string> = {
  '2xs': 'size-3.5', // 14px — compact UI (table actions, inline chips)
  xs: 'size-3', // 12px
  sm: 'size-4', // 16px
  md: 'size-5', // 20px
  lg: 'size-6', // 24px
  xl: 'size-8', // 32px
};

/** Container sizes — one step larger than the icon to provide padding. */
const CONTAINER_SIZE: Record<IconSize, string> = {
  '2xs': 'size-6', // 24px container for 14px icon
  xs: 'size-5', // 20px container for 12px icon
  sm: 'size-7', // 28px container for 16px icon
  md: 'size-9', // 36px container for 20px icon
  lg: 'size-12', // 48px container for 24px icon
  xl: 'size-16', // 64px container for 32px icon (chat welcome screen)
};

/** Container corner radius — matches size to keep proportions tight. */
const CONTAINER_RADIUS: Record<IconSize, string> = {
  '2xs': 'rounded-md',
  xs: 'rounded-md',
  sm: 'rounded-lg',
  md: 'rounded-xl',
  lg: 'rounded-2xl',
  xl: 'rounded-2xl',
};

const CONTAINER_VARIANT: Record<IconContainerVariant, string> = {
  accent: 'bg-accent/15 text-accent',
  surface: 'bg-surface-elevated text-foreground border border-border-subtle',
  muted: 'bg-surface-overlay text-muted-foreground',
};

/**
 * Maps semantic colour tokens to Tailwind text-colour classes.
 * Full strings required — no template literals (Tailwind v4 static scanner).
 */
export type IconColor =
  | 'default'
  | 'accent'
  | 'muted'
  | 'subtle'
  | 'rose'
  | 'status-healthy'
  | 'status-error'
  | 'status-degraded'
  | 'status-pending';

const ICON_COLOR: Record<IconColor, string> = {
  default: 'text-foreground',
  accent: 'text-accent',
  muted: 'text-muted-foreground',
  subtle: 'text-subtle',
  rose: 'text-rose-500',
  'status-healthy': 'text-status-healthy',
  'status-error': 'text-status-error',
  'status-degraded': 'text-status-degraded',
  'status-pending': 'text-status-pending',
};

export type IconSize = '2xs' | 'xs' | 'sm' | 'md' | 'lg' | 'xl';
export type IconContainerVariant = 'accent' | 'surface' | 'muted';

export interface IconProps {
  /** Icon to render — must exist in the Terra registry. */
  name: IconName;

  /**
   * Controls the rendered SVG size.
   * xs=12px  sm=16px  md=20px  lg=24px  xl=32px
   * @default 'sm'
   */
  size?: IconSize;

  /**
   * When true, wraps the icon in a rounded container box.
   * Use for feature icons, empty states, and card corner icons.
   */
  container?: boolean;

  /**
   * Visual style of the container when `container` is true.
   * @default 'accent'
   */
  containerVariant?: IconContainerVariant;

  /**
   * Accessible label for standalone icons that convey meaning.
   * When omitted the icon is `aria-hidden="true"` (decorative).
   *
   * ```tsx
   * <Icon name="trash" label="Delete" />
   * ```
   */
  label?: string;

  /**
   * Semantic colour token for the icon. Controls the SVG stroke/fill colour.
   * When `container` is true the container variant controls colour instead.
   */
  color?: IconColor;

  /**
   * When true, applies `fill-current` so the icon is rendered filled.
   * Useful for icons like `heart` that have a filled variant.
   */
  filled?: boolean;

  /**
   * className is intentionally not exposed. Size and colour are controlled
   * via typed props to keep icons consistent across the product.
   *
   * If you genuinely need a one-off style, open a Terra PR to add a prop.
   */
  className?: never;
}

// ── Component ─────────────────────────────────────────────────────────────────

const Icon = React.forwardRef<HTMLSpanElement, IconProps>(
  (
    {
      name,
      size = 'sm',
      container = false,
      containerVariant = 'accent',
      color,
      filled = false,
      label,
    },
    ref,
  ) => {
    const LucideIcon = ICON_REGISTRY[name];
    const svgClasses = cn(
      ICON_SIZE[size],
      'shrink-0 pointer-events-none',
      color && !container && ICON_COLOR[color],
      filled && 'fill-current',
    );

    const svgEl = (
      <LucideIcon
        className={svgClasses}
        aria-hidden={label ? undefined : true}
        aria-label={label}
        role={label ? 'img' : undefined}
        data-slot="icon"
      />
    );

    if (!container) {
      // Bare icon — render as a semantically neutral inline span so it can
      // sit inside flex rows, buttons, and list items without adding a block.
      return (
        <span
          ref={ref}
          data-slot="icon-wrapper"
          aria-hidden={label ? undefined : true}
          aria-label={label}
          style={{ display: 'contents' }}
        >
          {svgEl}
        </span>
      );
    }

    return (
      <span
        ref={ref}
        data-slot="icon-container"
        aria-hidden={label ? undefined : true}
        aria-label={label}
        className={cn(
          'inline-flex shrink-0 items-center justify-center',
          CONTAINER_SIZE[size],
          CONTAINER_RADIUS[size],
          CONTAINER_VARIANT[containerVariant],
        )}
      >
        {svgEl}
      </span>
    );
  },
);
Icon.displayName = 'Icon';

// ── Exports ───────────────────────────────────────────────────────────────────

export { Icon };
export type { IconName };
