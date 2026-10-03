'use client';

// ── useAgentsInsights ─────────────────────────────────────────────────────────
//
// Holds the Agents page rollups. The 7-day window arrives with the server
// render, so first paint has real data. Any other period is fetched when it is
// first selected, and each period is kept once loaded. Budgets and the tenant
// cap come with every response and do not depend on the window, so the latest
// one wins. A part that fails is left out of the result, so the page renders
// without it.

import { useEffect, useMemo, useRef, useState } from 'react';
import type { AgentBudgetStatus, AgentInsightsResponse } from '@orbit/shared/api';
import { fetchAgentInsights } from '../_services/agents-insights-api';
import { toBudgetRows, toPeriodData, toTenantBudget } from '../_lib/insights';
import type { AgentsInsights, Period, PeriodData } from '../_lib/spend';

export type WindowStatus = 'loading' | 'ready' | 'failed';

export interface AgentsInsightsState {
  /** Absent until a period has loaded, so a page whose insights are down shows none of it. */
  insights: AgentsInsights | undefined;
  /** The state of the selected period's figures. */
  status: WindowStatus;
}

/** The parts of a response that do not depend on the window. */
interface Shared {
  budgets: AgentBudgetStatus[] | undefined;
  cap: AgentInsightsResponse['tenant_cap'];
}

const sharedOf = (r: AgentInsightsResponse): Shared => ({ budgets: r.budgets, cap: r.tenant_cap });

export function useAgentsInsights(
  period: Period,
  agents: { slug: string; name: string; identityMode?: string }[],
  enabled: boolean,
  initial?: AgentInsightsResponse | null,
): AgentsInsightsState {
  const [periods, setPeriods] = useState<Partial<Record<Period, PeriodData>>>(() =>
    initial ? { [initial.period]: toPeriodData(initial.period, initial) } : {},
  );
  const [failed, setFailed] = useState<Partial<Record<Period, boolean>>>({});
  const [shared, setShared] = useState<Shared | undefined>(() =>
    initial ? sharedOf(initial) : undefined,
  );

  // Held in refs so the effect starts a fetch only for a period that is neither
  // loaded nor already on its way, whatever the render that triggers it.
  const loaded = useRef(new Set<Period>(initial ? [initial.period] : []));
  const inFlight = useRef(new Set<Period>());
  // A fetch answers whichever period it was for, so a response is dropped only
  // when the page has gone, never when the selection moved on.
  const mounted = useRef(true);
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);

  useEffect(() => {
    if (!enabled || loaded.current.has(period) || inFlight.current.has(period)) return;
    inFlight.current.add(period);
    void fetchAgentInsights(period).then(({ data }) => {
      inFlight.current.delete(period);
      if (!mounted.current) return;
      if (!data) {
        setFailed((f) => ({ ...f, [period]: true }));
        return;
      }
      loaded.current.add(period);
      setFailed((f) => ({ ...f, [period]: false }));
      setPeriods((p) => ({ ...p, [period]: toPeriodData(period, data) }));
      setShared(sharedOf(data));
    });
  }, [period, enabled]);

  return useMemo(() => {
    const hasData = Object.keys(periods).length > 0;
    const status: WindowStatus = periods[period] ? 'ready' : failed[period] ? 'failed' : 'loading';
    if (!hasData) return { insights: undefined, status };

    const rows = shared?.budgets ? toBudgetRows(shared.budgets, agents) : [];
    const tenantBudget = shared ? toTenantBudget(shared.cap, new Date()) : undefined;
    return {
      insights: {
        byPeriod: periods,
        ...(rows.length > 0 ? { budgets: rows } : {}),
        ...(tenantBudget ? { tenantBudget } : {}),
      },
      status,
    };
  }, [periods, failed, shared, period, agents]);
}
