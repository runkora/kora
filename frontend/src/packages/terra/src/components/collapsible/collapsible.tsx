'use client';

import * as React from 'react';
import * as CollapsiblePrimitive from '@radix-ui/react-collapsible';

/**
 * Collapsible
 *
 * An expandable/collapsible region built on the Radix UI Collapsible
 * primitive for correct `aria-expanded` / `aria-controls` wiring and
 * keyboard support. Terra adds only `data-slot` hooks — visual treatment
 * belongs to the composition (e.g. a tool tree row that expands per server).
 *
 * Pass `defaultOpen` for uncontrolled usage or `open` + `onOpenChange`
 * for controlled usage.
 *
 * ```tsx
 * <Collapsible defaultOpen>
 *   <CollapsibleTrigger asChild>
 *     <Button variant="ghost" size="sm">github-mcp</Button>
 *   </CollapsibleTrigger>
 *   <CollapsibleContent>…tool rows…</CollapsibleContent>
 * </Collapsible>
 * ```
 */
const Collapsible = React.forwardRef<
  React.ComponentRef<typeof CollapsiblePrimitive.Root>,
  React.ComponentPropsWithoutRef<typeof CollapsiblePrimitive.Root>
>((props, ref) => <CollapsiblePrimitive.Root ref={ref} data-slot="collapsible" {...props} />);
Collapsible.displayName = 'Collapsible';

/**
 * CollapsibleTrigger
 *
 * The control that toggles the collapsible region. Use `asChild` to render
 * an existing interactive element (usually a `Button`) as the trigger.
 */
const CollapsibleTrigger = React.forwardRef<
  React.ComponentRef<typeof CollapsiblePrimitive.Trigger>,
  React.ComponentPropsWithoutRef<typeof CollapsiblePrimitive.Trigger>
>((props, ref) => (
  <CollapsiblePrimitive.Trigger ref={ref} data-slot="collapsible-trigger" {...props} />
));
CollapsibleTrigger.displayName = 'CollapsibleTrigger';

/**
 * CollapsibleContent
 *
 * The region shown while the collapsible is open. Radix unmounts nothing —
 * it hides the element — so `data-state="open" | "closed"` is available for
 * styling in compositions.
 */
const CollapsibleContent = React.forwardRef<
  React.ComponentRef<typeof CollapsiblePrimitive.Content>,
  React.ComponentPropsWithoutRef<typeof CollapsiblePrimitive.Content>
>((props, ref) => (
  <CollapsiblePrimitive.Content ref={ref} data-slot="collapsible-content" {...props} />
));
CollapsibleContent.displayName = 'CollapsibleContent';

export type CollapsibleProps = React.ComponentPropsWithoutRef<typeof CollapsiblePrimitive.Root>;
export type CollapsibleTriggerProps = React.ComponentPropsWithoutRef<
  typeof CollapsiblePrimitive.Trigger
>;
export type CollapsibleContentProps = React.ComponentPropsWithoutRef<
  typeof CollapsiblePrimitive.Content
>;

export { Collapsible, CollapsibleTrigger, CollapsibleContent };
