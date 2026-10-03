'use client';

// ── useAgentsInsights ─────────────────────────────────────────────────────────
//
// Holds the Agents page rollups. The 7-day window arrives with the server
// render. Any other period is fetched when first selected and kept once loaded.

import { useEffect, useMemo, useState } from 'react';
import type { AgentInsightsResponse } from '@orbit/shared/api';
import { fetchAgentInsights } from '../_services/agents-api';
import { toBudgetRows, toPeriodData } from '../_lib/insights';
import type { AgentsInsights, Period, PeriodData } from '../_lib/spend';

type WindowStatus = 'loading' | 'ready' | 'failed';

interface Loaded {
  data: PeriodData;
  budgets: AgentInsightsResponse['budgets'];
}

const loadedOf = (r: AgentInsightsResponse): Loaded => ({
  data: toPeriodData(r.period, r),
  budgets: r.budgets,
});

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
  // A period is a response, or 'failed' once it was asked for and could not be had.
  const [periods, setPeriods] = useState<Partial<Record<Period, Loaded | 'failed'>>>(() =>
    initial ? { [initial.period]: loadedOf(initial) } : {},
  );

  // Only an unasked period is fetched. A failed one is asked again when selected
  // again, because the effect runs on each change of period.
  const current = periods[period];
  const asked = current !== undefined && current !== 'failed';
  useEffect(() => {
    if (asked) return;
    let cancelled = false;
    void fetchAgentInsights(period).then(({ data }) => {
      if (cancelled) return;
      setPeriods((p) => ({ ...p, [period]: data ? loadedOf(data) : 'failed' }));
    });
    return () => {
      cancelled = true;
    };
  }, [period, asked]);

  return useMemo(() => {
    const loaded = (Object.entries(periods) as [Period, Loaded | 'failed'][]).flatMap(([p, v]) =>
      v === 'failed' ? [] : [[p, v] as const],
    );
    const status: WindowStatus = current === 'failed' ? 'failed' : current ? 'ready' : 'loading';
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
  }, [periods, current, agents]);
}
