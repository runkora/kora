'use client';

import * as React from 'react';
import * as TabsPrimitive from '@radix-ui/react-tabs';
import { cn } from '../../utils';
import { Icon } from '../icon/icon';
import type { IconName } from '../icon/registry';

/** @tier 1 — Zero Orbit domain knowledge. Layered tab panels behind a shared trigger rail. */

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

// ─── Size + fill context ──────────────────────────────────────────────────────

type TabsSize = 'default' | 'compact';

interface TabsContextValue {
  size: TabsSize;
  fill: boolean;
}

/**
 * Carries the root's `size` and `fill` settings down to `TabsList`,
 * `TabsTrigger`, and `TabsContent` so a consumer sets density once on the
 * root rather than on every trigger.
 */
const TabsContext = React.createContext<TabsContextValue>({ size: 'default', fill: false });

// ── Size lookups ──────────────────────────────────────────────────────────────
// Full string literals required — Tailwind v4's static scanner cannot handle
// dynamic class construction.

const LIST_SIZE: Record<TabsSize, string> = {
  default: '',
  compact: 'px-[18px] gap-[2px]',
};

const TRIGGER_SIZE: Record<TabsSize, string> = {
  default: 'px-4 pb-2.5 pt-2 text-sm font-medium',
  compact:
    'inline-flex h-[38px] items-center px-3 text-[13px] font-medium data-[state=active]:font-semibold',
};

// ─── Root ─────────────────────────────────────────────────────────────────────

export interface TabsProps extends React.ComponentPropsWithoutRef<typeof TabsPrimitive.Root> {
  /**
   * Density of the tab rail, applied to every `TabsList` and `TabsTrigger`
   * beneath this root.
   *
   * - `'default'` — 14 px triggers on a comfortable rail.
   * - `'compact'` — a 38 px trigger row at 13 px, inset `0 18px` with 2 px
   *   between triggers. For dense inspectors and side panels.
   */
  size?: TabsSize;
  /**
   * When `true`, the root becomes a flex column that fills its parent and the
   * active `TabsContent` scrolls internally. Use when the tab rail must sit
   * flush below a panel header and stay pinned while only the panel content
   * scrolls. Default: `false`.
   */
  fill?: boolean;
}

/**
 * Tabs root.
 *
 * Pass `defaultValue` for uncontrolled usage or `value` + `onValueChange`
 * for controlled usage. `size` and `fill` reach the sub-components through
 * context, so they are set once here.
 */
const Tabs = React.forwardRef<React.ElementRef<typeof TabsPrimitive.Root>, TabsProps>(
  ({ className, size = 'default', fill = false, ...props }, ref) => {
    const context = React.useMemo<TabsContextValue>(() => ({ size, fill }), [size, fill]);
    return (
      <TabsContext.Provider value={context}>
        <TabsPrimitive.Root
          ref={ref}
          className={cn(fill && 'flex flex-col flex-1 min-h-0', className)}
          {...props}
        />
      </TabsContext.Provider>
    );
  },
);
Tabs.displayName = TabsPrimitive.Root.displayName;

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
>(({ className, ...props }, ref) => {
  const { size } = React.useContext(TabsContext);
  return (
    <TabsPrimitive.List
      ref={ref}
      className={cn('flex items-end border-b border-border-subtle', LIST_SIZE[size], className)}
      {...props}
    />
  );
});
TabsList.displayName = TabsPrimitive.List.displayName;

// ─── TabsTrigger ──────────────────────────────────────────────────────────────

export interface TabsTriggerProps extends React.ComponentPropsWithoutRef<
  typeof TabsPrimitive.Trigger
> {
  /**
   * Optional leading icon, rendered at `size="xs"` before the label. When set,
   * the trigger switches to an inline-flex row with a 6 px gap; without it the
   * trigger renders exactly as before.
   *
   * ```tsx
   * <TabsTrigger value="settings" icon="settings">Settings</TabsTrigger>
   * ```
   */
  icon?: IconName;
}

/**
 * TabsTrigger
 *
 * An individual tab button. Each trigger must have a `value` prop that
 * matches the corresponding `TabsContent` `value`. Pass `icon` for a leading
 * icon rendered before the label.
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
  TabsTriggerProps
>(({ className, icon, children, ...props }, ref) => {
  const { size } = React.useContext(TabsContext);
  return (
    <TabsPrimitive.Trigger
      ref={ref}
      className={cn(
        // Layout — negative bottom margin bleeds the 2 px border over the list rail
        '-mb-px',
        // Size — padding, height, and type scale
        TRIGGER_SIZE[size],
        // Icon row — only when a leading icon is present so plain triggers render as before
        icon && 'inline-flex items-center gap-1.5',
        // Typography
        'leading-none whitespace-nowrap select-none',
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
    >
      {icon ? <Icon name={icon} size="xs" /> : null}
      {children}
    </TabsPrimitive.Trigger>
  );
});
TabsTrigger.displayName = TabsPrimitive.Trigger.displayName;

// ─── TabsContent ──────────────────────────────────────────────────────────────

/**
 * TabsContent
 *
 * The panel rendered when its associated TabsTrigger is active. Adds top
 * spacing so the content sits comfortably below the tab rail.
 *
 * Under a `fill` root it takes the remaining column height and scrolls
 * internally, so the rail above it stays pinned.
 */
const TabsContent = React.forwardRef<
  React.ElementRef<typeof TabsPrimitive.Content>,
  React.ComponentPropsWithoutRef<typeof TabsPrimitive.Content>
>(({ className, ...props }, ref) => {
  const { size, fill } = React.useContext(TabsContext);
  return (
    <TabsPrimitive.Content
      ref={ref}
      className={cn(
        // Compact rails live inside a padded container (e.g. SidePanelBody),
        // which already supplies the spacing below the rail; adding more here
        // would double it.
        size === 'compact' ? 'pt-0' : 'pt-6',
        fill && 'flex-1 min-h-0 overflow-y-auto',
        'focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent',
        className,
      )}
      {...props}
    />
  );
});
TabsContent.displayName = TabsPrimitive.Content.displayName;

// ─── Exports ──────────────────────────────────────────────────────────────────

export { Tabs, TabsList, TabsTrigger, TabsContent };
