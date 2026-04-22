'use client';

// ── AnalyticsContext ──────────────────────────────────────────────────────────
//
// Provides analytics state to any component in the analytics page tree without
// prop-drilling. Two separate contexts handle the personal and tenant-wide paths
// so they can be used independently.

import { createContext, useContext } from 'react';

import { useAnalyticsMe, useAnalytics } from '../_hooks/use-analytics';
import type { AnalyticsStateShape } from '../_hooks/use-analytics';
import type { AnalyticsResponse } from '../_types';

// ── Personal analytics context ────────────────────────────────────────────────

const AnalyticsMeContext = createContext<AnalyticsStateShape | null>(null);

interface AnalyticsMeProviderProps {
  initialData: AnalyticsResponse | null;
  initialError: string | null;
  children: React.ReactNode;
}

export function AnalyticsMeProvider({
  initialData,
  initialError,
  children,
}: AnalyticsMeProviderProps) {
  const state = useAnalyticsMe(initialData, initialError);
  return <AnalyticsMeContext.Provider value={state}>{children}</AnalyticsMeContext.Provider>;
}

export function useAnalyticsMeContext(): AnalyticsStateShape {
  const ctx = useContext(AnalyticsMeContext);
  if (!ctx) throw new Error('useAnalyticsMeContext must be used inside <AnalyticsMeProvider>');
  return ctx;
}

// ── Tenant-wide analytics context ─────────────────────────────────────────────

const AnalyticsContext = createContext<AnalyticsStateShape | null>(null);

interface AnalyticsProviderProps {
  initialData: AnalyticsResponse | null;
  initialError: string | null;
  children: React.ReactNode;
}

export function AnalyticsProvider({ initialData, initialError, children }: AnalyticsProviderProps) {
  const state = useAnalytics(initialData, initialError);
  return <AnalyticsContext.Provider value={state}>{children}</AnalyticsContext.Provider>;
}

export function useAnalyticsContext(): AnalyticsStateShape {
  const ctx = useContext(AnalyticsContext);
  if (!ctx) throw new Error('useAnalyticsContext must be used inside <AnalyticsProvider>');
  return ctx;
}
