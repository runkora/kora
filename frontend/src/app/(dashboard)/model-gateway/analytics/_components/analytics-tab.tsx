'use client';

import { Heading, NativeSelect, Stack } from '@orbit/terra';
import type { ModelRow } from '../../_types';
import { useAnalyticsContext } from '../../_context/analytics-context';
import { AnalyticsDashboard } from '../../_components/analytics/analytics-dashboard';
import { AnalyticsPrincipalsTable } from '../../_components/analytics/analytics-principals-table';

interface AnalyticsTabProps {
  models: ModelRow[];
}

export function AnalyticsTab({ models }: AnalyticsTabProps) {
  const { data, status, error, days, granularity, modelId, setDays, setGranularity, setModelId } =
    useAnalyticsContext();

  const filterSlot =
    models.length > 0 ? (
      <NativeSelect
          style={{ width: '200px' }}
          value={modelId ?? ''}
          onChange={(e) => setModelId(e.target.value || null)}
          aria-label="Filter by model"
        >
          <option value="">All models</option>
          {models.map((m) => (
            <option key={m.id} value={m.id}>
              {m.name}
            </option>
          ))}
        </NativeSelect>
    ) : undefined;

  return (
    <Stack gap="comfortable">
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
        filterSlot={filterSlot}
      />

      <Stack gap="snug">
        <Heading variant="section">Users</Heading>
        <AnalyticsPrincipalsTable key={days} days={days} />
      </Stack>
    </Stack>
  );
}
