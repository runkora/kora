'use client';

import { AnalyticsProvider } from '../../_context/analytics-context';
import { AnalyticsTab } from './analytics-tab';
import type { AnalyticsResponse } from '../../_types';
import type { ModelRow } from '../../_types';

interface AnalyticsShellProps {
  initialData: AnalyticsResponse | null;
  initialError: string | null;
  models: ModelRow[];
}

export function AnalyticsShell({ initialData, initialError, models }: AnalyticsShellProps) {
  return (
    <AnalyticsProvider initialData={initialData} initialError={initialError}>
      <AnalyticsTab models={models} />
    </AnalyticsProvider>
  );
}
