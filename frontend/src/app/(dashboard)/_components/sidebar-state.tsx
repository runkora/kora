'use client';

/** @tier 3 — dashboard-scoped. Manages mobile sidebar open/close state for the dashboard shell. */

import { createContext, useCallback, useContext, useState, type ReactNode } from 'react';

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
