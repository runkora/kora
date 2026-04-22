'use client';

import { useAnalyticsContext } from '../../_context/analytics-context';
import { AnalyticsDashboard } from '../../_components/analytics/analytics-dashboard';

export function AnalyticsTab() {
  const { data, status, error, days, granularity, setDays, setGranularity } = useAnalyticsContext();

  return (
    <AnalyticsDashboard
      title="Analytics"
      description="Tenant-wide inference analytics — spend, tokens, and request volume across all principals."
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
