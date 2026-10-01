'use client';

/** @tier 1 — Zero Orbit domain knowledge. A modal, bottom-anchored sheet for mobile viewports. */

import * as React from 'react';
import * as DialogPrimitive from '@radix-ui/react-dialog';
import { cn } from '../../utils';
import { Icon } from '../icon';
import { ModalAccessibleTitle } from '../modal';

// Release thresholds — see the "Drag to dismiss" behaviour docs below.
const VELOCITY_THRESHOLD_PX_MS = 0.5;
const DISMISS_HEIGHT_RATIO = 0.3;
const TAP_THRESHOLD_PX = 4;
const VELOCITY_WINDOW_MS = 100;
const RELEASE_MS = 200;

export interface BottomSheetProps {
  /** Whether the sheet is open. */
  open: boolean;
  /** Called when the sheet requests to close — drag-dismiss, scrim tap, Esc,
   *  the built-in close button, or the browser/OS back button. */
  onOpenChange: (open: boolean) => void;
  /** Accessible name for the sheet (Radix `DialogTitle`, visually hidden). */
  title: string;
  children: React.ReactNode;
}

/**
 * BottomSheet
 *
 * A modal sheet anchored to the bottom of the viewport — the mobile-width
 * counterpart to a desktop popover or flyout. Built on Radix `Dialog` so
 * focus trapping, `aria-modal`, Escape-to-close, and background scroll
 * locking all come for free — Radix Dialog depends on `react-remove-scroll`,
 * which is specifically built to survive mobile browsers hiding/showing
 * their address bar on scroll, rather than a hand-rolled `overflow:hidden`.
 *
 * Three things this component does not leave optional, because relying on
 * any one of them alone is a known mobile-web failure mode:
 *
 *   - Always renders a visible close (×) button. A downward swipe can be
 *     hijacked by the browser's own pull-to-refresh, so dismissal never
 *     depends on the drag gesture alone.
 *   - Integrates with the History API: opening pushes a history entry, so
 *     the hardware/OS back button closes the sheet instead of leaving the
 *     page. Any other close path consumes that entry first, so the next
 *     real back-press afterwards isn't swallowed by a stale one.
 *   - Drag-to-dismiss tracks the finger 1:1 with no lag; only the *release*
 *     animation (spring-back or close) respects `prefers-reduced-motion` by
 *     becoming instant.
 *
 * Drag to dismiss: pointer-down on the pull tab starts a drag (menu items
 * and other content do not). The sheet follows the finger via
 * `translateY(max(0, dy))` — it never lifts above its resting position.
 * On release: closes if the drag moved ≥30% of the sheet's height, or if
 * release velocity was ≥0.5px/ms (a flick) measured over the last 100ms;
 * otherwise it springs back. A drag under 4px counts as a tap and does
 * nothing (matches `pointercancel` behaviour too).
 *
 * ```tsx
 * <BottomSheet open={open} onOpenChange={setOpen} title="Account">
 *   <YourContent />
 * </BottomSheet>
 * ```
 */
