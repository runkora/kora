'use client';

/**
 * @tier 3 — chat-scoped. The chat page's heading with the product mark in front of it, played once
 * on load: a disc drops into place and splits into the mark, then the words rise in one after
 * another with the viewer's name last. Under reduced motion both show in place.
 */

import { useLayoutEffect, useRef, type CSSProperties } from 'react';
import { Heading, Reveal, Stack } from '@runkora/hades';
import { SETTLE } from '@runkora/app/(chat)/agents/[slug]/runs/[runId]/_lib/run-styles';

/** The heading's font size. The mark is drawn this tall to sit level with the text. */
const SIZE = 24;

/**
 * The mark's geometry from `src/app/icon.svg`: two half-discs of radius 128, the lower one 34.14
 * right of and 8.54 below the upper, 264.54 tall in all.
 */
const UNIT = SIZE / 264.54;
const R = 128 * UNIT;
const SHIFT = `translate(${34.14 * UNIT}px, ${8.54 * UNIT}px)`;
const MARK_WIDTH = 290.14 * UNIT;

/** Milliseconds for each part. The words take WORDS from the first starting to the last landing. */
const DROP = 360;
const SPLIT = 280;
const WORD = 280;
const WORDS = 600;

const half = (lower: boolean): CSSProperties => ({
  left: 0,
  top: lower ? R : 0,
  width: 2 * R,
  height: R,
  borderRadius: lower ? `0 0 ${R}px ${R}px` : `${R}px ${R}px 0 0`,
  // Drawn in its end state: the mark. The animation starts it as one disc.
  background: lower ? 'var(--color-border-accent)' : 'var(--color-foreground)',
  transform: lower ? SHIFT : undefined,
});

/** "What should we work on next, <name>?", one item per word for the reveal. */
function wordsOf(name?: string): string[] {
  const words = ['What', 'should', 'we', 'work', 'on', name ? 'next,' : 'next?'];
  if (name) words.push(`${name}?`);
  return words.flatMap((w, i) => (i ? [' ', w] : [w]));
}

export function HeadingReveal({ name }: { name?: string }) {
  const disc = useRef<HTMLDivElement>(null);
  const lower = useRef<HTMLDivElement>(null);
  const words = wordsOf(name);
  const count = (words.length + 1) / 2;

  // Before paint, so the mark never shows in place first and then jumps back to a disc.
  useLayoutEffect(() => {
    if (window.matchMedia('(prefers-reduced-motion: reduce)').matches) return;
    // The disc falls in whole, speeding up as it drops.
    disc.current?.animate(
      [
        { transform: `translateY(${-SIZE * 0.75}px)`, opacity: 0 },
        { transform: 'translateY(0)', opacity: 1 },
      ],
      { duration: DROP, easing: 'cubic-bezier(0.5, 0, 0.75, 0)', fill: 'backwards' },
    );
    // Its lower half turns to the accent and steps aside, which is the mark.
    lower.current?.animate(
      [
        { transform: 'translate(0, 0)', background: 'var(--color-foreground)' },
        { transform: SHIFT, background: 'var(--color-border-accent)' },
      ],
      { duration: SPLIT, delay: DROP, easing: SETTLE, fill: 'backwards' },
    );
  }, []);

  return (
    <Stack direction="horizontal" align="center" justify="center" gap="cozy">
      <Stack
        ref={disc}
        position="relative"
        shrink={false}
        aria-hidden
        style={{ width: MARK_WIDTH, height: SIZE }}
      >
        <Stack position="absolute" style={half(false)} />
        <Stack ref={lower} position="absolute" style={half(true)} />
      </Stack>
      <Heading variant="section">
        <Reveal delay={DROP} duration={WORD} stagger={(WORDS - WORD) / (count - 1)}>
          {words}
        </Reveal>
      </Heading>
    </Stack>
  );
}
