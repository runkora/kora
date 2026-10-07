'use client';

// Holds the Agents page rollups: the server-rendered window, then each other period once selected.

import { useMemo } from 'react';
import type { AgentInsightsResponse } from '@orbit/shared/api';
import { fetchAgentInsights } from '../_services/agents-api';
import { toBudgetRows, toPeriodData } from '../_lib/insights';
import { usePeriodCache } from './use-period-cache';
import type { WindowStatus } from './use-period-cache';
import type { AgentsInsights, Period, PeriodData } from '../_lib/spend';

interface Loaded {
  data: PeriodData;
  budgets: AgentInsightsResponse['budgets'];
}

const loadedOf = (r: AgentInsightsResponse): Loaded => ({
  data: toPeriodData(r.period, r),
  budgets: r.budgets,
});

/** Loads one period's figures; null when the read failed. */
const loadInsights = (period: Period) =>
  fetchAgentInsights(period).then(({ data }) => (data ? loadedOf(data) : null));

export function useAgentsInsights(
  period: Period,
  agents: { slug: string; name: string }[],
  initial?: AgentInsightsResponse | null,
): {
  /** Absent until a period has loaded, so a page whose insights are down shows none of it. */
  insights: AgentsInsights | undefined;
  /** The state of the selected period's figures. */
  status: WindowStatus;
} {
  const { periods, status } = usePeriodCache(
    period,
    loadInsights,
    initial ? { period: initial.period, value: loadedOf(initial) } : undefined,
  );

  return useMemo(() => {
    const loaded = (Object.entries(periods) as [Period, Loaded | 'failed'][]).flatMap(([p, v]) =>
      v === 'failed' ? [] : [[p, v] as const],
    );
    if (loaded.length === 0) return { insights: undefined, status };

    // Budgets do not depend on the window, so any loaded period's will do.
    const rows = toBudgetRows(loaded[0]?.[1].budgets ?? [], agents);
    return {
      insights: {
        byPeriod: Object.fromEntries(loaded.map(([p, v]) => [p, v.data])),
        ...(rows.length > 0 ? { budgets: rows } : {}),
      },
      status,
    };
  }, [periods, status, agents]);
}
