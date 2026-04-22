'use client';

import { AnalyticsProvider } from '../../_context/analytics-context';
import { AnalyticsTab } from './analytics-tab';
import type { AnalyticsResponse } from '../../_types';

interface AnalyticsShellProps {
  initialData: AnalyticsResponse | null;
  initialError: string | null;
}

export function AnalyticsShell({ initialData, initialError }: AnalyticsShellProps) {
  return (
    <AnalyticsProvider initialData={initialData} initialError={initialError}>
      <AnalyticsTab />
    </AnalyticsProvider>
  );
}
