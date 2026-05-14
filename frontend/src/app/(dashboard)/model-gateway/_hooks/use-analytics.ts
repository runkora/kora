'use client';

import { useCallback, useEffect, useRef, useState } from 'react';

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
  tokenId: string | null;
  modelId: string | null;

  // ── actions ───────────────────────────────────────────────────────────────
  setDays: (days: AnalyticsDays) => void;
  setGranularity: (granularity: AnalyticsGranularity) => void;
  setTokenId: (tokenId: string | null) => void;
  setModelId: (modelId: string | null) => void;
  refresh: () => Promise<void>;
}

// ── Factory ───────────────────────────────────────────────────────────────────

type FetchFn = (params: {
  days: number;
  granularity: string;
  tokenId?: string | null;
  modelId?: string | null;
}) => Promise<{ data: AnalyticsResponse | null; error: string | null }>;

function makeUseAnalytics(fetchFn: FetchFn) {
  return function useAnalytics(
    initialData: AnalyticsResponse | null,
    initialError: string | null = null,
    initialDays?: AnalyticsDays,
  ): AnalyticsStateShape {
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
    const [tokenId, setTokenIdState] = useState<string | null>(null);
    const [modelId, setModelIdState] = useState<string | null>(null);

    const load = useCallback(
      async (d: AnalyticsDays, g: AnalyticsGranularity, tid: string | null, mid: string | null) => {
        setStatus('loading');
        setError(null);
        const result = await fetchFn({ days: d, granularity: g, tokenId: tid, modelId: mid });
        if (result.error) {
          setError(result.error);
          setStatus('error');
        } else {
          setData(result.data);
          setStatus('success');
        }
      },
      [],
    );

    // If the preference differs from what the SSR fetch used, reload on mount.
    const didInitFetch = useRef(false);
    useEffect(() => {
      if (didInitFetch.current) return;
      didInitFetch.current = true;
      if (initialDays !== undefined && initialDays !== ssrDays) {
        const initGranularity = (initialData?.granularity as AnalyticsGranularity) ?? 'daily';
        void load(initialDays, initGranularity, null, null);
      }
      // eslint-disable-next-line react-hooks/exhaustive-deps
    }, []); // intentional: fire once on mount with initial values

    const setDays = useCallback(
      (d: AnalyticsDays) => {
        setDaysState(d);
        void load(d, granularity, tokenId, modelId);
      },
      [granularity, tokenId, modelId, load],
    );

    const setGranularity = useCallback(
      (g: AnalyticsGranularity) => {
        setGranularityState(g);
        void load(days, g, tokenId, modelId);
      },
      [days, tokenId, modelId, load],
    );

    const setTokenId = useCallback(
      (tid: string | null) => {
        setTokenIdState(tid);
        void load(days, granularity, tid, modelId);
      },
      [days, granularity, modelId, load],
    );

    const setModelId = useCallback(
      (mid: string | null) => {
        setModelIdState(mid);
        void load(days, granularity, tokenId, mid);
      },
      [days, granularity, tokenId, load],
    );

    const refresh = useCallback(
      () => load(days, granularity, tokenId, modelId),
      [days, granularity, tokenId, modelId, load],
    );

    return {
      data,
      status,
      error,
      days,
      granularity,
      tokenId,
      modelId,
      setDays,
      setGranularity,
      setTokenId,
      setModelId,
      refresh,
    };
  };
}

// ── Exported hooks ────────────────────────────────────────────────────────────

export const useAnalyticsMe = makeUseAnalytics(fetchAnalyticsMe);
export const useAnalytics = makeUseAnalytics(fetchAnalytics);
