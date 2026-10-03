'use client';

// ── useAgentsInsights ─────────────────────────────────────────────────────────
//
// Loads the Agents page rollups from the BFF without blocking first render.
// Only the selected period is fetched, when it is first selected, and each
// period is kept once loaded. Budgets load once. A part that fails is left out
// of the result, so the page renders without it.

import { useEffect, useMemo, useRef, useState } from 'react';
import type { AgentBudgetsResponse } from '@orbit/shared/api';
import { fetchAgentBudgets, fetchAgentInsights } from '../_services/agents-insights-api';
import { toBudgetRows, toPeriodData, toTenantBudget } from '../_lib/insights';
import type { AgentsInsights, Period, PeriodData } from '../_lib/spend';

export type WindowStatus = 'loading' | 'ready' | 'failed';

export interface AgentsInsightsState {
  /** Absent until a period has loaded, so a page whose insights are down shows none of it. */
  insights: AgentsInsights | undefined;
  /** The state of the selected period's figures. */
  status: WindowStatus;
}

export function useAgentsInsights(
  period: Period,
  agents: { slug: string; name: string }[],
  enabled: boolean,
): AgentsInsightsState {
  const [periods, setPeriods] = useState<Partial<Record<Period, PeriodData>>>({});
  const [failed, setFailed] = useState<Partial<Record<Period, boolean>>>({});
  const [budgets, setBudgets] = useState<AgentBudgetsResponse | null>(null);

  // Held in refs so the effects start a fetch only for a period that is neither
  // loaded nor already on its way, whatever the render that triggers them.
  const loaded = useRef(new Set<Period>());
  const inFlight = useRef(new Set<Period>());

  useEffect(() => {
    if (!enabled || loaded.current.has(period) || inFlight.current.has(period)) return;
    inFlight.current.add(period);
    void fetchAgentInsights(period).then(({ data }) => {
      inFlight.current.delete(period);
      if (!data) {
        setFailed((f) => ({ ...f, [period]: true }));
        return;
      }
      loaded.current.add(period);
      setFailed((f) => ({ ...f, [period]: false }));
      setPeriods((p) => ({ ...p, [period]: toPeriodData(period, data) }));
    });
  }, [period, enabled]);

  useEffect(() => {
    if (!enabled) return;
    void fetchAgentBudgets().then(({ data }) => setBudgets(data));
  }, [enabled]);

  return useMemo(() => {
    const hasData = Object.keys(periods).length > 0;
    const status: WindowStatus = periods[period] ? 'ready' : failed[period] ? 'failed' : 'loading';
    if (!hasData && !budgets) return { insights: undefined, status };

    const rows = budgets ? toBudgetRows(budgets, agents) : [];
    const tenantBudget = budgets ? toTenantBudget(budgets.tenant_cap, new Date()) : undefined;
    return {
      insights: {
        ...(hasData ? { byPeriod: periods } : {}),
        ...(rows.length > 0 ? { budgets: rows } : {}),
        ...(tenantBudget ? { tenantBudget } : {}),
      },
      status,
    };
  }, [periods, failed, budgets, period, agents]);
}
