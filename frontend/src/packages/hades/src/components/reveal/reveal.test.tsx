import { afterEach, describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import { Reveal } from './reveal';

function reducedMotion(matches: boolean) {
  vi.spyOn(window, 'matchMedia').mockImplementation(
    (query) => ({ matches, media: query }) as MediaQueryList,
  );
}

function stubAnimate() {
  const animate = vi.fn(() => ({ cancel: vi.fn() }) as unknown as Animation);
  Object.defineProperty(HTMLElement.prototype, 'animate', { value: animate, configurable: true });
  return animate;
}

afterEach(() => vi.restoreAllMocks());

const words = ['One', ' ', 'two', ' ', 'three'];

describe('Reveal', () => {
  it('wraps each item and keeps the spaces between them as text', () => {
    reducedMotion(true);
    render(<Reveal data-testid="r">{words}</Reveal>);
    const root = screen.getByTestId('r');
    expect(root).toHaveTextContent('One two three');
    expect(root.querySelectorAll('[data-slot="reveal-item"]')).toHaveLength(3);
  });

  it('plays each item once, one stagger after the last', () => {
    reducedMotion(false);
    const animate = stubAnimate();
    render(
      <Reveal stagger={50} duration={200} delay={100}>
        {words}
      </Reveal>,
    );
    expect(animate).toHaveBeenCalledTimes(3);
    expect(animate.mock.calls.map((c) => (c as unknown[])[1])).toEqual(
      [100, 150, 200].map((delay) => expect.objectContaining({ delay, duration: 200 })),
    );
  });

  it('shows the items in place under reduced motion', () => {
    reducedMotion(true);
    const animate = stubAnimate();
    render(<Reveal>{words}</Reveal>);
    expect(animate).not.toHaveBeenCalled();
  });

  it('adds no live region', () => {
    reducedMotion(true);
    render(<Reveal data-testid="r">{words}</Reveal>);
    const root = screen.getByTestId('r');
    expect(root.querySelector('[aria-live],[role="status"],[role="alert"]')).toBeNull();
    expect(root).not.toHaveAttribute('aria-live');
  });
});
