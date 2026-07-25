'use client';

import { useCallback, useState } from 'react';

import type { AnalyticsResponse, AnalyticsDays, AnalyticsGranularity } from '../_types';
import { fetchAnalyticsPrincipalById } from '../_services/analytics-api';
import type { AnalyticsLoadStatus } from './use-analytics';

// ── usePrincipalAnalytics ─────────────────────────────────────────────────────
//
// Powers the admin drill-down view (GET /v1/analytics/principals/{id}). Unlike
// useAnalyticsMe/useAnalytics (see use-analytics.ts), the principal being
// viewed is fixed for the lifetime of the hook instance — the page remounts
// (key={principalId}) when navigating between principals, rather than this
// hook exposing a setter for it. Only days/granularity are mutable filter
// state, mirroring the rest of the analytics surface.

export interface PrincipalAnalyticsStateShape {
  data: AnalyticsResponse | null;
  status: AnalyticsLoadStatus;
  error: string | null;
  days: AnalyticsDays;
  granularity: AnalyticsGranularity;
  setDays: (days: AnalyticsDays) => void;
  setGranularity: (granularity: AnalyticsGranularity) => void;
  refresh: () => Promise<void>;
}

export function usePrincipalAnalytics(
  principalId: string,
  initialData: AnalyticsResponse | null,
  initialError: string | null = null,
  initialDays?: AnalyticsDays,
): PrincipalAnalyticsStateShape {
  const [data, setData] = useState<AnalyticsResponse | null>(initialData);
  const [status, setStatus] = useState<AnalyticsLoadStatus>(
    initialError ? 'error' : initialData ? 'success' : 'idle',
  );
  const [error, setError] = useState<string | null>(initialError);
  const ssrDays = (initialData?.period_days as AnalyticsDays | undefined) ?? 30;
  const [days, setDaysState] = useState<AnalyticsDays>(initialDays ?? ssrDays);
  const [granularity, setGranularityState] = useState<AnalyticsGranularity>(
    (initialData?.granularity as AnalyticsGranularity) ?? 'daily',
  );

  const load = useCallback(
    async (d: AnalyticsDays, g: AnalyticsGranularity) => {
      setStatus('loading');
      setError(null);
      const result = await fetchAnalyticsPrincipalById(principalId, { days: d, granularity: g });
      if (result.error) {
        setError(result.error);
        setStatus('error');
      } else {
        setData(result.data);
        setStatus('success');
      }
    },
    [principalId],
  );

  const setDays = useCallback(
    (d: AnalyticsDays) => {
      setDaysState(d);
      void load(d, granularity);
    },
    [granularity, load],
  );

  const setGranularity = useCallback(
    (g: AnalyticsGranularity) => {
      setGranularityState(g);
      void load(days, g);
    },
    [days, load],
  );

  const refresh = useCallback(() => load(days, granularity), [days, granularity, load]);

  return { data, status, error, days, granularity, setDays, setGranularity, refresh };
}
