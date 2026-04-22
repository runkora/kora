'use client';

import { useCallback, useState } from 'react';

import type { AnalyticsResponse, AnalyticsDays, AnalyticsGranularity } from '../_types';
import { fetchAnalyticsMe, fetchAnalytics } from '../_services/analytics-api';

// ── Types ─────────────────────────────────────────────────────────────────────

export type AnalyticsLoadStatus = 'idle' | 'loading' | 'success' | 'error';

export interface AnalyticsStateShape {
  // ── data ──────────────────────────────────────────────────────────────────
  data: AnalyticsResponse | null;
  status: AnalyticsLoadStatus;
  error: string | null;

  // ── filter state ──────────────────────────────────────────────────────────
  days: AnalyticsDays;
  granularity: AnalyticsGranularity;

  // ── actions ───────────────────────────────────────────────────────────────
  setDays: (days: AnalyticsDays) => void;
  setGranularity: (granularity: AnalyticsGranularity) => void;
  refresh: () => Promise<void>;
}

// ── Factory ───────────────────────────────────────────────────────────────────

type FetchFn = (params: {
  days: number;
  granularity: string;
}) => Promise<{ data: AnalyticsResponse | null; error: string | null }>;

function makeUseAnalytics(fetchFn: FetchFn) {
  return function useAnalytics(
    initialData: AnalyticsResponse | null,
    initialError: string | null = null,
  ): AnalyticsStateShape {
    const [data, setData] = useState<AnalyticsResponse | null>(initialData);
    const [status, setStatus] = useState<AnalyticsLoadStatus>(
      initialError ? 'error' : initialData ? 'success' : 'idle',
    );
    const [error, setError] = useState<string | null>(initialError);
    const [days, setDaysState] = useState<AnalyticsDays>(
      (initialData?.period_days as AnalyticsDays) ?? 30,
    );
    const [granularity, setGranularityState] = useState<AnalyticsGranularity>(
      (initialData?.granularity as AnalyticsGranularity) ?? 'daily',
    );

    const load = useCallback(async (d: AnalyticsDays, g: AnalyticsGranularity) => {
      setStatus('loading');
      setError(null);
      const result = await fetchFn({ days: d, granularity: g });
      if (result.error) {
        setError(result.error);
        setStatus('error');
      } else {
        setData(result.data);
        setStatus('success');
      }
    }, []);

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
  };
}

// ── Exported hooks ────────────────────────────────────────────────────────────

export const useAnalyticsMe = makeUseAnalytics(fetchAnalyticsMe);
export const useAnalytics = makeUseAnalytics(fetchAnalytics);
