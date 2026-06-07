'use client';

import { Heading, Stack } from '@orbit/terra';
import { useAnalyticsContext } from '../../_context/analytics-context';
import { AnalyticsDashboard } from '../../_components/analytics/analytics-dashboard';
import { AnalyticsPrincipalsTable } from '../../_components/analytics/analytics-principals-table';

export function PlatformUsageTab() {
  const { data, status, error, days, granularity, setDays, setGranularity } =
    useAnalyticsContext();

  return (
    <Stack gap="comfortable">
      <AnalyticsDashboard
        title="Platform Usage"
        description="Aggregate inference spend across all tenants — cost, tokens, and request volume for the selected period."
        data={data}
        status={status}
        error={error}
        days={days}
        granularity={granularity}
        onSetDays={setDays}
        onSetGranularity={setGranularity}
      />

      <Stack gap="snug">
        <Heading variant="section">Top Spenders</Heading>
        <AnalyticsPrincipalsTable key={days} days={days} />
      </Stack>
    </Stack>
  );
}
