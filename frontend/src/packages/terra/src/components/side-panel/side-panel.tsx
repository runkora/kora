'use client';

import * as React from 'react';
import * as DialogPrimitive from '@radix-ui/react-dialog';
import { cn } from '../../utils';
import { Heading, Text } from '../typography';
import { Icon } from '../icon';

// ── Size lookup ───────────────────────────────────────────────────────────────
// Full string literals required — Tailwind v4 static scanner cannot handle
// dynamic class construction.

const PANEL_SIZE: Record<Required<SidePanelProps>['size'], string> = {
  sm: 'w-[440px]',
  md: 'w-[580px]',
  lg: 'w-[720px]',
};

// ── SidePanel ─────────────────────────────────────────────────────────────────

export interface SidePanelProps {
  /** Whether the panel is open. */
  open: boolean;
  /** Called when the panel requests to close (Escape key, overlay click, or
   *  the built-in close button in SidePanelHeader). */
  onClose: () => void;
  /**
   * Width of the panel.
   * - `'sm'` — 440 px
   * - `'md'` — 580 px — default
   * - `'lg'` — 720 px
   */
  size?: 'sm' | 'md' | 'lg';
  /**
   * When `false`, clicking the overlay will NOT close the panel.
   * Use for flows where the user must take an explicit action before
   * dismissing (e.g. unsaved-changes guards).
   * Default: `true`.
   */
  dismissOnOutsideClick?: boolean;
  children: React.ReactNode;
}

/**
 * SidePanel
 *
 * A slide-over panel from the right edge of the viewport. Built on Radix UI's
 * Dialog primitive — handles keyboard (Escape), overlay-click dismissal, focus
 * trap, and ARIA automatically.
 *
 * Compose with `SidePanelHeader`, `SidePanelBody`, and `SidePanelFooter`:
 *
 * ```tsx
 * <SidePanel open={isOpen} onClose={() => setOpen(false)}>
 *   <SidePanelHeader title="Edit Virtual Model" />
 *   <SidePanelBody>
 *     <FormField label="Name" id="name"><Input id="name" /></FormField>
 *   </SidePanelBody>
 *   <SidePanelFooter>
 *     <Button variant="outline" size="sm" onClick={() => setOpen(false)}>Cancel</Button>
 *     <Button size="sm" type="submit">Save</Button>
 *   </SidePanelFooter>
 * </SidePanel>
 * ```
 *
 * **Accessibility**: `SidePanelHeader` renders the required `DialogTitle`. If you
 * omit `SidePanelHeader`, render `<SidePanelAccessibleTitle>` manually.
 */
const SidePanel = ({
  open,
  onClose,
  size = 'md',
  dismissOnOutsideClick = true,
  children,
}: SidePanelProps) => (
  <DialogPrimitive.Root
    open={open}
    onOpenChange={(isOpen) => {
      if (!isOpen) onClose();
    }}
  >
    <DialogPrimitive.Portal>
      <DialogPrimitive.Overlay
        className={cn(
          'fixed inset-0 z-40 bg-black/40',
          'data-[state=open]:animate-in data-[state=closed]:animate-out',
          'data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0',
          'duration-200',
        )}
      />
      <DialogPrimitive.Content
        asChild
        onPointerDownOutside={(e) => {
          if (!dismissOnOutsideClick) e.preventDefault();
        }}
        className={cn(
          // Positioning — right-side slide panel
          'fixed inset-y-0 right-0 z-50',
          // Layout
          'flex flex-col',
          // Visual
          'bg-surface shadow-2xl border-l border-border',
          // Animation — slide from right
          'data-[state=open]:animate-in data-[state=closed]:animate-out',
          'data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0',
          'data-[state=closed]:slide-out-to-right-full data-[state=open]:slide-in-from-right-full',
          'duration-300',
          PANEL_SIZE[size],
        )}
      >
        <aside data-slot="side-panel">{children}</aside>
      </DialogPrimitive.Content>
    </DialogPrimitive.Portal>
  </DialogPrimitive.Root>
);
SidePanel.displayName = 'SidePanel';

