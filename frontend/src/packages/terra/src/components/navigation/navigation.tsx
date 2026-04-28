import * as React from 'react';
import { cn } from '../../utils';
import { OVERFLOW_Y } from '../layout/sizing';
import type { OverflowValue } from '../layout/sizing';

// ── Props ─────────────────────────────────────────────────────────────────────

export interface NavigationProps {
  children: React.ReactNode;
  /**
   * Accessible label for the nav landmark. Distinguishes this navigation
   * from any other `<nav>` on the page for screen readers.
   *
   * @default 'Main'
   */
  'aria-label'?: string;
  /** @internal Used for internal Terra composition via cn(). */
  className?: string;
  id?: string;
  /** When `true`, adds `flex-1` so the nav grows to fill its container. */
  grow?: boolean;
  /**
   * `overflow-y` CSS property. Use `'auto'` to make the nav list scrollable
   * when its content is taller than the available space.
   */
  overflowY?: OverflowValue;
}

// ── Component ─────────────────────────────────────────────────────────────────

/**
 * Navigation
 *
 * A semantic `<nav>` landmark wrapper for the primary sidebar navigation
 * list. Renders the correct ARIA landmark role without requiring raw
 * `<nav>` in product code.
 *
 * ```tsx
 * <Navigation grow overflowY="auto">
 *   {NAV_GROUPS.map((group) => (
 *     <Stack key={group.label} gap="tight">
 *       {group.items.map((item) => (
 *         <NavItem key={item.href} state={...} asChild>
 *           <a href={item.href}>{item.label}</a>
 *         </NavItem>
 *       ))}
 *     </Stack>
 *   ))}
 * </Navigation>
 * ```
 */
const Navigation = React.forwardRef<HTMLElement, NavigationProps>(
  ({ children, 'aria-label': ariaLabel = 'Main', className, grow, overflowY, ...props }, ref) => (
    <nav
      ref={ref}
      aria-label={ariaLabel}
      data-slot="navigation"
      className={cn(grow && 'flex-1', overflowY && OVERFLOW_Y[overflowY], className)}
      {...props}
    >
      {children}
    </nav>
  ),
);

Navigation.displayName = 'Navigation';

export { Navigation };
