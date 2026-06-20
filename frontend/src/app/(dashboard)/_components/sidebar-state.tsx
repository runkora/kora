'use client';

/** @tier 3 — dashboard-scoped. Manages mobile sidebar open/close state for the dashboard shell. */

import { createContext, useCallback, useContext, useEffect, useState, type ReactNode } from 'react';

/** Tailwind `md` breakpoint — the sidebar becomes a fixed rail at and above this. */
const MD_BREAKPOINT = '(min-width: 768px)';

// ── Types ─────────────────────────────────────────────────────────────────────

interface SidebarStateContextValue {
  isOpen: boolean;
  open: () => void;
  close: () => void;
  toggle: () => void;
}

// ── Context ───────────────────────────────────────────────────────────────────

const SidebarStateContext = createContext<SidebarStateContextValue>({
  isOpen: false,
  open: () => {},
  close: () => {},
  toggle: () => {},
});

// ── Provider ──────────────────────────────────────────────────────────────────

export function SidebarStateProvider({ children }: { children: ReactNode }) {
  const [isOpen, setIsOpen] = useState(false);

  const open = useCallback(() => setIsOpen(true), []);
  const close = useCallback(() => setIsOpen(false), []);
  const toggle = useCallback(() => setIsOpen((v) => !v), []);

  // Force the drawer closed once the viewport reaches `md`. The drawer is a
  // modal Radix Dialog that locks body scroll and traps focus while open; the
  // `md:hidden` classes only hide it visually. Without this, opening the drawer
  // on mobile and then widening to desktop (rotate / resize / responsive
  // devtools) would leave the desktop layout scroll-locked and focus-trapped on
  // hidden content, with no visible affordance to close it.
  useEffect(() => {
    const mq = window.matchMedia(MD_BREAKPOINT);
    const handleChange = () => {
      if (mq.matches) setIsOpen(false);
    };
    handleChange();
    mq.addEventListener('change', handleChange);
    return () => mq.removeEventListener('change', handleChange);
  }, []);

  return (
    <SidebarStateContext.Provider value={{ isOpen, open, close, toggle }}>
      {children}
    </SidebarStateContext.Provider>
  );
}

// ── Hook ──────────────────────────────────────────────────────────────────────

export function useSidebarState(): SidebarStateContextValue {
  return useContext(SidebarStateContext);
}
