'use client';

import * as React from 'react';
import * as DialogPrimitive from '@radix-ui/react-dialog';
import { cn } from '../../utils';
import { Heading, Text } from '../typography';
import { Icon } from '../icon';

// ── Size lookup ──────────────────────────────────────────────────────────────
// Full string literals required — Tailwind v4 static scanner cannot handle
// dynamic class construction.

const MODAL_SIZE: Record<Required<ModalProps>['size'], string> = {
  sm: 'max-w-sm',
  md: 'max-w-md',
  lg: 'max-w-lg',
};

// ── Modal ────────────────────────────────────────────────────────────────────

export interface ModalProps {
  /** Whether the modal is open. */
  open: boolean;
  /** Called when the modal requests to close (Escape key, overlay click, or
   *  the built-in close button in ModalHeader). */
  onClose: () => void;
  /**
   * Maximum width of the modal panel.
   * - `'sm'` — `max-w-sm`  (384 px)
   * - `'md'` — `max-w-md`  (448 px) — default
   * - `'lg'` — `max-w-lg`  (512 px)
   */
  size?: 'sm' | 'md' | 'lg';
  /**
   * When `false`, clicking outside the modal panel will NOT close it.
   * Use for modals that require an explicit action (e.g. copy-once token display).
   * Default: `true`.
   */
  dismissOnOutsideClick?: boolean;
  children: React.ReactNode;
}

/**
 * Modal
 *
 * A styled dialog shell built on Radix UI's Dialog primitive.
 * Handles open/close state, keyboard (Escape), and overlay-click dismissal.
 *
 * Compose with `ModalHeader`, `ModalBody`, and `ModalFooter`:
 *
 * ```tsx
 * <Modal open={isOpen} onClose={() => setOpen(false)} size="md">
 *   <ModalHeader title="Confirm action" description="This cannot be undone." />
 *   <ModalBody>
 *     <Text size="supporting" color="muted">…</Text>
 *   </ModalBody>
 *   <ModalFooter>
 *     <Button variant="outline" size="sm" onClick={() => setOpen(false)}>Cancel</Button>
 *     <Button variant="destructive" size="sm">Delete</Button>
 *   </ModalFooter>
 * </Modal>
 * ```
 *
 * **Accessibility**: `ModalHeader` renders the required `DialogTitle`. If you
 * omit `ModalHeader`, render `<DialogTitle>` (re-exported as
 * `ModalAccessibleTitle`) manually to satisfy screen readers.
 */
const Modal = ({
  open,
  onClose,
  size = 'md',
  dismissOnOutsideClick = true,
  children,
}: ModalProps) => (
  <DialogPrimitive.Root
    open={open}
    onOpenChange={(isOpen) => {
      if (!isOpen) onClose();
    }}
  >
    <DialogPrimitive.Portal>
      <DialogPrimitive.Overlay
        className={cn(
          'fixed inset-0 z-50 bg-black/50 backdrop-blur-sm',
          'data-[state=open]:animate-in data-[state=closed]:animate-out',
          'data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0',
          'duration-200',
        )}
      />
      <DialogPrimitive.Content
        data-slot="modal"
        onPointerDownOutside={(e) => {
          if (!dismissOnOutsideClick) e.preventDefault();
        }}
        className={cn(
          // Positioning
          'fixed left-1/2 top-1/2 z-50 -translate-x-1/2 -translate-y-1/2',
          // Layout
          'flex w-full flex-col',
          // Visual
          'rounded-xl border border-border bg-surface p-0 shadow-2xl',
          // Animation
          'data-[state=open]:animate-in data-[state=closed]:animate-out',
          'data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0',
          'data-[state=closed]:zoom-out-95 data-[state=open]:zoom-in-95',
          'data-[state=closed]:slide-out-to-left-1/2 data-[state=closed]:slide-out-to-top-[48%]',
          'data-[state=open]:slide-in-from-left-1/2 data-[state=open]:slide-in-from-top-[48%]',
          'duration-200',
          MODAL_SIZE[size],
        )}
      >
        {children}
      </DialogPrimitive.Content>
    </DialogPrimitive.Portal>
  </DialogPrimitive.Root>
);
Modal.displayName = 'Modal';

// ── ModalHeader ──────────────────────────────────────────────────────────────

export interface ModalHeaderProps {
  /** Required — rendered as a `Heading variant="section"` and satisfies the
   *  Radix DialogTitle accessibility requirement. */
  title: string;
  /** Optional subtitle rendered below the title as muted supporting text. */
  description?: string;
  /** When `false`, the built-in close (×) button is hidden.
   *  Default: `true`. */
  showClose?: boolean;
  /** When `true`, removes the bottom border. Use for borderless modal designs. */
  borderless?: boolean;
}

/**
 * ModalHeader
 *
 * Renders the modal's title row with an optional description and a built-in
 * close button. Place as the first child of `Modal`.
 *
 * `ModalHeader` satisfies the Radix `DialogTitle` accessibility requirement —
 * if you build a custom header, render `<ModalAccessibleTitle>` yourself.
 *
 * ```tsx
 * <ModalHeader title="Create grant" description="Assign scopes to a principal." />
 * ```
 */
