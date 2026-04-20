import * as React from 'react';
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
  extends
    React.HTMLAttributes<HTMLElement>,
    React.RefAttributes<HTMLElement>,
    VariantProps<typeof navItemVariants> {
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
   * When true, merges NavItem styles into the immediate child element and
   * composes icon + label + badge as that child's content. Use with Next.js
   * `<Link>` or `<a>` for client-side routing.
   *
   * NOTE: We do NOT use Radix `Slot` here because Slot calls
   * `React.Children.only` on NavItem's children, which throws when we render
   * the icon-span / label-span / badge-span triple. Instead we use
   * `React.cloneElement` directly so that all three children can be composed
   * freely while still delegating the root element to the caller.
   *
   * ```tsx
   * <NavItem state="active" asChild>
   *   <a href="/agents">Agents</a>
   * </NavItem>
   * ```
   */
  asChild?: boolean;
}

/**
 * NavItem
 *
 * A sidebar navigation entry that carries icon, label, optional badge, and
 * active-state styling. Use `asChild` with Next.js `<Link>` or `<a>` to
 * preserve client-side routing while applying the full NavItem appearance.
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
 *   <a href="/agents">Agents</a>
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
function NavItem({
  className,
  state,
  size,
  icon,
  badge,
  asChild = false,
  children,
  ref,
  ...props
}: NavItemProps) {
  const itemClass = cn(navItemVariants({ state, size }), className);

  // Composes icon + label + badge around a given label node.
  const composeContent = (label: React.ReactNode) => (
    <>
      {icon && (
        <div aria-hidden="true" className="shrink-0">
          {icon}
        </div>
      )}
      <div className="flex-1 truncate">{label}</div>
      {badge !== undefined && badge !== null && (
        <div
          aria-label={`${badge} items`}
          className={cn(
            'ml-auto shrink-0 rounded-full px-1.5 py-0.5',
            'min-w-[1.25rem] text-center text-[10px] font-semibold leading-none tabular-nums',
            state === 'active' ? 'bg-accent/20 text-accent' : 'bg-surface-overlay text-muted',
          )}
        >
          {badge}
        </div>
      )}
    </>
  );

  // asChild: clone the single child, merge NavItem classes onto it, and
  // replace its children with the composed icon+label+badge content.
  // We avoid Radix Slot because Slot.render calls React.Children.only on
  // NavItem's own children — which throws when icon/label/badge are present
  // as three separate nodes.
  //
  // Note: `ref` is intentionally not forwarded in asChild mode — passing a
  // React.Ref value into cloneElement triggers the react-hooks/refs lint rule
  // ("cannot access refs during render"). In asChild mode the consumer owns
  // the element and can attach their own ref to it directly.
  if (asChild) {
    const child = React.Children.only(children) as React.ReactElement<
      React.HTMLAttributes<HTMLElement>
    >;
    return React.cloneElement(child, {
      className: cn(itemClass, child.props.className),
      ...(props as React.HTMLAttributes<HTMLElement>),
      children: composeContent(child.props.children),
    });
  }

  return (
    <button
      ref={ref as React.Ref<HTMLButtonElement>}
      className={itemClass}
      {...(props as React.HTMLAttributes<HTMLButtonElement>)}
    >
      {composeContent(children)}
    </button>
  );
}

NavItem.displayName = 'NavItem';

export { NavItem, navItemVariants };
