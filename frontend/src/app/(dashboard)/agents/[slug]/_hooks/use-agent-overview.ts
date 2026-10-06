'use client';

// Holds the agent Overview's figures: the server-rendered window, then each other period once selected.

import { useEffect, useState } from 'react';
import type { AgentOverviewResponse } from '@orbit/shared/api';
import { fetchAgentOverview } from '../_services/agent-detail-api';
import { toAgentOverview } from '../../_lib/overview';
import type { AgentOverview } from '../../_lib/overview';
import type { Period } from '../../_lib/spend';

export type OverviewStatus = 'loading' | 'ready' | 'failed';

export function useAgentOverview(
  slug: string | undefined,
  period: Period,
  initial?: AgentOverviewResponse | null,
): {
  /**
   * The selected period's figures. While it loads, the server-rendered period stays in
   * view; once it fails there are none. Each carries the period it covers.
   */
  overview: AgentOverview | undefined;
  status: OverviewStatus;
} {
  // The period the server rendered, which stays in view while another loads.
  const [seedPeriod] = useState(initial?.period);
  // A period is its figures, or 'failed' once it was asked for and could not be had.
  const [periods, setPeriods] = useState<Partial<Record<Period, AgentOverview | 'failed'>>>(() =>
    initial ? { [initial.period]: toAgentOverview(initial) } : {},
  );

  // A failed period is asked again when selected again.
  const current = periods[period];
  const asked = current !== undefined && current !== 'failed';
  useEffect(() => {
    if (asked || !slug) return;
    let cancelled = false;
    void fetchAgentOverview(slug, period).then(({ data }) => {
      if (cancelled) return;
      setPeriods((p) => ({ ...p, [period]: data ? toAgentOverview(data) : 'failed' }));
    });
    return () => {
      cancelled = true;
    };
  }, [slug, period, asked]);

  const seed = seedPeriod ? periods[seedPeriod] : undefined;
  const shown = current === undefined ? seed : current;
  return {
    overview: shown === 'failed' ? undefined : shown,
    status: current === 'failed' ? 'failed' : current ? 'ready' : 'loading',
  };
}
