'use client';

import { AnalyticsMeProvider } from '../../_context/analytics-context';
import { MyUsageTab } from './my-usage-tab';
import type { AnalyticsResponse, BudgetStatus } from '../../_types';
import type { PAT } from '@orbit/shared/api';

interface MyUsageShellProps {
  initialData: AnalyticsResponse | null;
  initialError: string | null;
  pats: PAT[];
  initialBudget: BudgetStatus | null;
}

export function MyUsageShell({
  initialData,
  initialError,
  pats,
  initialBudget,
}: MyUsageShellProps) {
  return (
    <AnalyticsMeProvider initialData={initialData} initialError={initialError}>
      <MyUsageTab pats={pats} initialBudget={initialBudget} />
    </AnalyticsMeProvider>
  );
}
