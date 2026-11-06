'use client';

/**
 * @tier 3 — triggers-owned, with two feature-area consumers: the Triggers page and the agent
 * page's Triggers tab. The range lives in `?window=`, which the Triggers page's server loader
 * reads too.
 */

import { usePathname, useRouter, useSearchParams } from 'next/navigation';
import { parseStatsRange } from '../_types/stats';
import type { StatsRange } from '../_types/stats';

export function useStatsRange(): { range: StatsRange; setRange: (range: string) => void } {
  const router = useRouter();
  const pathname = usePathname();
  const params = useSearchParams();
  const setRange = (range: string) => {
    const next = new URLSearchParams(params.toString());
    next.set('window', parseStatsRange(range));
    router.replace(`${pathname}?${next.toString()}`, { scroll: false });
  };
  return { range: parseStatsRange(params.get('window')), setRange };
}