// ── SidePanelHeader ───────────────────────────────────────────────────────────

export interface SidePanelHeaderProps {
  /** Required — rendered as a Heading and satisfies the Radix DialogTitle requirement. */
  title: string;
  /** Optional subtitle rendered below the title as muted supporting text. */
  description?: string;
  /** When `false`, the built-in close (×) button is hidden. Default: `true`. */
  showClose?: boolean;
}

const SidePanelHeader = ({ title, description, showClose = true }: SidePanelHeaderProps) => (
  <div
    data-slot="side-panel-header"
    className="flex items-start justify-between border-b border-border px-5 py-4 shrink-0"
  >
    <div className="flex flex-col gap-0.5">
      <DialogPrimitive.Title asChild>
        <Heading variant="section">{title}</Heading>
      </DialogPrimitive.Title>
      {description && (
        <DialogPrimitive.Description asChild>
          <Text size="supporting" color="muted">
            {description}
          </Text>
        </DialogPrimitive.Description>
      )}
    </div>
    {showClose && (
      <DialogPrimitive.Close asChild>
        <button
          type="button"
          aria-label="Close"
          className={cn(
            'inline-flex h-7 w-7 shrink-0 items-center justify-center rounded-md',
            'border-transparent bg-transparent text-muted',
            'transition-colors hover:bg-surface-overlay hover:text-foreground',
            'focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent',
          )}
        >
          <Icon name="close" size="xs" />
        </button>
      </DialogPrimitive.Close>
    )}
  </div>
);
SidePanelHeader.displayName = 'SidePanelHeader';

// ── SidePanelBody ─────────────────────────────────────────────────────────────

export interface SidePanelBodyProps {
  children: React.ReactNode;
  /**
   * When `true`, removes horizontal padding — use when the body content
   * (e.g. a full-width list or table) should reach the panel edges.
   */
  flush?: boolean;
  className?: string;
}

const SidePanelBody = ({ children, flush, className }: SidePanelBodyProps) => (
  <div
    data-slot="side-panel-body"
    className={cn('flex-1 overflow-y-auto', flush ? '' : 'px-5 py-4', className)}
  >
    {children}
  </div>
);
SidePanelBody.displayName = 'SidePanelBody';

// ── SidePanelFooter ───────────────────────────────────────────────────────────

export interface SidePanelFooterProps {
  children: React.ReactNode;
  /** Button alignment. Default: `'end'`. */
  justify?: 'start' | 'end' | 'between';
}

const JUSTIFY_CLASSES: Record<Required<SidePanelFooterProps>['justify'], string> = {
  start: 'justify-start',
  end: 'justify-end',
  between: 'justify-between',
};

const SidePanelFooter = ({ children, justify = 'end' }: SidePanelFooterProps) => (
  <div
    data-slot="side-panel-footer"
    className={cn(
      'flex items-center gap-3 border-t border-border px-5 py-4 shrink-0',
      JUSTIFY_CLASSES[justify],
    )}
  >
    {children}
  </div>
);
SidePanelFooter.displayName = 'SidePanelFooter';

// ── SidePanelAccessibleTitle ──────────────────────────────────────────────────

/**
 * Visually-hidden DialogTitle for panels with custom header layouts.
 * Use when `SidePanelHeader` is omitted.
 */
const SidePanelAccessibleTitle = React.forwardRef<
  HTMLHeadingElement,
  React.HTMLAttributes<HTMLHeadingElement>
>((props, ref) => (
  <DialogPrimitive.Title asChild>
    <h2 ref={ref} className="sr-only" {...props} />
  </DialogPrimitive.Title>
));
SidePanelAccessibleTitle.displayName = 'SidePanelAccessibleTitle';

export { SidePanel, SidePanelHeader, SidePanelBody, SidePanelFooter, SidePanelAccessibleTitle };
