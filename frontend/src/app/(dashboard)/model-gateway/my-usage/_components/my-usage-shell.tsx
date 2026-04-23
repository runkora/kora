'use client';

import { AnalyticsMeProvider } from '../../_context/analytics-context';
import { MyUsageTab } from './my-usage-tab';
import type { AnalyticsResponse } from '../../_types';
import type { PAT } from '@orbit/shared/api';

interface MyUsageShellProps {
  initialData: AnalyticsResponse | null;
  initialError: string | null;
  pats: PAT[];
}

export function MyUsageShell({ initialData, initialError, pats }: MyUsageShellProps) {
  return (
    <AnalyticsMeProvider initialData={initialData} initialError={initialError}>
      <MyUsageTab pats={pats} />
    </AnalyticsMeProvider>
  );
}
