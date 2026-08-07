/** @tier 1 — Zero Orbit domain knowledge. Fluid main + fixed sidebar two-column layout. */
import * as React from 'react';

import type { SpacingSize } from './spacing';
import { SIDEBAR_WIDTH_PX, type SidebarWidthToken } from './sizing';

// ── Gap pixel map ─────────────────────────────────────────────────────────────
// SpacingSize → pixel values. Mirrors the values in spacing.ts but as numbers
// for inline style use — Tailwind classes cannot be used in gridTemplateColumns.

const GAP_PX: Record<SpacingSize, number> = {
  none: 0,
  tight: 4,
  snug: 8,
  cozy: 12,
  comfortable: 16,
  relaxed: 24,
  loose: 32,
  spacious: 40,
};

// ── Types ─────────────────────────────────────────────────────────────────────

/**
 * The width of the fixed sidebar column.
 *
 * - A `SidebarWidthToken` (e.g. `"detail-panel"`) resolves to its named pixel
 *   value from the Terra sizing scale.
 * - A `number` is used as-is in pixels.
 * - A `string` (e.g. `"300px"`, `"20rem"`) is passed through verbatim as a CSS
 *   value — use only when no token fits.
 */
export type SidebarWidth = SidebarWidthToken | number | string;

export interface SidebarLayoutProps extends React.ComponentPropsWithoutRef<'div'> {
  /**
   * Width of the fixed sidebar column.
   * Use a `SidebarWidthToken` for token-governed widths; pass a number for ad-hoc
   * pixel values; pass a string for arbitrary CSS values (last resort).
   */
  sidebarWidth: SidebarWidth;
  /**
   * Which column is the fixed-width sidebar.
   * `"end"` (default) → `1fr <width>` (sidebar on the right).
   * `"start"` → `<width> 1fr` (sidebar on the left).
   */
  sidebarPosition?: 'start' | 'end';
  /** Column and row gap using the Terra spacing token scale. */
  gap?: SpacingSize;
  /** `align-items` value for both columns. Defaults to `"start"`. */
  align?: 'start' | 'center' | 'end' | 'stretch';
  className?: never;
}

export interface SidebarLayoutMainProps extends React.ComponentPropsWithoutRef<'div'> {
  className?: never;
}

export interface SidebarLayoutSidebarProps extends React.ComponentPropsWithoutRef<'div'> {
  className?: never;
}

// ── Helpers ───────────────────────────────────────────────────────────────────

function resolveSidebarWidth(width: SidebarWidth): string {
  if (typeof width === 'number') return `${width}px`;
  if (width in SIDEBAR_WIDTH_PX) return `${SIDEBAR_WIDTH_PX[width as SidebarWidthToken]}px`;
  return width;
}

function buildGridTemplateColumns(width: SidebarWidth, position: 'start' | 'end'): string {
  const resolved = resolveSidebarWidth(width);
  return position === 'start' ? `${resolved} 1fr` : `1fr ${resolved}`;
}

// ── Sub-components ────────────────────────────────────────────────────────────

const Main = React.forwardRef<HTMLDivElement, SidebarLayoutMainProps>(({ children, ...rest }, ref) => (
  <div ref={ref} data-slot="sidebar-layout-main" {...rest}>
    {children}
  </div>
));
Main.displayName = 'SidebarLayout.Main';

const Sidebar = React.forwardRef<HTMLDivElement, SidebarLayoutSidebarProps>(({ children, ...rest }, ref) => (
  <div ref={ref} data-slot="sidebar-layout-sidebar" {...rest}>
    {children}
  </div>
));
Sidebar.displayName = 'SidebarLayout.Sidebar';

// ── Root component ────────────────────────────────────────────────────────────

const SidebarLayoutRoot = React.forwardRef<HTMLDivElement, SidebarLayoutProps>(
  (
    {
      sidebarWidth,
      sidebarPosition = 'end',
      gap,
      align = 'start',
      style,
      children,
      ...rest
    },
    ref,
  ) => {
    const gridStyle: React.CSSProperties = {
      display: 'grid',
      gridTemplateColumns: buildGridTemplateColumns(sidebarWidth, sidebarPosition),
      alignItems: align,
      ...(gap !== undefined ? { gap: GAP_PX[gap] } : {}),
      ...style,
    };

    return (
      <div ref={ref} data-slot="sidebar-layout" style={gridStyle} {...rest}>
        {children}
      </div>
    );
  },
);
SidebarLayoutRoot.displayName = 'SidebarLayout';

// ── Composed export ───────────────────────────────────────────────────────────

export const SidebarLayout = Object.assign(SidebarLayoutRoot, {
  Main,
  Sidebar,
});