const BottomSheet = ({ open, onOpenChange, title, children }: BottomSheetProps) => {
  const contentRef = React.useRef<HTMLDivElement>(null);
  // Whether *this* sheet currently owns a pushed history entry, and whether
  // the in-flight close was triggered by popping that entry (a real
  // back-press) rather than some other dismissal path.
  const pushedRef = React.useRef(false);
  const poppingRef = React.useRef(false);

  // Reconciles the pushed history entry whenever `open` goes false, no
  // matter *why* — Radix dismissing itself (Esc, overlay, its own Close
  // button), a parent closing the sheet by changing the `open` prop
  // directly, drag-to-dismiss, or unmount. Consuming this in the effect's
  // cleanup (rather than only inside a close handler threaded through
  // Radix) means there's a single choke point regardless of the close
  // path, and it also makes React StrictMode's dev-only mount→cleanup→
  // remount cycle net out to one entry instead of two.
  React.useEffect(() => {
    if (!open) return;
    window.history.pushState({ sheet: title }, '');
    pushedRef.current = true;

    const handlePopState = () => {
      if (!pushedRef.current) return;
      pushedRef.current = false;
      poppingRef.current = true;
      onOpenChange(false);
    };
    window.addEventListener('popstate', handlePopState);

    return () => {
      window.removeEventListener('popstate', handlePopState);
      if (pushedRef.current && !poppingRef.current) {
        pushedRef.current = false;
        window.history.back();
      }
      poppingRef.current = false;
    };
    // Only re-run when `open` flips — `title` and `onOpenChange` aren't
    // expected to change while a single sheet lifecycle is in flight.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);

  // ── Drag-to-dismiss ────────────────────────────────────────────────────────

  const dragRef = React.useRef<{
    active: boolean;
    startY: number;
    sheetHeight: number;
    samples: { t: number; y: number }[];
  }>({ active: false, startY: 0, sheetHeight: 0, samples: [] });

  const setSheetTransform = (dy: number, animated: boolean, closing = false) => {
    const el = contentRef.current;
    if (!el) return;
    const reducedMotion = window.matchMedia('(prefers-reduced-motion: reduce)').matches;
    el.style.transition =
      animated && !reducedMotion
        ? `transform ${RELEASE_MS}ms cubic-bezier(${closing ? '0.4,0,1,1' : '0,0,0.2,1'})`
        : 'none';
    el.style.transform = dy === 0 ? '' : `translateY(${dy}px)`;
  };

  const handlePointerDown = (e: React.PointerEvent<HTMLDivElement>) => {
    const el = contentRef.current;
    if (!el) return;
    e.currentTarget.setPointerCapture(e.pointerId);
    dragRef.current = {
      active: true,
      startY: e.clientY,
      sheetHeight: el.offsetHeight,
      samples: [{ t: performance.now(), y: e.clientY }],
    };
  };

  const handlePointerMove = (e: React.PointerEvent<HTMLDivElement>) => {
    const drag = dragRef.current;
    if (!drag.active) return;
    const dy = Math.max(0, e.clientY - drag.startY);
    drag.samples.push({ t: performance.now(), y: e.clientY });
    setSheetTransform(dy, false);
  };

  const endDrag = (e: React.PointerEvent<HTMLDivElement>) => {
    const drag = dragRef.current;
    if (!drag.active) return;
    drag.active = false;

    const dy = Math.max(0, e.clientY - drag.startY);
    if (dy < TAP_THRESHOLD_PX) {
      setSheetTransform(0, false);
      return;
    }

    const now = performance.now();
    const windowStart = now - VELOCITY_WINDOW_MS;
    const recent = drag.samples.filter((s) => s.t >= windowStart);
    const first = recent[0] ?? drag.samples[0];
    const elapsed = Math.max(1, now - first.t);
    const velocity = (e.clientY - first.y) / elapsed;

    const shouldClose =
      dy >= drag.sheetHeight * DISMISS_HEIGHT_RATIO || velocity >= VELOCITY_THRESHOLD_PX_MS;

    if (shouldClose) {
      const reducedMotion = window.matchMedia('(prefers-reduced-motion: reduce)').matches;
      if (reducedMotion) {
        onOpenChange(false);
        return;
      }
      setSheetTransform(drag.sheetHeight, true, true);
      window.setTimeout(() => onOpenChange(false), RELEASE_MS);
    } else {
      setSheetTransform(0, true);
    }
  };

  return (
    <DialogPrimitive.Root open={open} onOpenChange={onOpenChange}>
      <DialogPrimitive.Portal>
        <DialogPrimitive.Overlay
          className={cn(
            'fixed inset-0 z-[59] bg-overlay-strong',
            'data-[state=open]:animate-in data-[state=closed]:animate-out',
            'data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0',
            'duration-200 motion-reduce:duration-0',
          )}
        />
        <DialogPrimitive.Content
          ref={contentRef}
          data-slot="bottom-sheet"
          className={cn(
            'fixed inset-x-0 bottom-0 z-[60] flex max-h-[85dvh] flex-col overflow-hidden',
            'rounded-t-lg border-t border-border-subtle bg-background',
            'data-[state=open]:animate-in data-[state=closed]:animate-out',
            'data-[state=closed]:slide-out-to-bottom data-[state=open]:slide-in-from-bottom',
            'duration-200 motion-reduce:duration-0',
          )}
        >
          <ModalAccessibleTitle>{title}</ModalAccessibleTitle>

          <div
            className="flex shrink-0 touch-none justify-center pb-1 pt-2"
            onPointerDown={handlePointerDown}
            onPointerMove={handlePointerMove}
            onPointerUp={endDrag}
            onPointerCancel={endDrag}
          >
            <div className="h-1 w-9 rounded-full bg-border" />
          </div>

          <DialogPrimitive.Close asChild>
            <button
              type="button"
              aria-label="Close"
              className={cn(
                'absolute right-2 top-2 inline-flex h-11 w-11 items-center justify-center rounded-md',
                'text-muted transition-colors hover:bg-surface-elevated hover:text-foreground',
                'focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent',
              )}
            >
              <Icon name="close" size="sm" />
            </button>
          </DialogPrimitive.Close>

          {/* Only the body scrolls — the drag handle and close button above
           *  stay fixed so the sheet always stays dismissible. */}
          <div className="overflow-y-auto pb-[calc(8px+env(safe-area-inset-bottom))]">
            {children}
          </div>
        </DialogPrimitive.Content>
      </DialogPrimitive.Portal>
    </DialogPrimitive.Root>
  );
};
BottomSheet.displayName = 'BottomSheet';

export { BottomSheet };
