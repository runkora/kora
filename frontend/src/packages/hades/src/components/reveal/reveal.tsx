'use client';

/** @tier 1 — Zero kora domain knowledge. Plays its children in once on mount, one after another. */

import * as React from 'react';

/** Each item rises from a little below its place and fades in. */
const FRAMES: Keyframe[] = [
  { opacity: 0, transform: 'translateY(0.4em)' },
  { opacity: 1, transform: 'translateY(0)' },
];

/** The easing of a move into place: quick at first, then slow to settle. */
export const SETTLE_EASING = 'cubic-bezier(0.2, 0.8, 0.2, 1)';

export interface RevealProps extends Omit<React.ComponentPropsWithoutRef<'span'>, 'className'> {
  /** Milliseconds between one item's start and the next's. */
  stagger?: number;
  /** Milliseconds each item takes. */
  duration?: number;
  /** Milliseconds before the first item starts. */
  delay?: number;
}

/**
 * Each child is one item, and a whitespace-only string between them stays as it is. The items
 * play once on mount. Under reduced motion they show in place. Nothing is announced to assistive
 * technology: the text reads as it would without the reveal. Mount it on the client: server HTML
 * shows the items in place until hydration plays them in.
 */
const Reveal = React.forwardRef<HTMLSpanElement, RevealProps>(
  ({ stagger = 60, duration = 280, delay = 0, children, ...props }, ref) => {
    const root = React.useRef<HTMLSpanElement | null>(null);
    const setRoot = (el: HTMLSpanElement | null) => {
      root.current = el;
      if (typeof ref === 'function') ref(el);
      else if (ref) ref.current = el;
    };

    // Before paint, so no item shows in place first and then jumps back to its start.
    React.useLayoutEffect(() => {
      const el = root.current;
      if (!el || window.matchMedia('(prefers-reduced-motion: reduce)').matches) return;
      const items = el.querySelectorAll<HTMLElement>(':scope > [data-slot="reveal-item"]');
      const played = Array.from(items, (item, i) =>
        item.animate(FRAMES, {
          duration,
          delay: delay + i * stagger,
          easing: SETTLE_EASING,
          // Holds the start frame through the delay and leaves no transform behind once done.
          fill: 'backwards',
        }),
      );
      return () => played.forEach((a) => a.cancel());
    }, [stagger, duration, delay]);

    return (
      <span ref={setRoot} data-slot="reveal" {...props}>
        {React.Children.map(children, (child) =>
          typeof child === 'string' && !child.trim() ? (
            child
          ) : (
            <span data-slot="reveal-item" className="inline-block">
              {child}
            </span>
          ),
        )}
      </span>
    );
  },
);
Reveal.displayName = 'Reveal';

export { Reveal };
