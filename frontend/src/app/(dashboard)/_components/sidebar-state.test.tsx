/**
 * Unit tests for the sidebar-state context.
 *
 * Covers the open/close/toggle API and the responsive auto-close: the drawer
 * must force itself shut once the viewport reaches `md` (768 px), because it is
 * a modal Radix Dialog whose `md:hidden` classes only hide it visually while it
 * keeps body scroll locked and focus trapped.
 *
 * `window.matchMedia` is replaced per-test with a controllable fake so we can
 * simulate the viewport crossing the breakpoint, then restored afterwards.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { act, renderHook } from '@testing-library/react';
import { SidebarStateProvider, useSidebarState } from './sidebar-state';

// ── Controllable matchMedia fake ────────────────────────────────────────────────

type Listener = () => void;

function installMatchMedia(initialMatches: boolean) {
  let matches = initialMatches;
  const listeners = new Set<Listener>();

  const mql = {
    get matches() {
      return matches;
    },
    media: '(min-width: 768px)',
    onchange: null,
    addEventListener: (_: string, cb: Listener) => listeners.add(cb),
    removeEventListener: (_: string, cb: Listener) => listeners.delete(cb),
    addListener: (cb: Listener) => listeners.add(cb),
    removeListener: (cb: Listener) => listeners.delete(cb),
    dispatchEvent: () => false,
  };

  window.matchMedia = vi.fn(() => mql) as unknown as typeof window.matchMedia;

  return {
    /** Simulate the viewport crossing the breakpoint and fire the change event. */
    setMatches(next: boolean) {
      matches = next;
      listeners.forEach((cb) => cb());
    },
  };
}

const renderProvider = () => renderHook(() => useSidebarState(), { wrapper: SidebarStateProvider });

// ── Tests ───────────────────────────────────────────────────────────────────────

describe('SidebarStateProvider / useSidebarState', () => {
  let originalMatchMedia: typeof window.matchMedia;

  beforeEach(() => {
    originalMatchMedia = window.matchMedia;
  });

  afterEach(() => {
    window.matchMedia = originalMatchMedia;
  });

  describe('open/close/toggle', () => {
    it('starts closed', () => {
      installMatchMedia(false);
      const { result } = renderProvider();
      expect(result.current.isOpen).toBe(false);
    });

    it('open() opens and close() closes', () => {
      installMatchMedia(false);
      const { result } = renderProvider();

      act(() => result.current.open());
      expect(result.current.isOpen).toBe(true);

      act(() => result.current.close());
      expect(result.current.isOpen).toBe(false);
    });

    it('toggle() flips the current state', () => {
      installMatchMedia(false);
      const { result } = renderProvider();

      act(() => result.current.toggle());
      expect(result.current.isOpen).toBe(true);

      act(() => result.current.toggle());
      expect(result.current.isOpen).toBe(false);
    });
  });

  describe('responsive auto-close', () => {
    it('forces the drawer closed when the viewport grows to >= md', () => {
      const mq = installMatchMedia(false); // start below md (mobile)
      const { result } = renderProvider();

      act(() => result.current.open());
      expect(result.current.isOpen).toBe(true);

      // Viewport widens past the md breakpoint (rotate / resize / devtools).
      act(() => mq.setMatches(true));
      expect(result.current.isOpen).toBe(false);
    });

    it('does not reopen when the viewport shrinks back below md', () => {
      const mq = installMatchMedia(false);
      const { result } = renderProvider();

      act(() => mq.setMatches(true)); // cross to desktop
      act(() => mq.setMatches(false)); // shrink back to mobile
      expect(result.current.isOpen).toBe(false);
    });
  });
});
