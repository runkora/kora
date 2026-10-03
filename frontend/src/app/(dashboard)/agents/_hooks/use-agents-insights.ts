'use client';

// ── useAgentsInsights ─────────────────────────────────────────────────────────
//
// Holds the Agents page rollups. The 7-day window arrives with the server
// render, so first paint has real data. Any other period is fetched when it is
// first selected, and each period is kept once loaded. Budgets and the tenant
// cap come with every response and do not depend on the window, so the latest
// one wins. A period that fails is left out, so the page renders without it.

import { useEffect, useMemo, useState } from 'react';
import type { AgentInsightsResponse } from '@orbit/shared/api';
import { ApiRoute } from '@orbit/lib/routing/api-routes';
import { toBudgetRows, toPeriodData, toTenantBudget } from '../_lib/insights';
import type { AgentsInsights, Period, PeriodData } from '../_lib/spend';

export type WindowStatus = 'loading' | 'ready' | 'failed';

export interface AgentsInsightsState {
  /** Absent until a period has loaded, so a page whose insights are down shows none of it. */
  insights: AgentsInsights | undefined;
  /** The state of the selected period's figures. */
  status: WindowStatus;
}

interface Loaded {
  data: PeriodData;
  budgets: AgentInsightsResponse['budgets'];
  cap: AgentInsightsResponse['tenant_cap'];
}

/** The rollups for one window, or null when they cannot be had. */
async function fetchAgentInsights(period: Period): Promise<AgentInsightsResponse | null> {
  try {
    const res = await fetch(ApiRoute.agentInsights.window(period), { cache: 'no-store' });
    return res.ok ? ((await res.json()) as AgentInsightsResponse) : null;
  } catch {
    return null;
  }
}

const loadedOf = (r: AgentInsightsResponse): Loaded => ({
  data: toPeriodData(r.period, r),
  budgets: r.budgets,
  cap: r.tenant_cap,
});

export function useAgentsInsights(
  period: Period,
  agents: { slug: string; name: string; identityMode?: string }[],
  enabled: boolean,
  initial?: AgentInsightsResponse | null,
): AgentsInsightsState {
  // A period is a response, or 'failed' once it was asked for and could not be had.
  const [periods, setPeriods] = useState<Partial<Record<Period, Loaded | 'failed'>>>(() =>
    initial ? { [initial.period]: loadedOf(initial) } : {},
  );
  const [latest, setLatest] = useState<Loaded | undefined>(() =>
    initial ? loadedOf(initial) : undefined,
  );

  // Only an unasked period is fetched. A failed one is retried when selected again,
  // because the effect runs on each change of period.
  const current = periods[period];
  const asked = current !== undefined && current !== 'failed';
  useEffect(() => {
    if (!enabled || asked) return;
    let cancelled = false;
    void fetchAgentInsights(period).then((response) => {
      if (cancelled) return;
      const loaded = response ? loadedOf(response) : 'failed';
      setPeriods((p) => ({ ...p, [period]: loaded }));
      if (loaded !== 'failed') setLatest(loaded);
    });
    return () => {
      cancelled = true;
    };
  }, [period, enabled, asked]);

  return useMemo(() => {
    const byPeriod = Object.fromEntries(
      Object.entries(periods).flatMap(([p, v]) => (v && v !== 'failed' ? [[p, v.data]] : [])),
    ) as Partial<Record<Period, PeriodData>>;
    const status: WindowStatus = current === 'failed' ? 'failed' : current ? 'ready' : 'loading';
    if (Object.keys(byPeriod).length === 0) return { insights: undefined, status };

    const rows = latest?.budgets ? toBudgetRows(latest.budgets, agents) : [];
    const tenantBudget = latest ? toTenantBudget(latest.cap, new Date()) : undefined;
    return {
      insights: {
        byPeriod,
        ...(rows.length > 0 ? { budgets: rows } : {}),
        ...(tenantBudget ? { tenantBudget } : {}),
      },
      status,
    };
  }, [periods, current, latest, agents]);
}
