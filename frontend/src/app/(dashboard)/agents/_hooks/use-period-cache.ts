'use client';

// Holds one value per reporting period: the server-rendered one, then each other period once selected.

import { useEffect, useState } from 'react';
import type { Period } from '../_lib/spend';

export type WindowStatus = 'loading' | 'ready' | 'failed';

/**
 * `load` must be stable and returns null on failure; without it nothing is fetched.
 * A period is its value, or 'failed' once it was asked for and could not be had.
 */
export function usePeriodCache<T>(
  period: Period,
  load: ((period: Period) => Promise<T | null>) | undefined,
  initial?: { period: Period; value: T },
): { periods: Partial<Record<Period, T | 'failed'>>; status: WindowStatus } {
  const [periods, setPeriods] = useState<Partial<Record<Period, T | 'failed'>>>(() =>
    initial ? { [initial.period]: initial.value } : {},
  );

  // A failed period is asked again when selected again.
  const current = periods[period];
  const asked = current !== undefined && current !== 'failed';
  useEffect(() => {
    if (asked || !load) return;
    let cancelled = false;
    void load(period).then((value) => {
      if (cancelled) return;
      setPeriods((p) => ({ ...p, [period]: value ?? 'failed' }));
    });
    return () => {
      cancelled = true;
    };
  }, [period, asked, load]);

  return { periods, status: current === 'failed' ? 'failed' : current ? 'ready' : 'loading' };
}
