'use client';

import { AnalyticsMeProvider } from '../../_context/analytics-context';
import { MyUsageTab } from './my-usage-tab';
import type { AnalyticsResponse } from '../../_types';

interface MyUsageShellProps {
  initialData: AnalyticsResponse | null;
  initialError: string | null;
}

export function MyUsageShell({ initialData, initialError }: MyUsageShellProps) {
  return (
    <AnalyticsMeProvider initialData={initialData} initialError={initialError}>
      <MyUsageTab />
    </AnalyticsMeProvider>
  );
}
