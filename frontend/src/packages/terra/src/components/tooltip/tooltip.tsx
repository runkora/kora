import * as React from 'react';
import * as TooltipPrimitive from '@radix-ui/react-tooltip';
import { cn } from '../../utils';

/**
 * Tooltip
 *
 * Floating label for supplementary context on interactive elements.
 * Built on Radix UI Tooltip for correct ARIA semantics, keyboard
 * accessibility, and collision-aware positioning.
 *
 * Re-exports the Radix primitives (`TooltipProvider`, `TooltipTrigger`,
 * `TooltipContent`) alongside a convenience `Tooltip` root, so you can
 * compose both simple and complex tooltip patterns.
 *
 * **Always wrap your app (or Storybook decorator) in `<TooltipProvider>`.**
 *
 * ```tsx
 * <TooltipProvider>
 *   <Tooltip>
 *     <TooltipTrigger asChild>
 *       <Button variant="ghost" size="icon"><InfoIcon /></Button>
 *     </TooltipTrigger>
 *     <TooltipContent>View policy details</TooltipContent>
 *   </Tooltip>
 * </TooltipProvider>
 * ```
 */
const TooltipProvider = TooltipPrimitive.Provider;
const Tooltip = TooltipPrimitive.Root;
const TooltipTrigger = TooltipPrimitive.Trigger;

const TooltipContent = React.forwardRef<
  React.ElementRef<typeof TooltipPrimitive.Content>,
  React.ComponentPropsWithoutRef<typeof TooltipPrimitive.Content>
>(({ className, sideOffset = 6, ...props }, ref) => (
  <TooltipPrimitive.Portal>
    <TooltipPrimitive.Content
      ref={ref}
      sideOffset={sideOffset}
      className={cn(
        // Surface
        'z-50 overflow-hidden rounded-md',
        'bg-surface-elevated border border-border-subtle',
        // Typography
        'px-3 py-1.5 text-xs text-foreground leading-snug',
        // Animation — fade + slide via Tailwind + Radix data attrs
        'animate-in fade-in-0 zoom-in-95',
        'data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=closed]:zoom-out-95',
        'data-[side=bottom]:slide-in-from-top-2',
        'data-[side=left]:slide-in-from-right-2',
        'data-[side=right]:slide-in-from-left-2',
        'data-[side=top]:slide-in-from-bottom-2',
        className,
      )}
      {...props}
    />
  </TooltipPrimitive.Portal>
));

TooltipContent.displayName = TooltipPrimitive.Content.displayName;

export { Tooltip, TooltipProvider, TooltipTrigger, TooltipContent };
