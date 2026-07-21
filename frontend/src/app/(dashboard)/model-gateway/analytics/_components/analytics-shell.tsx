'use client';

import { AnalyticsProvider } from '../../_context/analytics-context';
import { AnalyticsTab } from './analytics-tab';
import type { AnalyticsResponse } from '../../_types';
import type { ModelRow } from '../../_types';

interface AnalyticsShellProps {
  initialData: AnalyticsResponse | null;
  initialError: string | null;
  models: ModelRow[];
  /** Deep-linked tab, e.g. `?tab=principals` from the principal drill-down's back link. */
  initialTab?: 'overview' | 'principals';
}

export function AnalyticsShell({
  initialData,
  initialError,
  models,
  initialTab,
}: AnalyticsShellProps) {
  return (
    <AnalyticsProvider initialData={initialData} initialError={initialError}>
      <AnalyticsTab models={models} initialTab={initialTab} />
    </AnalyticsProvider>
  );
}
