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
 * ## Inline content
 *
 * Pass `display="inline"` when the content must stay inline (e.g. inside a
 * sentence). The visibility class switches from `block` to `inline` so the
 * element's display model is preserved when visible, and a `<span>` is
 * rendered instead of a `<div>`.
 *
 * ```tsx
 * <Show above="md" display="inline">
 *   <Text as="span" size="micro" color="subtle">Shift + Enter for new line</Text>
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

/** Hidden by default, shown (block) at the given breakpoint and above. */
const SHOW_ABOVE: Record<Breakpoint, string> = {
  sm: 'hidden sm:block',
  md: 'hidden md:block',
  lg: 'hidden lg:block',
  xl: 'hidden xl:block',
};

/** Shown (block) by default, hidden at the given breakpoint and above. */
const SHOW_BELOW: Record<Breakpoint, string> = {
  sm: 'block sm:hidden',
  md: 'block md:hidden',
  lg: 'block lg:hidden',
  xl: 'block xl:hidden',
};

/** Hidden by default, shown (inline) at the given breakpoint and above. */
const SHOW_ABOVE_INLINE: Record<Breakpoint, string> = {
  sm: 'hidden sm:inline',
  md: 'hidden md:inline',
  lg: 'hidden lg:inline',
  xl: 'hidden xl:inline',
};

/** Shown (inline) by default, hidden at the given breakpoint and above. */
const SHOW_BELOW_INLINE: Record<Breakpoint, string> = {
  sm: 'inline sm:hidden',
  md: 'inline md:hidden',
  lg: 'inline lg:hidden',
  xl: 'inline xl:hidden',
};

// ── Props ─────────────────────────────────────────────────────────────────────

interface ShowBaseProps {
  /**
   * Controls the CSS `display` value when the element is visible, and which
   * HTML element is rendered.
   *
   * - `'block'` (default) — renders a `<div>`; use for standalone sections,
   *   navigation, and layout blocks.
   * - `'inline'` — renders a `<span>`; use when content appears inside a text
   *   flow or another inline context where a block element would break layout.
   */
  display?: 'block' | 'inline';
  children: React.ReactNode;
}

interface ShowAboveProps extends ShowBaseProps {
  above: Breakpoint;
  below?: never;
}

interface ShowBelowProps extends ShowBaseProps {
  below: Breakpoint;
  above?: never;
}

type ShowProps = ShowAboveProps | ShowBelowProps;

// ── Component ─────────────────────────────────────────────────────────────────

/**
 * Show — responsive visibility wrapper.
 *
 * Renders a `<div>` (or `<span>` when `display="inline"`) with the
 * appropriate Tailwind responsive visibility class. The element contributes
 * no layout, spacing, or colour of its own beyond the visibility toggle.
 */
const Show = React.forwardRef<HTMLDivElement | HTMLSpanElement, ShowProps>(
  ({ display = 'block', above, below, children }, ref) => {
    const isInline = display === 'inline';
    const visibilityClass =
      above !== undefined
        ? isInline
          ? SHOW_ABOVE_INLINE[above]
          : SHOW_ABOVE[above]
        : isInline
          ? SHOW_BELOW_INLINE[below!]
          : SHOW_BELOW[below!];

    const As = isInline ? 'span' : 'div';

    return (
      <As ref={ref as React.Ref<HTMLDivElement>} className={cn(visibilityClass)}>
        {children}
      </As>
    );
  },
);
Show.displayName = 'Show';

// ── Exports ───────────────────────────────────────────────────────────────────

export { Show };
export type { ShowProps, Breakpoint };
