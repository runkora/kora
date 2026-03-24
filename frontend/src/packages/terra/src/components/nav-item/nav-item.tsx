import * as React from 'react';
import { Slot } from '@radix-ui/react-slot';
import { cva, type VariantProps } from 'class-variance-authority';
import { cn } from '../../utils';

/**
 * NavItem variants — active, default, and disabled states across two sizes.
 *
 * The active state uses a left accent bar + tinted background to communicate
 * the current location in the control plane.
 *
 * Size variants:
 *   md — standard sidebar navigation (14px / py-2) — the default
 *   sm — compact sidebar lists such as conversation history (12px / py-1.5)
 */
const navItemVariants = cva(
  [
    'relative flex items-center gap-2.5 w-full',
    'rounded-md px-3',
    'font-medium leading-none',
    'transition-colors duration-150',
    'focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent',
    'select-none cursor-pointer',
    '[&_svg]:shrink-0 [&_svg]:pointer-events-none',
  ],
  {
    variants: {
      state: {
        /**
         * Currently active route — accent left bar + tinted background.
         * The pseudo-element bar is achieved with a thick left border on
         * a positioned child so it doesn't collapse the padding box.
         */
        active: [
          'bg-accent/10 text-accent',
          'before:absolute before:inset-y-1 before:left-0 before:w-0.5',
          'before:rounded-full before:bg-accent',
        ],

        /**
         * Default / inactive — ghost-like, brightens on hover.
         */
        default: 'text-muted hover:bg-surface-elevated hover:text-foreground',

        /**
         * Disabled — pointer events off, reduced opacity.
         */
        disabled: 'text-subtle opacity-50 pointer-events-none',
      },

      /**
       * Size — controls text scale, padding, and icon size.
       *
       *   md — standard sidebar navigation; use for primary nav links
       *   sm — compact lists such as chat history, search results, recent files;
       *         reduces vertical rhythm so more items fit without scrolling
       */
      size: {
        /** Standard sidebar navigation — 14px text, py-2, 16px icons. */
        md: 'py-2 text-sm [&_svg]:size-4',
        /** Compact list items — 12px text, py-1.5, 14px icons. */
        sm: 'py-1.5 text-xs [&_svg]:size-3.5',
      },
    },
    defaultVariants: {
      state: 'default',
      size: 'md',
    },
  },
);

export interface NavItemProps
  extends React.HTMLAttributes<HTMLElement>, VariantProps<typeof navItemVariants> {
  /**
   * Optional icon rendered before the label. Accepts any React element —
   * best paired with a Lucide icon sized to the NavItem's `size` variant.
   */
  icon?: React.ReactNode;

  /**
   * Optional counter badge rendered after the label (e.g. unread alerts).
   * Keep values short (< 3 digits) to avoid truncation.
   */
  badge?: React.ReactNode;

  /**
   * When true, applies NavItem styles to the immediate child element rather
   * than rendering a `<button>`. Use with Next.js `<Link>` for routing.
   *
   * ```tsx
   * <NavItem asChild state="active">
   *   <Link href="/agents">Agents</Link>
   * </NavItem>
   * ```
   */
  asChild?: boolean;
}

/**
 * NavItem
 *
 * A sidebar navigation entry that carries icon, label, optional badge, and
 * active-state styling. Use `asChild` with Next.js `<Link>` to preserve
 * client-side routing while applying the full NavItem appearance.
 *
 * The three states — `default`, `active`, `disabled` — map to the route's
 * relationship to the current location and its availability.
 *
 * Use `size="sm"` for compact lists such as conversation history or search
 * results where vertical density is more important than readability at a glance.
 *
 * Standard primary navigation:
 *
 * ```tsx
 * <NavItem icon={<CpuIcon />} state="active" asChild>
 *   <Link href="/agents">Agents</Link>
 * </NavItem>
 * ```
 *
 * Compact conversation list:
 *
 * ```tsx
 * <NavItem size="sm" icon={<MessageSquare />}>Policy review thread</NavItem>
 * ```
 *
 * With a counter badge:
 *
 * ```tsx
 * <NavItem icon={<KeyIcon />} badge={3}>Credentials</NavItem>
 * ```
 */
const NavItem = React.forwardRef<HTMLElement, NavItemProps>(
  ({ className, state, size, icon, badge, asChild = false, children, ...props }, ref) => {
    const Comp = asChild ? Slot : 'button';

    return (
      <Comp
        ref={ref as React.Ref<HTMLButtonElement>}
        className={cn(navItemVariants({ state, size }), className)}
        {...props}
      >
        {icon && (
          <span aria-hidden="true" className="shrink-0">
            {icon}
          </span>
        )}
        <span className="flex-1 truncate">{children}</span>
        {badge !== undefined && badge !== null && (
          <span
            aria-label={`${badge} items`}
            className={cn(
              'ml-auto shrink-0 rounded-full px-1.5 py-0.5',
              'min-w-[1.25rem] text-center text-[10px] font-semibold leading-none tabular-nums',
              state === 'active' ? 'bg-accent/20 text-accent' : 'bg-surface-overlay text-muted',
            )}
          >
            {badge}
          </span>
        )}
      </Comp>
    );
  },
);

NavItem.displayName = 'NavItem';

export { NavItem, navItemVariants };
