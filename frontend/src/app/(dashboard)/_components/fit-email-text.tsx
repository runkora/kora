'use client';

/** @tier 3 — sidebar-footer-scoped. Shrinks an email to fit its column, single-line, no ellipsis. */

import { useLayoutEffect, useRef } from 'react';
import { Text } from '@clear-route/hades';

const MAX_PX = 11.5;

function fitToWidth(el: HTMLElement): void {
  const available = el.parentElement?.clientWidth ?? 0;
  if (!available) return;

  el.style.fontSize = `${MAX_PX}px`;
  const natural = el.offsetWidth;
  let size = natural > available ? Math.floor((MAX_PX * 10 * available) / natural) / 10 : MAX_PX;
  el.style.fontSize = `${size}px`;

  // Glyph widths don't scale perfectly linearly with font-size — step down
  // in 0.1px increments until it truly fits. No minimum size, no ellipsis.
  while (el.offsetWidth > available && size > 1) {
    size = Math.round((size - 0.1) * 10) / 10;
    el.style.fontSize = `${size}px`;
  }
}

/**
 * FitEmailText
 *
 * Renders `email` at up to 11.5px, shrinking proportionally (never below the
 * text's natural size, no ellipsis, no minimum) so it always fits on one
 * line within its parent's current width. The parent must be a fixed-height
 * row (16px in the footer trigger) so the row's own height never changes as
 * the font size does.
 *
 * Re-fits on mount, when `email` changes, once the real font finishes
 * loading (a fit computed against the fallback font measures narrower than
 * Geist and would overflow once Geist swaps in), on any resize of the
 * parent row, and before printing.
 */
export function FitEmailText({ email }: { email: string }) {
  const ref = useRef<HTMLElement>(null);

  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;

    const fit = () => fitToWidth(el);
    fit();

    let cancelled = false;
    document.fonts?.ready.then(() => {
      if (!cancelled) fit();
    });
    document.fonts?.addEventListener('loadingdone', fit);
    window.addEventListener('beforeprint', fit);

    let observer: ResizeObserver | undefined;
    if (typeof ResizeObserver !== 'undefined' && el.parentElement) {
      observer = new ResizeObserver(fit);
      observer.observe(el.parentElement);
    }

    return () => {
      cancelled = true;
      document.fonts?.removeEventListener('loadingdone', fit);
      window.removeEventListener('beforeprint', fit);
      observer?.disconnect();
    };
  }, [email]);

  return (
    <Text
      as="span"
      ref={ref}
      color="subtle"
      style={{ whiteSpace: 'nowrap', lineHeight: 1, flexShrink: 0 }}
    >
      {email}
    </Text>
  );
}
