import * as React from 'react';
import { Slot } from '@radix-ui/react-slot';
import { cn } from '../../utils';
import { GAP, GAP_X, GAP_Y, resolveResponsive } from './spacing';
import { MAX_W, MIN_W, MAX_H, MIN_H } from './sizing';
import type { Responsive, SpacingSize, Breakpoint } from './spacing';
import type { ContainerWidth, ContainerHeight } from './sizing';

// ── Types ─────────────────────────────────────────────────────────────────────

/** Number of equal-width columns supported by the Grid component. */
export type ColCount = 1 | 2 | 3 | 4 | 5 | 6 | 12;

// ── Lookup table ──────────────────────────────────────────────────────────────
// Full string literals required — Tailwind v4's static scanner cannot handle
// template literals or dynamic class construction.

const COLS: Record<Breakpoint, Record<ColCount, string>> = {
  base: {
    1: 'grid-cols-1',
    2: 'grid-cols-2',
    3: 'grid-cols-3',
    4: 'grid-cols-4',
    5: 'grid-cols-5',
    6: 'grid-cols-6',
    12: 'grid-cols-12',
  },
  sm: {
    1: 'sm:grid-cols-1',
    2: 'sm:grid-cols-2',
    3: 'sm:grid-cols-3',
    4: 'sm:grid-cols-4',
    5: 'sm:grid-cols-5',
    6: 'sm:grid-cols-6',
    12: 'sm:grid-cols-12',
  },
  md: {
    1: 'md:grid-cols-1',
    2: 'md:grid-cols-2',
    3: 'md:grid-cols-3',
    4: 'md:grid-cols-4',
    5: 'md:grid-cols-5',
    6: 'md:grid-cols-6',
    12: 'md:grid-cols-12',
  },
  lg: {
    1: 'lg:grid-cols-1',
    2: 'lg:grid-cols-2',
    3: 'lg:grid-cols-3',
    4: 'lg:grid-cols-4',
    5: 'lg:grid-cols-5',
    6: 'lg:grid-cols-6',
    12: 'lg:grid-cols-12',
  },
  xl: {
    1: 'xl:grid-cols-1',
    2: 'xl:grid-cols-2',
    3: 'xl:grid-cols-3',
    4: 'xl:grid-cols-4',
    5: 'xl:grid-cols-5',
    6: 'xl:grid-cols-6',
    12: 'xl:grid-cols-12',
  },
};

// ── Props ─────────────────────────────────────────────────────────────────────

export interface GridProps extends React.HTMLAttributes<HTMLDivElement> {
  /**
   * Number of equal-width columns. Accepts a `Responsive` value so the
   * column count changes at each breakpoint. Mobile-first: always start with
   * a single column and add columns as the viewport widens.
   *
   * Default: `1`
   *
   * ```tsx
   * // Always 2 columns
   * <Grid cols={2}>…</Grid>
   *
   * // 1 col on mobile, 2 on small, 3 on large+
   * <Grid cols={{ base: 1, sm: 2, lg: 3 }}>…</Grid>
   * ```
   */
  cols?: Responsive<ColCount>;

  /**
   * Uniform gap between all cells. Overridden on each axis by `gapX` / `gapY`.
   * Accepts a `Responsive` value.
   *
   * Default: `'comfortable'`
   *
   * ```tsx
   * <Grid cols={3} gap="relaxed">…</Grid>
   * <Grid cols={{ base: 1, md: 3 }} gap={{ base: 'snug', md: 'relaxed' }}>…</Grid>
   * ```
   */
  gap?: Responsive<SpacingSize>;

  /**
   * Horizontal (column) gap — overrides the `gap` prop on the x-axis only.
   * Use when you want different row and column spacing.
   *
   * ```tsx
   * <Grid cols={3} gapX="loose" gapY="snug">…</Grid>
   * ```
   */
  gapX?: Responsive<SpacingSize>;

  /**
   * Vertical (row) gap — overrides the `gap` prop on the y-axis only.
   *
   * ```tsx
   * <Grid cols={3} gapX="loose" gapY="snug">…</Grid>
   * ```
   */
  gapY?: Responsive<SpacingSize>;

  /**
   * Maximum width constraint. Accepts a semantic `ContainerWidth` token
   * or a `Responsive` value for breakpoint-specific widths.
   *
   * ```tsx
   * // Constrain a stats grid to dashboard panel width
   * <Grid cols={3} maxWidth="wide">…</Grid>
   *
   * // Full-width on mobile, capped on large screens
   * <Grid cols={{ base: 1, lg: 3 }} maxWidth={{ base: 'full', lg: 'wide' }}>…</Grid>
   * ```
   */
  maxWidth?: Responsive<ContainerWidth>;

  /**
   * Minimum width constraint. Accepts a semantic `ContainerWidth` token
   * or a `Responsive` value.
   *
   * ```tsx
   * <Grid cols={2} minWidth="card">…</Grid>
   * ```
   */
  minWidth?: Responsive<ContainerWidth>;

