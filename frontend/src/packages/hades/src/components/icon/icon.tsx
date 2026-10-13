import * as React from 'react';
import { cn } from '../../utils';
import { ICON_REGISTRY, type IconName } from './registry';

/**
 * Icon
 *
 * The Hades icon primitive. Renders any icon from the curated registry with
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
 * <Icon name="bot" size="md" container containerVariant="surface-subtle" circle />
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
 * parent Hades element. If you find yourself needing something `Icon` can't
 * express, the gap belongs in Hades — not in product code.
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
  'surface-subtle': 'bg-surface-elevated text-subtle border border-border-subtle',
  muted: 'bg-surface-elevated text-muted',
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
  muted: 'text-muted',
  subtle: 'text-subtle',
  rose: 'text-rose-500',
  'status-healthy': 'text-status-healthy',
  'status-error': 'text-status-error',
  'status-degraded': 'text-status-degraded',
  'status-pending': 'text-status-pending',
};

export type IconSize = '2xs' | 'xs' | 'sm' | 'md' | 'lg' | 'xl';
export type IconContainerVariant = 'accent' | 'surface' | 'surface-subtle' | 'muted';

export interface IconProps {
  /** Icon to render — must exist in the Hades registry. */
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
   * When `true`, renders the container as a circle (`rounded-full`) instead
   * of the default size-proportionate corner radius. Use for empty-state icons
   * and avatar-style feature icons.
   *
   * ```tsx
   * <Icon name="bot" size="md" container containerVariant="surface-subtle" circle />
   * ```
   */
  circle?: boolean;

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
   * Rotation angle applied to the icon wrapper span.
   * Use for animated chevrons (accordion toggles, collapsible rows) where
   * the icon must rotate to indicate open/closed state without a separate
   * layout wrapper.
   *
   * ```tsx
   * <Icon name="chevron-right" size="2xs" rotate={open ? 90 : 0} />
   * ```
   */
  rotate?: 0 | 45 | 90 | 135 | 180 | 270;

  /**
   * When true, adds `shrink-0` to the wrapper span so this icon does not
   * compress inside a flex row when sibling elements compete for space.
   */
  shrink?: boolean;

  /**
   * When true, applies `cursor-grab` (and `active:cursor-grabbing`) to the
   * wrapper span. Use on grip/drag-handle icons to signal draggability.
   */
  cursorGrab?: boolean;

  /**
   * When true, the icon wrapper is initially invisible and fades in when
   * an ancestor element with the Tailwind `group` class is hovered.
   * Use this for "copy" / "action" icons inside hover-reveal rows.
   *
   * ```tsx
   * // parent must have className="group" (or similar)
   * <Icon name="copy" size="2xs" color="muted" groupReveal />
   * ```
   */
  groupReveal?: boolean;

  /**
   * className is intentionally not exposed. Size and colour are controlled
   * via typed props to keep icons consistent across the product.
   *
   * If you genuinely need a one-off style, open a Hades PR to add a prop.
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
      circle = false,
      color,
      filled = false,
      label,
      rotate,
      shrink = false,
      cursorGrab = false,
      groupReveal = false,
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

    const svgEl = <LucideIcon className={svgClasses} aria-hidden={true} data-slot="icon" />;

    if (!container) {
      const needsBox = rotate !== undefined || groupReveal || shrink || cursorGrab;
      const wrapperStyle: React.CSSProperties = needsBox
        ? {
            display: 'inline-flex',
            alignItems: 'center',
            ...(rotate !== undefined && {
              transform: `rotate(${rotate}deg)`,
              transition: 'transform 150ms',
            }),
          }
        : { display: 'contents' };

      return (
        <span
          ref={ref}
          data-slot="icon-wrapper"
          aria-hidden={label ? undefined : true}
          aria-label={label}
          role={label ? 'img' : undefined}
          className={
            cn(
              groupReveal && 'opacity-0 transition-opacity group-hover:opacity-100',
              shrink && 'shrink-0',
              cursorGrab && 'cursor-grab active:cursor-grabbing',
            ) || undefined
          }
          style={wrapperStyle}
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
        role={label ? 'img' : undefined}
        className={cn(
          'inline-flex shrink-0 items-center justify-center',
          CONTAINER_SIZE[size],
          circle ? 'rounded-full' : CONTAINER_RADIUS[size],
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
