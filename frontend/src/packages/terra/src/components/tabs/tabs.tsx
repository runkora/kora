import * as React from 'react';
import * as TabsPrimitive from '@radix-ui/react-tabs';
import { cn } from '../../utils';

/**
 * Tabs
 *
 * A set of layered sections — "tab panels" — each associated with a trigger
 * in a shared tab list. Built on the Radix UI Tabs primitive for correct
 * `role="tablist"` / `role="tab"` / `role="tabpanel"` semantics and full
 * keyboard navigation (arrow keys, Home, End).
 *
 * The active tab is indicated by a 2 px accent underline that sits exactly on
 * top of the `TabsList` border, creating a seamless selected–unselected
 * visual separation without double borders.
 *
 * ```tsx
 * <Tabs defaultValue="spend">
 *   <TabsList>
 *     <TabsTrigger value="spend">Spend</TabsTrigger>
 *     <TabsTrigger value="tokens">Tokens</TabsTrigger>
 *   </TabsList>
 *   <TabsContent value="spend">…</TabsContent>
 *   <TabsContent value="tokens">…</TabsContent>
 * </Tabs>
 * ```
 */

// ─── Root ─────────────────────────────────────────────────────────────────────

/**
 * Tabs root — a thin alias over the Radix Root.
 *
 * Pass `defaultValue` for uncontrolled usage or `value` + `onValueChange`
 * for controlled usage.
 */
const Tabs = TabsPrimitive.Root;

// ─── TabsList ─────────────────────────────────────────────────────────────────

/**
 * TabsList
 *
 * The horizontal strip that houses `TabsTrigger` buttons. Renders a subtle
 * bottom border that the active trigger's underline indicator bleeds into,
 * creating the classic tab-rail appearance.
 */
const TabsList = React.forwardRef<
  React.ElementRef<typeof TabsPrimitive.List>,
  React.ComponentPropsWithoutRef<typeof TabsPrimitive.List>
>(({ className, ...props }, ref) => (
  <TabsPrimitive.List
    ref={ref}
    className={cn('flex items-end border-b border-border-subtle', className)}
    {...props}
  />
));
TabsList.displayName = TabsPrimitive.List.displayName;

// ─── TabsTrigger ──────────────────────────────────────────────────────────────

/**
 * TabsTrigger
 *
 * An individual tab button. Each trigger must have a `value` prop that
 * matches the corresponding `TabsContent` `value`.
 *
 * Visual states:
 *   - Inactive  — muted text, transparent underline border
 *   - Hover     — foreground text, subtle border hint
 *   - Active    — foreground text, 2 px accent bottom border overlapping
 *                 the TabsList border rail
 *   - Disabled  — 40 % opacity, pointer events off
 */
const TabsTrigger = React.forwardRef<
  React.ElementRef<typeof TabsPrimitive.Trigger>,
  React.ComponentPropsWithoutRef<typeof TabsPrimitive.Trigger>
>(({ className, ...props }, ref) => (
  <TabsPrimitive.Trigger
    ref={ref}
    className={cn(
      // Layout — negative bottom margin bleeds the 2 px border over the list rail
      '-mb-px px-4 pb-2.5 pt-2',
      // Typography
      'text-sm font-medium leading-none whitespace-nowrap select-none',
      // Colour + transition
      'text-subtle transition-colors duration-150',
      // Bottom-border indicator (always present; transparent when inactive)
      'border-b-2 border-transparent',
      // Hover
      'hover:text-foreground',
      // Focus ring
      'focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent',
      // Disabled
      'disabled:pointer-events-none disabled:opacity-40',
      // Active
      'data-[state=active]:text-foreground data-[state=active]:border-accent',
      className,
    )}
    {...props}
  />
));
TabsTrigger.displayName = TabsPrimitive.Trigger.displayName;

// ─── TabsContent ──────────────────────────────────────────────────────────────

/**
 * TabsContent
 *
 * The panel rendered when its associated TabsTrigger is active. Adds top
 * spacing so the content sits comfortably below the tab rail.
 */
const TabsContent = React.forwardRef<
  React.ElementRef<typeof TabsPrimitive.Content>,
  React.ComponentPropsWithoutRef<typeof TabsPrimitive.Content>
>(({ className, ...props }, ref) => (
  <TabsPrimitive.Content
    ref={ref}
    className={cn(
      'pt-6',
      'focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent',
      className,
    )}
    {...props}
  />
));
TabsContent.displayName = TabsPrimitive.Content.displayName;

// ─── Exports ──────────────────────────────────────────────────────────────────

export { Tabs, TabsList, TabsTrigger, TabsContent };