const ModalHeader = ({
  title,
  description,
  showClose = true,
  borderless = true,
}: ModalHeaderProps) => (
  <div
    data-slot="modal-header"
    className={cn('flex items-start justify-between p-5', !borderless && 'border-b border-border')}
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
            'inline-flex h-8 w-8 shrink-0 items-center justify-center rounded-md',
            'border-transparent bg-transparent text-muted',
            'transition-colors hover:bg-surface-overlay hover:text-foreground',
            'focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent',
          )}
        >
          <Icon name="close" size="sm" />
        </button>
      </DialogPrimitive.Close>
    )}
  </div>
);
ModalHeader.displayName = 'ModalHeader';

// ── ModalBody ────────────────────────────────────────────────────────────────

export interface ModalBodyProps {
  /**
   * Removes the default `p-5` padding. Use when the body contains flush
   * content such as a list, image, or embedded table that needs edge-to-edge
   * layout.
   */
  flush?: boolean;

  /**
   * Removes vertical padding only, keeping horizontal (`px-5`). Use for
   * compact modals where the header and footer already provide sufficient
   * visual separation without additional top/bottom whitespace in the body.
   */
  flushY?: boolean;

  /**
   * Removes the default `gap-5` between children. Use when child components
   * manage their own spacing or a zero-gap layout is intentional.
   */
  gapless?: boolean;

  /**
   * Constrains the body to `max-h-[60vh]` so it scrolls inside the viewport
   * instead of allowing the modal to grow to full-page height. Pair with
   * `overflow-y-auto` (already applied) for tall content like long lists or
   * rich editors.
   */
  constrained?: boolean;
  children?: React.ReactNode;
  /** @internal Used for internal Terra composition via cn(). */
  className?: string;
  id?: string;
}

/**
 * ModalBody
 *
 * Scrollable content area for modal body content. Renders with padding and
 * a vertical flex column layout with comfortable spacing between children.
 *
 * ```tsx
 * <ModalBody>
 *   <Field>…</Field>
 *   <Field>…</Field>
 * </ModalBody>
 * ```
 *
 * Use `flush` to remove padding, `gapless` to remove child spacing, or
 * `constrained` to cap height at 60 vh and enable scrolling.
 *
 * ```tsx
 * <ModalBody flush gapless constrained>
 *   <ScopeList />
 * </ModalBody>
 * ```
 */
const ModalBody = React.forwardRef<HTMLDivElement, ModalBodyProps>(
  ({ className, flush, flushY, gapless, constrained, ...props }, ref) => (
    <div
      ref={ref}
      data-slot="modal-body"
      className={cn(
        'flex flex-col',
        flush ? 'p-0' : flushY ? 'px-5 py-0' : 'p-5',
        gapless ? 'gap-0' : 'gap-5',
        constrained && 'max-h-[60vh] overflow-y-auto',
        className,
      )}
      {...props}
    />
  ),
);
ModalBody.displayName = 'ModalBody';

// ── ModalFooter ──────────────────────────────────────────────────────────────

export interface ModalFooterProps {
  /** Main-axis alignment for footer action buttons. Default: `'end'`. */
  justify?: 'start' | 'center' | 'end' | 'between';
  children?: React.ReactNode;
  /** @internal Used for internal Terra composition via cn(). */
  className?: string;
  /** When `true`, removes the top border. Use for borderless modal designs. */
  borderless?: boolean;
}

const FOOTER_JUSTIFY: Record<Required<ModalFooterProps>['justify'], string> = {
  start: 'justify-start',
  center: 'justify-center',
  end: 'justify-end',
  between: 'justify-between',
};

/**
 * ModalFooter
 *
 * Horizontal row for action buttons, pinned to the bottom of the modal with
 * a top border. Defaults to right-aligned (`justify="end"`).
 *
 * ```tsx
 * <ModalFooter>
 *   <Button variant="outline" size="sm" onClick={onClose}>Cancel</Button>
 *   <Button variant="destructive" size="sm">Delete</Button>
 * </ModalFooter>
 * ```
 */
const ModalFooter = React.forwardRef<HTMLElement, ModalFooterProps>(
  ({ className, justify = 'end', borderless = true, ...props }, ref) => (
    <footer
      ref={ref}
      data-slot="modal-footer"
      className={cn(
        'flex items-center gap-2.5 p-5',
        !borderless && 'border-t border-border',
        FOOTER_JUSTIFY[justify],
        className,
      )}
      {...props}
    />
  ),
);
ModalFooter.displayName = 'ModalFooter';

// ── ModalAccessibleTitle ──────────────────────────────────────────────────────

/**
 * ModalAccessibleTitle
 *
 * A visually-hidden `DialogTitle` for modals that build a custom header
 * without using `ModalHeader`. This satisfies the Radix accessibility
 * requirement without adding visible heading text.
 *
 * ```tsx
 * <Modal open={open} onClose={onClose}>
 *   <ModalAccessibleTitle>Confirm deletion</ModalAccessibleTitle>
 *   {/* custom header layout … *\/}
 * </Modal>
 * ```
 */
const ModalAccessibleTitle = React.forwardRef<
  React.ComponentRef<typeof DialogPrimitive.Title>,
  React.ComponentPropsWithoutRef<typeof DialogPrimitive.Title>
>(({ className, ...props }, ref) => (
  <DialogPrimitive.Title ref={ref} className={cn('sr-only', className)} {...props} />
));
ModalAccessibleTitle.displayName = 'ModalAccessibleTitle';

export { Modal, ModalHeader, ModalBody, ModalFooter, ModalAccessibleTitle };
