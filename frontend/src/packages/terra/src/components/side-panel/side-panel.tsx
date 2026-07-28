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

const HEADER_DENSITY: Record<Required<SidePanelHeaderProps>['density'], string> = {
  default: 'items-start px-5 py-4',
  compact: 'h-[60px] items-center px-[18px] gap-3',
};

const HEADER_TITLE_DENSITY: Record<Required<SidePanelHeaderProps>['density'], string> = {
  default: '',
  compact: 'font-mono text-[14px] font-semibold leading-none',
};

const HEADER_DESCRIPTION_DENSITY: Record<Required<SidePanelHeaderProps>['density'], string> = {
  default: '',
  compact: 'text-[11.5px]',
};

const BODY_PADDING: Record<Required<SidePanelBodyProps>['padding'], string> = {
  default: 'px-5 py-4',
  compact: 'p-[18px]',
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
   * Exact panel width (px or any CSS length). Overrides `size` when set — use
   * when a design spec calls for a specific width rather than a size token.
   */
  width?: number | string;
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
  width,
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
          'fixed inset-0 z-40 bg-overlay',
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
          width === undefined && PANEL_SIZE[size],
        )}
        style={width === undefined ? undefined : { width }}
      >
        <aside data-slot="side-panel" className="flex h-full flex-col">
          {children}
        </aside>
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
  /**
   * Controls placed beside the close button: a state toggle, a primary action.
   *
   * For the one control that defines whether the panel's subject is live at
   * all. Anything a reader would have to scroll to find does not belong here.
   */
  actions?: React.ReactNode;
  /**
   * Rendered before the title block — an avatar, status dot, or provider mark.
   * Never shrinks. Nothing is rendered when omitted.
   */
  leading?: React.ReactNode;
  /**
   * Header density.
   * - `'default'` — 20 px inline / 16 px block padding, section-scale title.
   * - `'compact'` — a fixed 60 px rail: `0 18px` padding, 12 px gaps, a 14 px
   *   mono title and 11.5 px description. Use when the panel is a dense
   *   inspector rather than a form.
   */
  density?: 'default' | 'compact';
}

const SidePanelHeader = ({
  title,
  description,
  showClose = true,
  actions,
  leading,
  density = 'default',
}: SidePanelHeaderProps) => {
  const titleBlock = (
    <div className="flex flex-col gap-0.5">
      <DialogPrimitive.Title asChild>
        <Heading variant="section" className={HEADER_TITLE_DENSITY[density]}>
          {title}
        </Heading>
      </DialogPrimitive.Title>
      {description && (
        <DialogPrimitive.Description asChild>
          <Text size="supporting" color="muted" className={HEADER_DESCRIPTION_DENSITY[density]}>
            {description}
          </Text>
        </DialogPrimitive.Description>
      )}
    </div>
  );

  return (
    <div
      data-slot="side-panel-header"
      className={cn(
        'flex justify-between border-b border-border shrink-0',
        HEADER_DENSITY[density],
      )}
    >
      {leading ? (
        <div className={cn('flex gap-3', density === 'compact' ? 'items-center' : 'items-start')}>
          <div data-slot="side-panel-header-leading" className="flex shrink-0 items-center">
            {leading}
          </div>
          {titleBlock}
        </div>
      ) : (
        titleBlock
      )}
      <div className="flex shrink-0 items-center gap-2">
        {actions}
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
    </div>
  );
};
SidePanelHeader.displayName = 'SidePanelHeader';

// ── SidePanelBody ─────────────────────────────────────────────────────────────

export interface SidePanelBodyProps {
  children: React.ReactNode;
  /**
   * When `true`, removes horizontal padding — use when the body content
   * (e.g. a full-width list or table) should reach the panel edges.
   */
  flush?: boolean;
  /**
   * Body padding.
   * - `'default'` — 20 px inline / 16 px block.
   * - `'compact'` — 18 px on all four sides, matching a compact header rail.
   *
   * Ignored when `flush` is set.
   */
  padding?: 'default' | 'compact';
  className?: string;
}

const SidePanelBody = ({ children, flush, padding = 'default', className }: SidePanelBodyProps) => (
  <div
    data-slot="side-panel-body"
    className={cn('flex-1 overflow-y-auto', flush ? '' : BODY_PADDING[padding], className)}
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
