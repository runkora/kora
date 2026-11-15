'use client';

/** @tier 1 — Zero kora domain knowledge. A menu of actions that opens from a button. */

import * as React from 'react';
import * as DropdownMenuPrimitive from '@radix-ui/react-dropdown-menu';
import { cva, type VariantProps } from 'class-variance-authority';
import { cn } from '../../utils';

/**
 * DropdownMenu
 *
 * A list of actions behind a trigger, such as an overflow ("…") button. Built
 * on Radix `DropdownMenu`, so the menu role, arrow-key movement, typeahead,
 * Escape and focus return come with it. Use `Popover` for content that is not
 * a list of actions.
 *
 * ```tsx
 * <DropdownMenu>
 *   <DropdownMenuTrigger asChild>
 *     <Button size="icon-sm" variant="ghost" aria-label="More actions">
 *       <Icon name="more-horizontal" size="2xs" />
 *     </Button>
 *   </DropdownMenuTrigger>
 *   <DropdownMenuContent>
 *     <DropdownMenuItem asChild>
 *       <Link href={traceUrl}>Trace</Link>
 *     </DropdownMenuItem>
 *   </DropdownMenuContent>
 * </DropdownMenu>
 * ```
 */
const DropdownMenu = DropdownMenuPrimitive.Root;
const DropdownMenuTrigger = DropdownMenuPrimitive.Trigger;

export type DropdownMenuContentProps = React.ComponentPropsWithoutRef<
  typeof DropdownMenuPrimitive.Content
>;

const DropdownMenuContent = React.forwardRef<
  React.ComponentRef<typeof DropdownMenuPrimitive.Content>,
  DropdownMenuContentProps
>(({ className, align = 'start', sideOffset = 6, ...props }, ref) => (
  <DropdownMenuPrimitive.Portal>
    <DropdownMenuPrimitive.Content
      ref={ref}
      data-slot="dropdown-menu-content"
      align={align}
      sideOffset={sideOffset}
      className={cn(
        'z-50 min-w-[10rem] rounded-md border border-border bg-surface-elevated p-1 shadow-lg',
        'data-[state=open]:animate-in data-[state=closed]:animate-out',
        'data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0',
        'data-[state=closed]:zoom-out-95 data-[state=open]:zoom-in-95',
        'data-[side=bottom]:slide-in-from-top-2 data-[side=top]:slide-in-from-bottom-2',
        className,
      )}
      {...props}
    />
  </DropdownMenuPrimitive.Portal>
));
DropdownMenuContent.displayName = 'DropdownMenuContent';

const dropdownMenuItemVariants = cva(
  [
    'flex cursor-default select-none items-center gap-2 rounded-sm px-2 py-1.5 text-sm outline-none',
    'data-[highlighted]:bg-surface-overlay data-[disabled]:pointer-events-none data-[disabled]:opacity-50',
    '[&_svg]:size-3.5 [&_svg]:shrink-0',
  ],
  {
    variants: {
      tone: {
        default: 'text-foreground',
        danger: 'text-status-error',
      },
    },
    defaultVariants: { tone: 'default' },
  },
);

export interface DropdownMenuItemProps
  extends
    React.ComponentPropsWithoutRef<typeof DropdownMenuPrimitive.Item>,
    VariantProps<typeof dropdownMenuItemVariants> {}

/** One action. `asChild` renders a link or another element as the item. */
const DropdownMenuItem = React.forwardRef<
  React.ComponentRef<typeof DropdownMenuPrimitive.Item>,
  DropdownMenuItemProps
>(({ className, tone, ...props }, ref) => (
  <DropdownMenuPrimitive.Item
    ref={ref}
    data-slot="dropdown-menu-item"
    className={cn(dropdownMenuItemVariants({ tone }), className)}
    {...props}
  />
));
DropdownMenuItem.displayName = 'DropdownMenuItem';

export { DropdownMenu, DropdownMenuTrigger, DropdownMenuContent, DropdownMenuItem };
