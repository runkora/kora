// @vitest-environment jsdom
// Radix Dialog (portal, focus management) — happy-dom is fine for most Terra
// components but this repo's own vitest config flags Radix-based ones as
// needing jsdom instead.
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { BottomSheet } from './bottom-sheet';

/**
 * Drag-to-dismiss physics (pointer capture, velocity thresholds) aren't
 * covered here — simulating realistic pointer-move sequences with timed
 * samples in this environment has low fidelity. These tests cover the
 * correctness-critical, reliably-testable parts: open/close semantics and
 * the History API pairing (requirement: the OS/back button closes the
 * sheet instead of leaving the page, and any other close path consumes the
 * pushed entry so a later real back-press isn't swallowed by a stale one).
 *
 * `history.pushState`/`back` are mocked for every test in this file, not
 * just the "History API" tests below — every render pushes a real history
 * entry otherwise, and jsdom fires `popstate` asynchronously, so a stray
 * event from an earlier, unmocked test can leak into a later test's timing
 * window and corrupt its component state.
 */
describe('BottomSheet', () => {
  let pushStateSpy: ReturnType<typeof vi.spyOn>;
  let backSpy: ReturnType<typeof vi.spyOn>;

  beforeEach(() => {
    pushStateSpy = vi.spyOn(window.history, 'pushState').mockImplementation(() => {});
    backSpy = vi.spyOn(window.history, 'back').mockImplementation(() => {});
  });

  afterEach(() => {
    pushStateSpy.mockRestore();
    backSpy.mockRestore();
  });

  function renderSheet(open: boolean, onOpenChange: (open: boolean) => void) {
    return render(
      <BottomSheet open={open} onOpenChange={onOpenChange} title="Account">
        <button type="button">Row action</button>
      </BottomSheet>,
    );
  }

  it('renders children and the accessible title when open', () => {
    renderSheet(true, () => {});
    expect(screen.getByText('Row action')).toBeInTheDocument();
    expect(screen.getByText('Account')).toBeInTheDocument();
  });

  it('renders nothing visible when closed', () => {
    renderSheet(false, () => {});
    expect(screen.queryByText('Row action')).not.toBeInTheDocument();
  });

  it('always renders a visible close button', () => {
    renderSheet(true, () => {});
    expect(screen.getByRole('button', { name: 'Close' })).toBeInTheDocument();
  });

  it('calls onOpenChange(false) when the close button is clicked', async () => {
    const user = userEvent.setup();
    const onOpenChange = vi.fn();
    renderSheet(true, onOpenChange);
    await user.click(screen.getByRole('button', { name: 'Close' }));
    expect(onOpenChange).toHaveBeenCalledWith(false);
  });

  it('calls onOpenChange(false) on Escape', () => {
    const onOpenChange = vi.fn();
    renderSheet(true, onOpenChange);
    fireEvent.keyDown(document.activeElement ?? document.body, { key: 'Escape' });
    expect(onOpenChange).toHaveBeenCalledWith(false);
  });

  describe('History API integration', () => {
    it('pushes a history entry when it opens', () => {
      renderSheet(true, () => {});
      expect(pushStateSpy).toHaveBeenCalledTimes(1);
    });

    it('does not push a history entry when closed', () => {
      renderSheet(false, () => {});
      expect(pushStateSpy).not.toHaveBeenCalled();
    });

    it('consumes the pushed entry via history.back() when closed by the close button', async () => {
      const user = userEvent.setup();
      const onOpenChange = vi.fn();
      renderSheet(true, onOpenChange);
      await user.click(screen.getByRole('button', { name: 'Close' }));
      expect(backSpy).toHaveBeenCalledTimes(1);
      expect(onOpenChange).toHaveBeenCalledWith(false);
    });

    it('does not call history.back() again when the close was itself triggered by a popstate', async () => {
      const onOpenChange = vi.fn();
      renderSheet(true, onOpenChange);
      expect(pushStateSpy).toHaveBeenCalledTimes(1);

      // Simulate the user pressing the OS/browser back button.
      fireEvent.popState(window);

      await waitFor(() => expect(onOpenChange).toHaveBeenCalledWith(false));
      expect(backSpy).not.toHaveBeenCalled();
    });
  });
});