  /**
   * Minimum height constraint. Accepts a semantic `ContainerHeight` token.
   *
   * `'screen'` uses `min-h-dvh` — dynamic viewport height, correct on mobile
   * browsers where the address bar resizes the visible area.
   *
   * ```tsx
   * <Grid minHeight="full">…</Grid>
   * ```
   */
  minHeight?: Responsive<ContainerHeight>;

  /**
   * Maximum height constraint. Accepts a semantic `ContainerHeight` token.
   *
   * ```tsx
   * <Grid maxHeight="screen" style={{ overflowY: 'auto' }}>…</Grid>
   * ```
   */
  maxHeight?: Responsive<ContainerHeight>;

  /**
   * When `true`, merges Grid's layout classes directly onto the immediate child
   * element via Radix Slot instead of rendering a wrapper `<div>`. Use this to
   * apply grid layout to semantic HTML elements without adding an extra DOM node.
   *
   * **Without `asChild`** — Grid renders a `<div>` that wraps your element:
   *
   * ```tsx
   * // DOM: <div class="grid grid-cols-2 gap-4"><ul>…</ul></div>
   * <Grid cols={2} gap="comfortable">
   *   <ul>{items.map(…)}</ul>
   * </Grid>
   * ```
   *
   * **With `asChild`** — grid classes land on `<ul>` directly, no extra div:
   *
   * ```tsx
   * // DOM: <ul class="grid grid-cols-2 gap-4">…</ul>
   * <Grid asChild cols={2} gap="comfortable">
   *   <ul>{items.map(…)}</ul>
   * </Grid>
   * ```
   *
   * Works with any single-child element: `<ul>`, `<ol>`, `<section>`, etc.
   */
  asChild?: boolean;
}

// ── Component ─────────────────────────────────────────────────────────────────

/**
 * Grid
 *
 * A CSS grid container for equal-column layouts. Replaces
 * `<div className="grid grid-cols-* gap-*">` with a composable,
 * fully responsive API.
 *
 * Always design mobile-first: `cols` defaults to 1 and should be widened at
 * larger breakpoints, not narrowed at smaller ones.
 *
 * For sequential layouts (flex columns/rows) use `Stack` instead.
 *
 * ```tsx
 * // Fixed 3-column stat card row
 * <Grid cols={3} gap="comfortable">
 *   <StatCard label="Agents" value="14" />
 *   <StatCard label="Policies" value="38" />
 *   <StatCard label="Events" value="4,821" />
 * </Grid>
 *
 * // Responsive: 1 col mobile → 2 col small → 3 col large
 * <Grid cols={{ base: 1, sm: 2, lg: 3 }} gap={{ base: 'snug', lg: 'comfortable' }}>
 *   {agents.map(a => <AgentCard key={a.id} agent={a} />)}
 * </Grid>
 *
 * // Tighter column gap, looser row gap
 * <Grid cols={4} gapX="snug" gapY="relaxed">…</Grid>
 *
 * // Constrained to dashboard panel width
 * <Grid cols={3} maxWidth="wide">…</Grid>
 *
 * // Without asChild — Grid renders a <div> that wraps the list
 * // DOM: <div class="grid grid-cols-1 md:grid-cols-2 gap-4"><ul>…</ul></div>
 * <Grid cols={{ base: 1, md: 2 }}>
 *   <ul>{items.map(i => <li key={i.id}>…</li>)}</ul>
 * </Grid>
 *
 * // With asChild — grid classes merge onto <ul> directly, no wrapper div
 * // DOM: <ul class="grid grid-cols-1 md:grid-cols-2 gap-4">…</ul>
 * <Grid asChild cols={{ base: 1, md: 2 }}>
 *   <ul>{items.map(i => <li key={i.id}>…</li>)}</ul>
 * </Grid>
 * ```
 */
const Grid = React.forwardRef<HTMLDivElement, GridProps>(
  (
    {
      className,
      cols = 1,
      gap = 'comfortable',
      gapX,
      gapY,
      maxWidth,
      minWidth,
      minHeight,
      maxHeight,
      asChild = false,
      ...props
    },
    ref,
  ) => {
    const Comp = (asChild ? Slot : 'div') as React.ElementType;
    return (
      <Comp
        ref={ref}
        className={cn(
          'grid',
          resolveResponsive(cols, COLS, 1),
          // gapX/gapY override the unified gap when present
          gapX || gapY
            ? cn(
                gapX
                  ? resolveResponsive(gapX, GAP_X, 'comfortable')
                  : resolveResponsive(gap, GAP_X, 'comfortable'),
                gapY
                  ? resolveResponsive(gapY, GAP_Y, 'comfortable')
                  : resolveResponsive(gap, GAP_Y, 'comfortable'),
              )
            : resolveResponsive(gap, GAP, 'comfortable'),
          maxWidth && resolveResponsive(maxWidth, MAX_W, 'full'),
          minWidth && resolveResponsive(minWidth, MIN_W, 'compact'),
          minHeight && resolveResponsive(minHeight, MIN_H, 'full'),
          maxHeight && resolveResponsive(maxHeight, MAX_H, 'full'),
          className,
        )}
        {...props}
      />
    );
  },
);

Grid.displayName = 'Grid';

export { Grid };
