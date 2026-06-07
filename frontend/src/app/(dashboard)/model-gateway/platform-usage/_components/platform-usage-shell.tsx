'use client';

import { AnalyticsProvider } from '../../_context/analytics-context';
import { PlatformUsageTab } from './platform-usage-tab';
import type { AnalyticsResponse } from '../../_types';

interface PlatformUsageShellProps {
  initialData: AnalyticsResponse | null;
  initialError: string | null;
}

export function PlatformUsageShell({ initialData, initialError }: PlatformUsageShellProps) {
  return (
    <AnalyticsProvider initialData={initialData} initialError={initialError}>
      <PlatformUsageTab />
    </AnalyticsProvider>
  );
}
