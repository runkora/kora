import * as React from 'react';
import { cn } from '../../utils';

/**
 * Show
 *
 * Responsive visibility primitive. Replaces raw `hidden sm:block` wrapper
 * divs with a semantically explicit component that makes intent clear.
 *
 * Exactly one of `above` or `below` must be provided.
 *
 * ## Show above a breakpoint (hidden on small, visible on larger)
 *
 * ```tsx
 * {/* Keyboard hint — only visible on sm and above *\/}
 * <Show above="sm">
 *   <Text size="caption" color="muted">⌘↵ to send</Text>
 * </Show>
 * ```
 *
 * ## Show below a breakpoint (visible on small, hidden on larger)
 *
 * ```tsx
 * {/* Mobile nav — only visible below md *\/}
 * <Show below="md">
 *   <MobileNav />
 * </Show>
 * ```
 *
 * ## Implementation note
 *
 * Class strings are written as full static literals (no template interpolation)
 * to satisfy the Tailwind v4 static-scanner requirement. All classes must
 * appear verbatim in source.
 */

// ── Breakpoints ───────────────────────────────────────────────────────────────

type Breakpoint = 'sm' | 'md' | 'lg' | 'xl';

/**
 * Full static class strings — no template literals.
 * Tailwind v4's scanner requires every class to appear verbatim in source.
 */

/** Hidden by default, shown at the given breakpoint and above. */
const SHOW_ABOVE: Record<Breakpoint, string> = {
  sm: 'hidden sm:block',
  md: 'hidden md:block',
  lg: 'hidden lg:block',
  xl: 'hidden xl:block',
};

/** Shown by default, hidden at the given breakpoint and above. */
const SHOW_BELOW: Record<Breakpoint, string> = {
  sm: 'block sm:hidden',
  md: 'block md:hidden',
  lg: 'block lg:hidden',
  xl: 'block xl:hidden',
};

// ── Props ─────────────────────────────────────────────────────────────────────

interface ShowAboveProps {
  /**
   * Show children at this breakpoint and above; hide on narrower viewports.
   * Cannot be combined with `below`.
   */
  above: Breakpoint;
  below?: never;
  children: React.ReactNode;
}

interface ShowBelowProps {
  /**
   * Show children below this breakpoint; hide at this breakpoint and above.
   * Cannot be combined with `above`.
   */
  below: Breakpoint;
  above?: never;
  children: React.ReactNode;
}

type ShowProps = ShowAboveProps | ShowBelowProps;

// ── Component ─────────────────────────────────────────────────────────────────

/**
 * Show — responsive visibility wrapper.
 *
 * Renders a `<div>` with the appropriate Tailwind responsive visibility
 * class. The div is intentionally unstyled beyond the visibility class —
 * it contributes no layout, spacing, or colour of its own.
 */
const Show = React.forwardRef<HTMLDivElement, ShowProps>(({ above, below, children }, ref) => {
  const visibilityClass = above !== undefined ? SHOW_ABOVE[above] : SHOW_BELOW[below!];

  return (
    <div ref={ref} className={cn(visibilityClass)}>
      {children}
    </div>
  );
});
Show.displayName = 'Show';

// ── Exports ───────────────────────────────────────────────────────────────────

export { Show };
export type { ShowProps, Breakpoint };
