import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import { HeadingReveal } from './heading-reveal';

const animate = vi.fn(() => ({ cancel() {} }) as unknown as Animation);

function reducedMotion(matches: boolean) {
  vi.spyOn(window, 'matchMedia').mockImplementation(
    (query) => ({ matches, media: query }) as MediaQueryList,
  );
}

beforeEach(() => {
  animate.mockClear();
  HTMLElement.prototype.animate = animate;
});
afterEach(() => vi.restoreAllMocks());

const timings = () =>
  animate.mock.calls.map((c) => (c as unknown[])[1] as KeyframeAnimationOptions);

describe('HeadingReveal', () => {
  it('reads as one sentence with the name last', () => {
    reducedMotion(true);
    render(<HeadingReveal name="Sam" />);
    expect(
      screen.getByRole('heading', { name: 'What should we work on next, Sam?' }),
    ).toBeInTheDocument();
  });

  it('ends at "next?" without a name', () => {
    reducedMotion(true);
    render(<HeadingReveal />);
    expect(
      screen.getByRole('heading', { name: 'What should we work on next?' }),
    ).toBeInTheDocument();
  });

  it('drops the disc, splits it, then plays the words from the split', () => {
    reducedMotion(false);
    render(<HeadingReveal name="Sam" />);
    const all = timings();
    expect(all).toContainEqual(expect.objectContaining({ duration: 360 }));
    expect(all).toContainEqual(expect.objectContaining({ duration: 280, delay: 360 }));
    // The split and the seven words each take 280 ms.
    const words = all.filter((t) => t.duration === 280).map((t) => t.delay as number);
    expect(words).toHaveLength(8);
    expect(Math.max(...words) + 280 - 360).toBe(600);
  });

  it('shows the mark and the words in place under reduced motion', () => {
    reducedMotion(true);
    render(<HeadingReveal name="Sam" />);
    expect(animate).not.toHaveBeenCalled();
  });
});
