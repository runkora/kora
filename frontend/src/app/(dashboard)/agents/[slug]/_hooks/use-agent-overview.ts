'use client';

// Holds the agent Overview's figures for the selected period.

import { useCallback, useState } from 'react';
import type { AgentOverviewResponse } from '@orbit/shared/api';
import { fetchAgentOverview } from '../_services/agent-detail-api';
import { toAgentOverview } from '../../_lib/overview';
import type { AgentOverview } from '../../_lib/overview';
import type { Period } from '../../_lib/spend';
import { usePeriodCache } from '../../_hooks/use-period-cache';
import type { WindowStatus } from '../../_hooks/use-period-cache';

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
  status: WindowStatus;
} {
  const load = useCallback(
    (p: Period) =>
      slug
        ? fetchAgentOverview(slug, p).then(({ data }) => data && toAgentOverview(data))
        : Promise.resolve(null),
    [slug],
  );
  const [seed] = useState(() =>
    initial ? { period: initial.period, value: toAgentOverview(initial) } : undefined,
  );
  const { periods, status } = usePeriodCache(period, slug ? load : undefined, seed);

  const shown = periods[period] ?? (seed && periods[seed.period]);
  return { overview: shown === 'failed' ? undefined : shown, status };
}
