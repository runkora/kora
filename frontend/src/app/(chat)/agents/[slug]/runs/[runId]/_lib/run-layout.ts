'use client';

/**
 * @tier 3 — run-page-scoped. The run page's layout, chosen from the page's own width so
 * a framed preview lays out like the device it stands for.
 */
import { createContext, useContext, useLayoutEffect, useRef, useState } from 'react';

/** wide: three panes. laptop: rail only. tablet: thread only, rail as a side sheet. phone: thread, rail as a bottom sheet. */
export type RunLayout = 'wide' | 'laptop' | 'tablet' | 'phone';

export function layoutFor(width: number): RunLayout {
  if (width >= 1280) return 'wide';
  if (width >= 1024) return 'laptop';
  if (width >= 768) return 'tablet';
  return 'phone';
}

/** The layout for the width of the element the returned ref is on, kept current as it resizes. */
export function useOwnLayout(initial: RunLayout = 'wide') {
  const [layout, setLayout] = useState<RunLayout>(initial);
  const ref = useRef<HTMLDivElement>(null);
  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;
    const observer = new ResizeObserver(() => setLayout(layoutFor(el.offsetWidth)));
    observer.observe(el);
    return () => observer.disconnect();
  }, []);
  return [layout, ref] as const;
}

export const RunLayoutContext = createContext<RunLayout>('wide');
export const useRunLayout = () => useContext(RunLayoutContext);
