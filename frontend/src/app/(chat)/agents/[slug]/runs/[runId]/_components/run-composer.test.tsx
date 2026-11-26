import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import type { RunView } from '../_lib/run-view';
import { Composer } from './run-composer';

vi.mock('../_context/run-context', () => ({
  useRunContext: () => ({
    addHumanMessage: vi.fn(),
    interruptRun: vi.fn(),
    canSteer: true,
    isStarting: false,
    isPending: false,
    stopError: null,
  }),
}));

const run = { agent: { name: 'Coder', slug: 'coder' }, queued: [] } as unknown as RunView;
const cancel = vi.fn();
const animate = vi.fn(() => ({ cancel }) as unknown as Animation);

function reducedMotion(matches: boolean) {
  vi.spyOn(window, 'matchMedia').mockImplementation(
    (query) => ({ matches, media: query }) as MediaQueryList,
  );
}

beforeEach(() => {
  animate.mockClear();
  cancel.mockClear();
  HTMLElement.prototype.animate = animate;
  // The frame's own place on the run page.
  vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockReturnValue(
    new DOMRect(300, 700, 720, 150),
  );
});
afterEach(() => vi.restoreAllMocks());

describe('Composer handed over from the chat page', () => {
  const from = new DOMRect(260, 300, 720, 130);

  it('moves from the chat composer’s left and bottom edges in 280 ms, and takes focus', () => {
    reducedMotion(false);
    render(<Composer run={run} enterFrom={from} />);
    expect(animate).toHaveBeenCalledTimes(1);
    expect(animate).toHaveBeenCalledWith(
      [{ transform: 'translate(-40px, -420px)' }, { transform: 'none' }],
      expect.objectContaining({ duration: 280 }),
    );
    expect(screen.getByRole('textbox', { name: 'Message Coder' })).toHaveFocus();
  });

  it('cancels the move when it unmounts', () => {
    reducedMotion(false);
    const { unmount } = render(<Composer run={run} enterFrom={from} />);
    unmount();
    expect(cancel).toHaveBeenCalled();
  });

  it('takes its place with no movement under reduced motion', () => {
    reducedMotion(true);
    render(<Composer run={run} enterFrom={from} />);
    expect(animate).not.toHaveBeenCalled();
  });

  it('neither moves nor takes focus on a run opened directly', () => {
    reducedMotion(false);
    render(<Composer run={run} />);
    expect(animate).not.toHaveBeenCalled();
    expect(screen.getByRole('textbox', { name: 'Message Coder' })).not.toHaveFocus();
  });
});
