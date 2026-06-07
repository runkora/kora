'use client';

import { AnalyticsMeProvider } from '../../_context/analytics-context';
import { MyUsageTab } from './my-usage-tab';
import type { AnalyticsResponse, QuotaStatus } from '../../_types';
import type { PAT } from '@orbit/shared/api';

interface MyUsageShellProps {
  initialData: AnalyticsResponse | null;
  initialError: string | null;
  pats: PAT[];
  initialQuota: QuotaStatus | null;
}

export function MyUsageShell({ initialData, initialError, pats, initialQuota }: MyUsageShellProps) {
  return (
    <AnalyticsMeProvider initialData={initialData} initialError={initialError}>
      <MyUsageTab pats={pats} initialQuota={initialQuota} />
    </AnalyticsMeProvider>
  );
}
