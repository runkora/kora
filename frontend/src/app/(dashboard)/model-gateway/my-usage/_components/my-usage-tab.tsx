'use client';

import { useAnalyticsMeContext } from '../../_context/analytics-context';
import { AnalyticsDashboard } from '../../_components/analytics/analytics-dashboard';

export function MyUsageTab() {
  const { data, status, error, days, granularity, setDays, setGranularity } =
    useAnalyticsMeContext();

  return (
    <AnalyticsDashboard
      title="My Usage"
      description="Your personal inference activity — spend, tokens, and request volume."
      data={data}
      status={status}
      error={error}
      days={days}
      granularity={granularity}
      onSetDays={setDays}
      onSetGranularity={setGranularity}
    />
  );
}
